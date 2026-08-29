import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { singerApi } from '../api/client';
import type { Singer } from '../api/types';
import Loading from '../components/ui/Loading';
import Pagination from '../components/ui/Pagination';
import { SortControl, type SortDir, type SortState } from '../components/ui/Sort';
import { useToast } from '../components/ui/ToastContext';
import VisibilityIcon from '../components/ui/VisibilityIcon';
import { useAuthStore, hasPermission, PERM } from '../store/auth';

type ViewMode = 'group' | 'list';

export default function SingersPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  // 既定は事務所別。名前順で通しで見たいときだけ一覧に切り替える。
  const view: ViewMode = searchParams.get('view') === 'list' ? 'list' : 'group';
  const page = parseInt(searchParams.get('page') || '1');
  const sort = searchParams.get('sort') || 'name';
  const dir: SortDir = searchParams.get('dir') === 'desc' ? 'desc' : 'asc';
  const canEdit = hasPermission(useAuthStore((s) => s.user), PERM.CONTENT_EDIT);
  const [showAddModal, setShowAddModal] = useState(false);
  const [channelInput, setChannelInput] = useState('');

  // 非表示チャンネルは閲覧者には出さない。権限があるときだけ一覧に含めて印を付ける。
  const includeHidden = canEdit;

  const groupedQuery = useQuery({
    queryKey: ['singers', 'grouped', includeHidden],
    queryFn: () => singerApi.listGrouped(includeHidden),
    enabled: view === 'group',
  });

  const listQuery = useQuery({
    queryKey: ['singers', page, sort, dir, includeHidden],
    queryFn: () => singerApi.list(page, 20, sort, dir, includeHidden),
    enabled: view === 'list',
  });

  const isLoading = view === 'group' ? groupedQuery.isLoading : listQuery.isLoading;
  const total = view === 'group' ? groupedQuery.data?.total : listQuery.data?.pagination.total;

  const buildParams = (next: { view?: ViewMode; page?: number; sort?: string; dir?: SortDir }) => {
    const params: Record<string, string> = {};
    const v = next.view ?? view;
    const p = next.page ?? page;
    const so = next.sort ?? sort;
    const d = next.dir ?? dir;
    if (v !== 'group') params.view = v;
    // ページ・並び替えは一覧表示だけの概念なので、事務所別では URL に残さない
    if (v === 'list') {
      if (p > 1) params.page = String(p);
      if (so !== 'name') params.sort = so;
      if (d !== 'asc') params.dir = d;
    }
    return params;
  };

  const handleSort = (next: SortState) => {
    setSearchParams(buildParams({ sort: next.sort, dir: next.dir, page: 1 }));
  };

  const handlePageChange = (newPage: number) => {
    setSearchParams(buildParams({ page: newPage }));
  };

  const syncMutation = useMutation({
    mutationFn: (channelId: string) => singerApi.create(channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      setShowAddModal(false);
      setChannelInput('');
    },
  });

  const visibilityMutation = useMutation({
    mutationFn: ({ id, isHidden }: { id: string; isHidden: boolean }) =>
      singerApi.setHidden(id, isHidden),
    onSuccess: (_, { isHidden }) => {
      queryClient.invalidateQueries({ queryKey: ['singers'] });
      showToast(
        isHidden ? 'チャンネルを一覧から非表示にしました' : 'チャンネルを一覧に表示しました',
        'success'
      );
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const handleAddSinger = (e: React.FormEvent) => {
    e.preventDefault();
    if (!channelInput.trim()) return;

    // 入力から Channel ID または @handle を抽出
    let channelValue = channelInput.trim();

    // YouTube URL の場合は Channel ID を抽出
    const channelUrlMatch = channelValue.match(/youtube\.com\/channel\/([^/?#]+)/);
    if (channelUrlMatch) {
      channelValue = channelUrlMatch[1];
    }

    const handleUrlMatch = channelValue.match(/youtube\.com\/@([^/?#]+)/);
    if (handleUrlMatch) {
      channelValue = `@${handleUrlMatch[1]}`;
    }

    syncMutation.mutate(channelValue);
  };

  const renderCard = (singer: Singer) => (
    <SingerCard
      key={singer.id}
      singer={singer}
      canEdit={canEdit}
      onToggleHidden={() =>
        visibilityMutation.mutate({ id: singer.id, isHidden: !singer.is_hidden })
      }
      toggling={visibilityMutation.isPending && visibilityMutation.variables?.id === singer.id}
    />
  );

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-3xl font-bold text-gray-900">チャンネル一覧</h1>

        {canEdit && (
          <button
            onClick={() => setShowAddModal(true)}
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors flex items-center gap-2"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
            チャンネルを追加
          </button>
        )}
      </div>

      {isLoading ? (
        <Loading />
      ) : (
        <>
          {total === 0 ? (
            <div className="text-center py-12 text-gray-500">
              チャンネルがありません。右上のボタンから追加してください。
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between gap-3 flex-wrap">
                <div className="text-sm text-gray-500">{total}件のチャンネル</div>
                <div className="flex items-center gap-3">
                  {view === 'list' && (
                    <SortControl
                      options={[
                        { value: 'name', label: '名前', firstDir: 'asc' },
                        { value: 'organization', label: '事務所', firstDir: 'asc' },
                      ]}
                      sort={sort}
                      dir={dir}
                      onSort={handleSort}
                    />
                  )}
                  {/* 表示切替：事務所別（既定） / 名前順の通し一覧 */}
                  <div className="flex rounded-lg border border-gray-300 overflow-hidden text-sm">
                    <button
                      onClick={() => setSearchParams(buildParams({ view: 'group' }))}
                      title="事務所別に表示"
                      className={`px-3 py-1.5 ${
                        view === 'group' ? 'bg-indigo-600 text-white' : 'bg-white text-gray-600 hover:bg-gray-50'
                      }`}
                    >
                      事務所別
                    </button>
                    <button
                      onClick={() => setSearchParams(buildParams({ view: 'list' }))}
                      title="全チャンネルを通しで表示"
                      className={`px-3 py-1.5 border-l border-gray-300 ${
                        view === 'list' ? 'bg-indigo-600 text-white' : 'bg-white text-gray-600 hover:bg-gray-50'
                      }`}
                    >
                      一覧
                    </button>
                  </div>
                </div>
              </div>

              {view === 'group' ? (
                <div className="space-y-8">
                  {groupedQuery.data?.groups.map((group) => (
                    <section key={group.organization || '__none__'} className="space-y-3">
                      <div className="flex items-baseline gap-2 border-b border-gray-200 pb-2">
                        <h2 className="text-lg font-semibold text-gray-900">
                          {group.display_name || group.organization || '所属なし'}
                        </h2>
                        <span className="text-sm text-gray-500">{group.singers.length}</span>
                      </div>
                      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                        {group.singers.map(renderCard)}
                      </div>
                    </section>
                  ))}
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {listQuery.data?.singers.map(renderCard)}
                  </div>

                  {listQuery.data && (
                    <Pagination
                      page={page}
                      totalPages={listQuery.data.pagination.total_pages}
                      onPageChange={handlePageChange}
                    />
                  )}
                </>
              )}
            </>
          )}
        </>
      )}

      {/* Add Singer Modal */}
      {showAddModal && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
            <h2 className="text-xl font-bold text-gray-900 mb-4">チャンネルを追加</h2>
            <form onSubmit={handleAddSinger}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  YouTube Channel ID / Handle
                </label>
                <input
                  type="text"
                  value={channelInput}
                  onChange={(e) => setChannelInput(e.target.value)}
                  placeholder="UCxxxxxxxxxxxxxxxxxxxxxx または @handle"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                />
                <p className="mt-2 text-sm text-gray-500">
                  YouTube Channel URL、@handle、またはChannel ID を入力してください
                </p>
              </div>

              {syncMutation.isError && (
                <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">
                  {(syncMutation.error as Error).message}
                </div>
              )}

              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => {
                    setShowAddModal(false);
                    setChannelInput('');
                  }}
                  className="px-4 py-2 text-gray-700 hover:text-gray-900"
                >
                  キャンセル
                </button>
                <button
                  type="submit"
                  disabled={syncMutation.isPending || !channelInput.trim()}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
                >
                  {syncMutation.isPending ? '追加中...' : 'チャンネルを追加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

// SingerCard は一覧のカード1枚。非表示チャンネルは content:edit を持つ利用者にだけ
// 薄く表示され、カード上のアイコンから表示/非表示を切り替えられる。
function SingerCard({
  singer,
  canEdit,
  onToggleHidden,
  toggling,
}: {
  singer: Singer;
  canEdit: boolean;
  onToggleHidden: () => void;
  toggling: boolean;
}) {
  return (
    <div className="relative">
      <Link
        to={`/singers/${singer.id}`}
        className={`bg-white rounded-lg shadow-sm border p-4 hover:shadow-md transition-shadow flex items-center gap-4 ${
          singer.is_hidden ? 'opacity-60' : ''
        }`}
      >
        {singer.photo_url ? (
          <img
            src={singer.photo_url}
            alt={singer.name}
            className="w-16 h-16 rounded-full object-cover"
            onError={(e) => {
              e.currentTarget.onerror = null;
              e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
            }}
          />
        ) : (
          <div className="w-16 h-16 rounded-full bg-gray-200 flex items-center justify-center">
            <svg className="w-8 h-8 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
          </div>
        )}
        <div className="flex-1 min-w-0">
          <h3 className="font-medium text-gray-900 truncate">{singer.name}</h3>
          {singer.english_name && (
            <p className="text-sm text-gray-500 truncate">{singer.english_name}</p>
          )}
          <div className="flex flex-wrap items-center gap-1 mt-1">
            {/* 「所属なし」を意味する分類（Independents など）は organization_name が空で、
                バッジも出ない。見出しが「所属なし」なのにバッジは別名、という矛盾を避けるため */}
            {singer.organization_name && (
              <span className="inline-block px-2 py-0.5 bg-purple-100 text-purple-700 text-xs rounded-full">
                {singer.organization_name}
              </span>
            )}
            {singer.is_hidden && (
              <span className="inline-block px-2 py-0.5 bg-gray-200 text-gray-600 text-xs rounded-full">
                非表示
              </span>
            )}
            {/* 会限を持っていて、まだ配信主に訊いていないチャンネル。
                **これは作業一覧**なので、決着済み（公開可／非公開）は出さない ──
                出すと「残っているのはどれか」が読み取れなくなる。
                本数と方針は content:edit のときだけ返るので、権限判定は要らない */}
            {(singer.members_only_stream_count ?? 0) > 0 && !singer.members_only_policy && (
              <span
                className="inline-block px-2 py-0.5 bg-amber-50 text-amber-700 border border-amber-300 text-xs rounded-full"
                title={`会限配信 ${singer.members_only_stream_count} 本。セットリストを公開してよいか配信主に未確認（伏せたまま）`}
              >
                会限 未確認（{singer.members_only_stream_count}）
              </span>
            )}
          </div>
        </div>
        <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
      </Link>

      {canEdit && (
        <button
          onClick={onToggleHidden}
          disabled={toggling}
          title={singer.is_hidden ? '一覧に表示する' : '一覧から非表示にする'}
          aria-label={singer.is_hidden ? '一覧に表示する' : '一覧から非表示にする'}
          className="absolute top-2 right-2 p-1.5 rounded-full bg-white/90 border border-gray-200 text-gray-400 hover:text-gray-700 hover:border-gray-300 disabled:opacity-50"
        >
          <VisibilityIcon hidden={singer.is_hidden} />
        </button>
      )}
    </div>
  );
}

