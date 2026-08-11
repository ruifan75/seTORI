package service

import (
	"encoding/json"
	"strings"

	"github.com/ruifan75/setori/internal/models"
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

// defaultStreamHidden は Holodex topic と seTORI 側のタグをどちらも「自動判定の
// 訊号」として扱う。どちらか一方を常に正しいとはみなさず、shorts は動画長と
// 組み合わせて判定する。
func defaultStreamHidden(topicID string, durationSeconds int, durationKnown bool, tagIDs []string) bool {
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

func automaticStreamHidden(stream models.Stream, tags []models.StreamTag) bool {
	tagIDs := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagIDs = append(tagIDs, tag.ID)
	}

	return defaultStreamHidden(
		streamTopicID(stream.HolodexData),
		int(stream.DurationSeconds.Int32),
		stream.DurationSeconds.Valid && stream.DurationSeconds.Int32 > 0,
		tagIDs,
	)
}

func streamTopicID(holodexData []byte) string {
	if len(holodexData) == 0 {
		return ""
	}

	var data struct {
		TopicID string `json:"topic_id"`
	}
	if err := json.Unmarshal(holodexData, &data); err != nil {
		return ""
	}
	return data.TopicID
}
