package app_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 「一个进程有不止一个窗口」这一组。
//
// 迷你播放器是第二个窗口（`internal/ui/miniplayer.go`），两个窗口共用同一个 App。
// 铁律三要的是**逐窗口**通知：只叫醒主窗口，迷你窗口停在旧内容（实测：只投递不
// 唤醒两个窗口各 +0 帧；只唤醒主窗口则迷你窗口停在旧内容）。
//
// 机制在这里测（挨个叫醒的那一份），「每一扇窗真的接上了自己那一条」在
// `internal/ui` 的 `TestTheMiniWindowIsWokenWhenTheStateChanges`——两半合起来才是
// SPEC 需求 46。

// TestEveryRegisteredWindowIsWokenWhenTheStateChanges 钉「改一次状态叫醒所有窗口」。
//
// 这一条曾经是**一个**回调（`SetWake`）：第二个窗口登记的时候把第一个顶掉，于是
// 「在迷你窗口里按下一首，主窗口上的歌名不动」——而界面上看不出任何异样（它明明
// 有内容，只是旧的）。
//
// 量的是**每一条登记各自被调了几次**，而不是「有没有被调到」：漏掉哪一个的失败
// 症状正是「另一个界面停在旧帧」，而它在画面上和「画错了」长得一样。
func TestEveryRegisteredWindowIsWokenWhenTheStateChanges(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var main, mini, third atomic.Int32
	rm1 := a.AddWake(func() { main.Add(1) })
	rm2 := a.AddWake(func() { mini.Add(1) })
	rm3 := a.AddWake(func() { third.Add(1) })
	t.Cleanup(rm1)
	t.Cleanup(rm2)
	t.Cleanup(rm3)

	a.SetVolume(0.5)

	for _, c := range []struct {
		name  string
		count *atomic.Int32
	}{{"主窗口", &main}, {"迷你窗口", &mini}, {"第三扇窗", &third}} {
		if got := c.count.Load(); got != 1 {
			t.Errorf("%s被叫醒了 %d 次，want 1：改一次状态必须叫醒**每一扇**画着这份"+
				"状态的窗口，只叫醒一个的话另一个就停在旧帧（铁律三）", c.name, got)
		}
	}
}

// TestARemovedWindowStopsGettingWoken 盯「窗口关掉之后别再叫醒它」。
//
// 迷你窗口「进 / 出」来回十次之后那个列表里就有十条已经不存在的窗口，每改一次状
// 态就多走十趟空转。摘掉的那一条必须**真的**不再被叫醒——而摘的动作要能调任意次
// （关窗流程走了两遍不是什么怪事）。
func TestARemovedWindowStopsGettingWoken(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var main, mini atomic.Int32
	removeMain := a.AddWake(func() { main.Add(1) })
	removeMini := a.AddWake(func() { mini.Add(1) })
	t.Cleanup(removeMain)

	a.SetVolume(0.5)
	if mini.Load() != 1 || main.Load() != 1 {
		t.Fatalf("起点就不对：主窗口 %d 次、迷你窗口 %d 次，want 各 1", main.Load(), mini.Load())
	}

	// 迷你窗口关掉了。
	removeMini()
	removeMini() // 再调一次必须是空操作

	a.SetVolume(0.25)
	if got := mini.Load(); got != 1 {
		t.Errorf("已经关掉的那扇窗又被叫醒了 %d 次（want 1，只有第一次那一次）", got)
	}
	if got := main.Load(); got != 2 {
		t.Errorf("主窗口一共被叫醒了 %d 次，want 2：摘掉一个不该把别的也摘掉", got)
	}
}

// TestSetWakeStillReplacesWhatIsThere 盯 `SetWake` 的老规矩没被改掉。
//
// 它是「装一个」，而登记第二个窗口用 `AddWake`。绝大多数调用方（全部测试）一辈子
// 只有一扇窗，两个函数在那儿是同一件事，所以没有把 `SetWake` 删掉——这一条盯的
// 就是「它仍然是替换而不是追加」：写错了的话同一个 App 上会攒下一串回调，而
// `go test` 里那种「被叫醒的次数」会莫名其妙地涨。
func TestSetWakeStillReplacesWhatIsThere(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var first, second atomic.Int32
	a.SetWake(func() { first.Add(1) })
	a.SetWake(func() { second.Add(1) })

	a.SetVolume(0.5)
	if got := first.Load(); got != 0 {
		t.Errorf("被换掉的那一条还被叫醒了 %d 次，want 0", got)
	}
	if got := second.Load(); got != 1 {
		t.Errorf("后装的那一条被叫醒了 %d 次，want 1", got)
	}
}

// TestTheLocalLibraryWakesEveryWindowThatRegistered 盯本地音乐库那一路。
//
// 它和播放状态是**两个**入口通往后台（`AddLocalWake`），而两处是同一个道理，所以
// 都收成「挨个叫醒」。只改播放状态那一半的话，本地音乐扫完之后主窗口照样停在
// 「扫描中」——而界面上看不出任何异样。
func TestTheLocalLibraryWakesEveryWindowThatRegistered(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.MountLocalMusic(app.LocalMusic{})

	var main, mini atomic.Int32
	a.AddLocalWake(func() { main.Add(1) })
	removeMini := a.AddLocalWake(func() { mini.Add(1) })

	a.RescanLocalMusic()
	waitWakes(t, "主窗口", &main)
	waitWakes(t, "迷你窗口", &mini)

	// 迷你窗口关掉了：它那一条必须**摘掉**。
	//
	// 🔴 这一条曾经写的是「只增不减是刻意的，一个进程最多两个外壳」——那个理由是
	// 假的：迷你窗口是**进/退来回新建外壳**的（界面层的 `newMiniShell`），而这一层
	// 原来只 append 不摘，于是每进退一次就多留一条**属于已经收掉的**外壳的唤醒，
	// 扫一遍库就多走一趟空转。只有进程退出时 `App.Close` 才清空，那时已经太晚。
	removeMini()
	removeMini() // 再调一次必须是空操作（关窗流程走了两遍）

	before := mini.Load()
	a.RescanLocalMusic()
	waitWakes(t, "主窗口", &main)
	if got := mini.Load(); got != before {
		t.Errorf("已经摘掉的那一条又被叫醒了 %d 次（want %d，只有第一次那一次）", got, before)
	}
}

// waitWakes 等那一扇窗被叫醒（后台扫描是异步的，只能等）。
func waitWakes(t *testing.T, name string, n *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for n.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("等了五秒，%s 还没被叫醒：后台投递完了却没有挨个通知，"+
				"它停在旧帧（铁律三）", name)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
