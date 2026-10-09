package handler

import (
	"context"
	"net/http"
	"time"
)

// 初期化・DB 接続などの起動準備も稼働時間に含める。壁時計の変更には追従しない。
var processStartedAt = time.Now()

const healthDBTimeout = time.Second

type apiHealthResponse struct {
	Status        string `json:"status"`
	Commit        string `json:"commit"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// 監視には利用者の視点が不要。Bearer が付いていても、認証の DB 照会で
// 疎通確認のタイムアウトを迂回しない。認可の分類は通常の判定に従う。
func isPublicHealthRequest(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	path := authzPath(req.URL.EscapedPath())
	if path != "/api/health" {
		return false
	}
	permission, login := requiredPermission(req.Method, path)
	return permission == "" && !login
}

// handleAPIHealth は DB の疎通・版・稼働秒数だけを返す。
// 接続文字列・設定・DB のエラーは応答にもログにも出さない。
func (r *Router) handleAPIHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	response := apiHealthResponse{
		Status:        "unavailable",
		Commit:        buildCommit,
		UptimeSeconds: int64(time.Since(processStartedAt) / time.Second),
	}
	status := http.StatusServiceUnavailable
	if r.db != nil {
		ctx, cancel := context.WithTimeout(req.Context(), healthDBTimeout)
		defer cancel()
		var value int
		if err := r.db.QueryRowContext(ctx, "SELECT 1").Scan(&value); err == nil && value == 1 {
			status = http.StatusOK
			response.Status = "ok"
		}
	}
	respondJSON(w, status, response)
}
