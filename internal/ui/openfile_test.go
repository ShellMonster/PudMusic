package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
// 这一条量的是「装它不炸、而且回调接的那个壳是对的」：直接调一次我们自己的
// 那一半。⚠️ **不真的走框架那条路**：那要一个真的应用（`mygo.App.Run()`），
// 而无头环境里跑不了（框架的 `App` 是包级的单例，测试里跑一次就把整个测试
// 进程的应用状态搅了）。真机那一半由打包后的冒烟验。
//
// 而「装在 Run 之前」那一半由 `shell_window.go` 的 `Run` 保证——那里
// `s.installLifecycle()` 排在 `mygo.App.Run()` 之前。
func TestTheOpenFileHookIsInstalledBeforeRun(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	b := newTestShell(t, onApp(a))
	s := b.s

	// 装一次（生产上由 `installLifecycle` 调，见 `lifecycle.go`）。
	s.installOpenFile()

	path := openFileFixture(t, "双击进来的.flac")
	s.handleOpenFile(path)
	cur, ok := a.Queue().Current()
	if !ok || cur.Path != path {
		t.Fatalf("回调那一半没通：队列的当前项是 %v，want %q", cur, path)
	}
}
