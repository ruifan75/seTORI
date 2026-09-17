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

// **発行された WHERE 全体を期待値と突き合わせる。**
//
// 条件の存在と直前の `AND` だけを見る形では、次のどれも素通りする
// （レビューで実証された）:
//
//   - 呼び出し側で条件の後ろに `= FALSE` を足す … 判定が反転する
//   - `CountByPreset` だけ条件を除去          … 件数と一覧が食い違う
//   - `RestrictedView` のときだけ除去          … 「権限で緩めない」が崩れる
//
// **4 経路 × 2 権限**を対象にし、条件を組み立てている関数（`randomWhere` /
// `presetWhere`）の出力と丸ごと比べる。期待値をテストに書き写さないので、
// 実装を変えた人は必ずこの関数を通る。
func TestPromotedSurfacesApplyChannelScope(t *testing.T) {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	paths := []struct {
		name string
		call func(*PerformanceRepository, ViewerAccess)
		want func(ViewerAccess) string
	}{
		{
			name: "おすすめ（FindRandom）",
			call: func(r *PerformanceRepository, a ViewerAccess) { r.FindRandom(10, nil, a) },
			want: randomWhere,
		},
		{
			name: "プリセット（FindByPreset）",
			call: func(r *PerformanceRepository, a ViewerAccess) { r.FindByPreset(PresetFilter{}, 10, a) },
			want: presetWhere,
		},
		{
			name: "プリセットの ID（FindIDsByPreset）",
			call: func(r *PerformanceRepository, a ViewerAccess) { r.FindIDsByPreset(PresetFilter{}, 10, a) },
			want: presetWhere,
		},
		{
			// **件数も通すこと。** 一覧から落として件数に残ると、何件伏せたかが残る。
			name: "プリセットの件数（CountByPreset）",
			call: func(r *PerformanceRepository, a ViewerAccess) { r.CountByPreset(PresetFilter{}, a) },
			want: presetWhere,
		},
	}

	// **両方の権限で見る。** 片方だけだと「RestrictedView のときだけ条件を外す」
	// 改変が通る ── この PR の「権限で緩めない」という判断がそこで崩れる。
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		for _, p := range paths {
			t.Run(p.name+"/"+accessName(access), func(t *testing.T) {
				db, rec := newRecordingDB(t)
				p.call(NewPerformanceRepository(db), access)

				// **条件の文字列が丸ごと現れること**を見る。
				// 断片の有無ではなく全体なので、後ろに `= FALSE` を足す／
				// 一部を書き換える、といった改変は一致しなくなる。
				// （WHERE は CTE の内側にあることが多く、最外層を取る
				// `whereClause` では届かないので contains で見る）
				want := norm(p.want(access))
				// **実装の関数に依存しない斡定。** 上の want は条件を組み立てている
				// 関数から作るので、**その関数を書き換えると期待値も一緒に変わり**、
				// 中身の改変を検出できない（実際 `= FALSE` を足す改変と
				// `RestrictedView` だけ外す改変がそれで通った）。
				// そこで、判定が**完全な合取項として**現れることを直接見る ──
				// 前後が ` AND ` なら、後ろに比較を足したり丸ごと外したりできない。
				conjunct := norm(" AND " + VisibleChannelExpr("st") + " AND ")

				found := false
				for _, q := range rec.all() {
					if !strings.Contains(q, "FROM performances p") {
						continue // 歌手・タグの付随クエリは対象外
					}
					found = true
					got := norm(q)
					if !strings.Contains(got, conjunct) {
						t.Errorf("チャンネルの判定が合取項として入っていない\nwant 部分列: %s\n got: %s", conjunct, got)
					}
					if !strings.Contains(got, want) {
						t.Errorf("条件が期待どおりに入っていない\nwant: %s\n got: %s", want, got)
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
