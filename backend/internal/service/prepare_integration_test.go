package service

import (
	"github.com/ruifan75/setori/internal/repository"
	"reflect"
	"testing"
)

func TestPreparationPostgresScopeAndSingleRun(t *testing.T) {
	db := reviewTestDB(t)
	if _, err := db.Exec(`INSERT INTO singers(id,name) VALUES ('owner','Owner'),('other','Other');
 INSERT INTO streams(id,title,stream_date,is_hidden,is_processed) VALUES
 ('fresh','fresh',NOW(),FALSE,FALSE),('cached','cached',NOW(),FALSE,FALSE),
 ('hidden','hidden',NOW(),TRUE,FALSE),('processed','processed',NOW(),FALSE,TRUE),
 ('guest','guest',NOW(),FALSE,FALSE);
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES
 ('fresh','owner',TRUE),('cached','owner',TRUE),('hidden','owner',TRUE),('processed','owner',TRUE),
 ('guest','owner',FALSE),('guest','other',TRUE);
 UPDATE streams SET chapter_raw='[]' WHERE id='cached';`); err != nil {
		t.Fatal(err)
	}
	tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, err := tasks.Start(TaskStreamPrepare, map[string]string{"singer_id": "owner"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &prepareFixture{run: run}
	svc := NewPrepareService(repository.NewStreamRepository(db), f, prepareBatch{f}, prepareFill{f}, tasks)
	svc.execute("owner", run)
	if !reflect.DeepEqual(f.events, []string{"chapter:fresh", "analysis"}) {
		t.Fatal("対象/段階が違う", f.events)
	}
	saved, err := tasks.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Kind != TaskStreamPrepare || saved.Phase != "analysis" || saved.Status != "done" || saved.Total != 4 || saved.Done != 4 || saved.Succeeded != 3 || saved.Skipped != 1 || saved.Failed != 0 {
		t.Fatalf("同一実行への記録=%+v", saved)
	}
}
