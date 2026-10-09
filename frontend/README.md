## 通信と取得失敗（issue #108）

- query は4xx・中断・プログラムの例外を再試行しない。5xx・通信障害は1秒・2秒・4秒後に最大3回再試行する（`src/queryPolicy.ts` / `src/queryClient.ts`）。
- mutation は自動再試行しない。オフラインでも送信を保留せず、失敗をその場で返す。復帰後に書き込みを自動実行しない。
- 上部の接続表示はブラウザの offline/online と API の接続障害を区別する。応答のない通信エラー・502/503/504・Cloudflareの520〜526で障害を表示する。500等の個別の失敗は画面内の取得エラーだけで示す。障害中は公開の `/api/health` を15秒ごとに確認し、1要求は5秒まで。200かつ `status: "ok"` だけで復帰とする。版APIや別の要求の成功では、DBの復帰を確認できないため解除しない。Bearer・本文・エラー詳細は疎通の記録へ入れない。
- health で復帰を確認したら、表示中で一時的に失敗した query だけを監視側で再取得する。ブラウザの再接続は React Query だけが扱い、保留中の取得を再開し、表示中の古い取得も取り直す。監視側は online イベントで重ねて取得しない。4xx・非表示の query・mutation は復帰を理由に再実行しない。
- 初回取得中は `isPending` を使う。`isLoading` はオフラインの保留中に false なので、空の一覧へ倒してしまう。失敗は `QueryError` 等で空・見つからない表示と分ける。
- 認証結果と視点の破棄は既存の store / `applyViewerChange` に任せる。再試行や疎通の復帰を理由に `setQueryData` や編集 state へ値をコピーしない。キャッシュ外へコピーする処理は応答後の `sameViewer` と `onViewerChange` の後始末を保つ。
- `node --test tests/queryRecovery.test.mjs tests/queryErrors.test.mjs` は実際の QueryClient・API client と画面の描画を検査する。DB・外部 API は使わない。

# React + TypeScript + Vite

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Babel](https://babeljs.io/) (or [oxc](https://oxc.rs) when used in [rolldown-vite](https://vite.dev/guide/rolldown)) for Fast Refresh
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/) for Fast Refresh

## React Compiler

The React Compiler is not enabled on this template because of its impact on dev & build performances. To add it, see [this documentation](https://react.dev/learn/react-compiler/installation).

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```
