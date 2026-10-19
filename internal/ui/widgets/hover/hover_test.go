package hover

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
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
	tt.Frame()
	grown, ok := tt.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针停在三角上时它不见了：%q", tt.Texts())
	}
	if want := rest.W * triScale; grown.W < want-1 {
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
