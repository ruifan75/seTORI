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

    // 確保 YouTube API 已加載
    const initPlayer = () => {
      if (!(window as any).YT) {
        setTimeout(initPlayer, 100);
        return;
      }

      try {
        // 清空容器
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

    // 檢查 YouTube API 是否已加載
    if ((window as any).YT && (window as any).YT.Player) {
      initPlayer();
    } else {
      // 加載 YouTube IFrame API
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
      // 不銷毀播放器，因為它會自動清理
    };
  }, [videoId, onReady]);

  return <div ref={containerRef} style={{ width: '100%', height: '100%' }} />;
}

// 暴露播放器實例，供其他組件使用
export const youtubePlayerSeekTo = (seconds: number) => {
  if (playerInstance && playerInstance.seekTo) {
    playerInstance.seekTo(seconds, true);
    if (playerInstance.playVideo) {
      playerInstance.playVideo();
    }
  }
};
