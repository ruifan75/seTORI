package service

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

var ErrPresetNotFound = fmt.Errorf("プレイリストが見つかりません")

// featuredSingerID はプリセットプレイリストが対象にするチャンネル（稀羽すう）。
//
// 収録済みの歌唱はほぼこのチャンネルのものなので、プリセットは歌手に紐付けて定義する。
// 紐付けないと、別のチャンネルの歌唱を登録した瞬間に「コラボ」「新着」へ黙って混ざる。
// 対象を増やすときは SingerID 違いの定義を Presets に並べる（キーの接頭辞も分ける）。
const featuredSingerID = "UCeqIMtLuGc3YgwkhEaG8oDg" // 稀羽すう - Suu Usuwa -

// presetMaxItems は 1 つのプリセットが返す上限。件数を指定しなければこの数まで返す。
//
// プレイリスト画面・全曲再生・コピーが同じ件数を見るように、既定と上限を分けていない。
// 分けていた頃は「画面には 50 曲しか出ていないのにコピーすると 190 曲入る」という
// 食い違いが起きた。ホームの列だけが明示的に少ない件数を指定する。
const presetMaxItems = 200

// Preset は運営が用意した歌単。定義はコードに持ち、中身は毎回 DB から計算する
// （理由は migration 048 のコメント）。
type Preset struct {
	Key         string
	Name        string
	Description string
	Filter      repository.PresetFilter
	// Limit は「条件には合うが、そこまでは出さない」ときの上限（0 なら presetMaxItems）。
	// 新着のように条件が緩いものは全件出すと歌単として意味をなさないため。
	Limit int
}

// itemLimit はこのプリセットが返す件数（要求された件数と定義の上限の小さいほう）。
func (p *Preset) itemLimit(requested int) int {
	maxItems := presetMaxItems
	if p.Limit > 0 && p.Limit < maxItems {
		maxItems = p.Limit
	}
	if requested < 1 || requested > maxItems {
		return maxItems
	}
	return requested
}

// Presets の並びがそのまま画面の並びになる。
var Presets = []Preset{
	{
		Key:         "suu-new",
		Name:        "新着",
		Description: "最近の配信で歌った曲",
		Filter:      repository.PresetFilter{SingerID: featuredSingerID},
		Limit:       100, // 条件が「全部」なので、ここを外すと新着ではなく全曲一覧になる
	},
	{
		Key:         "suu-collab",
		Name:        "コラボ",
		Description: "2人以上で歌った歌唱",
		// 配信タグ collaboration ではなく歌唱の歌手数で判定する。コラボ配信にも
		// 一人で歌う曲があり、逆に通常の歌枠にも飛び入りの合唱がある。
		Filter: repository.PresetFilter{SingerID: featuredSingerID, MultiSinger: true},
	},
	{
		Key:         "suu-3d",
		Name:        "3D",
		Description: "3D 配信の歌唱",
		// おうち3D は別物として扱うため除く（配信には 3d と home3D の両方が付く）。
		Filter: repository.PresetFilter{
			SingerID:    featuredSingerID,
			IncludeTags: []string{"3d"},
			ExcludeTags: []string{"home3D"},
		},
	},
	{
		Key:         "suu-cover",
		Name:        "歌ってみた",
		Description: "歌ってみたとして投稿された歌唱",
		Filter: repository.PresetFilter{
			SingerID:    featuredSingerID,
			IncludeTags: []string{"music_cover"},
		},
	},
	{
		Key:         "suu-original",
		Name:        "オリジナル曲",
		Description: "オリジナル曲の歌唱",
		Filter: repository.PresetFilter{
			SingerID:    featuredSingerID,
			IncludeTags: []string{"original_song"},
		},
	},
	{
		Key:         "suu-lullaby",
		Name:        "睡眠導入",
		Description: "静かに歌う睡眠導入の歌枠から",
		Filter: repository.PresetFilter{
			SingerID:    featuredSingerID,
			IncludeTags: []string{"lullaby"},
		},
	},
}

// FindPreset は定義済みのプリセットを探す。
func FindPreset(key string) *Preset {
	for i := range Presets {
		if Presets[i].Key == key {
			return &Presets[i]
		}
	}
	return nil
}

// PresetService はプリセットプレイリストの取得・フォロー・コピーを担う。
type PresetService struct {
	perfRepo     *repository.PerformanceRepository
	playlistRepo *repository.PlaylistRepository
}

func NewPresetService(perfRepo *repository.PerformanceRepository, playlistRepo *repository.PlaylistRepository) *PresetService {
	return &PresetService{perfRepo: perfRepo, playlistRepo: playlistRepo}
}

// followedSet はフォロー中のキー集合を返す（未ログインなら空）。
func (s *PresetService) followedSet(viewerID *uuid.UUID) (map[string]bool, error) {
	if viewerID == nil {
		return map[string]bool{}, nil
	}
	keys, err := s.playlistRepo.ListFollowedPresetKeys(*viewerID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(keys))
	for _, key := range keys {
		set[key] = true
	}
	return set, nil
}

func (s *PresetService) toResponse(p *Preset, followed map[string]bool) (dto.PresetPlaylistResponse, error) {
	// プリセットは閲覧者向けの面なので、編集者にも秘匿された配信は出さない。
	count, err := s.perfRepo.CountByPreset(p.Filter, repository.PublicAccess)
	if err != nil {
		return dto.PresetPlaylistResponse{}, err
	}
	// 出せる件数を超えて数えても仕方がないので、返す件数に揃える
	// （「730曲」と出したのに 100 曲しか並ばない、を防ぐ）。
	if maxItems := p.itemLimit(0); count > maxItems {
		count = maxItems
	}
	return dto.PresetPlaylistResponse{
		Key:         p.Key,
		Name:        p.Name,
		Description: p.Description,
		ItemCount:   count,
		IsFollowing: followed[p.Key],
	}, nil
}

// List は全プリセットを曲数つきで返す。
func (s *PresetService) List(viewerID *uuid.UUID) (*dto.PresetPlaylistListResponse, error) {
	followed, err := s.followedSet(viewerID)
	if err != nil {
		return nil, err
	}
	resp := &dto.PresetPlaylistListResponse{Presets: make([]dto.PresetPlaylistResponse, 0, len(Presets))}
	for i := range Presets {
		item, err := s.toResponse(&Presets[i], followed)
		if err != nil {
			return nil, err
		}
		resp.Presets = append(resp.Presets, item)
	}
	return resp, nil
}

// ListFollowed はフォロー中のプリセットだけを返す。
// 定義から消えたキーは黙って飛ばす（DB の行は残す。migration 048 のコメント参照）。
func (s *PresetService) ListFollowed(userID uuid.UUID) (*dto.PresetPlaylistListResponse, error) {
	keys, err := s.playlistRepo.ListFollowedPresetKeys(userID)
	if err != nil {
		return nil, err
	}
	followed := map[string]bool{}
	for _, key := range keys {
		followed[key] = true
	}

	resp := &dto.PresetPlaylistListResponse{Presets: make([]dto.PresetPlaylistResponse, 0, len(keys))}
	for _, key := range keys {
		preset := FindPreset(key)
		if preset == nil {
			continue
		}
		item, err := s.toResponse(preset, followed)
		if err != nil {
			return nil, err
		}
		resp.Presets = append(resp.Presets, item)
	}
	return resp, nil
}

// Get は 1 件のプリセットを返す。
func (s *PresetService) Get(key string, viewerID *uuid.UUID) (*dto.PresetPlaylistResponse, error) {
	preset := FindPreset(key)
	if preset == nil {
		return nil, ErrPresetNotFound
	}
	followed, err := s.followedSet(viewerID)
	if err != nil {
		return nil, err
	}
	resp, err := s.toResponse(preset, followed)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListItems はプリセットの中身を返す。
func (s *PresetService) ListItems(key string, limit int) ([]repository.PerformanceWithDetails, error) {
	preset := FindPreset(key)
	if preset == nil {
		return nil, ErrPresetNotFound
	}
	return s.perfRepo.FindByPreset(preset.Filter, preset.itemLimit(limit), repository.PublicAccess)
}

func (s *PresetService) Follow(userID uuid.UUID, key string) error {
	if FindPreset(key) == nil {
		return ErrPresetNotFound
	}
	return s.playlistRepo.FollowPreset(userID, key)
}

func (s *PresetService) Unfollow(userID uuid.UUID, key string) error {
	if FindPreset(key) == nil {
		return ErrPresetNotFound
	}
	return s.playlistRepo.UnfollowPreset(userID, key)
}

// AddToPlaylist はプリセットの現在の中身を、その人のプレイリストへ入れる。
// 追加先は既存のプレイリスト（PlaylistID）か、新規作成（Name）のどちらか。
//
// 入るのは押した時点の中身で、以後はプリセット側の変更と無関係になる
// （常に最新を追いたい人向けにはフォローがある）。
func (s *PresetService) AddToPlaylist(userID uuid.UUID, key string, req *dto.AddPresetToPlaylistRequest) (*dto.AddPresetToPlaylistResponse, error) {
	preset := FindPreset(key)
	if preset == nil {
		return nil, ErrPresetNotFound
	}

	ids, err := s.perfRepo.FindIDsByPreset(preset.Filter, preset.itemLimit(0), repository.PublicAccess)
	if err != nil {
		return nil, err
	}

	playlistID, created, err := s.resolveTarget(userID, preset, req)
	if err != nil {
		return nil, err
	}

	added, err := s.playlistRepo.AddItems(playlistID, ids)
	if err != nil {
		return nil, err
	}

	meta, err := s.playlistRepo.FindByIDWithMeta(playlistID)
	if err != nil {
		return nil, err
	}
	return &dto.AddPresetToPlaylistResponse{
		Playlist: toPlaylistResponse(meta, &userID),
		Added:    added,
		Skipped:  len(ids) - added,
		Created:  created,
	}, nil
}

// resolveTarget は追加先のプレイリストを決める（無ければ作る）。
// 他人のプレイリストは存在を伏せて ErrPlaylistNotFound にする（PlaylistService と同じ扱い）。
func (s *PresetService) resolveTarget(userID uuid.UUID, preset *Preset, req *dto.AddPresetToPlaylistRequest) (uuid.UUID, bool, error) {
	if req != nil && strings.TrimSpace(req.PlaylistID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(req.PlaylistID))
		if err != nil {
			return uuid.Nil, false, ErrPlaylistNotFound
		}
		existing, err := s.playlistRepo.FindByID(id)
		if err != nil {
			return uuid.Nil, false, err
		}
		if existing == nil || existing.UserID != userID {
			return uuid.Nil, false, ErrPlaylistNotFound
		}
		return id, false, nil
	}

	name := preset.Name
	if req != nil && strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	name, err := validateName(name)
	if err != nil {
		return uuid.Nil, false, err
	}

	playlist := &models.Playlist{
		UserID:      userID,
		Name:        name,
		Description: strings.TrimSpace(preset.Description + "（プリセットから追加）"),
		Visibility:  models.PlaylistPrivate,
	}
	if err := s.playlistRepo.Create(playlist); err != nil {
		return uuid.Nil, false, err
	}
	return playlist.ID, true, nil
}
