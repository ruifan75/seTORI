import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { performanceApi, songApi, streamApi, suggestionApi } from '../api/client';
import { usePlayerStore } from '../store/player';
import { usePlayerTime } from './usePlayerTime';
import { playerGetCurrentTime, playerGetDuration, playerPause, playerSeekTo } from './youtubePlayerControl';
import { usePerformanceTiming, invalidateContentQueries, formatSeconds } from './usePerformanceTiming';
import { useToast } from './ui/ToastContext';
import type { RangeNeighbour } from './RangeEditor';
import type { Performance } from '../api/types';

// 報告画面の中身（状態・変更の判定・送信）。**画面の形とは分けてある。**
//
// デスクトップは横並び、スマホはタブ切り替えと、外殻はまったく別物になるが、
// 送信の振り分け（時間→performance / 歌った人→singer_ids / 曲→perf.meta /
// アーティスト→song）はこの機能で一番間違えやすいところなので、
// **2 つ書くことはしない**。外殻だけ差し替える。

// 編集中の値。**歌唱に属するもの（時間・歌った人）と曲に属するもの（曲そのもの・
// 原曲アーティスト）が混ざっている**のが要点で、送信時はそれぞれ別の宛先へ分かれる。
export interface Draft {
  start: number;
  end: number;
  // 差し替え先の曲。'' は「登録されていない曲（承認時に作る）」を意味する
  songId: string;
  songName: string;
  artist: string;
  itunesId: number | null;
  artUrl: string | null;
  singerIds: string[];
}

const emptyDraft: Draft = {
  start: 0,
  end: 0,
  songId: '',
  songName: '',
  artist: '',
  itunesId: null,
  artUrl: null,
  singerIds: [],
};

function draftOf(p: Performance): Draft {
  return {
    start: p.start_seconds,
    end: p.end_seconds,
    songId: p.song_id,
    songName: p.song_name,
    artist: p.original_artist,
    itunesId: null,
    artUrl: null,
    singerIds: (p.singers ?? []).map((s) => s.id),
  };
}

// 歌った人は順番に意味が無いので、集合として比べる
function sameIds(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(b);
  return a.every((id) => set.has(id));
}

// 動画を重ねる矩形を親（PlayerBar）へ知らせる。
//
// enabled が false のときは null を渡して画面外へ退避させる ── スマホでは
// 「曲・歌手」タブに動画を出さない（キーボードが出ると測った位置と実際の
// レイアウトがずれるうえ、検索結果を出す場所も無い）。**退避であって停止ではない**
// ので音は鳴り続ける。
export function useVideoSlot(
  onVideoSlot: (rect: { top: number; left: number; width: number; height: number } | null) => void,
  enabled: boolean
) {
  const slotRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (!enabled) {
      onVideoSlot(null);
      return;
    }
    const report = () => {
      const el = slotRef.current;
      if (!el) return;
      const r = el.getBoundingClientRect();
      onVideoSlot({ top: r.top, left: r.left, width: r.width, height: r.height });
    };
    report();
    const observer = new ResizeObserver(report);
    if (slotRef.current) observer.observe(slotRef.current);
    // スマホはアドレスバーの出入りとキーボードで高さが変わる
    window.addEventListener('resize', report);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', report);
      onVideoSlot(null);
    };
  }, [onVideoSlot, enabled]);

  return slotRef;
}

export function usePerformanceReport() {
  const editing = usePlayerStore((s) => s.editing);
  const setEditing = usePlayerStore((s) => s.setEditing);
  const queue = usePlayerStore((s) => s.queue);
  const index = usePlayerStore((s) => s.index);
  const jumpTo = usePlayerStore((s) => s.jumpTo);
  const { canEdit, canSubmit, submit } = usePerformanceTiming();
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const invalidate = () => invalidateContentQueries(queryClient);

  const streamId = editing?.streamId ?? '';
  const performanceId = editing?.performanceId ?? null;

  const { data: stream } = useQuery({
    queryKey: ['stream', streamId],
    queryFn: () => streamApi.get(streamId),
    enabled: streamId !== '',
  });

  const performances = useMemo<Performance[]>(
    () => [...(stream?.performances ?? [])].sort((a, b) => a.start_seconds - b.start_seconds),
    [stream]
  );
  const target = performances.find((p) => p.id === performanceId) ?? null;
  // performanceId が null ＝「ここに登録されていない曲がある」という報告。
  // 対象の歌唱が無いので、差分ではなく内容そのものを送る（perf.missing）
  const missingMode = performanceId === null;

  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // 対象が切り替わったらレンダー中に同期で入れ替える（effect 経由だと
  // 1 フレームだけ前の曲の時刻が見える）。React が勧める「レンダー中の状態調整」の形
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const draftKey = target ? target.id : missingMode ? 'missing' : null;
  if (draftKey && loadedFor !== draftKey) {
    setLoadedFor(draftKey);
    // 抜けている曲は「今聴いているところ」から始める。曲名だけ入れれば送れる
    setDraft(target ? draftOf(target) : { ...emptyDraft, start: Math.round(playerGetCurrentTime('bar') ?? 0) });
    setNote('');
    setError('');
  }

  const patch = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));

  const currentTime = usePlayerTime('bar');
  const [duration, setDuration] = useState<number | null>(null);
  useEffect(() => {
    // 長さは読み込み完了まで 0 なので、取れるまで少し待つ
    const timer = setInterval(() => {
      const d = playerGetDuration('bar');
      if (d) {
        setDuration(d);
        clearInterval(timer);
      }
    }, 500);
    return () => clearInterval(timer);
  }, [streamId]);

  // 歌枠編集ページには埋め込みプレイヤーが居る。開いたままだと同じ動画が
  // 二重に鳴るので黙らせる（この画面が使うのは再生バーの方）
  useEffect(() => {
    playerPause('page');
  }, []);

  const close = () => setEditing(null);

  // Esc で閉じる。拡大表示の Esc より先に処理したいので capture で拾う
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.stopPropagation();
      setEditing(null);
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [setEditing]);

  // 対象を切り替える。プレイヤーもその曲へ移す（聴かずに直すことはできない）
  const selectTarget = (id: string) => {
    const perf = performances.find((p) => p.id === id);
    if (!perf) return;
    setEditing({ streamId, performanceId: id });
    const queued = queue.findIndex((t) => t.performanceId === id);
    if (queued >= 0 && queued !== index) jumpTo(queued);
    else playerSeekTo('bar', perf.start_seconds);
  };

  const openMissing = () => setEditing({ streamId, performanceId: null });

  // 隣の歌唱（自分以外）。時間軸の背景に描いて食い込みを見えるようにする
  const neighbours: RangeNeighbour[] = performances
    .filter((p) => p.id !== performanceId)
    .map((p) => ({
      id: p.id,
      label: p.song_name || '(曲名なし)',
      start: p.start_seconds,
      end: p.end_seconds > p.start_seconds ? p.end_seconds : p.start_seconds + 1,
    }));

  // 何が変わったか。**宛先が違うので別々に見る。**
  //   時間・歌った人 … その歌唱だけの話 → performance
  //   曲そのもの     … 別の曲へ繋ぎ替える → perf.meta（曲マスタの差し替え）
  //   原曲アーティスト … 曲の属性 → song（**その曲の全歌唱に効く**）
  const timingChanged =
    target !== null && (draft.start !== target.start_seconds || draft.end !== target.end_seconds);
  const singersChanged =
    target !== null && !sameIds(draft.singerIds, target.singers?.map((x) => x.id) ?? []);
  const songChanged =
    target !== null && (draft.songId !== target.song_id || draft.songName.trim() !== target.song_name);
  // 曲を選び直したならアーティストは新しい曲のものなので、単独の指摘としては数えない
  const artistChanged =
    target !== null && !songChanged && draft.artist.trim() !== target.original_artist;
  const changed = missingMode
    ? draft.songName.trim() !== ''
    : timingChanged || singersChanged || songChanged || artistChanged;

  const summary: string[] = [];
  if (missingMode) {
    if (draft.songName.trim()) {
      summary.push(`${formatSeconds(draft.start)} に「${draft.songName.trim()}」を追加`);
    }
  } else if (target) {
    if (draft.start !== target.start_seconds) {
      summary.push(`開始 ${formatSeconds(target.start_seconds)} → ${formatSeconds(draft.start)}`);
    }
    if (draft.end !== target.end_seconds) {
      const before = target.end_seconds === 0 ? '最後まで' : formatSeconds(target.end_seconds);
      const after = draft.end === 0 ? '最後まで' : formatSeconds(draft.end);
      summary.push(`終了 ${before} → ${after}`);
    }
    if (songChanged) summary.push(`曲 ${target.song_name} → ${draft.songName.trim()}`);
    if (artistChanged) {
      summary.push(`アーティスト ${target.original_artist || '（空）'} → ${draft.artist.trim() || '（空）'}`);
    }
    if (singersChanged) summary.push('歌った人');
  }

  // 送信は宛先ごとに分かれる。**1 つの API にまとめない** ── 影響範囲が違うものを
  // 1 件の提案にすると、レビューで「時間だけ採用、曲は却下」ができなくなる。
  const handleSubmit = async () => {
    if (draft.end !== 0 && draft.end <= draft.start) {
      setError(`終了（${formatSeconds(draft.end)}）は開始（${formatSeconds(draft.start)}）より後にしてください`);
      return;
    }
    if ((songChanged || missingMode) && draft.songName.trim() === '') {
      setError('曲を選ぶか、曲名を入力してください');
      return;
    }
    setError('');
    setBusy(true);
    try {
      // 抜けている曲は**権限があっても提案として送る**。承認時に曲マスタも
      // 作られるので、必ず人の目を通す
      if (missingMode) {
        await suggestionApi.create({
          kind: 'perf.missing',
          payload: {
            stream_id: streamId,
            song_name: draft.songName.trim(),
            original_artist: draft.artist.trim(),
            start_seconds: draft.start,
            end_seconds: draft.end,
            ...(draft.songId ? { song_id: draft.songId } : {}),
            ...(draft.singerIds.length > 0 ? { singer_ids: draft.singerIds } : {}),
            ...(draft.itunesId != null ? { itunes_id: draft.itunesId } : {}),
            ...(draft.artUrl ? { art_url: draft.artUrl } : {}),
          },
          note,
        });
        showToast('抜けている曲として報告しました。管理者の確認をお待ちください', 'success');
        invalidate();
        close();
        return;
      }
      if (!target) return;

      // 1. 時間 ── 権限の有無で保存/提案が分かれるので既存のフックに任せる
      if (timingChanged) {
        const ok = await submit(
          {
            performanceId: target.id,
            songName: target.song_name,
            start: target.start_seconds,
            end: target.end_seconds,
          },
          {
            ...(draft.start !== target.start_seconds ? { start: draft.start } : {}),
            ...(draft.end !== target.end_seconds ? { end: draft.end } : {}),
          },
          note
        );
        if (!ok) return; // トーストは submit 側が出している
      }

      // 2. 歌った人
      if (singersChanged) {
        if (canEdit) {
          await performanceApi.update(target.id, { singer_ids: draft.singerIds });
        } else {
          await suggestionApi.create({
            target_type: 'performance',
            target_id: target.id,
            fields: { singer_ids: draft.singerIds.join(',') },
            note,
          });
        }
      }

      // 3. 曲の差し替え
      if (songChanged) {
        const swap = {
          song_id: draft.songId,
          song_name: draft.songName.trim(),
          original_artist: draft.artist.trim(),
          ...(draft.itunesId != null ? { itunes_id: draft.itunesId } : {}),
          ...(draft.artUrl ? { art_url: draft.artUrl } : {}),
        };
        if (canEdit && draft.songId !== '') {
          // 既存の曲へ繋ぎ替えるだけなら単件更新で足りる。提案を作って自分で
          // 承認すると、承認済み一覧が自己承認で埋まる
          await performanceApi.update(target.id, { song_id: draft.songId });
        } else {
          const created = await suggestionApi.create({
            kind: 'perf.meta',
            target_id: target.id,
            song_swap: swap,
            note,
          });
          // 未登録の曲名へ差し替える場合は曲の作成を伴うので、権限があっても
          // 承認経路（findOrCreateSong）を通す。曲を作る API が単独では無い
          if (canEdit) await suggestionApi.approve(created.id);
        }
      }

      // 4. 原曲アーティスト（曲の属性なので宛先は songs）
      if (artistChanged && target.song_id) {
        if (canEdit) {
          // UpdateSongRequest は全項目更新なので、変えない曲名もそのまま送る
          await songApi.update(target.song_id, {
            name: target.song_name,
            original_artist: draft.artist.trim(),
          });
        } else {
          await suggestionApi.create({
            target_type: 'song',
            target_id: target.song_id,
            fields: { original_artist: draft.artist.trim() },
            note,
          });
        }
      }

      // 時間だけの変更は submit 側がトーストを出しているので二重に出さない
      if (singersChanged || songChanged || artistChanged) {
        showToast(canEdit ? '直しました' : '報告しました。管理者の確認をお待ちください', 'success');
      }
      invalidate();
      close();
    } catch (e) {
      setError(`送信できませんでした: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  return {
    editing,
    stream,
    streamId,
    performanceId,
    performances,
    target,
    missingMode,
    draft,
    patch,
    note,
    setNote,
    busy,
    error,
    canEdit,
    canSubmit,
    currentTime,
    duration,
    neighbours,
    songChanged,
    artistChanged,
    changed,
    summary,
    handleSubmit,
    close,
    selectTarget,
    openMissing,
  };
}
