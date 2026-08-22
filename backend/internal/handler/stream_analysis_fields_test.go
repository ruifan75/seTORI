package handler

import (
	"encoding/json"
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

// 解析結果（各タイムライン・分析時刻）は編集画面だけが読む中間生成物で、
// 閲覧向けの応答には載せない。
//
// このテストが守るのは「載せない側が omitempty で本当に消えるか」。
// 応答の型は同じなので、載せ忘れ／落とし忘れはコンパイルでは捕まらない。
func TestAnalysisFieldsAreOmittedWhenEmpty(t *testing.T) {
	encoded, err := json.Marshal(dto.StreamResponse{ID: "abc123", Title: "テスト"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}

	// 編集画面だけが読む欄位 ── 空なら応答に現れないこと
	for _, k := range []string{
		"holodex_timeline_songs",
		"comment_timeline_songs",
		"chapter_timeline_songs",
		"comment_songs_analyzed_at",
	} {
		if _, ok := back[k]; ok {
			t.Errorf("%s は空のとき応答に出てはいけない: %s", k, encoded)
		}
	}

	// 閲覧に要る欄位は残ること（落としすぎの検出）
	for _, k := range []string{"id", "title", "tags", "participants", "is_hidden", "is_processed"} {
		if _, ok := back[k]; !ok {
			t.Errorf("%s が応答から消えている: %s", k, encoded)
		}
	}
}
