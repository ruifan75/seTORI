import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { streamApi } from '../api/client';
import { usePlayerStore } from '../store/player';
import { PlayerScopeContext } from './playerScope';
import { usePlayerTime } from './usePlayerTime';
import {
  playerGetCurrentTime,
  playerGetDuration,
  playerPause,
  playerSeekTo,
} from './youtubePlayerControl';
import RangeEditor, { type RangeNeighbour } from './RangeEditor';
import SetlistStrip from './SetlistStrip';
import SongSearchInput from './SongSearchInput';
import ArtistSearchInput from './ArtistSearchInput';
import LoginToSuggest from './LoginToSuggest';
import {
  usePerformanceTiming,
  invalidateContentQueries,
  formatSeconds,
  parseSeconds,
} from './usePerformanceTiming';
import { formatTimeInput } from '../utils/timeFormat';
import { performanceApi, songApi, suggestionApi } from '../api/client';
import { useToast } from './ui/ToastContext';
import type { Performance, Singer } from '../api/types';

// 編集中の値。**歌唱に属するもの（時間・歌った人）と曲に属するもの（曲そのもの・
// 原曲アーティスト）が混ざっている**のが要点で、送信時はそれぞれ別の宛先へ分かれる。
interface Draft {
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

// 再生中に気づいた誤りを、聴きながら直すための画面。
//
// **区間の締め切りを外した状態で開く**（store の editing）。開始/終了がずれて
// いるとき正しい位置は今の区間の外にあるので、区間に閉じ込めたまま直させる
// ことはできない ── これが小さなポップオーバー（「開始はここ」「終了はここ」）
// を畳んでこの画面にした理由。あちらは押した瞬間の再生位置しか送れず、
// その再生位置が区間の外へ出られなかった。
//
// 動画はこの画面の中に置く。ただし iframe は再マウントすると再生が切れるので、
// ここに置くのはプレースホルダだけで、実物は PlayerBar が持つ fixed 要素を
// 測った位置へ動かして重ねる。
export default function PerformanceReportDialog({
  onVideoSlot,
}: {
  // 動画を重ねる矩形を親（PlayerBar）へ伝える
  onVideoSlot: (rect: { top: number; left: number; width: number; height: number } | null) => void;
}) {
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

  // 編集中の値。対象が変わったら入れ直す
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

  // 動画スロットの矩形を親へ渡す。ダイアログ側のレイアウトが唯一の基準になる
  const slotRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const report = () => {
      const el = slotRef.current;
      if (!el) return;
      const r = el.getBoundingClientRect();
      onVideoSlot({ top: r.top, left: r.left, width: r.width, height: r.height });
    };
    report();
    const observer = new ResizeObserver(report);
    if (slotRef.current) observer.observe(slotRef.current);
    window.addEventListener('resize', report);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', report);
      onVideoSlot(null);
    };
  }, [onVideoSlot]);

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
      close();
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 対象を切り替える。プレイヤーもその曲へ移す（聴かずに直すことはできない）
  const selectTarget = (id: string) => {
    const perf = performances.find((p) => p.id === id);
    if (!perf) return;
    setEditing({ streamId, performanceId: id });
    const queued = queue.findIndex((t) => t.performanceId === id);
    if (queued >= 0 && queued !== index) jumpTo(queued);
    else playerSeekTo('bar', perf.start_seconds);
  };

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
    if (artistChanged) summary.push(`アーティスト ${target.original_artist || '（空）'} → ${draft.artist.trim() || '（空）'}`);
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
      // 作られるので、必ず人の目を通す（既存の報告ダイアログと同じ判断）
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

  if (!editing) return null;

  return (
    // この画面の再生位置・試聴はすべてグローバル再生バーのもの
    <PlayerScopeContext value="bar">
      <div className="fixed inset-0 z-[65] bg-gray-950 text-white flex flex-col pb-[env(safe-area-inset-bottom)]">
        {/* ヘッダー */}
        <div className="shrink-0 flex items-center gap-3 px-4 h-12 border-b border-white/10">
          <span className="text-sm font-medium shrink-0 whitespace-nowrap">
            {missingMode ? '抜けている曲を報告' : '歌唱を報告'}
          </span>
          <span className="text-xs text-gray-400 truncate min-w-0">{stream?.title}</span>
          <button
            onClick={close}
            className="ml-auto p-2 -mr-2 text-gray-300 hover:text-white hover:bg-white/10 rounded-lg"
            title="閉じる（Esc）"
            aria-label="閉じる"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex-1 min-h-0 flex flex-col lg:flex-row">
          {/* 左：動画＋区間 */}
          <div className="flex-1 min-w-0 flex flex-col min-h-0">
            {/* 動画とトランスポートはスクロールさせない。**動画は fixed 要素を
                この枠へ重ねているだけ**なので、枠がスクロールで動くと追従の
                ために毎フレーム測り直すことになる（ずれる/重い） */}
            <div className="shrink-0 p-3 pb-2">
              <div
                ref={slotRef}
                className="w-full aspect-video max-h-[34vh] lg:max-h-[44vh] mx-auto bg-black rounded-lg"
                style={{ maxWidth: 'calc(44vh * 1.7778)' }}
              />
              <Transport currentTime={currentTime} />
            </div>

            {/* ここから下だけスクロールする */}
            <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-3 pb-3 space-y-3">

            {target || missingMode ? (
              <div className="rounded-lg bg-white text-gray-900 p-3">
                <RangeEditor
                  start={draft.start}
                  end={draft.end}
                  duration={duration}
                  neighbours={neighbours}
                  onChange={(patch) => setDraft((d) => ({ ...d, ...patch }))}
                />

                <div className="mt-3 grid grid-cols-2 gap-3">
                  <TimeField
                    label="開始"
                    value={draft.start}
                    original={target?.start_seconds ?? draft.start}
                    currentTime={currentTime}
                    onChange={(v) => setDraft((d) => ({ ...d, start: v }))}
                  />
                  <TimeField
                    label="終了"
                    value={draft.end}
                    original={target?.end_seconds ?? 0}
                    currentTime={currentTime}
                    allowEmpty
                    onChange={(v) => setDraft((d) => ({ ...d, end: v }))}
                  />
                </div>
              </div>
            ) : (
              <p className="text-sm text-gray-400">対象の歌唱が見つかりません</p>
            )}

            {(target || missingMode) && (
              <div className="rounded-lg bg-white text-gray-900 p-3 space-y-3">
                <div>
                  <label className="block text-xs font-medium text-gray-600 mb-1">
                    曲 <span className="font-normal text-gray-400">（登録済み / iTunes から選ぶ・無ければそのまま入力）</span>
                  </label>
                  <SongSearchInput
                    value={draft.songName}
                    onChange={(name) =>
                      // 検索から選ばずに打ち替えたなら「別の曲（未登録）」の指摘。
                      // 曲名の表記そのものを直したい場合は曲ページから直す
                      setDraft((d) => ({ ...d, songName: name, songId: '', itunesId: null, artUrl: null }))
                    }
                    onSelectSong={(song) =>
                      setDraft((d) => ({
                        ...d,
                        songId: song.id,
                        songName: song.name,
                        artist: song.original_artist,
                        itunesId: song.itunes_ids?.[0]?.itunes_id ?? null,
                        artUrl: song.arts ?? null,
                      }))
                    }
                    placeholder="曲名で検索"
                  />
                  {(songChanged || (missingMode && draft.songName.trim() !== '')) && (
                    <p className="mt-1 text-[11px] text-amber-700">
                      {draft.songId
                        ? missingMode
                          ? '登録済みの曲として追加します'
                          : 'この歌唱を別の曲へ繋ぎ替えます'
                        : 'この曲名は未登録です。承認時に新しい曲として登録されます'}
                    </p>
                  )}
                </div>

                <div>
                  <label className="block text-xs font-medium text-gray-600 mb-1">原曲アーティスト</label>
                  <ArtistSearchInput
                    value={draft.artist}
                    onChange={(artist) => setDraft((d) => ({ ...d, artist }))}
                    onSelectArtist={(a) => setDraft((d) => ({ ...d, artist: a.name }))}
                    placeholder="アーティスト名で検索"
                  />
                  {artistChanged && (
                    <p className="mt-1 text-[11px] text-amber-700">
                      アーティストは曲の属性です。直すとこの曲の歌唱すべてに反映されます
                    </p>
                  )}
                </div>

                <VocalPicker
                  selected={draft.singerIds}
                  participants={stream?.participants ?? []}
                  channelOwner={stream?.channel_owner}
                  current={target?.singers ?? []}
                  onToggle={(id) =>
                    setDraft((d) => ({
                      ...d,
                      singerIds: d.singerIds.includes(id)
                        ? d.singerIds.filter((x) => x !== id)
                        : [...d.singerIds, id],
                    }))
                  }
                />
              </div>
            )}

            {!canSubmit && (
              <div className="rounded-lg bg-white p-3">
                <LoginToSuggest message="誤りの報告にはログインが必要です。" />
              </div>
            )}

            {canSubmit && (
              <input
                type="text"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                placeholder={canEdit ? 'メモ（任意）' : '報告の理由（任意）'}
                className="w-full px-3 py-2 text-sm bg-white/10 border border-white/20 rounded-lg placeholder:text-gray-500 focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
            )}

            {error && <p className="text-xs text-red-400">{error}</p>}

            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-400 min-w-0 truncate">
                {changed
                  ? `送信する内容：${summary.join('、')}`
                  : missingMode
                    ? '曲名を入れると送れます（管理者が確認して追加します）'
                    : canEdit
                      ? '直すと即座に反映されます'
                      : '管理者への報告として送られます'}
              </span>
              <button
                type="button"
                onClick={close}
                disabled={busy}
                className="ml-auto px-4 py-2 text-sm border border-white/20 rounded-lg hover:bg-white/10 disabled:opacity-50"
              >
                キャンセル
              </button>
              <button
                type="button"
                onClick={handleSubmit}
                disabled={busy || !changed || !canSubmit}
                className="px-4 py-2 text-sm bg-indigo-600 font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-40"
              >
                {busy ? '送信中...' : missingMode || !canEdit ? '報告を送信' : '保存'}
              </button>
            </div>
            </div>
          </div>

          {/* 右：この配信の曲（対象の切り替え） */}
          <div className="shrink-0 lg:w-80 border-t lg:border-t-0 lg:border-l border-white/10 bg-white text-gray-900 flex flex-col max-h-[30vh] lg:max-h-none">
            <SetlistStrip
              performances={performances}
              currentId={performanceId}
              onSelect={selectTarget}
              onAddMissing={() => setEditing({ streamId, performanceId: null })}
            />
          </div>
        </div>
      </div>
    </PlayerScopeContext>
  );
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

// 再生の操作。**ダイアログが再生バーを覆うので、ここに無いと聴きながら
// 直せない**（区間の外を聴きにいくのがこの画面の目的なので、±5 秒も置く）。
function Transport({ currentTime }: { currentTime: number | null }) {
  const playing = usePlayerStore((s) => s.playing);
  const setPlaying = usePlayerStore((s) => s.setPlaying);

  const nudge = (delta: number) => {
    if (currentTime == null) return;
    playerSeekTo('bar', Math.max(0, currentTime + delta));
  };

  return (
    <div className="mt-2 flex items-center gap-2">
      <button
        type="button"
        onClick={() => nudge(-5)}
        className="px-2 py-1 text-xs rounded-lg text-gray-300 hover:bg-white/10"
        title="5秒戻す"
      >
        -5s
      </button>
      <button
        type="button"
        onClick={() => setPlaying(!playing)}
        className="p-2 bg-indigo-600 rounded-full hover:bg-indigo-700"
        title={playing ? '一時停止' : '再生'}
        aria-label={playing ? '一時停止' : '再生'}
      >
        {playing ? (
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" />
          </svg>
        ) : (
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        )}
      </button>
      <button
        type="button"
        onClick={() => nudge(5)}
        className="px-2 py-1 text-xs rounded-lg text-gray-300 hover:bg-white/10"
        title="5秒進める"
      >
        +5s
      </button>
      <span className="font-mono text-sm text-gray-200">
        {currentTime != null ? formatTimeInput(Math.floor(currentTime)) : '--:--'}
      </span>
      <span className="text-xs text-gray-500 truncate min-w-0">
        区間の終わりで止まりません。前後どこでも聴けます
      </span>
    </div>
  );
}

// ボーカル。**並べて押すのが主で、検索は例外**（歌枠のボーカルはほぼ必ず
// 配信の参加者の中に居る）。選択済みなのに参加者に居ない歌手も並べる ──
// 落とすと、値は送られるのに画面に出ないという最悪の形になる。
function VocalPicker({
  selected,
  participants,
  channelOwner,
  current,
  onToggle,
}: {
  selected: string[];
  participants: Singer[];
  channelOwner?: Singer;
  current: Singer[];
  onToggle: (id: string) => void;
}) {
  const options = useMemo(() => {
    const byId = new Map<string, Singer>();
    if (channelOwner) byId.set(channelOwner.id, channelOwner);
    for (const s of participants) if (!byId.has(s.id)) byId.set(s.id, s);
    for (const s of current) if (!byId.has(s.id)) byId.set(s.id, s);
    return [...byId.values()];
  }, [participants, channelOwner, current]);

  if (options.length === 0) return null;

  return (
    <div>
      <label className="block text-xs font-medium text-gray-600 mb-1">歌った人</label>
      <div className="flex flex-wrap gap-1.5">
        {options.map((singer) => {
          const on = selected.includes(singer.id);
          return (
            <button
              key={singer.id}
              type="button"
              onClick={() => onToggle(singer.id)}
              aria-pressed={on}
              title={singer.name}
              className={`inline-flex items-center gap-1.5 pl-1 pr-2.5 py-1 rounded-full border text-sm transition-colors ${
                on
                  ? 'bg-indigo-100 border-indigo-300 text-indigo-800'
                  : 'bg-white border-gray-200 text-gray-500 hover:border-indigo-300 hover:bg-indigo-50'
              }`}
            >
              {singer.photo_url ? (
                <img src={singer.photo_url} alt="" className={`w-5 h-5 rounded-full ${on ? '' : 'opacity-60'}`} />
              ) : (
                <span className="w-5 h-5 rounded-full bg-gray-200" />
              )}
              <span className="max-w-[9rem] truncate">{singer.name}</span>
              {/* 色だけに頼らない */}
              {on && <span aria-hidden="true">✓</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// 時刻 1 つの入力欄（手入力 ＋ 今の再生位置の取り込み ＋ そこから試聴）
function TimeField({
  label,
  value,
  original,
  currentTime,
  allowEmpty = false,
  onChange,
}: {
  label: string;
  value: number;
  original: number;
  currentTime: number | null;
  allowEmpty?: boolean;
  onChange: (v: number) => void;
}) {
  const [text, setText] = useState(value === 0 && allowEmpty ? '' : formatTimeInput(value));
  // 時間軸のドラッグで値が変わったら入力欄も追従する
  const [shownFor, setShownFor] = useState(value);
  if (shownFor !== value) {
    setShownFor(value);
    setText(value === 0 && allowEmpty ? '' : formatTimeInput(value));
  }

  const commit = () => {
    if (allowEmpty && text.trim() === '') {
      onChange(0);
      return;
    }
    const parsed = parseSeconds(text);
    if (parsed === null) {
      setText(value === 0 && allowEmpty ? '' : formatTimeInput(value)); // 不正なら戻す
      return;
    }
    onChange(parsed);
  };

  return (
    <div>
      <div className="flex items-center gap-1.5">
        <span className="text-xs text-gray-500 w-7 shrink-0">{label}</span>
        <input
          type="text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commit();
            }
          }}
          placeholder={allowEmpty ? '空欄=最後まで' : '0:00'}
          className="w-24 px-2 py-1 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        <button
          type="button"
          onClick={() => currentTime != null && onChange(Math.round(currentTime))}
          disabled={currentTime == null}
          className="px-2 py-1 text-xs bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 disabled:opacity-40"
          title={`今の再生位置を${label}にする`}
        >
          今ここ
        </button>
        <button
          type="button"
          onClick={() => playerSeekTo('bar', label === '終了' ? Math.max(0, value - 3) : value)}
          disabled={value === 0}
          className="px-1.5 py-1 text-red-600 bg-red-50 rounded-lg hover:bg-red-100 disabled:opacity-40"
          title={label === '終了' ? '終了の3秒前から再生' : 'ここから再生'}
        >
          <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M8 5v14l11-7z" />
          </svg>
        </button>
      </div>
      {value !== original && (
        <p className="mt-0.5 ml-8 text-[11px] font-mono text-indigo-600">
          {original === 0 ? '最後まで' : formatTimeInput(original)} → {value === 0 ? '最後まで' : formatTimeInput(value)}
        </p>
      )}
    </div>
  );
}
