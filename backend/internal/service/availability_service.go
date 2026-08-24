package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync"
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

	// 実行中の状態。**止められることが要件**（1300 件を数十分かけて回すので、
	// cookie の不備に気付いたときに再起動でしか止められないのは困る）。
	mu        sync.Mutex
	running   bool
	cancelled bool
	progress  AvailabilityProgress
}

// AvailabilityProgress は backfill の進み具合。log は直近 1000 件しか残らず、
// 20 件ごとの進捗行が失敗行を押し流すので、成功・失敗の数はここで持つ。
type AvailabilityProgress struct {
	Running bool `json:"running"`
	Total   int  `json:"total"`
	Done    int  `json:"done"`
	// Saved は **DB に記録できた** 件数（「動画が無い」もここ。再試行は要らない）。
	// Failed は **記録できなかった** 件数＝再試行が要るもの。error の有無ではない。
	Saved     int    `json:"saved"`
	Failed    int    `json:"failed"`
	Cancelled bool   `json:"cancelled"`
	LastError string `json:"last_error,omitempty"`
}

// Cancel は実行中の backfill に停止を要求する。
//
// **保証は「この呼び出しが返ったあと、新しい 1 件は始まらない」。** 既に始まっている
// ものは最後まで走る（最大で並列数ぶん）。途中で殺さないのは、yt-dlp の一時ファイルが
// 残るため。起動の予約は Cancel と同じ mutex の下で行うので、この線引きが成り立つ。
func (s *AvailabilityService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.cancelled = true
	}
}

func (s *AvailabilityService) isCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// Progress は現在の進捗を返す（走っていなければ最後の実行の結果）。
func (s *AvailabilityService) Progress() AvailabilityProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
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
// 戻り値の saved は**DB に記録できたか**。error とは独立している ──
// 「動画が無い」は error を返すが記録は成功しており、再試行の必要は無い。
// backfill の失敗数をこれで数えないと、まさに今回直した入力（秘匿でない取得不能動画）が
// 「保存できた」のに「失敗」と報告される。
func (s *AvailabilityService) Fetch(videoID string) (a ytdlpAvailability, saved bool, err error) {
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

	a = parseYtdlpAvailability(lastNonEmptyLine(stdout.String()))

	if runErr != nil {
		switch {
		case notInstalled(runErr):
			return a, false, fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.path)
		case ctx.Err() != nil:
			return a, false, fmt.Errorf("yt-dlp がタイムアウトしました（2分）")
		case isBotCheck(stderr.String()):
			// BOT 判定は全配信で一様に起きる。値を保存すると「調べ済み」になって
			// cookie を入れ直したあとの調べ直しから漏れるので、保存せずに返す。
			return a, false, botCheckError(s.ytdlpRunner)
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
			return a, false, fmt.Errorf("動画情報を取得できません（cookie 無しのため会限と区別できず、判定は保存しません）: %s",
				ytdlpErrorLine(stderr.String()))
		}
		switch classifyFetchFailure(stderr.String()) {
		case failureTransient:
			return a, false, fmt.Errorf("一時的な失敗のため保存しません（時間をおいて再実行してください）: %s",
				ytdlpErrorLine(stderr.String()))
		case failureUnknown:
			return a, false, fmt.Errorf("動画情報を取得できません（原因を判別できないため保存しません。再実行してください）: %s",
				ytdlpErrorLine(stderr.String()))
		}
		// failureVideoGone。availability は空、playable_in_embed は NULL。
		if err := s.save(videoID, a); err != nil {
			return a, false, err
		}
		// **保存はできている。** error は「取得できなかった」という事実を伝えるためで、
		// 再試行は要らない（次回の backfill 対象からも外れる）。
		return a, true, fmt.Errorf("動画情報を取得できません: %s", ytdlpErrorLine(stderr.String()))
	}

	// 終了コード 0 でも中身が空なら保存しない（NULL/NULL を書くと unavailable になる）。
	if !a.Resolved() && a.Availability == "" {
		return a, false, fmt.Errorf("yt-dlp が再生可否を返しませんでした（保存しません）")
	}
	if err := s.save(videoID, a); err != nil {
		return a, false, err
	}
	return a, true, nil
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

	// 二重起動を防ぐ。同じ対象へ 2 つ走らせても yt-dlp が倍になるだけで得が無い。
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return 0, concurrency, fmt.Errorf("すでに実行中です（停止するには cancel）")
	}
	s.running = true
	s.cancelled = false
	s.progress = AvailabilityProgress{Running: true, Total: len(ids)}
	s.mu.Unlock()

	go func() {
		logger.Infof("[availability] backfill を開始: %d 件 (並列 %d)", len(ids), concurrency)
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, id := range ids {
			// 速い経路。ここで落とせれば semaphore を待たずに済む。
			if s.isCancelled() {
				break
			}
			sem <- struct{}{}

			// **確認と起動を同じ mutex の下で行う。** 確認だけを別に取ると、
			// 解錠してから go するまでの間に cancel が入り、その 1 件が
			// cancel の応答を返したあとに開始してしまう。
			// Cancel が同じ mutex を取るので、cancelled=true が見えた時点で
			// 以後の go は発行されない ── 既に発行済みのものだけが走る。
			s.mu.Lock()
			if s.cancelled {
				s.mu.Unlock()
				<-sem
				break
			}
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()
				_, saved, err := s.Fetch(id)
				if err != nil {
					logger.Warnf("[availability] backfill %s: %v", id, err)
				}
				// **数えるのは「記録できたか」であって error の有無ではない。**
				// 「動画が無い」は error を返すが記録は成功しており、再試行は要らない。
				// ここを取り違えると、operator が「再試行が必要な件数」を過大に読む。
				s.mu.Lock()
				s.progress.Done++
				if saved {
					s.progress.Saved++
				} else {
					s.progress.Failed++
					if err != nil {
						s.progress.LastError = err.Error()
					}
				}
				n, sv, fl := s.progress.Done, s.progress.Saved, s.progress.Failed
				s.mu.Unlock()
				if n%20 == 0 || n == len(ids) {
					logger.Infof("[availability] backfill の進捗: %d/%d（記録 %d / 未記録 %d）", n, len(ids), sv, fl)
				}
			}(id)
			s.mu.Unlock()
		}
		wg.Wait()

		s.mu.Lock()
		s.running = false
		s.progress.Running = false
		s.progress.Cancelled = s.cancelled
		fin := s.progress
		s.mu.Unlock()

		// **試行数ではなく記録・未記録を出す。** 以前は「完了: N 件」と対象数を出していたので、
		// 全部失敗していても成功したように読めた。
		logger.Infof("[availability] backfill が終了: 記録 %d / 未記録 %d / 処理 %d / 対象 %d%s",
			fin.Saved, fin.Failed, fin.Done, fin.Total,
			map[bool]string{true: "（キャンセル）", false: ""}[fin.Cancelled])
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
