package ratelimit

import (
	"sync"
	"time"
)

// RateLimiter 提供 rate limiting 功能
type RateLimiter struct {
	maxRequests int           // 最大請求數
	window      time.Duration // 時間窗口
	requests    []time.Time   // 請求時間記錄
	mu          sync.Mutex
}

// NewRateLimiter 建立新的 rate limiter
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		window:      window,
		requests:    make([]time.Time, 0),
	}
}

// Wait 等待直到可以發送請求
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// 清理過期的請求記錄
	cutoff := now.Add(-rl.window)
	validRequests := make([]time.Time, 0)
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}
	rl.requests = validRequests

	// 如果已達到限制，等待直到最舊的請求過期
	if len(rl.requests) >= rl.maxRequests {
		oldestRequest := rl.requests[0]
		waitDuration := oldestRequest.Add(rl.window).Sub(now)
		if waitDuration > 0 {
			time.Sleep(waitDuration + 100*time.Millisecond) // 加一點緩衝
		}
		// 重新清理
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

	// 記錄新請求
	rl.requests = append(rl.requests, now)
}

// CanRequest 檢查是否可以立即發送請求（不等待）
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
