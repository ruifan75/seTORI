import { useEffect, useState } from 'react';
import TimestampTweaker from './TimestampTweaker';
import LoginToSuggest from './LoginToSuggest';
import {
  usePerformanceTiming,
  formatSeconds,
  parseSeconds,
  type TimingTarget,
} from './usePerformanceTiming';

// 歌唱の開始/終了をじっくり直すためのダイアログ。
// プレイヤーの通報パネルの「詳しく直す」と、曲詳細の歌唱一覧から開く。
//
// 編集権限があれば保存、無ければ修正提案として送られる（判定は usePerformanceTiming）。
// currentTime を渡すと「再生位置を使う」が有効になり、TimestampTweaker に赤い再生位置が出る。
export default function PerformanceTimingDialog({
  target,
  subtitle,
  currentTime,
  onClose,
}: {
  target: TimingTarget;
  subtitle?: string; // 配信タイトルなど、どの歌唱かを見分けるための補足
  currentTime?: number | null;
  onClose: () => void;
}) {
  const { canEdit, canSubmit, submit } = usePerformanceTiming();
  const [start, setStart] = useState(target.start);
  const [end, setEnd] = useState(target.end);
  const [startText, setStartText] = useState(formatSeconds(target.start));
  const [endText, setEndText] = useState(target.end === 0 ? '' : formatSeconds(target.end));
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);

  // Esc で閉じる（プレイヤー操作の邪魔をしないよう、確定は明示ボタンのみ）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const applyStart = (v: number) => {
    setStart(v);
    setStartText(formatSeconds(v));
  };
  const applyEnd = (v: number) => {
    setEnd(v);
    setEndText(v === 0 ? '' : formatSeconds(v));
  };

  const changed = start !== target.start || end !== target.end;

  const handleSubmit = async () => {
    setBusy(true);
    try {
      const change = {
        ...(start !== target.start ? { start } : {}),
        ...(end !== target.end ? { end } : {}),
      };
      if (await submit(target, change, note)) onClose();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-xl shadow-xl w-full max-w-md p-5 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4">
          <h2 className="text-lg font-bold text-gray-900 break-words">{target.songName}</h2>
          {subtitle && <p className="text-xs text-gray-500 mt-0.5 line-clamp-2">{subtitle}</p>}
          <p className="text-xs text-gray-400 mt-1">
            {canEdit ? '保存すると即座に反映されます' : '管理者への修正提案として送られます'}
          </p>
        </div>

        {!canSubmit && (
          <div className="rounded-lg bg-gray-50 border p-3">
            <LoginToSuggest />
          </div>
        )}

        <TimeRow
          label="開始"
          value={start}
          text={startText}
          onText={setStartText}
          onCommitText={(v) => applyStart(v)}
          onTweak={applyStart}
          currentTime={currentTime}
          mode="start"
          original={target.start}
        />

        <TimeRow
          label="終了"
          value={end}
          text={endText}
          onText={setEndText}
          onCommitText={(v) => applyEnd(v)}
          onTweak={applyEnd}
          currentTime={currentTime}
          mode="end"
          original={target.end}
          emptyHint="空欄 = 動画の最後まで"
          allowEmpty
        />

        {!canEdit && (
          <input
            type="text"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="提案の理由（任意）"
            className="mt-4 w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
          />
        )}

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
            onClick={handleSubmit}
            disabled={busy || !changed || !canSubmit}
            className="px-4 py-2 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 disabled:opacity-50"
          >
            {busy ? '送信中...' : canEdit ? '保存' : '提案を送信'}
          </button>
        </div>
      </div>
    </div>
  );
}

// 1つの時刻（開始 or 終了）の入力行：テキスト入力 + 再生位置の取り込み + ±6秒の微調整。
function TimeRow({
  label,
  value,
  text,
  onText,
  onCommitText,
  onTweak,
  currentTime,
  mode,
  original,
  emptyHint,
  allowEmpty = false,
}: {
  label: string;
  value: number;
  text: string;
  onText: (v: string) => void;
  onCommitText: (v: number) => void;
  onTweak: (v: number) => void;
  currentTime?: number | null;
  mode: 'start' | 'end';
  original: number;
  emptyHint?: string;
  allowEmpty?: boolean;
}) {
  const [error, setError] = useState('');

  const commit = () => {
    if (allowEmpty && text.trim() === '') {
      setError('');
      onCommitText(0);
      return;
    }
    const parsed = parseSeconds(text);
    if (parsed === null) {
      setError('1:23:45 / 2:30 / 150 の形式で入力してください');
      return;
    }
    setError('');
    onCommitText(parsed);
  };

  return (
    <div className="mt-4">
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-600 w-10 shrink-0">{label}</span>
        <input
          type="text"
          value={text}
          onChange={(e) => onText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commit();
            }
          }}
          placeholder={emptyHint ?? '0:00'}
          className="w-28 px-2 py-1.5 text-sm font-mono border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
        {currentTime != null && (
          <button
            type="button"
            onClick={() => onTweak(Math.round(currentTime))}
            className="px-2 py-1.5 text-xs bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200"
            title={`再生位置 ${formatSeconds(currentTime)} を${label}にする`}
          >
            再生位置（{formatSeconds(currentTime)}）
          </button>
        )}
        {value !== original && (
          <span className="text-xs text-indigo-600 font-mono">
            {original === 0 ? '最後まで' : formatSeconds(original)} →{' '}
            {value === 0 ? '最後まで' : formatSeconds(value)}
          </span>
        )}
      </div>
      {error && <p className="text-xs text-red-600 mt-1 ml-12">{error}</p>}
      {/* 0（＝最後まで）のときは基準が無いので微調整スライダーは出さない */}
      {value > 0 && (
        <div className="ml-12">
          <TimestampTweaker value={value} mode={mode} onChange={onTweak} currentTime={currentTime} />
        </div>
      )}
    </div>
  );
}
