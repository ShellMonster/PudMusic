// 弹幕缓存的挂载。
//
// # 必须在开窗口之前调
//
// `danmaku.NewCache` 要 `MkdirAll`——那是阻塞 IO，放在开窗口那条路上就是每启动
// 一次窗口白等一次（理由和 `MountLyrics` 完全一样）。它也不发任何请求，所以
// 「早晚」无所谓，排在开窗口之前只是因为「所有零件都齐了」这件事在那儿看得见
// 一点。
//
// # 拿不到目录不算错误
//
// 和歌词缓存、搜索历史、快捷键同一套规矩：记一条日志，接着用一份**不落盘**的
// 缓存。弹幕照样从 B 站取（只是每次都要重新拉一遍），而为了一个可有可无的目录
// 挡住开窗口不值当。
//
// # 目录在应用数据目录底下的 `danmaku/`
//
// 和歌词缓存（`lyrics/`）平行。⚠️ **别写成 `filepath.Join(dir, "danmaku")` 了
// 事**：空目录时 `Join` 返回的是**相对路径** `"danmaku"`（不是空串！），而
// `danmaku.NewCache` 只把**空串**当成「不落盘」——于是「拿不到数据目录」这一种
// 情况下，缓存会在**进程的当前工作目录**里建出 `danmaku/` 并真的写盘。
// 理由和 `lyricsCacheDir` 那一条完全一样。
package ui

import (
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/danmaku"
)

// danmakuDirName 是弹幕缓存放在应用数据目录底下的那一个文件夹。
const danmakuDirName = "danmaku"

// MountDanmaku 把弹幕缓存挂上。
//
// ⚠️ **拿不到应用数据目录的时候照样挂**：缓存不落盘只是每次都要重新拉一遍，
// 而弹幕那一摊（`App.Danmaku`）压根不需要缓存才能工作。
func MountDanmaku(a *app.App) {
	a.MountDanmaku(danmaku.NewCache(danmakuCacheDir(appDataDir())))
}

// danmakuCacheDir 是弹幕缓存落在哪儿。**空目录进、空目录出**。
//
// ⚠️ **不能写成 `filepath.Join(dir, danmakuDirName)` 了事**：空目录时 `Join` 返回
// 的是**相对路径**（不是空串！），而 `danmaku.NewCache` 只把**空串**当成
// 「不落盘」。理由见文件头那一段。
func danmakuCacheDir(userDataDir string) string {
	if userDataDir == "" {
		return ""
	}
	return filepath.Join(userDataDir, danmakuDirName)
}
