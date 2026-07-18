// テーブル・カード一覧の共通並び替え UI。
// SortableTh: 表頭クリックで昇順→降順をトグル。SortControl: カード一覧向けのドロップダウン。

export type SortDir = 'asc' | 'desc';

export interface SortState {
  sort: string;
  dir: SortDir;
}

// 同じ列を再クリックしたら方向を反転、別の列なら firstDir（既定 asc）で開始。
function nextSort(current: SortState, key: string, firstDir: SortDir = 'asc'): SortState {
  if (current.sort === key) {
    return { sort: key, dir: current.dir === 'asc' ? 'desc' : 'asc' };
  }
  return { sort: key, dir: firstDir };
}

interface SortableThProps {
  label: string;
  sortKey: string;
  sort: string;
  dir: SortDir;
  onSort: (next: SortState) => void;
  align?: 'left' | 'right';
  firstDir?: SortDir;
  className?: string;
}

// クリックで並び替えできる <th>。アクティブ列には昇順/降順の矢印を表示。
export function SortableTh({
  label,
  sortKey,
  sort,
  dir,
  onSort,
  align = 'left',
  firstDir = 'asc',
  className = '',
}: SortableThProps) {
  const active = sort === sortKey;
  return (
    <th
      className={`px-4 sm:px-6 py-3 text-xs font-medium text-gray-500 uppercase tracking-wider ${
        align === 'right' ? 'text-right' : 'text-left'
      } ${className}`}
    >
      <button
        type="button"
        onClick={() => onSort(nextSort({ sort, dir }, sortKey, firstDir))}
        aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
        className={`inline-flex items-center gap-1 group select-none transition-colors ${
          align === 'right' ? 'flex-row-reverse' : ''
        } ${active ? 'text-indigo-600' : 'hover:text-gray-700'}`}
        title={`${label}で並び替え`}
      >
        <span>{label}</span>
        <SortArrow active={active} dir={dir} />
      </button>
    </th>
  );
}

function SortArrow({ active, dir }: { active: boolean; dir: SortDir }) {
  if (!active) {
    // 非アクティブ列は薄い上下矢印でソート可能を示唆
    return (
      <svg className="w-3 h-3 text-gray-300 group-hover:text-gray-400" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
        <path d="M8 3l3 3.5H5L8 3zM8 13l-3-3.5h6L8 13z" />
      </svg>
    );
  }
  return (
    <svg className="w-3 h-3" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      {dir === 'asc' ? <path d="M8 4l4 5H4l4-5z" /> : <path d="M8 12L4 7h8l-4 5z" />}
    </svg>
  );
}

interface SortOption {
  value: string;
  label: string;
  firstDir?: SortDir;
}

interface SortControlProps {
  options: SortOption[];
  sort: string;
  dir: SortDir;
  onSort: (next: SortState) => void;
}

// カード一覧（表頭が無い）向けの並び替えコントロール：項目セレクト＋昇降トグル。
export function SortControl({ options, sort, dir, onSort }: SortControlProps) {
  const current = options.find((o) => o.value === sort) ?? options[0];
  return (
    <div className="inline-flex items-center gap-1.5 text-sm shrink-0">
      <span className="text-gray-400">並び替え</span>
      <select
        value={sort}
        onChange={(e) => {
          const opt = options.find((o) => o.value === e.target.value);
          onSort({ sort: e.target.value, dir: opt?.firstDir ?? 'asc' });
        }}
        className="px-2 py-1.5 border border-gray-300 rounded-lg bg-white text-gray-700 focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      <button
        type="button"
        onClick={() => onSort({ sort, dir: dir === 'asc' ? 'desc' : 'asc' })}
        title={dir === 'asc' ? '昇順（クリックで降順）' : '降順（クリックで昇順）'}
        className="px-2 py-1.5 border border-gray-300 rounded-lg bg-white text-gray-600 hover:bg-gray-50"
      >
        {dir === 'asc' ? '▲' : '▼'}
        <span className="sr-only">{current.label}の{dir === 'asc' ? '昇順' : '降順'}</span>
      </button>
    </div>
  );
}
