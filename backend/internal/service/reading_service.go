package service

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

// ReadingService は読み仮名データの一括エクスポート/インポートを担う。
// 外部の（インターネット情報を参照できる）AI で読みを埋めてもらい、再取り込みする運用を支える。
type ReadingService struct {
	artistRepo *repository.ArtistRepository
	songRepo   *repository.SongRepository
}

func NewReadingService(artistRepo *repository.ArtistRepository, songRepo *repository.SongRepository) *ReadingService {
	return &ReadingService{artistRepo: artistRepo, songRepo: songRepo}
}

// needsFix は読みが未整備（名前に漢字を含み、読みが空 or 読みに漢字が残る）かを返す。
func needsFix(name, reading string) bool {
	return repository.ContainsHan(name) && (reading == "" || repository.ContainsHan(reading))
}

// Export はアーティスト・楽曲の読みをまとめて返す。
// onlyNeedsFix=true のときは読みが未整備の項目のみを対象にする（AI で埋める作業を絞れる）。
func (s *ReadingService) Export(onlyNeedsFix bool) (*dto.ReadingsExport, error) {
	artists, err := s.artistRepo.ListAllReadings()
	if err != nil {
		return nil, err
	}
	songs, err := s.songRepo.ListAllReadings()
	if err != nil {
		return nil, err
	}

	out := &dto.ReadingsExport{Artists: []dto.ReadingItem{}, Songs: []dto.ReadingItem{}}
	for _, a := range artists {
		reading := ""
		if a.NameReading.Valid {
			reading = a.NameReading.String
		}
		if onlyNeedsFix && !needsFix(a.Name, reading) {
			continue
		}
		out.Artists = append(out.Artists, dto.ReadingItem{ID: a.ID.String(), Name: a.Name, Reading: reading})
	}
	for _, sg := range songs {
		reading := ""
		if sg.NameReading.Valid {
			reading = sg.NameReading.String
		}
		if onlyNeedsFix && !needsFix(sg.Name, reading) {
			continue
		}
		out.Songs = append(out.Songs, dto.ReadingItem{ID: sg.ID.String(), Name: sg.Name, Reading: reading})
	}
	return out, nil
}

// normalizeReading は読みを平仮名へ正規化（片仮名→平仮名、trim）し、採用可否を返す。
// 空文字は「読みを消す」意図として ok=true で通す。漢字が残るものは不可。
func normalizeReading(raw string) (reading string, ok bool) {
	r := repository.KataToHira(strings.TrimSpace(raw))
	if r == "" {
		return "", true
	}
	if repository.ContainsHan(r) {
		return "", false
	}
	return r, true
}

// Import は読みデータを取り込む。reading は平仮名へ正規化し、漢字が残る不正値はスキップする。
// アーティストの読みは所属楽曲の original_artist_reading にも伝播させる。
func (s *ReadingService) Import(data *dto.ReadingsExport) (*dto.ImportReadingsResult, error) {
	res := &dto.ImportReadingsResult{}

	for _, item := range data.Artists {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("アーティストID不正: %s", item.ID))
			continue
		}
		reading, ok := normalizeReading(item.Reading)
		if !ok {
			res.Skipped++
			continue
		}
		if err := s.artistRepo.UpdateReadingPropagate(id, reading); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("アーティスト %s: %v", item.ID, err))
			continue
		}
		res.ArtistsUpdated++
	}

	for _, item := range data.Songs {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("楽曲ID不正: %s", item.ID))
			continue
		}
		reading, ok := normalizeReading(item.Reading)
		if !ok {
			res.Skipped++
			continue
		}
		if err := s.songRepo.UpdateNameReading(id, reading); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("楽曲 %s: %v", item.ID, err))
			continue
		}
		res.SongsUpdated++
	}

	logger.Infof("readings import: artists=%d songs=%d skipped=%d errors=%d",
		res.ArtistsUpdated, res.SongsUpdated, res.Skipped, len(res.Errors))
	return res, nil
}
