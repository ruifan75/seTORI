package repository

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

// visibleChannelLiteral は `VisibleChannelExpr(alias)` の期待値を**実装を呼ばずに**作る。
// `TestVisibleChannelExpr` と同じ文字列で、alias だけ差し替えられるようにしたもの。
func visibleChannelLiteral(alias string) string {
	return "EXISTS (SELECT 1 FROM stream_singers ss" +
		" JOIN singers si ON si.id = ss.singer_id" +
		" WHERE ss.stream_id = " + alias + ".id AND si.is_hidden = FALSE)"
}

// wantListed は `ListedFor` の期待値。**部品は文字どおり固定されているものだけ**を使う
// （`NotRestricted` は `TestRestrictedExpressionsExact` が固定している）。
func wantListed(alias string, a ViewerAccess) string {
	if a == RestrictedView {
		return "TRUE"
	}
	return alias + ".is_hidden = FALSE AND " + NotRestricted(alias) + " AND " + visibleChannelLiteral(alias)
}

// `ListedFor` と、その部品の `DiscoverableFor` を完全一致で固定する。
//
// **`DiscoverableFor` もここで固定する。** これまでの検査は部分一致
// （`members_only` を含むか、`is_hidden = FALSE` を含むか）だけで、
// `" AND "` を `" OR "` にしても通った。`discoverClause` は
// `TestDiscoverClauseExact` で固定されているが、見た目が似ているだけの別実装なので
// あちらの検査はこちらを守らない（CLAUDE.md）。`ListedFor` の期待値に
// `DiscoverableFor` を使うと、そこが緩い分だけ一緒に緩む。
func TestListedForExact(t *testing.T) {
	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"DiscoverableFor/PublicAccess", DiscoverableFor("st", PublicAccess),
			"st.is_hidden = FALSE AND " + NotRestricted("st")},
		{"DiscoverableFor/RestrictedView", DiscoverableFor("st", RestrictedView), "TRUE"},
		{"ListedFor/PublicAccess", ListedFor("st", PublicAccess), wantListed("st", PublicAccess)},
		// **権限で外す。** チャンネルの表示は `is_hidden` と同じ「一覧の既定から外す」
		// 軸で、管理者に「歌唱があるのに 0 件に見える」を起こさない。
		{"ListedFor/RestrictedView", ListedFor("st", RestrictedView), "TRUE"},
	} {
		if c.got != c.want {
			t.Errorf("%s が変わっている\n got: %s\nwant: %s", c.name, c.got, c.want)
		}
	}

	// alias が全体に効くこと（タグの件数は配信を `s` と呼ぶ）。
	if got, want := ListedFor("s", PublicAccess), wantListed("s", PublicAccess); got != want {
		t.Errorf("alias が効いていない\n got: %s\nwant: %s", got, want)
	}
}

// whereAfter は marker（`FROM performances p` など）の後ろにある、**同じ深さの**
// WHERE 句を取り出す。副問い合わせの中の WHERE も、その副問い合わせの FROM を
// marker にすれば取れる。
//
// 終端は「同じ深さで ORDER BY / LIMIT / GROUP BY が来る」か「marker を含む括弧が
// 閉じる」。**終端まで取る**のは、末尾に `OR TRUE` を足す改変を逃さないため。
func whereAfter(t *testing.T, sqlText, marker string) string {
	t.Helper()
	norm := strings.Join(strings.Fields(sqlText), " ")
	if n := strings.Count(norm, marker); n != 1 {
		t.Fatalf("marker %q が %d 回現れる（1 回のはず）: %s", marker, n, norm)
	}
	upper := strings.ToUpper(norm)
	i := strings.Index(norm, marker) + len(marker)

	depth, start := 0, -1
	for ; i < len(norm); i++ {
		switch norm[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth < 0 {
			break
		}
		if depth != 0 {
			continue
		}
		if start < 0 {
			if strings.HasPrefix(upper[i:], " WHERE ") {
				start = i + 1
			}
			continue
		}
		stopped := false
		for _, stop := range []string{" ORDER BY ", " LIMIT ", " GROUP BY "} {
			if strings.HasPrefix(upper[i:], stop) {
				stopped = true
			}
		}
		if stopped {
			break
		}
	}
	if start < 0 {
		return ""
	}
	return strings.TrimSpace(norm[start:i])
}

// **曲ページ・曲一覧・タグ・アーティストの歌唱は、一覧に出していないチャンネルの
// ものを出さない**（#61）。件数も同じ条件で数える ── 一覧から落として件数に
// 残ると、何件伏せたかが残る。
//
// 発行された SQL を**実際に呼んで**取り、WHERE を**終端まで完全一致**で比べる。
// 期待値は `wantListed`（実装を呼ばずに組み立てたもの）から作る。
func TestListedSurfacesApplyChannelScope(t *testing.T) {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	songID := uuid.New()

	type query struct {
		pick   string // この文字列を含む SQL を対象にする
		marker string
		want   func(ViewerAccess) string
	}
	cases := []struct {
		name    string
		call    func(db *sql.DB, a ViewerAccess)
		queries []query
	}{
		{
			name: "曲ページの歌唱（FindBySongID）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewPerformanceRepository(db).FindBySongID(songID, 20, 0, a)
			},
			queries: []query{
				{"COUNT(*)", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE p.song_id = $1 AND " + wantListed("st", a)
				}},
				{"LIMIT $2", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE p.song_id = $1 AND " + wantListed("st", a)
				}},
			},
		},
		{
			name: "タグ別の歌唱（FindByTagID）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewPerformanceRepository(db).FindByTagID("acoustic", 20, 0, a)
			},
			queries: []query{
				{"COUNT(*)", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE ppt.tag_id = $1 AND " + wantListed("st", a)
				}},
				{"LIMIT $2", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE ppt.tag_id = $1 AND " + wantListed("st", a)
				}},
			},
		},
		{
			name: "曲の歌唱回数（GetPerformanceCount）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewSongRepository(db).GetPerformanceCount(songID, a)
			},
			queries: []query{
				{"COUNT(*)", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE p.song_id = $1 AND " + wantListed("st", a)
				}},
			},
		},
		{
			name: "曲一覧の歌唱回数（GetPerformanceCounts）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewSongRepository(db).GetPerformanceCounts([]uuid.UUID{songID}, a)
			},
			queries: []query{
				{"COUNT(*)", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE p.song_id = ANY($1::uuid[]) AND " + wantListed("st", a)
				}},
			},
		},
		{
			// ホームの「人気の楽曲」はこの並び（sort=performances）を使う。
			name: "曲一覧の歌唱数順（FindAll）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewSongRepository(db).FindAll(20, 0, "", "performances", "desc", a)
			},
			queries: []query{
				{"LEFT JOIN", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE " + wantListed("st", a)
				}},
			},
		},
		{
			// 検索語ありは別の分岐で SQL を組み立てる。
			name: "曲一覧の歌唱数順・検索あり（FindAll）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewSongRepository(db).FindAll(20, 0, "x", "performances", "desc", a)
			},
			queries: []query{
				{"LEFT JOIN", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE " + wantListed("st", a)
				}},
			},
		},
		{
			name: "歌唱タグの件数（SearchPerformanceTags）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewTagRepository(db).SearchPerformanceTags("", 10, a)
			},
			queries: []query{
				{"performance_performance_tags", "FROM performance_performance_tags ppt", func(a ViewerAccess) string {
					return "WHERE ppt.tag_id = t.id AND " + wantListed("s", a)
				}},
			},
		},
		{
			name: "アーティストの曲の歌唱回数（FindSongsByArtist）",
			call: func(db *sql.DB, a ViewerAccess) {
				NewArtistRepository(db).FindSongsByArtist(uuid.New(), 20, 0, "", "", a)
			},
			queries: []query{
				{"perf_count", "FROM performances p", func(a ViewerAccess) string {
					return "WHERE p.song_id = s.id AND " + wantListed("st", a)
				}},
			},
		},
	}

	// **両方の権限で見る。** 片方だけだと「RestrictedView でも絞る」
	// 「PublicAccess でも外す」のどちらかが通る。
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		for _, tc := range cases {
			t.Run(tc.name+"/"+accessName(access), func(t *testing.T) {
				db, rec := newRecordingDB(t)
				tc.call(db, access)

				for _, q := range tc.queries {
					var hit []string
					for _, issued := range rec.all() {
						if strings.Contains(issued, q.pick) && strings.Contains(norm(issued), q.marker) {
							hit = append(hit, issued)
						}
					}
					if len(hit) != 1 {
						t.Fatalf("%q を含む SQL が %d 本（1 本のはず）: %q", q.pick, len(hit), rec.all())
					}
					if got, want := whereAfter(t, hit[0], q.marker), norm(q.want(access)); got != want {
						t.Errorf("%q の WHERE が期待と違う\n got: %s\nwant: %s", q.pick, got, want)
					}
				}
			})
		}
	}
}

// **チャンネルの表示に従わせない場所。** 従わせると誤った判断へ誘導する：
//
//   - 歌手ページの歌唱数 … 非表示チャンネルのページは未ログインでも開ける設計
//     （CLAUDE.md §3）。通すと自分の歌唱が 0 件のページになる
//     （歌唱一覧の側は `TestSingerPageIsNotChannelScoped`）
//   - 統合候補の件数 … 統合の向きは件数で決めるので、非表示チャンネルの歌唱しか
//     無い曲が 0 件に見えると、残すべきほうを消す方向へ誘導する
//   - 配信検索 … 非表示の配信も意図的に含める（CLAUDE.md §2）。同じ性質の
//     チャンネルの軸だけ濾すのは筋が通らない
func TestChannelScopeStaysOffWhereItWouldMislead(t *testing.T) {
	needle := strings.Join(strings.Fields(visibleChannelLiteral("st")), " ")
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		db, rec := newRecordingDB(t)
		NewSingerRepository(db).GetPerformanceCount("UC-x", access)
		NewStreamRepository(db).SearchStreams(models.StreamSearchFilters{
			// 歌唱を参照する絞り込みを両方立てる（どちらも歌唱の副問い合わせを持つ）。
			VocalistIDs:       []string{"UC-x"},
			PerformanceTagIDs: []string{"acoustic"},
		}, 20, 0, access)
		issued := rec.all()
		if len(issued) < 2 {
			t.Fatalf("SQL が発行されていない: %q", issued)
		}
		for _, q := range issued {
			if strings.Contains(strings.Join(strings.Fields(q), " "), needle) {
				t.Errorf("チャンネルの判定が入っている（access=%s）: %s", accessName(access), q)
			}
		}

		if strings.Contains(strings.Join(strings.Fields(mergeCandidateSelect(access)), " "), needle) {
			t.Errorf("統合候補の件数にチャンネルの判定が入っている（access=%s）", accessName(access))
		}
	}
}

// WHERE だけでは、歌唱とは別の配信にチャンネル判定を適用していても通る。
// GetPerformanceCounts の発行 SQL 全体を固定し、JOIN の関連先も検査する。
func TestGetPerformanceCountsQueryExact(t *testing.T) {
	const publicWhere = "p.song_id = ANY($1::uuid[]) AND st.is_hidden = FALSE AND NOT COALESCE(st.restriction_override, " +
		"EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')" +
		" AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')" +
		" FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id" +
		" WHERE eo.stream_id = st.id AND eo.is_owner), FALSE))" +
		" AND EXISTS (SELECT 1 FROM stream_singers ss JOIN singers si ON si.id = ss.singer_id" +
		" WHERE ss.stream_id = st.id AND si.is_hidden = FALSE)"
	for _, tc := range []struct {
		name   string
		access ViewerAccess
		where  string
	}{
		{"public", PublicAccess, publicWhere},
		{"restricted-view", RestrictedView, "p.song_id = ANY($1::uuid[]) AND TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newRecordingDB(t)
			if _, err := NewSongRepository(db).GetPerformanceCounts([]uuid.UUID{uuid.MustParse("00000000-0000-0000-0000-000000000001")}, tc.access); err != nil {
				t.Fatal(err)
			}
			issued := rec.all()
			if len(issued) != 1 {
				t.Fatalf("queries = %d, want 1", len(issued))
			}
			want := "SELECT p.song_id, COUNT(*) FROM performances p JOIN streams st ON p.stream_id = st.id WHERE " + tc.where + " GROUP BY p.song_id"
			if got := strings.Join(strings.Fields(issued[0]), " "); got != want {
				t.Errorf("歌唱数の SQL が変わっている\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// タグ一覧では件数と一覧で FROM/JOIN が別々に組み立てられる。
// 両方の SQL 全体を固定し、片方の JOIN だけを取り違えた改変も検出する。
func TestFindByTagIDQueriesExact(t *testing.T) {
	const restricted = "COALESCE(st.restriction_override, EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only') AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow') FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id WHERE eo.stream_id = st.id AND eo.is_owner), FALSE))"
	const public = "st.is_hidden = FALSE AND NOT " + restricted + " AND EXISTS (SELECT 1 FROM stream_singers ss JOIN singers si ON si.id = ss.singer_id WHERE ss.stream_id = st.id AND si.is_hidden = FALSE)"
	const joins = "FROM performances p JOIN streams st ON p.stream_id = st.id "
	const tagJoin = "JOIN performance_performance_tags ppt ON ppt.performance_id = p.id "
	const projection = "SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index, p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed, st.title AS stream_title, st.stream_date, st.thumbnail_url, s.name AS song_name, s.original_artist, s.arts, " + restricted + " "
	for _, tc := range []struct {
		name   string
		access ViewerAccess
		scope  string
	}{
		{"public", PublicAccess, public}, {"restricted-view", RestrictedView, "TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newRecordingDB(t)
			if _, _, err := NewPerformanceRepository(db).FindByTagID("acoustic", 20, 0, tc.access); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"SELECT COUNT(*) " + joins + tagJoin + "WHERE ppt.tag_id = $1 AND " + tc.scope,
				projection + joins + "JOIN songs s ON p.song_id = s.id " + tagJoin + "WHERE ppt.tag_id = $1 AND " + tc.scope + " ORDER BY st.stream_date DESC, p.order_index ASC LIMIT $2 OFFSET $3",
			}
			issued := rec.all()
			if len(issued) != len(want) {
				t.Fatalf("queries=%d want=%d", len(issued), len(want))
			}
			for i, q := range issued {
				if got := strings.Join(strings.Fields(q), " "); got != want[i] {
					t.Errorf("query %d got: %s\nwant: %s", i, got, want[i])
				}
			}
		})
	}
}
