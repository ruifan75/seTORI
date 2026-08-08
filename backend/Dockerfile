# ===== ビルド =====
FROM golang:1.24-alpine AS build

WORKDIR /src

# 依存だけ先に取得してレイヤーキャッシュを効かせる
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 稼働中のバージョンを埋め込む（/api/version と管理画面が返す）。
# イメージに .git は入らないので、値は外から build args で渡す（deploy/deploy.sh）。
ARG GIT_COMMIT=dev
ARG BUILD_TIME=

# lib/pq は純 Go なので CGO 不要。静的バイナリにして実行イメージを軽くする
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.commit=${GIT_COMMIT} -X main.builtAt=${BUILD_TIME}" \
    -o /out/server ./cmd/server

# ===== 実行 =====
FROM alpine:3.21

# pg_dump / pg_restore はバックアップ機能が使う。
# PostgreSQL サーバーと同じメジャーバージョン（16）を入れること。
# クライアントがサーバーより古いと pg_dump は実行を拒否する。
RUN apk add --no-cache postgresql16-client ca-certificates tzdata

ENV TZ=Asia/Tokyo

WORKDIR /app
COPY --from=build /out/server /app/server

# 非 root で動かす。BACKUP_DIR は名前付きボリュームを載せる場所なので、
# 先にこのユーザー所有で作っておく（空のボリュームは初回マウント時に
# マウント先の所有権を引き継ぐため、これが無いと root 所有になって書けない）。
RUN adduser -D -u 10001 app \
    && mkdir -p /app/backups \
    && chown -R app:app /app

USER app

EXPOSE 8080

CMD ["/app/server"]
