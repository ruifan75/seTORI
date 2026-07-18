package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/chatend"
	"github.com/ruifan75/setori/pkg/util"
)

// ChatEndService 從 live chat 的「拍手」偵測每首歌的結束時間，並更新 comment_songs。
type ChatEndService struct {
	streamRepo *repository.StreamRepository
	ytdlp      string
	cacheDir   string
}

func NewChatEndService(streamRepo *repository.StreamRepository, ytdlpPath, cacheDir string) *ChatEndService {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	if cacheDir == "" {
		cacheDir = "data/chat_cache"
	}
	return &ChatEndService{streamRepo: streamRepo, ytdlp: ytdlpPath, cacheDir: cacheDir}
}

// AnalyzeStream 下載 live chat → 偵測拍手結束時間 → 更新 comment_songs 的 end。
// 以 comment_songs 既有的 start 為輸入，找出每首歌的曲末拍手作為 end。
func (s *ChatEndService) AnalyzeStream(videoID string) error {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return fmt.Errorf("find stream: %w", err)
	}
	if stream == nil || len(stream.CommentSongs) == 0 {
		return nil
	}

	var songs []dto.CommentSong
	if err := json.Unmarshal(stream.CommentSongs, &songs); err != nil || len(songs) == 0 {
		return nil
	}

	var duration int
	if stream.DurationSeconds.Valid {
		duration = int(stream.DurationSeconds.Int32)
	}

	songs, updated := s.DetectEndsForSongs(videoID, duration, songs)
	if updated == 0 {
		return nil
	}

	raw, err := json.Marshal(songs)
	if err != nil {
		return fmt.Errorf("marshal comment songs: %w", err)
	}
	raw = util.SanitizeJSONB(raw)
	stream.CommentSongs = raw
	if err := s.streamRepo.Update(stream); err != nil {
		return fmt.Errorf("update stream: %w", err)
	}
	logger.Infof("[chatend] %s: %d/%d 曲の拍手終了を取得", videoID, updated, len(songs))
	return nil
}

// DetectEnds 用 live chat 拍手為給定的 start 秒數偵測曲末 end，回傳 start→end 對應。
// 不寫 DB —— comment / holodex 兩個分析流程共用。live chat 不可用時回傳空 map。
func (s *ChatEndService) DetectEnds(videoID string, durationSeconds int, starts []int) map[int]int {
	if len(starts) == 0 {
		return nil
	}
	chatPath, err := s.fetchLiveChat(videoID)
	if err != nil {
		logger.Warnf("[chatend] %s: live chat 不可用、end 推定をスキップ: %v", videoID, err)
		return nil
	}
	events, err := chatend.ParseLiveChatFile(chatPath)
	if err != nil {
		logger.Warnf("[chatend] %s: parse live chat 失敗: %v", videoID, err)
		return nil
	}

	fstarts := make([]float64, len(starts))
	for i, st := range starts {
		fstarts[i] = float64(st)
	}
	ends := chatend.DetectEnds(fstarts, events, float64(durationSeconds), chatend.DefaultOptions())
	endByStart := make(map[int]int, len(ends))
	for _, e := range ends {
		if e.End != nil {
			endByStart[int(e.Start)] = int(*e.End)
		}
	}
	return endByStart
}

// DetectEndsForSongs 對 comment songs 套用拍手 end 偵測。
// 策略（配合使用者偏好）：
// - 如果原本就有 explicit end（comment 提供的 range 時間），則保留它，並把 chat 偵測值放到 ChatEnd。
// - 只有原本沒有 explicit end 時，才使用 chat 偵測的值。
// - 同時計算 EndDiff，方便前端在差異大時提醒使用者檢查。
func (s *ChatEndService) DetectEndsForSongs(videoID string, durationSeconds int, songs []dto.CommentSong) ([]dto.CommentSong, int) {
	if len(songs) == 0 {
		return songs, 0
	}
	starts := make([]int, len(songs))
	for i, sg := range songs {
		starts[i] = sg.Start
	}
	endByStart := s.DetectEnds(videoID, durationSeconds, starts)
	updated := 0

	for i := range songs {
		chatEnd, hasChat := endByStart[songs[i].Start]
		if !hasChat {
			continue
		}

		hasExplicitEnd := songs[i].End > 0 && !songs[i].IsEndTimeEstimated

		if hasExplicitEnd {
			// 保留 comment 的 explicit end，只記錄 chat 建議值
			songs[i].ChatEnd = chatEnd
			songs[i].EndDiff = abs(songs[i].End - chatEnd)
			// 不改 End 和 IsEndTimeEstimated
		} else {
			// 原本沒有明確 end，用 chat 的
			songs[i].End = chatEnd
			songs[i].IsEndTimeEstimated = false
			songs[i].ChatEnd = chatEnd // 讓前端知道這是 chat 值
			updated++
		}
	}
	return songs, updated
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// AnalyzeStreamAsync 在背景跑 AnalyzeStream（sync 時呼叫，不擋主流程）。
func (s *ChatEndService) AnalyzeStreamAsync(videoID string) {
	go func() {
		if err := s.AnalyzeStream(videoID); err != nil {
			logger.Warnf("[chatend] analyze error (%s): %v", videoID, err)
		}
	}()
}

// Backfill 對所有有 comment_songs 的歌回跑拍手 end 偵測（bounded concurrency）。
// 適合對既有資料補跑；已下載的 live chat 會用快取，所以重跑很便宜。
func (s *ChatEndService) Backfill(concurrency int) {
	ids, err := s.streamRepo.FindIDsWithCommentSongs()
	if err != nil {
		logger.Warnf("[chatend] backfill: list streams failed: %v", err)
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	logger.Infof("[chatend] backfill 開始: %d 件 (concurrency=%d)", len(ids), concurrency)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var done int64
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.AnalyzeStream(id); err != nil {
				logger.Warnf("[chatend] backfill %s: %v", id, err)
			}
			if n := atomic.AddInt64(&done, 1); n%10 == 0 || int(n) == len(ids) {
				logger.Infof("[chatend] backfill 進捗: %d/%d", n, len(ids))
			}
		}(id)
	}
	wg.Wait()
	logger.Infof("[chatend] backfill 完了: %d 件", len(ids))
}

// fetchLiveChat 用 yt-dlp 下載 live chat replay（已下載則用快取）。
func (s *ChatEndService) fetchLiveChat(videoID string) (string, error) {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	base := filepath.Join(s.cacheDir, videoID)
	chat := base + ".live_chat.json"
	if _, err := os.Stat(chat); err == nil {
		return chat, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ytdlp,
		"--skip-download", "--write-subs", "--sub-langs", "live_chat",
		"--socket-timeout", "30",
		"-o", base+".%(ext)s",
		"https://www.youtube.com/watch?v="+videoID)
	_ = cmd.Run() // 失敗就靠下面檢查檔案

	if _, err := os.Stat(chat); err == nil {
		return chat, nil
	}
	return "", fmt.Errorf("no live chat available for %s", videoID)
}

// EstimateEnds は任意の開始秒数リストに対する拍手 end 推定を返す（編集ページの単曲追加用）。
// live chat はローカルキャッシュされるため、2回目以降は安価。
func (s *ChatEndService) EstimateEnds(videoID string, starts []int) (map[int]int, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}
	var duration int
	if stream.DurationSeconds.Valid {
		duration = int(stream.DurationSeconds.Int32)
	}
	return s.DetectEnds(videoID, duration, starts), nil
}
