package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// StreamTransferService は配信 1 本を環境間で運ぶ（書き出しと取り込み）。
//
// 何のためにあるか：手元で Holodex から新しい配信を取り込み、セットリストを作り、
// それを本番へ上げる ── という進め方を、DB のダンプを使わずに成立させる。
// pg_dump は「取り替え」しかできないので、本番へ向けると users も API キーも
// 巻き添えにする。ここは「1 本ぶんを足す」ことしかしない。
//
// 運ぶ単位を配信 1 本にしてあるのは、それが人の作業の単位だからで、
// 同時に**衝突の範囲を閉じ込める**ためでもある。配信は YouTube 動画 ID が
// 主キーなので、両環境で必ず同じ行を指す。
//
// 設計の要点は 3 つ：
//
//   - UUID を運ばない（dto.StreamExport のコメント参照）
//   - 曲の照合は song_match_keys を通す。`UNIQUE(name, original_artist)` の完全一致で
//     引くと `深昏睡` と `深昏睡 (Deep coma)` が別物になり、向こうに重複が増える
//   - 取り込み先に既にセットリストがあれば**何も書かず全部審査へ回す**。
//     Holodex 同期が向こうで先に曲を作っていることがあり、
//     そこへ機械的に足すと時間が数秒ずれた重複が並ぶ
type StreamTransferService struct {
	streamRepo   *repository.StreamRepository
	perfRepo     *repository.PerformanceRepository
	singerRepo   *repository.SingerRepository
	songRepo     *repository.SongRepository
	tagRepo      *repository.TagRepository
	perfService  *PerformanceService
	suggestions  *SuggestionService
	matchService *SongMatchService
}

func NewStreamTransferService(
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
	singerRepo *repository.SingerRepository,
	songRepo *repository.SongRepository,
	tagRepo *repository.TagRepository,
	perfService *PerformanceService,
	suggestions *SuggestionService,
	matchService *SongMatchService,
) *StreamTransferService {
	return &StreamTransferService{
		streamRepo:   streamRepo,
		perfRepo:     perfRepo,
		singerRepo:   singerRepo,
		songRepo:     songRepo,
		tagRepo:      tagRepo,
		perfService:  perfService,
		suggestions:  suggestions,
		matchService: matchService,
	}
}

// ErrStreamExportVersion は読めない版の書き出しを渡されたときに返る。
var ErrStreamExportVersion = errors.New("対応していない書き出し形式です")

// ========== 書き出し ==========

// Export は配信 1 本を StreamExport へ写す。見つからなければ (nil, nil)。
// withCache=false なら解析キャッシュ（comment_raw など）を載せない。
func (s *StreamTransferService) Export(streamID string, withCache bool) (*dto.StreamExport, error) {
	stream, err := s.streamRepo.FindByID(streamID)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, nil
	}

	perfs, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		return nil, err
	}
	tags, err := s.streamRepo.GetTags(streamID)
	if err != nil {
		return nil, err
	}
	participants, err := s.streamRepo.GetSingers(streamID)
	if err != nil {
		return nil, err
	}
	owner, err := s.streamRepo.GetChannelOwner(streamID)
	if err != nil {
		return nil, err
	}

	out := &dto.StreamExport{
		Version:    dto.StreamExportVersion,
		ExportedAt: time.Now().UTC(),
		Stream: dto.ExportStream{
			ID:           stream.ID,
			Title:        stream.Title,
			StreamDate:   stream.StreamDate.Format("2006-01-02"),
			ThumbnailURL: stream.ThumbnailURL.String,
			IsProcessed:  stream.IsProcessed,
			IsHidden:     stream.IsHidden,
		},
		Singers:      []dto.ExportSinger{},
		Performances: []dto.ExportPerformance{},
	}
	if stream.DurationSeconds.Valid {
		d := stream.DurationSeconds.Int32
		out.Stream.DurationSeconds = &d
	}
	if owner != nil {
		out.Stream.OwnerID = owner.ID
	}
	for _, t := range tags {
		out.Stream.Tags = append(out.Stream.Tags, t.ID)
	}

	// 歌手は「配信の参加者」と「歌唱に紐づく歌手」の和集合。
	// 飛び入りで 1 曲だけ歌った相手は参加者に入っていないことがあり、
	// 落とすと取り込み側で performance_singers が FK 違反になる。
	singerIDs := newOrderedSet()
	for _, p := range participants {
		singerIDs.add(p.ID)
		out.Stream.ParticipantIDs = append(out.Stream.ParticipantIDs, p.ID)
	}
	if owner != nil {
		singerIDs.add(owner.ID)
	}
	for _, perf := range perfs {
		for _, sg := range perf.Singers {
			singerIDs.add(sg.ID)
		}
	}
	for _, id := range singerIDs.items {
		// GetSingers は organization を実効値（override 込み）で返すので、
		// 生の 2 列が要るここでは必ず SingerRepository.FindByID で読み直す。
		sg, err := s.singerRepo.FindByID(id)
		if err != nil {
			return nil, err
		}
		if sg == nil {
			continue
		}
		out.Singers = append(out.Singers, dto.ExportSinger{
			ID:                   sg.ID,
			Name:                 sg.Name,
			EnglishName:          sg.EnglishName.String,
			PhotoURL:             sg.PhotoURL.String,
			Organization:         sg.Organization.String,
			OrganizationOverride: sg.OrganizationOverride.String,
			MetadataSource:       sg.MetadataSource,
			IsHidden:             sg.IsHidden,
		})
	}

	// 読みは performances の JOIN に入っていないので曲ごとに読み直す。
	// 運ぶ理由：向こうで AI 補完をやり直さずに済む（§1.5）。
	readings := map[uuid.UUID]*models.Song{}
	for _, perf := range perfs {
		if _, ok := readings[perf.SongID]; !ok {
			song, err := s.songRepo.FindByID(perf.SongID)
			if err != nil {
				return nil, err
			}
			readings[perf.SongID] = song
		}
		item := dto.ExportPerformance{
			StartSeconds: perf.StartSeconds,
			EndSeconds:   perf.EndSeconds,
			EndSource:    perf.EndSource,
			EndConfirmed: perf.EndConfirmed,
			CustomTags:   perf.CustomTags,
			Song: dto.ExportSong{
				Name:           perf.SongName,
				OriginalArtist: perf.OriginalArtist,
				ArtURL:         perf.Arts.String,
			},
		}
		if song := readings[perf.SongID]; song != nil {
			item.Song.NameReading = song.NameReading.String
			item.Song.OriginalArtistReading = song.OriginalArtistReading.String
		}
		if perf.ItunesID.Valid {
			id := perf.ItunesID.Int64
			item.Song.ItunesID = &id
		}
		for _, t := range perf.Tags {
			item.Tags = append(item.Tags, t.ID)
		}
		for _, sg := range perf.Singers {
			item.SingerIDs = append(item.SingerIDs, sg.ID)
		}
		out.Performances = append(out.Performances, item)
	}

	if withCache {
		cache, err := s.exportCache(stream)
		if err != nil {
			return nil, err
		}
		out.Cache = cache
	}
	return out, nil
}

// exportCache は解析キャッシュを hash ごと写す。
func (s *StreamTransferService) exportCache(stream *models.Stream) (*dto.ExportAnalysisCache, error) {
	normalized, holodexSongsHash, err := s.streamRepo.GetHolodexSongsCache(stream.ID)
	if err != nil {
		return nil, err
	}
	commentHash, err := s.streamRepo.GetCommentSongsHash(stream.ID)
	if err != nil {
		return nil, err
	}
	chapterHash, err := s.streamRepo.GetChapterSongsHash(stream.ID)
	if err != nil {
		return nil, err
	}
	return &dto.ExportAnalysisCache{
		HolodexData:            json.RawMessage(stream.HolodexData),
		HolodexHash:            stream.HolodexHash.String,
		HolodexSongsNormalized: json.RawMessage(normalized),
		HolodexSongsHash:       holodexSongsHash.String,
		CommentRaw:             json.RawMessage(stream.CommentRaw),
		CommentSongs:           json.RawMessage(stream.CommentSongs),
		CommentSongsHash:       commentHash.String,
		ChapterRaw:             json.RawMessage(stream.ChapterRaw),
		ChapterSongs:           json.RawMessage(stream.ChapterSongs),
		ChapterSongsHash:       chapterHash.String,
	}, nil
}

// ========== 取り込み ==========

// ImportOptions は取り込みの動作。
type ImportOptions struct {
	// DryRun は書き込まずに件数だけ返す。取り込み先で何が起きるかを先に見るためのもの。
	DryRun bool
	// WithCache は解析キャッシュも書き込むか。
	WithCache bool
}

// Import は StreamExport を取り込む。
//
// **既存を消さない。** 配信と歌手は upsert、歌唱は「まだ 1 件も無いときだけ」作る。
// 取り込み先に既にセットリストがあれば 1 件も書かず、全部審査へ回す。
func (s *StreamTransferService) Import(data *dto.StreamExport, opts ImportOptions) (*dto.ImportStreamResult, error) {
	if data == nil || strings.TrimSpace(data.Stream.ID) == "" {
		return nil, errors.New("配信 ID がありません")
	}
	if data.Version > dto.StreamExportVersion {
		return nil, fmt.Errorf("%w: version=%d（このサーバーが読めるのは %d まで）",
			ErrStreamExportVersion, data.Version, dto.StreamExportVersion)
	}
	streamID := strings.TrimSpace(data.Stream.ID)
	res := &dto.ImportStreamResult{StreamID: streamID, DryRun: opts.DryRun}

	knownSingers, err := s.importSingers(data.Singers, opts.DryRun, res)
	if err != nil {
		return nil, err
	}
	if err := s.importStream(data, knownSingers, opts, res); err != nil {
		return nil, err
	}
	if err := s.importPerformances(streamID, data.Performances, knownSingers, opts, res); err != nil {
		return nil, err
	}
	if opts.WithCache {
		s.importCache(streamID, data.Cache, opts.DryRun, res)
	}

	logger.Infof("[import] %s: stream_created=%v singers=+%d/~%d performances=%d suggested=%d skipped=%d dry_run=%v",
		streamID, res.StreamCreated, res.SingersCreated, res.SingersUpdated,
		res.PerformancesCreated, res.Suggested, res.Skipped, opts.DryRun)
	return res, nil
}

// importSingers は歌手を upsert し、取り込み先に存在する歌手 ID の集合を返す。
//
// 空の項目で既存値を消さない（「書かれていない」は「消してよい」ではない）。
// これが要るのは、書き出し元が古かったり、手元では英語名を埋めていないだけ、
// という状況が普通にあるため。
func (s *StreamTransferService) importSingers(list []dto.ExportSinger, dryRun bool, res *dto.ImportStreamResult) (map[string]bool, error) {
	known := map[string]bool{}
	for _, in := range list {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			continue
		}
		cur, err := s.singerRepo.FindByID(id)
		if err != nil {
			return nil, err
		}
		known[id] = true
		if cur == nil {
			res.SingersCreated++
		} else {
			res.SingersUpdated++
		}
		if dryRun {
			continue
		}

		sg := &models.Singer{
			ID:             id,
			Name:           in.Name,
			EnglishName:    keepIfEmpty(in.EnglishName, cur, func(c *models.Singer) sql.NullString { return c.EnglishName }),
			PhotoURL:       keepIfEmpty(in.PhotoURL, cur, func(c *models.Singer) sql.NullString { return c.PhotoURL }),
			Organization:   keepIfEmpty(in.Organization, cur, func(c *models.Singer) sql.NullString { return c.Organization }),
			MetadataSource: in.MetadataSource,
		}
		if sg.Name == "" && cur != nil {
			sg.Name = cur.Name
		}
		// Upsert は is_hidden を INSERT のときしか書かない（同期で手動設定が戻らないように）。
		// organization_override も触らない。どちらもここで意図的に別の口から入れる。
		if err := s.singerRepo.Upsert(sg); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("歌手 %s: %v", id, err))
			continue
		}
		if in.OrganizationOverride != "" {
			if _, err := s.singerRepo.SetOrganizationOverride(id, in.OrganizationOverride); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("歌手 %s の所属（手動指定）を反映できませんでした: %v", id, err))
			}
		}
		// is_hidden は**新規作成のときだけ**運ぶ。既存を上書きすると、
		// 取り込み先で「隠す」と決めたチャンネルが取り込みのたびに表へ戻る。
		if cur == nil && in.IsHidden {
			if _, err := s.singerRepo.SetHidden(id, true); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("歌手 %s を非表示にできませんでした: %v", id, err))
			}
		}
	}
	return known, nil
}

// importStream は配信本体・タグ・参加者を反映する。
func (s *StreamTransferService) importStream(data *dto.StreamExport, knownSingers map[string]bool, opts ImportOptions, res *dto.ImportStreamResult) error {
	in := data.Stream
	streamID := strings.TrimSpace(in.ID)

	date, err := time.Parse("2006-01-02", in.StreamDate)
	if err != nil {
		return fmt.Errorf("stream_date が不正です (%q): %w", in.StreamDate, err)
	}
	cur, err := s.streamRepo.FindByID(streamID)
	if err != nil {
		return err
	}
	res.StreamCreated = cur == nil

	// 検証は dry-run でも通す。「取り込んだら何が落ちるか」を先に見るための機能なので、
	// 本番でだけ警告が増えるのでは用を成さない。
	//
	// タグは stream_tags への FK。取り込み先に無い ID が 1 つでも混ざると
	// SetTags が丸ごと失敗するので、実在するものだけに絞る。
	// **落としたものは必ず警告に出す** ── 黙って落とすと、タグが付かない理由が
	// どこにも残らない（perftag の語彙ずれで一度これをやっている。§4 参照）。
	tagIDs, dropped, err := s.filterStreamTags(in.Tags)
	if err != nil {
		return err
	}
	if len(dropped) > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("この環境に無い配信タグを飛ばしました: %s", strings.Join(dropped, ", ")))
	}
	participants, missing := filterKnown(in.ParticipantIDs, knownSingers)
	if len(missing) > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("名簿に無い参加者を飛ばしました: %s", strings.Join(missing, ", ")))
	}

	if opts.DryRun {
		return nil
	}

	st := &models.Stream{
		ID:           streamID,
		Title:        in.Title,
		StreamDate:   date,
		ThumbnailURL: keepIfEmpty(in.ThumbnailURL, cur, func(c *models.Stream) sql.NullString { return c.ThumbnailURL }),
		// is_hidden は Upsert では INSERT のときしか書かれない。
		// 既存配信の表示/非表示は取り込み先の判断を尊重する（歌手と同じ理由）。
		IsHidden: in.IsHidden,
	}
	if in.DurationSeconds != nil {
		st.DurationSeconds = sql.NullInt32{Int32: *in.DurationSeconds, Valid: true}
	} else if cur != nil {
		st.DurationSeconds = cur.DurationSeconds
	}
	// holodex_data は Upsert の対象なので、キャッシュを運ぶときだけ載せる。
	// 運ばないときに空で通すと、取り込み先が持っていた Holodex の元データを消してしまう。
	if opts.WithCache && data.Cache != nil && len(data.Cache.HolodexData) > 0 {
		st.HolodexData = data.Cache.HolodexData
		st.HolodexHash = sql.NullString{String: data.Cache.HolodexHash, Valid: data.Cache.HolodexHash != ""}
	} else if cur != nil {
		st.HolodexData = cur.HolodexData
		st.HolodexHash = cur.HolodexHash
	}
	if err := s.streamRepo.Upsert(st); err != nil {
		return fmt.Errorf("配信の取り込みに失敗: %w", err)
	}

	if err := s.streamRepo.SetTags(streamID, tagIDs); err != nil {
		return fmt.Errorf("配信タグの反映に失敗: %w", err)
	}
	if err := s.streamRepo.SetSingers(streamID, participants, in.OwnerID); err != nil {
		return fmt.Errorf("配信の参加者の反映に失敗: %w", err)
	}
	return nil
}

// importPerformances は歌唱記録を反映する。
//
// **取り込み先に歌唱が 1 件でもあれば、1 件も書かずに全部審査へ回す。**
// 向こうでは毎日 Holodex 同期が回っていて、同じ配信のセットリストを先に
// 作っていることがある。そこへ機械的に足すと、開始が数秒ずれただけの重複が並ぶ
// ──`UNIQUE(stream_id, song_id, start_seconds)` はこれを止められない。
// どちらが正しいかは人にしか決められないので、判断を人に渡す。
func (s *StreamTransferService) importPerformances(streamID string, list []dto.ExportPerformance, knownSingers map[string]bool, opts ImportOptions, res *dto.ImportStreamResult) error {
	if len(list) == 0 {
		return nil
	}
	existing, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		return err
	}

	if len(existing) > 0 {
		for _, in := range list {
			switch s.suggestPerformance(streamID, in, knownSingers, opts.DryRun) {
			case suggestAdded:
				res.Suggested++
			case suggestDuplicate:
				res.Skipped++
			default:
				res.Errors = append(res.Errors,
					fmt.Sprintf("審査へ回せませんでした: %s (%ds)", in.Song.Name, in.StartSeconds))
			}
		}
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("この配信には既に %d 件の歌唱があるため、取り込んだ %d 件は登録せず審査へ回しました",
				len(existing), len(list)))
		return nil
	}

	items := make([]dto.CreatePerformanceItem, 0, len(list))
	for _, in := range list {
		item, warnings := s.toCreateItem(in, knownSingers)
		res.Warnings = append(res.Warnings, warnings...)
		items = append(items, item)
	}
	if opts.DryRun {
		res.PerformancesCreated = len(items)
		return nil
	}
	// 曲の照合と作成は編集画面と同じ経路（findOrCreateSong）に任せる。
	// ここで自前に引き直すと、統合候補の記録も iTunes の紐付けも漏れる。
	if _, err := s.perfService.CreatePerformances(streamID, items); err != nil {
		return fmt.Errorf("歌唱の取り込みに失敗: %w", err)
	}
	res.PerformancesCreated = len(items)
	return nil
}

// toCreateItem は書き出しの 1 件を編集画面と同じ入力形式へ写す。
func (s *StreamTransferService) toCreateItem(in dto.ExportPerformance, knownSingers map[string]bool) (dto.CreatePerformanceItem, []string) {
	var warnings []string
	confirmed := in.EndConfirmed
	item := dto.CreatePerformanceItem{
		Name:                  in.Song.Name,
		NameReading:           in.Song.NameReading,
		OriginalArtist:        in.Song.OriginalArtist,
		OriginalArtistReading: in.Song.OriginalArtistReading,
		StartSeconds:          in.StartSeconds,
		EndSeconds:            in.EndSeconds,
		CustomTags:            in.CustomTags,
		ItunesID:              in.Song.ItunesID,
		EndSource:             in.EndSource,
		EndConfirmed:          &confirmed,
	}
	if in.Song.ArtURL != "" {
		art := in.Song.ArtURL
		item.ArtURL = &art
	}

	tags, dropped, err := s.filterPerformanceTags(in.Tags)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: 歌唱タグを確認できませんでした: %v", in.Song.Name, err))
	} else if len(dropped) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s: この環境に無い歌唱タグを飛ばしました: %s",
			in.Song.Name, strings.Join(dropped, ", ")))
	}
	item.Tags = tags

	singers, missing := filterKnown(in.SingerIDs, knownSingers)
	if len(missing) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s: 名簿に無い歌手を飛ばしました: %s",
			in.Song.Name, strings.Join(missing, ", ")))
	}
	item.SingerIDs = singers
	return item, warnings
}

type suggestOutcome int

const (
	suggestFailed suggestOutcome = iota
	suggestAdded
	suggestDuplicate
)

// suggestPerformance は歌唱 1 件を perf.missing の審査待ちとして積む。
//
// 曲はここで照合しておき、決まったものは payload に song_id を載せる
// ── 承認は song_id があれば曲名から引き直さないので、
// 照合の結果が承認の瞬間に捨てられない（§4.5）。
func (s *StreamTransferService) suggestPerformance(streamID string, in dto.ExportPerformance, knownSingers map[string]bool, dryRun bool) suggestOutcome {
	if s.suggestions == nil {
		return suggestFailed
	}
	songID, candidates, reasons := s.resolveSong(in.Song)
	reasons = append(reasons, reviewAddition)

	singers, _ := filterKnown(in.SingerIDs, knownSingers)
	// タグの確認に失敗したら、絞らずそのまま載せる。ここで空にすると
	// 「審査に回ったらタグが消えていた」になり、原因がどこにも残らない
	// （承認時の SetTags が改めて実在するものだけを付ける）。
	tags, _, err := s.filterPerformanceTags(in.Tags)
	if err != nil {
		tags = in.Tags
	}

	payload := &dto.MissingSongPayload{
		StreamID:              streamID,
		SongName:              in.Song.Name,
		OriginalArtist:        in.Song.OriginalArtist,
		StartSeconds:          in.StartSeconds,
		EndSeconds:            in.EndSeconds,
		SongID:                songID,
		SingerIDs:             singers,
		EndSource:             in.EndSource,
		Tags:                  tags,
		ItunesID:              in.Song.ItunesID,
		ArtURL:                in.Song.ArtURL,
		NameReading:           in.Song.NameReading,
		OriginalArtistReading: in.Song.OriginalArtistReading,
		CustomTags:            in.CustomTags,
		ReviewReasons:         reasons,
		Source:                suggestionSourceImport,
		Via:                   "rule",
		ConflictKind:          "addition",
		Candidates:            candidates,
		RawName:               in.Song.Name,
		RawArtist:             in.Song.OriginalArtist,
	}
	if dryRun {
		return suggestAdded
	}

	_, createErr := s.suggestions.Create(&dto.CreateSuggestionRequest{
		Kind:    KindMissingSong,
		Payload: payload,
		Note:    fmt.Sprintf("配信の取り込み（%s）", joinReasons(reasons)),
	}, SuggestionActor{ClientHint: suggestionSourceImport, System: true})
	if createErr != nil {
		if errors.Is(createErr, ErrDuplicateSuggestion) {
			return suggestDuplicate
		}
		logger.Warnf("[import] 審査の登録に失敗 (%s / %s): %v", streamID, in.Song.Name, createErr)
		return suggestFailed
	}
	return suggestAdded
}

// resolveSong は書き出された曲を、この環境の songs へ照合する。
//
// **`UNIQUE(name, original_artist)` の完全一致では引かない。** それだと
// `深昏睡` と `深昏睡 (Deep coma)` が別物になり、取り込みのたびに重複が増える。
// 引くのは編集画面と同じ song_match_keys（§7.5）で、閾値もそこに合わせる。
func (s *StreamTransferService) resolveSong(in dto.ExportSong) (songID string, candidates []dto.SongMatchCandidate, reasons []string) {
	if s.matchService == nil {
		return "", nil, []string{reviewUnmatched}
	}
	found, err := s.matchService.FindCandidates(in.Name, in.OriginalArtist, in.ItunesID)
	if err != nil {
		logger.Warnf("[import] 楽曲の照合に失敗 (%s): %v", in.Name, err)
		return "", nil, []string{reviewUnmatched}
	}
	if len(found) > 0 && found[0].Auto() {
		songID = found[0].Song.ID.String()
	} else {
		reasons = append(reasons, reviewUnmatched)
	}
	for _, c := range found {
		candidates = append(candidates, dto.SongMatchCandidate{
			SongID:  c.Song.ID.String(),
			Name:    c.Song.Name,
			Artist:  c.Song.OriginalArtist,
			Score:   c.Score,
			Reason:  c.Reason,
			ArtURL:  c.Song.Arts.String,
			IsMatch: songID != "" && c.Song.ID.String() == songID,
		})
	}
	return songID, candidates, reasons
}

// importCache は解析キャッシュを書き込む。**hash と対で書く**
// （中身だけ入れても hash が合わなければキャッシュ判定が外れ、運んだ意味が無い）。
// 失敗しても取り込み自体は止めない ── キャッシュは再取得できる。
func (s *StreamTransferService) importCache(streamID string, cache *dto.ExportAnalysisCache, dryRun bool, res *dto.ImportStreamResult) {
	if cache == nil || dryRun {
		return
	}
	record := func(name string, err error) {
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s を取り込めませんでした: %v", name, err))
			return
		}
		res.CacheImported = append(res.CacheImported, name)
	}

	if len(cache.CommentRaw) > 0 {
		record("comment_raw", s.streamRepo.SaveCommentRaw(streamID, cache.CommentRaw))
	}
	if len(cache.CommentSongs) > 0 && cache.CommentSongsHash != "" {
		record("comment_songs", s.streamRepo.SaveCommentSongs(streamID, cache.CommentSongs, cache.CommentSongsHash))
	}
	// chapter_raw は 3 態。`[]`（調べたが章節が無い）も意味のある値なので書く。
	if len(cache.ChapterRaw) > 0 {
		record("chapter_raw", s.streamRepo.SaveChapterRaw(streamID, cache.ChapterRaw))
	}
	if len(cache.ChapterSongs) > 0 && cache.ChapterSongsHash != "" {
		record("chapter_songs", s.streamRepo.SaveChapterSongs(streamID, cache.ChapterSongs, cache.ChapterSongsHash))
	}
	if len(cache.HolodexSongsNormalized) > 0 && cache.HolodexSongsHash != "" {
		record("holodex_songs_normalized", s.streamRepo.SaveHolodexSongs(streamID, cache.HolodexSongsNormalized, cache.HolodexSongsHash))
	}
}

// ========== 小道具 ==========

// suggestionSourceImport は審査画面に出す「どこから来た行か」。
const suggestionSourceImport = "import"

// orderedSet は「重複を除きつつ順序を保つ」ための最小限の入れ物。
// 書き出しの並びが実行ごとに変わると差分が読めなくなるので map を直接使わない。
type orderedSet struct {
	seen  map[string]bool
	items []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]bool{}}
}

func (o *orderedSet) add(v string) {
	if v == "" || o.seen[v] {
		return
	}
	o.seen[v] = true
	o.items = append(o.items, v)
}

// keepIfEmpty は「書かれていれば新しい値、書かれていなければ既存値」を返す。
// 空文字を「消してよい」と解釈しないためのもの。
func keepIfEmpty[T any](v string, cur *T, get func(*T) sql.NullString) sql.NullString {
	if strings.TrimSpace(v) != "" {
		return sql.NullString{String: v, Valid: true}
	}
	if cur != nil {
		return get(cur)
	}
	return sql.NullString{}
}

// filterKnown は known に含まれる ID だけを残し、落としたものを別に返す。
func filterKnown(ids []string, known map[string]bool) (kept, missing []string) {
	kept = []string{}
	for _, id := range ids {
		if known[id] {
			kept = append(kept, id)
			continue
		}
		missing = append(missing, id)
	}
	return kept, missing
}

// filterStreamTags は実在する配信タグだけを残す。
func (s *StreamTransferService) filterStreamTags(ids []string) (kept, dropped []string, err error) {
	if len(ids) == 0 {
		return []string{}, nil, nil
	}
	all, err := s.tagRepo.FindAllStreamTags()
	if err != nil {
		return nil, nil, err
	}
	valid := make(map[string]bool, len(all))
	for _, t := range all {
		valid[t.ID] = true
	}
	k, d := filterKnown(ids, valid)
	return k, d, nil
}

// filterPerformanceTags は実在する歌唱タグだけを残す。
func (s *StreamTransferService) filterPerformanceTags(ids []string) (kept, dropped []string, err error) {
	if len(ids) == 0 {
		return []string{}, nil, nil
	}
	validIDs, err := s.perfRepo.GetValidTagIDs(ids)
	if err != nil {
		return nil, nil, err
	}
	valid := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}
	k, d := filterKnown(ids, valid)
	return k, d, nil
}
