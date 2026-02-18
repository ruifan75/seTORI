import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { streamApi, performanceApi, aiApi, songApi, singerApi, itunesApi, holodexApi, commentApi, tagApi } from '../api/client';
import type { Singer, CreatePerformanceItem, AINormalizationItem, Song, UpdateStreamRequest, ITunesSearchResult, CommentSong, SongSuggestion } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/Toast';
import YoutubePlayer, { youtubePlayerSeekTo, youtubePlayerGetCurrentTime } from '../components/YoutubePlayer';


// 可編輯的直播資訊
interface EditableStreamInfo {
  title: string;
  streamDate: string;
  tagIds: string[];
  participantIds: string[];
  isProcessed: boolean;
  isHidden: boolean;
}


interface EditableSong {
  id: string;
  name: string;
  nameReading: string;
  artist: string;
  artistReading: string;
  start: number;
  end: number;
  tags: string[];
  singerIds: string[];
  // 新增欄位
  matchedSongId: string | null; // 配對到的歌曲 ID，null 表示要新增
  artUrl: string | null; // 封面圖 URL
  itunesId: number | null; // Holodex 提供的 iTunes ID
  trackDuration: number | null; // iTunes 歌曲長度（秒）
  originalName: string; // 追蹤原始名稱（用於判斷是否有修改）
  originalArtist: string; // 追蹤原始藝人
  // AI 正規化追蹤
  aiNormalizedName?: string; // AI 修改前的名稱（如果有被 AI 修改）
  aiNormalizedArtist?: string; // AI 修改前的藝人（如果有被 AI 修改）
  // 時間估計標記
  isEndTimeEstimated?: boolean; // 結束時間是否為估計値
  // 合併追蹤
  mergedFrom?: string[]; // AI 正規化後被合併的原始曲名
  // 自由文字 tag
  customTags: string[];
}

// AI 正規化後合併重複歌曲
// 條件：name 完全一致 + artist 完全一致 + start 時間差 ≤ 30s
function mergeDuplicateSongs(songs: EditableSong[]): EditableSong[] {
  const result: EditableSong[] = [];

  for (const song of songs) {
    let merged = false;
    for (let i = 0; i < result.length; i++) {
      const existing = result[i];
      if (
        existing.name === song.name &&
        existing.artist === song.artist &&
        Math.abs(existing.start - song.start) <= 30
      ) {
        // Merge: 保留資訊較完整的一方
        const hasRealEnd = (s: EditableSong) => s.end > 0 && !s.isEndTimeEstimated;
        const preferSong = hasRealEnd(song) && !hasRealEnd(existing);

        // 記錄被吸收方的原始名稱（AI 修改前的名稱）
        const absorbedName = preferSong
          ? (existing.aiNormalizedName || existing.originalName)
          : (song.aiNormalizedName || song.originalName);
        const prevMerged = existing.mergedFrom || [];

        if (preferSong) {
          result[i] = {
            ...song,
            tags: [...new Set([...song.tags, ...existing.tags])],
            matchedSongId: song.matchedSongId || existing.matchedSongId,
            artUrl: song.artUrl || existing.artUrl,
            itunesId: song.itunesId ?? existing.itunesId,
            trackDuration: song.trackDuration ?? existing.trackDuration,
            nameReading: song.nameReading || existing.nameReading,
            artistReading: song.artistReading || existing.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        } else {
          result[i] = {
            ...existing,
            tags: [...new Set([...existing.tags, ...song.tags])],
            matchedSongId: existing.matchedSongId || song.matchedSongId,
            artUrl: existing.artUrl || song.artUrl,
            itunesId: existing.itunesId ?? song.itunesId,
            trackDuration: existing.trackDuration ?? song.trackDuration,
            nameReading: existing.nameReading || song.nameReading,
            artistReading: existing.artistReading || song.artistReading,
            mergedFrom: [...prevMerged, absorbedName],
          };
        }
        merged = true;
        break;
      }
    }
    if (!merged) {
      result.push(song);
    }
  }

  return result;
}

// 歌曲搜尋輸入元件的 Props
interface SongSearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSelectSong: (song: Song) => void;
  placeholder?: string;
  showToast?: (message: string, type?: 'success' | 'error' | 'info') => void;
}

// 歌曲搜尋輸入元件（帶自動完成）
function SongSearchInput({ value, onChange, onSelectSong, placeholder, showToast }: SongSearchInputProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [dbSuggestions, setDbSuggestions] = useState<Song[]>([]);
  const [itunesSuggestions, setItunesSuggestions] = useState<ITunesSearchResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Debounce 搜尋（同時搜尋 DB 和 iTunes）
  useEffect(() => {
    if (searchQuery.length < 1) {
      setDbSuggestions([]);
      setItunesSuggestions([]);
      return;
    }

    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        // 並行搜尋 DB 和 iTunes
        const [dbResult, itunesResult] = await Promise.all([
          songApi.list(1, 5, searchQuery).catch(() => ({ songs: [], pagination: { page: 1, limit: 5, total: 0, total_pages: 0 } })),
          itunesApi.search(searchQuery).catch(() => ({ results: [] }))
        ]);
        setDbSuggestions(dbResult.songs);
        setItunesSuggestions(itunesResult.results.slice(0, 5)); // 限制 iTunes 結果數量
      } catch {
        setDbSuggestions([]);
        setItunesSuggestions([]);
      } finally {
        setIsLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery]);

  // 點擊外部關閉下拉選單
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
    // 如果這個 iTunes 結果已經在資料庫中，直接綁定到該 song
    if (itunes.existing_song) {
      const existingSong: Song = {
        id: itunes.existing_song.id,
        name: itunes.existing_song.name,
        name_reading: itunes.existing_song.name_reading,
        original_artist: itunes.existing_song.original_artist,
        original_artist_reading: itunes.existing_song.original_artist_reading,
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
      // 純 iTunes 結果，創建臨時對象
      const tempSong: Song = {
        id: '', // 空 ID 表示這是新歌曲
        name: itunes.track_name,
        original_artist: itunes.artist_name,
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

      {/* 搜尋建議下拉選單 */}
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

              {/* iTunes 結果（已在資料庫中）*/}
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
                              {song.performance_count}回演唱 · iTunes ID: {itunes.itunes_id}
                            </div>
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>
              )}

              {/* iTunes 結果（新歌曲）*/}
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

// 演唱者搜尋輸入元件的 Props
interface SingerSearchInputProps {
  onSelectSinger: (singer: Singer) => void;
  excludeIds?: string[];
  placeholder?: string;
}

// 演唱者搜尋輸入元件（帶自動完成）
function SingerSearchInput({ onSelectSinger, excludeIds = [], placeholder }: SingerSearchInputProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [suggestions, setSuggestions] = useState<Singer[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Debounce 搜尋
  useEffect(() => {
    if (searchQuery.length < 1) {
      setSuggestions([]);
      return;
    }

    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        const results = await singerApi.search(searchQuery, 10);
        // 過濾掉已選擇的
        setSuggestions(results.filter((s) => !excludeIds.includes(s.id)));
      } catch {
        setSuggestions([]);
      } finally {
        setIsLoading(false);
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [searchQuery, excludeIds]);

  // 點擊外部關閉下拉選單
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
    setIsOpen(true);
  };

  const handleSelectSinger = (singer: Singer) => {
    onSelectSinger(singer);
    setIsOpen(false);
    setSearchQuery('');
    setSuggestions([]);
  };

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={searchQuery}
        onChange={handleInputChange}
        onFocus={() => {
          if (suggestions.length > 0) setIsOpen(true);
        }}
        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent text-sm"
        placeholder={placeholder || "チャンネル名を入力して検索"}
      />

      {/* 搜尋建議下拉選單 */}
      {isOpen && (suggestions.length > 0 || isLoading) && (
        <div
          ref={dropdownRef}
          className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-lg shadow-lg max-h-60 overflow-auto"
        >
          {isLoading ? (
            <div className="px-4 py-3 text-sm text-gray-500">検索中...</div>
          ) : (
            suggestions.map((singer) => (
              <button
                key={singer.id}
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
            ))
          )}
        </div>
      )}
    </div>
  );
}

function formatTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;

  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

function parseTime(timeStr: string): number {
  if (!timeStr || timeStr.trim() === '') {
    return 0;
  }
  
  const parts = timeStr.split(':').map(s => {
    const num = parseInt(s, 10);
    return isNaN(num) ? 0 : num;
  });
  
  if (parts.length === 3) {
    return parts[0] * 3600 + parts[1] * 60 + parts[2];
  } else if (parts.length === 2) {
    return parts[0] * 60 + parts[1];
  } else if (parts.length === 1) {
    // 如果只有一個數字，視為秒數
    return parts[0];
  }
  return 0;
}

function formatTimeInput(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

// 格式化時長，顯示為 "+MM:SS" 或 "+H:MM:SS"
function formatDuration(seconds: number | null): string {
  if (seconds === null || seconds === 0) return '+??:??';
  
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  
  if (h > 0) {
    return `+${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `+${m}:${s.toString().padStart(2, '0')}`;
}

function formatStreamDate(dateStr: string): string {
  // 將 ISO 格式的日期轉換為本地時區的格式
  const date = new Date(dateStr);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');
  
  return `${year}/${month}/${day} ${hours}:${minutes}:${seconds}`;
}

export default function StreamDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const [isEditing, setIsEditing] = useState(false);
  const [editableSongs, setEditableSongs] = useState<EditableSong[]>([]);
  const [holodexTimelineSongs, setHolodexTimelineSongs] = useState<SongSuggestion[]>([]);
  const [commentTimelineSongs, setCommentTimelineSongs] = useState<CommentSong[]>([]);
  const [channelOwner, setChannelOwner] = useState<Singer | null>(null);
  const [participants, setParticipants] = useState<Singer[]>([]);
  const [editableStreamInfo, setEditableStreamInfo] = useState<EditableStreamInfo | null>(null);
  const [currentPlayerTime, setCurrentPlayerTime] = useState<number | null>(null);
  const [highlightedSongId, setHighlightedSongId] = useState<string | null>(null);

  const fetchTrackDurationByItunesId = async (itunesId: number): Promise<number | null> => {
    try {
      const result = await itunesApi.queryById(itunesId);
      if (result && result.track_time_millis) {
        return Math.round(result.track_time_millis / 1000);
      }
    } catch (error) {
      console.error('Failed to fetch iTunes duration:', error);
    }
    return null;
  };
  const [vocalistPopupSingers, setVocalistPopupSingers] = useState<Singer[] | null>(null);

  const { data: stream, isLoading } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => streamApi.get(id!),
    enabled: !!id,
    staleTime: 0, // 確保每次進入頁面都重新載入
  });

  const { data: streamTagsData = [] } = useQuery({
    queryKey: ['stream-tags'],
    queryFn: tagApi.listStreamTags,
  });
  const STREAM_TAGS = streamTagsData.map((t) => ({ id: t.id, label: t.display_name, color: t.color }));

  const { data: perfTagsData = [] } = useQuery({
    queryKey: ['performance-tags'],
    queryFn: tagApi.listPerformanceTags,
  });
  const PERFORMANCE_TAGS = perfTagsData.map((t) => ({ id: t.id, label: t.display_name, color: t.color }));

  // 當 stream 資料載入後，設置頻道擁有者和 timeline 資料
  useEffect(() => {
    setChannelOwner(stream?.channel_owner || null);
    // 載入儲存的 timeline 資料（沒有時也清空）
    setHolodexTimelineSongs(stream?.holodex_timeline_songs || []);
    setCommentTimelineSongs(stream?.comment_timeline_songs || []);
  }, [stream]);

  // 編輯模式時避免整頁滾動（改用區塊內滾動）
  useEffect(() => {
    if (!isEditing) return;
    const prevBodyOverflow = document.body.style.overflow;
    const prevHtmlOverflow = document.documentElement.style.overflow;
    document.body.style.overflow = 'hidden';
    document.documentElement.style.overflow = 'hidden';

    return () => {
      document.body.style.overflow = prevBodyOverflow;
      document.documentElement.style.overflow = prevHtmlOverflow;
    };
  }, [isEditing]);

  // 定期更新播放器當前時間（每秒）
  useEffect(() => {
    const interval = setInterval(() => {
      const time = youtubePlayerGetCurrentTime();
      setCurrentPlayerTime(time);
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  // 更新 Stream 資訊
  const updateStreamMutation = useMutation({
    mutationFn: (req: UpdateStreamRequest) => streamApi.update(id!, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
    },
    onError: (err: Error) => {
      showToast(`更新エラー: ${err.message}`, 'error');
    },
  });

  // 確認並直接建立演出記錄
  const createPerformancesMutation = useMutation({
    mutationFn: (performances: CreatePerformanceItem[]) =>
      performanceApi.create(id!, { performances }),
    onSuccess: (data) => {
      showToast(`${data.created_count}曲のセットリストを登録しました`, 'success');
      setIsEditing(false);
      setEditableSongs([]);
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
    },
    onError: (err: Error) => {
      showToast(`登録エラー: ${err.message}`, 'error');
    },
  });

  // AI 正規化
  const aiNormalizeMutation = useMutation({
    mutationFn: (items: AINormalizationItem[]) =>
      aiApi.normalize({ items }),
    onSuccess: async (data) => {
      // AI 結果を反映
      const updated: EditableSong[] = [...editableSongs];

      // 先同步處理所有建議（不再需要逐首 API 呼叫）
      for (const suggestion of data.suggestions) {
        if (suggestion.index >= updated.length) continue;
        const current = updated[suggestion.index];

        // DB に既存歌曲がある場合はその情報を使用、なければ AI 結果を使用
        const hasMatch = !!suggestion.matched_song_id;
        const finalName = hasMatch && suggestion.matched_song_name
          ? suggestion.matched_song_name : suggestion.normalized_name;
        const finalNameReading = hasMatch && suggestion.matched_song_name_reading != null
          ? suggestion.matched_song_name_reading : suggestion.normalized_name_reading;
        const finalArtist = hasMatch && suggestion.matched_song_artist
          ? suggestion.matched_song_artist : suggestion.original_artist;
        const finalArtistReading = hasMatch && suggestion.matched_song_artist_reading != null
          ? suggestion.matched_song_artist_reading : suggestion.original_artist_reading;
        const artUrl = (hasMatch ? suggestion.matched_song_art_url : null) || current.artUrl;
        const itunesId = suggestion.matched_song_itunes_id || current.itunesId || null;

        // iTunes ID が取得できた場合、トラック長を取得
        let trackDuration = current.trackDuration;
        if (itunesId && itunesId !== current.itunesId) {
          trackDuration = await fetchTrackDurationByItunesId(itunesId);
        }

        const nameChanged = current.name !== finalName;
        const artistChanged = current.artist !== finalArtist;

        updated[suggestion.index] = {
          ...current,
          name: finalName,
          nameReading: finalNameReading,
          artist: finalArtist,
          artistReading: finalArtistReading,
          tags: suggestion.tags,
          matchedSongId: suggestion.matched_song_id || null,
          artUrl,
          itunesId,
          trackDuration,
          // 保留正規化前の値
          aiNormalizedName: nameChanged ? current.name : undefined,
          aiNormalizedArtist: artistChanged ? current.artist : undefined,
          // 更新原始值以追蹤後續變更
          originalName: finalName,
          originalArtist: finalArtist,
        };
      }
      
      // 合併正規化後名稱相同的重複歌曲
      const merged = mergeDuplicateSongs(updated);
      const mergedCount = updated.length - merged.length;
      setEditableSongs(merged);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複をマージ）` : '';
      if (data.warning) {
        showToast(data.warning + mergeMsg, 'error');
      } else {
        showToast(`${data.suggestions.length}曲のAI正規化が完了しました${mergeMsg}`, 'success');
      }
    },
    onError: (err: Error) => {
      showToast(`AI正規化エラー: ${err.message}`, 'error');
    },
  });

  // 同步單一影片
  const syncVideoMutation = useMutation({
    mutationFn: () => holodexApi.syncVideo(id!),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      showToast(
        `同期完了: ${data.synced_count > 0 ? `${data.synced_count}件更新` : '変更なし'}`,
        'success'
      );
    },
    onError: (err: Error) => {
      showToast(`同期エラー: ${err.message}`, 'error');
    },
  });

  // 同步 seTORI 資料到 Holodex
  const syncToHolodexMutation = useMutation({
    mutationFn: () => holodexApi.syncSetoriToHolodex(id!),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      showToast(
        data.message || `Holodex に同期完了: ${data.synced_count > 0 ? `${data.synced_count}件` : '完了'}`,
        'success'
      );
    },
    onError: (err: Error) => {
      showToast(`Holodex 同期エラー: ${err.message}`, 'error');
    },
  });

  const loadFromHolodex = () => {
    if (!stream?.holodex_timeline_songs || stream.holodex_timeline_songs.length === 0) {
      showToast('Holodexデータがありません', 'info');
      return;
    }

    // 按開始時間排序
    const sortedSongs = [...stream.holodex_timeline_songs].sort((a, b) => a.start_seconds - b.start_seconds);
    
    // 從已載入的 stream 資料中讀取
    setHolodexTimelineSongs(sortedSongs);

    // 獲取預設的歌手ID（從participants或channelOwner）
    const defaultSingerIds = stream.participants?.map((p) => p.id) || (channelOwner ? [channelOwner.id] : []);

    // 轉換為可編輯歌曲
    const songs: EditableSong[] = sortedSongs.map((song, index) => ({
      id: `holodex-${index}`,
      name: song.name,
      nameReading: '',
      artist: song.original_artist,
      artistReading: '',
      start: song.start_seconds,
      end: song.end_seconds,
      tags: song.tags,
      singerIds: song.singer_ids.length > 0 ? song.singer_ids : defaultSingerIds,
      matchedSongId: null,
      artUrl: song.art_url || null,
      itunesId: song.itunes_id || null,
      trackDuration: null,
      originalName: song.name,
      originalArtist: song.original_artist,
      aiNormalizedName: undefined,
      aiNormalizedArtist: undefined,
      isEndTimeEstimated: false,
      customTags: [],
    }));
    setEditableSongs(songs);
    showToast(`Holodexから${songs.length}曲を読み込みました`, 'success');
  };

  const [commentAnalyzeLoading, setCommentAnalyzeLoading] = useState(false);

  const loadFromComments = async () => {
    if (!id) return;
    setCommentAnalyzeLoading(true);
    try {
      const result = await commentApi.analyze(id);
      const sortedSongs = [...result.songs].sort((a, b) => a.start - b.start);
      // timeline は stream.comment_timeline_songs のまま（未フィルタ）を維持

      const defaultSingerIds = stream?.participants?.map((p) => p.id) || (channelOwner ? [channelOwner.id] : []);
      const songs: EditableSong[] = sortedSongs.map((song, index) => ({
        id: `comment-${index}`,
        name: song.name,
        nameReading: '',
        artist: song.original_artist,
        artistReading: '',
        start: song.start,
        end: song.end,
        tags: [],
        singerIds: defaultSingerIds,
        matchedSongId: null,
        artUrl: null,
        itunesId: null,
        trackDuration: null,
        originalName: song.name,
        originalArtist: song.original_artist,
        aiNormalizedName: undefined,
        aiNormalizedArtist: undefined,
        isEndTimeEstimated: song.is_end_time_estimated,
        customTags: [],
      }));
      setEditableSongs(songs);
      showToast(`コメントから${songs.length}曲を読み込みました`, 'success');
    } catch (error) {
      showToast('コメント分析に失敗しました', 'error');
      console.error('Comment analysis failed:', error);
    } finally {
      setCommentAnalyzeLoading(false);
    }
  };

  const toggleEditing = () => {
    if (isEditing) {
      // 關閉編輯模式
      setIsEditing(false);
      setEditableSongs([]);
      setEditableStreamInfo(null);
    } else {
      // 開啟編輯模式，自動載入現有セトリ和直播資訊
      if (stream) {
        // 初始化直播資訊
        setEditableStreamInfo({
          title: stream.title,
          streamDate: stream.stream_date,
          tagIds: stream.tags.map((t) => t.id),
          participantIds: stream.participants?.map((p) => p.id) || [],
          isProcessed: stream.is_processed,
          isHidden: stream.is_hidden,
        });

        // 設定參與者列表（包括表演中的所有歌唱者）
        const allSingers = new Map<string, Singer>();
        
        // 先添加stream.participants
        (stream.participants || []).forEach(p => allSingers.set(p.id, p));
        
        // 再添加所有performances中的歌唱者
        if (stream.performances.length > 0) {
          stream.performances.forEach(perf => {
            perf.singers.forEach(singer => {
              allSingers.set(singer.id, singer);
            });
          });
        }
        
        setParticipants(Array.from(allSingers.values()));

        // 載入現有セトリ
        if (stream.performances.length > 0) {
          const songs: EditableSong[] = stream.performances.map((perf) => ({
            id: perf.id,
            name: perf.song_name,
            nameReading: '',
            artist: perf.original_artist,
            artistReading: '',
            start: perf.start_seconds,
            end: perf.end_seconds,
            tags: perf.tags.map((t) => t.id),
            singerIds: perf.singers.map((s) => s.id),
            matchedSongId: perf.song_id,
            artUrl: perf.arts || null,
            itunesId: null, // 現有 performance 不會有 iTunes ID
            trackDuration: null,
            originalName: perf.song_name,
            originalArtist: perf.original_artist,
            // 現有資料沒有 AI 修改或估計時間的標記
            aiNormalizedName: undefined,
            aiNormalizedArtist: undefined,
            isEndTimeEstimated: false,
            customTags: perf.custom_tags || [],
          }));
          setEditableSongs(songs);
        }
      }
      setIsEditing(true);
    }
  };

  const runAINormalization = () => {
    if (editableSongs.length === 0) return;
    const items: AINormalizationItem[] = editableSongs.map((song) => ({
      name: song.name,
      original_artist: song.artist,
      art_url: song.artUrl || undefined,
      itunes_id: song.itunesId || undefined,
    }));
    aiNormalizeMutation.mutate(items);
  };

  const handleSongChange = (index: number, field: keyof EditableSong, value: string | number | string[] | boolean | null) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const song = updated[index];

      // 如果修改了名稱或藝人，且原本有配對的歌曲，則重設為新歌曲
      if ((field === 'name' || field === 'artist') && song.matchedSongId) {
        const newName = field === 'name' ? value as string : song.name;
        const newArtist = field === 'artist' ? value as string : song.artist;

        if (newName !== song.originalName || newArtist !== song.originalArtist) {
          updated[index] = {
            ...song,
            [field]: value,
            matchedSongId: null
          };
          return updated;
        }
      }

      updated[index] = { ...song, [field]: value };
      return updated;
    });
  };

  // 選擇搜尋到的歌曲
  const handleSelectExistingSong = async (index: number, song: Song) => {
    const itunesId = song.itunes_ids && song.itunes_ids.length > 0 ? song.itunes_ids[0].itunes_id : null;
    const trackDuration = itunesId ? await fetchTrackDurationByItunesId(Number(itunesId)) : null;

    setEditableSongs((prev) => {
      const updated = [...prev];
      
      // 檢查是否是從 iTunes 選擇的（id 為空）
      if (!song.id) {
        // 從 iTunes 選擇：填入基本資訊和 iTunes ID
        updated[index] = {
          ...updated[index],
          name: song.name,
          artist: song.original_artist,
          artUrl: song.arts || null,
          itunesId: itunesId ? Number(itunesId) : null,
          trackDuration: trackDuration,
          matchedSongId: null, // 這是新歌曲
          originalName: song.name,
          originalArtist: song.original_artist,
        };
      } else {
        // 從 DB 選擇：填入完整資訊
        updated[index] = {
          ...updated[index],
          name: song.name,
          nameReading: song.name_reading || '',
          artist: song.original_artist,
          artistReading: song.original_artist_reading || '',
          artUrl: song.arts || null,
          itunesId: itunesId ? Number(itunesId) : null,
          trackDuration: trackDuration,
          matchedSongId: song.id,
          originalName: song.name,
          originalArtist: song.original_artist,
        };
      }
      
      return updated;
    });
  };

  const handleTimeChange = (index: number, field: 'start' | 'end', timeStr: string) => {
    // 允許用戶自由輸入，只在失去焦點時解析
    // 直接更新顯示值，延遲解析
    const seconds = parseTime(timeStr);
    if (!isNaN(seconds)) {
      handleSongChange(index, field, seconds);
    }
  };

  const toggleTag = (index: number, tagId: string) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const currentTags = updated[index].tags;
      if (currentTags.includes(tagId)) {
        updated[index] = { ...updated[index], tags: currentTags.filter((t) => t !== tagId) };
      } else {
        updated[index] = { ...updated[index], tags: [...currentTags, tagId] };
      }
      return updated;
    });
  };

  const removeSong = (index: number) => {
    setEditableSongs((prev) => prev.filter((_, i) => i !== index));
  };

  const addSong = () => {
    const lastSong = editableSongs[editableSongs.length - 1];
    const newStart = lastSong ? lastSong.end || lastSong.start + 240 : 0;
    const defaultSingerIds = channelOwner ? [channelOwner.id] : [];
    setEditableSongs((prev) => [
      ...prev,
      {
        id: `new-${Date.now()}`,
        name: '',
        nameReading: '',
        artist: '',
        artistReading: '',
        start: newStart,
        end: 0,
        tags: [],
        singerIds: defaultSingerIds,
        matchedSongId: null,
        artUrl: null,
        itunesId: null,
        trackDuration: null,
        originalName: '',
        originalArtist: '',
        customTags: [],
      },
    ]);
  };

  // Timeline から歌曲を追加（編集モード用）
  const addSongFromTimeline = (item: { start: number; end: number; label: string; artist: string }, source: string) => {
    const defaultSingerIds = channelOwner ? [channelOwner.id] : [];
    const newId = `${source}-add-${Date.now()}`;
    const newSong: EditableSong = {
      id: newId,
      name: item.label,
      nameReading: '',
      artist: item.artist,
      artistReading: '',
      start: item.start,
      end: item.end,
      tags: [],
      singerIds: defaultSingerIds,
      matchedSongId: null,
      artUrl: null,
      itunesId: null,
      trackDuration: null,
      originalName: item.label,
      originalArtist: item.artist,
      customTags: [],
    };
    setEditableSongs(prev => {
      const updated = [...prev, newSong].sort((a, b) => a.start - b.start);
      return updated;
    });
    showToast(`「${item.label}」を追加しました`, 'success');
    setTimeout(() => {
      setHighlightedSongId(newId);
      document.getElementById(`song-${newId}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      setTimeout(() => setHighlightedSongId(null), 3000);
    }, 100);
  };

  // seTORI timeline クリック → 対応する曲にスクロール（編集モード用）
  const scrollToEditableSong = (start: number) => {
    if (editableSongs.length === 0) return;
    const match = editableSongs.reduce((best, song) =>
      Math.abs(song.start - start) < Math.abs(best.start - start) ? song : best
    );
    if (Math.abs(match.start - start) <= 30) {
      setHighlightedSongId(match.id);
      document.getElementById(`song-${match.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      setTimeout(() => setHighlightedSongId(null), 3000);
    }
  };

  const handleConfirm = async () => {
    // 先更新 Stream 資訊（如果有變更）
    if (editableStreamInfo) {
      await updateStreamMutation.mutateAsync({
        title: editableStreamInfo.title,
        stream_date: editableStreamInfo.streamDate,
        tag_ids: editableStreamInfo.tagIds,
        participant_ids: editableStreamInfo.participantIds,
        is_processed: editableStreamInfo.isProcessed,
        is_hidden: editableStreamInfo.isHidden,
      });
    }

    // 如果沒有歌曲，刪除所有 performance
    if (editableSongs.length === 0) {
      try {
        await performanceApi.deleteAll(id!);
        showToast('セットリストを削除しました', 'success');
        setIsEditing(false);
        setEditableSongs([]);
        queryClient.invalidateQueries({ queryKey: ['stream', id] });
      } catch (err: any) {
        showToast(`削除エラー: ${err.message}`, 'error');
      }
      return;
    }

    // 終了時間のバリデーション
    const missingEndTime = editableSongs.filter(s => s.end === 0);
    if (missingEndTime.length > 0) {
      showToast(`終了時間が未設定の曲が${missingEndTime.length}件あります`, 'error');
      // 最初の未設定曲にスクロール
      const firstMissing = missingEndTime[0];
      document.getElementById(`song-${firstMissing.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      return;
    }

    // 更新 setlist
    const performances: CreatePerformanceItem[] = editableSongs.map((song) => ({
      name: song.name,
      name_reading: song.nameReading,
      original_artist: song.artist,
      original_artist_reading: song.artistReading,
      start_seconds: song.start,
      end_seconds: song.end,
      tags: song.tags,
      singer_ids: song.singerIds,
      art_url: song.artUrl || undefined,
      itunes_id: song.itunesId || undefined,
      custom_tags: song.customTags.length > 0 ? song.customTags : undefined,
    }));
    createPerformancesMutation.mutate(performances);
  };

  // 自動保存 Stream 資訊（用於標籤、參與者、狀態等）
  const autoSaveStreamInfo = async (info: Partial<EditableStreamInfo>) => {
    try {
      const updateData = {
        tag_ids: info.tagIds,
        participant_ids: info.participantIds,
        is_processed: info.isProcessed,
        is_hidden: info.isHidden,
      };
      await updateStreamMutation.mutateAsync(updateData as UpdateStreamRequest);
      showToast('更新しました', 'success');
    } catch (err) {
      showToast(`更新エラー: ${err instanceof Error ? err.message : '未知のエラー'}`, 'error');
    }
  };

  // 切換直播標籤並自動保存
  const toggleStreamTag = (tagId: string) => {
    if (!editableStreamInfo) return;
    const newTagIds = editableStreamInfo.tagIds.includes(tagId)
      ? editableStreamInfo.tagIds.filter((id) => id !== tagId)
      : [...editableStreamInfo.tagIds, tagId];
    const updatedInfo = { ...editableStreamInfo, tagIds: newTagIds };
    setEditableStreamInfo(updatedInfo);
    // 自動保存
    autoSaveStreamInfo({ tagIds: newTagIds });
  };

  // 自動保存參與者變更
  const updateParticipantsAndSave = (newParticipantIds: string[]) => {
    if (!editableStreamInfo) return;
    const updatedInfo = { ...editableStreamInfo, participantIds: newParticipantIds };
    setEditableStreamInfo(updatedInfo);
    // 自動保存
    autoSaveStreamInfo({ participantIds: newParticipantIds });
  };

  // 自動保存處理完成狀態
  const updateIsProcessedAndSave = (isProcessed: boolean) => {
    if (!editableStreamInfo) return;
    const updatedInfo = { ...editableStreamInfo, isProcessed };
    setEditableStreamInfo(updatedInfo);
    // 自動保存
    autoSaveStreamInfo({ isProcessed });
  };

  // 自動保存隱藏狀態
  const updateIsHiddenAndSave = (isHidden: boolean) => {
    if (!editableStreamInfo) return;
    const updatedInfo = { ...editableStreamInfo, isHidden };
    setEditableStreamInfo(updatedInfo);
    // 自動保存
    autoSaveStreamInfo({ isHidden });
  };

  // YouTube 播放器實例（必須在任何條件判斷之前）
  const playerInstanceRef = useRef<any>(null);

  if (isLoading) {
    return <Loading />;
  }

  if (!stream) {
    return (
      <div className="text-center py-12 text-gray-500">
        歌枠が見つかりませんでした
      </div>
    );
  }

  const youtubeUrl = `https://www.youtube.com/watch?v=${stream.id}`;

  const setoriTimeline = stream.performances.map((perf) => {
    const end = perf.end_seconds > 0 ? perf.end_seconds : perf.start_seconds;
    return {
      id: perf.id,
      start: perf.start_seconds,
      end,
      label: perf.song_name,
      artist: perf.original_artist,
    };
  });

  const holodexTimeline = holodexTimelineSongs.map((song, index) => {
    const end = song.end_seconds > 0 ? song.end_seconds : song.start_seconds;
    return {
      id: `holodex-${index}`,
      start: song.start_seconds,
      end,
      label: song.name,
      artist: song.original_artist || '',
    };
  });

  const commentTimeline = commentTimelineSongs.map((song, index) => {
    const end = song.end > 0 ? song.end : song.start;
    return {
      id: `comment-${index}`,
      start: song.start,
      end,
      label: song.name,
      artist: song.original_artist || '',
    };
  });

  const timelineDuration = Math.max(
    stream.duration_seconds || 0,
    ...setoriTimeline.map((s) => s.end),
    ...holodexTimeline.map((s) => s.end),
    ...commentTimeline.map((s) => s.end),
    1,
  );

  const getTimelineLeft = (start: number) => (start / timelineDuration) * 100;
  const getTimelineWidth = (start: number, end: number) =>
    Math.max(((end - start) / timelineDuration) * 100, 0.4);

  return (
    <>
      <div className="flex flex-col min-[1300px]:flex-row gap-6 w-full h-full min-h-0 overflow-hidden">
      {/* Left Column - Stream Info + YouTube Player */}
      <div className="w-full min-[1300px]:basis-2/5 min-[1300px]:shrink-0 min-[1300px]:self-stretch flex flex-row min-[1300px]:grid min-[1300px]:grid-rows-[2fr_3fr] gap-4 min-h-0 min-[1300px]:overflow-hidden shrink-0 max-h-[40vh] min-[1300px]:max-h-none">
        {/* Stream Header - 40% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border overflow-y-auto min-h-0">
          <div className="p-6">
            {isEditing && editableStreamInfo ? (
              <>
                {/* Title - Read Only */}
                <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
                {/* Date - Read Only */}
                <p className="text-gray-500 mt-2">{formatStreamDate(stream.stream_date)}</p>

                {/* Editable Tags */}
                <div className="mt-4">
                  <label className="block text-sm font-medium text-gray-700 mb-2">タグ</label>
                  <div className="flex flex-wrap gap-2">
                    {STREAM_TAGS.map((tag) => (
                      <button
                        key={tag.id}
                        onClick={() => toggleStreamTag(tag.id)}
                        className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                          editableStreamInfo.tagIds.includes(tag.id)
                            ? 'text-white'
                            : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                        }`}
                        style={editableStreamInfo.tagIds.includes(tag.id) ? { backgroundColor: tag.color } : {}}
                      >
                        {tag.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Editable Participants */}
                <div className="mt-4">
                  <label className="block text-sm font-medium text-gray-700 mb-2">参加者</label>
                  {/* Selected Participants */}
                  <div className="flex flex-wrap gap-2 mb-3">
                    {participants
                      .filter((singer) => editableStreamInfo.participantIds.includes(singer.id))
                      .map((singer) => (
                        <div
                          key={singer.id}
                          className="flex items-center gap-2 px-3 py-1 bg-indigo-100 text-indigo-800 rounded-full text-sm font-medium"
                        >
                          {singer.photo_url && (
                            <img
                              src={singer.photo_url}
                              alt={singer.name}
                              className="w-5 h-5 rounded-full"
                              onError={(e) => {
                                e.currentTarget.onerror = null;
                                e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                              }}
                            />
                          )}
                          {singer.name}
                          <button
                            onClick={() => {
                              const newIds = editableStreamInfo.participantIds.filter((id) => id !== singer.id);
                              updateParticipantsAndSave(newIds);
                            }}
                            className="ml-1 text-indigo-600 hover:text-indigo-800"
                            title="削除"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                            </svg>
                          </button>
                        </div>
                      ))}
                  </div>
                  {/* Singer Search Input */}
                  <SingerSearchInput
                    excludeIds={editableStreamInfo.participantIds}
                    onSelectSinger={(singer) => {
                      // 添加到 participants 列表（如果不存在）
                      if (!participants.find((p) => p.id === singer.id)) {
                        setParticipants((prev) => [...prev, singer]);
                      }
                      // 添加到選擇的 ID 列表並自動保存
                      updateParticipantsAndSave([...editableStreamInfo.participantIds, singer.id]);
                    }}
                    placeholder="チャンネル名を入力して参加者を追加..."
                  />
                </div>

                {/* Status Checkboxes */}
                <div className="mt-4 flex flex-wrap gap-4">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={editableStreamInfo.isProcessed}
                      onChange={(e) =>
                        updateIsProcessedAndSave(e.target.checked)
                      }
                      className="w-4 h-4 text-green-600 border-gray-300 rounded focus:ring-green-500"
                    />
                    <span className="text-sm font-medium text-gray-700">処理完了</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={editableStreamInfo.isHidden}
                      onChange={(e) =>
                        updateIsHiddenAndSave(e.target.checked)
                      }
                      className="w-4 h-4 text-red-600 border-gray-300 rounded focus:ring-red-500"
                    />
                    <span className="text-sm font-medium text-gray-700">非表示</span>
                  </label>
                </div>
              </>
            ) : (
              <>
                <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
                <p className="text-gray-500 mt-2">
                  {new Date(stream.stream_date).toLocaleString('ja-JP', {
                    year: 'numeric',
                    month: '2-digit',
                    day: '2-digit',
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                    hour12: false
                  })}
                </p>

                {/* Tags */}
                {stream.tags.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-4">
                    {stream.tags.map((tag) => (
                      <Tag key={tag.id} label={tag.display_name} color={tag.color} size="md" />
                    ))}
                  </div>
                )}

                {/* Participants */}
                {stream.participants && stream.participants.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-4">
                    {stream.participants.map((singer) => (
                      <div
                        key={singer.id}
                        className="flex items-center gap-2 px-3 py-1 bg-gray-100 rounded-full text-sm"
                      >
                        {singer.photo_url && (
                          <img
                            src={singer.photo_url}
                            alt={singer.name}
                            className="w-5 h-5 rounded-full"
                            onError={(e) => {
                              e.currentTarget.onerror = null;
                              e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                            }}
                          />
                        )}
                        <span className="text-gray-700">{singer.name}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}

            {/* Actions */}
            <div className="mt-6 flex gap-3">
              <a
                href={youtubeUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-2 px-4 py-2 bg-red-600 text-white font-medium rounded-lg hover:bg-red-700 transition-colors"
              >
                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                </svg>
                YouTubeで見る
              </a>
              <button
                onClick={toggleEditing}
                className={`inline-flex items-center gap-2 px-4 py-2 font-medium rounded-lg transition-colors ${
                  isEditing
                    ? 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                    : 'bg-indigo-600 text-white hover:bg-indigo-700'
                }`}
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                </svg>
                {isEditing ? '編集を閉じる' : '編集'}
              </button>
            </div>
          </div>
        </div>

        {/* Player + Timeline - 60% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border flex flex-col min-h-0">
          <div className="bg-black flex-1 min-h-[280px] flex items-center overflow-hidden">
            <YoutubePlayer
              videoId={stream.id}
              onReady={(player) => {
                playerInstanceRef.current = player;
              }}
            />
          </div>

          <div className="border-t py-3 px-0 shrink-0">
            <div className="space-y-1 px-3">
              <div className="relative h-3 bg-gray-100 rounded-none">
                {setoriTimeline.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      youtubePlayerSeekTo(item.start);
                      if (isEditing) scrollToEditableSong(item.start);
                    }}
                    className="absolute top-0 h-full rounded bg-indigo-500/80 hover:bg-indigo-600 transition-colors group"
                    style={{
                      left: `${getTimelineLeft(item.start)}%`,
                      width: `${getTimelineWidth(item.start, item.end)}%`,
                    }}
                  >
                    <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block z-50 pointer-events-none">
                      <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                        <div className="font-semibold">{item.label}</div>
                        {item.artist && <div className="text-gray-300">{item.artist}</div>}
                        <div className="text-gray-400 mt-1">
                          {formatTime(item.start)} - {formatTime(item.end)}
                        </div>
                        <div className="text-indigo-400 text-[10px] mt-1">seTORI</div>
                        {isEditing && <div className="text-gray-500 text-[10px]">クリックで曲にジャンプ</div>}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              <div className="relative h-3 bg-gray-100 rounded-none">
                {holodexTimeline.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      youtubePlayerSeekTo(item.start);
                      if (isEditing) addSongFromTimeline(item, 'holodex');
                    }}
                    className="absolute top-0 h-full rounded bg-blue-500/70 hover:bg-blue-600 transition-colors group"
                    style={{
                      left: `${getTimelineLeft(item.start)}%`,
                      width: `${getTimelineWidth(item.start, item.end)}%`,
                    }}
                  >
                    <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block z-50 pointer-events-none">
                      <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                        <div className="font-semibold">{item.label}</div>
                        {item.artist && <div className="text-gray-300">{item.artist}</div>}
                        <div className="text-gray-400 mt-1">
                          {formatTime(item.start)} - {formatTime(item.end)}
                        </div>
                        <div className="text-blue-400 text-[10px] mt-1">Holodex</div>
                        {isEditing && <div className="text-gray-500 text-[10px]">クリックで追加</div>}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              <div className="relative h-3 bg-gray-100 rounded-none">
                {commentTimeline.map((item) => {
                  const isPoint = item.end <= item.start;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      onClick={() => {
                        youtubePlayerSeekTo(item.start);
                        if (isEditing) addSongFromTimeline(item, 'comment');
                      }}
                      className={
                        isPoint
                          ? 'absolute top-1/2 -translate-y-1/2 w-2 h-2 rounded-full bg-orange-500 group'
                          : 'absolute top-0 h-full rounded bg-orange-500/70 hover:bg-orange-600 transition-colors group'
                      }
                      style={{
                        left: `${getTimelineLeft(item.start)}%`,
                        width: isPoint ? undefined : `${getTimelineWidth(item.start, item.end)}%`,
                      }}
                    >
                      <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block z-50 pointer-events-none">
                        <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                          <div className="font-semibold">{item.label}</div>
                          {item.artist && <div className="text-gray-300">{item.artist}</div>}
                          <div className="text-gray-400 mt-1">
                            {formatTime(item.start)}{!isPoint && ` - ${formatTime(item.end)}`}
                          </div>
                          <div className="text-orange-400 text-[10px] mt-1">Comment</div>
                          {isEditing && <div className="text-gray-500 text-[10px]">クリックで追加</div>}
                        </div>
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Right Column - Setlist */}
      <div className="w-full min-[1300px]:basis-3/5 min-[1300px]:shrink-0 min-w-0 min-h-0 min-[1300px]:self-stretch min-[1300px]:pr-6 flex flex-col">
        {/* Setlist Section - Unified View */}
        <div className="flex flex-col gap-3 mb-4 flex-none shrink-0">
          <h2 className="text-2xl font-bold text-gray-900">
            セットリスト ({isEditing ? editableSongs.length : stream.performances.length}曲)
          </h2>

          {/* Edit Mode Actions */}
          {isEditing && (
            <div className="flex justify-between items-center gap-2">
              <div className="flex flex-wrap gap-2">
                <button
                  onClick={() => syncVideoMutation.mutate()}
                  disabled={syncVideoMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-green-600 text-white font-medium rounded-lg hover:bg-green-700 transition-colors disabled:opacity-50"
                >
                  {syncVideoMutation.isPending ? '同期中...' : 'Holodex から同期'}
                </button>
                <button
                  onClick={() => syncToHolodexMutation.mutate()}
                  disabled={syncToHolodexMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-red-600 text-white font-medium rounded-lg hover:bg-red-700 transition-colors disabled:opacity-50"
                >
                  {syncToHolodexMutation.isPending ? 'Holodex へ同期中...' : 'seTORI から Holodex へ同期'}
                </button>
                <button
                  onClick={loadFromHolodex}
                  disabled={!stream?.holodex_timeline_songs || stream.holodex_timeline_songs.length === 0}
                  className="px-3 py-1.5 text-sm bg-blue-600 text-white font-medium rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50"
                >
                  Holodex データを読み込む
                </button>
                <button
                  onClick={loadFromComments}
                  disabled={!stream?.comment_timeline_songs?.length || commentAnalyzeLoading}
                  className="px-3 py-1.5 text-sm bg-green-600 text-white font-medium rounded-lg hover:bg-green-700 transition-colors disabled:opacity-50"
                >
                  {commentAnalyzeLoading ? 'コメント分析中...' : 'コメント データを読み込む'}
                </button>
                {editableSongs.length > 0 && (
                  <button
                    onClick={runAINormalization}
                    disabled={aiNormalizeMutation.isPending}
                    className="px-3 py-1.5 text-sm bg-purple-600 text-white font-medium rounded-lg hover:bg-purple-700 transition-colors disabled:opacity-50"
                  >
                    {aiNormalizeMutation.isPending ? 'AI処理中...' : 'AI正規化'}
                  </button>
                )}
              </div>
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={toggleEditing}
                  className="px-3 py-1.5 text-sm bg-gray-200 text-gray-700 font-medium rounded-lg hover:bg-gray-300 transition-colors"
                >
                  キャンセル
                </button>
                <button
                  onClick={handleConfirm}
                  disabled={createPerformancesMutation.isPending || updateStreamMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {createPerformancesMutation.isPending || updateStreamMutation.isPending ? '処理中...' : '変更を保存'}
                </button>
              </div>
            </div>
          )}
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
        {isEditing ? (
          /* Editable Setlist */
          <div className="bg-white rounded-lg shadow-sm border p-6">
            {/* Editable songs list will default to channel owner as vocalist */}

            <div className="space-y-4">
              {editableSongs.length > 0 && editableSongs.map((song, index) => (
                  <div key={song.id} id={`song-${song.id}`} className={`border rounded-lg p-4 transition-colors duration-500 ${highlightedSongId === song.id ? 'bg-yellow-100 border-yellow-400' : 'bg-gray-50'}`}>
                    <div className="flex justify-between items-start mb-3">
                      <div className="flex items-center gap-3">
                        {/* Art Thumbnail */}
                        {song.artUrl ? (
                          <img
                            src={song.artUrl}
                            alt={song.name}
                            className="w-12 h-12 object-cover rounded shadow-sm"
                          />
                        ) : (
                          <div className="w-12 h-12 bg-gray-200 rounded flex items-center justify-center">
                            <svg className="w-6 h-6 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                            </svg>
                          </div>
                        )}
                        <div>
                          <span className="text-sm font-medium text-gray-500">#{index + 1}</span>
                          {/* Song ID Badge */}
                          <div className="flex items-center gap-2 mt-1">
                            {song.matchedSongId ? (
                              <a
                                href={`/songs/${song.matchedSongId}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-xs font-mono text-indigo-600 hover:underline"
                              >
                                {song.matchedSongId.slice(0, 8)}...
                              </a>
                            ) : (
                              <span className="px-2 py-0.5 bg-green-100 text-green-700 text-xs font-medium rounded">
                                New
                              </span>
                            )}
                            {/* Removed edit song button */}
                          </div>
                        </div>
                      </div>
                      <button
                        onClick={() => removeSong(index)}
                        className="text-red-500 hover:text-red-700 text-sm"
                      >
                        削除
                      </button>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      {/* Song Name with Search */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          楽曲名 <span className="text-gray-400 font-normal">(入力して検索)</span>
                        </label>
                        <SongSearchInput
                          value={song.name}
                          onChange={(value) => handleSongChange(index, 'name', value)}
                          onSelectSong={(selectedSong) => handleSelectExistingSong(index, selectedSong)}
                          placeholder="楽曲名を入力して検索"
                          showToast={showToast}
                        />
                        {/* AI 正規化差異顯示 */}
                        {song.aiNormalizedName && (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{song.aiNormalizedName}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{song.name}</span>
                          </div>
                        )}
                        {/* 合併元顯示 */}
                        {song.mergedFrom && song.mergedFrom.length > 0 && (
                          <div className="mt-1 text-sm">
                            <span className="text-orange-600">マージ:</span>{' '}
                            <span className="text-gray-500">{song.mergedFrom.join(', ')}</span>
                          </div>
                        )}
                      </div>

                      {/* Artist */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          原曲アーティスト
                        </label>
                        <input
                          type="text"
                          value={song.artist}
                          onChange={(e) => handleSongChange(index, 'artist', e.target.value)}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                          placeholder="アーティスト名を入力"
                        />
                        {/* AI 正規化差異顯示 */}
                        {song.aiNormalizedArtist && (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{song.aiNormalizedArtist}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{song.artist}</span>
                          </div>
                        )}
                      </div>

                      {/* Start Time */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          開始時間
                        </label>
                        <div className="flex gap-2">
                          <input
                            key={`start-${song.id}-${song.start}`}
                            type="text"
                            defaultValue={formatTimeInput(song.start)}
                            onBlur={(e) => handleTimeChange(index, 'start', e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                handleTimeChange(index, 'start', e.currentTarget.value);
                                e.currentTarget.blur();
                              }
                            }}
                            className="w-32 px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono"
                            placeholder="0:00"
                          />
                          <button
                            onClick={() => {
                              const currentTime = youtubePlayerGetCurrentTime();
                              if (currentTime !== null) {
                                handleSongChange(index, 'start', Math.floor(currentTime));
                              }
                            }}
                            className="px-3 py-2 bg-blue-100 text-blue-600 rounded-lg hover:bg-blue-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
                            title="現在の再生時間を設定"
                          >
                            {currentPlayerTime !== null ? formatTimeInput(Math.floor(currentPlayerTime)) : '--:--'}
                          </button>
                          <button
                            onClick={() => youtubePlayerSeekTo(song.start)}
                            className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                            title="この時間から再生"
                          >
                            <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </button>
                        </div>
                      </div>

                      {/* End Time */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          終了時間 <span className="text-red-500">*</span>
                        </label>
                        <div className="flex gap-2">
                          <input
                            key={`end-${song.id}-${song.end}`}
                            type="text"
                            defaultValue={song.end ? formatTimeInput(song.end) : ''}
                            onBlur={(e) => handleTimeChange(index, 'end', e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                handleTimeChange(index, 'end', e.currentTarget.value);
                                e.currentTarget.blur();
                              }
                            }}
                            className={`w-32 px-3 py-2 border rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono ${
                              song.end === 0 ? 'border-red-400 bg-red-50' : song.isEndTimeEstimated ? 'border-orange-300 bg-orange-50' : 'border-gray-300'
                            }`}
                            placeholder={song.end === 0 ? "歌曲長度ボタンで自動設定" : "0:00"}
                          />
                          {/* 時長按鈕 */}
                          <button
                            onClick={() => {
                              if (song.trackDuration) {
                                const newEnd = song.start + song.trackDuration;
                                handleSongChange(index, 'end', newEnd);
                                handleSongChange(index, 'isEndTimeEstimated', false);
                              }
                            }}
                            disabled={!song.trackDuration}
                            className={`px-3 py-2 rounded-lg font-mono text-sm font-medium transition-colors whitespace-nowrap min-w-[5.5rem] ${
                              song.trackDuration
                                ? 'bg-green-100 text-green-700 hover:bg-green-200'
                                : 'bg-gray-100 text-gray-400 cursor-not-allowed'
                            }`}
                            title={song.trackDuration ? 'iTunes歌曲長度を適用' : '歌曲長度情報なし'}
                          >
                            {formatDuration(song.trackDuration)}
                          </button>
                          <button
                            onClick={() => {
                              const currentTime = youtubePlayerGetCurrentTime();
                              if (currentTime !== null) {
                                handleSongChange(index, 'end', Math.floor(currentTime));
                              }
                            }}
                            className="px-3 py-2 bg-blue-100 text-blue-600 rounded-lg hover:bg-blue-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
                            title="現在の再生時間を設定"
                          >
                            {currentPlayerTime !== null ? formatTimeInput(Math.floor(currentPlayerTime)) : '--:--'}
                          </button>
                          {song.end > 0 && (
                            <>
                              <button
                                onClick={() => youtubePlayerSeekTo(Math.max(song.end - 3, 0))}
                                className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                                title="終了時間の3秒前から再生"
                              >
                                <svg className="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                  <path d="M6 8a7 7 0 1 1 1.2 8.8" />
                                  <polyline points="6 3 6 8 11 8" />
                                  <text x="12" y="15" textAnchor="middle" fontSize="8" fontWeight="bold" fill="currentColor" stroke="none">3</text>
                                </svg>
                                <span className="sr-only">-3s</span>
                              </button>
                              <button
                                onClick={() => youtubePlayerSeekTo(song.end)}
                                className="px-3 py-2 bg-red-100 text-red-600 rounded-lg hover:bg-red-200 transition-colors"
                                title="終了時間から再生"
                              >
                                <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                                  <path d="M8 5v14l11-7z" />
                                </svg>
                              </button>
                            </>
                          )}
                        </div>
                        {/* 沒有結束時間的提示 */}
                        {song.end === 0 && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-500">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
                            </svg>
                            <span>終了時間は必須です</span>
                          </div>
                        )}
                        {/* 估計時間警告 */}
                        {song.end > 0 && song.isEndTimeEstimated && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-orange-600">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                            </svg>
                            <span>推定時間 - 要確認</span>
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Tags */}
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700 mb-2">
                        タグ
                      </label>
                      <div className="flex flex-wrap gap-2">
                        {PERFORMANCE_TAGS.map((tag) => (
                          <button
                            key={tag.id}
                            onClick={() => toggleTag(index, tag.id)}
                            className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                              song.tags.includes(tag.id)
                                ? 'text-white'
                                : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                            }`}
                            style={song.tags.includes(tag.id) ? { backgroundColor: tag.color } : {}}
                          >
                            {tag.label}
                          </button>
                        ))}
                        {/* Custom tags */}
                        {song.customTags.map((ct) => (
                          <span
                            key={ct}
                            className="inline-flex items-center gap-1 px-3 py-1 rounded-full text-sm font-medium bg-gray-500 text-white"
                          >
                            {ct}
                            <button
                              onClick={() => {
                                setEditableSongs((prev) => {
                                  const updated = [...prev];
                                  updated[index] = { ...updated[index], customTags: updated[index].customTags.filter((t) => t !== ct) };
                                  return updated;
                                });
                              }}
                              className="hover:text-red-200 ml-0.5"
                            >
                              ×
                            </button>
                          </span>
                        ))}
                        {/* Custom tag input */}
                        <input
                          type="text"
                          placeholder="+ カスタムタグ"
                          className="px-3 py-1 rounded-full text-sm border border-dashed border-gray-300 bg-transparent text-gray-500 focus:border-gray-500 focus:outline-none w-32"
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              const value = e.currentTarget.value.trim();
                              if (value && !song.customTags.includes(value)) {
                                setEditableSongs((prev) => {
                                  const updated = [...prev];
                                  updated[index] = { ...updated[index], customTags: [...updated[index].customTags, value] };
                                  return updated;
                                });
                                e.currentTarget.value = '';
                              }
                              e.preventDefault();
                            }
                          }}
                        />
                      </div>
                    </div>

                    {/* Vocalist (ボーカル) Selection */}
                    <div className="mt-4">
                      <label className="block text-sm font-medium text-gray-700 mb-2">
                        ボーカル
                      </label>
                      <div className="space-y-2">
                        {/* Display selected vocalists */}
                        {song.singerIds.length > 0 && (
                          <div className="flex flex-wrap gap-2 mb-2">
                            {song.singerIds
                              .slice()
                              .sort((a, b) => {
                                // 頻道擁有者排在最前面
                                if (channelOwner && a === channelOwner.id) return -1;
                                if (channelOwner && b === channelOwner.id) return 1;
                                return 0;
                              })
                              .map((singerId) => {
                              const singer = participants.find((p) => p.id === singerId);
                              return singer ? (
                                <div
                                  key={singerId}
                                  className="flex items-center gap-2 px-3 py-1 bg-indigo-100 text-indigo-700 rounded-full text-sm"
                                >
                                  {singer.photo_url && (
                                    <img
                                      src={singer.photo_url}
                                      alt={singer.name}
                                      className="w-5 h-5 rounded-full"
                                      onError={(e) => {
                                        e.currentTarget.onerror = null;
                                        e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                                      }}
                                    />
                                  )}
                                  <span>{singer.name}</span>
                                  <button
                                    onClick={() => {
                                      const newSingerIds = song.singerIds.filter((id) => id !== singerId);
                                      handleSongChange(index, 'singerIds', newSingerIds);
                                    }}
                                    className="ml-1 text-indigo-600 hover:text-indigo-800"
                                  >
                                    ✕
                                  </button>
                                </div>
                              ) : null;
                            })}
                          </div>
                        )}
                        {/* Vocalist search input */}
                        <SingerSearchInput
                          onSelectSinger={(singer) => {
                            if (!song.singerIds.includes(singer.id)) {
                              handleSongChange(index, 'singerIds', [...song.singerIds, singer.id]);
                              // 如果歌手不在participants中，加入
                              if (!participants.find(p => p.id === singer.id)) {
                                setParticipants([...participants, singer]);
                              }
                            }
                          }}
                          excludeIds={song.singerIds}
                          placeholder="ボーカルを検索して追加..."
                        />
                      </div>
                    </div>
                  </div>
                ))}

              {/* Add Song Button - always show */}
              <button
                onClick={addSong}
                className="w-full py-3 border-2 border-dashed border-gray-300 rounded-lg text-gray-500 hover:border-indigo-500 hover:text-indigo-500 transition-colors"
              >
                + 楽曲を追加
              </button>

              {/* Help text when list is empty */}
              {editableSongs.length === 0 && (
                <div className="text-center py-8 text-gray-500">
                  上のボタンから楽曲データを読み込むか、「+ 楽曲を追加」で手動追加してください
                </div>
              )}
            </div>
          </div>
        ) : (
          /* Read-only Setlist */
          stream.performances.length === 0 ? (
            <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
              セットリストがまだ登録されていません
            </div>
          ) : (
            <div className="bg-white rounded-lg shadow-sm border">
              <div className="overflow-x-auto min-[1300px]:max-h-[calc(100vh-14rem)] min-[1300px]:overflow-y-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50 sticky top-0 z-10">
                  <tr>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-24">
                      #
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                      時間
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      楽曲
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      アーティスト
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-40">
                      タグ
                    </th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider w-32">
                      ボーカル
                    </th>
                    <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider w-20">
                      再生
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-white divide-y divide-gray-200">
                  {stream.performances.map((perf, index) => {
                    const singerCount = perf.singers?.length || 0;
                    const showCount = singerCount > 3;
                    // 排序歌手：頻道所有者優先
                    const sortedSingers = perf.singers?.sort((a, b) => {
                      if (channelOwner && a.id === channelOwner.id) return -1;
                      if (channelOwner && b.id === channelOwner.id) return 1;
                      return 0;
                    }) || [];
                    const displaySingers = showCount ? sortedSingers.slice(0, 3) : sortedSingers;
                    
                    return (
                      <tr key={perf.id} className="hover:bg-gray-50">
                        {/* # with Art thumbnail */}
                        <td className="px-4 py-4">
                          <div className="relative w-16 h-16">
                            {perf.arts ? (
                              <img
                                src={perf.arts}
                                alt={perf.song_name}
                                className="w-16 h-16 object-cover rounded-lg shadow-sm"
                              />
                            ) : (
                              <div className="w-16 h-16 bg-gray-100 rounded-lg flex items-center justify-center">
                                <svg className="w-8 h-8 text-gray-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19V6l12-3v13M9 19c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zm12-3c0 1.105-1.343 2-3 2s-3-.895-3-2 1.343-2 3-2 3 .895 3 2zM9 10l12-3" />
                                </svg>
                              </div>
                            )}
                            {/* Index badge */}
                            <div className="absolute top-0 left-0 bg-indigo-600 text-white text-xs font-bold rounded-tl-lg rounded-br-lg px-2 py-0.5">
                              #{index + 1}
                            </div>
                          </div>
                        </td>
                        <td className="px-4 py-4 text-sm text-gray-500 font-mono">
                          {formatTime(perf.start_seconds)}
                          {perf.end_seconds > 0 && (
                            <span className="text-gray-400"> ~ {formatTime(perf.end_seconds)}</span>
                          )}
                        </td>
                        <td className="px-4 py-4">
                          <Link
                            to={`/songs/${perf.song_id}`}
                            className="text-indigo-600 hover:text-indigo-900 font-medium"
                          >
                            {perf.song_name}
                          </Link>
                        </td>
                        <td className="px-4 py-4 text-sm text-gray-500">
                          {perf.original_artist}
                        </td>
                        <td className="px-4 py-4">
                          <div className="flex flex-wrap gap-1">
                            {perf.tags.map((tag) => (
                              <Tag key={tag.id} label={tag.display_name} color={tag.color} />
                            ))}
                            {perf.custom_tags?.map((ct) => (
                              <Tag key={ct} label={ct} color="#6B7280" />
                            ))}
                          </div>
                        </td>
                        {/* Singer avatars */}
                        <td className="px-4 py-4">
                          {singerCount === 0 ? (
                            <span className="text-sm text-gray-400">なし</span>
                          ) : (
                            <div className="flex items-center relative h-8">
                              {displaySingers?.map((singer, singerIndex) => (
                                <Link
                                  key={singer.id}
                                  to={`/singers/${singer.id}`}
                                  title={singer.name}
                                  className="relative -ml-2 first:ml-0 hover:z-50"
                                  style={{
                                    zIndex: displaySingers.length - singerIndex,
                                  }}
                                >
                                  <img
                                    src={
                                      singer.photo_url ||
                                      `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                                    }
                                    alt={singer.name}
                                    className="w-8 h-8 rounded-full border-2 border-white shadow-sm hover:shadow-md transition-shadow"
                                    onError={(e) => {
                                      e.currentTarget.onerror = null;
                                      e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                                    }}
                                  />
                                </Link>
                              ))}
                              {showCount && (
                                <button
                                  onClick={() => setVocalistPopupSingers(sortedSingers)}
                                  title={perf.singers
                                    ?.map((s) => s.name)
                                    .join(', ')}
                                  className="relative -ml-2 w-8 h-8 rounded-full bg-gray-300 border-2 border-white flex items-center justify-center text-xs font-bold text-gray-700 cursor-pointer hover:bg-gray-400 transition-colors"
                                >
                                  +{singerCount - 3}
                                </button>
                              )}
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-4 text-right">
                          <button
                            onClick={() => youtubePlayerSeekTo(perf.start_seconds)}
                            className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-red-100 text-red-600 hover:bg-red-200 transition-colors"
                            title={`${perf.song_name} を再生`}
                          >
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
                </table>
              </div>
            </div>
          )
        )}
        </div>
      </div>
    </div>

    {/* Vocalist Popup */}
    {vocalistPopupSingers && (
      <div
        className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
        onClick={() => setVocalistPopupSingers(null)}
      >
        <div
          className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4 p-6"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex justify-between items-center mb-4">
            <h3 className="text-lg font-bold text-gray-900">ボーカル一覧</h3>
            <button
              onClick={() => setVocalistPopupSingers(null)}
              className="text-gray-400 hover:text-gray-600 transition-colors"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {vocalistPopupSingers.map((singer) => (
              <Link
                key={singer.id}
                to={`/singers/${singer.id}`}
                className="flex items-center gap-3 p-3 rounded-lg hover:bg-gray-50 transition-colors"
                onClick={() => setVocalistPopupSingers(null)}
              >
                <img
                  src={
                    singer.photo_url ||
                    `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                  }
                  alt={singer.name}
                  className="w-12 h-12 rounded-full border-2 border-gray-200"
                  onError={(e) => {
                    e.currentTarget.onerror = null;
                    e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                  }}
                />
                <div className="flex-1">
                  <div className="font-medium text-gray-900">{singer.name}</div>
                  {singer.english_name && (
                    <div className="text-sm text-gray-500">{singer.english_name}</div>
                  )}
                </div>
                <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
              </Link>
            ))}
          </div>
        </div>
      </div>
      )}
    </>
  );
}
