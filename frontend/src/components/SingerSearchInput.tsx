import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { singerApi } from '../api/client';
import type { Singer } from '../api/types';
import { useToast } from './ui/ToastContext';

// 歌手（ボーカル）の検索入力。
//
// **編集画面と審査画面で同じものを使う。** 審査側は参加者を全部 checkbox で
// 並べていたが、22 人のコラボ配信では壁になって選びにくかった。

// 歌手検索入力コンポーネントの Props
interface SingerSearchInputProps {
  onSelectSinger: (singer: Singer) => void;
  excludeIds?: string[];
  placeholder?: string;
  /** Channel ID / @handle / YouTube URL から新しい singer を登録できるようにする */
  allowCreate?: boolean;
}

function isChannelInput(value: string): boolean {
  const input = value.trim();
  return /^UC[\w-]{20,}$/.test(input)
    || /^@[^\s/?#]{3,}$/.test(input)
    || /^https?:\/\/(?:www\.|m\.)?(?:youtube\.com|youtu\.be)\//i.test(input);
}

// 歌手検索入力コンポーネント（オートコンプリート付き）
export default function SingerSearchInput({
  onSelectSinger,
  excludeIds = [],
  placeholder,
  allowCreate = false,
}: SingerSearchInputProps) {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [suggestions, setSuggestions] = useState<Singer[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // 検索をデバウンスする
  useEffect(() => {
    if (searchQuery.length < 1) {
      setSuggestions([]);
      return;
    }

    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        const results = await singerApi.search(searchQuery, 10);
        // 選択済みを除外
        setSuggestions(results.filter((s) => !excludeIds.includes(s.id)));
      } catch {
        setSuggestions([]);
      } finally {
        setIsLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery, excludeIds]);

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
    setSearchQuery(e.target.value);
    setCreateError('');
    setIsOpen(true);
  };

  const handleSelectSinger = (singer: Singer) => {
    onSelectSinger(singer);
    setIsOpen(false);
    setSearchQuery('');
    setSuggestions([]);
    setCreateError('');
  };

  const handleCreateSinger = async () => {
    const channelInput = searchQuery.trim();
    if (!allowCreate || !isChannelInput(channelInput) || isCreating) return;
    setIsCreating(true);
    setCreateError('');
    try {
      const created = await singerApi.create(channelInput);
      const singer = await singerApi.get(created.id);
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      queryClient.invalidateQueries({ queryKey: ['singer', created.id] });
      showToast(`「${singer.name}」を追加しました`, 'success');
      handleSelectSinger(singer);
    } catch (error) {
      setCreateError(`追加できませんでした: ${(error as Error).message}`);
    } finally {
      setIsCreating(false);
    }
  };

  const canCreate = allowCreate && isChannelInput(searchQuery);

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={searchQuery}
        onChange={handleInputChange}
        onFocus={() => {
          if (searchQuery.trim()) setIsOpen(true);
        }}
        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
        placeholder={placeholder || 'チャンネル名を入力して検索'}
      />

      {/* 検索候補ドロップダウン */}
      {isOpen && searchQuery.trim().length > 0 && (
        <div
          ref={dropdownRef}
          className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-lg shadow-lg max-h-60 overflow-auto"
        >
          {isLoading ? (
            <div className="px-4 py-3 text-sm text-gray-500">検索中...</div>
          ) : (
            <>
              {suggestions.map((singer) => (
                <button
                  key={singer.id}
                  type="button"
                  onClick={() => handleSelectSinger(singer)}
                  className="w-full px-4 py-3 text-left hover:bg-indigo-50 border-b border-gray-100 last:border-b-0 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    {singer.photo_url ? (
                      <img
                        src={singer.photo_url}
                        alt={singer.name}
                        className="w-8 h-8 rounded-full object-cover"
                        onError={(e) => {
                          e.currentTarget.onerror = null;
                          e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                        }}
                      />
                    ) : (
                      <div className="w-8 h-8 bg-gray-200 rounded-full flex items-center justify-center">
                        <svg className="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                        </svg>
                      </div>
                    )}
                    <div className="flex-1 min-w-0">
                      <div className="font-medium text-gray-900 truncate">{singer.name}</div>
                      {singer.english_name && (
                        <div className="text-sm text-gray-500 truncate">{singer.english_name}</div>
                      )}
                    </div>
                    {singer.organization && (
                      <span className="text-xs text-gray-400">{singer.organization}</span>
                    )}
                  </div>
                </button>
              ))}

              {suggestions.length === 0 && !canCreate && (
                <div className="px-4 py-3 text-sm text-gray-500">
                  見つかりません
                  {allowCreate && (
                    <span className="mt-1 block text-xs text-gray-400">
                      新規追加する場合は Channel ID、@handle、または YouTube URL を入力してください
                    </span>
                  )}
                </div>
              )}

              {canCreate && (
                <button
                  type="button"
                  onClick={handleCreateSinger}
                  disabled={isCreating}
                  className="w-full px-4 py-3 text-left text-sm font-medium text-indigo-600 hover:bg-indigo-50 disabled:text-gray-400 transition-colors"
                >
                  {isCreating ? 'チャンネルを追加中...' : `「${searchQuery.trim()}」を新しいチャンネルとして追加`}
                </button>
              )}

              {createError && <p className="px-4 py-2 text-xs text-red-600">{createError}</p>}
            </>
          )}
        </div>
      )}
    </div>
  );
}
