package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/service"
)

// maxImportBodyBytes は取り込み JSON の上限。
// 解析キャッシュ（comment_raw）を積むと配信 1 本でも数 MB になるので大きめに取るが、
// 無制限にはしない（1 vCPU / 1GB の本番機でメモリを持って行かれるため）。
const maxImportBodyBytes = 64 << 20 // 64 MiB

// handleExportStream は配信 1 本を別環境へ持って行ける形で書き出す（content:edit）。
// ?cache=1 で解析キャッシュ（comment_raw / comment_songs / チャプター / Holodex）も載せる。
func (r *Router) handleExportStream(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	withCache := isTruthy(req.URL.Query().Get("cache"))

	data, err := r.streamTransferService.Export(streamID, withCache)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if data == nil {
		respondError(w, http.StatusNotFound, "配信が見つかりません")
		return
	}

	// ファイルとして保存されることを想定した名前を付ける（手元へ落として別環境へ送る運用）。
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="stream-%s.json"`, sanitizeFilename(streamID)))
	respondJSON(w, http.StatusOK, data)
}

// handleImportStream は書き出した配信を取り込む（content:edit）。
// ?dry_run=1 で書き込まずに結果だけ返す。?cache=1 で解析キャッシュも取り込む。
func (r *Router) handleImportStream(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxImportBodyBytes)

	var data dto.StreamExport
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式: "+err.Error())
		return
	}

	result, err := r.streamTransferService.Import(&data, service.ImportOptions{
		DryRun:    isTruthy(req.URL.Query().Get("dry_run")),
		WithCache: isTruthy(req.URL.Query().Get("cache")),
	})
	if err != nil {
		if errors.Is(err, service.ErrStreamExportVersion) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// isTruthy はクエリの真偽値を読む（1 / true / yes を真とする）。
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// sanitizeFilename は Content-Disposition に入れても安全な文字だけを残す。
// YouTube の動画 ID は英数と -_ だけだが、経路をそのまま信用しない。
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
			b.WriteRune(ch)
		}
	}
	if b.Len() == 0 {
		return "stream"
	}
	return b.String()
}
