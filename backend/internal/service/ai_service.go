package service

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

// rate limit / 伺服器錯誤後，暫時跳過該 provider 的冷卻時間
const aiCooldownDuration = 60 * time.Second

// AIService 以多個 OpenAI 相容 provider 進行「嚴格優先序 + failover」。
// 永遠優先用 priority 最小（最前）的 provider，失敗 / 被冷卻時才換下一個。
// 遇到 429 / 5xx 的 provider 會被短暫冷卻跳過，避免反覆戳到 usage limit。
// 未設定任何 DB provider 時，退回環境變數的 Groq key（向後相容、零設定可用）。
type AIService struct {
	repo        *repository.AIProviderRepository
	fallbackKey string

	mu        sync.Mutex
	cooldowns map[int]time.Time // providerID -> 冷卻到期時間
}

// 確保 AIService 實作 ai.Chatter
var _ ai.Chatter = (*AIService)(nil)

func NewAIService(repo *repository.AIProviderRepository, fallbackGroqKey string) *AIService {
	return &AIService{
		repo:        repo,
		fallbackKey: fallbackGroqKey,
		cooldowns:   make(map[int]time.Time),
	}
}

// SimpleChat 實作 ai.Chatter：依序嘗試啟用的 provider，遇錯就換下一個。
func (s *AIService) SimpleChat(systemPrompt, userMessage string) (string, error) {
	providers, err := s.repo.FindEnabled()
	if err != nil {
		return "", fmt.Errorf("load ai providers: %w", err)
	}

	// 沒有設定任何 provider → 退回環境變數的 Groq key
	if len(providers) == 0 {
		if s.fallbackKey == "" {
			return "", errors.New("no AI provider configured")
		}
		return ai.NewClient(s.fallbackKey).SimpleChat(systemPrompt, userMessage)
	}

	// 依 priority 順序嘗試（providers 已由 repo 依 priority ASC 排序）：
	// 永遠先試最前面的，失敗 / 冷卻才換下一個。
	var lastErr error
	attempted := 0

	// 第一輪：跳過冷卻中的 provider
	for _, p := range providers {
		if s.inCooldown(p.ID) {
			continue
		}
		attempted++
		resp, err := s.tryProvider(p, systemPrompt, userMessage)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	// 第二輪：若全部都在冷卻中，仍依優先序嘗試一次（總比直接失敗好）
	if attempted == 0 {
		for _, p := range providers {
			resp, err := s.tryProvider(p, systemPrompt, userMessage)
			if err == nil {
				return resp, nil
			}
			lastErr = err
		}
	}

	// 全部 provider 失敗 → 最後再試環境變數 fallback
	if s.fallbackKey != "" {
		if resp, err := ai.NewClient(s.fallbackKey).SimpleChat(systemPrompt, userMessage); err == nil {
			return resp, nil
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no AI provider available")
	}
	return "", lastErr
}

// tryProvider 呼叫單一 provider，並在 429/5xx 時設定冷卻
func (s *AIService) tryProvider(p models.AIProvider, systemPrompt, userMessage string) (string, error) {
	client := ai.NewClientWith(p.BaseURL, p.Model, p.APIKey)
	resp, err := client.SimpleChat(systemPrompt, userMessage)
	if err == nil {
		return resp, nil
	}

	if shouldCooldown(err) {
		s.setCooldown(p.ID)
		log.Printf("[WARN] AI provider %q rate-limited/unavailable, cooling down %s: %v", p.Name, aiCooldownDuration, err)
	} else {
		log.Printf("[WARN] AI provider %q failed: %v", p.Name, err)
	}
	return "", fmt.Errorf("provider %q: %w", p.Name, err)
}

// shouldCooldown 判斷錯誤是否為 rate limit / 伺服器錯誤（值得暫時跳過該 provider）
func shouldCooldown(err error) bool {
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	}
	return false
}

func (s *AIService) inCooldown(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooldowns[id]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(s.cooldowns, id)
		return false
	}
	return true
}

func (s *AIService) setCooldown(id int) {
	s.mu.Lock()
	s.cooldowns[id] = time.Now().Add(aiCooldownDuration)
	s.mu.Unlock()
}
