import { Link } from 'react-router-dom';
import { usePlayerStore } from '../store/player';
import type { PerformanceListItem } from '../utils/performanceList';
import { formatTimeInput } from '../utils/timeFormat';
import ArtistLinks from './ArtistLinks';
import RestrictedBadge from './RestrictedBadge';
import Tag from './ui/Tag';
import PerformanceActions, { type PerformanceListAction } from './PerformanceActions';

/** 配信横断の歌唱一覧。曲一覧は曲を、同じ曲の履歴は配信を主見出しにする。 */
export default function PerformanceList({ items, emphasis = 'song', channelId, numbered = false, actionsFor }: {
  items: PerformanceListItem[];
  emphasis?: 'song' | 'stream';
  channelId?: string;
  numbered?: boolean;
  actionsFor?: (item: PerformanceListItem, index: number) => PerformanceListAction[];
}) {
  const currentId = usePlayerStore((state) => state.queue[state.index]?.performanceId);
  const playing = usePlayerStore((state) => state.playing);

  return (
    <ul aria-label="歌唱リスト" className="divide-y divide-gray-100 rounded-xl border border-gray-200 bg-white shadow-sm">
      {items.map((item, index) => {
        const { track } = item;
        const current = currentId === track.performanceId;
        const isPlaying = current && playing;
        const image = emphasis === 'stream' ? item.thumbnailUrl : track.artUrl || item.thumbnailUrl;
        const title = emphasis === 'stream' ? track.streamTitle || '配信を見る' : track.songName;
        const titleHref = emphasis === 'stream' ? `/streams/${track.streamId}` : track.songId ? `/songs/${track.songId}` : undefined;
        const singers = track.singers.length === 1 && track.singers[0].id === channelId ? [] : track.singers;
        const duration = track.end > track.start ? formatTimeInput(track.end - track.start) : '—';
        const playLabel = isPlaying ? `${track.songName} を一時停止` : current ? `${track.songName} の再生を再開` : `${track.songName} から連続再生`;
        return (
          <li key={track.performanceId} aria-current={current ? 'true' : undefined}
            className={`group grid grid-cols-[3rem_minmax(0,1fr)_5rem] items-start gap-x-3 gap-y-1 px-3 py-4 first:rounded-t-xl last:rounded-b-xl sm:px-4 md:grid-cols-[3.5rem_minmax(0,1fr)_4rem_5rem] ${current ? 'bg-indigo-50/70 shadow-[inset_3px_0_0_#7c3aed]' : 'hover:bg-gray-50/80'}`}>
            <button type="button" aria-label={playLabel} title={playLabel}
              onClick={() => {
                if (current) usePlayerStore.getState().setPlaying(!playing);
                else usePlayerStore.getState().playTracks(items.map((entry) => entry.track), index);
              }}
              className="relative row-span-3 flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-indigo-50 text-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 md:h-14 md:w-14">
              {image && <img src={image} alt="" loading="lazy" className="absolute inset-0 h-full w-full object-cover" />}
              <span className={`relative flex h-7 w-7 items-center justify-center rounded-full ${image ? 'bg-black/55 text-white' : 'bg-white/80'} ${isPlaying ? 'bg-indigo-600 text-white' : ''}`}>
                <svg className="h-4 w-4" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                  <path d={isPlaying ? 'M6 5h4v14H6zm8 0h4v14h-4z' : 'M8 5v14l11-7z'} />
                </svg>
              </span>
              {numbered && <span className="absolute bottom-0 left-0 rounded-tr bg-black/70 px-1 text-[10px] tabular-nums text-white">{index + 1}</span>}
            </button>

            <div className="min-w-0 self-center">
              {titleHref ? <Link to={titleHref} title={title} className="line-clamp-2 break-words text-sm font-semibold text-gray-900 hover:text-indigo-600 sm:text-base">{title}</Link>
                : <span className="line-clamp-2 break-words text-sm font-semibold text-gray-900 sm:text-base">{title}</span>}
              {emphasis === 'song' && <ArtistLinks artists={track.artists} fallback={track.artist} className="mt-0.5 block truncate text-xs text-gray-500" linkClassName="hover:text-indigo-600" />}
            </div>

            <span className="col-start-3 row-span-3 row-start-1 hidden self-center text-right font-mono text-xs tabular-nums text-gray-500 md:block" aria-label={`歌唱の長さ ${duration}`}>{duration}</span>
            <div className="col-start-3 row-start-1 -mr-1 md:col-start-4 md:row-span-3 md:self-center"><PerformanceActions item={item} actions={actionsFor?.(item, index)} /></div>

            <div className="col-span-2 col-start-2 min-w-0 text-xs leading-5 text-gray-500 md:col-span-1">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                {singers.length > 0 && <span className="min-w-0 break-words [overflow-wrap:anywhere]" aria-label="歌唱者">
                  {singers.map((singer, singerIndex) => <span key={singer.id}>{singerIndex > 0 && '・'}<Link to={`/channels/${singer.id}`} className="text-gray-600 hover:text-indigo-600">{singer.name}</Link></span>)}
                </span>}
                {track.streamDate && <time dateTime={track.streamDate}>{new Date(track.streamDate).toLocaleDateString('ja-JP')}</time>}
                <span className="font-mono text-gray-400 md:hidden" aria-label={`歌唱の長さ ${duration}`}>{duration}</span>
                {item.restricted && <RestrictedBadge />}
                {current && <span className="font-medium text-indigo-600">{playing ? '再生中' : '一時停止中'}</span>}
              </div>
              <Link to={`/streams/${track.streamId}`} title={`${track.streamTitle ?? '配信'} · ${formatTimeInput(track.start)}から`} className="flex min-w-0 items-baseline gap-1 text-gray-500 hover:text-indigo-600">
                {emphasis === 'song' && <><span className="truncate">{track.streamTitle || '配信を見る'}</span><span aria-hidden="true">·</span></>}
                <span className="shrink-0 tabular-nums">{formatTimeInput(track.start)}から</span>
              </Link>
            </div>
            {item.tags.length > 0 && <div className="col-span-2 col-start-2 mt-1 flex min-w-0 flex-wrap gap-1 [overflow-wrap:anywhere] md:col-span-1">
              {item.tags.map((tag) => <Tag key={tag.id} label={tag.display_name} color={tag.color} />)}
            </div>}
          </li>
        );
      })}
    </ul>
  );
}
