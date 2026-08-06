// Command setoribench evaluates the comment-analysis pipeline against ground truth.
//
// Ground truth = performances (人手で確定した歌唱記録) tied to a stream.
// Source       = comment_raw (歌枠のセトリコメント).
//
// -mode regex : pure deterministic path (comment.ParseComments). AI 失敗時のフォールバック。
// -mode ai    : production と同じ抽出（comment.ParseCommentsWithAI, AI 失敗時は regex）。
//
//	本機 DB の ai_providers を使う。呼び出しは高いので -cache でディスクに保存し、
//	-struct の on/off 再実行は同じ AI 結果を使い回す。
//
// どちらのモードでも抽出後に FilterSongsWith(structural) -> Dedup -> Validate を通し、
// 抽出タイムスタンプを ground truth と start 近接で突き合わせて precision/recall を出す。
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	_ "github.com/lib/pq"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/comment"
)

const matchThreshold = 20 // 秒: 抽出 start が GT start とこの範囲内なら「一致」とみなす

type gtSong struct {
	start int
	name  string
}

type fpItem struct {
	Stream string `json:"stream"`
	Start  int    `json:"start"`
	Name   string `json:"name"`
	Artist string `json:"artist"`
	Line   string `json:"line"`
}

func main() {
	dbURL := flag.String("db", envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/setori?sslmode=disable"), "database URL")
	mode := flag.String("mode", "regex", "extraction mode: regex | ai | stored (DB の comment_songs をそのまま評価)")
	structural := flag.Bool("struct", true, "apply structural non-song filter (false = keyword-only baseline)")
	noFilter := flag.Bool("nofilter", false, "skip FilterSongs entirely (stored の as-is 評価用)")
	limit := flag.Int("limit", 0, "limit number of streams (0 = all)")
	cachePath := flag.String("cache", "", "path to cache AI extraction JSON (avoids re-hitting the API)")
	showFP := flag.Int("fp", 40, "how many top false-positive names to print")
	fpDump := flag.String("fpdump", "", "dump false positives to JSON")
	tpDump := flag.String("tpdump", "", "dump true positives to JSON")
	flag.Parse()

	db, err := sql.Open("postgres", *dbURL)
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fatal(err)
	}

	// AI モード用の抽出器（production と同じ AIService をそのまま使う）
	var aiSvc *service.AIService
	if *mode == "ai" {
		aiSvc = service.NewAIService(repository.NewAIProviderRepository(db), os.Getenv("GROQ_API_KEY"))
	}
	cache := loadCache(*cachePath)

	filterKW, keepKW := loadKeywords(db)
	fmt.Printf("mode=%s structural=%v  (loaded %d filter / %d keep keywords)\n", *mode, *structural, len(filterKW), len(keepKW))

	streams := loadStreams(db)
	if *limit > 0 && *limit < len(streams) {
		streams = streams[:*limit]
	}
	fmt.Printf("streams with performances + comment_raw: %d\n\n", len(streams))

	var (
		totalGT, totalExtract, totalTP, totalFP, totalFN int
		aiFail                                           int
		fpNames                                          = map[string]int{}
		fpItems, tpItems                                 []fpItem
		streamsOverExtract                               int
	)

	for idx, sid := range streams {
		gt := loadGroundTruth(db, sid)
		comments := loadComments(db, sid)
		if len(gt) == 0 || len(comments) == 0 {
			continue
		}

		// ---- 抽出（production と同じ経路）----
		var parsed []comment.ParsedSong
		var usedRegexFallback bool
		if *mode == "stored" {
			parsed = loadStoredSongs(db, sid) // DB に保存済みの AI 分析結果をそのまま使う
		} else {
			_, cached := cacheGet(cache, sid)
			parsed, usedRegexFallback = extract(*mode, sid, comments, aiSvc, cache)
			if usedRegexFallback {
				aiFail++
			}
			if *mode == "ai" {
				tag := "AI"
				if usedRegexFallback {
					tag = "AI-FAILED->regex"
				} else if cached {
					tag = "cache"
				}
				fmt.Fprintf(os.Stderr, "[%3d/%d] %s  %s  (%d parsed)\n", idx+1, len(streams), sid, tag, len(parsed))
				// 逐次キャッシュ保存（途中で止めても AI 結果を失わない）
				if *cachePath != "" && !cached && (idx+1)%5 == 0 {
					saveCache(*cachePath, cache)
				}
			}
		}

		// ---- filter -> dedup -> validate（production と同じ後処理）----
		// -nofilter のときは FilterSongs を通さず、抽出をそのまま評価（stored の as-is 計測用）。
		filtered := parsed
		if !*noFilter {
			filtered = comment.FilterSongsWith(parsed, filterKW, keepKW, *structural)
		}
		deduped := comment.DeduplicateSongs(filtered)
		valid := comment.ValidateSongs(deduped)

		// ---- ground truth と突き合わせ（start 近接でマッチ）----
		gtUsed := make([]bool, len(gt))
		tp, fp := 0, 0
		for _, ex := range valid {
			mi := bestGTMatch(ex.Start, gt, gtUsed)
			item := fpItem{sid, ex.Start, ex.Name, ex.OriginalArtist, firstLine(ex.OriginalComment)}
			if mi >= 0 {
				gtUsed[mi] = true
				tp++
				tpItems = append(tpItems, item)
			} else {
				fp++
				fpNames[normalizeFPName(ex.Name)]++
				fpItems = append(fpItems, item)
			}
		}
		fn := 0
		for _, used := range gtUsed {
			if !used {
				fn++
			}
		}

		totalGT += len(gt)
		totalExtract += len(valid)
		totalTP += tp
		totalFP += fp
		totalFN += fn
		if len(valid) > len(gt) {
			streamsOverExtract++
		}
	}

	if *cachePath != "" {
		saveCache(*cachePath, cache)
	}

	fmt.Println("================ AGGREGATE ================")
	fmt.Printf("ground-truth songs : %d\n", totalGT)
	fmt.Printf("extracted songs    : %d\n", totalExtract)
	fmt.Printf("true positives     : %d\n", totalTP)
	fmt.Printf("false positives    : %d  (抽出したが GT に無い = 非曲の誤検出 or GT 欠落)\n", totalFP)
	fmt.Printf("false negatives    : %d  (GT にあるが抽出漏れ)\n", totalFN)
	if totalExtract > 0 {
		fmt.Printf("precision          : %.3f\n", float64(totalTP)/float64(totalExtract))
	}
	if totalGT > 0 {
		fmt.Printf("recall             : %.3f\n", float64(totalTP)/float64(totalGT))
	}
	fmt.Printf("streams over-extracting (extract>GT): %d / %d\n", streamsOverExtract, len(streams))
	if *mode == "ai" {
		fmt.Printf("streams that fell back to regex (AI failed): %d\n", aiFail)
	}

	fmt.Printf("\n================ TOP FALSE-POSITIVE NAMES (top %d) ================\n", *showFP)
	type kv struct {
		name string
		n    int
	}
	var ranked []kv
	for name, n := range fpNames {
		ranked = append(ranked, kv{name, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].name < ranked[j].name
	})
	for i, k := range ranked {
		if i >= *showFP {
			break
		}
		fmt.Printf("%4d  %s\n", k.n, k.name)
	}

	if *fpDump != "" {
		dumpJSON(*fpDump, fpItems)
		fmt.Printf("\nwrote %d FP items to %s\n", len(fpItems), *fpDump)
	}
	if *tpDump != "" {
		dumpJSON(*tpDump, tpItems)
		fmt.Printf("wrote %d TP items to %s\n", len(tpItems), *tpDump)
	}
}

// extract は mode に応じて production と同じ抽出を行う。
// ai モードでは ParseCommentsWithAI を使い、失敗時は ParseComments に退避（parseComments と同じ挙動）。
// cache があれば AI 結果を再利用し、無ければ呼び出して保存する。
func extract(mode, sid string, comments []string, aiSvc *service.AIService, cache map[string][]comment.ParsedSong) (songs []comment.ParsedSong, usedRegexFallback bool) {
	if mode != "ai" {
		return comment.ParseComments(comments), false
	}
	if cache != nil {
		if cached, ok := cache[sid]; ok {
			return cached, false
		}
	}
	songs, err := comment.ParseCommentsWithAI(aiSvc, comments)
	if err != nil {
		songs = comment.ParseComments(comments)
		usedRegexFallback = true
	}
	if cache != nil {
		cache[sid] = songs
	}
	return songs, usedRegexFallback
}

func bestGTMatch(start int, gt []gtSong, used []bool) int {
	best, bestDiff := -1, matchThreshold+1
	for i, g := range gt {
		if used[i] {
			continue
		}
		d := start - g.start
		if d < 0 {
			d = -d
		}
		if d <= matchThreshold && d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

func normalizeFPName(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 40 {
		s = string([]rune(s)[:40]) + "…"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func cacheGet(cache map[string][]comment.ParsedSong, sid string) ([]comment.ParsedSong, bool) {
	if cache == nil {
		return nil, false
	}
	v, ok := cache[sid]
	return v, ok
}

func loadCache(path string) map[string][]comment.ParsedSong {
	cache := map[string][]comment.ParsedSong{}
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(b, &cache)
	}
	return cache
}

func saveCache(path string, cache map[string][]comment.ParsedSong) {
	b, _ := json.MarshalIndent(cache, "", " ")
	os.WriteFile(path, b, 0o644)
}

func dumpJSON(path string, v any) {
	f, _ := os.Create(path)
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(v)
	f.Close()
}

func loadKeywords(db *sql.DB) (filter, keep []string) {
	rows, err := db.Query(`SELECT type, keyword FROM filter_keywords`)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t, k string
		if err := rows.Scan(&t, &k); err != nil {
			fatal(err)
		}
		switch t {
		case "filter":
			filter = append(filter, k)
		case "keep":
			keep = append(keep, k)
		}
	}
	return
}

func loadStreams(db *sql.DB) []string {
	rows, err := db.Query(`
		SELECT DISTINCT s.id
		FROM streams s
		JOIN performances p ON p.stream_id = s.id
		WHERE s.comment_raw IS NOT NULL
		  AND jsonb_typeof(s.comment_raw) = 'array'
		  AND jsonb_array_length(s.comment_raw) > 0
		ORDER BY s.id`)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func loadGroundTruth(db *sql.DB, sid string) []gtSong {
	rows, err := db.Query(`
		SELECT p.start_seconds, so.name
		FROM performances p JOIN songs so ON so.id = p.song_id
		WHERE p.stream_id = $1 ORDER BY p.start_seconds`, sid)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()
	var out []gtSong
	for rows.Next() {
		var g gtSong
		rows.Scan(&g.start, &g.name)
		out = append(out, g)
	}
	return out
}

// loadStoredSongs は DB に保存済みの comment_songs（過去に AI 分析した結果）を
// ParsedSong として読み出す。再分析せず「実際にユーザーが見ている出力」をそのまま評価する。
func loadStoredSongs(db *sql.DB, sid string) []comment.ParsedSong {
	var raw []byte
	if err := db.QueryRow(`SELECT comment_songs FROM streams WHERE id = $1`, sid).Scan(&raw); err != nil {
		return nil
	}
	var rows []struct {
		Start           int    `json:"start"`
		End             int    `json:"end"`
		Name            string `json:"name"`
		OriginalArtist  string `json:"original_artist"`
		OriginalComment string `json:"original_comment"`
	}
	json.Unmarshal(raw, &rows)
	out := make([]comment.ParsedSong, 0, len(rows))
	for _, r := range rows {
		out = append(out, comment.ParsedSong{
			Start:           r.Start,
			End:             r.End,
			Name:            r.Name,
			OriginalArtist:  r.OriginalArtist,
			OriginalComment: r.OriginalComment,
		})
	}
	return out
}

func loadComments(db *sql.DB, sid string) []string {
	var raw []byte
	if err := db.QueryRow(`SELECT comment_raw FROM streams WHERE id = $1`, sid).Scan(&raw); err != nil {
		return nil
	}
	var out []string
	json.Unmarshal(raw, &out)
	return out
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
