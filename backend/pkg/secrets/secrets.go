// Package secrets は設定値として DB に保存する機密（API キーなど）の暗号化を担う。
//
// なぜ暗号化するか：バックアップは pg_dump で DB 全体を吸い出し Google Drive へ
// 自動アップロードされる。機密を平文で DB に置くと、バックアップ1つの流出が
// 全 API キーの流出になる。鍵は DB に置けない（一緒に流出するため）ので環境変数から取る。
// これで .env に残る秘密は「この鍵1つだけ」になり、鍵の無いバックアップは無価値になる。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNoKey は暗号鍵が未設定のときに返る。機密の保存を拒否するために使う。
var ErrNoKey = errors.New("SETTINGS_ENCRYPTION_KEY が設定されていません")

// encryptedPrefix は暗号文であることの目印。
// 平文のまま保存されていた古い値と区別し、段階的な移行を可能にする。
const encryptedPrefix = "enc:v1:"

// Cipher は設定値の暗号化・復号を行う。鍵が空の場合は暗号化を提供しない。
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher は鍵文字列から Cipher を作る。
// 鍵は任意長の文字列でよく、SHA-256 で 32 バイトへ伸長する
// （利用者に base64 32 バイトを用意させるより運用が楽なため）。
// 空文字なら Enabled() == false の Cipher を返す（エラーにはしない）。
func NewCipher(key string) (*Cipher, error) {
	if strings.TrimSpace(key) == "" {
		return &Cipher{}, nil
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Enabled は暗号化が使えるか（鍵が設定されているか）を返す。
func (c *Cipher) Enabled() bool { return c.aead != nil }

// Encrypt は平文を "enc:v1:<base64(nonce||ciphertext)>" 形式へ変換する。
// 空文字は空文字のまま返す（未設定を暗号化しても意味がないため）。
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if !c.Enabled() {
		return "", ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt は Encrypt の逆。目印の無い値は平文とみなしてそのまま返す
// （暗号化を導入する前に保存された値を読めるようにするため）。
func (c *Cipher) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, encryptedPrefix) {
		return stored, nil // 平文（移行前の値）
	}
	if !c.Enabled() {
		// 暗号文があるのに鍵が無い＝鍵を失った状態。黙って空を返さず気付けるようにする。
		return "", ErrNoKey
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("decode secret: ciphertext too short")
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		// 鍵が変わった場合もここに来る
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

// IsEncrypted は保存値が暗号化済みかを返す。
func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, encryptedPrefix)
}

// Hint は UI 表示用の末尾4文字のヒントを返す（値そのものは返さない）。
func Hint(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	r := []rune(plaintext)
	if len(r) <= 4 {
		return strings.Repeat("•", len(r))
	}
	return "…" + string(r[len(r)-4:])
}
