// 满屏播放器那一行的**悬停反馈**：这一行坐着的是压暗过的封面，而悬停反馈原来
// 是按**页面白底**算的——浅色主题下指针一停上去，那几个图标反而**消失**
// （深绿 #107032 压深底只有 1.0:1，票 #248）。
//
// 这一组量的全是**画面上的东西**：图形的颜色、图形和它背后的底的对比度、轨道
// 的粗细——不是「调了哪个函数」。
package ui

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// fullTesterWithCoverSettings 造一个开着歌、**有封面**、**用给定设置**的界面。
//
// 和 `fullTesterWithCoverPNG` 的差别只有设置那一项——那几套方案各自的主色不
// 同，而「悬停那一档算出来是什么色」正是要量的东西。
func fullTesterWithCoverSettings(t *testing.T, st app.Settings, body []byte) (*Shell, *mygoui.Tester) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s, tt := newShell(t, withSettings(st))
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc

	item := app.LocalItem(flacPath(t))
	item.Cover = srv.URL + "/cover.png"
	if err := s.app.Enqueue(item); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	return s, tt
}

// glyphInk 是「那一枚图形的颜色」和「它背后的底色」。
//
// # 为什么自己扫而不用 scanGlyph
//
// `scanGlyph` 报的是**灰阶指纹**（比形状用），而这里要量的是**颜色**——悬停
// 那一档是主色还是白，指纹分不出来（两者的灰阶可能一样）。
//
// # 怎么认
//
//   - 框内的**众数** = 底色（没画东西的那些像素：那一块的背景，或者悬停时铺的
//     那层淡底）；
//   - 框内**离那个众数最远**的那个像素 = 图形本身（抗锯齿的边比核心淡，所以
//     取最远的那个就是核心那一档）。
//
// 图标的形状不规则（中心可能是空的），所以不能取中心像素。
func glyphInk(img *image.RGBA, box mygoui.Rect) (ink, bg color.RGBA) {
	x0, y0 := max(int(box.X), 0), max(int(box.Y), 0)
	x1, y1 := min(int(box.X+box.W), img.Rect.Max.X), min(int(box.Y+box.H), img.Rect.Max.Y)
	if x1 <= x0 || y1 <= y0 {
		return color.RGBA{}, color.RGBA{}
	}
	samples := make([]color.RGBA, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			samples = append(samples, img.RGBAAt(x, y))
		}
	}
	bg = modeOf(samples)
	far, dist := bg, -1
	for _, p := range samples {
		d := maxDiff(p, bg)
		if d > dist {
			far, dist = p, d
		}
	}
	return far, bg
}

// inkDistance 是两个颜色差多远（三个通道的差加起来）。
//
// 它量的是**「变了没有」**，不是「好不好看」：悬停那一帧图形的颜色和平时那一
// 帧差多少，差得够多才叫「量得出来，不是看着差不多」。
func inkDistance(a, b color.RGBA) int {
	return absInt(int(a.R)-int(b.R)) + absInt(int(a.G)-int(b.G)) + absInt(int(a.B)-int(b.B))
}

// TestTheFullPlayerRowKeysAreVisibleOnHover 断「满屏那一行的悬停反馈看得见」。
//
// # 这个票修的是什么
//
// 那一行坐着的是**压暗过的封面**，而悬停反馈原来按**页面白底**算：主色当字色
// 那一档（浅色主题下算出来是深绿 `#107032`）压在那上面只有 **1.0:1**——指针一
// 停上去，那几个图标**反而不见了**（票 #248）。
//
// # 三条判据
//
//  1. **图形变了**：悬停那一帧的颜色和平时那一帧差得够多（RGB 距离 ≥ 24）——
//     「量得出来，不是看着差不多」；
//  2. **图形看得见**：悬停时图形和**那一行的底**的对比度 ≥ 3:1（WCAG 1.4.11
//     非文字那一档）；
//  3. **多了一层淡底**：悬停时那一枚底下多出一层半透明的前景色——按**那一行的
//     底**算（深底压白、浅底压黑），不是按主题正文色算。
//
// ⚠️ **底色是「最亮的那一档封面」**：纯白的封面压完 55% 的黑是 `#737373`
// （`fullRowBase`），而按它算出来的颜色在**任何**一档封面上都读得出来。
//
// ⚠️ **第 2 条比的是「图形 vs 那一行的底」**：悬停时图形底下多出来的那层淡底
// （8% 的白）是**悬停反馈本身**，它把图形和底的对比度压低了一档——而图形读不读
// 得出来，判的是它和**这一行坐着的那块底**。
func TestTheFullPlayerRowKeysAreVisibleOnHover(t *testing.T) {
	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetDuskAmber} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			t.Run(string(preset)+"-"+shade, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}
				_, tt := fullTesterWithCoverSettings(t, st, whitePNG(t, 1200, 1200))
				openFull(t, tt)
				settleFull(t, tt)

				for _, label := range []string{"上一首", "下一首", "播放模式", "播放列表"} {
					box, ok := tt.Find(label)
					if !ok {
						t.Fatalf("找不到 %q，文字有 %q", label, tt.Texts())
					}
					// 指针先离开那一枚（Tester 的指针可能在窗口中间）。
					tt.Move(5, 400)
					tt.Frame()
					rest, base := glyphInk(tt.Image(), box)

					tt.Move(box.X+box.W/2, box.Y+box.H/2)
					tt.Frame()
					settleFull(t, tt)
					hot, over := glyphInk(tt.Image(), box)

					// 1. 图形变了。
					if d := inkDistance(hot, rest); d < 24 {
						t.Errorf("%s 悬停时图形的颜色几乎没变（%v → %v，距离 %d）——"+
							"「悬停反馈看得见」这一条不成立", label, rest, hot, d)
					}
					// 2. 图形和那一行的底看得见。
					if r := contrastRatio(hot, base); r < 3 {
						t.Errorf("%s 悬停时图形 %v 和它背后那一行的底 %v 只有 %.2f:1"+
							"（门槛 3:1）——图标在暗底上读不出来", label, hot, base, r)
					}
					// 3. 多了一层淡底（深底上压白 → 变亮）。
					if l := int(over.R) - int(base.R); l < 6 {
						t.Errorf("%s 悬停时底下没有多出淡底（底 %v → %v）——"+
							"「悬停」在画面上看不出来", label, base, over)
					}
				}
			})
		}
	}
}

// TestTheFullPlayerProgressBarIsNotAccentUntilHover 断满屏那条进度的两档。
//
// # 三条判据
//
//  1. **平时不是主色**：填过的那一段是**那一行的第二档前景色**（白 62%，无彩色）；
//  2. **悬停才主色**：指针落上去以后那一段换成主色（**有彩色**）——
//     和播放条那根同一个语言（票 #218）；
//  3. **轨道悬停时变粗**：6 → 8。
//
// ⚠️ **用的是经典那一套主题**：轨道是 `t.Border`，而那几套方案把 `t.Border`
// 换成了「正文色 12% 那一层」（半透明），压在压暗的封面上和底色只差九个通道
// ——量不出轨道有几像素粗。经典那一套的 `t.Border` 是不透明的，才量得出来。
func TestTheFullPlayerProgressBarIsNotAccentUntilHover(t *testing.T) {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight

	tt := fullProgressTester(t, st, app.State{HasTrack: true, Duration: 60, Position: 30})
	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}

	// 填过的那一段在**左边**（值 0.5），轨道在**右边**。
	fillX := int(box.X + box.W*0.25)
	trackX := int(box.X + box.W*0.75)
	restFill := pixelAt(tt.Image(), float32(fillX), box.Y+box.H/2)
	restTrack := trackThickness(tt, trackX, int(box.Y), int(box.Y+box.H))

	// 1. 平时不是主色：那一段是**无彩色**的（前景色）。
	if sp := channelSpread(restFill); sp > 6 {
		t.Errorf("满屏那条进度平时填过的一段是 %v（有彩色，跨度 %d）——"+
			"「平时不是主色」这一条不成立", restFill, sp)
	}

	tt.Move(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	settleFull(t, tt)

	hotFill := pixelAt(tt.Image(), float32(fillX), box.Y+box.H/2)
	hotTrack := trackThickness(tt, trackX, int(box.Y), int(box.Y+box.H))

	// 2. 悬停才主色。
	if sp := channelSpread(hotFill); sp < 12 {
		t.Errorf("满屏那条进度悬停时填过的一段是 %v（无彩色，跨度 %d）——"+
			"「悬停才主色」这一条不成立", hotFill, sp)
	}
	if d := inkDistance(hotFill, restFill); d < 24 {
		t.Errorf("满屏那条进度悬停时颜色几乎没变（%v → %v，距离 %d）", restFill, hotFill, d)
	}
	// 3. 轨道悬停时变粗。
	if hotTrack <= restTrack {
		t.Errorf("满屏那条进度的轨道悬停时是 %d 像素、平时是 %d 像素——"+
			"「悬停时变粗」这一条不成立", hotTrack, restTrack)
	}
}

// TestTheDisabledProgressBarIsNotAccent 断「不能拖的进度条不许是主色」。
//
// 没歌 / 不知道时长的时候那一条是**禁用的**（`st.Duration == 0` →
// `.Disabled(true)`），而这一屏上「主色 = 可交互」的信号正是那枚实心播放键——
// 一根**不能拖**的进度条不该是主色的（票 #248）。
//
// ⚠️ **判据是「无彩色」**：禁用的那一档是那一行的第二档前景色（白 62%），
// 没有任何彩色；而改之前那一档是主色（有彩色）。
func TestTheDisabledProgressBarIsNotAccent(t *testing.T) {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight

	// 时长 0 → 那一根是禁用的；而进度停在中间（`Position` 之外的那一份），
	// 所以填过的那一段确实画出来了。
	tt := fullProgressTester(t, st, app.State{HasTrack: true, Duration: 0, Position: 0})
	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}
	fill := pixelAt(tt.Image(), box.X+box.W*0.25, box.Y+box.H/2)
	if sp := channelSpread(fill); sp > 6 {
		t.Errorf("禁用的满屏进度条填过的那一段是 %v（有彩色，跨度 %d）——"+
			"一根不能拖的进度条不该是主色的", fill, sp)
	}
}

// TestRenderFullPlayerRowShots 把满屏播放器底下那一行存成 PNG。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerRowShots
//
// 这一行是**压暗过的封面**上的控件，而悬停反馈是这一票改的（票 #248）——
// 「看得见」这件事人眼一眼就知道，量出来的数只是补一手。
//
// ⚠️ **深浅两套 × 两套方案都要出**：浅色下算出来的主色那一档和深色下差得很远
// （浅色按页面白底算 → 深绿压深底等于看不见；深色按底算 → 亮一档的主色）。
func TestRenderFullPlayerRowShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetClassic} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			t.Run(string(preset)+"-"+shade, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}
				_, tt := fullTesterWithCoverSettings(t, st, whitePNG(t, 1200, 1200))
				openFull(t, tt)
				settleFull(t, tt)
				// 指针停在「上一首」上：那一行里最能看出悬停反馈的一枚。
				box, ok := tt.Find("上一首")
				if !ok {
					t.Fatalf("找不到「上一首」，文字有 %q", tt.Texts())
				}
				tt.Move(box.X+box.W/2, box.Y+box.H/2)
				tt.Frame()
				settleFull(t, tt)
				f, err := os.Create(filepath.Join(dir, "fullplayer-row-"+string(preset)+"-"+shade+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
				f.Close()
			})
		}
	}
}

// fullProgressTester 只画满屏那一行进度，State 由调用方给。
//
// 背景铺的是**最亮的那一档封面**（纯白封面压完 55% 的黑 = #737373）——
// 和 `fullRowBase` 算的是同一个数，所以量出来的对比度和真机上一样。
//
// ⚠️ **主题要自己装**（`applyFrameTheme`）：`withView` 换掉的是整个 view，
// 而主题是 `viewMain` 第一行装的——不装的话量到的是框架那一份默认主题
// （主色是它自己的蓝 #2563eb、Border 是它自己的 #d9d9de），不是应用给用户
// 看的那一套。
func fullProgressTester(t *testing.T, st app.Settings, state app.State) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, at(900, 120), withSettings(st), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			mygoui.Box(c).Fill().Background(fullScrim.Over(mygoui.Color{R: 255, G: 255, B: 255, A: 255})).
				Children(func() {
					s.viewFullProgress(c, state, fullDimColor)
				})
		}
	}))
	tt := b.tt
	tt.Frame()
	settleFull(t, tt)
	return tt
}

// trackThickness 是**轨道**在那一条竖线上占几行。
//
// 判据是「和这一块的底色不一样」：轨道是 `t.Border`（经典那一套是不透明的
// `#dbdbdb`），底色是压暗过的封面（`#737373`）——两者差得开。
func trackThickness(tt *mygoui.Tester, x, y0, y1 int) int {
	img := tt.Image()
	base := img.RGBAAt(x, y0)
	n := 0
	for y := y0; y < y1; y++ {
		p := img.RGBAAt(x, y)
		if maxDiff(p, base) > 6 {
			n++
		}
	}
	return n
}
