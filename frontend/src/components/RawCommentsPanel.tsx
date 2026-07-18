import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { commentApi } from '../api/client';

// 編集ページ「生コメント」タブの中身。
// コメント分析が誤ったときに原文を確認し、行内のタイムスタンプから直接曲を追加する。
// - タイムスタンプをクリック → プレイヤーをその秒数へシーク
// - ＋ボタン → その行から曲名/アーティストを推測して編集リストへ追加（終了時間は呼び出し側で chat 推定）

interface AddSongInput {
  start: number;
  name: string;
  artist: string;
}

interface Props {
  videoId: string;
  onSeek: (seconds: number) => void;
  onAddSong: (input: AddSongInput) => void;
}

// 行内のタイムスタンプ（H:MM:SS / MM:SS）を全部拾う
const TS_RE = /(\d{1,2}):(\d{2})(?::(\d{2}))?/g;
// g フラグ付き正規表現の .test() は lastIndex が残って交互に false になるため、判定用は別に持つ
const HAS_TS_RE = /\d{1,2}:\d{2}/;

function tsToSeconds(m: RegExpMatchArray): number {
  const [, a, b, c] = m;
  return c !== undefined
    ? parseInt(a) * 3600 + parseInt(b) * 60 + parseInt(c)
    : parseInt(a) * 60 + parseInt(b);
}

// 行のテキストから曲名/アーティストを推測する。
// 最後のタイムスタンプより後ろを取り、先頭の記号を落として「/」「-」等で分割。
function guessSong(line: string): { name: string; artist: string } {
  let text = line;
  let lastEnd = -1;
  for (const m of line.matchAll(TS_RE)) {
    lastEnd = (m.index ?? 0) + m[0].length;
  }
  if (lastEnd >= 0) text = line.slice(lastEnd);
  text = text.replace(/^[\s~〜\-–—・:：.。,、)）\]】>》]+/, '').trim();

  for (const sep of [' / ', '／', ' - ', ' − ', ' – ', '「']) {
    const idx = text.indexOf(sep);
    if (idx > 0) {
      if (sep === '「') {
        // 「曲名」アーティスト の形（アーティスト「曲名」も多いので前半をアーティストに）
        const close = text.indexOf('」', idx);
        if (close > idx) {
          return { name: text.slice(idx + 1, close).trim(), artist: text.slice(0, idx).trim() };
        }
        continue;
      }
      return { name: text.slice(0, idx).trim(), artist: text.slice(idx + sep.length).trim() };
    }
  }
  return { name: text, artist: '' };
}

// 1行分：タイムスタンプをチップ化し、＋で曲追加
function CommentLine({ line, onSeek, onAddSong }: { line: string; onSeek: Props['onSeek']; onAddSong: Props['onAddSong'] }) {
  const matches = [...line.matchAll(TS_RE)];
  if (matches.length === 0) {
    return <p className="text-gray-600 whitespace-pre-wrap break-words">{line}</p>;
  }

  // テキストをタイムスタンプで分割してチップを差し込む
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  matches.forEach((m, i) => {
    const idx = m.index ?? 0;
    if (idx > cursor) parts.push(<span key={`t${i}`}>{line.slice(cursor, idx)}</span>);
    const secs = tsToSeconds(m);
    parts.push(
      <button
        key={`ts${i}`}
        onClick={() => onSeek(secs)}
        className="inline-flex items-center px-1.5 rounded bg-indigo-50 text-indigo-700 font-mono text-xs hover:bg-indigo-100 transition-colors align-baseline"
        title="この時間にジャンプ"
      >
        {m[0]}
      </button>
    );
    cursor = idx + m[0].length;
  });
  if (cursor < line.length) parts.push(<span key="tail">{line.slice(cursor)}</span>);

  const firstSecs = tsToSeconds(matches[0]);
  const guess = guessSong(line);

  return (
    <div className="group flex items-start gap-1">
      <p className="flex-1 text-gray-800 whitespace-pre-wrap break-words leading-6">{parts}</p>
      <button
        onClick={() => onAddSong({ start: firstSecs, ...guess })}
        className="shrink-0 inline-flex items-center justify-center w-6 h-6 rounded-full text-gray-300 group-hover:text-indigo-600 hover:bg-indigo-50 transition-colors"
        title={`この行から曲を追加（${guess.name || '曲名なし'}・終了時間は chat から推定）`}
      >
        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
          <path d="M12 5v14m-7-7h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" fill="none" />
        </svg>
      </button>
    </div>
  );
}

export default function RawCommentsPanel({ videoId, onSeek, onAddSong }: Props) {
  const [filter, setFilter] = useState('');
  const [tsOnly, setTsOnly] = useState(true);

  // DB キャッシュ優先（comment_raw）なので通常は軽い
  const { data, isLoading, isError } = useQuery({
    queryKey: ['raw-comments', videoId],
    queryFn: () => commentApi.getComments(videoId),
    staleTime: Infinity,
  });

  const comments = useMemo(() => data?.comments ?? [], [data?.comments]);

  // コメント→行に分解してフィルタ。元コメントの区切りは保つ
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return comments
      .map((c) => {
        let lines = c.split('\n');
        if (tsOnly) lines = lines.filter((l) => HAS_TS_RE.test(l));
        if (q) lines = lines.filter((l) => l.toLowerCase().includes(q));
        return lines;
      })
      .filter((lines) => lines.length > 0);
  }, [comments, filter, tsOnly]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-3">
        <input
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="コメントを検索..."
          className="flex-1 px-2 py-1 text-sm border border-gray-300 rounded focus:ring-1 focus:ring-indigo-500 focus:border-indigo-500"
        />
        <label className="flex items-center gap-1.5 text-xs text-gray-600 whitespace-nowrap cursor-pointer">
          <input
            type="checkbox"
            checked={tsOnly}
            onChange={(e) => setTsOnly(e.target.checked)}
            className="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
          />
          タイムスタンプ行のみ
        </label>
      </div>

      <div className="space-y-2 text-sm">
        {isLoading ? (
          <p className="text-gray-400 py-2">読み込み中...</p>
        ) : isError ? (
          <p className="text-red-500 py-2">コメントを取得できませんでした</p>
        ) : filtered.length === 0 ? (
          <p className="text-gray-400 py-2">
            {comments.length === 0 ? 'コメントがありません' : '条件に合う行がありません'}
          </p>
        ) : (
          filtered.map((lines, i) => (
            <div key={i} className="rounded border bg-gray-50/60 p-2 space-y-0.5">
              {lines.map((line, j) => (
                <CommentLine key={j} line={line} onSeek={onSeek} onAddSong={onAddSong} />
              ))}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
