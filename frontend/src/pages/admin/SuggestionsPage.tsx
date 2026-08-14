import { useCallback, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { performanceApi, songApi, streamApi, suggestionApi } from '../../api/client';
import type {
  MissingSongPayload,
  Suggestion,
  SuggestionGroup,
  SuggestionKind,
  SuggestionStatus,
} from '../../api/types';
import Loading from '../../components/ui/Loading';
import Pagination from '../../components/ui/Pagination';
import MergeSuggestionsDialog from '../../components/MergeSuggestionsDialog';
import SetlistReviewCard from '../../components/SetlistReviewCard';
import YoutubePlayer from '../../components/YoutubePlayer';
import { useToast } from '../../components/ui/ToastContext';
import {
  FIELD_LABELS,
  TYPE_LABELS,
  changedKeysOf,
  detailPathOf,
  formatFieldValue,
  isActionable,
} from '../../components/suggestionDisplay';
import { OverlapWarning, SuggestionChanges } from '../../components/SuggestionChanges';
import AutoApplySettingsPanel from '../../components/AutoApplySettingsPanel';
import { formatSeconds } from '../../components/usePerformanceTiming';
import { youtubePlayerSeekTo } from '../../components/youtubePlayerControl';

const STATUS_TABS: { value: SuggestionStatus; label: string }[] = [
  { value: 'pending', label: '未処理' },
  { value: 'conflict', label: '要確認' },
  { value: 'approved', label: '承認済み' },
  { value: 'rejected', label: '却下' },
];

// 種別の絞り込み。一括セットリスト作成は一度に数百件積むことがあり、
// 何もしないと利用者から届いた提案がその中に埋もれる。画面を分けずに絞りで解く
// ── 分けると「どちらを見ればいいか」が増えるうえ、同じ配信の中で
// 「曲を足す」と「時間を直す」が別の画面に散る。
const KIND_TABS: { value: SuggestionKind | ''; label: string }[] = [
  { value: '', label: 'すべて' },
  { value: 'perf.missing', label: 'セットリスト追加' },
  { value: 'field', label: '内容の修正' },
  { value: 'perf.meta', label: '曲の差し替え' },
];

// 未処理・要確認は「対象ごとにまとめて見比べる」のが実際の作業。
// 承認済み・却下は履歴なので時系列の一覧のまま。
const GROUPED_TABS: SuggestionStatus[] = ['pending', 'conflict'];

const keyOf = (g: SuggestionGroup) => `${g.target_type}:${g.target_id}:${g.target_key}`;

export default function SuggestionsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [status, setStatus] = useState<SuggestionStatus>('pending');
  const [kind, setKind] = useState<SuggestionKind | ''>('');
  const [page, setPage] = useState(1);
  // どのグループのプレイヤーを開いているか。
  // undefined = まだ触っていない（下で既定を決める）／null = 利用者が閉じた。
  //
  // **同時に 1 つしか開かない。** YT プレイヤーの参照はグローバルに 1 つなので
  // （youtubePlayerControl）、複数開くと「今の再生位置を取り込む」が
  // 最後に載った別の動画を指してしまう。動画を何十本も読み込まない利点もある。
  const [openKey, setOpenKey] = useState<string | null | undefined>(undefined);
  const grouped = GROUPED_TABS.includes(status);

  const groupQuery = useQuery({
    queryKey: ['suggestions', 'grouped', status, kind, page],
    queryFn: () => suggestionApi.listGrouped(status, page, 20, kind || undefined),
    enabled: grouped,
  });
  const listQuery = useQuery({
    queryKey: ['suggestions', status, kind, page],
    queryFn: () => suggestionApi.list(status, page, 20, kind || undefined),
    enabled: !grouped,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['suggestions'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
    queryClient.invalidateQueries({ queryKey: ['artists'] });
    queryClient.invalidateQueries({ queryKey: ['streams'] });
    queryClient.invalidateQueries({ queryKey: ['performances'] });
  };

  // 1件を採用する。同じフィールドを触る他の提案は同時に却下する
  // （同じ項目に対する異なる値は両立しないため、残しても必ず衝突になる）。
  const adoptMutation = useMutation({
    mutationFn: async ({ pick, siblings }: { pick: Suggestion; siblings: Suggestion[] }) => {
      await suggestionApi.approve(pick.id, !!pick.conflicts && Object.keys(pick.conflicts).length > 0);
      const supersededIDs = siblings.map((s) => s.id);
      if (supersededIDs.length > 0) {
        await suggestionApi.batchReview(supersededIDs, 'reject', {
          note: '同じ項目の別の提案を採用したため',
        });
      }
      return supersededIDs.length;
    },
    onSuccess: (superseded) => {
      showToast(
        superseded > 0 ? `提案を反映しました（重複する${superseded}件は却下）` : '提案を反映しました',
        'success'
      );
      invalidate();
    },
    onError: (err: Error) => {
      showToast(`反映できません: ${err.message}`, 'error');
      invalidate();
    },
  });

  // 未登録曲の追加は「審査担当が画面で直した内容」を添えて承認する
  // （曲の差し替え・時間の微調整・歌手の選択を 1 往復で済ませる）。
  const approveMissingMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: MissingSongPayload }) =>
      suggestionApi.approve(id, false, payload),
    onSuccess: () => {
      showToast('歌唱記録を作成しました', 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`登録できません: ${err.message}`, 'error'),
  });

  const rejectMissingMutation = useMutation({
    mutationFn: ({ id, note, notThisSong }: { id: string; note: string; notThisSong: boolean }) =>
      suggestionApi.reject(id, note, notThisSong),
    onSuccess: (_r, v) => {
      showToast(
        v.notThisSong ? '否決を記録しました（次回は同じ組を提案しません）' : '却下しました',
        'success'
      );
      invalidate();
    },
    onError: (err: Error) => showToast(`却下できません: ${err.message}`, 'error'),
  });

  // 却下は「次から提案しない」という持続する副作用を持つので、取り消せる必要がある。
  const undoRejectionMutation = useMutation({
    mutationFn: (id: string) => suggestionApi.undoRejection(id),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`取り消せません: ${err.message}`, 'error'),
  });

  const batchRejectMutation = useMutation({
    mutationFn: (ids: string[]) => suggestionApi.batchReview(ids, 'reject'),
    onSuccess: (r) => {
      showToast(`${r.succeeded}件を却下しました`, 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`却下失敗: ${err.message}`, 'error'),
  });

  const busy =
    adoptMutation.isPending ||
    batchRejectMutation.isPending ||
    approveMissingMutation.isPending ||
    rejectMissingMutation.isPending ||
    undoRejectionMutation.isPending;

  // 絞りを変えたら開きかけのプレイヤーも畳む（別の配信を指したままにしない）
  const resetView = () => {
    setPage(1);
    setOpenKey(undefined);
  };
  const handleStatusChange = (s: SuggestionStatus) => {
    setStatus(s);
    resetView();
  };
  const handleKindChange = (k: SuggestionKind | '') => {
    setKind(k);
    resetView();
  };

  const isLoading = grouped ? groupQuery.isLoading : listQuery.isLoading;
  const pagination = grouped ? groupQuery.data?.pagination : listQuery.data?.pagination;
  const groups = groupQuery.data?.groups ?? [];
  const isEmpty = grouped ? groups.length === 0 : !listQuery.data || listQuery.data.suggestions.length === 0;

  // 未登録曲の追加は再生して確かめてからでないと判断できないので、
  // 先頭にそれがあれば最初から開いておく。値を見比べるだけの提案は開かない
  // （一覧を素早く流し見る作業を邪魔しないため）。
  const autoOpen = groups.find((g) => g.suggestions.some((s) => s.kind === 'perf.missing'));
  const effectiveOpenKey = openKey === undefined ? (autoOpen ? keyOf(autoOpen) : null) : openKey;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">修正提案のレビュー</h1>
        <p className="text-gray-500 mt-1 text-sm">
          利用者から届いた修正案と、一括セットリスト作成が決めきれなかった曲です。同じ対象への提案はまとめて表示されます。
          再生して確かめてから判断できるよう、対象ごとにプレイヤーを開けます。
        </p>
      </div>

      <AutoApplySettingsPanel />

      <div className="flex flex-wrap gap-3">
        {/* Status tabs */}
        <div className="inline-flex rounded-lg border border-gray-300 overflow-hidden text-sm">
          {STATUS_TABS.map((tab, i) => (
            <button
              key={tab.value}
              onClick={() => handleStatusChange(tab.value)}
              className={`px-4 py-2 transition-colors ${i > 0 ? 'border-l border-gray-300' : ''} ${
                status === tab.value ? 'bg-indigo-600 text-white' : 'bg-white text-gray-600 hover:bg-gray-50'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Kind tabs */}
        <div className="inline-flex rounded-lg border border-gray-300 overflow-hidden text-sm">
          {KIND_TABS.map((tab, i) => (
            <button
              key={tab.value || 'all'}
              onClick={() => handleKindChange(tab.value)}
              className={`px-3 py-2 transition-colors ${i > 0 ? 'border-l border-gray-300' : ''} ${
                kind === tab.value ? 'bg-gray-700 text-white' : 'bg-white text-gray-600 hover:bg-gray-50'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {isLoading ? (
        <Loading />
      ) : isEmpty ? (
        <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
          {status === 'pending' ? '未処理の提案はありません' : '該当する提案はありません'}
        </div>
      ) : (
        <>
          <div className="space-y-4">
            {grouped
              ? groups.map((g) => (
                  <GroupCard
                    key={keyOf(g)}
                    group={g}
                    busy={busy}
                    open={effectiveOpenKey === keyOf(g)}
                    onToggleOpen={() => setOpenKey(effectiveOpenKey === keyOf(g) ? null : keyOf(g))}
                    onAdopt={(pick, siblings) => adoptMutation.mutate({ pick, siblings })}
                    onApproveMissing={(id, payload) => approveMissingMutation.mutate({ id, payload })}
                    onRejectMissing={(id, note, notThisSong) =>
                      rejectMissingMutation.mutate({ id, note, notThisSong })
                    }
                    onRejectAll={(ids) => batchRejectMutation.mutate(ids)}
                    onMerged={invalidate}
                  />
                ))
              : listQuery.data!.suggestions.map((s) => (
                  <HistoryCard
                    key={s.id}
                    suggestion={s}
                    busy={busy}
                    onUndoRejection={() => undoRejectionMutation.mutate(s.id)}
                  />
                ))}
          </div>

          {pagination && pagination.total_pages > 1 && (
            <Pagination
              page={page}
              totalPages={pagination.total_pages}
              onPageChange={(p) => {
                setPage(p);
                setOpenKey(undefined);
              }}
            />
          )}
        </>
      )}
    </div>
  );
}

// GroupCard 同一対象に届いた提案をまとめて見比べるカード。
// 現在値を基準に各提案の差分を並べ、1つ採用すると同じ項目の他案は却下される。
//
// 開くと対象を再生できる。どの提案も「本当にそうなっているか」は録画を見ないと
// 分からない ── 時間のズレも、曲名が別の曲を指していないかも、耳で確かめる話になる。
function GroupCard({
  group,
  busy,
  open,
  onToggleOpen,
  onAdopt,
  onApproveMissing,
  onRejectMissing,
  onRejectAll,
  onMerged,
}: {
  group: SuggestionGroup;
  busy: boolean;
  open: boolean;
  onToggleOpen: () => void;
  onAdopt: (pick: Suggestion, siblings: Suggestion[]) => void;
  onApproveMissing: (id: string, payload: MissingSongPayload) => void;
  onRejectMissing: (id: string, note: string, notThisSong: boolean) => void;
  onRejectAll: (ids: string[]) => void;
  onMerged: () => void;
}) {
  const { suggestions, current } = group;
  const multiple = suggestions.length > 1;
  const [merging, setMerging] = useState(false);

  const playback = usePlaybackSource(group, open);

  // 現在値のうち、どれかの提案が触っている項目だけを見出しに出す
  const touched = [...new Set(suggestions.flatMap(changedKeysOf))];
  // 統合は「値を突き合わせて決める」操作なので、差分を持つ提案が複数あるときだけ意味がある
  const canMerge = touched.length > 0 && suggestions.filter((s) => changedKeysOf(s).length > 0).length > 1;

  return (
    <div className="bg-white rounded-lg shadow-sm border p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-600 mr-2">
            {TYPE_LABELS[group.target_type] ?? group.target_type}
          </span>
          <Link
            to={detailPathOf(group.target_type, group.target_id, group.target_key)}
            className="text-indigo-600 hover:text-indigo-900 font-medium break-words"
          >
            {group.target_label}
          </Link>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {multiple && (
            <span className="text-xs font-medium text-amber-700 bg-amber-50 px-2 py-0.5 rounded">
              {suggestions.length}件の提案
            </span>
          )}
          {playback.canPlay && (
            <button
              onClick={onToggleOpen}
              className={`px-2 py-1 text-xs rounded-lg border ${
                open
                  ? 'bg-indigo-50 border-indigo-300 text-indigo-700'
                  : 'bg-white border-gray-300 text-gray-600 hover:bg-gray-50'
              }`}
              title={open ? 'プレイヤーを閉じる' : '再生して確かめる'}
            >
              {open ? '閉じる' : '▶ 再生'}
            </button>
          )}
        </div>
      </div>

      {/* 現在値：提案を見比べるときの基準 */}
      {touched.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500">
          <span className="text-gray-400">現在</span>
          {touched.map((k) => (
            <span key={k}>
              {FIELD_LABELS[k] ?? k}{' '}
              <span className="font-mono text-gray-700">{formatFieldValue(k, current[k] ?? '')}</span>
            </span>
          ))}
        </div>
      )}

      {open && playback.videoId && (
        <div className="mt-3 space-y-3">
          <div className="aspect-video w-full bg-black rounded overflow-hidden">
            {/* key で作り直す。YoutubePlayer は一度初期化すると videoId の変更を拾わない */}
            <YoutubePlayer
              key={playback.videoId}
              videoId={playback.videoId}
              onReady={playback.onReady}
            />
          </div>
          {playback.existing.length > 0 && (
            <details className="rounded-lg border p-3" open>
              <summary className="text-sm font-medium text-gray-700 cursor-pointer">
                この配信の既存セットリスト（{playback.existing.length} 曲）
              </summary>
              <ul className="mt-2 space-y-1">
                {playback.existing.map((p) => (
                  <li key={p.id} className="flex items-center gap-2 text-xs">
                    <button
                      onClick={() => youtubePlayerSeekTo(p.start_seconds)}
                      className="font-mono text-indigo-600 hover:text-indigo-900"
                      title="ここから再生"
                    >
                      {formatSeconds(p.start_seconds)}
                    </button>
                    <span className="text-gray-700">{p.song_name}</span>
                    {p.original_artist && <span className="text-gray-400">/ {p.original_artist}</span>}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}

      <div className="mt-3 divide-y">
        {suggestions.map((s) =>
          s.kind === 'perf.missing' ? (
            <div key={s.id} className="py-2.5">
              <SetlistReviewCard
                suggestion={s}
                participants={playback.participants}
                busy={busy}
                onApprove={onApproveMissing}
                onReject={onRejectMissing}
              />
            </div>
          ) : (
            <SuggestionRow
              key={s.id}
              suggestion={s}
              busy={busy}
              onAdopt={() => {
                // 同じ項目を触る他の提案は両立しないので一緒に却下する
                const keys = new Set(changedKeysOf(s));
                const siblings = suggestions.filter(
                  (o) => o.id !== s.id && changedKeysOf(o).some((k) => keys.has(k))
                );
                onAdopt(s, siblings);
              }}
            />
          )
        )}
      </div>

      <div className="flex justify-end gap-2 mt-3">
        {canMerge && (
          <button
            onClick={() => setMerging(true)}
            disabled={busy}
            className="px-3 py-1.5 text-xs bg-white border border-indigo-300 text-indigo-700 rounded-lg hover:bg-indigo-50 disabled:opacity-50"
            title="値を見比べて1つに決める（中央値や、誰も出していない値も選べます）"
          >
            まとめて反映
          </button>
        )}
        <button
          onClick={() => onRejectAll(suggestions.map((s) => s.id))}
          disabled={busy}
          className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-50"
        >
          {multiple ? 'すべて却下' : '却下'}
        </button>
      </div>

      {merging && (
        <MergeSuggestionsDialog group={group} onClose={() => setMerging(false)} onDone={onMerged} />
      )}
    </div>
  );
}

// usePlaybackSource は対象をどう再生するかを解決する。
//
// 対象の種類ごとに「どの動画の何秒か」の求め方が違う：
//
//	stream      … 対象そのものが動画。参加者と既存セットリストも要る
//	performance … その歌唱の配信を開き、開始秒へ飛ぶ
//	song        … 歌唱履歴から 1 つ選んで聴く（どれでも同じ曲なので最新を使う）
//	artist      … 未対応（アーティストから歌唱を辿る経路がまだ無い）
//
// 開いているときだけ問い合わせる。一覧を表示しただけで何十本もの配信を
// 読みに行かないため。
function usePlaybackSource(group: SuggestionGroup, open: boolean) {
  const isStream = group.target_type === 'stream';
  const isPerformance = group.target_type === 'performance';
  const isSong = group.target_type === 'song';

  // 配信は**開いていなくても**引く。参加者のチェックボックスがこれに依存していて、
  // 開いたときだけ引くと「一度も開いていない組では歌手を選べない」ことになる
  // ── multi_singer の行がまさにそれで、歌手を選ばないまま登録すると
  // vocalist が空の歌唱ができる（この機能が防ぎたかったもの）。
  const { data: stream } = useQuery({
    queryKey: ['streams', group.target_key],
    queryFn: () => streamApi.get(group.target_key),
    enabled: isStream,
  });
  const { data: perf } = useQuery({
    queryKey: ['performances', group.target_id],
    queryFn: () => performanceApi.get(group.target_id),
    enabled: open && isPerformance,
  });
  const { data: songPerfs } = useQuery({
    queryKey: ['songs', group.target_id, 'performances', 'review'],
    queryFn: () => songApi.getPerformances(group.target_id, 1, 1),
    enabled: open && isSong,
  });

  let videoId = '';
  let seekTo: number | null = null;
  if (isStream) {
    videoId = group.target_key;
  } else if (isPerformance && perf) {
    videoId = perf.stream_id;
    seekTo = perf.start_seconds;
  } else if (isSong && songPerfs?.performances?.length) {
    const p = songPerfs.performances[0];
    videoId = p.stream_id;
    seekTo = p.start_seconds;
  }

  // 目的の位置から始める。プレイヤーが用意できた時点で飛ばす
  // （読み込み前に seek しても効かないので onReady で行う）。
  const onReady = useCallback(() => {
    if (seekTo !== null && seekTo > 0) {
      youtubePlayerSeekTo(seekTo);
    }
  }, [seekTo]);

  return {
    canPlay: isStream || isPerformance || isSong,
    videoId,
    onReady,
    participants: stream?.participants ?? [],
    existing: isStream ? (stream?.performances ?? []) : [],
  };
}

function SuggestionRow({
  suggestion,
  busy,
  onAdopt,
}: {
  suggestion: Suggestion;
  busy: boolean;
  onAdopt: () => void;
}) {
  const { conflicts } = suggestion;
  const hasConflict = !!conflicts && Object.keys(conflicts).length > 0;

  return (
    <div className={`py-2.5 ${hasConflict ? 'bg-amber-50/50 -mx-2 px-2 rounded' : ''}`}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-xs text-gray-400 shrink-0" title={suggestion.created_by_name ? '登録ユーザー' : '未ログイン'}>
          {suggestion.created_by_name || '匿名'}
        </span>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0 flex-1">
          <SuggestionChanges suggestion={suggestion} />
        </div>

        <button
          onClick={onAdopt}
          disabled={busy || !isActionable(suggestion)}
          className={`shrink-0 px-3 py-1 text-xs text-white font-medium rounded-lg disabled:opacity-50 ${
            hasConflict ? 'bg-amber-600 hover:bg-amber-700' : 'bg-indigo-600 hover:bg-indigo-700'
          }`}
        >
          {hasConflict
            ? '上書きして採用'
            : suggestion.kind === 'perf.meta'
              ? '差し替える'
              : 'これを採用'}
        </button>
      </div>

      <OverlapWarning overlaps={suggestion.overlaps} />

      {hasConflict && (
        <p className="text-xs text-amber-800 mt-1">
          ⚠ 提案後に対象が変更されています（
          {Object.entries(conflicts!)
            .map(([k, c]) => `${FIELD_LABELS[k] ?? k}: 提案時 ${formatFieldValue(k, c.expected)} → 現在 ${formatFieldValue(k, c.current)}`)
            .join('、')}
          ）
        </p>
      )}

      {suggestion.note && (
        <p className="text-xs text-gray-600 mt-1 whitespace-pre-wrap break-words">💬 {suggestion.note}</p>
      )}
    </div>
  );
}

// HistoryCard 承認済み・却下の履歴表示（時系列の一覧）。
//
// 未登録曲の却下だけは取り消せる。一括は同じ (配信, 開始秒, 曲名) を status に関係なく
// 積み直さないので、却下した行はそのままだと次の実行で二度と出てこない
// ── 判断を変えたときに戻す口がここ。
function HistoryCard({
  suggestion,
  busy,
  onUndoRejection,
}: {
  suggestion: Suggestion;
  busy: boolean;
  onUndoRejection: () => void;
}) {
  const canUndo = suggestion.status === 'rejected' && suggestion.kind === 'perf.missing';
  return (
    <div className="bg-white rounded-lg shadow-sm border p-4">
      <div className="flex flex-wrap items-start justify-between gap-2 mb-2">
        <div className="min-w-0">
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-600 mr-2">
            {TYPE_LABELS[suggestion.target_type] ?? suggestion.target_type}
          </span>
          <Link
            to={detailPathOf(suggestion.target_type, suggestion.target_id, suggestion.target_key)}
            className="text-indigo-600 hover:text-indigo-900 font-medium break-words"
          >
            {suggestion.target_label}
          </Link>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-xs text-gray-400">{suggestion.created_by_name || '匿名'}</span>
          <span
            className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
              suggestion.status === 'approved' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'
            }`}
          >
            {suggestion.status === 'approved' ? '承認済み' : '却下'}
            {suggestion.reviewed_at && ` · ${new Date(suggestion.reviewed_at).toLocaleDateString('ja-JP')}`}
          </span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <SuggestionChanges suggestion={suggestion} />
      </div>

      {suggestion.review_note && (
        <p className="text-xs text-gray-400 mt-1.5">{suggestion.review_note}</p>
      )}
      {suggestion.note && (
        <p className="text-xs text-gray-600 mt-1 whitespace-pre-wrap break-words">💬 {suggestion.note}</p>
      )}

      {canUndo && (
        <div className="flex justify-end mt-2">
          <button
            onClick={onUndoRejection}
            disabled={busy}
            title="この却下を取り消します。次回の一括作成でまた提案され、「この曲ではない」の記録も消えます"
            className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            却下を取り消す
          </button>
        </div>
      )}
    </div>
  );
}
