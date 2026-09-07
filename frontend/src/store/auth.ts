import { create } from 'zustand';
import { authApi, setAuthToken, setUnauthorizedHandler } from '../api/client';
import type { AuthUser } from '../api/types';

import { resetQueryCacheForAuthChange } from '../queryClient';
import { usePlayerStore } from './player';

const TOKEN_KEY = 'setori_token';

// 権限キー（バックエンドの pkg/auth/permissions.go と対応）
export const PERM = {
  ALL: '*',
  CONTENT_EDIT: 'content:edit',
  SYNC_RUN: 'sync:run',
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
  localStorage.removeItem(TOKEN_KEY);
  setAuthToken(null);
  set({ token: null, user: null, status: 'anonymous' });
  // **キャッシュも捨てる。** 応答の中身は権限で変わる（秘匿された配信の歌唱は
  // `restricted:view` を持つ人にしか返らない）ので、権限が変わったのに
  // 前の結果が残っていると、ログアウト後も admin の視界が見える。
  resetQueryCacheForAuthChange();
  // **キャッシュの外へコピーされたものも捨てる。** 再生キューは曲名・歌手・
  // 配信タイトルを**複製して**持つので、query を取り直しても残る。
  //
  // 代償：セッションが切れると再生も止まる。公開の曲だけを聴いていた人には
  // 損なので、**キューが秘匿を含むかで判断したい**ところだが、キューは
  // 秘匿かどうかを持っていない ── 持たせると今度はそれが判定の二重化になる。
  // 止まるほうを選ぶ（キューはリロードでも消えるので、期待値としても近い）。
  usePlayerStore.getState().clear();
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
    // **ログインでも捨てる。** 匿名で見ていた結果（秘匿を落とした 0 件など）が
    // 残っていると、権限を得たのに前の視界のままになる。
    resetQueryCacheForAuthChange();
  },

  loginWithOAuthCode: async (code) => {
    const { token, user } = await authApi.exchangeOAuthCode(code);
    localStorage.setItem(TOKEN_KEY, token);
    setAuthToken(token);
    set({ token, user, status: 'authenticated' });
    resetQueryCacheForAuthChange();
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
      // 起動時の復元。`status: 'loading'` の間に匿名で投げた query が
      // 残っていることがある。
      resetQueryCacheForAuthChange();
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
