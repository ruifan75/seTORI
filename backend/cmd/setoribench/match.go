package main

// 照合（抽出した曲名 → DB のどの楽曲か）の評価。
//
// 抽出の評価とは指標を分けている。抽出は「曲を見つけたか」、照合は「正しい曲に
// 結びついたか」で、後者は**間違え方によってコストが違う**。
//
//	自動採用して別の曲   … 歌唱が別の曲にぶら下がったまま誰も気づかない
//	人手に回る           … 運用コストは増えるが誤りは残らない
//	候補すら出ない       … 新曲として登録される。統合候補に出るので後から畳める
//
// これをまとめて「精度」にすると、閾値を下げて誤採用を増やす改悪が
// 数字の上では改善に見える。だから必ず分けて出す。

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/comment"
)

// matchTier は照合結果の段階。service 側の閾値（AutoMatchScore / ReviewScore）に対応する。
type matchTier int

const (
	tierNone   matchTier = iota // 候補なし → 新規登録される
	tierReview                  // 0.50〜0.85 → 人手に回る
	tierAuto                    // 0.85 以上 → 自動採用
)

// matchOutcome は 1 件の照合結果。
// 診断で読むものなので、JSON のキー名は全て明示する（大文字始まりが混ざると
// dump を読む側が取り違える）。
type matchOutcome struct {
	Stream      string  `json:"stream"`
	Start       int     `json:"start"`
	QueryName   string  `json:"query_name"`   // 照合に渡した曲名
	QueryArtist string  `json:"query_artist"` // 照合に渡したアーティスト
	GTName      string  `json:"gt_name"`      // 正解（performances が指す楽曲）
	GTArtist    string  `json:"gt_artist"`
	GotName     string  `json:"got_name"` // 最有力候補
	GotArtist   string  `json:"got_artist"`
	Reason      string  `json:"reason"`
	Score       float64 `json:"score"`
	Tier        string  `json:"tier"`     // auto | review | none
	Correct     bool    `json:"correct"`  // 最有力候補が正解の曲か
	InCands     bool    `json:"in_cands"` // 正解が候補列のどこかに居るか

	tier matchTier
}

// matchEval は照合の集計。
type matchEval struct {
	total       int
	autoCorrect int
	autoWrong   int
	reviewHit   int // 人手に回るが候補に正解が居る
	reviewMiss  int // 人手に回り、候補に正解が居ない
	none        int
	byReason    map[string]*reasonStat
}

type reasonStat struct{ n, correct, wrong int }

func newMatchEval() *matchEval {
	return &matchEval{byReason: map[string]*reasonStat{}}
}

func (e *matchEval) add(o matchOutcome) {
	e.total++
	switch o.tier {
	case tierAuto:
		if o.Correct {
			e.autoCorrect++
		} else {
			e.autoWrong++
		}
	case tierReview:
		if o.InCands {
			e.reviewHit++
		} else {
			e.reviewMiss++
		}
	default:
		e.none++
	}
	if o.tier != tierNone {
		rs := e.byReason[o.Reason]
		if rs == nil {
			rs = &reasonStat{}
			e.byReason[o.Reason] = rs
		}
		rs.n++
		if o.Correct {
			rs.correct++
		} else {
			rs.wrong++
		}
	}
}

func (e *matchEval) print(title string) {
	fmt.Printf("\n================ %s ================\n", title)
	if e.total == 0 {
		fmt.Println("評価対象なし")
		return
	}
	pct := func(n int) string { return fmt.Sprintf("%.3f (%d/%d)", float64(n)/float64(e.total), n, e.total) }

	fmt.Printf("評価対象               : %d 件\n", e.total)
	fmt.Printf("auto_match_rate        : %s\n", pct(e.autoCorrect+e.autoWrong))
	fmt.Printf("  ├ 正解               : %s\n", pct(e.autoCorrect))
	fmt.Printf("  └ FALSE MATCH        : %s  ← 別の曲に結びつく。黙って壊れる\n", pct(e.autoWrong))
	fmt.Printf("review_rate            : %s  （人手に回る）\n", pct(e.reviewHit+e.reviewMiss))
	fmt.Printf("  ├ 候補に正解あり     : %s\n", pct(e.reviewHit))
	fmt.Printf("  └ 候補に正解なし     : %s\n", pct(e.reviewMiss))
	fmt.Printf("no_candidate_rate      : %s  （新規登録される）\n", pct(e.none))

	if n := e.autoCorrect + e.autoWrong; n > 0 {
		fmt.Printf("\n自動採用した中での誤り : %.4f (%d/%d)\n", float64(e.autoWrong)/float64(n), e.autoWrong, n)
	}

	fmt.Printf("\n-- 判定理由の内訳（候補が出たもの）--\n")
	fmt.Printf("%-16s %6s %6s %6s\n", "reason", "件数", "正解", "誤り")
	type kv struct {
		name string
		rs   *reasonStat
	}
	var ranked []kv
	for name, rs := range e.byReason {
		ranked = append(ranked, kv{name, rs})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].rs.n > ranked[j].rs.n })
	for _, k := range ranked {
		fmt.Printf("%-16s %6d %6d %6d\n", k.name, k.rs.n, k.rs.correct, k.rs.wrong)
	}
}

// newMatchService は production と同じ依存で照合サービスを組む。
// withAliases=false なら学習層（song_aliases / artist_aliases）を外す。
// 両方を回して差を見ると、学習が実際にどれだけ効いているかが分かる。
func newMatchService(db *sql.DB, withAliases bool) *service.SongMatchService {
	var aliasRepo *repository.AliasRepository
	if withAliases {
		aliasRepo = repository.NewAliasRepository(db)
	}
	return service.NewSongMatchService(
		repository.NewSongMatchRepository(db),
		repository.NewSongRepository(db),
		repository.NewSongItunesRepository(db),
		aliasRepo,
	)
}

// newResolver は本番が留言経路で使うのと同じ NormalizationService を組む。
// AI クライアントは nil でよい ── ResolveMatch は AI を呼ばない約束の経路。
func newResolver(db *sql.DB, ms *service.SongMatchService) *service.NormalizationService {
	return service.NewNormalizationService(nil, repository.NewSongItunesRepository(db), ms)
}

// evalMatch は 1 件を **本番と同じ関数** に通し、結果の曲 ID を正解と突き合わせる。
//
// 照合入力は service.MatchInputs、照合そのものは NormalizationService.ResolveMatch
// （留言経路の reresolveMatches / backfill-matches が呼ぶのと同じもの）。
// 閾値の判定をベンチ側で書き直さないのが要点 ── 書き直すと、本番の線引きを変えたときに
// ベンチだけが古い線引きで「改善した」と言い続ける。
//
// 「正解が候補に居るか」は ResolveMatch が返す候補（ReviewScore 以上）だけで見る。
// それ未満は本番でも捨てられていて、人にも見えないため。
func evalMatch(ns *service.NormalizationService, sid string, p comment.ParsedSong, gt gtSong) matchOutcome {
	name, artist := service.MatchInputs(p.Name, p.OriginalArtist, p.NormalizedName, p.NormalizedArtist)
	o := matchOutcome{
		Stream: sid, Start: p.Start,
		QueryName: name, QueryArtist: artist,
		GTName: gt.name, GTArtist: gt.artist,
		tier: tierNone, Tier: "none",
	}

	res := ns.ResolveMatch(name, artist)
	want := gt.songID.String()
	for _, c := range res.MatchCandidates {
		if c.SongID == want {
			o.InCands = true
			break
		}
	}

	switch {
	case res.MatchedSongID != nil:
		o.tier, o.Tier = tierAuto, "auto"
		o.Reason, o.Score = res.MatchReason, res.MatchScore
		o.Correct = *res.MatchedSongID == want
		if res.MatchedSongName != nil {
			o.GotName = *res.MatchedSongName
		}
		if res.MatchedSongArtist != nil {
			o.GotArtist = *res.MatchedSongArtist
		}
	case len(res.MatchCandidates) > 0:
		best := res.MatchCandidates[0]
		o.tier, o.Tier = tierReview, "review"
		o.Reason, o.Score = best.Reason, best.Score
		o.GotName, o.GotArtist = best.Name, best.Artist
		o.Correct = best.SongID == want
	}
	return o
}

// isVerbatim は「照合に渡した表記が DB の表記と逐字で同じ」か。
//
// この組は当たって当然なので主指標から外す。GT の楽曲の一部はそのコメント自身から
// findOrCreateSong で作られていて、DB の表記＝コメントの表記になっている。
// 混ぜたまま測ると、照合が何もしなくても高い数字が出る。
func isVerbatim(o matchOutcome) bool {
	return o.QueryName == o.GTName && o.QueryArtist == o.GTArtist
}

// loadGroundTruthWithIDs は正解の楽曲 ID まで含めて読む。
func loadGroundTruthWithIDs(db *sql.DB, sid string) []gtSong {
	rows, err := db.Query(`
		SELECT p.start_seconds, so.id, so.name, so.original_artist
		FROM performances p JOIN songs so ON so.id = p.song_id
		WHERE p.stream_id = $1 ORDER BY p.start_seconds`, sid)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()
	var out []gtSong
	for rows.Next() {
		var g gtSong
		var id uuid.UUID
		if err := rows.Scan(&g.start, &id, &g.name, &g.artist); err != nil {
			fatal(err)
		}
		g.songID = id
		out = append(out, g)
	}
	return out
}
