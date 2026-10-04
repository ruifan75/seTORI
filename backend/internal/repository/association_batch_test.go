package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

type perfAssociationStep struct {
	query string
	args  []driver.Value
	rows  [][]driver.Value
	err   error
}
type perfAssociationConnector struct {
	steps []perfAssociationStep
	next  int
}

func (c *perfAssociationConnector) Connect(context.Context) (driver.Conn, error) {
	return &perfAssociationConn{c}, nil
}
func (*perfAssociationConnector) Driver() driver.Driver { return perfAssociationDriver{} }

type perfAssociationDriver struct{}

func (perfAssociationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type perfAssociationConn struct{ c *perfAssociationConnector }

func (*perfAssociationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*perfAssociationConn) Close() error              { return nil }
func (*perfAssociationConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (c *perfAssociationConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.c.next >= len(c.c.steps) {
		return nil, fmt.Errorf("unexpected extra query: %s", query)
	}
	step := c.c.steps[c.c.next]
	c.c.next++
	if got := strings.Join(strings.Fields(query), " "); got != step.query {
		return nil, fmt.Errorf("SQL mismatch\n got: %s\nwant: %s", got, step.query)
	}
	var values []driver.Value
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	if !reflect.DeepEqual(values, step.args) {
		return nil, fmt.Errorf("args=%v want=%v", values, step.args)
	}
	if step.err != nil {
		return nil, step.err
	}
	return &perfAssociationRows{rows: step.rows}, nil
}

type perfAssociationRows struct{ rows [][]driver.Value }

func (r *perfAssociationRows) Columns() []string {
	n := 0
	if len(r.rows) > 0 {
		n = len(r.rows[0])
	}
	return make([]string, n)
}
func (*perfAssociationRows) Close() error { return nil }
func (r *perfAssociationRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}
func perfAssociationDB(t *testing.T, steps []perfAssociationStep) *sql.DB {
	t.Helper()
	c := &perfAssociationConnector{steps: steps}
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		if c.next != len(c.steps) {
			t.Errorf("queries=%d want=%d", c.next, len(c.steps))
		}
	})
	return db
}
func perfAssociationQueries(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("testdata/association_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var queries map[string]string
	if err = json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	return queries
}
func perfAssociationArray(ids []string) string { return "{\"" + strings.Join(ids, "\",\"") + "\"}" }

// 全 SQL・束縛値は変更前 main の固定 fixture と照合する。実装の式から期待値を作らない。
// 0/1/80 件、公開/秘匿の視界で、関連の問い合わせが件数に依存しないことと付与先を確認する。
func TestPerformanceListsBatchAssociations(t *testing.T) {
	for _, source := range []string{"stream", "singer"} {
		for _, view := range []string{"public", "restricted"} {
			for _, n := range []int{0, 1, 80} {
				t.Run(fmt.Sprintf("%s/%s/%d", source, view, n), func(t *testing.T) {
					queries := perfAssociationQueries(t)
					steps, want := perfAssociationScenario(queries, source, view, n)
					got, total, err := perfAssociationCall(perfAssociationDB(t, steps), source, view)
					if err != nil {
						t.Fatal(err)
					}
					if len(got) != n || (source == "singer" && total != n) {
						t.Fatalf("got=%d total=%d want=%d", len(got), total, n)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("association result differs\n got: %+v\nwant: %+v", got, want)
					}
				})
			}
		}
	}
}
func TestPerformanceBatchAssociationFailuresAreErrors(t *testing.T) {
	for _, source := range []string{"stream", "singer"} {
		for _, which := range []string{"performance-tags", "performance-singers", "performance-artists"} {
			t.Run(source+"/"+which, func(t *testing.T) {
				queries := perfAssociationQueries(t)
				steps, _ := perfAssociationScenario(queries, source, "public", 1)
				failure := errors.New(which + " unavailable")
				for i := range steps {
					if steps[i].query == queries[which] {
						steps[i].err = failure
						steps = steps[:i+1]
						break
					}
				}
				_, _, err := perfAssociationCall(perfAssociationDB(t, steps), source, "public")
				if !errors.Is(err, failure) {
					t.Fatalf("err=%v want=%v", err, failure)
				}
			})
		}
	}
}
func perfAssociationCall(db *sql.DB, source, view string) ([]PerformanceWithDetails, int, error) {
	repo := NewPerformanceRepository(db)
	access := PublicAccess
	if view == "restricted" {
		access = RestrictedView
	}
	if source == "stream" {
		rows, err := repo.FindByStreamID("video", access)
		return rows, 0, err
	}
	return repo.FindBySingerID("owner", 100, 0, "", "", access)
}
func perfAssociationScenario(q map[string]string, source, view string, n int) ([]perfAssociationStep, []PerformanceWithDetails) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	song := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	artist := models.ArtistReference{ID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), Name: "原曲の人"}
	var rows, tagRows, singerRows [][]driver.Value
	var want []PerformanceWithDetails
	var ids []string
	for i := 0; i < n; i++ {
		id := uuid.MustParse(fmt.Sprintf("20000000-0000-0000-0000-%012d", i+1))
		ids = append(ids, id.String())
		restricted := view == "restricted" && i%2 == 0
		values := []driver.Value{id.String(), "video", song.String(), int64(i * 100), int64(i*100 + 90), int64(i), nil, "{live}", now, "manual", true}
		p := PerformanceWithDetails{Performance: models.Performance{ID: id, StreamID: "video", SongID: song, StartSeconds: i * 100, EndSeconds: i*100 + 90, OrderIndex: i, CustomTags: []string{"live"}, CreatedAt: now, EndSource: "manual", EndConfirmed: true}, SongName: "曲", OriginalArtist: "原曲", IsRestricted: restricted, Artists: []models.ArtistReference{artist}}
		if source == "stream" {
			values = append(values, "曲", "原曲", "art", int64(123), restricted)
			p.Arts = sql.NullString{String: "art", Valid: true}
			p.ItunesID = sql.NullInt64{Int64: 123, Valid: true}
		} else {
			values = append(values, "配信", "2026-01-02", "thumbnail", "曲", "原曲", restricted)
			p.StreamTitle = "配信"
			p.StreamDate = "2026-01-02"
			p.ThumbnailURL = sql.NullString{String: "thumbnail", Valid: true}
		}
		rows = append(rows, values)
		if i%2 == 0 {
			tagRows = append(tagRows, []driver.Value{id.String(), "acoustic", "弾き語り", "blue", now})
			p.Tags = []models.PerformanceTag{{ID: "acoustic", DisplayName: "弾き語り", Color: "blue", CreatedAt: now}}
		}
		if i%3 == 0 {
			singerRows = append(singerRows, []driver.Value{id.String(), "vocalist", "歌った人", nil, nil, "org", "事務所", false, "manual", now, now})
			p.Singers = []models.Singer{{ID: "vocalist", Name: "歌った人", Organization: sql.NullString{String: "org", Valid: true}, OrganizationName: sql.NullString{String: "事務所", Valid: true}, MetadataSource: "manual", CreatedAt: now, UpdatedAt: now}}
		}
		want = append(want, p)
	}
	steps := []perfAssociationStep{}
	args := []driver.Value{"video"}
	if source == "singer" {
		steps = append(steps, perfAssociationStep{query: q["singer-count-"+view], args: []driver.Value{"owner"}, rows: [][]driver.Value{{int64(n)}}})
		args = []driver.Value{"owner", int64(100), int64(0)}
	}
	steps = append(steps, perfAssociationStep{query: q[source+"-"+view], args: args, rows: rows})
	if n > 0 {
		steps = append(steps,
			perfAssociationStep{query: q["performance-tags"], args: []driver.Value{perfAssociationArray(ids)}, rows: tagRows},
			perfAssociationStep{query: q["performance-singers"], args: []driver.Value{perfAssociationArray(ids)}, rows: singerRows},
			perfAssociationStep{query: q["performance-artists"], args: []driver.Value{perfAssociationArray([]string{song.String()})}, rows: [][]driver.Value{{song.String(), artist.ID.String(), artist.Name}}})
	}
	return steps, want
}
