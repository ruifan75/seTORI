package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

type batchHolodexScript struct{ err error }

func (s batchHolodexScript) AnalyzeHolodexSongsForBatch(string, bool) ([]dto.SongSuggestion, error) {
	return []dto.SongSuggestion{}, s.err
}

type batchChapterScript struct {
	response *dto.AnalyzeCommentsResponse
	err      error
}

func (s batchChapterScript) AnalyzeChaptersForBatch(string) (*dto.AnalyzeCommentsResponse, error) {
	return s.response, s.err
}

// コメントだけを修正すると、他の入力の障害はまだ「曲なし」に化ける。
// 入力を確定できない経路を履歴へ通し、正常な 0 件も陽性対照にする。
func TestBatchFillOtherInputFailuresAreSkipped(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		holodexErr, chapterErr error
		response               *dto.AnalyzeCommentsResponse
		skipped                bool
	}{
		{name: "Holodex の読み込み失敗", holodexErr: errors.New("holodex DB unavailable"), response: &dto.AnalyzeCommentsResponse{}, skipped: true},
		{name: "チャプターの読み込み失敗", chapterErr: errors.New("chapter DB unavailable"), skipped: true},
		{name: "チャプターの AI 劣化", response: &dto.AnalyzeCommentsResponse{Warning: "AI unavailable"}, skipped: true},
		{name: "チャプターの見送り", response: &dto.AnalyzeCommentsResponse{Deferred: true}, skipped: true},
		{name: "チャプターの不明な応答", skipped: true},
		{name: "正常な空入力", response: &dto.AnalyzeCommentsResponse{}, skipped: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := reviewTestDB(t)
			comments := &batchCommentScript{errors: []error{ErrNoStoredComments}, responses: []*dto.AnalyzeCommentsResponse{nil}}
			s, id := batchFixture(t, db, comments)
			s.holodexService = batchHolodexScript{err: tc.holodexErr}
			s.chapterService = batchChapterScript{response: tc.response, err: tc.chapterErr}
			s.run(id, BatchFillModeUnprocessed, nil, false)
			runs, err := s.ListRuns(20)
			if err != nil || len(runs) != 1 {
				t.Fatalf("履歴: %+v %v", runs, err)
			}
			wantDone, wantSkipped := 1, []string{}
			if tc.skipped {
				wantDone, wantSkipped = 0, []string{"batch123456"}
			}
			if runs[0].StreamsTotal != 1 || runs[0].StreamsDone != wantDone || !reflect.DeepEqual(runs[0].SkippedStreamIDs, wantSkipped) || runs[0].Status != "done" || runs[0].SongsCreated != 0 || runs[0].SongsReview != 0 {
				t.Fatalf("入力の結果と履歴が不一致: %+v", runs[0])
			}
		})
	}
}
