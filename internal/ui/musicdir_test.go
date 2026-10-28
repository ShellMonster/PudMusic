package ui

// 这一包测试的「音乐」目录：**整个包都指到一个临时目录上**。
//
// ⚠️ **为什么是整个包，而不是每条测试各自替**：默认下载目录是「音乐」目录下
// 的应用子目录（`~/Music/BiuGo`），而那是**用户自己的目录**。这一包里任何
// 一条测试只要碰了它——哪怕只是「查一下路径」——就是在用户的磁盘上留下一个
// 文件夹。
//
// （票 #361 的评审就是在这儿抓到的一次：测试不能把真实用户目录作为临时目录。
// `~/Music/BiuGo` 自己长出来了。）
//
// 所以这里在 `TestMain` 里把**整个包**的「音乐」目录换掉——跟 `withUserDataDir`
// 为「用户数据目录」做的是同一件事（见 `settings_tab_config_test.go`）。

import (
	"fmt"
	"os"

	"github.com/egoist/mygo"
)

// pkgMusicDir 是这一包测试用的「音乐」目录（`TestMain` 里定的）。
//
// ⚠️ **每条测试把它替成自己的临时目录之后要回到这里**，而不是回到空串
// （空串 = 框架走默认那一条 = 用户真的 `~/Music`）。
var pkgMusicDir string

// useTempMusicDir 把这一包的「音乐」目录指到一个临时目录上，返回那个目录。
//
// 收尾是调用方的事（`TestMain` 结束时删掉它）——`os.Exit` 不走 defer。
func useTempMusicDir() (string, error) {
	dir, err := os.MkdirTemp("", "biu-music-test")
	if err != nil {
		return "", err
	}
	mygo.App.SetPath(mygo.PathMusic, dir)
	pkgMusicDir = dir
	return dir, nil
}

// dropTempMusicDir 把这一包的「音乐」目录放回默认那一条，并删掉临时目录。
func dropTempMusicDir(dir string) {
	mygo.App.SetPath(mygo.PathMusic, "")
	pkgMusicDir = ""
	_ = os.RemoveAll(dir)
}

// reportTempMusicDirFailure 把「拿不到临时的音乐目录」这件事说清楚。
//
// ⚠️ **它不许被当成「跑过了」**：这一包要是接着跑下去，任何一条碰了默认
// 下载目录的测试都会在**用户的磁盘上**留下一个文件夹（那正是这一条要挡的
// 事）。所以这一条是**退出码非零**，跟 `diskguard_test.go` 那道护栏一个待遇。
func reportTempMusicDirFailure(err error) {
	fmt.Fprintf(os.Stderr, "biu: 这一包的测试拿不到临时的「音乐」目录，"+
		"跑下去会在用户自己的 ~/Music 里留下文件夹，所以一条都不跑：%v\n", err)
}
