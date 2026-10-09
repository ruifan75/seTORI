package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
	"net/http"
	"strconv"
	"time"
)

func processedError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, repository.ErrProcessedConflict) {
		code = http.StatusConflict
	}
	if errors.Is(err, repository.ErrProcessedSelection) {
		code = http.StatusBadRequest
	}
	if errors.Is(err, sql.ErrNoRows) {
		code = http.StatusNotFound
	}
	respondError(w, code, err.Error())
}
func (r *Router) handleProcessedCandidates(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	f, err := parseProcessedFilters(req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, count, err := r.processedReview.Candidates(f, limit, offset, viewerAccess(req))
	if err != nil {
		processedError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"candidates": rows, "total": count})
}
func (r *Router) handleProcessedPreview(w http.ResponseWriter, req *http.Request) {
	var body struct {
		IDs            []string `json:"stream_ids"`
		AfterProcessed *bool    `json:"is_processed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 65536)).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効な配信一覧")
		return
	}
	if body.AfterProcessed == nil {
		respondError(w, http.StatusBadRequest, "is_processed は必須です")
		return
	}
	var by *uuid.UUID
	if u := currentUser(req); u != nil {
		by = &u.ID
	}
	id, err := r.processedReview.Preview(body.IDs, *body.AfterProcessed, by)
	if err != nil {
		processedError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"run_id": id, "count": len(body.IDs), "is_processed": *body.AfterProcessed})
}
func (r *Router) handleProcessedApply(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	n, err := r.processedReview.Apply(id)
	if err != nil {
		processedError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"changed": n})
}
func (r *Router) handleProcessedRevert(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	n, skipped, err := r.processedReview.Revert(id)
	if err != nil {
		processedError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"reverted": n, "skipped": skipped})
}
func (r *Router) handleProcessedRuns(w http.ResponseWriter, req *http.Request) {
	runs, err := r.processedReview.Runs()
	if err != nil {
		processedError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// 未指定の変更先は「付ける」。未知の値を「外す」へ読み替えない。
func parseProcessedFilters(req *http.Request) (repository.ProcessedReviewFilters, error) {
	q := req.URL.Query()
	f := repository.ProcessedReviewFilters{Query: q.Get("q"), ChannelID: q.Get("channel_id"), TagIDs: q["tag"], AfterProcessed: true}
	if len(f.TagIDs) > 20 {
		return f, errors.New("tag は20個以下にしてください")
	}
	switch q.Get("is_processed") {
	case "", "true":
	case "false":
		f.AfterProcessed = false
	default:
		return f, errors.New("is_processed は true / false のいずれかです")
	}
	for _, field := range []struct {
		key    string
		target **bool
	}{{"hidden", &f.Hidden}, {"has_performances", &f.HasPerformances}} {
		switch q.Get(field.key) {
		case "", "all":
		case "true", "false":
			value := q.Get(field.key) == "true"
			*field.target = &value
		default:
			return f, errors.New(field.key + " は all / true / false のいずれかです")
		}
	}
	for _, field := range []struct {
		key    string
		target **time.Time
	}{{"from", &f.From}, {"until", &f.Until}} {
		if value := q.Get(field.key); value != "" {
			date, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return f, errors.New(field.key + " は RFC3339 の日時にしてください")
			}
			*field.target = &date
		}
	}
	if f.From != nil && f.Until != nil && !f.From.Before(*f.Until) {
		return f, errors.New("開始日時は終了日時より前にしてください")
	}
	return f, nil
}
