import { useEffect, useRef, useState } from 'react';
import { songApi, itunesApi } from '../api/client';
import type { Song, ITunesSearchResult, ITunesQueryResult } from '../api/types';

// 楽曲の検索入力（DB と iTunes を同時に引くオートコンプリート）。
//
// **編集画面と審査画面で同じものを使う。** 別々に書いていた頃、審査側は DB だけを
// 2 文字以上で引いていて、`糸` `恋` `花` のような 1 文字の曲（本番に 14 曲ある）が
// 検索できず、iTunes からの新規登録もできなかった。
// 「同じ操作は同じ実装を通す」ためにここへ出してある。
//
// onSelectSong に渡る Song の id が空文字なら「iTunes にはあるが DB に無い曲」で、
// itunes_ids[0] に紐付けるべき iTunes ID が入っている。
//
// 数字だけを入れると iTunes ID の直引きになる。名前で当たらない曲
// （表記が違う・iTunes 側の綴りが特殊）に手で辿り着くための逃げ道で、
// 楽曲詳細ページの「iTunes ID 直接入力」と同じことをこの欄でできるようにしてある。

// 楽曲検索入力コンポーネントの Props
interface SongSearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSelectSong: (song: Song) => void;
  placeholder?: string;
  showToast?: (message: string, type?: 'success' | 'error' | 'info') => void;
}

// iTunes ID とみなす条件。実在する track ID は 8 桁以上なので、
// `39` `1925` `2525` のような数字だけの曲名を ID と誤認しない。
const ITUNES_ID_PATTERN = /^\d{8,}$/;

const toSearchResult = (q: ITunesQueryResult): ITunesSearchResult => ({
  itunes_id: q.itunes_id,
  collection_name: q.collection_name,
  track_name: q.track_name,
  artist_name: q.artist_name,
  artwork_url: q.artwork_url,
  country: q.country,
  existing_song: q.existing_song,
});

// 楽曲検索入力コンポーネント（オートコンプリート付き）
export default function SongSearchInput({ value, onChange, onSelectSong, placeholder, showToast }: SongSearchInputProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [dbSuggestions, setDbSuggestions] = useState<Song[]>([]);
  const [itunesSuggestions, setItunesSuggestions] = useState<ITunesSearchResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Debounce 検索（DB と iTunes を同時検索）
  useEffect(() => {
    if (searchQuery.length < 1) {
      setDbSuggestions([]);
      setItunesSuggestions([]);
      return;
    }

    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        // DB と iTunes を並行検索。数字だけなら iTunes ID の直引きも足す
        const itunesId = ITUNES_ID_PATTERN.test(searchQuery.trim()) ? Number(searchQuery.trim()) : null;
        const [dbResult, itunesResult, byId] = await Promise.all([
          songApi.list(1, 5, searchQuery).catch(() => ({ songs: [], pagination: { page: 1, limit: 5, total: 0, total_pages: 0 } })),
          itunesApi.search(searchQuery).catch(() => ({ results: [] })),
          itunesId ? itunesApi.queryById(itunesId).catch(() => null) : Promise.resolve(null),
        ]);
        setDbSuggestions(dbResult.songs);
        const results = itunesResult.results.slice(0, 5); // iTunes 結果数を制限
        if (byId && !results.some((r) => r.itunes_id === byId.itunes_id)) {
          results.unshift(toSearchResult(byId));
        }
        setItunesSuggestions(results);
      } catch {
        setDbSuggestions([]);
        setItunesSuggestions([]);
      } finally {
        setIsLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery]);

  // 外部クリックでドロップダウンを閉じる
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        dropdownRef.current &&
        !dropdownRef.current.contains(event.target as Node) &&
        inputRef.current &&
        !inputRef.current.contains(event.target as Node)
      ) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value;
    onChange(newValue);
    setSearchQuery(newValue);
    setIsOpen(true);
  };

  const handleSelectSong = (song: Song) => {
    onSelectSong(song);
    if (showToast) {
      showToast(`「${song.name}」を選択しました`, 'success');
    }
    setIsOpen(false);
    setSearchQuery('');
    setDbSuggestions([]);
    setItunesSuggestions([]);
  };

  const handleSelectItunes = (itunes: ITunesSearchResult) => {
    // この iTunes 結果が既にデータベースに存在する場合、その song に直接紐付ける
    if (itunes.existing_song) {
      const existingSong: Song = {
        id: itunes.existing_song.id,
        name: itunes.existing_song.name,
        name_reading: itunes.existing_song.name_reading,
        original_artist: itunes.existing_song.original_artist,
        original_artist_reading: itunes.existing_song.original_artist_reading,
        artists: [],
        arts: itunes.existing_song.arts,
        performance_count: itunes.existing_song.performance_count,
        created_at: '',
        updated_at: '',
        itunes_ids: [{
          itunes_id: itunes.itunes_id,
          collection_name: itunes.collection_name,
          country: itunes.country,
          is_primary: true,
        }],
      };
      onSelectSong(existingSong);
      if (showToast) {
        showToast(`「${existingSong.name}」を選択しました（データベース内）`, 'success');
      }
    } else {
      // 純粋な iTunes 結果、一時オブジェクトを作成
      const tempSong: Song = {
        id: '', // 空 ID は新規楽曲であることを示す
        name: itunes.track_name,
        original_artist: itunes.artist_name,
        artists: [],
        arts: itunes.artwork_url,
        performance_count: 0,
        created_at: '',
        updated_at: '',
        itunes_ids: [{
          itunes_id: itunes.itunes_id,
          collection_name: itunes.collection_name,
          country: itunes.country,
          is_primary: true,
        }],
      };
      onSelectSong(tempSong);
      if (showToast) {
        showToast(`iTunesから「${tempSong.name}」を選択しました`, 'success');
      }
    }
    setIsOpen(false);
    setSearchQuery('');
    setDbSuggestions([]);
    setItunesSuggestions([]);
  };

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={handleInputChange}
        onFocus={() => {
          if (value.trim().length > 0) {
            setSearchQuery(value);
            setIsOpen(true);
            return;
          }
          if (dbSuggestions.length > 0 || itunesSuggestions.length > 0) setIsOpen(true);
        }}
        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        placeholder={placeholder}
      />

      {/* 検索候補ドロップダウン */}
      {isOpen && (dbSuggestions.length > 0 || itunesSuggestions.length > 0 || isLoading) && (
        <div
          ref={dropdownRef}
          className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-lg shadow-lg max-h-96 overflow-auto"
        >
          {isLoading ? (
            <div className="px-4 py-3 text-sm text-gray-500">検索中...</div>
          ) : (
            <>
              {/* DB 結果 */}
              {dbSuggestions.length > 0 && (
                <div>
                  <div className="px-4 py-2 bg-gray-100 text-xs font-semibold text-gray-600 sticky top-0">
                    データベースから
                  </div>
                  {dbSuggestions.map((song) => (
                    <button
                      key={song.id}
                      onClick={() => handleSelectSong(song)}
                      className="w-full px-4 py-3 text-left hover:bg-indigo-50 border-b border-gray-100 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        {song.arts ? (
                          <img
                            src={song.arts}
                            alt={song.name}
                            className="w-10 h-10 object-cover rounded"
                          />
                        ) : (
                          <div className="w-10 h-10 bg-gray-200 rounded flex items-center justify-center">
                            <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                            </svg>
                          </div>
                        )}
                        <div className="flex-1 min-w-0">
                          <div className="font-medium text-gray-900 truncate">{song.name}</div>
                          <div className="text-sm text-gray-500 truncate">{song.original_artist}</div>
                          <div className="text-xs text-gray-400 mt-0.5">
                            {song.performance_count}回の演奏記録
                          </div>
                        </div>
                      </div>
                    </button>
                  ))}
                </div>
              )}

              {/* iTunes の結果（DB 登録済み）*/}
              {itunesSuggestions.filter(i => i.existing_song).length > 0 && (
                <div>
                  <div className="px-4 py-2 bg-green-50 text-xs font-semibold text-green-700 sticky top-0 flex items-center gap-1">
                    <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                      <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                    </svg>
                    データベース内（iTunes経由）
                  </div>
                  {itunesSuggestions.filter(i => i.existing_song).map((itunes) => {
                    const song = itunes.existing_song!;
                    return (
                      <button
                        key={itunes.itunes_id}
                        onClick={() => handleSelectItunes(itunes)}
                        className="w-full px-4 py-3 text-left hover:bg-green-50 border-b border-gray-100 transition-colors"
                      >
                        <div className="flex items-center gap-3">
                          {song.arts ? (
                            <img
                              src={song.arts}
                              alt={song.name}
                              className="w-10 h-10 object-cover rounded"
                            />
                          ) : (
                            <div className="w-10 h-10 bg-green-100 rounded flex items-center justify-center">
                              <svg className="w-5 h-5 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                              </svg>
                            </div>
                          )}
                          <div className="flex-1 min-w-0">
                            <div className="font-medium text-gray-900 truncate">{song.name}</div>
                            <div className="text-sm text-gray-500 truncate">{song.original_artist}</div>
                            <div className="text-xs text-gray-400 truncate">
                              歌唱回数 {song.performance_count} 回 · iTunes ID: {itunes.itunes_id}
                            </div>
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>
              )}

              {/* iTunes の結果（新規楽曲）*/}
              {itunesSuggestions.filter(i => !i.existing_song).length > 0 && (
                <div>
                  <div className="px-4 py-2 bg-blue-50 text-xs font-semibold text-blue-700 sticky top-0 flex items-center gap-1">
                    <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                      <path d="M18 3a1 1 0 00-1.196-.98l-10 2A1 1 0 006 5v9.114A4.369 4.369 0 005 14c-1.657 0-3 .895-3 2s1.343 2 3 2 3-.895 3-2V7.82l8-1.6v5.894A4.37 4.37 0 0015 12c-1.657 0-3 .895-3 2s1.343 2 3 2 3-.895 3-2V3z" />
                    </svg>
                    iTunesから（新規）
                  </div>
                  {itunesSuggestions.filter(i => !i.existing_song).map((itunes) => (
                    <button
                      key={itunes.itunes_id}
                      onClick={() => handleSelectItunes(itunes)}
                      className="w-full px-4 py-3 text-left hover:bg-blue-50 border-b border-gray-100 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        {itunes.artwork_url ? (
                          <img
                            src={itunes.artwork_url}
                            alt={itunes.track_name}
                            className="w-10 h-10 object-cover rounded"
                          />
                        ) : (
                          <div className="w-10 h-10 bg-blue-100 rounded flex items-center justify-center">
                            <svg className="w-5 h-5 text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                            </svg>
                          </div>
                        )}
                        <div className="flex-1 min-w-0">
                          <div className="font-medium text-gray-900 truncate">{itunes.track_name}</div>
                          <div className="text-sm text-gray-500 truncate">{itunes.artist_name}</div>
                          {itunes.collection_name && (
                            <div className="text-xs text-gray-400 truncate">{itunes.collection_name}</div>
                          )}
                        </div>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
    )}
    </div>
  );
}
