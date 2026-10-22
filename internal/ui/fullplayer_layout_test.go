// 票 #169 的验收：满屏顶栏/底栏的留白与最大宽度、播放键 48、模式/队列两枚、
// 进度旋钮只在 hover 时画、设置面板的四个开关三项颜色、标题 20/400。
//
// 每一条都断**画面**（Tester 量出来的框、逐像素采样）或者**代码里的那一个调
// 用**（20/400 这种字重的事，画面上没有可靠的办法把它和 500 分开）。
package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// fullPlayLabel 是这一帧播放键的名字（在播时叫「暂停」）。
func fullPlayLabel(s *Shell) string { return transportLabel(s.app.Snapshot().Playing) }

// TestTheFullPlayerPlayKeyKeepsTheRowForeground 断满屏那一枚**悬停时不变主色**。
//
// 🔴 **这一条是评审查出来的（major）**：满屏的底是**压暗过的封面**，不是应用底色，
// 而「悬停变主色」那一档是按**应用底色**算的（`widgets/ink` 的 `AccentOn`）——
// 浅色主题下它是 rgb(18,114,52) 的深绿，压在满屏那个底（实测 {115,115,115}）上
// **只有 1.0:1**，比底色还暗，**等于看不见**；而且那一行其余四枚图标都是满屏自己的
// 前景色（白 92%），**一枚变绿另外四枚不变，一眼就不成套**。
//
// 所以满屏那一枚走 `iconButton`（图形是那一行的前景色），不走 `iconButtonPlay`
// （那一枚带「悬停变主色」）。
//
// 判据：悬停之后那一枚图形**仍然是中性色**（白 92% 那一档），不是绿的。
// 深浅两套都断：深色下主色是亮的绿，一样不是中性色。
func TestTheFullPlayerPlayKeyKeepsTheRowForeground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **自己搭一套，不用 `fullTester`**：那一支一律深色（它数的是
			// 「接近白的像素」），而这里要量的是**浅色下**那一档——浅色下主色当字色
			// 那一档最深，正好压在满屏压暗过的封面上。
			s, tt := newShell(t)
			s.clock = newFakeClock(time.Unix(1_700_000_000, 0))
			tt.SetDark(dark)
			if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			tt.Frame()
			if !s.app.Snapshot().HasTrack {
				t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
			}
			openFull(t, tt)
			settleFull(t, tt)

			btn, ok := tt.Find(fullPlayLabel(s))
			if !ok {
				t.Fatalf("画面上找不到满屏的播放键，文字有 %q", tt.Texts())
			}
			// 指针落到那一枚上。
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()

			// 那一格里**离底色最远**的那个像素就是图形本身。
			//
			// ⚠️ **不能找「最白」的那个**：主色当字色那一档在浅色下是**比底色还暗**
			// 的深绿（rgb(18,114,52)），那一格里面最白的像素反而是**底色**
			// （实测 115）——找「最白」就永远找不到那一枚图形（这是第一版的错法，
			// 浅色那一档因此绿了）。
			bg := modeOf(columnLeftOf(tt.Image(), btn))
			got := farthestFrom(tt.Image(), btn, bg)
			if absInt(int(got.G)-int(got.R)) > 12 || absInt(int(got.B)-int(got.R)) > 12 {
				t.Errorf("满屏的播放键悬停之后是 %v（底色 %v）——那不是满屏自己的"+
					"前景色（白 92%%），是主色那一档（浅色主题下压在满屏那个底上"+
					"只有 1.0:1，等于看不见；而且旁边四枚图标都还是白的）",
					got, bg)
			}
		})
	}
}

// columnLeftOf 是按钮左边三像素那一列的像素（当参照用）。
//
// ⚠️ **满屏那一枚左边是空的**：上一首和它之间隔着 24 的缝，而满屏的底是**压暗过
// 的封面**——那一列采到的是封面本身（局部底色）。
func columnLeftOf(img *image.RGBA, btn mygoui.Rect) []color.RGBA {
	x := round32(btn.X) - 3
	col := make([]color.RGBA, 0, int(btn.H))
	for dy := range int(btn.H) {
		col = append(col, img.RGBAAt(clampX(img, x), round32(btn.Y)+dy))
	}
	return col
}

// farthestFrom 是 box 里离 bg 最远的那一个像素——**图形本身**。
//
// ⚠️ **不能改成「找最白的那个」**：主色当字色那一档在浅色下是**比底色还暗**的
// 深绿，那一格里面最白的像素反而是底色。
func farthestFrom(img *image.RGBA, box mygoui.Rect, bg color.RGBA) color.RGBA {
	best, bestD := bg, 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if d := maxDiff(p, bg); d > bestD {
				best, bestD = p, d
			}
		}
	}
	return best
}

// TestTheFullPlayerTopBarPaddingMatchesTheUpstream 断顶栏 `px-6 py-4`。
//
// 上游 `index.tsx:291` 那一层是 `px-6 py-4`：左右 24、上下 16。判据是**按钮在
// 画面上的位置**：关闭键是 32 见方、贴在左上（x=24、y=16）；设置键和它同一条
// 中线；歌名紧跟在关闭键右边 8（`space-x-2`）。
//
// ⚠️ 无头 Tester 没有标题栏（`TitleBar().Left` 是 0），所以左边量到的就是
// `fullTopPadX` 本身；有标题栏时那一段让给红黄绿，另有一条测试盯着。
func TestTheFullPlayerTopBarPaddingMatchesTheUpstream(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)

	// 上游那几个类名换算出来的字面量：px-6 = 24、py-4 = 16、space-x-2 = 8；
	// 关闭键点击区 32、图标 28。
	const wantPadX, wantPadY, wantKey, wantGap = 24, 16, 32, 8
	closeBtn, ok := tt.Find("关闭全屏")
	if !ok {
		t.Fatalf("找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
	if d := absF(closeBtn.X - wantPadX); d > 0.5 {
		t.Errorf("「关闭全屏」在 x=%.1f，want %d（上游 px-6）", closeBtn.X, wantPadX)
	}
	if d := absF(closeBtn.Y - wantPadY); d > 0.5 {
		t.Errorf("「关闭全屏」在 y=%.1f，want %d（上游 py-4）", closeBtn.Y, wantPadY)
	}
	if closeBtn.W != wantKey || closeBtn.H != wantKey {
		t.Errorf("「关闭全屏」是 %.0fx%.0f，want %dx%d",
			closeBtn.W, closeBtn.H, wantKey, wantKey)
	}

	gear, ok := tt.Find("全屏设置")
	if !ok {
		t.Fatalf("找不到「全屏设置」，文字有 %q", tt.Texts())
	}
	// 设置键不是贴右边的：上游把它摆在**左边那一组**（`max-w-2/5`）里歌名后面
	// （`index.tsx:293-320`），右边那一格留给 Windows / Linux 的窗口按钮。
	// 这里只断它和关闭键同一条中线（py-4 那一份上下留白真的用上了）。
	if d := absF(gear.Y + gear.H/2 - wantPadY - wantKey/2); d > 0.5 {
		t.Errorf("「全屏设置」的中线在 y=%.1f，顶栏中线是 %.1f", gear.Y+gear.H/2, float32(wantPadY)+wantKey/2)
	}

	title, ok := tt.Find(fullTestTitle)
	if !ok {
		t.Fatalf("找不到歌名 %q，文字有 %q", fullTestTitle, tt.Texts())
	}
	if d := title.X - (closeBtn.X + closeBtn.W) - wantGap; absF(d) > 0.5 {
		t.Errorf("歌名在 x=%.1f，关闭键右沿 %.1f，中间那道缝 %.1f，want %d（上游 space-x-2）",
			title.X, closeBtn.X+closeBtn.W, title.X-(closeBtn.X+closeBtn.W), wantGap)
	}
}

// TestTheFullPlayerBottomBarPaddingAndMaxWidth 断底栏 `px-12 mb-4 max-w-6xl`。
//
// 上游 `index.tsx:394`：左右 48、离底 16、最宽 1152。判据分两处量：
//
//   - **播放键的底沿**：离窗口底正好 `mb-4`（16）。它也是底栏里最高的那一枚。
//   - **进度条的位置和宽度**：窗口 1200 宽时内容区是 1152 − 96 = 1056；两边各
//     一个 48 宽的时间标签、两道 10 的缝，中间那条进度就该是 940。
func TestTheFullPlayerBottomBarPaddingAndMaxWidth(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	settleFull(t, tt)

	// 上游那几个类名换算出来的字面量：px-12 = 48、mb-4 = 16、max-w-6xl = 1152；
	// 播放键的盒子是 48（上游 `size-12 min-w-12`，图形正好填满）。
	const wantPadX, wantMargin, wantMaxW, wantHit = 48, 16, 1152, 48
	play, ok := tt.Find(fullPlayLabel(s))
	if !ok {
		t.Fatalf("找不到播放键，文字有 %q", tt.Texts())
	}
	if play.W != wantHit || play.H != wantHit {
		t.Errorf("播放键是 %.0fx%.0f，want %dx%d（上游的 `size-12 min-w-12`）",
			play.W, play.H, wantHit, wantHit)
	}
	if d := float32(fullWinH) - (play.Y + play.H) - wantMargin; d != 0 {
		t.Errorf("播放键底沿在 %.1f，离窗口底 %.1f，want %d（上游 mb-4）",
			play.Y+play.H, float32(fullWinH)-(play.Y+play.H), wantMargin)
	}
	if d := absF(play.X + play.W/2 - float32(fullWinW)/2); d > 1 {
		t.Errorf("播放键中心在 x=%.1f，窗口中线是 %.1f", play.X+play.W/2, float32(fullWinW)/2)
	}

	prog, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到进度条，文字有 %q", tt.Texts())
	}
	// ⚠️ **那两个读数是「最少 48 宽」，不是「正好 48 宽」**（票 #222）：超一小时的
	// 歌读数是 `01:30:00`，比 48 宽——钉死 48 的话它会**换行**。平时（读数短）
	// 它们就是 48，所以这里的算式一个字不用改。
	const timeW, gap = 48, 10 // viewFullProgress 里那两个数
	wantW := float32(wantMaxW - 2*wantPadX - 2*timeW - 2*gap)
	if d := absF(prog.W - wantW); d > 1 {
		t.Errorf("进度条宽 %.1f，want %.0f（1152 − 左右各 48 − 两个 48 的时间 − 两道 10 的缝）",
			prog.W, wantW)
	}
	wantX := float32((fullWinW-wantMaxW)/2 + wantPadX + timeW + gap)
	if d := absF(prog.X - wantX); d > 1 {
		t.Errorf("进度条左沿在 %.1f，want %.0f（居中的 1152 那一条 + px-12）", prog.X, wantX)
	}

	// 窗口再宽，那一条也不跟着摊开：`max-w-6xl` 是**上限**，多出来的两份
	// 平分在左右。1600 宽时进度条还是 940，中心落在窗口中线。
	const wideW = 1600
	tt.SetSize(wideW, fullWinH)
	tt.Frame()
	prog, ok = tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("窗口改成 %d 之后找不到进度条，文字有 %q", wideW, tt.Texts())
	}
	if d := absF(prog.W - wantW); d > 1 {
		t.Errorf("窗口 %d 宽时进度条变成了 %.1f，want 还是 %.0f——`max-w-6xl` 没有生效",
			wideW, prog.W, wantW)
	}
	if d := absF(prog.X + prog.W/2 - wideW/2); d > 1 {
		t.Errorf("窗口 %d 宽时进度条中心在 %.1f，不在窗口中线 %.1f",
			wideW, prog.X+prog.W/2, float32(wideW)/2)
	}
}

// TestTheFullPlayerTimeReadsDoNotWrap 断**满屏上超一小时的读数不换行**。
//
// 🔴 票 #222 把两个读数从「**正好** 48 宽」改成「**最少** 48 宽」——超一小时的歌
// 读数是 `01:30:00`（八个字符，比 48 宽），钉死 48 的话它会**换行**，而满屏的
// 底栏只有一行高。换行之后那一格变成「01:30:」加「00」两行，而**界面上看不出
// 坏了**（数字还在，只是分了两行）。
//
// ⚠️ **判据是盒高**：一行字是 12 号 × 1.1767 ≈ 14 像素，两行 ≈ 28。
//
// ⚠️ **顺带把那一档的代价钉住**：读数宽了，**进度条就窄了**（那一行是 flex，
// 读数占了多少就剩多少给滑块）。这是**故意**的——宁可进度条短一点，也不要读数
// 换行。
func TestTheFullPlayerTimeReadsDoNotWrap(t *testing.T) {
	long, _ := fullStateTester(t, app.State{
		HasTrack: true, Title: "一小时以上的一首",
		Duration: 75 * time.Minute, Position: 90 * time.Minute,
		Volume: 1, Rate: 1, PlayMode: app.PlayModeLoop,
	})
	for _, want := range []string{"01:30:00", "01:15:00"} {
		r, ok := long.Find(want)
		if !ok {
			t.Fatalf("满屏上找不到读数 %q，文字有 %q", want, long.Texts())
		}
		if r.H > 20 {
			t.Errorf("满屏上的读数 %q 占了 %.1f 像素高——它**换行**了"+
				"（一行是 14 上下，两行是 28）", want, r.H)
		}
	}

	// 那一档的代价：读数宽了，进度条就窄了。
	short, _ := fullStateTester(t, app.State{
		HasTrack: true, Title: "短的一首",
		Duration: 100 * time.Second, Position: 42 * time.Second,
		Volume: 1, Rate: 1, PlayMode: app.PlayModeLoop,
	})
	longBar, ok := long.Find("全屏播放进度")
	if !ok {
		t.Fatalf("满屏上找不到进度条，文字有 %q", long.Texts())
	}
	shortBar, ok := short.Find("全屏播放进度")
	if !ok {
		t.Fatalf("满屏上找不到进度条，文字有 %q", short.Texts())
	}
	if longBar.W >= shortBar.W {
		t.Errorf("超一小时那一档的进度条是 %.1f，短读数那一档是 %.1f——"+
			"读数宽了 8 个字符，进度条却一点没让", longBar.W, shortBar.W)
	}
}

// fullStateTester 只画满屏播放器，状态由测试给。
//
// ⚠️ **不走 `fullTester`**：那一个放的是真本地文件（4 秒），而这一族要摆
// 「超一小时」那一档——真放不出那么长的歌。
//
// ⚠️ **主题自己装**（`applyFrameTheme`）：满屏播放器是页面，按铁律二第一件正事
// 就是装主题；而隔离出来画的时候没人替它装。
func fullStateTester(t *testing.T, st app.State) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newTestShell(t, at(fullWinW, fullWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			// 直接开，不走 `openFullPlayer`：那一条会装一个三秒的隐藏定时器
			//（界面三秒不动就藏起来）。
			s.full.open = true
			s.viewFullPlayer(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// TestTheFullPlayerBottomBarHasPlayModeAndQueue 断底栏是
// 「模式 · 上一首 · 播放 · 下一首 · 队列」（上游 `index.tsx:396-399`）。
//
// 「队列」那一枚以前没摆，理由是抽屉画在主布局里、会被满屏盖住。现在满屏开着
// 的时候抽屉由满屏自己画（见 `viewFullPlayer` 末尾），所以这里连**按下去真的
// 开得出来**一起断——摆一枚按不动的键比不摆更坏。
func TestTheFullPlayerBottomBarHasPlayModeAndQueue(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	settleFull(t, tt)

	labels := []string{"播放模式", "上一首", fullPlayLabel(s), "下一首", "播放列表"}
	last := float32(-1)
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("底栏没有 %q，文字有 %q", label, tt.Texts())
		}
		if r.X <= last {
			t.Errorf("%q 在 x=%.1f，不在前一枚的右边（%.1f）——顺序不对", label, r.X, last)
		}
		last = r.X
	}

	// 按队列那枚：抽屉真的要开，而且要画在满屏**上面**。只看 `s.drawerOpen`
	// 不够：抽屉画在满屏底下的话这个布尔照样是 true，而用户什么也看不见。
	//
	// 判据是**抽屉那一块的颜色变了**：满屏的底（主题背景 + 压暗）和抽屉的
	// `t.Surface` 不是一个色。
	const probeX, probeY = float32(fullWinW - queueDrawerWidth/2), float32(120)
	before := tt.Image().RGBAAt(int(probeX), int(probeY))
	if err := tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了「播放列表」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !s.drawerOpen {
		t.Fatal("按了「播放列表」，抽屉的状态没有开")
	}
	if after := tt.Image().RGBAAt(int(probeX), int(probeY)); after == before {
		t.Error("抽屉开了，可它那一块的颜色没变——它画在满屏底下，用户看不见")
	}
	for _, want := range []string{"清空", "定位当前播放"} {
		if !tt.HasText(want) {
			t.Errorf("按了「播放列表」但抽屉里的 %q 没画出来（它被满屏盖住了？），文字有 %q",
				want, tt.Texts())
		}
	}

	// 抽屉底下让出来的是**满屏的底栏**（92 高），不是播放条那 88：进度条整条
	// 都该按得着。点它一下，抽屉不该被当成「点了蒙版」关掉。
	prog, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到进度条，文字有 %q", tt.Texts())
	}
	tt.ClickAt(prog.X+prog.W*0.6, prog.Y+prog.H/2)
	tt.Frame()
	if !s.drawerOpen {
		t.Error("抽屉开着时点了一下进度条，它被蒙版吃掉了（让出的高度还按播放条算？）")
	}

	// 点左边那片蒙版关掉（和主窗口里同一个手势）。
	tt.ClickAt(10, 400)
	tt.Frame()
	if s.drawerOpen {
		t.Error("点了抽屉外面，它没有关")
	}
}

// TestTheFullPlayerPlayGlyphIsFortyEight 断播放键的**图形**是 48 的。
//
// 按钮框是 48（上游 `size-12 min-w-12`，见上一条）；图形本身是 48——上游
// `music-play-control/index.tsx:23-25` 的 `RiPlayCircleFill size={48}`。
// 判据是图形墨迹的外接框：一个画满 viewBox 的圆形图标在 48 字号下量出来
// 40~48 之间（图形自己留了一圈边），34 那一档量出来只有 28 上下。
//
// ⚠️ **票 #217 之后那一枚底下什么都没有了**（原来是实心主色方块），于是「框内的
// 众数」是**图形本身**而不是底色，`scanGlyph` 量到的那一片「墨迹」其实是
// **圆盘边缘那一圈抗锯齿**——它的外接框和圆盘的外接框是同一个数（40 + 4 的
// 回扩 = 44），所以这一条量到的仍然是图形的大小。
func TestTheFullPlayerPlayGlyphIsFortyEight(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	settleFull(t, tt)

	g := scanGlyph(tt, fullPlayLabel(s))
	if g.ink == "" {
		t.Fatalf("播放键那一块没画东西，文字有 %q", tt.Texts())
	}
	const wantGlyph = 48 // 上游 `RiPlayCircleFill size={48}`
	long := max(g.box.W, g.box.H)
	if long < 40 || long > wantGlyph {
		t.Errorf("播放图形的外接框是 %.1fx%.1f，want 落在 40~%d（48 的字号）——"+
			"34 那一档量出来只有 28 上下", g.box.W, g.box.H, wantGlyph)
	}
}

// TestTheFullPlayerProgressKnobIsOnlyPaintedOnHover 断「旋钮只在悬停或按下时画」。
//
// 上游满屏那条进度传的是 `trackClassName="h-[6px]"`，而 `MusicPlayProgress`
// 自己按 `hideThumb={!showThumb}` 决定画不画旋钮（`music-progress/index.tsx:22`）。
// 以前这一根用的是常驻旋钮（20×16），一条进度上永远亮着一个白点。
//
// 判据是**进度条框里有没有接近白的像素**：深色主题下进度条自己只画主色/分隔线，
// 白色只可能来自旋钮。指针挪到别处时不画；压上去之后画。
func TestTheFullPlayerProgressKnobIsOnlyPaintedOnHover(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)
	settleFull(t, tt)

	prog, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到进度条，文字有 %q", tt.Texts())
	}
	// 指针先离开（Tester 默认在 (0,0)，但满屏打开时那一下可能已经移过来了）。
	tt.Move(5, 400)
	tt.Frame()
	if n := brightIn(tt.Image(), prog); n != 0 {
		t.Errorf("没悬停的时候进度条框里有 %d 个亮像素——旋钮一直画着", n)
	}

	tt.Move(prog.X+prog.W/2, prog.Y+prog.H/2)
	tt.Frame()
	if n := brightIn(tt.Image(), prog); n == 0 {
		t.Errorf("指针压在进度条上，旋钮还是没画出来（%v 一个亮像素都没有）", prog)
	}

	// 移开之后又收起来（不是「悬停过一次就常驻」）。
	tt.Move(5, 400)
	tt.Frame()
	if n := brightIn(tt.Image(), prog); n != 0 {
		t.Errorf("指针移开之后进度条框里还有 %d 个亮像素——旋钮没有收回去", n)
	}
}

// TestTheFullPlayerSettingsPanelHasEverySwitchAndColor 断设置面板补齐了
// 「四个开关 + 三项颜色」（上游 `settings-panel.tsx`）。
//
// 三项颜色各有各的**出现条件**，照上游：
//
//	歌词字体颜色   showLyrics 开着
//	频谱图颜色     showSpectrum 开着
//	背景颜色       showBlurredBackground 关着
//
// 所以这里一项一项地按开关，看颜色行是不是跟着出现/收起。默认值也钉住：
// 歌词开着、频谱关着、虚化开着（Biu 的 `defaultSettings`）。
func TestTheFullPlayerSettingsPanelHasEverySwitchAndColor(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()

	// 四个开关都在（找可见的那行标题；开关自己叫「开关：…」）。
	for _, want := range []string{"显示歌词", "显示频谱图", "显示封面", "显示虚化背景"} {
		if _, ok := tt.Find(want); !ok {
			t.Fatalf("设置面板里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 默认：歌词那项的颜色在（歌词默认开着），另外两项不在。
	//
	// ⚠️ 用 `Find`（精确等于）而不是 `HasText`（包含）：取色器的名字是
	// 「色块：歌词字体颜色」，包含关系会让「那一行收起了没有」永远为真。
	if _, ok := tt.Find("歌词字体颜色"); !ok {
		t.Errorf("歌词默认开着，却没有「歌词字体颜色」那一行")
	}
	if _, ok := tt.Find("频谱图颜色"); ok {
		t.Errorf("频谱默认关着，「频谱图颜色」那一行却已经在了")
	}
	if _, ok := tt.Find("背景颜色"); ok {
		t.Errorf("虚化背景默认开着，「背景颜色」那一行却已经在了")
	}
	// 三块取色器本身（不是只有标题文字）也要认得出来。
	for _, want := range []string{"色块：歌词字体颜色"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("「歌词字体颜色」只有字、没有取色器，文字有 %q", tt.Texts())
		}
	}

	// 打开频谱：颜色行出现。
	if err := tt.Click("开关：显示频谱图"); err != nil {
		t.Fatalf("点不了频谱开关：%v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("频谱图颜色"); !ok {
		t.Errorf("开了频谱，「频谱图颜色」那一行没有出现，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("色块：频谱图颜色"); !ok {
		t.Errorf("开了频谱，取色器没有出现，文字有 %q", tt.Texts())
	}

	// 关掉虚化背景：第三块颜色出现，而且默认显示的是主题底色（可以点开取色）。
	if err := tt.Click("开关：显示虚化背景"); err != nil {
		t.Fatalf("点不了虚化背景开关：%v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("背景颜色"); !ok {
		t.Errorf("关了虚化背景，「背景颜色」那一行没有出现，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("色块：背景颜色"); !ok {
		t.Errorf("关了虚化背景，取色器没有出现，文字有 %q", tt.Texts())
	}
	if s.app.FullPlayerPrefs().ShowBlurredBackground {
		t.Error("点了虚化背景开关，偏好里还是开着的")
	}

	// 关掉歌词：它的颜色行跟着收起（不是永远摆着）。
	if err := tt.Click("开关：显示歌词"); err != nil {
		t.Fatalf("点不了歌词开关：%v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("歌词字体颜色"); ok {
		t.Errorf("歌词关掉了，「歌词字体颜色」那一行还摆着，文字有 %q", tt.Texts())
	}

	// 色块真的能点开取色器，而且取色器开在设置面板里面**不会把面板顶掉**
	// （`ColorWell` 自己也是一个浮层；面板是另一层）。
	if err := tt.Click("色块：背景颜色"); err != nil {
		t.Fatalf("点不了背景色块：%v", err)
	}
	tt.Frame()
	if !tt.HasText("Hex") {
		t.Errorf("点了色块，取色器没有出现，文字有 %q", tt.Texts())
	}
	if !s.full.settings {
		t.Error("点开取色器之后设置面板自己关掉了")
	}
}

// TestTurningOffTheCoverHidesIt 断「显示封面」真的管画面。
//
// 摆一个按下去什么都不变的开关比不摆更坏（见 `fullplayer_settings.go` 顶上）。
func TestTurningOffTheCoverHidesIt(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	if _, ok := tt.Find("当前封面"); !ok {
		t.Fatalf("满屏里本来就没有封面，文字有 %q", tt.Texts())
	}
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v", err)
	}
	tt.Frame()
	if err := tt.Click("开关：显示封面"); err != nil {
		t.Fatalf("点不了封面开关：%v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("当前封面"); ok {
		t.Error("关掉「显示封面」之后封面还在画面上")
	}
	if s.app.FullPlayerPrefs().ShowCover {
		t.Error("偏好里没有记下封面关着")
	}
}

// TestTurningOffTheBlurredBackgroundUsesTheChosenColor 断第三块颜色真的管画面。
//
// 虚化背景关掉之后：底是 `BackgroundColor` 那一层纯色（空串 = 主题底色），
// 而且前景色要跟着底走——浅底上必须换成深色的字，否则用户挑了一个浅色背景
// 就什么也看不见了（上游 `readableColor`，`index.tsx:161-170`）。
func TestTurningOffTheBlurredBackgroundUsesTheChosenColor(t *testing.T) {
	s, tt, _ := fullTester(t)
	p := app.DefaultFullPlayerPrefs()
	p.ShowBlurredBackground, p.BackgroundColor = false, "#123456"
	s.app.SetFullPlayerPrefs(p)
	openFull(t, tt)
	tt.Frame()

	// 左上角那一块没有控件，量到的就是底色本身。
	if got := tt.Image().RGBAAt(5, 5); absDiff(got.R, 0x12) > 2 || absDiff(got.G, 0x34) > 2 || absDiff(got.B, 0x56) > 2 {
		t.Errorf("左上角是 %v，want 约 #123456（关掉虚化背景之后铺的那个颜色）", got)
	}
	// 深底：歌名的墨迹是亮的。
	title, ok := tt.Find(fullTestTitle)
	if !ok {
		t.Fatalf("找不到歌名，文字有 %q", tt.Texts())
	}
	if n := brightIn(tt.Image(), title); n == 0 {
		t.Error("深色背景上歌名一个亮像素都没有——字是暗的，看不见")
	}

	// 浅底反过来给深色的字：改成 #f0f0f0 之后，歌名那一块里必须出现深色的
	// 墨迹（白字压在上面对比度只有 1.1:1，等于没有——而底色本身是亮的，不会
	// 被 darkIn 数进去）。
	p.BackgroundColor = "#f0f0f0"
	s.app.SetFullPlayerPrefs(p)
	tt.Frame()
	title, _ = tt.Find(fullTestTitle)
	if n := darkIn(tt.Image(), title); n == 0 {
		t.Error("浅色背景上歌名没有一个深色像素——前景没有跟着底换成深色，字看不见")
	}

	// 中灰也归「浅」：0.5 那个阈值会给白字（对比度只有 3:1），0.179 给黑字
	// （7:1）。这是 Biu 的 color2k `readableColor` 用的那个分界。
	p.BackgroundColor = "#808080"
	s.app.SetFullPlayerPrefs(p)
	tt.Frame()
	title, _ = tt.Find(fullTestTitle)
	if n := darkIn(tt.Image(), title); n == 0 {
		t.Error("中灰背景上歌名用的是白字——白字在 #808080 上只有 3:1，该换黑字")
	}
}

// ---- 标题 20/400 ----

// TestTheFullPlayerTitleIsNotBold 断标题的字重是 400。
//
// # 为什么用 AST 而不是像素
//
// 像素那一层量不出「400 还是 500」：这两个字重在 20px 下墨迹只差一两个像素，
// 而抗锯齿和字体替换带来的差别比那还大。能钉住的是代码里的**那一个调用**
// （`FontWeight(400)` 且没有 `Bold()`），和 `pagerhythm_test.go` 钉页标题字号
// 是同一个路数。
//
// 字号那一半在 `fullplayer_glass_test.go` 的
// `TestTheFullPlayerTitleIsTwentyPixelsAndAtMostTwoFifths`（量行高）。
func TestTheFullPlayerTitleIsNotBold(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fullplayer.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 fullplayer.go 失败：%v", err)
	}

	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok || found {
			return true
		}
		// 只认那一条：`ui.Text(c, st.Title)…`。
		mentionsTitle := false
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Text" {
				return true
			}
			for _, arg := range call.Args {
				if s, ok := arg.(*ast.SelectorExpr); ok {
					if x, ok := s.X.(*ast.Ident); ok && x.Name == "st" && s.Sel.Name == "Title" {
						mentionsTitle = true
					}
				}
			}
			return true
		})
		if !mentionsTitle {
			return true
		}
		found = true
		weight := -1
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Bold":
				t.Errorf("满屏歌名还在调 `.Bold()`——票 #169 要的是 20/400（上游 h2 被 Preflight 重置成 inherit）")
			case "FontWeight":
				if len(call.Args) == 1 {
					if lit, ok := call.Args[0].(*ast.BasicLit); ok {
						if lit.Value == "400" {
							weight = 400
						}
					}
				}
			}
			return true
		})
		if weight != 400 {
			t.Errorf("满屏歌名没有写 `.FontWeight(400)`——找不到那一句，或者写的是别的字重")
		}
		return false
	})
	if !found {
		t.Fatal("fullplayer.go 里找不到 `ui.Text(c, st.Title)` 那一句——测试找错了地方")
	}
}

// TestRenderFullPlayerSettingsShot 存图：满屏播放器的设置面板，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerSettingsShot
//
// 三块颜色各有各的出现条件（见上一条），所以出图时把频谱打开、虚化关掉——这样
// 四个开关和三块颜色**在同一张图里**，评审一眼能对齐全不全。
func TestRenderFullPlayerSettingsShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := fullTesterWithCover(t)
			// 换假时钟：下面要推 90 帧等封面转场，真时钟下机器一慢就可能推过
			// 三秒、界面自己藏起来，出出来的是一张空图。
			s.clock = newFakeClock(time.Unix(1_700_000_000, 0))
			tt.SetDark(dark)
			// 先把三块颜色都摆出来：频谱开、虚化关、挑一个看得见的背景色。直接
			// 写偏好比在面板里点两下省事，测的也不是那两下（上面有专门的测试）。
			p := app.DefaultFullPlayerPrefs()
			p.ShowSpectrum, p.ShowBlurredBackground = true, false
			p.BackgroundColor = "#334455"
			s.app.SetFullPlayerPrefs(p)
			openFull(t, tt)
			// ⚠️ **不推钟等动画走完**：频谱开着、歌又在放，它每一帧都要下一帧
			// （`p.AnimationFrame()`），`settleFull` 会一直等不到。封面那个共享
			// 元素转场是 Tester 的钟驱动的，推 1.4 秒让它落到中间。
			for range 90 {
				tt.Advance(16 * time.Millisecond)
				tt.Frame()
			}
			if err := tt.Click("全屏设置"); err != nil {
				t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
			}
			tt.Frame()
			tt.Frame()
			save(t, dir, "62-全屏播放器-设置面板-"+name+".png", tt)
		})
	}
}

// TestEscapeOnTheOpenQueueClosesOnlyTheQueue 断两层的 Esc 不会一起关。
//
// 满屏和抽屉各有一个「Esc 关掉自己」的钩子。抽屉现在画在满屏上面——按一次
// Esc 只该收抽屉，不该连满屏一起退回普通窗口（用户按 Esc 是想关掉浮在上面的
// 那一层）。
func TestEscapeOnTheOpenQueueClosesOnlyTheQueue(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	settleFull(t, tt)
	if err := tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了「播放列表」：%v", err)
	}
	tt.Frame()
	if !s.drawerOpen {
		t.Fatal("抽屉没开")
	}
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.drawerOpen {
		t.Error("按了 Esc，抽屉没关")
	}
	if !s.full.open {
		t.Error("按一次 Esc 连满屏也一起关了——只该收浮在上面的抽屉")
	}
}

// ---- 小帮手 ----

// darkIn 是 box 里接近黑的像素个数（浅色背景上量深色的字）。
func darkIn(img *image.RGBA, box mygoui.Rect) int {
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R < 100 && p.G < 100 && p.B < 100 {
				n++
			}
		}
	}
	return n
}
