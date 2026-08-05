package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher("test-key")
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("鍵を渡したのに Enabled() が false")
	}

	for _, plaintext := range []string{"abc", "sk-1234567890", "日本語のキー", strings.Repeat("x", 500)} {
		enc, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if strings.Contains(enc, plaintext) {
			t.Errorf("暗号文に平文が含まれている: %q", enc)
		}
		if !IsEncrypted(enc) {
			t.Errorf("IsEncrypted が false: %q", enc)
		}
		got, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Errorf("往復で不一致: got %q want %q", got, plaintext)
		}
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	c, _ := NewCipher("test-key")
	a, _ := c.Encrypt("same-value")
	b, _ := c.Encrypt("same-value")
	if a == b {
		t.Error("同じ平文が同じ暗号文になっている（nonce が使われていない）")
	}
}

func TestEmptyValueStaysEmpty(t *testing.T) {
	c, _ := NewCipher("test-key")
	enc, err := c.Encrypt("")
	if err != nil || enc != "" {
		t.Errorf("空文字は空文字のままであるべき: got %q, err %v", enc, err)
	}
	dec, err := c.Decrypt("")
	if err != nil || dec != "" {
		t.Errorf("空文字の復号は空文字であるべき: got %q, err %v", dec, err)
	}
}

// 暗号化を導入する前に平文で保存されていた値が読めること（段階的移行のため）
func TestPlaintextPassesThrough(t *testing.T) {
	c, _ := NewCipher("test-key")
	got, err := c.Decrypt("legacy-plain-value")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "legacy-plain-value" {
		t.Errorf("平文がそのまま返らない: %q", got)
	}
}

func TestNoKeyRefusesToEncrypt(t *testing.T) {
	c, err := NewCipher("")
	if err != nil {
		t.Fatalf("NewCipher(\"\"): %v", err)
	}
	if c.Enabled() {
		t.Fatal("鍵が無いのに Enabled() が true")
	}
	if _, err := c.Encrypt("secret"); !errors.Is(err, ErrNoKey) {
		t.Errorf("鍵無しの Encrypt は ErrNoKey を返すべき: %v", err)
	}
	// 平文はそのまま読める（鍵が無くても既存運用を壊さない）
	if got, err := c.Decrypt("plain"); err != nil || got != "plain" {
		t.Errorf("鍵無しでも平文は読めるべき: %q %v", got, err)
	}
}

// 鍵を失った／取り違えた場合に、黙って空を返さずエラーになること
func TestNoKeyFailsOnCiphertext(t *testing.T) {
	withKey, _ := NewCipher("test-key")
	enc, _ := withKey.Encrypt("secret")

	noKey, _ := NewCipher("")
	if _, err := noKey.Decrypt(enc); !errors.Is(err, ErrNoKey) {
		t.Errorf("鍵無しで暗号文を復号したら ErrNoKey を返すべき: %v", err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	a, _ := NewCipher("key-a")
	b, _ := NewCipher("key-b")
	enc, _ := a.Encrypt("secret")
	if got, err := b.Decrypt(enc); err == nil {
		t.Errorf("異なる鍵で復号が成功してしまった: %q", got)
	}
}

func TestHintNeverLeaksWholeValue(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"ab":           "••",
		"abcd":         "••••",
		"abcdefghijkl": "…ijkl",
	}
	for in, want := range cases {
		if got := Hint(in); got != want {
			t.Errorf("Hint(%q) = %q, want %q", in, got, want)
		}
	}
}
