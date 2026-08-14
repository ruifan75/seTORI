package service

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type PerformanceService struct {
	perfRepo       *repository.PerformanceRepository
	songRepo       *repository.SongRepository
	songItunesRepo *repository.SongItunesRepository
	artistRepo     *repository.ArtistRepository
	streamRepo     *repository.StreamRepository
	matchService   *SongMatchService
}

func NewPerformanceService(
	perfRepo *repository.PerformanceRepository,
	songRepo *repository.SongRepository,
	songItunesRepo *repository.SongItunesRepository,
	artistRepo *repository.ArtistRepository,
	streamRepo *repository.StreamRepository,
	matchService *SongMatchService,
) *PerformanceService {
	return &PerformanceService{
		perfRepo:       perfRepo,
		songRepo:       songRepo,
		songItunesRepo: songItunesRepo,
		artistRepo:     artistRepo,
		streamRepo:     streamRepo,
		matchService:   matchService,
	}
}

// CreatePerformances 直接從前端編輯結果建立演出記錄
// 既存記録は全削除せず差分更新する（performance ID を保つため。ID はプレイリストから参照される）
func (s *PerformanceService) CreatePerformances(streamID string, items []dto.CreatePerformanceItem) (*dto.CreatePerformancesResponse, error) {
	desired := make([]models.Performance, 0, len(items))

	for _, item := range items {
		// 1. 尋找或建立歌曲（優先使用 iTunes ID 配對）
		song, isNewSong, err := s.findOrCreateSong(item)
		if err != nil {
			return nil, fmt.Errorf("find or create song: %w", err)
		}

		// 2. 如果有 iTunes ID，確保新增 iTunes ID 與歌曲的關聯
		if item.ItunesID != nil && *item.ItunesID > 0 {
			// 檢查這個 iTunes ID 是否已經被其他歌曲關聯
			existingItunes, _ := s.songItunesRepo.FindByItunesID(*item.ItunesID)

			// 只有在以下情況才新增：
			// 1. iTunes ID 還沒有被關聯到任何歌曲，或
			// 2. iTunes ID 已關聯到其他歌曲（這種情況不應該發生，但為安全起見檢查）
			if existingItunes == nil {
				// 檢查該歌曲是否已經有其他 iTunes ID
				existingSongItunes, _ := s.songItunesRepo.FindBySongID(song.ID)
				isPrimary := isNewSong || len(existingSongItunes) == 0 // 新歌曲或該歌曲還沒有任何 iTunes ID 時設為主要

				// iTunes ID 未被關聯，新增關聯
				songItunes := &models.SongITunes{
					SongID:    song.ID,
					ITunesID:  *item.ItunesID,
					IsPrimary: isPrimary,
				}
				if err := s.songItunesRepo.Create(songItunes); err != nil {
					// 記錄錯誤但不中斷
					fmt.Printf("create song itunes error: %v\n", err)
				}
			} else if existingItunes.SongID != song.ID {
				// iTunes ID 關聯到不同的歌曲（這可能表示重複的歌曲，記錄警告）
				fmt.Printf("warning: iTunes ID %d already associated with different song\n", *item.ItunesID)
			}
		}

		// 3. 目標状態を組み立てる（書き込みは後段の差分更新でまとめて行う）
		// 終了時間の由来。編集画面から来たものは人が見たとみなして confirmed=true を既定にし、
		// 一括自動作成のように人手を介さない経路は明示的に false を送ってもらう。
		endConfirmed := true
		if item.EndConfirmed != nil {
			endConfirmed = *item.EndConfirmed
		}

		desired = append(desired, models.Performance{
			StreamID:     streamID,
			SongID:       song.ID,
			StartSeconds: item.StartSeconds,
			EndSeconds:   item.EndSeconds,
			OrderIndex:   0, // 不再使用，改用 start_seconds 排序
			CustomTags:   item.CustomTags,
			EndSource:    item.EndSource,
			EndConfirmed: endConfirmed,
		})
	}

	// 4. 既存記録との差分を反映（ID を維持）。返る ID は desired と同じ並び。
	perfIDs, err := s.perfRepo.ReconcilePerformances(streamID, desired)
	if err != nil {
		return nil, fmt.Errorf("reconcile performances: %w", err)
	}

	// 5. タグ・歌手は毎回設定し直す（維持された記録も内容が変わりうるため、空でも呼ぶ）
	for i, item := range items {
		if err := s.perfRepo.SetTags(perfIDs[i], item.Tags); err != nil {
			return nil, fmt.Errorf("set performance tags: %w", err)
		}
		if err := s.perfRepo.SetSingers(perfIDs[i], item.SingerIDs); err != nil {
			return nil, fmt.Errorf("set performance singers: %w", err)
		}
	}

	return &dto.CreatePerformancesResponse{
		CreatedCount: len(perfIDs),
	}, nil
}

// findOrCreateSong 尋找或建立歌曲
//
// 照合は SongMatchService（曲名キー主導）に任せ、確信度で 3 通りに分ける。
//
//	自動採用の水準  → 既存曲に結びつける
//	似ているが未満  → 新曲を作る。ただし黙って作らず統合候補として記録する
//	似ていない      → 新曲を作る
//
// 真ん中の扱いがこの関数の肝。以前は「照合できなければ新曲」しかなかったので、
// 表記ゆれで外すたびに近似重複（"少女レイ / みきとP feat. 初音ミク" のような）が
// 静かに増え、次からはもっと当たらなくなるという悪循環になっていた。
// ここで記録しておけば、既存の統合機能で人が畳める。
//
// 返回：歌曲, 是否為新建立的, 錯誤
func (s *PerformanceService) findOrCreateSong(item dto.CreatePerformanceItem) (*models.Song, bool, error) {
	var candidates []MatchCandidate
	if s.matchService != nil {
		var err error
		candidates, err = s.matchService.FindCandidates(item.Name, item.OriginalArtist, item.ItunesID)
		if err != nil {
			return nil, false, fmt.Errorf("find song: %w", err)
		}
	}

	// 1. 自動採用できる候補があればそれを使う
	if len(candidates) > 0 && candidates[0].Auto() {
		song := &candidates[0].Song
		// 歌曲已存在，檢查是否需要補上封面圖
		if (!song.Arts.Valid || song.Arts.String == "") && item.ArtURL != nil && *item.ArtURL != "" {
			song.Arts = sql.NullString{String: *item.ArtURL, Valid: true}
			if err := s.songRepo.Update(song); err != nil {
				return nil, false, fmt.Errorf("update song arts: %w", err)
			}
		}
		return song, false, nil
	}

	// 2. 建立新歌曲
	song := &models.Song{
		Name:           item.Name,
		OriginalArtist: item.OriginalArtist,
	}
	// 設定讀音（如果有提供）
	if item.NameReading != "" {
		song.NameReading = sql.NullString{String: item.NameReading, Valid: true}
	}
	if item.OriginalArtistReading != "" {
		song.OriginalArtistReading = sql.NullString{String: item.OriginalArtistReading, Valid: true}
	}
	// 設定封面圖（如果有提供）
	if item.ArtURL != nil && *item.ArtURL != "" {
		song.Arts = sql.NullString{String: *item.ArtURL, Valid: true}
	}
	if err := s.songRepo.Create(song); err != nil {
		return nil, false, fmt.Errorf("create song: %w", err)
	}
	// artists / song_artists マッピングを同期（失敗は警告のみ）
	if err := s.artistRepo.SyncSongArtist(song.ID, song.OriginalArtist); err != nil {
		logger.Warnf("sync song artist mapping failed (song: %s): %v", song.ID, err)
	}

	// 3. 似た既存曲があったなら統合候補として残す。
	// 曲の登録自体は止めない（配信の編集を人質に取らない）が、
	// 見えないところで重複が積もるのは防ぐ。
	s.recordMergeCandidates(song, candidates)

	return song, true, nil
}

// recordMergeCandidates は新規作成した曲について、自動採用に届かなかったが
// 十分似ている既存曲を統合候補に記録する。失敗しても本筋は止めない。
func (s *PerformanceService) recordMergeCandidates(newSong *models.Song, candidates []MatchCandidate) {
	if s.matchService == nil {
		return
	}
	for _, c := range candidates {
		if !c.NeedsReview() {
			continue
		}
		if err := s.matchService.RecordMergeCandidate(newSong.ID, c.Song.ID, c.Score, c.Reason); err != nil {
			logger.Warnf("record merge candidate failed (%s ↔ %s): %v", newSong.ID, c.Song.ID, err)
			continue
		}
		logger.Infof("新規登録した楽曲が既存曲に似ています。統合候補として記録しました: %q / %q ↔ %q / %q (score=%.2f, %s)",
			newSong.Name, newSong.OriginalArtist, c.Song.Name, c.Song.OriginalArtist, c.Score, c.Reason)
	}
}

// ========== 単件更新（修正提案の反映・プレイヤーからの微調整の受け口） ==========

var (
	ErrPerformanceNotFound  = fmt.Errorf("歌唱記録が見つかりません")
	ErrInvalidTimeRange     = fmt.Errorf("終了時間は開始時間より後にしてください")
	ErrDuplicatePerformance = fmt.Errorf("同じ配信・同じ曲・同じ開始時間の歌唱記録が既にあります")
)

// GetByID は歌唱1件を配信・楽曲情報付きで返す。見つからなければ (nil, nil)。
func (s *PerformanceService) GetByID(id uuid.UUID) (*repository.PerformanceWithDetails, error) {
	return s.perfRepo.FindByID(id)
}

// UpdatePerformance は歌唱1件を部分更新する。nil のフィールドは現状のまま。
//
// セットリスト全体を送り直す CreatePerformances と違い、1件だけを触る。
// 再生中の「開始/終了がずれている」報告や修正提案の反映は、他の曲を巻き込まずに
// 1件だけ直す必要があるため、こちらを使う。performance ID は変えない
// （プレイリストが performance_id を参照しているため）。
func (s *PerformanceService) UpdatePerformance(id uuid.UUID, req *dto.UpdatePerformanceRequest) (*repository.PerformanceWithDetails, error) {
	cur, err := s.perfRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("find performance: %w", err)
	}
	if cur == nil {
		return nil, ErrPerformanceNotFound
	}

	next := cur.Performance // models.Performance のコピー（ID / stream_id は据え置き）

	if req.SongID != nil {
		songID, err := uuid.Parse(*req.SongID)
		if err != nil {
			return nil, fmt.Errorf("song_id が不正です")
		}
		song, err := s.songRepo.FindByID(songID)
		if err != nil {
			return nil, fmt.Errorf("find song: %w", err)
		}
		if song == nil {
			return nil, fmt.Errorf("差し替え先の曲が見つかりません")
		}
		next.SongID = songID
	}
	if req.StartSeconds != nil {
		if *req.StartSeconds < 0 {
			return nil, fmt.Errorf("開始時間が不正です")
		}
		next.StartSeconds = *req.StartSeconds
	}
	if req.EndSeconds != nil {
		if *req.EndSeconds < 0 {
			return nil, fmt.Errorf("終了時間が不正です")
		}
		next.EndSeconds = *req.EndSeconds
	}
	// end_seconds = 0 は「動画の最後まで」を意味するので範囲チェックから外す
	if next.EndSeconds != 0 && next.EndSeconds <= next.StartSeconds {
		return nil, ErrInvalidTimeRange
	}
	if req.CustomTags != nil {
		next.CustomTags = *req.CustomTags
	}

	if err := s.perfRepo.Update(&next); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicatePerformance
		}
		return nil, fmt.Errorf("update performance: %w", err)
	}

	if req.Tags != nil {
		if err := s.perfRepo.SetTags(id, *req.Tags); err != nil {
			return nil, fmt.Errorf("set performance tags: %w", err)
		}
	}
	if req.SingerIDs != nil {
		if err := s.perfRepo.SetSingers(id, *req.SingerIDs); err != nil {
			return nil, fmt.Errorf("set performance singers: %w", err)
		}
	}

	updated, err := s.perfRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("reload performance: %w", err)
	}
	if updated == nil {
		return nil, ErrPerformanceNotFound
	}
	logger.Infof("performance updated: %s (%s %d-%d)", id, updated.SongName, updated.StartSeconds, updated.EndSeconds)
	return updated, nil
}

// ========== 未登録曲の追加提案（MissingSongCreator） ==========

// StreamLabel は配信の表示名（タイトル）を返す。存在しなければ空文字。
// 提案の投稿時に「その配信が実在するか」を確かめ、表示用のラベルを作るために使う。
func (s *PerformanceService) StreamLabel(streamID string) (string, error) {
	stream, err := s.streamRepo.FindByID(streamID)
	if err != nil {
		return "", fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return "", nil
	}
	return stream.Title, nil
}

// OverlappingPerformances は提案の時間帯と重なる既存の歌唱を返す。
// レビュー時に「もう登録されている曲を報告していないか」へ気づけるようにするための情報で、
// 重なっていても承認は止めない（メドレーや掛け合いなど、正当に重なる歌唱があるため）。
func (s *PerformanceService) OverlappingPerformances(streamID string, start, end int) ([]dto.OverlapInfo, error) {
	perfs, err := s.perfRepo.FindOverlapping(streamID, start, end, uuid.Nil)
	if err != nil {
		return nil, err
	}
	out := make([]dto.OverlapInfo, 0, len(perfs))
	for _, p := range perfs {
		out = append(out, dto.OverlapInfo{
			SongName:     p.SongName,
			StartSeconds: p.StartSeconds,
			EndSeconds:   p.EndSeconds,
		})
	}
	return out, nil
}

// CreateFromMissingSong は「この配信のこの時点に曲がある」という報告から歌唱記録を作る。
//
// **payload に song_id があればそれを使う。** 一括セットリスト作成は照合まで済ませてから
// 審査へ回すので、承認のたびに曲名から引き直すとその結果が捨てられる。DB の表記と
// 食い違う組（`深昏睡` ↔ `深昏睡 (Deep coma)`）では、承認の瞬間に新曲が作られてしまう。
// 曲名から引き直すのは song_id を持たない報告（閲覧者からの投稿）だけ。
//
// 歌手・タグ・終了時間の由来も payload から反映する。以前は perfRepo.Create を呼ぶだけで
// SetSingers を通らず、multi_singer を審査に回す設計なのに承認しても vocalist が空だった。
func (s *PerformanceService) CreateFromMissingSong(p dto.MissingSongPayload) error {
	song, err := s.songForMissing(p)
	if err != nil {
		return err
	}

	// 終了時間の確度。審査担当が画面で時間を見て承認するので、終了時間があれば
	// confirmed とみなす。無い（0＝動画の最後まで）ものは確認しようがないので false。
	endConfirmed := p.EndSeconds > 0
	endSource := p.EndSource
	if endSource == "" && p.EndSeconds > 0 {
		endSource = repository.EndSourceManual
	}

	perf := &models.Performance{
		StreamID:     p.StreamID,
		SongID:       song.ID,
		StartSeconds: p.StartSeconds,
		EndSeconds:   p.EndSeconds,
		OrderIndex:   0, // start_seconds で並べるため使わない
		EndSource:    endSource,
		EndConfirmed: endConfirmed,
	}
	if err := s.perfRepo.Create(perf); err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicatePerformance
		}
		return fmt.Errorf("create performance: %w", err)
	}

	// 歌手は指定が無ければ配信のオーナー（居なければ参加者が1人のときだけその人）。
	// 誰も決まらなければ空のまま作る（複数人の配信で勝手に選ばない）。
	singerIDs := p.SingerIDs
	if len(singerIDs) == 0 {
		singerIDs = s.defaultSingerIDs(p.StreamID)
	}
	if len(singerIDs) > 0 {
		if err := s.perfRepo.SetSingers(perf.ID, singerIDs); err != nil {
			return fmt.Errorf("set performance singers: %w", err)
		}
	}
	if len(p.Tags) > 0 {
		if err := s.perfRepo.SetTags(perf.ID, p.Tags); err != nil {
			return fmt.Errorf("set performance tags: %w", err)
		}
	}

	logger.Infof("performance created from suggestion: %s @%ds (%s, singers=%d)",
		song.Name, p.StartSeconds, p.StreamID, len(singerIDs))
	return nil
}

// songForMissing は提案が指す楽曲を返す。song_id があればそれ、無ければ名前から探す／作る。
//
// song_id が指す曲が消えている（統合された・削除された）場合は名前で引き直す。
// 提案は溜まるものなので、承認するときに対象が消えていることは普通に起きる。
func (s *PerformanceService) songForMissing(p dto.MissingSongPayload) (*models.Song, error) {
	if id := strings.TrimSpace(p.SongID); id != "" {
		songID, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("song_id が不正です")
		}
		song, err := s.songRepo.FindByID(songID)
		if err != nil {
			return nil, fmt.Errorf("find song: %w", err)
		}
		if song != nil {
			return song, nil
		}
		logger.Warnf("missing song suggestion points at a song that no longer exists (%s); falling back to name lookup", id)
	}
	song, _, err := s.findOrCreateSong(dto.CreatePerformanceItem{
		Name:           p.SongName,
		OriginalArtist: p.OriginalArtist,
		ItunesID:       p.ItunesID,
	})
	if err != nil {
		return nil, fmt.Errorf("find or create song: %w", err)
	}
	return song, nil
}

// defaultSingerIDs は配信のオーナー（居なければ参加者が1人のときだけその人）を返す。
// 複数人の配信では誰が歌ったか決められないので空を返す（推測しない）。
func (s *PerformanceService) defaultSingerIDs(streamID string) []string {
	participants, owners, err := s.streamRepo.GetSingersForStreams([]string{streamID})
	if err != nil {
		logger.Warnf("default singers lookup failed (%s): %v", streamID, err)
		return nil
	}
	if owner := owners[streamID]; owner != nil {
		return []string{owner.ID}
	}
	if list := participants[streamID]; len(list) == 1 {
		return []string{list[0].ID}
	}
	return nil
}

// ========== 曲の差し替え提案（SongSwapper） ==========

// SongLabelOf は歌唱の現在の曲名と表示ラベルを返す。歌唱が無ければ空文字。
func (s *PerformanceService) SongLabelOf(performanceID uuid.UUID) (string, string, error) {
	perf, err := s.perfRepo.FindByID(performanceID)
	if err != nil {
		return "", "", fmt.Errorf("find performance: %w", err)
	}
	if perf == nil {
		return "", "", nil
	}
	label := perf.SongName
	if perf.OriginalArtist != "" {
		label += " / " + perf.OriginalArtist
	}
	if perf.StreamTitle != "" {
		label += "（" + perf.StreamTitle + "）"
	}
	return perf.SongName, label, nil
}

// ApplySongSwap は歌唱の曲を別の曲へ繋ぎ替える。
// SongID があればその曲へ、無ければ曲名から探す／作る（未登録の曲へ直す場合）。
// 歌唱の ID は変わらないので、この歌唱を参照しているプレイリストはそのまま残る。
func (s *PerformanceService) ApplySongSwap(performanceID uuid.UUID, p dto.SongSwapPayload) error {
	songID := strings.TrimSpace(p.SongID)
	if songID == "" {
		song, _, err := s.findOrCreateSong(dto.CreatePerformanceItem{
			Name:           p.SongName,
			OriginalArtist: p.OriginalArtist,
		})
		if err != nil {
			return fmt.Errorf("find or create song: %w", err)
		}
		songID = song.ID.String()
	}
	_, err := s.UpdatePerformance(performanceID, &dto.UpdatePerformanceRequest{SongID: &songID})
	return err
}

// ========== 修正提案の対象（TargetEditor） ==========

// GetEditableFields は修正提案の対象となる編集可能フィールドと表示ラベルを返す。
// 見つからなければ (nil, "", nil)。TargetEditor インターフェースを満たす。
//
// 現状は時間軸（開始/終了）のみ。曲の差し替えは「どの曲か」を選ぶ操作で、
// 文字列の差分としては表現できないため、別の提案種別として扱う。
func (s *PerformanceService) GetEditableFields(id uuid.UUID) (map[string]string, string, error) {
	perf, err := s.perfRepo.FindByID(id)
	if err != nil {
		return nil, "", err
	}
	if perf == nil {
		return nil, "", nil
	}
	fields := map[string]string{
		"start_seconds": strconv.Itoa(perf.StartSeconds),
		"end_seconds":   strconv.Itoa(perf.EndSeconds),
	}
	label := perf.SongName
	if perf.OriginalArtist != "" {
		label += " / " + perf.OriginalArtist
	}
	if perf.StreamTitle != "" {
		label += "（" + perf.StreamTitle + "）"
	}
	return fields, label, nil
}

// ApplyEditableFields は提案された編集値を歌唱記録へ反映する。
func (s *PerformanceService) ApplyEditableFields(id uuid.UUID, fields map[string]string) error {
	req := &dto.UpdatePerformanceRequest{}
	if v, ok := fields["start_seconds"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("開始時間が不正です: %s", v)
		}
		req.StartSeconds = &n
	}
	if v, ok := fields["end_seconds"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("終了時間が不正です: %s", v)
		}
		req.EndSeconds = &n
	}
	_, err := s.UpdatePerformance(id, req)
	return err
}

// DeleteByStreamID 刪除指定 stream 的所有演出記錄（セットリストの明示的な全削除用）
// 編集時の保存は ReconcilePerformances による差分更新を使い、ここは通らない。
func (s *PerformanceService) DeleteByStreamID(streamID string) error {
	// 取得所有演出
	performances, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		return fmt.Errorf("find performances: %w", err)
	}

	// 逐一刪除
	for _, perf := range performances {
		if err := s.perfRepo.Delete(perf.ID); err != nil {
			return fmt.Errorf("delete performance: %w", err)
		}
	}

	return nil
}
