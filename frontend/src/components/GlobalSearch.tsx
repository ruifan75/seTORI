import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation } from '@tanstack/react-query';
import { searchApi, holodexApi } from '../api/client';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { useToast } from './ui/Toast';

// GlobalSearch 統一検索ボックス。
// 楽曲・歌枠・チャンネルを横断検索し、YouTube URL / video ID を貼れば該当歌枠へ直行できる。
// 未登録の video ID は sync:run 権限があれば Holodex から同期して開ける。
export default function GlobalSearch({ autoFocus = false }: { autoFocus?: boolean }) {
  const navigate = useNavigate();
  const { showToast } = useToast();
  const canSync = hasPermission(useAuthStore((s) => s.user), PERM.SYNC_RUN);

  const [query, setQuery] = useState('');
  const [debounced, setDebounced] = useState('');
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // 250ms デバウンス
  useEffect(() => {
    const t = setTimeout(() => setDebounced(query.trim()), 250);
    return () => clearTimeout(t);
  }, [query]);

  const { data, isFetching } = useQuery({
    queryKey: ['global-search', debounced],
    queryFn: () => searchApi.global(debounced),
    enabled: debounced.length >= 1,
    staleTime: 1000 * 30,
  });

  // 外側クリックで閉じる
  useEffect(() => {
    const onMouseDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onMouseDown);
    return () => document.removeEventListener('mousedown', onMouseDown);
  }, []);

  const close = () => {
    setOpen(false);
    setQuery('');
  };

  const go = (path: string) => {
    close();
    navigate(path);
  };

  // 未登録動画の同期→遷移
  const syncMutation = useMutation({
    mutationFn: (videoId: string) => holodexApi.syncVideo(videoId),
    onSuccess: (_res, videoId) => {
      showToast('動画を同期しました', 'success');
      go(`/streams/${videoId}`);
    },
    onError: (err: Error) => showToast(`同期エラー: ${err.message}`, 'error'),
  });

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      setOpen(false);
      (e.target as HTMLInputElement).blur();
      return;
    }
    if (e.key === 'Enter' && data) {
      // Enter は最上位の結果へ：動画ID → 楽曲 → 歌枠 → チャンネル
      if (data.video_id && data.video_registered) {
        go(`/streams/${data.video_id}`);
      } else if (data.songs.length > 0) {
        go(`/songs/${data.songs[0].id}`);
      } else if (data.streams.length > 0) {
        go(`/streams/${data.streams[0].id}`);
      } else if (data.singers.length > 0) {
        go(`/singers/${data.singers[0].id}`);
      }
    }
  };

  const showPanel = open && debounced.length >= 1;
  const hasTextResults =
    data &&
    (data.songs.length > 0 ||
      data.streams.length > 0 ||
      data.singers.length > 0 ||
      data.stream_tags.length > 0 ||
      data.performance_tags.length > 0);

  return (
    <div ref={rootRef} className="relative w-full md:w-44 lg:w-64 xl:w-80">
      <div className="relative">
        <svg
          className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none"
          fill="none" stroke="currentColor" viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-4.35-4.35M17 11a6 6 0 11-12 0 6 6 0 0112 0z" />
        </svg>
        <input
          type="text"
          value={query}
          autoFocus={autoFocus}
          onChange={(e) => {
            setQuery(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={handleKeyDown}
          placeholder="検索 / URL・動画IDを貼り付け"
          className="w-full pl-9 pr-3 py-1.5 text-sm border border-gray-300 rounded-lg bg-gray-50 focus:bg-white focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        />
      </div>

      {showPanel && (
        <div className="absolute left-0 right-0 top-full mt-1 z-50 bg-white border border-gray-200 rounded-lg shadow-lg max-h-[70vh] overflow-y-auto">
          {/* YouTube URL / video ID マッチ */}
          {data?.video_id && (
            <div className="p-3 border-b">
              {data.video_registered ? (
                <button
                  onClick={() => go(`/streams/${data.video_id}`)}
                  className="w-full text-left px-3 py-2 rounded-lg bg-indigo-50 hover:bg-indigo-100 transition-colors"
                >
                  <span className="text-sm font-medium text-indigo-700">歌枠を開く</span>
                  <span className="block text-xs text-gray-500 font-mono mt-0.5">{data.video_id}</span>
                </button>
              ) : canSync ? (
                <button
                  onClick={() => syncMutation.mutate(data.video_id!)}
                  disabled={syncMutation.isPending}
                  className="w-full text-left px-3 py-2 rounded-lg bg-indigo-50 hover:bg-indigo-100 transition-colors disabled:opacity-60"
                >
                  <span className="text-sm font-medium text-indigo-700">
                    {syncMutation.isPending ? '同期中...' : '未登録の動画 — Holodexから同期して開く'}
                  </span>
                  <span className="block text-xs text-gray-500 font-mono mt-0.5">{data.video_id}</span>
                </button>
              ) : (
                <div className="px-3 py-2 text-sm text-gray-500">
                  未登録の動画IDです（<span className="font-mono text-xs">{data.video_id}</span>）
                </div>
              )}
            </div>
          )}

          {/* テキスト検索結果 */}
          {data && !data.video_id && (
            <>
              {(data.stream_tags.length > 0 || data.performance_tags.length > 0) && (
                <div className="py-1">
                  <div className="px-3 pt-2 pb-1 text-xs font-medium text-gray-400">タグ</div>
                  <div className="px-3 pb-2 flex flex-wrap gap-1.5">
                    {data.stream_tags.map((tag) => (
                      <button
                        key={`s-${tag.id}`}
                        onClick={() => go(`/tags/stream/${encodeURIComponent(tag.id)}`)}
                        className="inline-flex items-center gap-1 px-2.5 py-1 text-sm border rounded-full hover:opacity-75 transition-opacity"
                        style={{
                          backgroundColor: (tag.color || '#6366F1') + '20',
                          color: tag.color || '#6366F1',
                          borderColor: (tag.color || '#6366F1') + '40',
                        }}
                        title="配信タグ"
                      >
                        {tag.display_name}
                        <span className="text-xs opacity-70">配信 {tag.count}</span>
                      </button>
                    ))}
                    {data.performance_tags.map((tag) => (
                      <button
                        key={`p-${tag.id}`}
                        onClick={() => go(`/tags/performance/${encodeURIComponent(tag.id)}`)}
                        className="inline-flex items-center gap-1 px-2.5 py-1 text-sm border rounded-full hover:opacity-75 transition-opacity"
                        style={{
                          backgroundColor: (tag.color || '#9932CC') + '20',
                          color: tag.color || '#9932CC',
                          borderColor: (tag.color || '#9932CC') + '40',
                        }}
                        title="演出タグ"
                      >
                        {tag.display_name}
                        <span className="text-xs opacity-70">演出 {tag.count}</span>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {data.songs.length > 0 && (
                <div className="py-1">
                  <div className="px-3 pt-2 pb-1 text-xs font-medium text-gray-400">楽曲</div>
                  {data.songs.map((song) => (
                    <button
                      key={song.id}
                      onClick={() => go(`/songs/${song.id}`)}
                      className="w-full text-left px-3 py-2 hover:bg-gray-50 transition-colors"
                    >
                      <span className="text-sm text-gray-900 block truncate">{song.name}</span>
                      <span className="text-xs text-gray-500 block truncate">
                        {song.original_artist}
                        {song.performance_count > 0 && ` · ${song.performance_count}回`}
                      </span>
                    </button>
                  ))}
                </div>
              )}

              {data.streams.length > 0 && (
                <div className="py-1 border-t first:border-t-0">
                  <div className="px-3 pt-2 pb-1 text-xs font-medium text-gray-400">歌枠</div>
                  {data.streams.map((stream) => (
                    <button
                      key={stream.id}
                      onClick={() => go(`/streams/${stream.id}`)}
                      className="w-full text-left px-3 py-2 hover:bg-gray-50 transition-colors flex items-center gap-2"
                    >
                      {stream.thumbnail_url && (
                        <img src={stream.thumbnail_url} alt="" className="w-12 h-7 object-cover rounded shrink-0" />
                      )}
                      <span className="min-w-0">
                        <span className="text-sm text-gray-900 block truncate">{stream.title}</span>
                        <span className="text-xs text-gray-500">
                          {new Date(stream.stream_date).toLocaleDateString('ja-JP')}
                          {stream.is_hidden && ' · 非表示'}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              )}

              {data.artists.length > 0 && (
                <div className="py-1 border-t first:border-t-0">
                  <div className="px-3 pt-2 pb-1 text-xs font-medium text-gray-400">アーティスト</div>
                  {data.artists.map((artist) => (
                    <button
                      key={artist.id}
                      onClick={() => go(`/artists/${artist.id}`)}
                      className="w-full text-left px-3 py-2 hover:bg-gray-50 transition-colors"
                    >
                      <span className="text-sm text-gray-900 block truncate">
                        {artist.name}
                        <span className="text-xs text-gray-400 ml-2">{artist.song_count}曲</span>
                      </span>
                      {artist.name_reading && (
                        <span className="text-xs text-gray-500 block truncate">{artist.name_reading}</span>
                      )}
                    </button>
                  ))}
                </div>
              )}

              {data.singers.length > 0 && (
                <div className="py-1 border-t first:border-t-0">
                  <div className="px-3 pt-2 pb-1 text-xs font-medium text-gray-400">チャンネル</div>
                  {data.singers.map((singer) => (
                    <button
                      key={singer.id}
                      onClick={() => go(`/singers/${singer.id}`)}
                      className="w-full text-left px-3 py-2 hover:bg-gray-50 transition-colors flex items-center gap-2"
                    >
                      {singer.photo_url && (
                        <img src={singer.photo_url} alt="" className="w-7 h-7 rounded-full object-cover shrink-0" />
                      )}
                      <span className="text-sm text-gray-900 truncate">{singer.name}</span>
                    </button>
                  ))}
                </div>
              )}

              {!hasTextResults && !isFetching && (
                <div className="px-3 py-4 text-sm text-gray-400 text-center">結果がありません</div>
              )}
            </>
          )}

          {isFetching && !data && (
            <div className="px-3 py-4 text-sm text-gray-400 text-center">検索中...</div>
          )}
        </div>
      )}
    </div>
  );
}
