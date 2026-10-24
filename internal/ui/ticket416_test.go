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

// spaceTabTrack 扫出 UP 空间那一排 Tab 的**灰底轨道**（上下两条边）。
//
// ⚠️ **扫的是「投稿」那一列**：这一页的「动态」有两个（顶栏那枚按钮 + Tab 那一个），
// 而选中的那一块（动态）是**第一块**——滑块压在它底下，那一列扫不出轨道色。拿
// 第二块（「投稿」）那一列就躲开它了：轨道色在轨道的上下各 4 像素（`tabs.go` 的
// `mainTabs`：轨道 `p-1` + 滑块 `h-8`）。
//
// ⚠️ **扫的是「围着标签那一段连续」的轨道色**，不是「这一列上所有轨道色的行」：
// 动态卡片里那个视频块也是 `t.Surface`（经典主题下和轨道色同一个值），它要是
// 也被算进去，轨道下边就会量到那个块的底边去。
//
// ⚠️ **这一组量的时候选中的是「动态」那一块**：滑块在第一块底下，选中第二块的话
// 那一列扫到的是滑块（页面底色），量不出轨道。
//
// 返回的是轨道的**上下边**（不是左右）：左右那一头带着圆角，扫不准，而这一组
// 判据要的是「Tab 那一条的底边在哪儿」。
func spaceTabTrack(t *testing.T, tt *mygoui.Tester, th *mygoui.Theme) (top, bottom float32) {
	t.Helper()
	img := tt.Image()
	track := rgbaOf(trackColor(th))
	label, ok := tt.Find(upTabNames[1]) // 「投稿」
	if !ok || label.W <= 0 {
		t.Fatalf("画面上找不到 Tab「%s」，文字有 %q", upTabNames[1], tt.Texts())
	}
	cx := int(label.X + label.W/2)
	// 上边：从标签那行字**上面**那一行往上，连续的轨道色。
	lo := -1
	for y := int(label.Y) - 1; y >= 0; y-- {
		if img.RGBAAt(cx, y) != track {
			break
		}
		lo = y
	}
	// 下边：从标签那行字**下面**那一行往下，连续的轨道色。
	hi := -1
	for y := int(label.Y+label.H) + 1; y < img.Bounds().Dy(); y++ {
		if img.RGBAAt(cx, y) != track {
			break
		}
		hi = y
	}
	if lo < 0 || hi < 0 {
		t.Fatalf("「%s」那一列的轨道色不连成一段（%d..%d）——灰底轨道没画出来"+
			"（文字有 %q）", upTabNames[1], lo, hi, tt.Texts())
	}
	return float32(lo), float32(hi + 1)
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

// TestTheDynamicTabLeavesAGapBelowTheTabBar 钉住「Tab 标题和第一条动态之间有间距」。
//
// # 那个间距是 16
//
// 上游 UP 空间那个 Tab 的**内容面板**是 `px-0 py-4`：上下各 16。原来我们这一层
// 一点上下都没给，于是「动态」两个字底下一张卡片就顶上来（用户 2026-10-25 说的
// 「好像有些间距没控制好」）。
//
// ⚠️ **量的是「Tab 那一条的底边」到「第一条动态的上沿」**：中间不许有别的东西
// 顶替（那一段就是面板自己的 16）。
func TestTheDynamicTabLeavesAGapBelowTheTabBar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			s, tt := spaceDynamicShell(t, b, dark)
			th := BuildTheme(s.app.SettingsSnapshot(), dark)

			trackTop, trackBottom := spaceTabTrack(t, tt, th)
			card := dynamicCardBox(t, tt, spaceDynamicText)

			gap := card.Y - trackBottom
			if gap < 15 || gap > 17 {
				t.Errorf("Tab 那一条的底边在 %.0f，第一条动态的上沿在 %.0f，中间只有 %.1f，"+
					"want 16（上游的内容面板是 `px-0 py-4`）——「动态」两个字底下不许"+
					"紧贴着一张卡片", trackBottom, card.Y, gap)
			}
			// 轨道自己的高度也钉一下（上游 md 的 tab 是 h-8 + 上下各 4）。
			if got, want := trackBottom-trackTop, tabMD.track; got != want {
				t.Errorf("Tab 那一条高 %.0f，want %.0f", got, want)
			}
		})
	}
}

// TestTheDynamicItemIsFullWidthWithOneBottomBorder 钉住「动态条目是通栏的」。
//
// # 三件事
//
//  1. **左右不各缩进去一块**：卡片的内容（头像）就在卡片的左缘上，而卡片自己的
//     左缘离内容区左缘 16（上游的 `p-4`）、离窗口右缘也是 16。
//  2. **只有一条下边框**：上边、左边、右边都没有描边。
//  3. **直角**：那一条下边框是**横贯整条**的一行 1 像素，不是两头圆角的短段。
//
// # 判据为什么是像素
//
// 「有没有描边」「描边在哪一边」在画面上才看得见；而 `Find` 拿不到卡片自己的盒子
// 那一层描边（它给的是**行**那个盒子，行没有描边）。
func TestTheDynamicItemIsFullWidthWithOneBottomBorder(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newUIExplore(t)
			s, tt := spaceDynamicShell(t, b, dark)
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			img := tt.Image()

			card := dynamicCardBox(t, tt, spaceDynamicText)

			// ① 左右：内容离内容区左缘 16（上游的 `p-4`），离窗口右缘也是 16。
			wantLeft := float32(sideTargetWidth(s.app.SettingsSnapshot())) + 16
			if card.X != wantLeft {
				t.Errorf("那一条动态从 x=%.0f 开始，want %.0f（侧栏 %.0f + 面板的 16）——"+
					"上游那一档内容离内容区左缘是 16", card.X, wantLeft,
					sideTargetWidth(s.app.SettingsSnapshot()))
			}
			wantRight := float32(testWinW) - 16
			if card.X+card.W != wantRight {
				t.Errorf("那一条动态到 x=%.0f 结束，want %.0f（窗口 %d - 面板的 16）",
					card.X+card.W, wantRight, testWinW)
			}
			// 头像就在卡片的左缘上（卡片自己左右没有再缩进去一块）。
			av, ok := tt.Find(spaceDynamicFace)
			if !ok {
				t.Fatalf("画面上找不到那个头像，文字有 %q", tt.Texts())
			}
			if av.X != card.X {
				t.Errorf("头像在 x=%.0f，卡片在 x=%.0f——卡片自己左右又缩进去了一块，"+
					"上游那一档是通栏的（左右 0）", av.X, card.X)
			}

			// ② 描边：只有下边那一条。
			//
			// ⚠️ **描边那一色是「描边叠在页面底色上」**：方案那一档的 `t.Border` 是
			// 半透明的（文字色 @ 12%），画出来是它叠在底色上的结果。
			border := rgbaOf(th.Border.Over(th.Background))
			bottom := cardBottomRow(card)
			if got := img.RGBAAt(int(card.X), bottom); got != border {
				t.Errorf("卡片下边那一行是 %v，want %v（`t.Border` 叠在页面底色上）——"+
					"动态条目要留的那一条下边框没画出来", got, border)
			}
			// 那一条要**横贯整条**（直角、通栏）。
			for x := int(card.X); x < int(card.X+card.W); x++ {
				if got := img.RGBAAt(x, bottom); got != border {
					t.Fatalf("下边框在 x=%d 断了（%v，want %v）——那一条要横贯整条",
						x, got, border)
				}
			}
			// 上边 / 左边 / 右边都**不许**有描边。
			for x := int(card.X); x < int(card.X+card.W); x++ {
				if got := img.RGBAAt(x, int(card.Y)); got == border {
					t.Fatalf("卡片上边在 x=%d 也有一道描边（%v）——动态条目只留一条下边框",
						x, got)
				}
			}
			for _, x := range []int{int(card.X), int(card.X + card.W) - 1} {
				for y := int(card.Y); y < bottom; y++ {
					if got := img.RGBAAt(x, y); got == border {
						t.Fatalf("卡片侧边（x=%d）在 y=%d 也有一道描边（%v）——"+
							"动态条目只留一条下边框", x, y, got)
					}
				}
			}
		})
	}
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

// TestTheFourTabsLeaveTheSameGapBelowTheTabBar 钉住「四张 Tab 的间距口径一致」。
//
// # 为什么这一条要紧
//
// 原来这一页有**两套口径**：「动态」「收藏夹」「合集」那三张一点上下都没给（内容
// 紧贴 Tab 那一条），而「投稿」那一张自己的工具条带了上下内边距——于是同一页上
// 两张 Tab 的第一样东西离 Tab 那一条的距离不一样（用户 2026-10-25 说的）。
//
// 钉的是**四张 Tab 的第一样东西离 Tab 那一条的底边都是 16**。
func TestTheFourTabsLeaveTheSameGapBelowTheTabBar(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "四个 Tab 的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicWithFace("d1", spaceDynamicText, spaceDynamicFace),
		}, "offset": "", "has_more": false})
	})
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{"bvid": "BV1", "title": "一个投稿", "length": "3:20", "cid": "9"},
			}},
			"page": map[string]any{"count": 1},
		})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "一个收藏夹", "media_count": 3},
		}})
	})
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{
			"seasons_list": []any{map[string]any{"meta": map[string]any{
				"season_id": 11, "name": "一个合集", "total": 4,
			}}},
			"page": map[string]any{"total": 1},
		}})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	_, trackBottom := spaceTabTrack(t, tt, th)

	for tab := range upTabNames {
		sp.tab = tab
		tt.Frame()
		waitFrames(t, tt, "Tab "+upTabNames[tab]+" 加载完", func() bool {
			if tab == upTabVideo {
				return sp.videos.Paged().Loaded > 0
			}
			return sp.pagerOf(tab).started
		})
		tt.Frame()

		got := tabFirstThing(t, tt, s, th, tab, trackBottom)
		if got < trackBottom+15 || got > trackBottom+17 {
			t.Errorf("「%s」那一张 Tab 的第一样东西在 y=%.0f，want %.0f（Tab 那一条的"+
				"底边 %.0f + 16）——四张 Tab 的间距口径必须一致",
				upTabNames[tab], got, trackBottom+16, trackBottom)
		}
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
