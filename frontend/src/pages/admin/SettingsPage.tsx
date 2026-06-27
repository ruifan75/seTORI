import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { filterKeywordApi, tagApi, aiProviderApi } from '../../api/client';
import { useToast } from '../../components/ui/Toast';
import type { FilterKeyword, StreamTag, PerformanceTag, AIProvider, AIProviderInput } from '../../api/types';

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

const AI_PRESETS: { label: string; name: string; base_url: string; model: string }[] = [
  { label: 'Groq', name: 'Groq', base_url: 'https://api.groq.com/openai/v1', model: 'llama-3.3-70b-versatile' },
  { label: 'Google Gemini', name: 'Gemini', base_url: 'https://generativelanguage.googleapis.com/v1beta/openai', model: 'gemini-2.0-flash' },
  { label: 'Cerebras', name: 'Cerebras', base_url: 'https://api.cerebras.ai/v1', model: 'llama-3.3-70b' },
  { label: 'OpenRouter', name: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1', model: '' },
  { label: 'Ollama (本機)', name: 'Ollama', base_url: 'http://localhost:11434/v1', model: 'llama3.1' },
];

function AIProviderSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data: providers = [], isLoading } = useQuery({
    queryKey: ['ai-providers'],
    queryFn: aiProviderApi.list,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['ai-providers'] });

  const createMutation = useMutation({
    mutationFn: aiProviderApi.create,
    onSuccess: () => { invalidate(); showToast('プロバイダーを追加しました', 'success'); },
    onError: (err: Error) => showToast(`追加エラー: ${err.message}`, 'error'),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, input }: { id: number; input: Partial<AIProviderInput> }) => aiProviderApi.update(id, input),
    onSuccess: () => invalidate(),
    onError: (err: Error) => showToast(`更新エラー: ${err.message}`, 'error'),
  });

  const deleteMutation = useMutation({
    mutationFn: aiProviderApi.delete,
    onSuccess: () => { invalidate(); showToast('プロバイダーを削除しました', 'success'); },
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });

  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [model, setModel] = useState('');
  const [apiKey, setApiKey] = useState('');

  // 優先度の並び替え：隣の provider と priority を入れ替える
  const move = (idx: number, dir: -1 | 1) => {
    const cur = providers[idx];
    const other = providers[idx + dir];
    if (!cur || !other) return;
    updateMutation.mutate({ id: cur.id, input: { priority: other.priority } });
    updateMutation.mutate({ id: other.id, input: { priority: cur.priority } });
  };

  const applyPreset = (label: string) => {
    const preset = AI_PRESETS.find((p) => p.label === label);
    if (!preset) return;
    setName(preset.name);
    setBaseUrl(preset.base_url);
    setModel(preset.model);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !baseUrl.trim() || !model.trim() || !apiKey.trim()) return;
    createMutation.mutate(
      {
        name: name.trim(),
        base_url: baseUrl.trim(),
        model: model.trim(),
        api_key: apiKey.trim(),
        priority: providers.length,
      },
      {
        onSuccess: () => {
          setName('');
          setBaseUrl('');
          setModel('');
          setApiKey('');
        },
      }
    );
  };

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">AIプロバイダー管理</h2>
      <p className="text-gray-500 mb-6">
        歌名の正規化・コメント解析に使う OpenAI 互換 LLM プロバイダーを設定します。
        上にあるもの（優先度が高いもの）から使い、失敗・レート制限時に次へ自動で切り替えます（▲▼ で並び替え）。
        API キーは保存後は表示されません（末尾のみ）。
      </p>

      {isLoading ? (
        <p className="text-gray-400">読み込み中...</p>
      ) : (
        <div className="space-y-2 mb-6">
          {providers.length === 0 && (
            <p className="text-sm text-gray-400">
              プロバイダーがありません。未設定の場合は環境変数 GROQ_API_KEY が使われます。
            </p>
          )}
          {providers.map((p: AIProvider, idx: number) => (
            <div key={p.id} className="flex items-center gap-3 px-3 py-2 border rounded-lg">
              <input
                type="checkbox"
                checked={p.enabled}
                onChange={(e) => updateMutation.mutate({ id: p.id, input: { enabled: e.target.checked } })}
                title="有効/無効"
                className="h-4 w-4"
              />
              <div className="flex flex-col leading-none">
                <button
                  onClick={() => move(idx, -1)}
                  disabled={idx === 0}
                  className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-30"
                  title="上へ（優先度を上げる）"
                >
                  ▲
                </button>
                <button
                  onClick={() => move(idx, 1)}
                  disabled={idx === providers.length - 1}
                  className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-30"
                  title="下へ（優先度を下げる）"
                >
                  ▼
                </button>
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-xs text-gray-400 w-5 text-right">{idx + 1}.</span>
                  <span className="font-medium text-gray-900">{p.name}</span>
                  {!p.enabled && <span className="text-xs text-gray-400">（無効）</span>}
                </div>
                <div className="text-xs text-gray-500 truncate pl-7">
                  {p.model} · {p.base_url} · key {p.key_hint ?? (p.has_key ? '****' : 'なし')}
                </div>
              </div>
              <button
                onClick={() => deleteMutation.mutate(p.id)}
                className="text-sm text-red-600 hover:text-red-800"
                title="削除"
              >
                削除
              </button>
            </div>
          ))}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-2">
        <div className="flex flex-wrap gap-2">
          <select
            onChange={(e) => { applyPreset(e.target.value); e.target.value = ''; }}
            defaultValue=""
            className="px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
          >
            <option value="" disabled>プリセット...</option>
            {AI_PRESETS.map((p) => (
              <option key={p.label} value={p.label}>{p.label}</option>
            ))}
          </select>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="名前（例: Groq）"
            className="w-36 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
          />
          <input
            type="text"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="モデル"
            className="flex-1 min-w-[12rem] px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <input
            type="text"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="Base URL（例: https://api.groq.com/openai/v1）"
            className="flex-1 min-w-[16rem] px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
          />
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="API キー"
            className="flex-1 min-w-[12rem] px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
          />
          <button
            type="submit"
            disabled={createMutation.isPending || !name.trim() || !baseUrl.trim() || !model.trim() || !apiKey.trim()}
            className="px-4 py-1.5 text-sm text-white font-medium rounded-lg transition-colors disabled:opacity-50 bg-indigo-600 hover:bg-indigo-700"
          >
            追加
          </button>
        </div>
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

      {/* AI Providers */}
      <AIProviderSection />

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
