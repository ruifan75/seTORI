package repository

import (
	"os"
	"testing"
)

// readSourceForTest は実装のソースを読む（SQL のように、壊れてもコンパイルが
// 通る箇所を固定するため）。
func readSourceForTest(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
