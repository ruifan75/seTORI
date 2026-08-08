package handler

import (
	"testing"
	"time"
)

// 時計を差し替えて、実時間を待たずにロックと解除を確かめる。
func newTestLimiter() (*loginLimiter, *time.Time) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	l := newLoginLimiter()
	l.nowFunc = func() time.Time { return now }
	return l, &now
}

func TestIPLockoutAfterLimit(t *testing.T) {
	l, now := newTestLimiter()

	// 上限の手前までは通る。
	for i := 0; i < ipFailureLimit-1; i++ {
		if ok, _ := l.allow("ip1", "alice"); !ok {
			t.Fatalf("%d 回目で拒否された。まだ通るはず", i+1)
		}
		l.recordFailure("ip1", "alice")
	}
	if ok, _ := l.allow("ip1", "alice"); !ok {
		t.Fatal("上限に達する直前で拒否された")
	}
	l.recordFailure("ip1", "alice")

	ok, remain := l.allow("ip1", "alice")
	if ok {
		t.Fatalf("上限 %d に達したのに通ってしまった", ipFailureLimit)
	}
	if remain <= 0 || remain > loginLockout {
		t.Fatalf("残り時間が不正: %v", remain)
	}

	// ロックが明ければ戻る。
	*now = now.Add(loginLockout + time.Second)
	if ok, _ := l.allow("ip1", "alice"); !ok {
		t.Fatal("ロック時間を過ぎても解除されない")
	}
}

// ロックは鍵ごと。巻き添えで他の利用者や他の IP を止めない。
func TestLockoutIsScopedToKey(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < ipFailureLimit; i++ {
		l.recordFailure("ip1", "alice")
	}

	if ok, _ := l.allow("ip1", "alice"); ok {
		t.Fatal("ロックされているはず")
	}
	if ok, _ := l.allow("ip2", "bob"); !ok {
		t.Fatal("無関係な IP とユーザーまで巻き込んで止めている")
	}
	// 同じ IP からなら、ユーザー名を変えても IP 側のロックで止まる（CPU 保護）。
	if ok, _ := l.allow("ip1", "carol"); ok {
		t.Fatal("ユーザー名を変えるだけで IP のロックを回避できてしまう")
	}
}

// 窓から外れた古い失敗は数に入れない。
func TestFailuresExpireOutOfWindow(t *testing.T) {
	l, now := newTestLimiter()

	for i := 0; i < ipFailureLimit-1; i++ {
		l.recordFailure("ip1", "alice")
	}
	// 窓を越えて時間を進めると、これまでの失敗は無効になる。
	*now = now.Add(ipWindow + time.Second)
	l.recordFailure("ip1", "alice")

	if ok, _ := l.allow("ip1", "alice"); !ok {
		t.Fatal("窓の外の失敗まで数えてロックしている")
	}
}

// 成功したら記録を消す。打ち間違えた直後に正しく入れた人を、次回締め出さない。
func TestSuccessClearsFailures(t *testing.T) {
	l, _ := newTestLimiter()

	for i := 0; i < ipFailureLimit-1; i++ {
		l.recordFailure("ip1", "alice")
	}
	l.recordSuccess("ip1", "alice")

	// 消えていれば、ここからさらに limit-1 回失敗してもまだ通るはず。
	for i := 0; i < ipFailureLimit-1; i++ {
		l.recordFailure("ip1", "alice")
	}
	if ok, _ := l.allow("ip1", "alice"); !ok {
		t.Fatal("成功しても失敗の記録が残っている")
	}
}

// IP を変えながら同じアカウントを狙う攻撃は、ユーザー名側の上限で止まる。
func TestUsernameLimitCatchesDistributedAttempts(t *testing.T) {
	l, _ := newTestLimiter()

	// 毎回違う IP なので IP 側の上限には掛からない。
	for i := 0; i < userFailureLimit; i++ {
		ip := string(rune('a'+i%26)) + string(rune('0'+i/26))
		l.recordFailure(ip, "alice")
	}

	if ok, _ := l.allow("brand-new-ip", "alice"); ok {
		t.Fatalf("IP を分散させると %d 回の失敗が素通りしてしまう", userFailureLimit)
	}
	// 別アカウントは巻き込まない。
	if ok, _ := l.allow("brand-new-ip", "bob"); !ok {
		t.Fatal("別アカウントまで止めている")
	}
}

// 鍵を変えながら叩かれてもマップが際限なく育たない。
func TestGarbageCollectionDropsStaleRecords(t *testing.T) {
	l, now := newTestLimiter()

	for i := 0; i < 500; i++ {
		l.recordFailure(string(rune(i)), "")
	}
	if len(l.byIP) == 0 {
		t.Fatal("記録されていない")
	}

	// 窓もロックも過ぎれば掃除される。gc は 1 分に 1 回までなので
	// 時間を十分に進めてから、掃除の契機となる呼び出しを 1 回入れる。
	*now = now.Add(ipWindow + loginLockout + time.Minute)
	l.recordFailure("trigger", "")

	if len(l.byIP) > 2 {
		t.Fatalf("古い記録が残っている: %d 件", len(l.byIP))
	}
}

// 空の鍵（IP が取れないなど）でロックが誤爆しない。
func TestEmptyKeyIsIgnored(t *testing.T) {
	l, _ := newTestLimiter()

	for i := 0; i < ipFailureLimit*3; i++ {
		l.recordFailure("", "")
	}
	if ok, _ := l.allow("", ""); !ok {
		t.Fatal("鍵が空のときにロックしてしまっている")
	}
}
