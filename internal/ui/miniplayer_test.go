// 迷你播放器窗口（SPEC 需求 43~46）这一组。
//
// 这些测试跑在 MyGo 的 headless Tester 上：一扇窗一个 Tester，两扇窗共用同一个
// `app.App`——那正是这个功能最难在别的地方验的那一半（两扇窗共享一份状态，而改完
// 状态两扇窗都要重绘）。
//
// 量不到的一律单列在文件末尾（真窗口才量得到那四条），不在这里假装验过。

package ui

import (
	"image/color"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 窗口骨架
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniWindowIsAtBiuNumbers 盯窗口那几个开关。
//
// 数字和出处抄 Biu 的 `electron/mini-player.ts:12-27`：`width: 320, height: 100`、
// `transparent: true`、`alwaysOnTop: true`、`skipTaskbar: true`、`resizable: false`、
// `frame: false`。
//
// 🔴 **`Transparent` 和「自己画不透明底」是同一件事的两半**（这里量开关那一半，
// 画出来那一半在 `TestTheMiniCardIsVisibleOnATransparentWindow`）。只开
// `Transparent` 而忘了画底的话，窗口在桌面上是隐形的：它开着、有内容、它响应
// 点击，而用户什么都看不见。
//
// ⚠️ `Frameless: true` 在这里**是想要的**，和主窗口正相反。主窗口要的是「红黄绿
// 留着」（`window.go` 里 `hiddenTitleBar = !Frameless && …`），而 Biu 的迷你窗口
// 压根没有系统控件（`frame: false`），它自己画一整块。
func TestTheMiniWindowIsAtBiuNumbers(t *testing.T) {
	const (
		biuWidth  = 320 // electron/mini-player.ts:16
		biuHeight = 100 // electron/mini-player.ts:17
	)
	opts := MiniWindowOptions(nil)

	if opts.Width != biuWidth || opts.Height != biuHeight {
		t.Errorf("迷你窗口是 %d×%d，Biu 是 %d×%d", opts.Width, opts.Height, biuWidth, biuHeight)
	}
	if !opts.Transparent {
		t.Error("窗口不是透明的：Biu 那边 transparent: true，圆角那一圈要透出桌面去")
	}
	if !opts.AlwaysOnTop {
		t.Error("窗口不置顶：它被别的窗口一盖就找不着了，而它是靠「一直看得见」成立的")
	}
	if !opts.SkipTaskbar {
		t.Error("窗口没跳过任务栏/程序坞：会和主窗口抢同一个格子")
	}
	if !opts.DisableResize {
		t.Error("窗口可缩放：Biu 是 resizable: false，而且这一版只有 320×100 摆得下")
	}
	if !opts.Frameless {
		t.Error("没有 Frameless：Biu 那边 frame: false，迷你窗口自己画一整块")
	}
	if opts.StateKey == "" {
		// 没有它每次启动都回到同一个位置，用户拖到顺手的地方就白拖了。
		t.Error("没有 StateKey：窗口每次启动都回到同一个位置，用户拖过的地方不记得")
	}
	if opts.StateKey == "main" {
		t.Errorf("StateKey 和主窗口撞了（%q）：两个窗口会互相把对方的位置写掉", opts.StateKey)
	}
}

// TestTheMiniWindowParksInTheTopRightCorner 盯「贴边」。
//
// 迷你窗口第一次出现时停哪儿。Biu 那边是居中（`center: true`），我们改成贴右上角
// ——左上角被菜单栏占着、底部被程序坞占着，右上角是工作区里最不挡事的一块。
//
// ⚠️ **只管第一次**：窗口建过之后 `StateKey` 记住的位置会盖掉 X/Y
// （`mygo/window.go` 的 WindowState 那一段），而那一半没有代码可测（见文件末尾）。
func TestTheMiniWindowParksInTheTopRightCorner(t *testing.T) {
	work := mygo.Rectangle{X: 0, Y: 25, Width: 1920, Height: 1055}

	x, y := miniCorner(work)
	if x != work.Width-miniWidth-12 || y != work.Y+12 {
		t.Errorf("停在 (%d,%d)，want (%d,%d)", x, y, work.Width-miniWidth-12, work.Y+12)
	}
	if x <= work.X || x+miniWidth >= work.X+work.Width {
		t.Errorf("横着没有贴边：%d 不在 [%d,%d] 里", x, work.X, work.X+work.Width-miniWidth)
	}
	if y < work.Y {
		t.Errorf("纵着跑到工作区上边 (%d) 之外去了（工作区顶边是 %d）", y, work.Y)
	}

	// 多显示器：取的是**主显示器**那一块（`ensureMiniWindow` 里传的就是
	// `Screen.PrimaryDisplay().WorkArea`），窗口不能算到工作区外面去。
	second := mygo.Rectangle{X: 1920, Y: 0, Width: 2560, Height: 1440}
	if sx, _ := miniCorner(second); sx < second.X {
		t.Errorf("第二块屏幕上算出 x=%d，落在工作区左边 %d 之外", sx, second.X)
	}

	// 没有真显示器（单元测试就是这一支）：退回 (0,0) 让框架去居中，而不是算出
	// 一个 (-308, …) 那种谁都看不见的位置。
	if zx, zy := miniCorner(mygo.Rectangle{}); zx != 0 || zy != 0 {
		t.Errorf("没有显示器时算出了 (%d,%d)，want (0,0)", zx, zy)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 看得见
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniCardCoversTheWindow 量「透明窗口自己给了底」。
//
// 这条是被「窗口开着却看不见」这件事逼出来的：`Transparent: true` 之后框架把帧清
// 成透明（`mygo/window.go:433`「Backends keep transparent windows clear」），主题
// 背景**不填**——所以不自己画一块不透明底，320×100 的窗口在桌面上是隐形的，而它
// 明明开着、有内容、点得动。
//
// 断法是量像素，两个方向都要断：
//
//	正中间画成了主题的 `Surface` —— 这一块是有底的（窗口看得见）；
//	左上角**没有**画     —— 画的是一块带圆角的卡片，而不是拿整窗刷一层不透明色
//	                        （后者「看得见」，可它是方的，贴边悬浮那一层就没了）。
//
// 两条反过来都红：忘了画底 → 中间是画布自己的底色；画成一整块 → 角上也画上了。
//
// ⚠️ 「透明」这件事本身在无头环境里量不到：headless 的帧缓冲一开始是一张**不透
// 明的白纸**（`mygo/ui/headless.go` 的 `image()` 直接给宿主那张），所以「没画的
// 地方」和「画了白色」在像素上长得一样。开关那一半在
// `TestTheMiniWindowIsAtBiuNumbers`，真窗口那一半在文件末尾的清单里。
func TestTheMiniCardCoversTheWindow(t *testing.T) {
	s, _ := newMiniShell(t, nil)
	tt := miniTester(s)

	img := snapshot(tt)
	surface := BuildTheme(s.app.SettingsSnapshot(), false).Surface
	want := color.RGBA{surface.R, surface.G, surface.B, 255}

	// ⚠️ 量的是**覆盖率**而不是某一个像素：窗口正中间被控件压着（进度条和播放键
	// 就摆在那儿），钉死一个像素的话改一下版面就得跟着改这条断言。
	covered := 0
	for y := 0; y < miniHeight; y++ {
		for x := 0; x < miniWidth; x++ {
			if miniPixel(img, x, y) == want {
				covered++
			}
		}
	}
	half := miniWidth * miniHeight / 2
	if covered < half {
		t.Errorf("只有 %d/%d 个像素是主题的 Surface %#v：这一块没有自己的底，"+
			"透明窗口在桌面上是隐形的（Transparent 的开关在 TestTheMiniWindowIsAtBiuNumbers）",
			covered, miniWidth*miniHeight, want)
	}
	if corner := miniPixel(img, 0, 0); corner == want {
		t.Errorf("左上角也画成了 %#v：底是拿整窗刷满的，不是那块圆角卡片——"+
			"贴边悬浮那一层（圆角那一圈透出桌面）就没有了", want)
	}
}

// TestTheMiniFramesCarryTheChosenTheme 盯铁律二在**第二个窗口**上也成立。
//
// ⚠️ 这个坑是静默的：`SetTheme` 放到切换按钮的回调里只调一次，第一帧是对的、下一
// 帧就被 MyGo 在帧开头那次重置悄悄丢回去（theme.go 的说明）。漏在迷你窗口上的
// 症状是「主窗口跟着系统走、迷你窗口不跟」——而两块窗本来就长一个样，用户只会
// 当成偶发。
//
// 断法读**像素**而不是读 `c.Theme()`：Tester 的包装函数跑在 `Shell.View` **之前**，
// 那时候读到的是 MyGo 刚重置好的系统主题，正好是这一条要防的那个值。要量「这一帧
// 真正用的主题」，只能看它画出来的颜色。
func TestTheMiniFramesCarryTheChosenTheme(t *testing.T) {
	a := app.New()
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); _ = a.CloseSettings() })

	s := NewMini(a, nil)
	t.Cleanup(s.images.Close)
	tt := miniTester(s)
	// 系统此刻是浅色（Tester 的默认），设置默认也是「跟随系统」：起点是浅的。
	tt.SetDark(false)

	light := miniPixel(snapshot(tt), miniWidth/2, miniHeight/2)
	if miniIsDark(light) {
		t.Fatalf("起点就不对：跟随系统 + 系统浅色，画出来却是暗的（%#v）", light)
	}

	// 用户在设置里选了深色。
	if _, err := a.UpdateSettings(func(st *app.Settings) { st.ThemeMode = app.ThemeModeDark }); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	dark := miniPixel(snapshot(tt), miniWidth/2, miniHeight/2)
	if !miniIsDark(dark) {
		t.Errorf("改了设置之后迷你窗口还是浅的（%#v → %#v）：这一帧开头没有装主题，"+
			"用户在设置里选的深色在这个窗口上不生效，而主窗口是生效的", light, dark)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 崩了不许带走进程
// ─────────────────────────────────────────────────────────────────────────────

// TestAPanicInsideTheMiniViewLeavesTheProcessAlive 盯铁律四在**第二个窗口**上成立。
//
// MyGo 的 Dispatch 和 drain 都没有 recover，view 里 panic 会一路穿到 Cocoa 事件
// 循环上去把整个进程带走。迷你窗口是第二个窗口，漏包一层的话「迷你窗口画崩了」
// 的症状是**整个应用消失了**——连主窗口一起，音乐还在响而界面上什么都没有了。
//
// ⚠️ **这里不能自己再包一层 `Guard`**：那样量的是测试自己写的那一句，而不是
// `NewMini` 装上去的那一句（把它换掉整条测试照样绿）。所以这里改成让
// **`NewMini` 装的那条 view 自己炸**——把 `app` 摘掉，`viewMini` 第一行
// `applyFrameTheme` → `SettingsSnapshot` 就是一次真的空指针 panic。
// 「app 被摘掉了」不是产品里会发生的事；它在这里只是一件让真 panic 一定发生的事。
func TestAPanicInsideTheMiniViewLeavesTheProcessAlive(t *testing.T) {
	s, a := newMiniShell(t, nil)
	s.app = nil // 让 NewMini 装的那条 view 在第一行就炸

	tt := miniTester(s)
	if s.crash == nil {
		t.Fatalf("view 炸了而 recover 没接住（crash = %v）：`NewMini` 没有把迷你"+
			"那一帧包在 `Guard` 里——进程会被带走", s.crash)
	}
	if !tt.HasText("界面出错了") {
		t.Fatalf("崩了之后没有切到错误页，文字有 %q", tt.Texts())
	}
	// 重新接上：点「重新载入」之后这一扇窗回到正常画面，而应用还活着。
	s.app = a
	s.crash = nil
	tt.Frame()
	if !tt.HasText("暂无播放内容") {
		t.Errorf("重新载入之后没有回到正常画面，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 两个窗口共享一份状态（铁律三）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniWindowIsWokenWhenTheStateChanges 盯铁律三在这个窗口上的那一半：
// **迷你窗口自己**得被叫醒。
//
// 即时模式是按需绘制的：后台改了状态不叫醒它，画面就永远停在旧帧。而这一条量的是
// 整条链路上最容易漏的一节——迷你窗口建外壳时**有没有把自己的唤醒句柄登记进去**。
// 漏登记的症状是「在主窗口里按下一首，迷你窗口的歌名停在上一首」，而界面上看不出
// 任何异样（它明明有内容，只是旧了）。
//
// ⚠️ **量的是「帧数」而不是「画出来什么」**，因为「停在旧帧」和「画错了」在
// `Texts()` 上长得一模一样。`Tester` 是拉式的（不调 `Frame()` 一帧都不画），所以
// 「有人叫醒了」和「没人叫醒」的唯一区别就是 settle 自己多跑的那一帧——那一帧只有
// `requested` 被置上才会跑，而它只有 `Services().Invalidate()` 会置
// （`mygo/ui/headless.go` 的 settle）。**把 `bindMiniWindow` 里登记的那一句换成空
// 函数，这一条立刻挂。**
//
// ⚠️ 那次状态变化必须发生在**这一帧之内**（所以放在 view 里而不是 view 之后）：
// `settle` 在每一轮开头都把 `requested` 清掉，一次落在帧与帧之间的唤醒会被下一句
// `Frame()` 自己那一下吃掉，从外面完全看不见（`TestPlayBarCoverWakeRedrawsTheWindow`
// 用的也是这一招，它那一段的等待就发生在一帧之内）。
//
// 「改一次状态叫醒**所有**窗口」那一半在 `internal/app` 的
// `TestEveryRegisteredWindowIsWokenWhenTheStateChanges`——那是逐窗口分发的机制；
// 这一条量的是迷你窗口真的把**自己那一扇**接了上去。两半合起来才是 SPEC 需求 46。
func TestTheMiniWindowIsWokenWhenTheStateChanges(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	main := New(a)
	t.Cleanup(main.images.Close)
	mini := main.newMiniShell()
	// 没有真窗口就不会走 `attachMiniWindow` 的 `OnClosed`，所以那几摊逐窗口的
	// 资源（封面缓存、两个后台写盘的 goroutine）测试里得自己收。
	t.Cleanup(mini.releaseMini)
	// 生产里这一句会被调两次：建外壳时一次（`newMiniShell`），窗口真建出来时再一次
	//（`attachMiniWindow`）。多登记一条的症状是「摘的时候只摘掉后一条、前一条留到
	// 进程退出」——量不到（同一个句柄被叫两遍和被叫一遍在帧数上没有区别），
	// 所以这里量的是它没把 `wakeRemove` 清掉、后面照样叫得醒。
	mini.bindMiniWindow()
	if mini.wakeRemove == nil {
		t.Fatal("bindMiniWindow 第二次之后没有唤醒句柄可摘")
	}

	frames := 0
	changed := false
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		frames++
		mini.View(c)
		if changed {
			return
		}
		changed = true
		// 一次普通的后台状态变化，发生在这一帧**里面**。
		// 没放歌，所以播放器那个每 50ms 的时钟不会来搅和（「是谁叫醒的」要说得清）。
		a.SetVolume(0.42)
		time.Sleep(100 * time.Millisecond) // 给这一路 wake 走完
	}, miniWidth, miniHeight)

	if frames < 2 {
		t.Errorf("迷你窗口只画了 %d 帧：没人叫醒它，画面停在旧帧。"+
			"查 `bindMiniWindow` 有没有把这一扇窗的唤醒登记进去（SPEC 需求 46、铁律三）",
			frames)
	}
	if v := a.Snapshot().Volume; v != 0.42 {
		t.Errorf("状态没改成 0.42（是 %.2f），这一条测的就不是唤醒了", v)
	}
	if !tt.HasText("暂无播放内容") {
		t.Errorf("没有歌时没有说「暂无播放内容」，文字有 %q", tt.Texts())
	}
}

// TestTheMiniWindowShowsWhatTheMainWindowShows 盯「两个界面上不会显示不同」
// （SPEC 需求 46 的另一半）。
//
// 这一半和唤醒那一半是两件事：唤醒管「改完会不会重画」，这一条管「重画之后读到的
// 是不是同一份状态」。所以它量的是**画面上的字**，而且两扇窗一起量。
func TestTheMiniWindowShowsWhatTheMainWindowShows(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	main := New(a)
	t.Cleanup(main.images.Close)
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，免得播放器的时钟一直改状态

	mainTT := mygoui.NewTester(main.View, 760, 460)
	miniTT := miniTester(mini)

	if !miniTT.HasText(flacBaseName) {
		t.Fatalf("迷你窗口上没有歌名 %q，文字有 %q", flacBaseName, miniTT.Texts())
	}
	if !mainTT.HasText(flacBaseName) {
		t.Errorf("主窗口上没有同一首歌名 %q，文字有 %q", flacBaseName, mainTT.Texts())
	}
	// 两个窗口读的是同一份状态，所以它们显示的也必须是同一句「播放」（刚被暂停，
	// 那个键的作用是开始放）。
	for name, tt := range map[string]*mygoui.Tester{"主窗口": mainTT, "迷你窗口": miniTT} {
		if !tt.HasText("播放") {
			t.Errorf("%s上没有「播放」，而播放刚刚被暂停了，文字有 %q", name, tt.Texts())
		}
	}

	// 换一首：两个窗口都得跟着换。
	a.Next()
	mainTT.Frame()
	miniTT.Frame()

	if miniTT.HasText(flacBaseName) {
		t.Errorf("换歌之后迷你窗口还停在上一首，文字有 %q", miniTT.Texts())
	}
	if !miniTT.HasText(monoBaseName) {
		t.Errorf("换歌之后迷你窗口上没有新歌名 %q，文字有 %q", monoBaseName, miniTT.Texts())
	}
	if !mainTT.HasText(monoBaseName) {
		t.Errorf("换歌之后主窗口上没有新歌名 %q，文字有 %q", monoBaseName, mainTT.Texts())
	}
}

// TestClosingTheMiniWindowLeavesTheAppPlaying 盯「关掉迷你窗口 = 只关一扇窗」。
//
// 🔴 主窗口那个 `OnClosed` 是**整个应用收尾**（停播放、关设置、存搜索历史和快捷键，
// 见 `Attach`）。迷你窗口套上那一套的话，用户点一下迷你窗口里的「退出」就会**把正
// 在放的歌停掉**，而主窗口明明还开着。症状和真因隔得很远（「从迷你切回来，音乐
// 没了」），所以这里钉的是「退出去之后歌还在放、两个窗口的唤醒也还在」。
func TestClosingTheMiniWindowLeavesTheAppPlaying(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	main := New(a)
	t.Cleanup(main.images.Close)
	removeMain := a.AddWake(main.wake.invalidate)
	t.Cleanup(removeMain)
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，免得播放器的时钟一直改状态
	miniTT := miniTester(mini)
	if !miniTT.HasText(flacBaseName) {
		t.Fatalf("起点就不对，文字有 %q", miniTT.Texts())
	}

	// 关掉迷你窗口（生产上「退出」→ `toggleMiniPlayer` → `closeMiniPlayer`
	// → `dropMini`）。
	if !main.dropMini() {
		t.Fatal("dropMini 说没有迷你窗口")
	}
	if main.mini != nil {
		t.Error("主窗口还指着那个已经关掉的迷你窗口：下一次按「迷你」什么也不会发生")
	}

	// 歌还在，而且主窗口还能被叫醒（迷你那一条被摘掉了，App 那边还有主窗口那一条）。
	if st := a.Snapshot(); !st.HasTrack {
		t.Error("关掉迷你窗口把歌停了：它只是一扇窗，关窗不是退出应用")
	}
	frames := 0
	mainTT := mygoui.NewTester(func(c *mygoui.Context) { frames++; main.View(c) }, 760, 460)
	mainTT.Frame()
	a.SetVolume(0.11)
	mainTT.Frame()
	if frames < 3 {
		t.Errorf("关掉迷你窗口之后主窗口一共只画了 %d 帧：唤醒句柄没摘对", frames)
	}
	if !mainTT.HasText(flacBaseName) {
		t.Errorf("主窗口上已经没有那首歌了，文字有 %q", mainTT.Texts())
	}
}

// TestOnlyOneMiniWindowIsEverBuilt 盯「连按两下只开一个」。
//
// 两个迷你窗口各放一首歌是比「两个主窗口」更难解释的一种错（单实例的意义见
// `openWindow` 的注释）。判据放在 `newMiniShell` 里而不是 `openMiniPlayer` 里，是
// 因为窗口本身要事件循环才建得出来，而「有没有第二个外壳」是纯逻辑。
func TestOnlyOneMiniWindowIsEverBuilt(t *testing.T) {
	main, _ := newMiniShell(t, nil)

	first := main.newMiniShell()
	second := main.newMiniShell()
	if first != second {
		t.Error("按了两下「迷你」建出了两个外壳：两个迷你窗口各放一首歌，而用户" +
			"以为他只有一个")
	}
	if main.mini != first {
		t.Error("主窗口指的不是第一个外壳")
	}
	if first.main != main {
		t.Error("迷你窗口那一侧没有指回主窗口：关掉自己之后主窗口不知道它没了")
	}
}

// TestTheMiniBackButtonGoesBackToTheMainWindow 盯那个「退出」键接的真是那个开关。
//
// 接错了的话迷你窗口就成了一个只能进不能出的窗口：用户缩进来之后唯一一条回去的
// 路（关掉窗口 / 按快捷键）都在这个窗口够不着的地方——而「回不去」这件事在界面上
// 看着就是「按了没反应」。
//
// 断法是替掉那个开关（真窗口在测试里建不出来，和 autostart / pickImage 同一个
// 道理）然后点它，看主窗口那边是不是被叫到了。
func TestTheMiniBackButtonGoesBackToTheMainWindow(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	back := 0
	mini := NewMini(a, func() { back++ })
	t.Cleanup(mini.images.Close)
	tt := miniTester(mini)

	if err := tt.Click("退出"); err != nil {
		t.Fatal(err)
	}
	if back != 1 {
		t.Errorf("点了「退出」之后那个开关被调了 %d 次，want 1", back)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 控制得了播放（SPEC 需求 44、45）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniWindowStepsThroughTheQueue 盯上一首 / 下一首（SPEC 需求 44）。
//
// Biu 那边两个键在没歌或者只有一首歌时是灰的（`pages/mini-player/index.tsx:166`），
// 判据是 `!title || isSingle`；我们用 `App.CanPrev/CanNext`（和播放条上那两个键同一
// 个判据），它在队列里只有一首时仍然是亮的——按下去听到的是同一首从头来，表现成
// 「按了没反应」更让人困惑。
func TestTheMiniWindowStepsThroughTheQueue(t *testing.T) {
	s, a := newMiniShell(t, nil)
	tt := miniTester(s)

	// 队列空：键摆在那儿，但点不动（灰的）；没歌时标题那一句是 Biu 的
	//「暂无播放内容」（`pages/mini-player/index.tsx:128`）。
	if _, ok := tt.Find("下一首"); !ok {
		t.Fatalf("画面上找不到「下一首」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("暂无播放内容") {
		t.Errorf("没歌时没有说「暂无播放内容」，文字有 %q", tt.Texts())
	}
	if err := tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	if cur, ok := a.Queue().Current(); ok {
		t.Errorf("队列空的时候点了「下一首」，竟然放起来了：%q", cur.Title)
	}

	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，让播放键上写的是「播放」
	tt.Frame()

	for _, b := range []string{"上一首", "下一首", "播放"} {
		if _, ok := tt.Find(b); !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", b, tt.Texts())
		}
	}

	if err := tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(monoBaseName) {
		t.Errorf("点了「下一首」还停在 %q，文字有 %q", flacBaseName, tt.Texts())
	}

	if err := tt.Click("上一首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(flacBaseName) {
		t.Errorf("点了「上一首」没回到 %q，文字有 %q", flacBaseName, tt.Texts())
	}
}

// TestTheMiniVolumeSliderSetsTheVolume 盯音量（这一票点名要的）。
//
// Biu 的迷你播放器**没有音量**（`pages/mini-player/index.tsx` 里没有），所以这一条
// 没有上游可抄：滑块该在标题那一行还是控制那一行、给多宽，是我们自己排的账（见
// `viewMiniTitleRow`）。
//
// 断法：拖音量条之后 `App.Snapshot().Volume` 跟着变——**不是**量画出来那条槽的长
// 度（那个量的是「看得见」，量不到「按下去有没有用」）。
func TestTheMiniVolumeSliderSetsTheVolume(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(1)
	tt := miniTester(s)

	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	y := bar.Y + bar.H/2
	// 按到最左：音量应该是 0。
	tt.Press(bar.X+1, y)
	tt.Move(bar.X+1, y)
	tt.Release(bar.X+1, y)
	tt.Frame()
	if v := a.Snapshot().Volume; v > 0.05 {
		t.Errorf("把音量条按到最左，音量是 %.2f：拖动没接到 App 上", v)
	}

	// 按到最右：满。
	tt.Press(bar.X+bar.W-1, y)
	tt.Move(bar.X+bar.W-1, y)
	tt.Release(bar.X+bar.W-1, y)
	tt.Frame()
	if v := a.Snapshot().Volume; v < 0.95 {
		t.Errorf("把音量条按到最右，音量是 %.2f", v)
	}
}

// TestDraggingTheMiniProgressSeeksOnlyOnRelease 盯「按住的时候不 seek」。
//
// 分工：按住的时候进度条的值归用户，松手才 `ScrubEnd`（那是一次**真的** seek）。
// 不这么分的话，后台那个每 50ms 写回一次位置的时钟每拍都把值改回去——症状是
// **拖不动**：按住往右拖，值一松劲就弹回去。
//
// ⚠️ 按住看的是 `Pressed()`，不是 `Dragging()`：后者报的是 Drag / Draggable 两套
// 机制，普通 Slider 对它永远是 false（实测：按下滑块移动 200 DIP，Dragging 仍为
// false，Pressed 为 true）。用错了的话这一条会直接红。
func TestDraggingTheMiniProgressSeeksOnlyOnRelease(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	tt := miniTester(s)
	settle(t, tt, "时长读回来了", func() bool { return a.Snapshot().Duration > 0 })
	before := a.Snapshot().Position

	bar := progressRect(t, tt)
	y := bar.Y + bar.H/2
	tt.Press(bar.X+bar.W*0.1, y)
	tt.Move(bar.X+bar.W*0.8, y)

	if !s.miniHeld {
		t.Fatal("按住的时候 miniHeld 是假：ScrubStart 没调过，播放器那边不知道" +
			"「用户开始拖了」")
	}
	if got := a.Snapshot().Position; got != before {
		t.Errorf("按住的时候位置已经被改成 %.2f 秒（松手之前是 %.2f 秒）：值没归用户，"+
			"后台那个每 50ms 写回一次的时钟会和拖动打架，症状是**拖不动**——"+
			"按住往右拖，一松劲就弹回去", got.Seconds(), before.Seconds())
	}

	tt.Release(bar.X+bar.W*0.8, y)
	tt.Frame()
	if s.miniHeld {
		t.Error("松手之后 miniHeld 还是真")
	}
	dur := a.Snapshot().Duration
	if got, want := a.Snapshot().Position, dur*8/10; got < want-dur/10 {
		t.Errorf("松手之后位置是 %.2f 秒，而拖到了 %d 的八成（%.2f 秒）：没有 seek",
			got.Seconds(), dur, want.Seconds())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 真窗口才量得到（无头 Tester 里没有事件循环 / 真窗口），这里只列出来不假装验过：
//
//	1. 窗口真的透明、真的置顶、真的贴着屏幕边。`mygo.NewWindow` 和
//	   `mygo.Screen` 都要应用 ready，而它们头上有 `needsApp`，无头环境里一调就
//	   panic（`mygo/app.go:128`）。
//	2. 拖动：把整个 320×100 拖到别处。`mygo/ui/headless.go` 的 `startDrag` 是空实
//	   现，Tester 也不记窗口移动，所以量不到（`fullplayer` 那一票量「窗口真的被拖
//	   动」时撞的是同一堵墙）。
//	3. 从迷你窗口关掉它（关窗口 / 点关闭）：那走的是 `attachMiniWindow` 装的
//	   `OnClosed`，要真窗口。测试里量的是它前面那一半（`dropMini`）。
//	4. 从别的应用按全局快捷键「切换mini/完整模式」：Carbon 热键要真事件循环。
// ─────────────────────────────────────────────────────────────────────────────
