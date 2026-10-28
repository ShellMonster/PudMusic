package ui

// 加载态这一批的出图（2026-10-27）：设 BIU_SHOT_DIR 才存。
//
// 要看的是**「在等」这件事在浅色下看得见**：封面/头像/头图的加载档、
// 收藏夹页头的骨架、总览那三块的占位、歌词的加载态、榜单页头跟着滚。

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	mygoui "github.com/egoist/mygo/ui"
)

func TestRenderLoadingStateShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	// ① 网络图的四档并排（浅色）：没有地址 / 加载中 / 就绪 / 拉失败。
	//    这一张是这一批最要紧的一张——「加载中和没有地址分得开」全靠它。
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		b := newTestShell(t, at(560, 200), withView(func(s *Shell) func(c *mygoui.Context) {
			return func(c *mygoui.Context) {
				s.applyFrameTheme(c)
				mygoui.Row(c).Key("states").Gap(16).Padding(16).Children(func() {
					// 没有地址
					imgcache.View(c, s.images, "", 112, 112)
					// 加载中（不经过缓存）
					imgcache.Loading(c, 112, 112, imgcache.Shape{})
					// 头像那三档
					imgcache.Loading(c, 112, 112, imgcache.AvatarShape())
				})
			}
		}), systemIsDark(dark))
		b.tt.Frame()
		save(t, dir, "150-网络图三档-"+name+".png", b.tt)
		b.Close()
	}

	// ② 收藏夹页头：元信息还没回来（封面加载档 + 条目数不写 0 + 创建者留位）。
	b := newTestShell(t, append(blockedBiliOpts(t, true), at(1200, 800))...)
	b.s.navigate(collectionPathPrefix + "12345")
	b.tt.Frame()
	b.tt.Frame()
	save(t, dir, "151-收藏夹页头-加载中.png", b.tt)
	b.Close()

	// ③ 首页总览：新歌速递 / 排行榜那两块在拉时的骨架。
	startOnOverview(t)
	b2 := newTestShell(t, append(blockedBiliOpts(t, false), at(1200, 800))...)
	b2.tt.Frame()
	b2.tt.Frame()
	save(t, dir, "152-首页总览-加载中.png", b2.tt)
	b2.Close()

	// ④ 动态流：左栏作者列表在拉时的「正在加载关注的人…」。
	b3 := newTestShell(t, append(blockedBiliOpts(t, true), at(1200, 800))...)
	b3.s.navigate(dynamicFeedPath)
	b3.tt.Frame()
	b3.tt.Frame()
	b3.tt.Frame()
	save(t, dir, "153-动态流-作者栏加载中.png", b3.tt)
	b3.Close()

	// ⑤ 榜单页：页头（头图 + 表头）在列表的第 0 行。
	b4 := newTestShell(t, append(blockedBiliOpts(t, false), at(1200, 800))...)
	b4.s.navigate(homePath)
	b4.tt.Frame()
	b4.tt.Frame()
	save(t, dir, "154-首页-榜单页未进.png", b4.tt)
	b4.Close()
}
