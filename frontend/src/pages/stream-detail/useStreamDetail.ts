import { useState, useEffect, useRef, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { streamApi, performanceApi, aiApi, itunesApi, holodexApi, commentApi, tagApi } from '../../api/client';
import type { Channel, Performance, CreatePerformanceItem, AINormalizationItem, UpdateStreamRequest, CommentSong, SongSuggestion } from '../../api/types';
import { useToast } from '../../components/ui/ToastContext';
import { useAuthStore, hasPermission, PERM } from '../../store/auth';
import { onViewerChange, sameViewer, viewerID } from '../../queryClient';
import { usePlayerStore, type PlayerTrack } from '../../store/player';
import type { NoticeKind } from '../../components/UnplayableNotice';
import type { YouTubePlayerInstance } from '../../types/youtube';
import { extractRawCommentTimestamps } from '../../utils/rawCommentTimestamps';
import type { EditableSong } from './types';
import { mergeDuplicateSongs } from './utils';

import { createStreamSourceActions } from './createStreamSourceActions';
import { createStreamSetlistActions } from './createStreamSetlistActions';

// 呼び出し元のページと同じ React fiber に state と購読を置く。
export function useStreamDetail() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);
  const authStatus = useAuthStore((s) => s.status);

  const [isEditing, setIsEditing] = useState(false);
  // 再生時に実際に失敗したエラーコード。保存済みの判定より新しい事実なので優先する。
  //
  // **配信 ID を一緒に持つこと。** `/streams/:id` は同じルートなので、別の配信へ
  // 移っても このコンポーネントは作り直されず state が残る。コードだけを持つと、
  // 会限で 150 を受けたあと公開配信へ移動したときにそちらも案内になり、
  // プレイヤーを描かないので戻る道も無い。前のプレイヤーから遅れて届く
  // イベントを取り違えないためにも要る。
  const [playbackError, setPlaybackError] = useState<{ videoId: string; code: number } | null>(null);
  // YoutubePlayer の effect 依存に入る。id が変わるときはプレイヤー自体も作り直すので、
  // ここが変わって困ることは無い（毎レンダー変わるのは困る）。
  const handlePlaybackError = useCallback(
    (code: number) => {
      if (!id) return;
      setPlaybackError({ videoId: id, code });
    },
    [id],
  );
  // 編集モード左上のタブ（操作 / Holodex / コメント / 生コメント）
  const [editTab, setEditTab] = useState<'actions' | 'holodex' | 'comment' | 'chapter' | 'import' | 'raw'>('actions');
  // 閲覧モードのクイック編集 UI（タグ選択・参加チャンネル追加）の開閉
  const [tagPickerOpen, setTagPickerOpen] = useState(false);
  const [participantAddOpen, setParticipantAddOpen] = useState(false);
  const [editableSongs, setEditableSongs] = useState<EditableSong[]>([]);
  // 単曲編集フロー：現在フォーカス中の曲。選択曲だけ詳細カードを展開し、他は圧縮行にする
  const [selectedSongIndex, setSelectedSongIndex] = useState<number | null>(null);

  // 曲リストの読み込み/増減時に選択を境界内へ補正（空なら選択解除、未選択なら先頭）
  useEffect(() => {
    if (editableSongs.length === 0) {
      setSelectedSongIndex(null);
    } else if (selectedSongIndex === null || selectedSongIndex >= editableSongs.length) {
      setSelectedSongIndex(0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editableSongs.length]);

  // 編集モード中はグローバルプレイヤーを一時停止（ページ内プレイヤーとの二重再生を防ぐ）
  useEffect(() => {
    if (isEditing) usePlayerStore.getState().setPlaying(false);
  }, [isEditing]);

  // **利用者が変わったら、キャッシュの外へコピーしたものを全部捨てる。**
  //
  // 歌唱は `editableSongs` や `vocalistPopupSingers` へ**コピー**されるので、
  // そこから先は query cache と無関係になる ── `resetQueries()` は
  // キャッシュを取り直すだけで、既にコピーされた曲名・歌手・時刻には届かない。
  //
  // 登録は `onViewerChange` に寄せる。**個別に利用者 ID を見張る形は漏れる**
  // ── ログアウトだけ見ていてログイン（利用者の切り替え）を落とした、
  // というのを実際にやった。
  useEffect(
    () =>
      onViewerChange(() => {
        setIsEditing(false);
        setEditableSongs([]);
        setVocalistPopupSingers(null);
      }),
    [],
  );

  const confirmedCount = editableSongs.filter((s) => s.confirmed).length;
  const [holodexTimelineSongs, setHolodexTimelineSongs] = useState<SongSuggestion[]>([]);
  const [commentTimelineSongs, setCommentTimelineSongs] = useState<CommentSong[]>([]);
  const [chapterTimelineSongs, setChapterTimelineSongs] = useState<CommentSong[]>([]);
  const [channelOwner, setChannelOwner] = useState<Channel | null>(null);
  const [participants, setParticipants] = useState<Channel[]>([]);
  const [highlightedSongId, setHighlightedSongId] = useState<string | null>(null);

  const fetchTrackDurationByItunesId = async (itunesId: number): Promise<number | null> => {
    try {
      const result = await itunesApi.queryById(itunesId);
      if (result && result.track_time_millis) {
        return Math.round(result.track_time_millis / 1000);
      }
    } catch (error) {
      console.error('Failed to fetch iTunes duration:', error);
    }
    return null;
  };
  const [vocalistPopupSingers, setVocalistPopupSingers] = useState<Channel[] | null>(null);

  // **権限を query key に入れる。** 応答の中身が権限で変わる（解析結果と処理状態は
  // content:edit のときだけ載る）ので、同じ鍵で共有すると片方が古いまま残る。
  // staleTime: 0 でも足りない ── 鍵も enabled も同じまま認証状態だけ変わっても
  // 即座には引き直さないので、保存済みトークンでハードリロードすると
  // 「処理済みなのにチェックが外れて見える」（is_processed が応答に無いため）。
  // auth が loading のあいだ待つのは、その無駄な匿名リクエストを省くため。
  const { data: stream, isLoading } = useQuery({
    queryKey: ['stream', id, canEdit],
    queryFn: () => streamApi.get(id!),
    enabled: !!id && authStatus !== 'loading',
    staleTime: 0, // ページに入るたびに再読み込みを保証
  });

  // 編集画面の raw comment タイムラインと生コメントタブで同じキャッシュを共有する。
  //
  // 取得の条件は「編集中」ではなく「編集権限がある」。閲覧中でも入力元の時間帯を
  // 見たいことがあり、プレイヤーを 16:9 に固定した分の余白がちょうどそこに使える。
  // 端点自体が content:edit なので、権限が無ければ呼ばない（呼んでも 401）。
  const { data: rawCommentsData } = useQuery({
    queryKey: ['raw-comments', id],
    queryFn: () => commentApi.getComments(id!),
    enabled: !!id && canEdit,
    staleTime: Infinity,
  });

  const { data: streamTagsData = [] } = useQuery({
    queryKey: ['stream-tags'],
    queryFn: tagApi.listStreamTags,
  });
  const STREAM_TAGS = streamTagsData.map((t) => ({ id: t.id, label: t.display_name, color: t.color }));

  const { data: perfTagsData = [] } = useQuery({
    queryKey: ['performance-tags'],
    queryFn: tagApi.listPerformanceTags,
  });
  const PERFORMANCE_TAGS = perfTagsData.map((t) => ({ id: t.id, label: t.display_name, color: t.color }));

  // stream データ読み込み後に、チャンネルオーナーとタイムラインを設定
  useEffect(() => {
    setChannelOwner(stream?.channel_owner || null);
    // 保存されたタイムラインデータを読み込み（ない場合はクリア）
    setHolodexTimelineSongs(stream?.holodex_timeline_songs || []);
    setCommentTimelineSongs(stream?.comment_timeline_songs || []);
    setChapterTimelineSongs(stream?.chapter_timeline_songs || []);
  }, [stream]);

  // 編集モード時はページ全体のスクロールを避ける（ブロック内スクロールに変更）
  useEffect(() => {
    if (!isEditing) return;
    const prevBodyOverflow = document.body.style.overflow;
    const prevHtmlOverflow = document.documentElement.style.overflow;
    document.body.style.overflow = 'hidden';
    document.documentElement.style.overflow = 'hidden';

    return () => {
      document.body.style.overflow = prevBodyOverflow;
      document.documentElement.style.overflow = prevHtmlOverflow;
    };
  }, [isEditing]);

  // Stream 情報を更新
  const updateStreamMutation = useMutation({
    mutationFn: (req: UpdateStreamRequest) => streamApi.update(id!, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      // 裁定・タグ（members_only）・参加チャンネルはどれも見直し一覧（issue #26）の材料
      queryClient.invalidateQueries({ queryKey: ['restriction-review'] });
    },
    onError: (err: Error) => {
      showToast(`更新エラー: ${err.message}`, 'error');
    },
  });

  // 確認して直接パフォーマンス記録を作成
  const createPerformancesMutation = useMutation({
    mutationFn: (performances: CreatePerformanceItem[]) =>
      performanceApi.create(id!, { performances }),
    onSuccess: (data) => {
      showToast(`${data.created_count}曲のセットリストを登録しました`, 'success');
      setIsEditing(false);
      setEditableSongs([]);
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
    },
    onError: (err: Error) => {
      showToast(`登録エラー: ${err.message}`, 'error');
    },
  });

  // AI 正規化
  const aiNormalizeMutation = useMutation({
    // **開始時の視界を持ち回る。** onSuccess は古いレンダーの editableSongs を
    // コピーするので、待っている間に権限が変わると破棄した秘匿曲が戻る。
    mutationFn: async (items: AINormalizationItem[]) => ({
      startedAs: viewerID(),
      data: await aiApi.normalize({ items }),
    }),
    onSuccess: async ({ startedAs, data }) => {
      if (!sameViewer(startedAs)) return;
      // AI 結果を反映
      const updated: EditableSong[] = [...editableSongs];

      // すべての候補をまとめて処理する（曲ごとの API 呼び出しは不要）
      for (const suggestion of data.suggestions) {
        if (suggestion.index >= updated.length) continue;
        const current = updated[suggestion.index];

        // DB に既存楽曲がある場合はその情報を使用し、なければ AI の結果を使用する
        const hasMatch = !!suggestion.matched_song_id;
        const finalName = hasMatch && suggestion.matched_song_name
          ? suggestion.matched_song_name : suggestion.normalized_name;
        const finalNameReading = hasMatch && suggestion.matched_song_name_reading != null
          ? suggestion.matched_song_name_reading : suggestion.normalized_name_reading;
        const finalArtist = hasMatch && suggestion.matched_song_artist
          ? suggestion.matched_song_artist : suggestion.original_artist;
        const finalArtistReading = hasMatch && suggestion.matched_song_artist_reading != null
          ? suggestion.matched_song_artist_reading : suggestion.original_artist_reading;
        const artUrl = (hasMatch ? suggestion.matched_song_art_url : null) || current.artUrl;
        const itunesId = suggestion.matched_song_itunes_id || current.itunesId || null;

        // iTunes ID が取得できた場合、トラック長を取得
        let trackDuration = current.trackDuration;
        if (itunesId && itunesId !== current.itunesId) {
          trackDuration = await fetchTrackDurationByItunesId(itunesId);
        }

        const nameChanged = current.name !== finalName;
        const artistChanged = current.artist !== finalArtist;

        updated[suggestion.index] = {
          ...current,
          name: finalName,
          nameReading: finalNameReading,
          artist: finalArtist,
          artistReading: finalArtistReading,
          tags: suggestion.tags,
          matchedSongId: suggestion.matched_song_id || null,
          artUrl,
          itunesId,
          trackDuration,
          // 正規化前の値を保持する
          aiNormalizedName: nameChanged ? current.name : undefined,
          aiNormalizedArtist: artistChanged ? current.artist : undefined,
          // 以降の変更を追跡できるよう元の値を更新する
          originalName: finalName,
          originalArtist: finalArtist,
        };
      }

      // 正規化後の名前が同じ重複楽曲を統合する
      const merged = mergeDuplicateSongs(updated);
      const mergedCount = updated.length - merged.length;
      // **ループ内で iTunes を取りに行くので、書く直前にもう一度確かめる。**
      // 入口の照合だけでは、その await の間に権限が変わった場合を取り逃す。
      if (!sameViewer(startedAs)) return;
      setEditableSongs(merged);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      if (data.warning) {
        showToast(data.warning + mergeMsg, 'error');
      } else {
        showToast(`${data.suggestions.length}曲のAI正規化が完了しました${mergeMsg}`, 'success');
      }
    },
    onError: (err: Error) => {
      showToast(`AI正規化エラー: ${err.message}`, 'error');
    },
  });

  // 動画を 1 件同期する
  const syncVideoMutation = useMutation({
    mutationFn: () => holodexApi.syncVideo(id!),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      queryClient.invalidateQueries({ queryKey: ['restriction-review'] });
      showToast(
        `同期完了: ${data.synced_count > 0 ? `${data.synced_count}件更新` : '変更なし'}`,
        'success'
      );
    },
    onError: (err: Error) => {
      showToast(`同期エラー: ${err.message}`, 'error');
    },
  });

  // YouTube Data API から公開コメントを明示的に取り直す（Holodex fallback なし）
  const syncYouTubeCommentsMutation = useMutation({
    mutationFn: () => commentApi.syncYouTube(id!),
    onSuccess: async (data) => {
      // raw が変わると backend 側で旧 comment_songs cache は破棄される。
      setCommentTimelineSongs([]);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['raw-comments', id] }),
        queryClient.invalidateQueries({ queryKey: ['stream', id] }),
      ]);
      showToast(`YouTubeからコメント${data.comment_count}件を同期しました`, 'success');
    },
    onError: (err: Error) => {
      showToast(`YouTubeコメント同期エラー: ${err.message}`, 'error');
    },
  });

  // seTORI のデータを Holodex へ同期する
  const syncToHolodexMutation = useMutation({
    mutationFn: () => holodexApi.syncSetoriToHolodex(id!),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      showToast(
        data.message || `Holodex に同期完了: ${data.synced_count > 0 ? `${data.synced_count}件` : '完了'}`,
        'success'
      );
    },
    onError: (err: Error) => {
      showToast(`Holodex 同期エラー: ${err.message}`, 'error');
    },
  });

  const [holodexAnalyzeLoading, setHolodexAnalyzeLoading] = useState(false);

  const [commentAnalyzeLoading, setCommentAnalyzeLoading] = useState(false);

  const [chapterAnalyzeLoading, setChapterAnalyzeLoading] = useState(false);

  // live chat の拍手から end だけを取り直す（AI は呼ばない）。
  // 一括プレ分析はキャッシュ命中だと拍手 end を飛ばすので、後から埋めるのはこの経路。
  const chatEndMutation = useMutation({
    mutationFn: () => commentApi.analyzeChatEnds(id!),
    onSuccess: (res) => {
      // comment_songs が書き換わっているので、タイムラインの元データを読み直す
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      if (res.total === 0) {
        showToast('分析済みの曲がありません（先に「全部読み込む」を実行してください）', 'info');
      } else if (res.changed === 0) {
        showToast('拍手からは新しい終了時間を取得できませんでした', 'info');
      } else if (res.filled === 0) {
        // 全曲すでに終了時間があった場合。値は変えず、拍手の位置だけ記録している
        showToast(`${res.changed}曲で拍手の位置を記録しました（既存の終了時間は変更なし）`, 'success');
      } else {
        showToast(`${res.filled}/${res.total}曲の終了時間を拍手から取得しました`, 'success');
      }
    },
    onError: (err: Error) => showToast(`拍手解析に失敗しました: ${err.message}`, 'error'),
  });

  // YouTube プレイヤーのインスタンス（条件分岐より前に必ず初期化する）
  const playerInstanceRef = useRef<YouTubePlayerInstance | null>(null);

  const {
    loadFromHolodex,
    loadFromComments,
    loadFromChapters,
    addSuggestionSong,
    addCommentSongToList,
    addChapterSongToList,
    addFromRawComment,
    autoLoad,
  } = createStreamSourceActions({
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
  });
  const {
    selectSong,
    confirmAndNext,
    toggleEditing,
    runAINormalization,
    applyEndSource,
    handleSelectExistingSong,
    clearItunesId,
    handleTimeChange,
    toggleTag,
    removeSong,
    addSong,
    scrollToEditableSong,
    handleConfirm,
  } = createStreamSetlistActions({
    setSelectedSongIndex,
    editableSongs,
    setEditableSongs,
    isEditing,
    setIsEditing,
    stream,
    setParticipants,
    aiNormalizeMutation,
    fetchTrackDurationByItunesId,
    channelOwner,
    setHighlightedSongId,
    id,
    showToast,
    queryClient,
    createPerformancesMutation,
  });

  if (isLoading) {
    return { status: 'loading' } as const;
  }

  if (!stream) {
    return { status: 'missing' } as const;
  }

  const youtubeUrl = `https://www.youtube.com/watch?v=${stream.id}`;

  // 歌唱 1 件を再生トラックへ。**表とモバイルの行で同じものを使う**
  // （片方だけ欄が欠けると、キュー追加と報告で持ち物が変わる）
  const toRowTrack = (perf: Performance): PlayerTrack => ({
    performanceId: perf.id,
    streamId: perf.stream_id,
    songId: perf.song_id,
    songName: perf.song_name,
    artist: perf.original_artist,
    artists: perf.artists ?? [],
    artUrl: perf.arts,
    singers: perf.singers?.map((singer) => ({ id: singer.id, name: singer.name })) ?? [],
    streamTitle: stream.title,
    streamDate: stream.stream_date,
    start: perf.start_seconds,
    end: perf.end_seconds,
  });

  // 表示する案内の種類。保存済みの判定と、実際に再生できなかった事実の両方から決める。
  //
  // **実測を優先する。** 保存済みの判定は古くなるし（アーカイブが後から会限化する、
  // 権利で降ろされる）、`public` は「反証が無かった」という弱い結論でしかない
  // （docs/STREAM_VISIBILITY.md）。エラーコード 100 は動画が無い、101/150 は
  // 埋め込み不可（会限もここ。コードだけでは会限か埋め込み無効かを区別できない）。
  // 2 や 5 のような一時的・環境依存のコードでは案内へ切り替えない。
  // 今開いている配信で起きた失敗だけを見る（別の配信のものは無視する）。
  const playbackErrorCode = playbackError?.videoId === stream.id ? playbackError.code : null;
  const noticeKind: NoticeKind | null =
    playbackErrorCode === 100
      ? 'unavailable'
      : playbackErrorCode === 101 || playbackErrorCode === 150
        ? 'playback_failed'
        // **先に案内へ倒してよいのは会限だけ。** 他の判定（`unavailable` /
        // `embed_disabled`）は東京の VPS から見た結果でしかなく、所在地によっては
        // 再生できる。先に倒すとプレイヤーを描かないので `onError` も鳴らず、
        // 取り返せない（`UnplayableNotice` の NoticeKind を参照）。
        // **会限だけは先に案内へ倒す**（所在地に依らず 150 で落ちるため）。
        // 判定は `members_only` タグ ── 以前は yt-dlp の availability を見ていたが、
        // 実測で本番の会限 86 本のうち availability が捉えたのは 7 本だけで、
        // **タグが捉えていない会限は 1 本も無かった**。取得のために yt-dlp を
        // 焚く価値が無いので 2026-09-14 に外した。
        : stream.tags?.some((t) => t.id === 'members_only')
          ? 'members_only'
          : null;

  const setoriTimeline = stream.performances.map((perf) => {
    const end = perf.end_seconds > 0 ? perf.end_seconds : perf.start_seconds;
    return {
      id: perf.id,
      start: perf.start_seconds,
      end,
      label: perf.song_name,
      artist: perf.original_artist,
    };
  });

  // タイムラインは分析後の state ではなく、stream に保存された Holodex 原文を使う。
  const holodexTimeline = (stream.holodex_timeline_songs || []).map((song, index) => {
    const end = song.end_seconds > 0 ? song.end_seconds : song.start_seconds;
    return {
      id: `holodex-${index}`,
      start: song.start_seconds,
      end,
      label: song.name,
      artist: song.original_artist || '',
    };
  });

  const rawCommentTimeline = canEdit
    ? extractRawCommentTimestamps(rawCommentsData?.comments || [])
    : [];

  const timelineDuration = Math.max(
    stream.duration_seconds || 0,
    ...setoriTimeline.map((s) => s.end),
    ...(canEdit ? holodexTimeline.map((s) => s.end) : []),
    ...rawCommentTimeline.map((s) => s.start),
    1,
  );

  const getTimelineLeft = (start: number) => (start / timelineDuration) * 100;
  const getTimelineWidth = (start: number, end: number) =>
    Math.max(((end - start) / timelineDuration) * 100, 0.4);
  const getTooltipAlignClass = (startSeconds: number) => {
    const leftPercent = getTimelineLeft(startSeconds);
    if (leftPercent < 15) return 'left-0';
    if (leftPercent > 85) return 'right-0';
    return 'left-1/2 -translate-x-1/2';
  };

  // セットリスト→再生キュー用トラック（連続再生・キュー追加で共用）
  const performanceTracks = () =>
    stream.performances.map((perf) => ({
      performanceId: perf.id,
      streamId: perf.stream_id,
      songId: perf.song_id,
      songName: perf.song_name,
      artist: perf.original_artist,
      artists: perf.artists ?? [],
      artUrl: perf.arts,
      singers: perf.singers?.map((s) => ({ id: s.id, name: s.name })) ?? [],
      streamTitle: stream.title,
      streamDate: stream.stream_date,
      start: perf.start_seconds,
      end: perf.end_seconds,
    }));

  // 閲覧モードのクイック編集：タグ/参加チャンネル/非表示を編集モードを開かずに保存
  const quickSaveStream = async (patch: UpdateStreamRequest) => {
    try {
      await updateStreamMutation.mutateAsync(patch);
      showToast('更新しました', 'success');
    } catch {
      /* onError 側でトースト表示済み */
    }
  };
  return {
    status: 'ready',
    setEditTab,
    editTab,
    autoLoad,
    holodexAnalyzeLoading,
    commentAnalyzeLoading,
    chapterAnalyzeLoading,
    syncYouTubeCommentsMutation,
    loadFromHolodex,
    holodexTimelineSongs,
    loadFromComments,
    stream,
    runAINormalization,
    editableSongs,
    aiNormalizeMutation,
    syncVideoMutation,
    syncToHolodexMutation,
    addSuggestionSong,
    chatEndMutation,
    commentTimelineSongs,
    PERFORMANCE_TAGS,
    addCommentSongToList,
    loadFromChapters,
    chapterTimelineSongs,
    addChapterSongToList,
    addFromRawComment,
    youtubeUrl,
    canEdit,
    setTagPickerOpen,
    tagPickerOpen,
    STREAM_TAGS,
    quickSaveStream,
    participantAddOpen,
    setParticipantAddOpen,
    noticeKind,
    playerInstanceRef,
    handlePlaybackError,
    setoriTimeline,
    isEditing,
    scrollToEditableSong,
    getTimelineLeft,
    getTimelineWidth,
    getTooltipAlignClass,
    holodexTimeline,
    rawCommentTimeline,
    toggleEditing,
    performanceTracks,
    showToast,
    confirmedCount,
    handleConfirm,
    createPerformancesMutation,
    updateStreamMutation,
    selectedSongIndex,
    selectSong,
    highlightedSongId,
    removeSong,
    setEditableSongs,
    handleSelectExistingSong,
    handleTimeChange,
    toggleTag,
    applyEndSource,
    clearItunesId,
    participants,
    channelOwner,
    setParticipants,
    confirmAndNext,
    addSong,
    toRowTrack,
    setVocalistPopupSingers,
    vocalistPopupSingers,
  } as const;
}

export type StreamDetailModel = Extract<ReturnType<typeof useStreamDetail>, { status: 'ready' }>;
