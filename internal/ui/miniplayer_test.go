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
	"os"
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
// `transparent: true`、`alwaysOnTop: true`、`skipTaskbar: true`、`frame: false`。
//
// ⚠️ **Biu 那一行 `resizable: false` 我们故意没抄**，而这一条正是钉住那个改动的
// 后果——见下面 `DisableResize` 那段断言的注释。
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
		biuHeight = 84  // electron/mini-player.ts:17
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
	// 🔴 **这里断言的不是「不可缩放」，是「缩不到比设计尺寸更小」**。
	//
	// Biu 那边是 `resizable: false`，而照抄它会让 `StateKey` 整个失效：
	// `DisableResize` 去掉窗口的 resizable 位 → Cocoa 的 `isZoomed` 恒为真 →
	// `captureState` 的 `!Maximized` 那道判断不成立 → `window-state.json` 里连
	// `"mini-player"` 这个键都不会有，**每次开都回到右上角**（真机实测过）。
	// 完整的链路和四个变体的实测表在 `MiniWindowOptions` 的注释里。
	//
	// 「不能缩」改由 MinWidth/MinHeight 兜：用户拖不到比 320×100 更小，那三行控件
	// 就永远不会被裁掉。往大拖没有后果（右边那一列是 `Grow(1)`）。
	//
	// 所以断的是**下限**，而 `DisableResize` 一旦回来就会红——它正是那条让下限失效
	// 的路（顺带把上限也拿走了，那一条同样量不到：上限是 Cocoa 的行为，无头环境
	// 里没有真窗）。
	if opts.MinWidth != biuWidth || opts.MinHeight != biuHeight {
		t.Errorf("最小尺寸是 %d×%d，want %d×%d：用户能把这个窗口拖到比设计尺寸更小，"+
			"而那三行控件会被裁掉（`DisableResize` 那条路走不得，见上面那段注释）",
			opts.MinWidth, opts.MinHeight, biuWidth, biuHeight)
	}
	if opts.DisableResize {
		t.Error("又装回 DisableResize 了：那样 `isZoomed` 恒为真，`StateKey` 一条都存不下来，" +
			"用户拖到哪都不会被记住（`MiniWindowOptions` 的注释里有实测表）")
	}
	if !opts.Frameless {
		t.Error("没有 Frameless：Biu 那边 frame: false，迷你窗口自己画一整块")
	}
	if opts.StateKey == "" {
		// 没有它每次启动都回到同一个位置，用户拖到顺手的地方就白拖了——而下面那个
		// `DisableResize` 的断言断了「StateKey 存不存得下来」的那一半，两条合起来
		// 才是「拖到哪就记到哪」。
		t.Error("没有 StateKey：窗口每次启动都回到同一个位置，用户拖过的地方不记得")
	}
	if opts.StateKey == "main" {
		t.Errorf("StateKey 和主窗口撞了（%q）：两个窗口会互相把对方的位置写掉", opts.StateKey)
	}
}

// TestTheMiniWindowIsAPanel 盯票 #162：迷你窗是面板语义，点它不激活应用。
//
// 无头环境里这一条**只量得到选项**（`Panel: true`）：真正让应用保持不活动的是
// Cocoa 的 `NSPanel` + nonactivating 样式，只有真窗口才量得到（列在文件末尾）。
//
// 但它同时盯住票面点名的那个坑：**Panel 不能把位置记忆一起带走**。面板语义上
// 不能最大化，而「不能缩放 / 不能最大化」和「`isZoomed` 恒为真、`StateKey` 一条
// 都存不下来」只差一层（那个坑的实测表在 `MiniWindowOptions` 的注释里）。所以
// 这里钉的三条是「Panel 开着的时候，位置记忆的那几个前提还在」：
//
//   - `StateKey` 还在；
//   - `DisableResize` 没被打开（它才是那条路的开关）；
//   - 大小没有被 `Min == Max` 钉死（实测表里同样会让 `isZoomed` 恒真）。
func TestTheMiniWindowIsAPanel(t *testing.T) {
	opts := MiniWindowOptions(nil)
	if !opts.Panel {
		t.Error("迷你窗不是 Panel：用户点一下播放键，前台应用整个切走（票 #162）")
	}
	if opts.StateKey == "" {
		t.Error("Panel 把 StateKey 弄没了：窗口每次开都回到同一个位置，用户拖过的地方不记得")
	}
	if opts.DisableResize {
		t.Error("Panel 顺带打开了 DisableResize：`isZoomed` 恒为真，StateKey 一条都存不下来")
	}
	if opts.MaxWidth != 0 || opts.MaxHeight != 0 {
		t.Errorf("Panel 把窗口大小钉死了（Max %d×%d）：实测表里 min==max 会让 "+
			"`isZoomed` 恒为真，位置一条都存不下来", opts.MaxWidth, opts.MaxHeight)
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
// 成透明（`mygo/window.go:442`「Backends keep transparent windows clear」），主题
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
	if err := a.MountSettings(filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); _ = a.CloseSettings() })

	// ⚠️ **挂设置的是这个 App**（上面 `MountSettings` 装在它上面），所以迷你那个
	// 外壳也得是同一个 App 上的——`NewMini` 的第二个参数（主窗口的外壳）同样挂
	// 在它上面，于是两扇窗读的是同一份设置。
	main := newTestShell(t, onApp(a)).s
	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)
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

	main := newTestShell(t, onApp(a)).s
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

	main := newTestShell(t, onApp(a)).s
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

	main := newTestShell(t, onApp(a)).s
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

// TestTheMiniShellTeardownIsTheSharedOne 盯迷你那个外壳的收尾**走的是共用那一段**。
//
// ⚠️ 主干这一轮把收尾从「挂在窗口回调里的一大段」收成了 `Shell.Close`（收整个
// 应用），于是「只收这一扇窗的那几样」需要一个单独的入口——`closePerWindow`。
// 迷你窗口走的就是它：它**不能**走 `Close`，那个会 `s.app.Close()` 把正在放的歌停
// 掉，而主窗口明明还开着。
//
// 断两样：共用那一段**真的被调到了**（拿 `closed` 这道闸门量——它只有走过
// `closePerWindow` 才会置上，而三个「装定时器」的地方都看它），以及迷你那个外壳
// **没有**把整个应用收掉（App 还在，歌还在）。
//
// 这条也是回归防护：哪天有人图省事把 `releaseMini` 改成调 `Close()`，这里立刻红
// （歌没了）。
func TestTheMiniShellTeardownIsTheSharedOne(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停
	_ = miniTester(mini)

	if mini.closed {
		t.Fatal("起点就不对：还没收就已经是收过的了")
	}
	mini.releaseMini()

	if !mini.closed {
		t.Error("releaseMini 走完之后 `closed` 还是假：三个「装定时器」的地方不看它，" +
			"于是关掉之后还有一帧画到就会再装一个（`closePerWindow` 没被调到）")
	}
	if st := a.Snapshot(); !st.HasTrack {
		t.Error("收迷你那个外壳把歌停了：它是另一扇窗，关窗不是退出应用——" +
			"（`releaseMini` 若误调 `Close()`，`s.app.Close()` 就是这么停掉它的）")
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

// TestTheMiniWindowRegistersTheInAppShortcuts 盯迷你窗口**也**登记应用内快捷键。
//
// ⚠️ 不登记的后果不是「少几个键」，而是**设置页上那句话变成假的**：那一页写着
// 「应用内那一列：窗口在前台时按它就有用」，而迷你窗口恰恰是用户缩小之后最常停在
// 前面的那一扇。少了这一句，⌘P / ⌘← / ⌘→ / ⌘↑ / ⌘↓ 在小窗在前台时一条都不响，
// 而界面上一个字都不提（这一票刚把那句话从「切换mini/完整模式那一条除外」改成
// 现在的说法）。
//
// 断法：真的按 ⌘↑（音量加），断 **App 里的音量变了**——而不是断「有没有登记」。
func TestTheMiniWindowRegistersTheInAppShortcuts(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，免得播放器的时钟改状态
	tt := miniTester(s)
	a.SetVolume(0.5)

	tt.Key(mygoui.Cmd, mygoui.KeyUp) // ⌘↑ = 音量加（+0.05）
	tt.Frame()
	if got := a.Snapshot().Volume; got < 0.54 {
		t.Errorf("在迷你窗口里按了 ⌘↑ 之后音量是 %.2f，want 0.55：这一扇没登记应用内"+
			"快捷键（`viewMini` 里那一行 `viewShortcuts`），而设置页写着「窗口在前台时"+
			"按它就有用」", got)
	}
}

// TestTheMiniWindowUsesTheUsersShortcuts 盯**用户配的那一份**，不是出厂默认。
//
// 🔴 这是一个真缺陷，本票第一版就有，评审在真机上逮到的：
// `New` 建出来的是 `newShortcutKeys("")`——**读不到盘**，摆的是
// `defaultShortcutSettings()`；而 `mountShortcuts()` 只在主窗口的 `Run` 里调过一次。
// 于是迷你窗口每帧登记的全是**出厂默认**：
//
//   - 用户在设置页**删掉**的默认键，在小窗里照响（他明明已经让它失效了）；
//   - 用户**真配**的那条，小窗不认（他在小窗里按自己配的键，什么也不发生）。
//
// 真机量过（隔离 HOME + 死代理）：shortcuts.json 里把音量加减改成 ⌘⇧↑/⌘⇧↓，
// 主窗口认 ⌘⇧↓，小窗认 ⌘↓。而当时本票那两条测试**都抓不到**：一条按的正是默认值
// ⌘↑，另一条直接调 `runShortcut` 绕开了按键分发。
//
// 所以这一条**改用户的那一份配置再按**，而且断两面：配的那条要认，删掉的那条要不认。
func TestTheMiniWindowUsesTheUsersShortcuts(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	// ⚠️ **改的是主窗口那一份**。用户是在主窗口的设置页里改的（`settings_page`
	// 那一页只存在于主窗口），而迷你窗口压根不画设置页。改迷你自己那份的话，
	// 两条断法都会绿——第一版就是栽在这儿。
	main := newTestShell(t, onApp(a)).s
	mini := NewMini(a, main, nil)
	t.Cleanup(mini.closePerWindow)

	// 「共用同一份」这件事本身：不是「读到同一份文件」，而是**同一个实例**。
	// 是同一个实例才顺带对两件事——用户改完立刻生效（不用重读盘），
	// 而迷你窗口关掉时不会停掉主窗口那份的后台写盘循环。
	if mini.keys != main.keys {
		t.Fatalf("迷你窗口用的是自己那一份快捷键（%p），不是主窗口那份（%p）："+
			"它读不到盘，摆的是出厂默认值——用户删掉的默认键在小窗里照响、"+
			"用户真配的键小窗不认", mini.keys, main.keys)
	}

	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，免得播放器的时钟改状态

	// 用户在主窗口的设置页里把「音量加」从 ⌘↑ 改成 ⌘⇧↑。
	if err := main.keys.set(shortcutSlot{id: "volumeUp"}, "cmd+shift+up"); err != nil {
		t.Fatal(err)
	}

	tt := miniTester(mini)
	a.SetVolume(0.5)

	// ① 用户真配的那条：小窗得认。
	tt.Key(mygoui.Cmd|mygoui.Shift, mygoui.KeyUp)
	tt.Frame()
	if got := a.Snapshot().Volume; got < 0.54 {
		t.Errorf("在迷你窗口里按了用户配的 ⌘⇧↑ 之后音量是 %.2f，want 0.55："+
			"这一扇登记的不是用户那一份", got)
	}

	// ② 用户已经换掉的那条（默认的 ⌘↑）：小窗**不该**再认。
	a.SetVolume(0.5)
	tt.Key(mygoui.Cmd, mygoui.KeyUp)
	tt.Frame()
	if got := a.Snapshot().Volume; got > 0.51 {
		t.Errorf("用户已经把「音量加」从 ⌘↑ 换成 ⌘⇧↑ 了，小窗里按 ⌘↑ 音量还是 %.2f："+
			"这一扇登记的是出厂默认值", got)
	}
}

// TestTheMiniShortcutDoesNotOpenASecondMiniWindow 盯「在小窗里按 ⌘M 是关掉，不是再开一个」。
//
// ⚠️ 登记了应用内快捷键之后这一条才成立，而且它是个**真会发生的**错：迷你窗口那一侧
// 的 `s.mini` 是 nil（它不会开着第二个自己），而 `toggleMiniPlayer` 照常走下去就是
// `openMiniPlayer()` 建在**迷你外壳**上——两个迷你窗口各放一首歌。
//
// 断法是走真的一条命令（`runShortcut`，和 `viewShortcuts` 里那一句同一个），
// 看主窗口那一侧的 `mini` 有没有变 nil。
func TestTheMiniShortcutDoesNotOpenASecondMiniWindow(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	back := 0
	main := newTestShell(t, onApp(a)).s
	mini := NewMini(a, main, func() { back++ })
	t.Cleanup(mini.releaseMini)
	mini.main = main
	main.mini = mini

	// 从**迷你窗口那一侧**发那条命令——这正是「小窗在前台时按 ⌘M」走的那条路。
	mini.runShortcut("toggleMiniMode")

	if back != 1 {
		t.Errorf("迷你窗口那一侧的开关被调了 %d 次，want 1：没有绕回主窗口", back)
	}
	if mini.mini != nil {
		t.Errorf("迷你外壳上多了一个迷你：%p", mini.mini)
	}
}

// TestADeepLinkMakesRoomForTheMainWindow 盯「迷你开着时来 biu:// 链接」。
//
// ⚠️ `navigate` 改的是**主窗口**那一侧的路由（路由表是主窗口的），而主窗口在迷你期
// 间是收着的。不让开的话，按一个链接的结果是「窗口弹出来了，还是迷你那一扇，页面上
// 什么都没有」——`openWindow` 那一支优先叫回迷你窗就直接 return，连主窗口都不弹。
//
// 断两样纯逻辑：迷你被让开了、路由换到了。而「叫回窗口那一步」要过系统
// （`RestoreFromBackground`），无头环境里调不到——它就在 `OnOpenURL` 里挨着的那一行。
func TestADeepLinkMakesRoomForTheMainWindow(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停，免得播放器的时钟改状态
	_ = miniTester(mini)

	if main.mini == nil {
		t.Fatal("起点就不对：迷你窗口没开着")
	}

	main.handleDeepLink("/settings")

	if main.mini != nil {
		t.Error("链接过来了而迷你还开着：路由换的是主窗口那一侧，主窗口收着，" +
			"用户看见的还是迷你那一扇、页面上什么都没有")
	}
	if got := main.router.Path(); got != "/settings" {
		t.Errorf("路由停在 %q，want /settings", got)
	}
	// 音乐不受影响：让开一扇窗不是退出应用。
	if st := a.Snapshot(); !st.HasTrack {
		t.Error("让开迷你把歌停了")
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
	mini, _ := newMiniShell(t, func() { back++ })
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
	// ⚠️ **先暂停再量**。`enqueueTwo` 是让第一首开始放的，而放着的时候播放器那个
	// 每 50ms 一拍的钟会把当前位置写回 `App`（`app.go` 的 tick）——于是拿
	// `a.Snapshot().Position` 做「按住前后比较」是在和那条 goroutine 赛跑：
	// 不带 `-race` 九次全绿，带 `-race` 十二次红五次（失败全是「位置被改成
	// 0.01~0.05 秒」）。产品代码没有问题，是这条断言自己踩了那条钟。
	//
	// 暂停之后 `tick` 的 `live` 是假，位置不再自己走，于是后面那个「松手落在
	// 拖到的那一格」的断言是确定的。
	a.Toggle()
	if st := a.Snapshot(); st.Playing {
		t.Fatalf("起点就不对：暂停之后还在放，位置会自己走，量不到东西")
	}

	tt := miniTester(s)
	settleMini(t, tt, "时长读回来了", func() bool { return a.Snapshot().Duration > 0 })

	bar := progressRect(t, tt)
	y := bar.Y + bar.H/2
	tt.Press(bar.X+bar.W*0.1, y)
	tt.Move(bar.X+bar.W*0.8, y)

	if !s.miniHeld {
		t.Fatal("按住的时候 miniHeld 是假：ScrubStart 没调过，播放器那边不知道" +
			"「用户开始拖了」")
	}
	// ⚠️ **「按住的时候值归用户」这一条是由下面那个断言量的，不在这里另量一次。**
	//
	// 原来这里是「按住期间 `App` 里的位置一个字节都没变」——那条**测错了东西**：
	// `viewMiniProgress` 的第三支（`case !s.miniHeld && known: s.miniScrub = …`）
	// 写的是**滑块自己的值**，从头到尾不碰 `App` 的位置，所以位置变没变跟它无关。
	// 而它偏偏又是唯一和那条 50ms 的钟赛跑的一行。
	//
	// 真正要断的是「**拖到哪儿，松手就 seek 到哪儿**」：把那个 `!s.miniHeld` 守卫
	// 去掉（`case known:`），按住期间滑块每帧都被 App 的位置覆盖、弹回起点，
	// 松手那次 `ScrubEnd` 于是落在起点而不是拖到的那一格——下面那条立刻红。

	tt.Release(bar.X+bar.W*0.8, y)
	tt.Frame()
	if s.miniHeld {
		t.Error("松手之后 miniHeld 还是真：ScrubEnd 没调过，于是下一次按下去" +
			"不会重新 ScrubStart")
	}
	dur := a.Snapshot().Duration
	if got, want := a.Snapshot().Position, dur*8/10; got < want-dur/10 {
		t.Errorf("松手之后位置是 %.2f 秒，而拖到了 %d 的八成（%.2f 秒）：seek 落在了"+
			"别的地方——按住的时候值没归用户，被 App 每 50ms 写回的位置覆盖了，"+
			"症状是**拖不动**（按住往右拖，一松劲就弹回去）",
			got.Seconds(), dur, want.Seconds())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 出图
// ─────────────────────────────────────────────────────────────────────────────

// TestRenderMiniShots 把迷你播放器存成 PNG（01-浅色 / 02-深色），留给人眼对照。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderMiniShots
//
// ⚠️ **迷你窗单独出图**：它和主窗口是两个外壳、两扇窗，而这一扇只有 320×100——
// 混在整窗图里看不清封面那一列到底多宽、四个图标键摆在哪。
//
// ⚠️ **两套主题都要出**（DESIGN.md §八）：深色下那一列和右边那块的层次差别
// 更明显，而浅色下封面的圆角和卡片底几乎同色。
func TestRenderMiniShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	requireTestAudio(t)

	// ⚠️ **要挂设置**：深色那一档是用户在设置里选的，而迷你窗读的是**主窗口
	// 那个 App 上的设置**（`NewMini` 的第二个参数就是主窗口的外壳）——不挂的话
	// `UpdateSettings` 改的是一个没挂盘子的 App，画面上什么都不会变。
	a := app.New()
	if err := a.MountSettings(filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); _ = a.CloseSettings() })
	main := newTestShell(t, onApp(a)).s
	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)

	// 带封面：这一票的封面列就是 100，没有封面的图看不出那一列多宽。
	g := newGatedCover(t)
	g.open()
	if err := a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: testFLAC, Title: "第一首", Cover: g.url(),
	}); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：画面上那个键写着「播放」

	tt := miniTester(s)
	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return !miniCoverBox(tt).Empty()
	})
	save(t, dir, "mini-01-light.png", tt)

	// 深色：用户在设置里选的，这一扇窗下一帧就跟着变（铁律二）。
	if _, err := a.UpdateSettings(func(st *app.Settings) { st.ThemeMode = app.ThemeModeDark }); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	save(t, dir, "mini-02-dark.png", tt)
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
//	5. 面板语义本身：点迷你窗**不激活应用**（票 #162）。那靠的是 Cocoa 的
//	   nonactivating `NSPanel`，无头 Tester 里没有窗口可点。
//	6. Panel 开着的时候位置记忆仍然有效（票 #162 的硬指标）：`isZoomed` 是 Cocoa
//	   的行为，窗口状态也要真窗口才写得出来（见 `MiniWindowOptions` 的实测表）。
//	7. **迷你播放器歌词卷帘（票 #536）**：
//	   a. **窗真的变高了**：真窗的高度 = 84 + 卷帘高（236），而**窗高严格等于
//	      内容高度**——多出来的那一块是透明的，而**透明的那一块照样吃点击**
//	      （这一条要真的把底下那个应用的按钮点一下才验得出）。
//	   b. **`window-state.json` 里真的写着 320×84**：展开着关窗 → 落盘的是
//	      320×320 还是 320×84（`StateKey` 抓的是关窗那一刻的宽高，
//	      `mygo/window_state.go` 的 `captureState`）。而**重启后开出来是不是
//	      84 高**也是同一件事的另一半。
//	   c. **WorkArea 真的夹住了**：把窗拖到屏幕下半部再点封面，卷帘的下边
//	      不能超出工作区（而**放不下 120 的时候不展开**）。
//	   d. **Esc 到不到得了这一扇窗**：迷你窗是 `Panel`，点它**不激活应用**，
//	      于是键盘还在原来那个应用手里——Esc 走的是**那个应用的窗口**。
//	      ⇒ 这一版在**展开的那一下**补了两步（`miniCurtainActivate`）：
//	      **① 把应用带到前台**（`mygo.App.Focus()`）**② 让这一扇成为 key
//	      窗口**（`w.Focus()`）——缺一样 Esc 都到不了。
//	      ⚠️ **无头那一档只量得到第②样**（第①样要应用 ready，而无头 Tester
//	      里压根没有应用）：见 `minicurtain_state_test.go` 的
//	      `TestTheMiniCurtainFocusesItsWindowWhenItOpens`。
//	      ⇒ **真机上还要验的是第①样**：点封面之后**应用是不是真的到了前台**
//	      （而这正是评审在真机上抓到的那一半：2026-10-27 实测「访达仍在前台、
//	      窗仍 301 高、按 Esc 到不了」）。
//	   e. **悬停展开可不可用**：这一版**没做悬停**（面板失焦时收不到鼠标移动，
//	      BMG-280 §2.4），而「悬停能不能当增强」要等真窗上验过 hover 可用
//	      之后才谈得上。
//	8. **多显示器定位（票 #555）**：这一族的**系统行为**只有真机能验——
//	   无头那一档量到的是「定位规划」那半（`minidisplays_test.go`），而**真
//	   双屏 / 插拔 / 旋转不在台账里装**（台账只装代码能证明的那几条）。
//	   a. **从第二块屏打开**：把指针放在第二块屏上 → 点「迷你」→ 窗**落在第
//	      二块屏上**（不是主屏）。⚠️ **第二块屏在主屏左边时它的坐标是负的**
//	      （框架那一侧的全局坐标，见 `mygo/internal/darwin/window.go:1377`
//	      的 `rectFromMac`）——这一档要连着**负坐标**一起看。
//	   b. **跨屏拖动**：把迷你窗从一块屏拖到另一块屏 → 点封面 → 卷帘按**新
//	      那块屏**的工作区算（卷帘的下边不能超出那块屏的工作区；放不下
//	      120 的时候不展开）。
//	   c. **拔掉一块屏**：迷你窗在那块屏上时把它拔掉 → 窗**回到还剩下的那块
//	      屏的工作区里**（不是留在一个没人看得见的地方）。
//	   d. **改分辨率**：迷你窗所在那块屏改了分辨率 → 窗**夹回新的工作区**
//	      （窗的大小不变——`MinWidth/MinHeight` 不许它变小）。
//	   e. **旋转**：把那块屏转 90 度 → 迷你窗的落点仍在它的工作区里。
// ─────────────────────────────────────────────────────────────────────────────
