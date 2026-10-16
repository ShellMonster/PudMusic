package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// TestLockHelper 只是给下面两条测试当子进程用的，回答一个问题：锁拿到了没有。
func TestLockHelper(t *testing.T) {
	path := os.Getenv(lockAnswerEnv)
	if path == "" {
		t.Skip("只有被下面两条测试当子进程拉起来时才有事做")
	}
	got := "second"
	if Lock() {
		got = "first"
	}
	if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
		os.Exit(1)
	}
}

// isolateLockDir 把「应用数据目录」指到临时目录，别让测试在用户真的
// ~/Library/Application Support 下面建锁文件。
func isolateLockDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // darwin
	t.Setenv("XDG_CONFIG_HOME", dir) // linux
	t.Setenv("APPDATA", dir)         // windows
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

// helperCmd 造一个「去抢锁、把答案写到 file」的子进程。
//
// ⚠️ 输出接缓冲区而不是用 CombinedOutput：那是拿 os.Pipe 再开 goroutine 抄的，
// 子进程一旦把管道握在手里不放，Wait 会一直等下去——外面那个 ctx 只管杀进程、
// 管不了管道。WaitDelay 再兜一道底。这两条测试挂死的代价，比它们验的那件事
// 本身还大（整个 `go test ./...` 跟着挂）。
func helperCmd(file string, sink *bytes.Buffer) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), lockAnswerEnv+"="+file)
	cmd.Stdout, cmd.Stderr = sink, sink
	cmd.WaitDelay = lockChildTimeout
	return cmd
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
	if runtime.GOOS == "windows" {
		// unix socket 在 Windows 上要靠系统开着 AF_UNIX，这条在 macOS 上验。
		t.Skip("Windows 上 unix socket 的可用性随版本变化")
	}
	isolateLockDir(t)

	if !Lock() {
		t.Fatal("第一个进程没拿到锁，后面的断言都没意义")
	}
	t.Cleanup(releaseTestLock)

	answer := filepath.Join(t.TempDir(), "answer")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), lockChildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockHelper$")
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 unix socket 的可用性随版本变化，这条在 macOS 上验")
	}
	isolateLockDir(t)
	// 本进程不许占着锁：占着的话两个子进程都抢不到，这条就变成一个空过的绿。
	releaseTestLock()

	dir := t.TempDir()
	const rounds = 10
	bothFirst := 0
	for r := 1; r <= rounds; r++ {
		pa, pb := filepath.Join(dir, "pa"), filepath.Join(dir, "pb")
		var oa, ob bytes.Buffer
		ca, cb := helperCmd(pa, &oa), helperCmd(pb, &ob)

		// 两个都要 Start 之后才 Wait：一个等完了再起第二个，那就不是「同时」。
		if err := ca.Start(); err != nil {
			skipIfNoSpawn(t, err)
			t.Fatalf("起不来子进程：%v", err)
		}
		if err := cb.Start(); err != nil {
			_ = ca.Wait()
			skipIfNoSpawn(t, err)
			t.Fatalf("起不来子进程：%v", err)
		}
		if err := ca.Wait(); err != nil {
			t.Fatalf("第 %d 轮子进程 a 没跑完：%v\n%s", r, err, oa.String())
		}
		if err := cb.Wait(); err != nil {
			t.Fatalf("第 %d 轮子进程 b 没跑完：%v\n%s", r, err, ob.String())
		}

		a, b := readAnswer(t, pa), readAnswer(t, pb)
		if a == "first" && b == "first" {
			bothFirst++
			t.Errorf("第 %d 轮：两个进程同时启动，两个都拿到了锁 —— 会出现两个应用", r)
		}
	}
	if bothFirst == 0 {
		t.Logf("%d 轮同时启动，每轮最多只有一个拿到锁 ✓", rounds)
	}
}
