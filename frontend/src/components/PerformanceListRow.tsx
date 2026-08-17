import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import QueueAddButton from './QueueAddButton';
import ReportButton from './ReportButton';
import type { PlayerTrack } from '../store/player';

// 歌唱 1 件を縦積みで出す行（狭い画面向け）。
//
// **表をやめる。** 表の利点は列が揃って見比べられることだが、390px に 5〜7 列は
// 入らず、実際は横スクロール＋曲名やアーティストが 3〜4 行に折り返す形になっていて、
// 揃っている利点はとうに失われていた。縦に積んで各行を truncate する。
//
// 配信のセットリストと歌手の歌唱一覧で共有する。3 行目に出す中身は違う
// （片方は時間と歌手とタグ、もう片方は日付と歌枠）ので meta は呼び出し側から
// 渡すが、**押したときに何が拾うか**という骨格はここに 1 つだけ置く
// ── そこが一番間違えやすい。
//
// 行全体が再生ボタン。**背面に敷いた button と、その上に載る Link / 操作ボタン**
// という形にしてある（button の中に a や button を入れると不正な HTML になる）。
//
// クリックを通す/通さないは**各要素に直接**書くこと。親に `[&>*]:pointer-events-none`
// を置くと `li > *` になり、子が自分で付ける `pointer-events-auto` と詳細度が並んで
// Tailwind の出力順で勝敗が決まる ── 実際それで**行全体が押せなくなっていた**
// （孫要素の曲名リンクだけ生き残るので、動いているように見えてしまう）。
export default function PerformanceListRow({
  track,
  thumbnailUrl,
  badge,
  meta,
  youtubeUrl,
  playLabel,
  onPlay,
}: {
  track: PlayerTrack; // キュー追加・報告・曲名・アーティストの出どころ
  thumbnailUrl?: string;
  badge?: string; // 曲順など。無ければ出さない
  // 3 行目以降（時間・歌手・タグ／日付・歌枠 など）。**並べ方は呼び出し側が決める**
  // ── 配信詳細は 1 行に収まるが、歌手一覧は歌枠名が入るので 2 行に割る必要があり、
  // ここで flex を決め打ちすると入りきらない方が数文字に潰れる
  meta: ReactNode;
  youtubeUrl: string;
  playLabel: string; // 読み上げ文。行の役割が画面ごとに違う（ここから再生／連続再生）
  onPlay: () => void;
}) {
  return (
    <li className="relative flex items-start gap-3 px-3 py-2.5">
      {/* 行全体の再生ボタン（背面）。上に載る Link と操作ボタンが先に拾う */}
      <button
        type="button"
        onClick={onPlay}
        className="absolute inset-0 active:bg-gray-100"
        aria-label={playLabel}
      />

      {/* サムネイル：番号と再生の目印 */}
      <span className="pointer-events-none relative z-10 block h-14 w-14 shrink-0 overflow-hidden rounded-lg bg-gray-100 shadow-sm">
        {thumbnailUrl ? (
          <img src={thumbnailUrl} alt="" loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <span className="flex h-full w-full items-center justify-center">
            <svg className="h-7 w-7 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
            </svg>
          </span>
        )}
        {badge && (
          <span className="absolute left-0 top-0 rounded-br-lg bg-indigo-600 px-1.5 py-0.5 text-[10px] font-bold text-white">
            {badge}
          </span>
        )}
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
        {track.songId ? (
          <Link
            to={`/songs/${track.songId}`}
            className="pointer-events-auto block truncate font-medium text-gray-900"
          >
            {track.songName}
          </Link>
        ) : (
          <span className="block truncate font-medium text-gray-900">{track.songName}</span>
        )}
        {track.artist && <span className="block truncate text-xs text-gray-500">{track.artist}</span>}
        <span className="mt-1 block text-[11px] text-gray-400">{meta}</span>
      </span>

      {/* 操作。再生は行タップに譲ったので 3 つに収まる。
          指で押すので 40px にそろえる（表の中の 32px アイコンのままだと外しやすい） */}
      <span className="relative z-10 flex shrink-0 items-center">
        <QueueAddButton track={track} size="md" />
        <ReportButton
          track={track}
          className="inline-flex h-10 w-10 items-center justify-center rounded-full text-gray-400 active:bg-indigo-50 active:text-indigo-600"
        />
        <a
          href={youtubeUrl}
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

// 重なった歌手アイコン（3 人まで + 「+N」）。3 行目のメタとして使う。
// SingerAvatars は中身が Link なので、行タップの上には置けない
export function RowSingerAvatars({ singers }: { singers: { id: string; name: string; photo_url?: string }[] }) {
  if (singers.length === 0) return null;
  return (
    <span className="flex shrink-0 -space-x-1.5">
      {singers.slice(0, 3).map((singer) => (
        <span
          key={singer.id}
          title={singer.name}
          className="inline-flex h-5 w-5 items-center justify-center overflow-hidden rounded-full border border-white bg-indigo-100 text-[8px] font-semibold text-indigo-700"
        >
          <RowSingerImage singer={singer} />
        </span>
      ))}
      {singers.length > 3 && (
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full border border-white bg-gray-600 px-1 text-[8px] font-semibold text-white">
          +{singers.length - 3}
        </span>
      )}
    </span>
  );
}

function RowSingerImage({ singer }: { singer: { id: string; name: string; photo_url?: string } }) {
  return (
    <img
      src={singer.photo_url || `https://holodex.net/statics/channelImg/${singer.id}/50.png`}
      alt=""
      loading="lazy"
      className="h-full w-full object-cover"
      onError={(e) => {
        e.currentTarget.style.display = 'none';
      }}
    />
  );
}
