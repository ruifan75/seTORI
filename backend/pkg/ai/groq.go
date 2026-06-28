package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
