package handler

import "net/http"

// ビルド時に埋め込まれる稼働中のバージョン。
// 値は main パッケージの変数へ -ldflags で入れ、SetBuildInfo で渡ってくる
// （ldflags で書き込めるのは文字列変数だけなので、main で受けてから注入する）。
// go run で動かしているときは "dev" のまま。
var (
	buildCommit = "dev"
	buildTime   = ""
)

// SetBuildInfo は main から一度だけ呼ぶ。空文字は無視して既定値を残す。
func SetBuildInfo(commit, builtAt string) {
	if commit != "" {
		buildCommit = commit
	}
	if builtAt != "" {
		buildTime = builtAt
	}
}

// handleVersion は稼働中のビルドを返す。
//
// 主目的はブラウザを開かずに確認できること（`curl https://<ドメイン>/api/version`）。
// デプロイは compose のレイヤーキャッシュが効くため、バックエンドだけ・
// フロントだけが入れ替わることがある。突き合わせられるように両方が同じ
// commit を持ち、フロントは自分の値とこの値を比べて食い違いを警告する。
func (r *Router) handleVersion(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"commit":   buildCommit,
		"built_at": buildTime,
	})
}
