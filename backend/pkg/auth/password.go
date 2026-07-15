// Package auth はパスワードハッシュ・セッショントークン生成・権限判定を提供する。
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// pbkdf2 パラメータ。OWASP 推奨（PBKDF2-HMAC-SHA256）に沿う。
const (
	pbkdf2Iterations = 600000
	pbkdf2SaltLen    = 16
	pbkdf2KeyLen     = 32
)

// ErrInvalidHash は保存済みハッシュの形式が不正なとき返す。
var ErrInvalidHash = errors.New("invalid password hash format")

// HashPassword は平文パスワードを "pbkdf2_sha256$iter$saltB64$hashB64" 形式でハッシュ化する。
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword は平文と保存済みハッシュを定数時間で照合する。
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false, ErrInvalidHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, ErrInvalidHash
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, fmt.Errorf("derive key: %w", err)
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// GenerateToken は URL セーフなランダムセッショントークン（32 バイト）を返す。
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken は DB 保存用にトークンの SHA-256 を 16 進文字列で返す。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum)
}
