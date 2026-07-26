package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	// 2. AI hybrid 抽取（AI 判斷歌曲行 + 逐字驗證；失敗/未設定時自動退回純正則）
	parsedSongs := s.parseComments(comments)

	// 3. 過濾 + 去重 + 驗證（先過濾避免非歌曲項目影響去重）
	filterKW, keepKW, err := s.loadFilterKeywords()
	if err != nil {
		logger.Warnf("failed to load filter keywords, skipping filter: %v", err)
	}
	filteredSongs := comment.FilterSongs(parsedSongs, filterKW, keepKW)
	deduped := comment.DeduplicateSongs(filteredSongs)
	validSongs := comment.ValidateSongs(deduped)

	// 4. 轉成 CommentSong（逐字抽取結果）
	songs := make([]dto.CommentSong, len(validSongs))
	for i, song := range validSongs {
		songs[i] = dto.CommentSong{
			Start:              song.Start,
			End:                song.End,
			Name:               song.Name,
			OriginalArtist:     song.OriginalArtist,
			OriginalComment:    song.OriginalComment,
			IsEndTimeEstimated: song.IsEndTimeEstimated,
		}
	}

	// 5. 折り込んだ正規化（AI 正規化＋DB 照合し、結果を各曲に埋め込む）
	aiWarning := s.normalizeInto(songs)

	// 6. live chat の拍手で end を推定（start 基準でマッチ。利用不可なら据え置き）
	if s.chatEndService != nil {
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		songs, _ = s.chatEndService.DetectEndsForSongs(videoID, duration, songs)
	}

	// 7. 永続化（comment_songs + 來源 hash）→ 次回からはキャッシュを直接読む
	if rawHash != "" {
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
	Total    int `json:"total"`    // comment_songs を持つ歌回数
	Migrated int `json:"migrated"` // 旧アルゴリズム hash → 正規化 hash へ書き換えた数
	AlreadyOK int `json:"already_ok"` // 既に正規化 hash（快取が既に効く）
	Skipped  int `json:"skipped"`  // comment_raw が空 / hash 未設定 / 未知形式で触らなかった数
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

// parseComments 於 edit-time 解析留言：優先用 AI hybrid（AI 選取歌曲行 + 正則抽取文字），
// AI 成功時（含 0 曲）採用 AI 結果；僅在 AI 失敗或未設定 client 時退回純正則。
func (s *CommentService) parseComments(comments []string) []comment.ParsedSong {
	if s.aiClient != nil {
		songs, err := comment.ParseCommentsWithAI(s.aiClient, comments)
		if err != nil {
			logger.Warnf("AI comment parse failed, falling back to regex: %v", err)
		} else {
			logger.Infof("Using AI-extracted songs for analysis (%d songs)", len(songs))
			return songs
		}
	}
	logger.Infof("Using regex-only comment parse (no AI client or AI failed)")
	return comment.ParseComments(comments)
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
