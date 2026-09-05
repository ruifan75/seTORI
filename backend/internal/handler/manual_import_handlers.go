package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ruifan75/setori/internal/service"
)

// ========== 手動での取り込み（会限配信のため） ==========
//
// 会限配信は本番からは入力源を取れない ── コメントは YouTube Data API が
// 403（API キー方式なので cookie を足しても変わらない）、live chat は視聴資格が要る。
// メンバー資格のある編集者が手元で yt-dlp を回し、その出力をここへ持ち込む。
//
// **どちらも `content:edit`。** 権限は `requiredPermission` の
// `isStreamSubresource(..., "import")` が見る（ServeMux より前・パス文字列だけで
// 決まるので、ここに書いてあることが唯一の保護になる）。

// 取り込めるファイルの上限。
//
// **実測（7465 秒の歌枠）で live chat は 18MB を超えてなお増えていた**ので、
// 長い配信でも入るように大きめに取る。info.json はコメント本文だけなので桁が違う。
// 上限は**切り捨てではなく拒否**する ── 切ると、途中で切れた JSON を
// 「読めないファイル」として弾くのではなく、静かに一部だけ取り込むことになる。
const (
	maxLiveChatUploadBytes = 192 << 20 // 192MB
	maxInfoJSONUploadBytes = 32 << 20  // 32MB
)

// readUpload はアップロードされた 1 ファイルを読む（multipart の "file" 欄）。
//
// **上限は 2 か所で効かせる。** `MaxBytesReader` が読み取りそのものを止め、
// `ParseMultipartForm` がメモリへ載せる量を抑える。前者だけだと、上限ちょうどで
// 切れたファイルが「読めた」ように見える。
func readUpload(w http.ResponseWriter, req *http.Request, limit int64) ([]byte, error) {
	req.Body = http.MaxBytesReader(w, req.Body, limit+1)
	if err := req.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("ファイルを読み取れません（上限 %dMB）: %w", limit>>20, err)
	}
	f, _, err := req.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("ファイルが添付されていません: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("ファイルを読み取れません: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("ファイルが大きすぎます（上限 %dMB）", limit>>20)
	}
	if len(data) == 0 {
		return nil, errors.New("ファイルが空です")
	}
	return data, nil
}

// handleImportInfoJSON は yt-dlp の info.json からコメントを取り込む。
func (r *Router) handleImportInfoJSON(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	data, err := readUpload(w, req, maxInfoJSONUploadBytes)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := r.commentService.ImportInfoJSON(videoID, data)
	switch {
	case errors.Is(err, service.ErrInfoJSONMismatch):
		// **取り違えは 400 で名指しする。** どの配信のものだったかまで返す ──
		// 「読めません」だけだと、人はファイルが壊れていると考えて取り直す。
		respondError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, service.ErrInfoJSONUnreadable):
		respondError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// handleImportLiveChat は yt-dlp の live_chat.json を取り込む。
func (r *Router) handleImportLiveChat(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	data, err := readUpload(w, req, maxLiveChatUploadBytes)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := r.chatEndService.ImportLiveChat(videoID, data)
	switch {
	case errors.Is(err, service.ErrLiveChatUnreadable):
		respondError(w, http.StatusBadRequest,
			"live chat replay として読めません。yt-dlp が書いた .live_chat.json をそのまま添付してください（途中で切れたファイルも弾かれます）")
		return
	case err != nil:
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// handleGetImportedLiveChat は置いてある live chat の要約を返す（無ければ present=false）。
func (r *Router) handleGetImportedLiveChat(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	res, ok := r.chatEndService.CachedLiveChat(videoID)
	respondJSON(w, http.StatusOK, map[string]interface{}{"present": ok, "chat": res})
}

// handleDeleteImportedLiveChat は置いてある live chat を消す。
//
// **取り違えを取り消せることが、手動取り込みを許す条件。** ファイルがあると
// yt-dlp は呼ばれず force 分析でも読み直さないので、消せないと別の配信の
// チャットが恒久的に居座る。
func (r *Router) handleDeleteImportedLiveChat(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	if err := r.chatEndService.DeleteCachedLiveChat(videoID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
}
