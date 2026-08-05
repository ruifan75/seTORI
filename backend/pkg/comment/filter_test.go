package comment

import "testing"

// TestFilterSongsStructuralNonSong は、キーワード辞書では捕まえきれない
// 「非曲行」を構造ルール（絵文字のみ／引用符／罫線／過長）で除外できることを確認する。
// 各ケースの由来は歌枠コメントの実データ（ground truth と突き合わせて確定した false positive）。
func TestFilterSongsStructuralNonSong(t *testing.T) {
	tests := []struct {
		name     string
		song     ParsedSong
		filtered bool // true = 非曲として除外されるべき
	}{
		{
			name:     "絵文字のみの歌名",
			song:     ParsedSong{Start: 1058, Name: "📸", OriginalComment: "┗ 0:17:38 📸"},
			filtered: true,
		},
		{
			name:     "記号のみの歌名",
			song:     ParsedSong{Start: 1580, Name: "???", OriginalComment: "26:20 ???"},
			filtered: true,
		},
		{
			name:     "実況メモ（トピック「発言」）",
			song:     ParsedSong{Start: 2340, Name: "挨拶運動 Take1 「みんな、ただいまー!!!間違えた」", OriginalComment: "┣00:39:00 挨拶運動 Take1 「みんな、ただいまー!!!間違えた」"},
			filtered: true,
		},
		{
			name:     "罫線プレフィックスのネスト注釈",
			song:     ParsedSong{Start: 914, Name: "不仲営業", OriginalComment: "┗00:15:14 不仲営業 「空気悪いみたい今日」"},
			filtered: true,
		},
		{
			name:     "過長な歌名（感想文の丸ごと取り込み）",
			song:     ParsedSong{Start: 100, Name: "ここの高音が綺麗すぎて痺れて涙が出そうになったこの瞬間を一生忘れないと思う本当にありがとう", OriginalComment: "1:40 ここの高音が…"},
			filtered: true,
		},
		{
			name:     "keep 語(歌ってみた)を含む実況メモも構造ルールが優先して除外",
			song:     ParsedSong{Start: 4364, Name: "告知⑦💚「ド屑」歌ってみた", OriginalComment: "1:12:44 告知⑦💚「ド屑」歌ってみた"},
			filtered: true,
		},
		// --- 正しい曲は残すこと（false negative を出さない）---
		{
			name:     "通常の曲（歌名/歌手）",
			song:     ParsedSong{Start: 603, Name: "Lemon", OriginalArtist: "米津玄師", OriginalComment: "0:10:03 Lemon / 米津玄師"},
			filtered: false,
		},
		{
			name:     "作品情報の『』は歌名欄に無いので残す",
			song:     ParsedSong{Start: 910, Name: "風になる", OriginalArtist: "つじあやの", OriginalComment: "0:15:10 風になる / つじあやの\t「映画 猫の恩返し」"},
			filtered: false,
		},
		{
			name:     "記号混じりでもかな/漢字があれば残す",
			song:     ParsedSong{Start: 3947, Name: "愛♡スクリ～ム!", OriginalArtist: "AiScReam", OriginalComment: "1:05:47 愛♡スクリ～ム! / AiScReam"},
			filtered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterSongs([]ParsedSong{tt.song}, nil, nil)
			removed := len(got) == 0
			if removed != tt.filtered {
				t.Fatalf("FilterSongs removed=%v, want filtered=%v (name=%q)", removed, tt.filtered, tt.song.Name)
			}
		})
	}
}
