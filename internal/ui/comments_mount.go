// 评论缓存的挂载：一个目录，一个缓存对象。
//
// # 必须在开窗口之前调
//
// `comments.NewCache` 要 `MkdirAll`——那是阻塞 IO，放在开窗口那条路上就是每启动
// 一次窗口白等一次（和歌词缓存那一摊同一条理由，见 `lyrics_mount.go`）。
//
// # 拿不到目录不算错误
//
// 和歌词缓存、搜索历史、快捷键同一套规矩：记一条日志，接着用一份**不落盘**的
// 缓存。`comments.NewCache("")` 什么都不做，于是评论照旧看得见（只是每次都要
// 重新问一遍），而为了一个可有可无的目录挡住开窗口不值当。
//
// # 目录挂在用户数据目录底下
//
// ⚠️ **不能写成 `filepath.Join(dir, commentsDirName)` 了事**：空目录时 `Join` 返回的
// 是**相对路径** `"comments"`（不是空串！），而 `comments.NewCache` 只把**空串**
// 当成「不落盘」。于是「拿不到数据目录」这一种情况下，缓存会在**进程的当前工作
// 目录**里建出 `comments/` 并真的写盘——应用数据没拿到，却往用户碰巧启动它的那个
// 目录里写了一堆缓存文件。（这一条照抄 `lyrics_mount.go` 的 `lyricsCacheDir`。）
package ui

import (
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/comments"
)

// MountComments 把评论缓存挂上。
func MountComments(a *app.App) {
	a.MountComments(comments.NewCache(commentsCacheDir(appDataDir())))
}

// commentsCacheDir 是评论缓存落在哪儿。**空目录进、空目录出**。
func commentsCacheDir(userDataDir string) string {
	if userDataDir == "" {
		return ""
	}
	return filepath.Join(userDataDir, app.CommentsCacheDirName)
}
