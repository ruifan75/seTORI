package service

import (
	"crypto/sha256"
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
// comment_raw のハッシュをキャッシュキーとし、データが未変更かつ強制実行でなければ保存済みの comment_songs をそのまま返す（AI は呼ばない）。
//
// 利用者が「読み込む」を押した経路。抽出に加えて、決着しなかった行の
// AI 判定（別名義・楽曲の同一性）まで行う ── 押した人はどのみち待っているし、
// これから編集する配信なので判定する価値がある。
func (s *CommentService) AnalyzeComments(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeComments(videoID, force, false, true)
}

// AnalyzeCommentsForBatch は一括プレ分析用。**AI 判定は行わない。**
//
// 一括分析がやるのは「comment_raw → comment_songs」（抽出＋正規化＋拍手 end）まで。
// 照合とその判定は、人がその配信を開いて読み込むときの仕事なので分ける。
//
// 混ぜると、724 本を回すだけで 1 本あたり最大 3 回の AI 呼び出しになり、
// 誰も見ていない配信のために別名義の学習（システム全体の照合に効く）が進んでしまう。
func (s *CommentService) AnalyzeCommentsForBatch(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.analyzeComments(videoID, force, false, false)
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
	return s.analyzeComments(videoID, true, true, false)
}

func (s *CommentService) analyzeComments(videoID string, force, dryRun, adjudicate bool) (*dto.AnalyzeCommentsResponse, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}

	rawHash := hashStoredComments(stream.CommentRaw)

	// キャッシュ命中：comment_songs は現在の comment_raw から計算済みなので、そのまま返して AI は呼ばない
	if !force && rawHash != "" && len(stream.CommentSongs) > 0 {
		cachedHash, _ := s.streamRepo.GetCommentSongsHash(videoID)
		if cachedHash.Valid && cachedHash.String == rawHash {
			var cached []dto.CommentSong
			if err := json.Unmarshal(stream.CommentSongs, &cached); err == nil && len(cached) > 0 {
				// DB 照合だけ現在の状態へ再解決する（AI は打たない）。matched_song_id は
				// 分析時点の DB に依存するため、キャッシュに凍結された古いマッチ／未マッチを補正する。
				// 照合が変わったら保存する。応答にだけ乗せて DB を古いまま残すと、
				// 一覧や集計は誤ったまま、開いた画面だけ正しいという食い違いが残る。
				// hash は据え置く ── 変わったのは照合結果だけで、抽出元のコメントは同じ。
				// 照合は保存された値ではなく、今の DB で計算して返す。
				// 保存しないので書き戻しも要らない（曲が増えれば次に開いたとき自然に直る）。
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
	comments, err := s.getComments(videoID, stream, dryRun)
	if err != nil {
		return nil, err
	}
	// リモートから再取得した場合も、分析結果は実際に使ったコメント内容の hash に結び付ける。
	rawHash = hashComments(comments)

	// 2〜5. 抽出 → 除外・重複排除 → 正規化 → DB 照合（チャプター経路と共通）
	songs, aiWarning, path := s.ExtractSongs(comments)

	// 規則で決まらなかった行を AI に回し、決まったぶんだけ照合し直す。
	//
	// 呼ぶのは新規解析のときだけ ── キャッシュ命中と backfill は AI を呼ばない約束。
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
	if s.chatEndService != nil {
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		songs, _, _ = s.chatEndService.DetectEndsForSongs(videoID, duration, songs)
	}

	// 7. 永続化（comment_songs + 由来のハッシュ）→ 次回からはキャッシュを直接読む
	//
	// AI が失敗した回は保存しない。劣化結果（正規表現のみ・読みやタグ無し）を
	// キャッシュに載せると、既存の良い分析結果を上書きしてしまううえ、
	// 次回以降はキャッシュ命中で AI を呼ばなくなり劣化が固定される。
	// 保存しなければ以前の結果と hash がそのまま残り、復旧を待てる。
	saved := false
	switch {
	case dryRun:
		// 読み取り専用の口。何も書かない
	case rawHash == "":
		// hash が無い（コメントが空など）ので保存対象外
	case aiWarning != "":
		logger.Warnf("[comment] skipping cache write for %s due to AI degradation: %s", videoID, aiWarning)
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
		Songs:   songs,
		Warning: aiWarning,
		Stats:   buildStats(path, dryRun, saved, songs, aliasAsked, aliasLinked),
	}, nil
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
	// 本番データ 14 本・151 行で現行 AI の出力に辞書をかけた実測では、
	// **辞書が落とした行は 0**。落ちた 1 行は構造フィルタ（曲名が "1"）だった。
	// つまり辞書はもう AI の取りこぼしを拾っておらず、誤爆する余地だけが残っている。
	//
	// 辞書が要るのは AI が全部失敗して正規表現だけになったとき（path == "regex"）。
	// そこには判断する者がいないので、当初この辞書が作られた前提がそのまま生きている。
	//
	// 構造フィルタ（絵文字だけ・文字なし・40 字超・数字だけ）は**常に適用する**。
	// 形の判断で AI の判断とは競合せず、ground truth 4156 件で誤殺 0 件を確認済み。
	dictKW := filterKW
	if path != "regex" {
		dictKW = nil
	}
	filteredSongs := comment.FilterSongsWith(parsedSongs, dictKW, keepKW, true)
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

func hashComments(comments []string) string {
	if len(comments) == 0 {
		return ""
	}
	raw, err := json.Marshal(comments)
	if err != nil {
		return ""
	}
	return hashBytes(raw)
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

// HashBackfillResult は comment_songs_hash 補正の結果内訳。
type HashBackfillResult struct {
	Total     int `json:"total"`      // comment_songs を持つ歌枠の数
	Migrated  int `json:"migrated"`   // 旧アルゴリズム hash → 正規化 hash へ書き換えた数
	AlreadyOK int `json:"already_ok"` // 既に正規化 hash（キャッシュが有効）
	Skipped   int `json:"skipped"`    // comment_raw が空 / hash 未設定 / 未知形式で触らなかった数
}

// BackfillCommentSongsHashes は comment_songs_hash を現行の正規化アルゴリズムへ移行する。
//
// 背景: 以前は生の JSONB bytes の sha256 を保存していたが、現在のキャッシュ判定は
// 「unmarshal → json.Marshal → sha256」の正規化 hash を使う。旧形式で保存された
// 歌枠は hash が永遠に一致せず、force=false でも毎回 AI 再分析されていた。
//
// comment_raw は不変なので AI は一切呼ばない。安全のため、保存済み hash が
// 旧アルゴリズム（生bytes sha）と一致する場合のみ正規化 hash へ差し替える。
// 既に正規化済み・hash 未設定・未知形式のものは触らない（冪等・再実行安全）。

func (s *CommentService) BackfillCommentSongsHashes() (HashBackfillResult, error) {
	rows, err := s.streamRepo.FindCommentHashRows()
	if err != nil {
		return HashBackfillResult{}, fmt.Errorf("find comment hash rows: %w", err)
	}

	res := HashBackfillResult{Total: len(rows)}
	for _, row := range rows {
		canonical := hashStoredComments(row.CommentRaw)
		// comment_raw が空／壊れている、または hash 未設定ならキャッシュ対象外なので触らない
		if canonical == "" || !row.Hash.Valid || row.Hash.String == "" {
			res.Skipped++
			continue
		}
		if row.Hash.String == canonical {
			res.AlreadyOK++
			continue
		}
		// 旧アルゴリズム（生 JSONB bytes の sha256）と一致するものだけ移行する。
		// 一致しない = 由来不明なので、既存の comment_songs を誤って「有効」扱いしないよう据え置く。
		if row.Hash.String != hashBytes(row.CommentRaw) {
			res.Skipped++
			logger.Warnf("[comment] hash backfill: %s は未知形式のため据え置き（stored=%s）", row.ID, row.Hash.String)
			continue
		}
		if err := s.streamRepo.UpdateCommentSongsHash(row.ID, canonical); err != nil {
			logger.Warnf("[comment] hash backfill update failed (%s): %v", row.ID, err)
			res.Skipped++
			continue
		}
		res.Migrated++
	}

	logger.Infof("[comment] hash backfill 完了: total=%d migrated=%d already_ok=%d skipped=%d",
		res.Total, res.Migrated, res.AlreadyOK, res.Skipped)
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
func (s *CommentService) getComments(videoID string, stream *models.Stream, dryRun bool) ([]string, error) {
	if stream != nil && len(stream.CommentRaw) > 0 {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err == nil && len(comments) > 0 {
			return comments, nil
		}
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
