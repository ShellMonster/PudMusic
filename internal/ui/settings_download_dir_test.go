package ui

// 这一组盯的是**用户看到的下载目录是哪一条**。
//
// 设置里 `downloadPath` 的默认值是**空串**（`DefaultSettings("")`），而绝大多数
// 用户走的正是这一条——设置页上那一格显示成「未设置」，而歌其实落到了「音乐」
// 目录下的应用子目录里。用户看到的和实际发生的对不上，而这一格恰恰是回答「我
// 的歌下到哪儿了」的那一格。
//
// 出处：issue #361（默认落点从系统下载目录改成「音乐」目录下的应用子目录）。

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// withMusicDir 把「音乐」目录指到一个临时目录上。
//
// ⚠️ **测试不许往用户真的 ~/Music 里建东西**：默认下载目录是要建出来的，而建在
// 真的音乐目录里就是往用户的目录里塞一个文件夹。
func withMusicDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathMusic, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathMusic, "") })
	return dir
}

// defaultDownloadsDir 是这一组用的「默认落点」：音乐目录下的应用子目录。
func defaultDownloadsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(withMusicDir(t), platform.DownloadsDirName)
}

// withDownloadDir 把下载挂上，默认落点给一个。
//
// 走 `FallbackDir`（「设置里没选过时下到哪儿」那一条），而**不是**去改设置里的
// `DownloadPath`——这一组要验的正是「设置里没选过」那一条。
func withDownloadDir(fallback string) shellOpt {
	return mounted(func(t *testing.T, a *app.App) {
		if err := a.MountDownloads(app.Downloads{
			Media: http.DefaultClient, FallbackDir: fallback,
		}); err != nil {
			t.Fatalf("挂下载：%v", err)
		}
	})
}

// TestTheSettingsPageShowsWhereTheSongsWillLand 断设置页上「下载目录」那一格
// 显示的是**歌真正落下来的地方**，而不是一个空串。
//
// ⚠️ **反面的那一半也要断**：显示成「未设置」比显示错更坏——用户看到的是
// 「我还没设过下载目录」，于是他跑去设一个，而默认落点其实一直在生效。
//
// ⚠️ **判据是那一格拿到的路径**（`downloadDirShown`），不是画面上的字：只读的
// 输入框里那个值是**画上去的**，不进渲染树（`Tester.Texts()` 里没有它），
// 所以量画面上的字量不到它。
func TestTheSettingsPageShowsWhereTheSongsWillLand(t *testing.T) {
	want := defaultDownloadsDir(t)

	_, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), withDownloadDir(want))

	if got := s.downloadDirShown(s.app.SettingsSnapshot()); got != want {
		t.Errorf("设置页上「下载目录」那一格显示的是 %q，want 默认落点 %q", got, want)
	}
}

// TestTheSettingsPageStillShowsTheDirectoryTheUserPicked 断**手动选过的目录
// 照旧**：默认落点改了，但它只管「没选过」的那一条。
func TestTheSettingsPageStillShowsTheDirectoryTheUserPicked(t *testing.T) {
	fallback := defaultDownloadsDir(t)
	picked := filepath.Join(filepath.Dir(fallback), "我自己的")

	st := app.DefaultSettings(picked)
	_, s, _, _ := shellOnSettings(t, st, withDownloadDir(fallback))

	if got := s.downloadDirShown(s.app.SettingsSnapshot()); got != picked {
		t.Errorf("设置页上「下载目录」那一格显示的是 %q，want 用户选的那个 %q", got, picked)
	}
}

// TestTheDownloadPageShowsWhereTheSongsWillLand 断下载记录页页头上那条路径就是
// **歌真正落下来的地方**。
//
// ⚠️ 那一格（页头）是**画出来的字**，所以这里能真的量到——设置页那一格是只读
// 输入框，量不到（见上面那条的注释）。
//
// ⚠️ **反面的那一半也要断**：没选过设置时不能摆「还没有下载目录」——那个状态是
// 「连落盘的地方都没有」，而默认落点一直都在。
func TestTheDownloadPageShowsWhereTheSongsWillLand(t *testing.T) {
	want := defaultDownloadsDir(t)

	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountDownloads(app.Downloads{
		Media: http.DefaultClient, FallbackDir: want,
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	s := newDownloadShell(t, a)
	tt := ui.NewTester(s.View, dlWide, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)

	if !hasExactText(tt, want) {
		t.Errorf("下载记录页页头上没有写出下载目录 %q，文字有 %q", want, tt.Texts())
	}
	if tt.HasText(noDownloadDir.Title) {
		t.Errorf("下载记录页说「%s」，而默认落点 %q 一直都在", noDownloadDir.Title, want)
	}
}
