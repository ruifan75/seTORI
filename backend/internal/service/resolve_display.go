package service

import "github.com/ruifan75/setori/internal/dto"

// 照合（どの楽曲か）は**保存しない**。読み取るたびに現在の DB で計算する。
//
// 保存していた頃は、曲が増えたり統合したり別名義を学ぶたびに、保存済みの
// matched_song_id が黙って古くなっていた。それを追いかけるために
// backfill-matches という端点、comment_songs_hash を壊さないための注意書き、
// 「候補が変わったか」の差分判定が要り、しかも 2 つある解析経路
// （コメント / Holodex）で保存する・しないの判断が食い違っていた。
//
// 照合は索引を引くだけで数ミリ秒なので、読み取り時に計算すればこれらは全部要らない。
// 保存に値するのは AI を使う抽出と正規化、そして live chat の拍手 end だけ
// ── どれも元データが変わらない限り不変で、作り直すのに時間と金がかかる。

// resolveOne は 1 件を照合し、変更履歴を添えて返す。
// コメント経路（CommentSong）と Holodex 経路（SongSuggestion）で共用する
// ── 2 つに分けて書いていた頃、片方だけ保存するという食い違いが生まれた。
func (s *NormalizationService) resolveOne(rawName, rawArtist, normName, normArtist string, itunesID *int64) (dto.AISuggestionResult, []dto.FieldChange) {
	name, artist := MatchInputs(rawName, rawArtist, normName, normArtist)
	m := s.ResolveMatch(name, artist, itunesID)

	var changes []dto.FieldChange
	add := func(field, by, from, to, reason string, score float64) {
		// from が空でも記録する。「留言に歌手が書かれていなかったのに、
		// 照合で埋まった」は利用者がいちばん確かめたい変化で、
		// これを落とすと画面に理由なく名前が現れたように見える。
		if to == "" || from == to {
			return
		}
		changes = append(changes, dto.FieldChange{
			Field: field, By: by, From: from, To: to, Reason: reason, Score: score,
		})
	}

	// 抽出 → AI 正規化
	add("name", "ai_normalize", rawName, normName, "", 0)
	add("artist", "ai_normalize", rawArtist, normArtist, "", 0)

	// 正規化（無ければ抽出） → DB 照合。照合できた場合だけ
	if m.MatchedSongID != nil {
		if m.MatchedSongName != nil {
			add("name", "db_match", name, *m.MatchedSongName, m.MatchReason, m.MatchScore)
		}
		if m.MatchedSongArtist != nil {
			add("artist", "db_match", artist, *m.MatchedSongArtist, m.MatchReason, m.MatchScore)
		}
	}
	return m, changes
}

// ResolveForDisplay は保存済みの解析結果に現在の DB 照合を当て、
// 併せて「どの処理でその欄が変わったか」を記録する。**何も保存しない。**
//
// 呼ぶのは画面に出す直前（配信詳細の読み取り、解析の応答）。
func (s *NormalizationService) ResolveForDisplay(songs []dto.CommentSong) {
	if s == nil || s.matchService == nil {
		return
	}
	for i := range songs {
		// コメントには iTunes ID が無い
		m, changes := s.resolveOne(songs[i].Name, songs[i].OriginalArtist,
			songs[i].NormalizedName, songs[i].NormalizedArtist, nil)

		songs[i].MatchedSongID = m.MatchedSongID
		songs[i].MatchedSongName = m.MatchedSongName
		songs[i].MatchedSongNameReading = m.MatchedSongNameReading
		songs[i].MatchedSongArtist = m.MatchedSongArtist
		songs[i].MatchedSongArtistReading = m.MatchedSongArtistReading
		songs[i].MatchedSongArtURL = m.MatchedSongArtURL
		songs[i].MatchedSongItunesID = m.MatchedSongItunesID
		songs[i].MatchCandidates = m.MatchCandidates
		songs[i].Changes = changes
	}
}

// ResolveSuggestionsForDisplay は Holodex 経路の同じ処理。
// Holodex は曲ごとに iTunes ID を持っているので、必ず渡す。
func (s *NormalizationService) ResolveSuggestionsForDisplay(songs []dto.SongSuggestion) {
	if s == nil || s.matchService == nil {
		return
	}
	for i := range songs {
		m, changes := s.resolveOne(songs[i].Name, songs[i].OriginalArtist,
			songs[i].NormalizedName, songs[i].NormalizedArtist, songs[i].ItunesID)

		songs[i].MatchedSongID = m.MatchedSongID
		songs[i].MatchedSongName = m.MatchedSongName
		songs[i].MatchedSongNameReading = m.MatchedSongNameReading
		songs[i].MatchedSongArtist = m.MatchedSongArtist
		songs[i].MatchedSongArtistReading = m.MatchedSongArtistReading
		songs[i].MatchedSongArtURL = m.MatchedSongArtURL
		songs[i].MatchedSongItunesID = m.MatchedSongItunesID
		songs[i].MatchCandidates = m.MatchCandidates
		songs[i].Changes = changes
	}
}

// stripMatchForStorage は保存前に照合の結果を落とす。
//
// 照合は読み取り時に計算する約束なので、保存すると「古い答えが 2 か所にある」状態になる。
// 抽出・正規化・拍手 end だけを残す。
func stripMatchForStorage(songs []dto.CommentSong) []dto.CommentSong {
	out := make([]dto.CommentSong, len(songs))
	copy(out, songs)
	for i := range out {
		out[i].MatchedSongID = nil
		out[i].MatchedSongName = nil
		out[i].MatchedSongNameReading = nil
		out[i].MatchedSongArtist = nil
		out[i].MatchedSongArtistReading = nil
		out[i].MatchedSongArtURL = nil
		out[i].MatchedSongItunesID = nil
		out[i].MatchCandidates = nil
		out[i].Changes = nil
	}
	return out
}

// stripMatchFromSuggestions は Holodex 経路の同じ処理。
func stripMatchFromSuggestions(songs []dto.SongSuggestion) []dto.SongSuggestion {
	out := make([]dto.SongSuggestion, len(songs))
	copy(out, songs)
	for i := range out {
		out[i].MatchedSongID = nil
		out[i].MatchedSongName = nil
		out[i].MatchedSongNameReading = nil
		out[i].MatchedSongArtist = nil
		out[i].MatchedSongArtistReading = nil
		out[i].MatchedSongArtURL = nil
		out[i].MatchedSongItunesID = nil
		out[i].MatchCandidates = nil
		out[i].Changes = nil
	}
	return out
}
