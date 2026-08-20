package handler

import (
	"net/http"
	"testing"

	"github.com/ruifan75/setori/pkg/auth"
)

// 配信まわりの GET は「安全メソッドは公開」の既定に落ちるが、解析素材を返す 3 本だけは
// 例外にしている。キャッシュが無いと運用者の資格情報（API キー・メンバー cookie）で
// 外部へ取りに行くため、未ログインから叩けてはいけない。
//
// 既定に戻ると匿名リクエストがそれを使えるようになるので、ここで固定する。
func TestStreamAnalysisEndpointsRequireContentEdit(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		path      string
		wantPerm  string
		wantLogin bool
	}{
		// 外部フェッチを起こす GET（本題）
		{"生コメント", http.MethodGet, "/api/streams/abc123/comments", auth.PermContentEdit, true},
		{"チャプター（yt-dlp + cookie）", http.MethodGet, "/api/streams/abc123/chapters", auth.PermContentEdit, true},
		{"Holodex 楽曲", http.MethodGet, "/api/streams/abc123/holodex-songs", auth.PermContentEdit, true},

		// 一括処理は進捗も含めて編集権限（status に非表示配信の題名と ID が載る）
		{"一括分析の開始", http.MethodPost, "/api/streams/batch-analyze", auth.PermContentEdit, true},
		{"一括分析の進捗", http.MethodGet, "/api/streams/batch-analyze/status", auth.PermContentEdit, true},
		{"一括作成の進捗", http.MethodGet, "/api/streams/batch-fill/status", auth.PermContentEdit, true},

		// 巻き込んでいないこと：配信そのものの閲覧は公開のまま
		{"配信一覧", http.MethodGet, "/api/streams", "", false},
		{"配信詳細", http.MethodGet, "/api/streams/abc123", "", false},
		{"配信検索", http.MethodGet, "/api/streams/search", "", false},
		{"楽曲詳細", http.MethodGet, "/api/songs/abc123", "", false},

		// 書き込み側は元から content:edit（既定で拾えていることの確認）
		{"コメント再取得", http.MethodPost, "/api/streams/abc123/comments/sync-youtube", auth.PermContentEdit, true},
		{"チャプター再取得", http.MethodPost, "/api/streams/abc123/chapters/sync", auth.PermContentEdit, true},

		// ── ここから、判定を「語尾」「単純 prefix」で書いたときに壊れる形 ──
		//
		// 認可は ServeMux より前に path 文字列だけで決まるので、書き方をゆるめると
		// 将来ルートを足したときに黙って巻き込む／素通りする。どちらも気付きにくい。

		// 近い名前の別ルート：prefix だけで見ると巻き込まれ、公開のはずが 401 になる
		{"近い名前の別ルート(analyze)", http.MethodGet, "/api/streams/batch-analyze-report", "", false},
		{"近い名前の別ルート(fill)", http.MethodGet, "/api/streams/batch-fill-summary", "", false},

		// {id} の位置に何が来ても chapters ルートには違いないので、保護されるのが正しい
		// （ServeMux も /api/streams/{id}/chapters に id="search" として dispatch する）
		{"ID が変わっても chapters", http.MethodGet, "/api/streams/search/chapters", auth.PermContentEdit, true},
		// 資源名が ID の位置に来た形は配信詳細であって解析素材ではない
		{"ID の位置に資源名", http.MethodGet, "/api/streams/comments", "", false},

		// 語尾だけで見ると逆に漏れる形（サブリソースを足したとき）
		{"サブリソース", http.MethodGet, "/api/streams/abc123/comments/raw", auth.PermContentEdit, true},
		{"末尾スラッシュ", http.MethodGet, "/api/streams/abc123/comments/", auth.PermContentEdit, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perm, login := requiredPermission(tc.method, tc.path)
			if perm != tc.wantPerm || login != tc.wantLogin {
				t.Errorf("%s %s = (%q,%v), want (%q,%v)",
					tc.method, tc.path, perm, login, tc.wantPerm, tc.wantLogin)
			}
		})
	}
}
