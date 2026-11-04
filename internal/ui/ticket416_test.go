// 票 #416：UP 空间那一页的几张 Tab 的版式（间距 / 动态条目通栏 / 悬停棕带）。
//
// 用户 2026-10-25 说的三件事：
//
//  1. 「动态」两个字底下的第一条动态要**有间距**（原来一点都没有，紧贴着）；
//  2. 动态条目是**通栏**的（上游：通栏、只有一条下边框、透明底、直角）；
//  3. 指针停在一条动态上时，它底下**不再出现一条棕色的带**。
//
// # 判据为什么大半是像素
//
// 无头 Tester 的 `Find` 认的是「画着这句字**或者**标着这个名字」的元素，而这一页
// 上「动态」有**两个**（顶栏那枚按钮和 Tab 那一个），所以那一排 Tab 只能靠像素认
// （理由同 `pagerhythm_test.go` 顶上那一段）。
//
// # 上游那一档的出处
//
//   - Tab 的**内容面板**是 `px-0 py-4`（`user-profile/index.tsx:130` 那一层）：
//     上下各 16，左右那 16 由页面外层给。
//   - 动态条目是 `border-default-100 mb-2 w-full rounded-none border-b
//     bg-transparent pb-4`（`dynamic-feed/item.tsx:80`）+ CardHeader 的 `pt-4`：
//     通栏、只有一条下边框、透明底、直角，上下各 16，距下一条 8。
package ui

import (
	"math"
	"net/http"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// spaceDynamicText 是这一组测试里那条动态的正文。
//
// ⚠️ **它同时是列表那一行的无障碍名字**（`upRowLabel` 给的就是正文），而
// `Find` 认的就是「标着这个名字」的元素——所以 `Find` 给回来的是**整行**那个盒子
// （行排在它的子元素前面），而那一行里装的就是这一条动态。
const spaceDynamicText = "这一条动态的正文"

// spaceDynamicFace 是那条动态的头像地址。
//
// ⚠️ **头像地址是量位置用的**：`spaceAvatar` 把它挂在 Label 上，无头 Tester 靠它
// 找到那个盒子（空串的话头像照样画，但量不到）。
const spaceDynamicFace = "https://f/author-42.jpg"

// spaceDynamicShell 造一个 UP 空间，上面有一条动态（带头像和正文）。
//
// ⚠️ **深浅两套要分开造**：`systemIsDark` 是壳的一个选项，同一个壳上改不了。
func spaceDynamicShell(t *testing.T, b *uiExplore, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	uiSpaceData(b, "发动态的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicWithFace("d1", spaceDynamicText, spaceDynamicFace),
		}, "offset": "", "has_more": false})
	})
	s, tt := shotSpaceShell(t, b, dark, true, "77")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()
	return s, tt
}

// uiDynamicWithFace 是造一条带头像的动态。
func uiDynamicWithFace(id, text, face string) map[string]any {
	m := uiDynamic(id, text)
	mods := m["modules"].(map[string]any)
	mods["module_author"].(map[string]any)["face"] = face
	return m
}

// dynamicCardBox 是那一条动态**卡片**的盒子。
//
// ⚠️ **量的是「行」那个盒子**：列表给每一行挂了一个无障碍名字（就是这一条动态的
// 正文），而 `Find` 认的是「画着这句字**或者**标着这个名字」的元素——**行排在它
// 的子元素前面**，所以给回来的是整行。而那一行里装的就是这一条动态（卡片填满
// 整行），所以它就是卡片自己的盒子。
func dynamicCardBox(t *testing.T, tt *mygoui.Tester, text string) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("画面上找不到那一条动态（%q），文字有 %q", text, tt.Texts())
	}
	return r
}

// cardBottomRow 是卡片那个盒子**最下面那一行**的 y。
//
// ⚠️ **要向上取整再减一**：盒子的下沿常常落在两个像素之间（比如 470.63），
// 那一行是 470 而不是 469——那一行正是**下边框**所在的那一行。
func cardBottomRow(card mygoui.Rect) int {
	return int(math.Ceil(float64(card.Y+card.H))) - 1
}

// TestHoveringADynamicLeavesNoBrownBand 钉住「指针停在一条动态上时，底下不再出现
// 那条棕色的带」。
//
// # 那条带是怎么来的
//
// 框架给「指针压着的那一行」铺一层 `t.SurfaceHover`（`mygo/ui/list.go` 的
// styleFn：`w.bg = t.SurfaceHover`）。那一层铺的是**整行**的矩形，而**行比卡片
// 高**（差着卡片自己的下外边距）——于是卡片**底下**那一条露出了框架那一层。
// 暖纸主题下 `t.SurfaceHover` 是**淡棕**，用户看到的就是「莫名其妙的一层棕色
// 阴影」（2026-10-25）。
//
// # 判据是「两帧一模一样」
//
// 指针停在一条动态上，和指针停在别处，画面上**卡片底下那一段**必须一个像素都不差
// ——比「那个位置是什么颜色」稳：颜色要跟着主题和底色走，而「有没有变」不用。
//
// ⚠️ **不许靠把主题里那一档调淡蒙混过关**：调淡 `t.SurfaceHover` 会波及全应用
// （关注页的网格、收藏夹页、下载记录页…）。所以这一条量的是**那一段有没有变**。
func TestHoveringADynamicLeavesNoBrownBand(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			uiSpaceData(b, "发动态的 UP", 0, 1)
			b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"items": []any{
					uiDynamicWithFace("d1", spaceDynamicText, spaceDynamicFace),
					uiDynamicWithFace("d2", "第二条动态的正文", "https://f/author-43.jpg"),
				}, "offset": "", "has_more": false})
			})
			s, tt := shotSpaceShell(t, b, dark, true, "77")
			openSpace(t, s, tt, 42)
			sp := s.upSpaces[42]
			waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
			tt.Frame()

			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			card := dynamicCardBox(t, tt, spaceDynamicText)
			av, ok := tt.Find(spaceDynamicFace)
			if !ok {
				t.Fatalf("画面上找不到那个头像，文字有 %q", tt.Texts())
			}

			// ① 指针停在第一条动态上。
			tt.Move(av.X+av.W/2, av.Y+av.H/2)
			tt.Frame()
			hovered := tt.Image()

			// ② 指针停在播放条上（离内容区远远的）。
			tt.Move(600, 795)
			tt.Frame()
			away := tt.Image()

			// 卡片底下那一段（12 像素，从下边框**下面**那一行起）：两帧必须一模一样。
			bg := rgbaOf(th.Background)
			below := cardBottomRow(card) + 1
			for y := below; y < below+12; y++ {
				for x := int(card.X); x < int(card.X+card.W); x++ {
					if hovered.RGBAAt(x, y) != away.RGBAAt(x, y) {
						t.Fatalf("指针停在一条动态上，它底下那一段就变了（%d,%d：%v → %v）——"+
							"框架那一层行悬停底漏出来了", x, y, away.RGBAAt(x, y), hovered.RGBAAt(x, y))
					}
					if got := hovered.RGBAAt(x, y); got != bg {
						t.Fatalf("那条动态底下 (%d,%d) 是 %v，want 页面底色 %v——"+
							"指针停上去的时候底下不许出现一条棕色的带", x, y, got, bg)
					}
				}
			}
		})
	}
}

// tabFirstThing 是第 tab 张 Tab 的**第一样东西**的上沿。
//
// 三张 Tab 的第一样东西是**列表的第一行**（动态 / 收藏夹 / 合集），而列表给每一行
// 挂了一个无障碍名字（就是那一行的标题），`Find` 认的就是它——**行排在它的子元素
// 前面**，所以给回来的是**整行**那个盒子，而那一行的上沿就是面板内容的上沿。
//
// 「投稿」那一张不一样：它顶上第一样东西是「全部播放」那枚主色按钮，而那枚按钮
// 自己画字（`playAllButton` 不挂 Label），`Find` 给回来的是**字**的盒子，不是按钮
// 的盒子——按钮的上沿只能从像素上找。
func tabFirstThing(t *testing.T, tt *mygoui.Tester, s *Shell, th *mygoui.Theme, tab int, from float32) float32 {
	t.Helper()
	switch tab {
	case upTabDynamic:
		return dynamicCardBox(t, tt, spaceDynamicText).Y
	case upTabCollection:
		return dynamicCardBox(t, tt, "一个收藏夹").Y
	case upTabSeries:
		return dynamicCardBox(t, tt, "一个合集").Y
	}
	// 投稿：扫「全部播放」那枚按钮的上沿。
	//
	// ⚠️ **扫的是一段列，不是一列**：按钮两头是圆角，只扫一列量到的是圆角那一头
	// （比按钮的上沿低两三个像素）。扫一段再取最靠上的那一行，才是按钮的上沿。
	img := tt.Image()
	bg := rgbaOf(th.Background)
	x0 := int(sideTargetWidth(s.app.SettingsSnapshot())) + 16
	for y := int(from); y < img.Bounds().Dy(); y++ {
		for x := x0; x < x0+200; x++ {
			if img.RGBAAt(x, y) != bg {
				return float32(y)
			}
		}
	}
	t.Fatalf("「投稿」那一张 Tab 的工具条没画出来（从 y=%.0f 往下全是页面底色）", from)
	return 0
}
