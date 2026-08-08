package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/service"
)

func oauthErrStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrProviderNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrProviderNotEnabled):
		return http.StatusServiceUnavailable
	case errors.Is(err, service.ErrOAuthState), errors.Is(err, service.ErrOAuthCodeExpired):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrIdentityTaken), errors.Is(err, service.ErrEmailTaken),
		errors.Is(err, service.ErrLastLoginMethod):
		return http.StatusConflict
	case errors.Is(err, service.ErrUserInactive):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

// GET /api/auth/oauth/providers — 利用できる連携先（ログイン画面のボタン出し分け）
func (r *Router) handleListOAuthProviders(w http.ResponseWriter, _ *http.Request) {
	providers := r.oauthService.EnabledProviders()
	if providers == nil {
		providers = []string{}
	}
	respondJSON(w, http.StatusOK, map[string][]string{"providers": providers})
}

// POST /api/auth/oauth/{provider}/start — 認可画面の URL を返す
//
// ログイン中に呼ぶと「既存アカウントへの連携追加」として扱う。その判定は
// currentUser(req) ＝ Authorization ヘッダー頼りなので、**リダイレクトではなく
// URL を JSON で返す**必要がある。ブラウザの全ページ遷移ではヘッダーを付けられず、
// 連携追加のつもりが必ず新規ログインになってしまうため（フロントは受け取った
// URL へ自分で遷移する）。
func (r *Router) handleOAuthStart(w http.ResponseWriter, req *http.Request) {
	provider := req.PathValue("provider")

	var linkUserID *uuid.UUID
	if user := currentUser(req); user != nil {
		id := user.ID
		linkUserID = &id
	}

	authURL, err := r.oauthService.Start(provider, linkUserID)
	if err != nil {
		respondError(w, oauthErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

// GET /api/auth/oauth/{provider}/callback — プロバイダーからの戻り先
//
// 常にフロントエンドへリダイレクトして結果を伝える。成功時に渡すのは
// セッショントークンそのものではなく1回限り・60秒で失効する引き換えコード
// （URL やブラウザ履歴に長命な資格情報を残さないため）。
func (r *Router) handleOAuthCallback(w http.ResponseWriter, req *http.Request) {
	provider := req.PathValue("provider")
	q := req.URL.Query()

	// 利用者が同意画面で拒否した場合など
	if providerErr := q.Get("error"); providerErr != "" {
		r.redirectToFrontend(w, req, "", "認証がキャンセルされました")
		return
	}

	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		r.redirectToFrontend(w, req, "", "認証情報が不足しています")
		return
	}

	exchangeCode, err := r.oauthService.Callback(req.Context(), provider, code, state)
	if err != nil {
		// 失敗理由は利用者向けの文言のみ渡す（内部エラーはサーバー側に残す）
		logger.Warnf("[oauth] callback failed (%s): %v", provider, err)
		message := "ログインに失敗しました"
		if oauthErrStatus(err) != http.StatusInternalServerError {
			message = err.Error()
		}
		r.redirectToFrontend(w, req, "", message)
		return
	}
	r.redirectToFrontend(w, req, exchangeCode, "")
}

// redirectToFrontend は結果を持たせてフロントエンドの受け口へ戻す。
func (r *Router) redirectToFrontend(w http.ResponseWriter, req *http.Request, code, errMessage string) {
	base := strings.TrimSuffix(r.cfg.FrontendBaseURL, "/")
	q := url.Values{}
	if code != "" {
		q.Set("code", code)
	}
	if errMessage != "" {
		q.Set("error", errMessage)
	}
	http.Redirect(w, req, fmt.Sprintf("%s/login/oauth?%s", base, q.Encode()), http.StatusFound)
}

// POST /api/auth/oauth/exchange — 引き換えコードをセッショントークンに替える
func (r *Router) handleOAuthExchange(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が不正です")
		return
	}
	token, err := r.oauthService.RedeemExchangeCode(body.Code)
	if err != nil {
		respondError(w, oauthErrStatus(err), err.Error())
		return
	}
	user, err := r.authService.Authenticate(token)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "セッションの取得に失敗しました")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

// GET /api/auth/oauth/identities — 自分の連携一覧（要ログイン）
func (r *Router) handleListMyIdentities(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	identities, err := r.oauthService.ListIdentities(userID)
	if err != nil {
		respondError(w, oauthErrStatus(err), err.Error())
		return
	}
	if identities == nil {
		identities = []models.OAuthIdentity{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"identities": identities})
}

// DELETE /api/auth/oauth/{provider} — 連携解除（要ログイン）
func (r *Router) handleUnlinkOAuth(w http.ResponseWriter, req *http.Request) {
	userID, ok := r.requireLogin(w, req)
	if !ok {
		return
	}
	if err := r.oauthService.Unlink(userID, req.PathValue("provider")); err != nil {
		respondError(w, oauthErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "連携を解除しました"})
}
