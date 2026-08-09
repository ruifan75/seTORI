package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/auth"
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

// ValidationError は投稿内容が不正であることを示す。
// サーバー側の障害（500）と区別して 400 で返すために型で持つ。
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, a...)}
}

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

// 提案の種別。
const (
	KindField       = "field"        // 既存レコードのフィールド差し替え
	KindMissingSong = "perf.missing" // 未登録曲の追加報告
	KindSongSwap    = "perf.meta"    // 歌唱の曲の差し替え（「この曲ではない」）
)

// SuggestionService は閲覧モードからの修正提案の投稿・レビュー・反映を担う。
type SuggestionService struct {
	repo         *repository.SuggestionRepository
	settingsRepo *repository.AppSettingsRepository
	editors      map[string]TargetEditor
	missing      MissingSongCreator
	swapper      SongSwapper
}

func NewSuggestionService(
	repo *repository.SuggestionRepository,
	settingsRepo *repository.AppSettingsRepository,
	songService *SongService,
	artistService *ArtistService,
	performanceService *PerformanceService,
) *SuggestionService {
	return &SuggestionService{
		repo:         repo,
		settingsRepo: settingsRepo,
		editors: map[string]TargetEditor{
			"song":        songService,
			"artist":      artistService,
			"performance": performanceService,
		},
		missing: performanceService,
		swapper: performanceService,
	}
}

// Create は修正提案を登録する。対象の現状を before、提案値を after として保存する。
// 変更が無い（全フィールドが現状と同じ）場合は ErrNoChange。
func (s *SuggestionService) Create(req *dto.CreateSuggestionRequest, actor SuggestionActor) (*models.EditSuggestion, error) {
	if req.Kind == KindMissingSong {
		return s.createMissingSong(req, actor)
	}
	if req.Kind == KindSongSwap {
		return s.createSongSwap(req, actor)
	}

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

	// 複数人が同じズレを指摘していれば人手を待たずに反映する。
	// 失敗しても提案の投稿自体は成功として扱う（あくまで自動処理の上乗せ）。
	if err := s.tryAutoApply(req.TargetType, id); err != nil {
		logger.Warnf("auto apply check failed (%s %s): %v", req.TargetType, id, err)
	}
	return created, nil
}

// ========== 未登録曲の追加提案（perf.missing）==========

// MissingSongCreator は「この配信のこの時点に曲がある」という報告を実際の歌唱記録にする。
// PerformanceService が実装する。
type MissingSongCreator interface {
	CreateFromMissingSong(p dto.MissingSongPayload) error
	StreamLabel(streamID string) (string, error)
	// OverlappingPerformances は提案の時間帯と重なる既存の歌唱を返す（レビュー時の注意喚起用）。
	OverlappingPerformances(streamID string, start, end int) ([]dto.OverlapInfo, error)
}

// createMissingSong は未登録曲の報告を登録する。
// 既存レコードの修正ではないので before/after は持たず、payload に内容を入れる。
// 対象は配信（UUID を持たないので target_key に動画 ID を入れる）。
func (s *SuggestionService) createMissingSong(req *dto.CreateSuggestionRequest, actor SuggestionActor) (*models.EditSuggestion, error) {
	if s.missing == nil {
		return nil, ErrInvalidTarget
	}
	p := req.Payload
	if p == nil {
		return nil, invalid("追加したい曲の内容がありません")
	}
	p.StreamID = strings.TrimSpace(p.StreamID)
	p.SongName = strings.TrimSpace(p.SongName)
	p.OriginalArtist = strings.TrimSpace(p.OriginalArtist)
	if p.StreamID == "" {
		return nil, invalid("配信が指定されていません")
	}
	if p.SongName == "" {
		return nil, invalid("曲名は必須です")
	}
	if p.StartSeconds < 0 || p.EndSeconds < 0 {
		return nil, invalid("時間が不正です")
	}
	if p.EndSeconds != 0 && p.EndSeconds <= p.StartSeconds {
		return nil, ErrInvalidTimeRange
	}

	// 配信が実在するか（存在しない動画 ID を溜め込まないため）
	streamLabel, err := s.missing.StreamLabel(p.StreamID)
	if err != nil {
		return nil, err
	}
	if streamLabel == "" {
		return nil, ErrTargetNotFound
	}

	if err := s.checkRate(actor); err != nil {
		return nil, err
	}

	label := p.SongName
	if p.OriginalArtist != "" {
		label += " / " + p.OriginalArtist
	}
	label += "（" + streamLabel + "）"

	payloadJSON, _ := json.Marshal(p)
	sug := &models.EditSuggestion{
		TargetType:  "stream",
		TargetID:    uuid.Nil, // 配信は UUID を持たない。同一性は TargetKey で見る
		TargetKey:   p.StreamID,
		TargetLabel: label,
		Kind:        KindMissingSong,
		BeforeData:  []byte("{}"),
		AfterData:   []byte("{}"),
		Payload:     payloadJSON,
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
	logger.Infof("missing song suggestion created: %s @%ds (%s) by %s",
		p.SongName, p.StartSeconds, p.StreamID, actorLabel(actor))
	return created, nil
}

// approveMissingSong は未登録曲の報告を承認し、歌唱記録を作る。
// 曲が未登録なら曲も作られる（既存の findOrCreateSong と同じ経路）。
func (s *SuggestionService) approveMissingSong(sug *models.EditSuggestion, reviewer *models.User) error {
	if s.missing == nil {
		return ErrInvalidTarget
	}
	var p dto.MissingSongPayload
	if err := json.Unmarshal(sug.Payload, &p); err != nil {
		return fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	if err := s.missing.CreateFromMissingSong(p); err != nil {
		return err // 例：同じ時間に既に歌唱がある
	}
	if err := s.repo.UpdateStatus(sug.ID, "approved", reviewerID(reviewer), "歌唱記録を作成"); err != nil {
		return err
	}
	logger.Infof("missing song suggestion approved: %s (%s @%ds)", sug.ID, p.SongName, p.StartSeconds)
	return nil
}

// ========== 曲の差し替え提案（perf.meta）==========

// SongSwapper は「この歌唱は別の曲だ」という指摘を反映する。PerformanceService が実装する。
type SongSwapper interface {
	// SongLabelOf は歌唱の現在の曲名と、対象の表示ラベルを返す。歌唱が無ければ空文字。
	SongLabelOf(performanceID uuid.UUID) (songName string, label string, err error)
	// ApplySongSwap は歌唱の曲を差し替える（未登録の曲名なら曲も作る）。
	ApplySongSwap(performanceID uuid.UUID, p dto.SongSwapPayload) error
}

// createSongSwap は曲の差し替え提案を登録する。
// 曲の同一性は文字列の差分では表せない（別の曲マスタへ繋ぎ替える操作）ため、
// before/after ではなく payload で持つ。
func (s *SuggestionService) createSongSwap(req *dto.CreateSuggestionRequest, actor SuggestionActor) (*models.EditSuggestion, error) {
	if s.swapper == nil {
		return nil, ErrInvalidTarget
	}
	p := req.SongSwap
	if p == nil {
		return nil, invalid("差し替え先の曲がありません")
	}
	perfID, err := uuid.Parse(req.TargetID)
	if err != nil {
		return nil, ErrInvalidTarget
	}
	p.SongID = strings.TrimSpace(p.SongID)
	p.SongName = strings.TrimSpace(p.SongName)
	p.OriginalArtist = strings.TrimSpace(p.OriginalArtist)
	if p.SongID == "" && p.SongName == "" {
		return nil, invalid("差し替え先の曲を選ぶか、曲名を入力してください")
	}
	if p.SongID != "" {
		if _, err := uuid.Parse(p.SongID); err != nil {
			return nil, invalid("差し替え先の曲の指定が不正です")
		}
	}

	currentSong, label, err := s.swapper.SongLabelOf(perfID)
	if err != nil {
		return nil, err
	}
	if label == "" {
		return nil, ErrTargetNotFound
	}
	// 今と同じ曲を指しているなら提案する意味がない
	if p.SongID == "" && p.SongName == currentSong {
		return nil, ErrNoChange
	}
	p.CurrentSongName = currentSong

	if err := s.checkRate(actor); err != nil {
		return nil, err
	}

	swapJSON, _ := json.Marshal(p)
	sug := &models.EditSuggestion{
		TargetType:  "performance",
		TargetID:    perfID,
		TargetLabel: label,
		Kind:        KindSongSwap,
		BeforeData:  []byte("{}"),
		AfterData:   []byte("{}"),
		Payload:     swapJSON,
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
	logger.Infof("song swap suggestion created: %s → %s%s by %s",
		currentSong, p.SongName, p.SongID, actorLabel(actor))
	return created, nil
}

// approveSongSwap は曲の差し替えを反映する。
// 提案後に曲が別途差し替えられていれば、force が無い限り止めて人手の判断に回す
// （field と同じ lost update 対策）。
func (s *SuggestionService) approveSongSwap(sug *models.EditSuggestion, reviewer *models.User, force bool) error {
	if s.swapper == nil {
		return ErrInvalidTarget
	}
	var p dto.SongSwapPayload
	if err := json.Unmarshal(sug.Payload, &p); err != nil {
		return fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}

	if !force {
		currentSong, label, err := s.swapper.SongLabelOf(sug.TargetID)
		if err != nil {
			return err
		}
		if label == "" {
			return ErrTargetNotFound
		}
		if p.CurrentSongName != "" && currentSong != p.CurrentSongName {
			ce := &ConflictError{Fields: map[string]FieldConflict{
				"song": {Expected: p.CurrentSongName, Current: currentSong},
			}}
			if err := s.repo.UpdateStatus(sug.ID, "conflict", reviewerID(reviewer), ce.Error()); err != nil {
				return err
			}
			return ce
		}
	}

	if err := s.swapper.ApplySongSwap(sug.TargetID, p); err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(sug.ID, "approved", reviewerID(reviewer), "曲を差し替え"); err != nil {
		return err
	}
	logger.Infof("song swap approved: %s (%s → %s%s)", sug.ID, p.CurrentSongName, p.SongName, p.SongID)
	return nil
}

// ========== 自動適用 ==========
//
// 再生中のワンタップ通報は件数が多く、その大半は「開始/終了が数秒ずれている」だけの
// 単純な指摘になる。これを全部人手のレビューに積むと管理者が溺れるため、
// 十分に裏が取れたものは自動で反映する。
//
// 条件（すべて満たすときだけ）：
//   - 対象が歌唱記録で、フィールドが開始/終了秒
//   - ログイン済みユーザーからの提案であること（匿名は数に入れない）
//   - 異なる利用者が MinVotes 人以上、同じフィールドについて提案している
//   - その提案値のばらつきが MaxSpreadSeconds 秒以内（意見が割れていない）
//   - 現在値からの差が MaxDeltaSeconds 秒以内（大きな変更は人が見る）
//   - 提案時点のスナップショットが現在値と一致（対象が別途編集されていない）
//
// 採用値は中央値。極端な値1つに引きずられないようにするため。
//
// しきい値は運用しながら調整するものなので app_settings に置く（管理画面から変更可能）。

const settingsKeyAutoApply = "suggestion_auto_apply"

// AutoApplySettings は timing 提案の自動適用条件。
type AutoApplySettings struct {
	Enabled          bool `json:"enabled"`
	MinVotes         int  `json:"min_votes"`          // 何人以上の一致で反映するか
	MaxSpreadSeconds int  `json:"max_spread_seconds"` // 提案値のばらつきの許容
	MaxDeltaSeconds  int  `json:"max_delta_seconds"`  // 現在値からの差の許容
}

// DefaultAutoApplySettings は未設定時の既定値。
func DefaultAutoApplySettings() AutoApplySettings {
	return AutoApplySettings{Enabled: true, MinVotes: 2, MaxSpreadSeconds: 3, MaxDeltaSeconds: 5}
}

// GetAutoApplySettings は保存済みの設定（無ければ既定値）を返す。
func (s *SuggestionService) GetAutoApplySettings() AutoApplySettings {
	settings := DefaultAutoApplySettings()
	if s.settingsRepo != nil {
		if _, err := s.settingsRepo.Get(settingsKeyAutoApply, &settings); err != nil {
			logger.Warnf("auto apply settings load: %v", err)
		}
	}
	return settings
}

// clampAutoApply は極端な値で事故らないよう設定を安全な範囲へ丸める。
func clampAutoApply(in AutoApplySettings) AutoApplySettings {
	clamp := func(v, min, max int) int {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	// MinVotes の下限は 2：1票で反映するなら「提案」ではなく直接編集と変わらない
	in.MinVotes = clamp(in.MinVotes, 2, 20)
	in.MaxSpreadSeconds = clamp(in.MaxSpreadSeconds, 0, 60)
	in.MaxDeltaSeconds = clamp(in.MaxDeltaSeconds, 1, 300)
	return in
}

// UpdateAutoApplySettings は設定を保存する。
func (s *SuggestionService) UpdateAutoApplySettings(in AutoApplySettings) (AutoApplySettings, error) {
	if s.settingsRepo == nil {
		return AutoApplySettings{}, ErrInvalidTarget
	}
	in = clampAutoApply(in)
	if err := s.settingsRepo.Set(settingsKeyAutoApply, in); err != nil {
		return AutoApplySettings{}, err
	}
	logger.Infof("auto apply settings updated: %+v", in)
	return in, nil
}

// autoApplyFields は自動適用の対象フィールド（歌唱記録の時間軸のみ）。
var autoApplyFields = []string{"start_seconds", "end_seconds"}

// tryAutoApply は対象に溜まった pending 提案を見て、条件を満たすフィールドを自動反映する。
func (s *SuggestionService) tryAutoApply(targetType string, targetID uuid.UUID) error {
	if targetType != "performance" {
		return nil
	}
	cfg := s.GetAutoApplySettings()
	if !cfg.Enabled {
		return nil
	}
	editor, ok := s.editors[targetType]
	if !ok {
		return nil
	}
	current, _, err := editor.GetEditableFields(targetID)
	if err != nil || current == nil {
		return err
	}

	pending, err := s.repo.FindPendingTimingByTarget(targetType, targetID)
	if err != nil {
		return err
	}

	applied := map[string]string{}
	var usedIDs []uuid.UUID

	for _, field := range autoApplyFields {
		value, ids, ok := s.voteFor(pending, current, field, cfg)
		if !ok {
			continue
		}
		applied[field] = value
		usedIDs = append(usedIDs, ids...)
	}
	if len(applied) == 0 {
		return nil
	}

	if err := editor.ApplyEditableFields(targetID, applied); err != nil {
		// 反映できない（範囲が不正など）なら人手のレビューに残す
		logger.Warnf("auto apply rejected by target (%s): %v", targetID, err)
		return nil
	}
	for _, id := range usedIDs {
		if err := s.repo.UpdateStatus(id, "approved", nil, "複数の提案が一致したため自動適用"); err != nil {
			return err
		}
	}
	logger.Infof("auto applied %v to performance %s from %d suggestions", applied, targetID, len(usedIDs))
	return nil
}

// voteFor は1フィールドについて自動適用の可否を判定し、採用値と根拠になった提案 ID を返す。
func (s *SuggestionService) voteFor(pending []models.EditSuggestion, current map[string]string, field string, cfg AutoApplySettings) (string, []uuid.UUID, bool) {
	curValue, ok := current[field]
	if !ok {
		return "", nil, false
	}
	curSeconds, err := strconv.Atoi(curValue)
	if err != nil {
		return "", nil, false
	}

	// 1人1票（同じ人の複数提案は最新のものだけ数える）
	type vote struct {
		seconds int
		id      uuid.UUID
	}
	byUser := map[uuid.UUID]vote{}

	for _, sug := range pending {
		if sug.CreatedBy == nil {
			continue
		}
		var before, after map[string]string
		if json.Unmarshal(sug.BeforeData, &before) != nil || json.Unmarshal(sug.AfterData, &after) != nil {
			continue
		}
		proposed, ok := after[field]
		if !ok || proposed == before[field] {
			continue // このフィールドを変更しない提案
		}
		// 提案時点と現在値がズレている＝対象が別途編集済み。人手の判断に回す。
		if before[field] != curValue {
			continue
		}
		n, err := strconv.Atoi(proposed)
		if err != nil {
			continue
		}
		if abs(n-curSeconds) > cfg.MaxDeltaSeconds {
			continue // 大きな変更は自動で入れない
		}
		byUser[*sug.CreatedBy] = vote{seconds: n, id: sug.ID}
	}

	if len(byUser) < cfg.MinVotes {
		return "", nil, false
	}

	values := make([]int, 0, len(byUser))
	ids := make([]uuid.UUID, 0, len(byUser))
	for _, v := range byUser {
		values = append(values, v.seconds)
		ids = append(ids, v.id)
	}
	sort.Ints(values)
	if values[len(values)-1]-values[0] > cfg.MaxSpreadSeconds {
		return "", nil, false // 意見が割れている
	}
	return strconv.Itoa(median(values)), ids, true
}

// median は昇順に並んだ値の中央値。偶数個なら小さい側（開始/終了は整数秒で扱うため平均は取らない）。
func median(sorted []int) int {
	return sorted[(len(sorted)-1)/2]
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

// ListMine は自分が出した提案を返す。取り下げと結果の確認のための画面用。
// レビュー用の一覧と違い content:edit は要らない（自分の分しか見えない）。
func (s *SuggestionService) ListMine(user *models.User, status string, page, limit int) (*dto.SuggestionListResponse, error) {
	if user == nil {
		return nil, ErrSuggestionNotFound
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	items, total, err := s.repo.ListByCreator(user.ID, status, limit, (page-1)*limit)
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

// ListGrouped は提案を対象ごとにまとめて返す。
// 再生中のワンタップ通報は同じ歌唱に何件も集まるため、1件ずつではなく
// 対象単位で見比べて処理できるようにする。
func (s *SuggestionService) ListGrouped(status string, page, limit int) (*dto.SuggestionGroupListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	groups, total, err := s.repo.ListGroupedByTarget(status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}

	resp := make([]dto.SuggestionGroup, 0, len(groups))
	for _, g := range groups {
		items := make([]dto.SuggestionResponse, len(g.Suggestions))
		for i, it := range g.Suggestions {
			items[i] = s.toSuggestionResponse(it)
		}
		// 現在値はグループで1回だけ引く（提案ごとに引くと同じ対象を何度も読むことになる）
		current := map[string]string{}
		if editor, ok := s.editors[g.TargetType]; ok {
			if fields, _, err := editor.GetEditableFields(g.TargetID); err == nil && fields != nil {
				current = fields
			}
		}
		resp = append(resp, dto.SuggestionGroup{
			TargetType:  g.TargetType,
			TargetID:    g.TargetID,
			TargetKey:   g.TargetKey,
			TargetLabel: g.TargetLabel,
			Current:     current,
			Suggestions: items,
		})
	}
	return &dto.SuggestionGroupListResponse{
		Groups: resp,
		Pagination: dto.PaginationResponse{
			Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// BatchReview は複数の提案をまとめて承認/却下する。
// 1件でも失敗したら止めるのではなく、成功したものはそのまま残し、失敗の理由を個別に返す
// （同一対象への提案は互いに衝突しうるため、途中で止めると中途半端な状態になる）。
func (s *SuggestionService) BatchReview(ids []uuid.UUID, action string, reviewer *models.User, force bool, note string) *dto.BatchReviewResponse {
	resp := &dto.BatchReviewResponse{Results: make([]dto.BatchReviewResult, 0, len(ids))}
	for _, id := range ids {
		var err error
		switch action {
		case "approve":
			err = s.Approve(id, reviewer, force)
		case "reject":
			err = s.Reject(id, reviewer, note)
		default:
			err = ErrInvalidTarget
		}

		r := dto.BatchReviewResult{ID: id, OK: err == nil}
		if err != nil {
			r.Error = err.Error()
			var conflict *ConflictError
			if errors.As(err, &conflict) {
				r.Conflict = true
			}
			resp.Failed++
		} else {
			resp.Succeeded++
		}
		resp.Results = append(resp.Results, r)
	}
	return resp
}

// Merge は同一対象に集まった提案を、管理者が決めた値へ統合して反映する。
//
// 「どれか1つを丸ごと採用」では表せない決着のための操作：
// 3人が別々の秒数を出していて中央値にしたい、誰も出していない値にしたい、など。
// 反映後、指定された提案はすべて処理済みにする。採用値と一致していたものを承認、
// それ以外を却下として記録する（誰の指摘が通ったかを履歴に残すため）。
func (s *SuggestionService) Merge(req *dto.MergeSuggestionsRequest, reviewer *models.User) (*dto.MergeSuggestionsResponse, error) {
	editor, ok := s.editors[req.TargetType]
	if !ok {
		return nil, ErrInvalidTarget
	}
	targetID, err := uuid.Parse(req.TargetID)
	if err != nil {
		return nil, ErrInvalidTarget
	}
	if len(req.Fields) == 0 {
		return nil, invalid("反映する値がありません")
	}

	current, _, err := editor.GetEditableFields(targetID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrTargetNotFound
	}

	// 現在値と同じ項目は送っても意味がないので落とす（全部同じなら何もしない）
	apply := map[string]string{}
	for k, v := range req.Fields {
		v = strings.TrimSpace(v)
		if cur, known := current[k]; known && cur != v {
			apply[k] = v
		}
	}
	if len(apply) > 0 {
		if err := editor.ApplyEditableFields(targetID, apply); err != nil {
			return nil, err
		}
	}

	// 反映後の値を基準に、各提案が通ったか否かを判定する
	final := map[string]string{}
	for k, v := range current {
		final[k] = v
	}
	for k, v := range apply {
		final[k] = v
	}

	resp := &dto.MergeSuggestionsResponse{Applied: apply}
	note := strings.TrimSpace(req.Note)
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, invalid("無効な提案ID: %s", raw)
		}
		sug, err := s.repo.FindByID(id)
		if err != nil {
			return nil, err
		}
		if sug == nil || sug.Status == "approved" || sug.Status == "rejected" {
			continue // 取得の合間に処理済みになったものは触らない
		}

		changed, err := changedFieldsOf(sug)
		if err != nil {
			return nil, err
		}
		// 提案が変えたかった項目がすべて採用値と一致していれば「採用された」
		adopted := len(changed) > 0
		for k, v := range changed {
			if final[k] != v {
				adopted = false
				break
			}
		}

		status, reviewNote := "rejected", "統合して反映（別の値を採用）"
		if adopted {
			status, reviewNote = "approved", "統合して反映"
		}
		if note != "" {
			reviewNote += "：" + note
		}
		if err := s.repo.UpdateStatus(id, status, reviewerID(reviewer), reviewNote); err != nil {
			return nil, err
		}
		if adopted {
			resp.Approved++
		} else {
			resp.Rejected++
		}
	}

	logger.Infof("suggestions merged: %s %s applied=%v approved=%d rejected=%d",
		req.TargetType, targetID, apply, resp.Approved, resp.Rejected)
	return resp, nil
}

// Withdraw は提案を取り下げる（削除する）。
//
// 誤タップや勘違いに気づいた本人が引っ込められるようにするための操作。
// 却下ではなく削除する：本人が取り下げただけのものをレビュー履歴に積むと、
// 実際の判断の記録が埋もれるため。
//
// 引けるのは自分が出した未処理の提案だけ。content:edit を持つ管理者は他人の分も引ける
// （荒らしの掃除に使う）。他人のものは存在を伏せて ErrSuggestionNotFound を返す
// （プレイリストと同じ方針。ID の総当たりで存在を探れないようにする）。
func (s *SuggestionService) Withdraw(id uuid.UUID, user *models.User) error {
	if user == nil {
		return ErrSuggestionNotFound
	}
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}

	isReviewer := auth.HasPermission(user.Permissions, auth.PermContentEdit)
	isOwner := sug.CreatedBy != nil && *sug.CreatedBy == user.ID
	if !isOwner && !isReviewer {
		return ErrSuggestionNotFound
	}
	// 処理済みのものは取り下げられない（反映済みの変更は「元に戻す」の話であって
	// 取り下げではないし、却下済みのものを消しても意味がない）。
	if sug.Status == "approved" || sug.Status == "rejected" {
		return ErrAlreadyReviewed
	}

	if err := s.repo.Delete(id); err != nil {
		return err
	}
	logger.Infof("edit suggestion withdrawn: %s by %s", id, displayNameOf(user))
	return nil
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

	// 未登録曲の追加は「フィールドの差し替え」ではないので別経路（歌唱記録を新規作成する）
	if sug.Kind == KindMissingSong {
		return s.approveMissingSong(sug, reviewer)
	}
	// 曲の差し替えも同様（別の曲マスタへ繋ぎ替える）
	if sug.Kind == KindSongSwap {
		return s.approveSongSwap(sug, reviewer, force)
	}

	editor, ok := s.editors[sug.TargetType]
	if !ok {
		return ErrInvalidTarget
	}

	// 反映するのは「この提案が実際に変えるフィールド」だけ。after 全体を書き戻すと、
	// 提案が触っていないフィールドまで提案時点の値で上書きしてしまう
	// （例：同じ歌唱に終了時間の提案と開始時間の提案が来たとき、後から開始時間を承認すると
	// 先に反映した終了時間が提案時点の値に巻き戻る）。
	fields, err := changedFieldsOf(sug)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return ErrNoChange
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

// changedFieldsOf は提案が実際に変更するフィールドだけを取り出す（after ≠ before のもの）。
// 提案は「この項目をこの値にしたい」という意思表示であって、対象全体のスナップショットではない。
func changedFieldsOf(sug *models.EditSuggestion) (map[string]string, error) {
	var before, after map[string]string
	if err := json.Unmarshal(sug.BeforeData, &before); err != nil {
		return nil, fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	if err := json.Unmarshal(sug.AfterData, &after); err != nil {
		return nil, fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	changed := map[string]string{}
	for k, v := range after {
		if v != before[k] {
			changed[k] = v
		}
	}
	return changed, nil
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
		return nil // 呼び出し元が DB 上のユーザーでない場合（廃止済みの経路の名残）
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
		TargetKey:     m.TargetKey,
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

	if m.Kind == KindMissingSong {
		var p dto.MissingSongPayload
		if json.Unmarshal(m.Payload, &p) == nil {
			resp.Payload = &p
			// 既に登録されている曲を報告していないか、レビュー時に気づけるようにする。
			// 重なっていても承認は止めない（メドレーなど正当に重なる歌唱があるため）。
			if (m.Status == "pending" || m.Status == "conflict") && s.missing != nil {
				if ov, err := s.missing.OverlappingPerformances(p.StreamID, p.StartSeconds, p.EndSeconds); err == nil && len(ov) > 0 {
					resp.Overlaps = ov
				}
			}
		}
		return resp // 未登録曲の追加は既存フィールドを触らないので衝突判定は不要
	}

	if m.Kind == KindSongSwap {
		var p dto.SongSwapPayload
		if json.Unmarshal(m.Payload, &p) == nil {
			resp.SongSwap = &p
			// 提案後に曲が差し替えられていないかを一覧の時点で見せる
			if (m.Status == "pending" || m.Status == "conflict") && s.swapper != nil && p.CurrentSongName != "" {
				if cur, label, err := s.swapper.SongLabelOf(m.TargetID); err == nil && label != "" && cur != p.CurrentSongName {
					resp.Conflicts = map[string]dto.FieldConflict{
						"song": {Expected: p.CurrentSongName, Current: cur},
					}
				}
			}
		}
		return resp
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
