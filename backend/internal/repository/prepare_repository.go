package repository

import (
	"github.com/ruifan75/setori/internal/models"
)

// FindPreparationStreams は指定チャンネルが所有する表示中・未処理の配信。
// mention で発見した他人の配信まで準備しない。2 段ともこのスナップショットを使う。
func (r *StreamRepository) FindPreparationStreams(singerID string) ([]models.Stream, error) {
	rows, err := r.db.Query(`SELECT s.id, s.title, s.is_hidden, s.is_processed, s.chapter_raw
        FROM streams s
        WHERE s.is_hidden = FALSE AND s.is_processed = FALSE
          AND EXISTS (SELECT 1 FROM stream_singers ss
                      WHERE ss.stream_id = s.id AND ss.singer_id = $1 AND ss.is_owner = TRUE)
        ORDER BY s.stream_date ASC, s.id ASC`, singerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Stream{}
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title, &s.IsHidden, &s.IsProcessed, &s.ChapterRaw); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
