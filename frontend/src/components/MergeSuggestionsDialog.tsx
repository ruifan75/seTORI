import { useEffect, useState } from 'react';
import { suggestionApi } from '../api/client';
import type { Suggestion, SuggestionGroup } from '../api/types';
import { useToast } from './ui/ToastContext';

// 同じ対象に集まった提案を見比べて、1つの値へ決着させるダイアログ。
//
// 「どれか1つを丸ごと採用」では表せないケースのための操作：
// 3人が 6708 / 6710 / 6716 と提案していて中央値にしたい、誰も出していない値にしたい、
// 項目ごとに別の人の提案を採りたい、など。
//
// 反映後、採用値と一致した提案は承認、それ以外は却下として履歴に残る
// （誰の指摘が通ったかを追えるようにするため）。

const FIELD_LABELS: Record<string, string> = {
  name: '名前',
  name_reading: '読み',
  original_artist: 'アーティスト',
  original_artist_reading: 'アーティストの読み',
  start_seconds: '開始時間',
  end_seconds: '終了時間',
};

const TIME_FIELDS = new Set(['start_seconds', 'end_seconds']);

function formatValue(key: string, value: string): string {
  if (!TIME_FIELDS.has(key)) return value || '（空）';
  const n = Number(value);
  if (!Number.isFinite(n)) return value || '（空）';
  if (n === 0) return '最後まで';
  const h = Math.floor(n / 3600);
  const m = Math.floor((n % 3600) / 60);
  const s = n % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`;
}

function changedKeysOf(s: Suggestion): string[] {
  return Object.keys(s.after).filter((k) => (s.after[k] ?? '') !== (s.before[k] ?? ''));
}

// 数値項目のみ中央値を出せる。偶数個なら小さい側（自動適用と同じ規則）。
function medianOf(values: string[]): string | null {
  const nums = values.map(Number).filter((n) => Number.isFinite(n));
  if (nums.length < 2) return null;
  nums.sort((a, b) => a - b);
  return String(nums[Math.floor((nums.length - 1) / 2)]);
}

interface Candidate {
  value: string;
  label: string; // 誰の提案か／中央値か
}

export default function MergeSuggestionsDialog({
  group,
  onClose,
  onDone,
}: {
  group: SuggestionGroup;
  onClose: () => void;
  onDone: () => void;
}) {
  const { showToast } = useToast();
  const [busy, setBusy] = useState(false);

  // 提案が触っている項目だけを対象にする
  const fields = [...new Set(group.suggestions.flatMap(changedKeysOf))];

  // 初期値は現在値。管理者が項目ごとに選び直す
  const [draft, setDraft] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {};
    for (const k of fields) init[k] = group.current[k] ?? '';
    return init;
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  // 項目ごとの選択肢：現在値 + 各提案の値（同値はまとめる）+ 中央値
  const candidatesFor = (key: string): Candidate[] => {
    const out: Candidate[] = [{ value: group.current[key] ?? '', label: '現在' }];
    const byValue = new Map<string, string[]>();
    for (const s of group.suggestions) {
      if (!changedKeysOf(s).includes(key)) continue;
      const v = s.after[key] ?? '';
      byValue.set(v, [...(byValue.get(v) ?? []), s.created_by_name || '匿名']);
    }
    for (const [value, who] of byValue) {
      out.push({ value, label: who.join('、') });
    }
    const median = medianOf([...byValue.keys()]);
    if (median !== null && !out.some((c) => c.value === median)) {
      out.push({ value: median, label: '中央値' });
    }
    return out;
  };

  const changedAny = fields.some((k) => draft[k] !== (group.current[k] ?? ''));

  const apply = async () => {
    setBusy(true);
    try {
      const r = await suggestionApi.merge({
        target_type: group.target_type,
        target_id: group.target_id,
        fields: draft,
        ids: group.suggestions.map((s) => s.id),
      });
      const parts = Object.keys(r.applied).map((k) => `${FIELD_LABELS[k] ?? k} ${formatValue(k, r.applied[k])}`);
      showToast(
        parts.length > 0
          ? `${parts.join('、')} に統合しました（採用${r.approved}件・不採用${r.rejected}件）`
          : `値は変えずに${r.approved + r.rejected}件を処理しました`,
        'success'
      );
      onDone();
      onClose();
    } catch (e) {
      showToast(`統合できませんでした: ${(e as Error).message}`, 'error');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="bg-white rounded-xl shadow-xl w-full max-w-lg p-5 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-bold text-gray-900">提案をまとめて反映</h2>
        <p className="text-xs text-gray-500 mt-0.5 line-clamp-2">{group.target_label}</p>
        <p className="text-xs text-gray-400 mt-1">
          項目ごとに採用する値を選びます。反映後、この値と一致した提案は承認、それ以外は却下として記録されます。
        </p>

        {fields.map((key) => (
          <div key={key} className="mt-4">
            <div className="flex items-baseline gap-2">
              <span className="text-sm font-medium text-gray-700">{FIELD_LABELS[key] ?? key}</span>
              {draft[key] !== (group.current[key] ?? '') && (
                <span className="text-xs text-indigo-600 font-mono">
                  {formatValue(key, group.current[key] ?? '')} → {formatValue(key, draft[key])}
                </span>
              )}
            </div>

            <div className="flex flex-wrap gap-1.5 mt-1.5">
              {candidatesFor(key).map((c, i) => {
                const selected = draft[key] === c.value;
                return (
                  <button
                    key={`${c.value}-${i}`}
                    type="button"
                    onClick={() => setDraft((d) => ({ ...d, [key]: c.value }))}
                    className={`px-2 py-1 text-xs rounded-lg border transition-colors ${
                      selected
                        ? 'bg-indigo-600 text-white border-indigo-600'
                        : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-50'
                    }`}
                    title={c.label}
                  >
                    <span className="font-mono">{formatValue(key, c.value)}</span>
                    <span className={`ml-1.5 ${selected ? 'text-indigo-100' : 'text-gray-400'}`}>{c.label}</span>
                  </button>
                );
              })}
            </div>

            {/* 誰も出していない値にしたい場合の直接入力 */}
            <input
              type="text"
              value={draft[key]}
              onChange={(e) => setDraft((d) => ({ ...d, [key]: e.target.value }))}
              className="mt-1.5 w-40 px-2 py-1 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              aria-label={`${FIELD_LABELS[key] ?? key}の値`}
            />
            {TIME_FIELDS.has(key) && <span className="ml-2 text-xs text-gray-400">秒</span>}
          </div>
        ))}

        <div className="flex justify-end gap-2 mt-5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="px-4 py-2 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 disabled:opacity-50"
          >
            キャンセル
          </button>
          <button
            type="button"
            onClick={apply}
            disabled={busy}
            className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {busy ? '反映中...' : changedAny ? 'この値で反映' : '値を変えずに処理'}
          </button>
        </div>
      </div>
    </div>
  );
}
