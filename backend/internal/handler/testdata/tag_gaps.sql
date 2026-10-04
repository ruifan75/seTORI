WITH cached AS (
 SELECT s.id AS stream_id, (e->>'start')::int AS start_sec,
        COALESCE(e->>'name', '') AS name,
        ARRAY(SELECT jsonb_array_elements_text(e->'tags')) AS tags,
        'comment' AS src
 FROM streams s CROSS JOIN LATERAL jsonb_array_elements(s.comment_songs) e
 WHERE e ? 'tags' AND jsonb_array_length(e->'tags') > 0
 UNION ALL
 SELECT s.id, (e->>'start_seconds')::int,
        COALESCE(e->>'name', ''),
        ARRAY(SELECT jsonb_array_elements_text(e->'tags')),
        'holodex'
 FROM streams s CROSS JOIN LATERAL jsonb_array_elements(s.holodex_songs_normalized) e
 WHERE e ? 'tags' AND jsonb_array_length(e->'tags') > 0
),
perf AS (
 SELECT p.id, p.stream_id, p.start_seconds, so.name AS song_name,
        COALESCE(array_agg(ppt.tag_id) FILTER (WHERE ppt.tag_id IS NOT NULL), '{}') AS tags
 FROM performances p
 JOIN songs so ON so.id = p.song_id
 LEFT JOIN performance_performance_tags ppt ON ppt.performance_id = p.id
 GROUP BY p.id, so.name
),
paired AS (
 SELECT DISTINCT ON (c.src, c.stream_id, c.start_sec)
        pf.id AS performance_id, pf.tags AS perf_tags,
        c.src, c.name AS cached_name, c.tags AS cached_tags,
        -- 空白を落とした部分一致で「同じ曲を指していそうか」を見る。
        -- 解析側の曲名にはバージョン表記が残る（"幾億光年 piano ver."）ので完全一致では見られない。
        (position(lower(regexp_replace(pf.song_name, '[[:space:]　]', '', 'g'))
                  in lower(regexp_replace(c.name, '[[:space:]　]', '', 'g'))) > 0
         OR position(lower(regexp_replace(c.name, '[[:space:]　]', '', 'g'))
                  in lower(regexp_replace(pf.song_name, '[[:space:]　]', '', 'g'))) > 0) AS name_matches
 FROM cached c
 JOIN perf pf ON pf.stream_id = c.stream_id AND abs(pf.start_seconds - c.start_sec) <= 30
 ORDER BY c.src, c.stream_id, c.start_sec, abs(pf.start_seconds - c.start_sec)
),
gaps AS (
 SELECT pr.performance_id, pr.src, pr.cached_name, pr.name_matches, m.tag_id
 FROM paired pr
 CROSS JOIN LATERAL (
  SELECT t AS tag_id FROM unnest(pr.cached_tags) t
  WHERE NOT (t = ANY(pr.perf_tags))
    AND NOT EXISTS (
      SELECT 1 FROM performance_tag_checks k
      WHERE k.performance_id = pr.performance_id AND k.tag_id = t)
 ) m
)
SELECT g.performance_id, p.stream_id, st.title, p.start_seconds,
       p.song_id, so.name, so.original_artist,
       COALESCE((SELECT array_agg(ppt.tag_id ORDER BY ppt.tag_id)
                 FROM performance_performance_tags ppt
                 WHERE ppt.performance_id = g.performance_id), '{}') AS current_tags,
       array_agg(DISTINCT g.tag_id) AS missing_tags,
       array_agg(DISTINCT g.src) AS sources,
       min(g.cached_name) AS cached_name,
       bool_or(g.name_matches) AS name_matches
FROM gaps g
JOIN performances p ON p.id = g.performance_id
JOIN streams st ON st.id = p.stream_id
JOIN songs so ON so.id = p.song_id
WHERE {{RESTRICTION}}
GROUP BY g.performance_id, p.stream_id, st.title, st.stream_date,
         p.start_seconds, p.song_id, so.name, so.original_artist
ORDER BY st.stream_date DESC NULLS LAST, p.start_seconds
LIMIT $1
