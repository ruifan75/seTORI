import { Link } from 'react-router-dom';
import type { Performance } from '../api/types';
import ArtistLinks from './ArtistLinks';
import QueueAddButton from './QueueAddButton';
import SingerAvatars from './SingerAvatars';
import { performancesToTracks as toTracks } from '../store/player';

interface Props {
  performance: Performance;
  /** 押すとこの歌唱から連続再生する。並びの何番目かは呼び出し側が持つ。 */
  onPlay: () => void;
}

/**
 * 歌唱 1 件のカード（配信サムネイル + 楽曲アート + 曲名 + 歌ったチャンネル）。
 * ホームの「おすすめ」とプリセットプレイリストの横スクロール列で共通に使う。
 */
export default function PerformanceCard({ performance, onPlay }: Props) {
  return (
    <div className="w-56 shrink-0 snap-start bg-white rounded-lg shadow-sm border hover:shadow-md transition-shadow group">
      {/* 配信サムネイル + 右下に CD 風の楽曲アート */}
      <div className="relative">
        <button
          onClick={onPlay}
          className="block w-full relative overflow-hidden rounded-t-lg"
          title="この歌唱から連続再生"
        >
          {performance.thumbnail_url ? (
            <img src={performance.thumbnail_url} alt="" loading="lazy" className="w-full h-32 object-cover" />
          ) : (
            <div className="w-full h-32 bg-gray-200" />
          )}
          <span className="absolute inset-0 bg-black bg-opacity-0 group-hover:bg-opacity-30 flex items-center justify-center transition-all">
            <span className="w-10 h-10 rounded-full bg-indigo-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
              <svg className="w-5 h-5 text-white ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
            </span>
          </span>
        </button>
        {performance.arts && (
          <span className="absolute -bottom-4 right-2 w-14 h-14 pointer-events-none">
            <img src={performance.arts} alt="" loading="lazy" className="w-14 h-14 rounded-full object-cover border-2 border-white shadow-md" />
            {/* CD の中心穴 */}
            <span className="absolute inset-0 m-auto w-3 h-3 rounded-full bg-white border border-gray-300" />
          </span>
        )}
        <QueueAddButton
          track={toTracks([performance])[0]}
          className="absolute top-1.5 right-1.5 bg-white/90 shadow opacity-0 group-hover:opacity-100"
        />
      </div>
      <div className="p-3 pt-4">
        <Link
          to={`/songs/${performance.song_id}`}
          className="block h-10 text-sm font-medium leading-5 text-gray-900 hover:text-indigo-600 line-clamp-2 pr-14"
          title={performance.song_name}
        >
          {performance.song_name}
        </Link>
        <ArtistLinks
          artists={performance.artists}
          fallback={performance.original_artist}
          className="block text-xs text-gray-500 truncate pr-14"
          linkClassName="hover:text-indigo-600"
        />
        <div className="mt-1 flex min-h-7 items-center justify-between gap-2">
          {performance.stream_date ? (
            <time dateTime={performance.stream_date} className="min-w-0 text-xs text-gray-400">
              {new Date(performance.stream_date).toLocaleDateString('ja-JP')}
            </time>
          ) : <span />}
          <SingerAvatars singers={performance.singers} />
        </div>
      </div>
    </div>
  );
}
