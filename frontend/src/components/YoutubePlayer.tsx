import { useEffect, useRef } from 'react';

interface YoutubePlayerProps {
  videoId: string;
  onReady?: (player: any) => void;
}

let playerInstance: any = null;

export default function YoutubePlayer({ videoId, onReady }: YoutubePlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const isInitializedRef = useRef(false);

  useEffect(() => {
    if (!videoId || !containerRef.current || isInitializedRef.current) {
      return;
    }

    // YouTube API がロード済みであることを確認
    const initPlayer = () => {
      if (!(window as any).YT) {
        setTimeout(initPlayer, 100);
        return;
      }

      try {
        // コンテナをクリア
        if (containerRef.current) {
          containerRef.current.innerHTML = '';
        }

        playerInstance = new (window as any).YT.Player(containerRef.current, {
          width: '100%',
          height: '390',
          videoId: videoId,
          playerVars: {
            autoplay: 0,
            controls: 1,
            modestbranding: 1,
          },
          events: {
            onReady: (event: any) => {
              onReady?.(event.target);
            },
          },
        });

        isInitializedRef.current = true;
      } catch (error) {
        console.error('Failed to initialize YouTube player:', error);
      }
    };

    // YouTube API がロード済みかを確認
    if ((window as any).YT && (window as any).YT.Player) {
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
  }, [videoId, onReady]);

  return <div ref={containerRef} style={{ width: '100%', height: '100%' }} />;
}

// プレイヤーインスタンスを公開、他のコンポーネントで使用するため
export const youtubePlayerSeekTo = (seconds: number) => {
  if (playerInstance && playerInstance.seekTo) {
    playerInstance.seekTo(seconds, true);
    if (playerInstance.playVideo) {
      playerInstance.playVideo();
    }
  }
};

// 現在の再生時間を取得（秒）
export const youtubePlayerGetCurrentTime = (): number | null => {
  if (playerInstance && playerInstance.getCurrentTime) {
    return playerInstance.getCurrentTime();
  }
  return null;
};
