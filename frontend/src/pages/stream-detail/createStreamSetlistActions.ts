import { performanceApi, artistApi } from '../../api/client';
import { parseTime } from '../../utils/timeFormat';
import type { Channel, CreatePerformanceItem, AINormalizationItem, Song } from '../../api/types';
import type { useToast } from '../../components/ui/ToastContext';
import { sameViewer, viewerID } from '../../queryClient';
import { playerSeekTo } from '../../components/youtubePlayerControl';
import type { Dispatch, SetStateAction } from 'react';
import type { QueryClient, UseMutationResult } from '@tanstack/react-query';
import type { StreamDetailResponse, BatchAINormalizationResponse, CreatePerformancesResponse } from '../../api/types';
import type { EditableSong } from './types';

interface Context {
  setSelectedSongIndex: Dispatch<SetStateAction<number | null>>;
  editableSongs: EditableSong[];
  setEditableSongs: Dispatch<SetStateAction<EditableSong[]>>;
  isEditing: boolean;
  setIsEditing: Dispatch<SetStateAction<boolean>>;
  stream: StreamDetailResponse | undefined;
  setParticipants: Dispatch<SetStateAction<Channel[]>>;
  aiNormalizeMutation: UseMutationResult<{ startedAs: string | null; data: BatchAINormalizationResponse; }, Error, AINormalizationItem[], unknown>;
  fetchTrackDurationByItunesId: (itunesId: number) => Promise<number | null>;
  channelOwner: Channel | null;
  setHighlightedSongId: Dispatch<SetStateAction<string | null>>;
  id: string | undefined;
  showToast: ReturnType<typeof useToast>['showToast'];
  queryClient: QueryClient;
  createPerformancesMutation: UseMutationResult<CreatePerformancesResponse, Error, CreatePerformanceItem[], unknown>;
}

// React の state はページの hook に残し、このレンダーの操作だけを組み立てる。
export function createStreamSetlistActions(context: Context) {
  const {
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
  } = context;

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

  const toggleEditing = () => {
    if (isEditing) {
      // 編集モードを終了する
      setIsEditing(false);
      setEditableSongs([]);
    } else {
      // 編集モードを開始し、既存のセットリストを自動で読み込む
      if (stream) {
        // 参加チャンネルの候補一覧を設定する（歌唱に参加したすべての歌手を含む）
        const allSingers = new Map<string, Channel>();

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
    const startedAs = viewerID();
    const selectedItunesId = song.itunes_ids && song.itunes_ids.length > 0 ? Number(song.itunes_ids[0].itunes_id) : null;
    const selectedTrackDuration = selectedItunesId ? await fetchTrackDurationByItunesId(selectedItunesId) : null;

    if (!sameViewer(startedAs)) return;
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
    // 配信情報（タグ・参加チャンネル・非表示など）は閲覧モードのクイック編集で即時保存されるため、
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
    const startedAs = viewerID();
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
      // **各リクエストの前に確かめる。** ループの外に照合を置くだけでは、
      // 途中で利用者が変わっても残りの登録が**新しい認証情報で送られる**
      // ── 別の利用者の名義で即時反映または提案として記録されてしまう。
      if (!sameViewer(startedAs)) return;
      try {
        const res = await artistApi.proposeAlias(a.canonical, a.alias);
        if (res.applied) {
          applied++;
        } else {
          proposed++;
        }
      } catch (err) {
        console.error('alias registration failed:', err);
        // **通知に編集リスト由来の名義が載る。** 待っている間に権限が変われば、
        // 破棄したあとに秘匿入力由来の名義が再表示される。
        if (sameViewer(startedAs)) {
          showToast(`別名義の登録に失敗しました（${a.alias}）`, 'error');
        }
      }
    }
    if (!sameViewer(startedAs)) return;
    if (applied > 0) showToast(`${applied}件の別名義を登録しました`, 'success');
    if (proposed > 0) showToast(`${proposed}件の別名義を提案として登録しました`, 'info');
  };
  return { selectSong, confirmAndNext, toggleEditing, runAINormalization, handleSongChange, applyEndSource, handleSelectExistingSong, clearItunesId, handleTimeChange, toggleTag, removeSong, addSong, scrollToEditableSong, handleConfirm, registerCheckedAliases };
}
