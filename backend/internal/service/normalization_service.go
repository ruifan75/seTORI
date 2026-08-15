package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/perftag"
)

type NormalizationService struct {
	aiClient       ai.Chatter
	songItunesRepo *repository.SongItunesRepository
	matchService   *SongMatchService
}

// 楽曲の照合そのものは SongMatchService が持つので songRepo は要らない。
func NewNormalizationService(
	aiClient ai.Chatter,
	songItunesRepo *repository.SongItunesRepository,
	matchService *SongMatchService,
) *NormalizationService {
	return &NormalizationService{
		aiClient:       aiClient,
		songItunesRepo: songItunesRepo,
		matchService:   matchService,
	}
}

// バッチ処理用の system prompt (日本語版)
const batchSystemPrompt = `**最重要: 応答は純粋なJSON配列「のみ」を出力せよ。前後に ** や "Output only JSON." などの一切のテキストを付けるな。必ず [ で始まり ] で終わること。**

あなたは日本語楽曲データの正規化を専門とするアシスタントです。複数の楽曲を受け取り、一度にすべて処理してください。

タスク：
1. 元の楽曲名から正規化された曲名を抽出
   - 「演奏バージョンの表記」（Acoustic Ver., Short Ver. など、演奏方法を示すタグ）を除去
   - 「楽曲バージョンの表記」（Remix, Cover など）は異なる楽曲を表すため保持
   - 曲名に日本語と英語/ローマ字の両方が含まれる場合（例：「ぼくらのレットイットビー / Bokura no Let It Be」）、日本語の元の名前だけを保持
   - 元の曲名が英語の場合（「First Love」「Lemon」「KICK BACK」）は英語のまま保持し、カタカナに変換しない
   - 余分なスペースや記号を除去
2. 曲名とアーティスト名に平仮名の読みを付与
3. 演奏バージョンタグを識別

【重要】tags フィールドは以下の7種のタグIDのみ使用可能：
- acoustic（原曲名に Acoustic, アコースティック などを含む）
- piano（原曲名に Piano, ピアノ などを含む）
- 弾き語り（原曲名に 弾き語り を含む）
- acappella（原曲名に A Cappella, アカペラ などを含む）
- short（原曲名に Short, ショート などを含む）
- full（原曲名に Full, フル などを含む）
- medley（原曲名に Medley, メドレー などを含む）

注意：Remix、Cover、Live バージョンなどは異なる楽曲なので、曲名に保持してください。除去しないでください。

JSON配列形式で応答してください。各要素：
- index: 入力楽曲の番号 (number, 0から開始)
- normalized_name: 正規化後の曲名 (string)
- normalized_name_reading: 曲名の平仮名読み (string)
- original_artist: 照合に使うアーティスト (string)。**入力に書かれているものだけを返すこと。**
  入力が空なら空のまま返す（推測で埋めない）。
  原曲を知っていても書き換えない（DB はカバーした歌手を持っていることが多く、外れる）
- original_artist_reading: アーティスト名の平仮名読み (string)
- tags: 検出されたバージョンタグ（上記7種のみ） (array of strings)
- confidence: 信頼度 0.0-1.0 (number)

JSON配列のみ応答し、他のテキストは含めないでください。例：
[{"index":0,"normalized_name":"...","normalized_name_reading":"...","original_artist":"...","original_artist_reading":"...","tags":[],"confidence":0.9}]

**最重要（最後に繰り返す）**:
- 絶対にJSON配列として出力せよ。 [ {..}, {..} ] の形。
- オブジェクトをカンマで並べただけの出力は厳禁。
- 余計な文字は1文字も書くな。出力は [ で始まり ] で終わる純粋な配列のみ。`

// BatchAISuggestion バッチ AI 応答の単一項目フォーマット
type BatchAISuggestion struct {
	Index                 int      `json:"index"`
	NormalizedName        string   `json:"normalized_name"`
	NormalizedNameReading string   `json:"normalized_name_reading"`
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading"`
	Tags                  []string `json:"tags"`
	Confidence            float64  `json:"confidence"`
}

// BatchAINormalization バッチ AI 正規化（1回の呼び出しで全楽曲を処理）
func (s *NormalizationService) BatchAINormalization(items []dto.AINormalizationItem) (*dto.BatchAINormalizationResponse, error) {
	if len(items) == 0 {
		return &dto.BatchAINormalizationResponse{Suggestions: []dto.AISuggestionResult{}}, nil
	}

	// すべての楽曲を含むユーザーメッセージを構築
	userMessage := s.buildBatchMessage(items)

	logger.Debugf("AI normalization input userMessage: %s", userMessage)

	// AI を1回呼んで全楽曲を処理
	var warning string
	var suggestionMap map[int]BatchAISuggestion

	logger.Infof("AI batch normalization: items=%d, prompt_len=%d", len(items), len(userMessage))

	response, err := s.aiClient.SimpleChat(batchSystemPrompt, userMessage)
	if err != nil {
		// AI 呼び出し失敗。警告を記録し、DB照合のみ続行
		logger.Warnf("AI batch chat failed: %v", err)
		warning = fmt.Sprintf("AI正規化に失敗しました（%v）。DB照合のみ実行しました。", err)
		suggestionMap = make(map[int]BatchAISuggestion)
	} else {
		logger.Infof("AI batch normalization: response_len=%d", len(response))
		logger.Debugf("AI normalization raw response: %s", response)

		// バッチ応答をパース
		batchSuggestions, parseErr := s.parseBatchAIResponse(response)
		if parseErr != nil {
			// レスポンスが長い場合は先頭と末尾を表示して切り詰めを検知しやすくする
			respPreview := response
			if len(respPreview) > 1500 {
				respPreview = respPreview[:800] + " ... [truncated] ... " + respPreview[len(respPreview)-400:]
			}
			logger.Warnf("AI response parse failed: %v, response_preview: %s", parseErr, respPreview)
			warning = "AI応答の解析に失敗しました。DB照合のみ実行しました。"
			suggestionMap = make(map[int]BatchAISuggestion)
		} else {
			suggestionMap = make(map[int]BatchAISuggestion)
			for _, s := range batchSuggestions {
				suggestionMap[s.Index] = s
			}
			logger.Infof("AI batch normalization parse succeeded: %d suggestions", len(batchSuggestions))
			for i, sug := range batchSuggestions {
				logger.Debugf("AI norm sug %d: name=%q artist=%q", i, sug.NormalizedName, sug.OriginalArtist)
			}
		}
	}

	// AI 応答を結果に変換し、欠損項目を補完（AI失敗時は元データを使用）
	suggestions := make([]dto.AISuggestionResult, len(items))
	for i, item := range items {
		if aiSugg, ok := suggestionMap[i]; ok {
			// AI が項目を返しても曲名が空のことがある。そのまま使うと空文字で
			// 照合しにいって必ず外れるので、抽出時の名前へ落とす。
			// AI が項目自体を返さなかった場合（下の else）は元から同じことをしている。
			// キャッシュ命中時の再解決（CommentService.reresolveMatches）も同様。
			normalizedName := aiSugg.NormalizedName
			if strings.TrimSpace(normalizedName) == "" {
				normalizedName = item.Name
			}
			artist := aiSugg.OriginalArtist
			if strings.TrimSpace(artist) == "" {
				artist = item.OriginalArtist
			}

			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        normalizedName,
				NormalizedNameReading: aiSugg.NormalizedNameReading,
				OriginalArtist:        artist,
				OriginalArtistReading: aiSugg.OriginalArtistReading,
				// 原文の曲名（item.Name）を渡すのが要点。正規化後の名前からは
				// 「(1 Chorus)」のようなバージョン表記が既に削られているので、
				// ここで拾わないと二度と復元できない
				Tags:       perftag.Normalize(aiSugg.Tags, item.Name),
				Confidence: aiSugg.Confidence,
				Reasoning:  "",
			}

			// 既存楽曲とのマッチを試行（iTunes ID 優先 → 曲名 + アーティスト）
			s.matchAndPopulateSong(&suggestions[i], &item, normalizedName, artist)
		} else {
			// AI がこの項目を返さなかった、または失敗した場合は元データを使用

			suggestions[i] = dto.AISuggestionResult{
				Index:                 i,
				NormalizedName:        item.Name,
				NormalizedNameReading: "",
				OriginalArtist:        item.OriginalArtist,
				OriginalArtistReading: "",
				// AI が落ちても、曲名に書いてあるバージョン表記は拾える
				Tags:       perftag.Normalize(nil, item.Name),
				Confidence: 0,
				Reasoning:  "",
			}

			// それでも DB マッチを試行

			s.matchAndPopulateSong(&suggestions[i], &item, item.Name, item.OriginalArtist)
		}
	}

	// 曲名は一意に一致したのにアーティストだけ食い違ったものを、まとめて AI に問う。
	// ここで別名義（松任谷由実 = 荒井由実）が確定すれば照合しなおして拾える。
	// 判定は肯定・否定とも永続化されるので、同じ組を二度は聞かない。
	//
	// warning が付いているときは AI 呼び出し自体が失敗しているので試さない。
	return &dto.BatchAINormalizationResponse{Suggestions: suggestions, Warning: warning}, nil
}

// buildBatchMessage 構築包含所有楽曲のバッチメッセージ
func (s *NormalizationService) buildBatchMessage(items []dto.AINormalizationItem) string {
	var sb strings.Builder

	sb.WriteString("以下の楽曲リストを処理してください：\n\n")

	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] 楽曲名: %s", i, item.Name))
		if item.OriginalArtist != "" {
			sb.WriteString(fmt.Sprintf(" / アーティスト: %s", item.OriginalArtist))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// matchAndPopulateSong は既存楽曲との照合結果を result に詰める。
//
// 照合は SongMatchService に任せる（曲名キーで引いてアーティストで検証する）。
// 確信度が AutoMatchScore 以上のものだけを「マッチした」として扱い、
// それに満たないが似ているものは MatchCandidates に入れて UI に選ばせる。
// ここで候補を握り潰すと、"ひこうき雲 / 松任谷由実" のような別名義の曲が
// 「DB に無い曲」として新規登録され、近似重複が増えていく。
func (s *NormalizationService) matchAndPopulateSong(result *dto.AISuggestionResult, item *dto.AINormalizationItem, normalizedName, normalizedArtist string) {
	if s.matchService == nil {
		return
	}
	cands, err := s.matchService.FindCandidates(normalizedName, normalizedArtist, item.ItunesID)
	if err != nil {
		logger.Warnf("song match failed (%s / %s): %v", normalizedName, normalizedArtist, err)
		return
	}
	if len(cands) == 0 {
		return
	}

	// 自動採用の水準に届かないものは候補として返すだけにする
	for _, c := range cands {
		if c.Score < ReviewScore {
			continue
		}
		result.MatchCandidates = append(result.MatchCandidates, dto.SongMatchCandidate{
			SongID:  c.Song.ID.String(),
			Name:    c.Song.Name,
			Artist:  c.Song.OriginalArtist,
			Score:   c.Score,
			Reason:  c.Reason,
			ArtURL:  c.Song.Arts.String,
			IsMatch: c.Auto(),
		})
	}

	best := cands[0]
	if !best.Auto() {
		return
	}
	s.populateMatchedSong(result, &best.Song, best.Reason, best.Score)
}

// populateMatchedSong は決まった楽曲を result に写す。
// 規則で決めた場合（matchAndPopulateSong）と AI が決めた場合（applyAIMatch）で共用する
// ── 別々に書いていた頃、iTunes ID を片方だけ埋めるといった食い違いが起きた。
func (s *NormalizationService) populateMatchedSong(result *dto.AISuggestionResult, matchedSong *models.Song, reason string, score float64) {
	songID := matchedSong.ID.String()
	result.MatchedSongID = &songID
	result.MatchReason = reason
	result.MatchScore = score
	result.MatchedSongName = &matchedSong.Name
	result.MatchedSongArtist = &matchedSong.OriginalArtist
	if matchedSong.NameReading.Valid {
		result.MatchedSongNameReading = &matchedSong.NameReading.String
	}
	if matchedSong.OriginalArtistReading.Valid {
		result.MatchedSongArtistReading = &matchedSong.OriginalArtistReading.String
	}
	if matchedSong.Arts.Valid {
		result.MatchedSongArtURL = &matchedSong.Arts.String
	}

	// primary iTunes ID を取得する
	if s.songItunesRepo != nil {
		itunesRecords, err := s.songItunesRepo.FindBySongID(matchedSong.ID)
		if err == nil && len(itunesRecords) > 0 {
			for _, record := range itunesRecords {
				if record.IsPrimary {
					result.MatchedSongItunesID = &record.ITunesID
					break
				}
			}
			if result.MatchedSongItunesID == nil {
				result.MatchedSongItunesID = &itunesRecords[0].ITunesID
			}
		}
	}
}

// ResolveMatch は AI を呼ばず、正規化済みの名称・アーティストで DB 照合のみを行い、
// マッチ結果（matched_song_*）を埋めた AISuggestionResult を返す。
// キャッシュ命中時に、凍結された古いマッチ（曲が後から追加された等）を現在の DB 状態へ
// 再解決するために使う。マッチ無しなら matched_song_* は空のまま。
// itunesID は分かっていれば渡すこと（Holodex は曲ごとに持っている）。
// **最も強い証拠**なので、落とすと曲名が食い違う曲を取り逃す
// （`深昏睡／Deep Coma` と `深昏睡 (Deep coma)` は曲名キーが一致しない）。
func (s *NormalizationService) ResolveMatch(normalizedName, normalizedArtist string, itunesID *int64) dto.AISuggestionResult {
	var res dto.AISuggestionResult
	item := dto.AINormalizationItem{Name: normalizedName, OriginalArtist: normalizedArtist, ItunesID: itunesID}
	s.matchAndPopulateSong(&res, &item, normalizedName, normalizedArtist)
	return res
}

// parseBatchAIResponse バッチ AI 応答をパース
func (s *NormalizationService) parseBatchAIResponse(response string) ([]BatchAISuggestion, error) {
	response = ai.CleanJSONResponse(response)

	// Use Decoder to tolerate trailing data after the JSON array
	decoder := json.NewDecoder(strings.NewReader(response))
	var suggestions []BatchAISuggestion
	if err := decoder.Decode(&suggestions); err != nil {
		// 長い場合はプレビュー
		preview := response
		if len(preview) > 1500 {
			preview = preview[:800] + " ... [truncated] ... " + preview[len(preview)-400:]
		}
		errMsg := fmt.Sprintf("unmarshal batch AI response: %v", err)
		trimmed := strings.TrimSpace(response)
		if !strings.HasSuffix(trimmed, "]") && strings.HasPrefix(trimmed, "[") {
			errMsg += " (response looks truncated: does not end with ']')"
		}
		return nil, fmt.Errorf("%s, response_preview: %s", errMsg, preview)
	}

	return suggestions, nil
}
