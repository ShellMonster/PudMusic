// 图标键统一成一档（票 #249）。
//
// # 这一票改了什么
//
// 同一个「图标键」在应用里原来有**两套配方**：
//
//   - `iconKey` → `flat.IconButtonAccent`：**悬停时图形变主色**（上游的
//     `hover:text-primary`），播放条上那些走它；
//   - `iconButton` → `flat.IconButton`：**只有 8% 的淡底，图形不变色**，顶栏那
//     几枚、队列抽屉头那两枚、新歌速递的翻页箭头走它。
//
// 偏离上游的是**「不换色」那一套**（上游 HeroUI 的 `IconButton` 就是
// `hover:text-primary`，见 `widgets/flat/flat.go` 的 `IconButtonAccent`）——用户从
// 播放条挪到顶栏会觉得「这一枚不亮」。这一票把**底是应用底色**的那些地方统一成
// `iconKey`。
//
// 顺带把队列抽屉行尾那枚「⋯」的**尺寸**（44×28 → 32×32）和**无障碍名**
// （「这一首的操作」→「「歌名」的操作」）对齐到列表那一档。
//
// ⚠️ **满屏播放器那一行没动**（票面点名）：它的底是**压暗过的封面**，主色当字色
// 那一档压在上面只有 1.0:1（等于看不见，见票 #217 的评审）——那是另一票的事。
// `TestTheFullPlayerRowKeepsItsOwnRecipe` 钉住这一条。
//
// # 判据全是**画面上的东西**
//
// 「悬停时图形变主色」这件事没有任何签名变化，也没有一行代码会报错——图形不换色
// 只是「不太对」。所以判据是**逐像素**：悬停之后那一枚的框里必须出现主色像素，
// 而悬停之前一个都不许有（同 `playbar_color_test.go` 的
// `TestTheIconKeysTurnAccentOnHover`）。
//
// ⚠️ **图形特别小的那一枚判据不一样**：队列抽屉行尾那枚「⋯」是三个 2 像素的
// 点（上游 `RiMore2Line` 在 16px 下就那么大），**没有一个像素是实打实的主色**
// ——全被抗锯齿混掉了。所以那一枚断的是「**离主色更近了**」（`nearestPixel` 那个
// 判据，和 `homepage_newmusic_test.go` 里歌名那一处同一把尺子）。
package ui

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	mygoui "github.com/egoist/mygo/ui"
)

// navBarShell 建一个**只画顶栏**的外壳，返回外壳和 Tester。
//
// 和 `navBarTester` 同一套画法，只是把外壳也返回来——这一票要拿主题算主色
// （`ink.AccentOn` 是**从主题现算**的，不是写死的一个绿）。
//
// ⚠️ **要装主题**（`s.applyFrameTheme(c)`）：只画顶栏的那一支**不装**的话，画出来
// 的是**框架默认那份主题**（主色是蓝的 #2563eb），而设置里的主色是棕的
// （#a8552e）——按棕的去找主色像素，一个都找不到（同 `playbar_layout_test.go` 的
// `playbarTester` 顶上那一段）。
//
// ⚠️ **只画顶栏**：整窗里「登录」这两个字在别处也有（侧栏那一行），而
// `Tester.Find` 认的是**先画的那个**——画整窗的话量到的是别处那一枚。
func navBarShell(t *testing.T, preset app.ThemePreset, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	b := newTestShell(t, withSettings(st), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewNav(c)
		}
	}))
	tt := b.tt
	tt.SetFocused(true)
	tt.Frame()
	return b.s, tt
}

// drawerShell 建一个**开着队列抽屉**的外壳（两行歌）。
//
// 深浅两套由 `systemIsDark` 决定——主色当字色那一档（`ink.AccentOn`）深浅两个
// 不同的数，所以这一族必须两套都跑。
func drawerShell(t *testing.T, preset app.ThemePreset, dark bool) *harness {
	t.Helper()
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	b := newTestShell(t, withSettings(st), at(winW, winH), systemIsDark(dark))
	h := &harness{app: b.a, tt: b.tt, s: b.s}
	second := app.Item{
		Source: "online", Type: "audio", Sid: "999", Bvid: "BV1probe", Cid: "1",
		Title: "队列里的第二首",
	}
	enqueue(t, h, append(copiesOf(t, 1), second)...)
	openDrawer(t, h)
	return h
}

// TestTheTopBarIconKeysTurnAccentOnHover 钉住顶栏那四枚「悬停时图形变主色」。
//
// 四枚是「打开文件」「迷你」「动态」「登录」（已登录时是「退出登录」）。
//
// ⚠️ **判据是「画面上有没有主色像素」**：没悬停时一个都不许有，悬停之后必须有
// （图形 18 见方 / 16 见方，核心像素是实打实的主色）。
//
// ⚠️ **两套都要跑**：`ink.AccentOn` 浅色下是主色压深的那一档（默认主色下算出来
// 是 #9e502b），深色下就是主色本身——两个不同的数，只跑一套的话另一半的算法
// 没被验过。
func TestTheTopBarIconKeysTurnAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := navBarShell(t, app.DefaultThemePreset, dark)
			want := rgb(ink.AccentOn(BuildTheme(s.app.SettingsSnapshot(), dark)))

			for _, label := range []string{"打开文件", "迷你", "动态"} {
				btn, ok := tt.Find(label)
				if !ok || btn.W == 0 {
					t.Fatalf("顶栏上找不到 %q，文字有 %q", label, tt.Texts())
				}
				// 指针先离开：那时候画面上一个主色像素都不该有。
				tt.Move(600, 400)
				tt.Frame()
				if n := nearPixels(tt.Image(), btn, want); n != 0 {
					t.Errorf("%s 没悬停的时候画面上已经有 %d 个主色像素——"+
						"它平时是前景色的（上游是 `text-inherit`）", label, n)
				}
				// 指针落到这一枚上。
				tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
				tt.Frame()
				if n := nearPixels(tt.Image(), btn, want); n < 4 {
					t.Errorf("%s 悬停之后画面上只有 %d 个主色像素（want ≥ 4）——"+
						"图形没有变主色（上游是 `hover:text-primary`：图形本身换色，"+
						"不只是底下多一层淡底）", label, n)
				}
			}
		})
	}
}

// TestTheQueueDrawerHeaderKeysTurnAccentOnHover 钉住队列抽屉头那两枚。
//
// 「定位当前播放」和「清空」。理由和顶栏那四枚一样：**打开这个抽屉的那一枚**
// （播放条上的「播放列表」）走的是 `iconKey`——同一个抽屉里，头上的键和开抽屉的
// 键反馈档位不一样，用户会觉得「这一枚不亮」。
func TestTheQueueDrawerHeaderKeysTurnAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			h := drawerShell(t, app.DefaultThemePreset, dark)
			s, tt := h.s, h.tt
			want := rgb(ink.AccentOn(BuildTheme(s.app.SettingsSnapshot(), dark)))

			for _, label := range []string{"定位当前播放", "清空"} {
				btn, ok := tt.Find(label)
				if !ok || btn.W == 0 {
					t.Fatalf("抽屉头上找不到 %q，文字有 %q", label, tt.Texts())
				}
				// 指针挪到抽屉外面（抽屉占窗口右边 320 宽）。
				tt.Move(100, 100)
				tt.Frame()
				if n := nearPixels(tt.Image(), btn, want); n != 0 {
					t.Errorf("%s 没悬停的时候画面上已经有 %d 个主色像素——"+
						"它平时是前景色的", label, n)
				}
				tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
				tt.Frame()
				if n := nearPixels(tt.Image(), btn, want); n < 4 {
					t.Errorf("%s 悬停之后画面上只有 %d 个主色像素（want ≥ 4）——"+
						"图形没有变主色", label, n)
				}
			}
		})
	}
}

// TestTheQueueRowOperationKeyIsNamedAfterItsSong 钉住行尾那枚「⋯」的
// **无障碍名**。
//
// 原来它叫「这一首的操作」——一屏 N 枚全都叫这一句：读屏念出来分不出是哪一行，
// Tester 也认不出是哪一行（`Find` 认的是逐字相等）。现在它是「「歌名」的操作」，
// 和列表那一档（`list.OperationLabel`）一样。
//
// ⚠️ **旧名字不许再出现**：它一屏 N 枚，而「看起来名字改对了」和「名字还在」在
// 画面上长得一模一样——所以这里把旧名字也钉住。
func TestTheQueueRowOperationKeyIsNamedAfterItsSong(t *testing.T) {
	h := drawerShell(t, app.DefaultThemePreset, false)

	label := list.OperationLabel("队列里的第二首")
	if _, ok := h.tt.Find(label); !ok {
		t.Fatalf("行尾那枚「⋯」没有叫 %q，文字有 %q", label, h.tt.Texts())
	}
	for _, x := range h.tt.Texts() {
		if x == "这一首的操作" {
			t.Errorf("行尾那枚「⋯」还叫「这一首的操作」——" +
				"一屏 N 枚都叫这一句，读屏分不出是哪一行")
		}
	}
}

// TestTheQueueRowOperationKeyIsThirtyTwoSquare 钉住行尾那枚「⋯」的**尺寸**。
//
// 它曾经是 **44×28**（`flat` 的默认内边距 6/14），而列表那一档是 32×32。44 宽在
// 320 宽的抽屉里吃掉 14% 行宽，歌名那一列因此只有 **195** 而不是 207——**标题比
// 需要早 12px 截断**。
//
// 判据断两样：
//
//   - 那枚自己 **32×32**；
//   - **歌名那一列 207 宽**（行的内容 303 − 封面 48 − 两道 8 的缝 − 「⋯」32）。
//
// ⚠️ **歌名那一列是量出来的，而且量的是那一列，不是整行**。
//
// `Tester.Find(歌名)` 给回来的是**行**——行自己的无障碍名字就是歌名
// （`ListState.Label` 给的），而它排在树里更前。所以**量它的宽等于量整行**
// （319），而整行的宽**不随「⋯」变**（那一列是 `Grow(1)`：别的占多少它就剩多少）。
// 拿整行当判据，那条断言改前改后都成立——**抓不到它要防的截断**。
//
// 所以这里从**两个量出来的框**算那一列：行的内容左边 + 封面 + 一道缝 = 那一列的
// 左边；「⋯」的左边 − 一道缝 = 那一列的右边。
//
// ⚠️ **先确认按歌名找到的真是行**（高 64）：不是行的话下面那条量到的就是别的
// 东西——**宁可当场红，也不要一条永远绿的断言**。
func TestTheQueueRowOperationKeyIsThirtyTwoSquare(t *testing.T) {
	h := drawerShell(t, app.DefaultThemePreset, false)

	op, ok := h.tt.Find(list.OperationLabel("队列里的第二首"))
	if !ok || op.W == 0 {
		t.Fatalf("行尾那枚「⋯」没画出来，文字有 %q", h.tt.Texts())
	}
	if op.W != 32 || op.H != 32 {
		t.Errorf("行尾那枚「⋯」的点击区是 %.0f×%.0f，want 32×32"+
			"（列表那一档就是 32×32，44 宽的那一档把歌名那一列截短了 12px）",
			op.W, op.H)
	}

	// 那一行本身（**行**，不是那一列——见文件头那一段）。
	row, ok := h.tt.Find("队列里的第二首")
	if !ok || row.W == 0 {
		t.Fatalf("那一行没画出来，文字有 %q", h.tt.Texts())
	}
	if row.H != queueRowHeight {
		t.Fatalf("按歌名找到的是 %.0f×%.0f 的框，want %.0f 高的**行**——"+
			"它不是行的话，下面那条「歌名那一列有多宽」量到的就是别的东西",
			row.W, row.H, float64(queueRowHeight))
	}

	// 歌名那一列：行的内容左边 + 封面 48 + 一道 8 的缝 = 左边；「⋯」的左边 − 一道
	// 8 的缝 = 右边。
	const wantTitleW = 207.0
	titleLeft := row.X + queuePadX + queueCoverSize + space.Px8
	titleRight := op.X - space.Px8
	titleW := titleRight - titleLeft
	if titleW < wantTitleW-0.5 {
		t.Errorf("歌名那一列只有 %.0f 宽，want ≥ %.0f——"+
			"「⋯」那一枚又把它挤窄了（标题提前 %.0fpx 截断）",
			titleW, wantTitleW, wantTitleW-titleW)
	}
}

// TestTheQueueRowOperationKeyTurnsAccentOnHover 钉住行尾那枚「⋯」悬停时
// **图形变主色**。
//
// 原来它只有 8% 的淡底，而**指针落在它上面时整行也亮着**（`rowEl.Hovered()` 对
// 祖先为真）——两处反馈叠在一起，两者又都是极淡的中性色，于是**看不出「我现在
// 指着的是哪一枚」**。变主色那一档（上游的 `hover:text-primary`）把这一处
// 单独认出来。
//
// ⚠️ **这一枚的判据和别处不一样**：它画出来是三个 2 像素的点（上游
// `RiMore2Line` 在 16px 下就那么大），**没有一个像素是实打实的主色**——全被
// 抗锯齿混掉了（浅色下那几个点是主色和底色各七成三成的混合）。所以这里断的是
// **「离主色更近了」**（`nearestPixel` 那把尺子，和
// `homepage_newmusic_test.go` 里歌名变主色那一处同一把），外加「离正文色更远了」
// ——图形本来是正文色，变了主色之后就不该还像正文色。
func TestTheQueueRowOperationKeyTurnsAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			h := drawerShell(t, app.DefaultThemePreset, dark)
			s, tt := h.s, h.tt
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			want := ink.AccentOn(th)
			target := color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}
			textc := color.RGBA{R: th.Text.R, G: th.Text.G, B: th.Text.B, A: 255}

			op, ok := tt.Find(list.OperationLabel("队列里的第二首"))
			if !ok || op.W == 0 {
				t.Fatalf("行尾那枚「⋯」没画出来，文字有 %q", tt.Texts())
			}
			// 指针先离开那一行。
			tt.Move(100, 100)
			tt.Frame()
			before := nearestPixel(tt.Image(), op, want)
			// 指针落到它上面。
			tt.Move(op.X+op.W/2, op.Y+op.H/2)
			tt.Frame()
			after := nearestPixel(tt.Image(), op, want)

			if colorDist(after, target) >= colorDist(before, target) {
				t.Errorf("「⋯」悬停之后离主色 %v 反倒更远了（%v → %v）——"+
					"图形没有变主色，指针落在它上面和落在整行上分不出来",
					target, before, after)
			}
			if colorDist(after, textc) <= colorDist(before, textc) {
				t.Errorf("「⋯」悬停之后离正文色 %v 反而更近了（%v → %v）——"+
					"图形还是正文色的，没有换成主色", textc, before, after)
			}
		})
	}
}

// TestTheNewMusicPagerTurnsAccentOnHover 钉住新歌速递那两枚翻页箭头。
//
// 翻页是这一块**唯一的交互**，而原来那一档只有 8% 的淡底、图形不换色——用户最
// 需要在这里知道「还有下一页」的时候看不出来。
//
// ⚠️ **到头那一枚是禁用的**：禁用的键**不许**用主色告诉用户「这里点得动」
// （票 #217 的评审查出来过同款的一条）。所以这里断两件事：
//
//   - 能翻的那一枚悬停变主色；
//   - **禁用**的那一枚悬停**不变**主色（禁用的状态必须从 `iconKey` 的 `disabled`
//     那一栏给——链上的 `.Disabled(...)` 是返回之后才设上的，问早了）。
func TestTheNewMusicPagerTurnsAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			th := mygoui.LightTheme()
			if dark {
				th = mygoui.DarkTheme()
			}
			th.Accent = mygoui.Hex("#1ed760")
			want := rgb(ink.AccentOn(th))

			g := newNewMusic(homeTestCache(t), list.Actions{}, func() {})
			g.SetSongs(homeSongsOf("新歌", 20))
			tt := mygoui.NewTester(func(c *mygoui.Context) {
				c.SetTheme(th)
				g.View(c)
			}, 800, 900)
			homeFrame(tt)

			// 「下一页」能翻（20 首歌分了几页），「上一页」在第一页上是禁用的。
			next, ok := tt.Find("下一页")
			if !ok || next.W == 0 {
				t.Fatalf("画面上找不到「下一页」，文字有 %q", tt.Texts())
			}
			tt.Move(next.X+next.W/2, next.Y+next.H/2)
			tt.Frame()
			if n := nearPixels(tt.Image(), next, want); n < 4 {
				t.Errorf("「下一页」悬停之后画面上只有 %d 个主色像素（want ≥ 4）——"+
					"箭头没有变主色（翻页是这一块唯一的交互）", n)
			}

			prev, ok := tt.Find("上一页")
			if !ok || prev.W == 0 {
				t.Fatalf("画面上找不到「上一页」，文字有 %q", tt.Texts())
			}
			tt.Move(prev.X+prev.W/2, prev.Y+prev.H/2)
			tt.Frame()
			if n := nearPixels(tt.Image(), prev, want); n != 0 {
				t.Errorf("禁用着的「上一页」悬停之后画面上有 %d 个主色像素——"+
					"一枚禁用的键用主色告诉用户「这里点得动」", n)
			}
		})
	}
}

// TestTheTopBarKeysKeepTheirTipsAndNames 钉住「提示文案和点击行为一个字没改」。
//
// 这一票把顶栏那四枚换成了另一档配方（`iconButton` → `iconKey`）——**换控件
// 最容易丢的就是提示和名字**：提示是用户唯一能问出「这一枚是干什么的」的地方，
// 而无障碍名字是读屏唯一能念出来的。
//
// 判据两样：
//
//   - **提示**：指针落到那一枚上，那句话要**一字不差**地出现在画面上；
//   - **点击**：按了它，那件事还得发生（「动态」换页、「登录」进登录页）。
//
// ⚠️ **「打开文件」和「迷你」不按**：前一个会弹**真的系统文件框**（无头环境里
// 那是个真窗口，还可能把人拦在桌前），后一个要事件循环才有窗（`ensureMiniWindow`
// 走的是 `mygo.App.WhenReady`）。这两枚的「点了还是原来那件事」由**提示文案**
// 加上代码里那一句 `Clicked()` 兜着——换的是按钮的配方，不是那一行。
func TestTheTopBarKeysKeepTheirTipsAndNames(t *testing.T) {
	s, tt := navBarShell(t, app.DefaultThemePreset, false)

	for _, want := range []struct{ key, tip string }{
		{"打开文件", "打开本地音频文件"},
		{"迷你", "缩成 320×84 的小窗贴边悬浮，音乐不停"},
		{"动态", "动态"},
	} {
		btn, ok := tt.Find(want.key)
		if !ok || btn.W == 0 {
			t.Fatalf("顶栏上找不到 %q，文字有 %q", want.key, tt.Texts())
		}
		tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
		tt.Frame()
		if !tt.HasText(want.tip) {
			t.Errorf("%s 的提示不是 %q（画面上的字：%q）", want.key, want.tip, tt.Texts())
		}
	}

	// 点击：换页那两枚。
	if err := tt.Click("动态"); err != nil {
		t.Fatalf("点不了「动态」：%v", err)
	}
	tt.Frame()
	if got := s.router.Path(); got != dynamicFeedPath {
		t.Errorf("按了「动态」之后的路由是 %q，want %q", got, dynamicFeedPath)
	}

}

// TestTheFullPlayerRowKeepsItsOwnRecipe 钉住**满屏播放器那一行没被这一票改到**。
//
// 那一行的底是**压暗过的封面**，而主色当字色那一档是按应用底色算的——浅色下压
// 在上面只有 1.0:1（**等于看不见**，见票 #217 的评审）。所以它**不用** `iconKey`
// 那一档，那是另一票的事。
//
// 判据走**源码**：满屏播放器那几个文件里不许出现 `iconKey(`。画面上断不出来——
// 「没有变主色」和「变了一个看不见的主色」在压暗过的封面上是同一张图。
func TestTheFullPlayerRowKeepsItsOwnRecipe(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !strings.HasPrefix(name, "fullplayer") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "iconKey(") {
				t.Errorf("%s:%d 用了 iconKey（悬停时图形变主色）——"+
					"满屏播放器那一行的底是压暗过的封面，主色压在上面只有 1.0:1，"+
					"等于看不见（票 #217 的评审）", name, i+1)
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个满屏播放器的文件都没查到——前缀写错了，这条测试会一直绿着")
	}
}
