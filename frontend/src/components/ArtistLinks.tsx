import { Link } from 'react-router-dom';
import type { ArtistReference } from '../api/types';

interface ArtistLinksProps {
  artists?: ArtistReference[];
  fallback?: string;
  className?: string;
  linkClassName?: string;
  onNavigate?: () => void;
}

// artist UUID がある場合だけ詳細ページへリンクし、旧データは表示テキストへフォールバックする。
export default function ArtistLinks({
  artists = [],
  fallback = '',
  className = '',
  linkClassName = '',
  onNavigate,
}: ArtistLinksProps) {
  if (artists.length === 0) {
    return fallback ? <span className={className}>{fallback}</span> : null;
  }

  return (
    <span className={className}>
      {artists.map((artist, index) => (
        <span key={artist.id}>
          {index > 0 && '、'}
          <Link to={`/artists/${artist.id}`} onClick={onNavigate} className={linkClassName}>
            {artist.name}
          </Link>
        </span>
      ))}
    </span>
  );
}
