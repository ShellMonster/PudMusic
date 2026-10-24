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
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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

// TestThePlayAllButtonSitsAtTheRightEdgeOfTheContentArea 钉住「那一块绿贴在
// 内容区右沿，尺寸 114×40」。
//
// # 为什么右沿要跟着窗口走
//
// 那一行是 `FillWidth()` 的，`Justify(SpaceBetween)` 把「全部播放」推到行的
// 右端——而行有多宽是**内容区**说了算（侧栏右边到播放条左边，再减两边的
// `pagePadX`）。所以这一条在两个窗口宽度上都断：写死一个 1184 的话，窗口一
// 窄它就悬在中间了。
//
// ⚠️ **深浅两套都要断**：这一枚是实心主色，深色下也是那一块绿——而深色下
// **字和图标都是黑的**（主色亮，`t.AccentText` 是黑），只出浅色的话深色下
// 「黑字压亮绿」这件事根本验不到。
func TestThePlayAllButtonSitsAtTheRightEdgeOfTheContentArea(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 1200 是上游出图的那一档（`homeW`），那一档的右沿要正好是**上游
			// 那一枚的右沿**（1184）；900 是窄一档，用来证明右沿是**跟着内容区
			// 走的**而不是写死的。
			//
			// 内容区的右沿就是**窗口的右沿**（侧栏在左边、播放条在下边，右边
			// 没有别的东西），页面的横向内边距 `pagePadX` 把它往里收 16。
			for _, tc := range []struct{ winW, wantRight int }{
				{homeW, playAllWantRight},
				{900, 884},
			} {
				winW, wantRight := tc.winW, tc.wantRight
				b := newTestShell(t, at(winW, homeH), systemIsDark(dark),
					withSettings(app.DefaultSettings("")))
				s, tt := b.s, b.tt
				// ⚠️ **得有歌**：没有歌时那一枚是**禁用**的，而框架对禁用控件是
				// 整个乘 0.5 不透明度——那时候量到的是「淡绿」，不是主色。
				s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: homeSongsOf("歌", 3)}})
				homeFrame(tt)

				th := BuildTheme(s.app.SettingsSnapshot(), dark)
				box, ok := accentBlock(tt.Image(), rgb(th.Accent), playAllScanRegion(tt.Image()))
				if !ok {
					t.Fatalf("画面上找不到主色那一块——「全部播放」那一枚不见了")
				}
				wantW := playAllWantW
				wantH := playAllWantH
				if got := box.Dx(); got != wantW {
					t.Errorf("那一块绿是 %d 宽，want %d（上游 `size=\"md\"` 的实测值）——"+
						"它是首页和每一页列表上唯一的主色实心块，宽度差一档用户一眼就看得出",
						got, wantW)
				}
				if got := box.Dy(); got != wantH {
					t.Errorf("那一块绿是 %d 高，want %d（上游 `h-10`）", got, wantH)
				}
				if got := box.Max.X; got != wantRight {
					t.Errorf("那一块绿的右沿是 x=%d，want %d（内容区的右沿）——"+
						"它悬在 Tab 右边半截的话，既不像「这一页的操作」也不像「全局操作」",
						got, wantRight)
				}
				b.Close()
			}
		})
	}
}

// TestThePlayAllButtonHasThePlayIcon 钉住「那一枚里有一枚 18 的 ▶ 图标」。
//
// # 判据是**绿块里那片黑**
//
// 上游是 `<RiPlayFill size={18} />`（`startContent`，也就是**图标在字前面**）。
// 那一枚图形是 `RiPlayFill`：24×24 的画布里那个三角只占 x=8..19.4、y=4.4..19.6，
// 于是画成 18 的时候**墨迹只有 8.5×11.4**——**不是 18**。
//
// ⚠️ **别拿「18」当墨迹的宽去断**：那 18 是**盒子**，墨迹是三角自己的形状。
// 断的是「盒子左边有一片黑」+「那片黑的宽在 7~10 之间」。
//
// ⚠️ **只看绿块里**：绿块外面也有黑的（深色下页面底色就是黑的），量出去是
// 整个窗口的黑。
func TestThePlayAllButtonHasThePlayIcon(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
			s, tt := b.s, b.tt
			s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: homeSongsOf("歌", 3)}})
			homeFrame(tt)

			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			img := tt.Image()
			box, ok := accentBlock(img, rgb(th.Accent), playAllScanRegion(img))
			if !ok {
				t.Fatalf("画面上找不到主色那一块——「全部播放」那一枚不见了")
			}
			// 图标盒子在绿块的左边：内边距 16（`px-4`），然后是 18 宽的图标。
			ink, ok := darkInk(img, image.Rect(
				box.Min.X+playAllPadX, box.Min.Y+2,
				box.Min.X+playAllPadX+playAllIconSize, box.Max.Y-2))
			if !ok {
				t.Fatalf("绿块左边那一格没有黑的——那枚 ▶ 图标不见了（上游是 "+
					"`<RiPlayFill size={18} />`）")
			}
			if ink.Dx() < 7 || ink.Dx() > 10 {
				t.Errorf("那枚三角是 %d 宽——`RiPlayFill` 画成 18 的时候墨迹只有 8~9"+
					"（三角在 24 的画布里只占 11.4），%d 宽说明画的不是那张图",
					ink.Dx(), ink.Dx())
			}
			// ⚠️ **图标在字前面**（上游的 `startContent`）：它得在绿块的**左边**，
			// 而字在它右边。字的位置从画面上量（`Find` 给的是字的盒子）。
			txt, ok := tt.Find(playAllLabel)
			if !ok {
				t.Fatalf("画面上找不到 %q，文字有 %q", playAllLabel, tt.Texts())
			}
			if ink.Max.X >= int(txt.X) {
				t.Errorf("那枚三角在 x=%d..%d，而字从 x=%.0f 开始——图标得在字的"+
					"**前面**（上游的 `startContent`）", ink.Min.X, ink.Max.X, txt.X)
			}
			b.Close()
		})
	}
}

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
