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
		name    string
		stream  models.Stream
		outcome chatOutcome
		want    bool
		why     string
	}{
		{
			name:    "到達できたら保存する",
			stream:  streamEndedAgo(1 * time.Hour),
			outcome: chatOK,
			want:    false,
			why:     "拍手が 0 件でも、到達できたなら「拍手が無い」は確かな結論",
		},
		{
			name:    "配信直後で replay が無い → やり直す",
			stream:  streamEndedAgo(30 * time.Minute),
			outcome: chatNoReplay,
			want:    true,
			why:     "YouTube の変換待ち。保存すると end がコメントの値で固定される",
		},
		{
			name:    "境界の直前",
			stream:  streamEndedAgo(chatRetryWindow - time.Hour),
			outcome: chatNoReplay,
			want:    true,
		},
		{
			name:    "境界を過ぎたら結論する",
			stream:  streamEndedAgo(chatRetryWindow + time.Hour),
			outcome: chatNoReplay,
			want:    false,
			why:     "チャットが無効な配信を無限に再試行しないため",
		},
		{
			name:    "年齢が分からなければ保存側へ倒す",
			stream:  models.Stream{ID: "v2"}, // holodex_data も stream_date も無い
			outcome: chatNoReplay,
			want:    false,
			why:     "分からないものを無限に再試行するより、一度確定させる",
		},

		// ── 一時的な障害は年齢に関係なく保留する ──
		//
		// replay 無しと同じ扱いにすると、**BOT 判定や timeout の最中に解析した
		// 古い配信が「チャットの無い配信」として固定される**。障害はいつか直るが、
		// キャッシュは誰かが手で backfill するまで直らない。
		{
			name:    "障害は古い配信でも保留する",
			stream:  streamEndedAgo(30 * 24 * time.Hour),
			outcome: chatTransientError,
			want:    true,
			why:     "BOT 判定・timeout・yt-dlp 未導入。障害から結論しない",
		},
		{
			name:    "障害は年齢不明でも保留する",
			stream:  models.Stream{ID: "v4"},
			outcome: chatTransientError,
			want:    true,
		},

		// ── stream_date で代用するときは余裕を足す ──
		//
		// stream_date は**その日の 0 時**なので、実際の終了は最大でその翌日に及ぶ。
		// 余裕なしだと「48 時間経った」と言いながら実際は 24 時間しか
		// 経っていないことがある。
		{
			name:    "stream_date 代用：余裕の内側はまだ保留",
			stream:  models.Stream{ID: "v5", StreamDate: now.Add(-(chatRetryWindow + time.Hour))},
			outcome: chatNoReplay,
			want:    true,
			why:     "end_actual が無いので「48 時間経った」と言い切れない",
		},
		{
			name:    "stream_date 代用：余裕を足しても過ぎたら結論する",
			stream:  models.Stream{ID: "v6", StreamDate: now.Add(-(chatRetryWindow + streamDateSlack + time.Hour))},
			outcome: chatNoReplay,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := holdCacheForChat(tt.stream, tt.outcome, now); got != tt.want {
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
	got, ok, exact := streamEndedAt(s)
	if !ok {
		t.Fatal("終了時刻を取れなかった")
	}
	if !exact {
		t.Error("end_actual があるのに概算扱いになっている")
	}
	if !got.UTC().Truncate(time.Second).Equal(want) {
		t.Errorf("end_actual を優先していない: got %v, want %v", got, want)
	}
}

// holodex_data が壊れていても落ちず、概算（stream_date）へ落ちること。
func TestStreamEndedAtHandlesBrokenHolodexData(t *testing.T) {
	date := time.Now().Add(-5 * time.Hour)
	for _, raw := range []string{`{`, `null`, `{"end_actual":""}`, `{"end_actual":"not-a-time"}`} {
		got, ok, exact := streamEndedAt(models.Stream{StreamDate: date, HolodexData: []byte(raw)})
		if !ok || exact {
			t.Errorf("raw=%s: 概算として stream_date に落ちるべき (ok=%v exact=%v)", raw, ok, exact)
		}
		if !got.Equal(date) {
			t.Errorf("raw=%s: stream_date が返らない", raw)
		}
	}
}
