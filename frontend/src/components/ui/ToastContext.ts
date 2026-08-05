import { createContext, useContext } from 'react';

// 取り消しなど、トーストから直接引ける操作（例：「元に戻す」）。
// 押すとトーストは閉じる。
export interface ToastAction {
  label: string;
  onClick: () => void;
}

export interface ToastMessage {
  id: number;
  message: string;
  type: 'success' | 'error' | 'info';
  action?: ToastAction;
}

export interface ToastContextValue {
  showToast: (message: string, type?: ToastMessage['type'], action?: ToastAction) => void;
  removeToast: (id: number) => void;
}

export const ToastContext = createContext<ToastContextValue | null>(null);

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within ToastProvider');
  }
  return context;
}
