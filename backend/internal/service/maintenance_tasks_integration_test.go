package service

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
)

// 専用 DB の独立 schema だけに作り、reviewTestDB が schema ごと削除する。
// 実 handler の非同期・409 は maintenance_tasks_test.go、ここは実 SQL と永続化を検査する。
type maintenanceIntegrationAI struct {
	replies []string
	err     error
	calls   int
}

func (a *maintenanceIntegrationAI) SimpleChat(_, _ string) (string, error) {
	if a.err != nil {
		return "", a.err
	}
	if a.calls >= len(a.replies) {
		return "", errors.New("unexpected AI call")
	}
	reply := a.replies[a.calls]
	a.calls++
	return reply, nil
}

func TestReadingTaskPersistsWithDatabase(t *testing.T) {
	for _, aiFails := range []bool{false, true} {
		t.Run(fmt.Sprint(aiFails), func(t *testing.T) {
			db := reviewTestDB(t)
			artist, songA, songB := uuid.New(), uuid.New(), uuid.New()
			if _, err := db.Exec(`INSERT INTO artists (id,name) VALUES ($1,'歌手甲')`, artist); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO songs (id,name,original_artist) VALUES ($1,'曲A','歌手甲'),($2,'曲B','歌手甲')`, songA, songB); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO song_artists (song_id,artist_id) VALUES ($1,$3),($2,$3)`, songA, songB, artist); err != nil {
				t.Fatal(err)
			}
			ai := &maintenanceIntegrationAI{replies: []string{`[{"index":0,"reading":"こう","confidence":0.9}]`, `[{"index":0,"reading":"きょくこう","confidence":0.9},{"index":1,"reading":"おつ","confidence":0.1}]`}}
			if aiFails {
				ai.err = errors.New("quota")
			}
			s := NewArtistService(repository.NewArtistRepository(db), repository.NewSongRepository(db), ai)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			run, err := tasks.Start(TaskReadingsBackfill, map[string]int{"batch_size": 30}, nil)
			if err != nil {
				t.Fatal(err)
			}
			run.Execute(func() (string, error) { return s.BackfillReadings(run) })
			got, err := tasks.Get(run.ID)
			if err != nil || got == nil {
				t.Fatalf("task=%+v err=%v", got, err)
			}
			counts := []int{got.Total, got.Done, got.Succeeded, got.Skipped, got.Failed}
			want, status := []int{3, 3, 2, 1, 0}, "done"
			if aiFails {
				want, status = []int{3, 3, 0, 0, 3}, "failed"
			}
			if got.Kind != TaskReadingsBackfill || got.Phase != "songs" || got.Status != status || !reflect.DeepEqual(counts, want) || got.FinishedAt == nil || len(got.Failures) != got.Failed {
				t.Fatalf("task=%+v want=%s %v", got, status, want)
			}
			if !aiFails {
				var artistReading, songReading, propagated string
				if err := db.QueryRow(`SELECT name_reading FROM artists WHERE id=$1`, artist).Scan(&artistReading); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT name_reading,original_artist_reading FROM songs WHERE id=$1`, songA).Scan(&songReading, &propagated); err != nil {
					t.Fatal(err)
				}
				if artistReading != "こう" || songReading != "きょくこう" || propagated != "こう" {
					t.Fatalf("readings=%q %q %q", artistReading, songReading, propagated)
				}
			}
			again, err := tasks.Start(TaskReadingsBackfill, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			again.Finish("done", "retry")
		})
	}
}

func TestDuplicateScanTaskPersistsWithDatabase(t *testing.T) {
	for _, aiFails := range []bool{false, true} {
		t.Run(fmt.Sprint(aiFails), func(t *testing.T) {
			db := reviewTestDB(t)
			ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
			repo := repository.NewSongMatchRepository(db)
			for i, id := range ids {
				name := "曲甲"
				if i == 2 {
					name = "曲乙"
				}
				artist := fmt.Sprintf("原曲%d", i)
				if _, err := db.Exec(`INSERT INTO songs (id,name,original_artist,created_at) VALUES ($1,$2,$3,TIMESTAMPTZ '2026-01-01' + $4 * INTERVAL '1 second')`, id, name, artist, i); err != nil {
					t.Fatal(err)
				}
				if err := repo.Upsert(id, name, artist); err != nil {
					t.Fatal(err)
				}
			}
			ai := &maintenanceIntegrationAI{replies: []string{`[{"a":0,"b":2},{"a":0,"b":1}]`}}
			if aiFails {
				ai.err = errors.New("quota")
			}
			s := NewSongMatchService(repo, nil, nil, nil)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			run, err := tasks.Start(TaskDuplicateScan, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			run.Execute(func() (string, error) { return s.BackfillDuplicateCandidates(ai, run) })
			got, err := tasks.Get(run.ID)
			if err != nil || got == nil {
				t.Fatalf("task=%+v err=%v", got, err)
			}
			counts := []int{got.Total, got.Done, got.Succeeded, got.Skipped, got.Failed}
			want, status, added := []int{3, 3, 2, 1, 0}, "done", 2
			if aiFails {
				want, status, added = []int{2, 2, 1, 0, 1}, "failed", 1
			}
			if got.Kind != TaskDuplicateScan || got.Phase != "ai_scan" || got.Status != status || !reflect.DeepEqual(counts, want) || got.FinishedAt == nil || len(got.Failures) != got.Failed {
				t.Fatalf("task=%+v want=%s %v", got, status, want)
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM song_merge_candidates`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != added {
				t.Fatalf("candidates=%d want=%d", n, added)
			}
			again, err := tasks.Start(TaskDuplicateScan, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			again.Finish("done", "retry")
		})
	}
}
