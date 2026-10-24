package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// withMusicDir 把「音乐」目录指到一个临时目录上。
//
// ⚠️ **测试不许往用户真的 ~/Music 里建东西**：默认下载目录是用户自己的目录，
// 而这一层**只查路径**——可测试要是顺手建了它，那是在用户的磁盘上留下一个
// 文件夹。
//
// 收尾把这一格放回空串（`Path` 见到空串就走默认那一条），所以这一组跑完之后
// 别的测试拿到的还是系统真的那个音乐目录。
func withMusicDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathMusic, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathMusic, "") })
	return dir
}

// TestTheDefaultDownloadDirectoryIsTheAppFolderUnderTheMusicDirectory 断默认
// 落点是**「音乐」目录下的应用子目录**。
//
// ⚠️ **不是系统下载目录**：那儿是浏览器和安装包的地方，歌和那些东西混在一起，
// 用户在自己的下载目录里翻不到刚下的那几首（网易云、QQ 音乐、Spotify 都是
// 「音乐目录 + 以应用命名的子目录」）。
func TestTheDefaultDownloadDirectoryIsTheAppFolderUnderTheMusicDirectory(t *testing.T) {
	music := withMusicDir(t)

	got, err := DefaultDownloadsDir()
	if err != nil {
		t.Fatalf("算默认下载目录：%v", err)
	}
	want := filepath.Join(music, DownloadsDirName)
	if got != want {
		t.Errorf("默认下载目录是 %q，want %q", got, want)
	}
	if filepath.Dir(got) != music {
		t.Errorf("默认下载目录 %q 不在音乐目录 %q 下面", got, music)
	}
}

// TestTheDefaultDownloadDirectoryIsNotCreatedJustForLooking 断**这一层不建
// 目录**。
//
// ⚠️ 这一条是「测试不许往用户真的 ~/Music 里写东西」的那一半：这一层被
// `ui.downloadsDir`（开应用时）和设置页读着，而它一旦建目录，**光是打开应用
// 就会在用户的磁盘上留下一个文件夹**（测试里跑一次 `internal/ui` 也是一样）。
// 建目录是**落盘那一刻**的事（`service/download` 的 `ensureDir`）。
func TestTheDefaultDownloadDirectoryIsNotCreatedJustForLooking(t *testing.T) {
	music := withMusicDir(t)

	dir, err := DefaultDownloadsDir()
	if err != nil {
		t.Fatalf("算默认下载目录：%v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("只是查了一下路径，%q 却已经建出来了", dir)
	}
	// ⚠️ **音乐目录里面也不该多出那个子目录**：它就是上面那个 `dir`，
	// 这条断的是「查路径」这个动作本身没有碰文件系统。
	entries, err := os.ReadDir(music)
	if err != nil {
		t.Fatalf("读音乐目录：%v", err)
	}
	if len(entries) != 0 {
		t.Errorf("只是查了一下路径，音乐目录 %q 里却多了 %d 个东西", music, len(entries))
	}
}

// TestTheDownloadDirectoryIsNamedAfterTheAppNotAfterTheLegacyUserDataDirectory
// 断下载目录的子目录名是**显示名**，不是用户数据目录那个老名字。
//
// ⚠️ 这两个名字是两件互不相干的事（AGENTS.md §5）：
//
//   - `Biu MyGo` 是**用户数据目录**的名字，钉死在老名字上是为了老用户升级之后
//     设置、搜索历史、歌词缓存不丢（见 `datadir.go` 顶上那一节）。
//   - 下载目录是**新起**的，没有「老用户升级要保住什么」这个问题，跟着显示名走
//     正好。
//
// 把它们混起来的后果是：下载目录跟着「老名字」走，而应用叫 BiuGo——用户在
// Finder 里找「BiuGo」找不到那一摞歌。
func TestTheDownloadDirectoryIsNamedAfterTheAppNotAfterTheLegacyUserDataDirectory(t *testing.T) {
	if DownloadsDirName == LegacyUserDataDirName {
		t.Errorf("下载目录用了用户数据目录的名字 %q——那是「老数据在哪儿」，"+
			"跟「新下载落哪儿」是两件事", DownloadsDirName)
	}
	if DownloadsDirName != "BiuGo" {
		t.Errorf("下载目录的子目录名是 %q，want 显示名 BiuGo", DownloadsDirName)
	}
}

// TestTheDefaultDownloadDirectoryIsNotARelativePath 断拿不到家目录时**不**把歌
// 落到进程的当前工作目录里。
//
// ⚠️ 框架的 `PathMusic` 在拿不到家目录时给的是 `"Music"` 这样的相对路径——
// 那种落点会让歌落进进程的当前工作目录（双击图标启动的 macOS 应用那是 `/`），
// 用户在 Finder 里看不见，也永远不会去那儿找（同一个坑见
// `app.Downloads.FallbackDir` 的注释）。
//
// 所以那一条**退回系统下载目录**（它是绝对的，也是歌以前落的地方），而不是
// 就地给一个相对路径。
func TestTheDefaultDownloadDirectoryIsNotARelativePath(t *testing.T) {
	mygo.App.SetPath(mygo.PathMusic, "Music")
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathMusic, "") })

	// 系统下载目录也指到临时目录上：测试不许往用户真的 ~/Downloads 里写东西。
	dl := t.TempDir()
	mygo.App.SetPath(mygo.PathDownloads, dl)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathDownloads, "") })

	got, err := DefaultDownloadsDir()
	if err != nil {
		t.Fatalf("音乐目录用不了时该退回系统下载目录：%v", err)
	}
	if got != dl {
		t.Errorf("退回的下载目录是 %q，want 系统下载目录 %q", got, dl)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("退回的下载目录 %q 不是绝对路径", got)
	}
	// ⚠️ **别在当前工作目录里留下一个 Music 文件夹**：那正是这一条要挡的
	// 「歌落到一个诡异的目录里」。
	if _, err := os.Stat("Music"); err == nil {
		t.Error("在当前工作目录里建出了 Music——那正是要挡住的那件事")
	}
}
