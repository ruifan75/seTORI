import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { filterKeywordApi, tagApi, tagRuleApi, aiProviderApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
import { useAuthStore, hasPermission, PERM } from '../../store/auth';
import IntegrationSettingsSection from './IntegrationSettingsSection';
import BuildVersion from '../../components/BuildVersion';
import type { FilterKeyword, StreamTag, PerformanceTag, TagKeywordRule, AIProvider, AIProviderInput, AIModelInfo } from '../../api/types';

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

// model が空のものは、保存後に ▾ ボタンでプロバイダーから実際のモデル一覧を取得して選ぶ。
// モデル ID は頻繁に増減するので、古い ID を焼き込むよりこちらの方が安全。
const AI_PRESETS: { label: string; name: string; base_url: string; model: string }[] = [
  { label: 'OpenAI', name: 'OpenAI', base_url: 'https://api.openai.com/v1', model: '' },
  { label: 'Groq', name: 'Groq', base_url: 'https://api.groq.com/openai/v1', model: 'llama-3.3-70b-versatile' },
  { label: 'Google Gemini', name: 'Gemini', base_url: 'https://generativelanguage.googleapis.com/v1beta/openai', model: 'gemini-2.5-flash' },
  { label: 'Cerebras', name: 'Cerebras', base_url: 'https://api.cerebras.ai/v1', model: 'llama-3.3-70b' },
  { label: 'OpenRouter', name: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1', model: '' },
  { label: 'Ollama (本機)', name: 'Ollama', base_url: 'http://localhost:11434/v1', model: 'llama3.1' },
];

// ModelPicker モデル選択用の ▾ ボタン + 下拉清單。
// 開いたタイミングで fetcher を呼び、取得中はスピナーを表示する。
function ModelPicker({ fetcher, current, onSelect, disabled, disabledTitle }: {
  fetcher: () => Promise<AIModelInfo[]>;
  current: string;
  onSelect: (id: string) => void;
  disabled?: boolean;
  disabledTitle?: string;
}) {
  const [open, setOpen] = useState(false);
  const [models, setModels] = useState<AIModelInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const toggle = async () => {
    if (open) { setOpen(false); return; }
    setOpen(true);
    setError(null);
    setLoading(true);
    try {
      setModels(await fetcher());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'モデル一覧の取得に失敗しました');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="relative">
      <button
        type="button"
        onClick={toggle}
        disabled={disabled}
        title={disabled ? (disabledTitle ?? '') : '利用可能なモデルを取得'}
        className="px-2 py-[5px] border border-l-0 border-gray-300 rounded-r text-gray-500 hover:bg-gray-50 disabled:opacity-40 disabled:hover:bg-transparent"
      >▾</button>
      {open && (
        <>
          {/* backdrop：點擊外部關閉 */}
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-full mt-1 z-20 w-80 max-h-72 overflow-auto bg-white border border-gray-200 rounded-lg shadow-lg py-1 text-sm">
            {loading && (
              <div className="flex items-center gap-2 px-3 py-2 text-gray-500">
                <svg className="animate-spin h-4 w-4 text-indigo-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                </svg>
                モデルを取得中...
              </div>
            )}
            {!loading && error && (
              <div className="px-3 py-2 text-red-600 break-words">{error}</div>
            )}
            {!loading && !error && models.length === 0 && (
              <div className="px-3 py-2 text-gray-400">利用可能なモデルがありません</div>
            )}
            {!loading && !error && models.map((m) => (
              <button
                key={m.id}
                type="button"
                onClick={() => { onSelect(m.id); setOpen(false); }}
                className={`block w-full text-left px-3 py-1.5 hover:bg-indigo-50 ${m.id === current ? 'bg-indigo-50' : ''}`}
              >
                <span className={`font-mono ${m.id === current ? 'text-indigo-700 font-semibold' : 'text-gray-700'}`}>{m.id}</span>
                {(m.display_name || m.context_window) && (
                  <span className="block text-[11px] text-gray-400 truncate">
                    {m.display_name}
                    {m.context_window ? `${m.display_name ? ' · ' : ''}${Math.round(m.context_window / 1000)}K ctx` : ''}
                  </span>
                )}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function ProviderRow({ p, idx, total, onUpdate, onDelete, onMove }: {
  p: AIProvider;
  idx: number;
  total: number;
  onUpdate: (id: number, input: Partial<AIProviderInput>) => void;
  onDelete: (id: number) => void;
  onMove: (idx: number, dir: -1 | 1) => void;
}) {
  const [model, setModel] = useState(p.model);
  const [timeoutSec, setTimeoutSec] = useState(p.timeout_seconds || 60);

  const saveModel = (value?: string) => {
    const m = (value ?? model).trim();
    if (m && m !== p.model) onUpdate(p.id, { model: m });
  };

  const saveTimeout = () => {
    const v = Math.max(10, Math.min(300, Number(timeoutSec) || 60));
    if (v !== p.timeout_seconds) onUpdate(p.id, { timeout_seconds: v });
  };

  return (
    <div className="flex items-center gap-3 px-3 py-2 border rounded-lg">
      <input
        type="checkbox"
        checked={p.enabled}
        onChange={(e) => onUpdate(p.id, { enabled: e.target.checked })}
        title="有効/無効"
        className="h-4 w-4"
      />
      <div className="flex flex-col leading-none">
        <button onClick={() => onMove(idx, -1)} disabled={idx === 0}
          className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-30" title="上へ（優先度を上げる）">▲</button>
        <button onClick={() => onMove(idx, 1)} disabled={idx === total - 1}
          className="text-xs text-gray-400 hover:text-gray-700 disabled:opacity-30" title="下へ（優先度を下げる）">▼</button>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-400 w-5 text-right">{idx + 1}.</span>
          <span className="font-medium text-gray-900">{p.name}</span>
          {!p.enabled && <span className="text-xs text-gray-400">（無効）</span>}
        </div>
        <div className="flex items-center gap-1 text-xs text-gray-500 pl-7">
          <div className="flex items-center">
            <input
              value={model}
              onChange={(e) => setModel(e.target.value)}
              onBlur={() => saveModel()}
              onKeyDown={(e) => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); }}
              title="モデルを編集（Enter / フォーカスアウトで保存）"
              className="w-48 px-1.5 py-0.5 font-mono text-gray-700 border border-gray-200 rounded-l focus:ring-1 focus:ring-indigo-400 focus:border-transparent"
            />
            <ModelPicker
              fetcher={() => aiProviderApi.listModels(p.id)}
              current={model}
              onSelect={(id) => { setModel(id); saveModel(id); }}
              disabled={!p.has_key}
              disabledTitle="API キーが必要です"
            />
          </div>
          <span className="mx-1">·</span>
          <label className="text-xs text-gray-400 flex items-center gap-1" title="この provider の AI 呼び出しタイムアウト秒数。OpenRouter など遅いものは大きく（90〜180）">
            timeout
            <input
              type="number"
              min={10}
              max={300}
              step={5}
              value={timeoutSec}
              onChange={(e) => setTimeoutSec(parseInt(e.target.value) || 60)}
              onBlur={saveTimeout}
              onKeyDown={(e) => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); }}
              className="w-14 px-1 py-0.5 font-mono text-gray-700 border border-gray-200 rounded text-right"
            />
            <span>s</span>
          </label>
          <span className="truncate">· {p.base_url} · key {p.key_hint ?? (p.has_key ? '****' : 'なし')}</span>
        </div>
      </div>
      <button onClick={() => onDelete(p.id)} className="text-sm text-red-600 hover:text-red-800" title="削除">削除</button>
    </div>
  );
}

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
  const [timeoutSec, setTimeoutSec] = useState(60);

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
    const t = Math.max(10, Math.min(300, timeoutSec || 60));
    createMutation.mutate(
      {
        name: name.trim(),
        base_url: baseUrl.trim(),
        model: model.trim(),
        api_key: apiKey.trim(),
        priority: providers.length,
        timeout_seconds: t,
      },
      {
        onSuccess: () => {
          setName('');
          setBaseUrl('');
          setModel('');
          setApiKey('');
          setTimeoutSec(60);
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
        各 provider の timeout（秒）は個別に調整可能。OpenRouter など遅いものは 90〜180 に設定するとタイムアウトしにくくなります。
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
            <ProviderRow
              key={`${p.id}:${p.model}:${p.timeout_seconds}`}
              p={p}
              idx={idx}
              total={providers.length}
              onUpdate={(id, input) => updateMutation.mutate({ id, input })}
              onDelete={(id) => deleteMutation.mutate(id)}
              onMove={move}
            />
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
          <div className="flex flex-1 min-w-[12rem]">
            <input
              type="text"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="モデル"
              className="flex-1 min-w-0 px-3 py-1.5 text-sm border border-r-0 border-gray-300 rounded-l-lg"
            />
            <ModelPicker
              fetcher={() => aiProviderApi.previewModels({ base_url: baseUrl.trim(), api_key: apiKey.trim() })}
              current={model}
              onSelect={(id) => setModel(id)}
              disabled={!baseUrl.trim() || !apiKey.trim()}
              disabledTitle="先に Base URL と API キーを入力してください"
            />
          </div>
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
          <label className="flex items-center gap-1 text-sm text-gray-600" title="タイムアウト秒数（遅い provider は 90〜180 に）">
            timeout
            <input
              type="number"
              min={10}
              max={300}
              step={5}
              value={timeoutSec}
              onChange={(e) => setTimeoutSec(parseInt(e.target.value) || 60)}
              className="w-16 px-2 py-1.5 text-sm border border-gray-300 rounded-lg text-right font-mono"
            />
            <span className="text-xs">s</span>
          </label>
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

// TagRuleRow 単一 stream tag に対するキーワードルールの表示・追加・削除。
function TagRuleRow({ tag, rules, onAdd, onDelete, isAdding }: {
  tag: StreamTag;
  rules: TagKeywordRule[];
  onAdd: (tagId: string, keyword: string) => void;
  onDelete: (id: number) => void;
  isAdding: boolean;
}) {
  const [kw, setKw] = useState('');
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const t = kw.trim();
    if (t) { onAdd(tag.id, t); setKw(''); }
  };
  return (
    <div className="flex flex-wrap items-center gap-2 px-3 py-2 border rounded-lg">
      <span
        className="inline-flex items-center px-2.5 py-1 text-sm border rounded-full shrink-0"
        style={{ backgroundColor: tag.color + '20', color: tag.color, borderColor: tag.color + '40' }}
        title={tag.id}
      >
        {tag.display_name}
      </span>
      <span className="text-gray-300">←</span>
      <div className="flex flex-wrap gap-1 flex-1 min-w-[8rem]">
        {rules.length === 0 && <span className="text-xs text-gray-400">ルールなし</span>}
        {rules.map((r) => (
          <span key={r.id} className="inline-flex items-center gap-1 px-2 py-0.5 text-xs bg-gray-100 text-gray-700 rounded">
            {r.keyword}
            <button onClick={() => onDelete(r.id)} className="hover:text-red-600" title="削除">&times;</button>
          </span>
        ))}
      </div>
      <form onSubmit={handleSubmit} className="flex gap-1">
        <input
          type="text"
          value={kw}
          onChange={(e) => setKw(e.target.value)}
          placeholder="含む語（例: 雑談）"
          className="w-36 px-2 py-1 text-sm border border-gray-300 rounded-lg"
        />
        <button
          type="submit"
          disabled={isAdding || !kw.trim()}
          className="px-2.5 py-1 text-sm text-white rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
        >
          追加
        </button>
      </form>
    </div>
  );
}

function TagRuleSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data: streamTags = [] } = useQuery({ queryKey: ['stream-tags'], queryFn: tagApi.listStreamTags });
  const { data: rules = [], isLoading } = useQuery({ queryKey: ['tag-rules'], queryFn: tagRuleApi.list });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['tag-rules'] });

  const createMutation = useMutation({
    mutationFn: ({ tagId, keyword }: { tagId: string; keyword: string }) => tagRuleApi.create(tagId, keyword),
    onSuccess: () => invalidate(),
    onError: (err: Error) => showToast(`追加エラー: ${err.message}`, 'error'),
  });
  const deleteMutation = useMutation({
    mutationFn: (id: number) => tagRuleApi.delete(id),
    onSuccess: () => invalidate(),
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });
  const backfillMutation = useMutation({
    mutationFn: () => tagRuleApi.backfill(),
    onSuccess: (r) => {
      showToast(r.message, 'success');
      queryClient.invalidateQueries({ queryKey: ['streams'] });
    },
    onError: (err: Error) => showToast(`適用エラー: ${err.message}`, 'error'),
  });

  const rulesByTag = new Map<string, TagKeywordRule[]>();
  for (const r of rules) {
    const arr = rulesByTag.get(r.tag_id) ?? [];
    arr.push(r);
    rulesByTag.set(r.tag_id, arr);
  }

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <div className="flex items-start justify-between mb-2 gap-4">
        <h2 className="text-xl font-bold text-gray-900">タイトル自動タグ付けルール</h2>
        <button
          onClick={() => backfillMutation.mutate()}
          disabled={backfillMutation.isPending}
          className="shrink-0 px-3 py-1.5 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
          title="既存の全配信にルールを適用します"
        >
          {backfillMutation.isPending ? '適用中...' : '全配信に適用'}
        </button>
      </div>
      <p className="text-gray-500 mb-6">
        配信タイトルに指定の語を「含む」場合に、そのタグを自動で付与します（大小文字は無視・部分一致）。
        1配信に複数タグ可。付与のみで既存タグは消えません。同期時にも自動適用され、「全配信に適用」で既存分へ一括付与できます。
        新しい種別（雑談・ゲームなど）は先に「配信タグ管理」でタグを作ってからルールを追加してください。
      </p>

      {isLoading ? (
        <p className="text-gray-400">読み込み中...</p>
      ) : streamTags.length === 0 ? (
        <p className="text-sm text-gray-400">先に「配信タグ管理」でタグを作成してください。</p>
      ) : (
        <div className="space-y-2">
          {streamTags.map((tag: StreamTag) => (
            <TagRuleRow
              key={tag.id}
              tag={tag}
              rules={rulesByTag.get(tag.id) ?? []}
              onAdd={(tagId, keyword) => createMutation.mutate({ tagId, keyword })}
              onDelete={(id) => deleteMutation.mutate(id)}
              isAdding={createMutation.isPending}
            />
          ))}
        </div>
      )}
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

  const me = useAuthStore((s) => s.user);
  const canManageAI = hasPermission(me, PERM.AI_MANAGE);
  // 連携設定は資格情報の管理なので、バックエンドと同じく users:manage を要求する
  const canManageIntegrations = hasPermission(me, PERM.USERS_MANAGE);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-3xl font-bold text-gray-900">設定</h1>
        <BuildVersion />
      </div>

      {/* 外部サービス連携（要 users:manage） */}
      {canManageIntegrations && (
        <div className="bg-white rounded-lg shadow-sm border p-6">
          <IntegrationSettingsSection />
        </div>
      )}

      {/* AI Providers（要 ai:manage） */}
      {canManageAI && <AIProviderSection />}

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

      {/* Title-based auto-tagging rules */}
      <TagRuleSection />

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
