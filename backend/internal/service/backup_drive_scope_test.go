package service

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ruifan75/setori/pkg/gdrive"
)

// **世代整理は自分の印が付いたものだけを対象にする。**
//
// Drive のフォルダは名前で解決されるので、同じ Google アカウントなら手元も本番も
// 同じフォルダを共有する。しかも `drive_folder_id` と `drive_upload` は
// `app_settings` にあるので、本番を pg_dump で手元へ復元すると引き継がれる。
// 出所を見ずに「新しい順に N 件残して削除」すると、**手元が本番のバックアップを消す**
// （issue #64。実際 2026-09-13 に手元から 7 日ぶん上がっていた）。
func TestDriveObjectInstance(t *testing.T) {
	cases := []struct {
		name       string
		object     string
		wantTag    string
		wantTagged bool
	}{
		{"本番のもの", "production__setori_20260914_224202.dump", "production", true},
		{"手元のもの", "development__setori_20260914_224202.dump", "development", true},
		// **印が無いものは「自分のではない」側へ倒す。** 印を入れる前に上がった
		// ファイルが該当し、誰のものか分からないので消さない。
		{"印が無い（旧いファイル）", "setori_20260914_224202.dump", "", false},
		{"区切りが先頭にある", "__setori_20260914_224202.dump", "", false},
		{"印に使えない文字", "pro duction__setori.dump", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tag, tagged := driveObjectInstance(c.object)
			if tag != c.wantTag || tagged != c.wantTagged {
				t.Errorf("driveObjectInstance(%q) = (%q, %v), want (%q, %v)",
					c.object, tag, tagged, c.wantTag, c.wantTagged)
			}
		})
	}
}

// 印は**環境変数から取る**。`app_settings` に置くと pg_dump で複製され、
// 手元へ復元した瞬間に本番と同じ印を名乗る ── 原因そのものを繰り返す。
func TestBackupInstanceComesFromEnvironment(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	if got := backupInstance(); got != "production" {
		t.Errorf("backupInstance() = %q, want %q", got, "production")
	}

	// **空なら整理しない側へ倒す**ので、空を返すことが安全弁になっている。
	t.Setenv("ENVIRONMENT", "")
	if got := backupInstance(); got != "" {
		t.Errorf("未設定で %q を返した（空でないと整理が走ってしまう）", got)
	}

	// 名前に使えない値も空扱い（Drive 上の名前を読み戻せなくなるため）。
	t.Setenv("ENVIRONMENT", "pro duction")
	if got := backupInstance(); got != "" {
		t.Errorf("使えない値で %q を返した", got)
	}
}

// アップロード名は**印が付いているときだけ**変える。
// 付いていない環境で名前を変えると、印の無いファイルが増えるだけで害になる。
func TestDriveObjectName(t *testing.T) {
	if got := driveObjectName("production", "setori_x.dump"); got != "production__setori_x.dump" {
		t.Errorf("driveObjectName = %q", got)
	}
	if got := driveObjectName("", "setori_x.dump"); got != "setori_x.dump" {
		t.Errorf("印が無いのに名前を変えた: %q", got)
	}
}

// 往復：付けた名前から同じ印が読めること。**片方だけ直す改変を落とす。**
func TestDriveObjectNameRoundTrip(t *testing.T) {
	for _, instance := range []string{"production", "development", "staging-2"} {
		object := driveObjectName(instance, "setori_20260914_224202.dump")
		tag, tagged := driveObjectInstance(object)
		if !tagged || tag != instance {
			t.Errorf("%q: 付けた印を読み戻せない（%q, %v）", instance, tag, tagged)
		}
	}
}

func TestBackupInstanceUsesEnvironmentVariableName(t *testing.T) {
	// 変数名を変えると本番の設定が効かなくなるので固定する。
	os.Unsetenv("ENVIRONMENT")
	t.Setenv("ENVIRONMENT", "marker-check")
	if backupInstance() != "marker-check" {
		t.Error("ENVIRONMENT を読んでいない")
	}
}

// **削除の決定そのものを検査する。** 述語を個別に見ても、呼び出し側で
// 条件を落とせば守れない（PR #66 で同じ形の指摘を受けた）。
func TestDrivePruneTargets(t *testing.T) {
	// createdTime 降順。本番と手元が混ざった、実際に起きていた状態。
	files := []gdrive.File{
		{ID: "p5", Name: "production__setori_5.dump"},
		{ID: "d3", Name: "development__setori_3.dump"},
		{ID: "p4", Name: "production__setori_4.dump"},
		{ID: "old", Name: "setori_old.dump"}, // 印を入れる前のもの
		{ID: "p3", Name: "production__setori_3.dump"},
		{ID: "d2", Name: "development__setori_2.dump"},
		{ID: "p2", Name: "production__setori_2.dump"},
	}

	ids := func(fs []gdrive.File) []string {
		out := []string{}
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return out
	}
	eq := func(a []string, b ...string) bool {
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

	t.Run("本番は自分のぶんだけを古い順に落とす", func(t *testing.T) {
		got := ids(drivePruneTargets(files, "production", 2))
		if !eq(got, "p3", "p2") {
			t.Errorf("= %v, want [p3 p2]", got)
		}
	})

	// **他の環境のものを 1 つも含まないこと。** これがこの機能の目的。
	t.Run("他の環境のものには触らない", func(t *testing.T) {
		for _, f := range drivePruneTargets(files, "production", 1) {
			if tag, _ := driveObjectInstance(f.Name); tag != "production" {
				t.Errorf("他環境のファイルを削除対象にした: %s", f.Name)
			}
		}
	})

	// **印の無いファイルは誰のものか分からないので消さない。**
	t.Run("印の無いファイルには触らない", func(t *testing.T) {
		for _, f := range drivePruneTargets(files, "production", 1) {
			if f.ID == "old" {
				t.Error("印の無いファイルを削除対象にした")
			}
		}
	})

	// **安全弁**：印が無ければ 1 つも消さない。
	t.Run("印が無ければ何も消さない", func(t *testing.T) {
		if got := drivePruneTargets(files, "", 1); len(got) != 0 {
			t.Errorf("ENVIRONMENT が空なのに %v を消そうとした", ids(got))
		}
	})

	// 保持数は**自分のぶんで数える**。全体で数えると、他環境のファイルが多いだけで
	// 自分のバックアップが保持数を満たさないまま消える。
	t.Run("保持数は自分のぶんで数える", func(t *testing.T) {
		if got := drivePruneTargets(files, "production", 4); len(got) != 0 {
			t.Errorf("自分のぶんは 4 件なのに %v を消そうとした", ids(got))
		}
		if got := ids(drivePruneTargets(files, "development", 1)); !eq(got, "d2") {
			t.Errorf("= %v, want [d2]", got)
		}
	})
}

// **印は保存できる場所に置かない。** `BackupSettings` に入れると
// `saveSettings` が `app_settings` へ書き、pg_dump で手元へ複製される
// ── 復元した手元が本番の印を名乗るのは、この機能が防ごうとしているもの。
//
// **構造体を反射で見る。** JSON を marshal して確かめる形では、`omitempty` が
// 空の欄を落とすので**欄を足しても気付けない**（実際そう書いて負のコントロールが
// 通ってしまった）。
func TestBackupSettingsCannotPersistInstance(t *testing.T) {
	typ := reflect.TypeOf(BackupSettings{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.ToLower(f.Name)
		tag := strings.ToLower(strings.Split(f.Tag.Get("json"), ",")[0])
		for _, bad := range []string{"instance", "environment"} {
			if strings.Contains(name, bad) || strings.Contains(tag, bad) {
				t.Errorf("BackupSettings に %q（tag %q）がある ── 保存され、pg_dump で複製される。"+
					"環境の印は BackupService.Instance() から返すこと", f.Name, tag)
			}
		}
	}
}

// **印に区切り文字を入れられないこと。**
//
// 区切りは `__` なので、印に `_` が入ると読み戻せない：
// `production__canary` が作った `production__canary__setori_1.dump` は最初の `__`
// で切ると `production` になり、**production の世代整理が別環境のファイルを消す**。
// 末尾が `_` の `production_` も `production___setori_1.dump` → `production` で同じ。
//
// 区切りに使う文字を印の文字種から外すことで、この誤読を原理的に起こせなくする。
func TestInstanceCannotContainSeparator(t *testing.T) {
	for _, bad := range []string{
		"production__canary", // 区切りをそのまま含む
		"production_",        // 末尾の _ が区切りの一部になる
		"_production",        // 先頭の _ も同じ
		"pro duction",        // 名前に使えない文字
		"",
	} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", bad)
			if got := backupInstance(); got != "" {
				t.Errorf("backupInstance() = %q（印として使えない値を通した）", got)
			}
			// **付ける側でも弾く。** 呼び出し側頼みにすると、別の呼び出しが
			// 足された瞬間に読み戻せない名前を作れる。
			if got := driveObjectName(bad, "setori_1.dump"); got != "setori_1.dump" {
				t.Errorf("driveObjectName(%q, …) = %q（読み戻せない名前を作った）", bad, got)
			}
		})
	}

	// 陽性対照：使える値では往復すること。
	for _, ok := range []string{"production", "development", "staging-2", "prod.1"} {
		obj := driveObjectName(ok, "setori_1.dump")
		tag, tagged := driveObjectInstance(obj)
		if !tagged || tag != ok {
			t.Errorf("%q: 往復できない（%q, %v）", ok, tag, tagged)
		}
	}
}

// **別環境の印を持つファイルが、自分の整理で消えないこと**を名前の形から確かめる。
// 上の誤読が起きると、この検査が落ちる。
func TestPruneNeverTouchesOtherInstance(t *testing.T) {
	files := []gdrive.File{
		{ID: "mine1", Name: driveObjectName("production", "setori_1.dump")},
		{ID: "mine2", Name: driveObjectName("production", "setori_2.dump")},
		{ID: "other", Name: "production__canary__setori_3.dump"}, // 手で置かれた紛らわしい名前
	}
	for _, f := range drivePruneTargets(files, "production", 1) {
		if f.ID == "other" {
			t.Error("別環境の印を持つファイルを削除対象にした")
		}
	}
}
