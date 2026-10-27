package hover

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// 这一族量**这一包自己画出来的东西**：三样反馈的位置和大小。
//
// ⚠️ 为什么包内也要有一份，而不只是靠 `widgets/list` 和 `internal/ui` 两族从外面验：
// 外面那两族验的是「接上去以后界面长什么样」，而这一族量的是「**画出来那一层**
// 自己有没有落在该在的地方」——尺寸那一组（20 / 40 / 35% / 110% / 200ms）是上游抄来的
// 死数，而死数最容易被下一个人顺手改小一点：改成 14 之后，从外面看只是「三角小了一点」，
// 没有一条断言会红。
//
// ⚠️ 全部走无头渲染，**没有网络**：封面地址传空串（imgcache 画空框那一支）。
// 指针事件走的是真实的 `tt.Move` + `tt.Click`，不是回调。

const (
	testerW = 400
	testerH = 300
)

// newCache 造一个封面缓存。AddWake 不能省（理由同 `widgets/list` 的 newCache）。
func newCache(t *testing.T) *imgcache.Cache {
	t.Helper()
	c := imgcache.New(imgcache.Options{})
	c.AddWake(func() {})
	t.Cleanup(c.Close)
	return c
}

// coverTester 造一个只画一块封面的画面。
func coverTester(t *testing.T, cache *imgcache.Cache, draw func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 60).Padding(6).Children(func() { draw(c) })
	}, testerW, testerH)
	tt.Frame()
	return tt
}

// settle 推满一次淡入 / 淡出并画完那一刻的画面（理由见 hover.go 的 wavePhase）。
func settle(tt *ui.Tester) {
	tt.Advance(Fade)
	tt.Frame()
}

// TestRowCoverPutsATriangleInTheMiddleOfTheCover 钉住「封面上盖一层压暗 + 正中间一个
// 20px 的白色三角」。
//
// ⚠️ **量三角的盒子，不量封面**：封面是调用方给的（48 / 32），而这一包该负责的是那个
// 20。封面那一圈压暗和三角都是 `Attach` 在封面上的，所以「在中间」这一条量的就是
// 三角盒子的中心对不对得上封面的中心。
func TestRowCoverPutsATriangleInTheMiddleOfTheCover(t *testing.T) {
	const cover = 48
	cache := newCache(t)
	tt := coverTester(t, cache, func(c *ui.Context) {
		RowCover(c, cache, "", cover, true, "甲", nil)
	})

	tri, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("封面上没有那个播放三角，文字有 %q", tt.Texts())
	}
	if tri.W < 19 || tri.H < 19 {
		t.Errorf("三角只有 %.1f×%.1f，want 20×20", tri.W, tri.H)
	}
	// 封面的盒子就是画面里那个 60 的盒子（`coverTester` 留了 6 的边）。
	if want := 6 + (cover-tri.W)/2; tri.X < want-1 || tri.X > want+1 {
		t.Errorf("三角在 x=%.1f，封面正中间那一列是 %.1f——它没有居中", tri.X, want)
	}
}

// TestRowCoverHidesEverythingWhenOff 钉住「指针不在时那一块完全收掉」。
//
// ⚠️ 断的是 **`Find` 找不到**（而不只是「看不见」）：`Invisible()` 的原话之一是「has no
// text to find」，而那正是无头 Tester 能判「它不在」的唯一依据——`Opacity(0)` 那一种
// 「还在树上、只是透明」在 Tester 眼里是有标签的（见 hover.go 的 show）。
func TestRowCoverHidesEverythingWhenOff(t *testing.T) {
	cache := newCache(t)
	tt := coverTester(t, cache, func(c *ui.Context) {
		RowCover(c, cache, "", 48, false, "甲", nil)
	})
	if _, ok := tt.Find(FromLabel("甲")); ok {
		t.Errorf("指针不在封面上就有那个三角的标签了：%q", tt.Texts())
	}
}

// TestRowCoverGrowsTheTriangleOnHover 钉住「指针停在三角上时它放大到 110%」。
//
// ⚠️ 量得到是因为 `Tester.Find` 给回的是**变换后**的盒子（`mygo/ui/runtime.go` 的
// `visualRect`），而 `Scale` 恰好不改布局——所以这一条量的是画出来的大小。
//
// ⚠️ **必须推钟**：那一档放大**现在有动画了**（以前是瞬时的，每帧直接算 `Scale`），
// 而 Tester 的钟站着不动（`mygo/ui/headless.go`）——不推的话量到的是「放大还没
// 开始」，那一帧量出来是 1.0 倍。
func TestRowCoverGrowsTheTriangleOnHover(t *testing.T) {
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 60).Padding(6).Children(func() {
			RowCover(c, cache, "", 48, on, "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200) // 指针先落在画面外
	tt.Frame()

	on = true
	tt.Frame()
	settle(tt)
	rest, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针进来之后没有那个三角：%q", tt.Texts())
	}

	// 指针挪到三角的正中间。
	tt.Move(rest.X+rest.W/2, rest.Y+rest.H/2)
	tt.Frame()
	tt.Advance(motion.GrowTime)
	tt.Frame()
	grown, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针停在三角上时它不见了：%q", tt.Texts())
	}
	if want := rest.W * motion.GrowScale; grown.W < want-1 {
		t.Errorf("指针停在上面时它只有 %.1f 宽，want %.1f（上游的 110%%）", grown.W, want)
	}
}

// TestTheTriangleFadesInOverTwoHundredMilliseconds 钉住淡入的时长。
//
// ⚠️ **必须推钟**：Tester 的钟站着不动（`mygo/ui/headless.go`），不推的话这一步一步也
// 不走，而那一帧看起来是「淡入已经完成了」。
func TestTheTriangleFadesInOverTwoHundredMilliseconds(t *testing.T) {
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 60).Padding(6).Children(func() {
			RowCover(c, cache, "", 48, on, "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(30, 30)
	tt.Frame()
	tt.Frame()

	if _, ok := tt.Find(FromLabel("甲")); ok {
		t.Fatalf("指针移过去的第一帧就有那个三角了——那不是淡入，是「啪」地一下出现")
	}
	on = true
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find(FromLabel("甲")); ok {
		t.Fatalf("刚建出来那一帧就有那个三角了（那一帧淡入还是 0）")
	}
	settle(tt)
	if _, ok := tt.Find(FromLabel("甲")); !ok {
		t.Fatalf("推满 %v 之后那个三角不见了", Fade)
	}
}

// TestCardCoverPutsThePlayButtonAtTheBottomRight 钉住网格那一块：右下角一个 40 的主色圆
// 播放钮，离封面右边和下边各 8。
func TestCardCoverPutsThePlayButtonAtTheBottomRight(t *testing.T) {
	const coverW, coverH = 120, 140
	cache := newCache(t)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(coverW+12, coverH+12).Padding(6).Children(func() {
			CardCover(c, cache, "", coverW, coverH, true, "1.2万", "3:05", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()
	tt.Move(6+coverW/2, 6+coverH/2)
	tt.Frame()
	tt.Frame()
	settle(tt)

	btn, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("封面右下角没有那个播放钮：%q", tt.Texts())
	}
	if btn.W < 38 || btn.H < 38 {
		t.Errorf("那个播放钮只有 %.1f×%.1f，want 40×40（上游 `music-card/index.tsx:39-47`）",
			btn.W, btn.H)
	}
	if wantRight := float32(6 + coverW - btnInset); btn.X+btn.W < wantRight-1 {
		t.Errorf("播放钮的右边缘在 %.1f，封面的右边减去 %d 才是它该在的位置 %.1f",
			btn.X+btn.W, btnInset, wantRight)
	}
	if wantBottom := float32(6 + coverH - btnInset); btn.Y+btn.H < wantBottom-1 {
		t.Errorf("播放钮的下边缘在 %.1f，封面的下边减去 %d 才是它该在的位置 %.1f",
			btn.Y+btn.H, btnInset, wantBottom)
	}
}

// TestCardCoverLeavesTheDurationToTheLeftOfTheButton 钉住渐变条上的字：右半边是时长，
// 而且**让开了那个播放钮**。
func TestCardCoverLeavesTheDurationToTheLeftOfTheButton(t *testing.T) {
	const coverW, coverH = 120, 140
	cache := newCache(t)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(coverW+12, coverH+12).Padding(6).Children(func() {
			CardCover(c, cache, "", coverW, coverH, true, "1.2万", "3:05", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()
	tt.Move(6+coverW/2, 6+coverH/2)
	tt.Frame()
	tt.Frame()
	settle(tt)

	for _, want := range []string{"1.2万", "3:05"} {
		if _, ok := tt.Find(want); !ok {
			t.Fatalf("渐变条上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	clock, _ := tt.Find("3:05")
	btn, _ := tt.Find(FromLabel("甲"))
	if clock.X >= btn.X {
		t.Errorf("时长在 x=%.1f、播放钮在 x=%.1f——那一半压在按钮底下了", clock.X, btn.X)
	}
	count, _ := tt.Find("1.2万")
	if count.X >= clock.X {
		t.Errorf("播放量在 x=%.1f、时长在 x=%.1f——上游那一条是**左播放量、右时长**",
			count.X, clock.X)
	}
}

// TestCardCoverLeavesOutWhatItWasNotGiven 钉住「空串就是那一半不画」。
//
// 本地音乐和历史记录没有播放量（见 `list.Music.View`），而用户看见半行字会以为那一格
// 「数据不全」。
func TestCardCoverLeavesOutWhatItWasNotGiven(t *testing.T) {
	cache := newCache(t)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(132, 152).Padding(6).Children(func() {
			CardCover(c, cache, "", 120, 140, true, "", "3:05", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()
	tt.Move(60, 60)
	tt.Frame()
	tt.Frame()
	settle(tt)

	if _, ok := tt.Find("3:05"); !ok {
		t.Fatalf("连时长都没有：%q", tt.Texts())
	}
	if _, ok := tt.Find("0"); ok {
		t.Errorf("没有播放量却画了一个「0」：%q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 悬停放大 / 弹出来
// ─────────────────────────────────────────────────────────────────────────────

// changedPixels 数两张图在 box 里不一样的像素有几个。
func changedPixels(a, b *image.RGBA, box ui.Rect) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// TestTheCardCoverGrowsOnHover 钉住「网格卡片那张封面在悬停时放大」。
//
// ⚠️ **量的是像素，不是盒子**：封面那一格没有标签（`Tester.Find` 认不出来），
// 而放大的是**框里那张图**——外面那个框不动（上游那个 `overflow-hidden` 的框
// 也不动，见 `widgets/motion/hoverzoom.go`）。
//
// ⚠️ **量的那一块是封面的上半截**：下半截有封面底部那条渐变和右下角那枚圆钮，
// 它们淡入的时候也改像素——量到那儿就分不出「放大」和「淡入」了。而上半截
// 只有封面自己（那个「歌名首字 + 渐变」的占位），它变了就是放大画出来了。
//
// ⚠️ **封面不能是纯色**：一块纯色放大 5% 在画面上一个像素都不变。这里的占位
// 画的是「首字 + 渐变」，那个字够大（0.44 × 边长），放大 5% 之后它的边挪了
// 好几像素。
func TestTheCardCoverGrowsOnHover(t *testing.T) {
	const coverW, coverH = 120, 140
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(coverW+12, coverH+12).Padding(6).Children(func() {
			CardCover(c, cache, "", coverW, coverH, on, "", "", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200) // 指针先落在画面外
	tt.Frame()
	before := tt.Image()

	on = true
	tt.Frame()
	tt.Advance(motion.HoverZoomTime)
	tt.Frame()
	after := tt.Image()

	top := ui.Rect{X: 6, Y: 6, W: coverW, H: coverH / 2}
	if n := changedPixels(before, after, top); n < 20 {
		t.Errorf("指针移上去之后那张封面的上半截只有 %d 个像素变了——"+
			"悬停放大（%v）没画出来", n, motion.HoverZoomScale)
	}
}

// TestTheCardCoverZoomStaysInsideItsCorners 钉住「放大的封面被圆角裁住」。
//
// ⚠️ **判据是那两个角上一个像素都不许变**：卡片有圆角，不裁的话画面上是「封面
// 从圆角里长出来」——而那在画面上只是「角上多了一块颜色」，一眼看不出是哪儿
// 错了。
//
// ⚠️ **只断上面那两个角**：下面那两个角上有一枚 40 的圆钮和它那圈 `shadow-2xl`
// 的影（影落在盒子外面，会漫到角上），它们淡入的时候也改像素。
func TestTheCardCoverZoomStaysInsideItsCorners(t *testing.T) {
	const coverW, coverH = 120, 140
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(coverW+12, coverH+12).Padding(6).Children(func() {
			CardCover(c, cache, "", coverW, coverH, on, "", "", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()
	before := tt.Image()

	on = true
	tt.Frame()
	tt.Advance(motion.HoverZoomTime)
	tt.Frame()
	after := tt.Image()

	for _, c := range []struct {
		name string
		x, y int
	}{
		{"左上角", 6, 6},
		{"右上角", 6 + coverW - 1, 6},
	} {
		if before.RGBAAt(c.x, c.y) != after.RGBAAt(c.x, c.y) {
			t.Errorf("%s那个像素在 hover 之后变了（%v → %v）——"+
				"放大没有被圆角裁住，画面上是「封面从圆角里长出来」",
				c.name, before.RGBAAt(c.x, c.y), after.RGBAAt(c.x, c.y))
		}
	}
}

// TestThePlayButtonPopsOut 钉住「网格卡右下角那枚 40 的圆钮是**弹**出来的」。
//
// ⚠️ **量的是画出来的大小**：`Tester.Find` 给回的是**变换后**的盒子，而 `Scale`
// 不改布局——所以量的是那一枚钮画出来多大。
//
// ⚠️ **「弹」和「啪地一下出现」的区别就在头几帧**：淡入是 200ms，而缩放是
// 150ms——指针移过去之后的头一帧，那一枚钮**已经能看见**（淡入有了一点点），
// 但它**还没长到位**（85%）。两样都到位了之后它是 40。
func TestThePlayButtonPopsOut(t *testing.T) {
	const coverW, coverH = 120, 140
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(coverW+12, coverH+12).Padding(6).Children(func() {
			CardCover(c, cache, "", coverW, coverH, on, "", "", "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()

	on = true
	tt.Frame()
	tt.Advance(10 * time.Millisecond)
	tt.Frame()
	knob, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针移上去之后没有那个播放钮：%q", tt.Texts())
	}
	if knob.W >= btn-1 {
		t.Errorf("那枚钮刚出现时就已经是 %.1f 宽了——它是「啪」地一下出现的，不是弹出来的", knob.W)
	}

	tt.Advance(motion.PopTime)
	tt.Frame()
	knob, ok = tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("弹完之后那枚钮不见了：%q", tt.Texts())
	}
	if knob.W < btn-1 {
		t.Errorf("弹完之后那枚钮只有 %.1f 宽，want %d（弹到位就是满的）", knob.W, btn)
	}
}

// TestTheTrianglePopsOut 钉住「列表行封面上那枚三角是**弹**出来的」。
//
// ⚠️ 判据同 `TestThePlayButtonPopsOut`：头一帧它已经看得见（淡入有了一点），
// 但还没长到位（85%）；弹完是 20。
func TestTheTrianglePopsOut(t *testing.T) {
	cache := newCache(t)
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 60).Padding(6).Children(func() {
			RowCover(c, cache, "", 48, on, "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	tt.Move(200, 200)
	tt.Frame()

	on = true
	tt.Frame()
	tt.Advance(10 * time.Millisecond)
	tt.Frame()
	tri, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针移上去之后没有那个三角：%q", tt.Texts())
	}
	if tri.W >= triSize-1 {
		t.Errorf("那枚三角刚出现时就已经是 %.1f 宽了——它是「啪」地一下出现的，不是弹出来的", tri.W)
	}

	tt.Advance(motion.PopTime)
	tt.Frame()
	tri, ok = tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("弹完之后那枚三角不见了：%q", tt.Texts())
	}
	if tri.W < triSize-1 {
		t.Errorf("弹完之后那枚三角只有 %.1f 宽，want %d（弹到位就是满的）", tri.W, triSize)
	}
}

// TestWaveDrawsBarsThatMove 钉住「波形是跳动的，而且画在它那个盒子里」。
//
// ⚠️ **必须推钟**：Tester 的钟站着不动，而波形是拿钟算相位的（见 hover.go 的
// wavePhase）。
func TestWaveDrawsBarsThatMove(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(60, 30).Padding(8).Children(func() {
			Wave(c, "wave", 40, ui.RGB(0, 0, 255), "甲")
		})
	}, testerW, testerH)
	tt.Frame()
	wave, ok := tt.Find(PlayingLabel("甲"))
	if !ok {
		t.Fatalf("那个波形没有标签：%q", tt.Texts())
	}
	first := bars(t, tt, wave)
	if len(first) == 0 {
		t.Fatalf("那个盒子里一根竖条都没画：%+v", wave)
	}

	tt.Advance(wavePeriod / 2)
	tt.Frame()
	wave, _ = tt.Find(PlayingLabel("甲"))
	if got := bars(t, tt, wave); sameAs(first, got) {
		t.Errorf("推了半轮之后那几根竖条一根都没动过（%v）——那是一段画上去就不动的竖条",
			got)
	}
}

// TestTheWaveKeepsMovingOnRepaintFrames 钉住「波形在**只重画**的那些帧里照跳」。
//
// ## 为什么这一条最要紧
//
// 波形改走**局部重绘**之后（票 #392：`p.AnimationFrame()`，见 hover.go 的
// `Wave`），而局部重绘那一帧**不重建**（`mygo/ui/runtime.go` 的 `repaintFrame`）。
// 于是相位（拿钟算的那一个）有两个地方可以算：
//
//   - **画的时候**算 → 每一帧都是新的 → 波形照跳；
//   - **建的时候**算 → 闭包捕获住 → 局部重绘那一帧里它**被冻住** →
//     **波形一动不动**。
//
// ⚠️ **后者不报错、不崩溃、画面上只是「不跳了」**，而上面那条
// `TestWaveDrawsBarsThatMove` **抓不住它**——那条量的是 `tt.Frame()` 之后的样子，
// 而 Tester 的 `Frame()` **每次都是整棵元素树重建一遍**（`mygo/ui/headless.go` 的
// `settle()` 调的是 `runFrame`），于是相位在**建的时候**重算，条子照样动。
//
// 所以这一条量的必须是「**两段纯局部重绘的帧之间**」：那几帧**不重建**，于是
// 只有「相位在画的时候算」才可能让条子动。
//
// ## 怎么让 Tester 画出「纯局部重绘的帧」
//
// 无头 Tester 是**拉式**的（不 `Frame()` 一帧都不画），而 `Advance` 只画
// 「有人要了帧」的那些时刻。于是：
//
//  1. `ctx.Invalidate()`——一次**外面来的**唤醒（模拟 `App.tick`）：它要的那一帧
//     是**重建**（框架把 `rt.redraw` 清了）。
//  2. `Advance` 跑起来之后，波形自己要的那些帧走**局部重绘**（`rt.redraw` 是
//     真的），于是**重建的次数是 0**——这一条量的时候要**先断这个**：不然量到的
//     条子高度可能是「重建时算的相位」，而那不是这一条要验的东西。
func TestTheWaveKeepsMovingOnRepaintFrames(t *testing.T) {
	builds := 0
	var ctx *ui.Context
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		ctx = c
		ui.Box(c).Size(60, 30).Padding(8).Children(func() {
			Wave(c, "wave", 40, ui.RGB(0, 0, 255), "甲")
		})
	}, testerW, testerH)
	tt.Frame()
	wave, ok := tt.Find(PlayingLabel("甲"))
	if !ok {
		t.Fatalf("那个波形没有标签：%q", tt.Texts())
	}

	// 一次「外面来的」唤醒（模拟 `App.tick`：放歌时它每 50ms 叫醒每个窗口一次）。
	// ⚠️ **没有它就没有帧**：Tester 是拉式的，不有人要帧它一帧都不画。
	ctx.Invalidate()
	// ⚠️ **推四分之一轮**：相位整整走了 90°，那四根条子的高度**必然**变
	// （正弦在 90° 上取到极值，而极值本身就和起点的值不同）——推得更少的话，
	// 相位正好落在极值附近时条子可能看不出动，那条测试就成了「有时绿有时红」。
	tt.Advance(wavePeriod / 4)

	// 下面这一段是**纯局部重绘**：重建的次数必须是 0。
	before := builds
	first := bars(t, tt, wave)
	tt.Advance(wavePeriod / 4)
	if got := builds - before; got != 0 {
		t.Fatalf("这一段重建了 %d 次整棵元素树，want 0——"+
			"下面量的就不是「只重画的那几帧」了（重建的时候相位也会重算，"+
			"那样这条就验不到相位是不是在画的时候算的）", got)
	}
	wave, _ = tt.Find(PlayingLabel("甲"))
	second := bars(t, tt, wave)
	if sameAs(first, second) {
		t.Errorf("波形在纯局部重绘的那一段里一动不动：%v → %v——相位被冻住了。\n"+
			"相位**必须在画的时候算**（`p.Now()`），不能在建的时候算完塞进闭包：\n"+
			"局部重绘那一帧**不重建**，建的时候算出来的值会被**冻结**"+
			"（见 hover.go 的 `Wave` 和 `wavePhase`）。",
			first, second)
	}
}

// bars 是那个盒子里**每一列画出来多少像素**（跳动的就是这些数——条子的横坐标不动，
// 上下两端在动）。
//
// ⚠️ 量的是**每列的高度**而不是「有几列被画了」：后者在四根条子都还在的时候永远是
// 8，于是「一动也不动」和「在跳」量出来是同一个数（第一版就是量了它，恒绿）。
func bars(t *testing.T, tt *ui.Tester, wave ui.Rect) []int {
	t.Helper()
	img := tt.Image()
	x0, x1 := int(wave.X), int(wave.X+wave.W)
	y0, y1 := int(wave.Y), int(wave.Y+wave.H)
	bg := img.RGBAAt(x0, y0+int(wave.H/2))
	var out []int
	for x := x0; x < x1; x++ {
		n := 0
		for y := y0; y < y1; y++ {
			if diff(img.RGBAAt(x, y), bg) > 40 {
				n++
			}
		}
		if n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func sameAs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diff 是两个颜色差多远（三通道绝对差之和）。
//
// ⚠️ **不能只比一个通道**：条子是主色，而底色在有些主题下也偏红或偏绿，只比一个通道的
// 时候「条子有多高」量的是两个相近的数之差，差多少全看主题。
func diff(a, b color.RGBA) int {
	return absI(int(a.R)-int(b.R)) + absI(int(a.G)-int(b.G)) + absI(int(a.B)-int(b.B))
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestReduceMotionMakesTheFadeArriveAtOnce 钉住「系统开着减少动态效果时，
// 我们自己的淡入直接到位」。
//
// 这一包的淡入走框架的 `Animate`（`hover.go` 的 `fade`），框架在
// `Preferences.ReduceMotion` 时把值直接给到目标（`mygo/ui/motion.go:322`）——
// 所以这一条测的是**这条路上真的接上了**：第一帧就满，而不是「等 200ms」。
//
// 两条必须一起验：
//   - 默认（没开减少动态）：第一帧**还没到满**——不然「直接到位」这一条会因为
//     「根本没有淡入」而白白地绿；
//   - 开了之后第一帧就是满的，而且和默认那条走完时画出来的一样（同一个终点）。
func TestReduceMotionMakesTheFadeArriveAtOnce(t *testing.T) {
	const cover = 48
	// 封面在 60 的盒子里留了 6 的边，中心就是 (30,30)。
	const cx, cy = 30, 30

	firstFrame := func(reduce bool) (base, first, settled color.RGBA) {
		cache := newCache(t)
		on := false
		tt := coverTester(t, cache, func(c *ui.Context) {
			RowCover(c, cache, "", cover, on, "甲", nil)
		})
		if reduce {
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		}
		tt.Frame()
		base = tt.Image().RGBAAt(cx, cy)

		on = true
		tt.Frame()
		first = tt.Image().RGBAAt(cx, cy)

		settle(tt)
		settled = tt.Image().RGBAAt(cx, cy)
		return base, first, settled
	}

	base, first, settled := firstFrame(false)
	if diff(first, base) >= 10 {
		t.Fatalf("默认那一档的第一帧已经画满了（%v → %v）——这一条就没有对照，"+
			"「减少动态效果」那一半会白白地绿", base, first)
	}
	if diff(settled, base) < 20 {
		t.Fatalf("淡入走完之后画面没有变化（%v → %v），测试本身有问题", base, settled)
	}

	rbase, rfirst, rsettled := firstFrame(true)
	if diff(rfirst, rbase) < 20 {
		t.Errorf("开了减少动态效果之后第一帧还是空的（%v → %v）——淡入没有直接到位",
			rbase, rfirst)
	}
	if diff(rsettled, settled) > 4 {
		t.Errorf("两条路的终点不一样：减少动态 %v，默认走完 %v", rsettled, settled)
	}
}

// TestTheProfileGlyphIsNotAPlayTriangle 钉住「进他的 UP 空间」那一枚图形**不是**
// 播放三角（票 #455 / 报告 246 第 30 条）。
//
// 原来搜索页「用户」那一档的封面上是一枚播放三角、提示是「从这一首往后听」——
// 图形、提示、无障碍名三样都在说放歌，而用户按下去做的是跳到那个 UP 主的主页。
//
// 判据是**画出来的像素**：
//
//   - 播放三角是**朝右**的一枚楔子，它在盒子里**偏左**（左边缘是竖的，右边缘收成
//     一个尖），所以盒子**偏左、正中**那一个像素是实的；
//   - 「一个人」是**居中**的（一个脑袋加两块肩膀），那一个像素是空的——透出底下
//     的封面。
//
// ⚠️ **两块的底色必须一样**：封面那一格画的是「首字 + 渐变」（`imgcache` 的
// `drawGlyphCover`），而渐变是按**标题的哈希**算的——两块用同一个标题，底色才
// 一模一样，比的才是图形而不是底色。
//
// ⚠️ **底色从盒子自己的角上取**：那一角两种图形都是空的（三角在角上是空的，人头
// 也在角上是空的），所以它就是底色。
func TestTheProfileGlyphIsNotAPlayTriangle(t *testing.T) {
	const cover = 48
	cache := newCache(t)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Gap(40).Padding(6).Children(func() {
			RowCover(c, cache, "", cover, true, "甲", nil)
			RowCoverProfile(c, cache, "", cover, true, "甲", nil)
		})
	}, testerW, testerH)
	tt.Frame()
	settle(tt)

	play, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("没有那个播放三角，文字有 %q", tt.Texts())
	}
	prof, ok := tt.Find(ProfileLabel("甲"))
	if !ok {
		t.Fatalf("没有「进他的 UP 空间」那一枚，文字有 %q", tt.Texts())
	}
	if play.W < 19 || play.H < 19 {
		t.Fatalf("那个盒子只有 %.1f×%.1f，want 20×20", play.W, play.H)
	}

	img := tt.Image()
	// 底色：盒子左上角那一个像素（两种图形在那儿都是空的）。
	bg := img.RGBAAt(int(play.X)+1, int(play.Y)+1)
	bg2 := img.RGBAAt(int(prof.X)+1, int(prof.Y)+1)
	if diff(bg, bg2) > 12 {
		t.Fatalf("两块封面的底色不一样（%v / %v）——这一条比的就不是图形了", bg, bg2)
	}

	// ① 播放三角那一处是实的。
	triPix := img.RGBAAt(int(play.X+play.W*0.2), int(play.Y+play.H*0.5))
	if diff(triPix, bg) < 60 {
		t.Errorf("播放三角在盒子偏左正中那处不是实的（%v，底色 %v）——判据本身有问题", triPix, bg)
	}
	// ② 「进他的 UP 空间」那一处**不是**实的：那儿没有播放三角。
	profPix := img.RGBAAt(int(prof.X+prof.W*0.2), int(prof.Y+prof.H*0.5))
	if diff(profPix, bg2) > 60 {
		t.Errorf("「进他的 UP 空间」那一枚画的还是那枚播放三角（%v，底色 %v）", profPix, bg2)
	}
	// ③ 而它**画了东西**：那一枚不是空的。
	if n := inkIn(img, prof, bg); n == 0 {
		t.Errorf("「进他的 UP 空间」那一枚一个像素都没画——它得是一个「人」")
	}
}

// inkIn 数那个盒子里有多少像素**不是**底色。
//
// 判据是 `diff > 60`：底色是封面那个渐变压暗之后的一层淡彩，而图形是白的一层
// 92%——两者差着几百个通道值（渐变自己在盒子里那点变化只有十几）。
func inkIn(img *image.RGBA, box ui.Rect, bg color.RGBA) int {
	n := 0
	for x := int(box.X); x < int(box.X+box.W); x++ {
		for y := int(box.Y); y < int(box.Y+box.H); y++ {
			if diff(img.RGBAAt(x, y), bg) > 60 {
				n++
			}
		}
	}
	return n
}
