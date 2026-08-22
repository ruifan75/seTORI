package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

// AvailabilityService は配信が実際に再生できるかを yt-dlp で調べる。
//
// live chat とチャプターの取得にも相乗りさせてあるが（それぞれの fetch を参照）、
// **それだけでは埋まらない**：
//   - live chat はファイルキャッシュがあると yt-dlp を呼ぶ前に return する
//   - チャプターの backfill は is_hidden = FALSE に限定されている
//
// 判定したい会限の歌枠はどれも非表示側にあるので、専用の経路が要る。
type AvailabilityService struct {
	*ytdlpRunner // ChatEndService と**同じ実体**を共有する（cookie を 2 か所に持たない）
	streamRepo   *repository.StreamRepository
}

func NewAvailabilityService(streamRepo *repository.StreamRepository, chatEnd *ChatEndService) *AvailabilityService {
	return &AvailabilityService{ytdlpRunner: chatEnd.ytdlpRunner, streamRepo: streamRepo}
}

// Fetch は 1 配信の再生可否を調べて保存する。
//
// **`--ignore-no-formats-error` は付けない。** 他の経路はフォーマット一覧が空でも
// 続行したいので付けているが、その副作用で視聴できない動画まで
// availability = public で返ってくる。ここは再生可否そのものを知りたいので、
// 動画情報を取れないことを**エラーとして受け取る**（保存はする ── 「調べた結果、
// 取れなかった」も事実なので、未調査のまま毎回調べ直すのは避ける）。
func (s *AvailabilityService) Fetch(videoID string) (ytdlpAvailability, error) {
	args := []string{
		"--skip-download",
		"--no-warnings",
		"--socket-timeout", "30",
		"--print", availabilityPrintTemplate,
	}
	if cookiePath, cleanup := s.prepareCookies(); cookiePath != "" {
		defer cleanup()
		args = append(args, "--cookies", cookiePath)
	}
	args = append(args, "https://www.youtube.com/watch?v="+videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	a := parseYtdlpAvailability(lastNonEmptyLine(stdout.String()))

	if runErr != nil {
		switch {
		case notInstalled(runErr):
			return a, fmt.Errorf("yt-dlp が実行できません (%s): 未インストールの可能性があります", s.path)
		case ctx.Err() != nil:
			return a, fmt.Errorf("yt-dlp がタイムアウトしました（2分）")
		case isBotCheck(stderr.String()):
			// BOT 判定は全配信で一様に起きる。値を保存すると「調べ済み」になって
			// cookie を入れ直したあとの調べ直しから漏れるので、保存せずに返す。
			return a, botCheckError(s.ytdlpRunner)
		}
		// ここまで来たら動画そのものを取れなかった。
		//
		// **cookie が無いなら記録しない。** 未設定のときは「削除された」と
		// 「会限で見えない」が同じ失敗になり、どちらか分からないまま
		// unavailable として保存すると、会限の歌枠に
		// 「削除された可能性があります」と表示することになる。
		// エラー文字列で見分ける手もあるが、yt-dlp の文言は版で変わるので当てにしない
		// （cookie を入れて調べ直せば subscriber_only という構造化された値で返る）。
		if !s.HasCookies() {
			return a, fmt.Errorf("動画情報を取得できません（cookie 未設定のため会限と区別できず、判定は保存しません）: %s",
				ytdlpErrorLine(stderr.String()))
		}
		// 取れなかったこと自体を記録する ── availability は空、playable_in_embed は NULL。
		if err := s.save(videoID, a); err != nil {
			return a, err
		}
		return a, fmt.Errorf("動画情報を取得できません: %s", ytdlpErrorLine(stderr.String()))
	}

	if err := s.save(videoID, a); err != nil {
		return a, err
	}
	return a, nil
}

func (s *AvailabilityService) save(videoID string, a ytdlpAvailability) error {
	var avail sql.NullString
	if a.Availability != "" {
		avail = sql.NullString{String: a.Availability, Valid: true}
	}
	if err := s.streamRepo.SaveAvailability(videoID, avail, a.PlayableInEmbed); err != nil {
		return fmt.Errorf("save availability: %w", err)
	}
	return nil
}

// Backfill は未調査の配信をまとめて調べる（非同期。進捗はログに出す）。
// 対象に非表示を含めるのは FindIDsWithoutAvailability の注意書きのとおり。
func (s *AvailabilityService) Backfill(concurrency int) (int, error) {
	ids, err := s.streamRepo.FindIDsWithoutAvailability()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 8 {
		concurrency = 8
	}

	go func() {
		logger.Infof("[availability] backfill を開始: %d 件 (並列 %d)", len(ids), concurrency)
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var done int64
		for _, id := range ids {
			sem <- struct{}{}
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()
				if _, err := s.Fetch(id); err != nil {
					logger.Warnf("[availability] backfill %s: %v", id, err)
				}
				if n := atomic.AddInt64(&done, 1); n%20 == 0 || int(n) == len(ids) {
					logger.Infof("[availability] backfill の進捗: %d/%d", n, len(ids))
				}
			}(id)
		}
		wg.Wait()
		logger.Infof("[availability] backfill が完了: %d 件", len(ids))
	}()

	return len(ids), nil
}
