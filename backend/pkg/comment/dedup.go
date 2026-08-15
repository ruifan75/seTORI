package comment

import (
	"github.com/ruifan75/setori/pkg/util"
)

const (
	// タイムスタンプが近いとみなす閾値（秒）
	TimestampThreshold = 30
	// 曲名の類似度閾値
	SimilarityThreshold = 0.8
)

// DeduplicateSongs は重複する楽曲を除く。
// 規則：タイムスタンプが近く（30 秒以内）、曲名の類似度が 80% 以上なら同じ曲とみなす。
func DeduplicateSongs(songs []ParsedSong) []ParsedSong {
	if len(songs) == 0 {
		return songs
	}

	var result []ParsedSong

	for _, song := range songs {
		isDuplicate := false

		for i := range result {
			if isSimilar(song, result[i]) {
				// 両方の情報を統合し、各フィールドで最も完全な値を使う
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

// isSimilar は 2 曲が類似しているか判定する（重複の可能性）。
func isSimilar(a, b ParsedSong) bool {
	// タイムスタンプが近いか確認する
	timeDiff := abs(a.Start - b.Start)
	if timeDiff > TimestampThreshold {
		return false
	}

	// 曲名の類似度を確認する
	similarity := util.Similarity(a.Name, b.Name)
	return similarity >= SimilarityThreshold
}

// mergeParsedSong は類似する 2 曲を統合し、各フィールドで最も完全な値を使う。
func mergeParsedSong(existing, incoming ParsedSong) ParsedSong {
	merged := existing

	// アーティスト名：より完全な（長い）値を優先する
	if len(incoming.OriginalArtist) > len(merged.OriginalArtist) {
		merged.OriginalArtist = incoming.OriginalArtist
	}

	// 時刻：コメント自体に開始・終了時刻があるものを優先し、start/end は対で保持する。
	switch {
	case !hasExplicitEnd(merged) && hasExplicitEnd(incoming):
		merged.Start = incoming.Start
		merged.End = incoming.End
		merged.IsEndTimeEstimated = false
	case merged.End == 0 && incoming.End > 0:
		merged.End = incoming.End
		merged.IsEndTimeEstimated = incoming.IsEndTimeEstimated
	case incoming.End > 0 && merged.IsEndTimeEstimated && !incoming.IsEndTimeEstimated:
		merged.End = incoming.End
		merged.IsEndTimeEstimated = false
	}

	// より長い曲名を使う（より完全な可能性がある）
	if len(incoming.Name) > len(merged.Name) {
		merged.Name = incoming.Name
	}

	// 元コメントは長い方を残す
	if len(incoming.OriginalComment) > len(merged.OriginalComment) {
		merged.OriginalComment = incoming.OriginalComment
	}

	return merged
}

func hasExplicitEnd(song ParsedSong) bool {
	return song.End > 0 && !song.IsEndTimeEstimated
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// MergeSongs は異なる由来の楽曲一覧を統合する。
// 例：Holodex とコメント分析の結果を統合する。
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
				// secondary で不足している情報を補う
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
