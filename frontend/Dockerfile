# ===== ビルド =====
FROM node:22-alpine AS build

WORKDIR /src

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# 稼働中のバージョンを埋め込む（vite.config.ts の define が読む）。
# イメージに .git は入らないので、値は外から build args で渡す（deploy/deploy.sh）。
ARG GIT_COMMIT=dev
ARG BUILD_TIME=

# 同一オリジンで配信するので API は相対パスにする。
# .env.production 経由で渡すのは、Vite が確実に読む経路がこれだから
# （プロセス環境変数は設定によっては拾われない）。
# GIT_COMMIT / BUILD_TIME は vite.config.ts が process.env から読むので
# ここではプロセス環境変数として渡す。
RUN printf 'VITE_API_URL=\n' > .env.production \
    && GIT_COMMIT="${GIT_COMMIT}" BUILD_TIME="${BUILD_TIME}" npm run build

# ===== 配信 =====
FROM caddy:2-alpine

# Caddyfile は compose 側からマウントする（ドメイン変更で再ビルドしないため）
COPY --from=build /src/dist /srv
