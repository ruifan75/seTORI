package repository

import (
	"os"
	"strings"
	"testing"
)

func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// funcBody は関数 1 つぶんの本文を返す。**関数の外に当たる置換・検査を防ぐため**
// ── 件数と一覧は同じ WHERE の文言を持つので、ファイル全体を対象にすると
// 「どちらに当たったか」が分からない。
func funcBody(t *testing.T, src, sig string) string {
	t.Helper()
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("関数が見つからない: %s", sig)
	}
	j := strings.Index(src[i:], "\n}\n")
	if j < 0 {
		t.Fatalf("関数の終わりが見つからない: %s", sig)
	}
	return src[i : i+j]
}

// splitAtStreamList は件数のクエリと一覧のクエリの境目で分ける。
// 一覧は必ず streamListQuery を通る（列を継ぎ足す口を残さないため）。
func splitAtStreamList(t *testing.T, body, fn string) (countPart, listPart string) {
	t.Helper()
	k := strings.Index(body, "streamListQuery(")
	if k < 0 {
		t.Fatalf("%s: streamListQuery を通っていない", fn)
	}
	return body[:k], body[k:]
}
