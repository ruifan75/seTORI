import type { FieldChange } from '../api/types';
import { matchReasonLabel } from '../utils/matchReason';

// 入力欄の下に「元の値が、どの処理でどう変わったか」を出す。
//
// 編集画面に並ぶ曲名・歌手名は、留言に書かれていた文字そのままではない。
// AI の正規化と DB の照合がそれぞれ書き換えうる。どちらの仕業かを言わずに
// 結果だけ見せると、利用者は「自分が入れた覚えのない名前」を前にして
// 直すべきかどうか判断できない。
//
// 変わらなかった段は表示しない（「何も起きていない」を並べても読む負担が増えるだけ）。

const STEP_LABELS: Record<string, string> = {
  ai_normalize: 'AI正規化',
  db_match: 'DB照合',
};

interface Props {
  changes?: FieldChange[];
  field: 'name' | 'artist';
}

export default function FieldProvenance({ changes, field }: Props) {
  const rows = (changes || []).filter((c) => c.field === field);
  if (rows.length === 0) return null;

  return (
    <div className="mt-1 space-y-0.5 text-sm">
      {rows.map((c, i) => (
        <div key={i} className="flex flex-wrap items-baseline gap-x-1">
          <span
            className={
              c.by === 'db_match'
                ? 'shrink-0 rounded bg-emerald-50 px-1.5 text-xs text-emerald-700'
                : 'shrink-0 rounded bg-blue-50 px-1.5 text-xs text-blue-700'
            }
            title={
              c.by === 'db_match'
                ? `${matchReasonLabel(c.reason)}${c.score ? `（確信度 ${Math.round(c.score * 100)}%）` : ''}`
                : 'AI が正規化した結果'
            }
          >
            {STEP_LABELS[c.by] ?? c.by}
          </span>
          {/* 元が空＝留言に書かれていなかったもの。ここを黙って埋めると出所が分からなくなる */}
          {c.from ? (
            <span className="text-gray-400 line-through">{c.from}</span>
          ) : (
            <span className="text-gray-400">（未記入）</span>
          )}
          <span className="text-gray-300">→</span>
          <span className="font-medium text-gray-800">{c.to}</span>
          {c.by === 'db_match' && c.reason && (
            <span className="text-xs text-gray-400">{matchReasonLabel(c.reason)}</span>
          )}
        </div>
      ))}
    </div>
  );
}
