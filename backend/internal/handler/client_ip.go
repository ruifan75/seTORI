package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// clientIPResolver は転送ヘッダーを「直前の接続元が trusted proxy のときだけ」読む。
// 本番の直前 proxy は Docker 内部の Caddy。さらに origin への 80/443 は Vultr 側で
// Cloudflare の IP レンジだけに制限することで、CF-Connecting-IP の詐称を防ぐ。
type clientIPResolver struct {
	trusted []netip.Prefix
}

func newClientIPResolver(rawCIDRs string) (*clientIPResolver, error) {
	resolver := &clientIPResolver{}
	for _, value := range strings.FieldsFunc(rawCIDRs, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	}) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return resolver, fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (r *clientIPResolver) clientIP(req *http.Request) string {
	remote, ok := parseRemoteAddr(req.RemoteAddr)
	if !ok {
		return ""
	}
	if !r.isTrusted(remote) {
		return remote.Unmap().String()
	}

	// Cloudflare が origin へ付ける単一値。値が IP でない場合は採用しない。
	if value := strings.TrimSpace(req.Header.Get("CF-Connecting-IP")); value != "" {
		if ip, err := netip.ParseAddr(value); err == nil {
			return ip.Unmap().String()
		}
	}

	// Cloudflare 以外の trusted proxy でも動くよう、XFF は右から辿り、
	// proxy 網でない最初の IP を利用する。左端を無条件に採ると詐称できる。
	values := strings.Split(req.Header.Get("X-Forwarded-For"), ",")
	for i := len(values) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(values[i]))
		if err != nil {
			continue
		}
		if !r.isTrusted(ip) {
			return ip.Unmap().String()
		}
	}

	return remote.Unmap().String()
}

func (r *clientIPResolver) clientHint(req *http.Request) string {
	ip := r.clientIP(req)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])[:16]
}

func (r *clientIPResolver) isTrusted(ip netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(ip.Unmap()) || prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteAddr(value string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr(), true
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	return ip, err == nil
}
