import { Link, useLocation } from 'react-router-dom';

// 未ログインの人に、提案するにはログインが要ることを伝える案内。
//
// 導線そのものは隠さない：見えないと「直せる」ことに気づけないため、
// 押したときにここへ辿り着く。戻り先を state で渡すのでログイン後に元の画面へ戻れる。
export default function LoginToSuggest({ message }: { message?: string }) {
  const location = useLocation();
  return (
    <p className="text-sm text-gray-500">
      {message ?? '修正の提案にはログインが必要です。'}{' '}
      <Link
        to="/login"
        state={{ from: location.pathname }}
        className="text-indigo-600 hover:text-indigo-800 font-medium underline underline-offset-2"
      >
        ログイン
      </Link>
    </p>
  );
}
