package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

// 歌手ページは **StreamService とは別の toStreamResponse** を持っている。
// 同じ DTO を返すので「載せる／載せない」の判断が 2 か所に要り、片方だけ直すと
// 権限の穴になる。StreamService 側だけを見張っていると、こちらを
// 「常に載せる」に戻しても全テストが通ってしまうので、ここで固定する。
func TestSingerToStreamResponseHidesOperationalState(t *testing.T) {
	svc := &SingerService{}
	stream := models.Stream{
		ID:          "v1",
		Title:       "t",
		StreamDate:  time.Now(),
		IsProcessed: true,
	}

	keysOf := func(t *testing.T, isEditor bool) (map[string]json.RawMessage, dto0) {
		t.Helper()
		resp := svc.toStreamResponse(stream, nil, nil, isEditor)
		encoded, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &m); err != nil {
			t.Fatal(err)
		}
		return m, dto0{resp.IsProcessed}
	}

	t.Run("権限が無ければ欄位ごと消す", func(t *testing.T) {
		m, d := keysOf(t, false)
		if _, ok := m["is_processed"]; ok {
			t.Error("is_processed が閲覧向けの応答に載っている")
		}
		if d.processed != nil {
			t.Error("is_processed は nil であるべき")
		}
		// 落としすぎていないこと（配信そのものは誰でも見られる）
		for _, k := range []string{"id", "title", "stream_date", "is_hidden"} {
			if _, ok := m[k]; !ok {
				t.Errorf("%s が消えている（隠すのは処理状態だけ）", k)
			}
		}
	})

	t.Run("content:edit なら値を載せる", func(t *testing.T) {
		m, d := keysOf(t, true)
		if _, ok := m["is_processed"]; !ok {
			t.Fatal("is_processed が編集者向けの応答に載っていない")
		}
		if d.processed == nil || !*d.processed {
			t.Errorf("元の値 true が保たれていない: %v", d.processed)
		}
	})
}

// dto0 は上のテストで値まで見るための小さな入れ物。
type dto0 struct{ processed *bool }
