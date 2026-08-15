package ratelimit

import (
	"sync"
	"time"
)

// RateLimiter は rate limiting 機能を提供する。
type RateLimiter struct {
	maxRequests int           // 最大リクエスト数
	window      time.Duration // 時間ウィンドウ
	requests    []time.Time   // リクエスト時刻記録
	mu          sync.Mutex
}

// NewRateLimiter 新しい rate limiter を作成
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		window:      window,
		requests:    make([]time.Time, 0),
	}
}

// Wait 送信可能になるまで待機
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// 期限切れのリクエスト記録をクリーンアップ
	cutoff := now.Add(-rl.window)
	validRequests := make([]time.Time, 0)
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}
	rl.requests = validRequests

	// 制限に達した場合、最も古いリクエストが期限切れになるまで待機
	if len(rl.requests) >= rl.maxRequests {
		oldestRequest := rl.requests[0]
		waitDuration := oldestRequest.Add(rl.window).Sub(now)
		if waitDuration > 0 {
			time.Sleep(waitDuration + 100*time.Millisecond) // 少し余裕を持たせる
		}
		// 再クリーンアップ
		now = time.Now()
		cutoff = now.Add(-rl.window)
		validRequests = make([]time.Time, 0)
		for _, reqTime := range rl.requests {
			if reqTime.After(cutoff) {
				validRequests = append(validRequests, reqTime)
			}
		}
		rl.requests = validRequests
	}

	// 新しいリクエストを記録
	rl.requests = append(rl.requests, now)
}

// CanRequest 即時送信可能かどうかを確認（待機なし）
func (rl *RateLimiter) CanRequest() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	count := 0
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			count++
		}
	}

	return count < rl.maxRequests
}
