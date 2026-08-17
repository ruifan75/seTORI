import { Link } from 'react-router-dom';
import Tag from './ui/Tag';
import QueueAddButton from './QueueAddButton';
import ReportButton from './ReportButton';
import { SingerImage } from './SingerAvatars';
import type { PlayerTrack } from '../store/player';
import type { Performance, Singer } from '../api/types';

// 配信のセットリスト 1 行（狭い画面向け）。
//
// **表をやめる。** 表の利点は列が揃って見比べられることだが、390px に 7 列は
// 入らず、実際は横スクロール＋曲名やアーティストが 3〜4 行に折り返す形になっていて、
// 揃っている利点はとうに失われていた。縦に積んで各行を truncate する。
//
// 行全体が再生ボタン。**背面に敷いた button と、その上に載る Link / 操作ボタン**
// という形にしてある（button の中に a や button を入れると不正な HTML になる）。
//
// クリックを通す/通さないは**各要素に直接**書くこと。親に `[&>*]:pointer-events-none`
// を置くと `li > *` になり、子が自分で付ける `pointer-events-auto` と詳細度が並んで
// Tailwind の出力順で勝敗が決まる ── 実際それで**行全体が押せなくなっていた**
// （孫要素の曲名リンクだけ生き残るので、動いているように見えてしまう）。
export default function PerformanceListRow({
  performance,
  index,
  track,
  channelOwner,
  onPlay,
}: {
  performance: Performance;
  index: number;
  track: PlayerTrack;
  channelOwner?: Singer | null;
  onPlay: () => void;
}) {
  const perf = performance;
  const singers = sortSingers(perf.singers ?? [], channelOwner);
  const tags = [
    ...perf.tags.map((t) => ({ key: t.id, label: t.display_name, color: t.color })),
    ...(perf.custom_tags ?? []).map((t) => ({ key: t, label: t, color: '#6B7280' })),
  ];
  // 3 行目は 1 行に収める。タグが多い配信（アコースティック＋ピアノ＋…）で
  // 行が膨らむと、一覧としてざっと眺められなくなる
  const shownTags = tags.slice(0, 2);
  const restTags = tags.length - shownTags.length;

  return (
    <li className="relative flex items-start gap-3 px-3 py-2.5">
      {/* 行全体の再生ボタン（背面）。上に載る Link と操作ボタンが先に拾う */}
      <button
        type="button"
        onClick={onPlay}
        className="absolute inset-0 active:bg-gray-100"
        aria-label={`${perf.song_name} をここから再生`}
      />

      {/* サムネイル：番号と再生の目印 */}
      <span className="pointer-events-none relative z-10 block h-14 w-14 shrink-0 overflow-hidden rounded-lg bg-gray-100 shadow-sm">
        {perf.arts ? (
          <img src={perf.arts} alt="" loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <span className="flex h-full w-full items-center justify-center">
            <svg className="h-7 w-7 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
            </svg>
          </span>
        )}
        <span className="absolute left-0 top-0 rounded-br-lg bg-indigo-600 px-1.5 py-0.5 text-[10px] font-bold text-white">
          #{index + 1}
        </span>
        {/* 押せば鳴ることを見た目でも言う（行タップ＝再生は説明が要る） */}
        <span className="absolute inset-0 flex items-center justify-center">
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-black/45">
            <svg className="ml-0.5 h-4 w-4 text-white" fill="currentColor" viewBox="0 0 24 24">
              <path d="M8 5v14l11-7z" />
            </svg>
          </span>
        </span>
      </span>

      <span className="pointer-events-none relative z-10 block min-w-0 flex-1">
        {/* 曲ページへは行きたいので、曲名だけはリンクのまま拾わせる */}
        <Link
          to={`/songs/${perf.song_id}`}
          className="pointer-events-auto block truncate font-medium text-gray-900"
        >
          {perf.song_name}
        </Link>
        {perf.original_artist && (
          <span className="block truncate text-xs text-gray-500">{perf.original_artist}</span>
        )}
        <span className="mt-1 flex items-center gap-2 overflow-hidden text-[11px] text-gray-400">
          <span className="shrink-0 font-mono">
            {formatTime(perf.start_seconds)}
            {perf.end_seconds > 0 && `–${formatTime(perf.end_seconds)}`}
          </span>
          {singers.length > 0 && (
            <span className="flex shrink-0 -space-x-1.5">
              {singers.slice(0, 3).map((singer) => (
                <span
                  key={singer.id}
                  title={singer.name}
                  className="inline-flex h-5 w-5 overflow-hidden rounded-full border border-white bg-indigo-100 text-[8px] font-semibold text-indigo-700"
                >
                  <SingerImage singer={singer} />
                </span>
              ))}
              {singers.length > 3 && (
                <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full border border-white bg-gray-600 px-1 text-[8px] font-semibold text-white">
                  +{singers.length - 3}
                </span>
              )}
            </span>
          )}
          {shownTags.map((t) => (
            <span key={t.key} className="shrink-0">
              <Tag label={t.label} color={t.color} />
            </span>
          ))}
          {restTags > 0 && <span className="shrink-0">+{restTags}</span>}
        </span>
      </span>

      {/* 操作。再生は行タップに譲ったので 3 つに収まる */}
      {/* 指で押すので 40px にそろえる（表の中の 32px アイコンのままだと外しやすい） */}
      <span className="relative z-10 flex shrink-0 items-center">
        <QueueAddButton track={track} size="md" />
        <ReportButton
          track={track}
          className="inline-flex h-10 w-10 items-center justify-center rounded-full text-gray-400 active:bg-indigo-50 active:text-indigo-600"
        />
        <a
          href={perf.youtube_url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex h-10 w-10 items-center justify-center rounded-full text-gray-400 active:bg-red-50 active:text-red-600"
          title="YouTubeで開く"
          aria-label="YouTubeで開く"
        >
          <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
            <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
          </svg>
        </a>
      </span>
    </li>
  );
}

// チャンネル所有者を先頭に（表側と同じ並び）
function sortSingers(singers: Singer[], channelOwner?: Singer | null): Singer[] {
  if (!channelOwner) return singers;
  return [...singers].sort((a, b) => {
    if (a.id === channelOwner.id) return -1;
    if (b.id === channelOwner.id) return 1;
    return 0;
  });
}

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`;
}
