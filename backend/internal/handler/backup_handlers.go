package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ========== Backup Handlers（要 backup:manage） ==========

// handleBackupStatus は設定・ローカル一覧・Drive 連携状態をまとめて返す。
func (r *Router) handleBackupStatus(w http.ResponseWriter, req *http.Request) {
	backups, err := r.backupService.ListLocal()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("バックアップ一覧の取得失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"settings": r.backupService.GetSettings(),
		"backups":  backups,
		"gdrive":   r.backupService.DriveStatus(),
	})
}

// handleUpdateBackupSettings は自動バックアップ設定を更新する。
func (r *Router) handleUpdateBackupSettings(w http.ResponseWriter, req *http.Request) {
	var body struct {
		AutoEnabled    bool `json:"auto_enabled"`
		IntervalHours  int  `json:"interval_hours"`
		RetentionLocal int  `json:"retention_local"`
		RetentionDrive int  `json:"retention_drive"`
		DriveUpload    bool `json:"drive_upload"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	settings, err := r.backupService.UpdateSettings(body.AutoEnabled, body.IntervalHours, body.RetentionLocal, body.RetentionDrive, body.DriveUpload)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("設定の保存失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

// handleCreateBackup はバックアップを即時実行する。
func (r *Router) handleCreateBackup(w http.ResponseWriter, req *http.Request) {
	result, err := r.backupService.CreateBackup("manual")
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("バックアップ失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleDownloadBackup はローカルバックアップをダウンロードさせる。
func (r *Router) handleDownloadBackup(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	path, err := r.backupService.LocalPath(name)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, req, path)
}

// handleDeleteBackup はローカルバックアップを削除する。
func (r *Router) handleDeleteBackup(w http.ResponseWriter, req *http.Request) {
	if err := r.backupService.DeleteLocal(req.PathValue("name")); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "削除しました"})
}

// handleRestoreBackup はローカルバックアップからリストアする。
func (r *Router) handleRestoreBackup(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	if err := r.backupService.RestoreLocal(name); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("リストア失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("%s からリストアしました", name)})
}

// handleRestoreUpload はアップロードされたダンプファイルからリストアする。
func (r *Router) handleRestoreUpload(w http.ResponseWriter, req *http.Request) {
	// ダンプは大きくなり得るためメモリ上限を抑えて一時ファイルに流す
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "ファイルのアップロードに失敗しました")
		return
	}
	file, _, err := req.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "file フィールドがありません")
		return
	}
	defer file.Close()
	if err := r.backupService.RestoreFromReader(file); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("リストア失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "アップロードしたファイルからリストアしました"})
}

// ========== Google Drive 連携 ==========

// handleGDriveAuthStart はデバイスフローを開始し、ユーザーに提示するコードを返す。
func (r *Router) handleGDriveAuthStart(w http.ResponseWriter, req *http.Request) {
	auth, err := r.backupService.StartDriveAuth()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, auth)
}

// handleGDriveAuthPoll は承認状況を確認する。承認済みなら連携完了。
func (r *Router) handleGDriveAuthPoll(w http.ResponseWriter, req *http.Request) {
	var body struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.DeviceCode == "" {
		respondError(w, http.StatusBadRequest, "device_code が必要です")
		return
	}
	done, err := r.backupService.PollDriveAuth(body.DeviceCode)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"connected": done,
		"gdrive":    r.backupService.DriveStatus(),
	})
}

// handleGDriveDisconnect は連携を解除する。
func (r *Router) handleGDriveDisconnect(w http.ResponseWriter, req *http.Request) {
	if err := r.backupService.DisconnectDrive(); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("連携解除失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "Google Drive 連携を解除しました"})
}

// handleGDriveListFiles は Drive 上のバックアップ一覧を返す。
func (r *Router) handleGDriveListFiles(w http.ResponseWriter, req *http.Request) {
	files, err := r.backupService.ListDriveFiles()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// handleGDriveDeleteFile は Drive 上のバックアップを削除する。
func (r *Router) handleGDriveDeleteFile(w http.ResponseWriter, req *http.Request) {
	if err := r.backupService.DeleteDriveFile(req.PathValue("id")); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "削除しました"})
}

// handleGDriveRestoreFile は Drive 上のバックアップからリストアする。
func (r *Router) handleGDriveRestoreFile(w http.ResponseWriter, req *http.Request) {
	if err := r.backupService.RestoreFromDrive(req.PathValue("id")); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("リストア失敗: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "Google Drive のバックアップからリストアしました"})
}
