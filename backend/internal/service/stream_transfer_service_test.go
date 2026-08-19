package service

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
)

// 環境間でデータを運ぶ処理は、間違えても「動いてはいる」ように見える種類のものが多い。
// ここで固定するのは、実装を読まないと分からない判断だけ。

// chapter_raw は 3 態（NULL＝未調査 / []＝調べたが章節が無い / 中身あり）。
// 同一視すると、毎回全配信を取り直すか、取得を試す導線が消えるかのどちらかになる。
// omitempty が `[]` まで落としてしまうと、運んだ先で「未調査」に化ける。
func TestExportCacheKeepsChapterRawThreeStates(t *testing.T) {
	tests := []struct {
		name     string
		raw      json.RawMessage
		wantKey  bool
		wantJSON string
	}{
		{"未調査（NULL）", nil, false, ""},
		{"調べたが章節が無い", json.RawMessage(`[]`), true, `[]`},
		{"章節あり", json.RawMessage(`[{"title":"1曲目"}]`), true, `[{"title":"1曲目"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(&dto.ExportAnalysisCache{ChapterRaw: tt.raw})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, ok := back["chapter_raw"]
			if ok != tt.wantKey {
				t.Fatalf("chapter_raw の有無 = %v, want %v (json=%s)", ok, tt.wantKey, encoded)
			}
			if tt.wantKey && string(got) != tt.wantJSON {
				t.Errorf("chapter_raw = %s, want %s", got, tt.wantJSON)
			}
		})
	}
}

// 書き出しには UUID を載せない。載せると取り込み先で別のものを指すか、
// 何も指さずに FK 違反で静かに落ちる。形式を変えたときの歯止め。
func TestStreamExportCarriesNoUUIDFields(t *testing.T) {
	encoded, err := json.Marshal(&dto.StreamExport{
		Version: dto.StreamExportVersion,
		Performances: []dto.ExportPerformance{
			{StartSeconds: 10, Song: dto.ExportSong{Name: "翼をください", OriginalArtist: "赤い鳥"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{"song_id", "performance_id", "artist_id"} {
		if strings.Contains(string(encoded), banned) {
			t.Errorf("書き出しに %s が含まれています: %s", banned, encoded)
		}
	}
}

// 読めない版を渡されたら、部分的に書き込む前に止まること。
func TestImportRejectsNewerVersion(t *testing.T) {
	svc := &StreamTransferService{}
	_, err := svc.Import(&dto.StreamExport{
		Version: dto.StreamExportVersion + 1,
		Stream:  dto.ExportStream{ID: "abc123"},
	}, ImportOptions{})
	if err == nil {
		t.Fatal("新しい版を受け入れてしまいました")
	}
	if !strings.Contains(err.Error(), "対応していない") {
		t.Errorf("想定外のエラー: %v", err)
	}
}

func TestImportRejectsMissingStreamID(t *testing.T) {
	svc := &StreamTransferService{}
	if _, err := svc.Import(&dto.StreamExport{Version: dto.StreamExportVersion}, ImportOptions{}); err == nil {
		t.Fatal("配信 ID が無い書き出しを受け入れてしまいました")
	}
}

// 取り込み先に存在しない歌手を参照したまま書くと performance_singers が FK 違反になる。
// 落とすこと自体は正しいが、落としたものは呼び出し側へ返して警告に出せること。
func TestFilterKnown(t *testing.T) {
	known := map[string]bool{"UC_a": true, "UC_b": true}
	kept, missing := filterKnown([]string{"UC_a", "UC_x", "UC_b", "UC_y"}, known)

	if want := []string{"UC_a", "UC_b"}; !reflect.DeepEqual(kept, want) {
		t.Errorf("kept = %v, want %v", kept, want)
	}
	if want := []string{"UC_x", "UC_y"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

// 空の入力で既存値を消さないこと。「書かれていない」は「消してよい」ではない
// ── 書き出し元が古い／その項目を埋めていないだけ、という状況が普通にある。
func TestKeepIfEmpty(t *testing.T) {
	cur := &models.Singer{PhotoURL: sql.NullString{String: "https://old", Valid: true}}
	get := func(c *models.Singer) sql.NullString { return c.PhotoURL }

	if got := keepIfEmpty("https://new", cur, get); got.String != "https://new" {
		t.Errorf("新しい値が入りませんでした: %+v", got)
	}
	if got := keepIfEmpty("", cur, get); got.String != "https://old" {
		t.Errorf("空文字で既存値が消えました: %+v", got)
	}
	if got := keepIfEmpty("   ", cur, get); got.String != "https://old" {
		t.Errorf("空白だけの値で既存値が消えました: %+v", got)
	}
	if got := keepIfEmpty("", (*models.Singer)(nil), get); got.Valid {
		t.Errorf("新規作成で空文字が NULL になりませんでした: %+v", got)
	}
}

// 歌手の名簿は「参加者」と「歌唱に紐づく歌手」の和集合。重複を除きつつ順序は保つ
// （書き出しの並びが実行ごとに変われば差分が読めなくなる）。
func TestOrderedSet(t *testing.T) {
	s := newOrderedSet()
	for _, v := range []string{"UC_b", "UC_a", "UC_b", "", "UC_c", "UC_a"} {
		s.add(v)
	}
	if want := []string{"UC_b", "UC_a", "UC_c"}; !reflect.DeepEqual(s.items, want) {
		t.Errorf("items = %v, want %v", s.items, want)
	}
}
