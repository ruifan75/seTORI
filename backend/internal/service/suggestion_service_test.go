package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

// mkSuggestion は voteFor に食わせる提案1件を組み立てる。
// before は「提案時点の対象の値」、after は「提案する値」。
func mkSuggestion(user uuid.UUID, before, after map[string]string) models.EditSuggestion {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	return models.EditSuggestion{
		ID:         uuid.New(),
		CreatedBy:  &user,
		BeforeData: b,
		AfterData:  a,
	}
}

// timing は end_seconds だけを持つ before/after の組を作る短縮形。
func timing(beforeEnd, afterEnd string) (map[string]string, map[string]string) {
	return map[string]string{"start_seconds": "100", "end_seconds": beforeEnd},
		map[string]string{"start_seconds": "100", "end_seconds": afterEnd}
}

func TestVoteFor(t *testing.T) {
	current := map[string]string{"start_seconds": "100", "end_seconds": "200"}
	userA, userB, userC := uuid.New(), uuid.New(), uuid.New()

	tests := []struct {
		name    string
		pending []models.EditSuggestion
		wantOK  bool
		want    string
	}{
		{
			name: "1人だけでは自動適用しない",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "197"),
			},
			wantOK: false,
		},
		{
			name: "2人が近い値を出したら中央値を採用",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "197"),
				mkTiming(userB, "200", "198"),
			},
			wantOK: true,
			want:   "197", // 偶数個なので小さい側
		},
		{
			name: "3人の中央値を採る（極端な1つに引きずられない）",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "196"),
				mkTiming(userB, "200", "198"),
				mkTiming(userC, "200", "197"),
			},
			wantOK: true,
			want:   "197",
		},
		{
			name: "同じ人が2回出しても1票（最新が採られる）",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "197"),
				mkTiming(userA, "200", "198"),
			},
			wantOK: false,
		},
		{
			name: "値が割れているときは人手に回す",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "196"),
				mkTiming(userB, "200", "201"),
			},
			wantOK: false,
		},
		{
			name: "現在値から離れすぎている提案は自動適用しない",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "180"),
				mkTiming(userB, "200", "181"),
			},
			wantOK: false,
		},
		{
			name: "提案後に対象が編集されていたら数に入れない",
			pending: []models.EditSuggestion{
				// before が現在値（200）と違う＝この提案は古い前提に基づいている
				mkTiming(userA, "210", "197"),
				mkTiming(userB, "200", "198"),
			},
			wantOK: false,
		},
		{
			name: "匿名（created_by なし）は票に数えない",
			pending: func() []models.EditSuggestion {
				b, a := timing("200", "197")
				anon := mkSuggestion(uuid.New(), b, a)
				anon.CreatedBy = nil
				return []models.EditSuggestion{anon, mkTiming(userB, "200", "197")}
			}(),
			wantOK: false,
		},
		{
			name: "そのフィールドを変えない提案は無視する",
			pending: []models.EditSuggestion{
				mkTiming(userA, "200", "200"),
				mkTiming(userB, "200", "197"),
			},
			wantOK: false,
		},
	}

	s := &SuggestionService{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ids, ok := s.voteFor(tt.pending, current, "end_seconds", DefaultAutoApplySettings())
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (value=%q)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if got != tt.want {
				t.Errorf("value = %q, want %q", got, tt.want)
			}
			if len(ids) == 0 {
				t.Error("採用した提案の ID が返っていない（承認済みにできない）")
			}
		})
	}
}

// mkTiming は「開始 100 秒・終了 beforeEnd 秒の歌唱に対し、終了を afterEnd 秒へ」という提案を作る。
func mkTiming(user uuid.UUID, beforeEnd, afterEnd string) models.EditSuggestion {
	before, after := timing(beforeEnd, afterEnd)
	return mkSuggestion(user, before, after)
}

// 提案は「この項目をこの値に」という意思表示であって対象全体のスナップショットではない。
// after を丸ごと書き戻すと、同じ対象への別の提案を先に反映した分が巻き戻る。
func TestChangedFieldsOf(t *testing.T) {
	before, after := timing("200", "197")
	sug := mkSuggestion(uuid.New(), before, after)

	changed, err := changedFieldsOf(&sug)
	if err != nil {
		t.Fatalf("changedFieldsOf: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("変更フィールドは1つのはず: %v", changed)
	}
	if changed["end_seconds"] != "197" {
		t.Errorf("end_seconds = %q, want 197", changed["end_seconds"])
	}
	if _, ok := changed["start_seconds"]; ok {
		t.Error("変更していない start_seconds が含まれている（他の承認を巻き戻す原因になる）")
	}
}

// しきい値は運用しながら調整できるよう設定値になっている。
// 既定では自動適用されない組み合わせが、緩めた設定では通ることを確かめる。
func TestVoteForRespectsSettings(t *testing.T) {
	current := map[string]string{"start_seconds": "100", "end_seconds": "200"}
	userA, userB := uuid.New(), uuid.New()
	s := &SuggestionService{}

	// 現在値から 20 秒離れた提案：既定（MaxDelta=5）では通らない
	pending := []models.EditSuggestion{
		mkTiming(userA, "200", "180"),
		mkTiming(userB, "200", "181"),
	}
	if _, _, ok := s.voteFor(pending, current, "end_seconds", DefaultAutoApplySettings()); ok {
		t.Error("既定のしきい値では自動適用されないはず")
	}
	loose := AutoApplySettings{Enabled: true, MinVotes: 2, MaxSpreadSeconds: 3, MaxDeltaSeconds: 30}
	got, _, ok := s.voteFor(pending, current, "end_seconds", loose)
	if !ok || got != "180" {
		t.Errorf("緩めた設定では通るはず: ok=%v value=%q", ok, got)
	}

	// MinVotes を上げれば2人では足りなくなる
	strict := AutoApplySettings{Enabled: true, MinVotes: 3, MaxSpreadSeconds: 3, MaxDeltaSeconds: 30}
	if _, _, ok := s.voteFor(pending, current, "end_seconds", strict); ok {
		t.Error("MinVotes=3 では2人の一致で自動適用されないはず")
	}
}

func TestClampAutoApply(t *testing.T) {
	// 1票での自動適用は「提案」の意味が無くなるので下限 2 に丸める
	got := clampAutoApply(AutoApplySettings{Enabled: true, MinVotes: 1, MaxSpreadSeconds: -5, MaxDeltaSeconds: 9999})
	if got.MinVotes != 2 {
		t.Errorf("MinVotes = %d, want 2", got.MinVotes)
	}
	if got.MaxSpreadSeconds != 0 {
		t.Errorf("MaxSpreadSeconds = %d, want 0", got.MaxSpreadSeconds)
	}
	if got.MaxDeltaSeconds != 300 {
		t.Errorf("MaxDeltaSeconds = %d, want 300", got.MaxDeltaSeconds)
	}

	// 範囲内の値はそのまま
	in := AutoApplySettings{Enabled: false, MinVotes: 3, MaxSpreadSeconds: 4, MaxDeltaSeconds: 10}
	if clampAutoApply(in) != in {
		t.Errorf("範囲内の値が変わっている: %+v", clampAutoApply(in))
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		sorted []int
		want   int
	}{
		{[]int{5}, 5},
		{[]int{4, 6}, 4},    // 偶数個は小さい側
		{[]int{1, 5, 9}, 5}, // 外れ値に引きずられない
		{[]int{1, 2, 3, 4}, 2},
	}
	for _, tt := range tests {
		if got := median(tt.sorted); got != tt.want {
			t.Errorf("median(%v) = %d, want %d", tt.sorted, got, tt.want)
		}
	}
}
