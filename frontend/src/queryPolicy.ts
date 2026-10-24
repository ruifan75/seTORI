import axios from 'axios';

// 4xx・中断・プログラムの例外は待っても直らない。最初の要求＋再試行3回まで。
export function isTransientFailure(error: unknown): boolean {
  if (!axios.isAxiosError(error) || axios.isCancel(error)) return false;
  if (error.response) return error.response.status >= 500 && error.response.status < 600;
  return ['ERR_NETWORK', 'ECONNABORTED', 'ETIMEDOUT', 'ECONNRESET', 'ECONNREFUSED', 'ENOTFOUND'].includes(error.code ?? '');
}

export function retryQuery(failureCount: number, error: unknown): boolean {
  return failureCount < 3 && isTransientFailure(error);
}

export function queryRetryDelay(attempt: number): number {
  return Math.min(1000 * 2 ** attempt, 10000);
}
