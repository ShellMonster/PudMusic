package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
)

// 这一组验的是「系统把一个音频文件交给本应用」这一条路。
//
// ⚠️ **无头环境里量不到的那一半**：真正的「系统把文件交给应用」要有一个
// 打包的 bundle（macOS 的 Apple Event）或者一条命令行（Windows / Linux），
// 而无头 Tester 两条都没有。所以这里验的是**回调收到路径之后的那一半**
// （`Shell.handleOpenFile`）加上**判据那一半**（`checkOpenFile`）——
// 而「系统真的把文件递过来了」那一半由 `internal/platform` 那一组
// （`mygo.json` 的登记）和真机冒烟验。
//
// ⚠️ **不碰真 B 站、不碰真网络**：这一组全部走本地文件。

// openFileFixture 造一个「一个能放得起来的本地音频文件」。
//
// ⚠️ **用真的 FLAC 素材**：判「放不放得起」得真的打开它（那个仓库里的规矩：
// 「判据说可放、实际放不了」是界面上的谎言），而假文件会红在
// 「unsupported audio format」上——那验的是另一件事。
func openFileFixture(t *testing.T, name string) string {
	t.Helper()
	requireTestAudio(t)
	dst := filepath.Join(t.TempDir(), name)
	src, err := os.ReadFile(testFLAC)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestCheckOpenFileTellsTheThreeRejectionsApart 钉住「三种没收下要说成三件
// 不同的事」。
//
// ⚠️ **顺序不能反**：先判「在不在」，再判「是不是普通文件」，最后才判
// 「格式认不认」。反过来的话，一个**已经删掉**的 `.mp3` 会被报成「格式不
// 支持」——而真相是「那个文件已经不在了」，两者要用户做的事完全不同。
func TestCheckOpenFileTellsTheThreeRejectionsApart(t *testing.T) {
	dir := t.TempDir()

	// 一个真的在、格式也对的文件。
	ok := filepath.Join(dir, "在的.flac")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkOpenFile(ok); got != openFileOK {
		t.Errorf("一个在的 .flac 被判成 %v，want openFileOK", got)
	}

	// 不在了。
	if got := checkOpenFile(filepath.Join(dir, "没了.flac")); got != openFileMissing {
		t.Errorf("一个不存在的文件被判成 %v，want openFileMissing", got)
	}

	// 不是普通文件（一个目录）。
	sub := filepath.Join(dir, "一个目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkOpenFile(sub); got != openFileNotRegular {
		t.Errorf("一个目录被判成 %v，want openFileNotRegular", got)
	}

	// 格式放不了。
	bad := filepath.Join(dir, "老歌.wma")
	if err := os.WriteFile(bad, []byte("ASF fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkOpenFile(bad); got != openFileUnsupported {
		t.Errorf("一个 .wma 被判成 %v，want openFileUnsupported", got)
	}

	// 空路径。
	if got := checkOpenFile(""); got == openFileOK {
		t.Error("空路径被判成收下了")
	}
}

// TestTheUnsupportedTextSaysWhichFormatsWeTake 钉住「格式放不了」那一句
// **说清楚支持哪些**。
//
// ⚠️ 只说「格式不支持」的话，用户手里是一首 `.wma`，而他界面上还有别的歌可
// 听——他会以为整个应用都不放歌了。而这一句是**他能照着做的**（换个格式）。
func TestTheUnsupportedTextSaysWhichFormatsWeTake(t *testing.T) {
	text := openFileRejectText(openFileUnsupported, "/Users/x/老歌.wma")
	if !strings.Contains(text, "老歌.wma") {
		t.Errorf("那句话里没有文件名：%q", text)
	}
	if !strings.Contains(text, "放不了") {
		t.Errorf("那句话没说「放不了」：%q", text)
	}
	for _, ext := range []string{".mp3", ".flac", ".wav", ".m4a"} {
		if !strings.Contains(text, ext) {
			t.Errorf("那句话里没有 %s（用户要知道换成什么才行）：%q", ext, text)
		}
	}
	// ⚠️ **`.wma` 不能出现在「支持」那一栏里**。
	if strings.Contains(text, ".wma ") || strings.HasSuffix(text, ".wma）") {
		t.Errorf("那句话把 .wma 说成支持的了：%q", text)
	}
}

// TestTheMissingTextSaysTheFileIsGone 钉住「文件不在了」那一句。
//
// ⚠️ 它和「格式放不了」是**两件不同的事**：这一档要用户去把文件找回来，
// 那一档要用户去换个格式。混成一句「打不开」的话，用户只能自己猜。
func TestTheMissingTextSaysTheFileIsGone(t *testing.T) {
	text := openFileRejectText(openFileMissing, "/Users/x/没了.flac")
	if !strings.Contains(text, "没了.flac") {
		t.Errorf("那句话里没有文件名：%q", text)
	}
	if !strings.Contains(text, "不在") {
		t.Errorf("那句话没说「不在原来的位置上了」：%q", text)
	}
}

// TestOpeningAFilePlaysItAndKeepsTheQueue 钉住主验收线。
//
// 三样一起断：
//
//   - **它真的放了**（队列的当前项是它、状态里有歌）；
//   - **用户已经排好的队列没被清掉**（`DESIGN.md` §十第 4 条：清空队列
//     只有用户明确点了「放这一批 / 这一段」才有）；
//   - **界面上说了话**（不静默）。
func TestOpeningAFilePlaysItAndKeepsTheQueue(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	// 用户先排了两首（第一首在放）。
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	before := a.Queue().Len()
	if before < 2 {
		t.Fatalf("起点不对：队列里只有 %d 首", before)
	}

	path := openFileFixture(t, "双击进来的.flac")
	s.handleOpenFile(path)

	// 放起来了。
	cur, ok := a.Queue().Current()
	if !ok {
		t.Fatal("队列里没有当前项")
	}
	if cur.Path != path {
		t.Errorf("当前项是 %q，want %q", cur.Path, path)
	}
	if st := a.Snapshot(); !st.HasTrack {
		t.Error("状态里没有歌：这一首没被放起来")
	}
	// ⚠️ **队列没被清空**：原来那两首还在，新来的排在队尾。
	if got := a.Queue().Len(); got != before+1 {
		t.Errorf("队列里是 %d 首，want %d（原来那两首必须留着）", got, before+1)
	}
	// 界面上说了话。
	if !hasToast(s, "正在放") {
		t.Errorf("没有说「正在放」，提示有 %q", dropToastTexts(s))
	}
}

// TestOpeningAMissingFileSaysSoAndKeepsPlaying 钉住「文件不在了」那一条。
//
// ⚠️ **两样都要成立**：说了话，**而且原来放的那首还在放**。
//
// 反过来的话（说了话但把歌停了）更坏：用户双击了一个失效的快捷方式（或者
// 文件在处理的时候被删了），结果**正在听的歌停了**——那和「应用崩了」是同一
// 种体验，而用户只是双击了一下别的东西。
func TestOpeningAMissingFileSaysSoAndKeepsPlaying(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	before := a.Queue().Len()

	s.handleOpenFile(filepath.Join(t.TempDir(), "没了.flac"))

	if !hasToast(s, "已经不在原来的位置上了") {
		t.Errorf("没有说「那个文件已经不在原来的位置上了」，提示有 %q", dropToastTexts(s))
	}
	if st := a.Snapshot(); !st.HasTrack {
		t.Error("原来放的那首被停了：用户双击了一个失效的路径，不该把正在听的歌也搭进去")
	}
	if got := a.Queue().Len(); got != before {
		t.Errorf("队列里是 %d 首，want %d（没收下的文件不该进队列）", got, before)
	}
}

// TestOpeningAnUnsupportedFormatSaysSo 钉住「格式放不了」那一条。
//
// ⚠️ **`.wma` 是这一档的代表**：它在扫描白名单里（列得出来），但我们没有
// ASF 解码器。而系统那一档**不过扩展名过滤**（macOS 的 Apple Event 直接把
// 文件递过来），所以这一档是**真的会发生的**，不是假想。
func TestOpeningAnUnsupportedFormatSaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	bad := filepath.Join(t.TempDir(), "老歌.wma")
	if err := os.WriteFile(bad, []byte("ASF fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.handleOpenFile(bad)

	if !hasToast(s, "放不了") {
		t.Errorf("没有说「这个格式我们放不了」，提示有 %q", dropToastTexts(s))
	}
	if !hasToast(s, ".mp3") {
		t.Errorf("没有说清楚支持哪些格式，提示有 %q", dropToastTexts(s))
	}
	if st := a.Snapshot(); st.HasTrack {
		t.Error("一个放不了的格式被放起来了")
	}
}

// TestOpeningADirectorySaysSo 钉住「交过来的不是一个文件」那一条。
func TestOpeningADirectorySaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	dir := t.TempDir()
	s.handleOpenFile(dir)

	if !hasToast(s, "不是一个文件") {
		t.Errorf("没有说「不是一个文件」，提示有 %q", dropToastTexts(s))
	}
	if st := a.Snapshot(); st.HasTrack {
		t.Error("一个目录被放起来了")
	}
}

// TestOpeningAFileMakesRoomForTheMainWindow 钉住「迷你开着时收到文件」。
//
// ⚠️ **迷你要让开**：迷你开着的时候主窗口是收着的，而这一份要在**主窗口**
// 上放歌（迷你那一扇只画播放器自己）。不让开的话，歌在后台放起来了，而用户
// 眼前还是迷你那一扇——那和「点了没反应」是同一种体验。
func TestOpeningAFileMakesRoomForTheMainWindow(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	if main.mini == nil {
		t.Fatal("起点就不对：迷你窗口没开着")
	}

	path := openFileFixture(t, "双击进来的.flac")
	main.handleOpenFile(path)

	if main.mini != nil {
		t.Error("迷你还开着：这一首要在主窗口上放，而主窗口在迷你期间是收着的")
	}
	cur, ok := a.Queue().Current()
	if !ok || cur.Path != path {
		t.Errorf("队列的当前项是 %v，want %q", cur, path)
	}
}

// TestTheOpenFileHookIsInstalledBeforeRun 钉住「回调在 Run 之前装上了」。
//
// ⚠️ **这是这一票最容易踩的一条**：框架的 `OnOpenFile` 回调装晚了，**启动
// 命令行里的那一次文件就永远收不到**（框架在应用 ready 之后才交付启动参数，
// 而回调是在那之前注册的）。症状是「双击一个文件，应用起来了但什么也没放」，
// 而且**没有任何一句报错**。
//
// # 🔴 这一条要真的走**生产那条安装点**
//
// ⚠️ **上一版这一条是假的**：它**先手工调 `s.installOpenFile()`、再直接调
// `s.handleOpenFile(path)`**——于是**把 `lifecycle.go` 里那一行
// `s.installOpenFile()` 删掉，测试照样 PASS**（第二轮评审实测过，输出在
// `hook-test-mutation.txt`）。而「Run 之前漏装 hook」正是这一票最要紧、
// 也最静默的故障——**测它的那条测试抓不住它**。
//
// 所以这一版：
//
//   - **走 `installLifecycle()`**（生产上装钩子的那一个，在 `lifecycle.go`），
//     于是删掉 `lifecycle.go` 里那一行 → **这一条红**；
//   - **接缝换掉**（`openFileHooks`），于是能断言「注册确实发生了、而且
//     注册的是我们自己的那一半」。
//
// ⚠️ **不真的调框架那两个 API**：框架的 `mygo.App` 是包级单例，而
// `OnOpenFile` 注册上去的回调**没法摘干净**（注册了就在那儿），而
// 无头环境里跑不了真的 `App.Run()`。所以那一步走接缝（`openFileHooks`），
// 而**接缝的默认值就是框架那两个 API**（见 `openfile.go`）——生产上它就是
// 真的注册。
func TestTheOpenFileHookIsInstalledBeforeRun(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	// 接缝换掉：记下注册了什么。
	var registered []func(path string)
	restore := stubOpenFileHooks(t, func(fn func(path string)) func() {
		registered = append(registered, fn)
		return func() {}
	})
	defer restore()

	// 🔴 **走生产那条安装点**：`installLifecycle` 是生产上装钩子的那一个。
	s.installLifecycle()

	if len(registered) == 0 {
		t.Fatal("installLifecycle 走完了，一个 OnOpenFile 回调都没注册——" +
			"「Run 之前装上钩子」这一条没了：双击一个文件，应用起来了而什么也没放")
	}

	// 注册上去的**是我们自己的那一半**：把路径递进去，它真的放起来。
	path := openFileFixture(t, "双击进来的.flac")
	registered[0](path)

	cur, ok := a.Queue().Current()
	if !ok || cur.Path != path {
		t.Fatalf("回调那一半没通：队列的当前项是 %v，want %q", cur, path)
	}
}

// TestTheSecondInstanceHookIsInstalledBeforeRun 钉住「第二个实例把命令行
// 转交过来那一档也装上了」。
//
// ⚠️ **这一档和 `OnOpenFile` 是两件事**：框架在 `acceptSecondInstances` 里
// **先**跑 `OnSecondInstance` 的回调、**再**调 `deliverArgs`（`mygo@v0.6.4`
// `single_instance.go:145`）——而 `deliverArgs` **只交框架收得下的那些**
// （扩展名对、文件在、又是普通文件）。所以**收不下的那些（文件不在了、
// 格式放不了、是个目录）必须由我们这一遍说一声**，而这一遍就装在
// `OnSecondInstance` 上。
//
// ⚠️ **漏装它的症状和漏装 `OnOpenFile` 一样静默**：第二个实例起来、双击了
// 一个放不了的文件、第一个实例**一句话都不说**——用户看到的是「双击了
// 那个文件，应用什么反应也没有」。
func TestTheSecondInstanceHookIsInstalledBeforeRun(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	var registered []func(args []string, workingDir string)
	restore := stubSecondInstanceHook(t, func(fn func(args []string, workingDir string)) func() {
		registered = append(registered, fn)
		return func() {}
	})
	defer restore()

	// 🔴 **走生产那条安装点**。
	s.installLifecycle()

	if len(registered) == 0 {
		t.Fatal("installLifecycle 走完了，一个 OnSecondInstance 回调都没注册——" +
			"第二个实例转交过来的文件收不下的那些，一句话都不会说")
	}

	// 注册上去的**是我们自己的那一半**：递一个放不了的路径进去，它说一句话。
	bad := filepath.Join(t.TempDir(), "老歌.wma")
	if err := os.WriteFile(bad, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	registered[0]([]string{bad}, "")

	if !hasToast(s, "放不了") {
		t.Errorf("第二个实例转交过来的那个放不了的文件，没有被说一句话：提示有 %q",
			dropToastTexts(s))
	}
}

// stubOpenFileHooks 把「注册 OnOpenFile 那一档」换成自己的，测试结束时
// 还原。**只有这一条测试用**（其余的走生产那条路）。
//
// ⚠️ **必须还原**：接缝是**包级**的，而 `mygo.App` 的单例状态也活到整个
// 测试进程结束——不还原的话，后面的测试会注册到一个**已经不存在的**壳上，
// 而症状是「某一条测试莫名其妙地红」。
func stubOpenFileHooks(t *testing.T, fn func(func(path string)) func()) func() {
	t.Helper()
	before := openFileHooks.onOpenFile
	openFileHooks.onOpenFile = fn
	return func() { openFileHooks.onOpenFile = before }
}

// stubSecondInstanceHook 把「注册 OnSecondInstance 那一档」换成自己的，
// 测试结束时还原。理由同 `stubOpenFileHooks`。
func stubSecondInstanceHook(t *testing.T, fn func(func(args []string, workingDir string)) func()) func() {
	t.Helper()
	before := openFileHooks.onSecondInstance
	openFileHooks.onSecondInstance = fn
	return func() { openFileHooks.onSecondInstance = before }
}

// ─────────────────────────────────────────────────────────────────────────────
// 「收不下的要有用户可见的错误」
// ─────────────────────────────────────────────────────────────────────────────

// TestARejectedFileIsVisibleOnScreen 钉住「收不下的那句话**在画面上**」。
//
// 🔴 **这一条是这一票第二轮评审指出来的那个静默故障**：原来拒绝的那一支
// **只写进 `toasts` 的状态、而且不请求重绘**，于是：
//
//   - **当前帧上没有那句话**（界面停在旧帧上）；
//   - **迷你开着的时候更看不到**（迷你那一扇根本不画提示）。
//
// 而「状态里记着、画面上没有」在用户那里就是**「什么都没发生」**——
// 而这一票要求的就是「有用户可见的错误」。
//
// 判据是**画面上的文字**（`tt.HasText`），不是状态（`hasToast`）：状态里
// 有那句话是「记下了」，画面上有没有才是「用户看得见」。
//
// ⚠️ **不另外画一帧**：`handleOpenFile` 自己要的那一帧才是关键——「要一帧」
// 这件事本身就是要断的（`addToast` 原来不调 `invalidate`）。所以这里靠
// `repaint` 那一格（无头 Tester 里是 `tt.Frame()`，见 `shelltest_test.go`）：
// 没有它的话，画面上就还是旧的那一帧。
func TestARejectedFileIsVisibleOnScreen(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	// 画一帧，让界面稳下来（`Tester` 造出来那会儿已经画过，但先有个基线）。
	b.tt.Frame()

	missing := filepath.Join(t.TempDir(), "没了.flac")
	s.handleOpenFile(missing)

	// ⚠️ **不画新的一帧**：要断的就是「它自己会要一帧」这件事。
	if !b.tt.HasText("已经不在原来的位置上了") {
		t.Errorf("那句话没有出现在画面上（文字有 %q）——"+
			"「只投递不唤醒」的话，界面停在旧帧上，用户眼前什么都没变", b.tt.Texts())
	}
}

// TestARejectedFileIsVisibleWithTheMiniPlayerOpen 钉住「迷你开着的时候，
// 那句话在**迷你那一扇**上也看得见」。
//
// ⚠️ **迷你那一扇根本不画提示**（`viewToasts` 只在主窗口那一支调，见
// `shell_view.go:258`），而迷你常常就是用户眼前那一扇。所以这一句另有一份
// **两扇窗共看**的状态（见 `filenotice.go`）——迷你那一扇也画它。
//
// ⚠️ **迷你不让开**：让开的话那句话只落在**被叫回来的**主窗口上，而用户
// 刚才在看的是迷你那一扇——于是「用户眼前那一扇什么也没有」照旧发生，
// 症状和「双击没反应」一样。
func TestARejectedFileIsVisibleWithTheMiniPlayerOpen(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)
	if main.mini == nil {
		t.Fatal("起点就不对：迷你窗口没开着")
	}

	// 迷你那一扇的 Tester（**它自己那一扇**，不是主窗口那一扇）。
	miniTT := miniTester(mini)

	bad := filepath.Join(t.TempDir(), "老歌.wma")
	if err := os.WriteFile(bad, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	main.handleOpenFile(bad)

	// 主窗口那一条 toast 也在（用户看惯的是右下角那张卡片）。
	if !hasToast(main, "放不了") {
		t.Errorf("主窗口没有说「这个格式我们放不了」，提示有 %q", dropToastTexts(main))
	}
	// ⚠️ **而迷你那一扇（用户眼前那一扇）也要有那句话**：迷你**不画 toast**，
	// 所以它画的是**共享的那一份**（见 `filenotice.go`）。
	if !miniTT.HasText("放不了") {
		t.Errorf("迷你那一扇没有说「这个格式我们放不了」（文字有 %q）——"+
			"用户眼前那一扇什么也没有，症状和「双击没反应」一样", miniTT.Texts())
	}
}

// TestTheToastIsPaintedOnTheNextFrame 钉住「提示写进状态之后，画面上
// **自己**会有它」。
//
// ⚠️ **这一条断的是 `addToast` 那一层**：原来 `addToast` 只写状态、播报、
// 起一条收走的 goroutine，而 `invalidate` **只在收走那一刻才调**
// （`toast.go` 的 `expire`）。于是新加的那一条要等到「下一个别的原因碰巧
// 重画界面」时才出现——而按需绘制的界面上「下一个别的原因」可能一直没有。
//
// 症状是「点了没反应」：状态里那句话在（`s.toasts`），画面上没有。
func TestTheToastIsPaintedOnTheNextFrame(t *testing.T) {
	b := newTestShell(t)
	s := b.s

	b.tt.Frame()
	s.addToast("刚发生的一件事", notice.Plain)

	// ⚠️ **不画新的一帧**：要断的就是「它自己会要一帧」这件事。
	waitFrames(t, b.tt, "那句提示出现在画面上", func() bool {
		return b.tt.HasText("刚发生的一件事")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 「命令行里被框架丢掉的那一部分」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFrameworkDropsThosePathsBeforeOnOpenFile 钉住「框架那一层会先把
// 它们丢掉」这件事本身。
//
// ⚠️ **这一条是照抄框架的判据**（`mygo@v0.6.4` `deeplink.go:95-122` 的
// `fileArgs`）：扩展名在关联列表里、**文件在**、**是普通文件**——三条都
// 满足才投给 `OnOpenFile`。而**这三条之外的那些（文件不在了、格式放不了、
// 是个目录）框架一个字都不说**。
//
// 抄它不是为了量框架（量不到——那个函数是包内未导出的），而是为了**把
// 「我们自己那一遍认哪些」钉死**：认多了就是同一首歌进队列两次。
func TestTheFrameworkDropsThosePathsBeforeOnOpenFile(t *testing.T) {
	dir := t.TempDir()

	// 框架会交的：扩展名对、文件在、是普通文件。
	ok := filepath.Join(dir, "一首歌.flac")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !frameworkWouldDeliver(ok) {
		t.Errorf("框架会交 %q，而我们自己那一遍也当成交了——于是同一首歌进队列两次", ok)
	}

	// 框架会丢的三个。
	missing := filepath.Join(dir, "没了.flac")
	if frameworkWouldDeliver(missing) {
		t.Errorf("框架会交一个不存在的文件 %q：判据抄错了", missing)
	}
	sub := filepath.Join(dir, "一个目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if frameworkWouldDeliver(sub) {
		t.Errorf("框架会交一个目录 %q：判据抄错了", sub)
	}
	wma := filepath.Join(dir, "老歌.wma")
	if err := os.WriteFile(wma, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if frameworkWouldDeliver(wma) {
		t.Errorf("框架会交一个 .wma %q：判据抄错了", wma)
	}
}

// TestTheLaunchArgsPickUpWhatTheFrameworkDrops 钉住「我们自己认下的正是
// 框架丢掉的那一部分」。
//
// ⚠️ **两半都要断**：
//
//   - **框架会交的那些不在里面**（认多了 → 队列里两份同一首歌）；
//   - **框架丢掉的那几个在里面**（认少了 → 用户什么都没看到）。
func TestTheLaunchArgsPickUpWhatTheFrameworkDrops(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "一首歌.flac")
	if err := os.WriteFile(ok, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "没了.flac")
	sub := filepath.Join(dir, "一个目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	wma := filepath.Join(dir, "老歌.wma")
	if err := os.WriteFile(wma, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	txt := filepath.Join(dir, "说明.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := launchFileCandidates([]string{ok, missing, sub, wma, txt}, dir)

	if slices.Contains(got, ok) {
		t.Errorf("我们自己认下了 %q——框架也会交给 OnOpenFile，于是那一首进队列两次", ok)
	}
	for _, want := range []string{missing, sub, wma} {
		if !slices.Contains(got, want) {
			t.Errorf("%q 没被认下来：框架那一层已经把它丢了，用户就什么都看不到", want)
		}
	}
	if slices.Contains(got, txt) {
		t.Errorf("我们自己认下了 %q——那不是音频，双击它和应用没关系，"+
			"对它说「格式放不了」是对着一件用户没做的事报错", txt)
	}
}

// TestTheCommandLineRejectionsAreVisible 钉住「命令行里被丢掉的那些，
// 用户看得见」。
//
// ⚠️ **这一条是这一票第二轮评审指出来的第二个静默故障**：Windows / 命令行
// 那一侧，缺失 / 不支持 / 非普通文件的路径**在进入 `OnOpenFile` 之前就被
// 框架静默丢弃**——于是任务要求的那三类错误**在这个入口根本无法显示**。
//
// 所以这一条走**我们自己那一遍**（`handleLaunchArgs`），断的是「用户看得
// 见」——而判据是**画面上的文字**。
func TestTheCommandLineRejectionsAreVisible(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"文件不在了", filepath.Join(dir, "没了.flac"), "已经不在原来的位置上了"},
		{"格式放不了", filepath.Join(dir, "老歌.wma"), "放不了"},
		{"不是普通文件", filepath.Join(dir, "一个目录"), "不是一个文件"},
	}
	for _, c := range cases {
		if c.name == "不是普通文件" {
			if err := os.Mkdir(c.path, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if c.name == "格式放不了" {
			if err := os.WriteFile(c.path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := app.New()
			t.Cleanup(a.Close)
			b := newTestShell(t, onApp(a))
			s := b.s
			b.tt.Frame()

			s.handleLaunchArgs([]string{c.path}, dir)

			// ⚠️ **不另外画一帧**：要断的就是「它自己会要一帧」这件事
			// （`repaint` 那一格，见 `shelltest_test.go`）。
			if !b.tt.HasText(c.want) {
				t.Errorf("那句「%s」没有出现在画面上（文字有 %q）——"+
					"命令行那一侧被框架丢掉的东西，用户就什么都看不到",
					c.want, b.tt.Texts())
			}
		})
	}
}

// TestTheSecondInstanceArgsAreCheckedForTheOnesTheFrameworkDrops 钉住
// 「第二个实例转交过来的命令行里，收不下的那些也会被说一声」。
//
// 🔴 **这一条是这一票第二轮评审指出来的第二个静默故障**：Windows / 命令行
// 那一侧，缺失 / 不支持 / 非普通文件的路径**在进入 `OnOpenFile` 之前就被
// 框架静默丢弃**（`mygo@v0.6.4` `deeplink.go:95-122` 的 `fileArgs`）——于是
// 任务要求的那三类错误**在这个入口根本无法显示**。
//
// ⚠️ **而第二个实例那条路更隐蔽**：框架的 `acceptSecondInstances` 先跑
// `OnSecondInstance` 的回调、**再**调 `deliverArgs`（`single_instance.go:145`）
// ——而 `deliverArgs` **只交框架收得下的那些**。所以**我们这一遍必须在
// `OnSecondInstance` 上**，而且必须在框架那一遍**之前**跑（那正是框架的
// 顺序）。
func TestTheSecondInstanceArgsAreCheckedForTheOnesTheFrameworkDrops(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "没了.flac")
	wma := filepath.Join(dir, "老歌.wma")
	if err := os.WriteFile(wma, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "一个目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s
	b.tt.Frame()

	// **第二个实例把命令行转交过来**（生产上就是这一条路）。
	s.handleLaunchArgs([]string{missing, wma, sub}, dir)

	for _, want := range []string{"已经不在原来的位置上了", "放不了", "不是一个文件"} {
		if !b.tt.HasText(want) {
			t.Errorf("第二个实例转交过来的那个文件没有说「%s」（文字有 %q）——"+
				"框架在 `OnOpenFile` 之前就把它丢了，而用户什么都不知道",
				want, b.tt.Texts())
		}
	}
}

// TestALaunchArgThatTheFrameworkDeliversIsNotHandledTwice 钉住「框架会交的
// 那些，我们不重复处理」。
//
// ⚠️ **重复处理的后果是「队列里两首同一首歌」**：用户双击一个文件，
// 队列里出现两份——而界面上「抽屉按歌去重」，用户看不出出了两份，只觉得
// 「怎么多了一首」。
func TestALaunchArgThatTheFrameworkDeliversIsNotHandledTwice(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	path := openFileFixture(t, "双击进来的.flac")
	s.handleLaunchArgs([]string{path}, "")

	if got := a.Queue().Len(); got != 0 {
		t.Errorf("队列里有 %d 首——这一条框架会交给 OnOpenFile，我们再处理一遍就是两份", got)
	}
}

// TestTheLaunchArgKeepsTheWindowsDriveLetter 钉住「Windows 的盘符**不是
// scheme**」。
//
// 🔴 **这一条断的是这一票在 Windows 上最要紧的那一半**：资源管理器双击
// 一个音频交给本应用的命令行**就是** `C:\歌.flac` 那种写法，而
// `url.Parse` 把它认成 **scheme `c`**（一个字母）、path 是空的
// （`C:\歌.flac` → `scheme="c" opaque="\歌.flac"`；`C:/歌.flac` →
// `scheme="c" path="/歌.flac"`）。
//
// 所以**只按 `u.Scheme != "file"` 判的话，整个 Windows 的文件关联这一档
// 会被自己扔掉**——而那正是这一票要治的那个入口（症状是「双击一个音频，
// 应用起来了而什么也没放」，而且一句报错都没有）。
//
// ⚠️ **判据照抄框架那一段**（`mygo@v0.6.4` `deeplink.go:87` 的
// `urlArgs`）：scheme **只有一个字母**时**当作路径**——Windows 的盘符是
// 一个字母，而真正的 scheme 至少两个（`file`、`biu`）。
func TestTheLaunchArgKeepsTheWindowsDriveLetter(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// Windows 资源管理器交给我们的那两种写法。
		{`C:\歌.flac`, `C:\歌.flac`},
		{`C:/歌.flac`, `C:/歌.flac`},
		// ⚠️ **别的 scheme 仍然不认**：`biu://` 那一档是链接，不是文件。
		{`biu://settings`, ``},
	}
	for _, c := range cases {
		got, ok := launchArgPath(c.raw, "")
		if c.want == "" {
			if ok {
				t.Errorf("launchArgPath(%q) 认成了一个路径（%q）——"+
					"`biu://` 那一档是链接，对它说「文件放不了」是对着"+
					"一件用户没做的事报错", c.raw, got)
			}
			continue
		}
		if !ok {
			t.Errorf("launchArgPath(%q) 不认——而资源管理器交给我们的命令行"+
				"**就是**这种写法，于是整个 Windows 的文件关联这一档"+
				"被自己扔掉了", c.raw)
			continue
		}
		if got != c.want {
			t.Errorf("launchArgPath(%q) = %q，want %q", c.raw, got, c.want)
		}
	}
}
