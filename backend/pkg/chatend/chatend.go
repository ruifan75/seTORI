// Package chatend は YouTube live chat replay から各曲の終了時刻を検出する。
//
// 原理：歌枠には会場の拍手はないが、歌が終わると視聴者が chat に「拍手だけ」
// (888 / 拍手 / :clapping_hands: / 👏) を連投する。各曲について setlist の start を入力とし、
// (start+MinSong, next_start) の区間で最大の拍手群を探し、その群の開始時刻 − ReactionLag を終了時刻とする。
// 全 1,445 曲での実測は MAE 2.18 秒、99% が ±10 秒以内。
//
// これはテキストだけで完結するロジック（外部依存なし）。原型は独立した Python ツール
// （現在はこのプロジェクトに含まれない）で、本実装はその既定パラメータを引き継いでいる。
package chatend

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
)

// Event は chat の 1 コメント（動画内の秒数 + テキスト）。
type Event struct {
	T    float64
	Text string
}

// EndEstimate は 1 曲の終了時刻の推定結果。End が nil なら拍手が見つからなかったことを示す。
type EndEstimate struct {
	Start      float64
	End        *float64
	Confidence float64
}

// Options は検出パラメータ（既定値は DefaultOptions を参照）。
type Options struct {
	BinS         float64 // 時間 bin の幅
	ReactionLagS float64 // 拍手開始時刻 − この値 = 実際の終了時刻
	MinSpike     int     // 1 つの bin に最低何件の拍手があれば「群」の一部とみなすか
	MinSongS     float64 // 最短曲長。冒頭の拍手を拾わないため
	GapMergeS    float64 // 隣接する拍手 bin の間隔がこの値以下なら同じ群にまとめる
}

// DefaultOptions は全データで調整した既定パラメータを返す。
func DefaultOptions() Options {
	return Options{BinS: 2.0, ReactionLagS: 2.0, MinSpike: 3, MinSongS: 45.0, GapMergeS: 8.0}
}

// 「拍手のみ」のトークン。Python の _APPLAUSE_TOKEN と同じ定義にする。
var applauseRe = regexp.MustCompile(`(?i)(8{3,}|ぱち|パチ|拍手|:clapping_hands:|:clap\w*:|👏|🎉|🥳)`)

// 拍手トークンを除いた後に残る「無視できる文字」（空白・句読点・笑）。Python の _TRIVIAL と同じ定義にする。
var trivialRe = regexp.MustCompile(`[\s：:、。!！?？~〜ーｗw♪…_\-]+`)

// IsPureApplause はコメント全体が「拍手だけ」か判定する（他の文字や拍手以外の emote がないこと）。
func IsPureApplause(text string) bool {
	stripped := applauseRe.ReplaceAllString(text, "")
	if stripped == text {
		return false // 拍手 token が一つもない
	}
	return trivialRe.ReplaceAllString(stripped, "") == "" // 拍手を除いた後に何も残らない
}

// DetectEnds は各曲について starts を入力として拍手による終了時刻を検出する。
func DetectEnds(starts []float64, events []Event, streamEnd float64, opt Options) []EndEstimate {
	sorted := append([]float64(nil), starts...)
	sort.Float64s(sorted)

	// 拍手だけのイベント時刻（並び替え済み）
	var apTimes []float64
	for _, e := range events {
		if IsPureApplause(e.Text) {
			apTimes = append(apTimes, e.T)
		}
	}
	sort.Float64s(apTimes)

	if streamEnd <= 0 {
		if len(apTimes) > 0 {
			streamEnd = apTimes[len(apTimes)-1]
		} else if len(sorted) > 0 {
			streamEnd = sorted[len(sorted)-1] + 600
		}
	}

	nBins := int(streamEnd/opt.BinS) + 1
	counts := make([]int, nBins)
	for _, t := range apTimes {
		i := int(t / opt.BinS)
		if i >= 0 && i < nBins {
			counts[i]++
		}
	}
	gapBins := int(opt.GapMergeS / opt.BinS)
	if gapBins < 1 {
		gapBins = 1
	}

	results := make([]EndEstimate, len(sorted))
	for idx, start := range sorted {
		nxt := streamEnd
		if idx+1 < len(sorted) {
			nxt = sorted[idx+1]
		}
		loT := start + opt.MinSongS
		lo := int(loT / opt.BinS)
		hi := int(nxt / opt.BinS)
		if hi > len(counts)-1 {
			hi = len(counts) - 1
		}
		if hi <= lo {
			results[idx] = EndEstimate{Start: start}
			continue
		}
		win := counts[lo : hi+1]

		maxC := 0
		for _, c := range win {
			if c > maxC {
				maxC = c
			}
		}

		var end, conf float64
		if maxC >= opt.MinSpike {
			// 有意な bin をグループ化する
			var sigIdx []int
			for k, c := range win {
				if c >= opt.MinSpike {
					sigIdx = append(sigIdx, k)
				}
			}
			clusters := [][]int{{sigIdx[0]}}
			for _, k := range sigIdx[1:] {
				last := clusters[len(clusters)-1]
				if k-last[len(last)-1] <= gapBins {
					clusters[len(clusters)-1] = append(last, k)
				} else {
					clusters = append(clusters, []int{k})
				}
			}
			peakIdx := func(cl []int) int {
				best := cl[0]
				for _, k := range cl {
					if win[k] > win[best] {
						best = k
					}
				}
				return best
			}
			// select=peak：ピークが最大の群を選ぶ（＝曲末の集団拍手）
			ci, bestPeak := 0, -1
			for j, cl := range clusters {
				if p := win[peakIdx(cl)]; p > bestPeak {
					bestPeak, ci = p, j
				}
			}
			cl := clusters[ci]
			// その群の拍手開始時刻（イベント単位の精度）
			tLo := float64(lo+cl[0]) * opt.BinS
			tHi := float64(lo+cl[len(cl)-1]+1) * opt.BinS
			onset := tLo
			for _, t := range apTimes {
				if t >= tLo {
					if t <= tHi {
						onset = t
					}
					break
				}
			}
			end = onset - opt.ReactionLagS
			conf = float64(bestPeak) / float64(opt.MinSpike*2)
			if conf > 1 {
				conf = 1
			}
		} else {
			// 疎な場合は最初の拍手を使う
			var first float64
			found := false
			for _, t := range apTimes {
				if t >= loT && t <= nxt {
					first, found = t, true
					break
				}
			}
			if !found {
				results[idx] = EndEstimate{Start: start}
				continue
			}
			end = first - opt.ReactionLagS
			conf = 0.3
		}

		if end < start+1 {
			end = start + 1
		}
		if end > nxt {
			end = nxt
		}
		e := end
		results[idx] = EndEstimate{Start: start, End: &e, Confidence: conf}
	}
	return results
}

// ---- live chat JSONL の解析（yt-dlp --write-subs --sub-langs live_chat の出力）----

type liveChatLine struct {
	Replay struct {
		OffsetMs string `json:"videoOffsetTimeMsec"`
		Actions  []struct {
			Add struct {
				Item struct {
					Text *chatRenderer `json:"liveChatTextMessageRenderer"`
					Paid *chatRenderer `json:"liveChatPaidMessageRenderer"`
				} `json:"item"`
			} `json:"addChatItemAction"`
		} `json:"actions"`
	} `json:"replayChatItemAction"`
}

type chatRenderer struct {
	Message struct {
		Runs []struct {
			Text  string `json:"text"`
			Emoji *struct {
				Shortcuts []string `json:"shortcuts"`
			} `json:"emoji"`
		} `json:"runs"`
	} `json:"message"`
}

// ParseLiveChat は live_chat.json（1 行に 1 replay action の JSONL）を解析する。
//
// **2 つ目の戻り値は「live chat replay として読めたか」。**
// このパーサは壊れた行を黙って読み飛ばす設計なので、途中で切れたファイルや
// 中身が別物のファイルでも「0 件・エラー無し」になる。呼び出し側はそれを
// 「拍手が無かった」という確かな結論と区別できない ── サイズでは判別できない
// （十分に長くても中身が別物なら同じことが起きる）ので、
// **replay の記録を 1 つでも認識できたか**で見る。
func ParseLiveChat(r io.Reader) ([]Event, bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // 1 行が長い場合がある
	var events []Event
	// recognized は「これは live chat replay のファイルだ」と言える根拠。
	// **Event が 0 件でも真になりうる**（記録はあるが描画できるテキストが無い等）。
	// 逆にここが偽なら、ファイルはあっても中身が replay ではない。
	recognized := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var lc liveChatLine
		if err := json.Unmarshal(line, &lc); err != nil {
			continue // 壊れた行をスキップする
		}
		if lc.Replay.OffsetMs == "" {
			continue
		}
		recognized = true
		ms, err := parseInt(lc.Replay.OffsetMs)
		if err != nil {
			continue
		}
		text := ""
		for _, a := range lc.Replay.Actions {
			rend := a.Add.Item.Text
			if rend == nil {
				rend = a.Add.Item.Paid
			}
			if rend == nil {
				continue
			}
			for _, run := range rend.Message.Runs {
				if run.Text != "" {
					text += run.Text
				} else if run.Emoji != nil && len(run.Emoji.Shortcuts) > 0 {
					text += run.Emoji.Shortcuts[0]
				}
			}
		}
		if text != "" {
			events = append(events, Event{T: float64(ms) / 1000.0, Text: text})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, recognized, fmt.Errorf("scan live chat: %w", err)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].T < events[j].T })
	return events, recognized, nil
}

// ParseLiveChatFile はファイルから live chat を読み込んで解析する。
// ParseLiveChatFile はファイルから live chat を読み込んで解析する
// （戻り値は ParseLiveChat と同じ）。
func ParseLiveChatFile(path string) ([]Event, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	return ParseLiveChat(f)
}

func parseInt(s string) (int64, error) {
	var n int64
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid int: %q", s)
		}
		n = n*10 + int64(r-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}
