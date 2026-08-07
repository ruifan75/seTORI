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
	aiClient             ai.Chatter // 留言 AI hybrid 解析用（多 provider 輪替）
	normalizationService *NormalizationService
	chatEndService       *ChatEndService
}

func NewCommentService(
	holodexService *HolodexService,
	streamRepo *repository.StreamRepository,
	filterKeywordRepo *repository.FilterKeywordRepository,
	aiClient ai.Chatter,
	normalizationService *NormalizationService,
	chatEndService *ChatEndService,
) *CommentService {
	return &CommentService{
		holodexService:       holodexService,
		streamRepo:           streamRepo,
		filterKeywordRepo:    filterKeywordRepo,
		aiClient:             aiClient,
		normalizationService: normalizationService,
		chatEndService:       chatEndService,
	}
}

// AnalyzeComments 從留言分析歌曲：AI 抽取 → 正規化＋DB 照合 → live chat 拍手 end → 存 DB。
// 以 comment_raw 的 hash 為快取鍵：資料沒變且非強制時，直接回傳已存的 comment_songs（不打 AI）。
func (s *CommentService) AnalyzeComments(videoID string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}

	rawHash := hashStoredComments(stream.CommentRaw)

	// 快取命中：comment_songs 是用「現在這份 comment_raw」算出來的 → 直接回傳，不打 AI
	if !force && rawHash != "" && len(stream.CommentSongs) > 0 {
		cachedHash, _ := s.streamRepo.GetCommentSongsHash(videoID)
		if cachedHash.Valid && cachedHash.String == rawHash {
			var cached []dto.CommentSong
			if err := json.Unmarshal(stream.CommentSongs, &cached); err == nil && len(cached) > 0 {
				// DB 照合だけ現在の状態へ再解決する（AI は打たない）。matched_song_id は
				// 分析時点の DB に依存するため、キャッシュに凍結された古いマッチ／未マッチを補正する。
				s.reresolveMatches(cached)
				logger.Infof("/comments/analyze cache hit for %s (%d songs)", videoID, len(cached))
				return &dto.AnalyzeCommentsResponse{Songs: cached}, nil
			}
		}
	}

	// 1. 取得原始留言（DB 優先，否則 YouTube/Holodex 抓取）
	logger.Infof("starting comment analysis for %s (force=%v, raw len=%d)", videoID, force, len(stream.CommentRaw))
	comments, err := s.getComments(videoID, stream)
	if err != nil {
		return nil, err
	}
	// 遠端から取り直した場合も、分析結果は実際に使ったコメント内容の hash に結び付ける。
	rawHash = hashComments(comments)

	// 2. AI で抽出（統合経路では正規化と重複排除もここで済む）。失敗時は段階的に退避
	parsedSongs, preNormalized, extractWarning := s.parseComments(comments)

	// 3. 過濾 + 去重 + 驗證（先過濾避免非歌曲項目影響去重）
	//
	// 統合経路では AI が既に重複をまとめているが、DeduplicateSongs は通しておく。
	// 開始時刻が離れていれば別の歌唱として残るので取りこぼしは増えず、
	// AI が見落とした重複を拾う安全網として働く。
	filterKW, keepKW, err := s.loadFilterKeywords()
	if err != nil {
		logger.Warnf("failed to load filter keywords, skipping filter: %v", err)
	}
	filteredSongs := comment.FilterSongs(parsedSongs, filterKW, keepKW)
	deduped := comment.DeduplicateSongs(filteredSongs)
	validSongs := comment.ValidateSongs(deduped)

	// 4. 轉成 CommentSong（統合経路なら正規化結果も一緒に運ぶ）
	songs := make([]dto.CommentSong, len(validSongs))
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

	// 5. 正規化と DB 照合
	//
	// 統合経路では正規化が済んでいるので DB 照合だけを行う（AI を再度呼ばない）。
	// 2 段階経路に退避した場合はここで AI 正規化を実行する。
	var aiWarning string
	if preNormalized {
		s.reresolveMatches(songs)
	} else {
		aiWarning = s.normalizeInto(songs)
	}
	// 抽出の失敗は正規化の失敗より重い（曲そのものが取れていない）ので優先して伝える
	if extractWarning != "" {
		aiWarning = extractWarning
	}

	// 6. live chat の拍手で end を推定（start 基準でマッチ。利用不可なら据え置き）
	if s.chatEndService != nil {
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		songs, _ = s.chatEndService.DetectEndsForSongs(videoID, duration, songs)
	}

	// 7. 永続化（comment_songs + 來源 hash）→ 次回からはキャッシュを直接読む
	//
	// AI が失敗した回は保存しない。劣化結果（正規表現のみ・読みやタグ無し）を
	// キャッシュに載せると、既存の良い分析結果を上書きしてしまううえ、
	// 次回以降はキャッシュ命中で AI を呼ばなくなり劣化が固定される。
	// 保存しなければ以前の結果と hash がそのまま残り、復旧を待てる。
	switch {
	case rawHash == "":
		// hash が無い（コメントが空など）ので保存対象外
	case aiWarning != "":
		logger.Warnf("[comment] skipping cache write for %s due to AI degradation: %s", videoID, aiWarning)
	default:
		if songsJSON, mErr := json.Marshal(songs); mErr == nil {
			if err := s.streamRepo.SaveCommentSongs(videoID, songsJSON, rawHash); err != nil {
				logger.Warnf("[comment] save comment_songs failed (%s): %v", videoID, err)
			}
		}
	}

	logger.Infof("comment analysis completed for %s: %d songs", videoID, len(songs))
	return &dto.AnalyzeCommentsResponse{Songs: songs, Warning: aiWarning}, nil
}

// reresolveMatches は AI を呼ばず、正規化済みの名称で DB 照合だけをやり直し、
// matched_song_* を現在の DB 状態に更新する（キャッシュ命中時に呼ぶ）。
// 正規化名が無い古いデータは抽出名にフォールバックする。
func (s *CommentService) reresolveMatches(songs []dto.CommentSong) {
	if s.normalizationService == nil {
		return
	}
	for i := range songs {
		name := songs[i].NormalizedName
		if name == "" {
			name = songs[i].Name
		}
		artist := songs[i].NormalizedArtist
		if artist == "" {
			artist = songs[i].OriginalArtist
		}
		m := s.normalizationService.ResolveMatch(name, artist)
		songs[i].MatchedSongID = m.MatchedSongID
		songs[i].MatchedSongName = m.MatchedSongName
		songs[i].MatchedSongNameReading = m.MatchedSongNameReading
		songs[i].MatchedSongArtist = m.MatchedSongArtist
		songs[i].MatchedSongArtistReading = m.MatchedSongArtistReading
		songs[i].MatchedSongArtURL = m.MatchedSongArtURL
		songs[i].MatchedSongItunesID = m.MatchedSongItunesID
	}
}

// normalizeInto 對 songs 跑 AI 正規化＋DB 照合，把結果填回每首 song（in-place）。
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
	}
	// BatchAINormalization の Warning（AI 呼び出し失敗・応答解析失敗）をそのまま伝える
	return resp.Warning
}

// hashBytes 計算 JSONB 內容的 sha256（空內容回傳空字串）。
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

// BackfillCommentSongs 補填所有有 comment_raw 但沒有 comment_songs 的 stream
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

		stream.CommentSongs = songsJSON
		if err := s.streamRepo.Update(&stream); err != nil {
			logger.Warnf("backfill update error (video: %s): %v", stream.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// HashBackfillResult は comment_songs_hash 補正の結果内訳。
type HashBackfillResult struct {
	Total     int `json:"total"`      // comment_songs を持つ歌回数
	Migrated  int `json:"migrated"`   // 旧アルゴリズム hash → 正規化 hash へ書き換えた数
	AlreadyOK int `json:"already_ok"` // 既に正規化 hash（快取が既に効く）
	Skipped   int `json:"skipped"`    // comment_raw が空 / hash 未設定 / 未知形式で触らなかった数
}

// BackfillCommentSongsHashes は comment_songs_hash を現行の正規化アルゴリズムへ移行する。
//
// 背景: 以前は生の JSONB bytes の sha256 を保存していたが、現在の快取判定は
// 「unmarshal → json.Marshal → sha256」の正規化 hash を使う。旧形式で保存された
// 歌回は hash が永遠に一致せず、force=false でも毎回 AI 再分析されていた。
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
		// comment_raw が空 / 壊れている、または hash 未設定なら快取対象外 → 触らない
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

// loadFilterKeywords 從 DB 載入 filter/keep keywords
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

// parseComments 於 edit-time 解析留言。3 段階で退避する：
//
//	統合経路（抽出＋正規化＋重複排除を 1 回で）
//	  → 2 段階（抽出のみ。正規化は呼び出し側）
//	    → 純正則
//
// 返り値：
//   - 抽出結果
//   - preNormalized：正規化まで済んでいるか。true なら呼び出し側は AI 正規化を省き
//     DB 照合だけを行う
//   - warning：AI が失敗して結果が劣化していることを示す文言（成功時は空）
//
// warning を呼び出し側へ伝えないと、AI 障害が「この配信には曲が無い」という見た目で
// 通ってしまい、しかもその空の結果がキャッシュに保存されて既存の分析を上書きする。
func (s *CommentService) parseComments(comments []string) (songs []comment.ParsedSong, preNormalized bool, warning string) {
	if s.aiClient != nil {
		// 第1候補：統合経路（抽出＋正規化＋重複排除を 1 回で）
		songs, err := comment.ParseNormalizeAndDedupWithAI(s.aiClient, comments)
		if err == nil {
			logger.Infof("Using grouped AI extraction (%d songs)", len(songs))
			return songs, true, ""
		}
		if errors.Is(err, comment.ErrNoTimestampLines) {
			// 解析対象が無いだけで、AI の障害ではない。劣化として扱うと
			// 毎回警告が出るうえキャッシュも書かれず、無駄に再解析し続ける。
			logger.Infof("No timestamp lines in comments; nothing to extract")
			return nil, false, ""
		}
		logger.Warnf("grouped AI extraction failed, falling back to 2-stage: %v", err)

		// 第2候補：従来の 2 段階（抽出のみ。正規化は呼び出し側が行う）
		songs, err = comment.ParseCommentsWithAI(s.aiClient, comments)
		if err == nil {
			logger.Infof("Using AI-extracted songs for analysis (%d songs)", len(songs))
			return songs, false, ""
		}
		logger.Warnf("AI comment parse failed, falling back to regex: %v", err)
		return comment.ParseComments(comments), false,
			fmt.Sprintf("AI抽出に失敗しました（%v）。正規表現のみで解析しました。", err)
	}
	logger.Infof("Using regex-only comment parse (no AI client configured)")
	return comment.ParseComments(comments), false, ""
}

// getComments 從 DB 讀取非空的原始留言，若無則從 YouTube/Holodex 抓取並保存。
func (s *CommentService) getComments(videoID string, stream *models.Stream) ([]string, error) {
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
	if raw, marshalErr := json.Marshal(comments); marshalErr == nil {
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
