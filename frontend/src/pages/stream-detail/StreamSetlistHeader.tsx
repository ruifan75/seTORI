import { usePlayerStore } from '../../store/player';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamSetlistHeader({ model }: { model: StreamDetailModel }) {
  const {
    isEditing,
    editableSongs,
    stream,
    canEdit,
    toggleEditing,
    performanceTracks,
    showToast,
    confirmedCount,
    quickSaveStream,
    handleConfirm,
    createPerformancesMutation,
    updateStreamMutation,
  } = model;
  return (
    <div className="flex flex-col gap-3 mb-4 flex-none shrink-0">
      <div className={`flex items-center gap-3 ${isEditing ? 'max-lg:flex-wrap' : ''}`}>
        <h2 className={`text-2xl font-bold text-gray-900 ${isEditing ? 'max-lg:w-full' : ''}`}>
          セットリスト ({isEditing ? editableSongs.length : stream.performances.length}曲)
          {stream.is_restricted && (
            <span className="ml-2 align-middle text-xs font-normal px-2 py-0.5 rounded bg-amber-100 text-amber-800">
              非公開
            </span>
          )}
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
          /* lg 未満は見出しと操作を別行にし、操作も折り返す。処理完了は保存の直前（即時保存）。 */
          <div className="ml-auto flex items-center gap-2 shrink-0 max-lg:ml-0 max-lg:w-full max-lg:min-w-0 max-lg:flex-wrap max-lg:[&>button]:whitespace-nowrap">
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
                checked={stream.is_processed ?? false}
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
  );
}
