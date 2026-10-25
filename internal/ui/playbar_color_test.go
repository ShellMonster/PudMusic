// 播放条上的**颜色与反馈**那一批（票 #220 / 报告 §3.3 §3.4 §3.5 §3.6 §3.7
// §3.9 §3.10）。
//
// 这一族断的全是**画面上的东西**：悬停时图形有没有变主色、封面那一块是不是
// 6 的圆角、倍速浮层那一档是不是一块实心的主色、音量浮层那根滑块多大。
//
// ⚠️ **这一族的错法全是「静默」的**：图形没变色、圆角差了两像素、当前档还是
// 灰底绿字——**没有一样会报错**，界面只是「不太对」。所以判据全是量得出来的那
// 几样（像素、框的尺寸），不是函数名。
package ui

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheIconKeysTurnAccentOnHover 钉住「播放条上的图标键，悬停时图形变主色」。
//
// 上游 `src/components/icon-button/index.tsx:16`：
// `className={twMerge("hover:text-primary text-inherit", className)}` —— 悬停时
// 图形**从前景色变成主色**，不只是底下多一层淡底（报告 §3.5）。
//
// ⚠️ **不能用「众数」取图形色**：图标键底下那层 8% 的悬停底是一大片，而图形
// 只有 18×18——众数取到的是那层底，不是图形（`inkColor` 那套是给「底下什么
// 都没有」的播放键用的）。
//
// 所以这里断的是**「画面上有没有主色像素」**：没悬停时一个都不许有，悬停之后
// 必须有（图形是 18 见方，核心像素是实打实的主色）。
func TestTheIconKeysTurnAccentOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, fullBarState(), 1200, 0, dark)
			want := rgb(ink.AccentOn(BuildTheme(s.app.SettingsSnapshot(), dark)))

			for _, label := range []string{"播放模式", "下载", "加歌", "播放列表", "收藏"} {
				btn, ok := tt.Find(label)
				if !ok {
					t.Fatalf("播放条上找不到 %q，文字有 %q", label, tt.Texts())
				}
				// 指针先离开：那时候画面上一个主色像素都不该有。
				tt.Move(600, 20)
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

// TestTheDisabledIconKeysDoNotTurnAccent 钉住「禁用着的图标键悬停不变主色」。
//
// 🔴 **一枚禁用的键用主色告诉用户「这里点得动」**——那是错的（票 #217 的评审
// 查出来过同款的一条）。
//
// 原因在**顺序**：颜色是**建元素时**定的，而「悬停没有」也是**建元素时**问的
// （`Hovered()`）。问的时候如果**还没禁用**，那一刻问出来的是「它悬停着」——
// 于是一枚禁用的键也染上了主色。所以「禁不启用」必须**在建按钮的时候就给**
// （`iconKey` 的 `disabled` 那一栏），不能靠调用方链 `.Disabled(...)`——那是
// 返回之后才设上的。
//
// ⚠️ **样本是「上一首 / 下一首」那两枚**：无头外壳里**没有播放器**，所以它们
// 此刻都是禁用的（`App.CanPrev` 要 `player != nil`）。
//
// # 🔴 **判据是「悬停前后的图形墨迹色」，不是「数主色像素」
//
// 框架把**禁用**的控件整体乘 0.5 不透明度（`mygo/ui/paint.go:209`），所以一枚
// **被染了主色**的禁用键画出来是 **rgb(136,188,154)** 那一档，**不是**主色那个
// rgb(17,121,54)——「画面上有几个接近主色的像素」**一个都数不到**，于是那条判据
// 恰恰对它要防的那类 bug **是绿的**。
//
// 正确的那套在仓库里早就有：`TestTheDisabledPlayKeyDoesNotTurnAccent`
// （`playbar_layout_test.go`）比的是**悬停前后的图形墨迹色**——禁用的键**连那层
// 悬停底都不该有**（`widgets/flat` 的 `shade` 是在**画的时候**重新问一遍
// `IsDisabled()` 的），所以悬停前后那一片墨迹**一个像素都不该变**。这一条用的
// 就是它那一套。
func TestTheDisabledIconKeysDoNotTurnAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, fullBarState(), 1200, 0, dark)
			bg := rgb(BuildTheme(s.app.SettingsSnapshot(), dark).Background)

			for _, label := range []string{"上一首", "下一首"} {
				btn, ok := tt.Find(label)
				if !ok {
					t.Fatalf("播放条上找不到 %q，文字有 %q", label, tt.Texts())
				}
				// 指针先离开：那时候那一片墨迹是它本来的颜色。
				tt.Move(600, 20)
				tt.Frame()
				idle := inkColor(tt.Image(), btn, bg)

				// 指针落到这一枚上。
				tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
				tt.Frame()
				lit := inkColor(tt.Image(), btn, bg)

				if !sameColor(lit, idle) {
					t.Errorf("禁用着的 %s 悬停之后那一片墨迹从 %v 变成了 %v——"+
						"一枚禁用的键不该有任何变化（它连那层淡底都不该有）",
						label, idle, lit)
				}
				// 顺带断一句「它不是主色」：主色是绿的，前景色是中性色。
				//
				// ⚠️ **这一条不能省**：上面那条比的是「变了没有」，而一个**从
				// 一开始就绿**的键（比如把状态色写死了）它是「没变」——只有这条
				// 抓得住。
				if absInt(int(lit.G)-int(lit.R)) > 12 {
					t.Errorf("禁用着的 %s 悬停之后是 %v——那不是主色该有的样子，"+
						"也是一枚禁用的键不该有的颜色", label, lit)
				}
			}
		})
	}
}

// TestTheCoverHoverOverlayIsAccentAndRoundedSix 钉住封面悬停时浮出来的那一层。
//
// 上游 `layout/playbar/left/index.tsx:42-44`：
//
//	<div className="text-primary absolute … rounded-md bg-[rgba(0,0,0,0.5)] …">
//	  <RiArrowUpSLine size={32} />
//	</div>
//
// 两件事（报告 §3.3）：**箭头是主色**（我们原来给的是白 92%，明暗和上游正好
// 反过来）、**那一层的圆角是 6**（`rounded-md`，我们原来跟的是主题那一个 8）。
//
// ⚠️ **箭头取的是主色本身，不是 `ink.AccentOn` 那一档**：那一档是按**应用底色**
// 算的（浅色下是一个深绿 rgb(18,114,52)），而这里压的是**半黑罩**——罩子底下是
// 封面那张图，深绿压在上面读不出来。罩子永远是暗的，所以要的是亮的那一档。
func TestTheCoverHoverOverlayIsAccentAndRoundedSix(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			want := rgb(th.Accent)

			cover, ok := tt.Find("当前封面")
			if !ok {
				t.Fatalf("播放条上找不到封面，文字有 %q", tt.Texts())
			}

			// 指针先离开：那时候浮层不在画面上。
			tt.Move(600, 20)
			tt.Frame()
			idle := tt.Image()
			if n := nearPixels(idle, cover, want); n != 0 {
				t.Fatalf("没悬停的时候封面上已经有 %d 个主色像素——"+
					"那一层浮层压根不该在那儿", n)
			}

			// 指针落到封面上。
			tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
			tt.Frame()
			lit := tt.Image()

			// 箭头是主色。那一枚箭头是 32 见方的线框图形，墨迹有一百多个像素——
			// 所以「≥ 20 个主色像素」既抓得住「没变色」，又不会被抗锯齿的边
			// 缘骗过去。
			if n := nearPixels(lit, cover, want); n < 20 {
				t.Errorf("悬停之后封面那一块上只有 %d 个主色像素（want ≥ 20）——"+
					"箭头不是主色（上游是 `text-primary`，我们原来给的是白 92%%）", n)
			}

			// 圆角是 6：那一层是 56×56 的圆角块，而它压住的那一块**就是封面
			// 那一块**——所以量它的角就是量「罩子的角从第几个像素开始圆」。
			//
			// ⚠️ **判据是「被罩住的程度」**：那一层是半黑罩，罩得越满那个像素
			// 越暗；**罩满**的那个像素的下标就是圆角的起点（圆角 6 → 6，圆角 8
			// → 8）。
			first := overlayCornerX(idle, lit, cover)
			if first < 0 {
				t.Fatal("悬停之后封面那一块一个像素都没变——浮层压根没画出来")
			}
			if first > 7 {
				t.Errorf("浮层那一层的角从左数第 %d 个像素才圆起来——"+
					"它的圆角比 6 大（上游是 `rounded-md` = 6）", first)
			}
		})
	}
}

// TestThePlayBarCoverIsRoundedSix 钉住「播放条上那张封面的圆角是 6」。
//
// 上游 `layout/playbar/left/index.tsx:31-41` 的 `<Image radius="md" …>`——
// `rounded-md` 是 6，而我们原来跟的是**主题那个圆角令牌**（默认 8）。
//
// ⚠️ **那个令牌动不得**（`radius.default`，用户在设置里能调它），所以只有这一处
// 封面用 6（见 `internal/ui/playbar.go` 的 `coverRadius`）。
//
// 判据：封面那一格**第一行**上那一圈描边从左数第几个像素开始（圆角 6 → 5~6；
// 圆角 8 → 7~8）。描边是 1 像素的 `t.Border`，而占位图的底色和它差得开。
func TestThePlayBarCoverIsRoundedSix(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)
			th := BuildTheme(s.app.SettingsSnapshot(), dark)

			cover, ok := tt.Find("当前封面")
			if !ok {
				t.Fatalf("播放条上找不到封面，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			first := coverCornerX(img, cover, rgb(th.Background), rgb(th.Border))
			if first < 0 {
				t.Fatalf("封面那一格的第一行上连那条描边都找不到（边框是 %v）", rgb(th.Border))
			}
			if first > 7 {
				t.Errorf("封面那一格第一行上的描边从左数第 %d 个像素才圆起来——"+
					"它的圆角比 6 大（上游是 `radius=\"md\"` = 6）", first)
			}
		})
	}
}

// coverCornerX 是「封面那一格的角从第几个像素开始圆」的量法。
//
// 那一格是 56×56 的**圆角裁剪**（`imgcache.ViewRadius` 的 `Radius().Clip()`），
// 角上那一圈是「背景」和「描边」之间的过渡带——**描边越接近实色的那个像素，
// 角就越圆**。
//
// ⚠️ **判据是归一化的**：像素离背景还差多少，占「背景到描边」那一整段差的多
// 少。写成「和背景不一样」会量到过渡带的**起点**（抗锯齿横跨好几个像素），
// 那样量出来的数比圆角小两三个像素——**圆角 6 和圆角 8 会量成同一个数**。
//
// 返回的是**第一行**（角上那一行）上的下标。
func coverCornerX(img *image.RGBA, box mygoui.Rect, bg, border color.RGBA) int {
	const want = 0.9
	y := int(box.Y)
	for x := 0; x < int(box.W); x++ {
		p := img.RGBAAt(int(box.X)+x, y)
		f := (frac8(p.R, bg.R, border.R) + frac8(p.G, bg.G, border.G) + frac8(p.B, bg.B, border.B)) / 3
		if f >= want {
			return x
		}
	}
	return -1
}

// frac8 是「从 from 走到 to 走了整段的多少」。
func frac8(v, from, to uint8) float64 {
	span := int(to) - int(from)
	if span == 0 {
		return 0
	}
	return float64(int(v)-int(from)) / float64(span)
}

// overlayCornerX 是「那一层浮层的角从第几个像素开始圆」的量法。
//
// 浮层是半黑罩（`ui.RGBA(0,0,0,0.5)`），所以**罩满**的像素恰好是原来的一半
// （底下那一层是不透明的）。判据是「被压暗了多少，占压满时的多少」——
// 归一化之后和底色深浅无关，深浅两套主题量出来是同一个数。
func overlayCornerX(idle, lit *image.RGBA, box mygoui.Rect) int {
	const want = 0.9
	y := int(box.Y)
	for x := 0; x < int(box.W); x++ {
		a := idle.RGBAAt(int(box.X)+x, y)
		b := lit.RGBAAt(int(box.X)+x, y)
		f := (darkFrac(a.R, b.R) + darkFrac(a.G, b.G) + darkFrac(a.B, b.B)) / 3
		if f >= want {
			return x
		}
	}
	return -1
}

// darkFrac 是「被压暗了多少，占压满时（压到一半）的多少」。
//
// ⚠️ **分母是「原来那一档的一半」**：半黑罩压在**不透明**的东西上，罩满的时候
// 那个像素恰好是原来的一半。
func darkFrac(from, to uint8) float64 {
	full := float64(from) / 2
	if full == 0 {
		return 0
	}
	return (float64(from) - float64(to)) / full
}

// TestTheRateMenuIsSixtyWideWithASolidCurrentRow 钉住倍速浮层的样子。
//
// 上游 `components/music-rate/index.tsx:26-46`：
//
//	浮层   py-3 px-2 w-[60px] min-w-[60px]
//	七行   Button isIconOnly radius="md" size="sm" min-w-[40px]
//	当前档 color="primary" variant="solid"（一块实心绿）
//
// 三件事（报告 §3.10）：**浮层 60**、**那七行 44**（= 60 − 左右各 8 的内边距）、
// **当前那一档是实心主色**（我们原来是一块中性灰底 + 主色的字）。
//
// ⚠️ **「0.75x」不许被截**：44 宽装得下它，前提是那七行的按钮**左右没有内边距**
// （上游是 `isIconOnly`，HeroUI 给它 `px-0`）。我们那枚扁平按钮左右各留 14——
// 44 减去 28 只剩 16，那一档会被截成「0.…」。这一条由
// `TestRateMenuLabelsAreNotTruncated` 断着。
func TestTheRateMenuIsSixtyWideWithASolidCurrentRow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **要用带主题的那一份 tester**（`playbarTester`）：不然画面上的是
			// **框架默认那套主题**（主色是蓝的 #2563eb），量「实心主色」量到的
			// 是蓝的，而设置里的主色是绿的。
			tt, s := playbarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1}, 760, 0, dark)
			s.rateOpen = true
			tt.Frame()

			// 浮层本身：60 宽。
			menu, ok := tt.Find("倍速菜单")
			if !ok {
				t.Fatalf("画面上找不到倍速浮层，文字有 %q", tt.Texts())
			}
			// ⚠️ **断言里写的是字面的 60，不是 `rateMenuW`**：照抄被测对象的话，
			// 改常量的人顺手把断言也改了，这条就永远不会红（票 #217 的评审查过
			// 一条同款的）。
			if menu.W != 60 {
				t.Errorf("倍速浮层是 %.1f 宽，而上游那一列是 60 宽"+
					"（`w-[60px] min-w-[60px]`）", menu.W)
			}

			// 那七行：各 44 宽。
			for _, r := range app.RateSteps {
				row, ok := tt.Find("播放速率 " + rateLabel(r))
				if !ok {
					t.Fatalf("浮层里没有 %q 那一行，文字有 %q", rateLabel(r), tt.Texts())
				}
				if row.W != 44 {
					t.Errorf("%q 那一行是 %.1f 宽，而上游是 44 宽"+
						"（60 − 左右各 8 的内边距）", rateLabel(r), row.W)
				}
			}

			// 字号是 12（上游 `size="sm"` 的 `text-tiny`）。
			//
			// ⚠️ **量的是那一行字画出来的宽**：字号变大，字也变宽——而 44 宽
			// 装得下 14 号的「1.75x」（约 35），所以**光靠「没被截」抓不住它**。
			lbl, ok := tt.Find("1.75x")
			if !ok {
				t.Fatalf("浮层里没有「1.75x」那一行，文字有 %q", tt.Texts())
			}
			wantW := textWidths(t, []string{"1.75x"}, 12)["1.75x"]
			if absInt(int(lbl.W)-int(wantW)) > 1 {
				t.Errorf("「1.75x」画出来是 %.1f 宽，而 12 号字下应该是 %.1f 宽"+
					"（上游是 `size=\"sm\"` 的 `text-tiny` = 12）", lbl.W, wantW)
			}

			// 当前那一档：一块实心主色，字是主色底上该有的那一档。
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			cur, ok := tt.Find("播放速率 1x")
			if !ok {
				t.Fatalf("浮层里没有当前那一档，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			if got := probeFill(img, cur); !near(got, rgb(th.Accent)) {
				t.Errorf("当前那一档的底是 %v，而上游是**实心主色** %v"+
					"（`variant=\"solid\"`）", got, rgb(th.Accent))
			}
			// ⚠️ **内缩 6 再取墨迹**：那一格是 44 宽、圆角 4 的实心块，四个角上是
			// **浮层的底色**（浅色下是白的），而字只有十几个像素——不内缩的话
			// 众数取到的是那几个角，不是字。
			inner := mygoui.Rect{X: cur.X + 6, Y: cur.Y + 6, W: cur.W - 12, H: cur.H - 12}
			if got := darkestPixel(img, inner); !near(got, rgb(th.AccentText)) {
				t.Errorf("当前那一档的字是 %v，而主色底上该是 %v"+
					"（`t.AccentText`：主色底上读得出来的那一档）", got, rgb(th.AccentText))
			}
		})
	}
}

// TestThePlaylistKeyAndThePartKeyAreDifferentGlyphs 钉住「播放列表」和「分集」
// 是**两个不同的图形**。
//
// 上游那边它们本来就是两个图形：
//
//	播放列表  RiPlayListLine（`components/open-playlist-drawer-button/index.tsx:10-13`）
//	分集      RiListRadio  （`layout/playbar/left/page-list.tsx:25`）
//
// 而我们原来**两个都用 `Editor/list-radio.svg`**——那是一个**看得见的错**：两个
// 按钮一个开队列、一个开分集，用户分不出哪个是哪个（报告 §3.7）。
//
// 断两件事：**名字对得上上游**（那张抄自 Biu 源码的表），以及**画出来确实不是
// 一张图**（指纹不同）。
func TestThePlaylistKeyAndThePartKeyAreDifferentGlyphs(t *testing.T) {
	// 表里写的是**上游的字面文件名**（和 `buiIcons` 那张表同一个规矩）：
	// 改了常量而不改这一行，立刻红；要改这一行，得对着 Biu 源码看一眼。
	for _, c := range []struct{ what, upstream, got string }{
		{"播放列表", "Media/play-list-line.svg", iconQueue},
		{"分集", "Editor/list-radio.svg", iconPageList},
	} {
		if c.got != c.upstream {
			t.Errorf("「%s」该画的是 %s（照 Biu 的源码），生产代码给的是 %s",
				c.what, c.upstream, c.got)
		}
	}
	if iconQueue == iconPageList {
		t.Fatalf("「播放列表」和「分集」用的是同一张图（%s）——"+
			"上游那两个按钮用的是两个不同的图形", iconQueue)
	}

	// 画面上：两枚画出来不是同一张图。
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Gap(20).Children(func() {
			mygoui.ButtonBase(c).Label("播放列表").Children(func() {
				mygoui.Icon(c, icon(iconQueue)).FontSize(18)
			})
			mygoui.ButtonBase(c).Label("分集").Children(func() {
				mygoui.Icon(c, icon(iconPageList)).FontSize(18)
			})
		})
	}, 200, 60)
	tt.Frame()

	queue := scanGlyph(tt, "播放列表")
	part := scanGlyph(tt, "分集")
	if queue.ink == "" || part.ink == "" {
		t.Fatalf("两枚里有一枚没画东西（播放列表 %q / 分集 %q）", queue.ink, part.ink)
	}
	if queue.ink == part.ink {
		t.Error("「播放列表」和「分集」画出来是同一张图——" +
			"上游那两个按钮用的是两个不同的图形（`RiPlayListLine` / `RiListRadio`）")
	}
}

// TestTheVolumePopoverMatchesTheUpstreamSizes 钉住音量浮层里那几样的尺寸。
//
// 上游 `components/music-volume/index.tsx:126-149`：
//
//	滑块   orientation="vertical" size="sm" radius="full" color="primary"
//	       trackWrapper "h-40 w-[32px]"（**160 高、32 宽**）
//	旋钮   16
//	百分比 endContent "w-8 text-center text-xs"（**12px**）
//
// 我们原来的是 120 高、20 宽、旋钮 14、百分比 11（报告 §3.9）。
//
// ⚠️ **浮层是悬停才出来的**（上游是 300ms 延迟的 Tooltip），所以先悬停再量。
func TestTheVolumePopoverMatchesTheUpstreamSizes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, fullBarState(), 1200, 0, dark)
			hoverVolumeKey(t, tt)

			bar, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("浮层里找不到那根音量滑块，文字有 %q", tt.Texts())
			}
			// ⚠️ **字面的 32×160，不是那两个常量**（理由同上）。
			if bar.W != 32 || bar.H != 160 {
				t.Errorf("浮层里那根滑块是 %.1f×%.1f，而上游是 32×160"+
					"（`h-40 w-[32px]`）", bar.W, bar.H)
			}

			// 旋钮是 16 见方。
			//
			// ⚠️ **量的是「哪一行的主色像素最多」**：旋钮和填过的那一段都是主色，
			// 而旋钮那一行比轨道（4 宽）宽得多——所以最多的那一行就是旋钮所在
			// 那一行，它的宽度就是旋钮的宽度。
			//
			// ⚠️ **不能拿「不是轨道色」当墨迹**：滑块那个盒子里的**内边距**上是
			// 浮层的底色（`t.Background`），不是轨道色——那样量出来的是整个盒子。
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			img := tt.Image()
			if w := widestAccentRow(img, bar, rgb(th.Accent)); w < 15 || w > 17 {
				t.Errorf("旋钮是 %d 宽，而上游是 16（`size=\"sm\"`）", w)
			}

			// 百分比是 12px：它的**墨迹宽**要等于 12 号字下那几个字的自然宽。
			pct, ok := tt.Find("80%")
			if !ok {
				t.Fatalf("浮层里找不到那个百分比，文字有 %q", tt.Texts())
			}
			bg := rgb(th.Background)
			lo, hi, n := inkExtent(img, pct, bg)
			if n == 0 {
				t.Fatalf("那个百分比上量不到墨迹")
			}
			got := hi - lo + 1
			// ⚠️ **字面的 12**：12 号字下那几个字是 20 宽，11 号字下是 18——
			// 差两个像素，所以这个判据抓得住「字号退回 11」。
			want := textInkWidth(t, "80%", 12)
			if absInt(got-want) > 1 {
				t.Errorf("那个百分比的墨迹是 %d 宽，而 12 号字下应该是 %d 宽"+
					"（上游是 `text-xs` = 12）", got, want)
			}
		})
	}
}

// textInkWidth 量一串字在某个字号下**画出来**有多宽（那几个字的墨迹有多宽）。
//
// ⚠️ **量的是墨迹，不是控件的宽**：浮层里那个百分比是**定宽**的一格
// （`Width(volPanelW)` = 40，字在格子里居中、而且 `NoWrap` 不许折行），而字号变
// 的是**那几个字本身**——要断「字号对不对」，就得量字的墨迹。
//
// ⚠️ **那一格是 40 而不是滑块那 32**：「100%」在 12 号字下的排版宽是 31.8，
// 32 宽的格子只剩 0.2 像素余量，系统字号放大到 1.05 倍它就折成两行（票 #485
// 的评审抓出来的）。见 `TestTheVolumePercentDoesNotWrapAtBigTextScales`。
func textInkWidth(t *testing.T, s string, font float32) int {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Width(400).Children(func() {
			mygoui.Text(c, s).FontSize(font)
		})
	}, 400, 60)
	tt.Frame()
	img := tt.Image()
	// 底色取画面左上角那一块（Tester 的默认底），不写死颜色。
	bg := img.RGBAAt(0, 0)
	lo, hi, n := inkExtent(img, mygoui.Rect{X: 0, Y: 0, W: 400, H: 60}, bg)
	if n == 0 {
		t.Fatalf("连 %q 都量不到墨迹", s)
	}
	return hi - lo + 1
}

// widestAccentRow 是「box 里主色最多的那一行有几个主色像素」。
//
// 音量滑块上，旋钮和填过的那一段都是主色，而旋钮那一行最宽（16），其余行只有
// 轨道那 4 像素——所以最多的那一行的宽度就是旋钮的宽度。
func widestAccentRow(img *image.RGBA, box mygoui.Rect, accent color.RGBA) int {
	best := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		n := 0
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if near(img.RGBAAt(x, y), accent) {
				n++
			}
		}
		if n > best {
			best = n
		}
	}
	return best
}

// darkestPixel 是 box 里**最暗**的那一个像素。
//
// ⚠️ **为什么不用众数**：那一格是 12 号字（「1x」两个字），笔画只有一两个像素宽，
// 于是**抗锯齿的边缘像素比笔画的核心还多**——众数取到的是「主色和字混出来」
// 的那个中间色，不是字本身的颜色。最暗的那一个才是笔画的中心。
func darkestPixel(img *image.RGBA, box mygoui.Rect) color.RGBA {
	best := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	bestL := 1 << 30
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			l := int(p.R) + int(p.G) + int(p.B)
			if l < bestL {
				bestL, best = l, p
			}
		}
	}
	return best
}

// TestRenderPlayBarColorShots 把这一票改动的几个状态存成 PNG，留给人眼对照。
//
// 和报告（`~/.herdr-projects/reports/Biu_MyGo/124/`）里那几张同名，方便并排比。
//
//	go test ./internal/ui/ -run TestRenderPlayBarColorShots
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarColorShots
//
// ⚠️ **播放条摆在窗口底下**（`bottomBarTester`）：倍速和音量那两个浮层是**往上**
// 弹的，只画播放条的那一支把它摆在窗口顶上，于是浮层有一半在窗口外面。
func TestRenderPlayBarColorShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}

		// A1：平时（有歌在放）。
		tt, _ := bottomBarTester(t, fullBarState(), 1200, 360, dark)
		tt.Move(600, 40)
		tt.Frame()
		save(t, dir, "A1-播放条-有歌在放-"+name+".png", tt)

		// A2：封面悬停——罩上的箭头是主色，罩和封面都是 6 的圆角。
		tt, _ = bottomBarTester(t, fullBarState(), 1200, 360, dark)
		cover, ok := tt.Find("当前封面")
		if !ok {
			t.Fatalf("播放条上找不到封面，文字有 %q", tt.Texts())
		}
		tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
		tt.Frame()
		save(t, dir, "A2-播放条-封面悬停-"+name+".png", tt)

		// A3：倍速浮层——60 宽、那七行 44、当前那一档是实心主色。
		tt, s := bottomBarTester(t, fullBarState(), 1200, 360, dark)
		s.rateOpen = true
		tt.Frame()
		save(t, dir, "A3-播放条-倍速浮层-"+name+".png", tt)

		// A4：音量浮层——32×160 的竖滑块、16 的旋钮、12 号字的百分比。
		tt, _ = bottomBarTester(t, fullBarState(), 1200, 360, dark)
		hoverVolumeKey(t, tt)
		save(t, dir, "A4-播放条-音量浮层-"+name+".png", tt)

		// A5：图标键悬停——图形变主色（不是只多一层淡底）。
		tt, _ = bottomBarTester(t, fullBarState(), 1200, 360, dark)
		mode, ok := tt.Find("播放模式")
		if !ok {
			t.Fatalf("播放条上找不到「播放模式」，文字有 %q", tt.Texts())
		}
		tt.Move(mode.X+mode.W/2, mode.Y+mode.H/2)
		tt.Frame()
		save(t, dir, "A5-播放条-图标键悬停-"+name+".png", tt)
	}
}
