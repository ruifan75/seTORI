package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

type NormalizationService struct {
	aiClient *ai.Client
	songRepo *repository.SongRepository
}

func NewNormalizationService(
	groqAPIKey string,
	songRepo *repository.SongRepository,
) *NormalizationService {
	return &NormalizationService{
		aiClient: ai.NewClient(groqAPIKey),
		songRepo: songRepo,
	}
}

// 批量處理用的 system prompt
const batchSystemPrompt = `你是一個專門處理日本歌曲資料正規化的助手。你將收到多首歌曲，請一次處理所有歌曲。

你的任務是：
1. 從原始歌曲名稱中提取正規化的歌名
   - 去除「演出版本標記」（如 Acoustic Ver., Short Ver. 等，這些是表演方式的標籤）
   - 保留「歌曲版本標記」（如 Remix, Cover 等，因為這代表不同的歌曲）
   - 如果歌名同時包含日文和英文/羅馬字（如「ぼくらのレットイットビー / Bokura no Let It Be」），只保留日文原名
   - 如果原曲名本來就是英文（如「First Love」「Lemon」「KICK BACK」），保留英文原名，不要轉成カタカナ
   - 移除多餘的空格和符號
2. 為歌名和藝人名提供平假名讀音（ふりがな）
3. 識別演出版本標籤

【重要】tags 欄位只能使用以下 7 種標籤 ID（不能使用其他值）：
- acoustic（原曲名含有 Acoustic, アコースティック 等）
- piano（原曲名含有 Piano, ピアノ 等）
- 弾き語り（原曲名含有 弾き語り）
- acappella（原曲名含有 A Cappella, アカペラ 等）
- short（原曲名含有 Short, ショート 等）
- full（原曲名含有 Full, フル 等）
- medley（原曲名含有 Medley, メドレー 等）

注意：Remix、Cover、Live 版本等是不同的歌曲，應該保留在歌名中，不要移除。

請以 JSON 陣列格式回應，每個元素包含：
- index: 對應輸入的歌曲編號 (number, 從 0 開始)
- normalized_name: 正規化後的歌名 (string)
- normalized_name_reading: 歌名的平假名讀音 (string)
- original_artist: 原唱藝人 (string)
- original_artist_reading: 藝人名的平假名讀音 (string)
- tags: 偵測到的版本標籤，只能是上述 7 種之一 (array of strings)
- confidence: 信心度 0.0-1.0 (number)

只回應 JSON 陣列，不要有其他文字。格式範例：
[{"index":0,"normalized_name":"...","normalized_name_reading":"...","original_artist":"...","original_artist_reading":"...","tags":[],"confidence":0.9}]`

// BatchAISuggestion 批量 AI 回應的單項格式
type BatchAISuggestion struct {
	Index                 int      `json:"index"`
	NormalizedName        string   `json:"normalized_name"`
	NormalizedNameReading string   `json:"normalized_name_reading"`
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading"`
	Tags                  []string `json:"tags"`
	Confidence            float64  `json:"confidence"`
}

// BatchAINormalization 批量 AI 正規化（一次請求處理所有歌曲）
func (s *NormalizationService) BatchAINormalization(items []dto.AINormalizationItem) (*dto.BatchAINormalizationResponse, error) {
	if len(items) == 0 {
		return &dto.BatchAINormalizationResponse{Suggestions: []dto.AISuggestionResult{}}, nil
	}

	// 構建包含所有歌曲的用戶訊息
	userMessage := s.buildBatchMessage(items)

	// 一次呼叫 AI 處理所有歌曲
	response, err := s.aiClient.SimpleChat(batchSystemPrompt, userMessage)
	if err != nil {
		return nil, fmt.Errorf("AI batch chat: %w", err)
	}

	// 解析批量回應
	batchSuggestions, err := s.parseBatchAIResponse(response)
	if err != nil {
		// 解析失敗時，返回原始資料
		suggestions := make([]dto.AISuggestionResult, len(items))
		for i, item := range items {
			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        item.Name,
				NormalizedNameReading: "",
				OriginalArtist:        item.OriginalArtist,
				OriginalArtistReading: "",
				Tags:                  []string{},
				Confidence:            0,
				Reasoning:             fmt.Sprintf("AI解析失敗: %v", err),
			}
		}
		return &dto.BatchAINormalizationResponse{Suggestions: suggestions}, nil
	}

	// 將 AI 回應轉換為結果，並填補缺失的項目
	suggestionMap := make(map[int]BatchAISuggestion)
	for _, s := range batchSuggestions {
		suggestionMap[s.Index] = s
	}

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

			// 嘗試配對現有歌曲
			matchedSong, err := s.songRepo.FindByNameAndArtist(aiSugg.NormalizedName, aiSugg.OriginalArtist)
			if err == nil && matchedSong != nil {
				songID := matchedSong.ID.String()
				suggestions[i].MatchedSongID = &songID
			}
		} else {
			// AI 沒有返回此項目，使用原始資料
			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        item.Name,
				NormalizedNameReading: "",
				OriginalArtist:        item.OriginalArtist,
				OriginalArtistReading: "",
				Tags:                  []string{},
				Confidence:            0,
				Reasoning:             "AI未返回此項目",
			}

			// 也嘗試用原始名稱配對
			matchedSong, err := s.songRepo.FindByNameAndArtist(item.Name, item.OriginalArtist)
			if err == nil && matchedSong != nil {
				songID := matchedSong.ID.String()
				suggestions[i].MatchedSongID = &songID
			}
		}
	}

	return &dto.BatchAINormalizationResponse{Suggestions: suggestions}, nil
}

// buildBatchMessage 構建包含所有歌曲的批量訊息
func (s *NormalizationService) buildBatchMessage(items []dto.AINormalizationItem) string {
	var sb strings.Builder

	sb.WriteString("請處理以下歌曲清單：\n\n")

	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] 歌名: %s", i, item.Name))
		if item.OriginalArtist != "" {
			sb.WriteString(fmt.Sprintf(" / 藝人: %s", item.OriginalArtist))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// parseBatchAIResponse 解析批量 AI 回應
func (s *NormalizationService) parseBatchAIResponse(response string) ([]BatchAISuggestion, error) {
	response = strings.TrimSpace(response)

	// 移除可能的 markdown 代碼塊標記
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var suggestions []BatchAISuggestion
	if err := json.Unmarshal([]byte(response), &suggestions); err != nil {
		return nil, fmt.Errorf("unmarshal batch AI response: %w, response: %s", err, response)
	}

	return suggestions, nil
}
