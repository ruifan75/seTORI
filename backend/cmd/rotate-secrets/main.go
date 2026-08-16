package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ruifan75/setori/internal/database"
	"github.com/ruifan75/setori/internal/secretrotation"
	"github.com/ruifan75/setori/pkg/secrets"
)

func main() {
	apply := flag.Bool("apply", false, "commit the rotation; without this flag only validate")
	newKeyFile := flag.String("new-key-file", "", "path to a mode-0600 file containing the new key")
	flag.Parse()

	if strings.TrimSpace(*newKeyFile) == "" {
		fatal("--new-key-file is required")
	}
	newKey, err := readPrivateKeyFile(*newKeyFile)
	if err != nil {
		fatal("read new key: %v", err)
	}
	oldKey := strings.TrimSpace(os.Getenv("SETTINGS_ENCRYPTION_KEY"))
	if oldKey == "" {
		fatal("SETTINGS_ENCRYPTION_KEY is missing")
	}
	if len([]byte(newKey)) < 32 {
		fatal("new key must contain at least 32 bytes")
	}
	if oldKey == newKey {
		fatal("new key must differ from the old key")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fatal("DATABASE_URL is missing")
	}

	oldCipher, err := secrets.NewCipher(oldKey)
	if err != nil {
		fatal("initialize old cipher: %v", err)
	}
	newCipher, err := secrets.NewCipher(newKey)
	if err != nil {
		fatal("initialize new cipher: %v", err)
	}
	db, err := database.Connect(databaseURL)
	if err != nil {
		fatal("connect database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result, err := secretrotation.Rotate(ctx, db, oldCipher, newCipher, *apply)
	if err != nil {
		fatal("secret rotation failed: %v", err)
	}
	mode := "validated"
	if *apply {
		mode = "rotated"
	}
	fmt.Printf("secrets %s: integrations=%d gdrive_tokens=%d ai_provider_keys=%d\n",
		mode, result.IntegrationSecrets, result.GDriveTokens, result.AIProviderKeys)
}

func readPrivateKeyFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s permissions are %04o; expected 0600 or stricter", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return key, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
