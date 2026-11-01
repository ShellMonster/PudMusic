//go:build e2e

package main

import (
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"
	"github.com/egoist/mygo/ui"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// 列表那一行下面那条进度条的**无障碍**回归（票 #703 的 C 项）。
//
// # 为什么是「真窗口 + automation 树」
//
// 这一条断的是**读屏软件读得到的那一份**：那一行在无障碍树里是一个
// **值多少的进度条**，而且**有名字**。
//
// ⚠️ **无头 Tester 断不了这一层**：框架只在**辅助技术问起**时才描述那一帧
// （`mygo/ui/access.go` 的 `accessibilityOn`——真窗口里是系统的读屏软件问起，
// 无头窗口里没人问）。所以这里走 automation 插件的 `tree` 方法：它读的是
// **同一份无障碍树**（`mygo/ui/automation.go` 的 `AutomationTree`）。
//
// ⚠️ **而手搓那一版压根做不到**：两个 `ui.Box` 既没有角色也没有值——它们在
// 无障碍树里**根本不存在**（`accessRole` 对一个没标签、不可点的盒子返回 false）。
// 所以这一条是这一票真正的收益，也是它唯一的判据。

func main() {
	cache := imgcache.New(imgcache.Options{})
	cache.AddWake(func() {})
	defer cache.Close()

	l := list.New(cache, list.Actions{})
	l.SetSongs([]list.Music{{
		ID:       "a",
		Title:    "一首歌",
		Owner:    "甲 UP",
		Duration: 60 * time.Second,
		Progress: 30 * time.Second, // 一半
	}})

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "进度条无障碍 fixture",
			Width:   520,
			Height:  200,
			Content: ui.View(l.View),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
