package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	// デフォルト Groq OpenAI 互換 base とモデル
	groqBaseURL = "https://api.groq.com/openai/v1"
	groqModel   = "llama-3.3-70b-versatile"
)

// Chatter は LLM 対話インターフェース。単一 Client または複数 provider のローテーションで実装可能
type Chatter interface {
	SimpleChat(systemPrompt, userMessage string) (string, error)
}

// APIError LLM API から返される非 2xx エラー。failover 判断用のステータスコードを含む
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error: status=%d body=%s", e.StatusCode, e.Body)
}

// Client OpenAI 互換 LLM クライアント（Groq / Gemini / Cerebras など）
type Client struct {
	apiKey     string
	httpClient *http.Client
	model      string
	baseURL    string // OpenAI 互換 base、例: https://api.groq.com/openai/v1
}

// NewClient デフォルトの Groq クライアントを作成（後方互換）
func NewClient(apiKey string) *Client {
	return NewClientWith(groqBaseURL, groqModel, apiKey)
}

// NewClientWith 指定 base URL とモデルで OpenAI 互換クライアントを作成
func NewClientWith(baseURL, model, apiKey string) *Client {
	if baseURL == "" {
		baseURL = groqBaseURL
	}
	if model == "" {
		model = groqModel
	}
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model:   model,
		baseURL: baseURL,
	}
}

// ChatMessage チャットメッセージ
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest チャットリクエスト
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatResponse チャットレスポンス
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat チャットリクエストを送信
func (c *Client) Chat(messages []ChatMessage) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0.1, // より一貫した結果を得るため低めの温度を使用
		MaxTokens:   2048,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// SimpleChat 簡略化されたチャットメソッド
func (c *Client) SimpleChat(systemPrompt, userMessage string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	resp, err := c.Chat(messages)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices")
	}

	return resp.Choices[0].Message.Content, nil
}

// ModelInfo モデル選択 UI 用のメタデータ。chat（テキスト生成）に使えるモデルのみ返す
type ModelInfo struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	Description   string `json:"description,omitempty"`
}

// ListModels Client の base/key で利用可能なモデルを取得（メソッド版）
func (c *Client) ListModels() ([]ModelInfo, error) {
	return ListModels(c.baseURL, c.apiKey)
}

// ListModels 指定 base/key で chat 利用可能なモデル一覧を取得。
// Gemini は専用 API（supportedGenerationMethods で生成可能なものに絞る）、
// それ以外は OpenAI 互換 GET {base}/models を使い、メタデータがあれば text→text のみに絞る。
func ListModels(baseURL, apiKey string) ([]ModelInfo, error) {
	if strings.Contains(baseURL, "generativelanguage.googleapis.com") {
		return listGeminiModels(apiKey)
	}
	return listOpenAIModels(baseURL, apiKey)
}

// openaiModelsResponse OpenAI 互換 GET /models のレスポンス（Groq は active/modalities など拡張フィールドを含む）
type openaiModelsResponse struct {
	Data []struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`         // Groq の表示名
		DisplayName      string   `json:"display_name"` // 一部プロバイダーの表示名
		Active           *bool    `json:"active"`       // Groq: 廃止判定
		ContextWindow    int      `json:"context_window"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"data"`
}

func listOpenAIModels(baseURL, apiKey string) ([]ModelInfo, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	body, err := httpGetJSON(endpoint, apiKey)
	if err != nil {
		return nil, err
	}
	var mr openaiModelsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	out := make([]ModelInfo, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID == "" {
			continue
		}
		if m.Active != nil && !*m.Active {
			continue // 廃止モデルを除外（Groq）
		}
		// modality 情報があれば text→text のみ（whisper=音声、TTS=音声出力 などを除外）
		if len(m.InputModalities) > 0 && !contains(m.InputModalities, "text") {
			continue
		}
		if len(m.OutputModalities) > 0 && !contains(m.OutputModalities, "text") {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.DisplayName
		}
		out = append(out, ModelInfo{
			ID:            strings.TrimPrefix(m.ID, "models/"),
			DisplayName:   name,
			ContextWindow: m.ContextWindow,
		})
	}
	sortModels(out)
	return out, nil
}

// geminiModelsResponse Gemini 専用 GET /v1beta/models のレスポンス
type geminiModelsResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		Description                string   `json:"description"`
		InputTokenLimit            int      `json:"inputTokenLimit"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
	NextPageToken string `json:"nextPageToken"`
}

func listGeminiModels(apiKey string) ([]ModelInfo, error) {
	var out []ModelInfo
	pageToken := ""
	for {
		u := "https://generativelanguage.googleapis.com/v1beta/models?pageSize=200&key=" + url.QueryEscape(apiKey)
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		body, err := httpGetJSON(u, "") // key は query string なので Bearer 不要
		if err != nil {
			return nil, err
		}
		var gr geminiModelsResponse
		if err := json.Unmarshal(body, &gr); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		for _, m := range gr.Models {
			id := strings.TrimPrefix(m.Name, "models/")
			// generateContent 非対応（embedding / aqa など）を除外
			if !contains(m.SupportedGenerationMethods, "generateContent") {
				continue
			}
			// Gemini は画像・音声・動画・音楽生成も generateContent を使うため、
			// id のキーワードで text 出力でないモデルを除外する
			if !isGeminiTextModel(id) {
				continue
			}
			out = append(out, ModelInfo{
				ID:            id,
				DisplayName:   m.DisplayName,
				ContextWindow: m.InputTokenLimit,
				Description:   m.Description,
			})
		}
		if gr.NextPageToken == "" {
			break
		}
		pageToken = gr.NextPageToken
	}
	sortModels(out)
	return out, nil
}

// httpGetJSON GET して 2xx の body を返す。bearer が非空なら Authorization ヘッダを付与
func httpGetJSON(endpoint, bearer string) ([]byte, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

// geminiNonTextOutput は id に含まれると text 出力でない（画像/音声/動画/音楽）と判断するキーワード
var geminiNonTextOutput = []string{"image", "tts", "audio", "nano-banana", "imagen", "veo", "lyria"}

func isGeminiTextModel(id string) bool {
	for _, kw := range geminiNonTextOutput {
		if strings.Contains(id, kw) {
			return false
		}
	}
	return true
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

func sortModels(ms []ModelInfo) {
	sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
}
