package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

const (
	settingsKeyAutoFill = "auto_fill"
	// 実行結果は**設定とは別のキー**に書く。
	//
	// 同じ JSON を read-modify-write すると、実行結果の保存が設定を巻き戻す：
	// recordRun が enabled=true を読む → 運用者が PUT で無効化 →
	// recordRun が古い値ごと保存 → **自動処理が勝手に有効へ戻る**。
	// キーを分ければこの競合自体が存在しない。
	settingsKeyAutoFillRun = "auto_fill_last_run"
)

// AutoFillSettings は自動処理の設定（app_settings に JSON 保存）。
type AutoFillSettings struct {
	Enabled bool `json:"enabled"`
	// IntervalHours は実行間隔。**短くするほど無駄が増える。**
	// live chat が取れずに見送った配信は次の実行でやり直すので、
	// 間隔が短いほど「まだ変換中」の配信を何度も触ることになる
	// （issue #38 の 1 が入るまでは、そのたびに AI 抽出も走る）。
	IntervalHours int `json:"interval_hours"`
	// RefreshDays は「コメントを取り直す対象」の上限日数。
	//
	// **歌単は配信が終わったあとに貼られることが多い**ので、取り直さないと
	// 何度実行しても入力が変わらず結果も変わらない。一方で全配信を毎回
	// 取り直すと外部 API を 1 本につき 1 回叩くことになるので、日数で切る
	// （古い配信に今さら歌単が貼られることは稀）。
	RefreshDays int `json:"refresh_days"`
}

// AutoFillLastRun は最後の実行の記録（設定とは別キー。上の注記）。
type AutoFillLastRun struct {
	// At は**実際に処理した**最後の時刻。次回の判定はこれだけを見る。
	//
	// **見送りでここを進めない。** 進めると、予定時刻にたまたま手動の一括と
	// 重なっただけで次回が丸ごと 1 間隔ぶん先送りになる（168 時間設定なら 7 日後）。
	// 見送りは 10 分ごとの tick で次に拾えばよい。
	At *time.Time `json:"last_run_at,omitempty"`
	// SkippedAt / SkipNote は最後に見送ったとき（表示用。次回の判定には使わない）。
	// **実行の Note とは別に持つ** ── 混ぜると、古い実行時刻と新しい見送り理由が
	// 組み合わさって「実在しない一回の結果」に見える。
	SkippedAt *time.Time `json:"last_skipped_at,omitempty"`
	SkipNote  string     `json:"last_skip_note,omitempty"`
	Note      string     `json:"last_run_note,omitempty"`
	Error     string     `json:"last_run_error,omitempty"`
}

// AutoFillService は登録チャンネルの取り込みから歌単作成までを定期的に回す。
//
// **新しいことは何もしない。** 同期・コメント取り直し・一括セットリスト作成は
// どれも既にあるものを順に呼ぶだけで、この service が足すのは
// 「人がボタンを押す」の置き換えだけ（issue #35）。
//
// **審査と処理完了は自動化しない。** 確信の無いものは審査へ回り、
// `is_processed` は人が確かめてから付ける ── そこは設計上わざと残した関門。
type AutoFillService struct {
	settingsRepo *repository.AppSettingsRepository
	singerRepo   *repository.SingerRepository
	streamRepo   *repository.StreamRepository
	holodex      *HolodexService
	comments     *CommentService
	batchFill    *BatchFillService

	mu      sync.Mutex
	running bool
}

func NewAutoFillService(
	settingsRepo *repository.AppSettingsRepository,
	singerRepo *repository.SingerRepository,
	streamRepo *repository.StreamRepository,
	holodex *HolodexService,
	comments *CommentService,
	batchFill *BatchFillService,
) *AutoFillService {
	return &AutoFillService{
		settingsRepo: settingsRepo,
		singerRepo:   singerRepo,
		streamRepo:   streamRepo,
		holodex:      holodex,
		comments:     comments,
		batchFill:    batchFill,
	}
}

// defaultAutoFillSettings は保存が無いときの値。
//
// **既定は無効。** 外部 API と AI を自動で叩く仕組みなので、入れただけで
// 動き出してよいものではない（`singers.auto_fill_enabled` と同じ考え方）。
//
// **nil の settingsRepo を許さない**ので、ここだけ切り出してある ── 「repo が
// 無ければ既定」にすると、DI の配線漏れが「自動処理が静かに動かない」という
// 形で隠れる（エラーも出ない）。
func defaultAutoFillSettings() AutoFillSettings {
	return AutoFillSettings{
		Enabled:       false,
		IntervalHours: 6,
		RefreshDays:   30,
	}
}

// GetSettings は保存済み設定（無ければ既定）を返す。
//
// **既定は無効。** 外部 API と AI を自動で叩く仕組みなので、
// 入れただけで動き出してよいものではない（`auto_fill_enabled` と同じ考え方）。
func (s *AutoFillService) GetSettings() AutoFillSettings {
	settings := defaultAutoFillSettings()
	if _, err := s.settingsRepo.Get(settingsKeyAutoFill, &settings); err != nil {
		logger.Warnf("auto fill settings load: %v", err)
	}
	return settings
}

// GetLastRun は最後の実行の記録を返す。
func (s *AutoFillService) GetLastRun() AutoFillLastRun {
	var last AutoFillLastRun
	if _, err := s.settingsRepo.Get(settingsKeyAutoFillRun, &last); err != nil {
		logger.Warnf("auto fill last run load: %v", err)
	}
	return last
}

func (s *AutoFillService) saveSettings(settings AutoFillSettings) error {
	return s.settingsRepo.Set(settingsKeyAutoFill, settings)
}

// UpdateSettings は画面から変更できる項目だけを上書きする。
func (s *AutoFillService) UpdateSettings(enabled bool, intervalHours, refreshDays int) (AutoFillSettings, error) {
	clamp := func(v, min, max int) int {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	settings := s.GetSettings()
	settings.Enabled = enabled
	// 下限を 1 時間にするのは、短くするほど「まだ変換中」の配信を
	// 何度も触ることになるため（上の IntervalHours の注記）。
	settings.IntervalHours = clamp(intervalHours, 1, 24*7)
	settings.RefreshDays = clamp(refreshDays, 1, 365)
	if err := s.saveSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

// StartScheduler は定期チェックを開始する（goroutine）。
func (s *AutoFillService) StartScheduler() {
	go func() {
		// 起動直後の負荷を避けて少し待つ（バックアップのスケジューラと同じ）。
		time.Sleep(2 * time.Minute)
		s.runIfDue()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.runIfDue()
		}
	}()
}

func (s *AutoFillService) runIfDue() {
	settings := s.GetSettings()
	if !settings.Enabled {
		return
	}
	interval := time.Duration(settings.IntervalHours) * time.Hour
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if at := s.GetLastRun().At; at != nil && time.Since(*at) < interval {
		return
	}
	if _, err := s.RunOnce(); err != nil {
		logger.Warnf("[auto-fill] 実行に失敗: %v", err)
	}
}

// AutoFillResult は 1 回の実行で何が起きたか。
type AutoFillResult struct {
	Channels  int    `json:"channels"`
	Synced    int    `json:"synced"`
	Refreshed int    `json:"refreshed"`
	FillRunID string `json:"fill_run_id,omitempty"`
	// Failures は同期・コメント取得の失敗数。**ログだけにしない** ──
	// 全部失敗しても「正常に実行された」ように見えてはいけない。
	Failures int    `json:"failures"`
	Note     string `json:"note,omitempty"`
}

// RunOnce は 1 回ぶんの自動処理を実行する（手動実行の口でもある）。
//
// 順序に意味がある：
//
//  1. 同期          … 新しい配信が入ってくる
//  2. コメント取り直し … **歌単が空の配信だけ**。歌単は配信後に貼られることが多く、
//     取り直さないと何度実行しても入力が変わらない（issue #38）
//  3. 一括セットリスト作成 … unprocessed なので既にある歌単には触らない
func (s *AutoFillService) RunOnce() (AutoFillResult, error) {
	var res AutoFillResult

	// 二重起動を防ぐ。**判定と旗立てを同じロックの中で行う**
	// （分けると 2 つの tick が同時に通り抜ける）。
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return res, fmt.Errorf("自動処理が既に実行中です")
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	// **入力を書き換える前に一括の枠を押さえる。**
	//
	// 「走っていないか確認 → コメント更新 → 開始」の順では、確認と開始の間に
	// 手動の一括が始まりうる。その一括は古い raw=A を読み込んだあとで
	// こちらの更新（raw=B）を受け、**A のまま歌唱を作る**。開始を弾いても
	// 手遅れなので、確認と予約が原子的でなければならない（Reserve）。
	if !s.batchFill.Reserve() {
		res.Note = "一括セットリスト作成が実行中のため見送りました"
		logger.Infof("[auto-fill] %s", res.Note)
		s.recordSkip(res)
		return res, nil
	}
	// 予約したまま抜けると以後一括が開始できなくなる。**開始できた場合を除いて必ず解放する。**
	reserved := true
	defer func() {
		if reserved {
			s.batchFill.Release()
		}
	}()

	targets, err := s.singerRepo.FindAutoFillTargets()
	if err != nil {
		return res, fmt.Errorf("対象チャンネルの取得に失敗: %w", err)
	}
	res.Channels = len(targets)
	if len(targets) == 0 {
		res.Note = "対象チャンネルがありません"
		s.recordRun(res, nil)
		return res, nil // defer が予約を解放する
	}

	singerIDs := make([]string, 0, len(targets))
	for _, sg := range targets {
		singerIDs = append(singerIDs, sg.ID)
	}

	// 1. 同期。**1 チャンネルの失敗で全体を止めない** ── 他のチャンネルは
	//    独立して進められるし、止めると次の実行まで何も起きない。
	justSynced := map[string]bool{}
	for _, sg := range targets {
		syncRes, err := s.holodex.SyncChannel(sg.ID, 0, false)
		if err != nil {
			logger.Warnf("[auto-fill] %s の同期に失敗: %v", sg.ID, err)
			res.Failures++
			continue
		}
		if syncRes != nil {
			res.Synced += syncRes.SyncedCount
			// 同期で入ってきた配信を控える。**除外するかどうかは SQL 側で決める**
			// ── 新規というだけでは取得できた証拠にならない（取得に失敗しても
			// 新規として返るため）。実際にコメントが入ったものだけが除外される。
			for _, id := range syncRes.NewStreams {
				justSynced[id] = true
			}
		}
	}

	// **停止要求を見る。** 予約中も画面には停止ボタンが出るので、押されたのに
	// 続けると「停止しました」と答えたまま処理が進むことになる。
	if s.batchFill.Cancelled() {
		res.Note = "停止が要求されたため中止しました"
		logger.Infof("[auto-fill] %s", res.Note)
		s.recordSkip(res)
		return res, nil // defer が予約を解放する
	}

	// 2. コメントの取り直し（歌単が空・非表示でない・N 日以内）
	syncedIDs := make([]string, 0, len(justSynced))
	for id := range justSynced {
		syncedIDs = append(syncedIDs, id)
	}
	refreshed, refreshFailures := s.refreshComments(singerIDs, s.GetSettings().RefreshDays, syncedIDs)
	res.Refreshed = refreshed
	res.Failures += refreshFailures

	// 3. 一括セットリスト作成。既に手動で走っていれば譲る（次の実行で拾う）。
	if s.batchFill.Cancelled() {
		res.Note = "停止が要求されたため中止しました"
		logger.Infof("[auto-fill] %s", res.Note)
		s.recordSkip(res)
		return res, nil // defer が予約を解放する
	}

	runID, err := s.batchFill.StartReserved(BatchFillModeUnprocessed, singerIDs, false, nil)
	if err != nil {
		// StartReserved は失敗時に自分で予約を解放する。
		reserved = false
		s.recordRun(res, err)
		return res, fmt.Errorf("一括セットリスト作成の開始に失敗: %w", err)
	}
	// 開始できた＝予約は実行そのものへ引き継がれた。ここで解放してはいけない。
	reserved = false
	res.FillRunID = runID.String()

	logger.Infof("[auto-fill] 完了: チャンネル %d / 同期 %d / コメント取り直し %d",
		res.Channels, res.Synced, res.Refreshed)
	s.recordRun(res, nil)
	return res, nil
}

// refreshComments は歌単がまだ無い配信のコメントを取り直す。
//
// **全配信を取り直さない。** 既に歌単がある配信は取り直しても何も起きないし
// （自動処理は既存の歌単に触らない）、取り直しは外部 API を叩く。
// justSynced はこの実行の同期で入ってきた配信。**除外の判断は SQL 側**で行う
// （新規というだけでは取得できた証拠にならないため。repository の注記）。
func (s *AutoFillService) refreshComments(singerIDs []string, days int, justSynced []string) (refreshed, failures int) {
	ids, err := s.streamRepo.FindStreamsNeedingCommentRefresh(singerIDs, days, justSynced)
	if err != nil {
		logger.Warnf("[auto-fill] コメント取り直しの対象取得に失敗: %v", err)
		return 0, 1
	}
	for _, id := range ids {
		if err := s.comments.RefreshCommentRaw(id); err != nil {
			logger.Warnf("[auto-fill] %s のコメント取り直しに失敗: %v", id, err)
			failures++
			continue
		}
		refreshed++
	}
	if refreshed > 0 || failures > 0 {
		logger.Infof("[auto-fill] コメントを取り直しました: %d/%d 本（失敗 %d）",
			refreshed, len(ids), failures)
	}
	return refreshed, failures
}

// recordRun は最後の実行を設定へ書き戻す（画面に出すため）。
// recordSkip は見送りを記録する。**At は進めない**（上の注記）。
func (s *AutoFillService) recordSkip(res AutoFillResult) {
	last := s.GetLastRun()
	now := time.Now()
	last.SkippedAt = &now
	// **見送りの理由は別の欄に置く。** Note を上書きすると、古い last_run_at と
	// 新しい見送り理由が組み合わさって「実在しない一回の結果」に見える。
	last.SkipNote = res.Note
	if err := s.settingsRepo.Set(settingsKeyAutoFillRun, last); err != nil {
		logger.Warnf("[auto-fill] 見送りの記録に失敗: %v", err)
	}
}

func (s *AutoFillService) recordRun(res AutoFillResult, runErr error) {
	now := time.Now()
	last := s.GetLastRun()
	last.At = &now
	last.Error = ""
	last.Note = fmt.Sprintf("チャンネル %d / 同期 %d / コメント取り直し %d",
		res.Channels, res.Synced, res.Refreshed)
	if res.FillRunID != "" {
		// **「開始した」であって「終わった」ではない。** 一括作成は非同期なので、
		// その中の失敗はここには現れない。進捗は batch-fill の status を見る。
		last.Note += "／歌単作成を開始"
	}
	if res.Note != "" {
		last.Note += "（" + res.Note + "）"
	}
	// **ログだけにしない。** 同期やコメント取得が全部失敗しても
	// 「正常に実行された」ように見えると、動いていないことに気付けない。
	if res.Failures > 0 {
		last.Error = fmt.Sprintf("%d 件の失敗（詳細はログ）", res.Failures)
	}
	if runErr != nil {
		if last.Error != "" {
			last.Error += "／"
		}
		last.Error += runErr.Error()
	}
	if err := s.settingsRepo.Set(settingsKeyAutoFillRun, last); err != nil {
		logger.Warnf("[auto-fill] 実行結果の保存に失敗: %v", err)
	}
}
