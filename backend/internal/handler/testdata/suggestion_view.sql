SELECT id, target_type, target_id, target_key, target_label, kind,
 before_data, after_data, payload, note, status,
 created_by, created_by_name, client_hint, reviewed_by, review_note,
 created_at, reviewed_at
FROM edit_suggestions WHERE id = $1
 AND (
 CASE edit_suggestions.target_type
 WHEN 'performance' THEN EXISTS (
 SELECT 1 FROM performances p JOIN streams st ON st.id = p.stream_id
 WHERE p.id = edit_suggestions.target_id AND {{RESTRICTION}}
 )
 WHEN 'stream' THEN EXISTS (
 SELECT 1 FROM streams st
 WHERE st.id = edit_suggestions.target_key AND {{RESTRICTION}}
 )
 -- 配信に紐付かない対象は秘匿の対象外。**明示した種類だけ通す** ──
 -- ELSE TRUE にすると、将来知らない target_type が増えたときに公開側へ倒れる。
 WHEN 'song' THEN TRUE
 WHEN 'artist' THEN TRUE
 ELSE FALSE
 END
 )
