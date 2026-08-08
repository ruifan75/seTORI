import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // 稼働中のビルドを画面に出すための埋め込み。値は Dockerfile の build args
  // （deploy/deploy.sh が git から取る）→ 環境変数の順で渡ってくる。
  // ローカルの npm run dev では 'dev' になる。
  define: {
    __APP_COMMIT__: JSON.stringify(process.env.GIT_COMMIT || 'dev'),
    __APP_BUILT_AT__: JSON.stringify(process.env.BUILD_TIME || ''),
  },
  server: {
    host: '0.0.0.0',
    allowedHosts: ['macbook.ruifan'],
  },
})
