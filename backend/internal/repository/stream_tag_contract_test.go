package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

// 実際に発行された SQL 全体と引数を独立した期待値に照合し、返した行を Scan する。
// 既存の recordingDriver は変更しない（他の一覧のテストも使っている）。
// SQL の実行結果・EXPLAIN は *_integration_test.go の実 Postgres が担当する。
type streamTagSQLStep struct {
	query string
	args  []driver.Value
	rows  [][]driver.Value
}

type streamTagSQLConnector struct {
	steps []streamTagSQLStep
	next  int
}

func (c *streamTagSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &streamTagSQLConn{c}, nil
}
func (*streamTagSQLConnector) Driver() driver.Driver { return streamTagSQLDriver{} }

type streamTagSQLDriver struct{}

func (streamTagSQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use Connector")
}

type streamTagSQLConn struct{ c *streamTagSQLConnector }

func (*streamTagSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*streamTagSQLConn) Close() error              { return nil }
func (*streamTagSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (c *streamTagSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.c.next >= len(c.c.steps) {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	step := c.c.steps[c.c.next]
	c.c.next++
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if normalize(query) != normalize(step.query) {
		return nil, fmt.Errorf("SQL mismatch\n got: %s\nwant: %s", query, step.query)
	}
	var values []driver.Value
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	if !reflect.DeepEqual(values, step.args) {
		return nil, fmt.Errorf("arguments: %#v want %#v", values, step.args)
	}
	return &streamTagSQLRows{rows: step.rows}, nil
}

type streamTagSQLRows struct {
	rows [][]driver.Value
	next int
}

func (r *streamTagSQLRows) Columns() []string {
	columns := make([]string, len(r.rows[0]))
	for i := range columns {
		columns[i] = fmt.Sprintf("column_%d", i)
	}
	return columns
}
func (*streamTagSQLRows) Close() error { return nil }
func (r *streamTagSQLRows) Next(dest []driver.Value) error {
	if r.next == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

func streamTagContractDB(t *testing.T, steps ...streamTagSQLStep) *sql.DB {
	t.Helper()
	connector := &streamTagSQLConnector{steps: steps}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		db.Close()
		if connector.next != len(steps) {
			t.Errorf("queries: %d want %d", connector.next, len(steps))
		}
	})
	return db
}

// 実装の式を呼んで期待値を組み立てない。外側の JOIN・列・並び順も含めて固定する。
const streamTagWantAND = `(cardinality($1::text[]) = 0 OR streams.id IN (SELECT tf.stream_id FROM stream_stream_tags tf WHERE tf.tag_id = ANY($1::text[])
 GROUP BY tf.stream_id HAVING COUNT(DISTINCT tf.tag_id) = cardinality($1::text[])))`

const streamTagWantVisible = `streams.is_hidden = FALSE AND EXISTS (SELECT 1 FROM stream_channels ss JOIN channels si ON si.id = ss.channel_id
 WHERE ss.stream_id = streams.id AND si.is_hidden = FALSE)`

const streamTagWantColumns = `streams.id, streams.title, streams.stream_date, streams.duration_seconds,
 streams.thumbnail_url, streams.holodex_data, streams.holodex_hash, streams.comment_raw, streams.comment_songs,
 streams.is_processed, streams.is_hidden, streams.restriction_override, streams.created_at, streams.updated_at,
 COALESCE(streams.restriction_override, EXISTS (SELECT 1 FROM stream_stream_tags mt
 WHERE mt.stream_id = streams.id AND mt.tag_id = 'members_only') AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow') FROM stream_channels eo
 JOIN channels eg ON eg.id = eo.channel_id WHERE eo.stream_id = streams.id AND eo.is_owner), FALSE))`

func TestStreamTagListIssuedSQLAndScan(t *testing.T) {
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var twenty, quoted []string
	for i := 0; i < 20; i++ {
		tag := fmt.Sprintf("limit_%02d", i)
		twenty = append(twenty, tag)
		quoted = append(quoted, strconv.Quote(tag))
	}
	cases := []struct {
		raw   []string
		array string
	}{
		{nil, "{}"},
		{[]string{" singing ", "3d", "singing", "", "  "}, `{"singing","3d"}`},
		{append(twenty, " limit_00 ", ""), "{" + strings.Join(quoted, ",") + "}"},
	}
	for _, hidden := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("hidden=%v/tags=%v", hidden, tc.raw), func(t *testing.T) {
				filter := streamTagWantVisible
				if hidden {
					filter = "TRUE"
				}
				where := filter + " AND " + streamTagWantAND
				data := []byte(`{"songs":[{"name":"解析の曲"}]}`)
				comments := []byte(`[{"text":"元コメント"}]`)
				songs := []byte(`[{"name":"コメントの曲"}]`)
				db := streamTagContractDB(t,
					streamTagSQLStep{"SELECT COUNT(*) FROM streams WHERE " + where,
						[]driver.Value{tc.array}, [][]driver.Value{{int64(1)}}},
					streamTagSQLStep{"SELECT " + streamTagWantColumns + " FROM streams WHERE " + where + " ORDER BY stream_date ASC LIMIT $2 OFFSET $3",
						[]driver.Value{tc.array, int64(20), int64(40)},
						[][]driver.Value{{"fixture", "配信", date, int64(1800), nil, data, nil, comments, songs, true, hidden, true, date.Add(time.Hour), date.Add(2 * time.Hour), false}}})
				rows, total, err := NewStreamRepository(db).FindAll(20, 40, hidden, "date", "asc", tc.raw)
				if err != nil {
					t.Fatal(err)
				}
				want := models.Stream{ID: "fixture", Title: "配信", StreamDate: date,
					DurationSeconds: sql.NullInt32{Int32: 1800, Valid: true}, HolodexData: data,
					CommentRaw: comments, CommentSongs: songs, IsProcessed: true, IsHidden: hidden,
					RestrictionOverride: sql.NullBool{Bool: true, Valid: true},
					CreatedAt:           date.Add(time.Hour), UpdatedAt: date.Add(2 * time.Hour), IsRestrictedEffective: false}
				if total != 1 || !reflect.DeepEqual(rows, []models.Stream{want}) {
					t.Fatalf("Scan: total=%d rows=%+v want=%+v", total, rows, want)
				}
			})
		}
	}
}

func TestStreamTagCountsIssuedSQLAndLimit(t *testing.T) {
	query := `SELECT st.tag_id, COUNT(*) FROM stream_stream_tags st
 JOIN streams ON streams.id = st.stream_id WHERE ` + streamTagWantVisible + " AND " + streamTagWantAND + " GROUP BY st.tag_id"
	for _, tc := range []struct {
		raw   []string
		array string
	}{
		{nil, "{}"},
		{[]string{"singing", " 3d ", "", "singing"}, `{"singing","3d"}`},
	} {
		db := streamTagContractDB(t, streamTagSQLStep{query,
			[]driver.Value{tc.array}, [][]driver.Value{{"singing", int64(1)}, {"3d", int64(1)}}})
		counts, err := NewStreamRepository(db).CountByTagForList(tc.raw)
		if err != nil || !reflect.DeepEqual(counts, map[string]int{"singing": 1, "3d": 1}) {
			t.Fatalf("counts=%v err=%v", counts, err)
		}
	}

	var tags, quoted []string
	for i := 0; i < 20; i++ {
		tag := fmt.Sprintf("limit_%02d", i)
		tags = append(tags, tag)
		quoted = append(quoted, strconv.Quote(tag))
	}
	// 20 種は受理する。重複・空白は種類数に数えない。
	db20 := streamTagContractDB(t, streamTagSQLStep{query,
		[]driver.Value{"{" + strings.Join(quoted, ",") + "}"}, [][]driver.Value{{"limit_00", int64(1)}}})
	if _, err := NewStreamRepository(db20).CountByTagForList(append(append([]string{}, tags...), " limit_00 ", "")); err != nil {
		t.Fatal(err)
	}
	// 21 種は、両方とも SQL を発行する前に拒否する。
	repo := NewStreamRepository(streamTagContractDB(t))
	tags = append(tags, "limit_20")
	if _, _, err := repo.FindAll(20, 0, false, "", "", tags); !errors.Is(err, ErrTooManyStreamTags) {
		t.Fatalf("list limit: %v", err)
	}
	if _, err := repo.CountByTagForList(tags); !errors.Is(err, ErrTooManyStreamTags) {
		t.Fatalf("counts limit: %v", err)
	}
}

func TestStreamTagColorIssuedSQLAndScan(t *testing.T) {
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := `FROM stream_tags st JOIN stream_stream_tags sst ON st.id = sst.tag_id`
	db := streamTagContractDB(t,
		streamTagSQLStep{"SELECT st.id, st.display_name, COALESCE(st.color, ''), st.created_at " + base + " WHERE sst.stream_id = $1",
			[]driver.Value{"fixture"}, [][]driver.Value{{"no_color", "色なし", "", date}}},
		streamTagSQLStep{"SELECT sst.stream_id, st.id, st.display_name, COALESCE(st.color, ''), st.created_at " + base + " WHERE sst.stream_id = ANY($1)",
			[]driver.Value{`{"fixture"}`}, [][]driver.Value{{"fixture", "no_color", "色なし", "", date}}},
		streamTagSQLStep{"SELECT id, display_name, COALESCE(color, ''), created_at FROM stream_tags ORDER BY id",
			nil, [][]driver.Value{{"no_color", "色なし", "", date}}})
	repo := NewStreamRepository(db)
	want := []models.StreamTag{{ID: "no_color", DisplayName: "色なし", Color: "", CreatedAt: date}}
	if tags, err := repo.GetTags("fixture"); err != nil || !reflect.DeepEqual(tags, want) {
		t.Fatalf("single tags=%v err=%v", tags, err)
	}
	if tags, err := repo.GetTagsForStreams([]string{"fixture"}); err != nil || !reflect.DeepEqual(tags, map[string][]models.StreamTag{"fixture": want}) {
		t.Fatalf("batch tags=%v err=%v", tags, err)
	}
	if tags, err := NewTagRepository(db).FindAllStreamTags(); err != nil || !reflect.DeepEqual(tags, want) {
		t.Fatalf("vocabulary tags=%v err=%v", tags, err)
	}
}
