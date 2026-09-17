package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruifan75/setori/pkg/gdrive"
)

// **実際の経路（uploadToDrive）を呼んで、送った名前と消した ID を見る。**
//
// 純粋関数（driveObjectName / drivePruneTargets）だけを検査していたときは、
// 呼び出し側で `objectName := name` にしても、整理を全件削除にしても
// テストが通った（issue #64 のレビューで実証された）。**決定そのものではなく、
// 決定が使われていることまで固定する。**

type driveCall struct {
	uploadedNames []string
	deletedIDs    []string
}

type fakeDriveRT struct {
	mu       sync.Mutex
	calls    *driveCall
	files    []gdrive.File
	queryErr string // 一覧の要求条件がおかしかったときに記録する
}

func (f *fakeDriveRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	body := func(v any) *http.Response {
		b, _ := json.Marshal(v)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))),
			Header: http.Header{"Content-Type": []string{"application/json"}}}
	}

	switch {
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/files/"):
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.calls.deletedIDs = append(f.calls.deletedIDs, id)
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil

	case r.Method == http.MethodPost && strings.Contains(r.URL.String(), "uploadType=resumable"):
		// メタデータの POST。ここに名前が入っている。
		raw, _ := io.ReadAll(r.Body)
		var meta struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &meta)
		f.calls.uploadedNames = append(f.calls.uploadedNames, meta.Name)
		resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")),
			Header: http.Header{"Location": []string{"https://upload.example/session"}}}
		return resp, nil

	case strings.Contains(r.URL.String(), "upload.example/session"):
		return body(gdrive.File{ID: "new", Name: "uploaded"}), nil

	case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/files?"):
		q, _ := url.ParseQuery(r.URL.RawQuery)
		// 一覧の要求条件。**整理はこの経路から走る**ので、ここでも見る。
		// **完全一致**で見るのは、部分一致だと `in parents or` や
		// `originalFilename` のように「通るのに壊れている」形が作れるため。
		for _, c := range []struct{ key, want string }{
			{"q", "'folder' in parents and trashed = false"},
			{"fields", "nextPageToken,files(id,name,size,createdTime,mimeType)"},
			{"orderBy", "createdTime desc"},
		} {
			if got := q.Get(c.key); got != c.want {
				f.queryErr = c.key + "=" + got
			}
		}
		if q.Get("pageToken") != "" {
			return body(map[string]any{"files": []gdrive.File{}}), nil
		}
		return body(map[string]any{"files": f.files}), nil
	}
	return body(map[string]any{}), nil
}

// newUploadHarness は uploadToDrive を呼べる最小の BackupService を作る。
func newUploadHarness(t *testing.T, instance string, existing []gdrive.File) (*BackupService, *driveCall, string) {
	t.Helper()
	t.Setenv("ENVIRONMENT", instance)

	calls := &driveCall{}
	client := gdrive.NewClient("id", "secret")
	rt := &fakeDriveRT{calls: calls, files: existing}
	client.SetTransport(rt)
	t.Cleanup(func() {
		if rt.queryErr != "" {
			t.Errorf("一覧の要求条件がおかしい: %s", rt.queryErr)
		}
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "setori_new.dump")
	if err := os.WriteFile(path, []byte("dump"), 0o600); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	svc := &BackupService{drive: client, backupDir: dir}
	// アクセストークンの取得を迂回する（連携の検査はこのテストの対象ではない）。
	svc.accessToken = "tok"
	svc.accessTokenExp = time.Now().Add(time.Hour)
	return svc, calls, path
}

func TestUploadToDriveTagsTheObjectName(t *testing.T) {
	svc, calls, path := newUploadHarness(t, "production", nil)
	settings := BackupSettings{RetentionDrive: 5, DriveFolderID: "folder"}

	if err := svc.uploadToDrive(&settings, path, "setori_new.dump"); err != nil {
		t.Fatalf("uploadToDrive: %v", err)
	}
	if len(calls.uploadedNames) != 1 {
		t.Fatalf("アップロード回数 = %d", len(calls.uploadedNames))
	}
	if got := calls.uploadedNames[0]; got != "production__setori_new.dump" {
		t.Errorf("送った名前 = %q, want %q（印が付いていない）", got, "production__setori_new.dump")
	}
}

// **整理が他の環境・印なしへ及ばないこと**を、実際に発行された DELETE で見る。
func TestUploadToDrivePrunesOnlyOwnFiles(t *testing.T) {
	existing := []gdrive.File{
		{ID: "p3", Name: "production__setori_3.dump"},
		{ID: "other", Name: "development__setori_9.dump"},
		{ID: "p2", Name: "production__setori_2.dump"},
		{ID: "bare", Name: "setori_old.dump"},
		{ID: "p1", Name: "production__setori_1.dump"},
	}
	svc, calls, path := newUploadHarness(t, "production", existing)
	settings := BackupSettings{RetentionDrive: 2, DriveFolderID: "folder"}

	if err := svc.uploadToDrive(&settings, path, "setori_new.dump"); err != nil {
		t.Fatalf("uploadToDrive: %v", err)
	}

	if len(calls.deletedIDs) != 1 || calls.deletedIDs[0] != "p1" {
		t.Errorf("削除した ID = %v, want [p1]", calls.deletedIDs)
	}
	for _, id := range calls.deletedIDs {
		if id == "other" || id == "bare" {
			t.Errorf("他環境／印なしのファイルを削除した: %s", id)
		}
	}
}

// 印が無い環境では**1 件も消さない**（実際の DELETE が 0 件）。
func TestUploadToDriveDoesNotPruneWithoutInstance(t *testing.T) {
	existing := []gdrive.File{
		{ID: "p3", Name: "production__setori_3.dump"},
		{ID: "p2", Name: "production__setori_2.dump"},
		{ID: "p1", Name: "production__setori_1.dump"},
	}
	svc, calls, path := newUploadHarness(t, "", existing)
	settings := BackupSettings{RetentionDrive: 1, DriveFolderID: "folder"}

	if err := svc.uploadToDrive(&settings, path, "setori_new.dump"); err != nil {
		t.Fatalf("uploadToDrive: %v", err)
	}
	if len(calls.deletedIDs) != 0 {
		t.Errorf("ENVIRONMENT が空なのに削除した: %v", calls.deletedIDs)
	}
	if got := calls.uploadedNames[0]; got != "setori_new.dump" {
		t.Errorf("印が無いのに名前を変えた: %q", got)
	}
}
