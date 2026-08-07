import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { songApi, itunesApi } from '../../api/client';
import type { MergeCandidate, MergeCandidateSong } from '../../api/types';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 楽曲の重複候補レビュー。
//
// 同じ曲名で複数の楽曲がある状態には、少なくとも 3 通りの正解がある。
//
//   惑星ループ（Eve / ナユタン星人）    … artist 欄が原唱と作曲のどちらを記録したかの
//                                        違いでしかない → 統合する
//   翼をください（赤い鳥 / 桜高軽音部） … 同じ作曲だが編曲が大きく違う。
//                                        どちらを歌ったかが情報になる → 分けたまま
//   オレンジ（SPYAIR / 逢坂大河ほか）   … そもそも別の曲 → 分けたまま
//
// 「どれだけ違えば分けるべきか」は編集方針であってデータから導けない。
// そこで AI には事実（同じ作曲か・編曲は同系統か）だけを答えさせ、
// 統合するかどうかは人が決める。統合は破壊的なので自動実行はしない。

const REASON_LABELS: Record<string, string> = {
  same_title: '曲名が同じ',
  title_mismatch: '曲名は一致・アーティストが違う（別名義の可能性）',
  title_ambiguous: '同じ曲名の曲が複数ある',
  title_only: '曲名は一致・アーティスト未記入',
  fuzzy_title: '曲名が似ている',
};

function fmtDuration(ms?: number) {
  if (!ms) return null;
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

// iTunes の収録アルバムと再生時間。「編曲がどれだけ違うか」を
// ここを見ただけで判断できるようにするためのもの。試聴もここから。
function ItunesEvidence({ itunesId }: { itunesId: number }) {
  const { data } = useQuery({
    queryKey: ['itunes', itunesId],
    queryFn: () => itunesApi.queryById(itunesId),
    staleTime: 1000 * 60 * 60,
  });
  if (!data) return null;
  const dur = fmtDuration(data.track_time_millis);
  return (
    <div className="mt-1 text-xs text-gray-500">
      <div className="truncate" title={data.collection_name}>
        {data.collection_name}
        {dur && <span className="ml-1 tabular-nums">({dur})</span>}
      </div>
      {data.preview_url && (
        <audio controls preload="none" src={data.preview_url} className="mt-1 h-7 w-full max-w-[220px]" />
      )}
    </div>
  );
}

function SongCard({ song, badge }: { song: MergeCandidateSong; badge: string }) {
  return (
    <div className="flex-1 min-w-0 rounded border border-gray-200 p-3">
      <div className="mb-1 text-xs font-medium text-gray-500">{badge}</div>
      <div className="flex items-start gap-2">
        {song.art_url && (
          <img src={song.art_url} alt="" className="h-10 w-10 flex-shrink-0 rounded object-cover" />
        )}
        <div className="min-w-0 flex-1">
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
          {song.role && <div className="mt-0.5 text-xs text-purple-700">{song.role}</div>}
          <div className="mt-1 text-xs text-gray-500">歌唱 {song.performance_count} 件</div>
          {song.itunes_ids?.[0] && <ItunesEvidence itunesId={song.itunes_ids[0]} />}
        </div>
      </div>
    </div>
  );
}

function VerdictBanner({ c }: { c: MergeCandidate }) {
  const v = c.verdict;
  if (!v?.judged) {
    return <span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600">AI 未判定</span>;
  }
  // AI が「知らない」と答えた組は推奨が空。適当な推奨より正直なほうが役に立つ。
  if (!v.recommendation) {
    return (
      <span className="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-700" title={v.note}>
        AI: 判断材料なし
      </span>
    );
  }
  const merge = v.recommendation === 'merge';
  return (
    <span
      className={`rounded px-2 py-0.5 text-xs ${
        merge ? 'bg-blue-100 text-blue-800' : 'bg-gray-100 text-gray-700'
      }`}
    >
      AI: {merge ? '統合を推奨' : '分けたままを推奨'}
      {v.same_composition !== undefined && (
        <span className="ml-1 opacity-75">
          （作曲{v.same_composition ? '同' : '別'}・編曲{v.same_arrangement ? '同' : '別'}）
        </span>
      )}
    </span>
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

  const scanMutation = useMutation({
    mutationFn: songApi.scanDuplicates,
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`走査に失敗しました: ${e.message}`, 'error'),
  });

  const adjudicateMutation = useMutation({
    mutationFn: songApi.adjudicateDuplicates,
    onSuccess: (r) => {
      showToast(r.message, 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`AI 判定に失敗しました: ${e.message}`, 'error'),
  });

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
          `統合元の歌唱 ${source.performance_count} 件は統合先へ移り、統合元の曲は削除されます。\n` +
          `統合元の表記は「この曲を指す別表記」として学習されます。`,
      )
    ) {
      return;
    }
    mergeMutation.mutate({ sourceId: source.id, targetId: target.id });
  };

  if (isLoading) return <Loading />;

  const candidates = data?.candidates ?? [];
  const unjudged = candidates.filter((c) => !c.verdict?.judged).length;

  return (
    <div className="mx-auto max-w-5xl p-4">
      <h1 className="mb-1 text-2xl font-bold">楽曲の重複候補</h1>
      <p className="mb-4 text-sm text-gray-600">
        同じ曲名の楽曲が複数ある組です。同じ曲なら統合し、編曲が違うなど分けておきたいものは
        「別の曲」で却下してください。却下した組は再び出てきません。
      </p>

      <div className="mb-6 flex flex-wrap items-center gap-2">
        <button
          onClick={() => scanMutation.mutate()}
          disabled={scanMutation.isPending}
          className="rounded border border-gray-300 px-3 py-1.5 text-sm hover:bg-gray-50 disabled:opacity-50"
          title="既存データを走査して同じ曲名の組を探す"
        >
          {scanMutation.isPending ? '走査中…' : '既存データを走査'}
        </button>
        <button
          onClick={() => adjudicateMutation.mutate()}
          disabled={adjudicateMutation.isPending || unjudged === 0}
          className="rounded border border-purple-300 px-3 py-1.5 text-sm text-purple-800 hover:bg-purple-50 disabled:opacity-50"
          title="同じ作曲か・編曲は同系統かを AI に判定させる（統合は実行しません）"
        >
          {adjudicateMutation.isPending ? '判定中…' : `AI に判定させる${unjudged > 0 ? `（${unjudged}件）` : ''}`}
        </button>
      </div>

      {candidates.length === 0 ? (
        <div className="rounded border border-gray-200 p-8 text-center text-gray-500">
          未処理の重複候補はありません。
        </div>
      ) : (
        <ul className="space-y-4">
          {candidates.map((c) => (
            <li key={c.id} className="rounded border border-gray-200 p-4">
              <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
                <VerdictBanner c={c} />
                <span className="rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-800">
                  {REASON_LABELS[c.reason] ?? c.reason}
                </span>
                {c.origin === 'scan' && (
                  <span className="text-xs text-gray-400">既存データの走査で検出</span>
                )}
              </div>

              {c.verdict?.note && (
                <p className="mb-3 text-sm text-gray-700">
                  {c.verdict.note}
                  <span className="ml-1 text-xs text-gray-400">
                    — AI の判断理由。心当たりのない説明なら鵜呑みにしないでください
                  </span>
                </p>
              )}

              <div className="flex flex-col gap-3 sm:flex-row">
                <SongCard
                  song={c.new_song}
                  badge={c.origin === 'scan' ? '楽曲 A' : '新しく登録された曲'}
                />
                <SongCard
                  song={c.existing_song}
                  badge={c.origin === 'scan' ? '楽曲 B' : '既存の曲'}
                />
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
