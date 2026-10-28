package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio/extaf"
)

// 打开文件时资源必须换手干净：同一个文件连着打开两次，第一次的解码器和
// 文件句柄都得已经交出去；快速切歌也一样，不留 goroutine、不留句柄。
//
// 怎么看「已经交出去了」：
//
//   - extaf.LiveSources() 是 AudioToolbox 那边的字节源表，**有几个解码器还
//     没关**它就报几。那不是文件描述符，数得出来的是确切个数——漏一个就多 1。
//     之所以要它：FLAC 走纯 Go 解码器，关的是 os.File，只有 /dev/fd 能看见；
//     .m4a 走系统的 AudioToolbox，这条路是 /dev/fd 数不出来的。
//   - /dev/fd 数的是本进程打开的文件描述符，两边合起来才算把「句柄」盖全。
//
// ⚠️ LiveSources 是**整个进程**的计数，不是某个 App 的。所以每条测试开始前
// 都得确认前面留下的都归零，否则基线是错的。
//
// ⚠️ 下面的测试都先关掉 GC，理由见 noGC：MyGo 给每个 Player 挂了
// runtime.AddCleanup，漏掉的播放器会被 GC 兜底收掉。那是最后一道保险，
// 不能当成 app.Open 自己会关旧的证据。
//
// 判据都是确切的等号，不是「跑一百遍都绿」的那种：把 app.Open 里的
// `old.Close()` 删掉，下面两条立刻变红（已用变异测试验过）。

// m4a 素材：用系统自带的 afconvert 从 Ping.aiff 现造一个。
//
// 为什么非要 .m4a：只有它走 AudioToolbox，LiveSources 才数得出来。造不出来
// 就跳过——没有素材时跳过，好过拿 FLAC 假装验过。
func m4a(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("AudioToolbox 那条路只有 macOS 有")
	}
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("这台机器上没有 afconvert")
	}
	const ping = "/System/Library/Sounds/Ping.aiff"
	if _, err := os.Stat(ping); err != nil {
		t.Skipf("系统里没有 Ping.aiff：%v", err)
	}
	path := filepath.Join(t.TempDir(), "tone.m4a")
	if out, err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", "-b", "192000", ping, path).CombinedOutput(); err != nil {
		t.Skipf("造不出 .m4a 素材：%v\n%s", err, out)
	}
	return path
}

// noGC 在这条测试期间关掉自动 GC，结束前恢复。
//
// ⚠️ 为什么必须关：MyGo 的 NewPlayer 给每个播放器挂了 runtime.AddCleanup，
// 播放器只要没人引用了，GC 就会替我们把它关掉。app.Open 忘了关旧歌时，
// 那 39 个漏掉的播放器照样会在几秒内被 GC 收干净——app_test.go 里那条按
// fd 数判的老测试就是这么绿的：它等了 5 秒，GC 在第 2.5 秒把账抹平了。
//
// 那是运气，不是代码。真实运行时 GC 什么时候来没人管得住，双击同一首歌
// 连开几十次就是几十个把文件攥在手里的解码器。关掉 GC 之后，漏就是漏，
// 绿就是绿。
func noGC(t *testing.T) {
	t.Helper()
	old := debug.SetGCPercent(-1)
	t.Cleanup(func() { debug.SetGCPercent(old) })
}

// waitLive 是「解码器该剩几个」。等它到位，MyGo 关解码器是异步的。
func waitLive(t *testing.T, d time.Duration, what string, want int) {
	t.Helper()
	waitFor(t, d, what, func() bool { return extaf.LiveSources() == want })
	if got := extaf.LiveSources(); got != want {
		t.Errorf("解码器还挂着 %d 个，want %d（%s）", got, want, what)
	}
}

// TestReopeningTheSameFileReleasesThePreviousOne 钉住 app.Open 里那个顺序：
// **先把新歌换上，再关旧的**（app.go 里 Open 的结尾）。
//
// 同一个文件连着打开两次最容易暴露问题：第一份和第二份指向同一个文件，
// 关不关第一份，肉眼完全看不出来，可它一直占着文件描述符和 AudioToolbox
// 的 ExtAudioFileRef。真漏了，macOS 上表现成删文件时提示「文件正在使用」，
// 极难排查——所以这里用计数钉死，不靠感觉。
func TestReopeningTheSameFileReleasesThePreviousOne(t *testing.T) {
	path := m4a(t)
	waitLive(t, 3*time.Second, "开测之前没有残留的解码器", 0) // 基线必须是干净的
	noGC(t)

	a := newApp(t)
	if err := a.Open(path); err != nil {
		t.Fatal(err)
	}
	// 第一份确实在解码器表里，否则这条测试验的是空气。
	waitFor(t, 3*time.Second, "第一个播放器开起来", func() bool { return extaf.LiveSources() > 0 })
	if got := extaf.LiveSources(); got != 1 {
		t.Fatalf("刚打开一个文件却挂着 %d 个解码器，want 1", got)
	}
	fdsBefore := openFDs(t)

	// 同一个文件再开一次。
	if err := a.Open(path); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Title; got != "tone" {
		t.Fatalf("重开之后放的是 %q，want tone", got)
	}

	// 关键判据：第一份必须已经交出去了，新歌接管。
	// Open 里先 audio.Open 再 old.Close()，所以这个 1 只可能来自第一份已关。
	waitLive(t, 3*time.Second, "第一次那个解码器被释放", 1)

	// 文件描述符那一半同样要回到原样。
	waitFor(t, 3*time.Second, "第一次那个文件句柄被释放", func() bool { return openFDs(t) <= fdsBefore })
	if got := openFDs(t); got > fdsBefore {
		t.Errorf("重开之后打开的 fd 从 %d 涨到 %d，文件句柄漏了", fdsBefore, got)
	}

	a.Close()
	waitLive(t, 3*time.Second, "关掉应用之后解码器收干净", 0)
}

// TestRapidSwitchingLeavesNoGoroutinesNorHandles 是 SPEC「播放状态机」那条：
// **阻塞式读取必须可取消，否则快速切歌会遗留 goroutine 和缓存文件。**
//
// 连着切 30 次，判据是「切完之后活着的解码器回到 1」——不管切了几次，
// 剩下的都只有正在放的那一个。漏关的话这个数会跟着切歌次数一起涨，
// 一次都涨不了。
func TestRapidSwitchingLeavesNoGoroutinesNorHandles(t *testing.T) {
	path := m4a(t)
	waitLive(t, 3*time.Second, "开测之前没有残留的解码器", 0)
	noGC(t)

	grBefore := runtime.NumGoroutine()
	fdsBefore := openFDs(t)
	a := newApp(t)
	// 队列里放两首同名的，切 30 次会绕回来，最后落回队列第一项。
	if err := a.Enqueue(items(path, path)...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	for range 30 {
		a.Next()
	}

	// 切歌是异步收尾的，等它们自己走完。
	waitLive(t, 5*time.Second, "切歌留下的解码器都收了", 1)

	a.Close()
	waitLive(t, 3*time.Second, "关掉之后解码器全部收干净", 0)

	// goroutine：每个播放器一个解码 goroutine，漏 30 次就是多 30 个。
	// 留 1 个的余量给音频子系统自己的线程——那不是我们建的，也不是我们能关的。
	waitFor(t, 5*time.Second, "解码 goroutine 退出", func() bool { return runtime.NumGoroutine() <= grBefore+1 })
	if got := runtime.NumGoroutine(); got > grBefore+1 {
		t.Errorf("切歌加关闭之后 goroutine 从 %d 涨到 %d，解码 goroutine 没退出", grBefore, got)
	}
	if after := openFDs(t); after > fdsBefore {
		t.Errorf("切歌加关闭之后打开的 fd 从 %d 涨到 %d，文件句柄漏了", fdsBefore, after)
	}
}
