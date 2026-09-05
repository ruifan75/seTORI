package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/comment"
	"github.com/ruifan75/setori/pkg/util"
	"time"
)

type CommentService struct {
	holodexService       *HolodexService
	streamRepo           *repository.StreamRepository
	filterKeywordRepo    *repository.FilterKeywordRepository
	aiClient             ai.Chatter // コメントの AI ハイブリッド解析用（複数プロバイダーを順番に使用）
	normalizationService *NormalizationService
	chatEndService       *ChatEndService
	// 候補の確定（人の判断）を別表記として学習させるために要る。
	// 照合そのものは normalizationService 経由で足りるが、書き込みはこちら。
	matchService *SongMatchService
}

func NewCommentService(
	holodexService *HolodexService,
	streamRepo *repository.StreamRepository,
	filterKeywordRepo *repository.FilterKeywordRepository,
	aiClient ai.Chatter,
	normalizationService *NormalizationService,
	chatEndService *ChatEndService,
	matchService *SongMatchService,
) *CommentService {
	return &CommentService{
		holodexService:       holodexService,
		streamRepo:           streamRepo,
		filterKeywordRepo:    filterKeywordRepo,
		aiClient:             aiClient,
		normalizationService: normalizationService,
		chatEndService:       chatEndService,
		matchService:         matchService,
	}
}

var (
	ErrCommentSongNotFound = errors.New("指定された解析結果が見つかりません")
	ErrCommentSongChanged  = errors.New("解析結果が変わっています。画面を再読み込みしてください")
	ErrMatchSongNotFound   = errors.New("指定された楽曲が見つかりません")
	ErrUnlearnableName     = errors.New("この曲名からは照合キーを作れないため確定できません")
)

// AnalyzeComments はコメントから楽曲を分析する：AI 抽出 → 正規化 → live chat の拍手 end → DB 保存。
// comment_raw のハッシュ（＋抽出規則の版）をキャッシュキーとし、未変更かつ強制実行でなければ
// 保存済みの comment_songs を返す。**抽出・正規化は再実行しないが、未決着の照合 AI は走る**
// ── 照合は保存していないので、キャッシュを返すだけでは決まらない行が残るため。
//
// 利用者が「読み込む」を押した経路。抽出に加えて、決着しなかった行の
// AI 判定（別名義・楽曲の同一性）まで行う ── 押した人はどのみち待っているし、
// これから編集する配信なので判定する価値がある。
func (s *CommentService) AnalyzeComments(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeComments(videoID, interactiveAnalyzeOptions(force))
}

// AnalyzeCommentsForBatch は一括プレ分析用。**AI 判定は行わない。**
//
// 一括分析がやるのは「comment_raw → comment_songs」（抽出＋正規化＋拍手 end）まで。
// **規則による照合は ExtractSongs の中で走る**（応答に載せるため。保存はしない）。
// 分けているのは**未決着行の AI 判定**だけで、それは人がその配信を開いて
// 読み込むとき、または一括セットリスト作成（歌唱を作る側）の仕事。
//
// 混ぜると、724 本を回すだけで 1 本あたり最大 3 回の AI 呼び出しになり、
// 誰も見ていない配信のために別名義の学習（システム全体の照合に効く）が進んでしまう。
func (s *CommentService) AnalyzeCommentsForBatch(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeComments(videoID, batchAnalyzeOptions(force))
}

// AnalyzeCommentsDryRun は解析だけ行い、**何も書き込まない**。
//
// comment_songs も、遠隔から取り直したコメントも、別名義の学習も保存しない。
// 本番のデータを触らずに「今のパイプラインがこの配信に何を出すか」を測るための口で、
// 読み取り専用であることが取り柄なので、ここに書き込みを足さないこと。
//
// 別名義の AI 判定は**行わない**（判定は artist_alias_checks への書き込みを伴うため）。
// その分だけ本番の挙動とはズレる。stats.path と alias_pairs_asked=0 で判別できる。
func (s *CommentService) AnalyzeCommentsDryRun(videoID string) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeComments(videoID, dryRunAnalyzeOptions())
}

// analyzeOptions は解析経路の違い。**bool を並べない** ── 呼び出し側が
// `(id, force, false, false, true)` と書く形になると、どれがどれか読めない。
type analyzeOptions struct {
	// Force はキャッシュを無視して作り直す。
	Force bool
	// DryRun は何も書き込まない（測定用）。
	DryRun bool
	// Adjudicate は照合の AI 判定まで行う（対話の読み込みだけ）。
	Adjudicate bool
	// NoRemoteFetch は「保存済みのコメントが空でも遠隔から取りに行かない」。
	//
	// **一括は保存済みの入力を処理する仕組み**で、コメントを取ってくるのは同期と
	// `RefreshCommentRaw` の役目。ここで取りに行くと、別の入力源（Holodex の曲・
	// 章節）で対象に残った配信でも毎回外部呼び出しが起きる ── 会限なら必ず 403 で、
	// 何も得られないまま繰り返す。
	NoRemoteFetch bool
	// ProbeChatFirst は **AI 抽出より先に live chat を探る**。
	//
	// 抽出は AI を使う高い処理なのに、その後で「live chat がまだ無いので
	// 結論を出さない」と分かると結果ごと捨てることになる（PR #36 の Deferred）。
	// しかも見送った回は hash を保存しないので、**次の実行でも必ず再抽出する**
	// ── キャッシュが効く通常の経路と違い、繰り返すたびに AI を呼ぶ。
	//
	// live chat の取得は videoID しか要らない（曲目が要るのは後段の
	// 突き合わせだけ）ので先に探れる。曲になる配信ではコストは増えない
	// ── どのみち後で取りに行くし、取得結果は後段へ渡す。
	// **候補行はあるが曲にならない配信では探りが 1 回無駄になる**
	// （受け入れた取捨。下の注記を参照）。
	//
	// **対話の読み込みでは立てない。** 人はボタンを押した以上、拍手 end が
	// 無くても曲目を今見たい。ここで早退すると編集画面が壊れる。
	ProbeChatFirst bool
}

// batchAnalyzeOptions は一括・自動処理から。**chat を先に探る**のはここだけ。
func batchAnalyzeOptions(force bool) analyzeOptions {
	return analyzeOptions{Force: force, ProbeChatFirst: true, NoRemoteFetch: true}
}

// interactiveAnalyzeOptions は編集画面の読み込みから。
// **早退しない** ── 人はボタンを押した以上、拍手 end が無くても曲目を今見たい。
func interactiveAnalyzeOptions(force bool) analyzeOptions {
	return analyzeOptions{Force: force, Adjudicate: true}
}

// dryRunAnalyzeOptions は測定用（何も書き込まない）。
func dryRunAnalyzeOptions() analyzeOptions {
	return analyzeOptions{Force: true, DryRun: true}
}

func (s *CommentService) analyzeComments(videoID string, opts analyzeOptions) (*dto.AnalyzeCommentsResponse, error) {
	force, dryRun, adjudicate := opts.Force, opts.DryRun, opts.Adjudicate
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}

	rawHash := hashStoredComments(stream.CommentRaw)

	// キャッシュ命中：comment_songs は現在の comment_raw から計算済みなので抽出はやり直さない。
	// **AI の照合判定はここでも走る**（対話 analyze のとき）── 照合は保存していないので、
	// キャッシュを返すだけでは未決着の行が決まらないまま残る。
	if !force {
		cachedHash, _ := s.streamRepo.GetCommentSongsHash(videoID)
		if commentCacheHit(rawHash, cachedHash, stream.CommentSongs) {
			var cached []dto.CommentSong
			if err := json.Unmarshal(stream.CommentSongs, &cached); err == nil && len(cached) > 0 {
				// 照合は**保存された値ではなく、今の DB で計算して返す**。
				// キャッシュに照合結果は入っていない（保存前に stripMatchForStorage が落とす）ので、
				// ここで当て直さないと未照合のまま返ることになる。
				// 書き戻しはしない ── 曲が増えれば次に取り込んだとき自然に直る。
				s.normalizationService.ResolveForDisplay(cached)
				// 抽出のやり直しは不要でも、照合が決着しない行は残りうる。
				// 利用者が読み込みを押した瞬間なので、ここで判定してよい。
				var ca, cl int
				if s.normalizationService != nil && adjudicate && !dryRun {
					ca, cl = s.adjudicateAll(cached)
				}
				logger.Infof("/comments/analyze cache hit for %s (%d songs)", videoID, len(cached))
				return &dto.AnalyzeCommentsResponse{Songs: cached, Stats: buildStats("cache", dryRun, false, cached, ca, cl)}, nil
			}
		}
	}

	// 1. 元コメントを取得する（DB を優先し、なければ YouTube/Holodex から取得）
	logger.Infof("starting comment analysis for %s (force=%v, raw len=%d)", videoID, force, len(stream.CommentRaw))
	comments, err := s.getComments(videoID, stream, dryRun, opts.NoRemoteFetch)
	if err != nil {
		return nil, err
	}
	// リモートから再取得した場合も、分析結果は実際に使ったコメント内容の hash に結び付ける。
	rawHash = hashComments(comments)

	// **AI 抽出より先に live chat を探る**（一括だけ。analyzeOptions.ProbeChatFirst）。
	//
	// ここで早退しないと、抽出（AI）を使い切ってから「今回は結論を出さない」と
	// 分かることになり、その結果は捨てられる。しかも見送った回は hash を
	// 保存しないので、**次の実行でも必ず再抽出する** ── 定期実行では
	// 見送るたびに AI を呼び直すことになる。
	//
	// **候補行が無い配信では探らない。** タイムスタンプらしき行が 1 つも無ければ
	// 曲は必ず 0 件なので、end を付ける対象そのものが無い（`DetectEndsForSongs` は
	// 曲が 0 件なら取得せずに返るので、従来 chat を一切取りに行かなかった配信がある）。
	//
	// **候補があっても曲になるとは限らない** ── `12:34 雑談` のような行は候補には
	// なるが、AI は雑談として落とすので 0 曲になる。その配信では探りが無駄になり、
	// chat が未生成なら見送りにもなる。**これは受け入れた取捨**で、避けるには
	// AI を呼んで曲数を知るしかなく、それでは AI を節約するというこの仕組みの
	// 目的が消える。無駄になるのは yt-dlp の探り 1 回（成功すればキャッシュされる）で、
	// 代わりに節約できるのは AI 抽出そのもの。
	//
	// 取得した結果は**後段へ引き継ぐ** ── 引き継がないと、成功時も同じ JSONL を
	// 2 回読み込んで解析し、replay が無い配信では yt-dlp を 2 回実行することになる。
	var probed ChatLoad
	probeRan := false
	if opts.ProbeChatFirst && s.chatEndService != nil && comment.HasTimestampLines(comments) {
		// **Probe は DetectEnds と同じ検証を通す。** サイズだけ見て「使える」と
		// 判断すると、中身が壊れたキャッシュのときに AI 抽出を使い切ってから
		// transient と分かる ── この先行確認が避けたかった費用をそのまま払う。
		probed, probeRan = s.chatEndService.Probe(videoID), true
		if probed.Outcome() != chatOK {
			if holdCacheForChat(*stream, probed.Outcome(), time.Now()) {
				logger.Infof("[comment] %s: %s。AI 抽出を行わずに見送ります",
					videoID, holdReason(*stream, probed.Outcome(), time.Now()))
				return &dto.AnalyzeCommentsResponse{Deferred: true}, nil
			}
			// 見送らない＝結論にしてよい（十分に古い等）。このまま続ける。
			logger.Infof("[comment] %s: live chat は使えませんが結論として続行します", videoID)
		}
	}

	// 2〜5. 抽出 → 除外・重複排除 → 正規化 → DB 照合（チャプター経路と共通）
	songs, aiWarning, path := s.ExtractSongs(comments)

	// 規則で決まらなかった行を AI に回し、決まったぶんだけ照合し直す。
	//
	// backfill と一括プレ分析からは呼ばない（誰も見ていない配信のために AI を焚かない）。
	// **キャッシュ命中でも呼ぶ** ── 利用者が読み込みを押した瞬間で、
	// 照合は保存していないため未決着の行はキャッシュを返すだけでは決まらない。
	// dry-run では行わない。判定は checks テーブルへの書き込みを伴うため。
	//
	// 抽出がどの経路（統合 / 2 段階 / 正規表現）を通ったかに関わらず呼ぶ。以前は
	// 統合経路の中にだけ置いていて、「2 段階では BatchAINormalization が同じことをする」
	// という前提でそうしていたが、判定が match_ai.go へ集約された時点でその前提は消えていた
	// （＝退避した回だけ判定が走らなかった）。既に照合できた行は AdjudicateCommentSongs が
	// 飛ばすので、二重に聞くことにはならない。
	var aliasAsked, aliasLinked int
	if s.normalizationService != nil && adjudicate && !dryRun {
		aliasAsked, aliasLinked = s.adjudicateAll(songs)
	}

	// 6. live chat の拍手で end を推定（start 基準でマッチ。利用不可なら据え置き）
	//
	// chatState は live chat 取得の結果（到達 / replay 無し / 一時的な障害）。
	// キャッシュすると、その配信の end はコメントの値のまま固定される（下の 7 を参照）。
	chatState := chatOK
	switch {
	case s.chatEndService == nil:
	case probeRan && probed.Outcome() != chatOK:
		// 先行確認で「使えない」と分かっている。**もう一度取りに行かない**
		// ── replay が無い配信では yt-dlp をこの解析の中で 2 回走らせることになる。
		chatState = probed.Outcome()
	default:
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		// 先行確認で取得済みなら**その結果を渡す**（同じ JSONL を 2 回解析しない）。
		songs, _, _, chatState = s.chatEndService.DetectEndsForSongsLoaded(probed, videoID, duration, songs)
	}

	// 7. 永続化（comment_songs + 由来のハッシュ）→ 次回からはキャッシュを直接読む
	//
	// AI が失敗した回は保存しない。劣化結果（正規表現のみ・読みやタグ無し）を
	// キャッシュに載せると、既存の良い分析結果を上書きしてしまううえ、
	// 次回以降はキャッシュ命中で AI を呼ばなくなり劣化が固定される。
	// 保存しなければ以前の結果と hash がそのまま残り、復旧を待てる。
	saved := false
	// deferred … live chat が取れなかったので結論を出さずに見送った。
	// **呼び出し元は完了として数えてはいけない**（下の Deferred を見る）。
	deferred := false
	switch {
	case dryRun:
		// 読み取り専用の口。何も書かない
	case rawHash == "":
		// hash が無い（コメントが空など）ので保存対象外
	case aiWarning != "":
		logger.Warnf("[comment] skipping cache write for %s due to AI degradation: %s", videoID, aiWarning)
	case holdCacheForChat(*stream, chatState, time.Now()):
		deferred = true
		// **配信直後は live chat replay をまだ取得できない。** ここで保存すると
		// hash が入り、次の一括プレ分析はキャッシュ命中で拍手検出まで飛ばすので、
		// この配信の end はコメントに書かれた値のまま固定される。
		// 抽出をやり直すぶん AI を呼び直すことになるが、固定されるよりは安い。
		logger.Warnf("[comment] skipping cache write for %s: %s。次回やり直します",
			videoID, holdReason(*stream, chatState, time.Now()))
	default:
		// 照合の結果は保存しない（読み取り時に計算する）
		songsJSON, mErr := json.Marshal(stripMatchForStorage(songs))
		// 分析に使ったコメントそのものを書き込みのガードに使う。getComments が
		// 遠隔から取り直していれば、その内容が既に comment_raw に入っている。
		rawUsed, rErr := json.Marshal(comments)
		switch {
		case mErr != nil || rErr != nil:
			logger.Warnf("[comment] skipping cache write for %s: marshal failed", videoID)
		default:
			ok, err := s.streamRepo.SaveCommentSongs(videoID, songsJSON, rawHash, rawUsed)
			switch {
			case err != nil:
				logger.Warnf("[comment] save comment_songs failed (%s): %v", videoID, err)
			case !ok:
				// 分析中に comment_raw が差し替わった。古い入力から作った結果は捨てる。
				//
				// **これは成功ではないので呼び出し元へ返す。** ログだけにすると、
				// 一括分析は「err なし・Warning なし」を成功条件にしているため
				// done に数えて再試行せず、新しい raw のキャッシュが空のまま
				// 完了表示だけが進む。呼び出し元が新しい raw で引き直せるように
				// 区別できる形で返す。
				logger.Warnf("[comment] comment_raw changed during analysis (%s); discarding result", videoID)
				return nil, ErrCommentRawChanged
			default:
				saved = true
			}
		}
	}

	logger.Infof("comment analysis completed for %s: %d songs", videoID, len(songs))
	return &dto.AnalyzeCommentsResponse{
		Songs:    songs,
		Warning:  aiWarning,
		Deferred: deferred,
		Stats:    buildStats(path, dryRun, saved, songs, aliasAsked, aliasLinked),
	}, nil
}

// filterScopeForPath は抽出経路に応じて「辞書を使うか」を返す。
//
// production の分岐をここに閉じ込めてあるのはテストのため。呼び出し側で条件を書くと、
// テストが同じ条件を書き写すことになり、production を変えてもテストが通ってしまう。
//
//	grouped / two_stage … AI が is_song を判断済み。辞書も keep も使わない
//	regex               … 判断する者がいない。辞書と keep を使う
//	none                … 候補行が 0。どちらでも結果は変わらない
//
// keep も一緒に外すのは、`filter.go` が keep を数字だけの判定より先に返すため。
// AI 経路で keep だけ残すと、曲名が "1" の行が keep 語に救われて残ることがある。
func filterScopeForPath(path string, filterKW, keepKW []string) (dict, keep []string) {
	if path == "regex" {
		return filterKW, keepKW
	}
	return nil, nil
}

// ErrCommentRawChanged は分析中に comment_raw が差し替わったことを示す。
// 古い入力から作った結果は保存されていないので、呼び出し元は新しい raw で引き直すこと。
var ErrCommentRawChanged = errors.New("分析中に comment_raw が変更されました")

// ExtractSongs は「タイムスタンプ付きのテキスト」から歌唱行を抽出し、正規化して DB と照合する。
//
// **コメント経路とチャプター経路の共通の入口。** 入力元が違うだけで、そこから先
// （どの行が曲か・除外キーワード・重複排除・AI が落ちたときの退避）は同じ判断なので、
// 経路ごとに書くと片方にしか効かない修正が生まれる。`resolveOne` を 1 つに保っているのと
// 同じ理由で、ここも 1 つに保つこと。
//
// 照合結果は**保存しない**（呼び出し側が保存するのは抽出＋正規化まで）。
// 返り値の warning は「AI が失敗して結果が劣化している」ことを示す。空でなければ
// 呼び出し側はキャッシュへ書かないこと ── 劣化結果を保存すると、次回からキャッシュ命中で
// AI を呼ばなくなり劣化が固定される。
func (s *CommentService) ExtractSongs(texts []string) (songs []dto.CommentSong, warning, path string) {
	// 1. AI で抽出（統合経路では正規化と重複排除もここで済む）。失敗時は段階的に退避
	parsedSongs, preNormalized, extractWarning, path := s.parseComments(texts)

	// 2. 除外 + 重複排除 + 検証（楽曲以外の項目が重複排除へ影響しないよう先に除外）
	//
	// 統合経路では AI が既に重複をまとめているが、DeduplicateSongs は通しておく。
	// 開始時刻が離れていれば別の歌唱として残るので取りこぼしは増えず、
	// AI が見落とした重複を拾う安全網として働く。
	filterKW, keepKW, err := s.loadFilterKeywords()
	if err != nil {
		logger.Warnf("failed to load filter keywords, skipping filter: %v", err)
	}

	// **キーワード辞書は AI が歌唱かどうかを判断していない経路にだけ適用する。**
	//
	// grouped も two_stage も、プロンプトで「雑談・開演・終了・告知・スパチャ読み・
	// 実況メモ・絵文字だけの行・リスナーの感想」を除外している。辞書と同じ category を、
	// **行の文脈まで見て**判断しているということ。辞書は文字列しか見えないので、
	// 後段に置くと賢いほうの判断を馬鹿なほうが上書きする。
	//
	// 実害が出ていた：`Week End / 星野源` は `end` に当たって消えていた
	// （3 文字以下の ASCII は単語単位で比較するので "Week End" の End が一致する。
	// `花ざかりWeekend✿` は前が k なので残る）。issue #11。
	//
	// 実測（2026-08-22、本番データ 14 本・151 行）では、現行 AI の出力に辞書をかけて
	// **落ちた行は 0**（落ちた 1 行は構造フィルタ。曲名が "1"）。
	// ただし標本は「直近・表示中・抽出 5 曲以上」なので、grouped が成功しやすい母集団に偏る。
	// 非表示配信、抽出が数行しかない疎な配信、grouped 失敗時の two_stage、
	// 章節の START / 休憩 / BGM は入っていない。**この選び方の 14 本では 0 だった**、
	// までが言えること。それでも `Week End` の誤爆は現に起きているので、
	// 判断済みの経路で辞書を重ねる理由は無い。
	//
	// 辞書が要るのは AI が全部失敗して正規表現だけになったとき（path == "regex"）。
	// そこには判断する者がいないので、当初この辞書が作られた前提がそのまま生きている。
	//
	// 構造フィルタ（絵文字だけ・文字なし・40 字超・数字だけ）は**常に適用する**。
	// 形の判断で AI の判断とは競合せず、ground truth 4156 件で誤殺 0 件を確認済み。
	dictKW, keepForPath := filterScopeForPath(path, filterKW, keepKW)
	filteredSongs := comment.FilterSongsWith(parsedSongs, dictKW, keepForPath, true)
	deduped := comment.DeduplicateSongs(filteredSongs)
	validSongs := comment.ValidateSongs(deduped)

	// 3. CommentSong へ変換する（統合経路なら正規化結果も一緒に運ぶ）
	songs = make([]dto.CommentSong, len(validSongs))
	for i, song := range validSongs {
		songs[i] = dto.CommentSong{
			Start:              song.Start,
			End:                song.End,
			Name:               song.Name,
			OriginalArtist:     song.OriginalArtist,
			OriginalComment:    song.OriginalComment,
			IsEndTimeEstimated: song.IsEndTimeEstimated,

			NormalizedName:          song.NormalizedName,
			NormalizedNameReading:   song.NormalizedNameReading,
			NormalizedArtist:        song.NormalizedArtist,
			NormalizedArtistReading: song.NormalizedArtistReading,
			Tags:                    song.Tags,
			Confidence:              song.Confidence,
		}
	}

	// 4. 正規化と DB 照合
	//
	// 統合経路では正規化が済んでいるので DB 照合だけを行う（AI を再度呼ばない）。
	// 2 段階経路に退避した場合はここで AI 正規化を実行する。
	if preNormalized {
		s.normalizationService.ResolveForDisplay(songs)
	} else {
		warning = s.normalizeInto(songs)
	}
	// 抽出の失敗は正規化の失敗より重い（曲そのものが取れていない）ので優先して伝える
	if extractWarning != "" {
		warning = extractWarning
	}
	return songs, warning, path
}

// adjudicateAll は規則で決まらなかった行を AI に回す。
//
// **利用者が「読み込む」を押した時にだけ通る。** 呼ばない場所が 2 つある。
//   - 配信詳細の GET … 閲覧者が通るだけの経路。見ているだけで AI を呼ぶことになる
//   - 一括プレ分析 … 誰も見ていない配信のために AI を焚くことになる
func (s *CommentService) adjudicateAll(songs []dto.CommentSong) (asked, resolved int) {
	return s.normalizationService.AdjudicateCommentSongs(songs)
}

// buildStats は応答に載せる内訳を作る。ログを読まずに挙動を確かめられるようにするためのもの。
func buildStats(path string, dryRun, saved bool, songs []dto.CommentSong, aliasAsked, aliasLinked int) *dto.AnalyzeStats {
	st := &dto.AnalyzeStats{
		Path: path, DryRun: dryRun, Saved: saved, Extracted: len(songs),
		AliasPairsAsked: aliasAsked, AliasLinksAdded: aliasLinked,
	}
	for i := range songs {
		switch {
		case songs[i].MatchedSongID != nil:
			st.Matched++
		case len(songs[i].MatchCandidates) > 0:
			st.WithCandidates++
		default:
			st.Unmatched++
		}
	}
	return st
}

// strPtrEq は *string 同士を値で比べる（どちらも nil なら等しい）。
func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// matchInputs は照合に渡す名称を返す。正規化結果が無い古いデータは抽出名に落とす。
//
// 分析時に AI が空の曲名を返した行がこれに当たる（a0973fb 以前）。落とし先が無いと
// 空文字で照合して必ず外れる。確定操作もここと同じ入力で別表記を学習しないと、
// 学習した鍵と次回の照合の鍵がずれて当たらなくなる。
func matchInputs(s dto.CommentSong) (name, artist string) {
	return MatchInputs(s.Name, s.OriginalArtist, s.NormalizedName, s.NormalizedArtist)
}

// MatchInputs は matchInputs の中身。ベンチ（cmd/setoribench）が
// **本番とまったく同じ規則**で照合入力を決められるように公開している。
// ここを写し取って二重に持つと、片方を直したときにベンチだけが古い規則で測り続ける。
func MatchInputs(rawName, rawArtist, normName, normArtist string) (name, artist string) {
	name, artist = normName, normArtist
	if name == "" {
		name = rawName
	}
	if artist == "" {
		artist = rawArtist
	}
	return name, artist
}

// normalizeInto は songs を AI 正規化して DB と照合し、各 song に結果を書き戻す（in-place）。
// AI が失敗して抽出のみになった場合は warning 文字列を返す（成功時は空）。
func (s *CommentService) normalizeInto(songs []dto.CommentSong) string {
	if s.normalizationService == nil || len(songs) == 0 {
		return ""
	}
	items := make([]dto.AINormalizationItem, len(songs))
	for i, sg := range songs {
		items[i] = dto.AINormalizationItem{Name: sg.Name, OriginalArtist: sg.OriginalArtist}
	}
	resp, err := s.normalizationService.BatchAINormalization(items)
	if err != nil {
		logger.Warnf("[comment] normalization failed, keeping raw extraction: %v", err)
		return fmt.Sprintf("AI正規化に失敗しました: %v", err)
	}
	if resp.Warning != "" {
		logger.Infof("Normalization used fallback for some items: %s", resp.Warning)
	} else {
		logger.Infof("Batch AI normalization succeeded for %d items", len(items))
	}
	for _, sug := range resp.Suggestions {
		if sug.Index < 0 || sug.Index >= len(songs) {
			continue
		}
		songs[sug.Index].NormalizedName = sug.NormalizedName
		songs[sug.Index].NormalizedNameReading = sug.NormalizedNameReading
		songs[sug.Index].NormalizedArtist = sug.OriginalArtist
		songs[sug.Index].NormalizedArtistReading = sug.OriginalArtistReading
		songs[sug.Index].Tags = sug.Tags
		songs[sug.Index].Confidence = sug.Confidence
		songs[sug.Index].MatchedSongID = sug.MatchedSongID
		songs[sug.Index].MatchedSongName = sug.MatchedSongName
		songs[sug.Index].MatchedSongNameReading = sug.MatchedSongNameReading
		songs[sug.Index].MatchedSongArtist = sug.MatchedSongArtist
		songs[sug.Index].MatchedSongArtistReading = sug.MatchedSongArtistReading
		songs[sug.Index].MatchedSongArtURL = sug.MatchedSongArtURL
		songs[sug.Index].MatchedSongItunesID = sug.MatchedSongItunesID
		// 自動採用に届かなかった候補も持ち越す（編集画面で人に選ばせるため）
		songs[sug.Index].MatchCandidates = sug.MatchCandidates
	}
	// BatchAINormalization の Warning（AI 呼び出し失敗・応答解析失敗）をそのまま伝える
	return resp.Warning
}

// hashBytes は JSONB 内容の sha256 を計算する（空なら空文字列を返す）。
func hashBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashStoredComments は保存済み JSON を正規化して hash 化する。
// null / [] / 壊れた JSON はキャッシュ可能なコメントとして扱わない。
func hashStoredComments(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var comments []string
	if err := json.Unmarshal(raw, &comments); err != nil {
		return ""
	}
	return hashComments(comments)
}

// hashComments は抽出キャッシュの鍵を作る。
//
// **入力だけでなく抽出規則の版も混ぜる**（comment.RulesVersion）。入力の hash だけだと、
// 抽出規則を変えても comment_raw は変わらないのでキャッシュが命中し続け、
// 直したはずの不具合が通常の経路では直らない（辞書が実在の曲を消していた件。issue #11）。
// 版を上げれば保存済みの結果が自動で失効し、次に読み込んだときに作り直される。
func hashComments(comments []string) string {
	if len(comments) == 0 {
		return ""
	}
	raw, err := json.Marshal(comments)
	if err != nil {
		return ""
	}
	return hashBytes(append(raw, extractionRulesSalt()...))
}

// extractionRulesSalt は抽出規則の版を hash へ混ぜるための塩。
// comment / chapter の両方から使う（どちらも同じ ExtractSongs を通るため）。
func extractionRulesSalt() []byte {
	return []byte(fmt.Sprintf("\x1frules=%d", comment.RulesVersion))
}

// RefreshCommentRaw はコメントを取得し直して comment_raw を上書き保存する。
// 分析キャッシュは comment_raw のハッシュをキーにしているため、
// 内容が変わっていれば次回の AnalyzeComments で自動的に再分析される。
func (s *CommentService) RefreshCommentRaw(videoID string) error {
	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return fmt.Errorf("fetch comments: %w", err)
	}
	rawJSON, err := json.Marshal(comments)
	if err != nil {
		return fmt.Errorf("marshal comments: %w", err)
	}
	if err := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(rawJSON)); err != nil {
		return fmt.Errorf("save comment raw: %w", err)
	}
	logger.Infof("[comment] refreshed %d raw comments for %s", len(comments), videoID)
	return nil
}

// SyncYouTubeCommentRaw は YouTube Data API から明示的にコメントを取り直す。
// Holodex fallback は使わず、取得元を保証したうえで comment_raw を上書きする。
func (s *CommentService) SyncYouTubeCommentRaw(videoID string) (int, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return 0, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return 0, fmt.Errorf("stream not found: %s", videoID)
	}

	comments, err := s.holodexService.GetYouTubeVideoComments(videoID)
	if err != nil {
		return 0, fmt.Errorf("fetch comments from YouTube: %w", err)
	}
	rawJSON, err := json.Marshal(comments)
	if err != nil {
		return 0, fmt.Errorf("marshal YouTube comments: %w", err)
	}
	if err := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(rawJSON)); err != nil {
		return 0, fmt.Errorf("save YouTube comments: %w", err)
	}
	logger.Infof("[comment] synced %d raw comments from YouTube for %s", len(comments), videoID)
	return len(comments), nil
}

// BackfillCommentSongs は comment_raw があり comment_songs がない配信をすべて補完する。
func (s *CommentService) BackfillCommentSongs() (int, error) {
	streams, err := s.streamRepo.FindWithoutCommentSongs()
	if err != nil {
		return 0, fmt.Errorf("find streams: %w", err)
	}

	count := 0
	for _, stream := range streams {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err != nil || len(comments) == 0 {
			continue
		}

		parsed := comment.ParseComments(comments)
		if len(parsed) == 0 {
			continue
		}

		songsJSON, err := json.Marshal(parsed)
		if err != nil {
			logger.Warnf("backfill marshal error (video: %s): %v", stream.ID, err)
			continue
		}
		songsJSON = util.SanitizeJSONB(songsJSON)

		if err := s.streamRepo.UpdateCommentSongs(stream.ID, songsJSON); err != nil {
			logger.Warnf("backfill update error (video: %s): %v", stream.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// commentCacheHit は保存済みの抽出結果がそのまま使えるかを返す。
//
// **読み取り経路と監査の両方から呼ぶこと。** 片方だけで条件を書くと、
// 「監査は再解析不要と言うのに実際は再抽出される」というずれが生まれる。
// 実際、この判定を写して書いていた頃の監査は 3 回ずれた
// （v1 を落とす → hash 未設定を落とす → 空の結果を落とす）。
//
// force はここでは見ない。この関数が答えるのは「キャッシュが有効か」で、
// force は「有効でも今回は使わない」という呼び出し側の方針。
// 分けておけば、監査は force の概念を持たずに同じ判定を使える。
//
// rawHash が空（raw が無い／壊れている）ときは命中しない。その場合の読み取り経路は
// キャッシュを外したあと遠隔取得を試みるので、「解析できない」ではなく「取得が要る」。
func commentCacheHit(rawHash string, storedHash sql.NullString, storedSongs []byte) bool {
	if rawHash == "" || len(storedSongs) == 0 {
		return false
	}
	if !storedHash.Valid || storedHash.String != rawHash {
		return false
	}
	var cached []dto.CommentSong
	if err := json.Unmarshal(storedSongs, &cached); err != nil {
		return false
	}
	return len(cached) > 0
}

// HashBackfillResult は comment_songs_hash 補正の結果内訳。
type HashBackfillResult struct {
	Total int `json:"total"` // comment_songs を持つ歌枠の数
	// Migrated は常に 0。書き換えをやめたので残っているのは API 互換のためだけ。
	Migrated  int `json:"migrated"`
	AlreadyOK int `json:"already_ok"` // 現行版の hash。次の解析でキャッシュに命中する
	// Skipped は常に 0。分類を commentCacheHit に一本化したので使わなくなった
	// （raw が無い行も「次の解析で再抽出される」側に入る。取得は読み取り経路が試みる）。
	Skipped int `json:"skipped"`
	// NeedsReanalysis は次に解析したときキャッシュに命中しない数
	// （hash 未設定・旧世代・未知値をすべて含む）。force でなくても再抽出される。
	NeedsReanalysis int `json:"needs_reanalysis"`
}

// BackfillCommentSongsHashes は抽出キャッシュの状態を数える。**書き換えは一切しない（監査用）。**
//
// 元は hash の表記形式を移行する処理だった（生 JSONB bytes の sha256 → 正規化 hash）。
// いまは canonical に抽出規則の版（comment.RulesVersion）が混ざっているので、
// 旧規則で作った comment_songs へ現行版の hash を貼ると
// **辞書に消された曲が消えたまま固定される**（issue #11 の Week End）。
// 表記形式の移行は再抽出なしでできるが、規則の版は結果を計算し直さない限り昇格できない。
//
// **分類は「次に解析したときキャッシュに命中するか」で行う。** 世代を列挙すると、
// 新しい形式が増えたときに監査の答えが実装からずれる。実際、v0 だけを見ていた版は
// 本番の v1 474 行を取りこぼし、hash 未設定の 263 行も数え落としていた。
//
// 判定は commentCacheHit に集約してある（読み取り経路と同じ関数）。写して書くとずれる ──
// 実際 3 回ずれた（v1 を落とす → hash 未設定を落とす → 空の結果を落とす）。
//
//	キャッシュ命中する   … already_ok
//	命中しない           … needs_reanalysis
//
// 「命中しない」には、hash が無い／現行の鍵と違う／保存結果が空・decode 不能／
// ローカルの raw が無い、がすべて入る。最後のものだけは遠隔取得が要る
// （読み取り経路はキャッシュを外したあと getComments を試みる）ので、内訳をログに出す。
func (s *CommentService) BackfillCommentSongsHashes() (HashBackfillResult, error) {
	rows, err := s.streamRepo.FindCommentHashRows()
	if err != nil {
		return HashBackfillResult{}, fmt.Errorf("find comment hash rows: %w", err)
	}

	res := HashBackfillResult{Total: len(rows)}
	var noRaw, noHash, staleHash, emptyResult int
	for _, row := range rows {
		canonical := hashStoredComments(row.CommentRaw)
		if commentCacheHit(canonical, row.Hash, row.CommentSongs) {
			res.AlreadyOK++
			continue
		}

		// 命中しない＝次に解析したとき再抽出される。force でなくても再抽出される。
		res.NeedsReanalysis++

		// 内訳はログにだけ出す（何が起きているかを掴むため）。
		// **世代名は付けない** ── raw bytes と正規化後が一致する行では v0 と v1 の
		// hash が同値になるので、「どちらで作られたか」は hash からは決められない。
		switch {
		case canonical == "":
			noRaw++ // ローカルの raw では判定できない。読み取り経路は遠隔取得を試みる
		case !row.Hash.Valid || row.Hash.String == "":
			noHash++
		case row.Hash.String != canonical:
			staleHash++ // 現行の cache key と一致しない（旧規則・旧形式・未知値のいずれか）
		default:
			emptyResult++ // hash は現行だが、保存結果が空／decode できない
		}
	}

	logger.Infof("[comment] 監査の内訳: raw無し=%d hash無し=%d 鍵が不一致=%d 結果が空=%d",
		noRaw, noHash, staleHash, emptyResult)
	logger.Infof("[comment] hash backfill 完了: total=%d migrated=%d already_ok=%d skipped=%d needs_reanalysis=%d",
		res.Total, res.Migrated, res.AlreadyOK, res.Skipped, res.NeedsReanalysis)
	if res.NeedsReanalysis > 0 {
		logger.Infof("[comment] %d 件は抽出規則の版が古いため hash の付け替えでは救えません。再分析してください", res.NeedsReanalysis)
	}
	return res, nil
}

// loadFilterKeywords は DB から filter/keep キーワードを読み込む。
func (s *CommentService) loadFilterKeywords() (filterKW, keepKW []string, err error) {
	keywords, err := s.filterKeywordRepo.FindAll()
	if err != nil {
		return nil, nil, err
	}

	for _, kw := range keywords {
		switch kw.Type {
		case "filter":
			filterKW = append(filterKW, kw.Keyword)
		case "keep":
			keepKW = append(keepKW, kw.Keyword)
		}
	}

	return filterKW, keepKW, nil
}

// parseComments は編集時にコメントを解析する。3 段階で退避する：
//
//	統合経路（抽出＋正規化＋重複排除を 1 回で）
//	  → 2 段階（抽出のみ。正規化は呼び出し側）
//	    → 正規表現のみ
//
// 返り値：
//   - 抽出結果
//   - preNormalized：正規化まで済んでいるか。true なら呼び出し側は AI 正規化を省き
//     DB 照合だけを行う
//   - warning：AI が失敗して結果が劣化していることを示す文言（成功時は空）
//
// warning を呼び出し側へ伝えないと、AI 障害が「この配信には曲が無い」という見た目で
// 通ってしまい、しかもその空の結果がキャッシュに保存されて既存の分析を上書きする。
func (s *CommentService) parseComments(comments []string) (songs []comment.ParsedSong, preNormalized bool, warning, path string) {
	if s.aiClient != nil {
		// 第1候補：統合経路（抽出＋正規化＋重複排除を 1 回で）
		songs, err := comment.ParseNormalizeAndDedupWithAI(s.aiClient, comments)
		if err == nil {
			logger.Infof("Using grouped AI extraction (%d songs)", len(songs))
			return songs, true, "", "grouped"
		}
		if errors.Is(err, comment.ErrNoTimestampLines) {
			// 解析対象が無いだけで、AI の障害ではない。劣化として扱うと
			// 毎回警告が出るうえキャッシュも書かれず、無駄に再解析し続ける。
			logger.Infof("No timestamp lines in comments; nothing to extract")
			return nil, false, "", "none"
		}
		logger.Warnf("grouped AI extraction failed, falling back to 2-stage: %v", err)

		// 第2候補：従来の 2 段階（抽出のみ。正規化は呼び出し側が行う）
		songs, err = comment.ParseCommentsWithAI(s.aiClient, comments)
		if err == nil {
			logger.Infof("Using AI-extracted songs for analysis (%d songs)", len(songs))
			return songs, false, "", "two_stage"
		}
		logger.Warnf("AI comment parse failed, falling back to regex: %v", err)
		return comment.ParseComments(comments), false,
			fmt.Sprintf("AI抽出に失敗しました（%v）。正規表現のみで解析しました。", err), "regex"
	}
	logger.Infof("Using regex-only comment parse (no AI client configured)")
	return comment.ParseComments(comments), false, "", "regex"
}

// getComments は DB から空でない元コメントを読み込み、なければ YouTube/Holodex から取得して保存する。
// saveRaw=false のとき、遠隔から取り直したコメントを DB に書かない（dry-run 用）。
func (s *CommentService) getComments(videoID string, stream *models.Stream, dryRun, noRemote bool) ([]string, error) {
	if stream != nil && len(stream.CommentRaw) > 0 {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err == nil && len(comments) > 0 {
			return comments, nil
		}
	}
	// **保存済みが空なら、ここで取りに行かない経路がある**（一括）。
	// 取りに行くと、別の入力源で対象に残った配信でも毎回外部呼び出しが起きる。
	if noRemote {
		return nil, nil
	}

	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return nil, fmt.Errorf("get comments: %w", err)
	}
	if raw, marshalErr := json.Marshal(comments); marshalErr == nil && !dryRun {
		if saveErr := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(raw)); saveErr != nil {
			logger.Warnf("save comment raw error (video: %s): %v", videoID, saveErr)
		}
	}

	return comments, nil
}

// GetRawComments は保存済みの生コメント（comment_raw）を返す（編集ページの生コメント表示用）。
// 未保存または空配列のときは YouTube/Holodex から取得し、次回のために保存する。
func (s *CommentService) GetRawComments(videoID string) ([]string, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream != nil && len(stream.CommentRaw) > 0 {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err == nil && len(comments) > 0 {
			return comments, nil
		}
		// 壊れたキャッシュと空配列は無視して取り直す
	}

	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return nil, err
	}
	if raw, err := json.Marshal(comments); err == nil {
		if saveErr := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(raw)); saveErr != nil {
			logger.Warnf("save comment raw error (video: %s): %v", videoID, saveErr)
		}
	}
	return comments, nil
}
