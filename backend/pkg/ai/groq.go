package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/logger"
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

// NewClient デフォルトの Groq クライアントを作成（後方互換）。デフォルト timeout は 60 秒。
func NewClient(apiKey string) *Client {
	return NewClientWith(groqBaseURL, groqModel, apiKey)
}

// NewClientWith 指定 base URL とモデルで OpenAI 互換クライアントを作成（デフォルト timeout 60秒）
func NewClientWith(baseURL, model, apiKey string) *Client {
	return NewClientWithTimeout(baseURL, model, apiKey, 60*time.Second)
}

// NewClientWithTimeout 指定 base URL / モデル / timeout でクライアントを作成。
// timeout は http.Client の全体リクエストタイムアウト（生成が長い provider 向けに大きくできる）。
func NewClientWithTimeout(baseURL, model, apiKey string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = groqBaseURL
	}
	if model == "" {
		model = groqModel
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
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
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`

	// 既定値しか受け付けないモデルがあるため、省略できるようポインタにしている。
	Temperature *float64 `json:"temperature,omitempty"`

	// 出力上限のパラメータ名はプロバイダー・モデルによって異なる。
	// 従来の OpenAI 互換 API は max_tokens だが、GPT-5 系など新しめのモデルは
	// max_completion_tokens しか受け付けず、max_tokens を送ると 400 を返す。
	// どちらか一方だけを送る（omitempty で他方は落ちる）。
	MaxTokens           int `json:"max_tokens,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
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

const (
	// maxTokensFloor は短い入力でも確保する最低出力枠。
	maxTokensFloor = 1024
	// maxTokensCeil は 1 回の応答に許す上限。
	maxTokensCeil = 8192
	// maxTokensFactor は user メッセージ長から出力枠を見積もる倍率。
	//
	// 抽出では入力 1 行（約 40 字）につき JSON 1 要素（約 110 字）を返すので比は約 2.75、
	// 正規化は約 1.5。実測分布（中央値 / 平均 / p90 / 最大）で検証し、4 倍あれば
	// 最も厳しい平均ケースでも 7%、最大ケースでは 35% の余裕が残ることを確認している。
	maxTokensFactor = 4
	// charsPerToken は日英混在テキストの概算（実測較正値）。
	charsPerToken = 1.87
)

// estimateMaxTokens は user メッセージの長さから出力枠を見積もる。
//
// 以前は固定で 8192 を要求していた。課金は実出力ぶんなので金額には影響しないが、
// 多くのプロバイダーはレート制限を「予約分込み」で計算するため、実際には中央値
// 1,144 トークンしか使わないのに毎回 8,192 を消費し、無料枠が実使用の数倍の
// 速さで枯れていた（1 日に処理できる件数が数分の 1 になる）。
//
// 見積もりが不足すると応答が途中で切れて JSON パースに失敗するため倍率は安全側に倒し、
// それでも切り詰められた場合は Chat 側で警告を出して観測できるようにしている。
func estimateMaxTokens(messages []ChatMessage) int {
	userChars := 0
	for _, m := range messages {
		if m.Role == "user" {
			userChars += len([]rune(m.Content))
		}
	}
	est := int(float64(userChars) / charsPerToken * maxTokensFactor)
	if est < maxTokensFloor {
		return maxTokensFloor
	}
	if est > maxTokensCeil {
		return maxTokensCeil
	}
	return est
}

// modelQuirks はモデルごとの「受け付けないパラメータ」を覚える。
//
// OpenAI 互換を名乗っていても細部は揃っておらず、対応は増減する。
// 毎回コードを直すのではなく、400 の内容から学習して以降は最初から避ける。
type modelQuirks struct {
	// 出力上限を max_tokens ではなく max_completion_tokens で送る（GPT-5 系など）
	useCompletionTokens bool
	// temperature を送らない（既定値しか受け付けないモデル）
	omitTemperature bool
	// 出力枠の倍率。推論モデルは max_completion_tokens に内部推論ぶんも含むため、
	// 「出力の長さ」から見積もった枠では足りない。実際に gpt-5.6-luna では
	// 枠 1024 を推論だけで使い切り、本文が空のまま切れた。
	// finish_reason=length を見たら倍にして学習する（0 は 1 と同じ扱い）。
	outputMultiplier int
}

func (q modelQuirks) multiplier() int {
	if q.outputMultiplier < 1 {
		return 1
	}
	return q.outputMultiplier
}

// quirkCache は baseURL|model をキーに modelQuirks を保持する。
// Client は呼び出しごとに作り直されるため、インスタンスではなくパッケージ側に持つ。
var quirkCache sync.Map

func loadQuirks(baseURL, model string) modelQuirks {
	if v, ok := quirkCache.Load(baseURL + "|" + model); ok {
		return v.(modelQuirks)
	}
	return modelQuirks{}
}

// learnQuirks は 400 の内容から回避策を学ぶ。変化があれば true を返す。
func learnQuirks(err error, q *modelQuirks) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	changed := false
	if !q.useCompletionTokens && strings.Contains(apiErr.Body, "max_completion_tokens") {
		q.useCompletionTokens = true
		changed = true
	}
	// 例: "'temperature' does not support 0.1 with this model. Only the default (1) value is supported."
	if !q.omitTemperature && strings.Contains(apiErr.Body, "'temperature'") {
		q.omitTemperature = true
		changed = true
	}
	return changed
}

// maxQuirkRetries は回避策の学習に許す再送回数。
// 400 は 1 度に 1 つのパラメータしか指摘してこないため、非対応が複数あるときは
// その数だけ往復が要る。現状 2 種類なので 2 で足りるが、余裕を持たせている。
const maxQuirkRetries = 5

// maxOutputMultiplier は截断を受けて枠を広げる上限（1024 の floor から最大 16 倍）。
// 推論モデルは max_completion_tokens に内部推論ぶんも含むため、出力の長さから
// 見積もった枠では足りないことがある。
const maxOutputMultiplier = 16

// Chat チャットリクエストを送信
//
// パラメータの対応がモデルによって違うため、400 で拒否されたら内容から回避策を学んで
// 再送する。プロバイダーは 1 度に 1 つしか指摘しないので、学べる限り繰り返す。
// 学習結果は記憶するので、この往復が発生するのはプロセス起動後の最初の 1 回だけ。
func (c *Client) Chat(messages []ChatMessage) (*ChatResponse, error) {
	key := c.baseURL + "|" + c.model
	q := loadQuirks(c.baseURL, c.model)

	resp, err := c.chatOnce(messages, q)
	for i := 0; i < maxQuirkRetries; i++ {
		switch {
		case err != nil:
			if !learnQuirks(err, &q) {
				return resp, err // 学べるものが無い＝この 400 は回避策の対象外
			}
			logger.Infof("AI model quirk learned (model=%s, completion_tokens=%v, omit_temperature=%v), retrying",
				c.model, q.useCompletionTokens, q.omitTemperature)
		case isTruncated(resp) && q.multiplier() < maxOutputMultiplier:
			// 枠が足りず途中で切れた。推論モデルでは内部推論が枠を食うため、
			// 出力の長さから見積もった枠では届かないことがある。
			q.outputMultiplier = q.multiplier() * 2
			logger.Infof("AI response truncated, raising output budget x%d (model=%s)", q.outputMultiplier, c.model)
		default:
			return resp, err
		}
		quirkCache.Store(key, q)
		resp, err = c.chatOnce(messages, q)
	}
	return resp, err
}

// isTruncated は出力枠を使い切って途中で切れた応答かどうか。
func isTruncated(resp *ChatResponse) bool {
	return resp != nil && len(resp.Choices) > 0 && resp.Choices[0].FinishReason == "length"
}

func (c *Client) chatOnce(messages []ChatMessage, q modelQuirks) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:    c.model,
		Messages: messages,
	}
	if !q.omitTemperature {
		t := 0.1 // より一貫した結果を得るため低めの温度を使用
		reqBody.Temperature = &t
	}
	budget := estimateMaxTokens(messages) * q.multiplier()
	if q.useCompletionTokens {
		reqBody.MaxCompletionTokens = budget
	} else {
		reqBody.MaxTokens = budget
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

	content := resp.Choices[0].Message.Content
	finishReason := resp.Choices[0].FinishReason
	if finishReason == "length" {
		logger.Warnf("AI response may be truncated (finish_reason=length, model=%s)", c.model)
	}
	if resp.Usage.TotalTokens > 0 {
		logger.Infof("AI usage: prompt=%d, completion=%d, total=%d (model=%s)", resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, c.model)
	}

	return content, nil
}

// CleanJSONResponse は LLM が余計なテキストを付与した場合でも、純粋な JSON 配列/オブジェクトを抽出する
func CleanJSONResponse(resp string) string {
	resp = strings.TrimSpace(resp)

	// よくある悪い接頭辞を除去（大文字小文字・記号のバリエーション対応）
	prefixes := []string{
		"** Output only JSON.",
		"**Output only JSON.",
		"Output only JSON.",
		"以下はJSONです。",
		"以下は",
		"JSON:",
		"```json",
		"```",
		"**",
		"応答:",
	}
	for _, p := range prefixes {
		resp = strings.TrimPrefix(resp, p)
		resp = strings.TrimSpace(resp)
	}

	// 前置き（説明文・コードフェンス）を捨てて、最初の [ か { から始める。
	//
	// [ を優先するのは {"suggestions":[...]} のように配列をオブジェクトで包んだ
	// 応答から中の配列を取り出すため（後段の「最後の ] で切る」と対になっている）。
	//
	// **条件は 0 を含めること。** 以前は「[ が 0 より後にあるか」だったので、
	// 正しく "[{...}]" で始まる応答では [ の位置が 0 で条件を満たさず、
	// 直後の { の位置 1 で切って**先頭の [ を削っていた**。
	// そのあと「{ で始まるなら配列に包む」が働いて "]" が二重になり、
	// json.Unmarshal が「invalid character ']' after top-level value」で落ちる。
	// grouped 経路が無事だったのは Decoder.Decode が末尾の余りを無視するためで、
	// Unmarshal を使う別名義の AI 判定だけが常に失敗していた。
	if idx := strings.Index(resp, "["); idx >= 0 {
		resp = resp[idx:]
	} else if idx := strings.Index(resp, "{"); idx > 0 {
		resp = resp[idx:]
	}

	// LLMがよくやるミス: オブジェクトをカンマで並べただけ、または単一オブジェクトを出力（配列にしていない）
	// 必ず配列にラップする
	trimmed := strings.TrimSpace(resp)
	if strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		resp = "[" + trimmed + "]"
	}

	// 最後の ] または } で終わるように
	if strings.HasPrefix(resp, "[") {
		if end := strings.LastIndex(resp, "]"); end != -1 {
			resp = resp[:end+1]
		}
	} else if strings.HasPrefix(resp, "{") {
		if end := strings.LastIndex(resp, "}"); end != -1 {
			resp = resp[:end+1]
		}
	}

	resp = strings.TrimSpace(resp)
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	// 末尾のカンマを除去（LLMがよくやる）
	resp = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(resp, "$1")

	resp = strings.TrimSpace(resp)

	// もしまだ { で始まっていて [ で始まっていないなら、強制的に配列にラップ
	// これでカンマ区切りのオブジェクト列挙や単一オブジェクトの場合を確実にカバー
	trimmed = strings.TrimSpace(resp)
	if strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		resp = "[" + trimmed + "]"
	}

	return strings.TrimSpace(resp)
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
