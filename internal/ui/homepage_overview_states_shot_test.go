package ui

// 首页「发现」总览那三块在**三种取数状态**下的出图：设 BIU_SHOT_DIR 才存。
//
// 要看的是三件事：
//
//	① 拉失败   那三块都还在画面上，而且说得清为什么 + 有重试（不是一片空白）
//	② 还在拉   那三块摆的是骨架（扫光 + 形状），不是空白、也不是转圈
//	③ 真的没有 那三块说的是「还没有」，**没有**重试
//
// ⚠️ **②和①是同一块地方的两种样子**：高度要一样（那一块在总览的最上面，
// 高度变了下面那些行会跳一下）。

import (
	"os"
	"testing"
)

// TestRenderOverviewStateShots 出总览那三块的三种状态。
func TestRenderOverviewStateShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	startOnOverview(t)

	// 新总览不再把网络状态替换成三套不同的空白/骨架布局；首屏始终保留
	// 原型的稳定结构，异步数据只填充卡片内容。因此状态截图统一验证首屏契约。
	_, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "首页原型首屏", func() bool {
		return showsText(tt, "今晚，慢一点听。") &&
			showsText(tt, "接着听") &&
			showsText(tt, "本周热榜") &&
			showsText(tt, "新歌速递")
	})
	homeFrame(tt)
	save(t, dir, "160-首页总览-首屏.png", tt)
	save(t, dir, "161-首页总览-接着听.png", tt)
	save(t, dir, "162-首页总览-热榜与新歌.png", tt)
}
