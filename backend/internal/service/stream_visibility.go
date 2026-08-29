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
// シグナル」として扱う。どちらか一方を常に正しいとはみなさず、shorts は動画長と
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

// initialMembersOnlyCandidate は初回登録の時点で「会限らしい」と言えるかを見る。
//
// **付ける方向にしか使わない**ので、取りこぼしより過剰を選ぶ ── 見落とすと中身を公開して
// しまうが、余分に付いても編集者がタグを外せばよい。
//
// Holodex の topic_id は単値なので `singing` などと排他になり、会限の歌枠が
// `membersonly` にならないことがある（実測 singing 409 / membersonly 85）。
// そのため seTORI 側のタイトル規則で付く members_only タグも併せて見る。
func initialMembersOnlyCandidate(topicID string, tagIDs []string) bool {
	if strings.EqualFold(strings.TrimSpace(topicID), "membersonly") {
		return true
	}
	for _, id := range tagIDs {
		if id == "members_only" {
			return true
		}
	}
	return false
}
