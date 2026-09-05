package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"os/exec"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/util"
)

// ChapterService は配信者が付けた YouTube チャプターを楽曲の入力元にする。
//
// **なぜ 3 つ目の入力元が要るか。** Holodex にも曲が無く、コメントも取れない配信がある
// （実測 599 本中 47 本）。そのうち 20 本にチャプターがあり、14 本は
// `曲名 / アーティスト` が並ぶ完全な歌単だった（約 138 曲）。今はどこからも拾えていない。
//
// 形はコメント経路をそのまま写している：源（chapter_raw）→ 抽出キャッシュ（chapter_songs）
// → 照合は保存せず読み取り時に計算。抽出そのものも `CommentService.ExtractSongs` を
// 共有する ── チャプターは「タイムスタンプ付きのテキスト」でしかないので、
// どの行が曲かの判断を別に持つ理由が無い。
type ChapterService struct {
	streamRepo *repository.StreamRepository
	comments   *CommentService // 抽出・正規化の共通入口
	norm       *NormalizationService
	chatEnd    *ChatEndService
}

// NewChapterService は yt-dlp の実行環境を ChatEndService から借りる。
// **cookie を 2 か所に持たないため。** 管理→設定の差し替えは
// `ChatEndService.SetCookies` の 1 か所に届けば両方に効く ── 別に持つと、
// live chat は通るのにチャプターだけ BOT 判定で落ちる、という切り分けの難しい壊れ方をする。
func NewChapterService(
	streamRepo *repository.StreamRepository,
	comments *CommentService,
	norm *NormalizationService,
	chatEnd *ChatEndService,
) *ChapterService {
	return &ChapterService{streamRepo: streamRepo, comments: comments, norm: norm, chatEnd: chatEnd}
}

// runner は yt-dlp の実行環境（ChatEndService と同じ実体）。
func (s *ChapterService) runner() *ytdlpRunner { return s.chatEnd.ytdlpRunner }

// Chapter は yt-dlp が返す 1 章節。end は**次の章節の開始**なので、
// 「その曲が終わった時刻」ではない（後半の MC や拍手を含む）。
type Chapter struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Title string `json:"title"`
}

// chapterMatchWindow は「抽出された曲」と「元の章節」を突き合わせる許容差（秒）。
// AI は原文の時刻文字列をそのまま写すので普通は 0 秒ずれだが、
// 秒を落として書き戻す実装差に備えて少しだけ緩めてある。
const chapterMatchWindow = 3

// GetChapters は保存済みのチャプターを返す。未取得なら yt-dlp で取りに行って保存する。
func (s *ChapterService) GetChapters(videoID string) ([]Chapter, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}
	if chapters, ok := decodeChapters(stream.ChapterRaw); ok {
		return chapters, nil
	}
	return s.RefreshChapters(videoID)
}

// RefreshChapters は yt-dlp でチャプターを取り直して保存する。
// 章節が 1 つも無い配信では空配列を保存する ── 保存しないと、次に開くたびに
// yt-dlp を呼び直すことになる（無いことも結果のうち）。
func (s *ChapterService) RefreshChapters(videoID string) ([]Chapter, error) {
	chapters, err := s.fetchChapters(videoID)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return nil, fmt.Errorf("marshal chapters: %w", err)
	}
	if err := s.streamRepo.SaveChapterRaw(videoID, util.SanitizeJSONB(raw)); err != nil {
		return nil, fmt.Errorf("save chapter raw: %w", err)
	}
	logger.Infof("[chapter] %s: %d 章節を取得しました", videoID, len(chapters))
	return chapters, nil
}

// AnalyzeChapters は利用者が「チャプターから読み込む」を押した経路。
// 抽出に加えて、決着しなかった行の AI 判定まで行う。
func (s *ChapterService) AnalyzeChapters(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeChapters(videoID, force, true)
}

// AnalyzeChaptersForBatch は一括セットリスト作成用。**AI 判定は行わない**
// （一括は配信をまたいで 1 回にまとめて聞く。docs/SETLIST_FLOW.md）。
//
// **yt-dlp も呼ばない。** 一括は数百本を順に回すので、1 本あたり数秒の取得を挟むと
// 実行時間が桁で変わる。チャプターの取得は `Backfill` で先に済ませておく約束にした
// （コメントの一括プレ分析と同じ分業）。
func (s *ChapterService) AnalyzeChaptersForBatch(videoID string) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeChapters(videoID, false, false)
}

func (s *ChapterService) analyzeChapters(videoID string, force, adjudicate bool) (*dto.AnalyzeCommentsResponse, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}

	chapters, ok := decodeChapters(stream.ChapterRaw)
	if !ok {
		// 未取得のときだけ取りに行く。一括（adjudicate=false）からは呼ばない ──
		// 誰も見ていない配信のために yt-dlp を焚くことになる。
		if !adjudicate {
			return &dto.AnalyzeCommentsResponse{Songs: []dto.CommentSong{}}, nil
		}
		if chapters, err = s.RefreshChapters(videoID); err != nil {
			return nil, err
		}
	} else if force {
		if chapters, err = s.RefreshChapters(videoID); err != nil {
			return nil, err
		}
	}

	rawHash := hashChapters(chapters)

	// キャッシュ命中：chapter_raw が変わっていない → 照合だけ今の DB に対して計算して返す
	if !force && rawHash != "" && len(stream.ChapterSongs) > 0 {
		cachedHash, _ := s.streamRepo.GetChapterSongsHash(videoID)
		if cachedHash.Valid && cachedHash.String == rawHash {
			var cached []dto.CommentSong
			if err := json.Unmarshal(stream.ChapterSongs, &cached); err == nil && len(cached) > 0 {
				s.norm.ResolveForDisplay(cached)
				var ca, cl int
				if adjudicate {
					ca, cl = s.norm.AdjudicateCommentSongs(cached)
				}
				logger.Infof("/chapters/analyze cache hit for %s (%d songs)", videoID, len(cached))
				return &dto.AnalyzeCommentsResponse{Songs: cached, Stats: buildStats("cache", false, false, cached, ca, cl)}, nil
			}
		}
	}

	if len(chapters) == 0 {
		return &dto.AnalyzeCommentsResponse{Songs: []dto.CommentSong{}}, nil
	}

	// 抽出はコメントと同じ関数に通す。章節を 1 通のコメントに組み直して渡すので、
	// 「開始」「幕明け」「スパチャ読み」といった曲でない章節はあちらの判定で落ちる。
	songs, warning, path := s.comments.ExtractSongs([]string{chaptersAsText(chapters)})

	var aliasAsked, aliasLinked int
	if adjudicate {
		aliasAsked, aliasLinked = s.norm.AdjudicateCommentSongs(songs)
	}

	// 章節の end を戻す。**推定値として入れる**（章節の end は次の章節の開始なので、
	// 曲が終わったあとの MC を含む）。IsEndTimeEstimated を立てておくと、
	// 次の拍手検出が上書きしてくれる ── コメントに明記された end と同じ扱いにすると、
	// 拍手で得た確かな値に負けて推定値が残る。
	applyChapterEnds(songs, chapters)

	// 拍手 end（live chat）。ここで埋まったものだけが確かな end になる。
	// chatState は取得の結果で、保存の可否に効く（下を参照）。
	chatState := chatOK
	if s.chatEnd != nil {
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		songs, _, _, chatState = s.chatEnd.DetectEndsForSongs(videoID, duration, songs)
	}

	// 永続化。AI が失敗した回は保存しない（劣化結果をキャッシュに固定しないため）。
	// live chat に到達できなかった回も同じ理由で保存しない（chat_readiness.go）。
	saved := false
	switch {
	case rawHash == "":
	case warning != "":
		logger.Warnf("[chapter] skipping cache write for %s due to AI degradation: %s", videoID, warning)
	case holdCacheForChat(*stream, chatState, time.Now()):
		// コメント経路と同じ理由（chat_readiness.go）。配信直後は replay が
		// 取れず、保存すると hash 命中で拍手検出まで飛ばされ end が固定される。
		logger.Warnf("[chapter] skipping cache write for %s: live chat に到達できず、"+
			"終了から %s 以内なので変換待ちとみなして次回やり直します", videoID, chatRetryWindow)
	default:
		if b, mErr := json.Marshal(stripMatchForStorage(songs)); mErr == nil {
			if err := s.streamRepo.SaveChapterSongs(videoID, b, rawHash); err != nil {
				logger.Warnf("[chapter] save chapter_songs failed (%s): %v", videoID, err)
			} else {
				saved = true
			}
		}
	}

	logger.Infof("[chapter] %s: %d 章節から %d 曲を抽出", videoID, len(chapters), len(songs))
	return &dto.AnalyzeCommentsResponse{
		Songs:   songs,
		Warning: warning,
		Stats:   buildStats(path, false, saved, songs, aliasAsked, aliasLinked),
	}, nil
}

// Backfill はチャプターをまだ取得していない配信を順に取りに行く（同時実行数に上限あり）。
// 一括セットリスト作成の前に流しておくためのもの。
func (s *ChapterService) Backfill(concurrency int) {
	ids, err := s.streamRepo.FindIDsWithoutChapterRaw()
	if err != nil {
		logger.Warnf("[chapter] backfill: list streams failed: %v", err)
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	logger.Infof("[chapter] backfill を開始: %d 件 (concurrency=%d)", len(ids), concurrency)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var done, withChapters int64
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			chapters, err := s.RefreshChapters(id)
			if err != nil {
				logger.Warnf("[chapter] backfill %s: %v", id, err)
			} else if len(chapters) > 0 {
				atomic.AddInt64(&withChapters, 1)
			}
			if n := atomic.AddInt64(&done, 1); n%10 == 0 || int(n) == len(ids) {
				logger.Infof("[chapter] backfill の進捗: %d/%d", n, len(ids))
			}
		}(id)
	}
	wg.Wait()
	logger.Infof("[chapter] backfill が完了: %d 件中 %d 件にチャプターあり", len(ids), atomic.LoadInt64(&withChapters))
}

// fetchChapters は yt-dlp に章節だけを出力させる。
//
// `--print` を使うのは、動画情報の JSON 全体（数百 KB）を読まずに済ませるため。
// 章節が無い動画では "NA" が返る。
func (s *ChapterService) fetchChapters(videoID string) ([]Chapter, error) {
	args := []string{
		"--skip-download",
		"--no-warnings",
		"--socket-timeout", "30",
		// 映像フォーマットは 1 つも要らない。これが無いと、フォーマット一覧が空だった
		// ときに yt-dlp は「Requested format is not available」で止まる（本番の
		// alpine イメージには JS ランタイムが無いので実際に空になる。chatend と同じ理由）。
		"--ignore-no-formats-error",
		"--print", "%(chapters)j",
	}
	// ついでに再生可否を拾う（追加のリクエストは発生しない）。**必ず章節より後ろに置く**
	// ── yt-dlp は --print を指定した順に 1 行ずつ出すので、後ろに足せば
	// 章節 JSON の行を動かさずに済む（読む側も最後の行だけを見る）。
	// この実行はファイルを書かないので --no-simulate は要らない。
	args = append(args, availabilityArgs(false)...)
	if cookiePath, cleanup := s.runner().prepareCookies(); cookiePath != "" {
		defer cleanup()
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.runner().path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// 失敗の理由は名指しで残す（未インストール / BOT 判定 / それ以外で対処が全く違う）
	if runErr != nil {
		switch {
		case notInstalled(runErr):
			return nil, fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.runner().path)
		case ctx.Err() != nil:
			return nil, fmt.Errorf("yt-dlp がタイムアウトしました（2分）")
		case isBotCheck(stderr.String()):
			return nil, botCheckError(s.runner())
		default:
			return nil, fmt.Errorf("yt-dlp に失敗 (%v): %s", runErr, ytdlpErrorLine(stderr.String()))
		}
	}

	// 再生可否は章節が無くても拾えるので、章節の判定より先に保存する。
	s.saveAvailability(videoID, stdout.String())

	out := firstNonEmptyLine(stdout.String())
	if out == "" || out == "NA" || out == "null" {
		return []Chapter{}, nil // 章節の無い動画。これも結果なので空配列で保存する
	}

	var raw []struct {
		Start float64 `json:"start_time"`
		End   float64 `json:"end_time"`
		Title string  `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("yt-dlp の章節を解析できません: %w", err)
	}

	chapters := make([]Chapter, 0, len(raw))
	for _, c := range raw {
		title := strings.TrimSpace(c.Title)
		if title == "" {
			continue
		}
		chapters = append(chapters, Chapter{Start: int(c.Start), End: int(c.End), Title: title})
	}
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].Start < chapters[j].Start })
	return chapters, nil
}

// chaptersAsText は章節を 1 通のコメントに組み直す。
// 抽出側はタイムスタンプ付きのテキストを読むので、この形が共通の入力になる。
func chaptersAsText(chapters []Chapter) string {
	var b strings.Builder
	for _, c := range chapters {
		b.WriteString(formatTimestamp(c.Start))
		b.WriteString(" ")
		b.WriteString(c.Title)
		b.WriteString("\n")
	}
	return b.String()
}

// formatTimestamp は秒を HH:MM:SS / MM:SS に整形する（抽出側が読み取れる形）。
func formatTimestamp(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// applyChapterEnds は抽出された曲に、対応する章節の end を推定値として入れる。
// 既に end を持っている曲（章節名に終了時刻が書かれていた等）は触らない。
func applyChapterEnds(songs []dto.CommentSong, chapters []Chapter) {
	for i := range songs {
		if songs[i].End > 0 && !songs[i].IsEndTimeEstimated {
			continue
		}
		for _, c := range chapters {
			if abs(c.Start-songs[i].Start) > chapterMatchWindow || c.End <= songs[i].Start {
				continue
			}
			songs[i].End = c.End
			songs[i].IsEndTimeEstimated = true
			break
		}
	}
}

// decodeChapters は保存済み chapter_raw を読む。第 2 返り値は「取得済みか」で、
// **空配列でも true**（章節が無いと分かっていることと、まだ調べていないことは別）。
func decodeChapters(raw []byte) ([]Chapter, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var chapters []Chapter
	if err := json.Unmarshal(raw, &chapters); err != nil {
		return nil, false
	}
	return chapters, true
}

// hashChapters は抽出キャッシュの鍵。保存形式ではなく内容から作るので、
// 列の書き方が変わっても同じ章節なら同じ鍵になる。
func hashChapters(chapters []Chapter) string {
	if len(chapters) == 0 {
		return ""
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return ""
	}
	// コメントと同じく抽出規則の版を混ぜる（同じ ExtractSongs を通るので、
	// 規則を変えたら章節のキャッシュも失効させないと古い結果が残る）
	return hashBytes(append(raw, extractionRulesSalt()...))
}

// saveAvailability は yt-dlp の --print 出力から再生可否を拾って保存する
// （ChatEndService と同じ約束：取れなかったときは未調査のまま残す）。
func (s *ChapterService) saveAvailability(videoID, stdout string) {
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
		logger.Warnf("[chapter] %s の再生可否を保存できません: %v", videoID, err)
	}
}
