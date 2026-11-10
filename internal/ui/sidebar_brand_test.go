package ui

// 这一组锁的是侧栏左上角那枚唱片的「舞池」动效：
// 只在**放歌 + 窗口在前台 + 系统没开减弱动态**时动，其余时候看起来和以前
// 一模一样，而且**一帧都不要**。
//
// 判据只用两样：品牌区那一块的像素（`brandShot`）和 `FramePending`。前者看
// 「动没动」，后者看「还在不在要帧」——只看像素的话，一个「每帧重画同一张」
// 的实现照样是绿的。

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// brandOnlyView 只画品牌那一条，用的仍是真实的 App 状态。
//
// ⚠️ **为什么不画整窗**：放歌时播放条那根进度本来就每帧要一帧
// （`selfSliderLive`，和唱片无关），整窗的 `FramePending` 是它的，分不出这一
// 组要验的「这枚唱片要不要帧」。只画品牌，判据就只剩标志自己。
//
// 「看不看得见」那个开关照 `Shell.View` 的做法每帧装一次：局部重绘那一帧不
// 重建，标志在画的时候读的必须是**活的**指针。
func brandOnlyView() shellOpt {
	return withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			visible := s.visibleFlag()
			visible.Store(s.presence.seen())
			hover.SetVisibleFlag(visible)
			mygoui.Column(c).FillWidth().Children(func() {
				s.viewSideBrand(c, s.app.Snapshot(), false)
			})
		}
	})
}

// brandShot 把品牌区那一块像素抄下来。
//
// ⚠️ **必须抄**：`Tester.Image()` 的像素下一帧会被重画，直接留指针等于拿两
// 张同一个缓冲区比。范围是标志所在的那一带（x 16..60、纵向让过 macOS 红黄绿
// 之后的那 45 行）。
func brandShot(tt *mygoui.Tester, s *Shell) []color.RGBA {
	img := tt.Image()
	y0 := int(s.sideBrandTopPad()) + 16
	out := make([]color.RGBA, 0, 45*45)
	for y := y0; y <= y0+44; y++ {
		for x := 16; x <= 60; x++ {
			out = append(out, img.RGBAAt(x, y))
		}
	}
	return out
}

// brandChanged 报两帧品牌区有没有像素不同。
func brandChanged(a, b []color.RGBA) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if maxDiff(a[i], b[i]) > 0 {
			return true
		}
	}
	return false
}

// playingShell 造一个正在放歌的外壳。这台机器没有音频设备时跳过——鼓点的前提
// 就是「真的有歌在放」，没有它这一整组都验不到东西。
func playingShell(t *testing.T, opts ...shellOpt) *testShell {
	t.Helper()
	b := newTestShell(t, append([]shellOpt{brandOnlyView()}, opts...)...)
	openATrack(t, b.s)
	b.tt.Frame()
	if !b.a.Snapshot().Playing {
		t.Skip("这台机器没有可用的音频设备：放不起来，也就没有鼓点可验")
	}
	return b
}

// TestTheBrandMarkBeatsWhilePlaying 锁住「放歌且窗口在前台时，唱片会动」。
func TestTheBrandMarkBeatsWhilePlaying(t *testing.T) {
	b := playingShell(t)
	tt := b.tt

	before := brandShot(tt, b.s)
	// 半拍（300ms）：鼓点和绕行的小点都该挪了。不钉具体形状，只断「变了」。
	tt.Advance(motion.BrandBeatPeriod / 2)
	tt.Frame()

	if !brandChanged(before, brandShot(tt, b.s)) {
		t.Fatal("歌在放、窗口在前台，推进半拍品牌标志却一个像素都没变")
	}
}

// TestTheBrandMarkStopsWhenPaused 锁住「暂停之后唱片回到静态，也不再要帧」。
func TestTheBrandMarkStopsWhenPaused(t *testing.T) {
	b := playingShell(t)
	tt := b.tt

	b.a.Toggle()
	tt.Frame()
	if b.a.Snapshot().Playing {
		t.Fatal("点了暂停，快照里还在放")
	}
	if tt.FramePending() {
		t.Fatal("暂停了，画面还在请求下一帧")
	}

	before := brandShot(tt, b.s)
	tt.Advance(motion.BrandBeatPeriod / 2)
	tt.Frame()
	if brandChanged(before, brandShot(tt, b.s)) {
		t.Error("暂停了，唱片还在动")
	}
}

// TestTheBrandMarkStopsWhenTheWindowLeavesTheForeground 锁住「主窗口失焦之后
// 唱片停下来」——用户看不见它，再动就是白烧电。
func TestTheBrandMarkStopsWhenTheWindowLeavesTheForeground(t *testing.T) {
	b := playingShell(t)
	tt := b.tt

	b.s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("窗口失焦了，画面还在请求下一帧")
	}

	before := brandShot(tt, b.s)
	tt.Advance(motion.BrandBeatPeriod / 2)
	tt.Frame()
	if brandChanged(before, brandShot(tt, b.s)) {
		t.Error("窗口失焦了，唱片还在动")
	}
}

// TestTheBrandMarkStopsForReduceMotion 锁住「系统开了减弱动态时，放歌也不动」。
func TestTheBrandMarkStopsForReduceMotion(t *testing.T) {
	b := playingShell(t, withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt := b.tt

	if tt.FramePending() {
		t.Fatal("开了减弱动态，画面还在请求下一帧")
	}
	before := brandShot(tt, b.s)
	tt.Advance(motion.BrandBeatPeriod / 2)
	tt.Frame()
	if brandChanged(before, brandShot(tt, b.s)) {
		t.Error("开了减弱动态，唱片还在动")
	}
}

// TestTheBrandMarkRestartsAtTwelveAfterPause 锁住「暂停再播时唱片从 12 点重开」。
//
// 判据是像素：暂停再播之后的第一帧必须和最开始那一帧**逐像素一样**——相位
// 清零了才会一样；从暂停那一刻的相位接着走的话，那颗小点会落在半路上。
func TestTheBrandMarkRestartsAtTwelveAfterPause(t *testing.T) {
	b := playingShell(t)
	tt := b.tt

	first := brandShot(tt, b.s)
	// 半拍：先确认这一条的前提——唱片真的在动。不动的话「再播后一样」是
	// 白给的（前后都没变）。
	tt.Advance(motion.BrandBeatPeriod / 2)
	tt.Frame()
	if !brandChanged(first, brandShot(tt, b.s)) {
		t.Fatal("推进半拍之后品牌标志没动——这一条的前提不成立")
	}

	// 暂停，让时间白白走掉一秒（相位在停着的时候本来会「攒」起来），再播。
	b.a.Toggle()
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	b.a.Toggle()
	tt.Frame()

	if brandChanged(first, brandShot(tt, b.s)) {
		t.Error("暂停再播之后的第一帧和最开始那一帧不一样——唱片没有从 12 点重开")
	}
}

// TestTheBrandMarkStaysRoundWhilePlaying 锁住「绕圈的小点不啃外缘」。
//
// ⚠️ 这条是用户看见的：头点走到圆环外缘时，accent 会在黑 / 白圆上啃出
// 一个缺口，整个唱片看起来不是正圆。判据是像素几何——从 r13.5 到外缘
// r15 的那一圈不许出现 accent 色。轨道 11.0 + 头点 1.6 把最外压到 12.6；
// 把它挪回 12.9 这条就红。
func TestTheBrandMarkStaysRoundWhilePlaying(t *testing.T) {
	b := newTestShell(t, brandStateView(app.State{Playing: true}), systemIsDark(false))
	b.tt.Frame()
	th := BuildTheme(b.a.SettingsSnapshot(), false)
	accent := tok.Of(th).Accent
	cx := float64(b.tt.Image().Bounds().Dx()) / 2
	cy := float64(b.s.sideBrandTopPad()) + float64(space.Px16) + 16

	for i := 0; i < 3; i++ {
		if i > 0 {
			b.tt.Advance(motion.BrandBeatPeriod)
			b.tt.Frame()
		}
		img := b.tt.Image()
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				dx := float64(x) + 0.5 - cx
				dy := float64(y) + 0.5 - cy
				r := math.Hypot(dx, dy)
				if r < 13.5 {
					continue
				}
				c := img.RGBAAt(x, y)
				if absInt(int(c.R)-int(accent.R))+
					absInt(int(c.G)-int(accent.G))+
					absInt(int(c.B)-int(accent.B)) < 60 {
					t.Fatalf("第 %d 拍：r=%.2f 的外缘上出现了 accent 色 %v——头点贴外缘会把圆环啃出缺口", i, r, c)
				}
			}
		}
	}
}

// brandStateView 只画收起态品牌区（只有那枚标志），播放状态由调用方给定。
//
// ⚠️ **`visibleFlag` 每帧装真的那一份并置为可见**：标志在建的时候认下开关，
// 而 `withView` 绕过了 `Shell.View` 里那一次安装——不装的话，上一条测试留下的
// 开关会让它一帧都不动，量出来的是静态那一档。
func brandStateView(st app.State) shellOpt {
	return withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			visible := s.visibleFlag()
			visible.Store(true)
			hover.SetVisibleFlag(visible)
			s.applyFrameTheme(c)
			s.viewSideBrand(c, st, true)
		}
	})
}

// TestTheBrandBeatStaysInsideItsFrameBudget 锁住「播放时那枚唱片每帧不多分配」。
//
// 判据是**播放态相对静态态**的增量：静态那一档是同一块界面不画动效时的成本，
// 两者一比，动态路径自己的账就露出来了。上限只给 +1 次分配 / +48 字节——
// 那一次是 `CycleRate` 的闭包（现有实现，不是这次加的）；十二个尾迹点必须
// 一个字节都不多花（只有局部 float 和 `p.Fill`）。
func TestTheBrandBeatStaysInsideItsFrameBudget(t *testing.T) {
	staticShell := newTestShell(t, brandStateView(app.State{}))
	staticShell.tt.Frame()
	static := measureFrames(t, staticShell.tt, budgetFrames)

	beatShell := newTestShell(t, brandStateView(app.State{Playing: true}))
	// 先把相位零点定在第一帧，再走五分之一圈，让动态路径真的在画
	// （零点那一帧的「动」和静态只差几个像素，量不出回归）。
	beatShell.tt.Frame()
	beatShell.tt.Advance(motion.CoverSpinPeriod / 5)
	beat := measureFrames(t, beatShell.tt, budgetFrames)

	t.Logf("品牌标志：静态每帧 %.0f 次分配 / %.0f 字节；播放每帧 %.0f 次分配 / %.0f 字节",
		static.allocs, static.bytes, beat.allocs, beat.bytes)
	if beat.allocs > static.allocs+1 {
		t.Errorf("播放时每帧分配 %.0f 次，静态是 %.0f 次——动态路径多分配了"+
			"（上限只许 +1，那是 `CycleRate` 的闭包）",
			beat.allocs, static.allocs)
	}
	if beat.bytes > static.bytes+48 {
		t.Errorf("播放时每帧分配 %.0f 字节，静态是 %.0f 字节——尾迹不该新分配任何东西"+
			"（上限只许 +48 字节）", beat.bytes, static.bytes)
	}
}

// TestTheBrandMarkRequestsNothingWithoutATrack 锁住「没歌在放时唱片一动不动，
// 也不要动画帧」。
func TestTheBrandMarkRequestsNothingWithoutATrack(t *testing.T) {
	b := newTestShell(t, brandOnlyView())
	tt := b.tt
	tt.Frame()

	if tt.FramePending() {
		t.Fatal("没有歌在放，画面却在请求下一帧")
	}
	before := brandShot(tt, b.s)
	tt.Advance(time.Second)
	tt.Frame()
	if brandChanged(before, brandShot(tt, b.s)) {
		t.Error("没有歌在放，唱片却动了")
	}
}
