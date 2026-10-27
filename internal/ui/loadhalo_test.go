package ui

// 「正在取流」时绕着播放键转的那一圈光（票 #271）。
//
// # 这一族断的是六件事
//
//	有那一圈光	取流的时候，播放键**外面**真的有一圈在转的光
//	按钮照旧	那一枚**仍然是播放 / 暂停**：能按、名字照旧、禁用态照旧
//	它在转	钟往前走，光跟着转（不是「代码里有个角度」）
//	转多快	一圈 1.4 秒（对着 MUI / Material Web 那一档量的）
//	听系统的偏好	「减少动态效果」开着的时候**不转**（于是不画）
//
// # 判据是**画面上的像素**，不是「代码里有个角度」
//
// 一圈光是一段弧，它在哪、它有多宽、它是什么颜色——这些只有画面上看得见。
// 所以这一族量的是**图像**：播放键圆心周围那一圈里，哪些像素是强调色。

import (
	"image/color"
	"math"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// haloState 是「正在取流」那一副状态（播放条那一档）。
func haloState() app.State {
	st := budgetState()
	st.Loading = true
	return st
}

// haloTester 造一个只画播放条的外壳，喂一份手摆的状态。
//
// ⚠️ **状态是手摆的**（理由和 framebudget_test.go 的 `budgetState` 一样）：
// 取流这件事由应用层那一栏说，测试自己说。
func haloTester(t *testing.T, st app.State, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, at(1200, coverWinH), systemIsDark(dark),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				// ⚠️ **要装应用的主题**：不装的话画的是框架默认那套（蓝的），
				// 而这一族要量的是**应用自己的**强调色。
				s.applyFrameTheme(c)
				s.viewPlayBar(c, st)
			}
		}))
	tt := b.tt
	tt.Frame()
	return b.s, tt
}

// keyAccent 是播放键那一枚**实心圆**的颜色（也就是强调色）。
//
// ⚠️ **从画面上取，而不是从主题里拼**：那一枚圆自己就是强调色，而光圈
// 也是强调色——从画面上取，两边就一定是同一个颜色。
func keyAccent(t *testing.T, tt *mygoui.Tester, key mygoui.Rect) color.RGBA {
	t.Helper()
	img := tt.Image()
	counts := map[color.RGBA]int{}
	for y := int(key.Y); y < int(key.Y+key.H); y++ {
		for x := int(key.X); x < int(key.X+key.W); x++ {
			c := img.RGBAAt(x, y)
			counts[c]++
		}
	}
	best, bestN := color.RGBA{}, -1
	for c, n := range counts {
		if n > bestN {
			best, bestN = c, n
		}
	}
	return best
}

// haloPixels 是画面上**属于那一圈光**的那些像素：强调色、而且在播放键那个
// 实心圆的**外面**。
//
// 返回的是它们相对**播放键圆心**的极角（度，顺时针，从 12 点起）。
//
// ⚠️ **为什么按半径分开**：播放键那一枚自己就是一枚**实心强调色圆**，
// 而光圈是强调色的——不按半径分开的话，量到的全是那枚圆。
func haloPixels(t *testing.T, s *Shell, tt *mygoui.Tester) []float64 {
	t.Helper()
	key, ok := tt.Find("播放")
	if !ok {
		key, ok = tt.Find("暂停")
	}
	if !ok {
		t.Fatalf("画面上找不到播放键，文字有 %q", tt.Texts())
	}
	cx, cy := float64(key.X+key.W/2), float64(key.Y+key.H/2)
	accent := keyAccent(t, tt, key)
	img := tt.Image()
	b := img.Bounds()
	var angles []float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !near(img.RGBAAt(x, y), accent) {
				continue
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			r := math.Hypot(dx, dy)
			// 光圈的那一圈（见 loadhalo.go 的 loadHaloRadius）。
			lo := float64(loadHaloRadius(iconHitPlay) - loadHaloWidth(iconHitPlay)/2)
			hi := lo + float64(loadHaloWidth(iconHitPlay)) + 1
			if r < lo || r > hi {
				continue
			}
			angles = append(angles, math.Mod(math.Atan2(dx, -dy)*180/math.Pi+360, 360))
		}
	}
	return angles
}

// meanAngle 是那些极角的平均（度）。
//
// ⚠️ **按角度平均要绕着圈算**：直接加再除会在 350° 和 10° 那里算出 180°
// ——所以这里取的是**单位向量**的平均。
func meanAngle(angles []float64) float64 {
	var sx, sy float64
	for _, a := range angles {
		r := a * math.Pi / 180
		sx += math.Sin(r)
		sy += math.Cos(r)
	}
	return math.Mod(math.Atan2(sx, sy)*180/math.Pi+360, 360)
}

// TestTheHaloIsAroundThePlayKeyWhileLoading 断「取流的时候播放键外面真的
// 有一圈光」。
//
// 判据是**像素**：播放键那个圆外面那一圈半径上，有强调色的像素。
func TestTheHaloIsAroundThePlayKeyWhileLoading(t *testing.T) {
	s, tt := haloTester(t, haloState(), false)
	angles := haloPixels(t, s, tt)
	if len(angles) == 0 {
		t.Fatal("取流的时候播放键外面一个强调色的像素都没有——那一圈光没画出来")
	}
	// 光圈转过 100°，环的周长是 2π×27 ≈ 170 像素，粗 3 —— 那一段弧的面积
	// 差不多 140 个像素（抗锯齿再吃掉一些，实测 90 上下）。
	if len(angles) < 60 {
		t.Errorf("那一圈光只有 %d 个像素，太少了——它不像是绕着键转的一圈", len(angles))
	}
}

// TestThePlayKeyIsStillPlayWhileLoading 断**按钮照旧**。
//
// 🔴 **这是票 #282 的评审明写的那一条**：取流期间那一枚**仍然是播放 / 暂停**
// ——用户按得下暂停。而**不是**换成一枚转圈。
func TestThePlayKeyIsStillPlayWhileLoading(t *testing.T) {
	st := haloState()
	st.Playing = true
	_, tt := haloTester(t, st, false)
	key, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("取流的时候播放键不叫「暂停」了，文字有 %q——它被换成别的东西了", tt.Texts())
	}
	if key.W != iconHitPlay || key.H != iconHitPlay {
		t.Errorf("播放键是 %v，want %d×%d", key, iconHitPlay, iconHitPlay)
	}
}

// TestTheHaloTurns 断**它在转**。
//
// 判据：钟往前走 1/4 圈（350 毫秒）之后，那些像素的平均极角转过去了。
func TestTheHaloTurns(t *testing.T) {
	s, tt := haloTester(t, haloState(), false)
	before := haloPixels(t, s, tt)
	if len(before) == 0 {
		t.Fatal("取流的时候没有那一圈光")
	}
	tt.Advance(loadHaloPeriod / 4)
	tt.Frame()
	after := haloPixels(t, s, tt)
	if len(after) == 0 {
		t.Fatal("转了之后那一圈光不见了")
	}
	d := math.Mod(meanAngle(after)-meanAngle(before)+540, 360) - 180
	if d < 60 || d > 120 {
		t.Errorf("转了 1/4 圈（%v）之后光只转了 %.0f°，want 90°±30", loadHaloPeriod/4, d)
	}
}

// TestTheHaloTurnsOnceEveryPeriod 断**转多快**：一圈 1.4 秒。
//
// 判据是**转满一圈之后回到原处**：那是「转多快」最直接的量法——它量的是
// 周期，而不是某一帧的角度（后者差一点点就飘）。
//
// ⚠️ **光转满一圈回到原处，这一句话单看是断不出转速的**（压根没转也过）。
// 它是**和上面那条合起来**才成为判据的：上面那条断「350 毫秒转 90°」，
// 这一条断「1400 毫秒转回原处」——两条合起来才把周期钉在 1.4 秒（快一倍
// 的话上面那条会量到 180°，慢一倍的话这一条会量到 180°）。
func TestTheHaloTurnsOnceEveryPeriod(t *testing.T) {
	s, tt := haloTester(t, haloState(), false)
	before := haloPixels(t, s, tt)
	tt.Advance(loadHaloPeriod)
	tt.Frame()
	after := haloPixels(t, s, tt)
	if len(after) == 0 {
		t.Fatal("转了一圈之后那一圈光不见了")
	}
	d := math.Mod(math.Abs(meanAngle(after)-meanAngle(before)), 360)
	if d > 25 {
		t.Errorf("转了 %v（一圈）之后光转了 %.0f°，want 360°（也就是回到原处）", loadHaloPeriod, d)
	}
}

// TestNoHaloWithoutLoading 断**不取流的时候没有那一圈光**。
func TestNoHaloWithoutLoading(t *testing.T) {
	s, tt := haloTester(t, budgetState(), false)
	if angles := haloPixels(t, s, tt); len(angles) != 0 {
		t.Errorf("不取流的时候播放键外面有 %d 个强调色的像素——那一圈光不该在", len(angles))
	}
}

// TestNoHaloWithReduceMotion 断**「减少动态效果」开着的时候不转**。
//
// 那一圈光**就是**动效本身：留一个不动的圈在键边上，用户只会以为那个键被
// 选中了。取流的时候界面上没有别的提示（票 #581 把那行字去了）——开着
// 「减少动态效果」的用户拿到的反馈就是「没有光圈」：这是那道偏好的本意，
// 不是漏画。
func TestNoHaloWithReduceMotion(t *testing.T) {
	b := newTestShell(t, at(1200, coverWinH), systemIsDark(false),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.viewPlayBar(c, haloState())
			}
		}))
	tt := b.tt
	tt.SetPreferences(mygoui.Preferences{ReduceMotion: true})
	tt.Frame()
	if angles := haloPixels(t, b.s, tt); len(angles) != 0 {
		t.Errorf("「减少动态效果」开着的时候播放键外面有 %d 个强调色的像素——它不该转，也不该在", len(angles))
	}
}

// TestNoHaloAfterLoadingEnds 断**取流结束之后光圈收掉**。
func TestNoHaloAfterLoadingEnds(t *testing.T) {
	s, tt := haloTester(t, haloState(), false)
	if len(haloPixels(t, s, tt)) == 0 {
		t.Fatal("取流的时候没有那一圈光")
	}
	st := haloState()
	st.Loading = false
	s2, tt2 := haloTester(t, st, false)
	if angles := haloPixels(t, s2, tt2); len(angles) != 0 {
		t.Errorf("取流结束之后播放键外面还有 %d 个强调色的像素", len(angles))
	}
}

// TestTheHaloCostsAlmostNothingPerFrame 断**光圈自己的每帧代价**。
//
// ⚠️ **量的是「差」**：取流那一档（**带**光圈）减去取流那一档（**不带**
// 光圈）——差的那一份就是光圈自己的。
//
// 「不带光圈」那一档是靠**「减少动态效果」**去掉的（那是这一档唯一的开关，
// 见 loadhalo.go）：于是两边的差别**只有**光圈。

func TestTheHaloCostsAlmostNothingPerFrame(t *testing.T) {
	measure := func(reduce bool) frameCost {
		opts := []shellOpt{}
		if reduce {
			opts = append(opts, reduceMotion())
		}
		s, tt := newShell(t, append(opts, withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.applyFrameTheme(c)
				s.viewPlayBar(c, haloState())
			}
		}))...)
		_ = s
		return measureFrames(t, tt, budgetFrames)
	}
	without := measure(true)
	with := measure(false)
	t.Logf("取流那一档：不带光圈每帧 %.0f 次 / %.0f 字节；带光圈每帧 %.0f 次 / %.0f 字节",
		without.allocs, without.bytes, with.allocs, with.bytes)
	// 光圈自己的那一份：**一个绘制回调**（一次分配）。
	if d := with.allocs - without.allocs; d > 2 {
		t.Errorf("光圈自己每帧多了 %.0f 次分配，want ≤ 2（它只是键自己的一个绘制回调）", d)
	}
	if d := with.bytes - without.bytes; d > 300 {
		t.Errorf("光圈自己每帧多了 %.0f 字节，want ≤ 300", d)
	}
}

// TestTheHaloStopsWhenTheWindowIsInvisible 断**窗口看不见的时候光圈停**。
//
// 🔴 **看不见的窗口里空转**是满屏封面那一族的老毛病（票 #392）：那一档的
// 绘制每一帧都要一次电，而用户**看不见**。
//
// ⚠️ **这一条量在满屏播放器上**：那一档的画面上**没有别的会自己要帧的东西**
// ——于是**光圈是唯一会自己要帧的东西**，量它停没停就是干净的（票 #581 之后
// 播放条那一档取流时也没有转圈了，但满屏那一档本来就更干净）。
func TestTheHaloStopsWhenTheWindowIsInvisible(t *testing.T) {
	st := haloState()
	newFull := func() (*Shell, *mygoui.Tester) {
		b := newTestShell(t, at(1200, 800), withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.full.open = true
				s.viewFullPlayer(c, st)
			}
		}))
		tt := b.tt
		tt.Frame()
		return b.s, tt
	}

	// 对照：**看得见**的时候它转。
	s, tt := newFull()
	before := fullHaloAngle(t, s, tt)
	tt.Advance(loadHaloPeriod / 4)
	after := fullHaloAngle(t, s, tt)
	if math.Abs(angleDelta(after, before)) < 30 {
		t.Fatalf("看得见的时候光圈没有转（%.0f° → %.0f°）——这一条测的不是「停没停」",
			before, after)
	}

	// 看不见了：它停。
	s2, tt2 := newFull()
	s2.visibleFlag().Store(false)
	before2 := fullHaloAngle(t, s2, tt2)
	tt2.Advance(loadHaloPeriod / 4)
	after2 := fullHaloAngle(t, s2, tt2)
	if d := math.Abs(angleDelta(after2, before2)); d > 5 {
		t.Errorf("窗口看不见了光圈还转了 %.0f°——看不见的窗口里不该空转", d)
	}
}

// fullHaloAngle 是满屏播放器那一枚光圈的平均极角（度）。
func fullHaloAngle(t *testing.T, s *Shell, tt *mygoui.Tester) float64 {
	t.Helper()
	key, ok := tt.Find("播放")
	if !ok {
		key, ok = tt.Find("暂停")
	}
	if !ok {
		t.Fatalf("满屏上找不到播放键，文字有 %q", tt.Texts())
	}
	cx, cy := float64(key.X+key.W/2), float64(key.Y+key.H/2)
	accent := keyAccent(t, tt, key)
	img := tt.Image()
	b := img.Bounds()
	var angles []float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !near(img.RGBAAt(x, y), accent) {
				continue
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			r := math.Hypot(dx, dy)
			lo := float64(loadHaloRadius(fullPlayHit) - loadHaloWidth(fullPlayHit)/2)
			if r < lo || r > lo+float64(loadHaloWidth(fullPlayHit))+1 {
				continue
			}
			angles = append(angles, math.Mod(math.Atan2(dx, -dy)*180/math.Pi+360, 360))
		}
	}
	if len(angles) == 0 {
		t.Fatal("满屏那一枚外面没有光圈的像素")
	}
	return meanAngle(angles)
}

// angleDelta 是两个极角之间的差（度，正负都在 ±180 之间）。
func angleDelta(a, b float64) float64 {
	return math.Mod(a-b+540, 360) - 180
}
