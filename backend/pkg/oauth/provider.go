// Package oauth は外部アカウント（Google / X / Discord …）での認証を扱う。
// Provider を実装すれば対応先を増やせる。標準ライブラリのみで完結させている
// （pkg/gdrive と同じ方針）。
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// UserInfo は各プロバイダーから取得した利用者情報を共通形へ正規化したもの。
type UserInfo struct {
	// ProviderUserID はプロバイダー内で不変の一意 ID（Google なら sub）。
	// メールアドレスは変わりうるので、紐付けのキーには必ずこちらを使う。
	ProviderUserID string
	Email          string
	// EmailVerified はプロバイダー側でメールが確認済みか。
	// 既存アカウントへの自動紐付けを許すかの判断に使う。
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
}

// Provider は1つの外部アカウント連携先を表す。
type Provider interface {
	// Name は provider 列に保存する識別子（"google" / "x" / "discord"）。
	Name() string
	// AuthCodeURL は利用者を送る認可エンドポイントの URL を組み立てる。
	AuthCodeURL(state, redirectURI string) string
	// Exchange は認可コードをアクセストークンに交換し、利用者情報を取得する。
	Exchange(ctx context.Context, code, redirectURI string) (*UserInfo, error)
	// Configured は必要な資格情報が設定済みかを返す。未設定なら UI に出さない。
	Configured() bool
}

// GenerateState は CSRF 対策の state 値を作る。
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
