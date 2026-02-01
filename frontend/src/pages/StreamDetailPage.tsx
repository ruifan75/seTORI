import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams, Link } from 'react-router-dom';
import { streamApi, performanceApi, commentApi, aiApi, songApi, singerApi } from '../api/client';
import type { Singer, CreatePerformanceItem, AINormalizationItem, Song, UpdateStreamRequest, SongEndTimeEstimateRequest, EstimateEndTimesRequest } from '../api/types';
import Loading from '../components/ui/Loading';
import Tag from '../components/ui/Tag';
import { useToast } from '../components/ui/Toast';

// 預設的直播類型標籤
const STREAM_TAGS = [
  { id: 'singing', label: '歌枠', color: '#E91E63' },
  { id: 'anniversary', label: '周年', color: '#FFD700' },
  { id: 'birthday', label: '誕生日', color: '#FF69B4' },
  { id: 'concert', label: 'ライブ', color: '#9C27B0' },
  { id: 'karaoke', label: 'カラオケ', color: '#2196F3' },
  { id: 'unarchived', label: 'アーカイブなし', color: '#607D8B' },
  { id: 'members_only', label: 'メン限', color: '#4CAF50' },
];

// 可編輯的直播資訊
interface EditableStreamInfo {
  title: string;
  streamDate: string;
  tagIds: string[];
  participantIds: string[];
  isProcessed: boolean;
  isHidden: boolean;
}

// 預設的演出標籤
const PERFORMANCE_TAGS = [
  { id: 'acoustic', label: 'Acoustic', color: '#8B4513' },
  { id: 'piano', label: 'Piano', color: '#4169E1' },
  { id: 'hikigatari', label: '弾き語り', color: '#228B22' },
  { id: 'acappella', label: 'A Cappella', color: '#9932CC' },
  { id: 'short', label: 'Short', color: '#FF8C00' },
  { id: 'full', label: 'Full', color: '#20B2AA' },
  { id: 'medley', label: 'Medley', color: '#FF69B4' },
];

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
  originalName: string; // 追蹤原始名稱（用於判斷是否有修改）
  originalArtist: string; // 追蹤原始藝人
  // AI 正規化追蹤
  aiNormalizedName?: string; // AI 修改前的名稱（如果有被 AI 修改）
  aiNormalizedArtist?: string; // AI 修改前的藝人（如果有被 AI 修改）
  // 時間估計標記
  isEndTimeEstimated?: boolean; // 結束時間是否為估計值
}

// 歌曲搜尋輸入元件的 Props
interface SongSearchInputProps {
  value: string;
  onChange: (value: string) => void;
  onSelectSong: (song: Song) => void;
  placeholder?: string;
}

// 歌曲搜尋輸入元件（帶自動完成）
function SongSearchInput({ value, onChange, onSelectSong, placeholder }: SongSearchInputProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [suggestions, setSuggestions] = useState<Song[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Debounce 搜尋
  useEffect(() => {
    if (searchQuery.length < 2) {
      setSuggestions([]);
      return;
    }

    const timer = setTimeout(async () => {
      setIsLoading(true);
      try {
        const result = await songApi.list(1, 10, searchQuery);
        setSuggestions(result.songs);
      } catch {
        setSuggestions([]);
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
    setIsOpen(false);
    setSearchQuery('');
    setSuggestions([]);
  };

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={handleInputChange}
        onFocus={() => {
          if (suggestions.length > 0) setIsOpen(true);
        }}
        className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        placeholder={placeholder}
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
            suggestions.map((song) => (
              <button
                key={song.id}
                onClick={() => handleSelectSong(song)}
                className="w-full px-4 py-3 text-left hover:bg-indigo-50 border-b border-gray-100 last:border-b-0 transition-colors"
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
                  </div>
                  <div className="text-xs text-gray-400">
                    {song.performance_count}回
                  </div>
                </div>
              </button>
            ))
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
    if (searchQuery.length < 2) {
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
  const parts = timeStr.split(':').map(Number);
  if (parts.length === 3) {
    return parts[0] * 3600 + parts[1] * 60 + parts[2];
  } else if (parts.length === 2) {
    return parts[0] * 60 + parts[1];
  }
  return 0;
}

function formatTimeInput(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export default function StreamDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const [isEditing, setIsEditing] = useState(false);
  const [editableSongs, setEditableSongs] = useState<EditableSong[]>([]);
  const [channelOwner, setChannelOwner] = useState<Singer | null>(null);
  const [participants, setParticipants] = useState<Singer[]>([]);
  const [editableStreamInfo, setEditableStreamInfo] = useState<EditableStreamInfo | null>(null);
  const [vocalistPopupSingers, setVocalistPopupSingers] = useState<Singer[] | null>(null);

  const { data: stream, isLoading } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => streamApi.get(id!),
    enabled: !!id,
    staleTime: 0, // 確保每次進入頁面都重新載入
  });

  // 當 stream 資料載入後，設置頻道擁有者
  useEffect(() => {
    if (stream?.channel_owner) {
      setChannelOwner(stream.channel_owner);
    }
  }, [stream]);

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

  // Holodex から歌曲を読み込む
  const loadHolodexMutation = useMutation({
    mutationFn: () => performanceApi.loadHolodexSongs(id!),
    onSuccess: (data) => {
      if (data.songs.length === 0) {
        showToast('Holodexに楽曲データがありませんでした', 'info');
        return;
      }
      // 保存頻道擁有者和所有參與者
      setChannelOwner(data.channel_owner);
      setParticipants(data.participants || [data.channel_owner]);

      // 更新可編輯的直播資訊中的參與者
      if (editableStreamInfo) {
        setEditableStreamInfo({
          ...editableStreamInfo,
          participantIds: (data.participants || [data.channel_owner]).map(p => p.id),
        });
      }

      // 轉換為可編輯歌曲
      const songs: EditableSong[] = data.songs.map((song, index) => ({
        id: `holodex-${index}`,
        name: song.name,
        nameReading: '',
        artist: song.original_artist,
        artistReading: '',
        start: song.start_seconds,
        end: song.end_seconds,
        tags: song.tags,
        singerIds: song.singer_ids,
        matchedSongId: null,
        artUrl: song.art_url || null,
        itunesId: song.itunes_id || null,
        originalName: song.name,
        originalArtist: song.original_artist,
        // Holodex 的資料不需要標記 AI 或估計
        aiNormalizedName: undefined,
        aiNormalizedArtist: undefined,
        isEndTimeEstimated: false,
      }));
      setEditableSongs(songs);
      showToast(`Holodexから${songs.length}曲を読み込みました`, 'success');
    },
    onError: (err: Error) => {
      showToast(`Holodex読み込みエラー: ${err.message}`, 'error');
    },
  });

  // Comment 分析
  const analyzeCommentsMutation = useMutation({
    mutationFn: () => commentApi.analyze(id!),
    onSuccess: (data) => {
      if (data.songs.length === 0) {
        showToast('コメントから楽曲が見つかりませんでした', 'info');
        return;
      }
      // 轉換為可編輯歌曲（預設演唱者為頻道擁有者）
      const defaultSingerIds = channelOwner ? [channelOwner.id] : [];
      const songs: EditableSong[] = data.songs.map((song, index) => ({
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
        originalName: song.name,
        originalArtist: song.original_artist,
        aiNormalizedName: undefined,
        aiNormalizedArtist: undefined,
        isEndTimeEstimated: song.is_end_time_estimated,
      }));
      setEditableSongs(songs);
      showToast(`コメントから${songs.length}曲を検出しました`, 'success');
    },
    onError: (err: Error) => {
      showToast(`コメント分析エラー: ${err.message}`, 'error');
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
      const updated: EditableSong[] = [];
      
      for (const suggestion of data.suggestions) {
        if (suggestion.index < editableSongs.length) {
          const current = editableSongs[suggestion.index];
          // 檢查是否有被 AI 修改
          const nameChanged = current.name !== suggestion.normalized_name;
          const artistChanged = current.artist !== suggestion.original_artist;
          
          let artUrl = current.artUrl;
          
          // 如果有 matched_song_id 且沒有 art，則獲取 song 的 art
          if (suggestion.matched_song_id && !artUrl) {
            try {
              const song = await songApi.get(suggestion.matched_song_id);
              artUrl = song.arts || null;
            } catch (err) {
              // 忽略錯誤，使用原有的 artUrl
            }
          }
          
          updated[suggestion.index] = {
            ...current,
            name: suggestion.normalized_name,
            nameReading: suggestion.normalized_name_reading,
            artist: suggestion.original_artist,
            artistReading: suggestion.original_artist_reading,
            tags: suggestion.tags,
            matchedSongId: suggestion.matched_song_id || null,
            artUrl,
            // 保留 AI 修改前的值
            aiNormalizedName: nameChanged ? current.name : undefined,
            aiNormalizedArtist: artistChanged ? current.artist : undefined,
            // 更新原始值以追蹤後續變更
            originalName: suggestion.normalized_name,
            originalArtist: suggestion.original_artist,
          };
        }
      }
      
      setEditableSongs(updated);
      showToast(`${data.suggestions.length}曲のAI正規化が完了しました`, 'success');
    },
    onError: (err: Error) => {
      showToast(`AI正規化エラー: ${err.message}`, 'error');
    },
  });

  // 推算結束時間
  const estimateEndTimesMutation = useMutation({
    mutationFn: (req: EstimateEndTimesRequest) =>
      streamApi.estimateEndTimes(id!, req),
    onSuccess: (data) => {
      setEditableSongs((prev) => {
        const updated = [...prev];
        for (let i = 0; i < data.estimates.length && i < updated.length; i++) {
          const estimate = data.estimates[i];
          updated[i] = {
            ...updated[i],
            end: estimate.estimated_end,
            isEndTimeEstimated: estimate.is_end_time_estimated,
          };
        }
        return updated;
      });
      showToast('結束時間の推算が完了しました', 'success');
    },
    onError: (err: Error) => {
      showToast(`推算エラー: ${err.message}`, 'error');
    },
  });

  const loadFromHolodex = () => {
    loadHolodexMutation.mutate();
  };

  const loadFromComments = () => {
    analyzeCommentsMutation.mutate();
  };

  const estimateEndTimes = () => {
    if (!stream) return;
    
    const songs: SongEndTimeEstimateRequest[] = editableSongs.map((song, index) => ({
      start: song.start,
      end: song.end,
      name: song.name,
      artist: song.artist,
      itunes_id: song.itunesId || undefined,
      next_start: index + 1 < editableSongs.length ? editableSongs[index + 1].start : undefined,
      stream_end: stream.duration_seconds || undefined,
    }));

    estimateEndTimesMutation.mutate({
      songs,
      stream_end: stream.duration_seconds || 0,
      stream_title: stream.title,
    });
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
            originalName: perf.song_name,
            originalArtist: perf.original_artist,
            // 現有資料沒有 AI 修改或估計時間的標記
            aiNormalizedName: undefined,
            aiNormalizedArtist: undefined,
            isEndTimeEstimated: false,
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
    }));
    aiNormalizeMutation.mutate(items);
  };

  const handleSongChange = (index: number, field: keyof EditableSong, value: string | number | string[] | null) => {
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
  const handleSelectExistingSong = (index: number, song: Song) => {
    setEditableSongs((prev) => {
      const updated = [...prev];
      updated[index] = {
        ...updated[index],
        name: song.name,
        nameReading: song.name_reading || '',
        artist: song.original_artist,
        artistReading: song.original_artist_reading || '',
        artUrl: song.arts || null,
        matchedSongId: song.id,
        originalName: song.name,
        originalArtist: song.original_artist,
      };
      return updated;
    });
    showToast(`「${song.name}」を選択しました`, 'success');
  };

  const handleTimeChange = (index: number, field: 'start' | 'end', timeStr: string) => {
    const seconds = parseTime(timeStr);
    handleSongChange(index, field, seconds);
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
        originalName: '',
        originalArtist: '',
      },
    ]);
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

    // 如果有歌曲，更新 setlist
    if (editableSongs.length > 0) {
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
      }));
      createPerformancesMutation.mutate(performances);
    } else {
      // 沒有歌曲時，只保存 Stream 資訊並關閉編輯模式
      showToast('歌枠情報を保存しました', 'success');
      setIsEditing(false);
      setEditableStreamInfo(null);
      queryClient.invalidateQueries({ queryKey: ['stream', id] });
    }
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

  return (
    <div className="space-y-6">
      {/* Stream Header */}
      <div className="bg-white rounded-lg shadow-sm border overflow-hidden">
        <div className="md:flex">
          {/* Thumbnail */}
          <a
            href={youtubeUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="block md:w-96 flex-shrink-0 relative group"
          >
            {stream.thumbnail_url ? (
              <img
                src={stream.thumbnail_url}
                alt={stream.title}
                className="w-full h-56 md:h-full object-cover"
              />
            ) : (
              <div className="w-full h-56 md:h-full bg-gray-200 flex items-center justify-center">
                <span className="text-gray-400">No Image</span>
              </div>
            )}
            {/* Play overlay */}
            <div className="absolute inset-0 bg-black bg-opacity-0 group-hover:bg-opacity-30 flex items-center justify-center transition-all">
              <div className="w-16 h-16 rounded-full bg-red-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                <svg className="w-8 h-8 text-white ml-1" fill="currentColor" viewBox="0 0 24 24">
                  <path d="M8 5v14l11-7z" />
                </svg>
              </div>
            </div>
          </a>

          {/* Content */}
          <div className="p-6 flex-1">
            {isEditing && editableStreamInfo ? (
              <>
                {/* Title - Read Only */}
                <h1 className="text-2xl font-bold text-gray-900">{stream.title}</h1>
                {/* Date - Read Only */}
                <p className="text-gray-500 mt-2">{stream.stream_date}</p>

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
      </div>

      {/* Song Edit Modal - Removed */}

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

      {/* Setlist Section - Unified View */}
      <div>
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-2xl font-bold text-gray-900">
            セットリスト ({isEditing ? editableSongs.length : stream.performances.length}曲)
          </h2>

          {/* Edit Mode Actions */}
          {isEditing && (
            <div className="flex flex-wrap gap-3">
              <button
                onClick={loadFromHolodex}
                disabled={loadHolodexMutation.isPending}
                className="px-4 py-2 bg-blue-600 text-white font-medium rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50"
              >
                {loadHolodexMutation.isPending ? '読み込み中...' : 'Holodexから同期'}
              </button>
              <button
                onClick={loadFromComments}
                disabled={analyzeCommentsMutation.isPending}
                className="px-4 py-2 bg-green-600 text-white font-medium rounded-lg hover:bg-green-700 transition-colors disabled:opacity-50"
              >
                {analyzeCommentsMutation.isPending ? '分析中...' : 'コメントから読み込む'}
              </button>
              {editableSongs.length > 0 && (
                <>
                  <button
                    onClick={runAINormalization}
                    disabled={aiNormalizeMutation.isPending}
                    className="px-4 py-2 bg-purple-600 text-white font-medium rounded-lg hover:bg-purple-700 transition-colors disabled:opacity-50"
                  >
                    {aiNormalizeMutation.isPending ? 'AI処理中...' : 'AI正規化'}
                  </button>
                  <button
                    onClick={estimateEndTimes}
                    disabled={estimateEndTimesMutation.isPending}
                    className="px-4 py-2 bg-orange-600 text-white font-medium rounded-lg hover:bg-orange-700 transition-colors disabled:opacity-50"
                  >
                    {estimateEndTimesMutation.isPending ? '推算中...' : '結束時間を推算'}
                  </button>
                </>
              )}
            </div>
          )}
        </div>

        {isEditing ? (
          /* Editable Setlist */
          <div className="bg-white rounded-lg shadow-sm border p-6">
            {/* Editable songs list will default to channel owner as vocalist */}

            {editableSongs.length > 0 ? (
              <div className="space-y-4">
                {editableSongs.map((song, index) => (
                  <div key={song.id} className="border rounded-lg p-4 bg-gray-50">
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
                        <input
                          type="text"
                          value={formatTimeInput(song.start)}
                          onChange={(e) => handleTimeChange(index, 'start', e.target.value)}
                          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono"
                          placeholder="0:00"
                        />
                      </div>

                      {/* End Time */}
                      <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">
                          終了時間 <span className="text-gray-400">(任意)</span>
                        </label>
                        <input
                          type="text"
                          value={song.end ? formatTimeInput(song.end) : ''}
                          onChange={(e) => handleTimeChange(index, 'end', e.target.value)}
                          className={`w-full px-3 py-2 border rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent font-mono ${
                            song.isEndTimeEstimated ? 'border-orange-300 bg-orange-50' : 'border-gray-300'
                          }`}
                          placeholder={song.end === 0 ? "「結束時間を推算」で自動計算" : "0:00"}
                        />
                        {/* 沒有結束時間的提示 */}
                        {song.end === 0 && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-gray-500">
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
                            </svg>
                            <span>未設定 - ボタンをクリックして推算してください</span>
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

                {/* Add Song Button */}
                <button
                  onClick={addSong}
                  className="w-full py-3 border-2 border-dashed border-gray-300 rounded-lg text-gray-500 hover:border-indigo-500 hover:text-indigo-500 transition-colors"
                >
                  + 楽曲を追加
                </button>

                {/* Confirm Button */}
                <div className="flex justify-end pt-4 border-t">
                  <button
                    onClick={handleConfirm}
                    disabled={createPerformancesMutation.isPending || updateStreamMutation.isPending}
                    className="px-6 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                  >
                    {createPerformancesMutation.isPending || updateStreamMutation.isPending ? '処理中...' : '変更を保存'}
                  </button>
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="text-center py-8 text-gray-500">
                  上のボタンから楽曲データを読み込んでください
                </div>
                {/* Save button for stream info only (no songs) */}
                <div className="flex justify-end pt-4 border-t">
                  <button
                    onClick={handleConfirm}
                    disabled={updateStreamMutation.isPending}
                    className="px-6 py-2 bg-indigo-600 text-white font-medium rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                  >
                    {updateStreamMutation.isPending ? '処理中...' : '変更を保存'}
                  </button>
                </div>
              </div>
            )}
          </div>
        ) : (
          /* Read-only Setlist */
          stream.performances.length === 0 ? (
            <div className="text-center py-12 text-gray-500 bg-white rounded-lg border">
              セットリストがまだ登録されていません
            </div>
          ) : (
            <div className="bg-white rounded-lg shadow-sm border overflow-hidden">
              <table className="min-w-full divide-y divide-gray-200">
                <thead className="bg-gray-50">
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
                          <a
                            href={perf.youtube_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-red-100 text-red-600 hover:bg-red-200 transition-colors"
                          >
                            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                              <path d="M8 5v14l11-7z" />
                            </svg>
                          </a>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )
        )}
      </div>
    </div>
  );
}
