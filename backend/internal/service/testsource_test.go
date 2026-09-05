package service

import (
	"os"
	"testing"
)

// readFileForTest は実装のソースを読む（SQL や条件式のように、
// 壊れてもコンパイルが通る箇所を固定するため）。
func readFileForTest(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
