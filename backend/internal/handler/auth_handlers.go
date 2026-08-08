package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

// ctxKey はリクエストコンテキストのキー型。
type ctxKey string

const userCtxKey ctxKey = "currentUser"

// currentUser はコンテキストから現在のユーザーを取り出す（未ログインなら nil）。
func currentUser(req *http.Request) *models.User {
	if u, ok := req.Context().Value(userCtxKey).(*models.User); ok {
		return u
	}
	return nil
}

// userHasPermission は現在のユーザーが権限を持つかを返す（未ログインなら false）。
// エンドポイントの可否は authorize が先に判定するので、ここは「同じ端点でも
// 権限によって返す内容を変える」ための判定に使う（例：非表示チャンネルを一覧に含めるか）。
func userHasPermission(req *http.Request, perm string) bool {
	user := currentUser(req)
	if user == nil {
		return false
	}
	return auth.HasPermission(user.Permissions, perm)
}

// clientHint は匿名リクエストの同一性の手がかりを返す（IP の SHA-256 先頭16桁）。
// 生 IP は保存しないが、同じ相手からの連投は数えられる、という妥協点。
// リバースプロキシ下では X-Forwarded-For の先頭を使う。
func clientHint(req *http.Request) string {
	ip := req.RemoteAddr
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ip = strings.TrimSpace(strings.Split(xff, ",")[0])
	} else if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])[:16]
}

// bearerToken は Authorization ヘッダーから Bearer トークンを取り出す。
func bearerToken(req *http.Request) string {
	h := req.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

// ========== Auth Handlers ==========

func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.Username == "" || body.Password == "" {
		respondError(w, http.StatusBadRequest, "ユーザー名とパスワードは必須です")
		return
	}

	// 総当たりと bcrypt による CPU 消費を止める。判定はパスワード検証より前に置く
	// ——ロック中の相手にハッシュ計算をさせない（それ自体が攻撃の目的になり得る）。
	// 鍵は生 IP ではなく clientHint（IP のハッシュ）で、匿名投稿の数え方と揃える。
	ipKey := clientHint(req)
	userKey := strings.ToLower(strings.TrimSpace(body.Username))
	if ok, remain := r.loginLimiter.allow(ipKey, userKey); !ok {
		respondError(w, http.StatusTooManyRequests,
			fmt.Sprintf("試行が多すぎます。%d 分ほど待ってからやり直してください", int(remain.Minutes())+1))
		return
	}

	token, user, err := r.authService.Login(body.Username, body.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrUserInactive) {
			r.loginLimiter.recordFailure(ipKey, userKey)
			respondError(w, http.StatusUnauthorized, err.Error())
			return
		}
		// サーバー側の不具合は利用者の失敗ではないので数えない。
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	r.loginLimiter.recordSuccess(ipKey, userKey)
	logger.Infof("user %q logged in", user.Username)
	respondJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  user,
	})
}

func (r *Router) handleLogout(w http.ResponseWriter, req *http.Request) {
	if err := r.authService.Logout(bearerToken(req)); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "ログアウトしました"})
}

// handleMe は現在のユーザー（権限込み）を返す。認可 middleware で認証済みが保証される。
func (r *Router) handleMe(w http.ResponseWriter, req *http.Request) {
	user := currentUser(req)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "認証が必要です")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

// handleListPermissions は割り当て可能な権限カタログを返す（ロール編集 UI 用）。
func (r *Router) handleListPermissions(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, auth.AllPermissions())
}

// ========== User Management Handlers ==========

func (r *Router) handleListUsers(w http.ResponseWriter, req *http.Request) {
	users, err := r.authService.ListUsers()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, users)
}

func (r *Router) handleCreateUser(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		RoleID      string `json:"role_id"`
		IsActive    *bool  `json:"is_active"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	roleID, err := uuid.Parse(body.RoleID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なロールID")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	user, err := r.authService.CreateUser(body.Username, body.DisplayName, body.Password, roleID, isActive)
	if err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	logger.Infof("user %q created", user.Username)
	respondJSON(w, http.StatusCreated, user)
}

func (r *Router) handleUpdateUser(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なユーザーID")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
		RoleID      string `json:"role_id"`
		IsActive    *bool  `json:"is_active"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	roleID, err := uuid.Parse(body.RoleID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なロールID")
		return
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	// 自分自身を無効化してロックアウトするのを防ぐ
	if me := currentUser(req); me != nil && me.ID == id && !isActive {
		respondError(w, http.StatusBadRequest, "自分自身を無効化することはできません")
		return
	}

	user, err := r.authService.UpdateUser(id, body.DisplayName, roleID, isActive)
	if err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, user)
}

func (r *Router) handleChangeUserPassword(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なユーザーID")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if err := r.authService.ChangePassword(id, body.Password); err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "パスワードを変更しました"})
}

func (r *Router) handleDeleteUser(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なユーザーID")
		return
	}
	if me := currentUser(req); me != nil && me.ID == id {
		respondError(w, http.StatusBadRequest, "自分自身を削除することはできません")
		return
	}
	if err := r.authService.DeleteUser(id); err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "ユーザーを削除しました"})
}

// ========== Role Management Handlers ==========

func (r *Router) handleListRoles(w http.ResponseWriter, req *http.Request) {
	roles, err := r.authService.ListRoles()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, roles)
}

func (r *Router) handleCreateRole(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	role, err := r.authService.CreateRole(body.Name, body.Description, body.Permissions)
	if err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, role)
}

func (r *Router) handleUpdateRole(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なロールID")
		return
	}
	var body struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	role, err := r.authService.UpdateRole(id, body.Description, body.Permissions)
	if err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, role)
}

func (r *Router) handleDeleteRole(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なロールID")
		return
	}
	if err := r.authService.DeleteRole(id); err != nil {
		respondError(w, userErrStatus(err), err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "ロールを削除しました"})
}

// userErrStatus はサービス層のエラーを HTTP ステータスに対応づける。
func userErrStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrUsernameTaken), errors.Is(err, service.ErrRoleNameTaken),
		errors.Is(err, service.ErrRoleInUse), errors.Is(err, service.ErrSystemRole),
		errors.Is(err, service.ErrWeakPassword):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrUserNotFound), errors.Is(err, service.ErrRoleNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// withUser はリクエストコンテキストにユーザーを詰めた新しい *http.Request を返す。
func withUser(req *http.Request, user *models.User) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), userCtxKey, user))
}
