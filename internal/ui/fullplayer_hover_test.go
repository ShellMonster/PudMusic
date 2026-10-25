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
// # 四条判据
//
//  1. **图形变了**：悬停那一帧的颜色和平时那一帧差得够多（RGB 距离 ≥ 24）——
//     「量得出来，不是看着差不多」；
//  2. **图形看得见**：悬停时图形和**那一行的底**的对比度 ≥ 3:1（WCAG 1.4.11
//     非文字那一档）；
//     2b. **图形和悬停时真正压在它背后那一层**（淡底）也看得见（≥ 2.5:1）——
//     那一层是**悬停反馈本身**，它把底下的一切都压低一档（见下面那段）；
//  3. **多了一层淡底**：悬停时那一枚底下多出一层半透明的前景色——按**那一行的
//     底**算（深底压白、浅底压黑），不是按主题正文色算。
//
// ⚠️ **底色是「最亮的那一档封面」**：纯白的封面压完 55% 的黑是 `#737373`
// （`fullRowBase`），而按它算出来的颜色在**任何**一档封面上都读得出来。
//
// ⚠️ **第 2 条和第 2b 条量的不是同一个东西**：第 2 条量的是「图形 vs 这一行
// 坐着的那块底」，第 2b 条量的是「图形 vs 悬停时**真正压在它背后**的那一层
// 淡底」（8% 的白，上游 `bg-foreground/10` 就是这么一层）。**两层都要量**——
// 只量前者会漏掉「淡底把图形压下去一档」这件事。
//
// ⚠️ **深底上「色相」和「亮度对比」只能要一个**：那一行的图标平时是白 92%
// （对底 4.31:1），悬停时换成主色（色相明显的那一档）就必然掉一档亮度——
// 所以实测是：对底 3.10:1、对淡底 2.65:1。**相对改之前**（浅色主题下悬停是
// 深绿压深底 1.31:1，等于看不见）这是一大截。理由写在
// `widgets/flat` 的 `IconButtonAccentOnBase` 上面。
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
					//
					// ⚠️ **这是「图形 vs 那一行坐着的那块底」**：悬停时图形底下多出来
					// 那一层淡底（8% 的白）是**悬停反馈本身**（上游 `bg-foreground/10`
					// 就是这么一层），它把这一枚底下的一切都压低一档——所以下面第 2b
					// 条单独量「图形 vs 那一层淡底」。
					if r := contrastRatio(hot, base); r < 3 {
						t.Errorf("%s 悬停时图形 %v 和它背后那一行的底 %v 只有 %.2f:1"+
							"（门槛 3:1）——图标在暗底上读不出来", label, hot, base, r)
					}
					// 2b. 图形和**悬停时真正压在它背后那一层**（淡底）也得看得见。
					//
					// ⚠️ **门槛是 2.5:1 而不是 3:1**：那一层淡底是**悬停反馈本身**，
					// 它把底下的一切都压低一档（实测 2.65~2.67:1）——和上游那套
					// 「浅色图标压在浅色悬停底上」的写法一样，这一层天生就把局部
					// 对比度拉低。而它必须**比改之前那 1.0:1 高得多**：改之前浅色
					// 主题下悬停时那枚图标是深绿压深底，**等于看不见**。
					if r := contrastRatio(hot, over); r < 2.5 {
						t.Errorf("%s 悬停时图形 %v 和它背后真正压着的那层淡底 %v 只有 %.2f:1"+
							"（门槛 2.5:1）——图标在那层淡底上读不出来", label, hot, over, r)
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

	tt := fullProgressTester(t, st, app.State{HasTrack: true, Duration: 60, Position: 30}, 0.5)
	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}

	// 填过的那一段在**左边**（值 0.5），轨道在**右边**。
	fillX := int(box.X + box.W*0.25)
	trackX := int(box.X + box.W*0.75)
	restFill := pixelAt(tt.Image(), float32(fillX), box.Y+box.H/2)
	restTrackPix := pixelAt(tt.Image(), float32(trackX), box.Y+box.H/2)
	restTrack := trackThickness(tt, trackX, int(box.Y), int(box.Y+box.H))

	// 0. ⚠️ **先证明「填过的那一段确实画出来了」**：量的是 25% 那一格，而轨道在
	// 75% 那一格——两格得**不一样**，否则量到的就是轨道（那一段压根没画），后面
	// 那几条断言会**恒过**（这就是评审指出来的那一条：改之前的用例就是这么空的）。
	if maxDiff(restFill, restTrackPix) <= 6 {
		t.Errorf("满屏那条进度 25%% 那一格和 75%% 那一格（轨道）一样（%v）——"+
			"填过的那一段压根没画出来，后面那几条断言都是空的", restFill)
	}
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
// # 🔴 **进度得停在中间，否则这一条是空的**
//
// ⚠️ **时长为 0 的时候 `viewFullProgress` 不再改 `s.fullScrub`**（它只在
// `known`（`st.Duration > 0`）时按 `Position/Duration` 写），于是那一份进度**一直
// 是 0**——填过的那一段**压根没画**，量到的 25% 那一格就是**轨道**（无彩色），
// 断言恒过。**改之前的代码上这一条也是绿的**——它抓不住自己要抓的 bug。
//
// 所以这里**把进度强行停在 50%**（`s.fullScrub`）：填过的那一段这才真的画出来，
// 于是量到的才是它。
//
// # 三条判据
//
//  1. **那一段确实画出来了**：25% 那一格和 75% 那一格（轨道）**不一样**
//     ——不一样才说明量的是「填过的那一段」，不是轨道；
//  2. **它不是主色**：**无彩色**（轨道色）——改之前那一档是主色（有彩色，
//     实测 {98 191 131}，绿杠）。
//
// ⚠️ 第 1 条是**防空跑**的：没有它，第 2 条量的是轨道（也是无彩色），
// 恒过。
func TestTheDisabledProgressBarIsNotAccent(t *testing.T) {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight

	// 时长 0 → 那一根是禁用的；进度强行停在 50%（见上面那段注释）。
	tt := fullProgressTester(t, st, app.State{HasTrack: true, Duration: 0, Position: 0}, 0.5)
	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}
	fill := pixelAt(tt.Image(), box.X+box.W*0.25, box.Y+box.H/2)
	track := pixelAt(tt.Image(), box.X+box.W*0.75, box.Y+box.H/2)

	// 1. 那一段确实画出来了（否则后面两条量的都是轨道）。
	if maxDiff(fill, track) <= 6 {
		t.Errorf("禁用的满屏进度条 25%% 那一格和 75%% 那一格（轨道）一样（%v）——"+
			"填过的那一段压根没画出来，这条用例是空的", fill)
	}
	// 2. 它不是主色。
	if sp := channelSpread(fill); sp > 6 {
		t.Errorf("禁用的满屏进度条填过的那一段是 %v（有彩色，跨度 %d）——"+
			"一根不能拖的进度条不该是主色的（改之前那一档是主色）", fill, sp)
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
func fullProgressTester(t *testing.T, st app.Settings, state app.State, scrub float64) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, at(900, 120), withSettings(st), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			// ⚠️ **进度先摆到 scrub 上**：时长为 0 的时候 `viewFullProgress` 不再改
			// 它（那一份会一直是 0，填过的那一段压根不画——见
			// `TestTheDisabledProgressBarIsNotAccent` 那段注释）。
			s.fullScrub = scrub
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
