package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 楽曲の統合候補（照合が外れて新曲として登録されてしまったものの後始末）。
//
// 照合ルールをどれだけ良くしても、同一人物の別名義（松任谷由実 / 荒井由実）のように
// 文字列だけでは決められない組は残る。そこは新曲として登録しつつ候補に積み、
// 既存の統合機能（POST /api/songs/{id}/merge）で人が畳む。
// 候補に出さず黙って作ると、重複が見えないまま増え続ける。

// handleListMergeCandidates は未処理の統合候補を返す（content:edit）。
func (r *Router) handleListMergeCandidates(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	candidates, err := r.songMatchService.ListOpenMergeCandidates(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"candidates": toMergeCandidateResponses(candidates),
		"total":      len(candidates),
	})
}

// handleGetSongMergeCandidates は特定の楽曲に紐づく未処理候補を返す（楽曲詳細用・公開）。
func (r *Router) handleGetSongMergeCandidates(w http.ResponseWriter, req *http.Request) {
	songID, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}
	candidates, err := r.songMatchService.FindOpenMergeCandidatesForSong(songID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"candidates": toMergeCandidateResponses(candidates),
	})
}

// handleDismissMergeCandidate は「別の曲なので統合しない」と記録する（content:edit）。
// 却下できないと、正当に似ているだけの組が一覧に残り続けて邪魔になる。
func (r *Router) handleDismissMergeCandidate(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な候補ID")
		return
	}
	if err := r.songMatchService.DismissMergeCandidate(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "候補が見つからないか、既に処理済みです")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "統合候補を却下しました"})
}

func toMergeCandidateResponses(candidates []repository.MergeCandidate) []dto.MergeCandidateResponse {
	out := make([]dto.MergeCandidateResponse, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, dto.MergeCandidateResponse{
			ID:           c.ID.String(),
			Score:        c.Score,
			Reason:       c.Reason,
			NewSong:      toMergeCandidateSong(c.NewSong, c.PerfCountNew),
			ExistingSong: toMergeCandidateSong(c.ExistingSong, c.PerfCountOld),
		})
	}
	return out
}

func toMergeCandidateSong(s models.Song, perfCount int) dto.MergeCandidateSong {
	return dto.MergeCandidateSong{
		ID:               s.ID.String(),
		Name:             s.Name,
		OriginalArtist:   s.OriginalArtist,
		ArtURL:           s.Arts.String,
		PerformanceCount: perfCount,
	}
}
