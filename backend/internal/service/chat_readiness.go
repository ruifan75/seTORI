package service

import (
	"encoding/json"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

// chatRetryWindow は「まだ変換中かもしれない」と見なす期間。
//
// 配信が終わった直後は YouTube の変換が終わっておらず、live chat replay を
// しばらく取得できない。この間の「取れなかった」は結論ではなく途中経過。
//
// 長めに取ってあるのは、外したときの損害が非対称だから ── 短すぎると
// 「拍手 end が永久に付かない配信」が静かに残り、長すぎても
// 高々もう一度 AI を呼び直すだけで済む。
const chatRetryWindow = 48 * time.Hour

// streamEndedAt は配信の終了時刻を返す。
//
// Holodex の end_actual を第一候補にする（本番 1318 本すべてに入っている）。
// 無ければ配信日で代用する ── 日付だけでも「48 時間以内か」の判定には足りる。
func streamEndedAt(stream models.Stream) (time.Time, bool) {
	if len(stream.HolodexData) > 0 {
		var hd struct {
			EndActual string `json:"end_actual"`
		}
		if err := json.Unmarshal(stream.HolodexData, &hd); err == nil && hd.EndActual != "" {
			if t, err := time.Parse(time.RFC3339, hd.EndActual); err == nil {
				return t, true
			}
		}
	}
	if !stream.StreamDate.IsZero() {
		return stream.StreamDate, true
	}
	return time.Time{}, false
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
func holdCacheForChat(stream models.Stream, chatReachable bool, now time.Time) bool {
	if chatReachable {
		return false
	}
	endedAt, ok := streamEndedAt(stream)
	if !ok {
		return false
	}
	return now.Sub(endedAt) < chatRetryWindow
}
