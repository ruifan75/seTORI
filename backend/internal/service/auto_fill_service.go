package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

const settingsKeyAutoFill = "auto_fill"

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
	RefreshDays  int        `json:"refresh_days"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastRunNote  string     `json:"last_run_note,omitempty"`
	LastRunError string     `json:"last_run_error,omitempty"`
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
	if settings.LastRunAt != nil && time.Since(*settings.LastRunAt) < interval {
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
	Note      string `json:"note,omitempty"`
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

	targets, err := s.singerRepo.FindAutoFillTargets()
	if err != nil {
		return res, fmt.Errorf("対象チャンネルの取得に失敗: %w", err)
	}
	res.Channels = len(targets)
	if len(targets) == 0 {
		res.Note = "対象チャンネルがありません"
		s.recordRun(res, nil)
		return res, nil
	}

	singerIDs := make([]string, 0, len(targets))
	for _, sg := range targets {
		singerIDs = append(singerIDs, sg.ID)
	}

	// 1. 同期。**1 チャンネルの失敗で全体を止めない** ── 他のチャンネルは
	//    独立して進められるし、止めると次の実行まで何も起きない。
	for _, sg := range targets {
		syncRes, err := s.holodex.SyncChannel(sg.ID, 0, false)
		if err != nil {
			logger.Warnf("[auto-fill] %s の同期に失敗: %v", sg.ID, err)
			continue
		}
		if syncRes != nil {
			res.Synced += syncRes.SyncedCount
		}
	}

	// 2. コメントの取り直し（歌単が空・非表示でない・N 日以内）
	res.Refreshed = s.refreshComments(singerIDs, s.GetSettings().RefreshDays)

	// 3. 一括セットリスト作成。既に手動で走っていれば譲る（次の実行で拾う）。
	runID, err := s.batchFill.Start(BatchFillModeUnprocessed, singerIDs, false, nil)
	switch {
	case err == nil:
		res.FillRunID = runID.String()
	case errors.Is(err, ErrBatchFillAlreadyRunning):
		res.Note = "一括セットリスト作成が既に実行中のため見送りました"
		logger.Infof("[auto-fill] %s", res.Note)
	default:
		s.recordRun(res, err)
		return res, fmt.Errorf("一括セットリスト作成の開始に失敗: %w", err)
	}

	logger.Infof("[auto-fill] 完了: チャンネル %d / 同期 %d / コメント取り直し %d",
		res.Channels, res.Synced, res.Refreshed)
	s.recordRun(res, nil)
	return res, nil
}

// refreshComments は歌単がまだ無い配信のコメントを取り直す。
//
// **全配信を取り直さない。** 取り直しは 1 本につき外部 API 1 回で、
// 既に歌単がある配信は取り直しても何も起きない（自動処理は既存の歌単に触らない）。
func (s *AutoFillService) refreshComments(singerIDs []string, days int) int {
	ids, err := s.streamRepo.FindStreamsNeedingCommentRefresh(singerIDs, days)
	if err != nil {
		logger.Warnf("[auto-fill] コメント取り直しの対象取得に失敗: %v", err)
		return 0
	}
	refreshed := 0
	for _, id := range ids {
		if err := s.comments.RefreshCommentRaw(id); err != nil {
			logger.Warnf("[auto-fill] %s のコメント取り直しに失敗: %v", id, err)
			continue
		}
		refreshed++
	}
	if refreshed > 0 {
		logger.Infof("[auto-fill] コメントを取り直しました: %d/%d 本", refreshed, len(ids))
	}
	return refreshed
}

// recordRun は最後の実行を設定へ書き戻す（画面に出すため）。
func (s *AutoFillService) recordRun(res AutoFillResult, runErr error) {
	settings := s.GetSettings()
	now := time.Now()
	settings.LastRunAt = &now
	settings.LastRunNote = fmt.Sprintf("チャンネル %d / 同期 %d / コメント取り直し %d",
		res.Channels, res.Synced, res.Refreshed)
	if res.Note != "" {
		settings.LastRunNote += "（" + res.Note + "）"
	}
	settings.LastRunError = ""
	if runErr != nil {
		settings.LastRunError = runErr.Error()
	}
	if err := s.saveSettings(settings); err != nil {
		logger.Warnf("[auto-fill] 実行結果の保存に失敗: %v", err)
	}
}
