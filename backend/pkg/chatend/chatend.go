// Package chatend 從 YouTube live chat replay 偵測每首歌的結束時間。
//
// 原理：歌回沒有現場掌聲，但歌一結束觀眾會在 chat 刷「純拍手」(888 / 拍手 / :clapping_hands: / 👏)。
// 對每首歌（以 setlist 的 start 為輸入），在 (start+MinSong, next_start) 區間找「最大的純拍手群」，
// 取該群的起拍時刻 − ReactionLag 當作結束。全庫 1445 首實證 MAE 2.18s / 99% 在 ±10s 內。
//
// 這是純文字邏輯（無外部依賴），與 utawaku-timestamp/chat_end_detector.py 的預設行為對齊。
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

// Event 一則 chat 留言（影片內秒數 + 文字）。
type Event struct {
	T    float64
	Text string
}

// EndEstimate 單首歌的結束時間估計。End 為 nil 表示找不到拍手。
type EndEstimate struct {
	Start      float64
	End        *float64
	Confidence float64
}

// Options 偵測參數（預設值見 DefaultOptions）。
type Options struct {
	BinS         float64 // 時間分箱大小
	ReactionLagS float64 // 起拍時刻 − 此值 = 真實 end
	MinSpike     int     // 一個 bin 至少幾則拍手才算「群」的一部分
	MinSongS     float64 // 最短歌長，避免抓到開頭的拍手
	GapMergeS    float64 // 相鄰拍手 bin 間隔 ≤ 此值併為同一群
}

// DefaultOptions 回傳經全庫調校的預設參數。
func DefaultOptions() Options {
	return Options{BinS: 2.0, ReactionLagS: 2.0, MinSpike: 3, MinSongS: 45.0, GapMergeS: 8.0}
}

// 「純拍手」token；與 Python _APPLAUSE_TOKEN 對齊。
var applauseRe = regexp.MustCompile(`(?i)(8{3,}|ぱち|パチ|拍手|:clapping_hands:|:clap\w*:|👏|🎉|🥳)`)

// 拿掉拍手 token 後，剩下的「可忽略字元」(空白/標點/笑)；與 Python _TRIVIAL 對齊。
var trivialRe = regexp.MustCompile(`[\s：:、。!！?？~〜ーｗw♪…_\-]+`)

// IsPureApplause 判斷整則留言是否「只含拍手」(沒有其他文字或非拍手 emote)。
func IsPureApplause(text string) bool {
	stripped := applauseRe.ReplaceAllString(text, "")
	if stripped == text {
		return false // 完全沒有拍手 token
	}
	return trivialRe.ReplaceAllString(stripped, "") == "" // 去掉拍手後沒剩別的
}

// DetectEnds 對每首歌（以 starts 為輸入）偵測拍手結束時間。
func DetectEnds(starts []float64, events []Event, streamEnd float64, opt Options) []EndEstimate {
	sorted := append([]float64(nil), starts...)
	sort.Float64s(sorted)

	// 純拍手事件時刻（排序）
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
			// 顯著 bin → 分群
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
			// select=peak：選峰值最大的群（=曲末集體拍手）
			ci, bestPeak := 0, -1
			for j, cl := range clusters {
				if p := win[peakIdx(cl)]; p > bestPeak {
					bestPeak, ci = p, j
				}
			}
			cl := clusters[ci]
			// 該群的起拍時刻（精確到事件）
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
			// 稀疏：用最早的一則拍手
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

// ---- live chat JSONL 解析（yt-dlp --write-subs --sub-langs live_chat 的輸出）----

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

// ParseLiveChat 解析 live_chat.json（JSONL，每行一個 replay action）。
func ParseLiveChat(r io.Reader) ([]Event, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024) // 單行可能很長
	var events []Event
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var lc liveChatLine
		if err := json.Unmarshal(line, &lc); err != nil {
			continue // 略過壞行
		}
		if lc.Replay.OffsetMs == "" {
			continue
		}
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
		return nil, fmt.Errorf("scan live chat: %w", err)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].T < events[j].T })
	return events, nil
}

// ParseLiveChatFile 從檔案讀取並解析 live chat。
func ParseLiveChatFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
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
