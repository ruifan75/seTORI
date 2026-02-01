package comment

import (
	"sort"
)

const (
	// 預設歌曲長度（秒）- 當無法推算時使用
	DefaultSongDuration = 240 // 4 分鐘
	// 最大歌曲長度（秒）
	MaxSongDuration = 600 // 10 分鐘
	// 最小歌曲長度（秒）
	MinSongDuration = 60 // 1 分鐘
)

// EstimateEndTimes 推算缺失的結束時間
// 使用下一首歌的開始時間來推算
func EstimateEndTimes(songs []ParsedSong, totalDuration int) []ParsedSong {
	if len(songs) == 0 {
		return songs
	}

	// 先按開始時間排序
	result := make([]ParsedSong, len(songs))
	copy(result, songs)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start < result[j].Start
	})

	// 推算結束時間
	for i := range result {
		if result[i].End == 0 {
			if i+1 < len(result) {
				// 使用下一首的開始時間
				nextStart := result[i+1].Start
				estimatedEnd := nextStart

				// 確保推算的長度合理
				duration := estimatedEnd - result[i].Start
				if duration > MaxSongDuration {
					estimatedEnd = result[i].Start + DefaultSongDuration
				} else if duration < MinSongDuration {
					// 如果太短，可能是格式問題，使用預設長度
					estimatedEnd = result[i].Start + DefaultSongDuration
				}

				result[i].End = estimatedEnd
				result[i].IsEndTimeEstimated = true
			} else {
				// 最後一首歌
				if totalDuration > 0 && totalDuration > result[i].Start {
					// 使用影片總長度
					estimatedEnd := totalDuration
					duration := estimatedEnd - result[i].Start
					if duration > MaxSongDuration {
						estimatedEnd = result[i].Start + DefaultSongDuration
					}
					result[i].End = estimatedEnd
					result[i].IsEndTimeEstimated = true
				} else {
					// 使用預設長度
					result[i].End = result[i].Start + DefaultSongDuration
					result[i].IsEndTimeEstimated = true
				}
			}
		}
	}

	return result
}

// AssignOrderIndex 為歌曲分配順序索引
func AssignOrderIndex(songs []ParsedSong) []ParsedSong {
	result := make([]ParsedSong, len(songs))
	copy(result, songs)

	// 按開始時間排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Start < result[j].Start
	})

	return result
}

// ValidateSongs 驗證歌曲資料的合理性
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
