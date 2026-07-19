package auth

// 権限キーの定義。ロールの permissions 配列にこれらを入れて制御する。
// PermAll ('*') を持つロールは全権限を通す。
const (
	PermAll         = "*"            // すべての権限（管理者）
	PermContentEdit = "content:edit" // 曲/歌枠/歌手/演奏/タグ/コメント解析などの編集
	PermSyncRun     = "sync:run"     // Holodex 同期の実行
	PermAIManage    = "ai:manage"    // AI プロバイダー設定
	PermLogsView    = "logs:view"    // ログ閲覧・ログレベル変更
	PermUsersManage = "users:manage" // ユーザー/ロール管理
	// DB バックアップ/リストア・Google Drive 連携
	PermBackupManage = "backup:manage"
)

// PermissionInfo は UI 表示用の権限メタ情報。
type PermissionInfo struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// AllPermissions は割り当て可能な権限の一覧（UI のロール編集で使用）。
// '*' は特別扱いなので一覧には含めるが、UI 側で「全権限」として扱う。
func AllPermissions() []PermissionInfo {
	return []PermissionInfo{
		{Key: PermAll, Description: "全権限（管理者）"},
		{Key: PermContentEdit, Description: "コンテンツ編集（曲・歌枠・歌手・演奏・タグ・コメント解析）"},
		{Key: PermSyncRun, Description: "Holodex 同期の実行"},
		{Key: PermAIManage, Description: "AI プロバイダー設定"},
		{Key: PermLogsView, Description: "ログ閲覧・レベル変更"},
		{Key: PermUsersManage, Description: "ユーザー・ロール管理"},
		{Key: PermBackupManage, Description: "DB バックアップ・リストア"},
	}
}

// HasPermission は permissions（ロールの権限セット）が need を満たすか判定する。
// '*' を含む場合は常に true。
func HasPermission(permissions []string, need string) bool {
	if need == "" {
		return true
	}
	for _, p := range permissions {
		if p == PermAll || p == need {
			return true
		}
	}
	return false
}
