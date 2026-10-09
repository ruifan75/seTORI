import QueryError from '../components/ui/QueryError';
import Loading from '../components/ui/Loading';
import { useStreamDetail } from './stream-detail/useStreamDetail';
import StreamAnalysisTabs from './stream-detail/StreamAnalysisTabs';
import StreamInfoCard from './stream-detail/StreamInfoCard';
import StreamPlayerPanel from './stream-detail/StreamPlayerPanel';
import StreamSetlistHeader from './stream-detail/StreamSetlistHeader';
import StreamEditableSetlist from './stream-detail/StreamEditableSetlist';
import StreamPerformanceList from './stream-detail/StreamPerformanceList';
import StreamVocalistPopup from './stream-detail/StreamVocalistPopup';

export default function StreamDetailPage() {
  const model = useStreamDetail();
  if (model.status === 'error') return <QueryError error={model.error} onRetry={model.refetch} />;
  if (model.status === 'loading') return <Loading />;
  if (model.status === 'missing') {
    return (
      <div className="text-center py-12 text-gray-500">
        配信が見つかりませんでした
      </div>
    );
  }
  const { isEditing, vocalistPopupSingers } = model;
  return (
    <>
      {/* 1300px 以上：左右のペインが画面内に収まり各自スクロールする。
          それ未満：縦積みなので高さを固定せず、main ごとスクロールさせる
          ── h-full + overflow-hidden のままだと、左カラム（情報＋プレイヤー）が
          縦を食い尽くしてセットリストが数十pxに潰れ、下へ送る手段も無くなる */}
      <div className="flex flex-col min-[1300px]:flex-row gap-6 w-full min-h-0 min-[1300px]:h-full min-[1300px]:overflow-hidden">
      {/* Left Column - Stream Info + YouTube Player */}
      {/* モバイル（<sm）は情報カードとプレイヤーを縦積みにし高さ制限も外す。sm〜1300px は左右並び+40vh 制限 */}
      <div className="w-full min-[1300px]:basis-2/5 min-[1300px]:shrink-0 min-[1300px]:self-stretch flex flex-col sm:flex-row min-[1300px]:grid min-[1300px]:grid-rows-[minmax(0,1fr)_auto] gap-4 min-h-0 min-[1300px]:overflow-hidden shrink-0 max-h-none sm:max-h-[40vh] min-[1300px]:max-h-none">
        {/* Stream Header - 40% */}
        <div className="flex-1 min-w-0 bg-white rounded-lg shadow-sm border overflow-y-auto min-h-0">
          {isEditing ? (
            /* 編集モード：情報カードの代わりにデータ読み込みタブ */
            <StreamAnalysisTabs model={model} />
          ) : (
            <StreamInfoCard model={model} />
          )}
        </div>

        {/* Player + Timeline - 60% */}
        {/* **sm〜1300px では高さを 40vh に決め打つ。** 親が sm:max-h-[40vh] で絞るこの幅帯で、
            以前は `w-full aspect-video` の器が縦に縮められて 16:9 を保てなかった
            （実測 1299px で比 1.99。issue #16）。上限（1bb43a5）は下のセットリストを
            見せるためのものなので残し、器のほうを「高さから幅を決める」形にする。
            カードの高さが決まっていないと下のコンテナクエリの基準が無いので、ここで決める。 */}
        <StreamPlayerPanel model={model} />
      </div>

      {/* Right Column - Setlist */}
      <div className="w-full min-[1300px]:basis-3/5 min-[1300px]:shrink-0 min-w-0 min-h-0 min-[1300px]:self-stretch min-[1300px]:pr-6 flex flex-col">
        {/* Setlist Section - Unified View */}
        <StreamSetlistHeader model={model} />

        <div className="overflow-x-hidden min-[1300px]:flex-1 min-[1300px]:min-h-0 min-[1300px]:overflow-y-auto">
        {isEditing ? (
          /* Editable Setlist */
          <StreamEditableSetlist model={model} />
        ) : (
          /* Read-only Setlist */
          <StreamPerformanceList model={model} />
        )}
        </div>
      </div>
    </div>

    {/* Vocalist Popup */}
    {vocalistPopupSingers && (
      <StreamVocalistPopup model={model} />
      )}
    </>
  );
}
