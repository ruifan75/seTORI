package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStreamTagIDForHolodexTopic(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "original song", topic: "Original_Song", want: "original_song"},
		{name: "music cover", topic: "Music_Cover", want: "music_cover"},
		{name: "singing", topic: "singing", want: "singing"},
		{name: "karaoke case insensitive", topic: "Karaoke", want: "karaoke"},
		{name: "live", topic: "Live", want: "concert"},
		{name: "music video", topic: "Music_Video", want: "mv"},
		// 同名だが**表に明記した**もの。以前は「表に無いが同じ ID かもしれない」
		// 経路で通っていた。
		{name: "shorts is explicit", topic: "shorts", want: "shorts"},
		// 2026-08-29 に追加。抜けていて FK に当たらず捨てられていたもの。
		{name: "members only", topic: "membersonly", want: "members_only"},
		{name: "3d stream", topic: "3D_Stream", want: "3d"},
		{name: "birthday", topic: "Birthday", want: "birthday"},
		{name: "anniversary", topic: "Anniversary", want: "anniversary"},
		// **表に無いものは試さない。** 原文のまま渡すと FK エラーになり、
		// それを握りつぶしていたせいで membersonly の取りこぼしに気付けなかった。
		{name: "unknown topic", topic: "Outfit_Reveal", want: ""},
		{name: "game title", topic: "Persona", want: ""},
		{name: "trim spaces", topic: "  Original_Song  ", want: "original_song"},
		{name: "empty", topic: "  ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamTagIDForHolodexTopic(tt.topic); got != tt.want {
				t.Fatalf("streamTagIDForHolodexTopic(%q) = %q, want %q", tt.topic, got, tt.want)
			}
		})
	}
}

// 表に書いた tag_id が stream_tags に無いと、その topic の配信は同期のたびに
// FK エラーになる（付与だけ失敗して警告が出る）。実際 `membersonly` が
// この状態で、握りつぶされていたので誰も気付かなかった。
//
// タグは 001 / 038 / 039 / 057 に分かれて投入される。**本番で画面から作った
// タグは migration に載らない**ので、本番の stream_tags を見て表に足すと
// 手元では動いて新しい環境だけで壊れる（`3d` が実際にその状態だった）。
// migration が入れる集合と突き合わせる。
func TestHolodexTopicAliasesExistInMigrations(t *testing.T) {
	dir := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	rowID := regexp.MustCompile(`\(\s*'([a-zA-Z_0-9]+)'`)
	collect := func(body, stmt string) []string {
		var ids []string
		for _, chunk := range strings.Split(body, stmt)[1:] {
			// 次の文（;）までを対象にする。
			if i := strings.Index(chunk, ";"); i >= 0 {
				chunk = chunk[:i]
			}
			for _, m := range rowID.FindAllStringSubmatch(chunk, -1) {
				ids = append(ids, m[1])
			}
		}
		return ids
	}

	pairRe := regexp.MustCompile(`\(\s*'([a-zA-Z_0-9]+)'\s*,\s*'([^']+)'`)
	collectPairs := func(body, stmt string) [][2]string {
		var out [][2]string
		for _, chunk := range strings.Split(body, stmt)[1:] {
			if i := strings.Index(chunk, ";"); i >= 0 {
				chunk = chunk[:i]
			}
			for _, m := range pairRe.FindAllStringSubmatch(chunk, -1) {
				out = append(out, [2]string{m[1], m[2]})
			}
		}
		return out
	}

	seeded := map[string]string{}     // tag_id -> 最初に投入した migration
	rules := map[[2]string][]string{} // (tag_id, keyword) -> 宣言した migration 群
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, id := range collect(string(body), "INSERT INTO stream_tags") {
			if _, ok := seeded[id]; !ok {
				seeded[id] = e.Name()
			}
		}
		for _, kv := range collectPairs(string(body), "INSERT INTO tag_keyword_rules") {
			rules[kv] = append(rules[kv], e.Name())
		}
	}
	if len(seeded) == 0 || len(rules) == 0 {
		t.Fatal("migrations から投入を読み取れなかった（この検査自体が壊れている）")
	}

	for topic, tagID := range holodexTopicTagAliases {
		if _, ok := seeded[tagID]; !ok {
			t.Errorf("topic %q -> tag %q: migration が stream_tags に入れていない。"+
				"新しい DB では付与が必ず失敗する（本番に手作りしたタグを表へ足していないか）", topic, tagID)
		}
	}

	// タイトル規則の側も同じ穴を持つ。020 は「参照先の stream_tags が存在する
	// ものだけ」入れるので、タグを入れ忘れると**規則が黙って入らない**。
	// 実際 home3D / opening / relay がその状態で、`3d` だけ足したときに
	// 「おうち3D に 3d しか付かず 3D preset へ混入する」形で表面化した。
	//
	// **順序まで見る。** 「どこかで宣言」「どこかで投入」の集合比較では足りない
	// ── 空 DB は番号順に流れるので、タグより前に宣言された規則はその時点で
	// スキップされ、あとからタグだけ入れても規則は入らないままになる。
	// ファイル名は 0 埋めの連番なので、辞書順＝実行順。
	for kv, decls := range rules {
		tagID, keyword := kv[0], kv[1]
		seedFile, ok := seeded[tagID]
		if !ok {
			t.Errorf("tag %q: %s がタイトル規則 %q を宣言しているが、どの migration も "+
				"stream_tags に入れていない。新しい DB ではその規則が黙ってスキップされる",
				tagID, decls[0], keyword)
			continue
		}
		effective := false
		for _, d := range decls {
			if d >= seedFile {
				effective = true
				break
			}
		}
		if !effective {
			t.Errorf("tag %q の規則 %q は %s でしか宣言されておらず、タグの投入は %s。"+
				"空 DB では規則の時点でタグが無く、黙ってスキップされる（タグ投入と同時か、後で入れ直すこと）",
				tagID, keyword, decls, seedFile)
		}
	}
}
