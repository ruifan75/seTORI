import type { FieldChange } from '../api/types';
import { matchReasonLabel } from '../utils/matchReason';

// 入力欄の下に「元の値が、どの処理でどう変わったか」を出す。
//
// 編集画面に並ぶ曲名・歌手名は、コメントに書かれていた文字そのままではない。
// AI の正規化と DB の照合がそれぞれ書き換えうる。どちらの仕業かを言わずに
// 結果だけ見せると、利用者は「自分が入れた覚えのない名前」を前にして
// 直すべきかどうか判断できない。
//
// 変わらなかった段は表示しない（「何も起きていない」を並べても読む負担が増えるだけ）。

const STEP_LABELS: Record<string, string> = {
  ai_normalize: 'AI正規化',
  db_match: 'DB照合',
  ai_match: 'AI照合',
};

// 規則で決まらず AI が決めた段。確信度を必ず添える
// ── 人はここを見て「確かめる価値があるか」を決める。
const AI_STEP = 'ai_match';

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
                : c.by === AI_STEP
                  ? 'shrink-0 rounded bg-amber-50 px-1.5 text-xs text-amber-800'
                  : 'shrink-0 rounded bg-blue-50 px-1.5 text-xs text-blue-700'
            }
            title={
              c.by === 'ai_normalize'
                ? 'AI が正規化した結果'
                : `${matchReasonLabel(c.reason)}${c.score ? `（確信度 ${Math.round(c.score * 100)}%）` : ''}`
            }
          >
            {STEP_LABELS[c.by] ?? c.by}
          </span>
          {/* 元が空＝コメントに書かれていなかったもの。ここを黙って埋めると出所が分からなくなる */}
          {c.from ? (
            <span className="text-gray-400 line-through">{c.from}</span>
          ) : (
            <span className="text-gray-400">（未記入）</span>
          )}
          <span className="text-gray-300">→</span>
          <span className="font-medium text-gray-800">{c.to}</span>
          {c.by !== 'ai_normalize' && c.reason && (
            <span className="text-xs text-gray-400">{matchReasonLabel(c.reason)}</span>
          )}
          {/* AI が決めた段は確信度を本文に出す。tooltip だけだと気づかない */}
          {c.by === AI_STEP && !!c.score && (
            <span className="text-xs font-medium text-amber-700">{Math.round(c.score * 100)}%</span>
          )}
        </div>
      ))}
    </div>
  );
}
