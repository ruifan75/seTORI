package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
)

func (r *Router) handleRecordVisit(w http.ResponseWriter, req *http.Request) {
	if !activityOriginAllowed(req, r.cfg.FrontendBaseURL) {
		respondError(w, http.StatusForbidden, "許可されていない送信元です")
		return
	}
	// 必要なのは pathname だけ。小さい上限を付け、計測端点を大きな body の入口にしない。
	req.Body = http.MaxBytesReader(w, req.Body, 2048)
	var body struct {
		Path string `json:"path"`
	}
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if err := r.activityService.RecordVisit(
		r.clientIPResolver.clientIP(req),
		body.Path,
		req.UserAgent(),
		currentUser(req),
	); err != nil {
		// 計測障害で通常の画面表示を壊さない。サーバーログには残して調査可能にする。
		logger.Warnf("訪客活動を記録できませんでした: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// activityOriginAllowed は別サイトが訪問者のブラウザを使って表示数を水増しするのを防ぐ。
// Origin が無い非ブラウザ通信は許可するが、DB は日・IP・利用者単位の upsert なので
// リクエストごとに新しい行を作ることはない。
func activityOriginAllowed(req *http.Request, frontendBaseURL string) bool {
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, req.Host) {
		return true
	}
	frontend, err := url.Parse(frontendBaseURL)
	return err == nil && frontend.Host != "" && strings.EqualFold(parsed.Host, frontend.Host)
}

func (r *Router) handleActivityPolicy(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"retention_days": r.activityService.RetentionDays(),
	})
}

func (r *Router) handleListActivity(w http.ResponseWriter, req *http.Request) {
	days := positiveQueryInt(req, "days", 7)
	page := positiveQueryInt(req, "page", 1)
	limit := positiveQueryInt(req, "limit", 50)
	if days > r.activityService.RetentionDays() {
		days = r.activityService.RetentionDays()
	}
	if limit > 100 {
		limit = 100
	}

	items, total, err := r.activityService.List(
		days, page, limit,
		req.URL.Query().Get("kind"),
		req.URL.Query().Get("q"),
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"activity":       items,
		"total":          total,
		"page":           page,
		"limit":          limit,
		"retention_days": r.activityService.RetentionDays(),
	})
}

func (r *Router) handleActivityStats(w http.ResponseWriter, req *http.Request) {
	days := positiveQueryInt(req, "days", 7)
	stats, err := r.activityService.Stats(days)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"stats":          stats,
		"retention_days": r.activityService.RetentionDays(),
	})
}

func (r *Router) handleUserActivitySummaries(w http.ResponseWriter, req *http.Request) {
	days := positiveQueryInt(req, "days", 30)
	items, err := r.activityService.UserSummaries(days)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"users":          items,
		"retention_days": r.activityService.RetentionDays(),
	})
}

func positiveQueryInt(req *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(req.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
