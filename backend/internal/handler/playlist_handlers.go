package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/service"
)

// playlistErrStatus はサービス層のエラーを HTTP ステータスへ対応づける。
// 他人の private/unlisted は「無い」ものとして 404 を返す（存在を漏らさない）。
func playlistErrStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrPlaylistNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrPlaylistForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrPlaylistName), errors.Is(err, service.ErrPlaylistVisibility),
		errors.Is(err, service.ErrPerformanceInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// requireLogin はログイン済みのユーザー ID を返す。未ログインなら 401 を返して false。
func (r *Router) requireLogin(w http.ResponseWriter, req *http.Request) (uuid.UUID, bool) {
	user := currentUser(req)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "ログインが必要です")
		return uuid.Nil, false
	}
	return user.ID, true
}

// viewerID は閲覧者の ID を返す（未ログインなら nil）。公開範囲の判定に使う。
func viewerID(req *http.Request) *uuid.UUID {
	if user := currentUser(req); user != nil {
		return &user.ID
	}
	return nil
}

// parsePlaylistID はパスの {id} を UUID として取り出す。
func parsePlaylistID(w http.ResponseWriter, req *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "プレイリスト ID が不正です")
		return uuid.Nil, false
	}
	return id, true
}

// GET /api/playlists — 本人のプレイリスト一覧（要ログイン）
func (r *Router) handleListMyPlaylists(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	result, err := r.playlistService.ListMine(userID)
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// GET /api/playlists/public — 公開プレイリスト一覧（未ログインでも可）
func (r *Router) handleListPublicPlaylists(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	result, err := r.playlistService.ListPublic(limit, (page-1)*limit, viewerID(req))
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// POST /api/playlists — 作成（要ログイン）
func (r *Router) handleCreatePlaylist(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	var body dto.CreatePlaylistRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}
	result, err := r.playlistService.Create(userID, &body)
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, result)
}

// GET /api/playlists/{id} — 詳細
func (r *Router) handleGetPlaylist(w http.ResponseWriter, req *http.Request) {
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	result, err := r.playlistService.Get(id, viewerID(req))
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// PUT /api/playlists/{id} — 名称・説明・公開範囲の更新（所有者のみ）
func (r *Router) handleUpdatePlaylist(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	var body dto.UpdatePlaylistRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}
	result, err := r.playlistService.Update(id, userID, &body)
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// DELETE /api/playlists/{id} — 削除（所有者のみ）
func (r *Router) handleDeletePlaylist(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	if err := r.playlistService.Delete(id, userID); err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "プレイリストを削除しました"})
}

// GET /api/playlists/{id}/items — 収録曲（並び順）
func (r *Router) handleListPlaylistItems(w http.ResponseWriter, req *http.Request) {
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	perfs, err := r.playlistService.ListItems(id, viewerID(req))
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, r.streamService.ComposePerformanceList(perfs))
}

// POST /api/playlists/{id}/items — 1曲または複数曲の追加（所有者のみ）
func (r *Router) handleAddPlaylistItem(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	var body dto.AddPlaylistItemRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}
	performanceIDs := body.PerformanceIDs
	if len(performanceIDs) == 0 && body.PerformanceID != "" {
		performanceIDs = []string{body.PerformanceID}
	}
	added, err := r.playlistService.AddItems(id, userID, performanceIDs)
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, dto.AddPlaylistItemsResponse{
		Added:   added,
		Skipped: len(performanceIDs) - added,
	})
}

// DELETE /api/playlists/{id}/items/{performanceId} — 曲の削除（所有者のみ）
func (r *Router) handleRemovePlaylistItem(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	if err := r.playlistService.RemoveItem(id, userID, req.PathValue("performanceId")); err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "プレイリストから削除しました"})
}

// PUT /api/playlists/{id}/order — 並び替え（所有者のみ）
func (r *Router) handleReorderPlaylist(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	id, ok := parsePlaylistID(w, req)
	if !ok {
		return
	}
	var body dto.ReorderPlaylistRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}
	if err := r.playlistService.Reorder(id, userID, body.PerformanceIDs); err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "並び順を更新しました"})
}

// GET /api/playlists/shared/{slug} — 限定公開 URL からの詳細
func (r *Router) handleGetSharedPlaylist(w http.ResponseWriter, req *http.Request) {
	result, err := r.playlistService.GetByShareSlug(req.PathValue("slug"), viewerID(req))
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// GET /api/playlists/shared/{slug}/items — 限定公開 URL からの収録曲
func (r *Router) handleListSharedPlaylistItems(w http.ResponseWriter, req *http.Request) {
	perfs, err := r.playlistService.ListItemsByShareSlug(req.PathValue("slug"), viewerID(req))
	if err != nil {
		respondError(w, playlistErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, r.streamService.ComposePerformanceList(perfs))
}
