import YoutubePlayer from '../../components/YoutubePlayer';
import UnplayableNotice from '../../components/UnplayableNotice';
import { playerSeekTo } from '../../components/youtubePlayerControl';
import { formatTime } from './utils';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamPlayerPanel({ model }: { model: StreamDetailModel }) {
  const {
    noticeKind,
    stream,
    playerInstanceRef,
    handlePlaybackError,
    setoriTimeline,
    isEditing,
    scrollToEditableSong,
    getTimelineLeft,
    getTimelineWidth,
    getTooltipAlignClass,
    canEdit,
    holodexTimeline,
    rawCommentTimeline,
  } = model;
  return (
    <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border flex flex-col min-h-0 sm:h-[40vh] min-[1300px]:h-auto">
      {/* 映像の領域。sm〜1300px では残りの高さ（時間帯バーを除いた分）を占め、
          その中に **min(幅いっぱい, 高さ × 16/9)** の 16:9 を中央に置く。
          高さだけで幅を決めると狭い幅でカードから溢れ（800px で 476px 幅 vs 369px）、
          幅だけで決めると縦が溢れる ── 両方の小さいほうを取れば比も収まりも保てる。
          時間帯バーの高さは閲覧者 1 段・編集者 3 段で違うので、定数で引かずに
          コンテナクエリ（cqw / cqh）で実際の残りを基準にする。
          1300px 以上（grid が内容から高さを決める）と sm 未満（縦積み・上限なし）は従来どおり。 */}
      <div className="sm:flex-1 sm:min-h-0 sm:[container-type:size] sm:flex sm:items-center sm:justify-center min-[1300px]:flex-none min-[1300px]:[container-type:normal] min-[1300px]:block">
      {/* 16:9 の器。プレイヤーはこれを埋める（審査画面の器と同じ形）。 */}
      <div className="w-full sm:w-[min(100cqw,calc(100cqh*16/9))] min-[1300px]:w-full">
      {/* **基本は描いてみて、失敗したら onError で切り替える。**
          保存済みの判定は古くなるし（アーカイブが後から会限化する、権利で
          降ろされる）、`public` はそもそも「反証が無かった」という弱い結論
          でしかない（docs/STREAM_VISIBILITY.md）。さらに yt-dlp は東京の VPS で
          走るので、その結果は見る人の所在地では正しくないことがある。
          例外は会限だけ ── 実測で所在地に依らず 150 を返すので、必ず失敗する
          iframe を描くより理由を出すほうがよい（メンバー資格があっても同じ）。 */}
      {noticeKind ? (
        <UnplayableNotice kind={noticeKind} videoId={stream.id} />
      ) : (
        <div className="bg-black w-full aspect-video overflow-hidden">
          <YoutubePlayer
            videoId={stream.id}
            onReady={(player) => {
              playerInstanceRef.current = player;
            }}
            onError={handlePlaybackError}
          />
        </div>
      )}
      </div>
      </div>

      <div className="border-t py-3 px-0 shrink-0">
        {/* 時間帯バーはカード幅に置く。映像は高さ上限に合わせて幅が狭くなる場合がある。 */}
        <div className="space-y-1 px-3 sm:pl-9 sm:pr-8">
          <div className="relative h-3 bg-gray-100 rounded-none">
            {setoriTimeline.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => {
                  playerSeekTo('page', item.start);
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
          {/* 入力元（Holodex・生コメント）の時間帯。**編集中でなくても出す** ──
              プレイヤーを 16:9 に固定して空いた分をここに使う。
              データ自体が content:edit のときしか返らないので、閲覧者には出ない。 */}
          {canEdit && (
            <>
              <div className="relative h-3 bg-blue-50 rounded-none">
                {holodexTimeline.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => playerSeekTo('page', item.start)}
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
                    onClick={() => playerSeekTo('page', item.start)}
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
  );
}
