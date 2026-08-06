import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { suggestionApi } from '../api/client';
import type { AutoApplySettings } from '../api/types';
import { useToast } from './ui/ToastContext';

// timing 提案の自動適用条件を調整するパネル（レビュー画面の折りたたみ）。
//
// しきい値は運用しながら決めるもの（利用者数や通報の質で適切な値が変わる）なので、
// コードの定数ではなく設定にしてある。
export default function AutoApplySettingsPanel() {
  const { showToast } = useToast();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  // 触るまでは null。サーバーの値をそのまま見せ、編集したぶんだけ手元で持つ
  const [edited, setEdited] = useState<AutoApplySettings | null>(null);

  const { data } = useQuery({
    queryKey: ['suggestions', 'settings'],
    queryFn: () => suggestionApi.getSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (s: AutoApplySettings) => suggestionApi.updateSettings(s),
    onSuccess: (saved) => {
      // サーバーが範囲に丸めた結果を正とする
      setEdited(saved);
      queryClient.setQueryData(['suggestions', 'settings'], saved);
      showToast('自動反映の条件を保存しました', 'success');
    },
    onError: (err: Error) => showToast(`保存できませんでした: ${err.message}`, 'error'),
  });

  const draft = edited ?? data;
  if (!draft) return null;
  const setDraft = setEdited;

  const num = (key: keyof AutoApplySettings, min: number, max: number, label: string, hint: string) => (
    <label className="block">
      <span className="text-sm text-gray-700">{label}</span>
      <div className="flex items-center gap-2 mt-1">
        <input
          type="number"
          min={min}
          max={max}
          value={draft[key] as number}
          onChange={(e) => setDraft({ ...draft, [key]: Number(e.target.value) })}
          disabled={!draft.enabled}
          className="w-24 px-2 py-1 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent disabled:bg-gray-100 disabled:text-gray-400"
        />
        <span className="text-xs text-gray-400">{hint}</span>
      </div>
    </label>
  );

  return (
    <div className="bg-white rounded-lg border">
      <button
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-4 py-3 text-sm text-gray-700 hover:bg-gray-50"
      >
        <span>
          自動反映の条件
          <span className="ml-2 text-xs text-gray-400">
            {draft.enabled
              ? `${draft.min_votes}人以上・ばらつき${draft.max_spread_seconds}秒以内・現在値から${draft.max_delta_seconds}秒以内`
              : '無効'}
          </span>
        </span>
        <span className="text-gray-400">{open ? '▲' : '▼'}</span>
      </button>

      {open && (
        <div className="border-t px-4 py-4 space-y-4">
          <p className="text-xs text-gray-500">
            複数の利用者が同じ歌唱の開始/終了について近い値を提案したとき、中央値を自動で反映します。
            匿名の提案は数えません。対象が提案後に編集されていれば自動反映せず人手のレビューに回します。
          </p>

          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
              className="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
            />
            <span className="text-sm text-gray-700">自動反映を有効にする</span>
          </label>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {num('min_votes', 2, 20, '必要な人数', '2〜20人')}
            {num('max_spread_seconds', 0, 60, '値のばらつき', '0〜60秒')}
            {num('max_delta_seconds', 1, 300, '現在値からの差', '1〜300秒')}
          </div>

          <div className="flex justify-end">
            <button
              onClick={() => saveMutation.mutate(draft)}
              disabled={saveMutation.isPending}
              className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
            >
              {saveMutation.isPending ? '保存中...' : '保存'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
