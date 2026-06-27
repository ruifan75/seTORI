package comment

import (
	"github.com/ruifan75/setori/pkg/util"
)

const (
	// 時間戳相近的閾值（秒）
	TimestampThreshold = 30
	// 歌名相似度閾值
	SimilarityThreshold = 0.8
)

// DeduplicateSongs 去除重複的歌曲
// 規則：時間戳相近（≤30秒）且歌名相似度 ≥80% 視為同一首
func DeduplicateSongs(songs []ParsedSong) []ParsedSong {
	if len(songs) == 0 {
		return songs
	}

	var result []ParsedSong

	for _, song := range songs {
		isDuplicate := false

		for i := range result {
			if isSimilar(song, result[i]) {
				// 合併兩者的資訊，取各欄位最完整的版本
				result[i] = mergeParsedSong(result[i], song)
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			result = append(result, song)
		}
	}

	return result
}

// isSimilar 判斷兩首歌是否相似（可能是重複）
func isSimilar(a, b ParsedSong) bool {
	// 檢查時間戳是否相近
	timeDiff := abs(a.Start - b.Start)
	if timeDiff > TimestampThreshold {
		return false
	}

	// 檢查歌名相似度
	similarity := util.Similarity(a.Name, b.Name)
	return similarity >= SimilarityThreshold
}

// mergeParsedSong 合併兩首相似歌曲，取各欄位最完整的版本
func mergeParsedSong(existing, incoming ParsedSong) ParsedSong {
	merged := existing

	// 藝人名：優先較完整（較長）的版本
	if len(incoming.OriginalArtist) > len(merged.OriginalArtist) {
		merged.OriginalArtist = incoming.OriginalArtist
	}

	// 結束時間：優先「有值且非估計」的版本
	switch {
	case merged.End == 0 && incoming.End > 0:
		merged.End = incoming.End
		merged.IsEndTimeEstimated = incoming.IsEndTimeEstimated
	case incoming.End > 0 && merged.IsEndTimeEstimated && !incoming.IsEndTimeEstimated:
		// 既有的是估計值，incoming 是實際值 → 用實際值取代
		merged.End = incoming.End
		merged.IsEndTimeEstimated = false
	}

	// 取較長的歌名（可能更完整）
	if len(incoming.Name) > len(merged.Name) {
		merged.Name = incoming.Name
	}

	// 保留原始留言（取較長的）
	if len(incoming.OriginalComment) > len(merged.OriginalComment) {
		merged.OriginalComment = incoming.OriginalComment
	}

	return merged
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// MergeSongs 合併來自不同來源的歌曲列表
// 例如：合併 Holodex 和 Comment 分析的結果
func MergeSongs(primary, secondary []ParsedSong) []ParsedSong {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}

	result := make([]ParsedSong, len(primary))
	copy(result, primary)

	for _, song := range secondary {
		found := false
		for i := range result {
			if isSimilar(song, result[i]) {
				// 使用 secondary 來補充缺失的資訊
				if result[i].OriginalArtist == "" && song.OriginalArtist != "" {
					result[i].OriginalArtist = song.OriginalArtist
				}
				if result[i].End == 0 && song.End > 0 {
					result[i].End = song.End
				}
				found = true
				break
			}
		}

		if !found {
			result = append(result, song)
		}
	}

	return result
}
