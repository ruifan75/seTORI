package handler

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
// **本番は 1 vCPU / 1GB。** 上限は「入るように大きく」ではなく、実測から決める ──
// 2 時間 4 分の歌枠で live chat は 12.5MB だったので、5 時間級でも 64MB には収まる。
// 大きく取っても入るものが増えるわけではなく、落ちる余地が増えるだけ。
//
// 上限は**切り捨てではなく拒否**する ── 切ると、途中で切れた JSON を
// 「読めないファイル」として弾くのではなく、静かに一部だけ取り込むことになる。
const (
	maxLiveChatUploadBytes = 64 << 20 // 64MB（実測 12.5MB / 2 時間）
	maxInfoJSONUploadBytes = 32 << 20 // 32MB（JSON なので全部メモリへ載る）
)

// openUpload はアップロードされた 1 ファイルを開く（multipart の "file" 欄）。
//
// **上限は body の側で効かせる。** `MaxBytesReader` が読み取りそのものを止めるので、
// `ParseMultipartForm` はそこで失敗し、大きすぎるものは受理されない。
// メモリへ載せるのは 8MB までで、残りは Go が一時ファイルへ逃がす
// （リクエスト終了時に `finishRequest` が消すので、こちらで片付ける必要は無い）。
//
// 戻り値は**読み手**であって中身ではない。呼び出し側が必要なときだけ全部読む
// ── live chat は実測 12.5MB あり、本番（1 vCPU / 1GB）で毎回そのぶん
// RSS を跳ねさせたくない。
func openUpload(w http.ResponseWriter, req *http.Request, limit int64) (multipart.File, error) {
	req.Body = http.MaxBytesReader(w, req.Body, limit)
	if err := req.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("ファイルを読み取れません（上限 %dMB を超えていないか確認してください）", limit>>20)
	}
	f, hdr, err := req.FormFile("file")
	if err != nil {
		return nil, errors.New("ファイルが添付されていません")
	}
	if hdr.Size == 0 {
		f.Close()
		return nil, errors.New("ファイルが空です")
	}
	return f, nil
}

// readUploadAll は中身を全部読む（JSON のように全体が要るもの向け）。
func readUploadAll(w http.ResponseWriter, req *http.Request, limit int64) ([]byte, error) {
	f, err := openUpload(w, req, limit)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("ファイルを読み取れません: %w", err)
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
	data, err := readUploadAll(w, req, maxInfoJSONUploadBytes)
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
	// **読み切らずに渡す。** 実測 12.5MB あるので、本番（1 vCPU / 1GB）で
	// 毎回そのぶんメモリへ載せない。取り込み側が一時ファイルへ流して検証する。
	f, err := openUpload(w, req, maxLiveChatUploadBytes)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer f.Close()
	res, err := r.chatEndService.ImportLiveChat(videoID, f)
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
