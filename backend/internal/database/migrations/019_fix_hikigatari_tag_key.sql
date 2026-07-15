-- performance_tags の '弾き語り'（キーが日本語）を読みやすい英語キー 'self-accompanied' に修正する。
-- （弾き語り＝奏者が自分で伴奏しながら歌う＝self-accompanied）
-- 他のタグ（acoustic / piano / acappella …）と同じくキーは ASCII スラッグに揃える。
-- 表示名（display_name）は '弾き語り' のまま維持する。
--
-- performance_tags.id は performance_performance_tags.tag_id から FK 参照されており、
-- ON UPDATE CASCADE が無いため id を直接 UPDATE できない。
-- そこで「新キーを作成 → 関連を付け替え → 旧キーを削除」を1トランザクション（本ファイル）で行う。
-- 既に修正済み・そもそも存在しない環境では何もしない（冪等）。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM performance_tags WHERE id = '弾き語り') THEN
        -- 1. 新キー 'self-accompanied' を、表示名・色・作成日時を引き継いで作成
        INSERT INTO performance_tags (id, display_name, color, created_at)
            SELECT 'self-accompanied', display_name, color, created_at
            FROM performance_tags
            WHERE id = '弾き語り'
        ON CONFLICT (id) DO NOTHING;

        -- 2. 演出↔タグの関連を新キーへ付け替え。
        --    同じ演出が既に 'self-accompanied' を持つ場合は主キー (performance_id, tag_id) が
        --    衝突するため、その行は付け替えずに次の DELETE でまとめて除去する。
        UPDATE performance_performance_tags ppt
            SET tag_id = 'self-accompanied'
            WHERE ppt.tag_id = '弾き語り'
              AND NOT EXISTS (
                  SELECT 1 FROM performance_performance_tags x
                  WHERE x.performance_id = ppt.performance_id
                    AND x.tag_id = 'self-accompanied'
              );

        -- 3. 付け替えできなかった重複分（既に self-accompanied を持つ演出）を含め、旧キー参照を掃除
        DELETE FROM performance_performance_tags WHERE tag_id = '弾き語り';

        -- 4. 旧タグ（日本語キー）を削除
        DELETE FROM performance_tags WHERE id = '弾き語り';
    END IF;
END $$;
