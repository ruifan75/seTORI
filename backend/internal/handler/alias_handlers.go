package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ruifan75/setori/internal/dto"
)

// 照合の学習層の管理。
//
// AI が「同一人物」と判定した別名義は全楽曲の照合に効くので、
// 人が中身を見て取り消せる必要がある。ここはそのための窓口。

// handleListArtistAliases は別名義グループの一覧を返す（content:edit）。
func (r *Router) handleListArtistAliases(w http.ResponseWriter, req *http.Request) {
	groups, err := r.songMatchService.ListArtistAliasGroups()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]dto.ArtistAliasGroupResponse, 0, len(groups))
	for _, g := range groups {
		members := make([]dto.ArtistAliasMemberResponse, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, dto.ArtistAliasMemberResponse{
				NameKey:     m.NameKey,
				DisplayName: m.DisplayName,
				Source:      m.Source,
				Note:        m.Note,
			})
		}
		out = append(out, dto.ArtistAliasGroupResponse{GroupID: g.GroupID.String(), Members: members})
	}
	respondJSON(w, http.StatusOK, map[string]any{"groups": out})
}

// handleLinkArtistAliases は 2 つの表記を同一人物として登録する（content:edit）。
func (r *Router) handleLinkArtistAliases(w http.ResponseWriter, req *http.Request) {
	var body struct {
		NameA string `json:"name_a"`
		NameB string `json:"name_b"`
		Note  string `json:"note"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if err := r.songMatchService.LinkArtistAliases(body.NameA, body.NameB, "manual", body.Note); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "同一人物として登録しました"})
}

// handleUnlinkArtistAlias はグループから 1 名を外す（content:edit）。
// AI の誤判定を取り消す経路。解除は「別人である」という人の判定として記録され、
// 次の解析で AI が同じ組を再び結びつけることはない。
func (r *Router) handleUnlinkArtistAlias(w http.ResponseWriter, req *http.Request) {
	nameKey := req.PathValue("nameKey")
	if nameKey == "" {
		respondError(w, http.StatusBadRequest, "名前が指定されていません")
		return
	}
	if err := r.songMatchService.UnlinkArtistAlias(nameKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "別名義の登録が見つかりません")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "別名義の登録を解除しました"})
}

// handleListSongAliases は学習済みの楽曲別表記を返す（content:edit）。
func (r *Router) handleListSongAliases(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	rows, err := r.songMatchService.ListSongAliases(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]dto.SongAliasResponse, 0, len(rows))
	for _, x := range rows {
		out = append(out, dto.SongAliasResponse{
			NameKey:    x.NameKey,
			ArtistKey:  x.ArtistKey,
			Source:     x.Source,
			SongID:     x.Song.ID.String(),
			SongName:   x.Song.Name,
			SongArtist: x.Song.OriginalArtist,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"aliases": out})
}

// handleDeleteSongAlias は誤って学習した対応を取り消す（content:edit）。
func (r *Router) handleDeleteSongAlias(w http.ResponseWriter, req *http.Request) {
	nameKey := req.URL.Query().Get("name_key")
	artistKey := req.URL.Query().Get("artist_key")
	if nameKey == "" {
		respondError(w, http.StatusBadRequest, "name_key が必要です")
		return
	}
	if err := r.songMatchService.DeleteSongAlias(nameKey, artistKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "学習した対応が見つかりません")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "学習した対応を取り消しました"})
}
