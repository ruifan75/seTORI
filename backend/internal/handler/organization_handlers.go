package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/service"
)

// ========== Organization Handlers ==========
//
// 事務所は key（取り込み時の生の値）と display_name（画面に出す名前）を分けて持つ。
// 一覧の取得は公開（チャンネル一覧の見出しと、編集画面の選択肢に要る）。
// 追加・更新・削除は content:edit（既定の書き込み規則どおり）。

func (r *Router) handleListOrganizations(w http.ResponseWriter, req *http.Request) {
	result, err := r.orgService.GetAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateOrganization(w http.ResponseWriter, req *http.Request) {
	var body dto.CreateOrganizationRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.orgService.Create(&body)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOrganizationNameRequired),
			errors.Is(err, service.ErrOrganizationKeyRequired):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrOrganizationExists):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	logger.Infof("organization created: %s (%s)", result.Key, result.DisplayName)
	respondJSON(w, http.StatusCreated, result)
}

func (r *Router) handleUpdateOrganization(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		respondError(w, http.StatusBadRequest, "事務所のキーは必須です")
		return
	}

	var body dto.UpdateOrganizationRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.orgService.Update(key, &body)
	if err != nil {
		if errors.Is(err, service.ErrOrganizationNameRequired) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "事務所が見つかりません")
		return
	}

	logger.Infof("organization updated: %s (%s)", result.Key, result.DisplayName)
	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleDeleteOrganization(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		respondError(w, http.StatusBadRequest, "事務所のキーは必須です")
		return
	}

	deleted, err := r.orgService.Delete(key)
	if err != nil {
		// 所属チャンネルが残っている削除は 409。まずチャンネルを移すか外してもらう
		// （黙って所属を消すと、どのチャンネルが影響を受けたか分からなくなる）。
		if errors.Is(err, service.ErrOrganizationInUse) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		respondError(w, http.StatusNotFound, "事務所が見つかりません")
		return
	}

	logger.Infof("organization deleted: %s", key)
	respondJSON(w, http.StatusOK, map[string]string{"key": key})
}
