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
				// 選擇資訊更完整的那個
				if betterSong(song, result[i]) {
					result[i] = song
				}
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

// betterSong 判斷 a 是否比 b 更完整
func betterSong(a, b ParsedSong) bool {
	// 有藝人資訊的優先
	if a.OriginalArtist != "" && b.OriginalArtist == "" {
		return true
	}
	if a.OriginalArtist == "" && b.OriginalArtist != "" {
		return false
	}

	// 有結束時間的優先
	if a.End > 0 && b.End == 0 {
		return true
	}
	if a.End == 0 && b.End > 0 {
		return false
	}

	// 歌名較長的可能更完整
	return len(a.Name) > len(b.Name)
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
