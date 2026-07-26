package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

type NormalizationService struct {
	aiClient       ai.Chatter
	songRepo       *repository.SongRepository
	songItunesRepo *repository.SongItunesRepository
}

func NewNormalizationService(
	aiClient ai.Chatter,
	songRepo *repository.SongRepository,
	songItunesRepo *repository.SongItunesRepository,
) *NormalizationService {
	return &NormalizationService{
		aiClient:       aiClient,
		songRepo:       songRepo,
		songItunesRepo: songItunesRepo,
	}
}

// バッチ処理用の system prompt (日本語版)
const batchSystemPrompt = `**最重要: 応答は純粋なJSON配列「のみ」を出力せよ。前後に ** や "Output only JSON." などの一切のテキストを付けるな。必ず [ で始まり ] で終わること。**

あなたは日本語楽曲データの正規化を専門とするアシスタントです。複数の楽曲を受け取り、一度にすべて処理してください。

タスク：
1. 元の楽曲名から正規化された歌名を抽出
   - 「演奏バージョンの表記」（Acoustic Ver., Short Ver. など、演奏方法を示すタグ）を除去
   - 「楽曲バージョンの表記」（Remix, Cover など）は異なる楽曲を表すため保持
   - 歌名に日本語と英語/ローマ字の両方が含まれる場合（例：「ぼくらのレットイットビー / Bokura no Let It Be」）、日本語の原名のみ保持
   - 元の曲名が英語の場合（「First Love」「Lemon」「KICK BACK」）は英語のまま保持し、カタカナに変換しない
   - 余分なスペースや記号を除去
2. 歌名とアーティスト名に平仮名ふりがなを提供
3. 演奏バージョンタグを識別

【重要】tags フィールドは以下の7種のタグIDのみ使用可能：
- acoustic（原曲名に Acoustic, アコースティック などを含む）
- piano（原曲名に Piano, ピアノ などを含む）
- 弾き語り（原曲名に 弾き語り を含む）
- acappella（原曲名に A Cappella, アカペラ などを含む）
- short（原曲名に Short, ショート などを含む）
- full（原曲名に Full, フル などを含む）
- medley（原曲名に Medley, メドレー などを含む）

注意：Remix、Cover、Live バージョンなどは異なる楽曲なので、歌名に保持してください。除去しないでください。

JSON配列形式で応答してください。各要素：
- index: 入力楽曲の番号 (number, 0から開始)
- normalized_name: 正規化後の歌名 (string)
- normalized_name_reading: 歌名の平仮名読み (string)
- original_artist: 原曲アーティスト (string)
- original_artist_reading: アーティスト名の平仮名読み (string)
- tags: 検出されたバージョンタグ（上記7種のみ） (array of strings)
- confidence: 信頼度 0.0-1.0 (number)

JSON配列のみ応答し、他のテキストは含めないでください。例：
[{"index":0,"normalized_name":"...","normalized_name_reading":"...","original_artist":"...","original_artist_reading":"...","tags":[],"confidence":0.9}]

**最重要（最後に繰り返す）**:
- 絶対にJSON配列として出力せよ。 [ {..}, {..} ] の形。
- オブジェクトをカンマで並べただけの出力は厳禁。
- 余計な文字は1文字も書くな。出力は [ で始まり ] で終わる純粋な配列のみ。`

// BatchAISuggestion バッチ AI 応答の単一項目フォーマット
type BatchAISuggestion struct {
	Index                 int      `json:"index"`
	NormalizedName        string   `json:"normalized_name"`
	NormalizedNameReading string   `json:"normalized_name_reading"`
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading"`
	Tags                  []string `json:"tags"`
	Confidence            float64  `json:"confidence"`
}

// BatchAINormalization バッチ AI 正規化（1回の呼び出しで全楽曲を処理）
func (s *NormalizationService) BatchAINormalization(items []dto.AINormalizationItem) (*dto.BatchAINormalizationResponse, error) {
	if len(items) == 0 {
		return &dto.BatchAINormalizationResponse{Suggestions: []dto.AISuggestionResult{}}, nil
	}

	// すべての楽曲を含むユーザーメッセージを構築
	userMessage := s.buildBatchMessage(items)

	logger.Debugf("AI normalization input userMessage: %s", userMessage)

	// AI を1回呼んで全楽曲を処理
	var warning string
	var suggestionMap map[int]BatchAISuggestion

	logger.Infof("AI batch normalization: items=%d, prompt_len=%d", len(items), len(userMessage))

	response, err := s.aiClient.SimpleChat(batchSystemPrompt, userMessage)
	if err != nil {
		// AI 呼び出し失敗。警告を記録し、DB照合のみ続行
		logger.Warnf("AI batch chat failed: %v", err)
		warning = fmt.Sprintf("AI正規化に失敗しました（%v）。DB照合のみ実行しました。", err)
		suggestionMap = make(map[int]BatchAISuggestion)
	} else {
		logger.Infof("AI batch normalization: response_len=%d", len(response))
		logger.Debugf("AI normalization raw response: %s", response)

		// バッチ応答をパース
		batchSuggestions, parseErr := s.parseBatchAIResponse(response)
		if parseErr != nil {
			// レスポンスが長い場合は先頭と末尾を表示して切り詰めを検知しやすくする
			respPreview := response
			if len(respPreview) > 1500 {
				respPreview = respPreview[:800] + " ... [truncated] ... " + respPreview[len(respPreview)-400:]
			}
			logger.Warnf("AI response parse failed: %v, response_preview: %s", parseErr, respPreview)
			warning = "AI応答の解析に失敗しました。DB照合のみ実行しました。"
			suggestionMap = make(map[int]BatchAISuggestion)
		} else {
			suggestionMap = make(map[int]BatchAISuggestion)
			for _, s := range batchSuggestions {
				suggestionMap[s.Index] = s
			}
			logger.Infof("AI batch normalization parse succeeded: %d suggestions", len(batchSuggestions))
			for i, sug := range batchSuggestions {
				logger.Debugf("AI norm sug %d: name=%q artist=%q", i, sug.NormalizedName, sug.OriginalArtist)
			}
		}
	}

	// AI 応答を結果に変換し、欠損項目を補完（AI失敗時は元データを使用）
	suggestions := make([]dto.AISuggestionResult, len(items))
	for i, item := range items {
		if aiSugg, ok := suggestionMap[i]; ok {
			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        aiSugg.NormalizedName,
				NormalizedNameReading: aiSugg.NormalizedNameReading,
				OriginalArtist:        aiSugg.OriginalArtist,
				OriginalArtistReading: aiSugg.OriginalArtistReading,
				Tags:                  aiSugg.Tags,
				Confidence:            aiSugg.Confidence,
				Reasoning:             "",
			}

			// 既存楽曲とのマッチを試行（iTunes ID 優先 → 歌名 + アーティスト）
			s.matchAndPopulateSong(&suggestions[i], &item, aiSugg.NormalizedName, aiSugg.OriginalArtist)
		} else {
			// AI がこの項目を返さなかった、または失敗した場合は元データを使用

			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        item.Name,
				NormalizedNameReading: "",
				OriginalArtist:        item.OriginalArtist,
				OriginalArtistReading: "",
				Tags:                  []string{},
				Confidence:            0,
				Reasoning:             "",
			}

			// それでも DB マッチを試行

			s.matchAndPopulateSong(&suggestions[i], &item, item.Name, item.OriginalArtist)
		}
	}

	return &dto.BatchAINormalizationResponse{Suggestions: suggestions, Warning: warning}, nil
}

// buildBatchMessage 構築包含所有楽曲のバッチメッセージ
func (s *NormalizationService) buildBatchMessage(items []dto.AINormalizationItem) string {
	var sb strings.Builder

	sb.WriteString("以下の楽曲リストを処理してください：\n\n")

	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] 楽曲名: %s", i, item.Name))
		if item.OriginalArtist != "" {
			sb.WriteString(fmt.Sprintf(" / アーティスト: %s", item.OriginalArtist))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// matchAndPopulateSong 嘗試匹配 DB 歌曲並填入資訊
// 優先順序：iTunes ID → 歌名 + アーティスト
func (s *NormalizationService) matchAndPopulateSong(result *dto.AISuggestionResult, item *dto.AINormalizationItem, normalizedName, normalizedArtist string) {
	var matchedSong *models.Song
	var matchReason string

	// 1. 優先使用 iTunes ID 配對
	if item.ItunesID != nil && *item.ItunesID > 0 {
		song, err := s.songRepo.FindByItunesID(*item.ItunesID)
		if err == nil && song != nil {
			matchedSong = song
			matchReason = "itunes_id"
		}
	}

	// 2. 使用歌名 + 藝人配對
	if matchedSong == nil {
		song, err := s.songRepo.FindByNameAndArtist(normalizedName, normalizedArtist)
		if err == nil && song != nil {
			matchedSong = song
			matchReason = "name"
		}
	}

	if matchedSong == nil {
		return
	}

	// 填入匹配結果
	songID := matchedSong.ID.String()
	result.MatchedSongID = &songID
	result.MatchReason = matchReason
	result.MatchedSongName = &matchedSong.Name
	result.MatchedSongArtist = &matchedSong.OriginalArtist
	if matchedSong.NameReading.Valid {
		result.MatchedSongNameReading = &matchedSong.NameReading.String
	}
	if matchedSong.OriginalArtistReading.Valid {
		result.MatchedSongArtistReading = &matchedSong.OriginalArtistReading.String
	}
	if matchedSong.Arts.Valid {
		result.MatchedSongArtURL = &matchedSong.Arts.String
	}

	// 取得 primary iTunes ID
	if s.songItunesRepo != nil {
		itunesRecords, err := s.songItunesRepo.FindBySongID(matchedSong.ID)
		if err == nil && len(itunesRecords) > 0 {
			for _, record := range itunesRecords {
				if record.IsPrimary {
					result.MatchedSongItunesID = &record.ITunesID
					break
				}
			}
			if result.MatchedSongItunesID == nil {
				result.MatchedSongItunesID = &itunesRecords[0].ITunesID
			}
		}
	}
}

// ResolveMatch は AI を呼ばず、正規化済みの名称・アーティストで DB 照合のみを行い、
// マッチ結果（matched_song_*）を埋めた AISuggestionResult を返す。
// キャッシュ命中時に、凍結された古いマッチ（曲が後から追加された等）を現在の DB 状態へ
// 再解決するために使う。マッチ無しなら matched_song_* は空のまま。
func (s *NormalizationService) ResolveMatch(normalizedName, normalizedArtist string) dto.AISuggestionResult {
	var res dto.AISuggestionResult
	item := dto.AINormalizationItem{Name: normalizedName, OriginalArtist: normalizedArtist}
	s.matchAndPopulateSong(&res, &item, normalizedName, normalizedArtist)
	return res
}

// parseBatchAIResponse バッチ AI 応答をパース
func (s *NormalizationService) parseBatchAIResponse(response string) ([]BatchAISuggestion, error) {
	response = ai.CleanJSONResponse(response)

	// Use Decoder to tolerate trailing data after the JSON array
	decoder := json.NewDecoder(strings.NewReader(response))
	var suggestions []BatchAISuggestion
	if err := decoder.Decode(&suggestions); err != nil {
		// 長い場合はプレビュー
		preview := response
		if len(preview) > 1500 {
			preview = preview[:800] + " ... [truncated] ... " + preview[len(preview)-400:]
		}
		errMsg := fmt.Sprintf("unmarshal batch AI response: %v", err)
		trimmed := strings.TrimSpace(response)
		if !strings.HasSuffix(trimmed, "]") && strings.HasPrefix(trimmed, "[") {
			errMsg += " (response looks truncated: does not end with ']')"
		}
		return nil, fmt.Errorf("%s, response_preview: %s", errMsg, preview)
	}

	return suggestions, nil
}
