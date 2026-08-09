package service

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/database"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/gdrive"
)

const (
	settingsKeyBackup      = "backup"
	settingsKeyGDriveToken = "gdrive_token"
	driveBackupFolderName  = "seTORI Backups"
)

// backupNameRe はバックアップファイル名として許可する形式（パストラバーサル防止）。
var backupNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dump$`)

// BackupSettings は自動バックアップの設定（app_settings に JSON 保存）。
type BackupSettings struct {
	AutoEnabled      bool       `json:"auto_enabled"`
	IntervalHours    int        `json:"interval_hours"`
	RetentionLocal   int        `json:"retention_local"`
	RetentionDrive   int        `json:"retention_drive"`
	DriveUpload      bool       `json:"drive_upload"`
	DriveFolderID    string     `json:"drive_folder_id,omitempty"`
	LastBackupAt     *time.Time `json:"last_backup_at,omitempty"`
	LastBackupStatus string     `json:"last_backup_status,omitempty"`
}

// gdriveToken は連携済み Google アカウントのトークン（app_settings に JSON 保存）。
type gdriveToken struct {
	RefreshToken string `json:"refresh_token"`
	Email        string `json:"email"`
}

// BackupFileInfo はローカルバックアップ 1 件のメタ情報。
type BackupFileInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// DriveStatus は Google Drive 連携の状態。
type DriveStatus struct {
	Configured bool   `json:"configured"` // OAuth クライアント ID/シークレットが .env に設定済み
	Connected  bool   `json:"connected"`  // アカウント連携済み（リフレッシュトークン保存済み）
	Email      string `json:"email,omitempty"`
	FolderName string `json:"folder_name,omitempty"`
}

// BackupResult はバックアップ実行結果。
type BackupResult struct {
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	DriveUploaded bool   `json:"drive_uploaded"`
	DriveError    string `json:"drive_error,omitempty"`
}

// BackupService は pg_dump/pg_restore によるバックアップ・リストアと
// Google Drive への自動アップロードを担う。
// ホストに pg_dump が無い場合は docker exec（PostgreSQL コンテナ内のツール）で実行する。
type BackupService struct {
	db           *sql.DB
	settingsRepo *repository.AppSettingsRepository
	drive        *gdrive.Client

	databaseURL     string
	backupDir       string
	dockerContainer string

	dbUser    string // DATABASE_URL から抽出（docker exec 用）
	dbName    string
	useDocker bool

	mu sync.Mutex // バックアップ/リストアの直列化

	tokenMu        sync.Mutex // アクセストークンキャッシュ用（mu とは独立。mu 保持中にも取得するため）
	accessToken    string
	accessTokenExp time.Time
}

func NewBackupService(db *sql.DB, settingsRepo *repository.AppSettingsRepository, drive *gdrive.Client, databaseURL, backupDir, dockerContainer string) *BackupService {
	s := &BackupService{
		db:              db,
		settingsRepo:    settingsRepo,
		drive:           drive,
		databaseURL:     databaseURL,
		backupDir:       backupDir,
		dockerContainer: dockerContainer,
	}
	if u, err := url.Parse(databaseURL); err == nil {
		s.dbUser = u.User.Username()
		s.dbName = strings.TrimPrefix(u.Path, "/")
	}
	if s.dbUser == "" {
		s.dbUser = "postgres"
	}
	if s.dbName == "" {
		s.dbName = "setori"
	}
	// ホストに pg_dump があれば直接実行、無ければ docker exec にフォールバック
	if _, err := exec.LookPath("pg_dump"); err != nil {
		s.useDocker = true
	}
	return s
}

// ========== 設定 ==========

// GetSettings は保存済み設定（無ければデフォルト）を返す。
func (s *BackupService) GetSettings() BackupSettings {
	settings := BackupSettings{
		IntervalHours:  24,
		RetentionLocal: 7,
		RetentionDrive: 14,
		DriveUpload:    true,
	}
	if _, err := s.settingsRepo.Get(settingsKeyBackup, &settings); err != nil {
		logger.Warnf("backup settings load: %v", err)
	}
	return settings
}

func (s *BackupService) saveSettings(settings BackupSettings) error {
	return s.settingsRepo.Set(settingsKeyBackup, settings)
}

// UpdateSettings は UI から変更可能な項目のみ上書き保存する。
func (s *BackupService) UpdateSettings(autoEnabled bool, intervalHours, retentionLocal, retentionDrive int, driveUpload bool) (BackupSettings, error) {
	clamp := func(v, min, max int) int {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	settings := s.GetSettings()
	settings.AutoEnabled = autoEnabled
	settings.IntervalHours = clamp(intervalHours, 1, 24*30)
	settings.RetentionLocal = clamp(retentionLocal, 1, 100)
	settings.RetentionDrive = clamp(retentionDrive, 1, 100)
	settings.DriveUpload = driveUpload
	if err := s.saveSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

// ========== バックアップ ==========

// CreateBackup はバックアップを即時実行する（Drive 連携中はアップロードも行う）。
func (s *BackupService) CreateBackup(trigger string) (*BackupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createBackupLocked(trigger)
}

func (s *BackupService) createBackupLocked(trigger string) (*BackupResult, error) {
	settings := s.GetSettings()

	name := fmt.Sprintf("setori_%s.dump", time.Now().Format("20060102_150405"))
	if trigger == "pre-restore" {
		name = fmt.Sprintf("setori_%s_prerestore.dump", time.Now().Format("20060102_150405"))
	}
	path, err := s.dumpToFile(name)
	if err != nil {
		now := time.Now()
		settings.LastBackupAt = &now
		settings.LastBackupStatus = "失敗: " + err.Error()
		_ = s.saveSettings(settings)
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	logger.Infof("backup created (%s): %s (%d bytes)", trigger, name, info.Size())

	result := &BackupResult{Name: name, Size: info.Size()}

	// Drive アップロード（リストア前の安全バックアップはローカルのみ）
	if trigger != "pre-restore" && settings.DriveUpload && s.DriveConnected() {
		if err := s.uploadToDrive(&settings, path, name); err != nil {
			logger.Warnf("backup drive upload failed: %v", err)
			result.DriveError = err.Error()
		} else {
			result.DriveUploaded = true
		}
	}

	// ローカル世代整理
	if err := s.pruneLocal(settings.RetentionLocal); err != nil {
		logger.Warnf("backup prune local: %v", err)
	}

	now := time.Now()
	settings = s.GetSettings() // Drive folder ID が更新されている可能性があるため再読込
	settings.LastBackupAt = &now
	if result.DriveError != "" {
		settings.LastBackupStatus = "成功（Drive アップロード失敗: " + result.DriveError + "）"
	} else {
		settings.LastBackupStatus = "成功"
	}
	if err := s.saveSettings(settings); err != nil {
		logger.Warnf("backup settings save: %v", err)
	}
	return result, nil
}

// dumpToFile は pg_dump（custom format）を実行し backupDir/name に書き出す。
func (s *BackupService) dumpToFile(name string) (string, error) {
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return "", fmt.Errorf("バックアップディレクトリ作成失敗: %w", err)
	}
	path := filepath.Join(s.backupDir, name)
	tmp := path + ".tmp"

	var cmd *exec.Cmd
	if s.useDocker {
		cmd = exec.Command("docker", "exec", s.dockerContainer, "pg_dump", "-U", s.dbUser, "-Fc", s.dbName)
	} else {
		cmd = exec.Command("pg_dump", "-Fc", "-d", s.databaseURL)
	}

	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	var stderr strings.Builder
	cmd.Stdout = out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("pg_dump 失敗: %v: %s", runErr, truncateStr(stderr.String(), 300))
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// uploadToDrive はバックアップファイルを Drive のバックアップフォルダへアップロードし、世代整理する。
func (s *BackupService) uploadToDrive(settings *BackupSettings, path, name string) error {
	token, err := s.getAccessToken()
	if err != nil {
		return err
	}
	// フォルダ確保（ID を設定に保存し、消えていたら作り直す）
	folderID := settings.DriveFolderID
	if folderID == "" {
		folder, err := s.drive.EnsureFolder(token, driveBackupFolderName)
		if err != nil {
			return err
		}
		folderID = folder.ID
		settings.DriveFolderID = folderID
		if err := s.saveSettings(*settings); err != nil {
			logger.Warnf("backup settings save (folder id): %v", err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := s.drive.Upload(token, folderID, name, f, info.Size()); err != nil {
		// フォルダが削除済みの場合は作り直して 1 回だけ再試行
		if strings.Contains(err.Error(), "File not found") || strings.Contains(err.Error(), "notFound") {
			folder, ferr := s.drive.EnsureFolder(token, driveBackupFolderName)
			if ferr != nil {
				return err
			}
			settings.DriveFolderID = folder.ID
			if serr := s.saveSettings(*settings); serr != nil {
				logger.Warnf("backup settings save (folder id): %v", serr)
			}
			if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
				return err2
			}
			if _, err2 := s.drive.Upload(token, folder.ID, name, f, info.Size()); err2 != nil {
				return err2
			}
			folderID = folder.ID
		} else {
			return err
		}
	}

	// Drive 側の世代整理
	files, err := s.drive.ListFiles(token, folderID)
	if err != nil {
		logger.Warnf("backup drive list for prune: %v", err)
		return nil
	}
	retention := settings.RetentionDrive
	if retention < 1 {
		retention = 1
	}
	for i, file := range files { // createdTime desc 順
		if i < retention {
			continue
		}
		if err := s.drive.DeleteFile(token, file.ID); err != nil {
			logger.Warnf("backup drive prune delete %s: %v", file.Name, err)
		} else {
			logger.Infof("backup drive pruned: %s", file.Name)
		}
	}
	return nil
}

// pruneLocal はローカルの .dump を新しい順に retention 件だけ残して削除する。
func (s *BackupService) pruneLocal(retention int) error {
	if retention < 1 {
		retention = 1
	}
	files, err := s.listLocal()
	if err != nil {
		return err
	}
	for i, f := range files {
		if i < retention {
			continue
		}
		if err := os.Remove(filepath.Join(s.backupDir, f.Name)); err != nil {
			logger.Warnf("backup prune remove %s: %v", f.Name, err)
		} else {
			logger.Infof("backup pruned: %s", f.Name)
		}
	}
	return nil
}

// ListLocal はローカルバックアップ一覧（新しい順）を返す。
func (s *BackupService) ListLocal() ([]BackupFileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocal()
}

func (s *BackupService) listLocal() ([]BackupFileInfo, error) {
	entries, err := os.ReadDir(s.backupDir)
	if os.IsNotExist(err) {
		return []BackupFileInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := []BackupFileInfo{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, BackupFileInfo{Name: e.Name(), Size: info.Size(), ModifiedAt: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModifiedAt.After(files[j].ModifiedAt) })
	return files, nil
}

// ValidateName はバックアップファイル名の形式を検証する（パストラバーサル防止）。
func (s *BackupService) ValidateName(name string) error {
	if !backupNameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("無効なバックアップファイル名")
	}
	return nil
}

// LocalPath は検証済みファイル名のフルパスを返す（存在確認付き）。
func (s *BackupService) LocalPath(name string) (string, error) {
	if err := s.ValidateName(name); err != nil {
		return "", err
	}
	path := filepath.Join(s.backupDir, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("バックアップファイルが見つかりません: %s", name)
	}
	return path, nil
}

// UploadExistingToDrive は既にあるローカルバックアップを Drive へ送る。
//
// 作成時の自動アップロードだけでは、次のものが永久にローカル止まりになる：
//   - Drive 連携より前に作ったもの
//   - `DriveUpload` を無効にしていた間に作ったもの
//   - **アップロードが失敗したもの**（作成自体は成功しているので再試行の口が無い。
//     一時的な通信エラーやトークン失効で、気付かないまま手元にしか無い状態になる）
//   - リストア前の安全バックアップ（作成時は意図的にローカルのみ）
//
// 同名のファイルが Drive にあっても弾かない。重複より「送れないこと」のほうが困る。
func (s *BackupService) UploadExistingToDrive(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.LocalPath(name)
	if err != nil {
		return err
	}
	if !s.DriveConnected() {
		return fmt.Errorf("Google Drive と連携していません")
	}

	settings := s.GetSettings()
	if err := s.uploadToDrive(&settings, path, name); err != nil {
		return err
	}
	// uploadToDrive がフォルダ ID を作り直している場合があるので取り込む。
	if err := s.saveSettings(settings); err != nil {
		logger.Warnf("backup settings save (after manual upload): %v", err)
	}
	logger.Infof("backup uploaded to drive (manual): %s", name)
	return nil
}

// DeleteLocal はローカルバックアップを削除する。
func (s *BackupService) DeleteLocal(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.LocalPath(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// ========== リストア ==========

// RestoreLocal はローカルバックアップからリストアする。
func (s *BackupService) RestoreLocal(name string) error {
	path, err := s.LocalPath(name)
	if err != nil {
		return err
	}
	return s.restore(path)
}

// RestoreFromReader はアップロードされたダンプをリストアする（一時ファイル経由）。
func (s *BackupService) RestoreFromReader(r io.Reader) error {
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.backupDir, "upload_*.dump.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fmt.Errorf("アップロードファイルの保存失敗: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return s.restore(tmpPath)
}

// RestoreFromDrive は Drive 上のバックアップをダウンロードしてリストアする。
func (s *BackupService) RestoreFromDrive(fileID string) error {
	token, err := s.getAccessToken()
	if err != nil {
		return err
	}
	body, err := s.drive.Download(token, fileID)
	if err != nil {
		return err
	}
	defer body.Close()
	return s.RestoreFromReader(body)
}

// restore は DB を dump ファイルの内容で置き換える。
// 手順：安全バックアップ → 他接続の切断 → public スキーマ再作成 → pg_restore → マイグレーション再実行。
// 古いバックアップを復元した場合もマイグレーションで最新スキーマに追いつく。
func (s *BackupService) restore(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 直前の状態をローカルに退避（失敗時のロールバック用）
	if _, err := s.createBackupLocked("pre-restore"); err != nil {
		return fmt.Errorf("リストア前の安全バックアップに失敗したため中止しました: %w", err)
	}

	// 他の接続を切断（pg_restore の DROP がロック待ちにならないように）
	if _, err := s.db.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`); err != nil {
		return fmt.Errorf("既存接続の切断失敗: %w", err)
	}

	// スキーマを空にしてから restore（現行スキーマの残骸で migration が失敗しないように）
	if _, err := s.db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return fmt.Errorf("スキーマ再作成失敗: %w", err)
	}

	var cmd *exec.Cmd
	if s.useDocker {
		cmd = exec.Command("docker", "exec", "-i", s.dockerContainer, "pg_restore", "-U", s.dbUser, "-d", s.dbName, "--no-owner", "--single-transaction")
	} else {
		cmd = exec.Command("pg_restore", "-d", s.databaseURL, "--no-owner", "--single-transaction")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var stderr strings.Builder
	cmd.Stdin = f
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore 失敗: %v: %s", err, truncateStr(stderr.String(), 300))
	}

	// 復元したダンプが古いスキーマでも、マイグレーションで最新に追いつかせる
	if err := database.RunMigrations(s.db); err != nil {
		return fmt.Errorf("リストア後のマイグレーション失敗: %w", err)
	}
	logger.Infof("database restored from %s", filepath.Base(path))
	return nil
}

// ========== Google Drive 連携 ==========

// DriveConfigured は OAuth クライアントが設定済みかを返す。
func (s *BackupService) DriveConfigured() bool {
	return s.drive.Configured()
}

// DriveConnected はアカウント連携済みかを返す。
func (s *BackupService) DriveConnected() bool {
	if !s.drive.Configured() {
		return false
	}
	var tok gdriveToken
	found, err := s.settingsRepo.Get(settingsKeyGDriveToken, &tok)
	return err == nil && found && tok.RefreshToken != ""
}

// DriveStatus は連携状態のサマリを返す。
func (s *BackupService) DriveStatus() DriveStatus {
	st := DriveStatus{Configured: s.drive.Configured()}
	if !st.Configured {
		return st
	}
	var tok gdriveToken
	if found, err := s.settingsRepo.Get(settingsKeyGDriveToken, &tok); err == nil && found && tok.RefreshToken != "" {
		st.Connected = true
		st.Email = tok.Email
		st.FolderName = driveBackupFolderName
	}
	return st
}

// StartDriveAuth はデバイスフローを開始する。
func (s *BackupService) StartDriveAuth() (*gdrive.DeviceAuth, error) {
	if !s.drive.Configured() {
		return nil, fmt.Errorf("GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET が未設定です")
	}
	return s.drive.StartDeviceAuth()
}

// PollDriveAuth はユーザー承認を確認し、承認済みならトークンを保存して連携を完了する。
// 未承認の間は (false, nil) を返す。
func (s *BackupService) PollDriveAuth(deviceCode string) (bool, error) {
	tok, err := s.drive.PollDeviceToken(deviceCode)
	if err == gdrive.ErrAuthPending {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tok.RefreshToken == "" {
		return false, fmt.Errorf("リフレッシュトークンが取得できませんでした")
	}

	saved := gdriveToken{RefreshToken: tok.RefreshToken}
	if user, err := s.drive.About(tok.AccessToken); err == nil {
		saved.Email = user.EmailAddress
	} else {
		logger.Warnf("gdrive about: %v", err)
	}
	if err := s.settingsRepo.Set(settingsKeyGDriveToken, saved); err != nil {
		return false, err
	}
	s.tokenMu.Lock()
	s.accessToken = tok.AccessToken
	s.accessTokenExp = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	s.tokenMu.Unlock()
	logger.Infof("google drive connected: %s", saved.Email)
	return true, nil
}

// DisconnectDrive は連携を解除する（トークン無効化 + 保存情報の削除）。
func (s *BackupService) DisconnectDrive() error {
	var tok gdriveToken
	if found, _ := s.settingsRepo.Get(settingsKeyGDriveToken, &tok); found && tok.RefreshToken != "" {
		if err := s.drive.Revoke(tok.RefreshToken); err != nil {
			logger.Warnf("gdrive revoke: %v", err)
		}
	}
	if err := s.settingsRepo.Delete(settingsKeyGDriveToken); err != nil {
		return err
	}
	s.tokenMu.Lock()
	s.accessToken = ""
	s.accessTokenExp = time.Time{}
	s.tokenMu.Unlock()
	// フォルダ ID は連携アカウントに紐づくためクリア
	settings := s.GetSettings()
	settings.DriveFolderID = ""
	return s.saveSettings(settings)
}

// ListDriveFiles は Drive 上のバックアップ一覧を返す。
func (s *BackupService) ListDriveFiles() ([]gdrive.File, error) {
	token, err := s.getAccessToken()
	if err != nil {
		return nil, err
	}
	settings := s.GetSettings()
	if settings.DriveFolderID == "" {
		return []gdrive.File{}, nil
	}
	return s.drive.ListFiles(token, settings.DriveFolderID)
}

// DeleteDriveFile は Drive 上のバックアップを削除する。
func (s *BackupService) DeleteDriveFile(fileID string) error {
	token, err := s.getAccessToken()
	if err != nil {
		return err
	}
	return s.drive.DeleteFile(token, fileID)
}

// getAccessToken はキャッシュ済みアクセストークンを返し、期限切れならリフレッシュする。
func (s *BackupService) getAccessToken() (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.accessToken != "" && time.Now().Before(s.accessTokenExp) {
		return s.accessToken, nil
	}

	var tok gdriveToken
	found, err := s.settingsRepo.Get(settingsKeyGDriveToken, &tok)
	if err != nil {
		return "", err
	}
	if !found || tok.RefreshToken == "" {
		return "", fmt.Errorf("Google Drive が未連携です")
	}
	refreshed, err := s.drive.RefreshAccessToken(tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("Google Drive トークン更新失敗: %w", err)
	}
	s.accessToken = refreshed.AccessToken
	s.accessTokenExp = time.Now().Add(time.Duration(refreshed.ExpiresIn-60) * time.Second)
	return refreshed.AccessToken, nil
}

// ========== スケジューラ ==========

// StartScheduler は自動バックアップの定期チェックを開始する（goroutine）。
func (s *BackupService) StartScheduler() {
	go func() {
		// 起動直後の負荷を避けて少し待ってから初回チェック
		time.Sleep(1 * time.Minute)
		s.autoBackupIfDue()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.autoBackupIfDue()
		}
	}()
}

func (s *BackupService) autoBackupIfDue() {
	settings := s.GetSettings()
	if !settings.AutoEnabled {
		return
	}
	interval := time.Duration(settings.IntervalHours) * time.Hour
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if settings.LastBackupAt != nil && time.Since(*settings.LastBackupAt) < interval {
		return
	}
	logger.Infof("auto backup starting (interval %s)", interval)
	if _, err := s.CreateBackup("auto"); err != nil {
		logger.Errorf("auto backup failed: %v", err)
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
