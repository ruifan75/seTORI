package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 審査へ回す判定は「黙って壊れる」側の処理なので、条件ごとに固定しておく。
// 実装を読まないと分からない判断（推定 end は end 扱いしない、歌手は照合後ではなく
// 抽出時の値で見る）が、直したあとにまた戻らないようにするのが目的。

func TestHasReliableEnd(t *testing.T) {
	tests := []struct {
		name      string
		start     int
		end       int
		endSource string
		want      bool
	}{
		{"拍手検出", 100, 300, repository.EndSourceChat, true},
		{"Holodex が明示", 100, 300, repository.EndSourceHolodex, true},
		{"コメントに明示", 100, 300, repository.EndSourceComment, true},
		// これが直したかった穴。次の曲の開始で埋めた値は「終了時間がある」とは言えない。
		{"次の曲の開始からの推定", 100, 300, repository.EndSourceNextStart, false},
		{"由来なし", 100, 300, repository.EndSourceUnknown, false},
		{"既定値の 240 秒", 100, 340, repository.EndSourceDefault, false},
		{"そもそも end が無い", 100, 0, repository.EndSourceChat, false},
		{"end が start より前", 300, 100, repository.EndSourceChat, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := &fillRow{Start: tt.start, End: tt.end, EndSource: tt.endSource}
			if got := row.hasReliableEnd(); got != tt.want {
				t.Errorf("hasReliableEnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffAgainstExisting(t *testing.T) {
	songA, songB := uuid.New(), uuid.New()
	existing := []repository.PerformanceWithDetails{{
		Performance: models.Performance{SongID: songA, StartSeconds: 600, EndSeconds: 840},
	}}

	tests := []struct {
		name string
		row  *fillRow
		want existingDiff
	}{
		{
			"完全に一致",
			&fillRow{SongID: &songA, Start: 600, End: 840, EndSource: repository.EndSourceChat},
			existingSame,
		},
		{
			"数秒の揺れは同じとみなす",
			&fillRow{SongID: &songA, Start: 602, End: 841, EndSource: repository.EndSourceChat},
			existingSame,
		},
		{
			// 直したかった穴：曲が同じなら開始が 25 秒ずれていても same 扱いで黙って飛ばしていた
			"同じ曲だが開始が大きくずれる",
			&fillRow{SongID: &songA, Start: 625, End: 840, EndSource: repository.EndSourceChat},
			existingDiffers,
		},
		{
			"同じ曲だが終了がずれる",
			&fillRow{SongID: &songA, Start: 600, End: 900, EndSource: repository.EndSourceChat},
			existingDiffers,
		},
		{
			"終了が推定値なら終了は比べない",
			&fillRow{SongID: &songA, Start: 600, End: 900, EndSource: repository.EndSourceNextStart},
			existingSame,
		},
		{
			"同じ時間帯に別の曲",
			&fillRow{SongID: &songB, Start: 600, End: 840},
			existingDiffers,
		},
		{
			"曲が決まっていない",
			&fillRow{Start: 600, End: 840},
			existingDiffers,
		},
		{
			"時間帯が離れている",
			&fillRow{SongID: &songB, Start: 1800, End: 2000},
			existingAbsent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffAgainstExisting(existing, tt.row); got != tt.want {
				t.Errorf("diffAgainstExisting() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindDuplicateRow(t *testing.T) {
	songA := uuid.New()
	planned := []*fillRow{
		{SongID: &songA, Name: "ギラギラ", Start: 600},
		{Name: "宿命", Start: 1200},
	}

	// 一意制約は (stream_id, song_id, start_seconds) の完全一致しか止めないので、
	// 数秒ずれた同じ曲は自力で弾く必要がある。
	if findDuplicateRow(planned, &fillRow{SongID: &songA, Name: "ギラギラ", Start: 604}) == nil {
		t.Error("同じ曲が数秒ずれて重なっているのに重複と判定されなかった")
	}
	// 未照合どうしは曲名キーで見る
	if findDuplicateRow(planned, &fillRow{Name: "宿命", Start: 1210}) == nil {
		t.Error("未照合の同名が重なっているのに重複と判定されなかった")
	}
	// 同じ曲でも時間が離れていれば別の歌唱（アンコール等）
	if findDuplicateRow(planned, &fillRow{SongID: &songA, Name: "ギラギラ", Start: 3000}) != nil {
		t.Error("時間が離れた同じ曲を重複と判定した")
	}
	if findDuplicateRow(planned, &fillRow{Name: "怪獣の花唄", Start: 605}) != nil {
		t.Error("別の曲を重複と判定した")
	}
}

func TestExistingNotInSource(t *testing.T) {
	orphan := uuid.New()
	existing := []repository.PerformanceWithDetails{
		{Performance: models.Performance{ID: uuid.New(), StartSeconds: 600}},
		{Performance: models.Performance{ID: uuid.New(), StartSeconds: 1200}},
		{Performance: models.Performance{ID: orphan, StartSeconds: 1800}},
	}
	rows := []*fillRow{{Start: 605}, {Start: 1200}}

	got := existingNotInSource(existing, rows)
	if len(got) != 1 || got[0] != orphan {
		t.Errorf("existingNotInSource() = %v, want [%v]", got, orphan)
	}
	// 源が全部を含んでいれば空。ここが空でないと、実行のたびに
	// 「源に無い」件数が水増しされて履歴が信用できなくなる。
	if n := existingNotInSource(existing, []*fillRow{{Start: 600}, {Start: 1200}, {Start: 1800}}); len(n) != 0 {
		t.Errorf("existingNotInSource() = %v, want empty", n)
	}
}

func TestApplyMissingSongEdits(t *testing.T) {
	songA, songB := uuid.New().String(), uuid.New().String()
	base := dto.MissingSongPayload{
		StreamID: "abc123", SongName: "深昏睡", OriginalArtist: "wowaka",
		StartSeconds: 600, EndSeconds: 840, SongID: songA,
		EndSource: repository.EndSourceChat,
	}

	t.Run("何も直さなければそのまま", func(t *testing.T) {
		out, changed := applyMissingSongEdits(base, base)
		if len(changed) != 0 {
			t.Errorf("changed = %v, want empty", changed)
		}
		if out.EndSource != repository.EndSourceChat {
			t.Errorf("EndSource = %q, want chat（触っていないので由来は据え置き）", out.EndSource)
		}
	})

	t.Run("曲を差し替える", func(t *testing.T) {
		edits := base
		edits.SongID = songB
		out, changed := applyMissingSongEdits(base, edits)
		if out.SongID != songB {
			t.Errorf("SongID = %q, want %q", out.SongID, songB)
		}
		if len(changed) != 1 || changed[0] != "曲" {
			t.Errorf("changed = %v, want [曲]", changed)
		}
	})

	t.Run("照合を解除する", func(t *testing.T) {
		edits := base
		edits.SongID = ""
		out, _ := applyMissingSongEdits(base, edits)
		if out.SongID != "" {
			t.Errorf("SongID = %q, want 空（曲名から作り直す）", out.SongID)
		}
	})

	t.Run("時間を直したら由来は manual になる", func(t *testing.T) {
		edits := base
		edits.EndSeconds = 850
		out, changed := applyMissingSongEdits(base, edits)
		if out.EndSeconds != 850 {
			t.Errorf("EndSeconds = %d, want 850", out.EndSeconds)
		}
		// 人が直した値を chat 検出のものとして残すと、確度での絞り込みが嘘になる
		if out.EndSource != repository.EndSourceManual {
			t.Errorf("EndSource = %q, want manual", out.EndSource)
		}
		if len(changed) != 1 || changed[0] != "終了" {
			t.Errorf("changed = %v, want [終了]", changed)
		}
	})

	t.Run("歌手を選ぶ", func(t *testing.T) {
		edits := base
		edits.SingerIDs = []string{"UC1", "UC2"}
		out, changed := applyMissingSongEdits(base, edits)
		if len(out.SingerIDs) != 2 {
			t.Errorf("SingerIDs = %v, want 2 件", out.SingerIDs)
		}
		if len(changed) != 1 || changed[0] != "歌手" {
			t.Errorf("changed = %v, want [歌手]", changed)
		}
	})

	t.Run("配信は差し替えられない", func(t *testing.T) {
		edits := base
		edits.StreamID = "xyz789"
		out, _ := applyMissingSongEdits(base, edits)
		if out.StreamID != "abc123" {
			t.Errorf("StreamID = %q, want abc123（別の配信の話なら別の提案）", out.StreamID)
		}
	})
}
