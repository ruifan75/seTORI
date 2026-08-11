package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// 「この表記は、この既存曲と同じ楽曲か」を AI に判定させる。
//
// 規則で書こうとして行き詰まった場所がここ。深昏睡 と 深昏睡 (Deep coma) は同じ曲、
// Starry night と Starry night (instrumental) は別録音。どちらも「括弧内の未知語」で、
// 語彙リストでもスクリプトの違いでも切れない（前者は翻訳、後者は版）。
// 実測では接頭辞＋区切り記号で拾うと、同じ曲を指す率が 2 割しかなかった。
//
// LLM はこれを知っている。10 組で試したところ 9 組正解で、外した 1 組
// （惑星ループ Eve / ナユタン星人）も「編曲が同じカバーは同一」という基準を
// 足したら正しくなった。同じ基準で 翼をください（編曲違い）は別のまま保たれる。
//
// 設計は artist_alias_ai と同じ 3 点。
//  1. 聞くのは一度だけ。肯定・否定とも song_identity_checks に残す
//  2. 呼ぶのは解析時だけ。読み取り時（ResolveForDisplay）からは絶対に呼ばない
//     ── 画面を開くたびに AI 呼び出しが走るとページが止まる
//  3. 失敗は素通し。判定できなくても照合は規則の結果のまま進み、
//     決着しない組は統合候補として人に回る

const songIdentitySystemPrompt = `あなたは楽曲データベースの照合を助けるアシスタントです。
「コメントに書かれた曲」と「データベースにある曲」が**同じ楽曲か**を判定してください。

## same=true にするもの

- 表記の違いだけ（翻訳・ローマ字・副題・記号・クレジットの書き方）
  例: 深昏睡 と 深昏睡 (Deep coma)、革命道中 と 革命道中 - On The Way
- **カバー・歌ってみたでも、編曲が原曲と同じもの**
  誰が歌ったかは別に記録するので、楽曲としては同一に扱う
  例: 群青 / YOASOBI と 群青 / 歌ってみた
- アーティスト欄が作曲者と原唱のどちらを書いたかの違い
  例: 惑星ループ / ナユタン星人（作曲）と 惑星ループ / Eve（原唱）

## same=false にするもの

- **編曲・録音が違うもの**（これが最も重要な軸）
  instrumental / Remix / Reloaded / アコースティック / 和風アレンジ /
  大きく編曲し直したカバー
  例: Starry night と Starry night (instrumental) は別
  例: 翼をください / 赤い鳥 と 翼をください / 桜高軽音部 は編曲が違うので別
- 曲名が似ているだけの別の曲
  例: ダーリン と ダーリンダンス、オレンジ / SPYAIR と オレンジ / 逢坂大河
- **少しでも確信が持てないもの**

確信が持てない場合は必ず same=false にしてください。
false は新しい曲として登録されるだけで、あとから人が統合できます。
true を誤ると歌唱が別の曲にぶら下がったまま誰も気づきません。

## 出力

JSON配列のみ。説明文を付けないこと。
各要素: {"i":番号,"same":true/false,"why":"30字以内の理由"}`

// songIdentityQuestion は 1 件の問い。
type songIdentityQuestion struct {
	NameKey   string    // 判定を保存するときのキー
	ArtistKey string    //
	SongID    uuid.UUID // 突き合わせた既存曲
	QueryName string    // 画面・AI に見せる表記
	QueryArt  string
	SongName  string
	SongArt   string
}

type songIdentityVerdict struct {
	Index int    `json:"i"`
	Same  bool   `json:"same"`
	Why   string `json:"why"`
}

// adjudicateSongIdentity は未判定の組だけを AI に問い、同一と判定できた数を返す。
// 同一と判定した組は song_aliases にも書くので、次からは規則だけで即解決する。
func (s *NormalizationService) adjudicateSongIdentity(qs []songIdentityQuestion) int {
	if s.matchService == nil || s.aiClient == nil || len(qs) == 0 {
		return 0
	}

	pending, err := s.filterUncheckedSongPairs(qs)
	if err != nil {
		logger.Warnf("song identity check lookup failed: %v", err)
		return 0
	}
	if len(pending) == 0 {
		return 0
	}

	var sb strings.Builder
	sb.WriteString("次の組を判定してください：\n\n")
	for i, q := range pending {
		fmt.Fprintf(&sb, "[%d] コメント: %s ／ %s\n    データベース: %s ／ %s\n",
			i, q.QueryName, orDash(q.QueryArt), q.SongName, orDash(q.SongArt))
	}

	logger.Infof("AI に楽曲の同一性を問い合わせます: %d 組", len(pending))
	resp, err := s.aiClient.SimpleChat(songIdentitySystemPrompt, sb.String())
	if err != nil {
		// 判定できなくても照合は規則の結果のまま進む。統合候補として人に回るだけ。
		logger.Warnf("song identity adjudication failed: %v", err)
		return 0
	}

	var verdicts []songIdentityVerdict
	if err := json.Unmarshal([]byte(ai.CleanJSONResponse(resp)), &verdicts); err != nil {
		logger.Warnf("song identity response parse failed: %v (resp=%.200s)", err, resp)
		return 0
	}

	linked := 0
	for _, v := range verdicts {
		if v.Index < 0 || v.Index >= len(pending) {
			continue
		}
		q := pending[v.Index]
		if err := s.matchService.RecordSongIdentityVerdict(q.NameKey, q.ArtistKey, q.SongID, v.Same, "ai", v.Why); err != nil {
			logger.Warnf("record song identity verdict failed: %v", err)
			continue
		}
		if !v.Same {
			continue
		}
		// 同一と判定できたら別表記として学習する。次からは AI を通らず確信度 1.00 で解決する。
		if err := s.matchService.LearnSongAliasByKey(q.NameKey, q.ArtistKey, q.SongID, "ai"); err != nil {
			logger.Warnf("learn song alias from identity verdict failed: %v", err)
			continue
		}
		linked++
		logger.Infof("AI 判定: %q / %q は %q / %q と同じ楽曲（%s）",
			q.QueryName, q.QueryArt, q.SongName, q.SongArt, v.Why)
	}
	return linked
}

// filterUncheckedSongPairs は「まだ聞いていない組」だけに絞る。
func (s *NormalizationService) filterUncheckedSongPairs(qs []songIdentityQuestion) ([]songIdentityQuestion, error) {
	keys := make([]string, 0, len(qs))
	for _, q := range qs {
		keys = append(keys, songmatch.IdentityPairKey(q.NameKey, q.ArtistKey, q.SongID.String()))
	}
	checked, err := s.matchService.CheckedSongPairs(keys)
	if err != nil {
		return nil, err
	}
	var out []songIdentityQuestion
	seen := map[string]bool{}
	for i, q := range qs {
		k := keys[i]
		if _, done := checked[k]; done || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, q)
	}
	return out, nil
}

func orDash(s string) string {
	if s == "" {
		return "（未記入）"
	}
	return s
}

// AdjudicateSongIdentityForCommentSongs は照合が決着しなかった行について、
// 「似ている既存曲と同じ楽曲か」を AI に判定させる。
//
// 対象は「自動採用できていない」行。候補が出ているものはその候補を、
// 候補すら出ていないものは近似検索で拾い直してから聞く
// ── 深昏睡 と 深昏睡 (Deep coma) は曲名キーが一致しないので、
// 候補が出ないところまで含めて見ないと拾えない。
//
// **解析時にだけ呼ぶこと。** 読み取り時（ResolveForDisplay）から呼ぶと
// 画面を開くたびに AI 呼び出しが走る。
func (s *NormalizationService) AdjudicateSongIdentityForCommentSongs(songs []dto.CommentSong) (asked, linked int) {
	if s.matchService == nil || s.aiClient == nil {
		return 0, 0
	}
	var qs []songIdentityQuestion
	for i := range songs {
		if songs[i].MatchedSongID != nil {
			continue // 既に自動採用できている
		}
		name, artist := MatchInputs(songs[i].Name, songs[i].OriginalArtist,
			songs[i].NormalizedName, songs[i].NormalizedArtist)
		nameKey := songmatch.TitleKey(name)
		if nameKey == "" {
			continue // 照合キーを作れない表記は学習もできない
		}
		artistKey := songmatch.ParseArtist(artist).String()

		cands, err := s.matchService.CandidatesForIdentity(name, artist)
		if err != nil {
			logger.Warnf("identity candidate lookup failed (%s): %v", name, err)
			continue
		}
		for _, c := range cands {
			qs = append(qs, songIdentityQuestion{
				NameKey: nameKey, ArtistKey: artistKey, SongID: c.Song.ID,
				QueryName: name, QueryArt: artist,
				SongName: c.Song.Name, SongArt: c.Song.OriginalArtist,
			})
		}
	}
	if len(qs) == 0 {
		return 0, 0
	}
	return len(qs), s.adjudicateSongIdentity(qs)
}

// AdjudicateSongIdentityForSuggestions は Holodex 経路の同じ処理。
//
// Holodex は曲ごとに iTunes ID を持つので、そこで決着する分はここへ来ない。
// 残るのは ID が無い曲・ID が DB に無い曲で、コメント経路と同じ型の問いになる。
func (s *NormalizationService) AdjudicateSongIdentityForSuggestions(songs []dto.SongSuggestion) (asked, linked int) {
	if s.matchService == nil || s.aiClient == nil {
		return 0, 0
	}
	var qs []songIdentityQuestion
	for i := range songs {
		if songs[i].MatchedSongID != nil {
			continue
		}
		name, artist := MatchInputs(songs[i].Name, songs[i].OriginalArtist,
			songs[i].NormalizedName, songs[i].NormalizedArtist)
		nameKey := songmatch.TitleKey(name)
		if nameKey == "" {
			continue
		}
		artistKey := songmatch.ParseArtist(artist).String()

		cands, err := s.matchService.CandidatesForIdentity(name, artist)
		if err != nil {
			logger.Warnf("identity candidate lookup failed (%s): %v", name, err)
			continue
		}
		for _, c := range cands {
			qs = append(qs, songIdentityQuestion{
				NameKey: nameKey, ArtistKey: artistKey, SongID: c.Song.ID,
				QueryName: name, QueryArt: artist,
				SongName: c.Song.Name, SongArt: c.Song.OriginalArtist,
			})
		}
	}
	if len(qs) == 0 {
		return 0, 0
	}
	return len(qs), s.adjudicateSongIdentity(qs)
}

// repoIdentityPairKey は repository 側と同じキーの作り方（テストで一致を確かめる用）。
func repoIdentityPairKey(nameKey, artistKey string, songID uuid.UUID) string {
	return repository.SongIdentityPairKey(nameKey, artistKey, songID)
}
