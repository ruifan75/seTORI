package service

import (
	"errors"
	"fmt"
	"strings"
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
	reviewNoEnd       = "no_end"       // 終了時間が無い / 推定値でしかない
	reviewNoArtist    = "no_artist"    // 源に歌手が書かれていない
	reviewUnmatched   = "unmatched"    // どの曲か決まらない
	reviewMultiSinger = "multi_singer" // 配信に歌手が複数いる
	reviewConflict    = "conflict"     // 既存の歌唱と食い違う
	reviewLowConf     = "low_conf"     // AI の確信度が足りない
	reviewSourceGap   = "source_gap"   // 源が既存より少ない（取りこぼしを疑う）
	reviewDuplicate   = "duplicate"    // 同じ実行の中で同じ曲が重複している
)

// reliableEndSources は「源が終了時間を持っていた」と言える確度。
//
// no_end の判定はここで行う。**end の有無ではなく確度で見る**のが要点で、
// 以前は row.End <= row.Start だけを見ていたため、次の曲の開始で埋めた推定値
// （end_source = next_start）が「終了時間がある」と扱われ、審査を素通りしていた。
var reliableEndSources = map[string]bool{
	repository.EndSourceChat:    true, // live chat の拍手検出
	repository.EndSourceHolodex: true, // Holodex が明示
	repository.EndSourceComment: true, // コメントに明示されていた
	repository.EndSourceItunes:  true, // iTunes の再生時間から逆算
	repository.EndSourceManual:  true, // 人が入力した
}

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

	// RawName / RawArtist は**源から抽出したままの表記**。照合や AI 正規化で
	// Name / Artist が書き換わっても、こちらは動かさない。
	//
	// no_artist の判定はこちらで行う。以前は照合の後の Artist を見ていたので、
	// DB の楽曲から歌手名が補われた行が「歌手が書かれている」と扱われ、
	// 「AI に歌手を推測させない」という方針が審査の手前で骨抜きになっていた。
	RawName   string
	RawArtist string

	EndSource  string // 終了時間の由来（docs/DATA_COMPLETION.md の語彙）
	SongID     *uuid.UUID
	Via        string // rule / ai
	Confidence float64
	AIReason   string
	Review     []string

	ai *aiMatchRow // AI に回した行への参照（判定後に読む）
}

func (r *fillRow) needsReview() bool { return len(r.Review) > 0 }

func (r *fillRow) addReview(reason string) {
	for _, existing := range r.Review {
		if existing == reason {
			return
		}
	}
	r.Review = append(r.Review, reason)
}

// hasReliableEnd は源が終了時間を持っていたか。推定値（next_start）は持っていない扱い。
func (r *fillRow) hasReliableEnd() bool {
	return r.End > r.Start && reliableEndSources[r.EndSource]
}

// Start はジョブを開始する。
//
// singerIDs は対象チャンネル（空なら全部）。既定はそのチャンネルが**所有する**配信で、
// includeCollabs を立てるとゲスト参加した配信も含む。
func (s *BatchFillService) Start(mode string, singerIDs []string, includeCollabs bool, startedBy *uuid.UUID) (uuid.UUID, error) {
	switch mode {
	case BatchFillModeUnprocessed, BatchFillModeForce:
	default:
		return uuid.Nil, errors.New("無効なモードです（unprocessed / force）")
	}
	singerIDs = trimIDs(singerIDs)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return uuid.Nil, ErrBatchFillAlreadyRunning
	}

	var sid *string
	if len(singerIDs) > 0 {
		joined := strings.Join(singerIDs, ",")
		sid = &joined
	}
	runID, err := s.runRepo.CreateRun(mode, sid, startedBy)
	if err != nil {
		return uuid.Nil, err
	}

	s.running = true
	s.cancelled = false
	s.status = dto.BatchFillStatus{
		Running: true, Mode: mode, SingerIDs: singerIDs,
		IncludeCollabs: includeCollabs, RunID: runID.String(),
	}

	go s.run(runID, mode, singerIDs, includeCollabs)
	return runID, nil
}

// trimIDs は空文字を落として重複を除く。
func trimIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
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

// ListGaps は実行が見つけた「DB にあるが源に無い」歌唱を返す。
func (s *BatchFillService) ListGaps(runID uuid.UUID) ([]repository.BatchFillGap, error) {
	return s.runRepo.ListGaps(runID)
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

func (s *BatchFillService) run(runID uuid.UUID, mode string, singerIDs []string, includeCollabs bool) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.status.Running = false
		s.status.Current = ""
		s.mu.Unlock()
	}()

	streams, err := s.streamRepo.FindStreamsForFill(mode, singerIDs, includeCollabs)
	if err != nil {
		logger.Warnf("[batch-fill] 対象の取得に失敗: %v", err)
		s.finish(runID, "failed", "対象の取得に失敗しました")
		return
	}
	s.update(func(st *dto.BatchFillStatus) { st.Total = len(streams) })
	scope := "全チャンネル"
	if len(singerIDs) > 0 {
		scope = fmt.Sprintf("%d チャンネル（%s）", len(singerIDs),
			map[bool]string{true: "コラボ含む", false: "所有配信のみ"}[includeCollabs])
	}
	logger.Infof("[batch-fill] 開始: mode=%s %s %d 配信", mode, scope, len(streams))

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
	created, review, gaps := 0, 0, 0
	for streamID, rows := range byStream {
		if s.isCancelled() {
			break
		}
		res := s.applyStream(runID, streamID, rows, mode)
		created += res.created
		review += res.review
		gaps += res.gaps
		s.update(func(st *dto.BatchFillStatus) {
			st.Created = created
			st.Review = review
			st.Gaps = gaps
			st.AIAsked = asked
		})
		if err := s.runRepo.UpdateProgress(runID, len(streams), s.Status().Done, created, review, gaps, asked); err != nil {
			logger.Warnf("[batch-fill] 進捗の保存に失敗: %v", err)
		}
	}

	status, msg := "done", fmt.Sprintf("%d 曲を作成、%d 曲を審査へ", created, review)
	if gaps > 0 {
		msg += fmt.Sprintf("（源に無い既存 %d 曲）", gaps)
	}
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
			r.addReview(reviewSourceGap)
		}
		logger.Infof("[batch-fill] %s: Holodex %d 曲 / コメント %d 曲 ── 差が大きいので審査へ",
			streamID, len(holodexRows), len(commentRows))
	}
	return holodexRows
}

// applyResult は 1 配信を反映した結果。
type applyResult struct {
	created int // 自動で作った歌唱
	review  int // 人の審査へ回した曲
	gaps    int // DB にあるが源に無かった歌唱（force のみ）
}

// applyStream は 1 配信ぶんを反映する。
func (s *BatchFillService) applyStream(runID uuid.UUID, streamID string, rows []*fillRow, mode string) applyResult {
	// 配信に歌手が複数いるなら、誰が歌ったかを機械では埋められない。全行を審査へ。
	multi := s.hasMultipleSingers(streamID)
	singerIDs := s.perfService.defaultSingerIDs(streamID)

	existing, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		logger.Warnf("[batch-fill] 既存歌唱の取得に失敗 (%s): %v", streamID, err)
		return applyResult{}
	}

	var toCreate []dto.CreatePerformanceItem
	var origins []repository.BatchOrigin
	reviewCount := 0
	// この実行がこの配信に作る予定の行。**同じ実行の中の重複を弾くために要る**
	// ── 一意制約は (stream_id, song_id, start_seconds) の完全一致しか止めないので、
	// 源に数秒ずれた同じ曲が 2 行あると 2 件できてしまう。
	var planned []*fillRow

	for _, row := range rows {
		s.resolveFromAI(row)
		if multi {
			row.addReview(reviewMultiSinger)
		}
		if !row.hasReliableEnd() {
			row.addReview(reviewNoEnd)
		}
		// 歌手は**源に書かれていたか**で見る（照合で補われた値では見ない）
		if row.RawArtist == "" {
			row.addReview(reviewNoArtist)
		}
		if row.SongID == nil {
			row.addReview(reviewUnmatched)
		}
		if dup := findDuplicateRow(planned, row); dup != nil {
			// 同じ実行の中で同じ曲が重なった。片方は源の重複なので人に見せる。
			row.addReview(reviewDuplicate)
		}

		switch diff := diffAgainstExisting(existing, row); diff {
		case existingSame:
			// 既にまったく同じ内容がある。何もしない（審査にも積まない）。
			continue
		case existingDiffers:
			// 既にある歌唱は黙って書き換えない（force でも）。中身が違うので審査へ。
			row.addReview(reviewConflict)
		case existingAbsent:
			if len(existing) > 0 {
				// 既にセットリストがある配信には**書き足さない**。
				// CreatePerformances はセットリスト全体を受け取って差分を取るので、
				// 書き足すには既存分も送り直すことになり、その過程で既存の歌唱が
				// 曲名から引き直される（別の曲に付け替わりうる）。人の作ったものを
				// 機械の都合で作り直さない ── 追加は提案として出す。
				row.addReview(reviewConflict)
			}
		}

		if row.needsReview() {
			if s.createReviewSuggestion(runID, row, singerIDs) {
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
			SingerIDs:      singerIDs,
			ItunesID:       row.ItunesID,
			EndSource:      row.EndSource,
			EndConfirmed:   &endConfirmed,
		})
		origins = append(origins, repository.BatchOrigin{
			SongID: *row.SongID, StartSeconds: row.Start, Via: row.Via, Confidence: row.Confidence,
		})
		planned = append(planned, row)
	}

	// force で「既存にあるが源に無い」曲があれば実行に紐づけて残す。
	//
	// **提案としては積まない** ── 源（Holodex のセットリストもコメントも）は欠けているのが
	// 普通なので、欠落 1 件ごとに審査待ちを作ると人が処理できない量になり、しかも
	// 「源に無い」だけでは何をすべきか決まらない（消すべきとは限らない）。
	// かといってログに出すだけでは誰も気付けないので、実行履歴から辿れるようにする。
	gaps := 0
	if mode == BatchFillModeForce && len(existing) > 0 {
		missing := existingNotInSource(existing, rows)
		if len(missing) > 0 {
			gaps = len(missing)
			if err := s.runRepo.RecordGaps(runID, streamID, missing); err != nil {
				logger.Warnf("[batch-fill] 源に無い歌唱の記録に失敗 (%s): %v", streamID, err)
			}
			logger.Infof("[batch-fill] %s: 既存 %d 曲のうち %d 曲は源に無い",
				streamID, len(existing), gaps)
		}
	}

	if len(toCreate) == 0 {
		return applyResult{review: reviewCount, gaps: gaps}
	}
	if _, err := s.perfService.CreatePerformances(streamID, toCreate); err != nil {
		logger.Warnf("[batch-fill] 歌唱の作成に失敗 (%s): %v", streamID, err)
		return applyResult{review: reviewCount, gaps: gaps}
	}
	if err := s.runRepo.MarkOrigins(streamID, runID, origins); err != nil {
		logger.Warnf("[batch-fill] 由来の記録に失敗 (%s): %v", streamID, err)
	}
	return applyResult{created: len(origins), review: reviewCount, gaps: gaps}
}

// resolveFromAI は AI の判定を行へ写す。確信度が足りなければ採用しない。
//
// 採否にかかわらず候補と理由は行に残す。審査へ回った行で「AI が何を見て何と言ったか」を
// 画面に出せないと、人は結局ゼロから調べ直すことになる。
func (s *BatchFillService) resolveFromAI(row *fillRow) {
	if row.ai == nil {
		return
	}
	if row.ai.Why != "" {
		row.AIReason = row.ai.Why
	}
	if row.SongID != nil || row.ai.SongID == nil {
		return
	}
	if row.ai.Confidence < batchFillMinConfidence {
		row.addReview(reviewLowConf)
		row.Confidence = row.ai.Confidence
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
//
// **照合の結果（song_id）と監査情報を payload に載せる。** 載せていなかった頃は、
// 承認が曲名から引き直していたので一括の照合が承認の瞬間に捨てられ、
// 審査画面にも「なぜこの行が来たのか」を出す材料が無かった。
func (s *BatchFillService) createReviewSuggestion(runID uuid.UUID, row *fillRow, singerIDs []string) bool {
	if s.suggestions == nil {
		return false
	}
	payload := &dto.MissingSongPayload{
		StreamID:       row.StreamID,
		SongName:       row.Name,
		OriginalArtist: row.Artist,
		StartSeconds:   row.Start,
		EndSeconds:     row.End,
		Tags:           row.Tags,
		ItunesID:       row.ItunesID,
		EndSource:      row.EndSource,
		ReviewReasons:  row.Review,
		Source:         row.Source,
		Via:            row.Via,
		Confidence:     row.Confidence,
		AIReason:       row.AIReason,
		BatchRunID:     runID.String(),
		RawName:        row.RawName,
		RawArtist:      row.RawArtist,
		Candidates:     row.candidateDTOs(),
	}
	if row.SongID != nil {
		payload.SongID = row.SongID.String()
	}
	// 歌手が 1 人に決まる配信なら既定として入れておく（審査画面で外せる）。
	// 複数人の配信では空のまま出す ── 誰が歌ったかは機械では決められない。
	if !containsReason(row.Review, reviewMultiSinger) {
		payload.SingerIDs = singerIDs
	}

	_, err := s.suggestions.Create(&dto.CreateSuggestionRequest{
		Kind:    KindMissingSong,
		Payload: payload,
		Note:    fmt.Sprintf("一括作成の審査待ち（%s・源: %s）", joinReasons(row.Review), row.Source),
	}, SuggestionActor{ClientHint: "batch-fill", System: true})
	if err != nil {
		if errors.Is(err, ErrDuplicateSuggestion) {
			// もう待ち行列に居る。数えないだけで、失敗ではない。
			return false
		}
		logger.Warnf("[batch-fill] 審査の登録に失敗 (%s / %s): %v", row.StreamID, row.Name, err)
		return false
	}
	return true
}

// candidateDTOs は AI に見せた候補を審査画面用の形へ写す。
func (r *fillRow) candidateDTOs() []dto.SongMatchCandidate {
	if r.ai == nil || len(r.ai.Candidates) == 0 {
		return nil
	}
	out := make([]dto.SongMatchCandidate, 0, len(r.ai.Candidates))
	for _, c := range r.ai.Candidates {
		out = append(out, dto.SongMatchCandidate{
			SongID:  c.Song.ID.String(),
			Name:    c.Song.Name,
			Artist:  c.Song.OriginalArtist,
			Score:   c.Score,
			Reason:  c.Reason,
			ArtURL:  c.Song.Arts.String,
			IsMatch: r.SongID != nil && c.Song.ID == *r.SongID,
		})
	}
	return out
}

func containsReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func (s *BatchFillService) hasMultipleSingers(streamID string) bool {
	participants, _, err := s.streamRepo.GetSingersForStreams([]string{streamID})
	if err != nil {
		return true // 分からないときは安全側（人に見せる）
	}
	return len(participants[streamID]) > 1
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
		// 抽出したままの表記。no_artist の判定と審査画面の「元の値」に使う。
		RawName: sg.Name, RawArtist: sg.OriginalArtist,
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
		RawName: cs.Name, RawArtist: cs.OriginalArtist,
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

// 既存の歌唱と源の行を突き合わせた結果。
type existingDiff int

const (
	existingAbsent  existingDiff = iota // 同じ時間帯に既存の歌唱が無い
	existingSame                        // 曲も時間も一致（何もしなくてよい）
	existingDiffers                     // 同じ時間帯にあるが中身が違う（審査へ）
)

// fillMatchWindow は「同じ曲を指している」とみなす開始秒の窓。
const fillMatchWindow = 30

// fillTimeTolerance は時間の食い違いとみなす秒。源によって数秒の揺れは普通にあるので、
// これ以内なら「同じ」とみなす（毎回この差で審査に積むと人が処理できない）。
const fillTimeTolerance = 3

// diffAgainstExisting は既存の歌唱と源の行を突き合わせる。
//
// 以前は「曲 ID が一致するか」しか見ておらず、開始・終了が何十秒ずれていても
// same 扱いで黙って飛ばしていた。force は「既存と食い違う分を審査へ回す」ための
// モードなので、歌単として比べられる要素（曲・開始・終了）をすべて見る。
func diffAgainstExisting(existing []repository.PerformanceWithDetails, row *fillRow) existingDiff {
	for _, e := range existing {
		if abs(e.StartSeconds-row.Start) > fillMatchWindow {
			continue
		}
		if row.SongID == nil || e.SongID != *row.SongID {
			return existingDiffers // 同じ時間帯に別の曲がある（または曲が決まっていない）
		}
		if abs(e.StartSeconds-row.Start) > fillTimeTolerance {
			return existingDiffers
		}
		// 終了時間は源が持っているときだけ比べる。源に無いものを「食い違い」とは言えない。
		if row.hasReliableEnd() && abs(e.EndSeconds-row.End) > fillTimeTolerance {
			return existingDiffers
		}
		return existingSame
	}
	return existingAbsent
}

// findDuplicateRow はこの実行が既に作る予定の行のうち、同じ曲で時間が重なるものを返す。
func findDuplicateRow(planned []*fillRow, row *fillRow) *fillRow {
	for _, p := range planned {
		if abs(p.Start-row.Start) > fillMatchWindow {
			continue
		}
		if row.SongID != nil && p.SongID != nil && *p.SongID == *row.SongID {
			return p
		}
		if row.SongID == nil && p.SongID == nil && songmatch.TitleKey(p.Name) == songmatch.TitleKey(row.Name) {
			return p
		}
	}
	return nil
}

// existingNotInSource は「DB にあるが源に無い」歌唱の ID を返す（force の突き合わせ用）。
func existingNotInSource(existing []repository.PerformanceWithDetails, rows []*fillRow) []uuid.UUID {
	var out []uuid.UUID
	for _, e := range existing {
		found := false
		for _, row := range rows {
			if abs(e.StartSeconds-row.Start) <= fillMatchWindow {
				found = true
				break
			}
		}
		if !found {
			out = append(out, e.ID)
		}
	}
	return out
}

func joinReasons(reasons []string) string {
	labels := map[string]string{
		reviewNoEnd:       "終了時間が無い",
		reviewNoArtist:    "歌手が未記入",
		reviewUnmatched:   "曲が決まらない",
		reviewMultiSinger: "歌手が複数",
		reviewConflict:    "既存と食い違う",
		reviewLowConf:     "AI の確信度が低い",
		reviewSourceGap:   "源の取りこぼしの疑い",
		reviewDuplicate:   "同じ曲が重複",
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
