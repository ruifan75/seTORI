package repository

import (
	"errors"
	"testing"
)

// members_only はタグ定義を消すと stream_stream_tags の関連行が CASCADE で全部消え、
// **全会限配信の歌単が一斉に公開される**。しかも定義を作り直しても関連行は戻らない。
// DB 接続なしで守れるのはこの分岐だけなので、ここで固定しておく。
func TestDeleteStreamTagRejectsReserved(t *testing.T) {
	// db が nil でも予約タグの判定は SQL より前なので到達しない。
	r := &TagRepository{}

	err := r.DeleteStreamTag("members_only")
	if !errors.Is(err, ErrReservedStreamTag) {
		t.Fatalf("members_only の削除は ErrReservedStreamTag で拒否されるべき。got %v", err)
	}

	// 予約でないタグはここを素通りする（この先で nil db に触れるので、
	// 「拒否されないこと」だけを panic の有無で確かめる）。
	func() {
		defer func() { _ = recover() }()
		if err := r.DeleteStreamTag("singing"); errors.Is(err, ErrReservedStreamTag) {
			t.Errorf("singing は予約タグではない")
		}
	}()
}

func TestReservedStreamTagsCoversDetection(t *testing.T) {
	// 検出が読むタグは必ず予約に入れる。ずれると「消せてしまう」に戻る。
	if !reservedStreamTags[MembersOnlyTagID] {
		t.Fatalf("検出に使う %q が予約タグに入っていない", MembersOnlyTagID)
	}
}
