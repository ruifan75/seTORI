package handler

import (
	"sync"
	"time"
)

// ログイン試行の絞り込み。
//
// なぜ要るか：
//   - パスワードの総当たりを止める。管理者は権限が全部あるうえ、ユーザー名も
//     推測しやすい（admin など）ので、無制限に試せる状態は放置できない
//   - CPU の枯渇を防ぐ。bcrypt は意図的に重く、1 vCPU の本番機では
//     ログイン端点を叩き続けられるだけでサイト全体が遅くなる。
//     パスワードが当たらなくても攻撃として成立してしまう
//
// バックエンドは 1 インスタンス限定（OAuth の state も同様にメモリ保持）なので、
// 保存先はメモリでよい。複数インスタンスに増やすときはここも DB か Redis へ移す。
const (
	// IP 単位。単一の相手からの総当たりと CPU 枯渇の両方をここで止める。
	ipFailureLimit = 5
	ipWindow       = 5 * time.Minute

	// ユーザー名単位。IP を分散させてくる攻撃を拾う。
	//
	// しきい値を IP 側より緩くしてあるのは、ここを厳しくすると
	// 「わざと失敗させて対象アカウントを締め出す」妨害が成立するため。
	// 20 回を分散して撃つのは攻撃側にもそれなりの費用がかかる一方、
	// 本人が 20 回続けて間違えることはまず無い、という線引き。
	userFailureLimit = 20
	userWindow       = 15 * time.Minute

	// 上限に達してから、この時間が経つまで受け付けない。
	loginLockout = 15 * time.Minute
)

type failureRecord struct {
	// 直近の失敗時刻。窓から外れたものは判定時に捨てる。
	times []time.Time
	// 上限に達した時点で入る解除時刻。ゼロ値ならロックされていない。
	lockedUntil time.Time
}

// loginLimiter は失敗回数を数えてログイン試行を拒む。
// ゼロ値で使える。
type loginLimiter struct {
	mu      sync.Mutex
	byIP    map[string]*failureRecord
	byUser  map[string]*failureRecord
	lastGC  time.Time
	nowFunc func() time.Time // テスト用に時計を差し替える
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		byIP:    make(map[string]*failureRecord),
		byUser:  make(map[string]*failureRecord),
		nowFunc: time.Now,
	}
}

func (l *loginLimiter) now() time.Time {
	if l.nowFunc != nil {
		return l.nowFunc()
	}
	return time.Now()
}

// allow は試行してよいかを返す。ロック中なら false と残り時間を返す。
func (l *loginLimiter) allow(ip, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.gcLocked(now)

	// 早い方（残りが長い方）の解除時刻を返したいので、両方見る。
	var worst time.Duration
	for _, rec := range []*failureRecord{l.byIP[ip], l.byUser[username]} {
		if rec == nil || rec.lockedUntil.IsZero() {
			continue
		}
		if remain := rec.lockedUntil.Sub(now); remain > 0 && remain > worst {
			worst = remain
		}
	}
	if worst > 0 {
		return false, worst
	}
	return true, 0
}

// recordFailure は失敗を 1 件数え、上限に達していればロックする。
func (l *loginLimiter) recordFailure(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.gcLocked(now)

	bump(l.byIP, ip, now, ipWindow, ipFailureLimit)
	bump(l.byUser, username, now, userWindow, userFailureLimit)
}

// recordSuccess は成功した組み合わせの記録を消す。
// 正しく入れた本人が、直前の打ち間違いのせいで次に締め出されないようにする。
func (l *loginLimiter) recordSuccess(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.byIP, ip)
	delete(l.byUser, username)
}

func bump(m map[string]*failureRecord, key string, now time.Time, window time.Duration, limit int) {
	if key == "" {
		return
	}
	rec := m[key]
	if rec == nil {
		rec = &failureRecord{}
		m[key] = rec
	}

	// 窓から外れた失敗は落とす。
	cutoff := now.Add(-window)
	kept := rec.times[:0]
	for _, t := range rec.times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	rec.times = append(kept, now)

	if len(rec.times) >= limit {
		rec.lockedUntil = now.Add(loginLockout)
		// 数え直しから始めさせる（解除直後に 1 回で再ロックされないように）。
		rec.times = nil
	}
}

// gcLocked は用済みの記録を捨てる。
// これが無いと、ユーザー名や IP を変えながら叩かれるだけでマップが際限なく育つ。
// 呼び出し側で mu を保持していること。
func (l *loginLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now

	sweep := func(m map[string]*failureRecord, window time.Duration) {
		cutoff := now.Add(-window)
		for key, rec := range m {
			if rec.lockedUntil.After(now) {
				continue
			}
			// 窓内の失敗が 1 件も残っていなければ、この記録は無意味。
			live := false
			for _, t := range rec.times {
				if t.After(cutoff) {
					live = true
					break
				}
			}
			if !live {
				delete(m, key)
			}
		}
	}
	sweep(l.byIP, ipWindow)
	sweep(l.byUser, userWindow)
}
