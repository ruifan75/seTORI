package service

import (
	"database/sql"
	"errors"
	"testing"
)

// レート制限は消失と**同じ "Video unavailable" で来る**。どちらの述語にも当たるので、
// 順序だけが誤分類を防いでいる。ここで固定するのは述語の戻り値ではなく
// **決定そのもの**（classifyFetchFailure）── 述語を個別に検査するテストは、
// 呼び出し側で順序を入れ替えても通ってしまい、守りたいものを守らない。
func TestClassifyFetchFailure(t *testing.T) {
	// yt-dlp は reason と subreason を連結してから rate-limited の案内を足す
	// （extractor/youtube/_video.py）。先頭は消失と区別が付かない。
	rateLimited := "ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. " +
		"The current session has been rate-limited by YouTube for up to an hour."

	if got := classifyFetchFailure(rateLimited); got != failureTransient {
		t.Fatalf("レート制限を %v と判定した（failureTransient であるべき）", got)
	}
	// 前提の確認：この文字列は消失の目印にも当たる。当たらなくなったなら
	// isVideoGone を緩めた可能性があるので、順序の必要性ごと見直すこと。
	if !isVideoGone(rateLimited) {
		t.Fatal("前提が変わった：レート制限の文字列が isVideoGone に当たらなくなっている")
	}

	cases := []struct {
		name   string
		stderr string
		want   failureKind
	}{
		{"削除済み", "ERROR: [youtube] hVfDBfreYNI: Video unavailable. This video is not available", failureVideoGone},
		{"存在しない ID", "ERROR: [youtube] aaaaaaaaaaa: Video unavailable", failureVideoGone},
		{"通信障害", "ERROR: [youtube] abc: Unable to download API page: ('Unable to connect to proxy', ...)", failureTransient},
		{"レート制限(429)", "ERROR: unable to download video data: HTTP Error 429: Too Many Requests", failureTransient},
		{"BOT 判定", "ERROR: [youtube] abc: Sign in to confirm you're not a bot", failureTransient},
		{"知らない失敗", "ERROR: [youtube] abc: Something completely new", failureUnknown},
	}
	for _, c := range cases {
		if got := classifyFetchFailure(c.stderr); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// Resolved は片方だけでは true にならない。availability は initial_data（fatal=False）由来、
// playable_in_embed は player response 由来で、**別々に落ちる**。
// cookie 有りの会限で initial_data だけ失敗すると NA<TAB>True が終了コード 0 で返り、
// playable_in_embed だけを見ていると subscriber_only を落としたまま playable と誤判定する。
func TestResolvedRequiresBothFields(t *testing.T) {
	yes := sql.NullBool{Bool: true, Valid: true}
	cases := []struct {
		name string
		a    ytdlpAvailability
		want bool
	}{
		{"両方あり", ytdlpAvailability{Availability: "public", PlayableInEmbed: yes}, true},
		{"会限（両方あり）", ytdlpAvailability{Availability: "subscriber_only", PlayableInEmbed: yes}, true},
		{"initial_data だけ落ちた", ytdlpAvailability{PlayableInEmbed: yes}, false},
		{"動画を取れていない", ytdlpAvailability{Availability: "public"}, false},
		{"どちらも無い", ytdlpAvailability{}, false},
	}
	for _, c := range cases {
		if got := c.a.Resolved(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// 2 パスの分岐で守りたい 2 つの不変条件を固定する。どちらも実装を書いた直後に
// レビューで指摘された穴で、型では防げない（両方 bool / 文字列なので取り違えても通る）。
//
//	① ②へ進むのは①が終了コード 0 のときだけ
//	   ①が非ゼロなら、その失敗はフラグの有無で解けない（BOT 判定・レート制限・
//	   タイムアウト）。もう一度走らせても同じ理由で落ち、障害時に外部呼び出しが倍になる。
//	   さらに、①で「レート制限」と確定できていた証拠を捨てて②の短い応答だけを見ると、
//	   同じ動画を「動画が無い」として確定させうる。
//
//	② ②へ来たら①の結果は丸ごと捨てる
//	   値だけ差し替えて cookiesSent を①のものにしていたため、
//	   「①は cookie あり・②は cookie 無し」のとき、会限を「動画消失」と確定させえた。
func TestFetchTwoPassInvariants(t *testing.T) {
	// レート制限は「Video unavailable」で始まるので、①の証拠を捨てると
	// ②の短い応答で failureVideoGone に化ける。
	rateLimited := "ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. " +
		"The current session has been rate-limited by YouTube for up to an hour."
	shortGone := "ERROR: [youtube] abc: Video unavailable"

	if classifyFetchFailure(rateLimited) != failureTransient {
		t.Fatal("前提が変わった：レート制限が transient として分類されない")
	}
	if classifyFetchFailure(shortGone) != failureVideoGone {
		t.Fatal("前提が変わった：短い応答が videoGone として分類されない")
	}

	// ここで固定したいのは「①が非ゼロなら②を走らせない」という構造そのもの。
	// run は外部プロセスを起動するので単体では呼べない ── 代わりに、
	// 分岐の条件に使っている ok() が runErr だけを見ていることを確かめる。
	if (ytdlpRun{runErr: errors.New("boom")}).ok() {
		t.Error("runErr があるのに ok() が true。②へ進んでしまう")
	}
	if !(ytdlpRun{}).ok() {
		t.Error("runErr が無いのに ok() が false。②へ進めない")
	}

	// **②へ来たときに①の状態が残らないことは、テストでは示せない。**
	// ここで `r = pass2` と書いて確かめても、検査しているのはテスト自身の代入であって
	// Fetch の分岐ではない。実装側で struct ごと差し替える形にしてあるのが根拠で、
	// フィールドを個別に代入する書き方へ戻せばこの保証は失われる
	// （cookiesSent だけ①のものを見ていて、会限を「動画消失」と確定させえた）。
}
