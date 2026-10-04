import { Navigate, useLocation } from 'react-router-dom';

// 旧ブックマークの query/hash と符号化された ID をそのまま引き継ぐ。
// useParams の ID はデコード済みなので、パスの組み立てには使わない。
// このコンポーネントは旧ルートだけで使う。先頭セグメントが %73ingers でも
// Router は singers として照合するため、復号せずそのセグメントだけを置換する。
export default function LegacyChannelRedirect() {
  const { pathname, search, hash } = useLocation();
  return <Navigate replace to={{
    pathname: pathname.replace(/^\/[^/]+/, '/channels'),
    search,
    hash,
  }} />;
}
