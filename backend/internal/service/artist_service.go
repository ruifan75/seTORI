package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

// ArtistService は原曲アーティストの一覧/詳細と読み仮名の AI 補完を担う。
type ArtistService struct {
	artistRepo *repository.ArtistRepository
	songRepo   *repository.SongRepository
	aiService  *AIService
}

func NewArtistService(artistRepo *repository.ArtistRepository, songRepo *repository.SongRepository, aiService *AIService) *ArtistService {
	return &ArtistService{artistRepo: artistRepo, songRepo: songRepo, aiService: aiService}
}

func toArtistResponse(a models.Artist) dto.ArtistResponse {
	resp := dto.ArtistResponse{
		ID:        a.ID,
		Name:      a.Name,
		SongCount: a.SongCount,
	}
	if a.NameReading.Valid && a.NameReading.String != "" {
		resp.NameReading = &a.NameReading.String
	}
	return resp
}

// GetAll はアーティスト一覧（曲数付き・検索対応）を返す。
// sort は "name"（数字→英字→読み順、既定）か "songs"（曲数順）。
func (s *ArtistService) GetAll(page, limit int, search, sort, dir string) (*dto.ArtistListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	artists, total, err := s.artistRepo.ListWithCounts(limit, (page-1)*limit, search, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("list artists: %w", err)
	}

	resp := make([]dto.ArtistResponse, len(artists))
	for i, a := range artists {
		resp[i] = toArtistResponse(a)
	}
	return &dto.ArtistListResponse{
		Artists: resp,
		Pagination: dto.PaginationResponse{
			Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// GetByID はアーティスト詳細＋所属楽曲を返す。見つからなければ nil。
func (s *ArtistService) GetByID(id uuid.UUID, page, limit int, sort, dir string) (*dto.ArtistDetailResponse, error) {
	artist, err := s.artistRepo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return nil, nil
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	songs, counts, total, err := s.artistRepo.FindSongsByArtist(id, limit, (page-1)*limit, sort, dir)
	if err != nil {
		return nil, err
	}
	songIDs := make([]uuid.UUID, len(songs))
	for i, song := range songs {
		songIDs[i] = song.ID
	}
	artistMap, err := s.artistRepo.FindReferencesBySongIDs(songIDs)
	if err != nil {
		return nil, err
	}

	songResponses := make([]dto.SongResponse, len(songs))
	for i, song := range songs {
		songResponses[i] = buildSongResponse(song, counts[song.ID], nil)
		songResponses[i].Artists = toArtistReferences(artistMap[song.ID])
	}

	return &dto.ArtistDetailResponse{
		Artist: toArtistResponse(*artist),
		Songs:  songResponses,
		Pagination: dto.PaginationResponse{
			Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// ErrArtistNameTaken は改名先の名前が既存アーティストと衝突したとき返す。
var ErrArtistNameTaken = fmt.Errorf("同名のアーティストが既に存在します。統合機能を使ってください")

// Update は名前・読み仮名を更新する。名前変更時は所属楽曲の表示テキストも連動更新する。
func (s *ArtistService) Update(id uuid.UUID, name *string, reading *string) (*dto.ArtistResponse, error) {
	artist, err := s.artistRepo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return nil, nil
	}

	// 改名：他アーティストとの重複を拒否（統合したい場合はマージを使う）
	if name != nil {
		newName := strings.TrimSpace(*name)
		if newName == "" {
			return nil, fmt.Errorf("アーティスト名は必須です")
		}
		if newName != artist.Name {
			existing, err := s.artistRepo.FindByName(newName)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				return nil, ErrArtistNameTaken
			}
			if err := s.artistRepo.Rename(id, newName); err != nil {
				return nil, err
			}
		}
	}

	if reading != nil {
		// 読みはアーティスト単位で一元管理する方針（migration 021）。
		// 所属楽曲の original_artist_reading にも伝播させ、表示のドリフトを防ぐ。
		if err := s.artistRepo.UpdateReadingPropagate(id, strings.TrimSpace(*reading)); err != nil {
			return nil, err
		}
	}

	updated, err := s.artistRepo.FindByID(id)
	if err != nil || updated == nil {
		return nil, err
	}
	resp := toArtistResponse(*updated)
	return &resp, nil
}

// Merge は source アーティストを target に統合する。
func (s *ArtistService) Merge(sourceID, targetID uuid.UUID) (*dto.ArtistResponse, error) {
	if sourceID == targetID {
		return nil, fmt.Errorf("同じアーティストには統合できません")
	}
	source, err := s.artistRepo.FindByID(sourceID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, nil
	}
	if err := s.artistRepo.MergeArtists(sourceID, targetID); err != nil {
		return nil, err
	}
	logger.Infof("artist merged: %q -> target %s", source.Name, targetID)
	target, err := s.artistRepo.FindByID(targetID)
	if err != nil || target == nil {
		return nil, err
	}
	resp := toArtistResponse(*target)
	return &resp, nil
}

// GetEditableFields は修正提案の対象となる編集可能フィールドと表示ラベルを返す。
// 見つからなければ (nil, "", nil)。TargetEditor インターフェースを満たす。
// access は使わない（アーティストは配信に紐付かないので秘匿の対象外）。interface を揃えるために受け取る。
func (s *ArtistService) GetEditableFields(id uuid.UUID, _ repository.ViewerAccess) (map[string]string, string, error) {
	a, err := s.artistRepo.FindByID(id)
	if err != nil {
		return nil, "", err
	}
	if a == nil {
		return nil, "", nil
	}
	reading := ""
	if a.NameReading.Valid {
		reading = a.NameReading.String
	}
	// aliases は「同一人物の別名義」。権限の無い利用者が別名義を提案できるように、
	// 修正提案の編集可能フィールドとして開けてある（読点区切りの 1 行で扱う）。
	return map[string]string{
		"name":         a.Name,
		"name_reading": reading,
		"aliases":      strings.Join(a.Aliases, "、"),
	}, a.Name, nil
}

// ApplyEditableFields は提案された編集値をアーティストへ反映する。
// 名前変更時は所属楽曲の表示テキストも連動更新される（Update 経由）。
func (s *ArtistService) ApplyEditableFields(id uuid.UUID, fields map[string]string) error {
	var name, reading *string
	if v, ok := fields["name"]; ok {
		vv := v
		name = &vv
	}
	if v, ok := fields["name_reading"]; ok {
		vv := v
		reading = &vv
	}
	if v, ok := fields["aliases"]; ok {
		var aliases []string
		for _, part := range strings.Split(v, "、") {
			if t := strings.TrimSpace(part); t != "" {
				aliases = append(aliases, t)
			}
		}
		if err := s.artistRepo.UpdateAliases(id, aliases); err != nil {
			return err
		}
		// 別名義だけの提案なら名前は触らない（Update は名前の変更を伴うため）
		if name == nil && reading == nil {
			return nil
		}
	}

	result, err := s.Update(id, name, reading)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("アーティストが見つかりません")
	}
	return nil
}

// ========== 読み仮名の AI 補完 ==========

const readingSystemPrompt = `あなたは日本の音楽（J-POP・アニソン・ボカロ・VTuberオリジナル曲）に詳しいアシスタントです。
与えられた名前（アーティスト名または曲名）の正しい「読み」を平仮名で返してください。

ルール:
- 読みは平仮名のみ（漢字・片仮名を含めない。外来語・英語も平仮名にする。例: "Lemon" → "れもん"）
- 実在の有名なアーティスト/曲は正式な読みを使う（例: "米津玄師" → "よねづけんし"、"ヨルシカ" → "よるしか"）
- 確信が持てない場合は confidence を下げる
- 出力は JSON 配列のみ。説明文は不要

出力形式:
[{"index":0,"reading":"よねづけんし","confidence":0.95}]`

type readingSuggestion struct {
	Index      int     `json:"index"`
	Reading    string  `json:"reading"`
	Confidence float64 `json:"confidence"`
}

// isKanaReading は読みとして妥当（漢字を含まない・非空）かを判定する。
func isKanaReading(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !repository.ContainsHan(s)
}

// backfillBatch は名前リストの読みを AI に問い合わせ、index→reading を返す。
func (s *ArtistService) backfillBatch(kind string, names []string) (map[int]string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "以下の%sの読みを返してください:\n", kind)
	for i, n := range names {
		fmt.Fprintf(&b, "%d. %s\n", i, n)
	}

	response, err := s.aiService.SimpleChat(readingSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("ai chat: %w", err)
	}

	cleaned := ai.CleanJSONResponse(response)
	var suggestions []readingSuggestion
	if err := json.NewDecoder(strings.NewReader(cleaned)).Decode(&suggestions); err != nil {
		return nil, fmt.Errorf("parse ai readings: %w", err)
	}

	out := make(map[int]string)
	for _, sg := range suggestions {
		// 信頼度が低いもの・読みとして不正なものは採用しない
		if sg.Confidence >= 0.6 && isKanaReading(sg.Reading) && sg.Index >= 0 && sg.Index < len(names) {
			out[sg.Index] = strings.TrimSpace(sg.Reading)
		}
	}
	return out, nil
}

const readingBatchSize = 30

// BackfillReadings は読みが未整備なアーティスト・楽曲の読み仮名を AI で補完する。
// 1回の呼び出しで各対象を最大 batchLimit 件処理し、残数を返す（ボタン連打で続きを処理できる）。
func (s *ArtistService) BackfillReadings() (*dto.BackfillReadingsResponse, error) {
	resp := &dto.BackfillReadingsResponse{}

	// アーティスト
	artists, err := s.artistRepo.ListMissingReadings(readingBatchSize)
	if err != nil {
		return nil, err
	}
	if len(artists) > 0 {
		names := make([]string, len(artists))
		for i, a := range artists {
			names[i] = a.Name
		}
		readings, err := s.backfillBatch("アーティスト名", names)
		if err != nil {
			logger.Warnf("artist readings backfill failed: %v", err)
			resp.Warning = fmt.Sprintf("アーティスト読み補完に失敗: %v", err)
		} else {
			for i, a := range artists {
				if reading, ok := readings[i]; ok {
					if err := s.artistRepo.UpdateReadingPropagate(a.ID, reading); err == nil {
						resp.ArtistsUpdated++
					}
				}
			}
		}
	}

	// 楽曲名
	songs, err := s.songRepo.ListMissingNameReadings(readingBatchSize)
	if err != nil {
		return nil, err
	}
	if len(songs) > 0 {
		names := make([]string, len(songs))
		for i, sg := range songs {
			names[i] = sg.Name
		}
		readings, err := s.backfillBatch("曲名", names)
		if err != nil {
			logger.Warnf("song readings backfill failed: %v", err)
			if resp.Warning == "" {
				resp.Warning = fmt.Sprintf("曲名読み補完に失敗: %v", err)
			}
		} else {
			for i, sg := range songs {
				if reading, ok := readings[i]; ok {
					if err := s.songRepo.UpdateNameReading(sg.ID, reading); err == nil {
						resp.SongsUpdated++
					}
				}
			}
		}
	}

	logger.Infof("readings backfill: artists=%d songs=%d", resp.ArtistsUpdated, resp.SongsUpdated)
	return resp, nil
}

// FindByName は表示名でアーティストを引く。見つからなければ nil。
// 別名義の提案で「その名前が artists に居るか」を確かめるために使う。
func (s *ArtistService) FindByName(name string) (*models.Artist, error) {
	return s.artistRepo.FindByName(name)
}
