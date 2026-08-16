-- 訪客／登入使用者的頁面活動（日單位の集約）。
--
-- IP は個人情報になり得るため、API リクエストを無制限に生ログ化しない。
-- 同じ UTC 日・IP・利用者について 1 行だけ保持し、ページ表示数と最終表示先を更新する。
-- 未ログインとログイン後は actor_key を分け、同じ IP でも利用者ごとの動きは区別する。

CREATE TABLE IF NOT EXISTS visitor_activity (
    id                BIGSERIAL PRIMARY KEY,
    visit_date        DATE NOT NULL,
    ip_address        INET NOT NULL,
    user_id           UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_key         VARCHAR(64) NOT NULL,
    username_snapshot VARCHAR(64) NOT NULL DEFAULT '',
    first_seen        TIMESTAMP WITH TIME ZONE NOT NULL,
    last_seen         TIMESTAMP WITH TIME ZONE NOT NULL,
    page_views        INTEGER NOT NULL DEFAULT 1 CHECK (page_views > 0),
    last_path         VARCHAR(512) NOT NULL DEFAULT '/',
    user_agent        VARCHAR(512) NOT NULL DEFAULT '',

    UNIQUE (visit_date, ip_address, actor_key)
);

CREATE INDEX IF NOT EXISTS idx_visitor_activity_last_seen
    ON visitor_activity(last_seen DESC);

CREATE INDEX IF NOT EXISTS idx_visitor_activity_user
    ON visitor_activity(user_id, last_seen DESC)
    WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_visitor_activity_ip
    ON visitor_activity(ip_address, last_seen DESC);
