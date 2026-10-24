package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	mygoui "github.com/egoist/mygo/ui"
)

// 这个文件是内部 UI 测试的**基座**：App / Shell / Tester 都在这里造，
// 收尾也在这里做，而「等一个条件成立」只有下面那两条写法。
//
// ## 为什么要有这一层
//
// 以前每个测试文件各自写一遍 `app.New()` + `New(a)` + `NewTester(...)` +
// 三个 `t.Cleanup`。写漏一个的代价还不小：`New(a)` 建的 `shortcutKeys` 带着
// 一个后台写循环，红点那个每五分钟一跳的定时器会在画了顶栏之后被 view 装上，
// 而**收掉它们的地方只有窗口的 OnClosed**——无头 Tester 没有窗口，于是每一个
// 外壳都把它们留到测试二进制退出。几百条测试就是几百个挂着的 goroutine，
// 而更要命的是它们让同包的渲染时快时慢，把「等两秒」「等 3000 帧」这类按墙钟
// 写的断言从「多半能过」变成「看机器脸色」。
//
// 现在收尾是 `Shell.Close`，和真窗口关掉时走的是同一份代码（见 shell.go），
// 由 `newTestShell` 无条件挂上：造外壳的地方只有一处，收尾也就没有「忘了谁」。
//
// 用法见下面那几个 `newShell` / `shellWithSettings` ——它们是这一层之上
// 按主题分的薄封装，签名没动，所以上百条测试的调用点一行都不用改。

// testShell 是「一个外壳 + 它的 Tester + 它那个 App」。
type testShell struct {
	t  *testing.T
	a  *app.App
	s  *Shell
	tt *mygoui.Tester

	// settingsPath 是挂上去的那份设置在盘上的位置；没挂设置时是空串。
	settingsPath string
}

// shellOpt 改「造一个外壳」这件事的某一样。全部可选。
type shellOpt func(*shellSpec)

// shellSpec 是造外壳之前先把要求攒起来的地方。
type shellSpec struct {
	// app 是要挂的外壳所依附的那个 App。nil（默认）就现建一个；给了就用它，
	// 而收尾**不**关它（那是调用方的 App，它自己管）。
	app      *app.App
	settings *app.Settings // 非 nil：先把这份设置写到临时目录，再按「启动时读盘」那条路挂上
	width    int           // 0 就是 1200
	height   int           // 0 就是 800
	dark     *bool         // 非 nil：假装系统是深色/浅色
	prefs    *mygoui.Preferences
	// animating 打开切页动画。**默认关掉**，理由见 defaultTransition 的注释。
	animating bool
	mount     func(t *testing.T, a *app.App) // 在建外壳**之前**跑：那些 Mount* 都要赶在 New 之前
	stub      func(t *testing.T, s *Shell)   // 建好外壳之后、造 Tester 之前跑：换掉接缝上的桩
	// view 换掉 Tester 画的那一支，默认是 `s.View`。只有「这条测试就是要绕过
	// 整窗、单独画一块」的那种才需要它（画播放条、画登录页那一支）。
	view func(s *Shell) func(*mygoui.Context)
	// noTester 不要 Tester：这一条只要一个外壳（收尾、字段、纯逻辑）。
	noTester bool
	// migration 是这个外壳启动时看到的迁移报告。零值 = 什么都没迁（默认）。
	migration *MigrationReport
}

// withStartupMigration 造外壳时给它一份启动迁移报告（设置页「配置」那一段要读
// 它）。名字里那个「Startup」是为了和 `settings_tab_config_test.go` 里那个
// `withMigration(t, r)` 区分开——那是造完外壳之后再改的另一个用法。
//
// ⚠️ 默认是**零值**，而且基座在造外壳**之前**就把包级那份抹成零——理由见
// `resetMigrationReport`。
func withStartupMigration(r MigrationReport) shellOpt {
	return func(sp *shellSpec) { sp.migration = &r }
}

// resetMigrationReport 抹掉包级的迁移报告，并在测试结束时把它恢复回去。
//
// ## 为什么它归基座管，而不是每条测试自己记得收
//
// `migration` 是**包级的一个变量**（migrate_mount.go），而「点一下『从 Biu 导入』」
// 那条后台线会直接往它里面写（`rememberImport`）。于是它和外壳、和 App 都无关，
// 活到下一条测试：留在那儿的话，**后面每一个新建的 Shell 第一帧都弹一条 toast**
// （viewMain → announceMigration），而 toast 的卡片底色是**主色**（Biu 的
// `color: "primary"`）。`settings_test.go` 那些验窗口底色的测试取的采样点
// (700, 430)（900×600 窗口的右下角）**正好落在那张卡片上**，于是那几条红成
// 「画面那个点是 {30 215 96}」——那个数就是 DefaultPrimaryColor 逐字（实测）。
//
// 一个包级变量，测试收不掉——所以收回基座：**造外壳之前抹成零**，收尾时恢复。
// 「造之前抹」是关键：即便上一条测试的后台线晚了半拍才写进来，它落的那一刻本条
// 测试的外壳已经建好、第一帧也画完了，抹不掉它；要到**下一条**测试建外壳时才会被
// 抹掉，那时它已经害不到人了。
func resetMigrationReport(t *testing.T) {
	t.Helper()
	migration.mu.Lock()
	before := migration.r
	migration.r = MigrationReport{}
	migration.mu.Unlock()
	t.Cleanup(func() {
		migration.mu.Lock()
		migration.r = before
		migration.mu.Unlock()
	})
}

// onApp 让外壳挂在**外面那个** App 上（那些测试要拿同一个 App 验队列、相位之类
// 的东西，就自己建一个再递进来）。
//
// 收尾照旧走 `Shell.Close`，于是**它也会关那个 App**——`App.Close` /
// `CloseSettings` 都是幂等的，而调用方自己挂的那份收尾随后跑一遍什么也不做。
// 两边都收，比「一边收一边指望另一边」少一种漏法。
func onApp(a *app.App) shellOpt {
	return func(sp *shellSpec) { sp.app = a }
}

// withSettings 让外壳挂一份真设置（落在临时目录里）。
//
// 走的是「先写盘、再 MountSettings」那条生产路——和真程序启动时一样——而不是往
// 界面上硬塞一个 Settings 值。要改设置用 `a.UpdateSettings`。
func withSettings(st app.Settings) shellOpt {
	return func(sp *shellSpec) { sp.settings = &st }
}

// at 换 Tester 的窗口尺寸。
func at(w, h int) shellOpt {
	return func(sp *shellSpec) { sp.width, sp.height = w, h }
}

// systemDark 告诉 Tester「系统外观是深色」。
//
// ⚠️ 这是 **Tester 的偏好**（每帧开头框架会照它重置主题），不是系统的真实状态。
// 主题那一组测试要的就是这一条：设置说浅色、系统说深色，才验得出「设置压过了
// 系统」。
func systemIsDark(on bool) shellOpt {
	return func(sp *shellSpec) {
		d := on
		sp.dark = &d
	}
}

// withPrefs 设 Tester 的偏好（ReduceMotion、TextScale…）。
func withPrefs(p mygoui.Preferences) shellOpt {
	return func(sp *shellSpec) { sp.prefs = &p }
}

// withPageAnimation 把切页动画**打开**。
//
// 默认是关掉的（见 defaultTransition），所以要验动画的那一条必须显式要回来。
func withPageAnimation() shellOpt {
	return func(sp *shellSpec) { sp.animating = true }
}

// mounted 在建外壳之前挂应用层的那一摊（MountLibrary / MountExplore / …）。
func mounted(fn func(t *testing.T, a *app.App)) shellOpt {
	return func(sp *shellSpec) { sp.mount = fn }
}

// stubbed 在建好外壳之后换掉接缝上的桩（picker、autostart、pickImage…）。
func stubbed(fn func(t *testing.T, s *Shell)) shellOpt {
	return func(sp *shellSpec) { sp.stub = fn }
}

// withClock 把外壳那个时钟换成假时钟（clock.go）。
//
// 倒计时、防抖、红点这几个节奏都由它算，而它们在真时钟上要么等不到、要么要靠
// 「等两秒」赌——假时钟让「3 秒后回首页」是一行 Advance。
func withClock(c clockSource) shellOpt {
	return stubbed(func(_ *testing.T, s *Shell) { s.clock = c })
}

// withView 换掉 Tester 画的那一支。fn 先拿到外壳本身，再返回真正画的那一支。
//
// ⚠️ 分两步不是啰嗦：`mygo.NewTester` **当场就画一帧**，所以画的那一支在
// Tester 造完之前就会被调一次。一次给一个 `func(*mygoui.Context)` 的写法只能靠
// 一个还没赋值的外壳变量（`var b *testShell; b = newTestShell(...)`），而那一帧
// 正好在赋值之前跑——当场空指针。
//
// ⚠️ 只在「这条测试就是要绕过整窗、单独画一块」时用（画播放条那一块、画登录页
// 那一支）。要验整窗就用默认的 `s.View`——整窗才是大部分测试要的东西。
func withView(fn func(s *Shell) func(*mygoui.Context)) shellOpt {
	return func(sp *shellSpec) { sp.view = fn }
}

// withoutTester 只要一个外壳，不要 Tester。
func withoutTester() shellOpt {
	return func(sp *shellSpec) { sp.noTester = true }
}

// newTestShell 造「一个 App + 一个挂在它上面的 Shell + 一个画它的 Tester」。
//
// **收尾已经挂好了**（`t.Cleanup(sh.Close)`），测试自己不要再挂一遍。
func newTestShell(t *testing.T, opts ...shellOpt) *testShell {
	t.Helper()
	sp := &shellSpec{}
	for _, o := range opts {
		o(sp)
	}

	// 包级那个迁移报告先抹成零（理由见 resetMigrationReport 的注释）。**必须在
	// New(a) 之前**：外壳建好之后第一帧就会读它。
	resetMigrationReport(t)

	// 那一份「窗口看不看得见」是包级的（`hover.frameVisible`），而 `withView`
	// 可以绕过 `Shell.View` 单独画一块——那样它就不会被每帧重设，于是上一条
	// 测试留下的「看不见」会漏进这一条。每条测试都从「看得见」开始。
	hover.SetFrameVisible(true)
	if sp.migration != nil {
		migration.mu.Lock()
		migration.r = *sp.migration
		migration.mu.Unlock()
	}

	a := sp.app
	if a == nil {
		a = app.New()
	}

	// 设置排在最前面：MountSettings 要读盘，而那几个 Mount* 都要赶在 `New`
	// 之前（`New` 会把它们的回调接上，见 shell.go 里 New 的注释）。
	sh := &testShell{t: t, a: a}
	if sp.settings != nil {
		sh.settingsPath = writeTempSettings(t, *sp.settings)
		if err := a.MountSettings(sh.settingsPath); err != nil {
			t.Fatal(err)
		}
	}
	if sp.mount != nil {
		sp.mount(t, a)
	}

	// 封面磁盘缓存：替成临时目录。无头 Tester 里 `mygo.App.Path` 返回的也是
	// 一个**真的、可写的**用户缓存目录，不替的话每条测试都往那里写封面，
	// 测试之间还会串数据。收尾顺序（LIFO）：先 `Shell.Close` 把盘落完、把
	// writer 停掉，再恢复这一格，最后才是 `writableTempDir` 删目录。
	origCoverCacheDir := coverCacheDir
	cacheDir := writableTempDir(t, "covers")
	coverCacheDir = func() string { return cacheDir }
	t.Cleanup(func() { coverCacheDir = origCoverCacheDir })

	s := New(a)
	sh.s = s
	if sp.stub != nil {
		sp.stub(t, s)
	}
	// 🔴 关掉切页动画。Tester 一帧画完，250ms 的滑动在里面永远走不完：那一帧
	// 停在半路，上一页正往左挪、下一页正往右进，中间那道分界线正扫过页面区域。
	// 于是「点一下再读某个点」这一次读到窗口底色、下次读到分界线，同一条测试
	// 时而红时而绿。
	//
	// 造外壳的所有入口都关，是为了同一个理由：屏幕上的任何一个采样点都要是
	// 确定的。要验动画的那一条用 `withPageAnimation()` 要回来。
	s.router.Transition = defaultTransition(sp.animating)

	w, h := sp.width, sp.height
	if w == 0 {
		w = 1200
	}
	if h == 0 {
		h = 800
	}
	view := s.View
	if sp.view != nil {
		view = sp.view(s)
	}
	if !sp.noTester {
		tt := mygoui.NewTester(view, w, h)
		if sp.dark != nil {
			tt.SetDark(*sp.dark)
		}
		if sp.prefs != nil {
			tt.SetPreferences(*sp.prefs)
		}
		sh.tt = tt
	}

	// 收尾。`Shell.Close` 和真窗口关掉时走的是同一份代码（shell.go）：停掉
	// 四个定时器、封面缓存、应用状态，并把关着的设置 / 搜索历史 / 快捷键落完盘。
	//
	// ⚠️ 它是**无条件**挂的，不给「这个外壳特殊所以不收」留口子——以前那种
	// 留口子的写法就是漏的那几个（快捷键的后台写循环、红点定时器）。
	t.Cleanup(sh.Close)
	return sh
}

// Close 提前收尾。测试想在自己的断言跑完之前就收尾时用（绝大多数不需要，
// `t.Cleanup` 已经挂好了）。可以重复调。
func (sh *testShell) Close() {
	sh.t.Helper()
	sh.s.Close()
}

// defaultTransition 造外壳时该用哪一种切页动画。
func defaultTransition(animating bool) mygoui.Transition {
	if animating {
		return mygoui.TransitionAuto
	}
	return mygoui.TransitionNone
}

// writableTempDir 造一个**后台写盘会往里写**的临时目录，测试结束时删掉。
//
// # 为什么不直接用 `t.TempDir()`
//
// `t.TempDir()` 的清理是 `os.RemoveAll` **一遍**：先读目录项、逐个删、再删目录。
// 而后台写盘（设置、歌词缓存）是**原子写**——先在同一个目录里建一个
// `.settings-*.tmp` / `.lyrics-*.tmp`，写完再 rename 过去。于是只要那个临时文件
// 恰好在「读完目录项」和「删目录」之间出现，删目录就是 ENOTEMPTY：
//
//	--- FAIL: TestXxx
//	    testing.go:1617: TempDir RemoveAll cleanup: unlinkat .../001: directory not empty
//
// **这种红跟被测的逻辑一点关系都没有**：测的是播放条，挂在的是临时目录删不掉。
// 机器越忙（这台机器上别的会话同时在跑 `go test ./internal/ui/`），那个窗口越
// 容易撞上——`testing` 包的 `t.TempDir()` 清理**只在 Windows 上重试**
// （`isWindowsRetryable`），macOS 上不重试，于是只有 macOS 会红。
//
// 所以这里自己造目录、自己删，删的时候**重试**（和 `testing` 包在 Windows 上
// 做的同一件事）。
//
// 目录建在 `t.TempDir()` 底下：崩了也会被 Go 收走，而它那份清理跑在我们之后
// （后注册的先跑），那时候这一层已经空了。
func writableTempDir(t *testing.T, tag string) string {
	t.Helper()
	dir, err := os.MkdirTemp(t.TempDir(), tag+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAllWithRetry(t, dir) })
	return dir
}

// removeAllWithRetry 删一个目录，撞上「正有后台写盘在往里写」就重试。
//
// 重试到点还删不掉就报错——那说明确实有东西一直往里写，那是另一回事（真 bug），
// 不该在这里悄悄放过。
func removeAllWithRetry(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.RemoveAll(dir)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("删临时目录 %s：%v", dir, err)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writeTempSettings 把一份设置写到临时目录里的盘上，返回路径。
//
// 单独拎出来是因为「先写盘、再挂上去」这件事在好几个构造器里都要做一遍，
// 而它是**生产那条路**（真程序启动时也是先读盘），不是往界面上塞一个值。
func writeTempSettings(t *testing.T, st app.Settings) string {
	t.Helper()
	path := filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)
	store, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(cur *app.Settings) { *cur = st }); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestALeakedMigrationReportCannotReachTheNextShell 钉住「包级那个迁移报告收得掉」。
//
// ## 它钉的是哪一种坏
//
// 「点一下『从 Biu 导入』」那条后台线会**直接往包级的 `migration.r` 里写**
// （`rememberImport`），而那个变量和外壳、和 App 都无关，活到下一条测试。留在
// 那儿的话，**后面每一个新建的 Shell 第一帧都弹一条 toast**（`viewMain` →
// `announceMigration`），而 toast 的卡片底色是**主色**（Biu 的 `color: "primary"`）。
//
// 受害的是别的文件里读像素的那几条：它们的采样点 (700, 430)（900×600 窗口的右下
// 角）**正好落在换行了的那张卡片上**——卡片一旦换行就向上长，于是那一块变成主色。
// 症状是「画面那个点是 {30 215 96}」，那个数就是 `DefaultPrimaryColor` 逐字。
//
// 实测：15 次 `go test ./...` 里飘 1 次，而它飘的是**别的文件**的测试，追起来要
// 跨三个文件——症状离真因隔了三层。
//
// ## 为什么这里要手动往那个全局里塞一份「漏进来的」报告
//
// 真正漏它的那条路径是后台线**晚于收尾**才写（`reloadSettings` → `rememberImport`
// → `addToast`），而测试等的是 `reloadSettings` 改出来的设置，不是报告本身。复现
// 那一次调度很脆，直接把「漏进来了」这个状态摆出来更稳。
//
// ⚠️ **本条的判据是像素**，因为受害的正是读像素的那些测试：光断言「没有 toast」
// 也能过，但那不是用户看见的东西。
//
// 拿掉 `resetMigrationReport` 这一句本条立刻红（读到 {30 215 96}、toasts=1）。
func TestALeakedMigrationReportCannotReachTheNextShell(t *testing.T) {
	for i := range 20 {
		// 摆一份「上一条测试漏下来的」报告。
		migration.mu.Lock()
		migration.r = MigrationReport{
			Imported: app.Imported{
				Dir: "/Users/x/Biu", Settings: true,
				// 「请重新登录」那一句逼首帧那条 toast 换行，卡片才会向上长到
				// 采样点那一块；只有一行的话它压根够不着。
				Notes: []string{"Biu 那边是登录状态。这一版不搬过来——请在这个应用里重新登录一次。"},
			},
		}
		migration.mu.Unlock()

		tt, s, _, _ := shellWithSettings(t, app.ThemeModeDark, true)
		want := BuildTheme(s.app.SettingsSnapshot(), true).Background
		for f := range 3 {
			tt.Frame()
			// ⚠️ **推钟让入场走完**：提示是淡进来的（250ms），而 Tester 的钟
			// 站着不动——不推的话那条提示在头几帧里几乎是全透明的，采样点照样
			// 是背景色，于是这一条会**绿得像没漏**（它要抓的恰恰是那个漏）。
			settleToast(t, tt)
			got := tt.Image().RGBAAt(sampleX, sampleY)
			if got == (color.RGBA{R: want.R, G: want.G, B: want.B, A: want.A}) {
				continue
			}
			t.Fatalf("第 %d 轮第 %d 帧：采样点 (%d,%d) 是 %v，want %v——"+
				"说明包级那份迁移报告漏进来了，首帧弹了一条主色底色的 toast 盖在上面（屏幕上 %d 条）",
				i, f, sampleX, sampleY, got, want, len(s.visibleToasts()))
		}
	}
}

// TestRedrawingAfterCloseDoesNotRearmAnyTimer 钉住「关掉之后再画一帧，不会再装定时器」。
//
// ## 它挡的是什么
//
// 三个装定时器的地方（`feed.arm` 每五分钟一跳、`armNotFound` 每一秒一跳、
// `syncSearchWatch` 每 125ms 一跳）都是**每帧**都会到的，而 `Close` 收掉的是
// **某一个已经装上的**那一个。关掉之后只要还有一帧画到，它们就再装一个——
// 而那一个谁都不收，一路挂到测试进程结束。
//
// `close_while_loading_test.go` 正好是这个形状：`s.Close()` 之后紧跟一个
// `tt.Frame()`（它要验的正是「迟到的唤醒、失效的句柄、关掉的缓存全在这一帧里碰」）。
// 真窗口里 `OnClosed` 之后不会再有帧，所以这条靠「恰好不会」躲过去了；测试里躲不掉。
//
// 早先 `Close` 里那个「`closed` 就 return」也是半截修法：它让第二次 `Close` 收不掉关掉
// 之后新装的那些。
//
// ⚠️ **三个各测各的**，因为它们互相拆台：离开 404 会 disarm 倒计时，切离列表页会让
// `syncSearchWatch` 判定「不用醒」而 disarm 防抖钟——所以没法让三个同时装着再一起查。
//
// 判据是那三个字段本身（`feed.stop` / `notFoundStop` / `searchWatchStop`）——它们就是
// 「有没有装上」的那一位，不是在外面猜。
func TestRedrawingAfterCloseDoesNotRearmAnyTimer(t *testing.T) {
	for _, c := range []struct {
		name  string
		arm   func(t *testing.T, s *Shell, tt *mygoui.Tester)
		armed func(s *Shell) bool
	}{
		{
			name:  "红点那个每五分钟一跳的",
			arm:   func(_ *testing.T, _ *Shell, tt *mygoui.Tester) { tt.Frame() }, // 首帧就装
			armed: func(s *Shell) bool { return s.feed.stop != nil },
		},
		{
			name: "404 那个每一秒一跳的",
			arm: func(_ *testing.T, s *Shell, tt *mygoui.Tester) {
				s.navigate("/没有这条")
				tt.Frame()
			},
			armed: func(s *Shell) bool { return s.notFoundStop != nil },
		},
		{
			name: "搜索防抖那个每 125ms 一跳的",
			arm: func(_ *testing.T, s *Shell, tt *mygoui.Tester) {
				s.navigate(laterPath)
				tt.Frame()
				// 那个钟要「这一页上真的敲了字还没发出去」才装（syncSearchWatch 读
				// 的是 pendingSince），所以摆一个进去再让它同步一次。
				s.laterPage.pendingSince = time.Now()
				s.syncSearchWatch()
			},
			armed: func(s *Shell) bool { return s.searchWatchStop != nil },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, tt := newShell(t)
			// 先让它**真的装上**，否则这一条测的是「本来就没装」。
			c.arm(t, s, tt)
			if !c.armed(s) {
				t.Fatal("起点就不对：这个定时器本来就没装上，那这一条是空的")
			}

			s.Close()
			if c.armed(s) {
				t.Error("Close 走完它还装着")
			}

			// 关掉之后再画几帧：这一段就是 `close_while_loading_test.go` 的形状。
			for range 5 {
				tt.Frame()
			}
			if c.armed(s) {
				t.Error("关掉之后重绘又装上了——它会一路挂到测试进程结束")
			}

			// 而 `Close` 再走一遍也必须是真的走一遍（不是被 `closed` 挡掉）——
			// 上面那条哪天漏了一处，这一句就是最后一道网。
			_ = 0
			if c.armed(s) {
				t.Error("第二次 Close 没有收干净")
			}
		})
	}
}

// TestTheTestSuiteDoesNotGrowASecondWayToDoEitherThing 钉住本包的三条约定。
//
// ## 为什么需要一条测试来钉「约定」
//
// 三条约定都已经因为「漏一条」各付过账：
//
//   - **造外壳**：漏收 `shortcutKeys` 的后台写循环和几个定时器，于是同包几百个
//     外壳把它们留到测试二进制退出。量过：改之前建 40 个外壳残留 86 个 goroutine。
//   - **等条件**：墙钟预算扛不住同包的负载，满载时偶发红。
//   - **滚到尽头**：数「滚了几下」赌的是「滚完那几下的时候下一页回来了」，而那是
//     后台一趟 HTTP。实测满载时关注网格那条 `TestTheGridIsAVirtualGrid` 飘红，
//     症状是「滚到底之后末尾那几张没画出来」——离真因隔着三层，读报错的人只会
//     去查虚拟网格（见 `scrollToEnd` 那段注释）。
//
// 而头两条约定**已经在三次合并里各被重新违反过三次**：`#84` 的 `newHomeShell`、
// `#85` 的 `realdata_test.go` / `rowplayable_test.go`、这一次的
// `dynamiccard_test.go` / `list_favorite_test.go` / `sidebar_favorites_actions_test.go`
// ——每一次都是新写的测试顺手用回老写法（不是谁忘了，是那套写法在这个包里本来就
// 存在、读起来也通顺）。每一次都要等合并时才在编译器报错或者在跑出来时飘。
//
// 所以把「只有这两种等法、只有一个造外壳的地方、滚到尽头只有一个写法」写成断言：
// 新写的测试再犯，本地立刻红，而不是等到合并或者到板上才发现。
//
// ⚠️ `package ui_test` 的文件**不再是例外**：那四个（`playbar_*` / `queue_drawer_test.go`）
// 已经搬进 `package ui`，因为它们是这个包里唯一收不掉 `New(a)` 起的两个后台写循环和
// 首帧那个红点定时器的地方。往后再有 `package ui_test` 的文件，这一条会当场报错。
func TestTheTestSuiteDoesNotGrowASecondWayToDoEitherThing(t *testing.T) {
	// 这一票合成掉的那些重复辅助。它们和基座那两条同名同义，只是判据里没有
	// 「等什么」，失败时报不出有用的那句话。
	//
	// ⚠️ `settleFrame` **不在**这一列：它数的是「MyGo 一遍 Frame() 跑两遍 view、
	// 点击被消费时再多一遍」那个**固定遍数**，和机器负载无关，而它等的是那个遍数
	// 走完、不是任何后台结果。`collectionpage_test.go` 里它还活着、也有理由。
	retired := []string{
		"settle(", "settleSearch(", "waitUntilTrue(", "waitFrameFor(",
		"waitForFrames(", "waitSpace(", "waitUI(", "waitCover(", "waitForScan(",
	}

	for _, name := range filesInThisPackage(t) {
		// 这一条自己得放过：它逐个列着那些名字（不然就查不出来），而它也调 New(a)。
		if name == "shelltest_test.go" {
			continue
		}
		// 读不到就报错，不能当成「干净」：读不到的那份等于没查，而这一条的全部
		// 价值就在于「每个文件都查了」。
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("读不到 %s，这一就没法查了：%v", name, err)
		}
		src := string(b)
		// 认的是**包子句**（第一行非注释、非空的那一行），不是「文件里有没有出现
		// `package ui_test` 这几个字」——后者会把一条提到它的注释也算进去。
		//
		// ⚠️ 早先这里是**放过** `package ui_test` 的（理由是「那四个文件用不了内部基
		// 座」）。那是把责任推给了下一个读代码的人：那四个文件于是成了这个包里唯一
		// 不受收尾保证的地方，`New(a)` 起的两个后台写循环、首帧装的红点定时器，一个
		// 都没人收——而它们和其余几百条测试在同一个测试二进制里。
		// 现在那四个文件都已经搬进 `package ui` 了，所以这里改成**报错**：再有
		// 一个 `package ui_test` 的文件进来，那就是绕过收尾保证，当场指出来。
		if pkgClause(src) == "package ui_test" {
			t.Errorf("%s 是 package ui_test——它用不了基座，于是成���这个包里唯一不收"+
				"尾的地方（%s 起的后台写循环和定时器没人收）。搬进 package ui，"+
				"造外壳走 newTestShell。", name, pkgClause(src))
			continue
		}
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // 注释里提到它们是正常的（解释为什么没有了）
			}
			for _, call := range retired {
				if strings.Contains(line, call) {
					t.Errorf("%s:%d 用了已经合成掉的 %q——这个包里等条件只有基座那两条"+
						"（waitFor / waitFrames），见本文件头", name, i+1, call)
				}
			}
			// 造外壳：出现 `New(a)` 就是绕过了基座（基座自己在 shelltest_test.go 里调）。
			if strings.Contains(line, "New(a)") {
				t.Errorf("%s:%d 直接 New(a) 造外壳——走 `newTestShell`（本文件），"+
					"收尾是无条件挂上的，漏一次就是一组挂到测试二进制结束的 goroutine",
					name, i+1)
			}
		}
		// 「滚固定几下」也是「按次数等」的一种：见 scrollToEnd 那段注释。
		for _, at := range scrollsInsideFixedLoops(src) {
			t.Errorf("%s:%d 在一个跑固定遍数的 for 里滚轮——滚到尽头只能等"+
				"「尽头真的在画面上」（scrollToEnd），数次数赌的是「滚完那几下的时候"+
				"下一页已经回来了」，而那是后台一趟 HTTP，满载时回来不了那么快",
				name, at+1)
		}
	}
}

// boundedFor 认的是「跑固定遍数的 for」：`for range N` 和三子句那种（`for i := 0;
// i < N; i++` / `for i := 0; i < N {`）。
//
// ⚠️ 只认**循环头里有计数**的：`for st.AtEnd() { … }` 那种本来就是等条件，
// 不该被算进来。
var boundedFor = regexp.MustCompile(`^\s*for\s+(range\s+\d+\s*\{|\w+\s*:=\s*[^;{]*;\s*\w+\s*<\s*)`)

// scrollsInsideFixedLoops 报出「在一个跑固定遍数的 for 里滚轮」的那些行号。
//
// 为什么要机器查而不是靠自觉：这一条已经被新写的测试犯过四次（关注网格 ×3、
// 用户动态的作者栏 ×1），而它红起来的样子是「末尾那几张没画出来」——离真因隔着
// 三层（后台那一趟 → 行数翻倍 → 偏移停在旧高度），读报错的人只会去查虚拟网格。
func scrollsInsideFixedLoops(src string) []int {
	var at []int
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "//") || !boundedFor.MatchString(line) {
			continue
		}
		depth := strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			continue // 单行的 for，本包里没有
		}
		for j := i + 1; j < len(lines) && depth > 0; j++ {
			depth += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
			if depth > 0 && strings.Contains(lines[j], ".Scroll(") {
				at = append(at, j)
			}
		}
	}
	return at
}

// pkgClause 取一个 Go 源文件的包子句（第一行非空、非注释的那一行）。
//
// 为什么不用「文件里有没有 `package ui_test` 这几个字」：那个查法会把**提到**它的
// 注释也算进去——比如某条注释正因为在解释「为什么放过外部包」才写了它。
func pkgClause(src string) string {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		return trimmed
	}
	return ""
}

// filesInThisPackage 列出这个包里所有非外部（`package ui`）的测试文件。
func filesInThisPackage(t *testing.T) []string {
	t.Helper()
	// 编译期就在这个包里，所以这里就在这个目录；只认 `_test.go`。
	matches, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// ─────────────────────────────────────────────────────────────────────────────
// 等条件
//
// **只有这两种等法**，按「条件是不是画出来才算」选：
//
//   - waitFor     条件是后台 goroutine 直接写的状态（相位、计数、快照）。
//   - waitFrames  条件要等 view 把它收进画面（提示、列表的 Loaded、翻页器的
//     started）。后台投进信箱之后要靠**下一帧**的 adopt 才落到状态上，不画帧
//     就永远等不到。
//
// 两种都不数帧、不数毫秒：循环到条件成立为止，那个 deadline 只是「永远等不到」
// 的保险，不是预算——满载的机器上慢一点也还是能过。
// ─────────────────────────────────────────────────────────────────────────────

// waitBudget 是「等条件」的保险丝：条件真的永远不成立时，别让测试挂到
// go test 的十分钟上限。
//
// ⚠️ 它**不是**时间预算。真机上从后台投递到画出来要多久跟机器负载有关，
// 按「N 毫秒之内必须到」写的等法在满载的 CI 上必然偶发红，而它红的时候和被测
// 的逻辑一点关系都没有（实测：一个飘红的更新弹窗测试，28/30 次失败，快的机器
// 上必挂——因为它赌的是「上一条提示已经过期」，而提示活多久是墙钟说了算）。
const waitBudget = 10 * time.Second

// settleDrawer 推钟直到队列抽屉的进出场走完（面板滑到位）。
//
// # 为什么需要它
//
// ⚠️ **Tester 的钟站着不动**：不推它，抽屉就永远停在「刚打开」那一帧——而那一帧
// 面板**还在窗口外面**（它要从右边滑进来，见 queue_drawer.go）。于是「按名字找
// 抽屉里的东西」「量面板那一块的颜色」「存图」全都量的是**空的右半边**，而症状是
// 「找不到」「颜色没变」「图是空的」——**没有一条报错说是因为动画没走完**。
//
// # 为什么推钟而不是数帧
//
// 动画走的是**时间**（`c.Now()`），数帧那种等法会**静默失效**：不报错、不超时、
// 直接放行（AGENTS.md §2）。框架文档给的写法也是 `tt.Advance(d)`。
//
// # 判据是**阶段**，不是时间
//
// 「到位了没有」看的是 `drawerPhase`：开着是 `drawerShown`，关掉了（滑完了、
// 面板拆了）是 `drawerGone`。那是一个**状态**，不是「过了多少毫秒」——所以这一条
// 在快机器和慢机器上读的是同一个。
func settleDrawer(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	const step = 16 * time.Millisecond
	// 先画一帧：阶段机是在 view 里跑的，不画一帧它还没从 `drawerGone` 动。
	tt.Frame()
	for range 40 {
		switch s.drawerPhase {
		case drawerShown:
			return
		case drawerGone:
			// ⚠️ **再画一帧**：拆掉面板的那一帧里，面板**还在树上**（阶段是在
			// 建它的过程里改的），所以画面上还有它——下一帧才真的没了。
			//
			// 不画这一帧的话，「抽屉没了」这件事在画面上要晚一帧才成立，
			// 而量画面的用例会读到**上一帧**（抽屉还在）。
			tt.Frame()
			return
		}
		tt.Advance(step)
	}
	t.Fatalf("推了 %v 的钟，抽屉还没停（阶段 %d）", 40*step, s.drawerPhase)
}

// waitFor 等一个后台结果。**不要**在里面画帧——那是 waitFrames 的活。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，%s 还没发生", waitBudget, what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitFrames 等一个**要画帧**才成立的条件。
//
// ⚠️ **先画帧、再问条件**（不是反过来）：后台把结果投进信箱、下一帧的 adopt 把它
// 收进状态，而**同一帧里画出来的还是上一帧的样子**——标题那一行和它下面那句
// 提示不是同一帧落地的。先问条件会在「标题已经在了、提示还在路上」那一刻就返回。
func waitFrames(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	waitFramesFor(t, tt, waitBudget, what, cond)
}

// waitFramesFor 是 waitFrames 加一个「等多久」。
//
// ⚠️ **它只在一种场合用**：要等的那件事是**一趟后台的往返**，而那一趟在**满载的
// 机器上**（`go test ./...` 并行跑包，别的包正在抢 CPU）要好几秒——而共用那个
// 10 秒的保险丝是给「条件永远不成立」用的，不是给「慢」用的。
//
// ⚠️ **别拿它去放宽共用那个保险丝**：`internal/ui` 里有三百多处用它，
// 一起放宽的话，一个真坏掉的构建要等到 `go test` 的十分钟上限才说话
// （而那时候开发者看到的是「超时」，不是哪几条坏了）。
func waitFramesFor(t *testing.T, tt *mygoui.Tester, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，画了多少帧都还没：%s，文字有 %q", d, what, tt.Texts())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// scrollFar 一次滚到头的距离。
//
// ⚠️ **正的 DY 是往下滚**（`platform/surface.go` 的 PointerScroll 注释）。
// 写负数是往回滚，`Visible()` 一动不动，于是测试会一直是「等不到」。
//
// 这个数远大于任何一屏：它落到列表给的 MaxY 上就停，滚几下都一样。
const scrollFar = 100000

// scrollToEnd 滚到尽头，**等尽头真的在画面上**为止。
//
// x, y 是滚轮落在哪一点——**要落在那一块内容里面**，窗口左边有侧栏、下面有播放条，
// 滚到别处滚的是别的东西。cond 报「已经在尽头了」：虚拟列表给它的
// `ListState.AtEnd()`（框架自己判「这一帧露出了末尾」用的那一个），普通滚动
// 容器给「偏移已经落到 MaxY 上」。
//
// ## 为什么滚到尽头只能等，不能数次数
//
// 「滚 8 下」赌的是**滚完那 8 下的时候下一页已经回来了**。而下一页是后台 goroutine
// 里的一趟 HTTP，什么时候回来跟机器负载有关：空载时它早就回来了（那 8 下的末尾
// 已经越过第二页），满载时它回来得晚，于是滚动位置**停在第一页末尾的高度**上——
// 第二页到了、行数翻倍，可偏移不会自己跟过去（`ScrollState` 记的是偏移量，不是
// 「我盯着最后一行」），末尾那几张还在画面下面。
//
// 实测（`follow_test.go` 的虚拟网格，探针逐帧量的）：第二页在**第 2 下**落地时，
// 第 2 下之后画面还是 `[4..7]`（14 行里的中段），要第 3 下才真的到底。8 下够不够
// 完全取决于后台那一趟什么时候回来——同一个提交、同一份代码，慢一点就红。
//
// ⚠️ 这里**不是在放宽判据**：cond 就是「尽头已经露出来」本身，成立不了就一直
// 滚到保险丝（10 秒），报的仍然是同一句话。
//
// ⚠️ **cond 只对「有尽头」的列表成立**：B 站那种翻页永远翻不完的列表
// （fixture 给 `has_more` 恒为 true）根本没有末尾——哨兵每次被滚进画面就再拉一页，
// 而 `scrollToEnd` 会一直滚下去（实测一次滚到了第 2087 页还在拉），那个条件永远
// 不成立。这一类要等的是「哨兵被叫起来过一次」，写一步滚动 + 一个等条件就行。
func scrollToEnd(t *testing.T, tt *mygoui.Tester, x, y float32, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for {
		tt.Scroll(x, y, 0, scrollFar)
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，一直滚也滚不到尽头：%s，文字有 %q", waitBudget, what, tt.Texts())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// pixelBudget 是「等像素对上」画多少帧的上限。真的对不上就一直对不上，那时候
// 报的才是真问题（见 waitPixel）。
const pixelBudget = 20

// waitPixel 等画面上 (x, y) 那个采样点等于 want，返回它最后读到的颜色。
//
// ## 为什么固定像素的断言必须等，而不是画一帧就读
//
// 切路由时那一页是淡入的：刚点完的那一帧，底色还差百分之几（实测 255 里的
// 251）。那个差值跟主题对不对毫无关系，却会让「点一下再读 (700,430) 那个点」
// 的测试随机红——而且它随机红的时刻正好是「别的模块给顶栏多加了一个控件」那种
// 改动，跟主题一点关系都没有。
//
// ⚠️ **这不是把判据放宽**：条件就是 `want` 本身，一个字节都不差；等的只是「画出
// 那一帧」。真对不上就还是对不上，报的还是同一个错。
func waitPixel(t *testing.T, tt *mygoui.Tester, x, y int, want color.RGBA, what string) color.RGBA {
	t.Helper()
	var got color.RGBA
	for range pixelBudget {
		got = tt.Image().RGBAAt(x, y)
		if got == want {
			return got
		}
		tt.Frame()
	}
	t.Errorf("%s：画面 (%d,%d) 那个点是 %v，want %v", what, x, y, got, want)
	return got
}
