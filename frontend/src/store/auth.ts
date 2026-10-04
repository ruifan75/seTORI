import { create } from 'zustand';
import { isAxiosError } from 'axios';
import { authApi, setAuthToken, setUnauthorizedHandler } from '../api/client';
import type { AuthUser } from '../api/types';

import { applyViewerChange, viewerKey } from '../queryClient';

const TOKEN_KEY = 'setori_token';
// 明示した login / logout と共有 token の変更が認証を世代で区切る。
// 起動時検証や旧 Bearer の失効で、進行中の新しい認証を取り消さない。
let sessionRevision = 0;
let pendingLoginRevision: number | null = null;
let observedSharedToken: string | null = null;

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
  setAuthToken(null);
  set({ token: null, user: null, status: 'anonymous' });
  applyViewerChange(null, true);
}

// 失効したのはこの token だけ。別タブで保存済みの新 token は削除しない。
function forgetStoredToken(token: string) {
  if (localStorage.getItem(TOKEN_KEY) !== token) return;
  localStorage.removeItem(TOKEN_KEY);
  observedSharedToken = null;
}

function beginLogin(set: (partial: Partial<AuthState>) => void, status: AuthState['status']) {
  const revision = ++sessionRevision;
  pendingLoginRevision = revision;
  observedSharedToken = localStorage.getItem(TOKEN_KEY);
  // まだ検証していない起動時 token は新しい認証へ持ち越さない。
  // 失敗した認証が loading のまま残らないよう、このタブだけ未ログインにする。
  if (status === 'loading') clearLocalSession(set);
  return revision;
}

function establishSession(set: (partial: Partial<AuthState>) => void, token: string, user: AuthUser) {
  // storage イベントがまだ届いていなくても、認証中に別タブで
  // logout / token 変更があれば、その新しい操作を上書きしない。
  if (localStorage.getItem(TOKEN_KEY) !== observedSharedToken) {
    synchronizeSharedSession();
    throw new Error('ログイン処理は取り消されました');
  }
  localStorage.setItem(TOKEN_KEY, token);
  observedSharedToken = token;
  setAuthToken(token);
  set({ token, user, status: 'authenticated' });
  applyViewerChange(viewerKey(user.id, user.permissions));
}

export const useAuthStore = create<AuthState>((set, get) => ({
  token: null,
  user: null,
  status: 'loading',

  login: async (username, password) => {
    const revision = beginLogin(set, get().status);
    try {
      const { token, user } = await authApi.login(username, password);
      if (revision !== sessionRevision) throw new Error('ログイン処理は取り消されました');
      establishSession(set, token, user);
    } finally {
      if (pendingLoginRevision === revision) pendingLoginRevision = null;
    }
  },

  loginWithOAuthCode: async (code) => {
    const revision = beginLogin(set, get().status);
    try {
      const { token, user } = await authApi.exchangeOAuthCode(code);
      if (revision !== sessionRevision) throw new Error('ログイン処理は取り消されました');
      establishSession(set, token, user);
    } finally {
      if (pendingLoginRevision === revision) pendingLoginRevision = null;
    }
  },

  logout: async () => {
    const token = get().token ?? localStorage.getItem(TOKEN_KEY);
    // 通信を待つ間にも秘匿の表示・資格情報を残さない。
    sessionRevision++;
    pendingLoginRevision = null;
    localStorage.removeItem(TOKEN_KEY);
    observedSharedToken = null;
    clearLocalSession(set);
    try {
      if (token) await authApi.logout(token);
    } catch {
      // トークンが既に無効でもローカルは必ずクリアする
    }
  },

  // アプリ起動時：保存済みトークンがあれば /me で検証してユーザーを復元する
  init: async () => {
    // 子の OAuth effect が親の起動時 effect より先に動く。単発コードの
    // 引き換えを bootstrap（StrictMode の再実行も含む）で上書きしない。
    if (pendingLoginRevision === sessionRevision) return;
    const revision = ++sessionRevision;
    const token = localStorage.getItem(TOKEN_KEY);
    observedSharedToken = token;
    if (!token) {
      clearLocalSession(set);
      return;
    }
    setAuthToken(token);
    try {
      const user = await authApi.me();
      if (revision !== sessionRevision) return;
      if (localStorage.getItem(TOKEN_KEY) !== token) {
        synchronizeSharedSession();
        return;
      }
      set({ token, user, status: 'authenticated' });
      applyViewerChange(viewerKey(user.id, user.permissions));
    } catch (error) {
      if (revision !== sessionRevision) {
        // 新しい資格情報はまだ検証中だが、現在表示している旧 token の
        // 失効は確定した。旧コピーだけ捨て、新しい login は取り消さない。
        // 新しい /me が既に成功した場合は、旧 401 でも何も変えない。
        if (pendingLoginRevision === sessionRevision && get().token === token
          && isAxiosError(error) && error.response?.status === 401) {
          forgetStoredToken(token);
          clearLocalSession(set);
        }
        return;
      }
      if (localStorage.getItem(TOKEN_KEY) !== token) {
        synchronizeSharedSession();
        return;
      }
      // 接続失敗・5xx では、このタブの未検証データだけ捨てる。
      // 共有 token を消すと、正常に使っている別タブまでログアウトする。
      if (isAxiosError(error) && error.response?.status === 401) forgetStoredToken(token);
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
  const token = useAuthStore.getState().token;
  if (!token) return; // 起動時の未検証 token は init が処理する
  forgetStoredToken(token);
  clearLocalSession((partial) => useAuthStore.setState(partial));
});

// localStorage はタブ間で共有されるが、store と QueryClient は共有されない。
// 別タブの logout / アカウント変更でも、旧視点のコピーを先に捨ててから検証する。
function synchronizeSharedSession() {
  // イベントの newValue はキューに残った旧値かもしれない。
  // 最新値が既に処理済みなら、新しい login や検証を取り消さない。
  const token = localStorage.getItem(TOKEN_KEY);
  if (token === observedSharedToken) return;
  observedSharedToken = token;
  sessionRevision++;
  pendingLoginRevision = null;
  setAuthToken(null);
  useAuthStore.setState({ token: null, user: null, status: token ? 'loading' : 'anonymous' });
  applyViewerChange(null, true);
  if (token) void useAuthStore.getState().init();
}
if (typeof window !== 'undefined') window.addEventListener('storage', (event) => {
  if (event.storageArea !== localStorage || (event.key !== TOKEN_KEY && event.key !== null)) return;
  synchronizeSharedSession();
});
