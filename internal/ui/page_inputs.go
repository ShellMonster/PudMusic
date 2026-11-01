package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// captureFramePage takes **one** shared page seam snapshot for this frame.
//
// # 它为什么是「每帧一次」的那一份
//
// `pageInput()` 有一个每帧缓存：这一帧里所有页面读到的共享输入都是**同一份**
// （`Playback` / `Current` / `Account` / `FullPlayerPrefs` / `Images` /
// `DisplayView`）——而**取一次要采六个状态**（`app.Snapshot` / `CurrentItem` /
// `AccountSnapshot` / `FullPlayerPrefs` / `displayView`），每一个都是一次锁 +
// 一次值拷贝。
//
// ⇒ 所以这一份**每帧取一次**就够：取第二次（`pageInput()` 缓存没命中时的那条
// 兜底路）会把同一帧的六个状态**重新采一遍**——那是**纯浪费**，而且让「这一帧
// 花了多少」由「页面问了几次」决定。
//
// # ⚠️ **测量台也必须走这一条**（票 #721）
//
// 生产路上这一份是在 `Shell.View` 里取的（见 `shell_view.go`），而**测量台把
// `Shell.View` 整个换掉了**（`withView`）⇒ `framePage` 永远是空的 ⇒ `pageInput()`
// 的缓存**永远不命中** ⇒ **每一帧都重建整份 seam**。
//
// 实测：满屏播放器那一档**每帧重建 24 次**（生产是 1 次），于是那一档量到的
// 153 次里有约 114 次是**测量台自己造出来的**。
//
// ⇒ 所以这一句是**两条路共用的那一行**（生产在 `Shell.View` 里调它，测量台在
// `withView` 里也调它）——而测量台那一条**必须照抄生产的做法**，不然它量的
// 就不是「用户看见的那一屏」。
//
// # ⚠️ **App 被摘掉时不动这一份**
//
// 迷你外壳有一条测试会**临时把 App 摘掉**（`miniplayer_test.go` 的
// `s.app = nil`），而那时候 `s.pageSeam()` 会**空指针**——
//
// ⚠️ **而那一次失败要留在画的那一趟里面**（`Shell.View` 的注释）：
// 它是**崩溃兜底**要验的东西，不能被这一句挡在门外。所以 `s.app == nil` 时
// 这一句**什么都不做**（那一帧的 `pageInput()` 于是走兜底那条路，而那条路在
// `s.app == nil` 时同样会炸——**炸在画的那一趟里面**，正是要的样子）。
func (s *Shell) captureFramePage() {
	if s.app == nil {
		return
	}
	s.framePage = s.pageSeam()
}

func (s *Shell) pageInput() pageInput {
	if s.framePage.Input.Images != nil {
		return s.framePage.Input
	}
	return s.pageSeam().Input
}

func (s *Shell) pagePrefs() app.FullPlayerPrefs {
	return s.pageInput().FullPlayerPrefs
}

func (s *Shell) pageAccount() app.AccountSnapshot {
	return s.pageInput().Account
}

func (s *Shell) pageImages() *imgcache.Cache {
	return s.pageInput().Images
}

func (s *Shell) pageDisplayView() list.View {
	return s.pageInput().DisplayView
}
