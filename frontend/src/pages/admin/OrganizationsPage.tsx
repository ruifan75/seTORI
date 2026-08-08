import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { organizationApi } from '../../api/client';
import type { Organization } from '../../api/types';
import Loading from '../../components/ui/Loading';
import { useToast } from '../../components/ui/ToastContext';

// 事務所の管理画面。
//
// key は取り込み時の生の値（Holodex の org）なので変更できない。編集できるのは
// 表示名と並び順だけ。Holodex が新しい org を返すと取り込み時に
// display_name = key で自動作成されるので、この画面の主な仕事は
// 「自動で入った表示名を読める名前に直す」こと（hololive → ホロライブ など）。
export default function OrganizationsPage() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editForm, setEditForm] = useState({ display_name: '', sort_order: 0 });
  const [showAdd, setShowAdd] = useState(false);
  const [addForm, setAddForm] = useState({ key: '', display_name: '', sort_order: 0 });

  const { data, isLoading } = useQuery({
    queryKey: ['organizations'],
    queryFn: organizationApi.list,
  });

  // 事務所を変えるとチャンネル一覧の見出し・バッジ・並びが全部変わるので、
  // singers 系のキャッシュもまとめて捨てる。
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['organizations'] });
    queryClient.invalidateQueries({ queryKey: ['singers'] });
    queryClient.invalidateQueries({ queryKey: ['singer'] });
  };

  const updateMutation = useMutation({
    mutationFn: ({ key, ...req }: { key: string; display_name: string; sort_order: number }) =>
      organizationApi.update(key, req),
    onSuccess: () => {
      invalidate();
      setEditingKey(null);
      showToast('事務所を更新しました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const createMutation = useMutation({
    mutationFn: organizationApi.create,
    onSuccess: () => {
      invalidate();
      setShowAdd(false);
      setAddForm({ key: '', display_name: '', sort_order: 0 });
      showToast('事務所を追加しました', 'success');
    },
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const deleteMutation = useMutation({
    mutationFn: organizationApi.remove,
    onSuccess: () => {
      invalidate();
      showToast('事務所を削除しました', 'success');
    },
    // 所属チャンネルが残っていると 409。理由がそのまま出るのでメッセージを流す。
    onError: (err: Error) => showToast(err.message, 'error'),
  });

  const startEdit = (org: Organization) => {
    setEditingKey(org.key);
    setEditForm({ display_name: org.display_name, sort_order: org.sort_order });
  };

  const organizations = data?.organizations ?? [];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap justify-between items-center gap-3">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">事務所</h1>
          <p className="mt-1 text-sm text-gray-500">
            キーは取り込み時の値（Holodex の org）なので変更できません。表示名と並び順を調整できます。
          </p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors flex items-center gap-2"
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          事務所を追加
        </button>
      </div>

      {isLoading ? (
        <Loading />
      ) : organizations.length === 0 ? (
        <div className="text-center py-12 text-gray-500">事務所がありません。</div>
      ) : (
        <div className="bg-white rounded-lg shadow-sm border overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-left text-gray-500">
              <tr>
                <th className="px-4 py-3 font-medium">表示名</th>
                <th className="px-4 py-3 font-medium">キー（取り込み時の値）</th>
                <th className="px-4 py-3 font-medium w-24">並び順</th>
                <th className="px-4 py-3 font-medium w-28">チャンネル</th>
                <th className="px-4 py-3 font-medium w-24"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {organizations.map((org) => {
                const editing = editingKey === org.key;
                return (
                  <tr key={org.key} className="hover:bg-gray-50">
                    <td className="px-4 py-2">
                      {editing ? (
                        <input
                          type="text"
                          autoFocus
                          value={editForm.display_name}
                          onChange={(e) =>
                            setEditForm((prev) => ({ ...prev, display_name: e.target.value }))
                          }
                          className="w-full px-2 py-1 border border-gray-300 rounded focus:ring-2 focus:ring-indigo-500"
                        />
                      ) : (
                        <span className="font-medium text-gray-900">{org.display_name}</span>
                      )}
                    </td>
                    <td className="px-4 py-2">
                      <code className="text-xs text-gray-500">{org.key}</code>
                      {/* 表示名を直していない＝自動作成のままであることが一目で分かるように */}
                      {org.display_name === org.key && (
                        <span className="ml-2 text-xs text-amber-600">未設定</span>
                      )}
                    </td>
                    <td className="px-4 py-2">
                      {editing ? (
                        <input
                          type="number"
                          value={editForm.sort_order}
                          onChange={(e) =>
                            setEditForm((prev) => ({
                              ...prev,
                              sort_order: parseInt(e.target.value) || 0,
                            }))
                          }
                          className="w-20 px-2 py-1 border border-gray-300 rounded focus:ring-2 focus:ring-indigo-500"
                        />
                      ) : (
                        <span className="text-gray-500">{org.sort_order}</span>
                      )}
                    </td>
                    <td className="px-4 py-2 text-gray-500">{org.singer_count}</td>
                    <td className="px-4 py-2">
                      <div className="flex items-center gap-1 justify-end">
                        {editing ? (
                          <>
                            <button
                              onClick={() =>
                                updateMutation.mutate({ key: org.key, ...editForm })
                              }
                              disabled={updateMutation.isPending || !editForm.display_name.trim()}
                              title="保存"
                              aria-label="保存"
                              className="p-1.5 rounded text-green-600 hover:bg-green-50 disabled:opacity-50"
                            >
                              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                              </svg>
                            </button>
                            <button
                              onClick={() => setEditingKey(null)}
                              title="キャンセル"
                              aria-label="キャンセル"
                              className="p-1.5 rounded text-gray-400 hover:bg-gray-100"
                            >
                              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                              </svg>
                            </button>
                          </>
                        ) : (
                          <>
                            <button
                              onClick={() => startEdit(org)}
                              title="表示名・並び順を編集"
                              aria-label="表示名・並び順を編集"
                              className="p-1.5 rounded text-gray-400 hover:text-indigo-600 hover:bg-indigo-50"
                            >
                              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931z" />
                              </svg>
                            </button>
                            {/* 所属チャンネルがある事務所はサーバー側が 409 で断るので、
                                ここでも押せないようにして往復を省く */}
                            <button
                              onClick={() => deleteMutation.mutate(org.key)}
                              disabled={org.singer_count > 0 || deleteMutation.isPending}
                              title={
                                org.singer_count > 0
                                  ? '所属チャンネルがあるため削除できません'
                                  : '削除'
                              }
                              aria-label="削除"
                              className="p-1.5 rounded text-gray-400 hover:text-red-600 hover:bg-red-50 disabled:opacity-30 disabled:hover:text-gray-400 disabled:hover:bg-transparent"
                            >
                              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                              </svg>
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {showAdd && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
            <h2 className="text-xl font-bold text-gray-900 mb-4">事務所を追加</h2>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (!addForm.display_name.trim()) return;
                createMutation.mutate(addForm);
              }}
              className="space-y-4"
            >
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">表示名</label>
                <input
                  type="text"
                  autoFocus
                  value={addForm.display_name}
                  onChange={(e) => setAddForm((prev) => ({ ...prev, display_name: e.target.value }))}
                  placeholder="ホロライブ"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  キー（任意）
                </label>
                <input
                  type="text"
                  value={addForm.key}
                  onChange={(e) => setAddForm((prev) => ({ ...prev, key: e.target.value }))}
                  placeholder="省略すると表示名がそのままキーになります"
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500"
                />
                <p className="mt-1 text-xs text-gray-500">
                  Holodex が返す org と揃えたい場合だけ指定してください。あとから変更できません。
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">並び順</label>
                <input
                  type="number"
                  value={addForm.sort_order}
                  onChange={(e) =>
                    setAddForm((prev) => ({ ...prev, sort_order: parseInt(e.target.value) || 0 }))
                  }
                  className="w-24 px-3 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-indigo-500"
                />
                <p className="mt-1 text-xs text-gray-500">小さいほど先。同じなら表示名順。</p>
              </div>

              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setShowAdd(false)}
                  className="px-4 py-2 text-gray-700 hover:text-gray-900"
                >
                  キャンセル
                </button>
                <button
                  type="submit"
                  disabled={createMutation.isPending || !addForm.display_name.trim()}
                  className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-50"
                >
                  {createMutation.isPending ? '追加中...' : '追加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
