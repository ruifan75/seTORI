import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { aliasApi } from '../../api/client';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 照合の学習層の管理。
//
// ここに出るのは「文字列の比較では決められなかったので、人か AI が決めた」対応。
// とくに AI が判定したアーティストの別名義は**全楽曲の照合に効く**ので、
// 中身が見えて取り消せることが必須になる。取り消しは「別人である」という
// 人の判定として残るため、次の解析で AI が同じ組を結び直すことはない。

function SourceBadge({ source }: { source: string }) {
  const isAI = source === 'ai';
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-xs ${
        isAI ? 'bg-purple-100 text-purple-800' : 'bg-gray-100 text-gray-700'
      }`}
      title={isAI ? 'AI が同一人物と判定しました' : '人が登録しました'}
    >
      {isAI ? 'AI 判定' : '手動'}
    </span>
  );
}

function ArtistAliasSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [nameA, setNameA] = useState('');
  const [nameB, setNameB] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['aliases', 'artists'],
    queryFn: aliasApi.listArtists,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['aliases'] });

  const linkMutation = useMutation({
    mutationFn: () => aliasApi.linkArtists(nameA.trim(), nameB.trim()),
    onSuccess: () => {
      showToast('同一人物として登録しました', 'success');
      setNameA('');
      setNameB('');
      invalidate();
    },
    onError: (e: Error) => showToast(`登録に失敗しました: ${e.message}`, 'error'),
  });

  const unlinkMutation = useMutation({
    mutationFn: (nameKey: string) => aliasApi.unlinkArtist(nameKey),
    onSuccess: () => {
      showToast('別名義の登録を解除しました', 'success');
      invalidate();
    },
    onError: (e: Error) => showToast(`解除に失敗しました: ${e.message}`, 'error'),
  });

  const groups = data?.groups ?? [];

  return (
    <section className="mb-10">
      <h2 className="mb-1 text-xl font-bold">アーティストの別名義</h2>
      <p className="mb-4 text-sm text-gray-600">
        同一人物が使った別の名義をまとめます（例：荒井由実 と 松任谷由実）。
        曲名が一致してアーティストだけ違うとき、ここに登録があれば同じ曲として照合されます。
      </p>

      <div className="mb-5 flex flex-wrap items-center gap-2 rounded border border-gray-200 bg-gray-50 p-3">
        <input
          value={nameA}
          onChange={(e) => setNameA(e.target.value)}
          placeholder="例：荒井由実"
          className="w-48 rounded border border-gray-300 px-2 py-1.5 text-sm"
        />
        <span className="text-gray-500">=</span>
        <input
          value={nameB}
          onChange={(e) => setNameB(e.target.value)}
          placeholder="例：松任谷由実"
          className="w-48 rounded border border-gray-300 px-2 py-1.5 text-sm"
        />
        <button
          onClick={() => linkMutation.mutate()}
          disabled={!nameA.trim() || !nameB.trim() || linkMutation.isPending}
          className="rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
        >
          同一人物として登録
        </button>
      </div>

      {isLoading ? (
        <Loading />
      ) : groups.length === 0 ? (
        <p className="rounded border border-gray-200 p-6 text-center text-sm text-gray-500">
          登録された別名義はありません。
        </p>
      ) : (
        <ul className="space-y-2">
          {groups.map((g) => (
            <li key={g.group_id} className="rounded border border-gray-200 p-3">
              <div className="flex flex-wrap items-center gap-2">
                {g.members.map((m, i) => (
                  <span key={m.name_key} className="flex items-center gap-1.5">
                    {i > 0 && <span className="text-gray-400">=</span>}
                    <span className="font-medium">{m.display_name}</span>
                    <SourceBadge source={m.source} />
                    <button
                      onClick={() => unlinkMutation.mutate(m.name_key)}
                      disabled={unlinkMutation.isPending}
                      className="text-xs text-red-600 hover:underline disabled:opacity-50"
                      title="この名前をグループから外す（別人として記録されます）"
                    >
                      解除
                    </button>
                  </span>
                ))}
              </div>
              {g.members.find((m) => m.note) && (
                <p className="mt-1 text-xs text-gray-500">
                  {g.members.find((m) => m.note)?.note}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function SongAliasSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data, isLoading } = useQuery({
    queryKey: ['aliases', 'songs'],
    queryFn: () => aliasApi.listSongs(100),
  });

  const deleteMutation = useMutation({
    mutationFn: ({ nameKey, artistKey }: { nameKey: string; artistKey: string }) =>
      aliasApi.deleteSong(nameKey, artistKey),
    onSuccess: () => {
      showToast('学習した対応を取り消しました', 'success');
      queryClient.invalidateQueries({ queryKey: ['aliases'] });
    },
    onError: (e: Error) => showToast(`取り消しに失敗しました: ${e.message}`, 'error'),
  });

  const aliases = data?.aliases ?? [];

  return (
    <section>
      <h2 className="mb-1 text-xl font-bold">学習した楽曲の表記</h2>
      <p className="mb-4 text-sm text-gray-600">
        楽曲を統合したときに覚えた「この表記はこの曲」の対応です。
        次から同じ表記が来ると、照合を通さずここで解決します。
      </p>

      {isLoading ? (
        <Loading />
      ) : aliases.length === 0 ? (
        <p className="rounded border border-gray-200 p-6 text-center text-sm text-gray-500">
          まだ学習した表記はありません。重複候補を統合すると増えていきます。
        </p>
      ) : (
        <div className="overflow-x-auto rounded border border-gray-200">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-left text-xs text-gray-600">
              <tr>
                <th className="px-3 py-2">照合キー（曲名 / アーティスト）</th>
                <th className="px-3 py-2">解決先の楽曲</th>
                <th className="px-3 py-2 w-20"></th>
              </tr>
            </thead>
            <tbody>
              {aliases.map((a) => (
                <tr key={`${a.name_key}|${a.artist_key}`} className="border-t border-gray-100">
                  <td className="px-3 py-2 font-mono text-xs text-gray-700">
                    {a.name_key} / {a.artist_key || '（なし）'}
                  </td>
                  <td className="px-3 py-2">
                    <Link to={`/songs/${a.song_id}`} className="text-blue-600 hover:underline">
                      {a.song_name} / {a.song_artist}
                    </Link>
                  </td>
                  <td className="px-3 py-2 text-right">
                    <button
                      onClick={() =>
                        deleteMutation.mutate({ nameKey: a.name_key, artistKey: a.artist_key })
                      }
                      disabled={deleteMutation.isPending}
                      className="text-xs text-red-600 hover:underline disabled:opacity-50"
                      title="この対応を忘れさせる"
                    >
                      取り消し
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

export default function AliasesPage() {
  return (
    <div className="mx-auto max-w-5xl p-4">
      <h1 className="mb-1 text-2xl font-bold">照合の学習</h1>
      <p className="mb-8 text-sm text-gray-600">
        コメントの表記を楽曲データベースに突き合わせるとき、文字列の比較だけでは決められないものがあります。
        ここには、その判断として人と AI が決めた内容が溜まります。
      </p>
      <ArtistAliasSection />
      <SongAliasSection />
    </div>
  );
}
