package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
	"net/http"
	"strconv"
)

func visibilityError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, repository.ErrVisibilityConflict) {
		code = http.StatusConflict
	}
	if errors.Is(err, repository.ErrVisibilitySelection) {
		code = http.StatusBadRequest
	}
	if errors.Is(err, sql.ErrNoRows) {
		code = http.StatusNotFound
	}
	respondError(w, code, err.Error())
}
func (r *Router) handleVisibilityCandidates(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	rows, count, err := r.visibilityReview.Candidates(req.URL.Query().Get("dismissed") == "true", limit, offset)
	if err != nil {
		visibilityError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"candidates": rows, "total": count})
}
func (r *Router) handleVisibilityPreview(w http.ResponseWriter, req *http.Request) {
	var body struct {
		IDs []string `json:"stream_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 65536)).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効な配信一覧")
		return
	}
	var by *uuid.UUID
	if u := currentUser(req); u != nil {
		by = &u.ID
	}
	id, err := r.visibilityReview.Preview(body.IDs, by)
	if err != nil {
		visibilityError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"run_id": id, "count": len(body.IDs)})
}
func (r *Router) handleVisibilityApply(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	n, err := r.visibilityReview.Apply(id)
	if err != nil {
		visibilityError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"changed": n})
}
func (r *Router) handleVisibilityRevert(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	n, skipped, err := r.visibilityReview.Revert(id)
	if err != nil {
		visibilityError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"reverted": n, "skipped": skipped})
}
func (r *Router) handleVisibilityRuns(w http.ResponseWriter, req *http.Request) {
	runs, err := r.visibilityReview.Runs()
	if err != nil {
		visibilityError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
