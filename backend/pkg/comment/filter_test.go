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

// TestFilterSongsHeadingLikeNonSong は「配信の目次」を落とす規則と、
// **それに似ているが実在する曲名は落とさない**ことを固定する。
//
// 落とす側の由来は本番の未照合 1299 件（アーティスト欄が空のもの）、
// 残す側の由来は本番の songs 819 件。この 2 つを突き合わせて、
// 巻き添えが 0 件だった規則だけを採用している。
//
// ここが緩むと「曲を取りこぼす」方向に壊れる。雑音が 1 行増えるのは目に見えるが、
// 曲が 1 行消えるのは誰も気づかないので、こちらの方が高くつく。
func TestFilterSongsHeadingLikeNonSong(t *testing.T) {
	tests := []struct {
		name     string
		song     ParsedSong
		filtered bool
	}{
		// --- 落とす：配信の目次 ---
		{"〜の件", ParsedSong{Name: "ぱんt警察の件"}, true},
		{"〜の話", ParsedSong{Name: "石油王の船盛特典の話"}, true},
		{"〜の巻", ParsedSong{Name: "着せ替え大狂いの巻"}, true},
		{"〜の瞬間", ParsedSong{Name: "10万人達成の瞬間"}, true},
		{"〜の経緯", ParsedSong{Name: "歌枠リレー開催の経緯"}, true},
		{"〜旨", ParsedSong{Name: "ぱんt警察をやめろとは思っていない旨"}, true},
		{"末尾の感嘆符は無視して判定", ParsedSong{Name: "10万人達成の瞬間!"}, true},
		{"全体が丸括弧", ParsedSong{Name: "(ここの揺れだいすき)"}, true},
		{"全体が全角丸括弧", ParsedSong{Name: "（芳澤SHOWTIME 正妻やん）"}, true},
		{"末尾以外に句点がある", ParsedSong{Name: "食べてるもの紹介。お母さんが盛り付けてくれた"}, true},

		// --- 残す：実在する曲名 ---
		{"句点で終わる曲名", ParsedSong{Name: "らしく。", OriginalArtist: "稀羽すう"}, false},
		{"読点を含む曲名", ParsedSong{Name: "琥珀色の街、上海蟹の朝", OriginalArtist: "くるり"}, false},
		{"終助詞で終わる曲名", ParsedSong{Name: "死ぬのがいいわ", OriginalArtist: "藤井風"}, false},
		{"です で終わる曲名", ParsedSong{Name: "恋?で愛?で暴君です!"}, false},
		{"30文字の曲名", ParsedSong{Name: "One more time, One more chance"}, false},
		{"括弧付きのバージョン表記", ParsedSong{Name: "Starry night (instrumental)"}, false},
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
