import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { suggestionApi } from '../api/client';
import type { Suggestion, SuggestionStatus } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import { useToast } from '../components/ui/ToastContext';
import {
  STATUS_LABELS,
  TYPE_LABELS,
  detailPathOf,
} from '../components/suggestionDisplay';
import { SuggestionChanges } from '../components/SuggestionChanges';

// 自分が出した修正提案の一覧。
//
// 投稿直後のトーストからしか取り下げられないと、時間が経つと引っ込められない。
// ここから未処理のものを取り下げられるようにし、結果（反映済み・不採用）も確認できるようにする。

const TABS: { value: SuggestionStatus | ''; label: string }[] = [
  { value: '', label: 'すべて' },
  { value: 'pending', label: '確認待ち' },
  { value: 'approved', label: '反映済み' },
  { value: 'rejected', label: '不採用' },
];

const STATUS_STYLES: Record<SuggestionStatus, string> = {
  pending: 'bg-amber-100 text-amber-800',
  conflict: 'bg-amber-100 text-amber-800',
  approved: 'bg-green-100 text-green-800',
  rejected: 'bg-gray-100 text-gray-600',
};

export default function MySuggestionsPage() {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<SuggestionStatus | ''>('');
  const [page, setPage] = useState(1);

  const { data, isLoading } = useQuery({
    queryKey: ['suggestions', 'mine', status, page],
    queryFn: () => suggestionApi.listMine(status, page, 20),
  });

  const withdrawMutation = useMutation({
    mutationFn: (id: string) => suggestionApi.withdraw(id),
    onSuccess: (r) => {
      showToast(r.message, 'info');
      queryClient.invalidateQueries({ queryKey: ['suggestions'] });
    },
    onError: (err: Error) => {
      showToast(`取り消せませんでした: ${err.message}`, 'error');
      // 既に処理済みだった場合は一覧が古いので引き直す
      queryClient.invalidateQueries({ queryKey: ['suggestions'] });
    },
  });

  const handleTab = (v: SuggestionStatus | '') => {
    setStatus(v);
    setPage(1);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">自分の提案</h1>
        <p className="text-gray-500 mt-1 text-sm">
          あなたが送った修正提案です。確認待ちのものは取り消せます。
        </p>
      </div>

      <div className="inline-flex rounded-lg border border-gray-300 overflow-hidden text-sm">
        {TABS.map((tab, i) => (
          <button
            key={tab.value || 'all'}
            onClick={() => handleTab(tab.value)}
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
          {status === '' ? 'まだ提案はありません' : '該当する提案はありません'}
        </div>
      ) : (
        <>
          <div className="space-y-3">
            {data.suggestions.map((s) => (
              <MySuggestionCard
                key={s.id}
                suggestion={s}
                busy={withdrawMutation.isPending}
                onWithdraw={() => withdrawMutation.mutate(s.id)}
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

function MySuggestionCard({
  suggestion,
  busy,
  onWithdraw,
}: {
  suggestion: Suggestion;
  busy: boolean;
  onWithdraw: () => void;
}) {
  // 取り下げられるのは未処理のものだけ（処理済みはサーバー側でも 409）
  const open = suggestion.status === 'pending' || suggestion.status === 'conflict';

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
          <span className="text-xs text-gray-400">
            {new Date(suggestion.created_at).toLocaleDateString('ja-JP')}
          </span>
          <span
            className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
              STATUS_STYLES[suggestion.status]
            }`}
          >
            {STATUS_LABELS[suggestion.status]}
          </span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <SuggestionChanges suggestion={suggestion} />
      </div>

      {suggestion.note && (
        <p className="text-xs text-gray-600 mt-1.5 whitespace-pre-wrap break-words">💬 {suggestion.note}</p>
      )}

      {/* 却下理由・統合の記録など、結果の説明 */}
      {!open && suggestion.review_note && (
        <p className="text-xs text-gray-400 mt-1.5">{suggestion.review_note}</p>
      )}

      {open && (
        <div className="flex justify-end mt-3">
          <button
            onClick={onWithdraw}
            disabled={busy}
            className="px-3 py-1.5 text-xs bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            取り消す
          </button>
        </div>
      )}
    </div>
  );
}
