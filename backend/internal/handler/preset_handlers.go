package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ruifan75/setori/internal/service"
)

// presetErrStatus は未定義のキーを 404 にする。それ以外はプレイリストと同じ扱い。
func presetErrStatus(err error) int {
	if errors.Is(err, service.ErrPresetNotFound) {
		return http.StatusNotFound
	}
	return playlistErrStatus(err)
}

// GET /api/presets — プリセットプレイリストの一覧（未ログインでも可）
func (r *Router) handleListPresets(w http.ResponseWriter, req *http.Request) {
	result, err := r.presetService.List(viewerID(req))
	if err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// GET /api/presets/followed — フォロー中のプリセット（要ログイン）
func (r *Router) handleListFollowedPresets(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	result, err := r.presetService.ListFollowed(userID)
	if err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// GET /api/presets/{key} — 1 件の情報（未ログインでも可）
func (r *Router) handleGetPreset(w http.ResponseWriter, req *http.Request) {
	result, err := r.presetService.Get(req.PathValue("key"), viewerID(req))
	if err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// GET /api/presets/{key}/items — 中身（未ログインでも可）
func (r *Router) handleListPresetItems(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	perfs, err := r.presetService.ListItems(req.PathValue("key"), limit)
	if err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, r.streamService.ComposePerformanceList(perfs))
}

// POST /api/presets/{key}/follow — フォロー（要ログイン）
func (r *Router) handleFollowPreset(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	if err := r.presetService.Follow(userID, req.PathValue("key")); err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "フォローしました"})
}

// DELETE /api/presets/{key}/follow — フォロー解除（要ログイン）
func (r *Router) handleUnfollowPreset(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	if err := r.presetService.Unfollow(userID, req.PathValue("key")); err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "フォローを解除しました"})
}

// POST /api/presets/{key}/copy — 自分のプレイリストへ複製（要ログイン）
func (r *Router) handleCopyPreset(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	result, err := r.presetService.Copy(userID, req.PathValue("key"))
	if err != nil {
		respondError(w, presetErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, result)
}
