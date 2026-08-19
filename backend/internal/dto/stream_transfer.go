package dto

import (
	"encoding/json"
	"time"
)

// StreamExportVersion は書き出し形式の版。取り込み側が読める版かを判定する。
const StreamExportVersion = 1

// StreamExport は配信 1 本を「別の環境へ持って行ける形」に写したもの。
//
// **UUID を一切載せない。** songs / artists / performances の主キーは環境ごとに
// 独立して採番されるので、持って行っても向こうでは別のものを指すか、何も指さない。
// 同じ曲が両方の DB に別 UUID で存在するのはごく普通に起きる（Holodex 同期も
// findOrCreateSong も、それぞれの環境で勝手に曲を作る）。曲の UUID を運ぶと
// 歌唱がその UUID を参照したまま向こうへ着き、FK 違反で静かに落ちる。
//
// 代わりに曲は名前とアーティストで書き、取り込み側が**自分の songs に対して
// 照合し直す**（docs/SONG_MATCHING.md）。ここで `UNIQUE(name, original_artist)` の
// 完全一致ではなく song_match_keys を通すのが要点で、そうしないと
// `深昏睡` と `深昏睡 (Deep coma)` が別物になって向こうに重複が増える。
//
// 配信（YouTube 動画 ID）と歌手（YouTube チャンネル ID）は自然キーが主キーなので、
// 両方の環境で同じ値になる。そのまま運んでよい。
type StreamExport struct {
	Version      int                  `json:"version"`
	ExportedAt   time.Time            `json:"exported_at"`
	Stream       ExportStream         `json:"stream"`
	Singers      []ExportSinger       `json:"singers"`
	Performances []ExportPerformance  `json:"performances"`
	Cache        *ExportAnalysisCache `json:"cache,omitempty"`
}

// ExportStream は配信そのもの。
//
// order_index は運ばない。CreatePerformances が 0 固定で書いており
// （並びは start_seconds で決まる）、運ぶと往復するように見えて実際はしない。
type ExportStream struct {
	ID              string   `json:"id"` // YouTube 動画 ID
	Title           string   `json:"title"`
	StreamDate      string   `json:"stream_date"` // YYYY-MM-DD
	DurationSeconds *int32   `json:"duration_seconds,omitempty"`
	ThumbnailURL    string   `json:"thumbnail_url,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	OwnerID         string   `json:"owner_id,omitempty"` // 配信チャンネルの歌手 ID
	// ParticipantIDs は stream_singers。Singers 配列とは別に持つ ── あちらは
	// 「この書き出しに出てくる歌手の名簿」で、歌唱にだけ出る飛び入りも含むため、
	// そのまま参加者として書き戻すと配信の参加者が水増しされる。
	ParticipantIDs []string `json:"participant_ids,omitempty"`
	IsProcessed    bool     `json:"is_processed"`
	IsHidden       bool     `json:"is_hidden"`
}

// ExportSinger は歌手。配信の参加者と、歌唱に紐づく歌手の和集合を載せる
// （飛び入りの合唱相手は参加者に入っていないことがあるため）。
//
// organization と organization_override を**分けて運ぶ**。片方に潰すと、
// 取り込み側で「Holodex が言っていること」と「人が決めたこと」の区別が消える
// （§3 の 2 列に分けた理由がそのまま効く）。
type ExportSinger struct {
	ID                   string `json:"id"` // YouTube チャンネル ID
	Name                 string `json:"name"`
	EnglishName          string `json:"english_name,omitempty"`
	PhotoURL             string `json:"photo_url,omitempty"`
	Organization         string `json:"organization,omitempty"`          // Holodex 由来
	OrganizationOverride string `json:"organization_override,omitempty"` // 人の判断
	MetadataSource       string `json:"metadata_source,omitempty"`
	IsHidden             bool   `json:"is_hidden"`
}

// ExportPerformance は歌唱記録 1 件。曲は ID ではなく内容で書く。
type ExportPerformance struct {
	StartSeconds int        `json:"start_seconds"`
	EndSeconds   int        `json:"end_seconds"`
	EndSource    string     `json:"end_source,omitempty"`
	EndConfirmed bool       `json:"end_confirmed"`
	Tags         []string   `json:"tags,omitempty"`
	CustomTags   []string   `json:"custom_tags,omitempty"`
	SingerIDs    []string   `json:"singer_ids,omitempty"`
	Song         ExportSong `json:"song"`
}

// ExportSong は曲を「照合し直せるだけの情報」で書いたもの。
// ItunesID は最も強い証拠なので必ず載せる（§6.6）。
type ExportSong struct {
	Name                  string `json:"name"`
	NameReading           string `json:"name_reading,omitempty"`
	OriginalArtist        string `json:"original_artist"`
	OriginalArtistReading string `json:"original_artist_reading,omitempty"`
	ArtURL                string `json:"art_url,omitempty"`
	ItunesID              *int64 `json:"itunes_id,omitempty"`
}

// ExportAnalysisCache は解析キャッシュ。取り込み側で AI と yt-dlp を回し直さずに
// 済ませるために運ぶ（本番は 1 vCPU / 1GB なので、これを回し直させると重い）。
//
// **hash を必ず一緒に運ぶこと。** 中身だけ運んでも hash が合わなければ
// キャッシュ判定が外れて再解析が走り、運んだ意味が無くなる（§6.6）。
//
// chapter_raw は 3 態（NULL＝未調査 / []＝章節が無い / 中身あり）。
// omitempty は nil だけを落とし `[]` は残すので、この区別は往復しても保たれる。
type ExportAnalysisCache struct {
	HolodexData            json.RawMessage `json:"holodex_data,omitempty"`
	HolodexHash            string          `json:"holodex_hash,omitempty"`
	HolodexSongsNormalized json.RawMessage `json:"holodex_songs_normalized,omitempty"`
	HolodexSongsHash       string          `json:"holodex_songs_hash,omitempty"`
	CommentRaw             json.RawMessage `json:"comment_raw,omitempty"`
	CommentSongs           json.RawMessage `json:"comment_songs,omitempty"`
	CommentSongsHash       string          `json:"comment_songs_hash,omitempty"`
	ChapterRaw             json.RawMessage `json:"chapter_raw,omitempty"`
	ChapterSongs           json.RawMessage `json:"chapter_songs,omitempty"`
	ChapterSongsHash       string          `json:"chapter_songs_hash,omitempty"`
}

// ImportStreamResult は取り込みの結果。**何が起きたかを件数で返す**
// （readings の取り込みと同じ方針。黙って一部を落とすのが一番困るため）。
type ImportStreamResult struct {
	StreamID            string   `json:"stream_id"`
	DryRun              bool     `json:"dry_run"`
	StreamCreated       bool     `json:"stream_created"`
	SingersCreated      int      `json:"singers_created"`
	SingersUpdated      int      `json:"singers_updated"`
	PerformancesCreated int      `json:"performances_created"`
	Suggested           int      `json:"suggested"` // 審査へ回した件数
	Skipped             int      `json:"skipped"`   // 既に同じ提案が待ち行列に居るなどで積まなかった件数
	CacheImported       []string `json:"cache_imported,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
	Errors              []string `json:"errors,omitempty"`
}
