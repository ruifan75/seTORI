import type { Suggestion } from '../api/types';
import { FIELD_LABELS, changedKeysOf, formatFieldValue } from './suggestionDisplay';

// SuggestionChanges 種別を問わず「何をどう変えたいか」を1行で見せる。
export function SuggestionChanges({ suggestion }: { suggestion: Suggestion }) {
  if (suggestion.kind === 'perf.missing') return <MissingSongSummary suggestion={suggestion} />;
  if (suggestion.kind === 'perf.meta') return <SongSwapSummary suggestion={suggestion} />;

  const { before, after } = suggestion;
  return (
    <>
      {changedKeysOf(suggestion).map((k) => (
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
      ))}
    </>
  );
}

// MissingSongSummary 未登録曲の追加報告（差分ではなく「追加したい内容」）。
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

// OverlapWarning 未登録曲の追加提案が、既に登録済みの歌唱と時間的に重なっているときの注意書き。
// メドレーや掛け合いで正当に重なることもあるので承認は止めない（判断はレビュー担当に委ねる）。
export function OverlapWarning({ overlaps }: { overlaps: Suggestion['overlaps'] }) {
  if (!overlaps || overlaps.length === 0) return null;
  return (
    <p className="text-xs text-amber-800 mt-1">
      ⚠ この時間帯には既に{' '}
      {overlaps
        .map(
          (o) =>
            `${o.song_name}（${formatFieldValue('start_seconds', String(o.start_seconds))}–${
              o.end_seconds === 0 ? '最後' : formatFieldValue('end_seconds', String(o.end_seconds))
            }）`
        )
        .join('、')}{' '}
      が登録されています。同じ曲の重複報告でないか確認してください
    </p>
  );
}

// SongSwapSummary 曲の差し替え（「この曲ではない」）。曲そのものが変わる。
function SongSwapSummary({ suggestion }: { suggestion: Suggestion }) {
  const p = suggestion.song_swap;
  if (!p) return <span className="text-xs text-gray-400">内容が読み取れません</span>;
  return (
    <span className="text-sm flex items-center gap-1.5 flex-wrap">
      <span className="text-gray-500 text-xs">曲</span>
      <span className="px-1.5 py-0.5 rounded bg-red-50 text-red-700 line-through text-xs break-words">
        {p.current_song_name || '（不明）'}
      </span>
      <span className="text-gray-400 text-xs">→</span>
      <span className="px-1.5 py-0.5 rounded bg-green-50 text-green-700 font-medium text-xs break-words">
        {p.song_name}
        {p.original_artist ? ` / ${p.original_artist}` : ''}
      </span>
      {!p.song_id && <span className="text-[11px] text-amber-700">新規登録</span>}
    </span>
  );
}
