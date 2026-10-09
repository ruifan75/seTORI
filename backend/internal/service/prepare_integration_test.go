package service

import (
	"github.com/ruifan75/setori/internal/repository"
	"reflect"
	"testing"
)

func TestPreparationPostgresScopeAndSingleRun(t *testing.T) {
	db := reviewTestDB(t)
	if _, err := db.Exec(`INSERT INTO channels(id,name) VALUES ('owner','Owner'),('other','Other');
 INSERT INTO streams(id,title,stream_date,is_hidden,is_processed) VALUES
 ('fresh','fresh',NOW(),FALSE,FALSE),('cached','cached',NOW(),FALSE,FALSE),
 ('hidden','hidden',NOW(),TRUE,FALSE),('processed','processed',NOW(),FALSE,TRUE),
 ('guest','guest',NOW(),FALSE,FALSE),
 ('member','member',NOW(),FALSE,FALSE),('permitted-member','permitted-member',NOW(),FALSE,FALSE),
 ('restricted','restricted',NOW(),FALSE,FALSE);
 INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES
 ('fresh','owner',TRUE),('cached','owner',TRUE),('hidden','owner',TRUE),('processed','owner',TRUE),
 ('guest','owner',FALSE),('guest','other',TRUE),
 ('member','owner',TRUE),('permitted-member','owner',TRUE),('restricted','owner',TRUE);
 INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('member','members_only'),('permitted-member','members_only');
 UPDATE streams SET restriction_override=FALSE WHERE id='permitted-member';
 UPDATE streams SET restriction_override=TRUE WHERE id='restricted';
 UPDATE streams SET chapter_raw='[]' WHERE id='cached';`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStreamRepository(db)
	for _, id := range []string{"fresh", "cached", "hidden", "processed", "guest", "member", "permitted-member", "restricted", "missing"} {
		got, err := repo.PreparationStreamEligible("owner", id)
		want := id == "fresh" || id == "cached"
		if err != nil || got != want {
			t.Fatalf("対象の再検査 %s=%v want%v err=%v", id, got, want, err)
		}
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
