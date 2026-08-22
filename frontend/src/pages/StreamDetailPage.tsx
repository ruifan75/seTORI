import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { streamApi, performanceApi, aiApi, itunesApi, holodexApi, commentApi, chapterApi, tagApi, artistApi } from '../api/client';
import SingerSearchInput from '../components/SingerSearchInput';
import PerformanceFields from '../components/PerformanceFields';
import { formatTimeInput, parseTime } from '../utils/timeFormat';
import type { Singer, Performance, CreatePerformanceItem, AINormalizationItem, Song, UpdateStreamRequest, CommentSong, SongSuggestion, EndSource, FieldChange, ArtistAliasProposal } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/ToastContext';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { usePlayerStore, type PlayerTrack } from '../store/player';
import YoutubePlayer from '../components/YoutubePlayer';
import { playerSeekTo } from '../components/youtubePlayerControl';
import type { YouTubePlayerInstance } from '../types/youtube';
import QueueAddButton from '../components/QueueAddButton';
import PerformanceListRow, { RowSingerAvatars } from '../components/PerformanceListRow';
import ReportButton from '../components/ReportButton';
import RawCommentsPanel from '../components/RawCommentsPanel';
import SourceSongList from '../components/SourceSongList';
import ArtistLinks from '../components/ArtistLinks';
import { extractRawCommentTimestamps } from '../utils/rawCommentTimestamps';


// 編集可能な配信情報
interface EditableSong {
  id: string;
  name: string;
  nameReading: string;
  artist: string;
  artistReading: string;
  start: number;
  end: number;
  tags: string[];
  singerIds: string[];
  // 追加フィールド
  matchedSongId: string | null; // マッチした楽曲ID、null は新規作成を示す
  artUrl: string | null; // アートワークURL
  itunesId: number | null; // Holodex が提供する iTunes ID
  // itunesId が既に DB（song_itunes）に紐付いているか。false は保存時に新規紐付けが作られることを示す
  // （primary な iTunes ID は Holodex へのアップロードにも使われるため、誤りは外部に伝播する）
  itunesFromDb?: boolean;
  trackDuration: number | null; // iTunes の曲の長さ（秒）
  originalName: string; // 元の名称を追跡（変更判定用）
  originalArtist: string; // 元のアーティストを追跡
  // AI 正規化追跡
  aiNormalizedName?: string; // AI 変更前の名称（変更された場合）
  aiNormalizedArtist?: string; // AI 変更前のアーティスト（変更された場合）
  // AI が照合したときの「この 2 つは同じ人か」の申し送りと、人のチェック状態。
  // 別名義はその人の全楽曲に効くので、**保存したときにだけ**登録する。
  artistAlias?: ArtistAliasProposal;
  aliasChecked?: boolean;
  // 「抽出したままの値が、どの処理でどう変わったか」。AI 正規化と DB 照合を区別して出す。
  // aiNormalized* は 1 段しか表せず、どちらの仕業かも分からないので、表示はこちらを使う。
  changes?: FieldChange[];
  // 時間推定マーク
  isEndTimeEstimated?: boolean; // 終了時間が推定値かどうか
  // Chat の拍手検出による参考値（コメントに明記された終了時刻との差が大きい場合に警告）
  chatEnd?: number;
  endDiff?: number;
  // 由来の追跡と復元
  originalCommentEnd?: number; // コメント分析で明記されていた元の終了時刻（復元用）
  endSource?: EndSource;
  // マージ追跡
  mergedFrom?: string[]; // AI 正規化後にマージされた元の曲名
  // 自由文本タグ
  customTags: string[];
  // 単曲編集フロー：確認済みフラグ（ローカルのみ、保存は最後に一括）
  confirmed?: boolean;
}

// AI 正規化後に重複楽曲をマージ
// 条件：name が完全一致 + artist が完全一致 + start の時間差 ≤ 30s
function mergeDuplicateSongs(songs: EditableSong[]): EditableSong[] {
  const result: EditableSong[] = [];

  for (const song of songs) {
    let merged = false;
    for (let i = 0; i < result.length; i++) {
      const existing = result[i];
      if (
        existing.name === song.name &&
        existing.artist === song.artist &&
        Math.abs(existing.start - song.start) <= 30
      ) {
        // Merge: 情報がより完全な方を保持
        const hasRealEnd = (s: EditableSong) => s.end > 0 && !s.isEndTimeEstimated;
        const preferSong = hasRealEnd(song) && !hasRealEnd(existing);

        // 吸収された側の元の名称を記録（AI 変更前の名称）
        const absorbedName = preferSong
          ? (existing.aiNormalizedName || existing.originalName)
          : (song.aiNormalizedName || song.originalName);
        const prevMerged = existing.mergedFrom || [];

        if (preferSong) {
          result[i] = {
            ...song,
            tags: [...new Set([...song.tags, ...existing.tags])],
            matchedSongId: song.matchedSongId || existing.matchedSongId,
            artUrl: song.artUrl || existing.artUrl,
            itunesId: song.itunesId ?? existing.itunesId,
            itunesFromDb: song.itunesId != null ? song.itunesFromDb : existing.itunesFromDb,
            trackDuration: song.trackDuration ?? existing.trackDuration,
            nameReading: song.nameReading || existing.nameReading,
            artistReading: song.artistReading || existing.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        } else {
          result[i] = {
            ...existing,
            tags: [...new Set([...existing.tags, ...song.tags])],
            matchedSongId: existing.matchedSongId || song.matchedSongId,
            artUrl: existing.artUrl || song.artUrl,
            itunesId: existing.itunesId ?? song.itunesId,
            itunesFromDb: existing.itunesId != null ? existing.itunesFromDb : song.itunesFromDb,
            trackDuration: existing.trackDuration ?? song.trackDuration,
            nameReading: existing.nameReading || song.nameReading,
            artistReading: existing.artistReading || song.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        }
        merged = true;
        break;
      }
    }
    if (!merged) {
      result.push(song);
    }
  }

  return result;
}



/**
 * endSourceForSourceSong は入力元から読み込んだ曲の終了時間の由来を決める。
 * バックエンドの endSourceForComment / endSourceForChapter と同じ規則にしてある
 * ── 経路によって違う値が入ると、確度で絞り込む問い合わせが当てにならなくなる。
 */
function endSourceForSourceSong(song: CommentSong, source: 'comment' | 'chapter'): EndSource | undefined {
  if (song.end <= 0) return undefined;
  if (song.chat_end && song.chat_end === song.end) return 'chat';
  // チャプターの end は次の章節の開始なので、拍手で埋まっていない限り推定値でしかない
  if (source === 'chapter') return 'next_start';
  return song.is_end_time_estimated ? undefined : 'comment';
}

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;

  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}



// 長さをフォーマットし "+MM:SS" または "+H:MM:SS" で表示

export default function StreamDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const [isEditing, setIsEditing] = useState(false);
  // 編集モード左上のタブ（操作 / Holodex / コメント / 生コメント）
  const [editTab, setEditTab] = useState<'actions' | 'holodex' | 'comment' | 'chapter' | 'raw'>('actions');
  // 閲覧モードのクイック編集 UI（タグ選択・参加者追加）の開閉
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

  // 曲を選択してプレイヤーをその開始位置へ（Holodex の編集フローと同じ）
  const selectSong = (index: number, seek = true) => {
    setSelectedSongIndex(index);
    const song = editableSongs[index];
    if (seek && song && song.start >= 0) {
      playerSeekTo('page', song.start);
    }
  };

  // 確認して次へ：この曲を確認済みにし、次の未確認曲へフォーカス移動
  const confirmAndNext = (index: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      updated[index] = { ...updated[index], confirmed: true };
      return updated;
    });
    // index の後ろ→先頭の順で次の未確認曲を探す（自分は除く）
    const n = editableSongs.length;
    for (let step = 1; step < n; step++) {
      const i = (index + step) % n;
      if (!editableSongs[i].confirmed) {
        selectSong(i);
        return;
      }
    }
  };

  const confirmedCount = editableSongs.filter((s) => s.confirmed).length;
  const [holodexTimelineSongs, setHolodexTimelineSongs] = useState<SongSuggestion[]>([]);
  const [commentTimelineSongs, setCommentTimelineSongs] = useState<CommentSong[]>([]);
  const [chapterTimelineSongs, setChapterTimelineSongs] = useState<CommentSong[]>([]);
  const [channelOwner, setChannelOwner] = useState<Singer | null>(null);
  const [participants, setParticipants] = useState<Singer[]>([]);
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
  const [vocalistPopupSingers, setVocalistPopupSingers] = useState<Singer[] | null>(null);

  const { data: stream, isLoading } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => streamApi.get(id!),
    enabled: !!id,
    staleTime: 0, // ページに入るたびに再読み込みを保証
  });

  // 編集画面の raw comment タイムラインと生コメントタブで同じキャッシュを共有する。
  const { data: rawCommentsData } = useQuery({
    queryKey: ['raw-comments', id],
    queryFn: () => commentApi.getComments(id!),
    enabled: !!id && isEditing,
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
    mutationFn: (items: AINormalizationItem[]) =>
      aiApi.normalize({ items }),
    onSuccess: async (data) => {
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

  // force=true でキャッシュを無視し AI 再分析（再正規化）。通常はキャッシュ済み結果を即座に読み込む。
  // 編集リストへ入れるときの既定ボーカル（参加者 → チャンネル主）
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
    if (!id) return;
    if (!stream?.holodex_timeline_songs || stream.holodex_timeline_songs.length === 0) {
      showToast('Holodexデータがありません', 'info');
      return;
    }
    setHolodexAnalyzeLoading(true);
    try {
      // 分析（正規化＋DB照合＋拍手end）を実行し、結果をそのまま反映する
      const analyzed = await holodexApi.analyzeSongs(id, force);
      const sortedSongs = [...analyzed].sort((a, b) => a.start_seconds - b.start_seconds);
      setHolodexTimelineSongs(sortedSongs);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await suggestionToEditableSong(sortedSongs[index], `holodex-${index}`, getDefaultSingerIds()));
      }

      const merged = mergeDuplicateSongs(songs);
      const mergedCount = songs.length - merged.length;
      setEditableSongs(merged);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`Holodexから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      showToast('Holodex分析に失敗しました', 'error');
      console.error('Holodex analysis failed:', error);
    } finally {
      setHolodexAnalyzeLoading(false);
    }
  };

  const [commentAnalyzeLoading, setCommentAnalyzeLoading] = useState(false);

  // force=true でキャッシュを無視し AI 再分析（再正規化）。通常はキャッシュ済みの結果を即座に読み込む。
  const loadFromComments = async (force = false) => {
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
      setEditableSongs(merged);
      // 照合の結果（候補・変更履歴）はこの応答にしか無い。配信を開いただけの
      // 読み取りでは照合しないので、タイムライン側もここで差し替える。
      setCommentTimelineSongs(sortedSongs);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`コメントから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      showToast('コメント分析に失敗しました', 'error');
      console.error('Comment analysis failed:', error);
    } finally {
      setCommentAnalyzeLoading(false);
    }
  };

  const [chapterAnalyzeLoading, setChapterAnalyzeLoading] = useState(false);

  // 配信者が付けた目次から読み込む。Holodex にも曲が無く、コメントも取れない配信の受け皿。
  // force=true はチャプターを yt-dlp で取り直してから再分析する（数秒かかる）。
  const loadFromChapters = async (force = false) => {
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
      showToast('チャプター分析に失敗しました', 'error');
      console.error('Chapter analysis failed:', error);
    } finally {
      setChapterAnalyzeLoading(false);
    }
  };

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

  // 提案リストから1曲だけ編集リストへ追加（開始秒順に挿入し、ハイライトしてスクロール）
  const addSingleSong = (newSong: EditableSong) => {
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
    addSingleSong(await suggestionToEditableSong(song, `holodex-add-${Date.now()}`, getDefaultSingerIds()));
  };

  // コメントタブ：1曲追加
  const addCommentSongToList = async (song: CommentSong) => {
    addSingleSong(await commentSongToEditableSong(song, `comment-add-${Date.now()}`, getDefaultSingerIds()));
  };

  // チャプタータブ：1曲追加（終了時間の確度が違うので入力元を伝える）
  const addChapterSongToList = async (song: CommentSong) => {
    addSingleSong(await commentSongToEditableSong(song, `chapter-add-${Date.now()}`, getDefaultSingerIds(), 'chapter'));
  };

  // 自動採用に届かなかった候補（0.50〜0.85）を人が確定させる。
  //
  // ここは「アーティストが書かれていないので曲名だけでは決めきれない」
  // 「feat. や CV 名の表記が違う」といった、文字列では原理的に決まらない組が来る。
  // 確定は別表記として学習されるので、同じ表記は次から自動で当たる。
  const addFromRawComment = async ({ start, name, artist }: { start: number; name: string; artist: string }) => {
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
    });
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

  const toggleEditing = () => {
    if (isEditing) {
      // 編集モードを終了する
      setIsEditing(false);
      setEditableSongs([]);
    } else {
      // 編集モードを開始し、既存のセットリストを自動で読み込む
      if (stream) {
        // 参加者一覧を設定する（歌唱に参加したすべての歌手を含む）
        const allSingers = new Map<string, Singer>();
        
        // 先に stream.participants を追加する
        (stream.participants || []).forEach(p => allSingers.set(p.id, p));
        
        // 続けてすべての performance の歌手を追加する
        if (stream.performances.length > 0) {
          stream.performances.forEach(perf => {
            perf.singers.forEach(singer => {
              allSingers.set(singer.id, singer);
            });
          });
        }
        
        setParticipants(Array.from(allSingers.values()));

        // 既存のセットリストを読み込む
        if (stream.performances.length > 0) {
          const songs: EditableSong[] = stream.performances.map((perf) => ({
            id: perf.id,
            name: perf.song_name,
            nameReading: '',
            artist: perf.original_artist,
            artistReading: '',
            start: perf.start_seconds,
            end: perf.end_seconds,
            tags: perf.tags.map((t) => t.id),
            singerIds: perf.singers.map((s) => s.id),
            matchedSongId: perf.song_id,
            artUrl: perf.arts || null,
            // 既に紐付いている iTunes ID。null にしていた頃は、紐付け済みの曲まで
            // カードが「iTunes なし」と表示していた
            itunesId: perf.itunes_id ?? null,
            itunesFromDb: perf.itunes_id != null,
            trackDuration: null,
            originalName: perf.song_name,
            originalArtist: perf.original_artist,
            // 既存データには AI 変更の印がない
            aiNormalizedName: undefined,
            aiNormalizedArtist: undefined,
            isEndTimeEstimated: false,
            // 保存済みの由来を読み戻す。unknown は「記録開始前」なので
            // 由来なし扱いにして、推測し直さない（推測すると嘘の由来が付く）。
            endSource: perf.end_source && perf.end_source !== 'unknown' ? perf.end_source : undefined,
            customTags: perf.custom_tags || [],
          }));
          setEditableSongs(songs);
        }
      }
      setIsEditing(true);
    }
  };

  const runAINormalization = () => {
    if (editableSongs.length === 0) return;
    const items: AINormalizationItem[] = editableSongs.map((song) => ({
      name: song.name,
      original_artist: song.artist,
      art_url: song.artUrl || undefined,
      itunes_id: song.itunesId || undefined,
    }));
    aiNormalizeMutation.mutate(items);
  };

  const handleSongChange = (index: number, field: keyof EditableSong, value: string | number | string[] | boolean | null) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const song = updated[index];

      // 曲名またはアーティストを変更し、元の行が既存曲と照合済みなら新規曲として扱い直す
      if ((field === 'name' || field === 'artist') && song.matchedSongId) {
        const newName = field === 'name' ? value as string : song.name;
        const newArtist = field === 'artist' ? value as string : song.artist;

        if (newName !== song.originalName || newArtist !== song.originalArtist) {
          updated[index] = {
            ...song,
            [field]: value,
            matchedSongId: null
          };
          return updated;
        }
      }

      updated[index] = { ...song, [field]: value };

      // 終了時刻を手動変更したら、Chat の比較情報を消して由来を記録する
      if (field === 'end') {
        updated[index].chatEnd = undefined;
        updated[index].endDiff = undefined;
        updated[index].endSource = 'manual';
      }

      return updated;
    });
  };

  // 指定した由来の終了時刻を適用する
  const applyEndSource = (index: number, source: 'chat' | 'comment', newEnd?: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const s = updated[index];

      if (source === 'chat' && s.chatEnd !== undefined) {
        updated[index] = {
          ...s,
          end: s.chatEnd,
          endSource: 'chat',
          isEndTimeEstimated: false,
        };
      } else if (source === 'comment' && s.originalCommentEnd !== undefined) {
        updated[index] = {
          ...s,
          end: s.originalCommentEnd,
          endSource: 'comment',
          isEndTimeEstimated: false,
          chatEnd: undefined, // 比較状態を消す
          endDiff: undefined,
        };
      } else if (newEnd !== undefined) {
        updated[index] = { ...s, end: newEnd, endSource: source };
      }
      return updated;
    });
  };

  // 検索結果から楽曲を選ぶ
  const handleSelectExistingSong = async (index: number, song: Song) => {
    const selectedItunesId = song.itunes_ids && song.itunes_ids.length > 0 ? Number(song.itunes_ids[0].itunes_id) : null;
    const selectedTrackDuration = selectedItunesId ? await fetchTrackDurationByItunesId(selectedItunesId) : null;

    setEditableSongs((prev) => {
      const updated = [...prev];
      const current = updated[index];
      // 選んだ曲が iTunes ID を持たない場合は、行に載っている ID（Holodex 由来）を引き継ぐ。
      // これにより保存時に song_itunes へ紐付けが作られ、次回以降は iTunes ID で自動マッチする。
      // mergeDuplicateSongs と同じ ?? 規約。誤って引き継いだ場合は行の iTunes チップから解除できる。
      const itunesId = selectedItunesId ?? current.itunesId;
      // DB 楽曲（song.id あり）の ID は登録済み。純 iTunes 検索結果（id 空）は未登録なので保存時に紐付く
      const itunesFromDb = selectedItunesId != null ? !!song.id : current.itunesFromDb;
      // trackDuration は採用した ID と対応させる（取得失敗時に別 ID の値を流用しない）
      const trackDuration = selectedItunesId != null ? selectedTrackDuration : current.trackDuration;

      // iTunes から選択された項目か確認する（id が空）
      if (!song.id) {
        // iTunes から選択：基本情報と iTunes ID を設定する
        updated[index] = {
          ...updated[index],
          name: song.name,
          artist: song.original_artist,
          artUrl: song.arts || null,
          itunesId,
          itunesFromDb,
          trackDuration,
          matchedSongId: null, // 新規楽曲
          originalName: song.name,
          originalArtist: song.original_artist,
        };
      } else {
        // DB から選択：完全な情報を設定する
        updated[index] = {
          ...updated[index],
          name: song.name,
          nameReading: song.name_reading || '',
          artist: song.original_artist,
          artistReading: song.original_artist_reading || '',
          artUrl: song.arts || null,
          itunesId,
          itunesFromDb,
          trackDuration,
          matchedSongId: song.id,
          originalName: song.name,
          originalArtist: song.original_artist,
        };
      }
      
      return updated;
    });
  };

  // iTunes ID の紐付けを解除（誤った ID が song_itunes に焼き付くのを防ぐ）。
  // trackDuration も対応が崩れるため一緒に消す。
  const clearItunesId = (index: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      updated[index] = {
        ...updated[index],
        itunesId: null,
        itunesFromDb: undefined,
        trackDuration: null,
      };
      return updated;
    });
  };

  const handleTimeChange = (index: number, field: 'start' | 'end', timeStr: string) => {
    // 利用者の自由入力を許し、フォーカスが外れたときだけ解析する
    // 表示値だけを先に更新し、解析は遅延させる
    const seconds = parseTime(timeStr);
    if (!isNaN(seconds)) {
      handleSongChange(index, field, seconds);
    }
  };

  const toggleTag = (index: number, tagId: string) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const currentTags = updated[index].tags;
      if (currentTags.includes(tagId)) {
        updated[index] = { ...updated[index], tags: currentTags.filter((t) => t !== tagId) };
      } else {
        updated[index] = { ...updated[index], tags: [...currentTags, tagId] };
      }
      return updated;
    });
  };

  const removeSong = (index: number) => {
    setEditableSongs((prev) => prev.filter((_, i) => i !== index));
  };

  const addSong = () => {
    const lastSong = editableSongs[editableSongs.length - 1];
    const newStart = lastSong ? lastSong.end || lastSong.start + 240 : 0;
    const defaultSingerIds = channelOwner ? [channelOwner.id] : [];
    setEditableSongs((prev) => [
      ...prev,
      {
        id: `new-${Date.now()}`,
        name: '',
        nameReading: '',
        artist: '',
        artistReading: '',
        start: newStart,
        end: 0,
        tags: [],
        singerIds: defaultSingerIds,
        matchedSongId: null,
        artUrl: null,
        itunesId: null,
        trackDuration: null,
        originalName: '',
        originalArtist: '',
        customTags: [],
      },
    ]);
  };

  // seTORI timeline クリック → 対応する曲を選択して詳細カードを展開＋スクロール（編集モード用）
  const scrollToEditableSong = (start: number) => {
    if (editableSongs.length === 0) return;
    const match = editableSongs.reduce((best, song) =>
      Math.abs(song.start - start) < Math.abs(best.start - start) ? song : best
    );
    if (Math.abs(match.start - start) <= 30) {
      const idx = editableSongs.findIndex((s) => s.id === match.id);
      if (idx >= 0) selectSong(idx, false); // timeline クリックはプレイヤー側が既にシークするため seek しない
      setHighlightedSongId(match.id);
      setTimeout(() => {
        document.getElementById(`song-${match.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }, 50);
      setTimeout(() => setHighlightedSongId(null), 3000);
    }
  };

  const handleConfirm = async () => {
    // 配信情報（タグ・参加者・非表示など）は閲覧モードのクイック編集で即時保存されるため、
    // ここではセットリストのみ保存する。

    // 楽曲がなければ performance をすべて削除する
    if (editableSongs.length === 0) {
      try {
        await performanceApi.deleteAll(id!);
        showToast('セットリストを削除しました', 'success');
        setIsEditing(false);
        setEditableSongs([]);
        queryClient.invalidateQueries({ queryKey: ['stream', id] });
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        showToast(`削除エラー: ${message}`, 'error');
      }
      return;
    }

    // 終了時間のバリデーション
    const missingEndTime = editableSongs.filter(s => s.end === 0);
    if (missingEndTime.length > 0) {
      showToast(`終了時間が未設定の曲が${missingEndTime.length}件あります`, 'error');
      // 最初の未設定曲を選択（詳細カードを展開）してスクロール
      const firstMissing = missingEndTime[0];
      const idx = editableSongs.findIndex((s) => s.id === firstMissing.id);
      if (idx >= 0) selectSong(idx, false);
      setTimeout(() => {
        document.getElementById(`song-${firstMissing.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }, 50);
      return;
    }

    // セットリストを更新する
    const performances: CreatePerformanceItem[] = editableSongs.map((song) => ({
      name: song.name,
      name_reading: song.nameReading,
      original_artist: song.artist,
      original_artist_reading: song.artistReading,
      start_seconds: song.start,
      end_seconds: song.end,
      tags: song.tags,
      singer_ids: song.singerIds,
      art_url: song.artUrl || undefined,
      itunes_id: song.itunesId || undefined,
      custom_tags: song.customTags.length > 0 ? song.customTags : undefined,
      // 終了時間の由来。編集中ずっと追跡している値をそのまま送る
      // （送らないと保存時に失われ、自動生成と人手確認の区別が付かなくなる）。
      // 保存＝人が見たとみなすので end_confirmed は既定の true に任せる。
      end_source: song.endSource,
    }));
    // 別名義は保存と同時に登録する。読み込んだだけでは書かない
    // ── その人の全楽曲に効くので、人が見て残す判断をしたときにだけ入れる。
    // 権限が無ければバックエンドが提案として積む（docs/SETLIST_FLOW.md）。
    void registerCheckedAliases();
    createPerformancesMutation.mutate(performances);
  };

  // チェックの入った別名義を登録する。1 件ずつ独立して扱い、失敗しても保存は止めない
  // （別名義は付随的な情報で、セットリストの保存の方が主目的）。
  const registerCheckedAliases = async () => {
    const seen = new Set<string>();
    const targets = editableSongs
      .filter((s) => s.aliasChecked && s.artistAlias)
      .map((s) => s.artistAlias!)
      .filter((a) => {
        const key = `${a.canonical}\u001f${a.alias}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });

    let applied = 0;
    let proposed = 0;
    for (const a of targets) {
      try {
        const res = await artistApi.proposeAlias(a.canonical, a.alias);
        if (res.applied) {
          applied++;
        } else {
          proposed++;
        }
      } catch (err) {
        showToast(`別名義の登録に失敗しました（${a.alias}）`, 'error');
        console.error('alias registration failed:', err);
      }
    }
    if (applied > 0) showToast(`${applied}件の別名義を登録しました`, 'success');
    if (proposed > 0) showToast(`${proposed}件の別名義を提案として登録しました`, 'info');
  };

  // YouTube プレイヤーのインスタンス（条件分岐より前に必ず初期化する）
  const playerInstanceRef = useRef<YouTubePlayerInstance | null>(null);

  if (isLoading) {
    return <Loading />;
  }

  if (!stream) {
    return (
      <div className="text-center py-12 text-gray-500">
        歌枠が見つかりませんでした
      </div>
    );
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

  const rawCommentTimeline = isEditing
    ? extractRawCommentTimestamps(rawCommentsData?.comments || [])
    : [];

  const timelineDuration = Math.max(
    stream.duration_seconds || 0,
    ...setoriTimeline.map((s) => s.end),
    ...(isEditing ? holodexTimeline.map((s) => s.end) : []),
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

  // 閲覧モードのクイック編集：タグ/参加者/非表示を編集モードを開かずに保存
  const quickSaveStream = async (patch: UpdateStreamRequest) => {
    try {
      await updateStreamMutation.mutateAsync(patch);
      showToast('更新しました', 'success');
    } catch {
      /* onError 側でトースト表示済み */
    }
  };

  return (
    <>
      {/* 1300px 以上：左右のペインが画面内に収まり各自スクロールする。
          それ未満：縦積みなので高さを固定せず、main ごとスクロールさせる
          ── h-full + overflow-hidden のままだと、左カラム（情報＋プレイヤー）が
          縦を食い尽くしてセットリストが数十pxに潰れ、下へ送る手段も無くなる */}
      <div className="flex flex-col min-[1300px]:flex-row gap-6 w-full min-h-0 min-[1300px]:h-full min-[1300px]:overflow-hidden">
      {/* Left Column - Stream Info + YouTube Player */}
      {/* モバイル（<sm）は情報カードとプレイヤーを縦積みにし高さ制限も外す。sm〜1300px は左右並び+40vh 制限 */}
      <div className="w-full min-[1300px]:basis-2/5 min-[1300px]:shrink-0 min-[1300px]:self-stretch flex flex-col sm:flex-row min-[1300px]:grid min-[1300px]:grid-rows-[2fr_3fr] gap-4 min-h-0 min-[1300px]:overflow-hidden shrink-0 max-h-none sm:max-h-[40vh] min-[1300px]:max-h-none">
        {/* Stream Header - 40% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border overflow-y-auto min-h-0">
          {isEditing ? (
            /* 編集モード：情報カードの代わりにデータ読み込みタブ */
            <div className="flex flex-col min-h-0">
              <div className="flex border-b shrink-0 sticky top-0 bg-white z-10">
                {([
                  { key: 'actions', label: '操作' },
                  { key: 'holodex', label: 'Holodex' },
                  { key: 'comment', label: 'コメント' },
                  { key: 'chapter', label: 'チャプター' },
                  { key: 'raw', label: '生コメント' },
                ] as const).map((t) => (
                  <button
                    key={t.key}
                    onClick={() => setEditTab(t.key)}
                    className={`px-3.5 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors ${
                      editTab === t.key
                        ? 'border-indigo-600 text-indigo-600'
                        : 'border-transparent text-gray-500 hover:text-gray-700'
                    }`}
                  >
                    {t.label}
                  </button>
                ))}
              </div>

              <div className="p-4">
                {editTab === 'actions' && (
                  <div className="space-y-4">
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">読み込み</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={autoLoad}
                          disabled={holodexAnalyzeLoading || commentAnalyzeLoading || chapterAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                          title="Holodex → コメント → チャプター の優先順で読み込み、正規化と chat 時間チェックまで実行"
                        >
                          {holodexAnalyzeLoading || commentAnalyzeLoading || chapterAnalyzeLoading ? '読み込み中...' : '自動読み込み'}
                        </button>
                        <button
                          onClick={() => loadFromHolodex(false)}
                          disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {holodexAnalyzeLoading ? 'Holodex分析中...' : 'Holodex データ'}
                        </button>
                        <button
                          onClick={() => loadFromComments(false)}
                          disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {commentAnalyzeLoading ? 'コメント分析中...' : 'コメント データ'}
                        </button>
                        <button
                          onClick={runAINormalization}
                          disabled={editableSongs.length === 0 || aiNormalizeMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {aiNormalizeMutation.isPending ? 'AI処理中...' : 'AI正規化'}
                        </button>
                      </div>
                    </div>
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">コメント同期</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={() => syncYouTubeCommentsMutation.mutate()}
                          disabled={syncYouTubeCommentsMutation.isPending || commentAnalyzeLoading}
                          title="YouTube Data API から公開トップレベルコメントを取得し直します"
                          className="px-3 py-1.5 text-sm bg-red-50 text-red-700 border border-red-200 font-medium rounded-lg hover:bg-red-100 transition-colors disabled:opacity-50"
                        >
                          {syncYouTubeCommentsMutation.isPending ? 'YouTubeから同期中...' : 'YouTubeからコメント同期'}
                        </button>
                      </div>
                    </div>
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">Holodex 同期</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={() => syncVideoMutation.mutate()}
                          disabled={syncVideoMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {syncVideoMutation.isPending ? '同期中...' : 'Holodex から同期'}
                        </button>
                        <button
                          onClick={() => syncToHolodexMutation.mutate()}
                          disabled={syncToHolodexMutation.isPending}
                          title="seTORI のセットリストを Holodex に書き込みます（外部サービスへの反映）"
                          className="px-3 py-1.5 text-sm bg-amber-50 text-amber-700 border border-amber-300 font-medium rounded-lg hover:bg-amber-100 transition-colors disabled:opacity-50"
                        >
                          {syncToHolodexMutation.isPending ? 'Holodex へ同期中...' : 'seTORI から Holodex へ同期'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {editTab === 'holodex' && (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={() => loadFromHolodex(false)}
                        disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                        className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                        title="全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
                      >
                        {holodexAnalyzeLoading ? '分析中...' : '全部読み込む'}
                      </button>
                      <button
                        onClick={() => loadFromHolodex(true)}
                        disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                        title="キャッシュを無視して AI で再分析・再正規化します"
                        className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        再分析
                      </button>
                    </div>
                    {holodexTimelineSongs.length === 0 ? (
                      <p className="text-sm text-gray-400 py-2">Holodex データがありません</p>
                    ) : (
                      <div className="space-y-0.5">
                        {holodexTimelineSongs.map((song, i) => (
                          <div
                            key={i}
                            onClick={() => addSuggestionSong(song)}
                            className="flex items-baseline gap-2 px-2 py-1.5 rounded hover:bg-indigo-50 cursor-pointer group text-sm"
                            title="クリックで追加"
                          >
                            <span className="shrink-0 flex items-baseline gap-0.5">
                              <button
                                onClick={(e) => {
                                  e.stopPropagation();
                                  playerSeekTo('page', song.start_seconds);
                                }}
                                className="px-1.5 rounded bg-blue-50 text-blue-700 font-mono text-xs hover:bg-blue-100 transition-colors"
                                title="開始時間にジャンプ"
                              >
                                {formatTime(song.start_seconds)}
                              </button>
                              {song.end_seconds > 0 && (
                                <>
                                  <span className="text-gray-300 text-xs">〜</span>
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      playerSeekTo('page', song.end_seconds);
                                    }}
                                    className="px-1.5 rounded bg-blue-50/60 text-blue-600 font-mono text-xs hover:bg-blue-100 transition-colors"
                                    title="終了時間にジャンプ"
                                  >
                                    {formatTime(song.end_seconds)}
                                  </button>
                                </>
                              )}
                            </span>
                            <span className="min-w-0 flex-1 truncate">
                              <span className="text-gray-900 font-medium">{song.name}</span>
                              {song.original_artist && <span className="text-gray-500"> / {song.original_artist}</span>}
                            </span>
                            <span className="shrink-0 text-gray-300 group-hover:text-indigo-600 transition-colors">＋</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}

                {editTab === 'comment' && (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={() => loadFromComments(false)}
                        disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                        className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                        title="分析済みの全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
                      >
                        {commentAnalyzeLoading ? '分析中...' : '全部読み込む'}
                      </button>
                      <button
                        onClick={() => loadFromComments(true)}
                        disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                        title="キャッシュを無視して AI で再分析・再正規化します"
                        className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        再分析
                      </button>
                      <button
                        onClick={() => chatEndMutation.mutate()}
                        disabled={chatEndMutation.isPending || commentAnalyzeLoading}
                        title="live chat の拍手から終了時間だけを取り直します（AI は使いません。live chat のダウンロードで数十秒かかることがあります）"
                        className="px-2 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        {chatEndMutation.isPending ? (
                          <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                          </svg>
                        ) : (
                          /* 拍手＝手のアイコン */
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={1.8} viewBox="0 0 24 24">
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M7 11V6a1.5 1.5 0 013 0v4m0-4.5a1.5 1.5 0 013 0V10m0-3.5a1.5 1.5 0 013 0V13m0-2.5a1.5 1.5 0 013 0V15a6 6 0 01-6 6h-2a6 6 0 01-5.2-3L4 14.5a1.5 1.5 0 012.6-1.5L7 14"
                            />
                          </svg>
                        )}
                      </button>
                    </div>
                    {/* 最後に解析した時刻。プロンプトや抽出規則を変えたあと、
                        この配信がまだ古い規則のままかを判断する手がかりになる。
                        stream.updated_at は Holodex 同期でも動くので使えない。 */}
                    {stream.comment_songs_analyzed_at && (
                      <p className="px-1 text-xs text-gray-400">
                        最終解析:{' '}
                        {new Date(stream.comment_songs_analyzed_at).toLocaleString('ja-JP', {
                          year: 'numeric', month: '2-digit', day: '2-digit',
                          hour: '2-digit', minute: '2-digit', hour12: false,
                        })}
                      </p>
                    )}
                    <SourceSongList
                      songs={commentTimelineSongs}
                      performanceTags={PERFORMANCE_TAGS}
                      onAdd={addCommentSongToList}
                      emptyMessage="分析済みの曲がありません（「全部読み込む」で分析を実行）"
                    />
                  </div>
                )}

                {editTab === 'chapter' && (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={() => loadFromChapters(false)}
                        disabled={stream?.chapter_count === 0 || chapterAnalyzeLoading}
                        className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                        title="配信者が付けた目次から曲を取り出して編集リストへ読み込む（正規化＋chat 時間チェック込み）"
                      >
                        {chapterAnalyzeLoading ? '分析中...' : '全部読み込む'}
                      </button>
                      <button
                        onClick={() => loadFromChapters(true)}
                        disabled={chapterAnalyzeLoading}
                        title="チャプターを取り直して AI で再分析します（配信者が後から目次を足した場合）"
                        className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        再取得
                      </button>
                    </div>
                    {/* 「まだ調べていない（-1）」と「調べたが無い（0）」を書き分ける。
                        同じ文言にすると、取得を試せば済む配信が諦めた配信に見える */}
                    {stream.chapter_count === -1 && (
                      <p className="px-1 text-xs text-gray-400">
                        チャプターは未取得です（「全部読み込む」で YouTube から取得します）
                      </p>
                    )}
                    {stream.chapter_count === 0 && (
                      <p className="px-1 text-xs text-gray-400">この配信にチャプターはありません</p>
                    )}
                    <SourceSongList
                      songs={chapterTimelineSongs}
                      performanceTags={PERFORMANCE_TAGS}
                      onAdd={addChapterSongToList}
                      emptyMessage="分析済みの曲がありません（「全部読み込む」で分析を実行）"
                    />
                  </div>
                )}

                {editTab === 'raw' && (
                  <RawCommentsPanel
                    videoId={stream.id}
                    onSeek={(secs) => playerSeekTo('page', secs)}
                    onAddSong={addFromRawComment}
                  />
                )}
              </div>
            </div>
          ) : (
            <div className="p-6">
              <>
                <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
                {/* 日時 + YouTube リンク */}
                <p className="text-gray-500 mt-2 flex items-center gap-2">
                  {new Date(stream.stream_date).toLocaleString('ja-JP', {
                    year: 'numeric',
                    month: '2-digit',
                    day: '2-digit',
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                    hour12: false
                  })}
                  <a
                    href={youtubeUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center justify-center w-7 h-7 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                    title="YouTubeで見る"
                  >
                    <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                      <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                    </svg>
                  </a>
                </p>

                {/* Tags + 編集アイコン */}
                <div className="flex flex-wrap items-center gap-2 mt-4">
                  {stream.tags.map((tag) => (
                    <Tag key={tag.id} label={tag.display_name} color={tag.color} size="md" />
                  ))}
                  {canEdit && (
                    <button
                      onClick={() => setTagPickerOpen(!tagPickerOpen)}
                      className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                        tagPickerOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
                      }`}
                      title="タグを編集"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                    </button>
                  )}
                </div>
                {canEdit && tagPickerOpen && (
                  <div className="flex flex-wrap gap-2 mt-2 p-3 bg-gray-50 rounded-lg border">
                    {STREAM_TAGS.map((tag) => {
                      const ids = stream.tags.map((t) => t.id);
                      const active = ids.includes(tag.id);
                      return (
                        <button
                          key={tag.id}
                          onClick={() =>
                            quickSaveStream({
                              tag_ids: active ? ids.filter((i) => i !== tag.id) : [...ids, tag.id],
                            })
                          }
                          className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                            active ? 'text-white' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                          }`}
                          style={active ? { backgroundColor: tag.color } : {}}
                        >
                          {tag.label}
                        </button>
                      );
                    })}
                  </div>
                )}

                {/* Participants + 編集アイコン */}
                <div className="flex flex-wrap items-center gap-2 mt-4">
                  {stream.participants?.map((singer) => (
                    <div
                      key={singer.id}
                      className="flex items-center gap-2 px-3 py-1 bg-gray-100 rounded-full text-sm"
                    >
                      {/* リンクは名前と画像だけに掛ける。チップ全体を包むと、
                          「参加者から外す」の ✕ を押したときにチャンネルページへ飛ぶ */}
                      <Link
                        to={`/singers/${singer.id}`}
                        className="flex items-center gap-2 text-gray-700 hover:text-indigo-600 transition-colors"
                        title={`${singer.name} のチャンネルページを開く`}
                      >
                        {singer.photo_url && (
                          <img
                            src={singer.photo_url}
                            alt={singer.name}
                            className="w-5 h-5 rounded-full"
                            onError={(e) => {
                              e.currentTarget.onerror = null;
                              e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                            }}
                          />
                        )}
                        <span>{singer.name}</span>
                      </Link>
                      {canEdit && participantAddOpen && (
                        <button
                          onClick={() =>
                            quickSaveStream({
                              participant_ids: (stream.participants ?? [])
                                .map((p) => p.id)
                                .filter((pid) => pid !== singer.id),
                            })
                          }
                          className="text-gray-400 hover:text-red-600 transition-colors"
                          title="参加者から外す"
                        >
                          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                          </svg>
                        </button>
                      )}
                    </div>
                  ))}
                  {canEdit && (
                    <button
                      onClick={() => setParticipantAddOpen(!participantAddOpen)}
                      className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                        participantAddOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
                      }`}
                      title="参加者を編集"
                      aria-label="参加者を編集"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                    </button>
                  )}
                </div>
                {canEdit && participantAddOpen && (
                  <div className="mt-2">
                    <SingerSearchInput
                      excludeIds={stream.participants?.map((p) => p.id) ?? []}
                      onSelectSinger={(singer) =>
                        quickSaveStream({
                          participant_ids: [...(stream.participants?.map((p) => p.id) ?? []), singer.id],
                        })
                      }
                      placeholder="チャンネル名を入力して参加者を追加..."
                    />
                  </div>
                )}

                {/* 非表示 */}
                {canEdit && (
                  <label className="mt-4 flex items-center gap-2 cursor-pointer w-fit">
                    <input
                      type="checkbox"
                      checked={stream.is_hidden}
                      onChange={(e) => quickSaveStream({ is_hidden: e.target.checked })}
                      className="w-4 h-4 text-red-600 border-gray-300 rounded focus:ring-red-500"
                    />
                    <span className="text-sm font-medium text-gray-700">非表示</span>
                  </label>
                )}
              </>
            </div>
          )}
        </div>

        {/* Player + Timeline - 60% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border flex flex-col min-h-0">
          {/* 16:9 に固定する。flex-1 で縦に伸ばすと、プレイヤーは 16:9 のままなので
              余った高さが黒帯として残る（審査画面の器と同じ形に揃えてある）。 */}
          <div className="bg-black w-full aspect-video overflow-hidden">
            <YoutubePlayer
              videoId={stream.id}
              onReady={(player) => {
                playerInstanceRef.current = player;
              }}
            />
          </div>

          <div className="border-t py-3 px-0 shrink-0">
            {/* YouTube の進捗バーと左右端を揃える（デスクトップ UI は左側の余白が少し広い）。 */}
            <div className="space-y-1 px-3 sm:pl-9 sm:pr-8">
              <div className="relative h-3 bg-gray-100 rounded-none">
                {setoriTimeline.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      playerSeekTo('page', item.start);
                      if (isEditing) scrollToEditableSong(item.start);
                    }}
                    className="absolute top-0 h-full rounded bg-indigo-500/80 hover:bg-indigo-600 transition-colors group"
                    style={{
                      left: `${getTimelineLeft(item.start)}%`,
                      width: `${getTimelineWidth(item.start, item.end)}%`,
                    }}
                  >
                    <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
                      <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                        <div className="font-semibold">{item.label}</div>
                        {item.artist && <div className="text-gray-300">{item.artist}</div>}
                        <div className="text-gray-400 mt-1">
                          {formatTime(item.start)} - {formatTime(item.end)}
                        </div>
                        <div className="text-indigo-400 text-[10px] mt-1">seTORI</div>
                        {isEditing && <div className="text-gray-500 text-[10px]">クリックで曲にジャンプ</div>}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              {isEditing && (
                <>
                  <div className="relative h-3 bg-blue-50 rounded-none">
                    {holodexTimeline.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => playerSeekTo('page', item.start)}
                        className="absolute top-0 h-full rounded bg-blue-500/70 hover:bg-blue-600 transition-colors group"
                        style={{
                          left: `${getTimelineLeft(item.start)}%`,
                          width: `${getTimelineWidth(item.start, item.end)}%`,
                        }}
                      >
                        <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
                          <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                            <div className="font-semibold">{item.label}</div>
                            {item.artist && <div className="text-gray-300">{item.artist}</div>}
                            <div className="text-gray-400 mt-1">
                              {formatTime(item.start)} - {formatTime(item.end)}
                            </div>
                            <div className="text-blue-400 text-[10px] mt-1">Holodex（未処理）</div>
                          </div>
                        </div>
                      </button>
                    ))}
                  </div>
                  <div className="relative h-3 bg-orange-50 rounded-none">
                    {rawCommentTimeline.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => playerSeekTo('page', item.start)}
                        className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-2 h-2 rounded-full bg-orange-500 hover:bg-orange-600 transition-colors group"
                        style={{ left: `${getTimelineLeft(item.start)}%` }}
                      >
                        <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
                          <div className="w-max max-w-80 bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg text-left whitespace-normal">
                            <div className="font-mono text-gray-300">{formatTime(item.start)}</div>
                            <div className="mt-1 break-words">{item.label}</div>
                            <div className="text-orange-400 text-[10px] mt-1">Raw comment（未処理）</div>
                          </div>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Right Column - Setlist */}
      <div className="w-full min-[1300px]:basis-3/5 min-[1300px]:shrink-0 min-w-0 min-h-0 min-[1300px]:self-stretch min-[1300px]:pr-6 flex flex-col">
        {/* Setlist Section - Unified View */}
        <div className="flex flex-col gap-3 mb-4 flex-none shrink-0">
          <div className="flex items-center gap-3">
            <h2 className="text-2xl font-bold text-gray-900">
              セットリスト ({isEditing ? editableSongs.length : stream.performances.length}曲)
            </h2>
            {!isEditing && (
              <div className="flex items-center gap-1.5">
                {canEdit && (
                  <button
                    onClick={toggleEditing}
                    className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-500 border border-gray-300 hover:bg-gray-100 transition-colors"
                    title="セットリストを編集"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                    </svg>
                  </button>
                )}
                {stream.performances.length > 0 && (
                  <>
                    <button
                      onClick={() => usePlayerStore.getState().playTracks(performanceTracks(), 0)}
                      className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                      title="セットリストを連続再生"
                    >
                      <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                    </button>
                    <button
                      onClick={() => {
                        usePlayerStore.getState().enqueue(performanceTracks());
                        showToast('セットリストをキューに追加しました', 'success');
                      }}
                      className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors"
                      title="セットリストをキューに追加"
                    >
                      <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
                      </svg>
                    </button>
                  </>
                )}
              </div>
            )}
            {isEditing && (
              /* 編集コントロールはタイトルと同じ行の右側に寄せる。処理完了は保存の直前に置く（即時保存） */
              <div className="ml-auto flex items-center gap-2 shrink-0">
                <button
                  onClick={toggleEditing}
                  className="px-3 py-1.5 text-sm bg-gray-200 text-gray-700 font-medium rounded-lg hover:bg-gray-300 transition-colors"
                >
                  キャンセル
                </button>
                {editableSongs.length > 0 && (
                  <span
                    className={`text-xs font-medium px-2 py-1 rounded ${
                      confirmedCount === editableSongs.length
                        ? 'bg-green-100 text-green-700'
                        : 'bg-gray-100 text-gray-500'
                    }`}
                    title="確認済みの曲数（保存は確認状態に関係なく全曲行われます）"
                  >
                    ✓ {confirmedCount}/{editableSongs.length}
                  </span>
                )}
                <label className="flex items-center gap-1.5 cursor-pointer text-sm font-medium text-gray-700 whitespace-nowrap">
                  <input
                    type="checkbox"
                    checked={stream.is_processed}
                    onChange={(e) => quickSaveStream({ is_processed: e.target.checked })}
                    className="w-4 h-4 text-green-600 border-gray-300 rounded focus:ring-green-500"
                  />
                  処理完了
                </label>
                <button
                  onClick={handleConfirm}
                  disabled={createPerformancesMutation.isPending || updateStreamMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {createPerformancesMutation.isPending || updateStreamMutation.isPending ? '処理中...' : '変更を保存'}
                </button>
              </div>
            )}
          </div>
        </div>

        <div className="overflow-x-hidden min-[1300px]:flex-1 min-[1300px]:min-h-0 min-[1300px]:overflow-y-auto">
        {isEditing ? (
          /* Editable Setlist */
          <div className="bg-white rounded-lg shadow-sm border p-6">
            {/* Editable songs list will default to channel owner as vocalist */}

            <div className="space-y-2">
              {editableSongs.length > 0 && editableSongs.map((song, index) => (
                index !== selectedSongIndex ? (
                  /* 圧縮行：クリックで詳細カードを展開＋プレイヤーがその曲へジャンプ */
                  <button
                    key={song.id}
                    id={`song-${song.id}`}
                    onClick={() => selectSong(index)}
                    className={`w-full flex items-center gap-3 px-3 py-2 border rounded-lg text-left transition-colors ${
                      highlightedSongId === song.id
                        ? 'bg-yellow-100 border-yellow-400'
                        : song.confirmed
                          ? 'bg-white border-gray-200 hover:border-indigo-300'
                          : 'bg-gray-50 border-gray-200 hover:border-indigo-300'
                    }`}
                  >
                    {/* 確認状態 */}
                    <span className="shrink-0" title={song.confirmed ? '確認済み' : song.end > 0 ? '未確認' : '終了時間なし'}>
                      {song.confirmed ? (
                        <svg className="w-5 h-5 text-green-500" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                        </svg>
                      ) : song.end > 0 ? (
                        <span className="block w-5 h-5 rounded-full border-2 border-gray-300" />
                      ) : (
                        <span className="block w-5 h-5 rounded-full border-2 border-red-400 bg-red-50" />
                      )}
                    </span>
                    <span className="text-xs font-mono text-gray-400 w-6 text-right shrink-0">{index + 1}</span>
                    {song.artUrl ? (
                      <img src={song.artUrl} alt="" className="w-8 h-8 object-cover rounded shrink-0" />
                    ) : (
                      <span className="w-8 h-8 bg-gray-200 rounded shrink-0" />
                    )}
                    <span className="flex-1 min-w-0">
                      <span className="block text-sm font-medium text-gray-900 truncate">
                        {song.name || <span className="text-gray-400">（曲名未入力）</span>}
                      </span>
                      <span className="block text-xs text-gray-500 truncate">{song.artist}</span>
                    </span>
                    <span className="text-xs font-mono text-gray-500 shrink-0">
                      {formatTimeInput(song.start)} - {song.end > 0 ? formatTimeInput(song.end) : '--:--'}
                    </span>
                    {song.itunesId && !song.itunesFromDb && (
                      <span
                        className="px-1.5 py-0.5 bg-amber-100 text-amber-800 text-[10px] font-medium rounded shrink-0"
                        title={`iTunes ID ${song.itunesId} を保存時にこの楽曲へ紐付けます`}
                      >
                        iTunes＋
                      </span>
                    )}
                    {!song.matchedSongId && (
                      <span className="px-1.5 py-0.5 bg-green-100 text-green-700 text-[10px] font-medium rounded shrink-0">New</span>
                    )}
                  </button>
                ) : (
                  <div key={song.id} id={`song-${song.id}`} className={`border-2 rounded-lg p-4 transition-colors duration-500 ${highlightedSongId === song.id ? 'bg-yellow-100 border-yellow-400' : 'bg-white border-indigo-300 shadow-sm'}`}>
                    <div className="flex justify-between items-start mb-3">
                      <div className="flex items-center gap-3">
                        {/* Art Thumbnail */}
                        {song.artUrl ? (
                          <img
                            src={song.artUrl}
                            alt={song.name}
                            className="w-12 h-12 object-cover rounded shadow-sm"
                          />
                        ) : (
                          <div className="w-12 h-12 bg-gray-200 rounded flex items-center justify-center">
                            <svg className="w-6 h-6 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                            </svg>
                          </div>
                        )}
                        <div>
                          <span className="text-sm font-medium text-gray-500">#{index + 1}</span>
                        </div>
                      </div>
                      <button
                        onClick={() => removeSong(index)}
                        className="text-red-500 hover:text-red-700 text-sm"
                      >
                        削除
                      </button>
                    </div>

                    <PerformanceFields
                      value={song}
                      onChange={(patch) => setEditableSongs((prev) => {
                        const updated = [...prev];
                        updated[index] = { ...updated[index], ...patch };
                        return updated;
                      })}
                      onSelectSong={(selectedSong) => handleSelectExistingSong(index, selectedSong)}
                      onTimeChange={(field, timeStr) => handleTimeChange(index, field, timeStr)}
                      onToggleTag={(tagId) => toggleTag(index, tagId)}
                      onApplyEndSource={(source) => applyEndSource(index, source)}
                      onClearItunes={() => clearItunesId(index)}
                      performanceTags={PERFORMANCE_TAGS}
                      participants={participants}
                      channelOwner={channelOwner}
                      onAddParticipant={(singer) => setParticipants([...participants, singer])}
                      showToast={showToast}
                    />

                    {/* 確認ナビゲーション：前へ / 確認して次へ / 次へ */}
                    <div className="mt-4 pt-3 border-t flex items-center gap-2">
                      <button
                        onClick={() => selectSong((index - 1 + editableSongs.length) % editableSongs.length)}
                        disabled={editableSongs.length < 2}
                        className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-40"
                        title="前の曲へ"
                      >
                        ◀ 前へ
                      </button>
                      <button
                        onClick={() => confirmAndNext(index)}
                        disabled={song.end === 0}
                        title={song.end === 0 ? '終了時間を設定してください' : 'この曲を確認済みにして次の未確認曲へ'}
                        className={`flex-1 px-3 py-1.5 text-sm font-medium rounded-lg transition-colors disabled:opacity-40 ${
                          song.confirmed
                            ? 'bg-green-100 text-green-700 hover:bg-green-200'
                            : 'bg-indigo-600 text-white hover:bg-indigo-700'
                        }`}
                      >
                        {song.confirmed ? '✓ 確認済み（再確認で次へ）' : '✓ 確認して次へ'}
                      </button>
                      <button
                        onClick={() => selectSong((index + 1) % editableSongs.length)}
                        disabled={editableSongs.length < 2}
                        className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-40"
                        title="次の曲へ"
                      >
                        次へ ▶
                      </button>
                    </div>
                  </div>
                )
              ))}

              {/* Add Song Button - always show */}
              <button
                onClick={addSong}
                className="w-full py-3 border-2 border-dashed border-gray-300 rounded-lg text-gray-500 hover:border-indigo-500 hover:text-indigo-500 transition-colors"
              >
                + 楽曲を追加
              </button>

              {/* Help text when list is empty */}
              {editableSongs.length === 0 && (
                <div className="text-center py-8 text-gray-500">
                  上のボタンから楽曲データを読み込むか、「+ 楽曲を追加」で手動追加してください
                </div>
              )}
            </div>
          </div>
        ) : (
          /* Read-only Setlist */
          stream.performances.length === 0 ? (
            <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
              セットリストがまだ登録されていません
            </div>
          ) : (
            <div className="bg-white rounded-lg shadow-sm border">
              {/* 狭い画面は表をやめて縦積みの行にする。390px に 7 列は入らず、
                  実際は横スクロール＋曲名の折り返しになっていて、列が揃っている
                  という表の利点は失われていた */}
              <ul className="divide-y divide-gray-100 md:hidden">
                {stream.performances.map((perf, index) => {
                  const tags = [
                    ...perf.tags.map((t) => ({ key: t.id, label: t.display_name, color: t.color })),
                    ...(perf.custom_tags ?? []).map((t) => ({ key: t, label: t, color: '#6B7280' })),
                  ];
                  // チャンネル所有者を先頭に（表と同じ並び）
                  const singers = [...(perf.singers ?? [])].sort((a, b) => {
                    if (channelOwner && a.id === channelOwner.id) return -1;
                    if (channelOwner && b.id === channelOwner.id) return 1;
                    return 0;
                  });
                  return (
                    <PerformanceListRow
                      key={perf.id}
                      track={toRowTrack(perf)}
                      thumbnailUrl={perf.arts}
                      badge={`#${index + 1}`}
                      youtubeUrl={perf.youtube_url}
                      playLabel={`${perf.song_name} をここから再生`}
                      onPlay={() => playerSeekTo('page', perf.start_seconds)}
                      meta={
                        <span className="flex items-center gap-2 overflow-hidden">
                          <span className="shrink-0 font-mono">
                            {formatTime(perf.start_seconds)}
                            {perf.end_seconds > 0 && `–${formatTime(perf.end_seconds)}`}
                          </span>
                          <RowSingerAvatars singers={singers} />
                          {/* タグは 2 つまで。多い配信で行が膨らむと一覧として眺められない */}
                          {tags.slice(0, 2).map((t) => (
                            <span key={t.key} className="shrink-0">
                              <Tag label={t.label} color={t.color} />
                            </span>
                          ))}
                          {tags.length > 2 && <span className="shrink-0">+{tags.length - 2}</span>}
                        </span>
                      }
                    />
                  );
                })}
              </ul>

              <div className="hidden md:block overflow-x-auto min-[1300px]:max-h-[calc(100vh-14rem)] min-[1300px]:overflow-y-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50 sticky top-0 z-10">
                  <tr>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-24">
                      #
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                      時間
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      楽曲
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      アーティスト
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-40">
                      タグ
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                      ボーカル
                    </th>
                    <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-20">
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-white divide-y divide-gray-200">
                  {stream.performances.map((perf, index) => {
                    const rowTrack = toRowTrack(perf);
                    const singerCount = perf.singers?.length || 0;
                    const showCount = singerCount > 3;
                    // 歌手を並べ替える：チャンネル所有者を優先
                    const sortedSingers = perf.singers?.sort((a, b) => {
                      if (channelOwner && a.id === channelOwner.id) return -1;
                      if (channelOwner && b.id === channelOwner.id) return 1;
                      return 0;
                    }) || [];
                    const displaySingers = showCount ? sortedSingers.slice(0, 3) : sortedSingers;
                    
                    return (
                      <tr key={perf.id} className="hover:bg-gray-50">
                        {/* # with Art thumbnail */}
                        <td className="px-4 py-4">
                          <div className="relative w-16 h-16">
                            {perf.arts ? (
                              <img
                                src={perf.arts}
                                alt={perf.song_name}
                                className="w-16 h-16 object-cover rounded-lg shadow-sm"
                              />
                            ) : (
                              <div className="w-16 h-16 bg-gray-100 rounded-lg flex items-center justify-center">
                                <svg className="w-8 h-8 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                                </svg>
                              </div>
                            )}
                            {/* Index badge */}
                            <div className="absolute top-0 left-0 bg-indigo-600 text-white text-xs font-bold rounded-tl-lg rounded-br-lg px-2 py-0.5">
                              #{index + 1}
                            </div>
                          </div>
                        </td>
                        {/* 開始と終了は**常に2行**にする。1行に流していたときは
                            列幅（w-32）にちょうど収まるかどうかで折り返しが決まり、
                            13:25 は1行・1:23:15 は2行と行ごとに形が変わっていた */}
                        <td className="px-4 py-4 text-sm text-gray-500 font-mono">
                          <span className="block whitespace-nowrap">
                            {formatTime(perf.start_seconds)}
                            {perf.end_seconds > 0 && ' ~'}
                          </span>
                          {perf.end_seconds > 0 && (
                            <span className="block whitespace-nowrap text-gray-400">
                              {formatTime(perf.end_seconds)}
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-4">
                          <Link
                            to={`/songs/${perf.song_id}`}
                            className="text-indigo-600 hover:text-indigo-900 font-medium"
                          >
                            {perf.song_name}
                          </Link>
                        </td>
                        <td className="px-4 py-4 text-sm text-gray-500">
                          <ArtistLinks
                            artists={perf.artists}
                            fallback={perf.original_artist}
                            linkClassName="hover:text-indigo-600"
                          />
                        </td>
                        <td className="px-4 py-4">
                          <div className="flex flex-wrap gap-1">
                            {perf.tags.map((tag) => (
                              <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                            ))}
                            {perf.custom_tags?.map((ct) => (
                              <Tag key={ct} label={ct} color="#6B7280" />
                            ))}
                          </div>
                        </td>
                        {/* Singer avatars */}
                        <td className="px-4 py-4">
                          {singerCount === 0 ? (
                            <span className="text-sm text-gray-400">なし</span>
                          ) : (
                            <div className="flex items-center relative h-8">
                              {displaySingers?.map((singer, singerIndex) => (
                                <Link
                                  key={singer.id}
                                  to={`/singers/${singer.id}`}
                                  title={singer.name}
                                  className="relative -ml-2 first:ml-0 hover:z-50"
                                  style={{
                                    zIndex: displaySingers.length - singerIndex,
                                  }}
                                >
                                  <img
                                    src={
                                      singer.photo_url ||
                                      `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                                    }
                                    alt={singer.name}
                                    className="w-8 h-8 rounded-full border-2 border-white shadow-sm hover:shadow-md transition-shadow"
                                    onError={(e) => {
                                      e.currentTarget.onerror = null;
                                      e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                                    }}
                                  />
                                </Link>
                              ))}
                              {showCount && (
                                <button
                                  onClick={() => setVocalistPopupSingers(sortedSingers)}
                                  title={perf.singers
                                    ?.map((s) => s.name)
                                    .join(', ')}
                                  className="relative -ml-2 w-8 h-8 rounded-full bg-gray-300 border-2 border-white flex items-center justify-center text-xs font-bold text-gray-700 cursor-pointer hover:bg-gray-400 transition-colors"
                                >
                                  +{singerCount - 3}
                                </button>
                              )}
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-4 text-right">
                          <div className="inline-flex items-center gap-1.5">
                            <button
                              onClick={() => playerSeekTo('page', perf.start_seconds)}
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                              title={`${perf.song_name} を再生`}
                            >
                              <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M8 5v14l11-7z" />
                              </svg>
                            </button>
                            <QueueAddButton track={rowTrack} />
                            {/* 時間・曲・歌った人はすべて 1 つの報告画面で直す */}
                            <ReportButton track={rowTrack} />
                            <a
                              href={perf.youtube_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                              title="YouTubeで開く"
                            >
                              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                              </svg>
                            </a>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
                </table>
              </div>
            </div>
          )
        )}
        </div>
      </div>
    </div>

    {/* Vocalist Popup */}
    {vocalistPopupSingers && (
      <div
        className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
        onClick={() => setVocalistPopupSingers(null)}
      >
        <div
          className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4 p-6"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-bold text-gray-900">ボーカル一覧</h3>
            <button
              onClick={() => setVocalistPopupSingers(null)}
              className="text-gray-400 hover:text-gray-600 transition-colors"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {vocalistPopupSingers.map((singer) => (
              <Link
                key={singer.id}
                to={`/singers/${singer.id}`}
                className="flex items-center gap-3 p-3 rounded-lg hover:bg-gray-50 transition-colors"
                onClick={() => setVocalistPopupSingers(null)}
              >
                <img
                  src={
                    singer.photo_url ||
                    `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                  }
                  alt={singer.name}
                  className="w-12 h-12 rounded-full border-2 border-gray-200"
                  onError={(e) => {
                    e.currentTarget.onerror = null;
                    e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                  }}
                />
                <div className="flex-1">
                  <div className="font-medium text-gray-900">{singer.name}</div>
                  {singer.english_name && (
                    <div className="text-sm text-gray-500">{singer.english_name}</div>
                  )}
                </div>
                <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
              </Link>
            ))}
          </div>
        </div>
      </div>
      )}
    </>
  );
}
