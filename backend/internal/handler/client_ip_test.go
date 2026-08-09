package handler

import (
	"net/http/httptest"
	"testing"
)

// 由来ごとの優先順位。ここを取り違えると、絞り込みが「効いているつもりで
// 効いていない」形で壊れる（Cloudflare を挟んだ直後に実際に起きた）。
func TestClientIPPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "Cloudflare 経由なら CF-Connecting-IP を採る",
			remoteAddr: "172.71.0.5:41000", // Cloudflare エッジ
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.7",
				"X-Forwarded-For":  "198.51.100.1, 203.0.113.7",
			},
			want: "203.0.113.7",
		},
		{
			name:       "Cloudflare が無ければ XFF の先頭",
			remoteAddr: "10.0.0.2:5000",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.2"},
			want:       "203.0.113.9",
		},
		{
			name:       "ヘッダーが無ければ RemoteAddr（ポートは落とす）",
			remoteAddr: "192.0.2.44:57321",
			want:       "192.0.2.44",
		},
		{
			name:       "IPv6 の RemoteAddr でもポートを落とせる",
			remoteAddr: "[2001:db8::1]:443",
			want:       "2001:db8::1",
		},
		{
			name:       "空の XFF は無視して RemoteAddr へ落ちる",
			remoteAddr: "192.0.2.50:1234",
			headers:    map[string]string{"X-Forwarded-For": "  "},
			want:       "192.0.2.50",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/auth/login", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := clientIP(req); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// 別々の訪問者は別々の鍵になり、同じ訪問者は同じ鍵になる。
// これが崩れると絞り込みの数え上げが成立しない。
func TestClientHintDistinguishesVisitors(t *testing.T) {
	hintFor := func(cfIP string) string {
		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = "172.71.0.5:41000"
		req.Header.Set("CF-Connecting-IP", cfIP)
		return clientHint(req)
	}

	a1, a2 := hintFor("203.0.113.7"), hintFor("203.0.113.7")
	b := hintFor("203.0.113.8")

	if a1 != a2 {
		t.Fatal("同じ訪問者から違う鍵が出ている")
	}
	if a1 == b {
		t.Fatal("別の訪問者が同じ鍵に潰れている")
	}
	if a1 == "" {
		t.Fatal("鍵が空になっている")
	}
}
