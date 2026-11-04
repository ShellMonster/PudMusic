// 票 #288：「全部播放」那一枚——贴到内容区右沿、尺寸 114×40、带一枚 18 的 ▶
// 图标（UX 报告 C-04 + C-05）。
//
// # 判据是**画面上的那一块绿**，不是控件的盒子
//
// 「贴到右沿」和「114×40」都是**画面上的事**：那一块绿从哪儿起、到哪儿止、
// 多宽多高。而框架给不了「按钮的盒子」——`Tester.Find` 找到的是**字**的盒子
// （`mygo/ui/runtime.go` 收 label 的地方：文字节点自己一条，按钮那条要另外
// `Label()`，而那会在无障碍树上多出一份同样的字，见 `flatbutton.go` 的
// 「别给它补 `Label(label)`」）。
//
// 所以这里量的是**主色那一块的像素**：它就是那一枚按钮（首页那一行只有它一个
// 主色实心块），量出来的框就是用户看见的那一块。
//
// # 上游的数是逐像素量出来的
//
// 上游那一枚（HeroUI 的 `Button size="md"`）实测 **x=1070..1183、y=66..105**，
// 也就是 114×40，**右沿 1184 = 1200 − 侧栏 200 − 内容区右边距 16**——和我们
// 内容区的右沿是**同一个数**（`pagePadX` = 16，七页共用的那一个）。
//
// ⚠️ **别拿固定毫秒当判据**（AGENTS.md §2）：这里量的是像素，机器慢一点也不影响。
package ui

import (
	"image"
	"image/color"
)

// 上游那一枚的实测值（UX 报告 C-05 的实图逐像素量出来的）。
const (
	// playAllWantW 是上游那一枚的宽：**114**。
	playAllWantW = 114
	// playAllWantH 是上游那一枚的高：**40**。
	playAllWantH = 40
	// playAllWantRight 是上游那一枚的右沿：**1184**。
	//
	// 那是 1200 宽的窗里内容区的右沿：1200 − 侧栏 200 − 内容区右边距 16。
	// 我们这边是同一个数（`pagePadX` = `space.PageHorizontal` = 16）。
	playAllWantRight = 1184
)

// playAllScanRegion 是找那一块绿时扫的那一片：内容区的**右上角**。
//
// ⚠️ **不能扫整个窗口**：主色在别处也有（主 Tab 底下那枚滑块、侧栏当前项的
// 字），扫全屏量出来的是它们的并集。
//
// ⚠️ **右边到窗口边、上面到顶栏下面**：那一块绿贴着内容区的右沿，所以扫描区
// 的右边必须是窗口的右边（不然会把绿块切掉一截，量出来的宽就不对了）。
func playAllScanRegion(img *image.RGBA) image.Rectangle {
	return image.Rect(img.Rect.Dx()-400, 55, img.Rect.Dx(), 140)
}

// accentBlock 找画面上「颜色和 accent 一样的那一片」的盒子。
//
// ⚠️ **判据是「和主色一样」**（带一点容差），不是「绿」——主色是**用户可设的**
// （设置里能换），写死「绿」的话用户换了主色这条就红。
func accentBlock(img *image.RGBA, accent color.RGBA, region image.Rectangle) (image.Rectangle, bool) {
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y := max(region.Min.Y, img.Rect.Min.Y); y < min(region.Max.Y, img.Rect.Max.Y); y++ {
		for x := max(region.Min.X, img.Rect.Min.X); x < min(region.Max.X, img.Rect.Max.X); x++ {
			if !sameColor(img.RGBAAt(x, y), accent) {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if maxX < 0 || x > maxX {
				maxX = x
			}
			if minY < 0 || y < minY {
				minY = y
			}
			if maxY < 0 || y > maxY {
				maxY = y
			}
		}
	}
	if minX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), true
}

// inkAgainst 找 region 里「和主色底差得远的那一片」的盒子：图标的颜色跟着主题走
// （深底配浅字、浅底配深字），所以不按「黑」判，按「离底色远」判。
func inkAgainst(img *image.RGBA, region image.Rectangle, bg color.RGBA) (image.Rectangle, bool) {
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y := max(region.Min.Y, img.Rect.Min.Y); y < min(region.Max.Y, img.Rect.Max.Y); y++ {
		for x := max(region.Min.X, img.Rect.Min.X); x < min(region.Max.X, img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(bg.R))+absInt(int(p.G)-int(bg.G))+absInt(int(p.B)-int(bg.B)) < 300 {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			maxX = max(maxX, x)
			if minY < 0 || y < minY {
				minY = y
			}
			maxY = max(maxY, y)
		}
	}
	if minX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), true
}

// darkInk 找 region 里「黑的那一片」的盒子（深色下页面底色也是黑的，所以这一条
// 只在**绿块里面**用）。
func darkInk(img *image.RGBA, region image.Rectangle) (image.Rectangle, bool) {
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y := max(region.Min.Y, img.Rect.Min.Y); y < min(region.Max.Y, img.Rect.Max.Y); y++ {
		for x := max(region.Min.X, img.Rect.Min.X); x < min(region.Max.X, img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R > 90 || p.G > 90 || p.B > 90 {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if maxX < 0 || x > maxX {
				maxX = x
			}
			if minY < 0 || y < minY {
				minY = y
			}
			if maxY < 0 || y > maxY {
				maxY = y
			}
		}
	}
	if minX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), true
}
