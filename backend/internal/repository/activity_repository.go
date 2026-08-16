package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

// ActivityRepository は訪客／利用者のページ表示を日単位で集約して扱う。
type ActivityRepository struct {
	db *sql.DB
}

func NewActivityRepository(db *sql.DB) *ActivityRepository {
	return &ActivityRepository{db: db}
}

// RecordVisit は同じ UTC 日・IP・利用者の行を upsert する。
// actorKey は NULL の user_id を一意制約で扱うための内部キー。
func (r *ActivityRepository) RecordVisit(
	now time.Time,
	ip string,
	userID *uuid.UUID,
	username, path, userAgent string,
) error {
	actorKey := "anonymous"
	if userID != nil {
		actorKey = userID.String()
	}
	_, err := r.db.Exec(`
		INSERT INTO visitor_activity
			(visit_date, ip_address, user_id, actor_key, username_snapshot,
			 first_seen, last_seen, page_views, last_path, user_agent)
		VALUES (($1 AT TIME ZONE 'UTC')::date, $2::inet, $3, $4, $5, $1, $1, 1, $6, $7)
		ON CONFLICT (visit_date, ip_address, actor_key) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			username_snapshot = EXCLUDED.username_snapshot,
			last_seen = GREATEST(visitor_activity.last_seen, EXCLUDED.last_seen),
			page_views = visitor_activity.page_views + 1,
			last_path = EXCLUDED.last_path,
			user_agent = EXCLUDED.user_agent`,
		now, ip, userID, actorKey, username, path, userAgent)
	if err != nil {
		return fmt.Errorf("record visitor activity: %w", err)
	}
	return nil
}

type ActivityFilter struct {
	Since  time.Time
	Kind   string
	Search string
	Limit  int
	Offset int
}

// ListActivity は日別の活動を最終表示順で返す。
func (r *ActivityRepository) ListActivity(filter ActivityFilter) ([]models.VisitorActivity, int, error) {
	where, args := activityWhere(filter)

	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM visitor_activity a
		LEFT JOIN users u ON u.id = a.user_id `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count visitor activity: %w", err)
	}

	args = append(args, filter.Limit, filter.Offset)
	rows, err := r.db.Query(`
		SELECT a.id, a.visit_date, host(a.ip_address), a.user_id,
		       COALESCE(u.username, NULLIF(a.username_snapshot, ''), ''),
		       COALESCE(u.display_name, ''),
		       a.first_seen, a.last_seen, a.page_views, a.last_path, a.user_agent
		FROM visitor_activity a
		LEFT JOIN users u ON u.id = a.user_id
		`+where+`
		ORDER BY a.last_seen DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list visitor activity: %w", err)
	}
	defer rows.Close()

	items := make([]models.VisitorActivity, 0)
	for rows.Next() {
		var item models.VisitorActivity
		if err := rows.Scan(
			&item.ID, &item.VisitDate, &item.IPAddress, &item.UserID,
			&item.Username, &item.DisplayName, &item.FirstSeen, &item.LastSeen,
			&item.PageViews, &item.LastPath, &item.UserAgent,
		); err != nil {
			return nil, 0, fmt.Errorf("scan visitor activity: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate visitor activity: %w", err)
	}
	return items, total, nil
}

func activityWhere(filter ActivityFilter) (string, []any) {
	parts := []string{"a.last_seen >= $1"}
	args := []any{filter.Since}

	switch filter.Kind {
	case "anonymous":
		parts = append(parts, "a.user_id IS NULL")
	case "authenticated":
		parts = append(parts, "a.user_id IS NOT NULL")
	}

	if q := strings.TrimSpace(filter.Search); q != "" {
		args = append(args, "%"+q+"%")
		pos := fmt.Sprint(len(args))
		parts = append(parts, `(host(a.ip_address) ILIKE $`+pos+
			` OR COALESCE(u.username, a.username_snapshot, '') ILIKE $`+pos+
			` OR COALESCE(u.display_name, '') ILIKE $`+pos+`)`)
	}

	return "WHERE " + strings.Join(parts, " AND "), args
}

func (r *ActivityRepository) ActivityStats(since time.Time) (models.ActivityStats, error) {
	var stats models.ActivityStats
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT host(ip_address)),
		       COUNT(DISTINCT user_id) FILTER (WHERE user_id IS NOT NULL),
		       COALESCE(SUM(page_views), 0),
		       COUNT(DISTINCT host(ip_address)) FILTER (WHERE user_id IS NULL)
		FROM visitor_activity
		WHERE last_seen >= $1`, since).Scan(
		&stats.UniqueIPs, &stats.AuthenticatedUsers, &stats.PageViews, &stats.AnonymousIPs,
	)
	if err != nil {
		return stats, fmt.Errorf("visitor activity stats: %w", err)
	}
	return stats, nil
}

func (r *ActivityRepository) UserSummaries(since time.Time) ([]models.UserActivitySummary, error) {
	rows, err := r.db.Query(`
		SELECT u.id,
		       COALESCE(latest.ip_address, ''),
		       latest.last_seen,
		       COALESCE(period.page_views, 0),
		       COALESCE(period.distinct_ips, 0),
		       COALESCE(sess.active_sessions, 0)
		FROM users u
		LEFT JOIN LATERAL (
			SELECT host(a.ip_address) AS ip_address, a.last_seen
			FROM visitor_activity a
			WHERE a.user_id = u.id
			ORDER BY a.last_seen DESC
			LIMIT 1
		) latest ON TRUE
		LEFT JOIN LATERAL (
			SELECT SUM(a.page_views)::BIGINT AS page_views,
			       COUNT(DISTINCT host(a.ip_address))::BIGINT AS distinct_ips
			FROM visitor_activity a
			WHERE a.user_id = u.id AND a.last_seen >= $1
		) period ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::BIGINT AS active_sessions
			FROM sessions s
			WHERE s.user_id = u.id AND s.expires_at > NOW()
		) sess ON TRUE
		ORDER BY u.username`, since)
	if err != nil {
		return nil, fmt.Errorf("list user activity summaries: %w", err)
	}
	defer rows.Close()

	items := make([]models.UserActivitySummary, 0)
	for rows.Next() {
		var item models.UserActivitySummary
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&item.UserID, &item.LastIPAddress, &lastSeen, &item.PageViews,
			&item.DistinctIPs, &item.ActiveSessions,
		); err != nil {
			return nil, fmt.Errorf("scan user activity summary: %w", err)
		}
		if lastSeen.Valid {
			item.LastSeen = &lastSeen.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user activity summaries: %w", err)
	}
	return items, nil
}

func (r *ActivityRepository) DeleteBefore(cutoff time.Time) (int64, error) {
	result, err := r.db.Exec(`DELETE FROM visitor_activity WHERE last_seen < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge visitor activity: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("visitor activity rows affected: %w", err)
	}
	return n, nil
}
