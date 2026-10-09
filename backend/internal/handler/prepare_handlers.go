package handler

import (
	"errors"
	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/service"
	"net/http"
	"strings"
)

func (r *Router) handlePrepareStreams(w http.ResponseWriter, req *http.Request) {
	channelID := strings.TrimSpace(req.URL.Query().Get("singer_id"))
	if channelID == "" {
		respondError(w, http.StatusBadRequest, "singer_id は必須です")
		return
	}
	channel, err := r.channelService.GetByID(channelID, false, viewerAccess(req))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if channel == nil {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}
	var by *uuid.UUID
	if u := currentUser(req); u != nil {
		by = &u.ID
	}
	run, err := r.prepareService.Start(channelID, by)
	if errors.Is(err, service.ErrTaskRunning) || errors.Is(err, service.ErrBatchAlreadyRunning) || errors.Is(err, service.ErrBatchFillAlreadyRunning) {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]any{"task_id": run.ID})
}
func (r *Router) handleCancelPreparation(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	if err := r.taskRunService.CancelPreparation(id); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, http.StatusAccepted, map[string]string{"message": "停止を要求しました"})
}
