# デプロイ手順（VPS + Docker Compose + Caddy）

1 台の小さな VPS に Postgres・バックエンド・Caddy をまとめて置く構成。
Caddy が静的ファイルと `/api` の両方を同じオリジンで配信するので、CORS の設定は不要。
イメージは CI が GHCR へ発行し、この機械では pull するだけ（下記「更新をデプロイ」）。
証明書は Cloudflare Origin CA を置く（下記「オリジンサーバーの証明書」。更新は不要）。

```
インターネット
   │ 443
   ▼
 Caddy (web)  ── /api/* ──▶ backend:8080 ──▶ postgres:5432
   └─ それ以外 ──▶ /srv（React のビルド成果物）
```

## 前提

- Docker と Docker Compose v2 が入った Linux サーバー（メモリ 1GB 程度から動く）
- ドメインの DNS がそのサーバーを指していること
- 80 番と 443 番が外から到達できること
- **`deploy/certs/origin.crt` と `origin.key` を置いてあること**（下記「オリジンサーバーの証明書」）。
  無いと Caddy が起動しない

## オリジンサーバーの証明書

Cloudflare のプロキシ（オレンジ雲）を通す構成なので、証明書は 2 枚ある。

| 区間 | 証明書 | 管理 |
|---|---|---|
| 訪問者 ↔ Cloudflare | Cloudflare の Universal SSL | Cloudflare が自動更新。作業なし |
| Cloudflare ↔ オリジンサーバー | **Cloudflare Origin CA（有効期間 15 年）** | 更新不要。ここに置く |

**なぜ Let's Encrypt をやめたか**：プロキシを通すと TLS がエッジで終端されるため
TLS-ALPN-01 が使えず、更新経路が HTTP-01 だけになる。その HTTP-01 も Cloudflare
経由でしかオリジンサーバーに届かないので、更新の成否が Cloudflare 側の挙動に依存する。
さらに 80/443 を Cloudflare の IP レンジだけに絞ると経路はもっと細くなる。静かに失敗して
90 日後に全断、という事故の形が悪い。Origin CA なら更新自体が要らない。

置きかた（Cloudflare → SSL/TLS → Origin Server → Create Certificate。
**秘密鍵は作成時に一度しか表示されない**）：

```bash
mkdir -p deploy/certs && chmod 700 deploy/certs
# origin.crt と origin.key を貼る
chmod 600 deploy/certs/origin.key

# 証明書と鍵が対になっているかを確認（2 つの md5 が一致すること）
openssl x509 -noout -modulus -in deploy/certs/origin.crt | openssl md5
openssl rsa  -noout -modulus -in deploy/certs/origin.key | openssl md5
```

`certs/` は `.gitignore` 済み。**秘密鍵は絶対に git に入れないこと。**

⚠️ **この構成はオレンジ雲が常に有効であることが前提。** 灰色雲に戻すと訪問者が
直接オリジンサーバーに当たり、Origin CA は公開の信頼が無いのでサイト全体が開けなくなる。

⚠️ Cloudflare の SSL/TLS モードは **Full (strict)** にすること。

## 訪問者の IP

オリジンサーバーから見た接続元は Cloudflare のエッジなので、`RemoteAddr` は使えない。
バックエンドは `CF-Connecting-IP`（Cloudflare が必ず上書きする）を見る
（`internal/handler/auth_handlers.go` の `clientIP`）。

⚠️ **ヘッダーは詐称できるので、オリジンサーバーに直接到達できないことが前提。**
VPS のファイアウォールで 80/443 を Cloudflare の IP レンジだけに絞ること（TODO 30）。
これが無いと、オリジンサーバーの IP を知っている相手は Cloudflare を迂回して
`CF-Connecting-IP` を好きな値にでき、ログインの絞り込みも匿名投稿の
数え上げも回避できてしまう。

## 手順

### 1. 取得と設定

```bash
git clone <このリポジトリ> setori
cd setori/deploy
cp .env.example .env
```

`.env` を編集する。最低限、次の 6 つを埋める：

| 変数 | 生成方法・注意 |
|---|---|
| `DOMAIN` | 証明書を取るドメイン |
| `POSTGRES_PASSWORD` | `openssl rand -base64 24` |
| `SETTINGS_ENCRYPTION_KEY` | `openssl rand -base64 32`（開発環境とは別の鍵にする） |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | **`admin`/`admin` のままにしない** |
| `OAUTH_REDIRECT_BASE_URL` | `https://＜DOMAIN＞` |
| `FRONTEND_BASE_URL` | `https://＜DOMAIN＞` |

> **`SETTINGS_ENCRYPTION_KEY` の扱い**
>
> - **必ずパスワードマネージャーにも控えること。** 失うと DB に保存済みの
>   API キーが復号できなくなる（両方の環境で）
> - **バックアップと同じ場所には置かないこと。** バックアップは Google Drive へ
>   自動アップロードされるため、同梱すると暗号化の意味が無くなる。
>   鍵は `.env` にしか無く DB には保存されないので、pg_dump には入らない
> - **開発環境と本番環境では必ず別の鍵を使うこと。** 開発環境やその `.env` が
>   漏れても、本番バックアップ内の API キーを復号できないように境界を分ける。
> - 本番のダンプを開発環境へ復元すると、暗号化済みの機密は開発用の鍵では復号できない。
>   これは意図した挙動。復元後、管理画面の外部サービス連携に**開発用**の API キー・
>   OAuth secret・YouTube cookie をすべて入れ直し、AI プロバイダーにもそれぞれ
>   開発用の API キーを再登録する。本番用の資格情報は開発環境へコピーしない。
> - ここで必要なのは開発環境への「再登録」であり、外部サービス側で本番の API キーを
>   失効・再発行することではない。実際に漏えいした疑いがある場合だけ別途ローテーションする。
> - **稼働中の環境で鍵だけを直接差し替えないこと。** 現在の実装は旧鍵から新鍵への
>   再暗号化を自動では行わないため、既存の機密がすべて読めなくなる。鍵を更新する場合は、
>   旧鍵で復号して新鍵で再暗号化する移行処理を用意するか、計画停止のうえ全機密を再登録する。

### 2. 起動

パッケージは private なので、先に GHCR へログインする（この機械で一度だけ）：

```bash
echo <PAT> | docker login ghcr.io -u ruifan75 --password-stdin   # classic token / read:packages のみ
./deploy.sh
```

マイグレーションはバックエンドの起動時に自動実行される（`schema_migrations` で管理）。

```bash
docker compose logs -f backend   # 起動確認
```

`サーバーを起動します` が出れば完了。`https://＜DOMAIN＞` を開く。

### 3. 起動後にやること

1. `.env` で設定した管理者でログインし、**パスワードを変更**する
2. `管理 → 設定 → 外部サービス連携` で API キー（Holodex / YouTube など）を登録する
   - DB 上で暗号化され、再起動なしで反映される
   - 同じ画面の「YouTube cookie（cookies.txt）」も埋めておく。歌唱の終了時間を
     live chat の拍手から推定するのに使う。データセンターの IP は YouTube に
     BOT 判定されやすく、cookie が無いと取得できないことがある
     （失敗時は `docker compose logs backend` に原因が出る）
3. Google ログインを使う場合は、Google Cloud Console の
   「承認済みリダイレクト URI」に次を追加する
   ```
   https://＜DOMAIN＞/api/auth/oauth/google/callback
   ```

## 運用

### 更新をデプロイ

```bash
./deploy.sh
```

`git pull` → **GHCR からイメージを取得** → 差し替え、の順に走る。
本番機ではビルドしない：1 vCPU / 1GB に対して `vite build` の尖峰が 559MB あり、
稼働中のサービスと同居させるには余白が薄いため、ビルドは CI に寄せてある
（`.github/workflows/ci.yml` の `images` ジョブが main への push ごとに発行する）。

イメージのタグは**コミットの完全な SHA**。どの版が動いているかが一意に定まる。
push 直後は CI がまだ作り終えていないので、`deploy.sh` は最大 10 分待つ。

未コミットの変更があると、取得するイメージと手元のコードがずれるので中止する。

`deploy.sh` は `git pull` の後に **`exec` で自分を起動し直す**。bash はスクリプトを
一括で読まずバイト位置を進めながら実行するため、実行中に自分の中身が置き換わると
以降の挙動が保証されないため（`deploy.sh` を変更したコミットを入れるときに起きる）。
おかげで「先に手で `git pull` してから叩く」は不要。

#### 初回だけ：GHCR へログイン

リポジトリが private なのでパッケージも private になり、VPS 側に認証が要る。

```bash
echo <PAT> | docker login ghcr.io -u ruifan75 --password-stdin
```

PAT は **classic token で `read:packages` スコープのみ**で足りる。
認証情報は `~/.docker/config.json` に残るので、この作業は一度だけ。

#### CI を使わずに建てる

CI が落ちているとき、または手元の変更をすぐ試したいときだけ：

```bash
export GIT_COMMIT=$(git rev-parse --short HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export IMAGE_TAG=local
docker compose build && docker compose up -d --no-build
```

⚠️ 1GB の本番機では、稼働中のサービスと同時にビルドすると余白が薄い。
swap があるので落ちはしないが遅くなる。

`index.html` は `no-cache`、`/assets/*` はハッシュ付きで長期キャッシュなので、
再読み込みすれば新しい版に切り替わる。

### 稼働中のバージョンを確認する

```bash
curl https://＜DOMAIN＞/api/version     # {"commit":"3cca352","built_at":"..."}
```

`管理 → 設定` の右上にもフロントエンド・バックエンド両方の commit が出る。
**両者が食い違っていたら警告が出る**：片方の pull だけ失敗した場合や、
手元ビルドと GHCR のイメージが混ざった場合に起きる。これが分からないと
「直したはずの不具合が消えない」理由を追えない。出たら `./deploy.sh` で入れ直す。

### バックアップ

`管理 → バックアップ` から手動作成・自動化・復元ができる。
実体は `backups` という名前付きボリューム（コンテナ内 `/app/backups`）。
`pg_dump` はバックエンドのイメージに同梱してあるので、ホスト側の準備は要らない。

**サーバーごと失う事態に備えて、Google Drive 連携も有効にしておくこと。**
同じサーバーの中だけに置いたバックアップは、そのサーバーが壊れたら一緒に消える。

```bash
# ボリュームの中身を手元に落とす場合
docker compose cp backend:/app/backups ./backups-copy
```

### SSH で入る

⚠️ **`ssh <ドメイン>` は通らない。** オレンジ雲を有効にすると DNS は Cloudflare を
指すが、Cloudflare は 22 番を中継しない。オリジンサーバーの IP へ直接繋ぐこと。
IP を覚えずに済むよう手元の `~/.ssh/config` に別名を置く：

```
Host setori
    HostName <オリジンサーバーの IP>
    User <ユーザー名>
    IdentityFile ~/.ssh/<鍵>
```

### ログ

```bash
docker compose logs -f            # 全部
docker compose logs -f backend    # バックエンドだけ
```

### 停止

```bash
docker compose down       # コンテナだけ削除（データは残る）
docker compose down -v    # ⚠️ ボリュームごと削除。DB もバックアップも消える
```

## 設計上の制約

- **バックエンドは 1 インスタンスまで。** OAuth の state と一回限りの
  引き換えコードをメモリに持っているため、複数に増やすとログインが
  ランダムに失敗する。負荷が問題になったら先にここを DB へ移すこと。
- **常駐プロセスであることが前提。** セッションの掃除（1 時間ごと）と
  自動バックアップ（10 分ごと）が常駐 goroutine で動いているので、
  スケール・トゥ・ゼロする環境には載らない。
- Postgres のポートは外に公開していない。外部から繋ぐ必要が出たら
  compose に `ports` を足すのではなく SSH ポートフォワードを使うこと。
