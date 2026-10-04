package handler

import (
	"bytes"
	"net/http"

	"github.com/ruifan75/setori/internal/repository"
)

// withAnalysisAccess は解析素材を返すルートの入口を揃える。
// POST の分析結果・再取得も GET と同じ視界で扱う。PathValue は
// ServeMux が各セグメントを復号して設定した値を使う。
// 外部取得・解析中に公開可否が変わりうるため、応答を出す前にも確認する。
func (r *Router) withAnalysisAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !r.requireAnalysisAccess(w, req, req.PathValue("id")) {
			return
		}
		if viewerAccess(req) == repository.RestrictedView {
			next(w, req)
			return
		}
		response := &analysisResponse{header: make(http.Header)}
		next(response, req)
		if !r.requireAnalysisAccess(w, req, req.PathValue("id")) {
			return
		}
		for key, values := range response.header {
			w.Header()[key] = append([]string(nil), values...)
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(response.body.Bytes())
	}
}

// 解析の端点は JSON 応答で、ストリーミングしない。判定前に本文・ヘッダーを送らない。
type analysisResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *analysisResponse) Header() http.Header { return w.header }
func (w *analysisResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *analysisResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}
