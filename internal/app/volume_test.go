package app_test

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一组盯的是「音量三件套」（票 #311）：
//
//  1. 记全局音量——用户设的那一档落盘，重启之后还在。
//  2. 静音独立成布尔——静音的时候音量那一格不动，而且静音本身也落盘。
//  3. 「这一格不存在」和「这一格是 0」分得开。
//
// ⚠️ **第 2 条是最容易踩坑的那条**：静音一旦还是「音量 0」，而音量又开始落盘，
// 那么「静音 → 退出 → 重启」之后音量就永远是 0 了（Clementine 的老毛病）。
// 用户为了不吵把音量调到 20%，按了静音，第二天打开应用是**一点声都没有**。

// volumeApp 造一个挂着设置的 App：盘上的设置文件在 path 上。
func volumeApp(t *testing.T, path string) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestTheVolumeSurvivesARestart 验「用户设的音量落盘，重启之后还是那一档」。
//
// 这条治的是「每次启动音量跳回 100%」：用户为了不吵把音量调到 20%，重启之后
// 突然全速——耳机上那是真的惊吓。
func TestTheVolumeSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	first := volumeApp(t, path)
	first.SetVolume(0.2)
	if got := first.Snapshot().Volume; got != 0.2 {
		t.Fatalf("设完音量是 %v，want 0.2", got)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 重启。
	second := volumeApp(t, path)
	if got := second.Snapshot().Volume; got != 0.2 {
		t.Errorf("重启之后音量是 %v，want 0.2——它没有记住", got)
	}
}

// TestMuteIsItsOwnFlag 验「静音是它自己的一格」，而且它也落盘。
//
// 三件事：
//   - 静音的时候**音量那一格一动不动**（取消静音放回去的就是它）；
//   - 静音之后退出再启动，**音量不是 0**（那正是 Clementine 那个坑）；
//   - 静音本身重启之后还在（不然「静音」这一格存下来干什么）。
func TestMuteIsItsOwnFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	first := volumeApp(t, path)
	first.SetVolume(0.2)
	first.ToggleMute()

	st := first.Snapshot()
	if !st.Muted {
		t.Fatal("按了静音之后那一档不是静音")
	}
	if st.Volume != 0.2 {
		t.Errorf("静音之后音量变成了 %v，want 0.2——静音不该动音量那一格", st.Volume)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 重启。
	second := volumeApp(t, path)
	st = second.Snapshot()
	if st.Volume != 0.2 {
		t.Errorf("静音之后退出再启动，音量是 %v，want 0.2——静音把音量吃掉了", st.Volume)
	}
	if !st.Muted {
		t.Error("静音之后退出再启动，静音那一档没了")
	}
}

// TestUnmutingRestoresTheVolume 验「取消静音放回去的是用户设的那一档」。
func TestUnmutingRestoresTheVolume(t *testing.T) {
	a := volumeApp(t, filepath.Join(t.TempDir(), app.SettingsFileName))

	a.SetVolume(0.35)
	a.ToggleMute()
	if got := a.Snapshot().Volume; got != 0.35 {
		t.Fatalf("静音之后音量是 %v，want 0.35", got)
	}
	a.ToggleMute()
	st := a.Snapshot()
	if st.Muted {
		t.Error("再按一下还是静音的")
	}
	if st.Volume != 0.35 {
		t.Errorf("取消静音之后音量是 %v，want 0.35", st.Volume)
	}
}

// TestSettingANonZeroVolumeUnmutes 验「动了音量就是取消静音」。
//
// 用户按了音量加、滚滚轮、拖滑块，说的都是「我要出声」——静音着不动的话，那
// 一格就成了摆设（mpv 和 VLC 都是这样）。
func TestSettingANonZeroVolumeUnmutes(t *testing.T) {
	a := volumeApp(t, filepath.Join(t.TempDir(), app.SettingsFileName))

	a.SetVolume(0.4)
	a.ToggleMute()
	if !a.Snapshot().Muted {
		t.Fatal("没静音")
	}
	a.SetVolume(0.45)
	st := a.Snapshot()
	if st.Muted {
		t.Error("设了一个非零的音量之后还是静音的")
	}
	if st.Volume != 0.45 {
		t.Errorf("音量是 %v，want 0.45", st.Volume)
	}
}

// TestDraggingTheVolumeToZeroIsMute 验「把滑块拉到最下」就是静音。
//
// 音量 0 那一档在听感上和静音一样（图标也是同一张），所以它同时把静音那一格
// 按上——而落盘的音量仍然是 0，不是「没设过」。
func TestDraggingTheVolumeToZeroIsMute(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	a := volumeApp(t, path)
	a.SetVolume(0.4)
	a.SetVolume(0)
	st := a.Snapshot()
	if !st.Muted {
		t.Error("音量拉到 0 了还没静音")
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 重启：音量 0 是**用户自己拉的**，不是「没设过」。
	second := volumeApp(t, path)
	if got := second.Snapshot().Volume; got != 0 {
		t.Errorf("重启之后音量是 %v，want 0（用户自己拉到最下的）", got)
	}
}

// TestAnOldSettingsFileGetsTheDefaultVolume 验「老版本写的设置文件」读出来是
// 默认音量，**不是 0**。
//
// 老版本压根没有音量这一格（每一次启动都是 100%），所以那份文件里没有它。
// 而「这一格不存在」和「这一格是 0」在 JSON 里长得一模一样——分不开的话，
// 升级上来的用户打开应用是**一点声都没有**，而界面上不会说任何一个字。
func TestAnOldSettingsFileGetsTheDefaultVolume(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, `{"schemaVersion": 1, "appSettings": {"themeMode": "dark"}}`)

	a := volumeApp(t, path)
	if got := a.Snapshot().Volume; got != app.DefaultVolume {
		t.Errorf("老设置文件读出来的音量是 %v，want %v（默认，不是 0）",
			got, app.DefaultVolume)
	}
}

// TestAVolumeOfZeroIsNotTheSameAsNoVolume 验「用户自己拉到 0」和「这一格不存在」
// 是两回事。
func TestAVolumeOfZeroIsNotTheSameAsNoVolume(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, `{"schemaVersion": 1, "appSettings": {"volume": 0}}`)

	a := volumeApp(t, path)
	if got := a.Snapshot().Volume; got != 0 {
		t.Errorf("设置文件里写着音量 0，读出来是 %v，want 0", got)
	}
}

func TestThePlaybackRateSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	first := volumeApp(t, path)
	first.SetRate(1.5)
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	second := volumeApp(t, path)
	if got := second.Snapshot().Rate; got != 1.5 {
		t.Errorf("重启之后倍率是 %v，want 1.5", got)
	}
}
