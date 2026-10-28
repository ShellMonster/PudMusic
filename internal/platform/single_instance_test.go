package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/testproc"
)

// TestRouteFromURL 验 deep link 到路由的那一步。
//
// 这是「第二个实例唤起已有实例并跳转路由」的后半段：前半段（锁、转交命令行、
// 投递 OnOpenURL）全在框架里，这里只管「`biu://user/123` 到底该翻成哪一条」。
func TestRouteFromURL(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		// 本应用的 scheme
		{"biu://settings", "/settings", true},
		{"biu://user/123", "/user/123", true},
		{"biu:///local-music", "/local-music", true},
		{"biu://", "/", true},
		{"BIU://Settings", "/Settings", true}, // scheme 大小写不敏感，路径不是
		{"biu://settings?from=tray", "/settings", true},

		// 不是本应用的：同一进程里可能还处理着别的东西交给 OnOpenURL 的 URL，
		// 不认得就不能当成自己的路由。
		{"https://example.com/settings", "", false},
		{"biugo://settings", "", false},
		{"/settings", "", false},
		{"://", "", false},
	}
	for _, c := range cases {
		got, ok := RouteFromURL(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("RouteFromURL(%q) = %q, %v；want %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// TestURLSchemeIsDeclaredInMygoJSON 钉住一件只在打包之后才暴露的事。
//
// macOS 上 `biu://` 要靠 bundle 的 Info.plist 声明了才会被系统转交（框架的
// RegisterURLScheme 在 bundle 没声明时直接报错，`mygo/internal/darwin/deeplink.go`）。
// 那份 Info.plist 是 `mygo build` 从 mygo.json 的 urlSchemes 生成的——字段漏了，
// 代码这边一切正常，`go run` 也正常，只有装了包之后「点链接唤不起应用」。
var mygoConfig = struct {
	URLSchemes []string `json:"urlSchemes"`
}{}

func TestURLSchemeIsDeclaredInMygoJSON(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &mygoConfig); err != nil {
		t.Fatal(err)
	}
	for _, s := range mygoConfig.URLSchemes {
		if s == URLScheme {
			return
		}
	}
	t.Errorf("mygo.json 的 urlSchemes 是 %v，没有 %q：macOS 的 bundle 不会声明它，"+
		"点链接唤不起本应用（Linux 那边图标也不对）", mygoConfig.URLSchemes, URLScheme)
}

// lockAnswerEnv 是子进程把「锁拿到了没有」写下来的文件。
// 设了它，这一轮就是子进程（见 TestLockHelper）。
const lockAnswerEnv = "BIU_TEST_LOCK_ANSWER"

// lockChildTimeout 是子进程最多能花多久。给得很短：框架那把锁自己的超时是
// 拨号 1 秒 + 最多 5 次退避重试（`mygo/single_instance.go`），正常情况几十
// 毫秒就完了。这个上限只是防挂死，不是给人等的。
const lockChildTimeout = 15 * time.Second

// lockStayEnv 设成 "1" 时，子进程写完答案**不退出**。
// 用来当那个「活下来的第一个实例」，后面还要拿它验还能不能接收转交。
const lockStayEnv = "BIU_TEST_LOCK_STAY"

// lockProbeEnv 设成 "1" 时，子进程**只**调 lockExclusive，不碰框架那把 unix
// socket 锁，回答「拿到了没有」。
//
// 为什么单开这个模式：另外两条并发测试靠的是框架的 unix socket，而那条路在
// Windows 上能不能走要看系统（AF_UNIX，Win10 1703+ 才有）。而 `lockExclusive`
// 本身在三个平台上都要验——Windows 那份是一个独立的实例（LockFileEx），上一轮
// review 就抓到过它只在 darwin 上跑过、逻辑错了没人发现。这个模式不依赖任何
// socket，所以**每个平台都会跑**。
const lockProbeEnv = "BIU_TEST_LOCK_PROBE"

// TestLockHelper 只是给下面三条测试当子进程用的，回答一个问题：锁拿到了没有。
func TestLockHelper(t *testing.T) {
	path := os.Getenv(lockAnswerEnv)
	if path == "" {
		t.Skip("只有被下面几条测试当子进程拉起来时才有事做")
	}
	got := "second"
	if os.Getenv(lockProbeEnv) == "1" {
		// 只验内核那一把锁，不碰框架的 socket。
		got = "no"
		if lockExclusive() {
			got = "yes"
		}
	} else if Lock() {
		got = "first"
	}
	if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
		os.Exit(1)
	}
	if os.Getenv(lockStayEnv) == "1" {
		// 睡而不是 select{}：select{} 在所有 goroutine 都闲着时会被运行时
		// 判成死锁直接把进程干掉，这里要的是一个安静活着的进程。
		time.Sleep(5 * time.Minute)
	}
}

// isolateLockDir 把「应用数据目录」指到临时目录，别让测试在用户真的
// ~/Library/Application Support 下面建锁文件。
// isolateLockDir 把这个测试和它的子进程关进一个自己的家目录：应用的设置、
// 我们那把内核锁、以及框架那把 socket 锁，全都落在这个目录下面。
//
// ⚠️ 这个目录必须**短**。框架的 socket 路径是 `filepath.Join(dir, 文件名)`，
// unix socket 路径上限约 100 字节，超了它就退到 `/tmp`——于是所有测试、所有
// 跑次又撞回同一个 socket 上（`t.TempDir()` 在 macOS 上是
// `/var/folders/…/T/测试名…/001`，两下就超）。退回去之后的症状很阴险：上一条
// 测试的子进程刚被 Wait 回收，socket 文件还在（框架是在 App.OnQuit 里删的，
// 测试进程走不到那儿），下一个测试的子进程 Dial 上去拿到 false，于是报出来的是
// 「两个进程都没拿到锁」这种跟真实原因八竿子打不着的错，时好时坏。
func isolateLockDir(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "biulock")
	if err != nil {
		// 退一步用 t.TempDir()：隔离差一点（socket 可能共用），也比让测试
		// 直接失败好。真走到这一步说明 /tmp 不可写，那本身就该有人看见。
		dir = t.TempDir()
		t.Logf("用不了 /tmp，退回 t.TempDir()（socket 路径可能不再互相隔离）：%v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	t.Setenv("HOME", dir)            // darwin：应用数据目录
	t.Setenv("XDG_CONFIG_HOME", dir) // linux：同上
	t.Setenv("APPDATA", dir)         // windows：同上
	t.Setenv("TMPDIR", dir)          // os.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir) // 框架的 socket 优先落这儿
}

// releaseTestLock 放掉本进程手上那把内核锁。
//
// flock 记在文件描述符上，Close 就放掉。同一进程里不放开的话，后面那条并发
// 测试会变成「两个子进程都抢不到」的空过——那恰好是它最不该有的失败方式。
func releaseTestLock() {
	if lockFile != nil {
		_ = lockFile.Close()
		lockFile = nil
	}
}

// selfBinary 是本测试二进制**自己**的路径——子进程跑的就是它。
//
// ⚠️ **用 `internal/testproc` 那份副本**，不用 `os.Args[0]`：后者指向 `go test`
// 的工作目录里那一份，而**那一份的寿命不是我们的**（别的 `go` 进程会把那些
// 工作目录清掉——本机实测 2026-10-24：子进程起不来，红在
// `fork/exec /tmp/go-build…: no such file or directory`）。
//
// 备不出副本时退回 `os.Args[0]`（从前那份做法）：那份起不来就是起不来，
// 而这条用例要量的东西与副本无关。
func selfBinary() string {
	if p, err := testproc.Self(); err == nil {
		return p
	}
	return os.Args[0]
}

// helperCmd 造一个「去抢锁、把答案写到 file」的子进程。
//
// ⚠️ 输出接缓冲区而不是用 CombinedOutput：那是拿 os.Pipe 再开 goroutine 抄的，
// 子进程一旦把管道握在手里不放，Wait 会一直等下去——外面那个 ctx 只管杀进程、
// 管不了管道。WaitDelay 再兜一道底。这两条测试挂死的代价，比它们验的那件事
// 本身还大（整个 `go test ./...` 跟着挂）。
func helperCmd(file string, sink *bytes.Buffer) *exec.Cmd {
	return helperCmdStay(file, sink, false)
}

// helperCmdStay 造子进程；stay=true 时它写完答案不退出（当那个「活着的第一个
// 实例」用）。
func helperCmdStay(file string, sink *bytes.Buffer, stay bool) *exec.Cmd {
	return helperCmdMode(file, sink, stay, false)
}

// helperProbeCmd 造一个「只调 lockExclusive」的子进程。
func helperProbeCmd(file string, sink *bytes.Buffer, stay bool) *exec.Cmd {
	return helperCmdMode(file, sink, stay, true)
}

func helperCmdMode(file string, sink *bytes.Buffer, stay, probe bool) *exec.Cmd {
	cmd := exec.Command(selfBinary(), "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), lockAnswerEnv+"="+file)
	if stay {
		cmd.Env = append(cmd.Env, lockStayEnv+"=1")
	}
	if probe {
		cmd.Env = append(cmd.Env, lockProbeEnv+"=1")
	}
	cmd.Stdout, cmd.Stderr = sink, sink
	cmd.WaitDelay = lockChildTimeout
	return cmd
}

// waitAnswer 等子进程把答案写出来。
//
// 必须等，不能假定 Start 之后答案就在：两个进程在抢内核锁，那个抢到的还要再去
// 建 socket 才会写 ready，几十毫秒很正常。
func waitAnswer(t *testing.T, file string) string {
	t.Helper()
	deadline := time.Now().Add(lockChildTimeout)
	for {
		if b, err := os.ReadFile(file); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return strings.TrimSpace(string(b))
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，子进程一直没写答案（%s）", lockChildTimeout, file)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// skipIfNoSpawn 在「环境根本不让起进程」时跳过而不是失败——那种沙箱里这条
// 确实验不了，硬报失败只会让人以为代码坏了。
func skipIfNoSpawn(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, exec.ErrNotFound) {
		t.Skipf("这个环境里起不了子进程，这条验不了：%v", err)
	}
}

func readAnswer(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("子进程没写答案：%v", err)
	}
	return strings.TrimSpace(string(b))
}

// TestLockRejectsASecondProcess 走一次真的两个进程（先后启动）。
//
// 单实例没法在同一个进程里测两遍：框架的锁记的是「这个进程拿到了锁」，
// 第二次调用直接返回 true（`mygo/single_instance.go` 的 `singleInstance.locked`
// 那一支）。真正要验的是**另一个进程**启动时拿不到锁——所以这里把测试二进制
// 自己再跑一遍。
//
// 跑完会在临时目录里留一个 `mygo-<uid>-<hash>.sock`：框架是在 App.OnQuit 里
// 释放锁的，测试进程不会走到那儿。不清也没关系——下次启动时框架连不上就会把
// 它当成崩溃留下的陈旧 socket 删掉（`mygo/single_instance.go` 里那一支）。
func TestLockRejectsASecondProcess(t *testing.T) {
	requireUnixSocket(t)
	isolateLockDir(t)

	if !Lock() {
		t.Fatal("第一个进程没拿到锁，后面的断言都没意义")
	}
	t.Cleanup(releaseTestLock)

	answer := filepath.Join(t.TempDir(), "answer")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), lockChildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, selfBinary(), "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), lockAnswerEnv+"="+answer)
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = lockChildTimeout

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("子进程超时了（%v）：%s", ctx.Err(), out.String())
		}
		skipIfNoSpawn(t, err)
		t.Fatalf("子进程没跑完：%v\n%s", err, out.String())
	}
	if got := readAnswer(t, answer); got != "second" {
		t.Fatalf("第二个实例也拿到了锁（答 %q），want second", got)
	}
}

// TestLockRacesTwoFreshProcesses 钉住「两个实例真正同时启动」那一条。
//
// 框架那把锁在这种情况下会对**两个进程都返回 true**：谁都还没建好 socket，两个
// Dial 都失败；两个都 `os.Remove(path)`（后到的把先到的刚建出来的 socket 文件
// 删了）；两个 `net.Listen(path)` 都成功（路径空出来了，inode 不同）；两个都认为
// 自己拿到了。症状是用户把图标连点两下就起来两个应用，各放各的歌、各走各的
// 进度条——本 issue 那条验收就是防它。
//
// `Lock()` 上面压的那把内核锁治的就是这个：同一瞬间来的进程里，内核只认一个。
//
// 跑 10 轮不是凑数：修复前实测 20 轮里 17 轮两边都拿到锁，只跑一轮的话有
// 三成概率把坏的东西放过。
func TestLockRacesTwoFreshProcesses(t *testing.T) {
	requireUnixSocket(t)
	isolateLockDir(t)
	// 本进程不许占着锁：占着的话两个子进程都抢不到，这条就变成一个空过的绿。
	releaseTestLock()

	dir := t.TempDir()
	const rounds = 10
	bothFirst := 0
	for r := 1; r <= rounds; r++ {
		// ⚠️ **答案文件必须一轮一个**（这里曾经十轮共用 pa/pb 两个文件）。
		//
		// `waitAnswer` 是「文件里有东西就返回」，它分不清那个东西是**这一轮**
		// 写的还是上一轮留下的。上一轮那个子进程写完就退出了，文件还在；这一轮
		// 的两个进程还没写完，`waitAnswer` 已经把上一轮的 "first" 当成本轮的
		// 答案读了出来——于是「两边都 first」被凭空造出来。
		//
		// 症状是「前几轮绿、后几轮连着红」：机器一忙，子进程从 Start 到写完
		// 要几十上百毫秒，而 `waitAnswer` 第一个 5 ms 就去读了。实测全量并行
		// 跑的时候第 6~10 轮连着红，隔离跑偶尔红——**测的其实不是锁**。
		//
		// （main 上是同一个诊断、另一种解法：每轮开始前把上一轮的答案删掉。
		//  两种等价，这里留的是「路径一轮一个」——它不依赖删除成功。）
		pa := filepath.Join(dir, fmt.Sprintf("pa-%d", r))
		pb := filepath.Join(dir, fmt.Sprintf("pb-%d", r))
		var oa, ob bytes.Buffer
		// **两个都必须 stay**：抢到锁的那个如果写完答案就退出，内核立刻把锁还回去，
		// 没抢到的那个下一轮就合法地拿到锁、也报 first——那时候它确实是「第一个
		// 实例」，因为前一个已经死了。这不是 bug，是这个测法自己站不住。
		// 两个都活着，内核锁才在整轮里始终只有一个主人，「两边都 first」才是
		// 真的出了事。
		ca, cb := helperCmdStay(pa, &oa, true), helperCmdStay(pb, &ob, true)

		// 两个都要 Start 之后才等答案：一个等完了再起第二个，那就不是「同时」。
		if err := ca.Start(); err != nil {
			skipIfNoSpawn(t, err)
			t.Fatalf("起不来子进程：%v", err)
		}
		if err := cb.Start(); err != nil {
			_ = ca.Process.Kill()
			_, _ = ca.Process.Wait()
			skipIfNoSpawn(t, err)
			t.Fatalf("起不来子进程：%v", err)
		}

		a, b := waitAnswer(t, pa), waitAnswer(t, pb)
		for _, c := range []*exec.Cmd{ca, cb} {
			_ = c.Process.Kill()
			_, _ = c.Process.Wait()
		}

		if a == "first" && b == "first" {
			bothFirst++
			t.Errorf("第 %d 轮：两个进程同时启动，两个都拿到了锁 —— 会出现两个应用", r)
		}
	}
	if bothFirst == 0 {
		t.Logf("%d 轮同时启动，每轮最多只有一个拿到锁 ✓", rounds)
	}
}

// TestLockSurvivorKeepsForwarding 钉住「同时启动之后，活下来的那个还能不能收
// biu://」。
//
// 这条比「最多一个拿到锁」要紧一格。框架那把锁在同时启动时会先 `os.Remove`
// 掉对方刚建出来的 socket 文件——于是两个进程虽然只有一个活下来，**活下来的那个
// 的 socket 已经被删了**，之后每一个新实例都连不上它，路由再也递不进来。
// 症状是：用户连点两下之后 deep link 就再也唤不醒这个应用，而界面上看不出任何
// 异常（第二轮 review 实测 40/40 轮都这样）。
//
// 三步：两个同时起 → 断言正好一个 first → **让 first 那个继续活着** → 再起一个
// 第三个，它必须拿到 second（也就是成功转交）。第三步才是关键：前两步过了并不
// 代表 socket 还在。
func TestLockSurvivorKeepsForwarding(t *testing.T) {
	requireUnixSocket(t)
	isolateLockDir(t)
	releaseTestLock()

	dir := t.TempDir()

	// 跑三轮：踩中那个竞态要靠两个进程真正撞在同一瞬间，跑一轮有相当概率撞不上。
	// 这条测试是概率性的，不是结构性的断言——真正让它 100% 成立的是 Lock() 里
	// 「没拿到内核锁的先等 ready 再碰框架那把锁」那一步（见 single_instance.go）。
	// 这里能做的是：一旦那个顺序被改回去（比如有人图省事把两层调换，或者去掉
	// ready 那一等），三轮里总会有一轮抓到 socket 被删掉。
	for round := 1; round <= 3; round++ {
		t.Run(fmt.Sprintf("第%d轮", round), func(t *testing.T) {
			survivorRound(t, dir, fmt.Sprintf("r%d", round))
		})
	}
}

// survivorRound 是上面那一条的一轮：两个同时起，留活下来的那个，再起第三个。
func survivorRound(t *testing.T, dir, tag string) {
	// ⚠️ 两个子进程**各接各的**缓冲区。共用一个的话它们同时往里写，就是一个货
	// 真价实的数据竞争（-race 会报），而报的还跟这条测试要验的东西毫无关系。
	var outA, outB bytes.Buffer

	// 两个都 stay：起先不知道谁是 first，得两个都留着，之后再把输的那个杀掉。
	pa, pb := filepath.Join(dir, tag+"a"), filepath.Join(dir, tag+"b")
	a := helperCmdStay(pa, &outA, true)
	b := helperCmdStay(pb, &outB, true)
	if err := a.Start(); err != nil {
		skipIfNoSpawn(t, err)
		t.Fatalf("起不来子进程：%v", err)
	}
	if err := b.Start(); err != nil {
		_ = a.Process.Kill()
		_, _ = a.Process.Wait()
		skipIfNoSpawn(t, err)
		t.Fatalf("起不来子进程：%v", err)
	}
	defer func() {
		for _, c := range []*exec.Cmd{a, b} {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
			_ = c.Wait()
		}
	}()

	ansA := waitAnswer(t, pa)
	ansB := waitAnswer(t, pb)
	if ansA == ansB {
		t.Fatalf("两个进程都拿到了锁，或者都没拿到（a=%q b=%q）", ansA, ansB)
	}

	// 把输的那个杀掉，只留活下来的那个在跑。
	loser, winner := b, a
	if ansB == "first" {
		loser, winner = a, b
	}
	_ = loser.Process.Kill()
	_, _ = loser.Process.Wait()

	// 第三个实例：它必须连上活下来的那个、把命令行递过去，然后自己退出。
	// 连不上就会拿到 first —— 那就是 socket 已经没了。
	var out bytes.Buffer
	pc := filepath.Join(dir, tag+"c")
	c := helperCmd(pc, &out)
	if err := c.Run(); err != nil {
		skipIfNoSpawn(t, err)
		t.Fatalf("第三个实例没跑完：%v\n%s", err, out.String())
	}
	if got := readAnswer(t, pc); got != "second" {
		t.Fatalf("同时启动之后，活下来的那个已经收不到转交了（第三个实例拿到 %q，want second）——"+
			"它的 socket 被另一个进程的 os.Remove 删掉了，之后 deep link 再也唤不醒它\na: %s\nb: %s\nc: %s",
			got, outA.String(), outB.String(), out.String())
	}
	_ = winner
}

// requireUnixSocket 在这个平台的 unix socket 不可用时跳过。
//
// ⚠️ 别写成「Windows 一律跳过」：上一轮 review 指出过，正因为那样写，Windows 那份
// lockExclusive 的逻辑（LockFileEx，一个独立的实现）在 darwin 上从来没被执行过，
// 于是「建空文件 → 还没写 pid → 另一个进程删掉重建」这个竞态谁也没看见。Windows
// 10 1703+ 有 AF_UNIX，Go 的 net 也支持，所以真正该问的是「这台机器上能不能建
// unix socket」，不是「这是不是 Windows」。
func requireUnixSocket(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "probe.sock"))
	if err != nil {
		t.Skipf("这个平台上 unix socket 不可用，这两条验不了：%v", err)
		return
	}
	_ = ln.Close()
}

// waitAnswerIs 等子进程把答案写成 want 为止。
func waitAnswerIs(t *testing.T, file, want string) {
	t.Helper()
	deadline := time.Now().Add(lockChildTimeout)
	for {
		if b, err := os.ReadFile(file); err == nil {
			if got := strings.TrimSpace(string(b)); got == want {
				return
			} else if got != "" {
				t.Fatalf("等的是 %q，子进程写的是 %q", want, got)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v，子进程一直没写出 %q（%s）", lockChildTimeout, want, file)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestLockExclusiveIsExclusive 验内核那一把锁本身是互斥的，**三个平台都跑**。
//
// 为什么单独立一条：另外两条并发测试靠框架的 unix socket，在 Windows 上能不能走要
// 看系统有没有 AF_UNIX。而 `lockExclusive` 在三个平台上是**三份不同的实现**
// （unix 是 flock，Windows 是 LockFileEx），必须各自验一遍。
//
// 上一轮 review 抓到过 Windows 那份的问题：它建出一个空的锁文件、还没写 pid 的
// 那个窗口里，第二个进程读到 `pid<=0` 就把第一个的锁删了、自己重建——两个都成了
// first。而那两条并发测试在 Windows 上是 `t.Skip` 掉的，这个 bug 谁也没看见。
// 现在这条不依赖 socket，所以它一定会在 Windows 上跑。
func TestLockExclusiveIsExclusive(t *testing.T) {
	isolateLockDir(t)
	// 本进程不许占着锁，否则第一个子进程就抢不到了。
	releaseTestLock()

	dir := t.TempDir()
	pa := filepath.Join(dir, "a")
	var outA bytes.Buffer
	a := helperProbeCmd(pa, &outA, true)
	if err := a.Start(); err != nil {
		skipIfNoSpawn(t, err)
		t.Fatalf("起不来子进程：%v", err)
	}
	defer func() {
		if a.Process != nil {
			_ = a.Process.Kill()
		}
		_ = a.Wait()
	}()

	// 等它真的拿到锁（拿到了才会一直 stay 在那里）。
	waitAnswerIs(t, pa, "yes")

	// 它活着、锁在它手上，第二个必须拿不到。
	pc := filepath.Join(dir, "c")
	var outC bytes.Buffer
	c := helperProbeCmd(pc, &outC, false)
	if err := c.Run(); err != nil {
		skipIfNoSpawn(t, err)
		t.Fatalf("第二个子进程没跑完：%v\n%s", err, outC.String())
	}
	if got := readAnswer(t, pc); got != "no" {
		t.Fatalf("第一个进程还活着、锁在它手上，第二个却拿到了锁（%q）", got)
	}

	// 第一个退出（内核放回锁）之后，第三个应该能拿到——锁必须跟着进程走，
	// 不能留下一个「谁也拿不到」的死状态。
	_ = a.Process.Kill()
	_, _ = a.Process.Wait()

	pd := filepath.Join(dir, "d")
	var outD bytes.Buffer
	d := helperProbeCmd(pd, &outD, false)
	if err := d.Run(); err != nil {
		t.Fatalf("第三个子进程没跑完：%v\n%s", err, outD.String())
	}
	if got := readAnswer(t, pd); got != "yes" {
		t.Fatalf("持锁的进程已经退出，第三个还是拿不到锁（%q）：锁没有跟着进程走", got)
	}
}

// TestTheReadySignalIsNotInsideTheLockedFile 钉住「锁和 ready 信号不能是同一个
// 文件」。
//
// 上一轮 review 抓到的问题：Windows 上 `LockFileEx` 锁住的是一段**字节**，持锁的
// 进程通过**另一个句柄**也读写不进那一段（微软文档写明）。而当时锁的是
// `.instance.lock` 的第 0 个字节，ready 信号也写在同一个文件的第 0 个字节——于是
// `writeLockFile` 的 `os.WriteFile` 失败，错误被 `_ =` 吞掉，ready 永远不可见，
// 每个第二实例、每次 deep link 都白等 `lockReadyWait`（5 秒）才走兜底。
//
// 这条断言在**每个平台**上都成立，而且直接就是那次的教训：谁把两样东西又塞回一个
// 文件里，这条立刻红。
func TestTheReadySignalIsNotInsideTheLockedFile(t *testing.T) {
	if lockPath() == readyPath() {
		t.Fatalf("锁和 ready 信号用的是同一个文件（%s）：Windows 上 LockFileEx 锁住的"+
			"字节段连持锁进程自己都访问不到，ready 永远写不进去", lockPath())
	}
}

// TestTheReadySignalSurvivesTheHeldLock 验拿着锁的时候 ready 写得进去、读得回来。
//
// 这是上一轮那条的具体化：锁已经在手上（`lockExclusive` 返回 true），再
// `markLockReady()`，然后立刻 `lockIsReady()`。在 Windows 上，如果 ready 和锁在
// 同一个文件，这一步的读会拿到 false——而那次它是被吞掉的错误，谁也看不见。
func TestTheReadySignalSurvivesTheHeldLock(t *testing.T) {
	isolateLockDir(t)
	releaseTestLock()

	if !lockExclusive() {
		t.Fatal("拿不到内核锁，这条验不了")
	}
	t.Cleanup(releaseTestLock)

	// 刚拿到锁时：上一个进程留下的 ready 必须已经被抹掉。
	clearLockReady()
	if lockIsReady() {
		t.Error("拿到锁之后上一个进程留下的 ready 还在：后来者会以为第一个实例已就绪，" +
			"去抢一个还不存在的 socket")
	}

	markLockReady()
	if !lockIsReady() {
		pid, _ := readReadyFile()
		t.Fatalf("**持着锁**的时候 ready 信号读不回来（文件里读到 pid=%d）："+
			"锁住的那段字节和 ready 的内容重叠了。Windows 上 LockFileEx 会让持锁进程"+
			"通过第二个句柄也访问不到那一段", pid)
	}
}

// TestALateStarterDoesNotWaitForTheTimeout 验后来的实例**立刻**就绪，不等兜底。
//
// 这一条盯的是「每次第二实例固定等 5 秒」那个现象：`Lock()` 里等 ready 的上限是
// `lockReadyWait`，拿不到就白等那么多。真机上的代价是「唤起这个应用要等五秒」，
// 而且日志里一句错都没有。
//
// 所以这里量的是**时间**：第一个实例已经写好 ready 之后，后来者必须在 1 秒内被放行。
// 1 秒是 `lockPollEvery` 的六十多倍，正常路径一次轮询就该读到。
func TestALateStarterDoesNotWaitForTheTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("这条要两个进程各走一段 Lock（第一个还要真的建框架的 socket），" +
			"在 Windows 上要等真机；Windows 那份 ready 读写由上面两条同进程地兜住")
	}
	isolateLockDir(t)
	releaseTestLock()

	dir := t.TempDir()
	pa := filepath.Join(dir, "pa")
	var outA bytes.Buffer
	a := helperCmdStay(pa, &outA, true)
	if err := a.Start(); err != nil {
		skipIfNoSpawn(t, err)
		t.Fatalf("起不来子进程：%v", err)
	}
	defer func() {
		if a.Process != nil {
			_ = a.Process.Kill()
		}
		_ = a.Wait()
	}()
	waitAnswerIs(t, pa, "first") // 它已经建好 socket、写好 ready 了

	// 现在起第二个：它走「等 ready → 连上去转交 → 退出」那条路。
	pb := filepath.Join(dir, "pb")
	var outB bytes.Buffer
	start := time.Now()
	b := helperCmd(pb, &outB)
	if err := b.Run(); err != nil {
		t.Fatalf("第二个实例没跑完：%v\n%s", err, outB.String())
	}
	elapsed := time.Since(start)

	if got := readAnswer(t, pb); got != "second" {
		t.Fatalf("第二个实例拿到 %q，want second", got)
	}
	// 阈值取**超时的一半**，不是某个固定的小数：再慢也不该慢到兜底那一档。
	// 写死 1 秒会在 -race 下误报（竞态检测器把子进程启动拖慢好几倍——实测
	// 满负载时一次要 1.2 秒），而 5 秒那次是真的。
	if half := lockReadyWait / 2; elapsed > half {
		t.Errorf("第二个实例花了 %v 才转交完（上限 %v，超时兜底是 %v）："+
			"ready 信号多半读不到，后来者只能白等到兜底", elapsed, half, lockReadyWait)
	}
}
