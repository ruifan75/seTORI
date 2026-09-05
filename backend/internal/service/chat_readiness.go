package service

import (
	"encoding/json"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

// chatOutcome は live chat 取得の結果。**3 態に分けるのが要点。**
//
// 以前は「取れたか」の 2 態だったので、BOT 判定・timeout・yt-dlp 未導入といった
// **一時的な障害**まで「この配信にはチャットが無い」と同じ扱いになっていた。
// 障害の最中に解析した配信は、下の年齢判定を通って結論として保存されてしまう。
type chatOutcome int

const (
	// chatOK … 到達できた。拍手 0 件でも「拍手が無い」は確かな結論。
	chatOK chatOutcome = iota
	// chatNoReplay … yt-dlp は正常終了したが replay が無い。
	// **まだ変換中かもしれない**ので、結論にしてよいかは経過時間で決める。
	chatNoReplay
	// chatTransientError … BOT 判定・timeout・未導入・解析失敗など。
	// **経過時間に関係なく結論にしない。** 障害はいつか直るが、
	// 一度キャッシュに固定すると誰かが手で backfill するまで直らない。
	chatTransientError
)

// minLiveChatBytes は live chat のキャッシュとして信用する最小サイズ。
//
// **ファイルの存在は証拠にならない。** 途中で切れた／空のファイルが残っていると、
// パーサは壊れた行を黙って読み飛ばすので「0 件・エラー無し」になり、
// 「拍手が無かった」という確かな結論として保存されてしまう。しかもキャッシュなので
// force 分析でも同じファイルを読み直し、二度と回復しない。
//
// 実際の live_chat.json は 1 行 1 メッセージの JSONL で、数十 KB〜数 MB になる。
// ここは「明らかに壊れている」を弾くための下限で、正常な値の推定ではない
// （チャットが本当に静かだった配信を弾かないよう、小さめに取ってある）。
const minLiveChatBytes = 64

// chatRetryWindow は「まだ変換中かもしれない」と見なす期間。
//
// 配信が終わった直後は YouTube の変換が終わっておらず、live chat replay を
// しばらく取得できない。この間の「取れなかった」は結論ではなく途中経過。
//
// **観測（2026-09-05、実際の配信で確認）**：通常は 1 時間以内に取得できる。
// ただし**数日かかることが稀にある**。48 時間はその中間で、
// 「ほとんどの配信を待ちきれる」ことを優先した値。
//
// 外したときの損害が非対称なので、長い側へ倒してある：
//
//	短すぎる … 変換前に「チャットが無い配信」と確定し、**拍手 end が永久に
//	           付かないまま静かに残る**。気付く手段が無い
//	長すぎる … 対象の配信を何度か触り直すだけ。しかも一括では
//	           AI 抽出より先に chat を探るので（analyzeOptions.ProbeChatFirst）、
//	           繰り返すのは yt-dlp の探りだけで AI は呼ばれない
//
// **数日かかった稀な配信は取りこぼす**（48 時間を過ぎた時点で確定する）。
// これは受け入れた上での設計で、救う手段は残っている ──
// `POST /api/chat-ends/backfill` は hash を見ずに comment_songs を持つ全配信で
// 拍手検出をやり直すので、確定したあとでも後から埋められる。
const chatRetryWindow = 48 * time.Hour

// streamEndedAt は配信の終了時刻を返す。
//
// Holodex の end_actual を第一候補にする（本番 1318 本すべてに入っている）。
// 無ければ配信日で代用する ── 日付だけでも「48 時間以内か」の判定には足りる。
// 3 つ目の戻り値は「実際の終了時刻か」（false なら配信日で代用した概算）。
func streamEndedAt(stream models.Stream) (time.Time, bool, bool) {
	if len(stream.HolodexData) > 0 {
		var hd struct {
			EndActual string `json:"end_actual"`
		}
		if err := json.Unmarshal(stream.HolodexData, &hd); err == nil && hd.EndActual != "" {
			if t, err := time.Parse(time.RFC3339, hd.EndActual); err == nil {
				return t, true, true
			}
		}
	}
	if !stream.StreamDate.IsZero() {
		return stream.StreamDate, true, false
	}
	return time.Time{}, false, false
}

// holdReason は保留の理由を人が読める形で返す（ログ用）。**保留していないときは空。**
// 「変換待ち」と「一時的な障害」を同じ文言で出すと、古い配信に対して
// 「終了から 48 時間以内」と嘘のログが出る。
func holdReason(stream models.Stream, outcome chatOutcome, now time.Time) string {
	if !holdCacheForChat(stream, outcome, now) {
		return ""
	}
	if outcome == chatTransientError {
		return "live chat の取得が一時的に失敗した（BOT 判定・レート制限・timeout 等）"
	}
	return "live chat replay がまだ無く、配信からの経過時間が短い（変換待ち）"
}

// holdCacheForChat は「抽出結果をキャッシュせずに次回やり直すべきか」を返す。
//
// **live chat に到達できなかったことと、到達したが拍手が無かったことは
// 区別が付かない。** そのまま保存すると hash が入り、次の一括プレ分析は
// キャッシュ命中で拍手検出まで飛ばすので、その配信の end は
// コメントに書かれた値のまま固定される（CLAUDE.md §6.5）。
//
// かといって「到達できなければ常にやり直す」にはできない。チャットが
// 無効な配信は**永久に到達できず、毎回 AI を呼び直す**ことになる。
// そこで**終了からの経過時間で区切る**：まだ新しいうちは「変換待ち」と見なして
// やり直し、十分に古ければ「この配信にはチャットが無い」と結論する。
//
// 年齢が分からない配信は保存する側に倒す（分からないものを無限に
// 再試行するより、一度確定させて手動の backfill に任せるほうが安全）。
// **一時的な障害はこの限りではない** ── そちらは年齢に関係なく保留する。
func holdCacheForChat(stream models.Stream, outcome chatOutcome, now time.Time) bool {
	switch outcome {
	case chatOK:
		return false
	case chatTransientError:
		// 障害から結論しない。3 日前の配信を BOT 判定の最中に解析しても、
		// 「チャットが無い配信」として固定されてはいけない。
		return true
	}

	// chatNoReplay ── 変換待ちかもしれないので、経過時間で決める。
	endedAt, ok, exact := streamEndedAt(stream)
	if !ok {
		return false
	}
	window := chatRetryWindow
	if !exact {
		// 配信日しか無い＝**その日の 0 時**を終了時刻として扱っている。
		// 実際の終了は最大でその翌日にまで及ぶので、そのぶん余裕を足さないと
		// 「48 時間経った」と言いながら実際は 24 時間しか経っていないことがある。
		window += streamDateSlack
	}
	return now.Sub(endedAt) < window
}

// streamDateSlack は end_actual が無く配信日で代用するときの上乗せ。
// 配信日は 0 時なので、当日中に終わる配信なら実際の終了は最大 24 時間後。
// さらに日跨ぎと時差のぶんを見て 1 日足す。
const streamDateSlack = 48 * time.Hour
