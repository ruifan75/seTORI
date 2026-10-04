package service

import (
	"errors"
	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

type preparationStreams interface {
	FindPreparationStreams(string) ([]models.Stream, error)
}
type preparationChapters interface {
	RefreshChapters(string) ([]Chapter, error)
}
type preparationBatch interface {
	Reserve() bool
	Release()
	Cancelled() bool
	RunPrepared([]models.Stream, *TaskRun, func() bool) error
}
type preparationFill interface {
	Reserve() bool
	Release()
	Cancelled() bool
}

// PrepareService は入力を更新する前に一括分析・一括作成の両方を予約する。
// 準備は全チャンネルで 1 本まで。他 singer でも共有の解析入力へ同時に書かない。
type PrepareService struct {
	streams  preparationStreams
	chapters preparationChapters
	batch    preparationBatch
	fill     preparationFill
	tasks    *TaskRunService
}

func NewPrepareService(streams preparationStreams, chapters preparationChapters, batch preparationBatch, fill preparationFill, tasks *TaskRunService) *PrepareService {
	return &PrepareService{streams, chapters, batch, fill, tasks}
}
func (s *PrepareService) Start(singerID string, by *uuid.UUID) (*TaskRun, error) {
	if !s.fill.Reserve() {
		return nil, ErrBatchFillAlreadyRunning
	}
	if !s.batch.Reserve() {
		s.fill.Release()
		return nil, ErrBatchAlreadyRunning
	}
	run, err := s.tasks.Start(TaskStreamPrepare, map[string]string{"singer_id": singerID}, by)
	if err != nil {
		s.batch.Release()
		s.fill.Release()
		return nil, err
	}
	go s.execute(singerID, run)
	return run, nil
}
func (s *PrepareService) execute(singerID string, run *TaskRun) {
	defer s.fill.Release()
	defer s.batch.Release()
	status, message := "failed", "準備が完了しませんでした"
	defer func() { run.Finish(status, message) }()
	cancelled := func() bool { return run.Cancelled() || s.batch.Cancelled() || s.fill.Cancelled() }
	streams, err := s.streams.FindPreparationStreams(singerID)
	if err != nil {
		message = "対象の取得に失敗しました: " + err.Error()
		return
	}
	run.SetTotal(2 * len(streams))
	if err := run.SetPhase("chapters"); err != nil {
		message = "段階の保存に失敗しました: " + err.Error()
		return
	}
	for _, stream := range streams {
		if cancelled() {
			status, message = "cancelled", "停止しました（処理中の取得は完了しています）"
			return
		}
		if _, ok := decodeChapters(stream.ChapterRaw); ok {
			run.Skip()
			continue
		}
		if _, err := s.chapters.RefreshChapters(stream.ID); err != nil {
			run.Fail(stream.ID, "章節取得: "+err.Error())
		} else {
			run.Succeed()
		}
	}
	if cancelled() {
		status, message = "cancelled", "プレ分析の前に停止しました"
		return
	}
	if err := run.SetPhase("analysis"); err != nil {
		message = "段階の保存に失敗しました: " + err.Error()
		return
	}
	if err := s.batch.RunPrepared(streams, run, s.fill.Cancelled); err != nil {
		message = "プレ分析: " + err.Error()
		return
	}
	if cancelled() {
		status, message = "cancelled", "停止しました"
		return
	}
	status, message = "done", ""
	if run.FailedCount() > 0 {
		status = "failed"
	}
}

func (t *TaskRun) SetPhase(phase string) error { return t.svc.repo.SetPhase(t.ID, phase) }
func (t *TaskRun) Cancelled() bool             { return t.cancelled.Load() }

// CancelPreparation は準備だけを停止する。未実装の backfill 停止を成功と答えない。
func (s *TaskRunService) CancelPreparation(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.active[id]
	if run == nil {
		return errors.New("実行中の準備が見つかりません")
	}
	if run.kind != TaskStreamPrepare {
		return errors.New("この処理は停止に対応していません")
	}
	run.cancelled.Store(true)
	return nil
}
