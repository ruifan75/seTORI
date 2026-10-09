package service

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

	"github.com/ruifan75/setori/internal/repository"
)

type channelAssociationStep struct {
	query string
	args  []driver.Value
	rows  [][]driver.Value
	err   error
}
type channelAssociationConnector struct {
	steps []channelAssociationStep
	next  int
}

func (c *channelAssociationConnector) Connect(context.Context) (driver.Conn, error) {
	return &channelAssociationConn{c}, nil
}
func (*channelAssociationConnector) Driver() driver.Driver { return channelAssociationDriver{} }

type channelAssociationDriver struct{}

func (channelAssociationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type channelAssociationConn struct{ c *channelAssociationConnector }

func (*channelAssociationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*channelAssociationConn) Close() error              { return nil }
func (*channelAssociationConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (c *channelAssociationConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
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
	return &channelAssociationRows{rows: step.rows}, nil
}

type channelAssociationRows struct{ rows [][]driver.Value }

func (r *channelAssociationRows) Columns() []string {
	n := 0
	if len(r.rows) > 0 {
		n = len(r.rows[0])
	}
	return make([]string, n)
}
func (*channelAssociationRows) Close() error { return nil }
func (r *channelAssociationRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}
func channelAssociationDB(t *testing.T, steps []channelAssociationStep) *sql.DB {
	t.Helper()
	c := &channelAssociationConnector{steps: steps}
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		if c.next != len(c.steps) {
			t.Errorf("queries=%d want=%d", c.next, len(c.steps))
		}
	})
	return db
}
func channelAssociationQueries(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../repository/testdata/association_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var queries map[string]string
	if err = json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	return queries
}
func channelAssociationArray(ids []string) string { return "{\"" + strings.Join(ids, "\",\"") + "\"}" }

// チャンネルの配信一覧も一括で補完する。配信 metadata は秘匿配信でも公開であり、
// この経路に歌唱の可視条件を追加しない。固定 SQL で以前の WHERE と SELECT を維持する。
func TestChannelStreamListBatchesAssociations(t *testing.T) {
	for _, n := range []int{0, 1, 80} {
		for _, editor := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/editor=%v", n, editor), func(t *testing.T) {
				q := channelAssociationQueries(t)
				steps := channelAssociationScenario(q, n)
				db := channelAssociationDB(t, steps)
				s := NewChannelService(nil, repository.NewStreamRepository(db), nil)
				got, err := s.GetStreams("owner", 1, 100, nil, nil, editor)
				if err != nil {
					t.Fatal(err)
				}
				if got.Pagination.Total != n || len(got.Streams) != n {
					t.Fatalf("response=%+v", got)
				}
				for i, stream := range got.Streams {
					if stream.ID != fmt.Sprintf("video%06d", i) || stream.IsRestricted != (i%2 == 0) {
						t.Fatalf("stream=%+v", stream)
					}
					if len(stream.Tags) != 1 || stream.Tags[0].ID != "singing" || len(stream.Participants) != 1 || stream.Participants[0].ID != fmt.Sprintf("channel%d", i) {
						t.Fatalf("wrong association: %+v", stream)
					}
					if (stream.IsProcessed != nil) != editor || len(stream.HolodexTimelineSongs) != 0 || len(stream.CommentTimelineSongs) != 0 {
						t.Fatalf("operational/analysis response=%+v", stream)
					}
				}
			})
		}
	}
}
func TestChannelStreamBatchErrorsAreReturned(t *testing.T) {
	for _, which := range []string{"stream-tags", "stream-singers"} {
		t.Run(which, func(t *testing.T) {
			q := channelAssociationQueries(t)
			steps := channelAssociationScenario(q, 1)
			failure := errors.New(which + " unavailable")
			for i := range steps {
				if steps[i].query == q[which] {
					steps[i].err = failure
					steps = steps[:i+1]
					break
				}
			}
			s := NewChannelService(nil, repository.NewStreamRepository(channelAssociationDB(t, steps)), nil)
			_, err := s.GetStreams("owner", 1, 100, nil, nil, false)
			if !errors.Is(err, failure) {
				t.Fatalf("err=%v want=%v", err, failure)
			}
		})
	}
}
func channelAssociationScenario(q map[string]string, n int) []channelAssociationStep {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var rows, tags, channels [][]driver.Value
	var ids []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("video%06d", i)
		ids = append(ids, id)
		rows = append(rows, []driver.Value{id, "配信", now, int64(1000), nil, nil, nil, true, false, nil, now, now, i%2 == 0})
		tags = append(tags, []driver.Value{id, "singing", "歌枠", "blue", now})
		channels = append(channels, []driver.Value{id, true, fmt.Sprintf("channel%d", i), "チャンネル", nil, nil, nil, nil, false, "manual", now, now})
	}
	steps := []channelAssociationStep{
		{query: q["channel-stream-count"], args: []driver.Value{"owner"}, rows: [][]driver.Value{{int64(n)}}},
		{query: q["channel-stream-list"], args: []driver.Value{"owner", int64(100), int64(0)}, rows: rows},
	}
	if n > 0 {
		steps = append(steps, channelAssociationStep{query: q["stream-tags"], args: []driver.Value{channelAssociationArray(ids)}, rows: tags}, channelAssociationStep{query: q["stream-singers"], args: []driver.Value{channelAssociationArray(ids)}, rows: channels})
	}
	return steps
}
