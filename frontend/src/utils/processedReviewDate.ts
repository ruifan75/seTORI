// 日付欄は閲覧者のタイムゾーンの暦日。終了日は翌日の0時を送り、終日を含める。
export function reviewDateBoundary(date: string, end: boolean): string {
  if (!date) return '';
  const value = new Date(`${date}T00:00:00`);
  if (end) value.setDate(value.getDate() + 1);
  return value.toISOString();
}
