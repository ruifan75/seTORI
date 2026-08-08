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
#
# python3 は yt-dlp（拍手による歌唱 end 推定で live chat を落とす）用。
RUN apk add --no-cache postgresql16-client ca-certificates tzdata python3

# yt-dlp。apk 版は古くなりがちで、古い yt-dlp は YouTube に BOT 判定されて
# live chat を落とせなくなるため、公式リリースの zipapp を直接入れる
# （musl 環境では PyInstaller 版の単体バイナリが動かないので python3 + zipapp）。
# 既定は latest。壊れた版を掴んだら YTDLP_VERSION=2026.03.17 のように固定して再ビルドする。
ARG YTDLP_VERSION=latest
RUN if [ "$YTDLP_VERSION" = "latest" ]; then \
        url="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp"; \
    else \
        url="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp"; \
    fi && \
    wget -q -O /usr/local/bin/yt-dlp "$url" && \
    chmod 0755 /usr/local/bin/yt-dlp && \
    /usr/local/bin/yt-dlp --version

ENV TZ=Asia/Tokyo

WORKDIR /app
COPY --from=build /out/server /app/server

# 非 root で動かす。BACKUP_DIR と live chat のキャッシュは名前付きボリュームを
# 載せる場所なので、先にこのユーザー所有で作っておく（空のボリュームは初回マウント時に
# マウント先の所有権を引き継ぐため、これが無いと root 所有になって書けない）。
RUN adduser -D -u 10001 app \
    && mkdir -p /app/backups /app/data/chat_cache \
    && chown -R app:app /app

USER app

EXPOSE 8080

CMD ["/app/server"]
