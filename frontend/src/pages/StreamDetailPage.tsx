import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { streamApi, performanceApi, aiApi, songApi, singerApi, itunesApi, holodexApi, commentApi, tagApi } from '../api/client';
import type { Singer, CreatePerformanceItem, AINormalizationItem, Song, UpdateStreamRequest, ITunesSearchResult, CommentSong, SongSuggestion, EndSource, FieldChange } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/ToastContext';
import { useAuthStore, hasPermission, PERM } from '../store/auth';
import { usePlayerStore } from '../store/player';
import YoutubePlayer from '../components/YoutubePlayer';
import { youtubePlayerSeekTo, youtubePlayerGetCurrentTime } from '../components/youtubePlayerControl';
import type { YouTubePlayerInstance } from '../types/youtube';
import TimestampTweaker from '../components/TimestampTweaker';
import QueueAddButton from '../components/QueueAddButton';
import RawCommentsPanel from '../components/RawCommentsPanel';
import ArtistLinks from '../components/ArtistLinks';
import FieldProvenance from '../components/FieldProvenance';
import { extractRawCommentTimestamps } from '../utils/rawCommentTimestamps';
import { matchReasonLabel } from '../utils/matchReason';


// 編集可能な配信情報
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
  // 追加フィールド
  matchedSongId: string | null; // マッチした楽曲ID、null は新規作成を示す
  artUrl: string | null; // アートワークURL
  itunesId: number | null; // Holodex が提供する iTunes ID
  // itunesId が既に DB（song_itunes）に紐付いているか。false は保存時に新規紐付けが作られることを示す
  // （primary な iTunes ID は Holodex へのアップロードにも使われるため、誤りは外部に伝播する）
  itunesFromDb?: boolean;
  trackDuration: number | null; // iTunes 楽曲長（秒）
  originalName: string; // 元の名称を追跡（変更判定用）
  originalArtist: string; // 元のアーティストを追跡
  // AI 正規化追跡
  aiNormalizedName?: string; // AI 変更前の名称（変更された場合）
  aiNormalizedArtist?: string; // AI 変更前のアーティスト（変更された場合）
  // 「抽出したままの値が、どの処理でどう変わったか」。AI 正規化と DB 照合を区別して出す。
  // aiNormalized* は 1 段しか表せず、どちらの仕業かも分からないので、表示はこちらを使う。
  changes?: FieldChange[];
  // 時間推定マーク
  isEndTimeEstimated?: boolean; // 終了時間が推定値かどうか
  // Chat 拍手偵測參考值（當與 comment explicit end 差異大時提醒）
  chatEnd?: number;
  endDiff?: number;
  // 來源追蹤與還原
  originalCommentEnd?: number; // 來自 comment 分析的原始明確 end（用於還原）
  endSource?: EndSource;
  // マージ追跡
  mergedFrom?: string[]; // AI 正規化後にマージされた元の曲名
  // 自由文本タグ
  customTags: string[];
  // 単曲編集フロー：確認済みフラグ（ローカルのみ、保存は最後に一括）
  confirmed?: boolean;
}

// AI 正規化後に重複楽曲をマージ
// 条件：name が完全一致 + artist が完全一致 + start の時間差 ≤ 30s
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
        // Merge: 情報がより完全な方を保持
        const hasRealEnd = (s: EditableSong) => s.end > 0 && !s.isEndTimeEstimated;
        const preferSong = hasRealEnd(song) && !hasRealEnd(existing);

        // 吸収された側の元の名称を記録（AI 変更前の名称）
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
            itunesFromDb: song.itunesId != null ? song.itunesFromDb : existing.itunesFromDb,
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
            itunesFromDb: existing.itunesId != null ? existing.itunesFromDb : song.itunesFromDb,
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

// 楽曲検索入力コンポーネントの Props
interface SongSearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSelectSong: (song: Song) => void;
  placeholder?: string;
  showToast?: (message: string, type?: 'success' | 'error' | 'info') => void;
}

// 楽曲検索入力コンポーネント（オートコンプリート付き）
function SongSearchInput({ value, onChange, onSelectSong, placeholder, showToast }: SongSearchInputProps) {
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
        // DB と iTunes を並行検索
        const [dbResult, itunesResult] = await Promise.all([
          songApi.list(1, 5, searchQuery).catch(() => ({ songs: [], pagination: { page: 1, limit: 5, total: 0, total_pages: 0 } })),
          itunesApi.search(searchQuery).catch(() => ({ results: [] }))
        ]);
        setDbSuggestions(dbResult.songs);
        setItunesSuggestions(itunesResult.results.slice(0, 5)); // iTunes 結果数を制限
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

// 歌手検索入力コンポーネントの Props
interface SingerSearchInputProps {
  onSelectSinger: (singer: Singer) => void;
  excludeIds?: string[];
  placeholder?: string;
}

// 歌手検索入力コンポーネント（オートコンプリート付き）
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

      {/* 検索候補ドロップダウン */}
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
    // 数字のみの場合、秒数とみなす
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

// 長さをフォーマットし "+MM:SS" または "+H:MM:SS" で表示
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

export default function StreamDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);

  const [isEditing, setIsEditing] = useState(false);
  // 編集モード左上のタブ（操作 / Holodex / コメント / 生コメント）
  const [editTab, setEditTab] = useState<'actions' | 'holodex' | 'comment' | 'raw'>('actions');
  // 閲覧モードのクイック編集 UI（タグ選択・参加者追加）の開閉
  const [tagPickerOpen, setTagPickerOpen] = useState(false);
  const [participantAddOpen, setParticipantAddOpen] = useState(false);
  const [editableSongs, setEditableSongs] = useState<EditableSong[]>([]);
  // 単曲編集フロー：現在フォーカス中の曲。選択曲だけ詳細カードを展開し、他は圧縮行にする
  const [selectedSongIndex, setSelectedSongIndex] = useState<number | null>(null);

  // 曲リストの読み込み/増減時に選択を境界内へ補正（空なら選択解除、未選択なら先頭）
  useEffect(() => {
    if (editableSongs.length === 0) {
      setSelectedSongIndex(null);
    } else if (selectedSongIndex === null || selectedSongIndex >= editableSongs.length) {
      setSelectedSongIndex(0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editableSongs.length]);

  // 編集モード中はグローバルプレイヤーを一時停止（ページ内プレイヤーとの二重再生を防ぐ）
  useEffect(() => {
    if (isEditing) usePlayerStore.getState().setPlaying(false);
  }, [isEditing]);

  // 曲を選択してプレイヤーをその開始位置へ（Holodex の編集フローと同じ）
  const selectSong = (index: number, seek = true) => {
    setSelectedSongIndex(index);
    const song = editableSongs[index];
    if (seek && song && song.start >= 0) {
      youtubePlayerSeekTo(song.start);
    }
  };

  // 確認して次へ：この曲を確認済みにし、次の未確認曲へフォーカス移動
  const confirmAndNext = (index: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      updated[index] = { ...updated[index], confirmed: true };
      return updated;
    });
    // index の後ろ→先頭の順で次の未確認曲を探す（自分は除く）
    const n = editableSongs.length;
    for (let step = 1; step < n; step++) {
      const i = (index + step) % n;
      if (!editableSongs[i].confirmed) {
        selectSong(i);
        return;
      }
    }
  };

  const confirmedCount = editableSongs.filter((s) => s.confirmed).length;
  const [holodexTimelineSongs, setHolodexTimelineSongs] = useState<SongSuggestion[]>([]);
  const [commentTimelineSongs, setCommentTimelineSongs] = useState<CommentSong[]>([]);
  const [channelOwner, setChannelOwner] = useState<Singer | null>(null);
  const [participants, setParticipants] = useState<Singer[]>([]);
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
    staleTime: 0, // ページに入るたびに再読み込みを保証
  });

  // 編集画面の raw comment タイムラインと生コメントタブで同じキャッシュを共有する。
  const { data: rawCommentsData } = useQuery({
    queryKey: ['raw-comments', id],
    queryFn: () => commentApi.getComments(id!),
    enabled: !!id && isEditing,
    staleTime: Infinity,
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

  // stream データ読み込み後に、チャンネルオーナーとタイムラインを設定
  useEffect(() => {
    setChannelOwner(stream?.channel_owner || null);
    // 保存されたタイムラインデータを読み込み（ない場合はクリア）
    setHolodexTimelineSongs(stream?.holodex_timeline_songs || []);
    setCommentTimelineSongs(stream?.comment_timeline_songs || []);
  }, [stream]);

  // 編集モード時はページ全体のスクロールを避ける（ブロック内スクロールに変更）
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

  // プレイヤーの現在時刻を定期更新（1秒ごと）
  useEffect(() => {
    const interval = setInterval(() => {
      const time = youtubePlayerGetCurrentTime();
      setCurrentPlayerTime(time);
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  // Stream 情報を更新
  const updateStreamMutation = useMutation({
    mutationFn: (req: UpdateStreamRequest) => streamApi.update(id!, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
    },
    onError: (err: Error) => {
      showToast(`更新エラー: ${err.message}`, 'error');
    },
  });

  // 確認して直接パフォーマンス記録を作成
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
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
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

  // YouTube Data API から公開コメントを明示的に取り直す（Holodex fallback なし）
  const syncYouTubeCommentsMutation = useMutation({
    mutationFn: () => commentApi.syncYouTube(id!),
    onSuccess: async (data) => {
      // raw が変わると backend 側で旧 comment_songs cache は破棄される。
      setCommentTimelineSongs([]);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['raw-comments', id] }),
        queryClient.invalidateQueries({ queryKey: ['stream', id] }),
      ]);
      showToast(`YouTubeからコメント${data.comment_count}件を同期しました`, 'success');
    },
    onError: (err: Error) => {
      showToast(`YouTubeコメント同期エラー: ${err.message}`, 'error');
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

  const [holodexAnalyzeLoading, setHolodexAnalyzeLoading] = useState(false);

  // force=true で快取を無視し AI 再分析（再正規化）。通常はキャッシュ済み結果を秒読みする。
  // 編集リストへ入れるときの既定ボーカル（参加者 → チャンネル主）
  const getDefaultSingerIds = () =>
    stream?.participants?.map((p) => p.id) || (channelOwner ? [channelOwner.id] : []);

  // Holodex 分析結果（SongSuggestion）→ EditableSong 変換（一括読み込み・単曲追加で共用）。
  // end === chat_end のものは chat 由来、そうでない end は Holodex 明示値として扱う。
  const suggestionToEditableSong = async (
    song: SongSuggestion,
    editableId: string,
    defaultSingerIds: string[],
  ): Promise<EditableSong> => {
    const hasMatch = !!song.matched_song_id;
    const finalName = hasMatch && song.matched_song_name
      ? song.matched_song_name : (song.normalized_name || song.name);
    const finalNameReading = hasMatch && song.matched_song_name_reading != null
      ? song.matched_song_name_reading : (song.normalized_name_reading || '');
    const finalArtist = hasMatch && song.matched_song_artist
      ? song.matched_song_artist : (song.normalized_artist || song.original_artist);
    const finalArtistReading = hasMatch && song.matched_song_artist_reading != null
      ? song.matched_song_artist_reading : (song.normalized_artist_reading || '');
    const artUrl = (hasMatch ? song.matched_song_art_url : undefined) || song.art_url || null;
    // matched_song_itunes_id は DB 照合で得た既存の紐付け。無い場合のみ Holodex 提供の ID を使う
    // （後者は未登録なので、保存時に song_itunes へ新規紐付けが作られる）
    const itunesId = song.matched_song_itunes_id || song.itunes_id || null;
    const itunesFromDb = !!song.matched_song_itunes_id;
    const trackDuration = itunesId ? await fetchTrackDurationByItunesId(itunesId) : null;
    const explicitEnd = song.end_seconds > 0 && song.end_seconds !== song.chat_end;
    return {
      id: editableId,
      name: finalName,
      nameReading: finalNameReading,
      artist: finalArtist,
      artistReading: finalArtistReading,
      start: song.start_seconds,
      end: song.end_seconds,
      tags: song.tags || [],
      singerIds: song.singer_ids.length > 0 ? song.singer_ids : defaultSingerIds,
      matchedSongId: song.matched_song_id || null,
      artUrl,
      itunesId,
      itunesFromDb,
      trackDuration,
      originalName: finalName,
      originalArtist: finalArtist,
      aiNormalizedName: finalName !== song.name ? song.name : undefined,
      aiNormalizedArtist: finalArtist !== song.original_artist ? song.original_artist : undefined,
      changes: song.changes,
      isEndTimeEstimated: false,
      chatEnd: song.chat_end,
      endDiff: song.end_diff,
      originalCommentEnd: explicitEnd ? song.end_seconds : undefined,
      endSource: song.end_seconds > 0 ? (explicitEnd ? 'holodex' : 'chat') : undefined,
      customTags: [],
    };
  };

  // コメント分析結果（CommentSong）→ EditableSong 変換（一括読み込み・単曲追加で共用）
  const commentSongToEditableSong = async (
    song: CommentSong,
    editableId: string,
    defaultSingerIds: string[],
  ): Promise<EditableSong> => {
    const hasMatch = !!song.matched_song_id;
    const finalName = hasMatch && song.matched_song_name
      ? song.matched_song_name : (song.normalized_name || song.name);
    const finalNameReading = hasMatch && song.matched_song_name_reading != null
      ? song.matched_song_name_reading : (song.normalized_name_reading || '');
    const finalArtist = hasMatch && song.matched_song_artist
      ? song.matched_song_artist : (song.normalized_artist || song.original_artist);
    const finalArtistReading = hasMatch && song.matched_song_artist_reading != null
      ? song.matched_song_artist_reading : (song.normalized_artist_reading || '');
    const artUrl = (hasMatch ? song.matched_song_art_url : undefined) || null;
    const itunesId = song.matched_song_itunes_id || null;
    const trackDuration = itunesId ? await fetchTrackDurationByItunesId(itunesId) : null;
    return {
      id: editableId,
      name: finalName,
      nameReading: finalNameReading,
      artist: finalArtist,
      artistReading: finalArtistReading,
      start: song.start,
      end: song.end,
      tags: song.tags || [],
      singerIds: defaultSingerIds,
      matchedSongId: song.matched_song_id || null,
      artUrl,
      itunesId,
      itunesFromDb: itunesId != null, // コメント経路の ID は DB 照合由来のみ
      trackDuration,
      originalName: finalName,
      originalArtist: finalArtist,
      aiNormalizedName: finalName !== song.name ? song.name : undefined,
      aiNormalizedArtist: finalArtist !== song.original_artist ? song.original_artist : undefined,
      changes: song.changes,
      isEndTimeEstimated: song.is_end_time_estimated,
      chatEnd: song.chat_end,
      endDiff: song.end_diff,
      originalCommentEnd: song.end, // 載入時的 end 視為 comment 原始值
      endSource: song.end > 0 && !song.is_end_time_estimated ? 'comment' : undefined,
      customTags: [],
    };
  };

  const loadFromHolodex = async (force = false) => {
    if (!id) return;
    if (!stream?.holodex_timeline_songs || stream.holodex_timeline_songs.length === 0) {
      showToast('Holodexデータがありません', 'info');
      return;
    }
    setHolodexAnalyzeLoading(true);
    try {
      // 分析（正規化＋DB照合＋拍手end）を実行し、結果をそのまま反映する
      const analyzed = await holodexApi.analyzeSongs(id, force);
      const sortedSongs = [...analyzed].sort((a, b) => a.start_seconds - b.start_seconds);
      setHolodexTimelineSongs(sortedSongs);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await suggestionToEditableSong(sortedSongs[index], `holodex-${index}`, getDefaultSingerIds()));
      }

      const merged = mergeDuplicateSongs(songs);
      const mergedCount = songs.length - merged.length;
      setEditableSongs(merged);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`Holodexから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      showToast('Holodex分析に失敗しました', 'error');
      console.error('Holodex analysis failed:', error);
    } finally {
      setHolodexAnalyzeLoading(false);
    }
  };

  const [commentAnalyzeLoading, setCommentAnalyzeLoading] = useState(false);

  // force=true で快取を無視し AI 再分析（再正規化）。通常は快取済みの結果を秒読みする。
  const loadFromComments = async (force = false) => {
    if (!id) return;
    setCommentAnalyzeLoading(true);
    try {
      const result = await commentApi.analyze(id, force);
      const sortedSongs = [...result.songs].sort((a, b) => a.start - b.start);

      const songs: EditableSong[] = [];
      for (let index = 0; index < sortedSongs.length; index++) {
        songs.push(await commentSongToEditableSong(sortedSongs[index], `comment-${index}`, getDefaultSingerIds()));
      }

      const merged = mergeDuplicateSongs(songs);
      const mergedCount = songs.length - merged.length;
      setEditableSongs(merged);
      // 照合の結果（候補・変更履歴）はこの応答にしか無い。配信を開いただけの
      // 読み取りでは照合しないので、タイムライン側もここで差し替える。
      setCommentTimelineSongs(sortedSongs);
      const mergeMsg = mergedCount > 0 ? `（${mergedCount}曲の重複を統合）` : '';
      showToast(`コメントから${merged.length}曲を読み込みました${mergeMsg}`, 'success');
    } catch (error) {
      showToast('コメント分析に失敗しました', 'error');
      console.error('Comment analysis failed:', error);
    } finally {
      setCommentAnalyzeLoading(false);
    }
  };

  // live chat の拍手から end だけを取り直す（AI は呼ばない）。
  // 一括プレ分析はキャッシュ命中だと拍手 end を飛ばすので、後から埋めるのはこの経路。
  const chatEndMutation = useMutation({
    mutationFn: () => commentApi.analyzeChatEnds(id!),
    onSuccess: (res) => {
      // comment_songs が書き換わっているので、タイムラインの元データを読み直す
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
      if (res.total === 0) {
        showToast('分析済みの曲がありません（先に「全部読み込む」を実行してください）', 'info');
      } else if (res.changed === 0) {
        showToast('拍手からは新しい終了時間を取得できませんでした', 'info');
      } else if (res.filled === 0) {
        // 全曲すでに終了時間があった場合。値は変えず、拍手の位置だけ記録している
        showToast(`${res.changed}曲で拍手の位置を記録しました（既存の終了時間は変更なし）`, 'success');
      } else {
        showToast(`${res.filled}/${res.total}曲の終了時間を拍手から取得しました`, 'success');
      }
    },
    onError: (err: Error) => showToast(`拍手解析に失敗しました: ${err.message}`, 'error'),
  });

  // 提案リストから1曲だけ編集リストへ追加（開始秒順に挿入し、ハイライトしてスクロール）
  const addSingleSong = (newSong: EditableSong) => {
    setEditableSongs((prev) => [...prev, newSong].sort((a, b) => a.start - b.start));
    showToast(`「${newSong.name}」を追加しました`, 'success');
    setTimeout(() => {
      setHighlightedSongId(newSong.id);
      document.getElementById(`song-${newSong.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      setTimeout(() => setHighlightedSongId(null), 3000);
    }, 100);
  };

  // Holodex タブ：1曲追加
  const addSuggestionSong = async (song: SongSuggestion) => {
    addSingleSong(await suggestionToEditableSong(song, `holodex-add-${Date.now()}`, getDefaultSingerIds()));
  };

  // コメントタブ：1曲追加
  const addCommentSongToList = async (song: CommentSong) => {
    addSingleSong(await commentSongToEditableSong(song, `comment-add-${Date.now()}`, getDefaultSingerIds()));
  };

  // 自動採用に届かなかった候補（0.50〜0.85）を人が確定させる。
  //
  // ここは「アーティストが書かれていないので曲名だけでは決めきれない」
  // 「feat. や CV 名の表記が違う」といった、文字列では原理的に決まらない組が来る。
  // 確定は別表記として学習されるので、同じ表記は次から自動で当たる。
  const confirmMatchMutation = useMutation({
    mutationFn: ({ index, name, songId }: { index: number; name: string; songId: string }) =>
      commentApi.confirmMatch(id!, index, name, songId),
    onSuccess: (updated, { index }) => {
      // 別名義を学習したうえで照合し直した結果が返る。stream を読み直しても
      // 照合は付いてこない（読み取り時には引かない）ので、その場で差し替える。
      setCommentTimelineSongs((prev) => prev.map((s, i) => (i === index ? updated : s)));
      showToast(`「${updated.matched_song_name}」に結びつけました（次から自動で当たります）`, 'success');
    },
    onError: (err: Error) => showToast(`確定に失敗しました: ${err.message}`, 'error'),
  });

  // 生コメントタブ：タイムスタンプ行から1曲追加。chat 拍手で終了時間を推定してから挿入する
  const addFromRawComment = async ({ start, name, artist }: { start: number; name: string; artist: string }) => {
    let end = 0;
    let chatEnd: number | undefined;
    try {
      const { ends } = await commentApi.estimateChatEnds(id!, [start]);
      if (ends[String(start)]) {
        end = ends[String(start)];
        chatEnd = end;
      }
    } catch {
      /* 推定失敗時は end 未設定のまま（手動で設定） */
    }
    addSingleSong({
      id: `raw-add-${Date.now()}`,
      name: name || '(曲名未入力)',
      nameReading: '',
      artist,
      artistReading: '',
      start,
      end,
      tags: [],
      singerIds: getDefaultSingerIds(),
      matchedSongId: null,
      artUrl: null,
      itunesId: null,
      trackDuration: null,
      originalName: name,
      originalArtist: artist,
      isEndTimeEstimated: false,
      chatEnd,
      endSource: chatEnd !== undefined ? 'chat' : undefined,
      customTags: [],
    });
  };

  // 自動読み込み：Holodex → コメント の優先順。どちらも正規化＋chat 比較込み
  const autoLoad = async () => {
    if (holodexTimelineSongs.length > 0) {
      await loadFromHolodex(false);
    } else if (stream?.has_comment_raw) {
      await loadFromComments(false);
    } else {
      showToast('読み込めるデータがありません（Holodex から同期するか、コメントを取得してください）', 'info');
    }
  };

  const toggleEditing = () => {
    if (isEditing) {
      // 關閉編輯模式
      setIsEditing(false);
      setEditableSongs([]);
    } else {
      // 開啟編輯模式，自動載入現有セトリ
      if (stream) {
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
            // 現有資料沒有 AI 修改的標記
            aiNormalizedName: undefined,
            aiNormalizedArtist: undefined,
            isEndTimeEstimated: false,
            // 保存済みの由来を読み戻す。unknown は「記録開始前」なので
            // 由来なし扱いにして、推測し直さない（推測すると嘘の由来が付く）。
            endSource: perf.end_source && perf.end_source !== 'unknown' ? perf.end_source : undefined,
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

      // 手動修改結束時間時，清除 chat 比較資訊並標記來源
      if (field === 'end') {
        updated[index].chatEnd = undefined;
        updated[index].endDiff = undefined;
        updated[index].endSource = 'manual';
      }

      return updated;
    });
  };

  // 套用特定來源的結束時間
  const applyEndSource = (index: number, source: 'chat' | 'comment', newEnd?: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      const s = updated[index];

      if (source === 'chat' && s.chatEnd !== undefined) {
        updated[index] = {
          ...s,
          end: s.chatEnd,
          endSource: 'chat',
          isEndTimeEstimated: false,
        };
      } else if (source === 'comment' && s.originalCommentEnd !== undefined) {
        updated[index] = {
          ...s,
          end: s.originalCommentEnd,
          endSource: 'comment',
          isEndTimeEstimated: false,
          chatEnd: undefined, // 清除比較狀態
          endDiff: undefined,
        };
      } else if (newEnd !== undefined) {
        updated[index] = { ...s, end: newEnd, endSource: source };
      }
      return updated;
    });
  };

  // 選擇搜尋到的歌曲
  const handleSelectExistingSong = async (index: number, song: Song) => {
    const selectedItunesId = song.itunes_ids && song.itunes_ids.length > 0 ? Number(song.itunes_ids[0].itunes_id) : null;
    const selectedTrackDuration = selectedItunesId ? await fetchTrackDurationByItunesId(selectedItunesId) : null;

    setEditableSongs((prev) => {
      const updated = [...prev];
      const current = updated[index];
      // 選んだ曲が iTunes ID を持たない場合は、行に載っている ID（Holodex 由来）を引き継ぐ。
      // これにより保存時に song_itunes へ紐付けが作られ、次回以降は iTunes ID で自動マッチする。
      // mergeDuplicateSongs と同じ ?? 規約。誤って引き継いだ場合は行の iTunes チップから解除できる。
      const itunesId = selectedItunesId ?? current.itunesId;
      // DB 楽曲（song.id あり）の ID は登録済み。純 iTunes 検索結果（id 空）は未登録なので保存時に紐付く
      const itunesFromDb = selectedItunesId != null ? !!song.id : current.itunesFromDb;
      // trackDuration は採用した ID と対応させる（取得失敗時に別 ID の値を流用しない）
      const trackDuration = selectedItunesId != null ? selectedTrackDuration : current.trackDuration;

      // 檢查是否是從 iTunes 選擇的（id 為空）
      if (!song.id) {
        // 從 iTunes 選擇：填入基本資訊和 iTunes ID
        updated[index] = {
          ...updated[index],
          name: song.name,
          artist: song.original_artist,
          artUrl: song.arts || null,
          itunesId,
          itunesFromDb,
          trackDuration,
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
          itunesId,
          itunesFromDb,
          trackDuration,
          matchedSongId: song.id,
          originalName: song.name,
          originalArtist: song.original_artist,
        };
      }
      
      return updated;
    });
  };

  // iTunes ID の紐付けを解除（誤った ID が song_itunes に焼き付くのを防ぐ）。
  // trackDuration も対応が崩れるため一緒に消す。
  const clearItunesId = (index: number) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      updated[index] = {
        ...updated[index],
        itunesId: null,
        itunesFromDb: undefined,
        trackDuration: null,
      };
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

  // seTORI timeline クリック → 対応する曲を選択して詳細カードを展開＋スクロール（編集モード用）
  const scrollToEditableSong = (start: number) => {
    if (editableSongs.length === 0) return;
    const match = editableSongs.reduce((best, song) =>
      Math.abs(song.start - start) < Math.abs(best.start - start) ? song : best
    );
    if (Math.abs(match.start - start) <= 30) {
      const idx = editableSongs.findIndex((s) => s.id === match.id);
      if (idx >= 0) selectSong(idx, false); // timeline クリックはプレイヤー側が既にシークするため seek しない
      setHighlightedSongId(match.id);
      setTimeout(() => {
        document.getElementById(`song-${match.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }, 50);
      setTimeout(() => setHighlightedSongId(null), 3000);
    }
  };

  const handleConfirm = async () => {
    // 配信情報（タグ・参加者・非表示など）は閲覧モードのクイック編集で即時保存されるため、
    // ここではセットリストのみ保存する。

    // 如果沒有歌曲，刪除所有 performance
    if (editableSongs.length === 0) {
      try {
        await performanceApi.deleteAll(id!);
        showToast('セットリストを削除しました', 'success');
        setIsEditing(false);
        setEditableSongs([]);
        queryClient.invalidateQueries({ queryKey: ['stream', id] });
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        showToast(`削除エラー: ${message}`, 'error');
      }
      return;
    }

    // 終了時間のバリデーション
    const missingEndTime = editableSongs.filter(s => s.end === 0);
    if (missingEndTime.length > 0) {
      showToast(`終了時間が未設定の曲が${missingEndTime.length}件あります`, 'error');
      // 最初の未設定曲を選択（詳細カードを展開）してスクロール
      const firstMissing = missingEndTime[0];
      const idx = editableSongs.findIndex((s) => s.id === firstMissing.id);
      if (idx >= 0) selectSong(idx, false);
      setTimeout(() => {
        document.getElementById(`song-${firstMissing.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }, 50);
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
      // 終了時間の由来。編集中ずっと追跡している値をそのまま送る
      // （送らないと保存時に失われ、自動生成と人手確認の区別が付かなくなる）。
      // 保存＝人が見たとみなすので end_confirmed は既定の true に任せる。
      end_source: song.endSource,
    }));
    createPerformancesMutation.mutate(performances);
  };

  // YouTube 播放器實例（必須在任何條件判斷之前）
  const playerInstanceRef = useRef<YouTubePlayerInstance | null>(null);

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

  // タイムラインは分析後の state ではなく、stream に保存された Holodex 原文を使う。
  const holodexTimeline = (stream.holodex_timeline_songs || []).map((song, index) => {
    const end = song.end_seconds > 0 ? song.end_seconds : song.start_seconds;
    return {
      id: `holodex-${index}`,
      start: song.start_seconds,
      end,
      label: song.name,
      artist: song.original_artist || '',
    };
  });

  const rawCommentTimeline = isEditing
    ? extractRawCommentTimestamps(rawCommentsData?.comments || [])
    : [];

  const timelineDuration = Math.max(
    stream.duration_seconds || 0,
    ...setoriTimeline.map((s) => s.end),
    ...(isEditing ? holodexTimeline.map((s) => s.end) : []),
    ...rawCommentTimeline.map((s) => s.start),
    1,
  );

  const getTimelineLeft = (start: number) => (start / timelineDuration) * 100;
  const getTimelineWidth = (start: number, end: number) =>
    Math.max(((end - start) / timelineDuration) * 100, 0.4);
  const getTooltipAlignClass = (startSeconds: number) => {
    const leftPercent = getTimelineLeft(startSeconds);
    if (leftPercent < 15) return 'left-0';
    if (leftPercent > 85) return 'right-0';
    return 'left-1/2 -translate-x-1/2';
  };

  // セットリスト→再生キュー用トラック（連続再生・キュー追加で共用）
  const performanceTracks = () =>
    stream.performances.map((perf) => ({
      performanceId: perf.id,
      streamId: perf.stream_id,
      songId: perf.song_id,
      songName: perf.song_name,
      artist: perf.original_artist,
      artists: perf.artists ?? [],
      artUrl: perf.arts,
      singers: perf.singers?.map((s) => ({ id: s.id, name: s.name })) ?? [],
      streamTitle: stream.title,
      streamDate: stream.stream_date,
      start: perf.start_seconds,
      end: perf.end_seconds,
    }));

  // 閲覧モードのクイック編集：タグ/参加者/非表示を編集モードを開かずに保存
  const quickSaveStream = async (patch: UpdateStreamRequest) => {
    try {
      await updateStreamMutation.mutateAsync(patch);
      showToast('更新しました', 'success');
    } catch {
      /* onError 側でトースト表示済み */
    }
  };

  return (
    <>
      <div className="flex flex-col min-[1300px]:flex-row gap-6 w-full h-full min-h-0 overflow-hidden">
      {/* Left Column - Stream Info + YouTube Player */}
      {/* モバイル（<sm）は情報カードとプレイヤーを縦積みにし高さ制限も外す。sm〜1300px は左右並び+40vh 制限 */}
      <div className="w-full min-[1300px]:basis-2/5 min-[1300px]:shrink-0 min-[1300px]:self-stretch flex flex-col sm:flex-row min-[1300px]:grid min-[1300px]:grid-rows-[2fr_3fr] gap-4 min-h-0 min-[1300px]:overflow-hidden shrink-0 max-h-none sm:max-h-[40vh] min-[1300px]:max-h-none">
        {/* Stream Header - 40% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border overflow-y-auto min-h-0">
          {isEditing ? (
            /* 編集モード：情報カードの代わりにデータ読み込みタブ */
            <div className="flex flex-col min-h-0">
              <div className="flex border-b shrink-0 sticky top-0 bg-white z-10">
                {([
                  { key: 'actions', label: '操作' },
                  { key: 'holodex', label: 'Holodex' },
                  { key: 'comment', label: 'コメント' },
                  { key: 'raw', label: '生コメント' },
                ] as const).map((t) => (
                  <button
                    key={t.key}
                    onClick={() => setEditTab(t.key)}
                    className={`px-3.5 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors ${
                      editTab === t.key
                        ? 'border-indigo-600 text-indigo-600'
                        : 'border-transparent text-gray-500 hover:text-gray-700'
                    }`}
                  >
                    {t.label}
                  </button>
                ))}
              </div>

              <div className="p-4">
                {editTab === 'actions' && (
                  <div className="space-y-4">
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">読み込み</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={autoLoad}
                          disabled={holodexAnalyzeLoading || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                          title="Holodex → コメント の優先順で読み込み、正規化と chat 時間チェックまで実行"
                        >
                          {holodexAnalyzeLoading || commentAnalyzeLoading ? '読み込み中...' : '自動読み込み'}
                        </button>
                        <button
                          onClick={() => loadFromHolodex(false)}
                          disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {holodexAnalyzeLoading ? 'Holodex分析中...' : 'Holodex データ'}
                        </button>
                        <button
                          onClick={() => loadFromComments(false)}
                          disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {commentAnalyzeLoading ? 'コメント分析中...' : 'コメント データ'}
                        </button>
                        <button
                          onClick={runAINormalization}
                          disabled={editableSongs.length === 0 || aiNormalizeMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {aiNormalizeMutation.isPending ? 'AI処理中...' : 'AI正規化'}
                        </button>
                      </div>
                    </div>
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">コメント同期</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={() => syncYouTubeCommentsMutation.mutate()}
                          disabled={syncYouTubeCommentsMutation.isPending || commentAnalyzeLoading}
                          title="YouTube Data API から公開トップレベルコメントを取得し直します"
                          className="px-3 py-1.5 text-sm bg-red-50 text-red-700 border border-red-200 font-medium rounded-lg hover:bg-red-100 transition-colors disabled:opacity-50"
                        >
                          {syncYouTubeCommentsMutation.isPending ? 'YouTubeから同期中...' : 'YouTubeからコメント同期'}
                        </button>
                      </div>
                    </div>
                    <div>
                      <p className="text-xs font-medium text-gray-400 mb-1.5">Holodex 同期</p>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={() => syncVideoMutation.mutate()}
                          disabled={syncVideoMutation.isPending}
                          className="px-3 py-1.5 text-sm bg-indigo-50 text-indigo-700 border border-indigo-200 font-medium rounded-lg hover:bg-indigo-100 transition-colors disabled:opacity-50"
                        >
                          {syncVideoMutation.isPending ? '同期中...' : 'Holodex から同期'}
                        </button>
                        <button
                          onClick={() => syncToHolodexMutation.mutate()}
                          disabled={syncToHolodexMutation.isPending}
                          title="seTORI のセットリストを Holodex に書き込みます（外部サービスへの反映）"
                          className="px-3 py-1.5 text-sm bg-amber-50 text-amber-700 border border-amber-300 font-medium rounded-lg hover:bg-amber-100 transition-colors disabled:opacity-50"
                        >
                          {syncToHolodexMutation.isPending ? 'Holodex へ同期中...' : 'seTORI から Holodex へ同期'}
                        </button>
                      </div>
                    </div>
                  </div>
                )}

                {editTab === 'holodex' && (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={() => loadFromHolodex(false)}
                        disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                        className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                        title="全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
                      >
                        {holodexAnalyzeLoading ? '分析中...' : '全部読み込む'}
                      </button>
                      <button
                        onClick={() => loadFromHolodex(true)}
                        disabled={holodexTimelineSongs.length === 0 || holodexAnalyzeLoading}
                        title="キャッシュを無視して AI で再分析・再正規化します"
                        className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        再分析
                      </button>
                    </div>
                    {holodexTimelineSongs.length === 0 ? (
                      <p className="text-sm text-gray-400 py-2">Holodex データがありません</p>
                    ) : (
                      <div className="space-y-0.5">
                        {holodexTimelineSongs.map((song, i) => (
                          <div
                            key={i}
                            onClick={() => addSuggestionSong(song)}
                            className="flex items-baseline gap-2 px-2 py-1.5 rounded hover:bg-indigo-50 cursor-pointer group text-sm"
                            title="クリックで追加"
                          >
                            <span className="shrink-0 flex items-baseline gap-0.5">
                              <button
                                onClick={(e) => {
                                  e.stopPropagation();
                                  youtubePlayerSeekTo(song.start_seconds);
                                }}
                                className="px-1.5 rounded bg-blue-50 text-blue-700 font-mono text-xs hover:bg-blue-100 transition-colors"
                                title="開始時間にジャンプ"
                              >
                                {formatTime(song.start_seconds)}
                              </button>
                              {song.end_seconds > 0 && (
                                <>
                                  <span className="text-gray-300 text-xs">〜</span>
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      youtubePlayerSeekTo(song.end_seconds);
                                    }}
                                    className="px-1.5 rounded bg-blue-50/60 text-blue-600 font-mono text-xs hover:bg-blue-100 transition-colors"
                                    title="終了時間にジャンプ"
                                  >
                                    {formatTime(song.end_seconds)}
                                  </button>
                                </>
                              )}
                            </span>
                            <span className="min-w-0 flex-1 truncate">
                              <span className="text-gray-900 font-medium">{song.name}</span>
                              {song.original_artist && <span className="text-gray-500"> / {song.original_artist}</span>}
                            </span>
                            <span className="shrink-0 text-gray-300 group-hover:text-indigo-600 transition-colors">＋</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}

                {editTab === 'comment' && (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={() => loadFromComments(false)}
                        disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                        className="flex-1 px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                        title="分析済みの全曲を編集リストへ読み込む（正規化＋chat 時間チェック込み）"
                      >
                        {commentAnalyzeLoading ? '分析中...' : '全部読み込む'}
                      </button>
                      <button
                        onClick={() => loadFromComments(true)}
                        disabled={!stream?.has_comment_raw || commentAnalyzeLoading || syncYouTubeCommentsMutation.isPending}
                        title="キャッシュを無視して AI で再分析・再正規化します"
                        className="px-3 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 font-medium rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        再分析
                      </button>
                      <button
                        onClick={() => chatEndMutation.mutate()}
                        disabled={chatEndMutation.isPending || commentAnalyzeLoading}
                        title="live chat の拍手から終了時間だけを取り直します（AI は使いません。live chat のダウンロードで数十秒かかることがあります）"
                        className="px-2 py-1.5 text-sm bg-white text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-50"
                      >
                        {chatEndMutation.isPending ? (
                          <svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                          </svg>
                        ) : (
                          /* 拍手＝手のアイコン */
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={1.8} viewBox="0 0 24 24">
                            <path
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              d="M7 11V6a1.5 1.5 0 013 0v4m0-4.5a1.5 1.5 0 013 0V10m0-3.5a1.5 1.5 0 013 0V13m0-2.5a1.5 1.5 0 013 0V15a6 6 0 01-6 6h-2a6 6 0 01-5.2-3L4 14.5a1.5 1.5 0 012.6-1.5L7 14"
                            />
                          </svg>
                        )}
                      </button>
                    </div>
                    {/* 最後に解析した時刻。プロンプトや抽出規則を変えたあと、
                        この配信がまだ古い規則のままかを判断する手がかりになる。
                        stream.updated_at は Holodex 同期でも動くので使えない。 */}
                    {stream.comment_songs_analyzed_at && (
                      <p className="px-1 text-xs text-gray-400">
                        最終解析:{' '}
                        {new Date(stream.comment_songs_analyzed_at).toLocaleString('ja-JP', {
                          year: 'numeric', month: '2-digit', day: '2-digit',
                          hour: '2-digit', minute: '2-digit', hour12: false,
                        })}
                      </p>
                    )}
                    {commentTimelineSongs.length === 0 ? (
                      <p className="text-sm text-gray-400 py-2">
                        分析済みの曲がありません（「全部読み込む」で分析を実行）
                      </p>
                    ) : (
                      <div className="space-y-0.5">
                        {commentTimelineSongs.map((song, i) => (
                          <div key={i}>
                          <div
                            onClick={() => addCommentSongToList(song)}
                            className="flex items-baseline gap-2 px-2 py-1.5 rounded hover:bg-indigo-50 cursor-pointer group text-sm"
                            title="クリックで追加"
                          >
                            <span className="shrink-0 flex items-baseline gap-0.5">
                              <button
                                onClick={(e) => {
                                  e.stopPropagation();
                                  youtubePlayerSeekTo(song.start);
                                }}
                                className="px-1.5 rounded bg-orange-50 text-orange-700 font-mono text-xs hover:bg-orange-100 transition-colors"
                                title="開始時間にジャンプ"
                              >
                                {formatTime(song.start)}
                              </button>
                              {song.end > 0 && (
                                <>
                                  <span className="text-gray-300 text-xs">〜</span>
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      youtubePlayerSeekTo(song.end);
                                    }}
                                    className="px-1.5 rounded bg-orange-50/60 text-orange-600 font-mono text-xs hover:bg-orange-100 transition-colors"
                                    title="終了時間にジャンプ"
                                  >
                                    {formatTime(song.end)}
                                  </button>
                                </>
                              )}
                            </span>
                            <span className="min-w-0 flex-1 truncate">
                              <span className="text-gray-900 font-medium">{song.name}</span>
                              {song.original_artist && <span className="text-gray-500"> / {song.original_artist}</span>}
                              {song.matched_song_id && song.matched_song_name && (
                                <span className="ml-1 text-xs text-emerald-600" title="DB の楽曲に照合済み">
                                  → {song.matched_song_name}
                                </span>
                              )}
                            </span>
                            <span className="shrink-0 text-gray-300 group-hover:text-indigo-600 transition-colors">＋</span>
                          </div>

                          {/*
                            照合が決めきれなかった候補。songmatch が 0.50〜0.85 で返したもので、
                            「アーティストが書かれていないので曲名だけでは決めきれない」
                            「feat. や CV 名の表記が違う」といった、文字列では決まらない組が来る。
                            自動採用させると同名異曲（オレンジ）を巻き込むので、人が選ぶ。
                          */}
                          {canEdit && !song.matched_song_id && (song.match_candidates?.length ?? 0) > 0 && (
                            <div className="ml-2 mb-1 flex flex-wrap items-center gap-1 pl-14">
                              <span className="shrink-0 text-[11px] text-gray-400">候補</span>
                              {song.match_candidates!.map((c) => (
                                <button
                                  key={c.song_id}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    confirmMatchMutation.mutate({ index: i, name: song.name, songId: c.song_id });
                                  }}
                                  disabled={confirmMatchMutation.isPending}
                                  title={`${matchReasonLabel(c.reason)}（確信度 ${Math.round(c.score * 100)}%）\nこれに決めると、同じ表記は次から自動で当たります`}
                                  className="max-w-full truncate rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs text-amber-900 transition-colors hover:border-amber-400 hover:bg-amber-100 disabled:opacity-50"
                                >
                                  {c.name}
                                  {c.artist && <span className="text-amber-700/70"> / {c.artist}</span>}
                                  <span className="ml-1 text-amber-600/60">{Math.round(c.score * 100)}%</span>
                                </button>
                              ))}
                            </div>
                          )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}

                {editTab === 'raw' && (
                  <RawCommentsPanel
                    videoId={stream.id}
                    onSeek={(secs) => youtubePlayerSeekTo(secs)}
                    onAddSong={addFromRawComment}
                  />
                )}
              </div>
            </div>
          ) : (
            <div className="p-6">
              <>
                <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
                {/* 日時 + YouTube リンク */}
                <p className="text-gray-500 mt-2 flex items-center gap-2">
                  {new Date(stream.stream_date).toLocaleString('ja-JP', {
                    year: 'numeric',
                    month: '2-digit',
                    day: '2-digit',
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                    hour12: false
                  })}
                  <a
                    href={youtubeUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center justify-center w-7 h-7 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                    title="YouTubeで見る"
                  >
                    <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                      <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                    </svg>
                  </a>
                </p>

                {/* Tags + 編集アイコン */}
                <div className="flex flex-wrap items-center gap-2 mt-4">
                  {stream.tags.map((tag) => (
                    <Tag key={tag.id} label={tag.display_name} color={tag.color} size="md" />
                  ))}
                  {canEdit && (
                    <button
                      onClick={() => setTagPickerOpen(!tagPickerOpen)}
                      className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                        tagPickerOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
                      }`}
                      title="タグを編集"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                    </button>
                  )}
                </div>
                {canEdit && tagPickerOpen && (
                  <div className="flex flex-wrap gap-2 mt-2 p-3 bg-gray-50 rounded-lg border">
                    {STREAM_TAGS.map((tag) => {
                      const ids = stream.tags.map((t) => t.id);
                      const active = ids.includes(tag.id);
                      return (
                        <button
                          key={tag.id}
                          onClick={() =>
                            quickSaveStream({
                              tag_ids: active ? ids.filter((i) => i !== tag.id) : [...ids, tag.id],
                            })
                          }
                          className={`px-3 py-1 rounded-full text-sm font-medium transition-colors ${
                            active ? 'text-white' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                          }`}
                          style={active ? { backgroundColor: tag.color } : {}}
                        >
                          {tag.label}
                        </button>
                      );
                    })}
                  </div>
                )}

                {/* Participants + 編集アイコン */}
                <div className="flex flex-wrap items-center gap-2 mt-4">
                  {stream.participants?.map((singer) => (
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
                      {canEdit && participantAddOpen && (
                        <button
                          onClick={() =>
                            quickSaveStream({
                              participant_ids: (stream.participants ?? [])
                                .map((p) => p.id)
                                .filter((pid) => pid !== singer.id),
                            })
                          }
                          className="text-gray-400 hover:text-red-600 transition-colors"
                          title="参加者から外す"
                        >
                          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                          </svg>
                        </button>
                      )}
                    </div>
                  ))}
                  {canEdit && (
                    <button
                      onClick={() => setParticipantAddOpen(!participantAddOpen)}
                      className={`inline-flex items-center justify-center w-7 h-7 rounded-full transition-colors ${
                        participantAddOpen ? 'text-indigo-600 bg-indigo-50' : 'text-gray-400 hover:text-indigo-600 hover:bg-indigo-50'
                      }`}
                      title="参加者を編集"
                      aria-label="参加者を編集"
                    >
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                    </button>
                  )}
                </div>
                {canEdit && participantAddOpen && (
                  <div className="mt-2">
                    <SingerSearchInput
                      excludeIds={stream.participants?.map((p) => p.id) ?? []}
                      onSelectSinger={(singer) =>
                        quickSaveStream({
                          participant_ids: [...(stream.participants?.map((p) => p.id) ?? []), singer.id],
                        })
                      }
                      placeholder="チャンネル名を入力して参加者を追加..."
                    />
                  </div>
                )}

                {/* 非表示 */}
                {canEdit && (
                  <label className="mt-4 flex items-center gap-2 cursor-pointer w-fit">
                    <input
                      type="checkbox"
                      checked={stream.is_hidden}
                      onChange={(e) => quickSaveStream({ is_hidden: e.target.checked })}
                      className="w-4 h-4 text-red-600 border-gray-300 rounded focus:ring-red-500"
                    />
                    <span className="text-sm font-medium text-gray-700">非表示</span>
                  </label>
                )}
              </>
            </div>
          )}
        </div>

        {/* Player + Timeline - 60% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border flex flex-col min-h-0">
          <div className="bg-black flex-1 min-h-[200px] sm:min-h-[280px] flex items-center overflow-hidden">
            <YoutubePlayer
              videoId={stream.id}
              onReady={(player) => {
                playerInstanceRef.current = player;
              }}
            />
          </div>

          <div className="border-t py-3 px-0 shrink-0">
            {/* YouTube の進捗バーと左右端を揃える（デスクトップ UI は左側の余白が少し広い）。 */}
            <div className="space-y-1 px-3 sm:pl-9 sm:pr-8">
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
                    <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
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
              {isEditing && (
                <>
                  <div className="relative h-3 bg-blue-50 rounded-none">
                    {holodexTimeline.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => youtubePlayerSeekTo(item.start)}
                        className="absolute top-0 h-full rounded bg-blue-500/70 hover:bg-blue-600 transition-colors group"
                        style={{
                          left: `${getTimelineLeft(item.start)}%`,
                          width: `${getTimelineWidth(item.start, item.end)}%`,
                        }}
                      >
                        <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
                          <div className="bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg whitespace-nowrap">
                            <div className="font-semibold">{item.label}</div>
                            {item.artist && <div className="text-gray-300">{item.artist}</div>}
                            <div className="text-gray-400 mt-1">
                              {formatTime(item.start)} - {formatTime(item.end)}
                            </div>
                            <div className="text-blue-400 text-[10px] mt-1">Holodex（未処理）</div>
                          </div>
                        </div>
                      </button>
                    ))}
                  </div>
                  <div className="relative h-3 bg-orange-50 rounded-none">
                    {rawCommentTimeline.map((item) => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => youtubePlayerSeekTo(item.start)}
                        className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-2 h-2 rounded-full bg-orange-500 hover:bg-orange-600 transition-colors group"
                        style={{ left: `${getTimelineLeft(item.start)}%` }}
                      >
                        <div className={`absolute bottom-full ${getTooltipAlignClass(item.start)} mb-2 hidden group-hover:block z-50 pointer-events-none`}>
                          <div className="w-max max-w-80 bg-gray-900 text-white text-xs rounded-lg p-2 shadow-lg text-left whitespace-normal">
                            <div className="font-mono text-gray-300">{formatTime(item.start)}</div>
                            <div className="mt-1 break-words">{item.label}</div>
                            <div className="text-orange-400 text-[10px] mt-1">Raw comment（未処理）</div>
                          </div>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Right Column - Setlist */}
      <div className="w-full min-[1300px]:basis-3/5 min-[1300px]:shrink-0 min-w-0 min-h-0 min-[1300px]:self-stretch min-[1300px]:pr-6 flex flex-col">
        {/* Setlist Section - Unified View */}
        <div className="flex flex-col gap-3 mb-4 flex-none shrink-0">
          <div className="flex items-center gap-3">
            <h2 className="text-2xl font-bold text-gray-900">
              セットリスト ({isEditing ? editableSongs.length : stream.performances.length}曲)
            </h2>
            {!isEditing && (
              <div className="flex items-center gap-1.5">
                {canEdit && (
                  <button
                    onClick={toggleEditing}
                    className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-500 border border-gray-300 hover:bg-gray-100 transition-colors"
                    title="セットリストを編集"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                    </svg>
                  </button>
                )}
                {stream.performances.length > 0 && (
                  <>
                    <button
                      onClick={() => usePlayerStore.getState().playTracks(performanceTracks(), 0)}
                      className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                      title="セットリストを連続再生"
                    >
                      <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z" /></svg>
                    </button>
                    <button
                      onClick={() => {
                        usePlayerStore.getState().enqueue(performanceTracks());
                        showToast('セットリストをキューに追加しました', 'success');
                      }}
                      className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-indigo-600 hover:bg-indigo-50 transition-colors"
                      title="セットリストをキューに追加"
                    >
                      <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                        <path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" />
                      </svg>
                    </button>
                  </>
                )}
              </div>
            )}
            {isEditing && (
              /* 編集コントロールはタイトルと同じ行の右側に寄せる。処理完了は保存の直前に置く（即時保存） */
              <div className="ml-auto flex items-center gap-2 shrink-0">
                <button
                  onClick={toggleEditing}
                  className="px-3 py-1.5 text-sm bg-gray-200 text-gray-700 font-medium rounded-lg hover:bg-gray-300 transition-colors"
                >
                  キャンセル
                </button>
                {editableSongs.length > 0 && (
                  <span
                    className={`text-xs font-medium px-2 py-1 rounded ${
                      confirmedCount === editableSongs.length
                        ? 'bg-green-100 text-green-700'
                        : 'bg-gray-100 text-gray-500'
                    }`}
                    title="確認済みの曲数（保存は確認状態に関係なく全曲行われます）"
                  >
                    ✓ {confirmedCount}/{editableSongs.length}
                  </span>
                )}
                <label className="flex items-center gap-1.5 cursor-pointer text-sm font-medium text-gray-700 whitespace-nowrap">
                  <input
                    type="checkbox"
                    checked={stream.is_processed}
                    onChange={(e) => quickSaveStream({ is_processed: e.target.checked })}
                    className="w-4 h-4 text-green-600 border-gray-300 rounded focus:ring-green-500"
                  />
                  処理完了
                </label>
                <button
                  onClick={handleConfirm}
                  disabled={createPerformancesMutation.isPending || updateStreamMutation.isPending}
                  className="px-3 py-1.5 text-sm bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {createPerformancesMutation.isPending || updateStreamMutation.isPending ? '処理中...' : '変更を保存'}
                </button>
              </div>
            )}
          </div>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
        {isEditing ? (
          /* Editable Setlist */
          <div className="bg-white rounded-lg shadow-sm border p-6">
            {/* Editable songs list will default to channel owner as vocalist */}

            <div className="space-y-2">
              {editableSongs.length > 0 && editableSongs.map((song, index) => (
                index !== selectedSongIndex ? (
                  /* 圧縮行：クリックで詳細カードを展開＋プレイヤーがその曲へジャンプ */
                  <button
                    key={song.id}
                    id={`song-${song.id}`}
                    onClick={() => selectSong(index)}
                    className={`w-full flex items-center gap-3 px-3 py-2 border rounded-lg text-left transition-colors ${
                      highlightedSongId === song.id
                        ? 'bg-yellow-100 border-yellow-400'
                        : song.confirmed
                          ? 'bg-white border-gray-200 hover:border-indigo-300'
                          : 'bg-gray-50 border-gray-200 hover:border-indigo-300'
                    }`}
                  >
                    {/* 確認状態 */}
                    <span className="shrink-0" title={song.confirmed ? '確認済み' : song.end > 0 ? '未確認' : '終了時間なし'}>
                      {song.confirmed ? (
                        <svg className="w-5 h-5 text-green-500" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                        </svg>
                      ) : song.end > 0 ? (
                        <span className="block w-5 h-5 rounded-full border-2 border-gray-300" />
                      ) : (
                        <span className="block w-5 h-5 rounded-full border-2 border-red-400 bg-red-50" />
                      )}
                    </span>
                    <span className="text-xs font-mono text-gray-400 w-6 text-right shrink-0">{index + 1}</span>
                    {song.artUrl ? (
                      <img src={song.artUrl} alt="" className="w-8 h-8 object-cover rounded shrink-0" />
                    ) : (
                      <span className="w-8 h-8 bg-gray-200 rounded shrink-0" />
                    )}
                    <span className="flex-1 min-w-0">
                      <span className="block text-sm font-medium text-gray-900 truncate">
                        {song.name || <span className="text-gray-400">（曲名未入力）</span>}
                      </span>
                      <span className="block text-xs text-gray-500 truncate">{song.artist}</span>
                    </span>
                    <span className="text-xs font-mono text-gray-500 shrink-0">
                      {formatTimeInput(song.start)} - {song.end > 0 ? formatTimeInput(song.end) : '--:--'}
                    </span>
                    {song.itunesId && !song.itunesFromDb && (
                      <span
                        className="px-1.5 py-0.5 bg-amber-100 text-amber-800 text-[10px] font-medium rounded shrink-0"
                        title={`iTunes ID ${song.itunesId} を保存時にこの楽曲へ紐付けます`}
                      >
                        iTunes＋
                      </span>
                    )}
                    {!song.matchedSongId && (
                      <span className="px-1.5 py-0.5 bg-green-100 text-green-700 text-[10px] font-medium rounded shrink-0">New</span>
                    )}
                  </button>
                ) : (
                  <div key={song.id} id={`song-${song.id}`} className={`border-2 rounded-lg p-4 transition-colors duration-500 ${highlightedSongId === song.id ? 'bg-yellow-100 border-yellow-400' : 'bg-white border-indigo-300 shadow-sm'}`}>
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
                            {/* iTunes ID：未登録（amber）は保存時に song_itunes へ紐付けが作られる。
                                primary な ID は Holodex へのアップロードにも使われるため誤りは外部に伝播する */}
                            {song.itunesId && (
                              <span
                                className={`inline-flex items-center gap-1 px-2 py-0.5 text-xs font-mono rounded ${
                                  song.itunesFromDb
                                    ? 'bg-gray-100 text-gray-600'
                                    : 'bg-amber-100 text-amber-800'
                                }`}
                                title={
                                  song.itunesFromDb
                                    ? `iTunes ID ${song.itunesId}（登録済み）`
                                    : `iTunes ID ${song.itunesId}（未登録：保存時にこの楽曲へ紐付けます）`
                                }
                              >
                                iTunes: {song.itunesId}
                                {!song.itunesFromDb && <span aria-hidden="true">＋</span>}
                                <button
                                  onClick={() => clearItunesId(index)}
                                  className="ml-0.5 text-gray-400 hover:text-red-600"
                                  title="この iTunes ID の紐付けを外す"
                                >
                                  ×
                                </button>
                              </span>
                            )}
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
                        {/* 由来（元の値 → どの処理 → 今の値）。changes が無い古い経路は従来表示 */}
                        {song.changes?.length ? (
                          <FieldProvenance changes={song.changes} field="name" />
                        ) : song.aiNormalizedName ? (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{song.aiNormalizedName}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{song.name}</span>
                          </div>
                        ) : null}
                        {/* 合併元顯示 */}
                        {song.mergedFrom && song.mergedFrom.length > 0 && (
                          <div className="mt-1 text-sm">
                            <span className="text-orange-600">統合:</span>{' '}
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
                        {/* 由来（元の値 → どの処理 → 今の値）。changes が無い古い経路は従来表示 */}
                        {song.changes?.length ? (
                          <FieldProvenance changes={song.changes} field="artist" />
                        ) : song.aiNormalizedArtist ? (
                          <div className="mt-1 text-sm">
                            <span className="text-gray-500">AI修正:</span>{' '}
                            <span className="line-through text-gray-400">{song.aiNormalizedArtist}</span>
                            {' → '}
                            <span className="text-blue-600 font-medium">{song.artist}</span>
                          </div>
                        ) : null}
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
                            className="px-3 py-2 bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
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
                        {/* ±6秒微調整（離すと確定して試聴） */}
                        <TimestampTweaker
                          value={song.start}
                          mode="start"
                          currentTime={currentPlayerTime}
                          onChange={(v) => handleSongChange(index, 'start', v)}
                        />
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
                                ? 'bg-indigo-100 text-indigo-700 hover:bg-indigo-200'
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
                            className="px-3 py-2 bg-indigo-100 text-indigo-700 rounded-lg hover:bg-indigo-200 transition-colors font-mono text-sm whitespace-nowrap min-w-[5.5rem]"
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
                        {/* ±6秒微調整（離すと終了3秒前から試聴して締めを確認） */}
                        {song.end > 0 && (
                          <TimestampTweaker
                            value={song.end}
                            mode="end"
                            currentTime={currentPlayerTime}
                            onChange={(v) => handleSongChange(index, 'end', v)}
                          />
                        )}
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
                        {/* Chat 與 Comment end 差異過大警告 + 套用按鈕 */}
                        {song.endDiff !== undefined && song.endDiff >= 10 && (
                          <div className="mt-1 flex items-center gap-2 text-xs text-amber-700 bg-amber-50 px-2 py-1 rounded">
                            <div className="flex items-center gap-1">
                              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                                <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                              </svg>
                              <span>
                                Chat との差 {song.endDiff} 秒
                                {song.chatEnd !== undefined && `（Chat: ${formatTimeInput(song.chatEnd)}）`}
                              </span>
                            </div>
                            <button
                              onClick={() => applyEndSource(index, 'chat')}
                              className="px-2 py-0.5 bg-amber-200 hover:bg-amber-300 text-amber-800 rounded text-xs font-medium"
                            >
                              Chat の値を適用
                            </button>
                          </div>
                        )}

                        {/* 還原為 Comment 原始 end 的按鈕 */}
                        {song.originalCommentEnd !== undefined && song.end !== song.originalCommentEnd && (
                          <button
                            onClick={() => applyEndSource(index, 'comment')}
                            className="mt-1 text-xs text-blue-600 hover:text-blue-800 underline"
                          >
                            元の値に戻す ({song.originalCommentEnd ? formatTimeInput(song.originalCommentEnd) : ''})
                          </button>
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

                    {/* 確認ナビゲーション：前へ / 確認して次へ / 次へ */}
                    <div className="mt-4 pt-3 border-t flex items-center gap-2">
                      <button
                        onClick={() => selectSong((index - 1 + editableSongs.length) % editableSongs.length)}
                        disabled={editableSongs.length < 2}
                        className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-40"
                        title="前の曲へ"
                      >
                        ◀ 前へ
                      </button>
                      <button
                        onClick={() => confirmAndNext(index)}
                        disabled={song.end === 0}
                        title={song.end === 0 ? '終了時間を設定してください' : 'この曲を確認済みにして次の未確認曲へ'}
                        className={`flex-1 px-3 py-1.5 text-sm font-medium rounded-lg transition-colors disabled:opacity-40 ${
                          song.confirmed
                            ? 'bg-green-100 text-green-700 hover:bg-green-200'
                            : 'bg-indigo-600 text-white hover:bg-indigo-700'
                        }`}
                      >
                        {song.confirmed ? '✓ 確認済み（再確認で次へ）' : '✓ 確認して次へ'}
                      </button>
                      <button
                        onClick={() => selectSong((index + 1) % editableSongs.length)}
                        disabled={editableSongs.length < 2}
                        className="px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50 transition-colors disabled:opacity-40"
                        title="次の曲へ"
                      >
                        次へ ▶
                      </button>
                    </div>
                  </div>
                )
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
                          <ArtistLinks
                            artists={perf.artists}
                            fallback={perf.original_artist}
                            linkClassName="hover:text-indigo-600"
                          />
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
                          <div className="inline-flex items-center gap-1.5">
                            <button
                              onClick={() => youtubePlayerSeekTo(perf.start_seconds)}
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
                              title={`${perf.song_name} を再生`}
                            >
                              <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M8 5v14l11-7z" />
                              </svg>
                            </button>
                            <QueueAddButton
                              track={{
                                performanceId: perf.id,
                                streamId: perf.stream_id,
                                songId: perf.song_id,
                                songName: perf.song_name,
                                artist: perf.original_artist,
                                artists: perf.artists ?? [],
                                artUrl: perf.arts,
                                singers: perf.singers?.map((s) => ({ id: s.id, name: s.name })) ?? [],
                                streamTitle: stream.title,
                                streamDate: stream.stream_date,
                                start: perf.start_seconds,
                                end: perf.end_seconds,
                              }}
                            />
                            <a
                              href={perf.youtube_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center justify-center w-8 h-8 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                              title="YouTubeで開く"
                            >
                              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                                <path d="M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z" />
                              </svg>
                            </a>
                          </div>
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
