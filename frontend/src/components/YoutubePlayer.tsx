import { useEffect, useRef } from 'react';
import { setYoutubePlayerInstance } from './youtubePlayerControl';
import type { YouTubePlayerEvent, YouTubePlayerInstance } from '../types/youtube';

interface YoutubePlayerProps {
  videoId: string;
  onReady?: (player: YouTubePlayerInstance) => void;
}

export default function YoutubePlayer({ videoId, onReady }: YoutubePlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const isInitializedRef = useRef(false);

  useEffect(() => {
    if (!videoId || !containerRef.current || isInitializedRef.current) {
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
        // コンテナをクリア
        if (containerRef.current) {
          containerRef.current.innerHTML = '';
        }

        const origin = window.location.origin;
        const player = new YT.Player(container, {
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
              setYoutubePlayerInstance(event.target);
              onReady?.(event.target);
            },
          },
        });
        setYoutubePlayerInstance(player);

        isInitializedRef.current = true;
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
  }, [videoId, onReady]);

  return <div ref={containerRef} style={{ width: '100%', height: '100%' }} />;
}
