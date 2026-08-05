package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/pkg/secrets"
)

// GET /api/settings/integrations — 外部サービス連携の設定状態（要 users:manage）
//
// 値そのものは決して返さない。設定済みか・末尾4桁・どこ由来かだけを返す。
func (r *Router) handleGetIntegrationSettings(w http.ResponseWriter, _ *http.Request) {
	resp, err := r.settingsService.Describe()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}

// PUT /api/settings/integrations — 設定の更新（要 users:manage）
//
// 機密は空文字なら「変更なし」。消したい場合は clear に項目名を入れる。
func (r *Router) handleUpdateIntegrationSettings(w http.ResponseWriter, req *http.Request) {
	var body dto.UpdateIntegrationSettingsRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}

	if err := r.settingsService.Update(&body); err != nil {
		if errors.Is(err, secrets.ErrNoKey) {
			respondError(w, http.StatusPreconditionFailed,
				"機密を保存するには SETTINGS_ENCRYPTION_KEY を設定してください（バックアップ流出時に鍵が無ければ復号できないようにするため）")
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := r.settingsService.Describe()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}
