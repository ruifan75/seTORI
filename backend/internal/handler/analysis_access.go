package handler

import "net/http"

// withAnalysisAccess は解析素材を返すルートの入口を揃える。
// POST の分析結果・再取得も GET と同じ視界で扱う。PathValue は
// ServeMux が各セグメントを復号して設定した値を使う。
func (r *Router) withAnalysisAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !r.requireAnalysisAccess(w, req, req.PathValue("id")) {
			return
		}
		next(w, req)
	}
}
