package service

import (
	"strings"
)

const shortFormMaxDurationSeconds = 180

// これらのタグが付いた配信は通常一覧へ出す。ただし shorts の印があり、かつ
// 3 分以下（または長さ不明）の動画は、音楽タグが誤って付いていても短尺として隠す。
var visibleMusicStreamTagIDs = []string{
	"concert",
	"karaoke",
	"music_cover",
	"mv",
	"original_song",
	"singing",
}

// initialStreamHidden は Holodex topic と seTORI 側のタグをどちらも「初回判定の
// 訊号」として扱う。どちらか一方を常に正しいとはみなさず、shorts は動画長と
// 組み合わせて判定する。
func initialStreamHidden(topicID string, durationSeconds int, durationKnown bool, tagIDs []string) bool {
	tags := make(map[string]struct{}, len(tagIDs))
	for _, tagID := range tagIDs {
		tagID = strings.ToLower(strings.TrimSpace(tagID))
		if tagID != "" {
			tags[tagID] = struct{}{}
		}
	}

	topicID = strings.ToLower(strings.TrimSpace(topicID))
	_, hasShortsTag := tags["shorts"]
	if (topicID == "shorts" || hasShortsTag) && (!durationKnown || durationSeconds <= shortFormMaxDurationSeconds) {
		return true
	}

	for _, tagID := range visibleMusicStreamTagIDs {
		if _, ok := tags[tagID]; ok {
			return false
		}
	}

	return true
}
