import { useCallback, useEffect, useRef, useState } from 'react';
import { setPlayerInstance, playerGetCurrentTime } from './youtubePlayerControl';
import type { YouTubePlayerEvent, YouTubePlayerInstance } from '../types/youtube';

interface YoutubePlayerProps {
  videoId: string;
  onReady?: (player: YouTubePlayerInstance) => void;
}

export default function YoutubePlayer({ videoId, onReady }: YoutubePlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  // 「どの動画を・何回目の読み込みで」初期化したか。
  //
  // 単なる真偽値だと videoId が変わっても作り直されず、呼び出し側が `key` で
  // 丸ごと remount する必要があった。逆に毎回作り直すと、親が渡す onReady が
  // インライン関数のときレンダーのたびに初期化が走る。合成キーで両方を防ぐ。
  const initTokenRef = useRef<string | null>(null);

  // 再読み込み後に戻す再生位置。位置を失うと編集中に見ていた場所を探し直しになる
  const resumeAtRef = useRef<number | null>(null);
  const [reloadNonce, setReloadNonce] = useState(0);

  // 再生が止まったときにプレイヤーだけを作り直す。
  //
  // YouTube は「ストリーミング端末数の上限」等で再生を止めることがあり、
  // ページを再読み込みすると**編集中の内容がすべて消える**。ここだけ
  // 作り直せば編集は保たれる。
  const reload = useCallback(() => {
    const now = playerGetCurrentTime('page');
    resumeAtRef.current = now && now > 0 ? now : null;
    setPlayerInstance('page', null);
    initTokenRef.current = null;
    setReloadNonce((n) => n + 1);
  }, []);

  useEffect(() => {
    const token = `${videoId}#${reloadNonce}`;
    if (!videoId || !containerRef.current || initTokenRef.current === token) {
      return;
    }

    // YouTube API がロード済みであることを確認
    const initPlayer = () => {
      const YT = window.YT;
      const container = containerRef.current;
      if (!YT?.Player || !container) {
        setTimeout(initPlayer, 100);
        return;
      }

      try {
        // **YT.Player は渡した要素を iframe で「置き換える」。** ref が指す div を
        // そのまま渡すと、2 回目以降その div は DOM から消えていて、作り直した
        // プレイヤーが画面に現れない（現在時刻も取れない）。呼び出し側が `key` で
        // 丸ごと remount しないと動かなかったのはこれが理由。
        // 毎回コンテナの中に使い捨ての div を作り、置き換えられるのはそちらにする。
        container.innerHTML = '';
        const mount = document.createElement('div');
        container.appendChild(mount);

        const origin = window.location.origin;
        const player = new YT.Player(mount, {
          origin,
          width: '100%',
          height: '390',
          videoId: videoId,
          playerVars: {
            autoplay: 0,
            controls: 1,
            modestbranding: 1,
            origin,
          },
          events: {
            onReady: (event: YouTubePlayerEvent) => {
              setPlayerInstance('page', event.target);
              // 再読み込み前の位置へ戻す（読み込み完了前に seek しても効かない）
              const resumeAt = resumeAtRef.current;
              if (resumeAt != null) {
                resumeAtRef.current = null;
                try {
                  event.target.seekTo(resumeAt, true);
                } catch {
                  // 位置を戻せなくても再生自体は復帰しているので続行する
                }
              }
              onReady?.(event.target);
            },
          },
        });
        setPlayerInstance('page', player);

        initTokenRef.current = token;
      } catch (error) {
        console.error('Failed to initialize YouTube player:', error);
      }
    };

    // YouTube API がロード済みかを確認
    if (window.YT?.Player) {
      initPlayer();
    } else {
      // YouTube IFrame API をロード
      if (!document.getElementById('youtube-api')) {
        const script = document.createElement('script');
        script.id = 'youtube-api';
        script.src = 'https://www.youtube.com/iframe_api';
        script.async = true;
        script.onload = () => {
          setTimeout(initPlayer, 500);
        };
        document.body.appendChild(script);
      }
    }

    return () => {
      // プレイヤーを破棄しない（自動クリーンアップされるため）
    };
  }, [videoId, onReady, reloadNonce]);

  return (
    <div className="relative w-full h-full">
      <div ref={containerRef} style={{ width: '100%', height: '100%' }} />
      <button
        onClick={reload}
        title="プレイヤーを再読み込み（再生位置は保ちます。編集中の内容は消えません）"
        aria-label="プレイヤーを再読み込み"
        className="absolute top-1 right-1 z-10 rounded bg-black/40 px-1.5 py-0.5 text-xs text-white/70 opacity-40 transition hover:bg-black/70 hover:text-white hover:opacity-100"
      >
        ⟳
      </button>
    </div>
  );
}
