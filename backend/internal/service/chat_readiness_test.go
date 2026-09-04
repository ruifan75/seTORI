package service

import (
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

func streamEndedAgo(d time.Duration) models.Stream {
	end := time.Now().Add(-d).UTC().Format(time.RFC3339)
	return models.Stream{
		ID:          "v1",
		StreamDate:  time.Now().Add(-d),
		HolodexData: []byte(`{"end_actual":"` + end + `"}`),
	}
}

// 境界を外すと壊れ方が 2 通りある。短すぎれば「拍手 end が永久に付かない配信」が
// 静かに残り、長すぎれば（＝常にやり直す）チャットが無効な配信で毎回 AI を呼び直す。
func TestHoldCacheForChat(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		stream    models.Stream
		reachable bool
		want      bool
		why       string
	}{
		{
			name: "到達できたら保存する", stream: streamEndedAgo(1 * time.Hour),
			reachable: true, want: false,
			why: "拍手が 0 件でも、到達できたなら「拍手が無い」という結論は確か",
		},
		{
			name: "配信直後で到達できない → やり直す", stream: streamEndedAgo(30 * time.Minute),
			reachable: false, want: true,
			why: "YouTube の変換待ち。保存すると end がコメントの値で固定される",
		},
		{
			name: "境界の直前", stream: streamEndedAgo(chatRetryWindow - time.Hour),
			reachable: false, want: true,
		},
		{
			name: "境界を過ぎたら結論する", stream: streamEndedAgo(chatRetryWindow + time.Hour),
			reachable: false, want: false,
			why: "チャットが無効な配信を無限に再試行しないため",
		},
		{
			name:      "年齢が分からなければ保存側へ倒す",
			stream:    models.Stream{ID: "v2"}, // holodex_data も stream_date も無い
			reachable: false, want: false,
			why: "分からないものを無限に再試行するより、一度確定させる",
		},
		{
			name:      "holodex_data が無くても stream_date で判定できる",
			stream:    models.Stream{ID: "v3", StreamDate: now.Add(-2 * time.Hour)},
			reachable: false, want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := holdCacheForChat(tt.stream, tt.reachable, now); got != tt.want {
				t.Errorf("holdCacheForChat = %v, want %v（%s）", got, tt.want, tt.why)
			}
		})
	}
}

func TestStreamEndedAtPrefersHolodex(t *testing.T) {
	// end_actual と stream_date が食い違うとき、実際の終了時刻である
	// end_actual を採る（stream_date は日付で、長時間配信ではずれる）。
	want := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	s := models.Stream{
		StreamDate:  time.Now().Add(-30 * time.Hour),
		HolodexData: []byte(`{"end_actual":"` + want.Format(time.RFC3339) + `"}`),
	}
	got, ok := streamEndedAt(s)
	if !ok {
		t.Fatal("終了時刻を取れなかった")
	}
	if !got.UTC().Truncate(time.Second).Equal(want) {
		t.Errorf("end_actual を優先していない: got %v, want %v", got, want)
	}
}
