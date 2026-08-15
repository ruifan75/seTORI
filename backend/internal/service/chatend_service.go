package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/chatend"
)

// ChatEndService は live chat の「拍手」から各曲の終了時刻を検出し、comment_songs を更新する。
type ChatEndService struct {
	streamRepo *repository.StreamRepository
	ytdlp      string
	cacheDir   string

	mu         sync.RWMutex
	cookieData string // cookies.txt の中身（管理画面の設定 / YTDLP_COOKIES_FILE 由来）
}

func NewChatEndService(streamRepo *repository.StreamRepository, ytdlpPath, cacheDir string) *ChatEndService {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	if cacheDir == "" {
		cacheDir = "data/chat_cache"
	}
	// yt-dlp が無いと全配信で live chat が利用できなくなる。配信ごとの警告からは
	// 「その配信にチャットが無い」のか「そもそも実行できていない」のか読み取れないので、
	// 起動時に一度だけ切り分けて残す。
	if _, err := exec.LookPath(ytdlpPath); err != nil {
		logger.Warnf("[chatend] yt-dlp が見つかりません (%s): 拍手による end 推定は全配信でスキップされます。"+
			"インストールしてください（本番イメージは backend/Dockerfile に同梱）", ytdlpPath)
	}
	return &ChatEndService{streamRepo: streamRepo, ytdlp: ytdlpPath, cacheDir: cacheDir}
}

// SetCookies は管理画面で設定された cookies.txt の中身を差し替える（再起動なしで効く）。
func (s *ChatEndService) SetCookies(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookieData = strings.TrimSpace(data)
}

// prepareCookies は yt-dlp に渡す cookie ファイルを用意し、後始末の関数を返す。
// 設定は中身で持っているので毎回一時ファイルへ書き出す。yt-dlp は終了時に
// cookie をファイルへ書き戻すため、共有の実ファイルを直接渡すと
// 読み取り専用マウントや Backfill の並列実行で壊れる。未設定なら空文字を返す。
func (s *ChatEndService) prepareCookies() (string, func()) {
	noop := func() {}

	s.mu.RLock()
	data := s.cookieData
	s.mu.RUnlock()

	if data == "" {
		return "", noop
	}

	tmp, err := os.CreateTemp("", "setori-cookies-*.txt")
	if err != nil {
		logger.Warnf("[chatend] cookie の一時ファイルを作れません: %v", err)
		return "", noop
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(data + "\n"); err != nil {
		tmp.Close()
		os.Remove(name)
		logger.Warnf("[chatend] cookie の書き出しに失敗しました: %v", err)
		return "", noop
	}
	tmp.Close()
	return name, func() { os.Remove(name) }
}

// HasCookies は cookie が設定されているかを返す（失敗時の案内を出し分けるため）。
func (s *ChatEndService) HasCookies() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cookieData != ""
}

// AnalyzeResult は AnalyzeStream の結果（UI に何が起きたかを伝えるため）。
type AnalyzeResult struct {
	Total   int `json:"total"`   // 対象になった曲数
	Filled  int `json:"filled"`  // end が空だったので拍手 end で埋めた曲数
	Changed int `json:"changed"` // 実際に書き換わった曲数（ChatEnd の記録だけの曲も含む）
}

// AnalyzeStream は live chat をダウンロードし、拍手による終了時刻を検出して comment_songs の end を更新する。
// comment_songs にある start を入力とし、各曲の曲末拍手を end として探す。
func (s *ChatEndService) AnalyzeStream(videoID string) (AnalyzeResult, error) {
	var res AnalyzeResult

	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return res, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil || len(stream.CommentSongs) == 0 {
		return res, nil
	}

	var songs []dto.CommentSong
	if err := json.Unmarshal(stream.CommentSongs, &songs); err != nil || len(songs) == 0 {
		return res, nil
	}

	var duration int
	if stream.DurationSeconds.Valid {
		duration = int(stream.DurationSeconds.Int32)
	}

	songs, filled, changed := s.DetectEndsForSongs(videoID, duration, songs)
	res = AnalyzeResult{Total: len(songs), Filled: filled, Changed: changed}
	// 既に end があった曲でも ChatEnd / EndDiff は保存する。値そのものは変えないが、
	// コメントの end と拍手の end がずれている曲を UI で拾えるようにするため
	// （filled だけを保存条件にしていた頃は、この差分が毎回捨てられていた）。
	if changed == 0 {
		return res, nil
	}

	raw, err := json.Marshal(songs)
	if err != nil {
		return res, fmt.Errorf("marshal comment songs: %w", err)
	}
	// 解析結果の欄だけを書く。hash は据え置く（抽出元のコメントは変えていない）。
	// 行まるごと書き戻すと、拍手 end の付与が title や is_hidden まで巻き込む。
	if err := s.streamRepo.UpdateCommentSongs(videoID, raw); err != nil {
		return res, fmt.Errorf("update comment songs: %w", err)
	}
	logger.Infof("[chatend] %s: %d/%d 曲の拍手終了を取得（%d 曲を更新）", videoID, filled, len(songs), changed)
	return res, nil
}

// DetectEnds は指定された start 秒数に対して live chat の拍手から曲末 end を検出し、start→end の対応を返す。
// DB には書かず、comment / holodex の両分析フローで共用する。live chat を使えなければ空の map を返す。
func (s *ChatEndService) DetectEnds(videoID string, durationSeconds int, starts []int) map[int]int {
	if len(starts) == 0 {
		return nil
	}
	chatPath, err := s.fetchLiveChat(videoID)
	if err != nil {
		logger.Warnf("[chatend] %s: live chat を利用できないため、end の推定をスキップ: %v", videoID, err)
		return nil
	}
	events, err := chatend.ParseLiveChatFile(chatPath)
	if err != nil {
		logger.Warnf("[chatend] %s: live chat の解析に失敗: %v", videoID, err)
		return nil
	}

	fstarts := make([]float64, len(starts))
	for i, st := range starts {
		fstarts[i] = float64(st)
	}
	ends := chatend.DetectEnds(fstarts, events, float64(durationSeconds), chatend.DefaultOptions())
	endByStart := make(map[int]int, len(ends))
	for _, e := range ends {
		if e.End != nil {
			endByStart[int(e.Start)] = int(*e.End)
		}
	}
	return endByStart
}

// DetectEndsForSongs は comment songs に拍手による end 検出を適用する。
// 方針（利用者の希望に合わせる）：
// - 元から explicit end（コメントにある範囲の終了時刻）があれば保持し、Chat の検出値を ChatEnd に入れる。
// - 元の explicit end がない場合だけ Chat の検出値を使う。
// - EndDiff も計算し、差が大きい場合にフロントエンドで確認を促せるようにする。
//
// 戻り値は (songs, filled, changed)。filled は end を埋めた曲数、
// changed は ChatEnd/EndDiff だけの記録も含めた「実際に書き換わった」曲数で、
// 呼び出し側が保存の要否を判断するのに使う。
func (s *ChatEndService) DetectEndsForSongs(videoID string, durationSeconds int, songs []dto.CommentSong) ([]dto.CommentSong, int, int) {
	if len(songs) == 0 {
		return songs, 0, 0
	}
	starts := make([]int, len(songs))
	for i, sg := range songs {
		starts[i] = sg.Start
	}
	endByStart := s.DetectEnds(videoID, durationSeconds, starts)
	filled, changed := 0, 0

	for i := range songs {
		chatEnd, hasChat := endByStart[songs[i].Start]
		if !hasChat {
			continue
		}

		hasExplicitEnd := songs[i].End > 0 && !songs[i].IsEndTimeEstimated

		if hasExplicitEnd {
			// コメントの explicit end は保持し、Chat の提案値だけを記録する
			diff := abs(songs[i].End - chatEnd)
			if songs[i].ChatEnd != chatEnd || songs[i].EndDiff != diff {
				changed++ // 同じ値なら書き戻さない（再実行しても無駄な UPDATE を出さない）
			}
			songs[i].ChatEnd = chatEnd
			songs[i].EndDiff = diff
			// End と IsEndTimeEstimated は変更しない
		} else {
			// 元に明確な end がなければ Chat の値を使う
			songs[i].End = chatEnd
			songs[i].IsEndTimeEstimated = false
			songs[i].ChatEnd = chatEnd // フロントエンドへ Chat の値だと伝える
			filled++
			changed++
		}
	}
	return songs, filled, changed
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Backfill は comment_songs を持つすべての歌枠で拍手 end を検出する（同時実行数に上限あり）。
// 既存データの補完向け。ダウンロード済みの live chat はキャッシュを使うため、再実行は軽い。
func (s *ChatEndService) Backfill(concurrency int) {
	ids, err := s.streamRepo.FindIDsWithCommentSongs()
	if err != nil {
		logger.Warnf("[chatend] backfill: list streams failed: %v", err)
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	logger.Infof("[chatend] backfill を開始: %d 件 (concurrency=%d)", len(ids), concurrency)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var done int64
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := s.AnalyzeStream(id); err != nil {
				logger.Warnf("[chatend] backfill %s: %v", id, err)
			}
			if n := atomic.AddInt64(&done, 1); n%10 == 0 || int(n) == len(ids) {
				logger.Infof("[chatend] backfill の進捗: %d/%d", n, len(ids))
			}
		}(id)
	}
	wg.Wait()
	logger.Infof("[chatend] backfill が完了: %d 件", len(ids))
}

// fetchLiveChat は yt-dlp で live chat replay をダウンロードする（取得済みならキャッシュを使う）。
func (s *ChatEndService) fetchLiveChat(videoID string) (string, error) {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	base := filepath.Join(s.cacheDir, videoID)
	chat := base + ".live_chat.json"
	if _, err := os.Stat(chat); err == nil {
		return chat, nil
	}

	args := []string{
		"--skip-download", "--write-subs", "--sub-langs", "live_chat",
		"--socket-timeout", "30",
		// 映像フォーマットは 1 つも要らない（欲しいのは live_chat の字幕だけ）。
		// これが無いと、フォーマット一覧が空だったときに yt-dlp は
		// 「Requested format is not available」で字幕を書く前に止まる。
		// 新しい yt-dlp は YouTube の抽出に JS ランタイムを要求するようになり、
		// ランタイムの無い環境（本番の alpine イメージ）では実際に空になる。
		"--ignore-no-formats-error",
		"-o", base + ".%(ext)s",
	}
	// cookie があれば渡す。YouTube に BOT 判定されている環境ではこれが無いと落ちる。
	if cookiePath, cleanup := s.prepareCookies(); cookiePath != "" {
		defer cleanup()
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ytdlp, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// 警告だけで終了コードが非 0 になることがあるので、まずファイルの有無で判断する。
	if _, err := os.Stat(chat); err == nil {
		return chat, nil
	}

	// ここから先は失敗の理由を残す。yt-dlp 未インストール・BOT 判定・単に
	// チャットが無い配信は対処が全く違うのに、以前はどれも同じ文言になっていた。
	switch {
	// PATH 上の名前なら ErrNotFound、絶対パスなら fs.ErrNotExist で返る
	case errors.Is(runErr, exec.ErrNotFound), errors.Is(runErr, fs.ErrNotExist):
		return "", fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.ytdlp)
	case ctx.Err() != nil:
		return "", fmt.Errorf("yt-dlp がタイムアウトしました（3分）")
	case isBotCheck(stderr.String()):
		// 全配信で一様に起きるので、原因と対処をここで名指ししておく
		if s.HasCookies() {
			return "", fmt.Errorf("YouTube に BOT 判定されました: 設定済みの cookie が失効している可能性があります（管理→設定で入れ直してください）")
		}
		return "", fmt.Errorf("YouTube に BOT 判定されました: 管理→設定の「YouTube cookie」に cookies.txt を登録してください")
	case runErr != nil:
		return "", fmt.Errorf("yt-dlp に失敗 (%v): %s", runErr, ytdlpErrorLine(stderr.String()))
	}
	return "", fmt.Errorf("この配信に live chat replay がありません")
}

// isBotCheck は yt-dlp の出力が YouTube の BOT 判定かどうかを見る。
func isBotCheck(stderr string) bool {
	return strings.Contains(stderr, "Sign in to confirm you") ||
		strings.Contains(stderr, "confirm you’re not a bot") ||
		strings.Contains(stderr, "confirm you're not a bot")
}

// ytdlpErrorLine は stderr から原因の行だけを取り出す（ログを 1 行に収めるため）。
func ytdlpErrorLine(stderr string) string {
	line := ""
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "ERROR:") {
			line = l // ERROR 行があればそれを優先（最後のものを採る）
		} else if line == "" {
			line = l
		}
	}
	if line == "" {
		return "(stderr なし)"
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}

// EstimateEnds は任意の開始秒数リストに対する拍手 end 推定を返す（編集ページの単曲追加用）。
// live chat はローカルキャッシュされるため、2回目以降は安価。
func (s *ChatEndService) EstimateEnds(videoID string, starts []int) (map[int]int, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}
	var duration int
	if stream.DurationSeconds.Valid {
		duration = int(stream.DurationSeconds.Int32)
	}
	return s.DetectEnds(videoID, duration, starts), nil
}
