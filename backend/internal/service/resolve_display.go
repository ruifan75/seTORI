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
// ── どれも comment_raw が変わらない限り不変で、作り直すのに時間と金がかかる。

// ResolveForDisplay は保存済みの解析結果に現在の DB 照合を当て、
// 併せて「どの処理でその欄が変わったか」を記録する。**何も保存しない。**
//
// 呼ぶのは画面に出す直前（配信詳細の読み取り、解析の応答）。
func (s *NormalizationService) ResolveForDisplay(songs []dto.CommentSong) {
	if s == nil || s.matchService == nil {
		return
	}
	for i := range songs {
		name, artist := MatchInputs(songs[i].Name, songs[i].OriginalArtist,
			songs[i].NormalizedName, songs[i].NormalizedArtist)
		m := s.ResolveMatch(name, artist)

		songs[i].MatchedSongID = m.MatchedSongID
		songs[i].MatchedSongName = m.MatchedSongName
		songs[i].MatchedSongNameReading = m.MatchedSongNameReading
		songs[i].MatchedSongArtist = m.MatchedSongArtist
		songs[i].MatchedSongArtistReading = m.MatchedSongArtistReading
		songs[i].MatchedSongArtURL = m.MatchedSongArtURL
		songs[i].MatchedSongItunesID = m.MatchedSongItunesID
		songs[i].MatchCandidates = m.MatchCandidates
		songs[i].Changes = buildFieldChanges(songs[i], m.MatchReason, m.MatchScore)
	}
}

// buildFieldChanges は 抽出 → 正規化 → 照合 の各段で名前がどう変わったかを並べる。
// 変わらなかった段は入れない（「何も起きていない」を並べても読む側の負担になるだけ）。
func buildFieldChanges(s dto.CommentSong, reason string, score float64) []dto.FieldChange {
	var out []dto.FieldChange

	add := func(field, by, from, to, reason string, score float64) {
		// from が空でも記録する。「留言に歌手が書かれていなかったのに、
		// 照合で埋まった」は利用者がいちばん確かめたい変化で、
		// これを落とすと画面に理由なく名前が現れたように見える。
		if to == "" || from == to {
			return
		}
		out = append(out, dto.FieldChange{
			Field: field, By: by, From: from, To: to, Reason: reason, Score: score,
		})
	}

	// 抽出 → AI 正規化
	add("name", "ai_normalize", s.Name, s.NormalizedName, "", 0)
	add("artist", "ai_normalize", s.OriginalArtist, s.NormalizedArtist, "", 0)

	// 正規化（無ければ抽出） → DB 照合。照合できた場合だけ
	if s.MatchedSongID != nil {
		name, artist := MatchInputs(s.Name, s.OriginalArtist, s.NormalizedName, s.NormalizedArtist)
		if s.MatchedSongName != nil {
			add("name", "db_match", name, *s.MatchedSongName, reason, score)
		}
		if s.MatchedSongArtist != nil {
			add("artist", "db_match", artist, *s.MatchedSongArtist, reason, score)
		}
	}
	return out
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
