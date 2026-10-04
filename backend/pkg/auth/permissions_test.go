package auth

import "testing"

func TestHolodexUploadPermissionMetadata(t *testing.T) {
	if PermHolodexUpload != "holodex:upload" {
		t.Fatalf("upload key = %q, want holodex:upload", PermHolodexUpload)
	}
	want := PermissionInfo{
		Key:         "holodex:upload",
		Description: "Holodex へのセットリスト送信（運用者の名義で外部に書き込み）",
	}
	count := 0
	for _, info := range AllPermissions() {
		if info.Key == want.Key {
			count++
			if info != want {
				t.Errorf("upload metadata = %#v, want %#v", info, want)
			}
		}
	}
	if count != 1 {
		t.Fatalf("upload permission entries = %d, want 1", count)
	}
}
