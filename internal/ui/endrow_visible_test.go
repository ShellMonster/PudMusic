package ui

// 首页滚到底时，「没有更多了」那一行必须**整句都看得见**。
//
// # 这一条为什么必须存在（2026-10-29 的视觉走查）
//
// 出图看出来的：滚到最底那一屏，那一行只剩字形上半截（800 / 900 高的窗口上
// 实测墨迹 5 行，同一句字完整时是 11 行）。**再滚一次就变完整**——所以它不是
// 「播放条压住它」（播放条是内容区的兄弟，在下方），而是列表在**换成
// 「没有更多了」的那一帧**没重新吸附：框架每一帧从**上一帧**的布局出发，而末尾
// 那一行是这一帧才换的画法、高度和上一档（转圈）不一样。
//
// 判据按**像素**数墨迹行数：比「y 坐标小于某个数」稳——窗口高度、播放条高度、
// 列表内边距谁改都不会误报。
//
// ⚠️ **只滚一次**：多滚一次的话，那种「再滚一次就正常」的坏法会被盖住，
// 而用户滚到底看到的就是**第一屏**。

import (
	"image"
	"testing"
)

// inkRowCount 数某个区域里「有深色墨迹」的行数。
//
// 阈值 0.62 亮度：比它暗的算字（次要色在浅色底上约 0.45），比它亮的算底。
func inkRowCount(img *image.RGBA, x0, x1, y0, y1 int) int {
	b := img.Bounds()
	x0, y0 = max(x0, b.Min.X), max(y0, b.Min.Y)
	x1, y1 = min(x1, b.Max.X), min(y1, b.Max.Y)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			lum := (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
			if lum < 0.62 {
				n++
				break
			}
		}
	}
	return n
}

// TestTheEndOfTheListIsFullyVisibleOnTheFirstStop 钉住「滚到底那一屏，
// 那句『没有更多了』整句都在」。
func TestTheEndOfTheListIsFullyVisibleOnTheFirstStop(t *testing.T) {
	// 基准：窗口 1400 高时内容填不满，列表不用吸底，那一句一定完整可见。
	full := endLabelInkRows(t, 1400)
	if full < 8 {
		t.Fatalf("基准那一档只量到 %d 行墨迹——基准本身不成立，先看「没有更多了」是不是没画出来", full)
	}

	// 受测：800（默认）与 900 —— 两档都要吸底，正是那条坏法出现的地方。
	for _, winH := range []int{800, 900} {
		got := endLabelInkRows(t, winH)
		if got < full {
			t.Errorf("窗口 %d 高、刚滚到底那一屏「没有更多了」只有 %d 行墨迹，完整时应是 %d 行——"+
				"末尾那一行在换成「没有更多了」的那一帧没重新吸附，用户看到的是被切的那一屏",
				winH, got, full)
		}
	}
}

// endLabelInkRows 在指定窗口高度下滚到底，数量那一句字的墨迹行数。
//
// ⚠️ **只滚一次**（见文件头）：多滚一次会把要抓的那种坏法盖住。
func endLabelInkRows(t *testing.T, winH int) int {
	t.Helper()
	b := newUIExplore(t)
	homePagedArchives(b, 2)
	_, tt, _ := newHomeShell(t, b, at(homeW, winH))
	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})
	tt.Scroll(homeW/2, float32(winH)/2, 0, 100000)
	waitFrames(t, tt, "第二页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "2", 1))
	})
	tt.Scroll(homeW/2, float32(winH)/2, 0, 100000)
	waitFrames(t, tt, "说到头了", func() bool { return showsText(tt, "没有更多了") })

	r, ok := tt.Find("没有更多了")
	if !ok {
		t.Fatalf("窗口高 %d 时画面上没有「没有更多了」", winH)
	}
	// 量到内容区下沿为止：再往下是播放条，不是这一页的东西。
	bottom := winH - playbarHeight
	return inkRowCount(tt.Image(), int(r.X), int(r.X+r.W)+1, int(r.Y)-2, min(int(r.Y+r.H)+2, bottom))
}
