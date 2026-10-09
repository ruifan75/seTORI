package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/repository"
)

// 共有 driver は変更しない。発行 SQL の不一致は、service がエラーを握りつぶしても失敗させる。
type uploadLedgerConnector struct {
	*channelAssociationConnector
	t        *testing.T
	affected int64
	writeErr error
	countErr error
	attempts int
	recorded bool
}
type uploadLedgerConn struct {
	*channelAssociationConn
	c *uploadLedgerConnector
}

func (c *uploadLedgerConnector) Connect(context.Context) (driver.Conn, error) {
	return &uploadLedgerConn{channelAssociationConn: &channelAssociationConn{c.channelAssociationConnector}, c: c}, nil
}
func (c *uploadLedgerConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.channelAssociationConn.QueryContext(ctx, query, args)
	if err != nil {
		c.c.t.Error(err)
	}
	return rows, err
}
func (c *uploadLedgerConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.c.attempts++
	if query != `UPDATE streams SET holodex_uploaded_at = NOW() WHERE id = $1` || len(args) != 1 || args[0].Value != "hVfDBfreYNI" {
		err := fmt.Errorf("ledger SQL=%s args=%v", query, args)
		c.c.t.Error(err)
		return nil, err
	}
	if c.c.writeErr != nil {
		return nil, c.c.writeErr
	}
	c.c.recorded = c.c.affected == 1
	return uploadLedgerResult{driver.RowsAffected(c.c.affected), c.c.countErr}, nil
}

type uploadLedgerResult struct {
	driver.Result
	countErr error
}

func (r uploadLedgerResult) RowsAffected() (int64, error) {
	n, err := r.Result.RowsAffected()
	if r.countErr != nil {
		return n, r.countErr
	}
	return n, err
}

type uploadFixtureTransport func(*http.Request) (*http.Response, error)

func (f uploadFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestHolodexUploadRequiresRecordedPublicSource(t *testing.T) {
	const id = "hVfDBfreYNI"
	const perfID = "10000000-0000-0000-0000-000000000001"
	const songID = "10000000-0000-0000-0000-000000000002"
	const automatic = `EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = streams.id AND mt.tag_id = 'members_only') AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow') FROM stream_channels eo JOIN channels eg ON eg.id = eo.channel_id WHERE eo.stream_id = streams.id AND eo.is_owner), FALSE)`
	const streamQuery = `SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, comment_songs_analyzed_at, chapter_raw, chapter_songs, is_processed, is_hidden, restriction_override, holodex_uploaded_at, holodex_upload_unknown, availability, playable_in_embed, availability_checked_at, created_at, updated_at,
 COALESCE(streams.restriction_override, ` + automatic + `) AS is_restricted_effective,
 streams.restriction_override IS FALSE AND ` + automatic + ` AND streams.restriction_override_auto IS DISTINCT FROM TRUE AS restriction_needs_review FROM streams WHERE id = $1`
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	queries := channelAssociationQueries(t)
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		restricted bool
		affected   int64
		writeErr   error
		countErr   error
		wantErr    bool
		puts       int
	}{
		{name: "public-positive", affected: 1, puts: 1},
		{name: "restricted-never-upload", restricted: true},
		{name: "ledger-failure-stops-upload", writeErr: errors.New("ledger unavailable"), wantErr: true},
		{name: "deleted-after-reading-stops-upload", wantErr: true},
		{name: "unknown-record-count-stops-upload", affected: 1, countErr: errors.New("record count unavailable"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			streamRow := []driver.Value{id, "title", now, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, true, nil, nil, false, nil, nil, nil, now, now, tc.restricted, false}
			perfs := channelAssociationStep{query: queries["stream-public"], args: []driver.Value{id}}
			if !tc.restricted {
				perfs.rows = [][]driver.Value{{perfID, id, songID, int64(30), int64(60), int64(0), nil, "{}", now, "manual", true, "fixture song", "fixture artist", nil, nil, false}}
			}
			steps := []channelAssociationStep{{query: normalize(streamQuery), args: []driver.Value{id}, rows: [][]driver.Value{streamRow}}, perfs}
			if !tc.restricted {
				steps = append(steps,
					channelAssociationStep{query: queries["performance-tags"], args: []driver.Value{`{"` + perfID + `"}`}},
					channelAssociationStep{query: queries["performance-singers"], args: []driver.Value{`{"` + perfID + `"}`}},
					channelAssociationStep{query: queries["performance-artists"], args: []driver.Value{`{"` + songID + `"}`}},
				)
				{
					// 失敗時にも曲を返せるようにし、停止を忘れた実装の PUT 自体を捕捉する。
					steps = append(steps, channelAssociationStep{
						query: `SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at FROM songs WHERE id = $1`,
						args:  []driver.Value{songID}, rows: [][]driver.Value{{songID, "fixture song", nil, "fixture artist", nil, nil, now, now}},
					})
				}
			}
			c := &uploadLedgerConnector{channelAssociationConnector: &channelAssociationConnector{steps: steps}, t: t, affected: tc.affected, writeErr: tc.writeErr, countErr: tc.countErr}
			db := sql.OpenDB(c)
			t.Cleanup(func() {
				db.Close()
				wantQueries := len(steps)
				if tc.wantErr {
					wantQueries--
				}
				if c.next != wantQueries {
					t.Errorf("queries=%d want=%d", c.next, wantQueries)
				}
			})
			previous := http.DefaultTransport
			puts, gets := 0, 0
			http.DefaultTransport = uploadFixtureTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "holodex.net" {
					t.Errorf("unexpected external request: %s", req.URL)
					return nil, errors.New("blocked")
				}
				if req.Method == "GET" && req.URL.Path == "/api/v2/videos/"+id {
					gets++
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"hVfDBfreYNI","songs":[]}`)), Request: req}, nil
				}
				if req.Method == "PUT" && req.URL.Path == "/api/v2/songs" {
					puts++
					if !c.recorded {
						t.Error("PUT ran before a successful ledger write")
					}
					if req.Header.Get("Authorization") != "Bearer fixture-editor" {
						t.Error("wrong synthetic editor token")
					}
					body, err := io.ReadAll(req.Body)
					if err != nil || !strings.Contains(string(body), `"name":"fixture song"`) {
						t.Errorf("body=%s err=%v", body, err)
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
				}
				t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				return nil, errors.New("blocked")
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			s := NewHolodexService("fixture-key", "", "", repository.NewStreamRepository(db), nil, "fixture-editor")
			s.perfRepo = repository.NewPerformanceRepository(db)
			s.songRepo = repository.NewSongRepository(db)
			result, err := s.SyncSetoriToHolodex(id)
			if (err != nil) != tc.wantErr || puts != tc.puts || gets != 1 {
				t.Fatalf("result=%+v err=%v puts=%d gets=%d", result, err, puts, gets)
			}
			attempts := 1
			if tc.restricted {
				attempts = 0
			}
			if c.attempts != attempts {
				t.Fatalf("attempts=%d want=%d", c.attempts, attempts)
			}
			if !tc.wantErr && (result == nil || result.SyncedCount != tc.puts) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
