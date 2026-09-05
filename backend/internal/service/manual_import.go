package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/pkg/comment"
	"github.com/ruifan75/setori/pkg/util"
)

// ========== 手動での取り込み（会限配信のため） ==========
//
// **会限配信は入力源が全部塞がっている。** コメントは YouTube Data API が
// `commentThreads/forbidden` で 403 を返し（API キー方式なので cookie を足しても
// 変わらない）、live chat は視聴資格が要る。Holodex にも曲は無い。
//
// 一方、メンバー資格のある編集者が手元で yt-dlp を回せば両方取れる。
// そこで取れたものを持ち込む口をここに置く。**新しい解析は何も無い** ──
// 既にある入力源の置き場所へ、人が運んできたものを入れるだけ。

// InfoJSONImport は取り込んだ info.json の要約（画面で確認するため）。
type InfoJSONImport struct {
	VideoID   string `json:"video_id"`
	Title     string `json:"title"`
	Duration  int    `json:"duration_seconds"`
	Total     int    `json:"total"`      // info.json に入っていたコメント数
	TopLevel  int    `json:"top_level"`  // うち返信でないもの
	Saved     int    `json:"saved"`      // 実際に comment_raw へ入れた数（本文が空のものは落とす）
	WithTimes int    `json:"with_times"` // うち時刻らしき表記を含むもの（歌単の当たりを付ける材料）
}

// ErrInfoJSONUnreadable は info.json として読めないこと。
var ErrInfoJSONUnreadable = errors.New("yt-dlp の info.json として読めません")

// ErrInfoJSONMismatch は別の配信の info.json であること。
var ErrInfoJSONMismatch = errors.New("別の配信の info.json です")

// ErrInfoJSONNoComments は comments が入っていないこと（欄が無い／null／空）。
//
// **「取り込んだが 0 件」として通してはいけない。** `--write-comments` の
// 付け忘れが最も多い失敗で、黙って 0 件を受理すると、人は取り込めたと思い、
// 何も起きない理由が分からない。
var ErrInfoJSONNoComments = errors.New("info.json にコメントが入っていません")

// ytdlpInfoJSON は info.json のうち使う欄だけ。
type ytdlpInfoJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
	// **ポインタにするのは「欄が無い／null」と「明示的な空配列」を区別するため。**
	// `--write-comments` を付け忘れた info.json は comments を持たないので、
	// 値型だと空配列と同じ姿になり、「取り込んだが 0 件」として通ってしまう。
	Comments *[]struct {
		Text      string `json:"text"`
		Parent    string `json:"parent"`
		LikeCount *int   `json:"like_count"`
		IsPinned  bool   `json:"is_pinned"`
	} `json:"comments"`
}

// ImportInfoJSON は編集者が手元の yt-dlp で取った info.json からコメントを取り込む。
//
// **取り違えは id で弾く。** info.json は動画 ID を持っているので、live chat と違って
// 機械で確かめられる（live chat のファイルには ID がどこにも入っていない）。
// 確かめられるものは確かめる ── 別の配信のコメントから歌単を作ると、
// 誤りが歌唱記録として残り、あとから来た人には由来が分からない。
//
// **保存は `SaveCommentRaw` を通す。** raw が変われば `comment_songs` と hash が
// NULL に戻るので、次の解析が自動でやり直される。ここで解析まではしない
// （人が画面で確かめてから走らせる）。
func (s *CommentService) ImportInfoJSON(videoID string, data []byte) (InfoJSONImport, error) {
	out, texts, err := parseInfoJSON(videoID, data)
	if err != nil {
		return out, err
	}

	raw, err := json.Marshal(texts)
	if err != nil {
		return out, fmt.Errorf("marshal comments: %w", err)
	}
	// 取り込みは必ず非空（`parseInfoJSON` が 0 件を弾く）なので、
	// SaveCommentRaw の「空で非空を潰さない」歯止めには当たらない。
	// それでも書けたかは確かめる ── 当たったなら前提が崩れている。
	written, err := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(raw))
	if err != nil {
		return out, fmt.Errorf("save comment raw: %w", err)
	}
	if !written {
		return out, fmt.Errorf("コメントを保存できませんでした（配信が見つからないか、取り込む中身が空です）")
	}

	logger.Infof("[import] %s: info.json からコメントを取り込みました（%d 件・うち時刻表記 %d 件）",
		videoID, out.Saved, out.WithTimes)
	return out, nil
}

// parseInfoJSON は取り込む中身を決める（保存はしない）。
//
// **保存と分けてあるのは、取り違えの判定と並びの規則をテストできるようにするため。**
// DB を要る形にしておくと、この関数が本当に id を見ているのか、それとも別の理由で
// 落ちているのかを確かめられない。
func parseInfoJSON(videoID string, data []byte) (InfoJSONImport, []string, error) {
	var out InfoJSONImport

	var info ytdlpInfoJSON
	if err := json.Unmarshal(data, &info); err != nil {
		return out, nil, fmt.Errorf("%w: %v", ErrInfoJSONUnreadable, err)
	}
	if info.ID == "" {
		// id が無いものは info.json ではない（live_chat.json を投げた等）。
		return out, nil, ErrInfoJSONUnreadable
	}
	if info.ID != videoID {
		return out, nil, fmt.Errorf("%w（%s のもの）", ErrInfoJSONMismatch, info.ID)
	}

	switch {
	case info.Comments == nil:
		return out, nil, fmt.Errorf("%w（--write-comments を付けて取り直してください）", ErrInfoJSONNoComments)
	case len(*info.Comments) == 0:
		return out, nil, fmt.Errorf("%w（この配信のコメントは 0 件でした）", ErrInfoJSONNoComments)
	}
	comments := *info.Comments

	out.VideoID = info.ID
	out.Title = info.Title
	out.Duration = info.Duration
	out.Total = len(comments)

	// **並びは「関連度の高い順」に寄せる。** 既存の取得経路は YouTube API を
	// `order=relevance` で叩いており、抽出規則はその並びの前提で調整されている。
	// info.json の並びは取得順なので、固定と いいね数で近づける。
	idx := make([]int, len(comments))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := comments[idx[a]], comments[idx[b]]
		if x.IsPinned != y.IsPinned {
			return x.IsPinned
		}
		lx, ly := 0, 0
		if x.LikeCount != nil {
			lx = *x.LikeCount
		}
		if y.LikeCount != nil {
			ly = *y.LikeCount
		}
		return lx > ly
	})

	// **返信も入れる。** 既存経路は top-level しか取らないが、こちらは
	// 「他に取りようが無い」配信のための口なので、落として歌単を取り逃すより
	// 入れて構造フィルタに任せるほうがよい。件数は分けて返し、画面で見せる。
	texts := make([]string, 0, len(comments))
	for _, i := range idx {
		c := comments[i]
		if c.Parent == "" || c.Parent == "root" {
			out.TopLevel++
		}
		t := strings.TrimSpace(c.Text)
		if t == "" {
			continue
		}
		texts = append(texts, t)
		// 判定は抽出そのものと同じ関数を通す（自前で正規表現を持つと、
		// 抽出規則を変えたときに画面の件数だけ古くなる）。
		if comment.HasTimestampLines([]string{t}) {
			out.WithTimes++
		}
	}
	out.Saved = len(texts)
	if out.Saved == 0 {
		// 本文が全部空。取り込んでも `comment_raw` は空配列になるだけで、
		// **既存を潰さない歯止めに当たって黙って何も起きない**。そう言う。
		return out, nil, fmt.Errorf("%w（本文のあるコメントが 1 件もありません）", ErrInfoJSONNoComments)
	}
	return out, texts, nil
}
