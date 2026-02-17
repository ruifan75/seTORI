import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { filterKeywordApi } from '../../api/client';
import { useToast } from '../../components/ui/Toast';
import type { FilterKeyword } from '../../api/types';

function KeywordSection({
  title,
  description,
  type,
  keywords,
  onAdd,
  onDelete,
  isAdding,
}: {
  title: string;
  description: string;
  type: 'filter' | 'keep';
  keywords: FilterKeyword[];
  onAdd: (keyword: string, type: 'filter' | 'keep') => void;
  onDelete: (id: number) => void;
  isAdding: boolean;
}) {
  const [newKeyword, setNewKeyword] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = newKeyword.trim();
    if (trimmed) {
      onAdd(trimmed, type);
      setNewKeyword('');
    }
  };

  const tagColor = type === 'filter'
    ? 'bg-red-100 text-red-800 border-red-200'
    : 'bg-green-100 text-green-800 border-green-200';

  const buttonColor = type === 'filter'
    ? 'bg-red-600 hover:bg-red-700'
    : 'bg-green-600 hover:bg-green-700';

  return (
    <div>
      <h3 className="text-lg font-semibold text-gray-900 mb-1">{title}</h3>
      <p className="text-sm text-gray-500 mb-3">{description}</p>

      <div className="flex flex-wrap gap-2 mb-4">
        {keywords.length === 0 && (
          <span className="text-sm text-gray-400">キーワードがありません</span>
        )}
        {keywords.map((kw) => (
          <span
            key={kw.id}
            className={`inline-flex items-center gap-1 px-2.5 py-1 text-sm border rounded-full ${tagColor}`}
          >
            {kw.keyword}
            <button
              onClick={() => onDelete(kw.id)}
              className="ml-0.5 hover:opacity-70 transition-opacity"
              title="削除"
            >
              &times;
            </button>
          </span>
        ))}
      </div>

      <form onSubmit={handleSubmit} className="flex gap-2">
        <input
          type="text"
          value={newKeyword}
          onChange={(e) => setNewKeyword(e.target.value)}
          placeholder="キーワードを入力..."
          className="flex-1 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        <button
          type="submit"
          disabled={isAdding || !newKeyword.trim()}
          className={`px-3 py-1.5 text-sm text-white font-medium rounded-lg transition-colors disabled:opacity-50 ${buttonColor}`}
        >
          追加
        </button>
      </form>
    </div>
  );
}

export default function SettingsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data: keywords = [], isLoading } = useQuery({
    queryKey: ['filter-keywords'],
    queryFn: filterKeywordApi.list,
  });

  const createMutation = useMutation({
    mutationFn: ({ keyword, type }: { keyword: string; type: 'filter' | 'keep' }) =>
      filterKeywordApi.create(keyword, type),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['filter-keywords'] });
      showToast('キーワードを追加しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`追加エラー: ${err.message}`, 'error');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => filterKeywordApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['filter-keywords'] });
      showToast('キーワードを削除しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`削除エラー: ${err.message}`, 'error');
    },
  });

  const filterKeywords = keywords.filter((kw) => kw.type === 'filter');
  const keepKeywords = keywords.filter((kw) => kw.type === 'keep');

  const handleAdd = (keyword: string, type: 'filter' | 'keep') => {
    createMutation.mutate({ keyword, type });
  };

  const handleDelete = (id: number) => {
    deleteMutation.mutate(id);
  };

  return (
    <div className="space-y-8">
      <h1 className="text-3xl font-bold text-gray-900">設定</h1>

      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">フィルターキーワード管理</h2>
        <p className="text-gray-500 mb-6">
          コメントから歌曲を読み込む際に、除外・保持するキーワードを管理します。
        </p>

        {isLoading ? (
          <p className="text-gray-400">読み込み中...</p>
        ) : (
          <div className="space-y-8">
            <KeywordSection
              title="除外キーワード"
              description="このキーワードを含む項目は歌曲として認識されません（例: トーク、休憩、BGM）"
              type="filter"
              keywords={filterKeywords}
              onAdd={handleAdd}
              onDelete={handleDelete}
              isAdding={createMutation.isPending}
            />

            <hr className="border-gray-200" />

            <KeywordSection
              title="保持キーワード"
              description="除外キーワードより優先され、このキーワードを含む項目は歌曲として保持されます（例: cover、piano）"
              type="keep"
              keywords={keepKeywords}
              onAdd={handleAdd}
              onDelete={handleDelete}
              isAdding={createMutation.isPending}
            />
          </div>
        )}
      </div>
    </div>
  );
}
