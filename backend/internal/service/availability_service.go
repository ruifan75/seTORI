package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

// AvailabilityService は配信が実際に再生できるかを yt-dlp で調べる。
//
// live chat とチャプターの取得にも相乗りさせてあるが（それぞれの fetch を参照）、
// **それだけでは埋まらない**：
//   - live chat はファイルキャッシュがあると yt-dlp を呼ぶ前に return する
//   - チャプターの backfill は is_hidden = FALSE に限定されている
//
// 判定したい会限の歌枠はどれも非表示側にあるので、専用の経路が要る。
type AvailabilityService struct {
	*ytdlpRunner // ChatEndService と**同じ実体**を共有する（cookie を 2 か所に持たない）
	streamRepo   *repository.StreamRepository
}

func NewAvailabilityService(streamRepo *repository.StreamRepository, chatEnd *ChatEndService) *AvailabilityService {
	return &AvailabilityService{ytdlpRunner: chatEnd.ytdlpRunner, streamRepo: streamRepo}
}

// Fetch は 1 配信の再生可否を調べて保存する。
//
// **`--ignore-no-formats-error` は付けない。** 他の経路はフォーマット一覧が空でも
// 続行したいので付けているが、その副作用で視聴できない動画まで
// availability = public で返ってくる。ここは再生可否そのものを知りたいので、
// 動画情報を取れないことを**エラーとして受け取る**（保存はする ── 「調べた結果、
// 取れなかった」も事実なので、未調査のまま毎回調べ直すのは避ける）。
func (s *AvailabilityService) Fetch(videoID string) (ytdlpAvailability, error) {
	args := []string{
		"--skip-download",
		"--no-warnings",
		"--socket-timeout", "30",
		"--print", availabilityPrintTemplate,
	}
	// **HasCookies() ではなく実際に渡せたかを見る。** prepareCookies は一時ファイルの
	// 作成・書き込みに失敗すると空パスを返すので、設定済みでも cookie 無しで走ることがある。
	cookiesSent := false
	if cookiePath, cleanup := s.prepareCookies(); cookiePath != "" {
		defer cleanup()
		args = append(args, "--cookies", cookiePath)
		cookiesSent = true
	}
	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	a := parseYtdlpAvailability(lastNonEmptyLine(stdout.String()))

	if runErr != nil {
		switch {
		case notInstalled(runErr):
			return a, fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.path)
		case ctx.Err() != nil:
			return a, fmt.Errorf("yt-dlp がタイムアウトしました（2分）")
		case isBotCheck(stderr.String()):
			// BOT 判定は全配信で一様に起きる。値を保存すると「調べ済み」になって
			// cookie を入れ直したあとの調べ直しから漏れるので、保存せずに返す。
			return a, botCheckError(s.ytdlpRunner)
		}
		// ここから先は「動画が無い」と「今回の実行が失敗した」の区別が要る。
		//
		// **実行が失敗しただけのものを記録しないこと。** 記録すると
		// availability_checked_at が立ち、`playabilityOf` は unavailable を返して
		// 公開配信のプレイヤーを消し、`FindIDsWithoutAvailability` は二度と拾わない。
		// 740 件の backfill 中に 429 や一時的な通信障害が起きれば、残り全部が
		// まとめて誤分類される（実測：到達できない proxy を挟むと、公開動画の
		// wB3qGgT1XIQ が exit=1・stdout 空で返る ── 削除済みと同じ形）。
		//
		// 記録するのは**「動画が無い」と読める応答が返ったときだけ**。
		// 文言に依存するのは避けたかったが、ここでは**外したときに安全側へ倒れる**
		// ── 一致しなければ未調査のまま残り、次の backfill が拾い直す。
		// （会限を文字列で見分けるのは逆に危険なので、そちらは cookie を入れて
		// subscriber_only という構造化された値で受け取る。）
		if !cookiesSent {
			// cookie 無しでは「削除された」と「会限で見えない」が同じ失敗になる。
			// 会限の歌枠に「削除された可能性があります」と出さないよう、記録しない。
			return a, fmt.Errorf("動画情報を取得できません（cookie 無しのため会限と区別できず、判定は保存しません）: %s",
				ytdlpErrorLine(stderr.String()))
		}
		switch classifyFetchFailure(stderr.String()) {
		case failureTransient:
			return a, fmt.Errorf("一時的な失敗のため保存しません（時間をおいて再実行してください）: %s",
				ytdlpErrorLine(stderr.String()))
		case failureUnknown:
			return a, fmt.Errorf("動画情報を取得できません（原因を判別できないため保存しません。再実行してください）: %s",
				ytdlpErrorLine(stderr.String()))
		}
		// failureVideoGone。availability は空、playable_in_embed は NULL。
		if err := s.save(videoID, a); err != nil {
			return a, err
		}
		return a, fmt.Errorf("動画情報を取得できません: %s", ytdlpErrorLine(stderr.String()))
	}

	// 終了コード 0 でも中身が空なら保存しない（NULL/NULL を書くと unavailable になる）。
	if !a.Resolved() && a.Availability == "" {
		return a, fmt.Errorf("yt-dlp が再生可否を返しませんでした（保存しません）")
	}
	if err := s.save(videoID, a); err != nil {
		return a, err
	}
	return a, nil
}

func (s *AvailabilityService) save(videoID string, a ytdlpAvailability) error {
	var avail sql.NullString
	if a.Availability != "" {
		avail = sql.NullString{String: a.Availability, Valid: true}
	}
	if err := s.streamRepo.SaveAvailability(videoID, avail, a.PlayableInEmbed); err != nil {
		return fmt.Errorf("save availability: %w", err)
	}
	return nil
}

// Backfill は未調査の配信をまとめて調べる（非同期。進捗はログに出す）。
// 対象に非表示を含めるのは FindIDsNeedingAvailability の注意書きのとおり。
//
// recheckWeak を立てると、`public` で確定している行も調べ直す。
// **`public` は「反証が無かった」という結論であって、公開だと確かめた証拠ではない**
// ── yt-dlp は会限を決める badge が取れなかった場合も public を出す。
//
// **実際に使う並列数を返す。** 丸めた値を呼び出し側へ返さないと、応答は
// 要求どおりの数を示しているのに実際は 8 で走る、という食い違いになる。
func (s *AvailabilityService) Backfill(concurrency int, recheckWeak bool) (targets, effectiveConcurrency int, err error) {
	ids, err := s.streamRepo.FindIDsNeedingAvailability(recheckWeak)
	if err != nil {
		return 0, 0, err
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 8 {
		concurrency = 8
	}
	if len(ids) == 0 {
		return 0, concurrency, nil
	}

	go func() {
		logger.Infof("[availability] backfill を開始: %d 件 (並列 %d)", len(ids), concurrency)
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var done int64
		for _, id := range ids {
			sem <- struct{}{}
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()
				if _, err := s.Fetch(id); err != nil {
					logger.Warnf("[availability] backfill %s: %v", id, err)
				}
				if n := atomic.AddInt64(&done, 1); n%20 == 0 || int(n) == len(ids) {
					logger.Infof("[availability] backfill の進捗: %d/%d", n, len(ids))
				}
			}(id)
		}
		wg.Wait()
		logger.Infof("[availability] backfill が完了: %d 件", len(ids))
	}()

	return len(ids), concurrency, nil
}

// failureKind は取得に失敗したときの扱い。
type failureKind int

const (
	// failureTransient … 今回はたまたま取れなかった。記録せず、次の backfill に任せる。
	failureTransient failureKind = iota
	// failureVideoGone … 動画そのものが無い。unavailable として記録してよい。
	failureVideoGone
	// failureUnknown … どちらとも読めない。記録しない（安全側）。
	failureUnknown
)

// classifyFetchFailure は stderr から失敗の種類を決める。
//
// **判定の順序がこの関数の中身そのもの。** 一時障害と消失は同じ文字列で来ることがあるので
// （下記）、一時障害を先に見る。呼び出し側が 2 つの述語を順に呼ぶ形にしていると、
// 順序を入れ替えても型は通りテストも書きにくいので、決定をここに閉じ込めてある。
func classifyFetchFailure(stderr string) failureKind {
	if isTransientFailure(stderr) {
		return failureTransient
	}
	if isVideoGone(stderr) {
		return failureVideoGone
	}
	return failureUnknown
}

// isTransientFailure は「今回はたまたま取れなかった」を見る。
// **単独で使わないこと**（classifyFetchFailure を通す）。
//
// **レート制限は "Video unavailable" で始まる。** YouTube が返す reason が
// `Video unavailable`、subreason が `This content isn't available, try again later` で、
// yt-dlp はこれを連結してから rate-limited の案内を足す
// （`extractor/youtube/_video.py`。wiki: Extractors#this-content-isnt-available-try-again-later）。
// つまり **"Video unavailable" だけで消失と判定すると、レート制限に当たった公開配信を
// 恒久的に unavailable として記録する**。backfill は 700 件超を並列で回すので、
// これは起きにくい事故ではなく、起こしにいく事故になる。
func isTransientFailure(stderr string) bool {
	for _, marker := range []string{
		"try again later",
		"rate-limited",
		"HTTP Error 429",
		"Too Many Requests",
		"Unable to download", // API ページ・webpage の取得失敗（通信障害）
		"Unable to connect",  // proxy / DNS
		"timed out",
		"Temporary failure",
		"captcha",            // captcha を要求されている＝この実行が通らないだけ
		"Sign in to confirm", // BOT 判定（呼び出し側でも見ているが、ここでも落とす）
	} {
		if strings.Contains(stderr, marker) {
			return true
		}
	}
	return false
}

// isVideoGone は stderr が「その動画はもう無い」と言っているかを見る。
//
// **一致しなければ保存しない**ので、文言が変わったときは「未調査のまま」に倒れる。
// 誤って unavailable と記録するより、次の backfill で調べ直せるほうがよい。
// 実測（yt-dlp 2026.03.17 / 2026.08.19）：
//
//	削除・非公開      ERROR: [youtube] xxx: Video unavailable
//	存在しない ID     ERROR: [youtube] xxx: Video unavailable
//	権利で降ろされた  ERROR: [youtube] xxx: Video unavailable. This video is not available
//
// **同じ "Video unavailable" でレート制限も来る**ので、必ず classifyFetchFailure を通すこと
// （この関数を単独で呼ぶと、その順序が失われる）。
func isVideoGone(stderr string) bool {
	for _, marker := range []string{
		"Video unavailable",
		"This video has been removed",
		"This video is private",
		"Private video",
		"This video is no longer available",
		"account associated with this video has been terminated",
	} {
		if strings.Contains(stderr, marker) {
			return true
		}
	}
	return false
}
