// Package streamtag は初回表示判定と見直しで使う共通のタグ語彙。
package streamtag

const ShortFormMaxDurationSeconds = 180

// MusicIDs は通常一覧へ出す音楽系タグ。呼び出し側で語彙を書き換えないようコピーを返す。
func MusicIDs() []string {
	return []string{"concert", "karaoke", "music_cover", "mv", "original_song", "singing"}
}
