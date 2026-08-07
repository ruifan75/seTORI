package service

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// SongMatchService は「この曲名・アーティストは DB のどの楽曲か」を判定する。
//
// 以前は FindByNameAndArtist が *models.Song を返すだけで、答えは
// 「見つかった / 見つからない」の二値しかなかった。そのため
// 「曲名は確実に一致しているがアーティスト表記が違う」（松任谷由実 / 荒井由実 のような
// 同一人物の別名義）を、存在しない曲と同じ扱いにするしかなかった。
// 呼び出し側はそれを新曲として登録するので、照合漏れがそのまま近似重複になる。
//
// ここでは確信度つきの候補列を返し、判断を呼び出し側に委ねる。
//
//	>= AutoMatchScore   自動で結びつけてよい
//	>= ReviewScore      似ている。黙って新曲を作ってはいけない（人の確認に回す）
//	<  ReviewScore      別の曲とみなしてよい
type SongMatchService struct {
	matchRepo  *repository.SongMatchRepository
	songRepo   *repository.SongRepository
	itunesRepo *repository.SongItunesRepository
}

func NewSongMatchService(
	matchRepo *repository.SongMatchRepository,
	songRepo *repository.SongRepository,
	itunesRepo *repository.SongItunesRepository,
) *SongMatchService {
	return &SongMatchService{matchRepo: matchRepo, songRepo: songRepo, itunesRepo: itunesRepo}
}

const (
	// AutoMatchScore 以上なら自動で既存曲に結びつける。
	AutoMatchScore = 0.85
	// ReviewScore 以上なら「似ている」。新曲を作る場合でも統合候補として記録する。
	ReviewScore = 0.50
	// fuzzyThreshold は曲名キーが一致しなかったときの trigram の下限。
	fuzzyThreshold = 0.55
	fuzzyLimit     = 5
)

// MatchReason は判定の根拠。UI とログに出すので安定した文字列にしておく。
const (
	ReasonITunes         = "itunes_id"       // iTunes Track ID が一致
	ReasonExact          = "exact"           // 曲名・アーティストが逐字一致
	ReasonTitleArtist    = "title_artist"    // 曲名キー一致 + アーティストも一致
	ReasonTitlePrimary   = "title_primary"   // 曲名キー一致 + アーティストの主体が一致
	ReasonTitleOverlap   = "title_overlap"   // 曲名キー一致 + アーティスト名が部分的に共通
	ReasonTitleOnly      = "title_only"      // 曲名キー一致 + アーティスト不明
	ReasonTitleMismatch  = "title_mismatch"  // 曲名キー一致 + アーティストが違う（別名義の可能性）
	ReasonTitleAmbiguous = "title_ambiguous" // 同じ曲名キーの曲が複数あり、決め手が無い
	ReasonFuzzy          = "fuzzy_title"     // 曲名が近いだけ
)

// MatchCandidate は照合候補 1 件。
type MatchCandidate struct {
	Song   models.Song
	Score  float64
	Reason string
}

// Auto はこの候補を自動採用してよいか。
func (c MatchCandidate) Auto() bool { return c.Score >= AutoMatchScore }

// NeedsReview はこの候補が「似ているので人に見せるべき」水準か。
func (c MatchCandidate) NeedsReview() bool { return c.Score >= ReviewScore && c.Score < AutoMatchScore }

// FindCandidates は確信度の高い順に候補を返す。候補が無ければ空。
//
// 探索の順序は「iTunes ID → 曲名キー → 曲名の近似」。
// 曲名を先に引くのは、実測（820曲）で曲名キーの衝突がわずか 3 組しかないのに対し、
// アーティスト表記は 12% がクレジット文字列で当てにならないため。
func (s *SongMatchService) FindCandidates(name, artist string, itunesID *int64) ([]MatchCandidate, error) {
	// 1. iTunes ID は人手で紐づけた最も強い証拠
	if itunesID != nil && *itunesID > 0 {
		song, err := s.songRepo.FindByItunesID(*itunesID)
		if err != nil {
			return nil, fmt.Errorf("find by itunes id: %w", err)
		}
		if song != nil {
			return []MatchCandidate{{Song: *song, Score: 1.0, Reason: ReasonITunes}}, nil
		}
	}

	nameKey := songmatch.TitleKey(name)
	queryArtist := songmatch.ParseArtist(artist)

	// 2. 曲名キーで引く
	hits, err := s.matchRepo.FindByNameKey(nameKey)
	if err != nil {
		return nil, err
	}
	if len(hits) > 0 {
		return scoreHits(hits, name, artist, queryArtist), nil
	}

	// 3. 曲名キーすら一致しないときだけ近似検索。ここは自動採用の水準には届かせない
	//    （"惑星ループ" と "惑星ループ2" のような別曲を拾いうるため）。
	similar, err := s.matchRepo.FindSimilarByName(name, fuzzyThreshold, fuzzyLimit)
	if err != nil {
		return nil, err
	}
	var out []MatchCandidate
	for _, h := range similar {
		score := 0.35
		// アーティストまで一致するなら人に見せる価値がある
		if rel := songmatch.CompareArtists(queryArtist, h.ArtistKey); rel >= songmatch.ArtistPrimary {
			score = 0.65
		}
		out = append(out, MatchCandidate{Song: h.Song, Score: score, Reason: ReasonFuzzy})
	}
	return out, nil
}

// scoreHits は曲名キーが一致した候補にアーティストで点をつける。
func scoreHits(hits []repository.KeyedSong, name, artist string, queryArtist songmatch.ArtistKey) []MatchCandidate {
	unique := len(hits) == 1
	out := make([]MatchCandidate, 0, len(hits))

	for _, h := range hits {
		// 逐字一致は文句なし
		if h.Song.Name == name && h.Song.OriginalArtist == artist {
			out = append(out, MatchCandidate{Song: h.Song, Score: 1.0, Reason: ReasonExact})
			continue
		}

		var score float64
		var reason string
		switch songmatch.CompareArtists(queryArtist, h.ArtistKey) {
		case songmatch.ArtistSame:
			score, reason = 0.97, ReasonTitleArtist
		case songmatch.ArtistPrimary:
			score, reason = 0.95, ReasonTitlePrimary
		case songmatch.ArtistOverlap:
			score, reason = 0.90, ReasonTitleOverlap
		case songmatch.ArtistUnknown:
			// アーティストが書かれていない。曲名がその 1 曲しか指しえないなら
			// かなり確からしいが、自動採用の一線は越えさせない。
			// 誤って結びつけると歌唱が別の曲にぶら下がったまま気づけないのに対し、
			// 新曲として登録してしまっても統合候補に出るので後から畳める。
			//
			// なお複数該当のときも候補としては残す。アーティスト不明で新規登録すると
			// 「アーティストが空の曲」ができてしまい、それが正解であることはまず無い。
			if unique {
				score, reason = 0.80, ReasonTitleOnly
			} else {
				score, reason = 0.55, ReasonTitleAmbiguous
			}
		default: // ArtistNone
			// 曲名は一致、アーティストは不一致。
			// 同一人物の別名義（松任谷由実 / 荒井由実）か、同名異曲かの区別が
			// 文字列だけではつかない。人に見せて決めてもらう水準に置く。
			if unique {
				score, reason = 0.60, ReasonTitleMismatch
			} else {
				score, reason = 0.30, ReasonTitleAmbiguous
			}
		}
		out = append(out, MatchCandidate{Song: h.Song, Score: score, Reason: reason})
	}

	sortCandidates(out)
	return out
}

// Best は最有力候補を返す。候補が無ければ nil。
func (s *SongMatchService) Best(name, artist string, itunesID *int64) (*MatchCandidate, error) {
	cands, err := s.FindCandidates(name, artist, itunesID)
	if err != nil || len(cands) == 0 {
		return nil, err
	}
	return &cands[0], nil
}

// sortCandidates は確信度の降順に並べる（同点は元の順＝古い曲が先）。
func sortCandidates(c []MatchCandidate) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].Score > c[j-1].Score; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

// RebuildKeys は照合キーを最新の規則で作り直す（起動時に呼ぶ）。
func (s *SongMatchService) RebuildKeys() (int, error) {
	return s.matchRepo.RebuildStale()
}

// ---------- 統合候補 ----------

// RecordMergeCandidate は新規作成した曲と、それに似た既存曲の組を記録する。
func (s *SongMatchService) RecordMergeCandidate(newSongID, existingSongID uuid.UUID, score float64, reason string) error {
	return s.matchRepo.RecordMergeCandidate(newSongID, existingSongID, score, reason)
}

// ListOpenMergeCandidates は未処理の統合候補を返す。
func (s *SongMatchService) ListOpenMergeCandidates(limit int) ([]repository.MergeCandidate, error) {
	return s.matchRepo.ListOpenMergeCandidates(limit)
}

// CountOpenMergeCandidates は未処理件数を返す。
func (s *SongMatchService) CountOpenMergeCandidates() (int, error) {
	return s.matchRepo.CountOpenMergeCandidates()
}

// FindOpenMergeCandidatesForSong は楽曲詳細で出すための候補を返す。
func (s *SongMatchService) FindOpenMergeCandidatesForSong(songID uuid.UUID) ([]repository.MergeCandidate, error) {
	return s.matchRepo.FindOpenMergeCandidatesForSong(songID)
}

// DismissMergeCandidate は「別の曲なので統合しない」と記録する。
// 却下しておかないと同じ組が毎回一覧に出続けてしまう。
func (s *SongMatchService) DismissMergeCandidate(id uuid.UUID) error {
	return s.matchRepo.SetMergeCandidateStatus(id, "dismissed")
}

// ResolveCandidatesForMergedSong は統合が済んだ楽曲に紐づく候補を閉じる。
func (s *SongMatchService) ResolveCandidatesForMergedSong(sourceID, targetID uuid.UUID) error {
	return s.matchRepo.ResolveCandidatesForMergedSong(sourceID, targetID)
}
