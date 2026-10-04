import { create } from 'zustand';
import { authApi, setAuthToken, setUnauthorizedHandler } from '../api/client';
import type { AuthUser } from '../api/types';

import { applyViewerChange, viewerKey } from '../queryClient';

const TOKEN_KEY = 'setori_token';
// 認証中も logout / 別の認証が起きる。最後に開始した操作だけがセッションを確立できる。
let sessionRevision = 0;

// 権限キー（バックエンドの pkg/auth/permissions.go と対応）
export const PERM = {
  ALL: '*',
  CONTENT_EDIT: 'content:edit',
  SYNC_RUN: 'sync:run',
  HOLODEX_UPLOAD: 'holodex:upload',
  AI_MANAGE: 'ai:manage',
  LOGS_VIEW: 'logs:view',
  USERS_MANAGE: 'users:manage',
  BACKUP_MANAGE: 'backup:manage',
} as const;

// hasPermission はユーザーが指定権限を持つか判定する純関数（コンポーネントの reactive 判定用）。
export function hasPermission(user: AuthUser | null, permission: string): boolean {
  if (!user || !user.permissions) return false;
  return user.permissions.includes(PERM.ALL) || user.permissions.includes(permission);
}

interface AuthState {
  token: string | null;
  user: AuthUser | null;
  status: 'loading' | 'authenticated' | 'anonymous';
  login: (username: string, password: string) => Promise<void>;
  /** OAuth コールバックで受け取った引き換えコードからセッションを確立する */
  loginWithOAuthCode: (code: string) => Promise<void>;
  logout: () => Promise<void>;
  init: () => Promise<void>;
  can: (permission: string) => boolean;
}

function clearLocalSession(set: (partial: Partial<AuthState>) => void) {
  sessionRevision++;
  localStorage.removeItem(TOKEN_KEY);
  setAuthToken(null);
  set({ token: null, user: null, status: 'anonymous' });
  applyViewerChange(null, true);
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  status: 'loading',

  login: async (username, password) => {
    const revision = ++sessionRevision;
    const { token, user } = await authApi.login(username, password);
    if (revision !== sessionRevision) throw new Error('ログイン処理は取り消されました');
    localStorage.setItem(TOKEN_KEY, token);
    setAuthToken(token);
    set({ token, user, status: 'authenticated' });
    applyViewerChange(viewerKey(user.id, user.permissions));
  },

  loginWithOAuthCode: async (code) => {
    const revision = ++sessionRevision;
    const { token, user } = await authApi.exchangeOAuthCode(code);
    if (revision !== sessionRevision) throw new Error('ログイン処理は取り消されました');
    localStorage.setItem(TOKEN_KEY, token);
    setAuthToken(token);
    set({ token, user, status: 'authenticated' });
    applyViewerChange(viewerKey(user.id, user.permissions));
  },

  logout: async () => {
    const token = get().token ?? localStorage.getItem(TOKEN_KEY);
    // 通信を待つ間にも秘匿の表示・資格情報を残さない。
    clearLocalSession(set);
    try {
      if (token) await authApi.logout(token);
    } catch {
      // トークンが既に無効でもローカルは必ずクリアする
    }
  },

  // アプリ起動時：保存済みトークンがあれば /me で検証してユーザーを復元する
  init: async () => {
    const revision = ++sessionRevision;
    const token = localStorage.getItem(TOKEN_KEY);
    if (!token) {
      clearLocalSession(set);
      return;
    }
    setAuthToken(token);
    try {
      const user = await authApi.me();
      if (revision !== sessionRevision) return;
      set({ token, user, status: 'authenticated' });
      applyViewerChange(viewerKey(user.id, user.permissions));
    } catch {
      if (revision === sessionRevision) clearLocalSession(set);
    }
  },

  can: (permission) => {
    const user = get().user;
    if (!user || !user.permissions) return false;
    return user.permissions.includes(PERM.ALL) || user.permissions.includes(permission);
  },
}));

// セッション失効（バックエンドが 401 を返した）時は自動でログアウト状態にする
setUnauthorizedHandler(() => {
  if (!useAuthStore.getState().token) return; // もともと未ログインなら何もしない
  clearLocalSession((partial) => useAuthStore.setState(partial));
});

// localStorage はタブ間で共有されるが、store と QueryClient は共有されない。
// 別タブの logout / アカウント変更でも、旧視点のコピーを先に捨ててから検証する。
if (typeof window !== 'undefined') window.addEventListener('storage', (event) => {
  if (event.storageArea !== localStorage || (event.key !== TOKEN_KEY && event.key !== null)) return;
  sessionRevision++;
  setAuthToken(null);
  const token = localStorage.getItem(TOKEN_KEY);
  useAuthStore.setState({ token: null, user: null, status: token ? 'loading' : 'anonymous' });
  applyViewerChange(null, true);
  if (token) void useAuthStore.getState().init();
});
