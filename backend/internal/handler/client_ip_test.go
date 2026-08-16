package handler

import (
	"net/http/httptest"
	"testing"
)

func mustIPResolver(t *testing.T, cidrs string) *clientIPResolver {
	t.Helper()
	resolver, err := newClientIPResolver(cidrs)
	if err != nil {
		t.Fatalf("newClientIPResolver: %v", err)
	}
	return resolver
}

func TestClientIPTrustBoundary(t *testing.T) {
	resolver := mustIPResolver(t, "172.16.0.0/12, 10.0.0.0/8")
	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "trusted Caddy からの Cloudflare header を採る",
			remoteAddr: "172.18.0.5:41000",
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.7",
				"X-Forwarded-For":  "198.51.100.1, 203.0.113.7",
			},
			want: "203.0.113.7",
		},
		{
			name:       "untrusted 直結元の偽 CF header は無視する",
			remoteAddr: "192.0.2.44:57321",
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.99",
				"X-Forwarded-For":  "203.0.113.98",
			},
			want: "192.0.2.44",
		},
		{
			name:       "trusted proxy の XFF は右から読む",
			remoteAddr: "10.0.0.2:5000",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9, 172.20.0.4"},
			want:       "203.0.113.9",
		},
		{
			name:       "IPv6 を正規化する",
			remoteAddr: "[2001:db8::1]:443",
			want:       "2001:db8::1",
		},
		{
			name:       "IPv4 mapped IPv6 を IPv4 に寄せる",
			remoteAddr: "[::ffff:192.0.2.50]:1234",
			want:       "192.0.2.50",
		},
		{
			name:       "不正な CF header は XFF へ fallback",
			remoteAddr: "172.18.0.5:41000",
			headers: map[string]string{
				"CF-Connecting-IP": "not-an-ip",
				"X-Forwarded-For":  "203.0.113.12",
			},
			want: "203.0.113.12",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/auth/login", nil)
			req.RemoteAddr = tc.remoteAddr
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			if got := resolver.clientIP(req); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInvalidTrustedProxyCIDR(t *testing.T) {
	if _, err := newClientIPResolver("172.16.0.0/12, definitely-not-a-cidr"); err == nil {
		t.Fatal("invalid CIDR should fail")
	}
}

func TestClientHintDistinguishesVisitors(t *testing.T) {
	resolver := mustIPResolver(t, "172.16.0.0/12")
	hintFor := func(cfIP string) string {
		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = "172.18.0.5:41000"
		req.Header.Set("CF-Connecting-IP", cfIP)
		return resolver.clientHint(req)
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
