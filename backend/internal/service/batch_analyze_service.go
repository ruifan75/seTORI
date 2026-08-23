package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

// BatchAnalyzeService は未処理配信の一括プレ分析ジョブ（singleton）。
// 逐次で AnalyzeComments（抽出→AI正規化→拍手end→キャッシュ）を回す。
// setlist の自動作成はしない：結果はキャッシュに載るだけで、確認・保存は人が行う。
//
// AI 冷却への配慮：
//   - 配信間に固定インターバルを挟んでリクエストを平滑化
//   - AI が失敗（全プロバイダー冷却等）した配信は、冷却明けを待って
//     force=true（劣化キャッシュを無視）で再試行。規定回数失敗したら記録して先へ進む
//   - キャッシュ済みの配信は AI を呼ばず秒で通過する（hash キャッシュ）
type BatchAnalyzeService struct {
	commentService *CommentService
	streamRepo     *repository.StreamRepository

	mu        sync.Mutex
	running   bool
	cancelled bool
	status    dto.BatchAnalyzeStatus
}

const (
	batchStreamInterval = 2 * time.Second  // 配信間のインターバル
	batchCooldownWait   = 90 * time.Second // AI 失敗時の冷却待ち
	batchMaxAttempts    = 3                // 配信ごとの最大試行回数
)

var ErrBatchAlreadyRunning = errors.New("一括分析は既に実行中です")

// バッチの対象モード
const (
	BatchModeUnanalyzed  = "unanalyzed"  // 分析結果が一度も無い配信（is_processed 問わず）
	BatchModeUnprocessed = "unprocessed" // 未処理（ユーザー未確認）の配信すべて
	BatchModeRefresh     = "refresh"     // 未処理の配信のコメントを再取得してから分析
	BatchModeReanalyze   = "reanalyze"   // comment_raw を持つ配信すべてを force で再分析（分析済みも作り直す）
)

func NewBatchAnalyzeService(commentService *CommentService, streamRepo *repository.StreamRepository) *BatchAnalyzeService {
	return &BatchAnalyzeService{commentService: commentService, streamRepo: streamRepo}
}

// Start はジョブを開始する（実行中なら ErrBatchAlreadyRunning）。
// singerID を指定するとそのチャンネルが参加した配信のみが対象（空なら全チャンネル）。
// Start は一括プレ分析を開始する。hidden は非表示配信の扱い
// （nil=両方 / false=非表示を除く / true=非表示だけ）。
func (s *BatchAnalyzeService) Start(mode, singerID string, hidden *bool) error {
	switch mode {
	case BatchModeUnanalyzed, BatchModeUnprocessed, BatchModeRefresh, BatchModeReanalyze:
	default:
		return errors.New("無効なモードです（unanalyzed / unprocessed / refresh / reanalyze）")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return ErrBatchAlreadyRunning
	}
	s.running = true
	s.cancelled = false
	s.status = dto.BatchAnalyzeStatus{Running: true, Mode: mode, SingerID: singerID, Hidden: hiddenLabel(hidden)}

	go s.run(mode, singerID, hidden)
	return nil
}

// Cancel は実行中のジョブに停止を要求する（処理中の1件が終わり次第停止）。
func (s *BatchAnalyzeService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.cancelled = true
	}
}

// Status は進捗のスナップショットを返す。
func (s *BatchAnalyzeService) Status() dto.BatchAnalyzeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *BatchAnalyzeService) isCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func (s *BatchAnalyzeService) update(fn func(*dto.BatchAnalyzeStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.status)
}

func (s *BatchAnalyzeService) run(mode, singerID string, hidden *bool) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.status.Running = false
		s.status.Current = ""
		if s.cancelled {
			s.status.Message = "キャンセルされました"
		} else if s.status.Failed > 0 {
			s.status.Message = "完了（一部失敗あり）"
		} else {
			s.status.Message = "完了"
		}
		s.mu.Unlock()
		logger.Infof("[batch-analyze] finished: done=%d failed=%d", s.status.Done, s.status.Failed)
	}()

	streams, err := s.streamRepo.FindStreamsForBatch(mode, singerID, hidden)
	if err != nil {
		logger.Warnf("[batch-analyze] list streams failed: %v", err)
		s.update(func(st *dto.BatchAnalyzeStatus) { st.Message = "対象の取得に失敗しました" })
		return
	}

	// reanalyze は分析済みも作り直すため、最初から force でキャッシュを無視する。
	forceStart := mode == BatchModeReanalyze

	s.update(func(st *dto.BatchAnalyzeStatus) { st.Total = len(streams) })
	logger.Infof("[batch-analyze] started: mode=%s singer=%q hidden=%s %d streams", mode, singerID, hiddenLabel(hidden), len(streams))

	for _, stream := range streams {
		if s.isCancelled() {
			return
		}
		s.update(func(st *dto.BatchAnalyzeStatus) { st.Current = stream.Title })

		// refresh モード：先にコメントを再取得（内容が変わればハッシュが変わり、
		// 後続の分析でキャッシュを外れて自動的に再分析される）
		if mode == BatchModeRefresh {
			if err := s.commentService.RefreshCommentRaw(stream.ID); err != nil {
				logger.Warnf("[batch-analyze] refresh comments failed (%s): %v（既存の raw で分析を続行）", stream.ID, err)
			}
		}

		if s.processOne(stream.ID, forceStart) {
			s.update(func(st *dto.BatchAnalyzeStatus) { st.Done++ })
		} else if s.isCancelled() {
			return
		} else {
			s.update(func(st *dto.BatchAnalyzeStatus) {
				st.Failed++
				st.FailedIDs = append(st.FailedIDs, stream.ID)
			})
		}

		time.Sleep(batchStreamInterval)
	}
}

// processOne は1配信を分析する。AI 劣化（warning あり）は冷却待ち後に force で再試行。
// forceStart=true のときは初回から force（キャッシュ無視）で分析する（reanalyze 用）。
func (s *BatchAnalyzeService) processOne(videoID string, forceStart bool) bool {
	force := forceStart
	for attempt := 1; attempt <= batchMaxAttempts; attempt++ {
		if s.isCancelled() {
			return false
		}

		// 一括プレ分析は抽出までにとどめる。**この経路では**照合の AI 判定を行わない
		// ── 行うのは対話の analyze と、歌唱を作る batch-fill。
		resp, err := s.commentService.AnalyzeCommentsForBatch(videoID, force)
		if err == nil && resp.Warning == "" {
			return true
		}
		// 分析中にコメントが差し替わっただけなら、待たずに読み直す。
		// 90 秒の冷却は AI プロバイダーの劣化明けを待つためのもので、
		// 競合に使うと 1 回で 90 秒・2 回で 180 秒待ってから failed になる。
		if errors.Is(err, ErrCommentRawChanged) {
			logger.Warnf("[batch-analyze] %s attempt %d: コメントが変わったので読み直します", videoID, attempt)
			force = true
			if attempt == batchMaxAttempts {
				return false
			}
			continue
		}
		if err != nil {
			logger.Warnf("[batch-analyze] %s attempt %d failed: %v", videoID, attempt, err)
		} else {
			logger.Warnf("[batch-analyze] %s attempt %d AI degraded: %s", videoID, attempt, resp.Warning)
		}

		if attempt == batchMaxAttempts {
			return false
		}
		// 劣化結果がキャッシュに載っているため、次は force で作り直す。
		// AI プロバイダーの冷却明けを待ってから再試行する。
		force = true
		if !s.sleepInterruptible(batchCooldownWait) {
			return false
		}
	}
	return false
}

// sleepInterruptible はキャンセルに反応しつつ待機する。継続可否を返す。
func (s *BatchAnalyzeService) sleepInterruptible(d time.Duration) bool {
	const step = time.Second
	for waited := time.Duration(0); waited < d; waited += step {
		if s.isCancelled() {
			return false
		}
		time.Sleep(step)
	}
	return !s.isCancelled()
}

// hiddenLabel は非表示の扱いを画面とログ用の文字列にする（API の語彙と揃える）。
func hiddenLabel(hidden *bool) string {
	if hidden == nil {
		return "all"
	}
	if *hidden {
		return "true"
	}
	return "false"
}

// ParseHiddenFilter は API の hidden パラメータを絞り込みへ変換する。
// 語彙は GET /api/singers/{id}/streams?hidden= と同じ：
// ""/"false"=非表示を除く（既定）、"true"=非表示だけ、"all"=両方。
//
// **知らない値は既定へ倒さずエラーにする。** ここは読み取りではなく、
// 数百本を AI にかける背景ジョブの起動口で、対象集合が 700 本近く変わる。
// `hidden=TRUE` や JSON の真偽値 `true` のような惜しい間違いを既定
// （＝非表示を除く）として受け付けると、意図と違う対象で走り出したうえに
// 202 が返るので、呼んだ側は成功したと思い込む。
func ParseHiddenFilter(v string) (*bool, error) {
	switch v {
	case "", "false":
		f := false
		return &f, nil
	case "true":
		t := true
		return &t, nil
	case "all":
		return nil, nil
	default:
		return nil, fmt.Errorf("hidden は false / true / all のいずれかで指定してください（受け取った値: %q）", v)
	}
}
