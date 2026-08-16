package secretrotation

import (
	"encoding/json"
	"testing"

	"github.com/ruifan75/setori/pkg/secrets"
)

func mustCipher(t *testing.T, key string) *secrets.Cipher {
	t.Helper()
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return cipher
}

func TestRotateJSONSecretFields(t *testing.T) {
	oldCipher := mustCipher(t, "old-key-for-testing")
	newCipher := mustCipher(t, "new-key-for-testing")
	oldEncrypted, err := oldCipher.Encrypt("first-secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"encrypted": oldEncrypted,
		"legacy":    "second-secret",
		"empty":     "",
		"plain":     "leave-me-alone",
		"unknown":   map[string]any{"nested": true},
	})
	if err != nil {
		t.Fatal(err)
	}

	rotated, count, err := rotateJSONSecretFields(raw, []string{"encrypted", "legacy", "empty"}, oldCipher, newCipher)
	if err != nil {
		t.Fatalf("rotateJSONSecretFields: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(rotated, &got); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{"encrypted": "first-secret", "legacy": "second-secret"} {
		var stored string
		if err := json.Unmarshal(got[field], &stored); err != nil {
			t.Fatal(err)
		}
		if !secrets.IsEncrypted(stored) {
			t.Fatalf("%s was not encrypted", field)
		}
		plain, err := newCipher.Decrypt(stored)
		if err != nil || plain != want {
			t.Fatalf("%s decrypted to %q, %v; want %q", field, plain, err, want)
		}
	}
	if string(got["plain"]) != `"leave-me-alone"` {
		t.Fatalf("plain field changed: %s", got["plain"])
	}
	if string(got["unknown"]) != `{"nested":true}` {
		t.Fatalf("unknown field changed: %s", got["unknown"])
	}
}

func TestRotateJSONSecretFieldsRejectsWrongOldKey(t *testing.T) {
	actualOld := mustCipher(t, "actual-old-key")
	wrongOld := mustCipher(t, "wrong-old-key")
	newCipher := mustCipher(t, "new-key")
	stored, err := actualOld.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"secret": stored})

	if _, _, err := rotateJSONSecretFields(raw, []string{"secret"}, wrongOld, newCipher); err == nil {
		t.Fatal("wrong old key should fail")
	}
}
