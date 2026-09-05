-- 「非表示だが現行規則で曲が出た配信」を人が見たあとの**否定の記録**（issue #42）。
--
-- 非表示の配信を掃き直すと、抽出結果が出るものが残る。それは
-- **誤判定かもしれない**（本当は歌回なのに隠れている）が、**そうでないことも多い**
-- ── 実況メモや雑談のタイムスタンプが曲として拾われるため。
--
-- **自動で非表示を解除しない。** 2026-08-29 に手で見直したときの実測では、
-- 誤判定は両方向にあった（雑談が歌枠と判定される／本物の歌枠が隠れる）。
-- 自動で解くと雑談が発見面へ出る。
--
-- **記録するのは否定だけ**（`song_identity_checks` / `performance_tag_checks` と同じ）。
-- 「見たが歌回ではない」を残さないと同じ配信が毎回候補に出続け、作業一覧として
-- 使えなくなる。肯定（歌回だった）は非表示の解除と歌単の作成そのものが記録になる。
--
-- **差分は保存しない。** 候補は毎回計算する（現行規則が変われば候補も変わるべきなので、
-- 保存すると古い判断が残る）。ここに置くのは人の判断だけ。
CREATE TABLE IF NOT EXISTS non_singing_checks (
    stream_id  VARCHAR(64) PRIMARY KEY REFERENCES streams(id) ON DELETE CASCADE,
    checked_by UUID REFERENCES users(id) ON DELETE SET NULL,
    checked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    -- 判断の理由（任意）。後から見て「なぜ歌回でないと決めたか」を辿れるように。
    note TEXT
);

COMMENT ON TABLE non_singing_checks IS
    '非表示の配信を見て「歌回ではない」と判断した記録。候補一覧から外し続けるためのもの';
