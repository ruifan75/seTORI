import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { suggestionApi } from '../../api/client';
import type { Suggestion, SuggestionGroup, SuggestionStatus } from '../../api/types';
import Loading from '../../components/ui/Loading';
import Pagination from '../../components/ui/Pagination';
import { useToast } from '../../components/ui/ToastContext';

const STATUS_TABS: { value: SuggestionStatus; label: string }[] = [
  { value: 'pending', label: '未処理' },
  { value: 'conflict', label: '要確認' },
  { value: 'approved', label: '承認済み' },
  { value: 'rejected', label: '却下' },
];

// 未処理・要確認は「対象ごとにまとめて見比べる」のが実際の作業。
// 承認済み・却下は履歴なので時系列の一覧のまま。
const GROUPED_TABS: SuggestionStatus[] = ['pending', 'conflict'];

// フィールドキー → 日本語ラベル（差分表示用）
const FIELD_LABELS: Record<string, string> = {
  name: '名前',
  name_reading: '読み',
  original_artist: 'アーティスト',
  original_artist_reading: 'アーティストの読み',
  start_seconds: '開始時間',
  end_seconds: '終了時間',
};

const TYPE_LABELS: Record<string, string> = {
  song: '楽曲',
  artist: 'アーティスト',
  performance: '歌唱',
  stream: '曲の追加',
};

// 秒数フィールドは M:SS / H:MM:SS でも見せる（6714 だけでは判断できないため）
const TIME_FIELDS = new Set(['start_seconds', 'end_seconds']);

function formatFieldValue(key: string, value: string): string {
  if (!TIME_FIELDS.has(key)) return value || '（空）';
  const n = Number(value);
  if (!Number.isFinite(n)) return value || '（空）';
  if (n === 0) return '最後まで';
  const h = Math.floor(n / 3600);
  const m = Math.floor((n % 3600) / 60);
  const s = n % 60;
  const clock =
    h > 0
      ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
      : `${m}:${String(s).padStart(2, '0')}`;
  return `${clock}（${n}s）`;
}

// この提案が実際に変更するフィールド。
// 未登録曲の追加は既存レコードを触らないので、差分としては空になる（payload 側で表示する）。
function changedKeysOf(s: Suggestion): string[] {
  return Object.keys(s.after).filter((k) => (s.after[k] ?? '') !== (s.before[k] ?? ''));
}

// この提案が処理可能か（差分か payload のどちらかがある）
function isActionable(s: Suggestion): boolean {
  return s.kind === 'perf.missing' ? !!s.payload : changedKeysOf(s).length > 0;
}

function detailPathOf(targetType: string, targetID: string, targetKey: string): string {
  if (targetType === 'song') return `/songs/${targetID}`;
  if (targetType === 'artist') return `/artists/${targetID}`;
  if (targetType === 'stream') return `/streams/${targetKey}`;
  return '/songs'; // 歌唱は単独ページを持たない（対象名に配信が入っている）
}

export default function SuggestionsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [status, setStatus] = useState<SuggestionStatus>('pending');
  const [page, setPage] = useState(1);
  const grouped = GROUPED_TABS.includes(status);

  const groupQuery = useQuery({
    queryKey: ['suggestions', 'grouped', status, page],
    queryFn: () => suggestionApi.listGrouped(status, page, 20),
    enabled: grouped,
  });
  const listQuery = useQuery({
    queryKey: ['suggestions', status, page],
    queryFn: () => suggestionApi.list(status, page, 20),
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

  const batchRejectMutation = useMutation({
    mutationFn: (ids: string[]) => suggestionApi.batchReview(ids, 'reject'),
    onSuccess: (r) => {
      showToast(`${r.succeeded}件を却下しました`, 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`却下失敗: ${err.message}`, 'error'),
  });

  const singleMutation = useMutation({
    mutationFn: ({ id, action, force }: { id: string; action: 'approve' | 'reject'; force: boolean }) =>
      action === 'approve' ? suggestionApi.approve(id, force) : suggestionApi.reject(id),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (err: Error) => {
      showToast(`処理できません: ${err.message}`, 'error');
      invalidate();
    },
  });

  const busy = adoptMutation.isPending || batchRejectMutation.isPending || singleMutation.isPending;

  const handleStatusChange = (s: SuggestionStatus) => {
    setStatus(s);
    setPage(1);
  };

  const isLoading = grouped ? groupQuery.isLoading : listQuery.isLoading;
  const pagination = grouped ? groupQuery.data?.pagination : listQuery.data?.pagination;
  const isEmpty = grouped
    ? !groupQuery.data || groupQuery.data.groups.length === 0
    : !listQuery.data || listQuery.data.suggestions.length === 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">修正提案のレビュー</h1>
        <p className="text-gray-500 mt-1 text-sm">
          閲覧モードのユーザーから届いた修正案です。同じ対象への提案はまとめて表示されます。
          複数の利用者が同じ時間のズレを指摘した場合は、自動で反映されることがあります。
        </p>
      </div>

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
              ? groupQuery.data!.groups.map((g) => (
                  <GroupCard
                    key={`${g.target_type}:${g.target_id}`}
                    group={g}
                    busy={busy}
                    onAdopt={(pick, siblings) => adoptMutation.mutate({ pick, siblings })}
                    onRejectAll={(ids) => batchRejectMutation.mutate(ids)}
                  />
                ))
              : listQuery.data!.suggestions.map((s) => (
                  <HistoryCard key={s.id} suggestion={s} />
                ))}
          </div>

          {pagination && pagination.total_pages > 1 && (
            <Pagination page={page} totalPages={pagination.total_pages} onPageChange={setPage} />
          )}
        </>
      )}
    </div>
  );
}

// GroupCard 同一対象に届いた提案をまとめて見比べるカード。
// 現在値を基準に各提案の差分を並べ、1つ採用すると同じ項目の他案は却下される。
function GroupCard({
  group,
  busy,
  onAdopt,
  onRejectAll,
}: {
  group: SuggestionGroup;
  busy: boolean;
  onAdopt: (pick: Suggestion, siblings: Suggestion[]) => void;
  onRejectAll: (ids: string[]) => void;
}) {
  const { suggestions, current } = group;
  const multiple = suggestions.length > 1;

  // 現在値のうち、どれかの提案が触っている項目だけを見出しに出す
  const touched = [...new Set(suggestions.flatMap(changedKeysOf))];

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
        {multiple && (
          <span className="text-xs font-medium text-amber-700 bg-amber-50 px-2 py-0.5 rounded shrink-0">
            {suggestions.length}件の提案
          </span>
        )}
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

      <div className="mt-3 divide-y">
        {suggestions.map((s) => (
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
        ))}
      </div>

      <div className="flex justify-end mt-3">
        <button
          onClick={() => onRejectAll(suggestions.map((s) => s.id))}
          disabled={busy}
          className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-50"
        >
          {multiple ? 'すべて却下' : '却下'}
        </button>
      </div>
    </div>
  );
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
  const { before, after, conflicts } = suggestion;
  const changed = changedKeysOf(suggestion);
  const hasConflict = !!conflicts && Object.keys(conflicts).length > 0;

  return (
    <div className={`py-2.5 ${hasConflict ? 'bg-amber-50/50 -mx-2 px-2 rounded' : ''}`}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-xs text-gray-400 shrink-0" title={suggestion.created_by_name ? '登録ユーザー' : '未ログイン'}>
          {suggestion.created_by_name || '匿名'}
        </span>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0 flex-1">
          {suggestion.kind === 'perf.missing' ? (
            <MissingSongSummary suggestion={suggestion} />
          ) : (
            changed.map((k) => (
              <span key={k} className="text-sm flex items-center gap-1.5 flex-wrap">
                <span className="text-gray-500 text-xs">{FIELD_LABELS[k] ?? k}</span>
                <span className="px-1.5 py-0.5 rounded bg-red-50 text-red-700 line-through text-xs break-words">
                  {formatFieldValue(k, before[k])}
                </span>
                <span className="text-gray-400 text-xs">→</span>
                <span className="px-1.5 py-0.5 rounded bg-green-50 text-green-700 font-medium text-xs break-words">
                  {formatFieldValue(k, after[k])}
                </span>
              </span>
            ))
          )}
        </div>

        <button
          onClick={onAdopt}
          disabled={busy || !isActionable(suggestion)}
          className={`shrink-0 px-3 py-1 text-xs text-white font-medium rounded-lg disabled:opacity-50 ${
            hasConflict ? 'bg-amber-600 hover:bg-amber-700' : 'bg-indigo-600 hover:bg-indigo-700'
          }`}
        >
          {suggestion.kind === 'perf.missing' ? '登録する' : hasConflict ? '上書きして採用' : 'これを採用'}
        </button>
      </div>

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

// MissingSongSummary 未登録曲の追加報告の中身（差分ではなく「追加したい内容」）。
function MissingSongSummary({ suggestion }: { suggestion: Suggestion }) {
  const p = suggestion.payload;
  if (!p) return <span className="text-xs text-gray-400">内容が読み取れません</span>;
  return (
    <span className="text-sm flex items-center gap-1.5 flex-wrap">
      <span className="px-1.5 py-0.5 rounded bg-green-50 text-green-700 font-medium text-xs break-words">
        {p.song_name}
        {p.original_artist ? ` / ${p.original_artist}` : ''}
      </span>
      <span className="text-xs text-gray-500 font-mono">
        {formatFieldValue('start_seconds', String(p.start_seconds))}
        {' – '}
        {p.end_seconds === 0 ? '最後まで' : formatFieldValue('end_seconds', String(p.end_seconds))}
      </span>
    </span>
  );
}

// HistoryCard 承認済み・却下の履歴表示（時系列の一覧）。
function HistoryCard({ suggestion }: { suggestion: Suggestion }) {
  const { before, after } = suggestion;
  const changed = changedKeysOf(suggestion);

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
        {suggestion.kind === 'perf.missing' ? (
          <MissingSongSummary suggestion={suggestion} />
        ) : (
          changed.map((k) => (
            <span key={k} className="text-sm flex items-center gap-1.5 flex-wrap">
              <span className="text-gray-500 text-xs">{FIELD_LABELS[k] ?? k}</span>
              <span className="px-1.5 py-0.5 rounded bg-red-50 text-red-700 line-through text-xs">
                {formatFieldValue(k, before[k])}
              </span>
              <span className="text-gray-400 text-xs">→</span>
              <span className="px-1.5 py-0.5 rounded bg-green-50 text-green-700 font-medium text-xs">
                {formatFieldValue(k, after[k])}
              </span>
            </span>
          ))
        )}
      </div>

      {suggestion.review_note && (
        <p className="text-xs text-gray-400 mt-1.5">{suggestion.review_note}</p>
      )}
      {suggestion.note && (
        <p className="text-xs text-gray-600 mt-1 whitespace-pre-wrap break-words">💬 {suggestion.note}</p>
      )}
    </div>
  );
}
