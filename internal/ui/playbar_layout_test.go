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

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// playbarTester 只画播放条，State 从外面传进来。
//
// 和 `metaBarTester`（playbar_meta_test.go）的差别有三个：**能指定深浅**、
// 这一份总是带一个**有时长的**状态（旋钮那条测试要进度条是**能用的**：框架的
// `Hovered()` 对禁用的控件恒为 false，`mygo/ui/input.go:1116`），以及
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
// ⚠️ **位置必须是 0**：旋钮在 `frac=0` 那头，而填过的那一段轨道也是主色——
// 位置不在 0 的话，「轨道上有一段主色」和「旋钮画出来了」就分不开了
// （见 `TestTheProgressKnobIsOnlyDrawnWhileHovered`）。
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

// TestThePlayKeyIsBigEnoughToHit 断播放键的尺寸。
//
// ⚠️ **断的是量出来的那个图形，不是常量**：这一票把图标从 30 放到 48
// （上游的 `size={48}`），而 30 是全条上最小的一枚——用户要按的那一枚反而最难点。
//
// 判据是三条：
//
//  1. **按钮自己够大**（48 起）——那是点击区。
//
//  2. **比上下首大**（上下首是 18 的图标、32 的点击区）。只断第 1 条的话，
//     一枚 48 的空按钮也算过。
//
//  3. 🔴 **图形本身够大**（≥ 36 宽）——**这一条是第 1 条抓不到的**：点击区是
//     「图标 + 每边 5 的内边距」，把图标缩回 30 而点击区不动的话，按钮还是 58，
//     第 1 条照样绿——而用户看见的那枚**图形**小了三成。
//
//     图形多宽是**量**出来的：主色按钮的底是主色、图形是白（或者深色，看主色
//     多亮），所以「和底色不一样的那一片」就是图形。
func TestThePlayKeyIsBigEnoughToHit(t *testing.T) {
	// ⚠️ **用「暂停」那一张**：播放那一枚画的是**一个三角形**，而三角形落在
	// 24×24 的画板上只占 11 宽——量它的墨迹宽度，48 的图标量出 12，30 的量出 7，
	// 门槛定在哪儿都别扭。**暂停是两条竖**（`RiPauseFill` 那类），墨迹是一块
	// 干净的矩形：48 的图标量出 40 宽，30 的量出 25——门槛放在 36，两头都离得开。
	st := barState()
	st.Playing = true
	tt, s := playbarTester(t, st, 1200, 0, false)

	play, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("画面上找不到「暂停」，文字有 %q", tt.Texts())
	}
	if play.W < 48 || play.H < 48 {
		t.Errorf("播放键只有 %v——它得是 48 像素那么大（上游的 `size={48}`，"+
			"而它原来是 30，全条上最小的一枚）", play)
	}
	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	if play.W <= prev.W {
		t.Errorf("播放键 %v 比「上一首」%v 还小——它得是全条上最大的一枚",
			play, prev)
	}

	// 图形本身：主色底上那一块不是底色的像素有多宽。
	//
	// ⚠️ **量的是按钮**里面那一块（每边缩 3 像素）：按钮是主色、播放条的底是白的，
	// 两者的交界有抗锯齿的边——那一圈混出来的颜色和主色差得开，算进去的话
	// 量到的永远是**按钮的整个宽**（58），图标缩到 30 也照样绿。
	a := BuildTheme(s.app.SettingsSnapshot(), false).Accent
	bg := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}
	inner := mygoui.Rect{X: play.X + 3, Y: play.Y + 3, W: play.W - 6, H: play.H - 6}
	lo, hi, n := inkExtent(tt.Image(), inner, bg)
	if n == 0 {
		t.Fatalf("播放键上没画东西：%q", tt.Texts())
	}
	if hi-lo+1 < 36 {
		t.Errorf("播放键上的图形只有 %d 宽（%d..%d）——图标是 48，"+
			"而它原来只有 30（全条上最小的一枚）", hi-lo+1, lo, hi)
	}
	_ = s
}

// inkExtent 是 box 里「不是底色」的那一片像素有多宽（左右沿和个数）。
//
// 判据是**和底色不一样**：主色按钮的底是主色，图形是白（或者深色，看主色多亮）——
// 所以「不是底色」的那一片就是图形。
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
// ⚠️ **位置必须在 0**（`barState`）：填过的那一段轨道也是主色，位置不在 0 的
// 时候「轨道上有一段主色」和「旋钮画出来了」在像素上是一回事。
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

// TestTheVolumePopoverHasAVerticalSlider 断音量浮层里有一根**竖**滑块，
// 底下是百分比。
//
// 上游 `music-volume` 那个浮层里就是一根 `orientation="vertical"` 的滑块加一个
// `{Math.round(volume * 100)}%`。
//
// ⚠️ **浮层是悬停才出来的**（上游是 300ms 延迟的 Tooltip），所以这一条先悬停
// 再量。
func TestTheVolumePopoverHasAVerticalSlider(t *testing.T) {
	tt, s := playbarTester(t, barState(), 1200, 0, false)

	hoverVolumeKey(t, tt)

	sl, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
	}
	if sl.H <= sl.W {
		t.Errorf("浮层里那根滑块是 %v——它是横的，而上游那根是竖的"+
			"（`music-volume` 的 `orientation=\"vertical\"`）", sl)
	}
	if !tt.HasText("80%") {
		t.Errorf("浮层底下没有那个百分比，文字有 %q", tt.Texts())
	}

	// 🔴 **值映射也得是竖的**：只量「元素是竖的」抓不住「画的是横的」——
	// 元素的高和宽是摆出来的，画法换了它还是那么高那么宽，而用户看见的是一根
	// 横着的滑块（按在底下不动）。
	//
	// 框架的竖直映射是 `1-(y-cy)/ch`（`mygo/ui/value_input.go:207`）：**底下是 0、
	// 顶上是 1**。横着的话，按在底下（同一个 x）就是中间那一档。
	pressAt := func(y float32) {
		tt.Press(sl.X+sl.W/2, y)
		tt.Release(sl.X+sl.W/2, y)
		tt.Frame()
	}
	pressAt(sl.Y + sl.H - 2)
	if got := s.app.Snapshot().Volume; got > 0.15 {
		t.Errorf("按在滑块**底下**之后音量是 %v，want ≈0——它不是一根竖的滑块", got)
	}
	pressAt(sl.Y + 2)
	if got := s.app.Snapshot().Volume; got < 0.85 {
		t.Errorf("按在滑块**顶上**之后音量是 %v，want ≈1——它不是一根竖的滑块", got)
	}
}

// TestClickingTheVolumeIconMutes 断「点图标静音」真的接上了。
//
// ⚠️ **静音 = 音量 0**：播放器那边压根没有静音这一档，而应用层也只有音量
// （`App.SetVolume`）。所以这一条断的是**应用里的音量真的变成了 0**，而不是
// 「图标换了张」。
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
	if got := h.app.Snapshot().Volume; got != 0 {
		t.Errorf("点了音量图标之后音量是 %v，want 0——它没有静音", got)
	}

	// 静音了之后那一枚换了个名字（上游的 `aria-label={isMuted ? "取消静音" : "静音"}`）。
	if err := h.tt.Click("取消静音"); err != nil {
		t.Fatalf("点不动「取消静音」：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before {
		t.Errorf("取消静音之后音量是 %v，want %v——它没有放回静音之前那个音量",
			got, before)
	}
}

// TestTheWheelOverTheVolumeIconChangesTheVolume 断滚轮 ±0.05 真的接上了。
//
// 上游 `music-volume` 的 `onWheel`：`const delta = event.deltaY > 0 ? -0.05 : 0.05`。
// 0.05 和快捷键那一步（`shortcutVolumeStep`）是同一个数——两条路走一样的步子。
//
// ⚠️ **往上是加**：框架的滚轮事件 `DY > 0` 是往下滚，上游那边往下滚是**减**。
func TestTheWheelOverTheVolumeIconChangesTheVolume(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	// ⚠️ **起点得在中档**：音量默认是 1（顶格），往上滚是 1.05 —— 钳完还是 1，
	// 于是这一条量出来的是「没变」，而它其实接上了。
	h.app.SetVolume(0.5)
	h.tt.Frame()

	before := h.app.Snapshot().Volume
	icon, ok := volumeKeyOnTheBar(h.tt)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", h.tt.Texts())
	}

	// 往上滚一格：音量 +0.05。
	h.tt.Scroll(icon.X+icon.W/2, icon.Y+icon.H/2, 0, -100)
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before+volWheelStep {
		t.Errorf("在音量图标上往上滚了一格之后音量是 %v，want %v（+%.2f）",
			got, before+volWheelStep, volWheelStep)
	}

	// 往下滚一格：音量 -0.05。
	h.tt.Scroll(icon.X+icon.W/2, icon.Y+icon.H/2, 0, 100)
	h.tt.Frame()
	if got := h.app.Snapshot().Volume; got != before {
		t.Errorf("在音量图标上往下滚了一格之后音量是 %v，want %v（滚回去）",
			got, before)
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
// ⚠️ **窗口只留 88 高**（播放条那一行）：整窗的截图在 #127 那一族里已经有了
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

// TestTheVolumeTrackIsFilledFromTheBottom 钉「轨道填的是**已经调上去的那一段**」。
//
// ⚠️ **这一条是被评审指出来的**：竖着的那一根原来是从**顶**往下填到旋钮的，
// 填成了**剩下的**那一段——于是 0% 显示满格、100% 显示空格。
//
// 判据：轨道**上半段**（离 0% 时旋钮最远的那一头）在 100% 时是主色、在 0% 时不是。
// 那一段两头都离旋钮远（旋钮 100% 时在顶上、0% 时在底下），所以影子和旋钮都
// 够不着它。
func TestTheVolumeTrackIsFilledFromTheBottom(t *testing.T) {
	for _, c := range []struct {
		vol    float64
		filled bool
	}{{0, false}, {1, true}} {
		t.Run(fmt.Sprint(c.vol), func(t *testing.T) {
			st := barState()
			st.Volume = c.vol
			tt, s := bottomBarTester(t, st, 1200, 320, false)
			hoverVolumeKey(t, tt)

			sl, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
			}
			box := mygoui.Rect{
				X: sl.X + sl.W/2 - 2,
				Y: sl.Y + 30,
				W: 4, H: 20,
			}
			a := BuildTheme(s.app.SettingsSnapshot(), false).Accent
			accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}
			n := nearPixels(tt.Image(), box, accent)
			switch {
			case c.filled && n == 0:
				t.Errorf("音量 100%%，轨道上半段 %v 却一个主色像素都没有——它没有填满", box)
			case !c.filled && n != 0:
				t.Errorf("音量 0%%，轨道上半段 %v 却有 %d 个主色像素——"+
					"它填反了（填的是**剩下**的那一段）", box, n)
			}
		})
	}
}

// TestTheVolumePopoverClosesOnItsOwn 钉「浮层会自己收掉」。
//
// ⚠️ **这一条是被评审指出来的**：原来那段只读 `s.clock.Now()`，**没有排任何到点
// 的唤醒**——于是「指针离开了」这件事**只在那一帧**被算一次，而不播放的时候
// **没有任何东西在要帧**：浮层于是永远留在画面上，遮住内容区。
//
// 判据是**画面上那个浮层没了**（那根滑块不在了），不是某个布尔。
func TestTheVolumePopoverClosesOnItsOwn(t *testing.T) {
	tt, s := bottomBarTester(t, barState(), 1200, 320, false)
	// 假时钟：这一条要等 400 毫秒，不真等（`clock.go` 那一层）。
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc

	// 悬停 → 浮层出来。
	hoverVolumeKey(t, tt)
	if _, ok := tt.Find("音量"); !ok {
		t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
	}

	// 指针移到空白处：既不在图标上，也不在浮层里。
	tt.Move(200, 60)
	// 🔴 **先让悬停那个动画走完**：走完之后**没有任何东西在要帧**了——
	// 「浮层自己收掉」真正要考的就是这一段（不播放的时候没有人要帧，
	// 原来那一版于是永远停在画面上）。
	tt.Advance(volHoverDelay + 50*time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find("音量"); !ok {
		t.Fatalf("指针刚离开浮层它就没了，文字有 %q——它不该这么急", tt.Texts())
	}

	// 推**应用**那个钟：到点了就该**有一帧被要**（那一段排了到点的唤醒）。
	fc.Advance(volCloseGrace * 2)
	if !tt.FramePending() {
		t.Fatalf("过了宽限没有一帧被要（文字有 %q）——"+
			"那一段必须自己排一个到点的唤醒：不播放的时候没有任何东西在要帧，"+
			"而它只在**那一帧**算过一次「指针离开了」", tt.Texts())
	}
	tt.Frame()

	if _, ok := tt.Find("音量"); ok {
		t.Errorf("过了宽限浮层还开着（文字有 %q）——它得自己收掉", tt.Texts())
	}
}
