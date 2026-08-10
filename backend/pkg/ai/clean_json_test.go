package ai

import (
	"encoding/json"
	"testing"
)

// 正しい JSON 配列を壊さないことを固定する。
//
// 以前は "[{...}]" のように **正しく [ で始まる応答**で先頭の [ を削り、
// そのあと「{ で始まるなら配列に包む」が働いて "]" が二重になっていた。
// json.Unmarshal を使う経路（別名義の AI 判定）はこれで必ず失敗し、
// AI が正しく答えていた判定がすべて捨てられていた。
// grouped 経路が無事だったのは Decoder.Decode が末尾の余りを無視するため。
func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"正しい配列はそのまま通る", `[{"i":0,"same":true},{"i":1,"same":false}]`},
		{"単一オブジェクトは配列に包む", `{"i":0,"same":true}`},
		{"コードフェンス付き", "```json\n[{\"i\":0,\"same\":true}]\n```"},
		{"前置き付き", "以下はJSONです。\n[{\"i\":0,\"same\":true}]"},
		{"末尾カンマ", `[{"i":0,"same":true},]`},
		{"カンマ区切りのオブジェクト列挙", `{"i":0,"same":true},{"i":1,"same":false}`},
		// AI が配列をオブジェクトで包んで返すことがある。中の配列を取り出せること
		// （[ を優先する挙動。ここが壊れると正規化が AI の結果を捨てて素通しになる）
		{"配列をオブジェクトで包んだ応答", `{"suggestions":[{"i":0,"same":true}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanJSONResponse(tt.in)
			var v []map[string]any
			// Unmarshal は入力全体が 1 つの値であることを要求する。
			// Decoder と違って末尾の余りを見逃さないので、壊れていれば必ず落ちる。
			if err := json.Unmarshal([]byte(got), &v); err != nil {
				t.Fatalf("json.Unmarshal(%q) = %v", got, err)
			}
			if len(v) == 0 {
				t.Fatalf("要素が空になった: %q", got)
			}
		})
	}
}
