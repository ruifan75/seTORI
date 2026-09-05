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
	chapterService *ChapterService
	normalization  *NormalizationService
	perfService    *PerformanceService
	suggestions    *SuggestionService

	mu        sync.Mutex
	running   bool
	cancelled bool
	status    dto.BatchFillStatus
}

const (
	// BatchFillModeUnprocessed は歌唱がまだ 1 つも無く、**かつ処理済みでない**配信を埋める。
	// 人が「処理した」と言ったものは、歌唱が 0 件でも触らない
	// （確認して「この配信に歌は無い」と判断した結果を毎回やり直さないため）。
	BatchFillModeUnprocessed = "unprocessed"
	// BatchFillModeForce は入力元を持つ配信すべてを見る（処理済みも含む）。
	// 既存と食い違う分は審査へ回す。**明示的に「全部もう一度考え直す」ための口**なので、
	// 人の裁定では絞らない。
	BatchFillModeForce = "force"

	// 自動で歌唱を作ってよい AI の確信度。これ未満は人の審査へ回す。
	batchFillMinConfidence = 0.85
	// 配信間のインターバル（AI とデータベースへの負荷を平す）
	batchFillInterval = 1 * time.Second
)

// 審査へ回す理由。画面にそのまま出すので、増やすときは表示側も見ること。
const (
	reviewNoEnd       = "no_end"       // 終了時間が無い / 推定値でしかない
	reviewNoArtist    = "no_artist"    // 入力元に歌手が書かれていない
	reviewUnmatched   = "unmatched"    // どの曲か決まらない
	reviewMultiSinger = "multi_singer" // 配信に歌手が複数いる
	reviewConflict    = "conflict"     // 既存の歌唱と食い違う
	reviewLowConf     = "low_conf"     // AI の確信度が足りない
	reviewCommentOnly = "comment_only" // Holodex に無く、コメントにだけあった曲
	reviewChapterOnly = "chapter_only" // 配信者が付けたチャプターだけを頼りに拾った曲
	reviewAddition    = "addition"     // 既にセットリストがある配信への追加
	reviewDuplicate   = "duplicate"    // 同じ実行の中で同じ曲が重複している
)

// reliableEndSources は「入力元が終了時間を持っていた」と言える確度。
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

// ErrBatchFillCancelled は予約中に停止が要求されたため開始しなかった。
// **エラーだが異常ではない** ── 次の実行で拾えばよい。
var ErrBatchFillCancelled = errors.New("停止が要求されたため開始しませんでした")

func NewBatchFillService(
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
	runRepo *repository.BatchFillRepository,
	commentService *CommentService,
	holodexService *HolodexService,
	chapterService *ChapterService,
	normalization *NormalizationService,
	perfService *PerformanceService,
	suggestions *SuggestionService,
) *BatchFillService {
	return &BatchFillService{
		streamRepo: streamRepo, perfRepo: perfRepo, runRepo: runRepo,
		commentService: commentService, holodexService: holodexService, chapterService: chapterService,
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
	Source   string // holodex / comment / chapter

	// RawName / RawArtist は**入力元から抽出したままの表記**。照合や AI 正規化で
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

	// ConflictKind / Existing は既存の歌唱と突き合わせた結果。
	// 「食い違う」だけでは何を見ればいいか分からないので、どこが違うのかと相手を持ち越す。
	ConflictKind string
	Existing     *repository.PerformanceWithDetails

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

// hasReliableEnd は入力元が終了時間を持っていたか。推定値（next_start）は持っていない扱い。
func (r *fillRow) hasReliableEnd() bool {
	return r.End > r.Start && reliableEndSources[r.EndSource]
}

// Start はジョブを開始する。
//
// singerIDs は対象チャンネル（空なら全部）。既定はそのチャンネルが**所有する**配信で、
// includeCollabs を立てるとゲスト参加した配信も含む。
// Reserve は「これから開始する」を**原子的に**押さえる。
//
// **入力を書き換える前に呼ぶこと。** 「走っていないか確認 → 入力を更新 → 開始」
// の順では、確認と開始の間に手動の一括が始まりうる。その一括は古い入力
// （raw=A）を読み込んだあとで更新（raw=B）を受け、**A のまま歌唱を作る**。
// 開始を弾いても手遅れで、二重起動を防ぐだけでは足りない。
//
// 押さえたら必ず StartReserved か Release を呼ぶこと（呼ばないと以後
// 一括が一切開始できなくなる）。
func (s *BatchFillService) Reserve() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	// **前回の停止要求を持ち越さない。** 実行終了時の後始末は running しか
	// 落とさないので、手動の一括を止めたあと cancelled=true が残る。
	// 拾うと、次の自動処理が同期を全部やってから「停止要求」で見送ることになる。
	s.cancelled = false
	s.status = dto.BatchFillStatus{Running: true, Mode: "reserved"}
	return true
}

// Cancelled は停止が要求されたかを返す。
//
// **予約中も画面には停止ボタンが出る**（status.Running=true にするため）。
// 予約の所有者がこれを見ないと、「停止しました」と答えたのに処理が続く。
func (s *BatchFillService) Cancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// Release は Reserve を取り消す（開始しないと決めたとき）。
func (s *BatchFillService) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.cancelled = false
	s.status = dto.BatchFillStatus{}
}

// StartReserved は Reserve 済みの前提で開始する。
func (s *BatchFillService) StartReserved(mode string, singerIDs []string, includeCollabs bool, startedBy *uuid.UUID) (uuid.UUID, error) {
	return s.start(mode, singerIDs, includeCollabs, startedBy, true)
}

func (s *BatchFillService) Start(mode string, singerIDs []string, includeCollabs bool, startedBy *uuid.UUID) (uuid.UUID, error) {
	return s.start(mode, singerIDs, includeCollabs, startedBy, false)
}

func (s *BatchFillService) start(mode string, singerIDs []string, includeCollabs bool, startedBy *uuid.UUID, reserved bool) (uuid.UUID, error) {
	switch mode {
	case BatchFillModeUnprocessed, BatchFillModeForce:
	default:
		if reserved {
			s.Release()
		}
		return uuid.Nil, errors.New("無効なモードです（unprocessed / force）")
	}
	singerIDs = trimIDs(singerIDs)

	s.mu.Lock()
	defer s.mu.Unlock()
	// 予約済みなら running は既に立っている（立てたのは自分）。
	if s.running && !reserved {
		return uuid.Nil, ErrBatchFillAlreadyRunning
	}
	// **予約中に入った停止要求をここで見る。** 呼び出し側が直前に確認しても、
	// 確認とロック取得の間に cancel が入りうる（同じ TOCTOU）。同じロックの
	// 中で見て断るのが唯一の閉じ方。予約は解放して次の実行へ回す。
	if reserved && s.cancelled {
		s.running = false
		s.cancelled = false
		s.status = dto.BatchFillStatus{}
		return uuid.Nil, ErrBatchFillCancelled
	}

	var sid *string
	if len(singerIDs) > 0 {
		joined := strings.Join(singerIDs, ",")
		sid = &joined
	}
	runID, err := s.runRepo.CreateRun(mode, sid, startedBy)
	if err != nil {
		if reserved {
			// 予約したまま抜けると、以後一括が一切開始できなくなる。
			s.running = false
			s.status = dto.BatchFillStatus{}
		}
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

// ListGaps は実行が見つけた「DB にあるが入力元に無い」歌唱を返す。
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

	// ---- 第 1 段：入力元を読み、規則で照合できるところまで進める ----
	byStream := map[string][]*fillRow{}
	var unresolved []*aiMatchRow
	dedupe := map[string]*aiMatchRow{} // 同じ表記は 1 回だけ聞く

	for _, stream := range streams {
		if s.isCancelled() {
			s.finish(runID, "cancelled", "キャンセルされました")
			return
		}
		s.update(func(st *dto.BatchFillStatus) { st.Current = stream.ID })

		rows, ok := s.loadRows(stream.ID)
		if !ok {
			// 入力元を確定できなかった配信。Done に数えると「扱った」ように見えるので
			// 失敗として残す（別ソースで代用したセットリストを作るよりよい）。
			s.update(func(st *dto.BatchFillStatus) {
				st.Skipped++
				st.SkippedIDs = append(st.SkippedIDs, stream.ID)
			})
			continue
		}
		if len(rows) == 0 {
			s.update(func(st *dto.BatchFillStatus) { st.Done++ })
			continue
		}
		for _, row := range rows {
			if row.SongID != nil {
				continue
			}
			// 同じ表記の行はひとつの問いにまとめる（楽曲カタログを何度も送らないため）
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
	// 配信ごとに聞くと楽曲カタログ（約 12k トークン）を配信の数だけ送ることになる。
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
		msg += fmt.Sprintf("（入力元に無い既存 %d 曲）", gaps)
	}
	if s.isCancelled() {
		status, msg = "cancelled", "キャンセルされました（途中まで反映済み）"
	}
	s.finish(runID, status, msg)
	logger.Infof("[batch-fill] 終了: %s", msg)
}

// loadRows は配信の入力元を読んで作業単位に落とす。Holodex を優先する。
//
// Holodex の曲は **iTunes ID を持っている**（最も強い証拠で、曲名が食い違っていても照合できる）。
// ただし Holodex のセットリストは欠けていることがあるので、コメント側の曲数が明らかに多ければ
// その配信は人に見てもらう（黙って少ない方を採らない）。
// loadRows は 1 配信ぶんの入力元を読む。
//
// 第 2 返り値が false なら「この配信は今回は扱えない」。**行が 0 件なのとは違う。**
// コメントが競合で読めなかっただけなのに 0 件として返すと、呼び出し側は
// 入力元が無いと解釈してチャプターや Holodex 単独へ進み、あるはずのコメント固有の曲を
// 落としたセットリストを作ってしまう。
func (s *BatchFillService) loadRows(streamID string) ([]*fillRow, bool) {
	var holodexRows, commentRows []*fillRow

	if sugs, err := s.holodexService.AnalyzeHolodexSongsForBatch(streamID, false); err == nil {
		for _, sg := range sugs {
			holodexRows = append(holodexRows, suggestionToFillRow(streamID, sg))
		}
	} else {
		logger.Warnf("[batch-fill] Holodex 読み込み失敗 (%s): %v", streamID, err)
	}

	resp, err := s.commentService.AnalyzeCommentsForBatch(streamID, false)
	switch {
	case errors.Is(err, ErrCommentRawChanged):
		// 分析中にコメントが差し替わって結果が捨てられた状態。
		// **「コメントが無い」と同じ扱いにしてはいけない** ── 空のまま先へ進むと、
		// Holodex に曲が無ければチャプターへ落ち、曲があってもコメント固有の差分を
		// 丸ごと落とす。入力元が無いのではなく、新しい raw で引き直すべき状態。
		logger.Warnf("[batch-fill] 分析中にコメントが変わりました (%s)。読み直します", streamID)
		retry, rErr := s.commentService.AnalyzeCommentsForBatch(streamID, true)
		switch {
		case errors.Is(rErr, ErrNoStoredComments):
			// **これは「読み直しの失敗」ではない。** 分析中にコメントが空へ差し替わった
			// （取得できなくなった／元から 0 件だった）だけで、他の入力源は生きている。
			// ここで諦めると、先に取得済みの Holodex や章節まで捨てて skipped になる。
			logger.Infof("[batch-fill] %s: コメントが空になりました。他の入力源で続行します", streamID)
			retry = nil
		case rErr != nil || retry == nil:
			// **二度目も駄目ならこの配信ごと諦める。** ここで resp=nil にして先へ進むと、
			// 「コメントを諦めて別ソースで続行」になり、最初に直したかった取りこぼしが戻る。
			logger.Warnf("[batch-fill] コメントの読み直しに失敗 (%s): %v。この配信は今回は扱いません", streamID, rErr)
			return nil, false
		}
		resp = retry
	case err != nil:
		logger.Warnf("[batch-fill] コメント分析に失敗 (%s): %v", streamID, err)
		resp = nil
	}
	// **見送りは「コメントが無い」ではない。** live chat がまだ取得できないので
	// 結論を出さなかった状態で、上の ErrCommentRawChanged と同じ危険がある ──
	// 空のまま先へ進むと、Holodex に曲が無ければチャプターへ落ち、曲があっても
	// コメント固有の差分を丸ごと落とす。**この配信ごと今回は扱わない。**
	// 初回と読み直しの両方を見る（読み直しでも見送りになりうる）。
	if resp != nil && resp.Deferred {
		logger.Infof("[batch-fill] live chat 待ちのため見送り (%s)。この配信は今回は扱いません", streamID)
		return nil, false
	}
	if resp != nil {
		s.normalization.ResolveForDisplay(resp.Songs)
		for _, cs := range resp.Songs {
			commentRows = append(commentRows, commentSongToFillRow(streamID, cs))
		}
	}

	if len(holodexRows) == 0 && len(commentRows) == 0 {
		// **チャプターは他がどちらも空のときだけ。** 配信者が付けた目次なので表記は
		// 信用できるが、区切りは「その曲の場面」であって歌唱そのものではなく（MC を含む）、
		// 曲でない章節も混ざる。Holodex の iTunes ID もコメントの明示 end も
		// 捨ててまで採る理由が無いので、拾えるものが他に無い配信の受け皿に留める。
		//
		// **全行を審査へ回す。** 章節から曲を切り出したのは AI の判定なので、
		// コメントにしか無い行を自動で作らないのと同じ扱いにする。
		chapterRows := s.loadChapterRows(streamID)
		for _, c := range chapterRows {
			c.addReview(reviewChapterOnly)
		}
		if len(chapterRows) > 0 {
			logger.Infof("[batch-fill] %s: Holodex もコメントも無いのでチャプターから %d 曲を審査へ", streamID, len(chapterRows))
		}
		return chapterRows, true
	}

	if len(holodexRows) == 0 {
		return commentRows, true
	}

	// Holodex を入力元に採るが、**コメントにしか無い曲を落とさない**。
	//
	// 以前は曲数の比（1.5 倍以上かつ 3 曲以上の差）で「取りこぼしの疑い」を出していたが、
	// それでは 5 曲 vs 6 曲のような差が素通りする。実際 6SOyUVuOq9k では
	// Holodex 5 曲・コメント 6 曲で、コメントにしか無い `Snow halation` が
	// 提案にも歌唱にもならずに消えていた。**しかも 1 曲だけ足りない（最後の曲を
	// 登録し忘れた）というのが Holodex の欠け方として一番多い**ので、
	// 比率の門で守るのは的が外れている。
	//
	// 数を比べるのをやめ、**時間で 1 曲ずつ突き合わせる**。Holodex 側に対応が無い
	// コメントの行は拾って審査へ回す。コメント抽出には誤検出があるので
	// **自動では作らない**（reviewCommentOnly が付くので必ず人が見る）。
	rows := holodexRows
	extra := 0
	for _, c := range commentRows {
		if hasCounterpart(holodexRows, c.Start) {
			continue
		}
		c.addReview(reviewCommentOnly)
		rows = append(rows, c)
		extra++
	}
	if extra > 0 {
		logger.Infof("[batch-fill] %s: Holodex %d 曲 / コメント %d 曲 ── コメントにしか無い %d 曲を審査へ",
			streamID, len(holodexRows), len(commentRows), extra)
	}
	return rows, true
}

// loadChapterRows は保存済みのチャプターから作業単位を作る。
// **yt-dlp は呼ばない**（AnalyzeChaptersForBatch の約束）。取得は
// `POST /api/chapters/backfill` で先に済ませておく。
func (s *BatchFillService) loadChapterRows(streamID string) []*fillRow {
	if s.chapterService == nil {
		return nil
	}
	resp, err := s.chapterService.AnalyzeChaptersForBatch(streamID)
	if err != nil || resp == nil {
		if err != nil {
			logger.Warnf("[batch-fill] チャプター読み込み失敗 (%s): %v", streamID, err)
		}
		return nil
	}
	s.normalization.ResolveForDisplay(resp.Songs)
	rows := make([]*fillRow, 0, len(resp.Songs))
	for _, cs := range resp.Songs {
		rows = append(rows, chapterSongToFillRow(streamID, cs))
	}
	return rows
}

// hasCounterpart は同じ時間帯（±fillMatchWindow 秒）の行が入力元にあるか。
func hasCounterpart(rows []*fillRow, start int) bool {
	for _, r := range rows {
		if abs(r.Start-start) <= fillMatchWindow {
			return true
		}
	}
	return false
}

// applyResult は 1 配信を反映した結果。
type applyResult struct {
	created int // 自動で作った歌唱
	review  int // 人の審査へ回した曲
	gaps    int // DB にあるが入力元に無かった歌唱（force のみ）
}

// applyStream は 1 配信ぶんを反映する。
func (s *BatchFillService) applyStream(runID uuid.UUID, streamID string, rows []*fillRow, mode string) applyResult {
	// 配信に歌手が複数いるなら、誰が歌ったかを機械では埋められない。全行を審査へ。
	multi := s.hasMultipleSingers(streamID)
	singerIDs := s.perfService.defaultSingerIDs(streamID)

	// 一括セットリスト作成は編集者の操作。秘匿された配信も対象に含める
	// （中身を作れないと、公開してよいと決まったときに何も無い）。
	existing, err := s.perfRepo.FindByStreamID(streamID, repository.EditorAccess)
	if err != nil {
		logger.Warnf("[batch-fill] 既存歌唱の取得に失敗 (%s): %v", streamID, err)
		return applyResult{}
	}

	var toCreate []dto.CreatePerformanceItem
	var origins []repository.BatchOrigin
	reviewCount := 0
	// この実行がこの配信に作る予定の行。**同じ実行の中の重複を弾くために要る**
	// ── 一意制約は (stream_id, song_id, start_seconds) の完全一致しか止めないので、
	// 入力元に数秒ずれた同じ曲が 2 行あると 2 件できてしまう。
	var planned []*fillRow

	for _, row := range rows {
		s.resolveFromAI(row)
		if multi {
			row.addReview(reviewMultiSinger)
		}
		if !row.hasReliableEnd() {
			row.addReview(reviewNoEnd)
		}
		// 歌手は**入力元に書かれていたか**で見る（照合で補われた値では見ない）
		if row.RawArtist == "" {
			row.addReview(reviewNoArtist)
		}
		if row.SongID == nil {
			row.addReview(reviewUnmatched)
		}
		if dup := findDuplicateRow(planned, row); dup != nil {
			// 同じ実行の中で同じ曲が重なった。片方は入力元の重複なので人に見せる。
			row.addReview(reviewDuplicate)
		}

		switch cmp := diffAgainstExisting(existing, row); cmp.diff {
		case existingSame:
			// 既にまったく同じ内容がある。何もしない（審査にも積まない）。
			continue
		case existingDiffers:
			// 既にある歌唱は黙って書き換えない（force でも）。中身が違うので審査へ。
			// どこが違うか（曲 / 開始 / 終了）と相手を持ち越す。
			row.addReview(reviewConflict)
			row.ConflictKind = cmp.kind
			row.Existing = cmp.existing
		case existingAbsent:
			if len(existing) > 0 {
				// 既にセットリストがある配信には**書き足さない**。
				// CreatePerformances はセットリスト全体を受け取って差分を取るので、
				// 書き足すには既存分も送り直すことになり、その過程で既存の歌唱が
				// 曲名から引き直される（別の曲に付け替わりうる）。人の作ったものを
				// 機械の都合で作り直さない ── 追加は提案として出す。
				//
				// これは「食い違い」ではなく「追加」。同じ徽章にまとめると、
				// 突き合わせる相手を探しても見つからず人が混乱する。
				row.addReview(reviewAddition)
				row.ConflictKind = conflictAddition
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

	// force で「既存にあるが入力元に無い」曲があれば実行に紐づけて残す。
	//
	// **提案としては積まない** ── 源（Holodex のセットリストもコメントも）は欠けているのが
	// 普通なので、欠落 1 件ごとに審査待ちを作ると人が処理できない量になり、しかも
	// 「入力元に無い」だけでは何をすべきか決まらない（消すべきとは限らない）。
	// かといってログに出すだけでは誰も気付けないので、実行履歴から辿れるようにする。
	gaps := 0
	if mode == BatchFillModeForce && len(existing) > 0 {
		missing := existingNotInSource(existing, rows)
		if len(missing) > 0 {
			gaps = len(missing)
			if err := s.runRepo.RecordGaps(runID, streamID, missing); err != nil {
				logger.Warnf("[batch-fill] 入力元に無い歌唱の記録に失敗 (%s): %v", streamID, err)
			}
			logger.Infof("[batch-fill] %s: 既存 %d 曲のうち %d 曲は入力元に無い",
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
		ConflictKind:   row.ConflictKind,
		Existing:       existingDTO(row.Existing),
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
		Note:    fmt.Sprintf("一括作成の審査待ち（%s・入力元: %s）", joinReasons(row.Review), row.Source),
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

// existingDTO は突き合わせた既存の歌唱を審査画面用の形へ写す。
func existingDTO(e *repository.PerformanceWithDetails) *dto.ExistingPerformance {
	if e == nil {
		return nil
	}
	return &dto.ExistingPerformance{
		ID:             e.ID.String(),
		SongName:       e.SongName,
		OriginalArtist: e.OriginalArtist,
		StartSeconds:   e.StartSeconds,
		EndSeconds:     e.EndSeconds,
	}
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
		// コメント経路と同じくタグを引き継ぐ。落としていたので、正規化が
		// Short Ver. を検出しても審査へ渡る前に消えていた
		Tags: sg.Tags,
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

// chapterSongToFillRow はチャプター由来の行。コメント由来との違いは Source と end の確度だけ。
func chapterSongToFillRow(streamID string, cs dto.CommentSong) *fillRow {
	row := commentSongToFillRow(streamID, cs)
	row.Source = "chapter"
	row.EndSource = endSourceForChapter(cs)
	return row
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

// endSourceForChapter は章節由来の end の確度。
//
// **拍手で埋まっていない限り next_start にする。** 章節の end は次の章節の開始そのもので、
// 「その曲が終わった時刻」ではない（曲のあとの MC や拍手を含む）。`comment` と同格に
// すると、確度で絞り込む問い合わせが嘘になり、審査を素通りする。
func endSourceForChapter(cs dto.CommentSong) string {
	if cs.End <= 0 {
		return "unknown"
	}
	if cs.ChatEnd > 0 && cs.ChatEnd == cs.End {
		return "chat"
	}
	return "next_start"
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

// 既存の歌唱と入力元の行を突き合わせた結果。
type existingDiff int

const (
	existingAbsent  existingDiff = iota // 同じ時間帯に既存の歌唱が無い
	existingSame                        // 曲も時間も一致（何もしなくてよい）
	existingDiffers                     // 同じ時間帯にあるが中身が違う（審査へ）
)

// fillMatchWindow は「同じ曲を指している」とみなす開始秒の窓。
const fillMatchWindow = 30

// fillTimeTolerance は時間の食い違いとみなす秒。入力元によって数秒の揺れは普通にあるので、
// これ以内なら「同じ」とみなす（毎回この差で審査に積むと人が処理できない）。
const fillTimeTolerance = 3

// diffAgainstExisting は既存の歌唱と入力元の行を突き合わせる。
//
// 以前は「曲 ID が一致するか」しか見ておらず、開始・終了が何十秒ずれていても
// same 扱いで黙って飛ばしていた。force は「既存と食い違う分を審査へ回す」ための
// モードなので、歌唱として比べられる要素（曲・開始・終了）をすべて見る。
// 食い違いの種類。審査画面にそのまま出すので、増やすときは表示側も見ること。
const (
	conflictSong     = "song"     // 同じ時間帯に別の曲がある（または曲が決まっていない）
	conflictStart    = "start"    // 同じ曲だが開始がずれる
	conflictEnd      = "end"      // 同じ曲だが終了がずれる
	conflictAddition = "addition" // 既にセットリストがある配信への追加（相手が居ない）
)

// existingComparison は入力元の行と既存の歌唱を突き合わせた結果。
//
// **どこが違うのかまで返す。** 種類だけ返していた頃は、審査画面に
// 「既存と食い違う」としか出せず、人は何を見ればいいのか分からなかった。
// 判定した側は理由を知っているのだから、捨てずに持ち越す。
type existingComparison struct {
	diff     existingDiff
	kind     string                             // conflictSong / conflictStart / conflictEnd
	existing *repository.PerformanceWithDetails // 突き合わせた相手（absent のときは nil）
}

func diffAgainstExisting(existing []repository.PerformanceWithDetails, row *fillRow) existingComparison {
	for i := range existing {
		e := &existing[i]
		if abs(e.StartSeconds-row.Start) > fillMatchWindow {
			continue
		}
		if row.SongID == nil || e.SongID != *row.SongID {
			return existingComparison{existingDiffers, conflictSong, e}
		}
		if abs(e.StartSeconds-row.Start) > fillTimeTolerance {
			return existingComparison{existingDiffers, conflictStart, e}
		}
		// 終了時間は入力元が持っているときだけ比べる。入力元に無いものを「食い違い」とは言えない。
		if row.hasReliableEnd() && abs(e.EndSeconds-row.End) > fillTimeTolerance {
			return existingComparison{existingDiffers, conflictEnd, e}
		}
		return existingComparison{diff: existingSame, existing: e}
	}
	return existingComparison{diff: existingAbsent}
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

// existingNotInSource は「DB にあるが入力元に無い」歌唱の ID を返す（force の突き合わせ用）。
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
		reviewCommentOnly: "コメントにのみ存在",
		reviewAddition:    "既存の歌唱への追加",
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
