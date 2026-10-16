package platform

import (
	"context"
	"encoding/json"
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
// 设了它，这一轮就是子进程，不再是测试。
const lockAnswerEnv = "BIU_TEST_LOCK_ANSWER"

// TestLockRejectsASecondProcess 走一次真的两个进程。
//
// 单实例没法在同一个进程里测两遍：框架的锁记的是「这个进程拿到了锁」，
// 第二次调用直接返回 true（`mygo/single_instance.go` 的 `singleInstance.locked`
// 那一支）。真正要验的是**另一个进程**启动时拿不到锁、并且它的命令行被转交给
// 第一个——所以这里把测试二进制自己再跑一遍。
//
// 跑完会在临时目录里留一个 `mygo-<uid>-<hash>.sock`：框架是在 App.OnQuit 里
// 释放锁的，测试进程不会走到那儿。不清也没关系——下次启动时框架连不上就会把
// 它当成崩溃留下的陈旧 socket 删掉（`mygo/single_instance.go` 里那一支）。
func TestLockRejectsASecondProcess(t *testing.T) {
	// 子进程：只回答一个问题，然后把答案写进文件。
	if path := os.Getenv(lockAnswerEnv); path != "" {
		got := "second"
		if Lock() {
			got = "first"
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			os.Exit(1)
		}
		return
	}
	if runtime.GOOS == "windows" {
		// unix socket 在 Windows 上要靠系统开着 AF_UNIX，这条在 macOS 上验。
		t.Skip("Windows 上 unix socket 的可用性随版本变化")
	}

	if !Lock() {
		t.Fatal("第一个进程没拿到锁，后面的断言都没意义")
	}

	answer := filepath.Join(t.TempDir(), "answer")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockRejectsASecondProcess$")
	cmd.Env = append(os.Environ(), lockAnswerEnv+"="+answer)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子进程跑不起来：%v\n%s", err, out)
	}
	got, err := os.ReadFile(answer)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "second" {
		t.Fatalf("第二个实例也拿到了锁（答 %q），want second", got)
	}
}
