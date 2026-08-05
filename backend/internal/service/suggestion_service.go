package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// TargetEditor は修正提案の対象（曲・アーティスト・歌唱記録）が満たすインターフェース。
// SongService / ArtistService / PerformanceService が実装する。
type TargetEditor interface {
	GetEditableFields(id uuid.UUID) (map[string]string, string, error)
	ApplyEditableFields(id uuid.UUID, fields map[string]string) error
}

var (
	ErrInvalidTarget      = fmt.Errorf("対象の種類が不正です")
	ErrTargetNotFound     = fmt.Errorf("対象が見つかりません")
	ErrNoChange           = fmt.Errorf("変更がありません")
	ErrSuggestionNotFound = fmt.Errorf("提案が見つかりません")
	ErrAlreadyReviewed    = fmt.Errorf("この提案は既に処理済みです")
	ErrTooManySuggestions = fmt.Errorf("提案の送信が多すぎます。しばらく待ってから再度お試しください")
)

// 投稿の rate limit。ログイン済みは緩め、匿名は厳しめ。
// 再生中のワンタップ通報を載せると件数が跳ねるため、DB 側の件数で素朴に抑える。
const (
	suggestionRateWindowSeconds = 600 // 10 分
	suggestionRateLimitUser     = 30
	suggestionRateLimitAnon     = 8
)

// FieldConflict は承認時に検出した「提案時点の値」と「現在の値」のズレ。
type FieldConflict struct {
	Expected string `json:"expected"` // 提案時点のスナップショット（before_data）
	Current  string `json:"current"`  // 現在の値
}

// ConflictError は提案の作成後に対象が別途編集されていたことを示す。
// そのまま承認すると他人の編集を黙って巻き戻すため、承認を止めて人手の判断に回す。
type ConflictError struct {
	Fields map[string]FieldConflict
}

func (e *ConflictError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "提案の作成後に対象が変更されています（" + strings.Join(keys, ", ") + "）"
}

// SuggestionActor は提案の投稿者。User が nil なら匿名投稿。
// ClientHint は匿名投稿の同一性の手がかり（IP のハッシュ。生 IP は保持しない）。
type SuggestionActor struct {
	User       *models.User
	ClientHint string
}

// SuggestionService は閲覧モードからの修正提案の投稿・レビュー・反映を担う。
type SuggestionService struct {
	repo    *repository.SuggestionRepository
	editors map[string]TargetEditor
}

func NewSuggestionService(
	repo *repository.SuggestionRepository,
	songService *SongService,
	artistService *ArtistService,
	performanceService *PerformanceService,
) *SuggestionService {
	return &SuggestionService{
		repo: repo,
		editors: map[string]TargetEditor{
			"song":        songService,
			"artist":      artistService,
			"performance": performanceService,
		},
	}
}

// Create は修正提案を登録する。対象の現状を before、提案値を after として保存する。
// 変更が無い（全フィールドが現状と同じ）場合は ErrNoChange。
func (s *SuggestionService) Create(req *dto.CreateSuggestionRequest, actor SuggestionActor) (*models.EditSuggestion, error) {
	editor, ok := s.editors[req.TargetType]
	if !ok {
		return nil, ErrInvalidTarget
	}
	id, err := uuid.Parse(req.TargetID)
	if err != nil {
		return nil, ErrInvalidTarget
	}

	if err := s.checkRate(actor); err != nil {
		return nil, err
	}

	before, label, err := editor.GetEditableFields(id)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrTargetNotFound
	}

	// after = before に提案値を上書き（既知フィールドのみ採用）。差分の有無を確認する。
	after := make(map[string]string, len(before))
	changed := false
	for k, cur := range before {
		if v, ok := req.Fields[k]; ok {
			nv := strings.TrimSpace(v)
			after[k] = nv
			if nv != cur {
				changed = true
			}
		} else {
			after[k] = cur
		}
	}
	if !changed {
		return nil, ErrNoChange
	}

	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	sug := &models.EditSuggestion{
		TargetType:  req.TargetType,
		TargetID:    id,
		TargetLabel: label,
		Kind:        "field",
		BeforeData:  beforeJSON,
		AfterData:   afterJSON,
		Note:        strings.TrimSpace(req.Note),
		ClientHint:  actor.ClientHint,
	}
	if actor.User != nil {
		sug.CreatedBy = &actor.User.ID
		sug.CreatedByName = displayNameOf(actor.User)
	}
	created, err := s.repo.Create(sug)
	if err != nil {
		return nil, err
	}
	logger.Infof("edit suggestion created: %s %s (%s) by %s", req.TargetType, id, label, actorLabel(actor))
	return created, nil
}

// checkRate は直近ウィンドウ内の投稿数で投稿を制限する。
// 判定に失敗した場合（DB エラー）は投稿を止めない：濫用対策より投稿できることを優先する。
func (s *SuggestionService) checkRate(actor SuggestionActor) error {
	limit := suggestionRateLimitAnon
	var by *uuid.UUID
	if actor.User != nil {
		limit = suggestionRateLimitUser
		by = &actor.User.ID
	}
	n, err := s.repo.CountRecentBy(by, actor.ClientHint, suggestionRateWindowSeconds)
	if err != nil {
		logger.Warnf("suggestion rate check failed: %v", err)
		return nil
	}
	if n >= limit {
		return ErrTooManySuggestions
	}
	return nil
}

// List は status（空なら全件）で絞った提案一覧を返す。
func (s *SuggestionService) List(status string, page, limit int) (*dto.SuggestionListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	items, total, err := s.repo.List(status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	resp := make([]dto.SuggestionResponse, len(items))
	for i, it := range items {
		resp[i] = s.toSuggestionResponse(it)
	}
	return &dto.SuggestionListResponse{
		Suggestions: resp,
		Pagination: dto.PaginationResponse{
			Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// CountPending は未処理提案数を返す（バッジ表示用）。
func (s *SuggestionService) CountPending() (int, error) {
	return s.repo.CountPending()
}

// Approve は提案を対象へ反映し approved にする。反映に失敗した場合はステータスを変えない。
//
// force=false のとき、提案時点のスナップショット（before_data）と現在値を突き合わせ、
// ズレていれば反映せず status=conflict にして *ConflictError を返す。
// これが無いと「古い提案の承認」が他人の編集を黙って巻き戻す（lost update）。
// force=true は管理者が差分を見た上で「現在値を上書きしてよい」と判断した場合。
func (s *SuggestionService) Approve(id uuid.UUID, reviewer *models.User, force bool) error {
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}
	// conflict は「人手の判断待ち」なので、force 付きの再承認だけ受け付ける。
	if sug.Status == "approved" || sug.Status == "rejected" {
		return ErrAlreadyReviewed
	}
	if sug.Status == "conflict" && !force {
		return ErrAlreadyReviewed
	}
	editor, ok := s.editors[sug.TargetType]
	if !ok {
		return ErrInvalidTarget
	}

	var fields map[string]string
	if err := json.Unmarshal(sug.AfterData, &fields); err != nil {
		return fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}

	if !force {
		conflicts, err := s.detectConflicts(editor, sug)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			ce := &ConflictError{Fields: conflicts}
			if err := s.repo.UpdateStatus(id, "conflict", reviewerID(reviewer), ce.Error()); err != nil {
				return err
			}
			logger.Warnf("edit suggestion conflict: %s (%s)", id, ce.Error())
			return ce
		}
	}

	if err := editor.ApplyEditableFields(sug.TargetID, fields); err != nil {
		return err // 反映失敗（例：対象削除済み・名前衝突）。ステータスは変えず残す。
	}
	note := ""
	if force {
		note = "衝突を承知の上で上書き承認"
	}
	if err := s.repo.UpdateStatus(id, "approved", reviewerID(reviewer), note); err != nil {
		return err
	}
	logger.Infof("edit suggestion approved: %s (force=%v)", id, force)
	return nil
}

// detectConflicts は提案時点の before_data と対象の現在値を比べ、ズレたフィールドを返す。
// 提案が触っていないフィールド（before と after が同じ）のズレは無視する。
func (s *SuggestionService) detectConflicts(editor TargetEditor, sug *models.EditSuggestion) (map[string]FieldConflict, error) {
	current, _, err := editor.GetEditableFields(sug.TargetID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrTargetNotFound
	}

	var before, after map[string]string
	if err := json.Unmarshal(sug.BeforeData, &before); err != nil {
		return nil, fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	if err := json.Unmarshal(sug.AfterData, &after); err != nil {
		return nil, fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}

	conflicts := map[string]FieldConflict{}
	for k, want := range before {
		if after[k] == want {
			continue // この提案が変更しないフィールドは衝突判定の対象外
		}
		if cur, ok := current[k]; ok && cur != want {
			conflicts[k] = FieldConflict{Expected: want, Current: cur}
		}
	}
	return conflicts, nil
}

// Reject は提案を却下する（対象は変更しない）。
func (s *SuggestionService) Reject(id uuid.UUID, reviewer *models.User, note string) error {
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}
	if sug.Status == "approved" || sug.Status == "rejected" {
		return ErrAlreadyReviewed
	}
	return s.repo.UpdateStatus(id, "rejected", reviewerID(reviewer), strings.TrimSpace(note))
}

func reviewerID(u *models.User) *uuid.UUID {
	if u == nil || u.ID == uuid.Nil {
		return nil // API_AUTH_TOKEN 経由の疑似管理者は DB 上のユーザーではない
	}
	return &u.ID
}

func displayNameOf(u *models.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func actorLabel(actor SuggestionActor) string {
	if actor.User != nil {
		return displayNameOf(actor.User)
	}
	if actor.ClientHint != "" {
		return "anonymous(" + actor.ClientHint + ")"
	}
	return "anonymous"
}

func (s *SuggestionService) toSuggestionResponse(m models.EditSuggestion) dto.SuggestionResponse {
	before := map[string]string{}
	after := map[string]string{}
	_ = json.Unmarshal(m.BeforeData, &before)
	_ = json.Unmarshal(m.AfterData, &after)

	resp := dto.SuggestionResponse{
		ID:            m.ID,
		TargetType:    m.TargetType,
		TargetID:      m.TargetID,
		TargetLabel:   m.TargetLabel,
		Kind:          m.Kind,
		Before:        before,
		After:         after,
		Note:          m.Note,
		Status:        m.Status,
		CreatedBy:     m.CreatedBy,
		CreatedByName: m.CreatedByName,
		ReviewNote:    m.ReviewNote,
		CreatedAt:     m.CreatedAt,
		ReviewedAt:    m.ReviewedAt,
	}

	// pending の間に対象が変わっていないかを一覧の時点で見せる（承認前に気付けるように）。
	if m.Status == "pending" || m.Status == "conflict" {
		if editor, ok := s.editors[m.TargetType]; ok {
			if conflicts, err := s.detectConflicts(editor, &m); err == nil && len(conflicts) > 0 {
				resp.Conflicts = map[string]dto.FieldConflict{}
				for k, c := range conflicts {
					resp.Conflicts[k] = dto.FieldConflict{Expected: c.Expected, Current: c.Current}
				}
			}
		}
	}
	return resp
}
