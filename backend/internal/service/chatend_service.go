package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruifan75/setori/internal/dto"
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
	log.Printf("[chatend] %s: %d/%d 曲の拍手終了を取得", videoID, updated, len(songs))
	return nil
}

// DetectEndsForSongs 用 live chat 拍手為給定 songs（含 start）偵測 end，回傳填好 end 的 songs 與更新數。
// 不寫 DB —— 供分析流程在持久化前呼叫。live chat 不可用時原樣返回（end 維持估計值）。
func (s *ChatEndService) DetectEndsForSongs(videoID string, durationSeconds int, songs []dto.CommentSong) ([]dto.CommentSong, int) {
	if len(songs) == 0 {
		return songs, 0
	}
	chatPath, err := s.fetchLiveChat(videoID)
	if err != nil {
		log.Printf("[chatend] %s: live chat 不可用、end 推定をスキップ: %v", videoID, err)
		return songs, 0
	}
	events, err := chatend.ParseLiveChatFile(chatPath)
	if err != nil {
		log.Printf("[chatend] %s: parse live chat 失敗: %v", videoID, err)
		return songs, 0
	}

	starts := make([]float64, len(songs))
	for i, sg := range songs {
		starts[i] = float64(sg.Start)
	}
	ends := chatend.DetectEnds(starts, events, float64(durationSeconds), chatend.DefaultOptions())
	endByStart := make(map[int]float64, len(ends))
	for _, e := range ends {
		if e.End != nil {
			endByStart[int(e.Start)] = *e.End
		}
	}

	updated := 0
	for i := range songs {
		if end, ok := endByStart[songs[i].Start]; ok {
			songs[i].End = int(end)
			songs[i].IsEndTimeEstimated = false // 拍手は視聴者の即時反応なので非推定とみなす
			updated++
		}
	}
	return songs, updated
}

// AnalyzeStreamAsync 在背景跑 AnalyzeStream（sync 時呼叫，不擋主流程）。
func (s *ChatEndService) AnalyzeStreamAsync(videoID string) {
	go func() {
		if err := s.AnalyzeStream(videoID); err != nil {
			log.Printf("[chatend] analyze error (%s): %v", videoID, err)
		}
	}()
}

// Backfill 對所有有 comment_songs 的歌回跑拍手 end 偵測（bounded concurrency）。
// 適合對既有資料補跑；已下載的 live chat 會用快取，所以重跑很便宜。
func (s *ChatEndService) Backfill(concurrency int) {
	ids, err := s.streamRepo.FindIDsWithCommentSongs()
	if err != nil {
		log.Printf("[chatend] backfill: list streams failed: %v", err)
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	log.Printf("[chatend] backfill 開始: %d 件 (concurrency=%d)", len(ids), concurrency)

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
				log.Printf("[chatend] backfill %s: %v", id, err)
			}
			if n := atomic.AddInt64(&done, 1); n%10 == 0 || int(n) == len(ids) {
				log.Printf("[chatend] backfill 進捗: %d/%d", n, len(ids))
			}
		}(id)
	}
	wg.Wait()
	log.Printf("[chatend] backfill 完了: %d 件", len(ids))
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
