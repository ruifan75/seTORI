package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivateKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-key")
	if err := os.WriteFile(path, []byte("  a-secure-new-key-value  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPrivateKeyFile(path)
	if err != nil {
		t.Fatalf("readPrivateKeyFile: %v", err)
	}
	if got != "a-secure-new-key-value" {
		t.Fatalf("key = %q", got)
	}
}

func TestReadPrivateKeyFileRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-key")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateKeyFile(path); err == nil {
		t.Fatal("mode 0644 should be rejected")
	}
}
