package comment

import (
	"sort"
)

const (
	// デフォルト楽曲長（秒）- 推定できない場合に使用
	DefaultSongDuration = 240 // 4 分鐘
	// 最大楽曲長（秒）
	MaxSongDuration = 600 // 10 分鐘
	// 最小楽曲長（秒）
	MinSongDuration = 60 // 1 分鐘
)

// EstimateEndTimes 欠落した終了時間を推定
// 次の曲の開始時間を使用して推定
func EstimateEndTimes(songs []ParsedSong, totalDuration int) []ParsedSong {
	if len(songs) == 0 {
		return songs
	}

	// 開始時間順にソート
	result := make([]ParsedSong, len(songs))
	copy(result, songs)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start < result[j].Start
	})

	// 終了時間を推定
	for i := range result {
		if result[i].End == 0 {
			if i+1 < len(result) {
				// 次の曲の開始時間を使用
				nextStart := result[i+1].Start
				estimatedEnd := nextStart

				// 推定長が妥当であることを確認
				duration := estimatedEnd - result[i].Start
				if duration > MaxSongDuration {
					estimatedEnd = result[i].Start + DefaultSongDuration
				} else if duration < MinSongDuration {
					// 短すぎる場合はフォーマット問題の可能性、デフォルト長を使用
					estimatedEnd = result[i].Start + DefaultSongDuration
				}

				result[i].End = estimatedEnd
				result[i].IsEndTimeEstimated = true
			} else {
				// 最後の曲
				if totalDuration > 0 && totalDuration > result[i].Start {
					// 動画の全長を使用
					estimatedEnd := totalDuration
					duration := estimatedEnd - result[i].Start
					if duration > MaxSongDuration {
						estimatedEnd = result[i].Start + DefaultSongDuration
					}
					result[i].End = estimatedEnd
					result[i].IsEndTimeEstimated = true
				} else {
					// デフォルト長を使用
					result[i].End = result[i].Start + DefaultSongDuration
					result[i].IsEndTimeEstimated = true
				}
			}
		}
	}

	return result
}

// AssignOrderIndex 楽曲に順序インデックスを割り当て
func AssignOrderIndex(songs []ParsedSong) []ParsedSong {
	result := make([]ParsedSong, len(songs))
	copy(result, songs)

	// 開始時間順にソート
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start < result[j].Start
	})

	return result
}

// ValidateSongs 楽曲データの妥当性を検証
func ValidateSongs(songs []ParsedSong) []ParsedSong {
	var valid []ParsedSong

	for _, song := range songs {
		// 跳過開始時間為負的
		if song.Start < 0 {
			continue
		}

		// 跳過結束時間早於開始時間的
		if song.End > 0 && song.End <= song.Start {
			continue
		}

		// 跳過沒有歌名的
		if song.Name == "" {
			continue
		}

		valid = append(valid, song)
	}

	return valid
}
