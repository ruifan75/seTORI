# ===== ビルド =====
FROM node:22-alpine AS build

WORKDIR /src

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# 同一オリジンで配信するので API は相対パスにする。
# .env.production 経由で渡すのは、Vite が確実に読む経路がこれだから
# （プロセス環境変数は設定によっては拾われない）。
RUN printf 'VITE_API_URL=\n' > .env.production \
    && npm run build

# ===== 配信 =====
FROM caddy:2-alpine

# Caddyfile は compose 側からマウントする（ドメイン変更で再ビルドしないため）
COPY --from=build /src/dist /srv
