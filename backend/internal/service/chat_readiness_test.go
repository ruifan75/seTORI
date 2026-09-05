package service

import (
	"os"
	"path/filepath"
	"strings"
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

// 保留の理由はログにそのまま出る。障害を「変換待ち」と説明すると、
// 古い配信に対して「終了から 48 時間以内」という嘘のログになる。
func TestHoldReasonDistinguishesCause(t *testing.T) {
	now := time.Now()
	old := streamEndedAgo(30 * 24 * time.Hour)

	if r := holdReason(old, chatTransientError, now); r == "" {
		t.Fatal("障害は保留されるので理由が要る")
	} else if !strings.Contains(r, "一時的") {
		t.Errorf("障害の理由が「一時的」と読めない: %q", r)
	}

	fresh := streamEndedAgo(1 * time.Hour)
	if r := holdReason(fresh, chatNoReplay, now); !strings.Contains(r, "変換待ち") {
		t.Errorf("変換待ちの理由がそう読めない: %q", r)
	}

	// 保留していないときは空（呼び出し側がログを出さない判断に使える）
	if r := holdReason(fresh, chatOK, now); r != "" {
		t.Errorf("保留していないのに理由がある: %q", r)
	}
}

// 壊れた／空のキャッシュを chatOK として確定しないこと。
// 実ファイルで確かめる（サイズ判定なのでモックでは意味が無い）。
func TestSuspectLiveChatCacheIsRejected(t *testing.T) {
	dir := t.TempDir()
	svc := NewChatEndService(nil, "yt-dlp-does-not-exist-for-this-test", dir)

	// 途中で切れたファイル（codex の指摘そのもの：`{` だけ）
	broken := filepath.Join(dir, "vid1.live_chat.json")
	if err := os.WriteFile(broken, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	// yt-dlp は存在しないので取得は失敗するが、**キャッシュを信用していれば
	// そこで chatOK を返して終わる**。返らないこと＝小さいファイルを弾いたこと。
	_, outcome, err := svc.fetchLiveChat("vid1")
	if err == nil || outcome == chatOK {
		t.Fatalf("壊れたキャッシュを採用してしまった (outcome=%v err=%v)", outcome, err)
	}
	if _, statErr := os.Stat(broken); statErr == nil {
		t.Error("壊れたキャッシュが残っている（取り直せない）")
	}

	// 正常系を巻き込んでいないこと。**中身も本物にする** ── 以前ここは
	// `{"a":1}` を並べただけで、サイズは足りるが replay としては無効だった。
	// つまりこのテスト自体が「長ければ中身が壊れていても通る」ことを実証していた。
	good := filepath.Join(dir, "vid2.live_chat.json")
	record := `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[` +
		`{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":` +
		`{"message":{"runs":[{"text":"888"}]}}}}}]}}` + "\n"
	if err := os.WriteFile(good, []byte(strings.Repeat(record, 4)), 0o644); err != nil {
		t.Fatal(err)
	}
	path, outcome, err := svc.fetchLiveChat("vid2")
	if err != nil || outcome != chatOK || path != good {
		t.Errorf("正常なキャッシュを使っていない (outcome=%v err=%v)", outcome, err)
	}

	// **サイズは有効性の根拠にならない。** 長さは足りるが replay ではないファイルは、
	// パーサが全行を読み飛ばして「0 件・エラー無し」になる。これを結論にすると
	// 壊れたキャッシュのまま確定するので、解析側で気付いて消す必要がある。
	junk := filepath.Join(dir, "vid3.live_chat.json")
	if err := os.WriteFile(junk, []byte(strings.Repeat(`{"a":1}`+"\n", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, outcome := svc.DetectEnds("vid3", 600, []int{0}); outcome != chatTransientError {
		t.Errorf("中身が replay でないファイルを結論にしてしまった (outcome=%v)", outcome)
	}
	if _, statErr := os.Stat(junk); statErr == nil {
		t.Error("中身が壊れたキャッシュが残っている（取り直せない）")
	}
}
