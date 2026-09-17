package repository

import (
	"strings"
	"testing"
)

// **押し出す面（おすすめ・プリセット）は、一覧に出していないチャンネルの
// 歌唱を出さない。**
//
// #59 で `/streams` とタグ別一覧に入れた判定を、歌唱を押し出す側にも通す。
// 入れないと、チャンネル一覧に出していないチャンネルの歌枠の曲が
// **ホームのいちばん上**に出る（実測：母集合 5563 件に対し 23 件、
// うち 14 曲は他に歌唱が無いので**この経路でしか出てこない**）。
func TestPromotedClauseUsesChannelVisibility(t *testing.T) {
	want := " AND " + VisibleChannelExpr("st")
	if got := promotedClause(); got != want {
		t.Errorf("promotedClause() = %q\nwant %q", got, want)
	}
}

// **実際に発行された SQL で見る。** 条件を関数に切り出しても、
// 呼び出し側が使うのをやめれば守れない（PR #59 / #67 で同じ形の指摘を受けた）。
func TestPromotedSurfacesApplyChannelScope(t *testing.T) {
	cases := []struct {
		name string
		call func(*PerformanceRepository)
	}{
		{
			name: "おすすめ（FindRandom）",
			call: func(r *PerformanceRepository) { r.FindRandom(10, nil, PublicAccess) },
		},
		{
			name: "プリセット（FindByPreset）",
			call: func(r *PerformanceRepository) { r.FindByPreset(PresetFilter{}, 10, PublicAccess) },
		},
		{
			name: "プリセットの ID だけ（FindIDsByPreset）",
			call: func(r *PerformanceRepository) { r.FindIDsByPreset(PresetFilter{}, 10, PublicAccess) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newRecordingDB(t)
			tc.call(NewPerformanceRepository(db))

			queries := rec.all()
			if len(queries) == 0 {
				t.Fatal("SQL が発行されていない")
			}
			want := strings.Join(strings.Fields(VisibleChannelExpr("st")), " ")
			for _, q := range queries {
				norm := strings.Join(strings.Fields(q), " ")
				// 歌手・タグの付随クエリは対象外（歌唱を選ぶ本体だけを見る）。
				if !strings.Contains(norm, "FROM performances p") {
					continue
				}
				if !strings.Contains(norm, want) {
					t.Errorf("チャンネルの判定が入っていない:\n%s", norm)
				}
				// **AND で繋がれていること。** `OR` だと条件が無効になる。
				i := strings.Index(norm, want)
				before := strings.TrimSpace(norm[:i])
				if !strings.HasSuffix(before, "AND") {
					tail := before
					if len(tail) > 48 {
						tail = "…" + tail[len(tail)-48:]
					}
					t.Errorf("AND で繋がれていない（直前: %q）", tail)
				}
			}
		})
	}
}

// **歌手ページには通さない。** 非表示チャンネルのページは未ログインでも
// 開ける設計（CLAUDE.md §3、既存ページからのリンクが 404 にならないように）。
// 通すと自分の配信が 0 件のページになる。
func TestSingerPageIsNotChannelScoped(t *testing.T) {
	db, rec := newRecordingDB(t)
	NewPerformanceRepository(db).FindBySingerID("UC-x", 10, 0, "", "", PublicAccess)

	want := strings.Join(strings.Fields(VisibleChannelExpr("st")), " ")
	for _, q := range rec.all() {
		norm := strings.Join(strings.Fields(q), " ")
		if strings.Contains(norm, want) {
			t.Error("歌手ページにチャンネルの判定が入っている（非表示チャンネルのページが 0 件になる）")
		}
	}
}
