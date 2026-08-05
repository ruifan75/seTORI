import type { PlaylistVisibility } from '../api/types';

const LABELS: Record<PlaylistVisibility, { text: string; className: string; title: string }> = {
  private: {
    text: '非公開',
    className: 'bg-gray-100 text-gray-600',
    title: '自分だけが見られます',
  },
  unlisted: {
    text: '限定公開',
    className: 'bg-amber-100 text-amber-800',
    title: '共有リンクを知っている人だけが見られます（一覧には出ません）',
  },
  public: {
    text: '公開',
    className: 'bg-green-100 text-green-700',
    title: '誰でも見られ、公開一覧に掲載されます',
  },
};

// プレイリストの公開範囲バッジ。誤って公開しないよう、状態を常に明示する。
export default function VisibilityBadge({ visibility }: { visibility: PlaylistVisibility }) {
  const label = LABELS[visibility];
  return (
    <span
      className={`shrink-0 px-2 py-0.5 text-xs font-medium rounded ${label.className}`}
      title={label.title}
    >
      {label.text}
    </span>
  );
}
