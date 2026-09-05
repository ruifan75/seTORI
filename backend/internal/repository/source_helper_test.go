package repository

import (
	"os"
	"strings"
	"testing"
)

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// section は from で始まり、次に until が現れるまでを返す。
func section(t *testing.T, src, from, until string) string {
	t.Helper()
	i := strings.Index(src, from)
	if i < 0 {
		t.Fatalf("%q が見つからない（関数名が変わった？）", from)
	}
	rest := src[i+len(from):]
	if j := strings.Index(rest, until); j >= 0 {
		return rest[:j]
	}
	return rest
}
