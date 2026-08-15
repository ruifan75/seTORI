package service

import (
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
