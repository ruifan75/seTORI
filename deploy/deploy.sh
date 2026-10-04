#!/usr/bin/env bash
#
# 本番へのデプロイ。VPS 上で `./deploy.sh` と叩く。
#
# CI（.github/workflows/ci.yml）が main への push ごとに GHCR へイメージを発行し、
# ここでは **pull するだけ**。本番機は 1 vCPU / 1GB で、`npm ci && vite build` の
# 尖峰が 559MB あるため、稼働中のサービスと同居させるには余白が薄い。
#
# CI を待てない・使えないときは、末尾の「CI を使わずに建てる」を参照。
set -euo pipefail

# $0 は相対パスで渡され得るので、cd する前に絶対パスへ直しておく
# （下の exec で自分を起動し直すのに使う）。
SCRIPT="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$(dirname "$SCRIPT")"
REPO_ROOT="$(cd .. && pwd)"

# git pull は deploy.sh 自身も置き換える。bash はスクリプトを一括で読まず
# バイト位置を進めながら実行するので、**実行中に自分の中身が変わると以降の挙動が
# 保証されない**（新しいファイルの途中から読み始めることがある）。
# そこで pull だけ先に済ませ、新しい内容で自分を起動し直してから本題に入る。
#
# DEPLOY_REEXEC=1 を自分で渡せば pull を飛ばせる。CI が落ちていて手元で
# 建てたいときや、pull せずに now のコミットで入れ直したいときに使う。
if [ "${DEPLOY_REEXEC:-}" != "1" ]; then
	# 未コミットの変更があると、取得するイメージ（＝コミットのタグ）と手元のコードがずれる。
	if ! git -C "$REPO_ROOT" diff --quiet || ! git -C "$REPO_ROOT" diff --cached --quiet; then
		echo "作業ツリーに未コミットの変更があります。取得するイメージと実際のコードが" >&2
		echo "食い違うため中止します。commit するか git stash してから実行してください。" >&2
		exit 1
	fi

	git -C "$REPO_ROOT" pull --ff-only
	DEPLOY_REEXEC=1 exec "$SCRIPT" "$@"
fi

# イメージのタグはコミットの完全な SHA。どの版が動いているか一意に定まる。
IMAGE_TAG="$(git -C "$REPO_ROOT" rev-parse HEAD)"
SHORT="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
export IMAGE_TAG

echo "デプロイします: ${SHORT}"

# CI がまだ発行し終えていないことがある（push 直後は数分かかる）。
# 「タグが無い」と「認証されていない」は対処が違うので分けて扱う。
echo "イメージを取得します（CI のビルド完了を待ちます）..."
pull_log="$(mktemp)"
trap 'rm -f "$pull_log"' EXIT

for attempt in $(seq 1 20); do
	if docker compose pull --quiet >"$pull_log" 2>&1; then
		echo "取得しました。"
		break
	fi
	if grep -qiE 'denied|unauthorized|authentication required' "$pull_log"; then
		echo >&2
		echo "GHCR への認証に失敗しました。パッケージは private なのでログインが要ります:" >&2
		echo "  echo <PAT> | docker login ghcr.io -u ruifan75 --password-stdin" >&2
		echo "PAT は read:packages スコープのみで足ります（classic token）。" >&2
		exit 1
	fi
	if [ "$attempt" -eq 20 ]; then
		echo >&2
		echo "イメージ ${SHORT} を取得できませんでした。CI が失敗している可能性があります:" >&2
		echo "  https://github.com/ruifan75/seTORI/actions" >&2
		tail -5 "$pull_log" >&2
		exit 1
	fi
	printf '  まだありません（%d/20）。30 秒後に再試行します\n' "$attempt"
	sleep 30
done

# --no-build を付けるのは、取得に失敗した状態で黙って本番機がビルドを始めるのを防ぐため
# （メモリが薄いので、気付かないうちに始まると他のサービスを圧迫する）。
docker compose up -d --no-build --remove-orphans

echo
echo "稼働中のバージョンを確認します:"
# 公開されている経路そのもので確かめる（Cloudflare と証明書まで含めて確認できる）。
DOMAIN="$(grep -E '^DOMAIN=' .env | head -1 | cut -d= -f2- || true)"
if [ -z "$DOMAIN" ]; then
	echo "  .env に DOMAIN がありません。稼働版を確認できません。" >&2
	exit 1
fi

for _ in $(seq 1 30); do
	# 旧コンテナの 200 を成功としない。CI と VPS で SHA の省略桁数が
	# 違っても、7 桁以上の commit が対象の完全 SHA と一致するか確かめる。
	# 1 回の問い合わせにも期限を置き、接続したまま待ち続けない。
	if version_response="$(curl -fsS --max-time 10 "https://${DOMAIN}/api/version")" &&
		version_fields="$(printf '%s\n' "$version_response" | grep -oE '"commit"[[:space:]]*:' | tr -d '[:space:]')" &&
		[[ "$version_fields" == '"commit":' ]] &&
		version_commit="$(printf '%s\n' "$version_response" | grep -oE '"commit"[[:space:]]*:[[:space:]]*"[0-9a-f]{7,40}"' | cut -d '"' -f4)" &&
		[[ "$IMAGE_TAG" == "$version_commit"* ]]; then
		printf '%s\n' "$version_response"
		exit 0
	fi
	sleep 2
done

echo "  https://${DOMAIN}/api/version で対象版 ${SHORT} を確認できませんでした。" >&2
echo "  docker compose logs -f backend / docker compose logs web を確認してください。" >&2
exit 1

# ===== CI を使わずに建てる =====
#
# CI が落ちている、または手元の変更をすぐ試したいときだけ：
#
#   export GIT_COMMIT=$(git rev-parse --short HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
#   export IMAGE_TAG=local
#   docker compose build && docker compose up -d --no-build
#
# ⚠️ 1GB の本番機では、稼働中のサービスと同時にビルドすると余白が薄い。
#    swap があるので落ちはしないが遅くなる。
