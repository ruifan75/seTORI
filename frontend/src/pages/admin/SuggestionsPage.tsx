import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { suggestionApi } from '../../api/client';
import type { Suggestion, SuggestionStatus } from '../../api/types';
import Loading from '../../components/ui/Loading';
import Pagination from '../../components/ui/Pagination';
import { useToast } from '../../components/ui/Toast';

const STATUS_TABS: { value: SuggestionStatus; label: string }[] = [
  { value: 'pending', label: '未処理' },
  { value: 'approved', label: '承認済み' },
  { value: 'rejected', label: '却下' },
];

// フィールドキー → 日本語ラベル（差分表示用）
const FIELD_LABELS: Record<string, string> = {
  name: '名前',
  name_reading: '読み',
  original_artist: 'アーティスト',
  original_artist_reading: 'アーティストの読み',
};

const TYPE_LABELS: Record<string, string> = { song: '楽曲', artist: 'アーティスト' };

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
  };

  const approveMutation = useMutation({
    mutationFn: (id: string) => suggestionApi.approve(id),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (err: Error) => showToast(`承認失敗: ${err.message}`, 'error'),
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
                onApprove={() => approveMutation.mutate(s.id)}
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
  onApprove: () => void;
  onReject: () => void;
  busy: boolean;
}) {
  const { before, after } = suggestion;
  const changedKeys = Object.keys(after).filter((k) => (after[k] ?? '') !== (before[k] ?? ''));
  const detailPath =
    suggestion.target_type === 'song' ? `/songs/${suggestion.target_id}` : `/artists/${suggestion.target_id}`;

  return (
    <div className="bg-white rounded-lg shadow-sm border p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-2 mb-3">
        <div className="min-w-0">
          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-600 mr-2">
            {TYPE_LABELS[suggestion.target_type] ?? suggestion.target_type}
          </span>
          <Link to={detailPath} className="text-indigo-600 hover:text-indigo-900 font-medium break-words">
            {suggestion.target_label}
          </Link>
        </div>
        <span className="text-xs text-gray-400 shrink-0">
          {new Date(suggestion.created_at).toLocaleString('ja-JP')}
        </span>
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
                  {before[k] || '（空）'}
                </span>
                <span className="text-gray-400">→</span>
                <span className="px-2 py-0.5 rounded bg-green-50 text-green-700 font-medium break-words">
                  {after[k] || '（空）'}
                </span>
              </div>
            </div>
          ))
        )}
      </div>

      {suggestion.note && (
        <p className="mt-3 text-sm text-gray-600 bg-gray-50 rounded p-2 whitespace-pre-wrap break-words">
          💬 {suggestion.note}
        </p>
      )}

      {suggestion.status === 'pending' ? (
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onReject}
            disabled={busy}
            className="px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            却下
          </button>
          <button
            onClick={onApprove}
            disabled={busy || changedKeys.length === 0}
            className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            承認して反映
          </button>
        </div>
      ) : (
        <div className="flex justify-end mt-4">
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
