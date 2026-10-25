// 动画换到 `AnimatePose` 那一档（票 #276）。
//
// # 框架只有一条性能分水岭
//
// 这一帧是**重建 view 树**还是**只重绘**（`mygo/ui/pose.go`）：
//
//	AnimateTo / AnimateWith / Animate   每一帧都要**重建 view + 布局**
//	                                      （值被 view 读，所以每帧都得重跑 view）
//	AnimatePose                          **只重绘**：元素自己在只画帧里动
//	                                      （框架原话：「the cheapest motion there is」）
//
// 判据只有一条：**换完之后那个动画值还会不会被 view 读**——会被读就还是每帧重建，
// 就白换。
//
// # 这一族量的是「建了几遍」，不是「花了几毫秒」
//
// 分配次数和建树次数都是**计数**，不受机器快慢影响（AGENTS.md §2：不要用固定帧数
// / 固定毫秒当判据）。
package ui

import (
	"testing"
	"time"
)

// TestTheFullPlayerBarsSlideWithoutRebuildingTheWindow 断「满屏那两条滑出去的时候
// 不重建界面」。
//
// # 为什么这一处要紧
//
// 「界面藏起来 / 回来」是最容易连着点的交互（指针一动就回来），而满屏开着的时候
// **重建一遍就是整棵满屏播放器重来一次**（实测整窗重建一帧 ≈ 97 次分配，
// 报告 BMG-264 §6.2）。
//
// # 判据要拿「指针动的那一帧」分开放
//
// 指针一动，界面回来——**那一帧**的重建是「指针动了」带来的，跟动画无关。
// 要数的是**它之后**的那一段：动画在跑的那 600 毫秒。
//
// ⚠️ **别拿「整段一共重建了几遍」当判据**：那一段里别的动画（歌词那一行）也在
// 重建界面，数字会被它们搅进来。
func TestTheFullPlayerBarsSlideWithoutRebuildingTheWindow(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()
	settleFull(t, tt)
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)

	// 界面藏起来了；指针一动，它回来（顶栏和底下那一条一起淡入 + 滑回来）。
	tt.Move(604, 404)
	tt.Frame()
	// ⚠️ **上面那一帧的重建是「指针动了」带来的**——从这儿开始数。
	s.builds = buildTally{}

	tt.Advance(600 * time.Millisecond)
	if got := s.builds.full; got != 0 {
		t.Errorf("满屏那两条滑回来的那一段重建了 %d 遍满屏播放器，want 0。\n"+
			"它们走的是 `AnimatePose` 那一档（只重绘帧，不重建 view）：\n"+
			"**淡出和位移都在那一个 pose 里**——只换不透明度的话，位移那一条还在 "+
			"view 里被读，每帧照样重建整棵满屏播放器（报告 BMG-264 §7 A1）。",
			got)
	}
}

// TestTheLikeBackgroundGrowsWithoutRebuildingThePlaybar 断「赞的那一圈长出来的时候
// 播放条没有重建」。
//
// # 为什么不能按那一键来量
//
// 按下去的话**图标层也会弹一下**，而图标层还在重建那一档（它的倍数要和「按下
// 缩放」相乘，两个数都要读——见 `like.go` 的 `likeButton`）——重建是**它**要的，
// 不是背景层要的。
//
// ⇒ 这里**直接把「赞没赞」翻过去**（`SetLike`），不按那一键：于是这一段里动的
// 只有背景层。
func TestTheLikeBackgroundGrowsWithoutRebuildingThePlaybar(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	e.tt.Frame()

	// 直接把「赞没赞」翻过去（**不按那一键**，理由见文件头）。
	e.s.app.SetLike(true, func(error) {})
	waitFrames(t, e.tt, "赞出去了", func() bool { return e.s.app.LikeState().Liked })

	// 状态落地那一帧是重建（状态变了），从这儿开始数。
	e.s.builds = buildTally{}
	e.tt.Advance(400 * time.Millisecond)
	if got := e.s.builds.playbar; got != 0 {
		t.Errorf("赞的那一圈长出来的那一段重建了 %d 遍播放条，want 0。\n"+
			"背景层走的是 `AnimatePose` 那一档（只重绘帧，不重建 view）：\n"+
			"**缩放和不透明度都在那一个 pose 里**——只换一半的话，另一半还在 view "+
			"里被读，每帧照样重建整个界面（报告 BMG-264 §7 A1）。", got)
	}
}
