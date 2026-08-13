package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// BatchFillService は範囲を指定してセットリストを自動で埋めるジョブ（singleton）。
//
// 一括プレ分析（BatchAnalyzeService）とは性質が違う。あちらは
// comment_raw → comment_songs の抽出だけで、何度回しても主データは変わらない。
// こちらは **performances（主データ）に直接書く**ので、
//
//   - 何をどう作ったかを台帳（batch_fill_runs）に残す
//   - 確信が持てないものは書かず、人の審査（perf.missing 提案）へ回す
//   - 実行単位で撤回できる
//
// の 3 つが要る。詳細と判断の根拠は docs/SETLIST_FLOW.md。
type BatchFillService struct {
	streamRepo     *repository.StreamRepository
	perfRepo       *repository.PerformanceRepository
	runRepo        *repository.BatchFillRepository
	commentService *CommentService
	holodexService *HolodexService
	normalization  *NormalizationService
	perfService    *PerformanceService
	suggestions    *SuggestionService

	mu        sync.Mutex
	running   bool
	cancelled bool
	status    dto.BatchFillStatus
}

const (
	// BatchFillModeUnprocessed は歌唱がまだ 1 つも無い配信だけを埋める。
	BatchFillModeUnprocessed = "unprocessed"
	// BatchFillModeForce は源を持つ配信すべてを見る。既存と食い違う分は審査へ回す。
	BatchFillModeForce = "force"

	// 自動で歌唱を作ってよい AI の確信度。これ未満は人の審査へ回す。
	batchFillMinConfidence = 0.85
	// 配信間のインターバル（AI とデータベースへの負荷を平す）
	batchFillInterval = 1 * time.Second
)

// 審査へ回す理由。画面にそのまま出すので、増やすときは表示側も見ること。
const (
	reviewNoEnd       = "no_end"       // 終了時間が無い
	reviewNoArtist    = "no_artist"    // 歌手が書かれていない
	reviewUnmatched   = "unmatched"    // どの曲か決まらない
	reviewMultiSinger = "multi_singer" // 配信に歌手が複数いる
	reviewConflict    = "conflict"     // 既存の歌唱と食い違う
	reviewLowConf     = "low_conf"     // AI の確信度が足りない
)

var ErrBatchFillAlreadyRunning = errors.New("一括セットリスト作成は既に実行中です")

func NewBatchFillService(
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
	runRepo *repository.BatchFillRepository,
	commentService *CommentService,
	holodexService *HolodexService,
	normalization *NormalizationService,
	perfService *PerformanceService,
	suggestions *SuggestionService,
) *BatchFillService {
	return &BatchFillService{
		streamRepo: streamRepo, perfRepo: perfRepo, runRepo: runRepo,
		commentService: commentService, holodexService: holodexService,
		normalization: normalization, perfService: perfService, suggestions: suggestions,
	}
}

// fillRow は 1 曲ぶんの作業単位。
type fillRow struct {
	StreamID string
	Name     string
	Artist   string
	Start    int
	End      int
	Tags     []string
	ItunesID *int64
	Source   string // holodex / comment

	EndSource  string // 終了時間の由来（docs/DATA_COMPLETION.md の語彙）
	SongID     *uuid.UUID
	Via        string // rule / ai
	Confidence float64
	Review     []string

	ai *aiMatchRow // AI に回した行への参照（判定後に読む）
}

func (r *fillRow) needsReview() bool { return len(r.Review) > 0 }

// Start はジョブを開始する。
func (s *BatchFillService) Start(mode, singerID string, startedBy *uuid.UUID) (uuid.UUID, error) {
	switch mode {
	case BatchFillModeUnprocessed, BatchFillModeForce:
	default:
		return uuid.Nil, errors.New("無効なモードです（unprocessed / force）")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return uuid.Nil, ErrBatchFillAlreadyRunning
	}

	var sid *string
	if singerID != "" {
		sid = &singerID
	}
	runID, err := s.runRepo.CreateRun(mode, sid, startedBy)
	if err != nil {
		return uuid.Nil, err
	}

	s.running = true
	s.cancelled = false
	s.status = dto.BatchFillStatus{Running: true, Mode: mode, SingerID: singerID, RunID: runID.String()}

	go s.run(runID, mode, singerID)
	return runID, nil
}

// Cancel は実行中のジョブに停止を要求する（処理中の 1 件が終わり次第停止）。
func (s *BatchFillService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.cancelled = true
	}
}

// Status は現在の進捗を返す。
func (s *BatchFillService) Status() dto.BatchFillStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ListRuns は過去の実行を返す。
func (s *BatchFillService) ListRuns(limit int) ([]repository.BatchFillRun, error) {
	return s.runRepo.ListRuns(limit)
}

// RevertRun は実行が作った歌唱をまとめて消す。
func (s *BatchFillService) RevertRun(runID uuid.UUID) (int64, error) {
	n, err := s.runRepo.DeleteByRun(runID)
	if err != nil {
		return 0, err
	}
	logger.Infof("[batch-fill] 実行 %s を撤回しました（%d 件の歌唱を削除）", runID, n)
	return n, nil
}

func (s *BatchFillService) run(runID uuid.UUID, mode, singerID string) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.status.Running = false
		s.status.Current = ""
		s.mu.Unlock()
	}()

	streams, err := s.streamRepo.FindStreamsForFill(mode, singerID)
	if err != nil {
		logger.Warnf("[batch-fill] 対象の取得に失敗: %v", err)
		s.finish(runID, "failed", "対象の取得に失敗しました")
		return
	}
	s.update(func(st *dto.BatchFillStatus) { st.Total = len(streams) })
	logger.Infof("[batch-fill] 開始: mode=%s singer=%q %d 配信", mode, singerID, len(streams))

	// ---- 第 1 段：源を読み、規則で照合できるところまで進める ----
	byStream := map[string][]*fillRow{}
	var unresolved []*aiMatchRow
	dedupe := map[string]*aiMatchRow{} // 同じ表記は 1 回だけ聞く

	for _, stream := range streams {
		if s.isCancelled() {
			s.finish(runID, "cancelled", "キャンセルされました")
			return
		}
		s.update(func(st *dto.BatchFillStatus) { st.Current = stream.ID })

		rows := s.loadRows(stream.ID)
		if len(rows) == 0 {
			s.update(func(st *dto.BatchFillStatus) { st.Done++ })
			continue
		}
		for _, row := range rows {
			if row.SongID != nil {
				continue
			}
			// 同じ表記の行はひとつの問いにまとめる（曲庫を何度も送らないため）
			key := songmatch.TitleKey(row.Name) + "\x1f" + songmatch.ParseArtist(row.Artist).String()
			ai, ok := dedupe[key]
			if !ok {
				ai = s.normalization.newAIMatchRowFromCandidates(row.Name, row.Artist)
				dedupe[key] = ai
				unresolved = append(unresolved, ai)
			}
			row.ai = ai
		}
		byStream[stream.ID] = rows
		s.update(func(st *dto.BatchFillStatus) { st.Done++ })
		time.Sleep(batchFillInterval)
	}

	// ---- 第 2 段：AI に一度だけまとめて聞く ----
	//
	// 配信ごとに聞くと曲庫（約 12k トークン）を配信の数だけ送ることになる。
	// 実測（356 配信）では候補ゼロが 82 行・65 種・58 配信で、
	// 配信ごとなら 58 回、まとめれば 9 回で済む。
	asked := 0
	if len(unresolved) > 0 && !s.isCancelled() {
		s.update(func(st *dto.BatchFillStatus) { st.Phase = "ai" })
		asked, _ = s.normalization.AdjudicateMatches(unresolved)
		logger.Infof("[batch-fill] AI 判定: %d 種の表記を問い合わせ", len(unresolved))
	}

	// ---- 第 3 段：書くか、審査へ回すか ----
	s.update(func(st *dto.BatchFillStatus) { st.Phase = "write" })
	created, review := 0, 0
	for streamID, rows := range byStream {
		if s.isCancelled() {
			break
		}
		c, r := s.applyStream(runID, streamID, rows, mode)
		created += c
		review += r
		s.update(func(st *dto.BatchFillStatus) {
			st.Created = created
			st.Review = review
			st.AIAsked = asked
		})
		if err := s.runRepo.UpdateProgress(runID, len(streams), s.Status().Done, created, review, asked); err != nil {
			logger.Warnf("[batch-fill] 進捗の保存に失敗: %v", err)
		}
	}

	status, msg := "done", fmt.Sprintf("%d 曲を作成、%d 曲を審査へ", created, review)
	if s.isCancelled() {
		status, msg = "cancelled", "キャンセルされました（途中まで反映済み）"
	}
	s.finish(runID, status, msg)
	logger.Infof("[batch-fill] 終了: %s", msg)
}

// loadRows は配信の源を読んで作業単位に落とす。Holodex を優先する。
//
// Holodex の曲は **iTunes ID を持っている**（最も強い証拠で、曲名が食い違っていても照合できる）。
// ただし Holodex のセットリストは欠けていることがあるので、コメント側の曲数が明らかに多ければ
// その配信は人に見てもらう（黙って少ない方を採らない）。
func (s *BatchFillService) loadRows(streamID string) []*fillRow {
	var holodexRows, commentRows []*fillRow

	if sugs, err := s.holodexService.AnalyzeHolodexSongsForBatch(streamID, false); err == nil {
		for _, sg := range sugs {
			holodexRows = append(holodexRows, suggestionToFillRow(streamID, sg))
		}
	} else {
		logger.Warnf("[batch-fill] Holodex 読み込み失敗 (%s): %v", streamID, err)
	}

	if resp, err := s.commentService.AnalyzeCommentsForBatch(streamID, false); err == nil && resp != nil {
		s.normalization.ResolveForDisplay(resp.Songs)
		for _, cs := range resp.Songs {
			commentRows = append(commentRows, commentSongToFillRow(streamID, cs))
		}
	}

	if len(holodexRows) == 0 {
		return commentRows
	}
	// コメント側が明らかに多い（1.5 倍以上かつ 3 曲以上の差）なら取りこぼしを疑う
	if len(commentRows) >= len(holodexRows)*3/2 && len(commentRows)-len(holodexRows) >= 3 {
		for _, r := range holodexRows {
			r.Review = append(r.Review, reviewConflict)
		}
		logger.Infof("[batch-fill] %s: Holodex %d 曲 / コメント %d 曲 ── 差が大きいので審査へ",
			streamID, len(holodexRows), len(commentRows))
	}
	return holodexRows
}

// applyStream は 1 配信ぶんを反映する。戻り値は (作成, 審査へ回した)。
func (s *BatchFillService) applyStream(runID uuid.UUID, streamID string, rows []*fillRow, mode string) (int, int) {
	// 配信に歌手が複数いるなら、誰が歌ったかを機械では埋められない。全行を審査へ。
	multi := s.hasMultipleSingers(streamID)

	existing, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		logger.Warnf("[batch-fill] 既存歌唱の取得に失敗 (%s): %v", streamID, err)
		return 0, 0
	}

	var toCreate []dto.CreatePerformanceItem
	var origins []repository.BatchOrigin
	reviewCount := 0

	for _, row := range rows {
		s.resolveFromAI(row)
		if multi {
			row.Review = append(row.Review, reviewMultiSinger)
		}
		if row.End <= row.Start {
			row.Review = append(row.Review, reviewNoEnd)
		}
		if row.Artist == "" {
			row.Review = append(row.Review, reviewNoArtist)
		}
		if row.SongID == nil {
			row.Review = append(row.Review, reviewUnmatched)
		}
		if overlapsExisting(existing, row) {
			// 既にある歌唱は黙って書き換えない（force でも）。
			// 中身が違うなら審査へ回し、同じなら何もしない。
			if mode == BatchFillModeForce && !sameAsExisting(existing, row) {
				row.Review = append(row.Review, reviewConflict)
			} else {
				continue
			}
		} else if len(existing) > 0 {
			// 既にセットリストがある配信には**書き足さない**。
			// CreatePerformances はセットリスト全体を受け取って差分を取るので、
			// 書き足すには既存分も送り直すことになり、その過程で既存の歌唱が
			// 曲名から引き直される（別の曲に付け替わりうる）。人の作ったものを
			// 機械の都合で作り直さない ── 追加は提案として出す。
			row.Review = append(row.Review, reviewConflict)
		}

		if row.needsReview() {
			if s.createReviewSuggestion(row) {
				reviewCount++
			}
			continue
		}

		endConfirmed := false // 人手を介していない
		toCreate = append(toCreate, dto.CreatePerformanceItem{
			Name:           row.Name,
			OriginalArtist: row.Artist,
			StartSeconds:   row.Start,
			EndSeconds:     row.End,
			Tags:           row.Tags,
			SingerIDs:      s.defaultSingerIDs(streamID),
			ItunesID:       row.ItunesID,
			EndSource:      row.EndSource,
			EndConfirmed:   &endConfirmed,
		})
		origins = append(origins, repository.BatchOrigin{
			SongID: *row.SongID, StartSeconds: row.Start, Via: row.Via, Confidence: row.Confidence,
		})
	}

	if len(toCreate) == 0 {
		return 0, reviewCount
	}
	if _, err := s.perfService.CreatePerformances(streamID, toCreate); err != nil {
		logger.Warnf("[batch-fill] 歌唱の作成に失敗 (%s): %v", streamID, err)
		return 0, reviewCount
	}
	if err := s.runRepo.MarkOrigins(streamID, runID, origins); err != nil {
		logger.Warnf("[batch-fill] 由来の記録に失敗 (%s): %v", streamID, err)
	}
	return len(origins), reviewCount
}

// resolveFromAI は AI の判定を行へ写す。確信度が足りなければ採用しない。
func (s *BatchFillService) resolveFromAI(row *fillRow) {
	if row.SongID != nil || row.ai == nil || row.ai.SongID == nil {
		return
	}
	if row.ai.Confidence < batchFillMinConfidence {
		row.Review = append(row.Review, reviewLowConf)
		return
	}
	song, err := s.normalization.matchService.FindSong(*row.ai.SongID)
	if err != nil || song == nil {
		return
	}
	row.SongID = &song.ID
	row.Name = song.Name
	if song.OriginalArtist != "" {
		row.Artist = song.OriginalArtist
	}
	row.Via = "ai"
	row.Confidence = row.ai.Confidence
}

// createReviewSuggestion は審査待ちとして perf.missing の提案を積む。
//
// 自動反映の条件は「異なるログインユーザーが N 人以上」なので、
// 一括が作った提案は出所が 1 つしか無く、自動では反映されない。
func (s *BatchFillService) createReviewSuggestion(row *fillRow) bool {
	if s.suggestions == nil {
		return false
	}
	_, err := s.suggestions.Create(&dto.CreateSuggestionRequest{
		Kind: KindMissingSong,
		Payload: &dto.MissingSongPayload{
			StreamID:       row.StreamID,
			SongName:       row.Name,
			OriginalArtist: row.Artist,
			StartSeconds:   row.Start,
			EndSeconds:     row.End,
		},
		Note: fmt.Sprintf("一括作成の審査待ち（%s・源: %s）", joinReasons(row.Review), row.Source),
	}, SuggestionActor{ClientHint: "batch-fill", System: true})
	if err != nil {
		logger.Warnf("[batch-fill] 審査の登録に失敗 (%s / %s): %v", row.StreamID, row.Name, err)
		return false
	}
	return true
}

func (s *BatchFillService) hasMultipleSingers(streamID string) bool {
	participants, _, err := s.streamRepo.GetSingersForStreams([]string{streamID})
	if err != nil {
		return true // 分からないときは安全側（人に見せる）
	}
	return len(participants[streamID]) > 1
}

func (s *BatchFillService) defaultSingerIDs(streamID string) []string {
	participants, owners, err := s.streamRepo.GetSingersForStreams([]string{streamID})
	if err != nil {
		return nil
	}
	if owner := owners[streamID]; owner != nil {
		return []string{owner.ID}
	}
	if list := participants[streamID]; len(list) == 1 {
		return []string{list[0].ID}
	}
	return nil
}

func (s *BatchFillService) isCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func (s *BatchFillService) update(f func(*dto.BatchFillStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.status)
}

func (s *BatchFillService) finish(runID uuid.UUID, status, message string) {
	s.update(func(st *dto.BatchFillStatus) { st.Message = message })
	if err := s.runRepo.FinishRun(runID, status, message); err != nil {
		logger.Warnf("[batch-fill] 実行記録の更新に失敗: %v", err)
	}
}

// ---------- 変換と比較 ----------

func suggestionToFillRow(streamID string, sg dto.SongSuggestion) *fillRow {
	name, artist := MatchInputs(sg.Name, sg.OriginalArtist, sg.NormalizedName, sg.NormalizedArtist)
	row := &fillRow{
		StreamID: streamID, Name: name, Artist: artist,
		Start: sg.StartSeconds, End: sg.EndSeconds, ItunesID: sg.ItunesID, Source: "holodex",
		// 由来は画面（StreamDetailPage）と同じ規則で決める。**"batch" のような
		// 独自の値を入れないこと** ── end_source は「どれだけ信用できるか」を表す
		// 語彙で、誰が書いたかを表す欄ではない（docs/DATA_COMPLETION.md）。
		EndSource: endSourceForHolodex(sg),
	}
	applyMatched(row, sg.MatchedSongID, sg.MatchedSongName, sg.MatchedSongArtist)
	return row
}

func commentSongToFillRow(streamID string, cs dto.CommentSong) *fillRow {
	name, artist := MatchInputs(cs.Name, cs.OriginalArtist, cs.NormalizedName, cs.NormalizedArtist)
	row := &fillRow{
		StreamID: streamID, Name: name, Artist: artist,
		Start: cs.Start, End: cs.End, Tags: cs.Tags, Source: "comment",
		EndSource: endSourceForComment(cs),
	}
	applyMatched(row, cs.MatchedSongID, cs.MatchedSongName, cs.MatchedSongArtist)
	return row
}

// endSourceForHolodex / endSourceForComment は終了時間の由来を決める。
// 画面が編集中ずっと追跡しているのと同じ規則にしてある
// ── 経路によって違う値が入ると、確度で絞り込む問い合わせが当てにならなくなる。
func endSourceForHolodex(sg dto.SongSuggestion) string {
	if sg.EndSeconds <= 0 {
		return "unknown"
	}
	if sg.ChatEnd > 0 && sg.ChatEnd == sg.EndSeconds {
		return "chat"
	}
	return "holodex"
}

func endSourceForComment(cs dto.CommentSong) string {
	if cs.End <= 0 {
		return "unknown"
	}
	if cs.ChatEnd > 0 && cs.ChatEnd == cs.End {
		return "chat"
	}
	if cs.IsEndTimeEstimated {
		return "next_start" // 推定値。確度は低い（画面でも由来なしとして扱われる）
	}
	return "comment"
}

func applyMatched(row *fillRow, id, name, artist *string) {
	if id == nil {
		return
	}
	parsed, err := uuid.Parse(*id)
	if err != nil {
		return
	}
	row.SongID = &parsed
	row.Via = "rule"
	row.Confidence = 1
	if name != nil {
		row.Name = *name
	}
	if artist != nil && *artist != "" {
		row.Artist = *artist
	}
}

// overlapsExisting は同じ時間帯に既存の歌唱があるか。
// 一意制約が (stream_id, song_id, start_seconds) なので、秒がずれた重複は防げない。
// 30 秒の窓で見て、既にあるものは触らない。
func overlapsExisting(existing []repository.PerformanceWithDetails, row *fillRow) bool {
	for _, e := range existing {
		if abs(e.StartSeconds-row.Start) <= 30 {
			return true
		}
	}
	return false
}

func sameAsExisting(existing []repository.PerformanceWithDetails, row *fillRow) bool {
	for _, e := range existing {
		if abs(e.StartSeconds-row.Start) <= 30 {
			return row.SongID != nil && e.SongID == *row.SongID
		}
	}
	return false
}

func joinReasons(reasons []string) string {
	labels := map[string]string{
		reviewNoEnd:       "終了時間が無い",
		reviewNoArtist:    "歌手が未記入",
		reviewUnmatched:   "曲が決まらない",
		reviewMultiSinger: "歌手が複数",
		reviewConflict:    "既存と食い違う",
		reviewLowConf:     "AI の確信度が低い",
	}
	out := ""
	for i, r := range reasons {
		if i > 0 {
			out += "・"
		}
		if l, ok := labels[r]; ok {
			out += l
		} else {
			out += r
		}
	}
	return out
}
