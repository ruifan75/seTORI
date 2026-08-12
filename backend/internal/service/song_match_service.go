package service

import (
	"fmt"
	"sort"
	"strings"
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
	ReasonArtistAlias    = "artist_alias"    // 曲名キー一致 + アーティストが別名義として登録済み
	ReasonAI             = "ai"              // 規則では決まらず、AI が同じ曲だと判定した
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
	nameKey := songmatch.TitleKey(name)
	queryArtist := songmatch.ParseArtist(artist)

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

	rows, err := s.aliasRepo.ListArtistAliases()
	if err != nil {
		// 別名義が引けなくても素の照合はできる。落とさずに素通しする。
		logger.Warnf("load artist aliases failed: %v", err)
		return nil
	}
	// 表示名を照合キーへ畳むのはここ。畳み方の規則は pkg/songmatch が持つので、
	// DB には人が読める表記のまま置いておく（規則を変えても作り直しが要らない）。
	loaded := map[string]string{}
	for _, row := range rows {
		canon := songmatch.ParseArtist(row.Name).Primary
		if canon == "" {
			continue
		}
		for _, alias := range row.Aliases {
			if k := songmatch.ParseArtist(alias).Primary; k != "" && k != canon {
				loaded[k] = canon
			}
		}
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

// AddArtistAlias は「この 2 つは同じ人物」を登録する。
// canonical が本体で、alias がその別名義（DB の楽曲に付いている方を本体にする）。
func (s *SongMatchService) AddArtistAlias(canonical, alias string) error {
	canonical, alias = strings.TrimSpace(canonical), strings.TrimSpace(alias)
	if canonical == "" || alias == "" {
		return fmt.Errorf("アーティスト名が空です")
	}
	if songmatch.ParseArtist(canonical).Primary == songmatch.ParseArtist(alias).Primary {
		return fmt.Errorf("同じ名前どうしは登録できません")
	}
	if err := s.aliasRepo.AddArtistAlias(canonical, alias); err != nil {
		return err
	}
	s.invalidateAliasCache()
	logger.Infof("アーティストの別名義を登録しました: %q = %q", canonical, alias)
	return nil
}

// RemoveArtistAlias は別名義を外し、同時に「別人」として記録する。
//
// 外すだけでは足りない。記録を残さないと、次の読み込みで AI が同じ組を
// また同一人物として提案してくる。
func (s *SongMatchService) RemoveArtistAlias(canonical, alias string) error {
	if err := s.aliasRepo.RemoveArtistAlias(canonical, alias); err != nil {
		return err
	}
	keyA := songmatch.ParseArtist(canonical).Primary
	keyB := songmatch.ParseArtist(alias).Primary
	if keyA != "" && keyB != "" && keyA != keyB {
		if err := s.aliasRepo.RecordArtistRejection(keyA, keyB, "manual", "人が別名義の登録を解除"); err != nil {
			logger.Warnf("record manual rejection failed: %v", err)
		}
	}
	s.invalidateAliasCache()
	return nil
}

// RejectedArtistPairs は「別人」と記録済みの組を返す（AI への再問い合わせ抑止）。
func (s *SongMatchService) RejectedArtistPairs(pairKeys []string) (map[string]bool, error) {
	return s.aliasRepo.FindArtistRejections(pairKeys)
}

// RecordArtistRejection は「この 2 つは別人」を残す。
func (s *SongMatchService) RecordArtistRejection(keyA, keyB, source, note string) error {
	return s.aliasRepo.RecordArtistRejection(keyA, keyB, source, note)
}

// ---------- 楽曲 ----------

// FindSong は照合先の楽曲を1件引く。
func (s *SongMatchService) FindSong(id uuid.UUID) (*models.Song, error) {
	return s.songRepo.FindByID(id)
}

// OnSongMerged は統合の後始末。消える楽曲を指していた否定の記録を統合先へ移す。
// 移さないと「この表記はこの曲ではない」が楽曲ごと消え（ON DELETE CASCADE）、
// 統合するたびに同じ誤判定が復活する。
func (s *SongMatchService) OnSongMerged(source, target *models.Song) {
	if s.aliasRepo == nil || source == nil || target == nil {
		return
	}
	if err := s.aliasRepo.RepointSongIdentityChecks(nil, source.ID, target.ID); err != nil {
		logger.Warnf("repoint song identity checks failed: %v", err)
	}
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

// RejectedSongPairs は「別の曲」と記録済みの組を返す（AI への再問い合わせ抑止）。
func (s *SongMatchService) RejectedSongPairs(pairKeys []string) (map[string]bool, error) {
	return s.aliasRepo.FindSongRejections(pairKeys)
}

// RecordSongRejection は「この表記はこの曲ではない」を残す。
func (s *SongMatchService) RecordSongRejection(nameKey, artistKey string, songID uuid.UUID, source, note string) error {
	return s.aliasRepo.RecordSongRejection(nameKey, artistKey, songID, source, note)
}

// CandidatesForAI は AI に見せる候補を集める。
//
// FindCandidates と分けているのは、**求めるものが違う**から。
// 照合は精度が要る（誤ると歌唱が別の曲にぶら下がる）が、こちらは召回でよい
// ── 後ろに AI という裁き手が居るので、外れが混ざっても捨てられるだけ。
// 実測でも接頭辞で拾うと同じ曲を指す率は 2 割だったが、裁き手が居るなら十分。
func (s *SongMatchService) CandidatesForAI(name, artist string) ([]MatchCandidate, error) {
	cands, err := s.FindCandidates(name, artist, nil)
	if err != nil {
		return nil, err
	}
	// 既に人に見せる水準の候補があるなら、それを聞けばよい
	var out []MatchCandidate
	for _, c := range cands {
		if c.Score >= ReviewScore {
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out, nil
	}

	// 候補が無い場合だけ、曲名キーの接頭辞で拾い直す。
	// 深昏睡 → 深昏睡deepcoma、革命道中 → 革命道中ontheway のように、
	// コメントの表記が DB のキーの接頭辞になっている型を拾うため。
	//
	// **文字数で足切りしないこと。** 一度 4 文字未満を弾いていて「深昏睡」(3 文字) が
	// 漏れ、2 文字に下げても「唱」(Ado) のような 1 文字の曲名が漏れた。
	// 実測すると短い曲名は珍しくない（819 曲中 1 文字 14・2 文字 29・3 文字 64）。
	// そもそも当たり過ぎるかどうかを決めるのはヒット数であって字数ではない
	// ── 「怪獣」は 2 文字でも 1 件しか当たらず、「恋」は 1 文字で 10 件当たる。
	// 数は下の LIMIT で抑える（キー長の昇順なので、近いものから順に取れる）。
	nameKey := songmatch.TitleKey(name)
	hits, err := s.matchRepo.FindByNameKeyPrefix(nameKey, identityPrefixLimit)
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		out = append(out, MatchCandidate{Song: h.Song, Score: 0.0, Reason: ReasonFuzzy})
	}
	return out, nil
}

// identityPrefixLimit は 1 件あたりの問い合わせ数の上限（AI の入力を膨らませないため）。
// 当たり過ぎるキー（「恋」で 10 件など）はここで頭打ちになる。
const identityPrefixLimit = 3

// ---------- AI に見せる曲庫 ----------

// AICatalog は登録曲の一覧を AI へ渡すためのスナップショット。
//
// 短い id（s1, s2, …）を振り直しているのは、UUID を 820 行ぶん並べると
// それだけで入力の大半を占めてしまうため。対応表は呼び出し側が持つ。
type AICatalog struct {
	IDs  map[string]uuid.UUID
	text string
}

// CatalogForAI は現在の登録曲からスナップショットを作る。
//
// 一括処理では**開始時に一度だけ**作って使い回すこと。理由は 2 つ。
//   - 一覧は問いの先頭に置くので、内容が同じなら provider の prefix cache に載る
//   - その回の処理で自分が作った新曲を、次の行の照合相手にしない
//     （AI の誤りが次の判定の材料になると、誤りが増幅する）
func (s *SongMatchService) CatalogForAI() (*AICatalog, error) {
	songs, err := s.matchRepo.ListAllForScan()
	if err != nil {
		return nil, fmt.Errorf("catalog for ai: %w", err)
	}
	c := &AICatalog{IDs: make(map[string]uuid.UUID, len(songs))}
	var sb strings.Builder
	for i, song := range songs {
		key := fmt.Sprintf("s%d", i+1)
		c.IDs[key] = song.ID
		fmt.Fprintf(&sb, "%s|%s|%s\n", key, song.Name, song.OriginalArtist)
	}
	c.text = sb.String()
	return c, nil
}

// Len は一覧に載っている曲数。
func (c *AICatalog) Len() int { return len(c.IDs) }

// Prompt は曲庫つきの問いを組み立てる。
// **一覧を先頭に置くこと。** 呼び出しごとに変わらない部分を前に固めておくと、
// provider の prefix cache が効いて 2 回目以降の入力が桁で安くなる。
func (c *AICatalog) Prompt(rows []*aiMatchRow) string {
	var sb strings.Builder
	sb.WriteString("登録済みの楽曲一覧（形式 id|曲名|アーティスト）:\n")
	sb.WriteString(c.text)
	sb.WriteString("\n次の各行について、一覧の中に同じ曲があればその id を、無ければ null を返してください。\n\n")
	for i, r := range rows {
		fmt.Fprintf(&sb, "%d. 曲名「%s」 アーティスト「%s」\n", i, r.Name, orDash(r.Artist))
	}
	return sb.String()
}
