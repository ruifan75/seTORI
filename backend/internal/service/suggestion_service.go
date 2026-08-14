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
	"github.com/ruifan75/setori/pkg/songmatch"
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
	// ErrDuplicateSuggestion は同じ行についての報告が既にあることを示す。
	// 一括はこれを「もう待ち行列に居る」として黙って数えないので、判別できるよう型で持つ。
	ErrDuplicateSuggestion = fmt.Errorf("同じ内容の提案が既にあります")
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
	// System は seTORI 自身が積んだ提案（一括セットリスト作成の審査待ちなど）。
	// **投稿の絞り込みを飛ばす。** 絞り込みは濫用への対処であって、
	// 権限のある人が明示的に始めた処理を止めるためのものではない。
	//
	// 実際、一括の初回実行で審査へ回すはずの 303 行のうち **295 行が
	// この制限に当たって静かに消えた**（作られもせず、待ち行列にも入らない）。
	System bool
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
	// matchService は「この表記はこの曲ではない」という否決を残すために使う。
	// 残さないと、次の一括実行が同じ組をまた審査へ積む。
	matchService *SongMatchService
}

func NewSuggestionService(
	repo *repository.SuggestionRepository,
	settingsRepo *repository.AppSettingsRepository,
	songService *SongService,
	artistService *ArtistService,
	performanceService *PerformanceService,
	matchService *SongMatchService,
) *SuggestionService {
	return &SuggestionService{
		repo:         repo,
		settingsRepo: settingsRepo,
		editors: map[string]TargetEditor{
			"song":        songService,
			"artist":      artistService,
			"performance": performanceService,
		},
		missing:      performanceService,
		swapper:      performanceService,
		matchService: matchService,
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

	// 同じ行の提案を積み直さない。
	//
	// 判定が無かったころは、一括を 2 回回すと同じ (配信, 開始秒, 曲名) が 2 件並び、
	// 却下したものも次の実行でまた出ていた（System は投稿の絞り込みも飛ばすので歯止めが無い）。
	if dup, err := s.findDuplicateMissingSong(p); err != nil {
		logger.Warnf("missing song duplicate check failed (%s): %v", p.StreamID, err)
	} else if dup != nil {
		return nil, fmt.Errorf("%w（%s・%s）", ErrDuplicateSuggestion, dup.Status, dup.CreatedAt.Local().Format("01/02 15:04"))
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

// missingSongDedupeWindow は「同じ行の報告」とみなす開始秒の窓。
// 既存の歌唱との突き合わせ（overlapsExisting）と同じ 30 秒に揃えてある。
const missingSongDedupeWindow = 30

// findDuplicateMissingSong は同じ配信・ほぼ同じ開始秒・同じ曲名キーの提案を探す。
// status は問わない（却下済みが一番効く ── 覚えていないと同じものが何度でも出る）。
func (s *SuggestionService) findDuplicateMissingSong(p *dto.MissingSongPayload) (*models.EditSuggestion, error) {
	existing, err := s.repo.FindMissingSongsByStream(p.StreamID)
	if err != nil {
		return nil, err
	}
	nameKey := songmatch.TitleKey(p.SongName)
	for i := range existing {
		var q dto.MissingSongPayload
		if json.Unmarshal(existing[i].Payload, &q) != nil {
			continue
		}
		if abs(q.StartSeconds-p.StartSeconds) > missingSongDedupeWindow {
			continue
		}
		// 曲が決まっているものどうしは ID で、決まっていなければ曲名キーで見る。
		// 表記が揺れていても同じ曲を指していれば同じ行の報告とみなす。
		if p.SongID != "" && q.SongID == p.SongID {
			return &existing[i], nil
		}
		if nameKey != "" && songmatch.TitleKey(q.SongName) == nameKey {
			return &existing[i], nil
		}
	}
	return nil, nil
}

// approveMissingSong は未登録曲の報告を承認し、歌唱記録を作る。
// payload が song_id を持っていればその曲へ、無ければ曲名から探す／作る。
//
// edits は審査担当が画面で直した内容（曲の差し替え・時間・歌手・曲名）。
// 一括が積んだ審査待ちは「そのまま登録する」ものではなく「人が直して登録する」ものなので、
// 承認と修正を 1 往復で済ませられるようにしてある。nil ならそのまま登録する。
func (s *SuggestionService) approveMissingSong(sug *models.EditSuggestion, reviewer *models.User, edits *dto.MissingSongPayload) error {
	if s.missing == nil {
		return ErrInvalidTarget
	}
	var p dto.MissingSongPayload
	if err := json.Unmarshal(sug.Payload, &p); err != nil {
		return fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	note := "歌唱記録を作成"
	if edits != nil {
		var changes []string
		p, changes = applyMissingSongEdits(p, *edits)
		if len(changes) > 0 {
			note += "（審査時に修正：" + strings.Join(changes, "・") + "）"
		}
	}
	if p.SongName == "" && p.SongID == "" {
		return invalid("曲名は必須です")
	}
	if p.StartSeconds < 0 || p.EndSeconds < 0 {
		return invalid("時間が不正です")
	}
	if p.EndSeconds != 0 && p.EndSeconds <= p.StartSeconds {
		return ErrInvalidTimeRange
	}

	if err := s.missing.CreateFromMissingSong(p); err != nil {
		return err // 例：同じ時間に既に歌唱がある
	}
	if err := s.repo.UpdateStatus(sug.ID, "approved", reviewerID(reviewer), note); err != nil {
		return err
	}
	logger.Infof("missing song suggestion approved: %s (%s @%ds) by %s", sug.ID, p.SongName, p.StartSeconds, reviewerLabel(reviewer))
	return nil
}

// applyMissingSongEdits は審査担当の修正を提案の内容へ重ねる。
// 直した項目の一覧も返す（レビュー履歴に「何を直して通したか」を残すため）。
//
// 配信 ID は差し替えさせない。別の配信の話になるなら、それは別の提案。
func applyMissingSongEdits(base, edits dto.MissingSongPayload) (dto.MissingSongPayload, []string) {
	out := base
	var changed []string

	// 曲の差し替え。song_id を空文字で送ると「照合を解除して曲名から作り直す」意味になるので、
	// 送られてきたかどうか（＝画面で触ったか）ではなく値の違いで判定する。
	if newID := strings.TrimSpace(edits.SongID); newID != base.SongID {
		out.SongID = newID
		changed = append(changed, "曲")
	}
	if name := strings.TrimSpace(edits.SongName); name != "" && name != base.SongName {
		out.SongName = name
		if out.SongID == base.SongID {
			changed = append(changed, "曲名")
		}
	}
	if artist := strings.TrimSpace(edits.OriginalArtist); artist != base.OriginalArtist {
		out.OriginalArtist = artist
		changed = append(changed, "歌手名")
	}
	if edits.StartSeconds != base.StartSeconds {
		out.StartSeconds = edits.StartSeconds
		changed = append(changed, "開始")
	}
	if edits.EndSeconds != base.EndSeconds {
		out.EndSeconds = edits.EndSeconds
		changed = append(changed, "終了")
	}
	// 時間を人が直したなら、由来は推定値ではなく人の手（manual）になる。
	// ここを据え置くと「chat 検出の値」として保存され、確度の絞り込みが嘘になる。
	if out.StartSeconds != base.StartSeconds || out.EndSeconds != base.EndSeconds {
		out.EndSource = repository.EndSourceManual
	}
	if !sameStrings(edits.SingerIDs, base.SingerIDs) {
		out.SingerIDs = edits.SingerIDs
		changed = append(changed, "歌手")
	}
	if len(edits.Tags) > 0 && !sameStrings(edits.Tags, base.Tags) {
		out.Tags = edits.Tags
		changed = append(changed, "タグ")
	}
	return out, changed
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func reviewerLabel(u *models.User) string {
	if u == nil {
		return "system"
	}
	return displayNameOf(u)
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
	if actor.System {
		return nil
	}
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

// List は status / kind（どちらも空なら全件）で絞った提案一覧を返す。
func (s *SuggestionService) List(status, kind string, page, limit int) (*dto.SuggestionListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	items, total, err := s.repo.List(status, kind, limit, (page-1)*limit)
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
// kind が空でなければその種別だけを返す（レビュー画面の種別の絞り込み用）。
func (s *SuggestionService) ListGrouped(status, kind string, page, limit int) (*dto.SuggestionGroupListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	groups, total, err := s.repo.ListGroupedByTarget(status, kind, limit, (page-1)*limit)
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
	// 処理済みのものは取り下げられない。反映済みの変更は「元に戻す」の話であって
	// 取り下げではない。却下済みのものは UndoRejection で扱う ── perf.missing の却下は
	// 「次から提案しない」という持続する副作用を持つので、消せる必要がある
	// （取り下げとは別の操作。引っ込めるのは投稿者、取り消すのは審査担当）。
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
	return s.ApproveWithEdits(id, reviewer, force, nil)
}

// ApproveWithEdits は承認時に審査担当が加えた修正を添えて反映する（perf.missing のみ）。
// edits が nil なら Approve と同じ。
func (s *SuggestionService) ApproveWithEdits(id uuid.UUID, reviewer *models.User, force bool, edits *dto.MissingSongPayload) error {
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
		return s.approveMissingSong(sug, reviewer, edits)
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
	return s.RejectWithVerdict(id, reviewer, note, false)
}

// RejectWithVerdict は却下する。notThisSong を立てると「この表記はこの曲ではない」を
// song_identity_checks に残し、次の一括実行が同じ組を提案しないようにする。
//
// 却下そのものは「今回は登録しない」という程度の意思表示でしかないので、既定では
// 何も学習しない。学習させるのは審査担当が**曲の同一性について**明確に否決したときだけ
// ── ここを一緒くたにすると、「時間が違うから却下」がその曲の否定として残ってしまう。
func (s *SuggestionService) RejectWithVerdict(id uuid.UUID, reviewer *models.User, note string, notThisSong bool) error {
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
	if err := s.repo.UpdateStatus(id, "rejected", reviewerID(reviewer), strings.TrimSpace(note)); err != nil {
		return err
	}
	if notThisSong {
		s.recordSongVerdict(sug, note)
	}
	return nil
}

// recordSongVerdict は perf.missing の否決を「別の曲」として残す。
// 失敗しても却下自体は成立しているので、警告に留める。
// songVerdict は否決の対象（どの表記が、どの曲でないか）。
type songVerdict struct {
	name, artist       string
	nameKey, artistKey string
	songIDs            []uuid.UUID
}

// songVerdictOf は提案から否決の対象を割り出す。
//
// **記録するときと取り消すときで必ず同じ鍵を使う**ため 1 か所にまとめてある
// ── ずれると「取り消したのに消えていない」という、画面からは追えない壊れ方をする。
func songVerdictOf(sug *models.EditSuggestion) (songVerdict, bool) {
	if sug.Kind != KindMissingSong {
		return songVerdict{}, false
	}
	var p dto.MissingSongPayload
	if json.Unmarshal(sug.Payload, &p) != nil {
		return songVerdict{}, false
	}
	// 鍵は**抽出したままの表記**から作る。次の実行が突き合わせるのはその表記であって、
	// 照合で書き換わった後の曲名ではない。
	name, artist := p.RawName, p.RawArtist
	if name == "" {
		name = p.SongName
	}
	if artist == "" {
		artist = p.OriginalArtist
	}
	v := songVerdict{
		name: name, artist: artist,
		nameKey:   songmatch.TitleKey(name),
		artistKey: songmatch.ParseArtist(artist).String(),
	}
	if v.nameKey == "" {
		return songVerdict{}, false
	}
	// 曲が決まっている提案は「その曲ではない」、決まっていない提案は
	// 「候補のどれでもない」という否決になる。
	if p.SongID != "" {
		if id, err := uuid.Parse(p.SongID); err == nil {
			v.songIDs = append(v.songIDs, id)
		}
	} else {
		for _, c := range p.Candidates {
			if id, err := uuid.Parse(c.SongID); err == nil {
				v.songIDs = append(v.songIDs, id)
			}
		}
	}
	return v, len(v.songIDs) > 0
}

func (s *SuggestionService) recordSongVerdict(sug *models.EditSuggestion, note string) {
	if s.matchService == nil {
		return
	}
	v, ok := songVerdictOf(sug)
	if !ok {
		return
	}
	for _, songID := range v.songIDs {
		if err := s.matchService.RecordSongRejection(v.nameKey, v.artistKey, songID, "review", note); err != nil {
			logger.Warnf("record song rejection failed (%s): %v", sug.ID, err)
		}
	}
	logger.Infof("song identity rejected by review: %q / %q ≠ %d 曲", v.name, v.artist, len(v.songIDs))
}

// UndoRejection は却下を取り消し、次の一括実行でまた提案されるようにする。
//
// **却下には持続する副作用がある**ので、取り消す口が要る。
// 一括は同じ (配信, 開始秒 ±30, 曲名キー) の提案を status に関係なく積み直さないので、
// 却下した行はそのままだと永久に出てこない。「この曲ではない」を押していれば
// song_identity_checks にも残っていて、候補からも外れたままになる。
//
// 対象は perf.missing の却下だけ。field / perf.meta の却下は次の提案を妨げないので、
// 消しても履歴が減るだけで得るものが無い。
func (s *SuggestionService) UndoRejection(id uuid.UUID, reviewer *models.User) error {
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}
	if sug.Kind != KindMissingSong {
		return invalid("却下の取り消しは未登録曲の報告だけが対象です")
	}
	if sug.Status != "rejected" {
		return invalid("却下されていない提案です")
	}

	// 先に否決の記録を消す。ここで失敗しても提案の行は残るので、やり直せる。
	if s.matchService != nil {
		if v, ok := songVerdictOf(sug); ok {
			for _, songID := range v.songIDs {
				if err := s.matchService.RemoveSongRejection(v.nameKey, v.artistKey, songID); err != nil {
					return fmt.Errorf("否決の記録を取り消せませんでした: %w", err)
				}
			}
		}
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	logger.Infof("rejection undone: %s by %s", id, reviewerLabel(reviewer))
	return nil
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
