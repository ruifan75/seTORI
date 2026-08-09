package service

import (
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
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
	aliasRepo  *repository.AliasRepository

	// アーティストの別名義は照合のたびに要るので読み込んだまま持つ。
	// 数百件規模で、書き込みは人の操作か AI 判定のときだけなので、
	// 書いた側が明示的に捨てる方式にしている（TTL だと反映の遅れが読めない）。
	aliasMu     sync.RWMutex
	artistCanon map[string]string
}

func NewSongMatchService(
	matchRepo *repository.SongMatchRepository,
	songRepo *repository.SongRepository,
	itunesRepo *repository.SongItunesRepository,
	aliasRepo *repository.AliasRepository,
) *SongMatchService {
	return &SongMatchService{matchRepo: matchRepo, songRepo: songRepo, itunesRepo: itunesRepo, aliasRepo: aliasRepo}
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
	ReasonSongAlias      = "song_alias"      // 学習済みの別表記（過去に人が統合した組）
	ReasonArtistAlias    = "artist_alias"    // 曲名キー一致 + アーティストが別名義として登録済み
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
// 探索の順序は「学習済みの別表記 → iTunes ID → 曲名キー → 曲名の近似」。
// 曲名を先に引くのは、実測（820曲）で曲名キーの衝突がわずか 3 組しかないのに対し、
// アーティスト表記は 12% がクレジット文字列で当てにならないため。
func (s *SongMatchService) FindCandidates(name, artist string, itunesID *int64) ([]MatchCandidate, error) {
	nameKey := songmatch.TitleKey(name)
	queryArtist := songmatch.ParseArtist(artist)

	// 0. 過去に人が「これは同じ曲だ」と判断した表記なら、そこで終わり。
	//    類似度計算も AI も通さないので、使うほど速く・確実になる。
	if s.aliasRepo != nil {
		if songID, err := s.aliasRepo.FindSongAlias(nameKey, queryArtist.String()); err != nil {
			logger.Warnf("song alias lookup failed: %v", err)
		} else if songID != nil {
			song, err := s.songRepo.FindByID(*songID)
			if err != nil {
				return nil, fmt.Errorf("find aliased song: %w", err)
			}
			if song != nil {
				return []MatchCandidate{{Song: *song, Score: 1.0, Reason: ReasonSongAlias}}, nil
			}
		}
	}

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

	canon := s.artistAliasMap()

	// 2. 曲名キーで引く
	hits, err := s.matchRepo.FindByNameKey(nameKey)
	if err != nil {
		return nil, err
	}
	if len(hits) > 0 {
		return scoreHits(hits, name, artist, queryArtist, canon), nil
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
		if rel, _ := compareArtists(queryArtist, h.ArtistKey, canon); rel >= songmatch.ArtistPrimary {
			score = 0.65
		}
		out = append(out, MatchCandidate{Song: h.Song, Score: score, Reason: ReasonFuzzy})
	}
	return out, nil
}

// compareArtists は別名義を織り込んでアーティストを突き合わせる。
// 2 つ目の戻り値は「素の表記では一致せず、別名義の登録があって初めて一致した」ことを示す。
//
// 素の比較を先に試すのは、判定の根拠を UI とログに正しく出すため。
// 別名義で救われた一致はそう見えていないと、誤った別名義登録に気づけない。
func compareArtists(a, b songmatch.ArtistKey, canon map[string]string) (songmatch.ArtistRelation, bool) {
	direct := songmatch.CompareArtists(a, b)
	if direct != songmatch.ArtistNone || len(canon) == 0 {
		return direct, false
	}
	viaAlias := songmatch.CompareArtists(canonicalizeArtistKey(a, canon), canonicalizeArtistKey(b, canon))
	return viaAlias, viaAlias != songmatch.ArtistNone
}

// canonicalizeArtistKey は各名前を別名義グループの代表に寄せる。
// 「松任谷由実」と「荒井由実」が同じグループなら、どちらも同じ文字列になる。
func canonicalizeArtistKey(k songmatch.ArtistKey, canon map[string]string) songmatch.ArtistKey {
	out := songmatch.ArtistKey{Primary: k.Primary}
	if c, ok := canon[k.Primary]; ok {
		out.Primary = c
	}
	seen := map[string]bool{}
	for _, t := range k.Tokens {
		if c, ok := canon[t]; ok {
			t = c
		}
		if !seen[t] {
			seen[t] = true
			out.Tokens = append(out.Tokens, t)
		}
	}
	sort.Strings(out.Tokens)
	return out
}

// scoreHits は曲名キーが一致した候補にアーティストで点をつける。
func scoreHits(hits []repository.KeyedSong, name, artist string, queryArtist songmatch.ArtistKey, canon map[string]string) []MatchCandidate {
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
		rel, viaAlias := compareArtists(queryArtist, h.ArtistKey, canon)
		switch rel {
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
				// 同じ曲名の曲が複数ある状態そのものが要確認。実データでは
				// 「artist 欄の意味が違うだけの二重登録（惑星ループ）」
				// 「編曲違いで意図的に別（翼をください）」
				// 「そもそも同名異曲（オレンジ）」が混在していて、データからは区別できない。
				// 以前はここを 0.30 にして黙殺していたが、それでは重複が生き延びる。
				score, reason = 0.50, ReasonTitleAmbiguous
			}
		}
		// 別名義の登録で救われた一致は、そうと分かる理由に差し替える
		if viaAlias && score >= AutoMatchScore {
			reason = ReasonArtistAlias
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

// ---------- アーティストの別名義（Layer 2） ----------

// artistAliasMap は別名義の対応表を返す。初回だけ DB を読み、以後は保持する。
// 別名義が 1 件も無い場合も「読んだ」状態にして毎回問い合わせないようにする。
func (s *SongMatchService) artistAliasMap() map[string]string {
	if s.aliasRepo == nil {
		return nil
	}
	s.aliasMu.RLock()
	m := s.artistCanon
	s.aliasMu.RUnlock()
	if m != nil {
		return m
	}

	loaded, err := s.aliasRepo.LoadArtistAliasMap()
	if err != nil {
		// 別名義が引けなくても素の照合はできる。落とさずに素通しする。
		logger.Warnf("load artist alias map failed: %v", err)
		return nil
	}
	if loaded == nil {
		loaded = map[string]string{}
	}
	s.aliasMu.Lock()
	s.artistCanon = loaded
	s.aliasMu.Unlock()
	return loaded
}

// invalidateAliasCache は別名義を書き換えたあとに呼ぶ。
func (s *SongMatchService) invalidateAliasCache() {
	s.aliasMu.Lock()
	s.artistCanon = nil
	s.aliasMu.Unlock()
}

// LinkArtistAliases は 2 つのアーティスト表記を同一人物として登録する。
// 表記はそのまま渡してよい（内部で照合キーへ畳む）。
func (s *SongMatchService) LinkArtistAliases(displayA, displayB, source, note string) error {
	keyA := songmatch.ParseArtist(displayA).Primary
	keyB := songmatch.ParseArtist(displayB).Primary
	if keyA == "" || keyB == "" {
		return fmt.Errorf("アーティスト名が空です")
	}
	if keyA == keyB {
		return fmt.Errorf("同じ名前どうしは登録できません")
	}
	if err := s.aliasRepo.LinkArtists(keyA, displayA, keyB, displayB, source, note); err != nil {
		return err
	}
	// 判定履歴にも残す。ここを書かないと AI に何度も同じ組を聞いてしまう。
	if err := s.aliasRepo.RecordCheck(keyA, keyB, true, source, note); err != nil {
		logger.Warnf("record alias check failed: %v", err)
	}
	s.invalidateAliasCache()
	logger.Infof("アーティストの別名義を登録しました: %q = %q (%s)", displayA, displayB, source)
	return nil
}

// UnlinkArtistAlias は別名義グループから 1 名を外す（多くは AI の誤判定の取り消し）。
//
// 外すだけでは足りない。判定履歴を放置すると、次の解析で同じ組がまた
// 「未判定」として AI に送られ、同じ誤りが復活する。人が外した組は
// source=manual の否定として残し、AI の判断より優先させる。
func (s *SongMatchService) UnlinkArtistAlias(nameKey string) error {
	// 外す前に、同じグループの誰と切り離すことになるのかを控えておく
	var siblings []string
	groups, err := s.aliasRepo.ListArtistAliasGroups()
	if err != nil {
		logger.Warnf("list alias groups before unlink failed: %v", err)
	} else {
		for _, g := range groups {
			if !containsMember(g.Members, nameKey) {
				continue
			}
			for _, m := range g.Members {
				if m.NameKey != nameKey {
					siblings = append(siblings, m.NameKey)
				}
			}
		}
	}

	if err := s.aliasRepo.UnlinkArtist(nameKey); err != nil {
		return err
	}
	for _, other := range siblings {
		if err := s.aliasRepo.RecordCheck(nameKey, other, false, "manual", "人が別名義の登録を解除"); err != nil {
			logger.Warnf("record manual unlink verdict failed: %v", err)
		}
	}
	s.invalidateAliasCache()
	logger.Infof("アーティストの別名義を解除しました: %s（%d 組を別人として記録）", nameKey, len(siblings))
	return nil
}

func containsMember(members []repository.ArtistAliasMember, nameKey string) bool {
	for _, m := range members {
		if m.NameKey == nameKey {
			return true
		}
	}
	return false
}

// ListArtistAliasGroups は別名義グループの一覧を返す。
func (s *SongMatchService) ListArtistAliasGroups() ([]repository.ArtistAliasGroup, error) {
	return s.aliasRepo.ListArtistAliasGroups()
}

// CheckedArtistPairs は既に判定済みの組を返す（AI への再問い合わせ抑止）。
func (s *SongMatchService) CheckedArtistPairs(pairKeys []string) (map[string]bool, error) {
	return s.aliasRepo.FindCheckedPairs(pairKeys)
}

// RecordArtistAliasVerdict は「同一人物ではない」も含めて判定を残す。
func (s *SongMatchService) RecordArtistAliasVerdict(keyA, keyB string, same bool, source, note string) error {
	return s.aliasRepo.RecordCheck(keyA, keyB, same, source, note)
}

// ---------- 楽曲の別表記（Layer 3） ----------

// FindSong は照合先の楽曲を1件引く。
// 人が候補を確定させる操作で「その曲がまだ在るか」を確かめるために要る
// （消えた曲を指す別表記を学習すると、次の照合が毎回 FK 違反で落ちる）。
func (s *SongMatchService) FindSong(id uuid.UUID) (*models.Song, error) {
	return s.songRepo.FindByID(id)
}

// LearnSongAlias は「この表記はこの楽曲だ」を記録する。
func (s *SongMatchService) LearnSongAlias(name, artist string, songID uuid.UUID, source string) error {
	nameKey := songmatch.TitleKey(name)
	artistKey := songmatch.ParseArtist(artist).String()
	return s.aliasRepo.PutSongAlias(nil, nameKey, artistKey, songID, source)
}

// ListSongAliases は学習済みの別表記を返す。
func (s *SongMatchService) ListSongAliases(limit int) ([]repository.SongAliasRow, error) {
	return s.aliasRepo.ListSongAliases(limit)
}

// DeleteSongAlias は学習した対応を取り消す。
func (s *SongMatchService) DeleteSongAlias(nameKey, artistKey string) error {
	return s.aliasRepo.DeleteSongAlias(nameKey, artistKey)
}

// LearnFromMerge は楽曲の統合から別表記を学習する。
//
// 030 の受け皿のおかげで、照合を外した表記はそのまま新曲として登録されている。
// つまり統合元の名前は「照合に失敗したときの生の表記」そのもので、
// 統合はそれを「実はこの曲だった」と人が確定させる操作になっている。
// ここを学習に使うと、次から同じ表記が来ても迷わない。
func (s *SongMatchService) LearnFromMerge(source, target *models.Song) {
	if s.aliasRepo == nil || source == nil || target == nil {
		return
	}
	// 統合元を指していた学習済みの表記は統合先へ付け替える（曲ごと消えるため）
	if err := s.aliasRepo.RepointSongAliases(nil, source.ID, target.ID); err != nil {
		logger.Warnf("repoint song aliases failed: %v", err)
	}

	srcName, srcArtist := songmatch.TitleKey(source.Name), songmatch.ParseArtist(source.OriginalArtist)
	dstName, dstArtist := songmatch.TitleKey(target.Name), songmatch.ParseArtist(target.OriginalArtist)
	if srcName == dstName && srcArtist.String() == dstArtist.String() {
		return // 照合キーが同じなら学ぶことは無い（元から当たる）
	}
	if err := s.aliasRepo.PutSongAlias(nil, srcName, srcArtist.String(), target.ID, "merge"); err != nil {
		logger.Warnf("learn song alias failed: %v", err)
		return
	}
	logger.Infof("統合から別表記を学習しました: %q / %q → %q / %q",
		source.Name, source.OriginalArtist, target.Name, target.OriginalArtist)
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
