package ui

import (
	"image/color"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
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

// writeTempSettings 把一份设置写到临时目录里的盘上，返回路径。
//
// 单独拎出来是因为「先写盘、再挂上去」这件事在好几个构造器里都要做一遍，
// 而它是**生产那条路**（真程序启动时也是先读盘），不是往界面上塞一个值。
func writeTempSettings(t *testing.T, st app.Settings) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
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
	deadline := time.Now().Add(waitBudget)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，画了多少帧都还没：%s，文字有 %q", waitBudget, what, tt.Texts())
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

// waitToastGone 等右下角那些提示**真的**退场了。
//
// ⚠️ 不要用「把存活时间改短 + 睡一下」代替它。提示是浮在右下角的一层，它盖住
// 设置页上滚到底部的那几行——「检查更新」那个按钮就在那儿。于是「赌它已经过期」
// 就是在赌机器够不够慢：实测同一个测试在整套里跑时好时坏，单独跑 30 次挂 28 次
// （快的机器上，从「画完那一帧」到「按第二次按钮」不到 1ms，提示还在屏幕上，
// 那一下点在了提示卡片上，被它吃掉了——而症状是「等了两秒，那个后台结果还没
// 到」，离真正的原因隔了三层）。
//
// 判据用 `visibleToasts`（viewToasts 画的就是它）而不是「按毫秒推算」：这一条
// 等的正是**画面上还有没有提示**，而那正是「点得着按钮」的充分条件。
func waitToastGone(t *testing.T, tt *mygoui.Tester, s *Shell) {
	t.Helper()
	waitFrames(t, tt, "右下角的提示退场", func() bool { return len(s.visibleToasts()) == 0 })
	// 再画一帧再问一次：visibleToasts 是在 viewToasts 里读的，那一帧之前收掉的
	// 条目，画面上要再过一帧才真的没有。
	tt.Frame()
	if n := len(s.visibleToasts()); n != 0 {
		t.Fatalf("提示退场之后又冒出 %d 条，文字有 %q", n, tt.Texts())
	}
}
