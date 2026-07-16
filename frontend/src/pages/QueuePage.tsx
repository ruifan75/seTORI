import { Link } from 'react-router-dom';
import { usePlayerStore } from '../store/player';

function fmt(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  const m = Math.floor(s / 60);
  return `${m}:${String(s % 60).padStart(2, '0')}`;
}

// 再生キューの全画面表示。バーの小さなパネルでは切れてしまう
// 曲名・配信タイトルをフル表示し、ジャンプ・削除・全消去ができる。
export default function QueuePage() {
  const { queue, index, playing, jumpTo, removeAt, clear } = usePlayerStore();

  if (queue.length === 0) {
    return (
      <div className="text-center py-16 text-gray-500 space-y-2">
        <p>再生キューは空です</p>
        <p className="text-sm text-gray-400">
          楽曲ページや歌枠ページの「▶ 再生」からキューを作成できます
        </p>
      </div>
    );
  }

  const totalSec = queue.reduce((acc, t) => acc + (t.end > t.start ? t.end - t.start : 0), 0);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-3xl font-bold text-gray-900">再生キュー</h1>
        <span className="text-sm text-gray-500">
          {queue.length}曲 · 合計 {fmt(totalSec)}
        </span>
        <button
          onClick={clear}
          className="ml-auto px-3 py-1.5 text-sm bg-white border border-gray-300 text-gray-600 rounded-lg hover:bg-gray-50"
        >
          キューをクリア
        </button>
      </div>

      <div className="bg-white rounded-lg shadow-sm border divide-y">
        {queue.map((t, i) => {
          const isCurrent = i === index;
          return (
            <div
              key={`${t.performanceId}-${i}`}
              className={`flex items-start gap-3 px-4 py-3 ${isCurrent ? 'bg-indigo-50' : 'hover:bg-gray-50'}`}
            >
              {/* 再生 / 位置 */}
              <button
                onClick={() => jumpTo(i)}
                className={`mt-1 w-8 h-8 rounded-full flex items-center justify-center shrink-0 transition-colors ${
                  isCurrent
                    ? 'bg-indigo-600 text-white'
                    : 'bg-gray-100 text-gray-500 hover:bg-indigo-100 hover:text-indigo-600'
                }`}
                title={isCurrent ? (playing ? '再生中' : '一時停止中') : 'この曲を再生'}
              >
                {isCurrent && playing ? (
                  <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>
                ) : (
                  <svg className="w-4 h-4 ml-0.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                )}
              </button>

              {t.artUrl ? (
                <img src={t.artUrl} alt="" className="w-12 h-12 object-cover rounded shrink-0 mt-0.5" />
              ) : (
                <span className="w-12 h-12 bg-gray-100 rounded shrink-0 mt-0.5" />
              )}

              {/* フル表示（truncate しない） */}
              <div className="flex-1 min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2">
                  {t.songId ? (
                    <Link
                      to={`/songs/${t.songId}`}
                      className={`font-medium hover:text-indigo-600 ${isCurrent ? 'text-indigo-700' : 'text-gray-900'}`}
                    >
                      {t.songName}
                    </Link>
                  ) : (
                    <span className={`font-medium ${isCurrent ? 'text-indigo-700' : 'text-gray-900'}`}>{t.songName}</span>
                  )}
                  {t.artist && (
                    <Link
                      to={`/artists?search=${encodeURIComponent(t.artist)}`}
                      className="text-sm text-gray-500 hover:text-indigo-600"
                    >
                      {t.artist}
                    </Link>
                  )}
                </div>
                <div className="text-sm text-gray-500 mt-0.5 break-words">
                  {t.streamDate && (
                    <span className="font-mono text-gray-400">
                      {new Date(t.streamDate).toLocaleDateString('ja-JP')} ·{' '}
                    </span>
                  )}
                  {t.streamTitle && (
                    <Link to={`/streams/${t.streamId}`} className="hover:text-gray-800">
                      {t.streamTitle}
                    </Link>
                  )}
                </div>
                {t.singers.length > 0 && (
                  <div className="text-xs text-gray-400 mt-0.5">
                    歌唱:{' '}
                    {t.singers.map((s, j) => (
                      <span key={s.id}>
                        {j > 0 && '、'}
                        <Link to={`/singers/${s.id}`} className="hover:text-indigo-600">
                          {s.name}
                        </Link>
                      </span>
                    ))}
                  </div>
                )}
              </div>

              <span className="text-sm font-mono text-gray-400 shrink-0 mt-1">
                {t.end > t.start ? fmt(t.end - t.start) : '--:--'}
              </span>
              <button
                onClick={() => removeAt(i)}
                className="mt-1 text-gray-300 hover:text-red-500 shrink-0"
                title="キューから削除"
              >
                ✕
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
