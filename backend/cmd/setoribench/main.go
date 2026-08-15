// Command setoribench evaluates the comment-analysis pipeline against ground truth.
//
// Ground truth = performances (人手で確定した歌唱記録) tied to a stream.
// Source       = comment_raw (歌枠のセトリコメント).
//
// -mode regex    : pure deterministic path (comment.ParseComments). AI 失敗時のフォールバック。
// -mode ai       : 2 段階の抽出（comment.ParseCommentsWithAI, AI 失敗時は regex）。
//
//	production では grouped が失敗したときの退避先。
//
// -mode combined : 抽出と正規化を 1 回で行う（comment.ParseAndNormalizeWithAI）。
// -mode grouped  : **production の既定**。抽出＋正規化＋重複排除まで 1 回で行う
//
//	（comment.ParseNormalizeAndDedupWithAI）。コメント境界を見せ、
//	AI に「どの行をまとめたか」を src で申告させる。
//
// AI を使うモードはローカル DB の ai_providers を使う。呼び出しは高コストなので -cache で
// ディスクに保存し、-struct の on/off 再実行は同じ AI 結果を使い回す。
//
// ⚠️ キャッシュは stream ID だけをキーにしている。モードが違えば結果も違うので、
// モードごとに別のパスを渡すこと。プロンプトを変えて測り直すときも同じで、
// 前の版の結果を読むと「変えたのに何も変わらない」ように見える。
//
//	go run ./cmd/setoribench -mode ai       -cache /tmp/bench-ai.json
//	go run ./cmd/setoribench -mode combined -cache /tmp/bench-combined.json
//	go run ./cmd/setoribench -mode grouped  -cache /tmp/bench-grouped.json
//
// # 照合の評価（-match、既定 on）
//
// 抽出が当てた曲だけを対象に、DB のどの楽曲に結びついたかを測る。詳細は match.go。
//
// # 非曲の雑音を測る（-ids / -nogt / -extractdump）
//
// 非曲の見出し（開会式・提供・ここすき等）は **performances が登録されていない
// 雑談配信に偏っている**ため、GT ベースの precision/recall では測れない。
// 対象を名指しして、抽出された中身をそのまま書き出して数える。
//
//	go run ./cmd/setoribench -mode grouped -ids noisy.txt -nogt \
//	    -cache /tmp/noise.json -extractdump /tmp/noise-extract.json
//
// 抽出のルールやプロンプトを変えるときは **雑音（-ids）と取りこぼし（GT）の両方**を
// 見ること。厳しくすれば雑音は減るが曲まで落ちる。片方だけ見ると改悪に気づけない。
//
// 抽出後は FilterSongsWith(structural) -> Dedup -> Validate を通し、抽出タイムスタンプを
// ground truth と start 近接で突き合わせて precision/recall を出す。
//
// grouped は AI 側で既に重複排除されているため、後段の DeduplicateSongs は
// ほぼ素通りになる。両者の統合判断の差は、precision/recall と extracted 件数に表れる。
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ruifan75/setori/pkg/secrets"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/comment"
)

const matchThreshold = 20 // 秒: 抽出 start が GT start とこの範囲内なら「一致」とみなす

type gtSong struct {
	start  int
	name   string
	artist string
	songID uuid.UUID // 照合の正解。performances が指している楽曲
}

type fpItem struct {
	Stream string `json:"stream"`
	Start  int    `json:"start"`
	Name   string `json:"name"`
	Artist string `json:"artist"`
	Line   string `json:"line"`
}

// benchCipher は setoribench 用の復号器。api_key は暗号化して保存されているので、
// 本番と同じ鍵（SETTINGS_ENCRYPTION_KEY）が無いと AI を呼べない。
func benchCipher() *secrets.Cipher {
	c, err := secrets.NewCipher(os.Getenv("SETTINGS_ENCRYPTION_KEY"))
	if err != nil {
		c, _ = secrets.NewCipher("")
	}
	return c
}

func main() {
	dbURL := flag.String("db", envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/setori?sslmode=disable"), "database URL")
	mode := flag.String("mode", "regex", "extraction mode: regex | ai | combined | grouped | stored (DB の comment_songs をそのまま評価)")
	structural := flag.Bool("struct", true, "apply structural non-song filter (false = keyword-only baseline)")
	noFilter := flag.Bool("nofilter", false, "skip FilterSongs entirely (stored の as-is 評価用)")
	limit := flag.Int("limit", 0, "limit number of streams (0 = all)")
	cachePath := flag.String("cache", "", "path to cache AI extraction JSON (avoids re-hitting the API)")
	showFP := flag.Int("fp", 40, "how many top false-positive names to print")
	fpDump := flag.String("fpdump", "", "dump false positives to JSON")
	tpDump := flag.String("tpdump", "", "dump true positives to JSON")
	idsFile := flag.String("ids", "", "file with stream IDs (one per line) to evaluate instead of all GT streams")
	noGT := flag.Bool("nogt", false, "evaluate streams that have no performances (雑音の計測用。precision/recall は出ない)")
	extractDump := flag.String("extractdump", "", "dump every extracted item (GT の有無によらず) to JSON")
	evalMatching := flag.Bool("match", true, "evaluate song matching (抽出した曲名が正しい楽曲に結びつくか)")
	noAlias := flag.Bool("noalias", false, "disable the artist alias layer (artists.aliases) to measure its contribution")
	badMatchDump := flag.String("baddump", "", "dump false matches / missed matches to JSON")
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
	if usesAI(*mode) {
		aiSvc = service.NewAIService(repository.NewAIProviderRepository(db, benchCipher()), os.Getenv("GROQ_API_KEY"))
	}
	cache := loadCache(*cachePath)

	filterKW, keepKW := loadKeywords(db)
	fmt.Printf("mode=%s structural=%v  (loaded %d filter / %d keep keywords)\n", *mode, *structural, len(filterKW), len(keepKW))

	// 非曲の雑音は「performances が登録されていない配信」（雑談配信など）に偏っている。
	// そこは GT が無いので precision/recall では測れない。-ids で対象を名指しし、
	// -nogt で GT 突き合わせを外して、抽出した中身そのものを見る。
	streams := loadStreams(db)
	if *idsFile != "" {
		streams = readLines(*idsFile)
		fmt.Printf("stream IDs from %s: %d\n", *idsFile, len(streams))
	}
	if *limit > 0 && *limit < len(streams) {
		streams = streams[:*limit]
	}
	fmt.Printf("streams with performances + comment_raw: %d\n\n", len(streams))

	// 照合の評価器。照合キーは起動時に作り直される想定なので、ここでも揃えておく
	// （古い規則のキーのまま測ると、直したはずの改善が数字に出ない）。
	var resolver *service.NormalizationService
	if *evalMatching {
		matchSvc := newMatchService(db, !*noAlias)
		resolver = newResolver(db, matchSvc)
		if n, err := matchSvc.RebuildKeys(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: 照合キーの再構築に失敗: %v\n", err)
		} else if n > 0 {
			fmt.Printf("照合キーを %d 件作り直しました\n", n)
		}
		if *noAlias {
			fmt.Println("学習層（song_aliases / artist_aliases）を無効にして測ります")
		}
	}

	var (
		totalGT, totalExtract, totalTP, totalFP, totalFN int
		aiFail                                           int
		fpNames                                          = map[string]int{}
		fpItems, tpItems, extractItems                   []fpItem
		streamsOverExtract                               int
		matchAll                                         = newMatchEval()
		matchNonVerbatim                                 = newMatchEval()
		badMatches                                       []matchOutcome
	)

	for idx, sid := range streams {
		gt := loadGroundTruthWithIDs(db, sid)
		comments := loadComments(db, sid)
		if len(comments) == 0 || (len(gt) == 0 && !*noGT) {
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
			if usesAI(*mode) {
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

		// 抽出したものを全部書き出す（GT の有無によらず）。
		// プロンプトを変えた前後で「同じ配信から何が出てくるようになったか」を比べる用。
		for _, ex := range valid {
			extractItems = append(extractItems, fpItem{sid, ex.Start, ex.Name, ex.OriginalArtist, firstLine(ex.OriginalComment)})
		}

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

				// 照合の評価は「抽出が当たった曲」だけを対象にする。
				// 抽出を外した曲まで混ぜると、照合の成績が抽出の成績に汚染される。
				if resolver != nil {
					o := evalMatch(resolver, sid, ex, gt[mi])
					matchAll.add(o)
					// DB の表記と抽出の表記が文字列として同じ組は、その曲がこのコメントから
					// 作られた（＝当たって当然）可能性が高い。主指標からは外す。
					if !isVerbatim(o) {
						matchNonVerbatim.add(o)
					}
					// 誤採用（黙って壊れる）と候補なし（新規登録される）を書き出す。
					// 人手に回るものは誤りが残らないので対象外。
					if (o.tier == tierAuto && !o.Correct) || o.tier == tierNone {
						badMatches = append(badMatches, o)
					}
				}
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
	// GT が無い対象（-nogt）で precision 0.000 と出すと、抽出が全部外れたように読める。
	// 実際は突き合わせる正解が無いだけなので、抽出件数だけを出す。
	if totalGT == 0 {
		fmt.Printf("extracted songs    : %d  （GT が無いので精度は出せない。-extractdump で中身を見ること）\n", totalExtract)
	} else {
		fmt.Printf("ground-truth songs : %d\n", totalGT)
		fmt.Printf("extracted songs    : %d\n", totalExtract)
		fmt.Printf("true positives     : %d\n", totalTP)
		fmt.Printf("false positives    : %d  (抽出したが GT に無い = 非曲の誤検出 or GT 欠落)\n", totalFP)
		fmt.Printf("false negatives    : %d  (GT にあるが抽出漏れ)\n", totalFN)
		if totalExtract > 0 {
			fmt.Printf("precision          : %.3f\n", float64(totalTP)/float64(totalExtract))
		}
		fmt.Printf("recall             : %.3f\n", float64(totalTP)/float64(totalGT))
		fmt.Printf("streams over-extracting (extract>GT): %d / %d\n", streamsOverExtract, len(streams))
	}
	if usesAI(*mode) {
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

	if resolver != nil {
		matchNonVerbatim.print("MATCHING（主指標：DB 表記と抽出表記が食い違う組）")
		matchAll.print("MATCHING（参考：文字列完全一致の組を含む全件）")
		fmt.Println("\n※ 主指標から文字列完全一致を外すのは、GT の楽曲の一部がそのコメント自身から")
		fmt.Println("   findOrCreateSong で作られており、当たって当然の組が数字を押し上げるため。")
	}

	if *extractDump != "" {
		dumpJSON(*extractDump, extractItems)
		fmt.Printf("wrote %d extracted items to %s\n", len(extractItems), *extractDump)
	}
	if *fpDump != "" {
		dumpJSON(*fpDump, fpItems)
		fmt.Printf("\nwrote %d FP items to %s\n", len(fpItems), *fpDump)
	}
	if *badMatchDump != "" {
		dumpJSON(*badMatchDump, badMatches)
		fmt.Printf("wrote %d bad-match items to %s\n", len(badMatches), *badMatchDump)
	}
	if *tpDump != "" {
		dumpJSON(*tpDump, tpItems)
		fmt.Printf("wrote %d TP items to %s\n", len(tpItems), *tpDump)
	}
}

// usesAI は AI を呼ぶモードかどうか。
func usesAI(mode string) bool { return mode == "ai" || mode == "combined" || mode == "grouped" }

// extract は mode に応じて production と同じ抽出を行う。
// ai / combined モードでは AI を呼び、失敗時は ParseComments に退避（parseComments と同じ挙動）。
// cache があれば AI 結果を再利用し、無ければ呼び出して保存する。
func extract(mode, sid string, comments []string, aiSvc *service.AIService, cache map[string][]comment.ParsedSong) (songs []comment.ParsedSong, usedRegexFallback bool) {
	if !usesAI(mode) {
		return comment.ParseComments(comments), false
	}
	if cache != nil {
		if cached, ok := cache[sid]; ok {
			return cached, false
		}
	}

	var err error
	switch mode {
	case "grouped":
		// 抽出＋正規化＋重複排除を 1 回で。AI が src で申告したグループを
		// Go 側が mergeParsedSong で畳み込む。既に重複排除済みで返る。
		songs, err = comment.ParseNormalizeAndDedupWithAI(aiSvc, comments)
	case "combined":
		// 抽出と正規化を 1 回で行う経路。2 段階との差を測るために用意している。
		// precision/recall は同じ土俵で比較できるが、この経路は NormalizedName や
		// Tags も埋めるため、タグ付与の正しさは -struct 併用で出力を目視すること。
		songs, err = comment.ParseAndNormalizeWithAI(aiSvc, comments)
	default:
		songs, err = comment.ParseCommentsWithAI(aiSvc, comments)
	}
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

// loadStoredSongs は DB に保存済みの comment_songs（過去に AI 分析した結果）を
// ParsedSong として読み出す。再分析せず「実際にユーザーが見ている出力」をそのまま評価する。
//
// 正規化結果も一緒に読む。照合は正規化後の名称で行われるので、ここを落とすと
// ベンチだけが抽出したままの表記で照合し、本番より悪い数字が出る。
func loadStoredSongs(db *sql.DB, sid string) []comment.ParsedSong {
	var raw []byte
	if err := db.QueryRow(`SELECT comment_songs FROM streams WHERE id = $1`, sid).Scan(&raw); err != nil {
		return nil
	}
	var rows []struct {
		Start            int    `json:"start"`
		End              int    `json:"end"`
		Name             string `json:"name"`
		OriginalArtist   string `json:"original_artist"`
		OriginalComment  string `json:"original_comment"`
		NormalizedName   string `json:"normalized_name"`
		NormalizedArtist string `json:"normalized_artist"`
	}
	json.Unmarshal(raw, &rows)
	out := make([]comment.ParsedSong, 0, len(rows))
	for _, r := range rows {
		out = append(out, comment.ParsedSong{
			Start:            r.Start,
			End:              r.End,
			Name:             r.Name,
			OriginalArtist:   r.OriginalArtist,
			OriginalComment:  r.OriginalComment,
			NormalizedName:   r.NormalizedName,
			NormalizedArtist: r.NormalizedArtist,
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

// readLines は空行を除いた行を返す（-ids 用）。
func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
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
