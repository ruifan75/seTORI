package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	*ytdlpRunner // yt-dlp の場所と cookie。ChapterService と同じ実体を共有する

	streamRepo *repository.StreamRepository
	cacheDir   string
}

func NewChatEndService(streamRepo *repository.StreamRepository, ytdlpPath, cacheDir string) *ChatEndService {
	if cacheDir == "" {
		cacheDir = "data/chat_cache"
	}
	return &ChatEndService{ytdlpRunner: newYtdlpRunner(ytdlpPath), streamRepo: streamRepo, cacheDir: cacheDir}
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

	// この経路は拍手 end の付与そのものが目的で、到達できなければ 0 件になるだけ。
	// 抽出結果のキャッシュは触らないので、到達可否で分岐する必要は無い。
	songs, filled, changed, _ := s.DetectEndsForSongs(videoID, duration, songs)
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
// usableLiveChatFile はそのファイルを live chat として使ってよいかを見る。
//
// **取得の前後で同じ関数を通すこと。** キャッシュ側にだけ検証を置いていた頃は、
// yt-dlp が小さい途中ファイルを残して失敗しても、存在するというだけで
// 成功として返っていた（runErr や transient の判定より先に）。
//
// ここでの判定はサイズだけの**安い前濾し**で、有効性の根拠ではない。
// 中身が replay かどうかは解析時に見る（ParseLiveChatFile の 2 つ目の戻り値）。
func usableLiveChatFile(path, videoID, label string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Size() >= minLiveChatBytes {
		return true
	}
	logger.Warnf("[chatend] %s: live chat の%sが小さすぎます (%d bytes)。取り直します",
		videoID, label, info.Size())
	_ = os.Remove(path)
	return false
}

// DetectEnds は拍手 end を返す。**2 つ目の戻り値は取得の結果（3 態）。**
//
// 「取れなかった」と「取れたが拍手が無かった」は**どちらも空の結果**になるので、
// 戻り値で区別しないと呼び出し側では見分けられない。配信直後は YouTube の変換が
// 終わっておらず replay をしばらく取得できないため、混同すると
// 「まだ取れないだけ」の配信を「拍手が無い配信」として確定させてしまう。
func (s *ChatEndService) DetectEnds(videoID string, durationSeconds int, starts []int) (map[int]int, chatOutcome) {
	if len(starts) == 0 {
		return nil, chatOK
	}
	chatPath, outcome, err := s.fetchLiveChat(videoID)
	if err != nil {
		logger.Warnf("[chatend] %s: live chat を利用できないため、end の推定をスキップ: %v", videoID, err)
		return nil, outcome
	}
	events, recognized, err := chatend.ParseLiveChatFile(chatPath)
	if err != nil {
		// ファイルは取れたが読めない（途中で切れた等）。**結論にはしない。**
		logger.Warnf("[chatend] %s: live chat の解析に失敗: %v", videoID, err)
		return nil, chatTransientError
	}
	// **サイズは有効性の根拠にならない。** 十分に長くても中身が replay でなければ、
	// パーサは全行を読み飛ばして「0 件・エラー無し」を返す ── それを
	// 「拍手が無かった」と結論すると、壊れたキャッシュのまま確定してしまう。
	// 記録を 1 つも認識できなければ、消してから次回取り直す。
	if !recognized {
		logger.Warnf("[chatend] %s: live chat replay として読めませんでした。キャッシュを消して取り直します", videoID)
		_ = os.Remove(chatPath)
		return nil, chatTransientError
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
	return endByStart, chatOK
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
// DetectEndsForSongs は comment songs に拍手 end を適用する。
// 4 つ目の戻り値は取得の結果（3 態。DetectEnds の注記を参照）。
func (s *ChatEndService) DetectEndsForSongs(videoID string, durationSeconds int, songs []dto.CommentSong) ([]dto.CommentSong, int, int, chatOutcome) {
	if len(songs) == 0 {
		return songs, 0, 0, chatOK
	}
	starts := make([]int, len(songs))
	for i, sg := range songs {
		starts[i] = sg.Start
	}
	endByStart, outcome := s.DetectEnds(videoID, durationSeconds, starts)
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
	return songs, filled, changed, outcome
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
func (s *ChatEndService) fetchLiveChat(videoID string) (string, chatOutcome, error) {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return "", chatTransientError, fmt.Errorf("create cache dir: %w", err)
	}
	base := filepath.Join(s.cacheDir, videoID)
	chat := base + ".live_chat.json"
	// **存在するだけでは証拠にならない。** 途中で切れた／空のファイルが残っていると、
	// ParseLiveChatFile は壊れた行を黙って読み飛ばすので「0 件・エラー無し」になり、
	// 「拍手が無かった」という結論として確定してしまう。しかもキャッシュなので
	// force 分析でも同じファイルを読み直し、二度と回復しない。
	if usableLiveChatFile(chat, videoID, "キャッシュ") {
		return chat, chatOK, nil
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
	// ついでに再生可否を拾う（追加のリクエストは発生しない）。この実行は
	// live chat をファイルへ書くので --no-simulate が要る（ytdlp.go の注意書き）。
	args = append(args, availabilityArgs(true)...)
	// cookie があれば渡す。YouTube に BOT 判定されている環境ではこれが無いと落ちる。
	if cookiePath, cleanup := s.prepareCookies(); cookiePath != "" {
		defer cleanup()
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// 再生可否は chat の取得に失敗していても拾えることがある（会限は
	// 「chat は取れないが subscriber_only とは分かる」）ので、先に保存する。
	s.saveAvailability(videoID, stdout.String())

	// 警告だけで終了コードが非 0 になることがあるので、まずファイルの有無で判断する。
	// **キャッシュと同じ検証を通す。** 存在だけを見ると、yt-dlp が途中まで書いて
	// 失敗したファイルが runErr や transient の判定より先に成功として返る。
	if usableLiveChatFile(chat, videoID, "取得したファイル") {
		return chat, chatOK, nil
	}

	// ここから先は失敗の理由を残す。yt-dlp 未インストール・BOT 判定・単に
	// チャットが無い配信は対処が全く違うのに、以前はどれも同じ文言になっていた。
	//
	// **「replay が無い」と「一時的に取れなかった」を戻り値でも分ける。**
	// 呼び出し側は前者を結論として扱ってよいが、後者から結論すると
	// 障害の最中に解析した配信が「チャットの無い配信」として固定される。
	switch {
	case notInstalled(runErr):
		return "", chatTransientError, fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.path)
	case ctx.Err() != nil:
		return "", chatTransientError, fmt.Errorf("yt-dlp がタイムアウトしました（3分）")
	case isBotCheck(stderr.String()):
		// 全配信で一様に起きるので、原因と対処をここで名指ししておく
		return "", chatTransientError, botCheckError(s.ytdlpRunner)
	case runErr != nil:
		return "", chatTransientError, fmt.Errorf("yt-dlp に失敗 (%v): %s", runErr, ytdlpErrorLine(stderr.String()))
	}
	// **終了コード 0 でも「取れなかっただけ」がある。** `--ignore-no-formats-error`
	// を付けているので、レート制限や一時的な不可視は警告へ降格し、runErr は nil の
	// ままファイルだけが無い状態になる。しかもレート制限の stderr は
	// `Video unavailable` で始まるので、素朴に見ると「消えた動画」と読める
	// （availability_service.go の isTransientFailure の注記）。
	// ここを chatNoReplay に落とすと、古い配信では結論として保存されてしまう。
	if isTransientFailure(stderr.String()) {
		return "", chatTransientError, fmt.Errorf("live chat を取得できませんでした（一時的な失敗）: %s",
			ytdlpErrorLine(stderr.String()))
	}

	// yt-dlp は正常終了したがファイルが無い。**まだ変換中の可能性がある**ので、
	// 「この配信にはチャットが無い」と言い切れるのは配信から十分に時間が
	// 経っているときだけ（chat_readiness.go）。
	return "", chatNoReplay, fmt.Errorf("live chat replay が見つかりません（変換待ちの可能性があります）")
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
	ends, _ := s.DetectEnds(videoID, duration, starts)
	return ends, nil
}

// saveAvailability は yt-dlp の --print 出力から再生可否を拾って保存する。
// 取れなかったときは何も書かない（未調査のまま残す ── 「調べたが不明」を
// 記録すると、次に cookie を入れたときの調べ直しが対象から漏れる）。
func (s *ChatEndService) saveAvailability(videoID, stdout string) {
	a := parseYtdlpAvailability(lastNonEmptyLine(stdout))
	// **抽出が最後まで通ったときだけ保存する**（Resolved の注意書き）。この経路は
	// --ignore-no-formats-error を付けているので、レート制限や削除でも終了コード 0 で
	// availability=public が返る。ここで保存すると、一時的にレート制限へ当たった
	// 公開配信が unavailable として恒久的に記録され、二度と調べ直されない。
	if !a.Resolved() {
		return
	}
	var avail sql.NullString
	if a.Availability != "" {
		avail = sql.NullString{String: a.Availability, Valid: true}
	}
	if err := s.streamRepo.SaveAvailability(videoID, avail, a.PlayableInEmbed); err != nil {
		logger.Warnf("[chatend] %s の再生可否を保存できません: %v", videoID, err)
	}
}
