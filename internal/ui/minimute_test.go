// 播放条上「静音」那一枚的图形判据（票 #345 留下的：静音那张和响那张归一化之后
// 仍然分得开）。迷你窗里的音量 / 静音入口按原型（DESIGN.md §0′.T5）已经删掉，
// 这里只剩播放条那一半。
package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// miniMuteIconLabel 是播放条音量键**未静音**时的可及名字。
const miniMuteIconLabel = "静音"

// miniMuteIconLabelMuted 是它**静音**时的可及名字。
const miniMuteIconLabelMuted = "取消静音"

// sameShape 比两段形状串是不是同一张图（**按图形自身的包围盒归一化**后比）。
//
// ⚠️ **不能逐格比**：两枚的**尺寸不一样**（播放条那枚是 32 的命中区、迷你窗
// 那枚是 22），同一张 SVG 按比例缩放之后墨迹的**格数**就不一样了——逐格比
// 必然不等，而那不是「不同的图」。
//
// 所以先**裁到图形自己的包围盒**（去掉四周的空白），再缩到同一个格子数。
// 线宽差半个像素不会改轮廓（那不是另一张图，是同一张图的不同缩放），而
// 「喇叭带叉」和「喇叭带波纹」的轮廓**一定**不同。
func sameShape(a, b string) bool {
	na, nb := normalizeShape(a), normalizeShape(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb
}

// shapeGrid 是归一化之后的形状格子数（24×24）。
//
// 24 是「喇叭带叉」和「喇叭带波纹」这两张图都还分得开的最小格子数（实测
// 两者归一化之后的轮廓在这一个尺度上不同）。
const shapeGrid = 24

// normalizeShape 把形状串**裁到墨迹的包围盒**再缩到 shapeGrid × shapeGrid。
//
// ⚠️ **先裁再缩**：不裁的话，图形在框里的**位置**（播放条那枚的图形在 32 的
// 命中区里居中，迷你窗那枚在 22 的里面居中，比例一样但绝对偏移不同）会把缩放
// 后的格子错开。裁掉四周的空白之后，剩下的就是图形本身。
//
// ⚠️ **缩的是「哪些格子有墨迹」，不是墨迹的浓淡**：线宽差半个像素不该改判。
func normalizeShape(s string) string {
	rows := strings.Split(strings.TrimRight(s, "\n"), "\n")
	// 墨迹的包围盒。
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y, r := range rows {
		for x := 0; x < len(r); x++ {
			if r[x] != 'x' {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if minY < 0 || y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX < 0 {
		return ""
	}
	w, h := maxX-minX+1, maxY-minY+1

	var b strings.Builder
	for gy := 0; gy < shapeGrid; gy++ {
		for gx := 0; gx < shapeGrid; gx++ {
			// 目标格子对应源矩形里的那一小块：有一格墨迹就算有。
			x0 := minX + gx*w/shapeGrid
			x1 := minX + (gx+1)*w/shapeGrid
			y0 := minY + gy*h/shapeGrid
			y1 := minY + (gy+1)*h/shapeGrid
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			has := false
			for y := y0; y < y1 && !has; y++ {
				if y < 0 || y >= len(rows) {
					continue
				}
				row := rows[y]
				for x := x0; x < x1 && x < len(row); x++ {
					if row[x] == 'x' {
						has = true
						break
					}
				}
			}
			if has {
				b.WriteByte('x')
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// glyphInRect 量出 r 那一块里的图形（墨迹形状），底色用众数找。
//
// 找不到图形就返回空串（调用方据此报错）。
func glyphInRect(tt *mygoui.Tester, r mygoui.Rect) (int, string) {
	img := tt.Image()
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)

	counts := map[color.RGBA]int{}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
				continue
			}
			counts[img.RGBAAt(x, y)]++
		}
	}
	var bg color.RGBA
	best := 0
	for p, n := range counts {
		if n > best {
			bg, best = p, n
		}
	}

	var shape strings.Builder
	ink := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
				continue
			}
			if img.RGBAAt(x, y) == bg {
				shape.WriteByte('.')
				continue
			}
			ink++
			shape.WriteByte('x')
		}
		shape.WriteByte('\n')
	}
	return ink, shape.String()
}

// TestTheMuteGlyphDiffersFromTheLoudOne 钉住「归一化之后两张图仍然分得开」。
//
// ⚠️ **这是给上一条兜底的**：那条比的是「两扇窗静音时画的是同一张图」，而
// 这一条比的是「静音那张和响那张**不是**同一张图」。两条合起来才说明
// 归一化没有把不同的图判成一样的（否则上一条会**假绿**）。
func TestTheMuteGlyphDiffersFromTheLoudOne(t *testing.T) {
	bar := playBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.9, Muted: true}, 1200, false)
	muteRect, ok := volumeKeyOnTheBar(bar)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", bar.Texts())
	}
	_, muteShape := glyphInRect(bar, muteRect)

	loud := playBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.9}, 1200, false)
	loudRect, ok := loud.Find("静音")
	if !ok {
		t.Fatalf("播放条上找不到「静音」那一枚，文字有 %q", loud.Texts())
	}
	_, loudShape := glyphInRect(loud, loudRect)

	if sameShape(muteShape, loudShape) {
		t.Errorf("静音那张和响那张归一化之后一模一样——静音看不出来")
	}
}
