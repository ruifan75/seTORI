import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { suggestionApi } from '../../api/client';
import type { Suggestion, SuggestionStatus } from '../../api/types';
import Loading from '../../components/ui/Loading';
import Pagination from '../../components/ui/Pagination';
import { useToast } from '../../components/ui/ToastContext';

const STATUS_TABS: { value: SuggestionStatus; label: string }[] = [
  { value: 'pending', label: '未処理' },
  { value: 'conflict', label: '要確認' },
  { value: 'approved', label: '承認済み' },
  { value: 'rejected', label: '却下' },
];

// フィールドキー → 日本語ラベル（差分表示用）
const FIELD_LABELS: Record<string, string> = {
  name: '名前',
  name_reading: '読み',
  original_artist: 'アーティスト',
  original_artist_reading: 'アーティストの読み',
  start_seconds: '開始時間',
  end_seconds: '終了時間',
};

const TYPE_LABELS: Record<string, string> = { song: '楽曲', artist: 'アーティスト', performance: '歌唱' };

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

export default function SuggestionsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [status, setStatus] = useState<SuggestionStatus>('pending');
  const [page, setPage] = useState(1);

  const { data, isLoading } = useQuery({
    queryKey: ['suggestions', status, page],
    queryFn: () => suggestionApi.list(status, page, 20),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['suggestions'] });
    queryClient.invalidateQueries({ queryKey: ['suggestions', 'count'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
    queryClient.invalidateQueries({ queryKey: ['artists'] });
    queryClient.invalidateQueries({ queryKey: ['streams'] });
    queryClient.invalidateQueries({ queryKey: ['performances'] });
  };

  const approveMutation = useMutation({
    mutationFn: ({ id, force }: { id: string; force: boolean }) => suggestionApi.approve(id, force),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    // 衝突（409）で止まった場合もサーバーが status を conflict にしているので、一覧を引き直す
    onError: (err: Error) => {
      showToast(`承認できません: ${err.message}`, 'error');
      invalidate();
    },
  });

  const rejectMutation = useMutation({
    mutationFn: (id: string) => suggestionApi.reject(id),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`却下失敗: ${err.message}`, 'error'),
  });

  const handleStatusChange = (s: SuggestionStatus) => {
    setStatus(s);
    setPage(1);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">修正提案のレビュー</h1>
        <p className="text-gray-500 mt-1 text-sm">
          閲覧モードのユーザーから届いた修正案です。承認すると対象へ反映されます。
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
      ) : !data || data.suggestions.length === 0 ? (
        <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
          {status === 'pending' ? '未処理の提案はありません' : '該当する提案はありません'}
        </div>
      ) : (
        <>
          <div className="space-y-4">
            {data.suggestions.map((s) => (
              <SuggestionCard
                key={s.id}
                suggestion={s}
                onApprove={(force) => approveMutation.mutate({ id: s.id, force })}
                onReject={() => rejectMutation.mutate(s.id)}
                busy={approveMutation.isPending || rejectMutation.isPending}
              />
            ))}
          </div>

          {data.pagination.total_pages > 1 && (
            <Pagination page={page} totalPages={data.pagination.total_pages} onPageChange={setPage} />
          )}
        </>
      )}
    </div>
  );
}

function SuggestionCard({
  suggestion,
  onApprove,
  onReject,
  busy,
}: {
  suggestion: Suggestion;
  onApprove: (force: boolean) => void;
  onReject: () => void;
  busy: boolean;
}) {
  const { before, after, conflicts } = suggestion;
  const changedKeys = Object.keys(after).filter((k) => (after[k] ?? '') !== (before[k] ?? ''));
  const hasConflict = !!conflicts && Object.keys(conflicts).length > 0;
  const detailPath =
    suggestion.target_type === 'song'
      ? `/songs/${suggestion.target_id}`
      : suggestion.target_type === 'artist'
        ? `/artists/${suggestion.target_id}`
        : `/songs`; // 歌唱は単独ページを持たないため一覧へ（配信は target_label に含まれる）
  const isOpen = suggestion.status === 'pending' || suggestion.status === 'conflict';

  return (
    <div className={`bg-white rounded-lg shadow-sm border p-4 sm:p-5 ${hasConflict ? 'border-amber-300' : ''}`}>
      <div className="flex flex-wrap items-start justify-between gap-2 mb-3">
        <div className="min-w-0">
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-600 mr-2">
            {TYPE_LABELS[suggestion.target_type] ?? suggestion.target_type}
          </span>
          <Link to={detailPath} className="text-indigo-600 hover:text-indigo-900 font-medium break-words">
            {suggestion.target_label}
          </Link>
        </div>
        <div className="flex items-center gap-2 shrink-0 text-xs text-gray-400">
          <span title={suggestion.created_by_name ? '登録ユーザーからの提案' : '未ログインからの提案'}>
            {suggestion.created_by_name || '匿名'}
          </span>
          <span>·</span>
          <span>{new Date(suggestion.created_at).toLocaleString('ja-JP')}</span>
        </div>
      </div>

      {/* Diff */}
      <div className="space-y-2">
        {changedKeys.length === 0 ? (
          <p className="text-sm text-gray-400">変更なし</p>
        ) : (
          changedKeys.map((k) => (
            <div key={k} className="text-sm flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-2">
              <span className="text-gray-500 sm:w-40 shrink-0">{FIELD_LABELS[k] ?? k}</span>
              <div className="flex items-center gap-2 min-w-0 flex-wrap">
                <span className="px-2 py-0.5 rounded bg-red-50 text-red-700 line-through break-words">
                  {formatFieldValue(k, before[k])}
                </span>
                <span className="text-gray-400">→</span>
                <span className="px-2 py-0.5 rounded bg-green-50 text-green-700 font-medium break-words">
                  {formatFieldValue(k, after[k])}
                </span>
              </div>
            </div>
          ))
        )}
      </div>

      {/* 衝突：提案後に対象が別途編集されている。承認するとその編集を巻き戻す。 */}
      {hasConflict && (
        <div className="mt-3 rounded border border-amber-300 bg-amber-50 p-3 text-sm">
          <p className="font-medium text-amber-900">
            ⚠ この提案が作られた後、対象が別途編集されています
          </p>
          <div className="mt-2 space-y-1">
            {Object.entries(conflicts!).map(([k, c]) => (
              <div key={k} className="flex flex-wrap items-center gap-2 text-amber-900">
                <span className="text-amber-700 sm:w-40 shrink-0">{FIELD_LABELS[k] ?? k}</span>
                <span>
                  提案時 <code className="px-1 rounded bg-white">{formatFieldValue(k, c.expected)}</code>
                  {' → '}
                  現在 <code className="px-1 rounded bg-white font-medium">{formatFieldValue(k, c.current)}</code>
                </span>
              </div>
            ))}
          </div>
          <p className="mt-2 text-xs text-amber-700">
            そのまま承認すると、現在の値は提案内容で上書きされます。
          </p>
        </div>
      )}

      {suggestion.note && (
        <p className="mt-3 text-sm text-gray-600 bg-gray-50 rounded p-2 whitespace-pre-wrap break-words">
          💬 {suggestion.note}
        </p>
      )}

      {isOpen ? (
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onReject}
            disabled={busy}
            className="px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            却下
          </button>
          <button
            onClick={() => onApprove(hasConflict)}
            disabled={busy || changedKeys.length === 0}
            className={`px-4 py-2 text-sm text-white font-medium rounded-lg disabled:opacity-50 ${
              hasConflict ? 'bg-amber-600 hover:bg-amber-700' : 'bg-indigo-600 hover:bg-indigo-700'
            }`}
          >
            {hasConflict ? '現在の値を上書きして承認' : '承認して反映'}
          </button>
        </div>
      ) : (
        <div className="flex items-center justify-end gap-2 mt-4">
          {suggestion.review_note && (
            <span className="text-xs text-gray-400 truncate">{suggestion.review_note}</span>
          )}
          <span
            className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
              suggestion.status === 'approved' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-600'
            }`}
          >
            {suggestion.status === 'approved' ? '承認済み' : '却下'}
            {suggestion.reviewed_at && ` · ${new Date(suggestion.reviewed_at).toLocaleDateString('ja-JP')}`}
          </span>
        </div>
      )}
    </div>
  );
}
