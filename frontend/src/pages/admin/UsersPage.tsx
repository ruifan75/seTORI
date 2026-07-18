import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { userApi, roleApi } from '../../api/client';
import { useToast } from '../../components/ui/ToastContext';
import { useAuthStore } from '../../store/auth';
import type { AuthUser, Role, PermissionInfo, CreateUserRequest } from '../../api/types';

const ALL_PERM = '*';

// ========== ユーザー行 ==========

function UserRow({
  user,
  roles,
  isSelf,
  onUpdate,
  onChangePassword,
  onDelete,
}: {
  user: AuthUser;
  roles: Role[];
  isSelf: boolean;
  onUpdate: (id: string, req: { display_name: string; role_id: string; is_active: boolean }) => void;
  onChangePassword: (id: string, password: string) => void;
  onDelete: (id: string) => void;
}) {
  const [pwOpen, setPwOpen] = useState(false);
  const [newPw, setNewPw] = useState('');

  return (
    <div className="flex flex-wrap items-center gap-3 px-3 py-2 border rounded-lg">
      <div className="flex-1 min-w-[10rem]">
        <div className="flex items-center gap-2">
          <span className="font-medium text-gray-900">{user.username}</span>
          {isSelf && <span className="text-xs text-indigo-600">（自分）</span>}
          {!user.is_active && <span className="text-xs text-red-500">（無効）</span>}
        </div>
        <div className="text-xs text-gray-400">
          {user.display_name || '—'}
          {user.last_login && ` · 最終ログイン ${new Date(user.last_login).toLocaleString('ja-JP')}`}
        </div>
      </div>

      {/* ロール */}
      <select
        value={user.role_id}
        onChange={(e) => onUpdate(user.id, { display_name: user.display_name, role_id: e.target.value, is_active: user.is_active })}
        className="px-2 py-1 text-sm border border-gray-300 rounded-lg"
        title="ロール"
      >
        {roles.map((r) => (
          <option key={r.id} value={r.id}>{r.name}</option>
        ))}
      </select>

      {/* 有効/無効 */}
      <label className="flex items-center gap-1 text-sm text-gray-600" title={isSelf ? '自分自身は無効化できません' : '有効/無効'}>
        <input
          type="checkbox"
          checked={user.is_active}
          disabled={isSelf}
          onChange={(e) => onUpdate(user.id, { display_name: user.display_name, role_id: user.role_id, is_active: e.target.checked })}
          className="h-4 w-4 disabled:opacity-40"
        />
        有効
      </label>

      <button
        onClick={() => setPwOpen((v) => !v)}
        className="text-sm text-indigo-600 hover:text-indigo-800"
      >
        パスワード
      </button>
      <button
        onClick={() => onDelete(user.id)}
        disabled={isSelf}
        className="text-sm text-red-600 hover:text-red-800 disabled:opacity-40"
        title={isSelf ? '自分自身は削除できません' : '削除'}
      >
        削除
      </button>

      {pwOpen && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (newPw.length < 4) return;
            onChangePassword(user.id, newPw);
            setNewPw('');
            setPwOpen(false);
          }}
          className="w-full flex items-center gap-2 pt-2 border-t mt-1"
        >
          <input
            type="password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            placeholder="新しいパスワード（4文字以上）"
            className="flex-1 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
            autoFocus
          />
          <button type="submit" disabled={newPw.length < 4}
            className="px-3 py-1.5 text-sm text-white rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50">
            変更
          </button>
          <button type="button" onClick={() => { setPwOpen(false); setNewPw(''); }}
            className="px-3 py-1.5 text-sm text-gray-600 hover:text-gray-800">
            取消
          </button>
        </form>
      )}
    </div>
  );
}

function UsersSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();
  const me = useAuthStore((s) => s.user);

  const { data: users = [], isLoading } = useQuery({ queryKey: ['users'], queryFn: userApi.list });
  const { data: roles = [] } = useQuery({ queryKey: ['roles'], queryFn: roleApi.list });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['users'] });

  const createMutation = useMutation({
    mutationFn: (req: CreateUserRequest) => userApi.create(req),
    onSuccess: () => { invalidate(); showToast('ユーザーを作成しました', 'success'); },
    onError: (err: Error) => showToast(`作成エラー: ${err.message}`, 'error'),
  });
  const updateMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: { display_name: string; role_id: string; is_active: boolean } }) => userApi.update(id, req),
    onSuccess: () => invalidate(),
    onError: (err: Error) => showToast(`更新エラー: ${err.message}`, 'error'),
  });
  const passwordMutation = useMutation({
    mutationFn: ({ id, password }: { id: string; password: string }) => userApi.changePassword(id, password),
    onSuccess: () => showToast('パスワードを変更しました', 'success'),
    onError: (err: Error) => showToast(`変更エラー: ${err.message}`, 'error'),
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => userApi.delete(id),
    onSuccess: () => { invalidate(); showToast('ユーザーを削除しました', 'success'); },
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });

  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [roleId, setRoleId] = useState('');

  const handleCreate = (e: React.FormEvent) => {
    e.preventDefault();
    const rid = roleId || roles[0]?.id;
    if (!username.trim() || password.length < 4 || !rid) return;
    createMutation.mutate(
      { username: username.trim(), display_name: displayName.trim(), password, role_id: rid, is_active: true },
      { onSuccess: () => { setUsername(''); setDisplayName(''); setPassword(''); setRoleId(''); } }
    );
  };

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">ユーザー管理</h2>
      <p className="text-gray-500 mb-6">
        ログインアカウントを管理します。ロールで編集権限が決まります。未ログインのユーザーは閲覧のみ可能です。
      </p>

      {isLoading ? (
        <p className="text-gray-400">読み込み中...</p>
      ) : (
        <div className="space-y-2 mb-6">
          {users.map((u) => (
            <UserRow
              key={u.id}
              user={u}
              roles={roles}
              isSelf={me?.id === u.id}
              onUpdate={(id, req) => updateMutation.mutate({ id, req })}
              onChangePassword={(id, pw) => passwordMutation.mutate({ id, password: pw })}
              onDelete={(id) => deleteMutation.mutate(id)}
            />
          ))}
        </div>
      )}

      <form onSubmit={handleCreate} className="flex flex-wrap gap-2 items-center pt-4 border-t">
        <input
          type="text" value={username} onChange={(e) => setUsername(e.target.value)}
          placeholder="ユーザー名" autoComplete="off"
          className="w-40 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
        />
        <input
          type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)}
          placeholder="表示名（任意）"
          className="w-40 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
        />
        <input
          type="password" value={password} onChange={(e) => setPassword(e.target.value)}
          placeholder="パスワード（4文字以上）" autoComplete="new-password"
          className="w-48 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
        />
        <select
          value={roleId || roles[0]?.id || ''}
          onChange={(e) => setRoleId(e.target.value)}
          className="px-2 py-1.5 text-sm border border-gray-300 rounded-lg"
        >
          {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
        <button
          type="submit"
          disabled={createMutation.isPending || !username.trim() || password.length < 4}
          className="px-4 py-1.5 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
        >
          ユーザー追加
        </button>
      </form>
    </div>
  );
}

// ========== ロール ==========

function RoleCard({
  role,
  permissions,
  onSave,
  onDelete,
  isSaving,
}: {
  role: Role;
  permissions: PermissionInfo[];
  onSave: (id: string, description: string, perms: string[]) => void;
  onDelete: (id: string) => void;
  isSaving: boolean;
}) {
  const [description, setDescription] = useState(role.description);
  const [selected, setSelected] = useState<string[]>(role.permissions ?? []);

  const hasAll = selected.includes(ALL_PERM);
  const dirty =
    description !== role.description ||
    JSON.stringify([...selected].sort()) !== JSON.stringify([...(role.permissions ?? [])].sort());

  const toggle = (key: string) => {
    setSelected((prev) => (prev.includes(key) ? prev.filter((p) => p !== key) : [...prev, key]));
  };

  const regularPerms = permissions.filter((p) => p.key !== ALL_PERM);
  const allPerm = permissions.find((p) => p.key === ALL_PERM);

  return (
    <div className="border rounded-lg p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-gray-900">{role.name}</span>
          {role.is_system && <span className="text-xs px-1.5 py-0.5 bg-gray-100 text-gray-500 rounded">組み込み</span>}
        </div>
        {!role.is_system && (
          <button onClick={() => onDelete(role.id)} className="text-sm text-red-600 hover:text-red-800">削除</button>
        )}
      </div>

      <input
        type="text"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="説明"
        className="w-full mb-3 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
      />

      <div className="space-y-1.5 mb-3">
        {allPerm && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={hasAll} onChange={() => toggle(ALL_PERM)} className="h-4 w-4" />
            <span className="font-medium">{allPerm.description}</span>
          </label>
        )}
        {regularPerms.map((p) => (
          <label key={p.key} className={`flex items-center gap-2 text-sm ${hasAll ? 'opacity-40' : ''}`}>
            <input
              type="checkbox"
              checked={hasAll || selected.includes(p.key)}
              disabled={hasAll}
              onChange={() => toggle(p.key)}
              className="h-4 w-4"
            />
            <span className="text-gray-700">{p.description}</span>
            <code className="text-[11px] text-gray-400">{p.key}</code>
          </label>
        ))}
      </div>

      <button
        onClick={() => onSave(role.id, description, selected)}
        disabled={!dirty || isSaving}
        className="px-3 py-1.5 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40"
      >
        保存
      </button>
    </div>
  );
}

function RolesSection() {
  const queryClient = useQueryClient();
  const { showToast } = useToast();

  const { data: roles = [], isLoading } = useQuery({ queryKey: ['roles'], queryFn: roleApi.list });
  const { data: permissions = [] } = useQuery({ queryKey: ['permissions'], queryFn: roleApi.listPermissions });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['roles'] });
    queryClient.invalidateQueries({ queryKey: ['users'] });
  };

  const updateMutation = useMutation({
    mutationFn: ({ id, description, perms }: { id: string; description: string; perms: string[] }) =>
      roleApi.update(id, description, perms),
    onSuccess: () => { invalidate(); showToast('ロールを更新しました', 'success'); },
    onError: (err: Error) => showToast(`更新エラー: ${err.message}`, 'error'),
  });
  const createMutation = useMutation({
    mutationFn: ({ name, description, perms }: { name: string; description: string; perms: string[] }) =>
      roleApi.create(name, description, perms),
    onSuccess: () => { invalidate(); showToast('ロールを作成しました', 'success'); },
    onError: (err: Error) => showToast(`作成エラー: ${err.message}`, 'error'),
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => roleApi.delete(id),
    onSuccess: () => { invalidate(); showToast('ロールを削除しました', 'success'); },
    onError: (err: Error) => showToast(`削除エラー: ${err.message}`, 'error'),
  });

  const [newName, setNewName] = useState('');

  return (
    <div className="bg-white rounded-lg shadow-sm border p-6">
      <h2 className="text-xl font-bold text-gray-900 mb-2">ロール・権限管理</h2>
      <p className="text-gray-500 mb-6">
        ロールごとに許可する操作（権限）を設定します。組み込みロール（admin / editor / viewer）は削除できませんが、権限は調整できます。
      </p>

      {isLoading ? (
        <p className="text-gray-400">読み込み中...</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 mb-6">
          {roles.map((r) => (
            <RoleCard
              key={r.id}
              role={r}
              permissions={permissions}
              onSave={(id, description, perms) => updateMutation.mutate({ id, description, perms })}
              onDelete={(id) => deleteMutation.mutate(id)}
              isSaving={updateMutation.isPending}
            />
          ))}
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!newName.trim()) return;
          createMutation.mutate(
            { name: newName.trim(), description: '', perms: [] },
            { onSuccess: () => setNewName('') }
          );
        }}
        className="flex gap-2 items-center pt-4 border-t"
      >
        <input
          type="text" value={newName} onChange={(e) => setNewName(e.target.value)}
          placeholder="新しいロール名（例: moderator）"
          className="w-56 px-3 py-1.5 text-sm border border-gray-300 rounded-lg"
        />
        <button
          type="submit"
          disabled={createMutation.isPending || !newName.trim()}
          className="px-4 py-1.5 text-sm text-white font-medium rounded-lg bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50"
        >
          ロール追加
        </button>
      </form>
    </div>
  );
}

export default function UsersPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold text-gray-900">ユーザー・権限</h1>
      <UsersSection />
      <RolesSection />
    </div>
  );
}
