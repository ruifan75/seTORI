import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { filterKeywordApi, tagApi } from '../../api/client';
import { useToast } from '../../components/ui/Toast';
import type { FilterKeyword, StreamTag, PerformanceTag } from '../../api/types';

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

const TAG_COLOR_PALETTE = [
  '#E91E63', '#9C27B0', '#673AB7', '#3F51B5', '#2196F3',
  '#00BCD4', '#009688', '#4CAF50', '#8BC34A', '#FF9800',
  '#FF5722', '#795548', '#607D8B', '#F44336', '#FFD700',
  '#20B2AA', '#8B4513', '#4169E1', '#228B22', '#FF69B4',
  '#9932CC', '#FF8C00', '#6366F1', '#EC4899', '#14B8A6',
];

function pickUnusedColor(usedColors: string[]): string {
  const usedSet = new Set(usedColors.map((c) => c.toUpperCase()));
  const available = TAG_COLOR_PALETTE.filter((c) => !usedSet.has(c.toUpperCase()));
  if (available.length === 0) return TAG_COLOR_PALETTE[Math.floor(Math.random() * TAG_COLOR_PALETTE.length)];
  return available[Math.floor(Math.random() * available.length)];
}

function TagSection({
  title,
  description,
  tags,
  onAdd,
  onDelete,
  isAdding,
}: {
  title: string;
  description: string;
  tags: (StreamTag | PerformanceTag)[];
  onAdd: (id: string, displayName: string, color: string) => void;
  onDelete: (id: string) => void;
  isAdding: boolean;
}) {
  const usedColors = tags.map((t) => t.color);
  const [newId, setNewId] = useState('');
  const [newDisplayName, setNewDisplayName] = useState('');
  const [newColor, setNewColor] = useState(() => pickUnusedColor(usedColors));

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedId = newId.trim();
    const trimmedName = newDisplayName.trim();
    if (trimmedId && trimmedName) {
      onAdd(trimmedId, trimmedName, newColor);
      setNewId('');
      setNewDisplayName('');
      setNewColor(pickUnusedColor([...usedColors, newColor]));
    }
  };

  return (
    <div>
      <h3 className="text-lg font-semibold text-gray-900 mb-1">{title}</h3>
      <p className="text-sm text-gray-500 mb-3">{description}</p>

      <div className="flex flex-wrap gap-2 mb-4">
        {tags.length === 0 && (
          <span className="text-sm text-gray-400">タグがありません</span>
        )}
        {tags.map((tag) => (
          <span
            key={tag.id}
            className="inline-flex items-center gap-1 px-2.5 py-1 text-sm border rounded-full"
            style={{
              backgroundColor: tag.color + '20',
              color: tag.color,
              borderColor: tag.color + '40',
            }}
          >
            {tag.display_name}
            <span className="text-xs opacity-60">({tag.id})</span>
            <button
              onClick={() => onDelete(tag.id)}
              className="ml-0.5 hover:opacity-70 transition-opacity"
              title="削除"
            >
              &times;
            </button>
          </span>
        ))}
      </div>

      <form onSubmit={handleSubmit} className="flex gap-2 items-center">
        <input
          type="text"
          value={newId}
          onChange={(e) => setNewId(e.target.value)}
          placeholder="ID（例: singing）"
          className="w-36 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        <input
          type="text"
          value={newDisplayName}
          onChange={(e) => setNewDisplayName(e.target.value)}
          placeholder="表示名（例: 歌枠）"
          className="flex-1 px-3 py-1.5 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        <input
          type="color"
          value={newColor}
          onChange={(e) => setNewColor(e.target.value)}
          className="w-10 h-8 p-0.5 border border-gray-300 rounded-lg cursor-pointer"
          title="色を選択"
        />
        <button
          type="submit"
          disabled={isAdding || !newId.trim() || !newDisplayName.trim()}
          className="px-3 py-1.5 text-sm text-white font-medium rounded-lg transition-colors disabled:opacity-50 bg-indigo-600 hover:bg-indigo-700"
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

  // Filter keywords
  const { data: keywords = [], isLoading: isLoadingKeywords } = useQuery({
    queryKey: ['filter-keywords'],
    queryFn: filterKeywordApi.list,
  });

  const createKeywordMutation = useMutation({
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

  const deleteKeywordMutation = useMutation({
    mutationFn: (id: number) => filterKeywordApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['filter-keywords'] });
      showToast('キーワードを削除しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`削除エラー: ${err.message}`, 'error');
    },
  });

  // Stream tags
  const { data: streamTags = [], isLoading: isLoadingStreamTags } = useQuery({
    queryKey: ['stream-tags'],
    queryFn: tagApi.listStreamTags,
  });

  const createStreamTagMutation = useMutation({
    mutationFn: ({ id, displayName, color }: { id: string; displayName: string; color: string }) =>
      tagApi.createStreamTag(id, displayName, color),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stream-tags'] });
      showToast('タグを追加しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`追加エラー: ${err.message}`, 'error');
    },
  });

  const deleteStreamTagMutation = useMutation({
    mutationFn: (id: string) => tagApi.deleteStreamTag(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stream-tags'] });
      showToast('タグを削除しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`削除エラー: ${err.message}`, 'error');
    },
  });

  // Performance tags
  const { data: performanceTags = [], isLoading: isLoadingPerfTags } = useQuery({
    queryKey: ['performance-tags'],
    queryFn: tagApi.listPerformanceTags,
  });

  const createPerfTagMutation = useMutation({
    mutationFn: ({ id, displayName, color }: { id: string; displayName: string; color: string }) =>
      tagApi.createPerformanceTag(id, displayName, color),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['performance-tags'] });
      showToast('タグを追加しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`追加エラー: ${err.message}`, 'error');
    },
  });

  const deletePerfTagMutation = useMutation({
    mutationFn: (id: string) => tagApi.deletePerformanceTag(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['performance-tags'] });
      showToast('タグを削除しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`削除エラー: ${err.message}`, 'error');
    },
  });

  const filterKeywords = keywords.filter((kw) => kw.type === 'filter');
  const keepKeywords = keywords.filter((kw) => kw.type === 'keep');

  return (
    <div className="space-y-8">
      <h1 className="text-3xl font-bold text-gray-900">設定</h1>

      {/* Filter Keywords */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">フィルターキーワード管理</h2>
        <p className="text-gray-500 mb-6">
          コメントから歌曲を読み込む際に、除外・保持するキーワードを管理します。
        </p>

        {isLoadingKeywords ? (
          <p className="text-gray-400">読み込み中...</p>
        ) : (
          <div className="space-y-8">
            <KeywordSection
              title="除外キーワード"
              description="このキーワードを含む項目は歌曲として認識されません（例: トーク、休憩、BGM）"
              type="filter"
              keywords={filterKeywords}
              onAdd={(keyword, type) => createKeywordMutation.mutate({ keyword, type })}
              onDelete={(id) => deleteKeywordMutation.mutate(id)}
              isAdding={createKeywordMutation.isPending}
            />

            <hr className="border-gray-200" />

            <KeywordSection
              title="保持キーワード"
              description="除外キーワードより優先され、このキーワードを含む項目は歌曲として保持されます（例: cover、piano）"
              type="keep"
              keywords={keepKeywords}
              onAdd={(keyword, type) => createKeywordMutation.mutate({ keyword, type })}
              onDelete={(id) => deleteKeywordMutation.mutate(id)}
              isAdding={createKeywordMutation.isPending}
            />
          </div>
        )}
      </div>

      {/* Stream Tags */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">配信タグ管理</h2>
        <p className="text-gray-500 mb-6">
          配信に付けるタグを管理します（例: 歌枠、周年、誕生日）。
        </p>

        {isLoadingStreamTags ? (
          <p className="text-gray-400">読み込み中...</p>
        ) : (
          <TagSection
            title="配信タグ"
            description="配信の種類を分類するためのタグ"
            tags={streamTags}
            onAdd={(id, displayName, color) => createStreamTagMutation.mutate({ id, displayName, color })}
            onDelete={(id) => deleteStreamTagMutation.mutate(id)}
            isAdding={createStreamTagMutation.isPending}
          />
        )}
      </div>

      {/* Performance Tags */}
      <div className="bg-white rounded-lg shadow-sm border p-6">
        <h2 className="text-xl font-bold text-gray-900 mb-2">演出タグ管理</h2>
        <p className="text-gray-500 mb-6">
          演出（パフォーマンス）に付けるバージョンタグを管理します（例: Acoustic、弾き語り）。
        </p>

        {isLoadingPerfTags ? (
          <p className="text-gray-400">読み込み中...</p>
        ) : (
          <TagSection
            title="演出タグ"
            description="演出のバージョンや形式を示すタグ"
            tags={performanceTags}
            onAdd={(id, displayName, color) => createPerfTagMutation.mutate({ id, displayName, color })}
            onDelete={(id) => deletePerfTagMutation.mutate(id)}
            isAdding={createPerfTagMutation.isPending}
          />
        )}
      </div>
    </div>
  );
}
