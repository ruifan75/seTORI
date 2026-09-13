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

// ifElseBlocks は `if <cond> {` … `} else {` … `}` を 3 つに割る。
//
// **分岐ごとに見ないと、条件を「もう片方の分岐へ移す」改変を見逃す。**
// 実際、分岐をまとめて見ていたときは、通常表示の一覧から条件を外して
// includeHidden 側へ移しても検査が通った ── 件数は濾しているのに一覧は
// 素通りという、いちばん起きてほしくない状態が固定できていなかった。
func ifElseBlocks(t *testing.T, body, cond string, nth int) (ifPart, elsePart string) {
	t.Helper()
	head := "\tif " + cond + " {\n"
	pos := 0
	for n := 0; n <= nth; n++ {
		i := strings.Index(body[pos:], head)
		if i < 0 {
			t.Fatalf("if %s の %d 個目が見つからない", cond, nth+1)
		}
		pos += i + len(head)
	}
	rest := body[pos:]
	mid := strings.Index(rest, "\n\t} else {\n")
	if mid < 0 {
		t.Fatalf("else が見つからない: if %s (%d 個目)", cond, nth+1)
	}
	after := rest[mid+len("\n\t} else {\n"):]
	end := strings.Index(after, "\n\t}\n")
	if end < 0 {
		t.Fatalf("分岐の終わりが見つからない: if %s (%d 個目)", cond, nth+1)
	}
	return rest[:mid], after[:end]
}

// assertAndedVisibleChannel は、断片の中の VisibleChannelExpr が
// **AND で繋がれている**ことまで確かめる。
//
// **呼び出しが在るだけでは足りない。** `AND` を `OR` に変えると一覧だけが広がり、
// 件数は絞られたままなので元の症状（件数と一覧の食い違い）が再発する。それでも
// 「分岐に呼び出しが在る」という検査は通ってしまう ── 実測で通った。
//
// Go の文字列連結（`" + ` や "`+") を落としてから直前の語を見る。
func assertAndedVisibleChannel(t *testing.T, name, snippet string) {
	t.Helper()
	const call = "VisibleChannelExpr("
	stripConcat := strings.NewReplacer("\"", "", "`", "", "+", "", "\n", " ", "\t", " ")

	found := 0
	for i := 0; ; {
		k := strings.Index(snippet[i:], call)
		if k < 0 {
			break
		}
		pos := i + k
		before := strings.TrimSpace(stripConcat.Replace(snippet[:pos]))
		if !strings.HasSuffix(before, "AND") {
			tail := before
			if len(tail) > 48 {
				tail = "…" + tail[len(tail)-48:]
			}
			t.Errorf("%s: VisibleChannelExpr が AND で繋がれていない（直前: %q）", name, tail)
		}
		found++
		i = pos + len(call)
	}
	if found == 0 {
		t.Errorf("%s: VisibleChannelExpr が無い", name)
	}
}
