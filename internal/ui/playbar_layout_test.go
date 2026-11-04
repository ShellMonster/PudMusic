// 播放条重做那一票（GitHub #135）的验收。
//
// 这一族断的是**画面上的东西**：进度条的长度随窗口变、三键的左右次序、音量
// 浮层里那根竖滑块、旋钮只在悬停时出现、播放键的尺寸。
//
// ⚠️ **断的都是位置和像素，不是函数名**：这一票改的每一件事在界面上都是
// 「看得见但不出错」的那种——次序反了照样点得动，旋钮不画也照样能拖，
// 宽度不变也照样能 seek。判据只有量得出来的那几样才算数。
package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// playbarTester 只画播放条，State 从外面传进来。
//
// 和 `metaBarTester`（playbar_meta_test.go）的差别有三个：**能指定深浅**、
// 这一份总是带一个**有时长的**状态（旋钮那条测试要进度条是**能用的**：框架的
// `Hovered()` 对禁用的控件恒为 false，`mygo/ui/input.go:1118`），以及
// **它把应用的主题装上了**（`applyFrameTheme`）——
//
// ⚠️ **最后那一条是这一族要读颜色的前提**：真窗口里每一帧开头都装一次主题
// （`viewMain` 的第一行），而只画播放条的那一支不装——于是画出来的是**框架默认
// 那份主题**（主色是蓝的 #2563eb），而设置里的主色是绿的（#1ed760）。按绿的去找
// 主色像素，一个都找不到。
func playbarTester(t *testing.T, st app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	if height == 0 {
		height = coverWinH
	}
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// barState 是这一族用的那份状态：一首有歌、有时长、位置在 0 的。
//
// ⚠️ **位置必须是 0**：旋钮在 `frac=0` 那头，而填过的那一段轨道**悬停时**也是
// 主色（票 #218 之后）——位置不在 0 的话，「轨道上有一段主色」和「旋钮画出来了」
// 就分不开了（见 `TestTheProgressKnobIsOnlyDrawnWhileHovered`）。
func barState() app.State {
	return app.State{
		HasTrack: true, Title: "一首歌",
		Artist: "某个 UP 主", ArtistMid: 12345,
		Rate: 1, Volume: 0.8, PlayMode: app.PlayModeLoop,
		Duration: 100 * time.Second, Position: 0,
	}
}

// TestTheProgressBarGrowsWithTheWindow 断「窗口越宽，进度条越长」。
//
// ⚠️ **断的是单调，不是某个像素数**：进度条该有多长取决于窗口有多宽、也取决于
// 右边那一组有多宽（音量收成图标之后它窄了 141），所以钉死一个数的话，改动
// 右边任何一枚键都会把它顶红——而那不是回归。
//
// 中间那一档（1200）**必须**在：只断 900 和 1600 的话，「中间那一列不参与剩余
// 空间分配、于是两端一样宽」这种坏法在两端是看不出来的（两端都是 80）。
//
// 🔴 **票 #216 之后这条仍然成立，但机制换了**：进度条不再是「铺满中间那一列」，
// 而是中间那一列的 **3/4**（上游 `w-3/4`），而中间那一列是三列里的**三份**
// （上游的 1:3:1）。新机制下的确切长度由
// `TestTheMiddleColumnIsThreePartsWide` 那条钉着，这一条只管「越宽越长」。
func TestTheProgressBarGrowsWithTheWindow(t *testing.T) {
	var prevW float32
	var prevAt int
	for _, width := range []int{900, 1200, 1600} {
		tt, _ := playbarTester(t, barState(), width, 0, false)

		bar, ok := tt.Find("播放进度")
		if !ok {
			t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, tt.Texts())
		}
		if width != 900 && bar.W <= prevW {
			t.Errorf("宽度 %d：进度条只有 %.1f 宽，而宽度 %d 那档是 %.1f——"+
				"它没有跟着窗口变长", width, bar.W, prevAt, prevW)
		}
		prevW, prevAt = bar.W, width
	}
}

// TestTheTransportKeysArePrevPlayNext 断三键的**位置顺序**。
//
// ⚠️ **断的是谁在左谁在右，不是「三个键都在」**：三个键都在而次序反了，
// 别的测试全绿——而次序反了的后果是「用户从全屏退回播放条时，肌肉记忆里
// 「中间是播放」的位置按下去是下一首」。
//
// 上游的 `MusicPlayControl` 就是 `RiSkipBackFill` / `RiPlayCircleFill` /
// `RiSkipForwardFill`（`components/music-play-control/index.tsx:16-18`），
// 而**我们自己的**满屏播放器和迷你播放器也是这个次序。
func TestTheTransportKeysArePrevPlayNext(t *testing.T) {
	tt, _ := playbarTester(t, barState(), 1200, 0, false)

	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
	}
	next, ok := tt.Find("下一首")
	if !ok {
		t.Fatalf("画面上找不到「下一首」，文字有 %q", tt.Texts())
	}

	if !(prev.X < play.X && play.X < next.X) {
		t.Errorf("三键的次序是「上一首 %.1f · 播放 %.1f · 下一首 %.1f」——"+
			"播放键得在**中间**（上游的 `MusicPlayControl` 就是上一首 · 播放 · 下一首，"+
			"而我们自己的满屏播放器也是那个次序）", prev.X, play.X, next.X)
	}
}

// TestTheLeftColumnIsOneFifthOfTheBar 断左边那一列是五份里的一份。
//
// 上一条量的是「中间那一列和三份的关系」，这一条量的是**左边那一列自己**：
// 歌名的框宽 = 歌名那一列的宽 = 左边那一列的宽 − 封面 − 两道缝 − 第三段，
// 而**歌名够长时它的框就是整个容器那么宽**（`MaxLines(1)` + 省略号）。
//
// 🔴 **票 #219 之后左边那一列是三段**（上游 `playbar/left/index.tsx:29`）：
//
//	封面 │ 缝 │ 歌名·UP名 │ 缝 │ 分集·收藏·赞
//
// 所以歌名那一列要减掉**两道**缝（改之前只有一道——键在歌名下面那一行），
// 还要减掉**第三段**（三枚键横着并排的那一段）。
//
// ⚠️ **第三段的宽度是量出来的，不是写死的**：它是**跟着内容走的**（上游那一段
// 也是 `flex`）——单 P 的稿子少一枚分集键，那一段就窄 32，而歌名那一列跟着
// 宽 32。这个 tester 背后是一个空的 App，所以那一段里只有「收藏」和
// 「评论」两枚（分集要真的 App，赞也是）。
//
// ⚠️ **要歌名够长**：短歌名的框是它自己的自然宽，量的是字不是容器。
// ⚠️ **两档都要量**（1280 / 1400）：1280 上左边那一列是**内容撑出来的**
// （轨道 249.6 < 保底 307，`playbarLeftMinW`），1400 上也是（273.6 < 307）——
// 而**轨道说话**那一档要到窗口 ≥ 1567 才开始（307 × 5 + 32）。判据里那个
// `max(track, playbarLeftMinW)` 就是这两档的分界。
func TestTheLeftColumnIsOneFifthOfTheBar(t *testing.T) {
	st := barState()
	st.Title = "一首歌的名字很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长很长"

	for _, width := range []int{1280, 1400} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, st, width, 0, false)

			title, ok := tt.Find(st.Title)
			if !ok {
				t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
			}

			// 第三段有多宽：量画出来的那几枚键的跨度。
			//
			// ⚠️ **「评论」也在里面**（票 #573 之后第三段是四枚）：漏掉它的话
			// 量出来的第三段比真的窄——而歌名那一列的宽是拿第三段的宽算的，
			// 差出来的那一截全算到歌名头上。
			//
			// ⚠️ **它那一枚的名字可能带数字**（票 #682 起评论数进了名字）：这个
			// 测试背后是一个**空的 App**（没有 bvid，于是 `CommentCount()` 是 0、
			// 名字就是「评论」），但别把这里写死成只认那两个字——用
			// `findCommentsKey` 找，带不带数字都认得出来。
			segLo, segHi := float32(1e9), float32(-1e9)
			for _, k := range []string{"收藏", "赞"} {
				r, ok := tt.Find(k)
				if !ok {
					continue
				}
				segLo = min32(segLo, r.X)
				segHi = max32(segHi, r.X+r.W)
			}
			if r, ok := findCommentsKey(tt); ok {
				segLo = min32(segLo, r.X)
				segHi = max32(segHi, r.X+r.W)
			}
			if segHi < segLo {
				t.Fatalf("第三段那几枚键一枚都量不到，文字有 %q", tt.Texts())
			}

			got := title.W
			// 原型 `.bar`：三列 `1fr 1.3fr 1fr`，缝 24，左右内边距 20；左列里是
			// 封面 56 │ 缝 12 │ 歌名·UP 名 │ 缝 12 │ 赞·收藏·评论。
			track := (float32(width) - playbarPadX*2 - 2*24) / 3.3
			want := track - coverSize - 2*12 - (segHi - segLo)
			if absFloat(got-want) > 1 {
				t.Errorf("歌名那一列只有 %.1f 宽，而它该是 %.1f——"+
					"左边那一列是 1fr / 1.3fr / 1fr 里的第一份（原型 .bar），"+
					"而它里面是三段（封面 │ 歌名·UP名 │ 那一排键）", got, want)
			}
		})
	}
}

// absFloat 是两个浮点数相差多少（差一点点算 0）。
func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheLeftColumnKeysAreNotCoveredByTheMiddleColumn 断左列**撑出轨道**的那
// 一截上的键点得动。
//
// 🔴 **这一条钉的是票 #216 踩到的一个坑**：中间那一列在 Grid 里是左列的
// **后一个兄弟**，而框架的命中测试取**最上面**那个含点的元素
// （`mygo/ui/input.go:145` 从 hits 列表末尾往前找），于是中间那一列那整个框
// （900 宽下从 x=189.6 起）把左列撑出来的那一截盖住了——**收藏、赞、音质徽章
// 这些在左列右端的键点了没反应**，而**界面上看不出坏了**（它们好好地画在
// 那儿，一根汗毛都没少）。
//
// ⚠️ **临界宽度**：中间那一列的左沿 = 16 + (窗口 − 32) / 5，左列右端 =
// 16 + 307（`playbarLeftMinW`，票 #682 之后），两者一碰就是窗口 ≈ 1583。所以
// 这里量的是**最窄的那几档**（应用的窗口下限是 900）——那几档上左列整个撑出
// 轨道，右端的键全落在中间那一列的框底下。
//
// ⚠️ **判据是悬停**：指针停在键的正中心上，那句话要出来。悬停是命中链的第一
// 环——命中链错了，悬停和点击一起错，而**点击**更难断（点下去要弹东西，
// 弹的东西又是另一条路）。
func TestTheLeftColumnKeysAreNotCoveredByTheMiddleColumn(t *testing.T) {
	for _, width := range []int{900, 1000, 1100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			fav, ok := tt.Find("收藏")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到「收藏」，文字有 %q", width, tt.Texts())
			}
			tt.Move(fav.X+fav.W/2, fav.Y+fav.H/2)
			tt.Frame()
			if !tt.HasText(favTooltip(false)) {
				t.Errorf("宽度 %d：指针停在「收藏」上（x=%.1f..%.1f）却没有那句话——"+
					"它被中间那一列的命中框盖住了（那一片点了没反应，而界面上看不出坏了）",
					width, fav.X, fav.X+fav.W)
			}
		})
	}
}

// TestTheRightGroupHugsTheRightEdge 断右边那一组贴在播放条内容区的右沿，
// 而且**一枚键都没有跑到窗口外面去**。
//
// 🔴 **这一条钉的是票 #216 落地时踩到的一个坑**：第三列是 1 份（900 宽下
// 只有 173.6），而右边那一组是**定宽**的 243.2——框架给格子里那一块的宽度
// 是它**自己的内容宽**（于是比轨道宽），而横向排布在装不下的时候**一律
// 靠左排**，于是它从轨道的左沿开始排：最右边那枚「加歌」键落在 x=921.6，
// **而窗口只有 900**。那枚键看不见、点不着，**而界面上看不出坏了**（其余
// 几枚都好好地在那儿）。
//
// ⚠️ **断的是「最右边那枚键的右沿 == 内容区的右沿」**：内容区的右沿是窗口宽
// 减去右边那 16 的内边距（`playbarPadX`）。
//
// ⚠️ **竖直方向也断**：那一组在 76 高的播放条里**上下居中**（上游
// `playbar/right/index.tsx:13` 的 `items-center`）——它现在是绝对定位的，
// 高度得自己给，给歪了就会贴着上沿或下沿。
func TestTheRightGroupHugsTheRightEdge(t *testing.T) {
	for _, width := range []int{760, 900, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt, _ := playbarTester(t, barState(), width, 0, false)

			// 原型的右列最右是音量滑块（没有「加歌」）。
			last, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到「音量」那一枚，文字有 %q", width, tt.Texts())
			}

			wantX := float32(width - playbarPadX)
			if absFloat(last.X+last.W-wantX) > 1 {
				t.Errorf("宽度 %d：最右边那枚键的右沿在 %.1f，而内容区的右沿在 %.1f——"+
					"它跑到窗口外面去了（那一枚看不见也点不着）",
					width, last.X+last.W, wantX)
			}

			wantY := float32(playbarHeight) / 2
			if absFloat(last.Y+last.H/2-wantY) > 1 {
				t.Errorf("宽度 %d：那一枚键的垂直中心在 %.1f，而播放条的正中在 %.1f——"+
					"它没有上下居中", width, last.Y+last.H/2, wantY)
			}
		})
	}
}

// playKeyEdge 取播放键「左边中点」往里 2 像素处的颜色：圆形底在这里是实心的。
func playKeyEdge(tt *mygoui.Tester, btn mygoui.Rect) color.RGBA {
	return tt.Image().RGBAAt(round32(btn.X)+2, round32(btn.Y)+int(btn.H)/2)
}

// TestThePlayKeyIsBigEnoughToHit 断主播放键的尺寸（原型 `.play-btn`：44×44，正文色底的实心圆，
// 图形是页面底色）。
//
// ⚠️ **量的是画面上那一片圆，不是常量**，也不写 `iconHitPlay`（写成常量的话，
// 把常量改回 48 这一条照样绿——「44」得由测试里的字面数钉住）。
//
// 判据：
//
//  1. 盒子 44×44，比上一首大（只断盒子的话，一枚 44 的空盒子也算过）。
//  2. 🔴 **圆本身够大**（正文色那一片外接框 ≥ 42）——盒子对而圆画小了，前一条抓不到。
//  3. 🔴 **图形落在圆中间**（页面底色那一片的中心在圆心 ±3 内；播放三角的
//     字形自己偏右约 1.5，所以不要求死居中）。
func TestThePlayKeyIsBigEnoughToHit(t *testing.T) {
	st := barState()
	st.Playing = true
	tt, s := playbarTester(t, st, 1200, 0, false)
	tt.Move(600, 300) // 指针挪到播放条之外：悬停色会改变圆的颜色
	tt.Frame()

	play, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("画面上找不到「暂停」，文字有 %q", tt.Texts())
	}
	const wantBox = 44
	if d := absFloat(play.W-wantBox) + absFloat(play.H-wantBox); d > 2 {
		t.Errorf("播放键的盒子是 %.1fx%.1f，原型是 44×44", play.W, play.H)
	}
	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	if play.W <= prev.W {
		t.Errorf("播放键 %v 比「上一首」%v 还小——它得是全条上最大的一枚", play, prev)
	}

	th := BuildTheme(s.app.SettingsSnapshot(), false)
	pal := tok.Of(th)
	accent, on := rgb(pal.Text), rgb(pal.Bg)
	img := tt.Image()
	var dx0, dx1, gx0, gx1, gy0, gy1 = 1 << 30, -1, 1 << 30, -1, 1 << 30, -1
	for y := int(play.Y); y < int(play.Y+play.H); y++ {
		for x := int(play.X); x < int(play.X+play.W); x++ {
			switch p := img.RGBAAt(x, y); {
			case near(p, accent):
				dx0, dx1 = min(dx0, x), max(dx1, x)
			case near(p, on):
				gx0, gx1, gy0, gy1 = min(gx0, x), max(gx1, x), min(gy0, y), max(gy1, y)
			}
		}
	}
	if dx1 < 0 || dx1-dx0+1 < 42 {
		t.Fatalf("播放键上的圆只有 %d 宽（%d..%d）——原型是直径 44 的正文色实心圆", dx1-dx0+1, dx0, dx1)
	}
	if gx1 < 0 {
		t.Fatalf("播放键的圆里没有画出图形（找不到页面底色 %v）", on)
	}
	cx, cy := play.X+play.W/2, play.Y+play.H/2
	if d := absFloat(float32(gx0+gx1)/2-cx) + absFloat(float32(gy0+gy1)/2-cy); d > 4 {
		t.Errorf("播放键上的图形（%d..%d, %d..%d）偏离圆心 (%.1f,%.1f) 共 %.1f 像素", gx0, gx1, gy0, gy1, cx, cy, d)
	}
}

// TestThePlayKeyIsASolidAccentCircle 断主播放键是**实心圆**：原型 `.play-btn` 的底是
// `--text`（正文色），图形是 `--bg`。名字沿用旧的，台账里登记的就是它。
//
// ⚠️ **这条替换了原来的 `TestThePlayKeyHasNoSolidBackground`**——那条钉的是上游
// Biu 的形状（无底、前景色图形）。应用没发版，产品定了新视觉（设计规范 §5），
// 判据整个反过来：
//
//  1. 圆的边缘（左边中点往里 2 像素）是强调色；
//  2. 方盒的角是播放条底色——它是圆，不是圆角方块；
//  3. 图形是 AccentText。
//
// 播放 / 暂停两档、浅深两套都断。
func TestThePlayKeyIsASolidAccentCircle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, playing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/正在播放=%v", name, playing), func(t *testing.T) {
				st := barState()
				st.Playing = playing
				tt, s := playbarTester(t, st, 1200, 0, dark)
				tt.Move(600, 300)
				tt.Frame()

				label := "播放"
				if playing {
					label = "暂停"
				}
				btn, ok := tt.Find(label)
				if !ok {
					t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
				}
				th := BuildTheme(s.app.SettingsSnapshot(), dark)
				pal := tok.Of(th)
				bg, accent, on := rgb(th.Background), rgb(pal.Text), rgb(pal.Bg)

				if got := playKeyEdge(tt, btn); !near(got, accent) {
					t.Errorf("%q 的圆边缘是 %v，该是正文色 %v（原型 .play-btn）", label, got, accent)
				}
				x0, y0 := round32(btn.X), round32(btn.Y)
				x1, y1 := x0+int(btn.W)-1, y0+int(btn.H)-1
				for _, p := range [][2]int{{x0 + 1, y0 + 1}, {x1 - 1, y0 + 1}, {x0 + 1, y1 - 1}, {x1 - 1, y1 - 1}} {
					if got := tt.Image().RGBAAt(p[0], p[1]); !sameColor(got, bg) {
						t.Errorf("%q 的方盒角 (%d,%d) 是 %v，该是底色 %v——它应当是圆，不是方块", label, p[0], p[1], got, bg)
					}
				}
				// 图形的颜色：圆里离 AccentText 最近的那个像素。暂停的两道竖条只有
				// ~1.7 像素宽，抗锯齿后没有一个像素是纯色，所以不能取众数、也不能要求
				// 恰好等于——要求「最近的那个」落在 AccentText 附近（≤ 48/通道）。
				best := 1 << 30
				for y := int(btn.Y) + 8; y < int(btn.Y+btn.H)-8; y++ {
					for x := int(btn.X) + 8; x < int(btn.X+btn.W)-8; x++ {
						p := tt.Image().RGBAAt(x, y)
						best = min(best, absInt(int(p.R)-int(on.R))+absInt(int(p.G)-int(on.G))+absInt(int(p.B)-int(on.B)))
					}
				}
				if best > 3*48 {
					t.Errorf("%q 的圆里找不到 AccentText %v 色的图形（最近的像素也差 %d）", label, on, best)
				}
			})
		}
	}
}

// TestTheDisabledPlayKeyDoesNotTurnAccent 断**禁用**着的播放键**悬停时底不变**，
// 而且不是强调色（一枚禁用的键不该用强调色告诉用户「这里点得动」）。
//
// 原因在顺序：悬停是**建元素时**问的，禁不禁用要在问之前定下来（`iconButtonPlay`
// 的 `disabled` 参数，票 #217 的评审查出来过同款）。
func TestTheDisabledPlayKeyDoesNotTurnAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := barState()
			st.HasTrack = false // 没有歌 → 那一枚是禁用的
			tt, s := playbarTester(t, st, 1200, 0, dark)

			btn, ok := tt.Find("播放")
			if !ok {
				t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
			}
			tt.Move(600, 300)
			tt.Frame()
			idle := playKeyEdge(tt, btn)

			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			lit := playKeyEdge(tt, btn)

			if !sameColor(lit, idle) {
				t.Errorf("禁用着的播放键悬停之后底从 %v 变成了 %v", idle, lit)
			}
			if th := BuildTheme(s.app.SettingsSnapshot(), dark); near(lit, rgb(th.Accent)) || near(lit, rgb(th.AccentHover)) {
				t.Errorf("禁用着的播放键的底是 %v——不该是强调色", lit)
			}
		})
	}
}

// inkColor 是 box 里「不是底色」的那一片像素的众数色——**图形本身的颜色**。
//
// ⚠️ **判据和底色不一样**：这一枚底下什么都没有，所以播放条底色上「不是底色的
// 那一片」就是图形。众数而不是平均：抗锯齿那一圈是过渡色，平均出来谁也不像。
func inkColor(img *image.RGBA, box mygoui.Rect, bg color.RGBA) color.RGBA {
	var samples []color.RGBA
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if p := img.RGBAAt(x, y); distinctPixel(p, bg) {
				samples = append(samples, p)
			}
		}
	}
	if len(samples) == 0 {
		return color.RGBA{}
	}
	return modeOf(samples)
}

// inkExtent 是 box 里「不是底色」的那一片像素有多宽（左右沿和个数）。
//
// 判据是**和底色不一样**：播放键底下什么都没有，所以播放条底色上「不是底色的
// 那一片」就是图形（深色下白、浅色下近黑）。
//
// ⚠️ **左右沿是真的最小和最大**，不是「扫描顺序里第一个和最后一个」：扫描是逐行
// 从上往下的，一个**圆**（暂停那一枚就是 `RiPauseCircleFill`）的最后一行最窄，
// 于是「最后一个」落在圆的下沿（12 宽），而真正的左右沿是圆的最大处（40 宽）。
func inkExtent(img *image.RGBA, box mygoui.Rect, bg color.RGBA) (lo, hi, n int) {
	lo, hi = -1, -1
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if distinctPixel(p, bg) {
				if lo < 0 || x < lo {
					lo = x
				}
				if x > hi {
					hi = x
				}
				n++
			}
		}
	}
	return lo, hi, n
}

// TestTheProgressKnobIsOnlyDrawnWhileHovered 断旋钮只在悬停或按下时画。
//
// 上游是 `hideThumb={!showThumb}`（`music-play-progress`）：那颗圆点一直亮着的话，
// 它在整条进度上全程抢注意力，而它是用户真正要抓的东西。
//
// 判据是**画面上有没有主色的那一团**：旋钮是主色的，而没有悬停的时候那一块
// 只有 4 像素的轨道（`t.Border`）——两者分得开。
//
// ⚠️ **位置必须在 0**（`barState`）：填过的那一段轨道**悬停时**也是主色（票 #218
// 之后：不悬停是前景色，悬停才主色），位置不在 0 的时候「轨道上有一段主色」和
// 「旋钮画出来了」在像素上是一回事。
func TestTheProgressKnobIsOnlyDrawnWhileHovered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarTester(t, barState(), 1200, 0, dark)

			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			knob := knobSpot(bar)
			a := BuildTheme(s.app.SettingsSnapshot(), dark).Accent
			accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}

			if n := nearPixels(tt.Image(), knob, accent); n != 0 {
				t.Errorf("没有悬停的时候旋钮却画出来了（%v 那一块有 %d 个接近主色的像素）",
					knob, n)
			}

			// 悬停到进度条上。
			tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
			tt.Frame()

			if n := nearPixels(tt.Image(), knob, accent); n == 0 {
				t.Errorf("悬停之后旋钮没有画出来（%v 那一块一个接近主色的像素都没有）", knob)
			}
		})
	}
}

// knobSpot 是旋钮该在的那一块。
//
// ⚠️ **位置是算出来的，不是量出来的**：滑块的左边沿是**边框框**（含内边距），
// 而旋钮的中心在「内容框左边沿 + 半个旋钮」——内边距留的就是那半个旋钮
// （框架的规矩，`coreSlider` 的 `PaddingX(kw / 2)`），于是旋钮的中心正好是
// 「滑块左边沿 + 一个旋钮」。
func knobSpot(bar mygoui.Rect) mygoui.Rect {
	kw := knobHoverLook.knobW
	return mygoui.Rect{
		X: bar.X + kw - kw/2,
		Y: bar.Y + (bar.H-kw)/2,
		W: kw, H: kw,
	}
}

// nearPixels 数 box 里有多少个像素接近 want。
//
// 门槛是**每个通道各差 24 以内**：主色是 #1ed760，而抗锯齿的边上的像素是它和
// 底色混出来的——差得远的那些不算。
func nearPixels(img *image.RGBA, box mygoui.Rect, want color.RGBA) int {
	const slack = 24
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(want.R)) <= slack &&
				absInt(int(p.G)-int(want.G)) <= slack &&
				absInt(int(p.B)-int(want.B)) <= slack {
				n++
			}
		}
	}
	return n
}

// TestClickingTheVolumeIconMutes 断「点图标静音」真的接上了。
//
// ⚠️ **静音是它自己的一格**（票 #311 / 报告 §3.3 的 B2）：静音的时候**音量那一格
// 一动不动**——取消静音放回去的就是它（落盘的也是它）。而「真正没声」靠的是
// 那一格布尔，不是把音量改成 0：合成一档的话，落盘之后「静音后退出再启动，
// 音量变成 0」就成了必然（Clementine 的老毛病）。
//
// 再点一下要**放回去**：放回去的是静音之前那个音量（80%），不是 50%。
func TestClickingTheVolumeIconMutes(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	before := h.app.Snapshot().Volume
	if before <= 0 {
		t.Fatalf("起点就不对：音量是 %v", before)
	}

	if err := h.tt.Click("静音"); err != nil {
		t.Fatalf("点不动音量那一枚：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	st := h.app.Snapshot()
	if !st.Muted {
		t.Error("点了音量图标之后没有静音")
	}
	if st.Volume != before {
		t.Errorf("点了音量图标之后音量变成了 %v，want %v——静音不该动音量那一格",
			st.Volume, before)
	}

	// 静音了之后那一枚换了个名字（上游的 `aria-label={isMuted ? "取消静音" : "静音"}`）。
	if err := h.tt.Click("取消静音"); err != nil {
		t.Fatalf("点不动「取消静音」：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	st = h.app.Snapshot()
	if st.Muted {
		t.Error("再点一下还是静音的")
	}
	if st.Volume != before {
		t.Errorf("取消静音之后音量是 %v，want %v——它没有放回静音之前那个音量",
			st.Volume, before)
	}
}

// TestAMutedPlayerStillKeepsItsVolume 断「静音的时候那一格音量还在」。
//
// ⚠️ **这一条是「静音独立成布尔」在画面上的样子**：用户为了不吵把音量调到 20%，
// 按了静音——取消静音放回去的必须是 20%，而不是「不知道是多少」。
//
// 而图标那一枚说的是「按下去会怎样」：静音的时候它是「取消静音」。
func TestAMutedPlayerStillKeepsItsVolume(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	h.app.SetVolume(0.2)
	h.tt.Frame()

	if err := h.tt.Click("静音"); err != nil {
		t.Fatalf("点不动音量那一枚：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()

	if _, ok := volumeKeyOnTheBar(h.tt); !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", h.tt.Texts())
	}
	if got := h.app.Snapshot().Volume; got != 0.2 {
		t.Errorf("静音之后音量是 %v，want 0.2——静音把音量吃掉了", got)
	}
}

// TestRenderPlayBarRedoShots 存图：播放条重做之后的样子，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarRedoShots
//
// ⚠️ **深浅两套都要出**：旋钮是**主色**的，而播放条的底在浅色下就是白的
// （`t.Background` = `#ffffff`）——浅色那一套才看得出「一颗白旋钮落在白底上」
// 那种坏（那正是旋钮不能画成白色的原因）。
//
// ⚠️ **窗口只留 76 高**（播放条那一行）：整窗的截图在 #127 那一族里已经有了
// （`chrome_shot_test.go` 的 01-整窗-），这一族看的是播放条**自己**。
func TestRenderPlayBarRedoShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, width := range []int{1200, 1600} {
			tt, _ := playbarTester(t, barState(), width, playbarHeight, dark)
			// 指针停在进度条上：旋钮只在悬停或按下时画。
			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			tt.Move(bar.X+bar.W*0.35, bar.Y+bar.H/2)
			tt.Frame()
			save(t, dir, fmt.Sprintf("70-播放条-%d-%s.png", width, name), tt)
		}

		// 音量浮层开着：一根竖滑块 + 底下的百分比。
		//
		// ⚠️ **这一张把播放条摆在窗口底下**（真窗口里它就在那儿）：浮层是从
		// 播放条**往上**弹的，摆在顶上就被窗口裁掉半张。
		tt, _ := bottomBarTester(t, barState(), 1200, 320, dark)
		hoverVolumeKey(t, tt)
		save(t, dir, "72-播放条-音量浮层-"+name+".png", tt)

		// 封面悬停：那一层黑罩和它正中那枚箭头（票 #173 把它放到 32）。
		//
		// ⚠️ **必须悬停才有**：那一层是 `wrap.Hovered()` 才画的，而箭头是**白色
		// 92%** 压在 50% 的黑罩上——不悬停就只剩一块空的封面。
		tt, _ = playbarTester(t, barState(), 1200, playbarHeight, dark)
		cover, ok := tt.Find("当前封面")
		if !ok {
			t.Fatalf("画面上找不到封面，文字有 %q", tt.Texts())
		}
		tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
		tt.Frame()
		save(t, dir, "71-播放条-封面悬停-"+name+".png", tt)
	}
}

// TestRenderPlayKeyShots 存图：播放键改成上游那一枚之后的样子，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayKeyShots
//
// ⚠️ **四张都要**：平时（图形是**前景色**）、悬停（图形**变主色**）、正在播放
// （形状换成 `RiPauseCircleFill`）、正在播放时悬停——「悬停变主色」是这一票的
// 一半，而**平时那一张和悬停那一张长得完全不一样**。
//
// 🔴 **每一张的文件名都要带上状态**（评审指出来的）：第一版把「悬停」那一张
// **写了两遍**（两档状态都写同一个文件名），后写的那一遍把先写的盖掉了——于是
// 报告里那张标着「悬停」的图**其实是「正在播放」那一档**，而「悬停 + 播放三角」
// 那一张**根本不存在**。这一条现在每一张的名字里都有状态。
//
// ⚠️ **窗口留 160 高、指针停在中段**：这一族只画播放条（`playbarTester`），而它
// 摆在窗口的**顶上**（那 76 高），所以窗口的中段是空的——指针要停在那儿才算
// 「不在任何一枚键上」（三键是居中的，x=600 就是中间那一枚）。
func TestRenderPlayKeyShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, playing := range []bool{false, true} {
			st := barState()
			st.Playing = playing
			label := "播放"
			idle, hover := "73-播放键-平时-"+name, "74-播放键-悬停-"+name
			if playing {
				label = "暂停"
				idle, hover = "75-播放键-正在播放-"+name, "76-播放键-正在播放时悬停-"+name
			}

			tt, _ := playbarTester(t, st, 1200, 160, dark)
			tt.Move(600, 130) // 播放条之外
			tt.Frame()
			save(t, dir, idle+".png", tt)

			// 悬停：图形变主色。
			btn, ok := tt.Find(label)
			if !ok {
				t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
			}
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			save(t, dir, hover+".png", tt)
		}
	}
}

// bottomBarTester 画一条**摆在窗口底下**的播放条。
//
// ⚠️ **播放条在真窗口里是贴着底的**（`viewMain` 的最后一块），而音量浮层是
// 从它**往上**弹的——只画播放条的那一支把它摆在窗口顶上，于是浮层有一半在
// 窗口外面。这一支是给存图用的。
func bottomBarTester(t *testing.T, st app.State, width, height int, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			mygoui.Column(c).Fill().Children(func() {
				mygoui.Row(c).Grow(1)
				s.viewPlayBar(c, st)
			})
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// hoverVolumeKey 把指针停在音量图标上，等那 300 毫秒过去（浮层是悬停**一会儿**
// 才出来的，上游那个 Tooltip 的 `delay={300}`）。
//
// ⚠️ **必须推钟**：无头 Tester 的钟站着不动（只有 `Advance` 能推它），而那
// 300 毫秒是框架的**动画**在走——不推钟它永远走不完，于是浮层永远不出来。
// 这一条是「用 `tt.Advance(d)` 配 `tt.FramePending()`」那个写法（CLAUDE.md §2）。
func hoverVolumeKey(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	icon, ok := volumeKeyOnTheBar(tt)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", tt.Texts())
	}
	tt.Move(icon.X+icon.W/2, icon.Y+icon.H/2)
	tt.Advance(volHoverDelay)
	tt.Frame()
}
