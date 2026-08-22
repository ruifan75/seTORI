package service

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ruifan75/setori/internal/logger"
)

// ytdlpRunner は yt-dlp を呼ぶ側が共有する実行環境（バイナリの場所と cookie）。
//
// **インスタンスは 1 つだけ作って使い回すこと。** cookie は管理→設定から
// 再起動なしで差し替えられる（`SettingsService.OnChange` → `SetCookies`）ので、
// 利用者ごとに別の runner を持つと、片方だけ古い cookie のまま残る。
// 実際 live chat とチャプターは同じ BOT 判定に一緒に当たるので、
// 「チャットは通るのにチャプターだけ落ちる」という切り分けの難しい壊れ方になる。
type ytdlpRunner struct {
	path string

	mu         sync.RWMutex
	cookieData string // cookies.txt の中身（管理画面の設定 / YTDLP_COOKIES_FILE 由来）
}

func newYtdlpRunner(path string) *ytdlpRunner {
	if path == "" {
		path = "yt-dlp"
	}
	// yt-dlp が無いと live chat もチャプターも全配信で使えなくなる。配信ごとの警告からは
	// 「その配信に無い」のか「そもそも実行できていない」のか読み取れないので、
	// 起動時に一度だけ切り分けて残す。
	if _, err := exec.LookPath(path); err != nil {
		logger.Warnf("[ytdlp] yt-dlp が見つかりません (%s): 拍手による end 推定とチャプターの取得は全配信でスキップされます。"+
			"インストールしてください（本番イメージは backend/Dockerfile に同梱）", path)
	}
	return &ytdlpRunner{path: path}
}

// SetCookies は管理画面で設定された cookies.txt の中身を差し替える（再起動なしで効く）。
func (r *ytdlpRunner) SetCookies(data string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cookieData = strings.TrimSpace(data)
}

// HasCookies は cookie が設定されているかを返す（失敗時の案内を出し分けるため）。
func (r *ytdlpRunner) HasCookies() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cookieData != ""
}

// prepareCookies は yt-dlp に渡す cookie ファイルを用意し、後始末の関数を返す。
// 設定は中身で持っているので毎回一時ファイルへ書き出す。yt-dlp は終了時に
// cookie をファイルへ書き戻すため、共有の実ファイルを直接渡すと
// 読み取り専用マウントや backfill の並列実行で壊れる。未設定なら空文字を返す。
func (r *ytdlpRunner) prepareCookies() (string, func()) {
	noop := func() {}

	r.mu.RLock()
	data := r.cookieData
	r.mu.RUnlock()

	if data == "" {
		return "", noop
	}

	tmp, err := os.CreateTemp("", "setori-cookies-*.txt")
	if err != nil {
		logger.Warnf("[ytdlp] cookie の一時ファイルを作れません: %v", err)
		return "", noop
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(data + "\n"); err != nil {
		tmp.Close()
		os.Remove(name)
		logger.Warnf("[ytdlp] cookie の書き出しに失敗しました: %v", err)
		return "", noop
	}
	tmp.Close()
	return name, func() { os.Remove(name) }
}

// notInstalled は「yt-dlp そのものを実行できなかった」かを見る。
// PATH 上の名前なら ErrNotFound、絶対パスなら fs.ErrNotExist で返る。
func notInstalled(runErr error) bool {
	return errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, fs.ErrNotExist)
}

// isBotCheck は yt-dlp の出力が YouTube の BOT 判定かどうかを見る。
func isBotCheck(stderr string) bool {
	return strings.Contains(stderr, "Sign in to confirm you") ||
		strings.Contains(stderr, "confirm you’re not a bot") ||
		strings.Contains(stderr, "confirm you're not a bot")
}

// ytdlpErrorLine は stderr から原因の行だけを取り出す（ログを 1 行に収めるため）。
func ytdlpErrorLine(stderr string) string {
	line := ""
	for _, l := range strings.Split(stderr, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "ERROR:") {
			line = l // ERROR 行があればそれを優先（最後のものを採る）
		} else if line == "" {
			line = l
		}
	}
	if line == "" {
		return "(stderr なし)"
	}
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}

// botCheckError は BOT 判定の案内文を作る（cookie の有無で対処が違う）。
func botCheckError(r *ytdlpRunner) error {
	if r.HasCookies() {
		return errors.New("YouTube に BOT 判定されました: 設定済みの cookie が失効している可能性があります（管理→設定で入れ直してください）")
	}
	return errors.New("YouTube に BOT 判定されました: 管理→設定の「YouTube cookie」に cookies.txt を登録してください")
}

// ── 再生可否（availability）を既存の実行に相乗りさせる ───────────────────────
//
// 会限の歌枠にセットリストを作れるようにするための判定材料。詳細は issue #3。
//
// **`--print` は `--simulate` を含む。** live chat のように**ファイルを書く**実行に
// そのまま足すと、yt-dlp は何も書かずに終わる（実測：追加しただけで
// live_chat.json が作られなくなった）。落ちも警告も出ず、
// 「この配信に live chat replay がありません」になるだけなので気付けない。
// ファイルを書く実行には必ず `--no-simulate` を添えること。
//
// 出力は 1 行、タブ区切りで availability と playable_in_embed。取れない値は "NA"。
const (
	availabilityPrintTemplate = "%(availability)s\t%(playable_in_embed)s"
	ytdlpFieldMissing         = "NA"
)

// ytdlpAvailability は yt-dlp が返した再生可否。どちらも「取れなかった」を持てる。
type ytdlpAvailability struct {
	Availability    string // public / subscriber_only / unlisted / premium_only …。空＝取れなかった
	PlayableInEmbed sql.NullBool
}

// Resolved は**動画情報を最後まで取れたか**。
//
// `availability` の有無では判定できない。`--ignore-no-formats-error` を付けた実行では、
// YouTube が再生を断っても（削除・レート制限）yt-dlp は警告だけ出して**終了コード 0 で続行**し
// （`raise_no_formats(expected=True)` → `report_warning`）、部分的なメタデータから
// `availability = public` を出してしまう。実測でも視聴不可の hVfDBfreYNI が
// `public|NA` を返した。
//
// 一方 `playable_in_embed` は抽出が最後まで通ったときにだけ埋まるので、これを
// 「信用してよいか」の目印にする。相乗り経路はこれが false なら**保存しない**
// ── そこで保存すると、レート制限に当たっただけの公開配信が unavailable として
// 恒久的に記録され、二度と再試行されない。
func (a ytdlpAvailability) Resolved() bool {
	return a.PlayableInEmbed.Valid
}

// availabilityArgs は再生可否を拾うための引数を返す。
// writesFile が true の実行（live chat のダウンロードなど）には --no-simulate を添える。
func availabilityArgs(writesFile bool) []string {
	args := []string{"--print", availabilityPrintTemplate}
	if writesFile {
		args = append(args, "--no-simulate")
	}
	return args
}

// parseYtdlpAvailability は --print の 1 行を読む。
//
// **`--ignore-no-formats-error` が付いていると、視聴できない動画でも
// availability が "public" で返る**（実測：削除済みの hVfDBfreYNI が
// フラグ有りで public、無しで "Video unavailable" のエラー）。
// 本番の両経路はこのフラグを常用しているので、availability 単独は信用できない。
// 一方 playable_in_embed はそのとき "NA" になるため、
// 「動画情報を最後まで取れたか」の判定はこちらで行う。
func parseYtdlpAvailability(line string) ytdlpAvailability {
	var a ytdlpAvailability
	parts := strings.Split(strings.TrimSpace(line), "\t")
	if len(parts) > 0 && parts[0] != ytdlpFieldMissing {
		a.Availability = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		switch strings.TrimSpace(parts[1]) {
		case "True":
			a.PlayableInEmbed = sql.NullBool{Bool: true, Valid: true}
		case "False":
			a.PlayableInEmbed = sql.NullBool{Bool: false, Valid: true}
		}
	}
	return a
}

// lastNonEmptyLine は stdout の最後の非空行を返す。
// yt-dlp は --print を指定した順に 1 行ずつ出すので、availability を最後に
// 足しておけば、他の --print（チャプター）や余分な出力と混ざらない。
func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// firstNonEmptyLine は stdout の最初の非空行を返す。
func firstNonEmptyLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
