package repository

import (
	"github.com/ruifan75/setori/internal/models"
)

// preparationWhere は指定チャンネルが所有する表示中・未処理の配信。
// mention で発見した他人の配信、会限として検出された配信、人が秘匿した配信は取得しない。
// 公開の裁定は歌単の公開許可なので、外部取得の対象判定とは分けて検査する。
func preparationWhere() string {
	return `s.is_hidden = FALSE AND s.is_processed = FALSE
          AND EXISTS (SELECT 1 FROM stream_singers ss
                      WHERE ss.stream_id = s.id AND ss.singer_id = $1 AND ss.is_owner = TRUE)
          AND NOT ` + MembersOnlyDetectedExpr("s") + `
          AND s.restriction_override IS DISTINCT FROM TRUE`
}

// FindPreparationStreams は開始時に候補を列挙する。各段階の直前にも再検査が必要。
func (r *StreamRepository) FindPreparationStreams(singerID string) ([]models.Stream, error) {
	rows, err := r.db.Query(`SELECT s.id, s.title, s.is_hidden, s.is_processed, s.chapter_raw
        FROM streams s
        WHERE `+preparationWhere()+`
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

// PreparationStreamEligible は待機中の編集・同期・削除を各取得／解析の直前に見直す。
func (r *StreamRepository) PreparationStreamEligible(singerID, streamID string) (bool, error) {
	var eligible bool
	err := r.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM streams s WHERE `+preparationWhere()+` AND s.id = $2)`, singerID, streamID).Scan(&eligible)
	return eligible, err
}
