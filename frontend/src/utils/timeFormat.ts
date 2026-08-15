// 編集画面と審査画面で共有する時間の整形。

export function formatTimeInput(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export function formatDuration(seconds: number | null): string {
  if (seconds === null || seconds === 0) return '+??:??';
  
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  
  if (h > 0) {
    return `+${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  }
  return `+${m}:${s.toString().padStart(2, '0')}`;
}

export function parseTime(timeStr: string): number {
  if (!timeStr || timeStr.trim() === '') {
    return 0;
  }
  
  const parts = timeStr.split(':').map(s => {
    const num = parseInt(s, 10);
    return isNaN(num) ? 0 : num;
  });
  
  if (parts.length === 3) {
    return parts[0] * 3600 + parts[1] * 60 + parts[2];
  } else if (parts.length === 2) {
    return parts[0] * 60 + parts[1];
  } else if (parts.length === 1) {
    // 数字のみの場合、秒数とみなす
    return parts[0];
  }
  return 0;
}
