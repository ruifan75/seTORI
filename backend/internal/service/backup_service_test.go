package service

import (
	"errors"
	"testing"

	"github.com/ruifan75/setori/pkg/secrets"
)

func TestGDriveTokenEncryptionRoundTrip(t *testing.T) {
	cipher, err := secrets.NewCipher("test-settings-encryption-key")
	if err != nil {
		t.Fatal(err)
	}
	original := gdriveToken{RefreshToken: "refresh-token", Email: "user@example.com"}

	stored, err := encryptGDriveToken(cipher, original)
	if err != nil {
		t.Fatalf("encryptGDriveToken: %v", err)
	}
	if stored.RefreshToken == original.RefreshToken || !secrets.IsEncrypted(stored.RefreshToken) {
		t.Fatal("refresh token was not encrypted")
	}
	if stored.Email != original.Email {
		t.Fatalf("email changed to %q", stored.Email)
	}

	got, err := decryptGDriveToken(cipher, stored)
	if err != nil {
		t.Fatalf("decryptGDriveToken: %v", err)
	}
	if got != original {
		t.Fatalf("round trip = %#v, want %#v", got, original)
	}
}

func TestGDriveTokenReadsLegacyPlaintext(t *testing.T) {
	cipher, err := secrets.NewCipher("test-settings-encryption-key")
	if err != nil {
		t.Fatal(err)
	}
	legacy := gdriveToken{RefreshToken: "legacy-plaintext", Email: "user@example.com"}
	got, err := decryptGDriveToken(cipher, legacy)
	if err != nil {
		t.Fatalf("decryptGDriveToken: %v", err)
	}
	if got != legacy {
		t.Fatalf("legacy token changed: %#v", got)
	}
}

func TestGDriveTokenRefusesSaveWithoutKey(t *testing.T) {
	if _, err := encryptGDriveToken(nil, gdriveToken{RefreshToken: "secret"}); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("error = %v, want ErrNoKey", err)
	}
}
