-- performance_tag_checks … 「この歌唱にこのタグは付けない」という否定の記録。
--
-- タグ漏れの一覧（/admin/missing-tags）は「解析キャッシュがタグを付けているのに
-- 歌唱には付いていない」差分を出す。差分は派生値なので毎回計算し直せるが、
-- **人が意図的に付けなかったもの**は計算では区別できず、放っておくと一覧に残り続ける。
-- 残り続けると作業一覧として使えなくなるうえ、別のレビュー担当が「漏れ」と読んで
-- 付けてしまう。song_identity_checks と同じ考えで、否定だけを記録する。
--
-- 肯定は記録しない（付けたタグは performance_performance_tags にあるので、
-- 次の計算で自然に差分から消える）。
--
-- tag_id に FK を張らないのは、performance_tags に無い ID（語彙のずれで
-- キャッシュに残った値）も「無視」できるようにするため。付けることはできないが、
-- 一覧から消せないと他の行が埋もれる。
CREATE TABLE IF NOT EXISTS performance_tag_checks (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL,
    checked_by UUID REFERENCES users(id) ON DELETE SET NULL,
    checked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (performance_id, tag_id)
);
