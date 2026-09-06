package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
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
	candidates, err := r.songMatchService.ListOpenMergeCandidates(limit, viewerAccess(req))
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
	candidates, err := r.songMatchService.FindOpenMergeCandidatesForSong(songID, viewerAccess(req))
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
		resp := dto.MergeCandidateResponse{
			ID:           c.ID.String(),
			Score:        c.Score,
			Reason:       c.Reason,
			Origin:       c.Origin,
			NewSong:      toMergeCandidateSong(c.NewSong, c.PerfCountNew, c.ItunesNew, c.Verdict.RoleNew),
			ExistingSong: toMergeCandidateSong(c.ExistingSong, c.PerfCountOld, c.ItunesOld, c.Verdict.RoleExisting),
		}
		if c.Verdict.At.Valid {
			resp.Verdict = &dto.MergeVerdictResponse{
				SameComposition: c.Verdict.SameComposition,
				SameArrangement: c.Verdict.SameArrangement,
				Recommendation:  c.Verdict.Recommendation,
				Note:            c.Verdict.Note,
				Source:          c.Verdict.Source,
				Judged:          true,
			}
		}
		out = append(out, resp)
	}
	return out
}

func toMergeCandidateSong(s models.Song, perfCount int, itunesIDs []int64, role string) dto.MergeCandidateSong {
	return dto.MergeCandidateSong{
		ID:               s.ID.String(),
		Name:             s.Name,
		OriginalArtist:   s.OriginalArtist,
		ArtURL:           s.Arts.String,
		PerformanceCount: perfCount,
		ItunesIDs:        itunesIDs,
		Role:             role,
	}
}

// handleScanDuplicates は既存データを走査して同名の組を候補に積む（content:edit）。
// 取り込み時の検出は「これから作る曲」しか見ないので、導入前からあった重複は
// これを走らせないと誰にも気づかれない。
func (r *Router) handleScanDuplicates(w http.ResponseWriter, req *http.Request) {
	// ① 曲名キーが同じ組。確実で無料
	added, err := r.songMatchService.ScanDuplicates()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// ② キーが違う組（邦題と原題、誤字、ローマ字）。①では原理的に見つからない。
	//    AI 呼び出しが失敗しても①の結果は返す（走査全体を落とさない）。
	byAI, aiErr := r.songMatchService.ScanDuplicatesWithAI(r.aiService)
	if aiErr != nil {
		logger.Warnf("[dup] AI 全件走査に失敗しました: %v", aiErr)
	}

	msg := fmt.Sprintf("%d 件の重複候補を追加しました（曲名キー %d / AI %d）", added+byAI, added, byAI)
	if aiErr != nil {
		msg = fmt.Sprintf("%d 件の重複候補を追加しました（曲名キーのみ。AI 走査は失敗: %v）", added, aiErr)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"added":    added + byAI,
		"by_key":   added,
		"by_ai":    byAI,
		"ai_error": aiErr != nil,
		"message":  msg,
	})
}

// handleAdjudicateDuplicates は未判定の候補について AI の見立てを取る（content:edit）。
// **統合は実行しない。**同名の組には「統合すべき」「編曲違いで分けるべき」
// 「そもそも別の曲」が混在し、その線引きは編集方針なので人が決める。
func (r *Router) handleAdjudicateDuplicates(w http.ResponseWriter, req *http.Request) {
	saved, err := r.songMatchService.AdjudicateDuplicates(r.aiService, 30)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"judged":  saved,
		"message": fmt.Sprintf("%d 件を判定しました", saved),
	})
}
