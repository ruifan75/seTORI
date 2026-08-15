package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

// rate limit／サーバーエラー後にそのプロバイダーを一時的にスキップするクールダウン時間。
const aiCooldownDuration = 60 * time.Second

// AIService は複数の OpenAI 互換プロバイダーを「厳密な優先順 + failover」で使う。
// priority が最小（先頭）のプロバイダーを常に優先し、失敗またはクールダウン中の場合だけ次へ進む。
// 429 / 5xx を返したプロバイダーは短時間スキップし、usage limit へ繰り返し到達するのを防ぐ。
// DB にプロバイダーがなければ環境変数の Groq key に戻る（後方互換、設定なしでも利用可能）。
type AIService struct {
	repo        *repository.AIProviderRepository
	fallbackMu  sync.RWMutex // fallbackKey は管理画面から実行中に差し替えられる
	fallbackKey string

	mu        sync.Mutex
	cooldowns map[int]time.Time // providerID -> クールダウン終了時刻
}

// AIService が ai.Chatter を実装していることを保証する。
var _ ai.Chatter = (*AIService)(nil)

// SetFallbackKey は provider 未設定時に使う Groq キーを差し替える。
func (s *AIService) SetFallbackKey(key string) {
	s.fallbackMu.Lock()
	s.fallbackKey = key
	s.fallbackMu.Unlock()
}

func (s *AIService) fallback() string {
	s.fallbackMu.RLock()
	defer s.fallbackMu.RUnlock()
	return s.fallbackKey
}

func NewAIService(repo *repository.AIProviderRepository, fallbackGroqKey string) *AIService {
	return &AIService{
		repo:        repo,
		fallbackKey: fallbackGroqKey,
		cooldowns:   make(map[int]time.Time),
	}
}

// SimpleChat は ai.Chatter を実装し、有効なプロバイダーを順番に試してエラーなら次へ進む。
func (s *AIService) SimpleChat(systemPrompt, userMessage string) (string, error) {
	providers, err := s.repo.FindEnabled()
	if err != nil {
		return "", fmt.Errorf("load ai providers: %w", err)
	}

	// プロバイダーが一つもなければ環境変数の Groq key に戻る
	if len(providers) == 0 {
		if s.fallback() == "" {
			return "", errors.New("no AI provider configured")
		}
		// フォールバックには長めの既定 timeout（60 秒）を使う
		return ai.NewClientWithTimeout("", "", s.fallback(), 60*time.Second).SimpleChat(systemPrompt, userMessage)
	}

	// priority 順に試す（providers は repo で priority ASC に並び替え済み）：
	// 常に先頭から試し、失敗またはクールダウン中の場合だけ次へ進む。
	var lastErr error
	attempted := 0

	// 1 周目：クールダウン中のプロバイダーをスキップする
	for _, p := range providers {
		if s.inCooldown(p.ID) {
			continue
		}
		attempted++
		resp, err := s.tryProvider(p, systemPrompt, userMessage)
		if err == nil {
			logger.Infof("AI provider succeeded: %q (model=%s)", p.Name, p.Model)
			return resp, nil
		}
		lastErr = err
	}

	// 2 周目：すべてクールダウン中でも優先順に一度試す（即座に失敗するよりよい）
	if attempted == 0 {
		for _, p := range providers {
			resp, err := s.tryProvider(p, systemPrompt, userMessage)
			if err == nil {
				logger.Infof("AI provider succeeded (second round): %q", p.Name)
				return resp, nil
			}
			lastErr = err
		}
	}

	// すべてのプロバイダーが失敗したら、最後に環境変数のフォールバックを試す
	if s.fallback() != "" {
		if resp, err := ai.NewClientWithTimeout("", "", s.fallback(), 60*time.Second).SimpleChat(systemPrompt, userMessage); err == nil {
			logger.Infof("AI fallback (env key) succeeded")
			return resp, nil
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no AI provider available")
	}
	return "", lastErr
}

// tryProvider は一つのプロバイダーを呼び出し、429/5xx ならクールダウンを設定する。
func (s *AIService) tryProvider(p models.AIProvider, systemPrompt, userMessage string) (string, error) {
	timeout := 60 * time.Second
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	client := ai.NewClientWithTimeout(p.BaseURL, p.Model, p.APIKey, timeout)
	resp, err := client.SimpleChat(systemPrompt, userMessage)
	if err == nil {
		return resp, nil
	}

	if shouldCooldown(err) {
		s.setCooldown(p.ID)
		logger.Warnf("AI provider %q rate-limited/unavailable, cooling down %s: %v", p.Name, aiCooldownDuration, err)
	} else {
		logger.Warnf("AI provider %q failed: %v", p.Name, err)
	}
	return "", fmt.Errorf("provider %q: %w", p.Name, err)
}

// shouldCooldown は一時的にプロバイダーを飛ばすべき rate limit／サーバーエラーか判定する。
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
