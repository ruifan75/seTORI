import { useEffect, useRef, useState } from 'react';
import { artistApi } from '../api/client';
import type { Artist } from '../api/types';

// 原曲アーティストの入力（登録済みの表記から選べるオートコンプリート）。
//
// **`artists` の 1 行 ＝ `songs.original_artist` の 1 つの表記**。
// `SyncSongArtist` は連名も分割せず表記そのままで 1 行にするので、ここで選ぶことは
// 「他の曲と同じ表記を使う」ことと同義になる。自由入力のままだと表記ゆれが増え、
// 実際に 平井堅 / 平井 堅、スガシカオ / スガ シカオ、藤井風 / 藤井 風 のような
// 空白違いの重複が既に出来ている。
//
// **選択を強制はしない。** 新しいアーティストの登録は「そのまま打つ」ことで行う。
// 候補は寄せるための助けであって、関門ではない。
//
// 並びは曲数の多い順。同じ人の表記が割れているとき、定着している方が上に来る。
interface Props {
  value: string;
  onChange: (value: string) => void;
  // 選択時。読みは DB に入っているものを引き継ぐ（空なら渡さない）
  onSelectArtist: (artist: Artist) => void;
  placeholder?: string;
}

export default function ArtistSearchInput({ value, onChange, onSelectArtist, placeholder }: Props) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [suggestions, setSuggestions] = useState<Artist[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // 1 文字から引く。`Ado` `HY` のような短い名義があり、
  // 2 文字以上にすると検索できなくなる（楽曲検索で実際に起きた）
  useEffect(() => {
    if (query.length < 1) {
      setSuggestions([]);
      return;
    }
    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        const result = await artistApi.list(1, 8, query, 'songs', 'desc');
        setSuggestions(result.artists ?? []);
      } catch {
        setSuggestions([]);
      } finally {
        setIsLoading(false);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [query]);

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

  const select = (artist: Artist) => {
    onSelectArtist(artist);
    setIsOpen(false);
    setQuery('');
    setSuggestions([]);
  };

  // 打った通りの表記が候補に無いなら、それは新しいアーティストになる。
  // 黙って作らせず、そうなることをその場で言う
  const isNew =
    query.trim().length > 0 &&
    !isLoading &&
    !suggestions.some((a) => a.name === query.trim());

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setQuery(e.target.value);
          setIsOpen(true);
        }}
        onFocus={() => {
          if (value.trim().length > 0) {
            setQuery(value);
            setIsOpen(true);
            return;
          }
          if (suggestions.length > 0) setIsOpen(true);
        }}
        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        placeholder={placeholder}
      />

      {isOpen && (suggestions.length > 0 || isLoading || isNew) && (
        <div
          ref={dropdownRef}
          className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-lg shadow-lg max-h-72 overflow-auto"
        >
          {isLoading ? (
            <div className="px-4 py-3 text-sm text-gray-500">検索中...</div>
          ) : (
            <>
              {suggestions.map((artist) => (
                <button
                  key={artist.id}
                  onClick={() => select(artist)}
                  className="w-full px-3 py-2 text-left hover:bg-indigo-50 border-b border-gray-100 last:border-b-0 transition-colors"
                >
                  <div className="flex items-baseline gap-2">
                    <span className="font-medium text-gray-900 truncate">{artist.name}</span>
                    {artist.name_reading && (
                      <span className="text-xs text-gray-400 truncate">{artist.name_reading}</span>
                    )}
                    <span className="ml-auto shrink-0 text-xs text-gray-400">{artist.song_count}曲</span>
                  </div>
                </button>
              ))}
              {isNew && (
                <div className="px-3 py-2 text-xs text-green-700 bg-green-50">
                  「{query.trim()}」は未登録です。このまま保存すると新しいアーティストになります
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
