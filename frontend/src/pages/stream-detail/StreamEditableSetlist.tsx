import PerformanceFields from '../../components/PerformanceFields';
import { formatTimeInput } from '../../utils/timeFormat';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamEditableSetlist({ model }: { model: StreamDetailModel }) {
  const {
    editableSongs,
    selectedSongIndex,
    selectSong,
    highlightedSongId,
    removeSong,
    setEditableSongs,
    handleSelectExistingSong,
    handleTimeChange,
    toggleTag,
    applyEndSource,
    clearItunesId,
    PERFORMANCE_TAGS,
    participants,
    channelOwner,
    setParticipants,
    showToast,
    confirmAndNext,
    addSong,
  } = model;
  return (
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
                  </div>
                </div>
                <button
                  onClick={() => removeSong(index)}
                  className="text-red-500 hover:text-red-700 text-sm"
                >
                  削除
                </button>
              </div>

              <PerformanceFields
                value={song}
                onChange={(patch) => setEditableSongs((prev) => {
                  const updated = [...prev];
                  updated[index] = { ...updated[index], ...patch };
                  return updated;
                })}
                onSelectSong={(selectedSong) => handleSelectExistingSong(index, selectedSong)}
                onTimeChange={(field, timeStr) => handleTimeChange(index, field, timeStr)}
                onToggleTag={(tagId) => toggleTag(index, tagId)}
                onApplyEndSource={(source) => applyEndSource(index, source)}
                onClearItunes={() => clearItunesId(index)}
                performanceTags={PERFORMANCE_TAGS}
                participants={participants}
                channelOwner={channelOwner}
                onAddParticipant={(singer) => setParticipants([...participants, singer])}
                showToast={showToast}
              />

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
  );
}
