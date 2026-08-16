package service

import (
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

const (
	defaultActivityRetentionDays = 30
	maxActivityRetentionDays     = 365
	maxActivityPathLength        = 512
	maxActivityUserAgentLength   = 512
)

type ActivityService struct {
	repo          *repository.ActivityRepository
	retentionDays int
	now           func() time.Time

	cleanupMu   sync.Mutex
	lastCleanup time.Time
}

func NewActivityService(repo *repository.ActivityRepository, retentionDays int) *ActivityService {
	if retentionDays < 1 {
		retentionDays = defaultActivityRetentionDays
	}
	if retentionDays > maxActivityRetentionDays {
		retentionDays = maxActivityRetentionDays
	}
	return &ActivityService{
		repo:          repo,
		retentionDays: retentionDays,
		now:           time.Now,
	}
}

func (s *ActivityService) RetentionDays() int {
	return s.retentionDays
}

func (s *ActivityService) RecordVisit(ip, path, userAgent string, user *models.User) error {
	parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		// IP が分からない行を作ると全訪客が同じ行に集約されるため、記録しない。
		return nil
	}
	ip = parsed.Unmap().String()
	path = sanitizeActivityPath(path)
	userAgent = truncateRunes(strings.TrimSpace(userAgent), maxActivityUserAgentLength)

	var userID *uuid.UUID
	username := ""
	if user != nil {
		id := user.ID
		userID = &id
		username = user.Username
	}

	now := s.now().UTC()
	if err := s.repo.RecordVisit(now, ip, userID, username, path, userAgent); err != nil {
		return err
	}
	s.maybeCleanup(now)
	return nil
}

func (s *ActivityService) List(days, page, limit int, kind, search string) ([]models.VisitorActivity, int, error) {
	days, page, limit = normalizeActivityQuery(days, page, limit, s.retentionDays)
	return s.repo.ListActivity(repository.ActivityFilter{
		Since:  s.now().UTC().Add(-time.Duration(days) * 24 * time.Hour),
		Kind:   normalizeActivityKind(kind),
		Search: truncateRunes(strings.TrimSpace(search), 128),
		Limit:  limit,
		Offset: (page - 1) * limit,
	})
}

func (s *ActivityService) Stats(days int) (models.ActivityStats, error) {
	days, _, _ = normalizeActivityQuery(days, 1, 50, s.retentionDays)
	return s.repo.ActivityStats(s.now().UTC().Add(-time.Duration(days) * 24 * time.Hour))
}

func (s *ActivityService) UserSummaries(days int) ([]models.UserActivitySummary, error) {
	days, _, _ = normalizeActivityQuery(days, 1, 50, s.retentionDays)
	return s.repo.UserSummaries(s.now().UTC().Add(-time.Duration(days) * 24 * time.Hour))
}

func (s *ActivityService) maybeCleanup(now time.Time) {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < 12*time.Hour {
		return
	}
	s.lastCleanup = now
	cutoff := now.Add(-time.Duration(s.retentionDays) * 24 * time.Hour)
	if n, err := s.repo.DeleteBefore(cutoff); err != nil {
		logger.Warnf("訪客活動の期限切れデータ削除に失敗しました: %v", err)
	} else if n > 0 {
		logger.Infof("期限切れの訪客活動を %d 件削除しました", n)
	}
}

func sanitizeActivityPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "/"
	}
	// query / fragment は OAuth code や検索語を含み得るので保存しない。
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	return truncateRunes(path, maxActivityPathLength)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func normalizeActivityKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "anonymous", "authenticated":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "all"
	}
}

func normalizeActivityQuery(days, page, limit, retention int) (int, int, int) {
	if days < 1 {
		days = 7
	}
	if days > retention {
		days = retention
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return days, page, limit
}
