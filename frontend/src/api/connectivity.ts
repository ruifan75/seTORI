import axios, { type AxiosInstance } from 'axios';
import { isTransientFailure } from '../queryPolicy';

type Connection = { online: boolean; apiUnavailable: boolean };
let connection: Connection = { online: true, apiUnavailable: false };
const listeners = new Set<() => void>();
let requestNumber = 0;
let latestCompleted = 0;
let baseURL: string | undefined;

export const connectionSnapshot = () => connection;
export function subscribeConnection(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
export function setBrowserOnline(online: boolean) {
  if (connection.online === online) return;
  connection = { ...connection, online };
  for (const listener of listeners) listener();
}

function completed(number: number, reachable: boolean | null) {
  // 古い要求の失敗で、より新しい疎通成功を取り消さない。
  if (number < latestCompleted) return;
  // 障害中の個別応答は health の成功/失敗の照合を妨げない。
  if (reachable === null && connection.apiUnavailable) return;
  latestCompleted = number;
  // 個別の応答だけでは DB の復帰を確かめられない。解除は health の成功だけ。
  if (reachable === null) return;
  if (connection.apiUnavailable === !reachable) return;
  connection = { ...connection, apiUnavailable: !reachable };
  for (const listener of listeners) listener();
}

// 再試行する失敗と、API 全体の接続障害は別。500 はその要求の失敗として扱う。
export function isAPIUnavailable(error: unknown): boolean {
  if (!axios.isAxiosError(error) || axios.isCancel(error)) return false;
  if (error.response) return [502, 503, 504, 520, 521, 522, 523, 524, 525, 526].includes(error.response.status);
  return isTransientFailure(error);
}

// 認証用の interceptor とは分け、本文・Bearer・エラー詳細は保持しない。
export function observeAPIConnection(api: AxiosInstance) {
  baseURL = api.defaults.baseURL;
  const requests = new WeakMap<object, number>();
  api.interceptors.request.use((config) => {
    requests.set(config, ++requestNumber);
    return config;
  });
  api.interceptors.response.use((response) => {
    completed(requests.get(response.config) ?? 0, null);
    return response;
  }, (error: unknown) => {
    if (axios.isAxiosError(error) && !axios.isCancel(error) && (error.response || isAPIUnavailable(error))) {
      completed(requests.get(error.config ?? {}) ?? 0, isAPIUnavailable(error) ? false : null);
    }
    return Promise.reject(error);
  });
}

// 復帰の確認は DB の疎通も見る公開 health API。認証 client を使わず、書き込みをしない。
export async function probeAPI(signal?: AbortSignal): Promise<boolean> {
  const number = ++requestNumber;
  try {
    const response = await axios.get(`${baseURL ?? ''}/api/health`, {
      timeout: 5000, signal, headers: { Accept: 'application/json' },
    });
    const reachable = response.status === 200 && response.data?.status === 'ok';
    completed(number, reachable);
    return reachable;
  } catch (error) {
    if (!axios.isCancel(error)) completed(number, false);
    return false;
  }
}
