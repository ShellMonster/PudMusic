// 全屏播放器这一票的验收：毛玻璃在、底下的内容看得清、点得动，底部控制条和顶栏
// 一样地淡入，旋钮和分集面板那两处玻璃，外加歌名的字号和宽度。
//
// 这一族走的是仓库现成的接缝：外壳与测试器基座（shelltest_test.go）、假时钟
// （clock.go）、`BIU_SHOT_DIR` 出图、逐像素采样。
//
// ⚠️ **断言的是「那一层是毛玻璃」和「糊开了」，不是和 Biu 的截图逐像素对齐**：
// 框架的毛玻璃只有两档、也没有按设备分级那套入口（票面写明「不做」），所以
// 逐像素对齐本来就不是这一票的判据。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/plugins/glass"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 毛玻璃 ----

// TestTheFullPlayerBackdropIsTheFrameworksOwnGlass 断「背景那一层是毛玻璃材质」。
//
// 判据是**材质本身**，不是画面上的像素：那一层糊的必须是**框架自带**的那一份
// （`plugins/glass`：Metal / HLSL / GLSL 三套着色器 + CPU 等价实现，三端一致），
// 而不是我们自己画的等效效果。
//
// ⚠️ 为什么读得到：框架的 `Element.Material` 只往节点上设、没有读回来的办法
// （`mygo/ui/frame.go:144`），所以这一层把「背景那一层糊的是什么」收在一个地方
// （`fullBackdropMaterial`），这里读的就是它。换回一层纯色、或者换成自绘的
// 等效效果，这一条立刻红。
func TestTheFullPlayerBackdropIsTheFrameworksOwnGlass(t *testing.T) {
	switch fullBackdropMaterial(fullBackdropBlur).(type) {
	case glass.Blur, glass.Glass:
		// 就是框架那一份。
	default:
		t.Fatalf("背景那一层糊的是 %T，不是框架自带的毛玻璃（glass.Blur / glass.Glass）",
			fullBackdropMaterial(fullBackdropBlur))
	}
	// 而它得真的是**糊**的：半径不能是 0（0 等于没糊）。
	if b, ok := fullBackdropMaterial(fullBackdropBlur).(glass.Blur); ok && b.Radius <= 0 {
		t.Errorf("背景那一层的模糊半径是 %g——那等于没糊", b.Radius)
	}
	// 而画布上的那个半径要换算过：画布是窗口的 1/4，屏幕上的糊的程度仍然是 σ=32
	// DIP（窗口多大、屏幕多密都一样）。
	if got, want := fullBackdropSigma(300, 1200), float32(8); got != want {
		t.Errorf("300 宽的画布上的半径是 %g，want %g（画布是窗口的 1/4，"+
			"屏幕上的模糊强度要一样）", got, want)
	}
}

// TestTheBackdropIsBlurredOnceAndThenShownAsAPicture 断「毛玻璃不是每帧重渲一遍」。
//
// ## 为什么要有这一条
//
// 毛玻璃是**画的时候才生效**的（框架的 `Material`）：满窗 1200×800 上 σ=32，
// **软件渲染**（框架的回退路）每一帧要多花 **80~100 毫秒**——满屏会掉到
// 8~12 帧/秒（GPU 上没有这个代价，报告里有实测）。
//
// 所以背景那一层是**一张画布**：封面变了或者窗口改了尺寸才重画一次（那一次才
// 糊），稳态下每一帧只是把那张画布当图贴。
//
// 判据是**画布被重画了几次**：画十帧，它只该重画一次。
func TestTheBackdropIsBlurredOnceAndThenShownAsAPicture(t *testing.T) {
	s, tt := fullTesterWithCover(t)
	waitFrames(t, tt, "封面下回来了", func() bool {
		return s.images.Get(s.app.Snapshot().CoverURL).Ready()
	})
	openFull(t, tt)
	waitFrames(t, tt, "背景糊好了", func() bool { return s.full.bgDraws > 0 })

	before := s.full.bgDraws
	for range 10 {
		tt.Frame()
	}
	if got := s.full.bgDraws - before; got != 0 {
		t.Errorf("画了十帧，那张画布被重画了 %d 次——毛玻璃于是每帧重渲一遍"+
			"（软件渲染上那是 80~100 毫秒一帧）", got)
	}
}

// TestTheFullPlayerBackdropIsActuallyBlurred 断「那一层真的把封面糊开了」。
//
// ## 判据是**边摊开了多宽**，不是「每一步多大」
//
// 背景那一层是**窗口的 1/4 大小的一张画布**，放大四倍铺满整窗——光靠这个放大
// （双线性）就能把封面上那条硬边摊成一条两像素宽的渐变。所以「相邻两像素差多少」
// **分不出「糊了」和「只是放大了」**：实测不糊时那一步是 26、糊了是 2，门槛要是
// 放在 30 上，**把毛玻璃整个拿掉这条测试照样绿**。
//
// 真正分得出的是**边摊开的宽度**：毛玻璃的模糊是 σ=32 DIP，20%~80% 那一段实测
// **56 像素**宽；没有模糊时只有 **2 像素**（那点宽度全是四倍放大给的）。门槛取
// 20：两边都差着两倍以上。
//
// ⚠️ **先断「封面真的在画面上」**：不然「没有边可糊」也会绿（封面没下回来的时候
// 背景是一片平色，那一段会宽得离谱）。两半的明暗差就是「封面在」的证据。
func TestTheFullPlayerBackdropIsActuallyBlurred(t *testing.T) {
	s, tt := fullTesterWithCover(t)
	waitFrames(t, tt, "封面下回来了", func() bool {
		return s.images.Get(s.app.Snapshot().CoverURL).Ready()
	})
	openFull(t, tt)

	// 等的是「糊开了」这个**状态**，不是「第一帧」：毛玻璃读的是「它之前画过
	// 的东西」，而底（封面）刚画上去的那一帧，渲染器未必已经把它糊开。
	//
	// ⚠️ 这个等法也是断言：糊一辈子不糊开的话，10 秒之后这条会红（红的还是
	// 「封面糊开了」这一句）。
	waitFrames(t, tt, "封面糊开了", func() bool { return coverEdgeSpread(tt) >= 20 })

	img := tt.Image()
	const (
		edgeX = fullWinW / 2 // 封面左右对半的那条竖线落在窗口正中
		row   = fullWinH / 2
	)
	// 左半暗、右半亮：封面确实铺在画面上。
	left := img.RGBAAt(edgeX-200, row)
	right := img.RGBAAt(edgeX+200, row)
	if int(right.R)-int(left.R) < 40 {
		t.Fatalf("封面没铺满画面（左 %v、右 %v）——这一条量的是「有没有糊」，"+
			"封面不在就无从谈起", left, right)
	}
	// 那条竖边糊开了没有：它摊开的宽度够不够。
	if w := coverEdgeSpread(tt); w < 20 {
		t.Errorf("封面中间那条竖边只摊开了 %d 像素（毛玻璃的模糊是 σ=32 DIP，"+
			"摊开五十几像素）——背景那一层没有糊", w)
	}
}

// coverEdgeSpread 是封面中间那条竖边**摊开的宽度**（像素）。
//
// 量的是「20% 到 80%」那一段：底色从左边那一档走到右边那一档，中间这 60% 落在
// 哪一段里。毛玻璃的模糊是 σ=32 DIP，那一段实测 **56 像素**；没有模糊时只有
// **2 像素**——那点宽度全是四倍放大给的。
//
// ⚠️ **不能用「相邻两像素差多少」**：放大（双线性）自己就能把硬边摊成一步 26 的
// 渐变，和糊过的（一步 2）差得并不远——门槛放在哪儿都有一边抓不住。
func coverEdgeSpread(tt *mygoui.Tester) int {
	img := tt.Image()
	const (
		edgeX = fullWinW / 2
		row   = fullWinH / 2
	)
	left := float64(img.RGBAAt(edgeX-300, row).R)
	right := float64(img.RGBAAt(edgeX+300, row).R)
	lo, hi := left+0.2*(right-left), left+0.8*(right-left)
	first, last := -1, -1
	for x := edgeX - 300; x < edgeX+300; x++ {
		v := float64(img.RGBAAt(x, row).R)
		if v >= lo && v <= hi {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	if first < 0 {
		return 0
	}
	return last - first + 1
}

// TestTheFullPlayerContentIsStillClickableThroughTheGlass 断「糊了之后点得动」。
//
// 毛玻璃那一层是**满窗大小**的：它要是吃掉了点击，满屏就成了一个点不动的屏。
// 判据是走真的那一下点击（点「关闭全屏」，满屏真的关掉）。
func TestTheFullPlayerContentIsStillClickableThroughTheGlass(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)

	if err := tt.Click("关闭全屏"); err != nil {
		t.Fatalf("点不了「关闭全屏」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if s.full.open {
		t.Error("满屏里那一层毛玻璃把点击吃了——「关闭全屏」按下去满屏还在")
	}
}

// TestTheTextOverTheBlurredBackgroundIsStillReadable 断「底下的字看得清」。
//
// 满屏上的字全是白的（`fullOnColor` / `fullDimColor`），底下是糊过的封面加一层
// 55% 的黑（`fullScrim`）。判据是**画出来的对比度**：字最亮的那一个像素，和它
// 背后底色的那一个像素，算 WCAG 的对比度。
//
// ⚠️ **20px / 400 是普通文字**（#169 把字重从 700 改成了 400）：WCAG AA 对它
// 那一档的门槛是 **4.5:1**，而实测最坏的一档（**纯白**封面）是 **4.31:1**，
// 差 0.19。这一条断的是**还读得出来**这条底线（3:1），够不到 AA 的那一点和
// 12px 的时间标签（2.85:1）一起归可及性那一票（issue #139 的「增加对比度」）。
// 把门槛直接提到 4.5 会让这条红——而红的那一处不在这一票的范围里。
//
// ⚠️ 满屏这一屏不画歌词：歌词区在迷你播放器那一页（`nowplaying.go`）。设置面板
// 里「显示歌词」那一项存的是同一个改动，但这一屏没有歌词可显示，所以这一条量
// 的是现有的那些字。
func TestTheTextOverTheBlurredBackgroundIsStillReadable(t *testing.T) {
	// 纯白的封面：底色最亮的那一档。
	s, tt := fullTesterWithCoverPNG(t, whitePNG(t, 1200, 1200))
	waitFrames(t, tt, "封面下回来了", func() bool {
		return s.images.Get(s.app.Snapshot().CoverURL).Ready()
	})
	openFull(t, tt)
	tt.Frame()

	title := s.app.Snapshot().Title
	box, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到歌名 %q，文字有 %q", title, tt.Texts())
	}
	// 等封面**画上来**：封面没画上来的时候底下是主题的深色底，那样的对比度
	// 好看得多——量的是一个不存在的情形。
	//
	// 白封面压完 55% 的黑是 115（255 × 0.45），而没有封面时底下是主题的深色底
	// （十几）。所以底色到 80 以上就说明封面已经铺上来了（门槛取在两者中间，
	// 离两头都远）。
	waitFrames(t, tt, "封面画上来了", func() bool {
		p := tt.Image().RGBAAt(int(box.X)-10, int(box.Y+box.H/2))
		return int(p.R) > 80
	})
	img := tt.Image()
	// 字最亮的那一个像素（抗锯齿的边不算，要字的核心）。
	best := color.RGBA{}
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if p := img.RGBAAt(x, y); int(p.R) > int(best.R) {
				best = p
			}
		}
	}
	// 底色：字框左边十像素那一块里**最亮**的那几个（底越亮越难读，取最坏的一档）。
	bg := color.RGBA{}
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		if p := img.RGBAAt(int(box.X)-10, y); int(p.R) > int(bg.R) {
			bg = p
		}
	}
	ratio := contrastRatio(best, bg)
	t.Logf("歌名 %v、它背后的底色 %v，对比度 %.2f:1", best, bg, ratio)
	// 门槛 3:1 是「还读得出来」的底线；AA 的 4.5:1 差 0.19，见这条的注释。
	if ratio < 3 {
		t.Errorf("歌名和它背后的底色对比度只有 %.2f:1（字 %v、底 %v）——"+
			"糊过的背景太亮，白字读不出来", ratio, best, bg)
	}
}

// ---- 底部控制条的淡入 ----

// TestTheBottomControlsComeBackTheSameWayTheTopBarDoes 断「底下那一条和顶栏
// 一样的出现方式」。
//
// 判据是**同一帧里两者的对比**：界面藏了三秒之后指针一动，顶栏是慢慢淡回来的
// （`Opacity(AnimateTo(…))`），底下那一条原来写的是 `Opacity(shown)`——那是
// 「啪」地一下出现的。于是同一帧里：顶栏还看不见，底下那一条已经全亮了。
//
// 一个界面里两种响应，用户看得见：手一回来，底下那一条先「啪」地一下弹出来，
// 顶栏才慢慢淡回来。
func TestTheBottomControlsComeBackTheSameWayTheTopBarDoes(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()
	settleFull(t, tt)

	// 底下那一条和顶栏各占窗口的一条横带。
	topStrip := mygoui.Rect{X: 0, Y: 0, W: fullWinW, H: fullTopHeight}
	bottomStrip := mygoui.Rect{X: 0, Y: fullWinH - fullControlsHeight, W: fullWinW, H: fullControlsHeight}
	// 两条各自「全亮」的时候有多亮（当基准）。
	topShown := maxBright(tt.Image(), topStrip)
	bottomShown := maxBright(tt.Image(), bottomStrip)
	if topShown < 100 || bottomShown < 100 {
		t.Fatalf("两条全亮时不够亮（顶栏 %d、底下 %d）——这一条量的是「淡入的那一帧", topShown, bottomShown)
	}

	// 三秒到点 + 淡出走完（两个钟都要推，见 advanceFull）。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)

	// 指针一动，界面回来。这一帧是**淡入刚开始**的那一帧。
	tt.Move(604, 404)
	tt.Frame()

	// 顶栏：还在淡入（不透明度是 0，整个元素一个像素都不画）。
	if got := maxBright(tt.Image(), topStrip); got > topShown/2 {
		t.Fatalf("顶栏在淡入的第一帧就亮到了 %d（全亮是 %d）——它不是慢慢淡回来的", got, topShown)
	}
	// 底下那一条：**必须一样**。原来它是 `Opacity(shown)`——一下子就是 1，
	// 于是这一帧它已经全亮了（244），而顶栏还在黑着：同一个界面里两种响应。
	if got := maxBright(tt.Image(), bottomStrip); got > bottomShown/2 {
		t.Errorf("底下那一条在淡入的第一帧就亮到了 %d（全亮是 %d）——它是「啪」地一下"+
			"出现的，和顶栏不是一回事", got, bottomShown)
	}

	// 走完动画之后它得回来（这一半是「淡入」的另一半：不是永远不出现）。
	settleFull(t, tt)
	if got := maxBright(tt.Image(), bottomStrip); got < bottomShown/2 {
		t.Errorf("淡入走完之后底下那一条只有 %d（全亮是 %d）——它没有回来", got, bottomShown)
	}
}

// ---- 状态缺口 ----

// TestTheProgressKnobStaysVisibleWhileDragging 断「拖进度条时旋钮不响应」。
//
// 框架那一颗旋钮被按住的时候会变成一片透镜：白面整个褪掉，只留一圈黑边
// （`mygo/ui/widgets.go` 的 `paintKnob`）。满屏的底永远是深的，那圈黑边落在上面
// 几乎看不见——用户按住旋钮拖，旋钮反而不见了。
//
// 判据是**画面上那一块还有没有亮的东西**：按住的时候旋钮得还在，而且旋钮得跟着
// 拖动走。
func TestTheProgressKnobStaysVisibleWhileDragging(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)

	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}
	at := float32(0.25)
	x := box.X + box.W*at
	y := box.Y + box.H/2
	knob := mygoui.Rect{X: x - 16, Y: box.Y, W: 32, H: box.H}

	tt.Press(x, y)
	settleFull(t, tt)

	if n := brightIn(tt.Image(), knob); n == 0 {
		t.Errorf("按住进度条的时候旋钮不见了（那一块 %v 一个亮像素都没有）——"+
			"框架那一颗按住会褪成黑边透镜，深色底上等于没有", knob)
	}

	// 旋钮得跟着拖动走。
	tt.Move(box.X+box.W*0.6, y)
	settleFull(t, tt)
	moved := mygoui.Rect{X: box.X + box.W*0.6 - 16, Y: box.Y, W: 32, H: box.H}
	if n := brightIn(tt.Image(), moved); n == 0 {
		t.Errorf("拖到 60%% 之后旋钮不在那儿了（%v 一个亮像素都没有）", moved)
	}
	tt.Release(box.X+box.W*0.6, y)
	settleFull(t, tt)
}

// TestTheSpectrumSwitchMovesItsKnobAtOnce 断「开关按下去，旋钮立刻就过去」。
//
// 判据是**旋钮在哪半边**：开关是 36×20 的一颗，旋钮是一颗白圆，关着的时候在左
// 半边、开着的时候在右半边。
//
// ⚠️ 原来用 `ui.Switch`：它的旋钮是**滑过去**的（`mygo/ui/widgets.go` 的
// `coreSwitch`：`sw.Animate("knob", …, 140*time.Millisecond)`），而滑动的起点是
// **按下之前**那个位置——于是按下去的那一帧旋钮一动不动，要等 140ms 之后才开始
// 滑。而无头 Tester 的钟**站着不动**，那 140ms 永远走不完：实测按下去之后开关
// 那一块**一个像素都没变**。
//
// 这一层用的是本包自己的 `staticSwitch`（见 static_switch.go）：旋钮直接跳到新的
// 位置。
func TestTheSpectrumSwitchMovesItsKnobAtOnce(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if tt.HasText("频谱图颜色") {
		t.Fatal("频谱还关着，颜色那一行已经在了")
	}

	sw, ok := tt.Find("开关：显示频谱图")
	if !ok {
		t.Fatalf("找不到那个开关，文字有 %q", tt.Texts())
	}
	// 关着的时候旋钮在左半边。
	if left, right := knobHalves(tt.Image(), sw); left == 0 || right != 0 {
		t.Fatalf("开关关着的时候旋钮不在左半边（左 %d、右 %d）", left, right)
	}

	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	// ⚠️ **不推钟**：这一帧就该是新的样子。
	tt.Frame()

	if left, right := knobHalves(tt.Image(), sw); right == 0 || left != 0 {
		t.Errorf("按下去之后旋钮没有到右半边（左 %d、右 %d）——它是滑过去的，"+
			"而那 140ms 要等钟走", left, right)
	}
	// 底下那一行跟着开关走（这一条不区分修没修，是顺带钉住的行为）。
	if !tt.HasText("频谱图颜色") {
		t.Errorf("开关开了，颜色那一行没有出现，文字有 %q", tt.Texts())
	}
}

// TestTheFullPlayerTitleIsTwentyPixelsAndAtMostTwoFifths 断「歌名的字号和宽度」。
//
// 上游是 `text-xl`（**20 像素**）而且限了宽度（标题最多占**五分之二**），我们是
// 18 像素、不限宽度——差两号。
//
// 判据：
//
//   - **行高**：20px 的字行高 23.5 上下（18px 的是 21 上下）——差两号在行高上
//     看得出来。
//   - **宽度**：那个框最多是窗口的五分之二。不限的话它 `Grow(1)` 会把顶栏剩下
//     的空全占了（实测约 960），限了就是 480。
func TestTheFullPlayerTitleIsTwentyPixelsAndAtMostTwoFifths(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)

	title := fullTestTitle
	box, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到歌名 %q，文字有 %q", title, tt.Texts())
	}
	if box.H < 22.5 {
		t.Errorf("歌名的行高是 %v——20px 的字该有 23.5 上下（18px 只有 21 上下）", box.H)
	}
	if max := float32(fullWinW) * 0.4; box.W > max+1 {
		t.Errorf("歌名的框 %v 宽，超过了窗口的五分之二（%v）——上游限着「标题最多占五分之二」", box.W, max)
	}
}

// fullTestTitle 是这一条用的那个歌名（本地文件的歌名是文件名，见 app.Open）。
const fullTestTitle = "stereo16"

// TestThePartsPanelIsAGlassPanelWhoseTextIsReadable 断「分集面板是那一块玻璃，
// 而且上面的字读得清」。
//
// 原来那一格是 `ui.RGBA(255, 255, 255, 0.12)`——一层十二个百分点的白，压在糊过
// 的封面上，**白字读不出来**（封面一亮，字就没了）。
//
// 现在它是**票面「毛玻璃的 Tint 配方」那四样**：`Material(glass.Glass{Tint:
// 背景色的 96%})` + `TextColor` + `Border` + `Shadow`。
//
// 判据是**画面上的对比度**：面板上那个「分集」的字，和它背后那一块面板的底色。
// 底色掺了背景色（深色主题下是深色），字是主题的字色 → 对比度够。
func TestThePartsPanelIsAGlassPanelWhoseTextIsReadable(t *testing.T) {
	s, tt := onlineTester(t, nil, true)
	s.openFullPlayer()
	waitFrames(t, tt, "问失败", func() bool { return s.app.PartsErr() != nil })
	tt.Frame()
	if err := tt.Click("显示分集列表"); err != nil {
		t.Fatalf("点不了分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()

	word, ok := tt.Find("分集")
	if !ok {
		t.Fatalf("面板上没有「分集」那两个字，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	// 字最亮的那一个像素（字的核心，不是抗锯齿的边）。
	best := color.RGBA{}
	for y := int(word.Y); y < int(word.Y+word.H); y++ {
		for x := int(word.X); x < int(word.X+word.W); x++ {
			if p := img.RGBAAt(x, y); int(p.R) > int(best.R) {
				best = p
			}
		}
	}
	// 面板的底色：那个字**下面**那一行（字框的下沿 + 2 像素）。
	floor := int(word.Y+word.H) + 2
	if floor >= img.Rect.Max.Y {
		floor = img.Rect.Max.Y - 1
	}
	bg := img.RGBAAt(int(word.X), floor)
	ratio := contrastRatio(best, bg)
	t.Logf("「分集」的字 %v、面板的底色 %v，对比度 %.2f:1", best, bg, ratio)
	if ratio < 4.5 {
		t.Errorf("面板上的字和它背后的底色对比度只有 %.2f:1（字 %v、底 %v）——"+
			"玻璃得掺上背景色，字才读得清", ratio, best, bg)
	}
}

// TestTheKnobStaysVisibleAllThroughTheDrag 断「拖的整个过程里旋钮都在」。
//
// 交叉淡入的关键：不透明的面和玻璃**一起**淡，中间没有哪一帧是空的。
//
// 判据是**每一帧**：按住之后推着钟走八步，每一步旋钮那一块都得有亮的像素。
// 只画玻璃的话，中间那几帧（面已经淡完、玻璃还没上来）旋钮是看不见的。
func TestTheKnobStaysVisibleAllThroughTheDrag(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)

	box, ok := tt.Find("全屏播放进度")
	if !ok {
		t.Fatalf("找不到满屏那条进度，文字有 %q", tt.Texts())
	}
	x := box.X + box.W*0.25
	y := box.Y + box.H/2
	knob := mygoui.Rect{X: x - 16, Y: box.Y, W: 32, H: box.H}

	tt.Press(x, y)
	for i := range 8 {
		tt.Advance(30 * time.Millisecond)
		tt.Frame()
		if n := brightIn(tt.Image(), knob); n == 0 {
			t.Errorf("按住 %dms 的时候旋钮不见了（那一块 %v 一个亮像素都没有）——"+
				"交叉淡入的中间不能是空的", (i+1)*30, knob)
		}
	}
	tt.Release(x, y)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
}

// ---- 出图 ----

// TestRenderFullPlayerShots 存图：满屏播放器，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerShots
//
// ⚠️ **深浅两套都要出**：满屏的控件和字全是白的（`fullOnColor`），而背景是糊过
// 的封面——浅色封面和深色封面糊出来是两回事，只出一套等于没验。
func TestRenderFullPlayerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **假时钟**：满屏那个「三秒藏界面」的钟用真时钟的话，每三秒会叫醒
			// 界面一次——那一下落在「等动画走完」的当口，动画就永远走不完
			// （实测机器一忙就飘：推了 1.28s 的钟还在要帧）。
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
				withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("关闭全屏")
			})
			settleFull(t, b.tt)
			save(t, dir, "60-全屏播放器-毛玻璃-"+name+".png", b.tt)

			// 悬停到播放键上：那一枚**不会**变主色（满屏的底是压暗过的封面，
			// 主色当字色那一档压在上面只有 1.0:1——理由见 `fullplayer.go` 的
			// `viewFullControls`）。
			if btn, ok := b.tt.Find(transportLabel(b.a.Snapshot().Playing)); ok {
				b.tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
				b.tt.Frame()
				save(t, dir, "61-全屏播放器-播放键悬停-"+name+".png", b.tt)
			}
			b.Close()
		})
	}
}

// TestRenderFullPlayerPartsPanelShot 存图：满屏里那个**分集面板**（那一块玻璃）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerPartsPanelShot
//
// 为什么要单独存一张：面板是**浮在背景上面**的一块玻璃，它的边、它的影、它和
// 背后那层糊过的封面分不分得开，只看满屏那一张看不出来。
func TestRenderFullPlayerPartsPanelShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	s, tt := onlineTester(t, nil, true)
	s.openFullPlayer()
	waitFrames(t, tt, "问失败", func() bool { return s.app.PartsErr() != nil })
	tt.Frame()
	if err := tt.Click("显示分集列表"); err != nil {
		t.Fatalf("点不了分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	settleFull(t, tt)
	save(t, dir, "61-全屏播放器-分集面板-玻璃.png", tt)
}

// ---- 帮手 ----

// splitCoverPNG 编一张左右对半（黑 | 白）的 PNG，当封面用。
//
// 为什么是**硬边**：毛玻璃糊的就是边，而硬边糊开之后那一段是渐变的——「有没有
// 糊」于是有一个能量的判据（相邻两像素的差）。
func splitCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.RGBA{A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// whitePNG 编一张纯白的 PNG，当封面用（底色最亮的那一档）。
func whitePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fullTesterWithCover 造一个开着歌、而且那首歌**有封面**的界面。
//
// 和 fullTester 的差别只有封面那一个字段——而没有封面的话背景是一片平色，
// 「糊没糊」就无从量起（没有边可糊）。
func fullTesterWithCover(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	return fullTesterWithCoverPNG(t, splitCoverPNG(t, 1200, 1200))
}

// fullTesterWithCoverPNG 是上面那一个，封面由调用方给。
func fullTesterWithCoverPNG(t *testing.T, body []byte) (*Shell, *mygoui.Tester) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	s, tt := newShell(t)
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc
	tt.SetDark(true)

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

// brightIn 是 box 里接近白的像素个数（深色主题下界面自己画的东西）。
func brightIn(img *image.RGBA, box mygoui.Rect) int {
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R > 190 && p.G > 190 && p.B > 190 {
				n++
			}
		}
	}
	return n
}

// knobHalves 数开关那一个框的左右两半里各有多少个亮像素（旋钮是白的）。
func knobHalves(img *image.RGBA, box mygoui.Rect) (left, right int) {
	mid := box.X + box.W/2
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R < 190 || p.G < 190 || p.B < 190 {
				continue
			}
			if float32(x) < mid {
				left++
			} else {
				right++
			}
		}
	}
	return left, right
}

// maxBright 是 box 里最亮的那一个像素的 R 通道。
//
// 为什么用「最亮」而不是「够亮的个数」：淡入是**不透明度**在动，而一个半透明的
// 白字（0.5 → 122）够不到「接近白」那个门槛——数个数的话，动画中途看起来跟
// 「一个都没画」一模一样。最亮的那一个像素是**连续**的，门槛定在「全亮的一半」
// 上，两种状态就分得开了。
func maxBright(img *image.RGBA, box mygoui.Rect) int {
	best := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if p := img.RGBAAt(x, y); int(p.R) > best {
				best = int(p.R)
			}
		}
	}
	return best
}

// contrastRatio 是 a 和 b 的 WCAG 对比度。
func contrastRatio(a, b color.RGBA) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// relLuminance 是 WCAG 那个相对亮度。
func relLuminance(c color.RGBA) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

// settleFull 推钟直到框架不再要帧（动画走完了）。
//
// 用的是仓库自己的写法（见 settings_test.go 的 settleSettingsPage）：
// **`tt.Advance(d)` 配 `tt.FramePending()`**，不数帧。Tester 的钟站着不动，
// 数帧那种等法会静默失效（CLAUDE.md §2）。
func settleFull(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	const step = 16 * time.Millisecond
	for range 80 {
		tt.Advance(step)
		tt.Frame()
		if !tt.FramePending() {
			return
		}
	}
	t.Fatal("推了 1.28s 的钟，界面还在要帧——动画停不下来")
}
