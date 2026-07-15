import { create } from 'zustand';
import { authApi, setAuthToken, setUnauthorizedHandler } from '../api/client';
import type { AuthUser } from '../api/types';

const TOKEN_KEY = 'setori_token';

// 権限キー（バックエンドの pkg/auth/permissions.go と対応）
export const PERM = {
  ALL: '*',
  CONTENT_EDIT: 'content:edit',
  SYNC_RUN: 'sync:run',
  AI_MANAGE: 'ai:manage',
  LOGS_VIEW: 'logs:view',
  USERS_MANAGE: 'users:manage',
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
  logout: () => Promise<void>;
  init: () => Promise<void>;
  can: (permission: string) => boolean;
}

function clearLocalSession(set: (partial: Partial<AuthState>) => void) {
  localStorage.removeItem(TOKEN_KEY);
  setAuthToken(null);
  set({ token: null, user: null, status: 'anonymous' });
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  status: 'loading',

  login: async (username, password) => {
    const { token, user } = await authApi.login(username, password);
    localStorage.setItem(TOKEN_KEY, token);
    setAuthToken(token);
    set({ token, user, status: 'authenticated' });
  },

  logout: async () => {
    try {
      await authApi.logout();
    } catch {
      // トークンが既に無効でもローカルは必ずクリアする
    }
    clearLocalSession(set);
  },

  // アプリ起動時：保存済みトークンがあれば /me で検証してユーザーを復元する
  init: async () => {
    const token = localStorage.getItem(TOKEN_KEY);
    if (!token) {
      set({ status: 'anonymous' });
      return;
    }
    setAuthToken(token);
    try {
      const user = await authApi.me();
      set({ token, user, status: 'authenticated' });
    } catch {
      clearLocalSession(set);
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
