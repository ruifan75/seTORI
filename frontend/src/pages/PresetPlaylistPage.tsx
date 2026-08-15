import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { presetPlaylistApi } from '../api/client';
import type { Performance } from '../api/types';
import { usePlayerStore, performancesToTracks } from '../store/player';
import Loading from '../components/ui/Loading';
import QueueAddButton from '../components/QueueAddButton';
import ArtistLinks from '../components/ArtistLinks';
import SingerAvatars from '../components/SingerAvatars';
import PresetActions from '../components/PresetActions';
import { formatTimeInput } from '../utils/timeFormat';

/**
 * プリセットプレイリストの詳細（運営が用意した歌単）。
 *
 * 中身はサーバー側で毎回計算されるので編集も並び替えもできない。
 * 自分で手を入れたい人は「コピー」で通常のプレイリストへ複製する。
 * 誰でも同じものが見えるため、共有は URL をそのまま渡すだけでよい（share_slug は無い）。
 */
export default function PresetPlaylistPage() {
  const { key } = useParams<{ key: string }>();

  const presetQuery = useQuery({
    queryKey: ['presets', 'detail', key],
    queryFn: () => presetPlaylistApi.get(key!),
    enabled: !!key,
    retry: false,
  });

  const itemsQuery = useQuery({
    queryKey: ['preset-items', key, 'all'],
    queryFn: () => presetPlaylistApi.items(key!),
    enabled: !!key,
    retry: false,
  });

  const preset = presetQuery.data;
  const items: Performance[] = itemsQuery.data?.performances ?? [];

  const playFrom = (startIndex: number) => {
    usePlayerStore.getState().playTracks(performancesToTracks(items), startIndex);
  };

  const playShuffled = () => {
    const tracks = performancesToTracks(items);
    for (let i = tracks.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [tracks[i], tracks[j]] = [tracks[j], tracks[i]];
    }
    usePlayerStore.getState().playTracks(tracks, 0);
  };

  if (presetQuery.isLoading) return <Loading />;

  if (presetQuery.isError || !preset) {
    return (
      <div className="text-center py-16 space-y-3">
        <p className="text-gray-900 font-medium">プレイリストが見つかりません</p>
        <Link to="/playlists" className="inline-block text-indigo-600 hover:underline">
          プレイリスト一覧へ
        </Link>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-3xl font-bold text-gray-900">{preset.name}</h1>
          <span className="px-2 py-0.5 text-xs font-medium text-indigo-700 bg-indigo-50 border border-indigo-200 rounded-full">
            プリセット
          </span>
        </div>
        {preset.description && <p className="text-gray-600">{preset.description}</p>}
        <p className="text-sm text-gray-500">
          {preset.item_count} 曲 · 条件に合う歌唱が登録されると自動で増えます
          {/* 読み込み中に「先頭 0 曲」と出さないよう、件数が確定してから注記する */}
          {items.length > 0 && items.length < preset.item_count && `（表示は先頭 ${items.length} 曲）`}
        </p>
      </div>

      {/* 操作 */}
      <div className="flex flex-wrap items-center gap-2">
        <button
          onClick={() => playFrom(0)}
          disabled={items.length === 0}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:bg-gray-300 transition-colors"
        >
          再生
        </button>
        <button
          onClick={playShuffled}
          disabled={items.length === 0}
          className="px-4 py-2 border border-gray-300 rounded-lg hover:bg-gray-50 disabled:text-gray-300 transition-colors"
        >
          ランダム再生
        </button>
        <PresetActions preset={preset} />
      </div>

      {/* 収録曲 */}
      {itemsQuery.isLoading ? (
        <Loading />
      ) : items.length === 0 ? (
        <p className="text-gray-500 py-8 text-center">条件に合う歌唱がまだありません。</p>
      ) : (
        <ul className="space-y-2">
          {items.map((perf, index) => (
            <li
              key={perf.id}
              className="flex items-center gap-3 bg-white border border-gray-200 rounded-lg px-3 py-2"
            >
              <span className="w-8 text-right text-xs font-mono text-gray-400 shrink-0">{index + 1}</span>
              {perf.arts ? (
                <img src={perf.arts} alt="" loading="lazy" className="w-10 h-10 object-cover rounded shrink-0" />
              ) : (
                <span className="w-10 h-10 bg-gray-200 rounded shrink-0" />
              )}
              <div className="flex-1 min-w-0">
                <Link
                  to={`/songs/${perf.song_id}`}
                  className="block font-medium text-gray-900 truncate hover:text-indigo-600"
                >
                  {perf.song_name}
                </Link>
                <span className="block text-sm text-gray-500 truncate">
                  <ArtistLinks artists={perf.artists} fallback={perf.original_artist} />
                </span>
              </div>

              <SingerAvatars singers={perf.singers} />

              <Link
                to={`/streams/${perf.stream_id}`}
                className="hidden sm:block text-xs font-mono text-gray-400 shrink-0 hover:text-indigo-600"
                title={perf.stream_title}
              >
                {perf.stream_date ? new Date(perf.stream_date).toLocaleDateString('ja-JP') : formatTimeInput(perf.start_seconds)}
              </Link>

              <button
                onClick={() => playFrom(index)}
                className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors shrink-0"
                title="ここから再生"
              >
                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M8 5v14l11-7z" />
                </svg>
              </button>

              <QueueAddButton track={performancesToTracks([perf])[0]} className="shrink-0" />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
