package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// withMusicDir 把「音乐」目录指到一个临时目录上。
//
// ⚠️ **测试不许往用户真的 ~/Music 里建东西**：默认下载目录是要建出来的，而建在
// 真的音乐目录里就是往用户的目录里塞一个文件夹。
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

// TestTheDefaultDownloadDirectoryIsCreatedIfItIsMissing 断目录不存在时**会建**。
//
// ⚠️ 不建的话第一次下载会失败，而失败是 `os.Create` 报的「no such file or
// directory」——用户看到的是一句他看不懂的话，实际原因只是「那个文件夹还没建」。
func TestTheDefaultDownloadDirectoryIsCreatedIfItIsMissing(t *testing.T) {
	music := withMusicDir(t)

	dir, err := EnsureDefaultDownloadsDir()
	if err != nil {
		t.Fatalf("准备默认下载目录：%v", err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("默认下载目录没建出来：%v", err)
	}
	if !fi.IsDir() {
		t.Errorf("%q 不是目录", dir)
	}
	if filepath.Dir(dir) != music {
		t.Errorf("建出来的 %q 不在音乐目录 %q 下面", dir, music)
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
// 建它就会在当前工作目录里造出一个 `Music` 文件夹，而歌会落到那儿（双击图标
// 启动的 macOS 应用那是 `/`），用户在 Finder 里看不见，也永远不会去那儿找
// （同一个坑见 `app.Downloads.FallbackDir` 的注释）。
//
// 所以那一条**退回系统下载目录**（它是绝对的，也是歌以前落的地方），而不是
// 就地建一个。
func TestTheDefaultDownloadDirectoryIsNotARelativePath(t *testing.T) {
	mygo.App.SetPath(mygo.PathMusic, "Music")
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathMusic, "") })

	// 系统下载目录也指到临时目录上：测试不许往用户真的 ~/Downloads 里写东西。
	dl := t.TempDir()
	mygo.App.SetPath(mygo.PathDownloads, dl)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathDownloads, "") })

	if _, err := DefaultDownloadsDir(); err == nil {
		t.Error("音乐目录是相对路径时不该给出一个下载目录")
	}
	got, err := EnsureDefaultDownloadsDir()
	if err != nil {
		t.Fatalf("音乐目录用不了时该退回系统下载目录：%v", err)
	}
	if got != dl {
		t.Errorf("退回的下载目录是 %q，want 系统下载目录 %q", got, dl)
	}
	// ⚠️ **别在当前工作目录里留下一个 Music 文件夹**：那正是这一条要挡的
	// 「歌落到一个诡异的目录里」。
	if _, err := os.Stat("Music"); err == nil {
		t.Error("在当前工作目录里建出了 Music——那正是要挡住的那件事")
	}
}
