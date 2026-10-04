// 外部の応答を window.open へ直接渡さない。React の href の保護はここでは働かない。
export function itunesTrackURL(value: string | null | undefined, itunesId: number): string {
  try {
    const url = new URL(value ?? '');
    if (url.protocol === 'https:' && !url.username && !url.password
      && ['music.apple.com', 'itunes.apple.com'].includes(url.hostname)) return url.href;
  } catch { /* 空欄や不正な URL は既知のリンクへ戻す */ }
  return `https://music.apple.com/song/${encodeURIComponent(String(itunesId))}`;
}
