import { holodexApi, commentApi, chapterApi } from '../../api/client';
import type { Singer, CommentSong, SongSuggestion } from '../../api/types';
import type { useToast } from '../../components/ui/ToastContext';
import { sameViewer, viewerID } from '../../queryClient';
import { analysisFailureMessage } from '../../utils/apiError';
import type { Dispatch, SetStateAction } from 'react';
import type { QueryClient } from '@tanstack/react-query';
import type { StreamDetailResponse } from '../../api/types';
import type { EditableSong } from './types';
import { mergeDuplicateSongs, endSourceForSourceSong } from './utils';

interface Context {
  stream: StreamDetailResponse | undefined;
  channelOwner: Singer | null;
  fetchTrackDurationByItunesId: (itunesId: number) => Promise<number | null>;
  id: string | undefined;
  showToast: ReturnType<typeof useToast>['showToast'];
  setHolodexAnalyzeLoading: Dispatch<SetStateAction<boolean>>;
  setHolodexTimelineSongs: Dispatch<SetStateAction<SongSuggestion[]>>;
  setEditableSongs: Dispatch<SetStateAction<EditableSong[]>>;
  setCommentAnalyzeLoading: Dispatch<SetStateAction<boolean>>;
  setCommentTimelineSongs: Dispatch<SetStateAction<CommentSong[]>>;
  setChapterAnalyzeLoading: Dispatch<SetStateAction<boolean>>;
  setChapterTimelineSongs: Dispatch<SetStateAction<CommentSong[]>>;
  queryClient: QueryClient;
  setHighlightedSongId: Dispatch<SetStateAction<string | null>>;
  holodexTimelineSongs: SongSuggestion[];
}

// React の state はページの hook に残し、このレンダーの操作だけを組み立てる。
export function createStreamSourceActions(context: Context) {
  const {
    stream,
    channelOwner,
    fetchTrackDurationByItunesId,
    id,
    showToast,
    setHolodexAnalyzeLoading,
    setHolodexTimelineSongs,
    setEditableSongs,
    setCommentAnalyzeLoading,
    setCommentTimelineSongs,
    setChapterAnalyzeLoading,
    setChapterTimelineSongs,
    queryClient,
    setHighlightedSongId,
    holodexTimelineSongs,
  } = context;

  // force=true でキャッシュを無視し AI 再分析（再正規化）。通常はキャッシュ済み結果を即座に読み込む。
  // 編集リストへ入れるときの既定ボーカル（参加チャンネル → チャンネル主）
  const getDefaultSingerIds = () =>
    stream?.participants?.map((p) => p.id) || (channelOwner ? [channelOwner.id] : []);

  // Holodex 分析結果（SongSuggestion）→ EditableSong 変換（一括読み込み・単曲追加で共用）。
  // end === chat_end のものは chat 由来、そうでない end は Holodex 明示値として扱う。
  const suggestionToEditableSong = async (
    song: SongSuggestion,
    editableId: string,
    defaultSingerIds: string[],
  ): Promise<EditableSong> => {
    const hasMatch = !!song.matched_song_id;
    const finalName = hasMatch && song.matched_song_name
      ? song.matched_song_name : (song.normalized_name || song.name);
    const finalNameReading = hasMatch && song.matched_song_name_reading != null
      ? song.matched_song_name_reading : (song.normalized_name_reading || '');
    const finalArtist = hasMatch && song.matched_song_artist
      ? song.matched_song_artist : (song.normalized_artist || song.original_artist);
    const finalArtistReading = hasMatch && song.matched_song_artist_reading != null
      ? song.matched_song_artist_reading : (song.normalized_artist_reading || '');
    const artUrl = (hasMatch ? song.matched_song_art_url : undefined) || song.art_url || null;
    // matched_song_itunes_id は DB 照合で得た既存の紐付け。無い場合のみ Holodex 提供の ID を使う
    // （後者は未登録なので、保存時に song_itunes へ新規紐付けが作られる）
    const itunesId = song.matched_song_itunes_id || song.itunes_id || null;
    const itunesFromDb = !!song.matched_song_itunes_id;
    const trackDuration = itunesId ? await fetchTrackDurationByItunesId(itunesId) : null;
    const explicitEnd = song.end_seconds > 0 && song.end_seconds !== song.chat_end;
    return {
      id: editableId,
      name: finalName,
      nameReading: finalNameReading,
      artist: finalArtist,
      artistReading: finalArtistReading,
      start: song.start_seconds,
      end: song.end_seconds,
      tags: song.tags || [],
      singerIds: song.singer_ids.length > 0 ? song.singer_ids : defaultSingerIds,
      matchedSongId: song.matched_song_id || null,
      artUrl,
      itunesId,
      itunesFromDb,
      trackDuration,
      originalName: finalName,
      originalArtist: finalArtist,
      aiNormalizedName: finalName !== song.name ? song.name : undefined,
      aiNormalizedArtist: finalArtist !== song.original_artist ? song.original_artist : undefined,
      changes: song.changes,
      artistAlias: song.artist_alias,
      // AI が同一人物と言った場合だけ既定でチェックを入れる。
      // 作曲者と原曲歌手の取り違え（メルト / 初音ミク に対し DB は ryo (supercell)）は
      // 「同じ曲だが別人」なので入らない ── 本番ではこちらの方が多い。
      aliasChecked: song.artist_alias?.same_artist ?? false,
      isEndTimeEstimated: false,
      chatEnd: song.chat_end,
      endDiff: song.end_diff,
      originalCommentEnd: explicitEnd ? song.end_seconds : undefined,
      endSource: song.end_seconds > 0 ? (explicitEnd ? 'holodex' : 'chat') : undefined,
      customTags: [],
    };
  };

  // コメント分析結果（CommentSong）→ EditableSong 変換（一括読み込み・単曲追加で共用）
  // 入力元は 'comment'（視聴者のコメント）か 'chapter'（配信者が付けた目次）。
  // 終了時間の確度が違うので分ける ── チャプターの end は次の章節の開始そのもので、
  // 「その曲が終わった時刻」ではない（曲のあとの MC を含む）。
  const commentSongToEditableSong = async (
    song: CommentSong,
    editableId: string,
    defaultSingerIds: string[],
    source: 'comment' | 'chapter' = 'comment',
  ): Promise<EditableSong> => {
    const hasMatch = !!song.matched_song_id;
    const finalName = hasMatch && song.matched_song_name
      ? song.matched_song_name : (song.normalized_name || song.name);
    const finalNameReading = hasMatch && song.matched_song_name_reading != null
      ? song.matched_song_name_reading : (song.normalized_name_reading || '');
    const finalArtist = hasMatch && song.matched_song_artist
      ? song.matched_song_artist : (song.normalized_artist || song.original_artist);
    const finalArtistReading = hasMatch && song.matched_song_artist_reading != null
      ? song.matched_song_artist_reading : (song.normalized_artist_reading || '');
    const artUrl = (hasMatch ? song.matched_song_art_url : undefined) || null;
    const itunesId = song.matched_song_itunes_id || null;
    const trackDuration = itunesId ? await fetchTrackDurationByItunesId(itunesId) : null;
    return {
      id: editableId,
      name: finalName,
      nameReading: finalNameReading,
      artist: finalArtist,
      artistReading: finalArtistReading,
      start: song.start,
      end: song.end,
      tags: song.tags || [],
      singerIds: defaultSingerIds,
      matchedSongId: song.matched_song_id || null,
      artUrl,
      itunesId,
      itunesFromDb: itunesId != null, // コメント経路の ID は DB 照合由来のみ
      trackDuration,
      originalName: finalName,
      originalArtist: finalArtist,
      aiNormalizedName: finalName !== song.name ? song.name : undefined,
      aiNormalizedArtist: finalArtist !== song.original_artist ? song.original_artist : undefined,
      changes: song.changes,
      artistAlias: song.artist_alias,
      // AI が同一人物と言った場合だけ既定でチェックを入れる。
      // 作曲者と原曲歌手の取り違え（メルト / 初音ミク に対し DB は ryo (supercell)）は
      // 「同じ曲だが別人」なので入らない ── 本番ではこちらの方が多い。
      aliasChecked: song.artist_alias?.same_artist ?? false,
      isEndTimeEstimated: song.is_end_time_estimated,
      chatEnd: song.chat_end,
      endDiff: song.end_diff,
      originalCommentEnd: song.end, // 読み込み時の終了時刻を入力元由来の元値として扱う
      endSource: endSourceForSourceSong(song, source),
      customTags: [],
    };
  };

  const loadFromHolodex = async (force = false) => {
    const startedAs = viewerID();
    if (!id) return;
    if (!stream?.holodex_timeline_songs || stream.holodex_timeline_songs.length === 0) {
      showToast('Holodexデータがありません', 'info');
      return;
    }
    setHolodexAnalyzeLoading(true);
    try {
      // 分析（正規化＋DB照合＋拍手end）を実行し、結果をそのまま反映する
      const analyzed = await holodexApi.analyzeSongs(id, force);
      // **照合は応答の直後、最初の state 更新より前に置く。**
      // あとに置くと、編集リストへの反映は止まってもタイムラインには
      // 秘匿の曲名が残る（実際そうなっていた）。
      if (!sameViewer(startedAs)) return;
      const sortedSongs = [...analyzed].sort((a, b) => a.start_seconds - b.start_seconds);
      setHolodexTimelineSongs(sortedSongs);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await suggestionToEditableSong(sortedSongs[index], `holodex-${index}`, getDefaultSingerIds()));
      }

      // **ループ内で iTunes を取りに行くので、書く直前にもう一度確かめる。**
      // 応答直後の照合は「分析結果を書くか」を決めるもので、その後の
      // await までは守らない ── 前回ここを「重複」と読んで消してしまった。
      if (!sameViewer(startedAs)) return;
      const merged = mergeDuplicateSongs(songs);
      const mergedCount = songs.length - merged.length;
      setEditableSongs(merged);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`Holodexから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      if (!sameViewer(startedAs)) return;
      showToast(analysisFailureMessage('Holodex分析', error), 'error');
      console.error('Holodex analysis failed:', error);
    } finally {
      if (sameViewer(startedAs)) setHolodexAnalyzeLoading(false);
    }
  };

  // force=true でキャッシュを無視し AI 再分析（再正規化）。通常はキャッシュ済みの結果を即座に読み込む。
  const loadFromComments = async (force = false) => {
    const startedAs = viewerID();
    if (!id) return;
    setCommentAnalyzeLoading(true);
    try {
      const result = await commentApi.analyze(id, force);
      const sortedSongs = [...result.songs].sort((a, b) => a.start - b.start);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await commentSongToEditableSong(sortedSongs[index], `comment-${index}`, getDefaultSingerIds()));
      }

      const merged = mergeDuplicateSongs(songs);
      const mergedCount = songs.length - merged.length;
      if (!sameViewer(startedAs)) return;
      setEditableSongs(merged);
      // 照合の結果（候補・変更履歴）はこの応答にしか無い。配信を開いただけの
      // 読み取りでは照合しないので、タイムライン側もここで差し替える。
      setCommentTimelineSongs(sortedSongs);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`コメントから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      if (!sameViewer(startedAs)) return;
      showToast(analysisFailureMessage('コメント分析', error), 'error');
      console.error('Comment analysis failed:', error);
    } finally {
      if (sameViewer(startedAs)) setCommentAnalyzeLoading(false);
    }
  };

  // 配信者が付けた目次から読み込む。Holodex にも曲が無く、コメントも取れない配信の受け皿。
  // force=true はチャプターを yt-dlp で取り直してから再分析する（数秒かかる）。
  const loadFromChapters = async (force = false) => {
    const startedAs = viewerID();
    if (!id) return;
    setChapterAnalyzeLoading(true);
    try {
      const result = await chapterApi.analyze(id, force);
      const sortedSongs = [...result.songs].sort((a, b) => a.start - b.start);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await commentSongToEditableSong(sortedSongs[index], `chapter-${index}`, getDefaultSingerIds(), 'chapter'));
      }

      const merged = mergeDuplicateSongs(songs);
      if (!sameViewer(startedAs)) return;
      setEditableSongs(merged);
      setChapterTimelineSongs(sortedSongs);
      // chapter_count（未取得か / 章節が無いか）が変わりうるので配信を読み直す
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      if (merged.length === 0) {
        showToast('チャプターから曲を取り出せませんでした', 'info');
      } else {
        showToast(`チャプターから${merged.length}曲を読み込みました`, 'success');
      }
    } catch (error) {
      if (!sameViewer(startedAs)) return;
      showToast(analysisFailureMessage('チャプター分析', error), 'error');
      console.error('Chapter analysis failed:', error);
    } finally {
      if (sameViewer(startedAs)) setChapterAnalyzeLoading(false);
    }
  };

  // 提案リストから1曲だけ編集リストへ追加（開始秒順に挿入し、ハイライトしてスクロール）
  // **非同期の完了後に呼ばれることがある**（`addSuggestionSong` は
  // `suggestionToEditableSong` を await する）。待っている間に権限が変われば
  // 編集リストは破棄されているので、そこへ戻すと秘匿の曲名が復活する。
  const addSingleSong = (newSong: EditableSong, startedAs: string | null) => {
    if (!sameViewer(startedAs)) return;
    setEditableSongs((prev) => [...prev, newSong].sort((a, b) => a.start - b.start));
    showToast(`「${newSong.name}」を追加しました`, 'success');
    setTimeout(() => {
      setHighlightedSongId(newSong.id);
      document.getElementById(`song-${newSong.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      setTimeout(() => setHighlightedSongId(null), 3000);
    }, 100);
  };

  // Holodex タブ：1曲追加
  const addSuggestionSong = async (song: SongSuggestion) => {
    const startedAs = viewerID();
    addSingleSong(await suggestionToEditableSong(song, `holodex-add-${Date.now()}`, getDefaultSingerIds()), startedAs);
  };

  // コメントタブ：1曲追加
  const addCommentSongToList = async (song: CommentSong) => {
    const startedAs = viewerID();
    addSingleSong(await commentSongToEditableSong(song, `comment-add-${Date.now()}`, getDefaultSingerIds()), startedAs);
  };

  // チャプタータブ：1曲追加（終了時間の確度が違うので入力元を伝える）
  const addChapterSongToList = async (song: CommentSong) => {
    const startedAs = viewerID();
    addSingleSong(await commentSongToEditableSong(song, `chapter-add-${Date.now()}`, getDefaultSingerIds(), 'chapter'), startedAs);
  };

  // 自動採用に届かなかった候補（0.50〜0.85）を人が確定させる。
  //
  // ここは「アーティストが書かれていないので曲名だけでは決めきれない」
  // 「feat. や CV 名の表記が違う」といった、文字列では原理的に決まらない組が来る。
  // 確定は別表記として学習されるので、同じ表記は次から自動で当たる。
  const addFromRawComment = async ({ start, name, artist }: { start: number; name: string; artist: string }) => {
    const startedAs = viewerID();
    let end = 0;
    let chatEnd: number | undefined;
    try {
      const { ends } = await commentApi.estimateChatEnds(id!, [start]);
      if (ends[String(start)]) {
        end = ends[String(start)];
        chatEnd = end;
      }
    } catch {
      /* 推定失敗時は end 未設定のまま（手動で設定） */
    }
    addSingleSong({
      id: `raw-add-${Date.now()}`,
      name: name || '(曲名未入力)',
      nameReading: '',
      artist,
      artistReading: '',
      start,
      end,
      tags: [],
      singerIds: getDefaultSingerIds(),
      matchedSongId: null,
      artUrl: null,
      itunesId: null,
      trackDuration: null,
      originalName: name,
      originalArtist: artist,
      isEndTimeEstimated: false,
      chatEnd,
      endSource: chatEnd !== undefined ? 'chat' : undefined,
      customTags: [],
    }, startedAs);
  };

  // 自動読み込み：Holodex → コメント の優先順。どちらも正規化＋chat 比較込み
  const autoLoad = async () => {
    if (holodexTimelineSongs.length > 0) {
      await loadFromHolodex(false);
    } else if (stream?.has_comment_raw) {
      await loadFromComments(false);
    } else if (stream?.chapter_count !== 0) {
      // チャプターは最後の受け皿。表記は配信者が書いたものなので信用できるが、
      // 区切りは「その曲の場面」であって歌唱そのものではない（曲のあとの MC を含む）。
      // 章節が無いと分かっている配信（0）だけを除く ── 未取得（-1）は試す価値がある。
      await loadFromChapters(false);
    } else {
      showToast('読み込めるデータがありません（Holodex から同期するか、コメントを取得してください）', 'info');
    }
  };
  return { getDefaultSingerIds, suggestionToEditableSong, commentSongToEditableSong, loadFromHolodex, loadFromComments, loadFromChapters, addSingleSong, addSuggestionSong, addCommentSongToList, addChapterSongToList, addFromRawComment, autoLoad };
}
