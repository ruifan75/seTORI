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

// innerWhere は SQL から WHERE 句を取り出す。**CTE の中にあるものも拾う。**
//
// `whereClause`（stream_scope_sql_test.go）は最外層しか見ないが、ここで見たい
// WHERE は `WITH … AS ( … )` の内側にある。開始位置の括弧の深さを覚えておき、
// **その深さで `ORDER BY` が来るか、深さが下がった時点**で終端とする。
func innerWhere(t *testing.T, sqlText string) string {
	t.Helper()
	norm := strings.Join(strings.Fields(sqlText), " ")
	upper := strings.ToUpper(norm)

	start, startDepth, depth := -1, 0, 0
	for i := 0; i < len(norm); i++ {
		switch norm[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if start < 0 && strings.HasPrefix(upper[i:], "WHERE ") && (i == 0 || norm[i-1] == ' ') {
			start, startDepth = i, depth
			continue
		}
		if start < 0 {
			continue
		}
		// 開始と同じ深さで ORDER BY が来たら終わり。
		if depth == startDepth && strings.HasPrefix(upper[i:], " ORDER BY ") {
			return strings.TrimSpace(norm[start:i])
		}
		// 深さが下がった＝この WHERE を含む括弧が閉じた。
		if depth < startDepth {
			return strings.TrimSpace(norm[start:i])
		}
	}
	if start < 0 {
		return ""
	}
	return strings.TrimSpace(norm[start:])
}

// **発行された WHERE を、実装から独立した期待値と完全一致で突き合わせる。**
//
// ここに至るまでに 2 回失敗している:
//
//  1. 条件の存在と直前の `AND` だけを見る → 後ろに `= FALSE` を足す改変が通る
//  2. 条件を組み立てる関数（`randomWhere` / `presetWhere`）の出力と比べる →
//     **その関数を書き換えると期待値も一緒に変わる**ので、中身の改変が通る。
//     しかも部分一致なので、呼び出し側で末尾に `OR TRUE` を足す改変も通る
//
// そこで期待値は**この関数の中で組み立て**、**終端まで完全一致**させる。
// 使ってよいのは別のテストで固定されている部品だけ（`VisibleChannelExpr` は
// `TestVisibleChannelExpr`、秘匿の 2 軸は `TestDiscoverableForDropsFilterOnlyForRestrictedView`)。
func TestPromotedSurfacesApplyChannelScope(t *testing.T) {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	scope := func(a ViewerAccess) string {
		return "WHERE TRUE" + a.discoverClause() + " AND " + VisibleChannelExpr("st")
	}
	// **メン限・アーカイブなしを除く条件は押し出す面で共通。**
	excludeHidden := func(alias string) string {
		return " AND NOT EXISTS ( SELECT 1 FROM stream_stream_tags " + alias +
			" WHERE " + alias + ".stream_id = st.id AND " + alias +
			".tag_id IN ('members_only', 'unarchived') )"
	}

	wantRandom := func(a ViewerAccess) string {
		return scope(a) + " AND NOT (p.song_id = ANY($2::uuid[]))" + excludeHidden("sst")
	}
	wantPreset := func(a ViewerAccess) string {
		return scope(a) + excludeHidden("hid") +
			" AND ($1 = '' OR EXISTS ( SELECT 1 FROM performance_singers fs" +
			" WHERE fs.performance_id = p.id AND fs.singer_id = $1 ))" +
			" AND (cardinality($2::text[]) = 0 OR EXISTS ( SELECT 1 FROM stream_stream_tags inc" +
			" WHERE inc.stream_id = st.id AND inc.tag_id = ANY($2::text[]) ))" +
			" AND NOT EXISTS ( SELECT 1 FROM stream_stream_tags exc" +
			" WHERE exc.stream_id = st.id AND exc.tag_id = ANY($3::text[]) )" +
			" AND (NOT $4 OR ( SELECT count(*) FROM performance_singers ms" +
			" WHERE ms.performance_id = p.id ) > 1)"
	}

	paths := []struct {
		name string
		call func(*PerformanceRepository, ViewerAccess)
		want func(ViewerAccess) string
	}{
		{"おすすめ（FindRandom）", func(r *PerformanceRepository, a ViewerAccess) { r.FindRandom(10, nil, a) }, wantRandom},
		{"プリセット（FindByPreset）", func(r *PerformanceRepository, a ViewerAccess) { r.FindByPreset(PresetFilter{}, 10, a) }, wantPreset},
		{"プリセットの ID（FindIDsByPreset）", func(r *PerformanceRepository, a ViewerAccess) { r.FindIDsByPreset(PresetFilter{}, 10, a) }, wantPreset},
		// **件数も通すこと。** 一覧から落として件数に残ると、何件伏せたかが残る。
		{"プリセットの件数（CountByPreset）", func(r *PerformanceRepository, a ViewerAccess) { r.CountByPreset(PresetFilter{}, a) }, wantPreset},
	}

	// **両方の権限で見る。** 片方だけだと「RestrictedView のときだけ条件を外す」
	// 改変が通る ── この PR の「権限で緩めない」という判断がそこで崩れる。
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		for _, p := range paths {
			t.Run(p.name+"/"+accessName(access), func(t *testing.T) {
				db, rec := newRecordingDB(t)
				p.call(NewPerformanceRepository(db), access)

				want := norm(p.want(access))
				found := false
				for _, q := range rec.all() {
					if !strings.Contains(q, "FROM performances p") {
						continue // 歌手・タグの付随クエリは対象外
					}
					found = true
					if got := innerWhere(t, q); got != want {
						t.Errorf("WHERE が期待と違う\n got: %s\nwant: %s", got, want)
					}
				}
				if !found {
					t.Fatalf("歌唱を選ぶ SQL が発行されていない: %q", rec.all())
				}
			})
		}
	}
}

func accessName(a ViewerAccess) string {
	if a == RestrictedView {
		return "RestrictedView"
	}
	return "PublicAccess"
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
