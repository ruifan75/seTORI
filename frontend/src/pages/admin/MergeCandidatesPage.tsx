import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { songApi } from '../../api/client';
import type { MergeCandidate, MergeCandidateSong } from '../../api/types';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 楽曲の重複候補レビュー。
//
// コメントから取り込んだ曲を DB と照合するとき、確信度が自動採用に届かないものは
// 新曲として登録したうえでここに積まれる。典型は同一人物の別名義
// （"ひこうき雲 / 松任谷由実" と "ひこうき雲 / 荒井由実"）で、
// 文字列の比較だけでは同じ曲かどうか決められない。
//
// 以前はこの判断がつかないケースを黙って新曲にしていたため、重複が
// 見えないまま増え続けていた。ここに出して人が畳めるようにするのが目的。

// 判定理由の説明。なぜ候補に挙がったかが分からないと判断できない。
const REASON_LABELS: Record<string, string> = {
  title_mismatch: '曲名は一致・アーティストが違う（別名義の可能性）',
  title_ambiguous: '同じ曲名の曲が複数あり決め手がない',
  title_only: '曲名は一致・アーティスト未記入',
  fuzzy_title: '曲名が似ている',
};

function SongCard({ song, badge }: { song: MergeCandidateSong; badge: string }) {
  return (
    <div className="flex-1 min-w-0 rounded border border-gray-200 p-3">
      <div className="mb-1 text-xs font-medium text-gray-500">{badge}</div>
      <div className="flex items-start gap-2">
        {song.art_url && (
          <img src={song.art_url} alt="" className="h-10 w-10 flex-shrink-0 rounded object-cover" />
        )}
        <div className="min-w-0">
          <Link
            to={`/songs/${song.id}`}
            className="block truncate font-medium text-blue-600 hover:underline"
            title={song.name}
          >
            {song.name}
          </Link>
          <div className="truncate text-sm text-gray-600" title={song.original_artist}>
            {song.original_artist || <span className="text-gray-400">（アーティスト未記入）</span>}
          </div>
          <div className="mt-1 text-xs text-gray-500">歌唱 {song.performance_count} 件</div>
        </div>
      </div>
    </div>
  );
}

export default function MergeCandidatesPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data, isLoading } = useQuery({
    queryKey: ['song-merge-candidates'],
    queryFn: () => songApi.mergeCandidates(100),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['song-merge-candidates'] });
    queryClient.invalidateQueries({ queryKey: ['songs'] });
  };

  // 統合の向きは「歌唱記録の多い側に寄せる」。統合元は削除されるので、
  // 履歴が多いほうを残したほうが失うものが少ない。
  const mergeMutation = useMutation({
    mutationFn: ({ sourceId, targetId }: { sourceId: string; targetId: string }) =>
      songApi.merge(sourceId, targetId),
    onSuccess: () => {
      showToast('楽曲を統合しました', 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`統合に失敗しました: ${e.message}`, 'error'),
  });

  const dismissMutation = useMutation({
    mutationFn: (id: string) => songApi.dismissMergeCandidate(id),
    onSuccess: () => {
      showToast('別の曲として記録しました', 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`却下に失敗しました: ${e.message}`, 'error'),
  });

  const handleMerge = (c: MergeCandidate) => {
    const keepNew = c.new_song.performance_count > c.existing_song.performance_count;
    const source = keepNew ? c.existing_song : c.new_song;
    const target = keepNew ? c.new_song : c.existing_song;
    if (
      !window.confirm(
        `「${source.name} / ${source.original_artist}」を\n` +
          `「${target.name} / ${target.original_artist}」に統合します。\n\n` +
          `統合元の歌唱 ${source.performance_count} 件は統合先へ移り、統合元の曲は削除されます。`,
      )
    ) {
      return;
    }
    mergeMutation.mutate({ sourceId: source.id, targetId: target.id });
  };

  if (isLoading) return <Loading />;

  const candidates = data?.candidates ?? [];

  return (
    <div className="mx-auto max-w-5xl p-4">
      <h1 className="mb-1 text-2xl font-bold">楽曲の重複候補</h1>
      <p className="mb-6 text-sm text-gray-600">
        取り込み時に既存の曲と照合しきれず、新しく登録された曲です。同じ曲なら統合してください。
      </p>

      {candidates.length === 0 ? (
        <div className="rounded border border-gray-200 p-8 text-center text-gray-500">
          未処理の重複候補はありません。
        </div>
      ) : (
        <ul className="space-y-4">
          {candidates.map((c) => (
            <li key={c.id} className="rounded border border-gray-200 p-4">
              <div className="mb-3 flex items-center gap-2 text-sm">
                <span className="rounded bg-amber-100 px-2 py-0.5 text-amber-800">
                  {REASON_LABELS[c.reason] ?? c.reason}
                </span>
                <span className="text-gray-400">確信度 {Math.round(c.score * 100)}%</span>
              </div>

              <div className="flex flex-col gap-3 sm:flex-row">
                <SongCard song={c.new_song} badge="新しく登録された曲" />
                <SongCard song={c.existing_song} badge="既存の曲" />
              </div>

              <div className="mt-3 flex justify-end gap-2">
                <button
                  onClick={() => dismissMutation.mutate(c.id)}
                  disabled={dismissMutation.isPending}
                  className="rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                  title="同じ曲ではないので、今後この組を候補に出さない"
                >
                  別の曲
                </button>
                <button
                  onClick={() => handleMerge(c)}
                  disabled={mergeMutation.isPending}
                  className="rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
                  title="歌唱記録の多いほうに寄せて統合する"
                >
                  統合する
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
