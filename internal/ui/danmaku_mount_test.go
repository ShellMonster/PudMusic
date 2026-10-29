package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/danmaku"
)

// TestTheDanmakuCacheNeverLandsInTheWorkingDirectory 钉住「拿不到应用数据目录时缓存
// 落哪儿」。
//
// 🔴 理由和歌词缓存那一条完全一样：`filepath.Join("", "danmaku")` 给的是**相对
// 路径**（不是空串），而缓存那一层只把**空串**当成「不落盘」——于是它在**进程的
// 当前工作目录**里建出 `danmaku/` 并真的写盘。
func TestTheDanmakuCacheNeverLandsInTheWorkingDirectory(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work) // 假装应用是从这个目录启动的

	c := danmaku.NewCache(danmakuCacheDir(""))
	if got := c.Dir(); got != "" {
		t.Fatalf("拿不到数据目录时缓存目录是 %q，want 空串（空串 = 不落盘）", got)
	}
	if err := c.Save("BV1", "1", danmaku.Entry{Items: []bili.Danmaku{{Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	// 当前工作目录里一个文件都不许多出来。
	if _, err := os.Stat(filepath.Join(work, "danmaku")); err == nil {
		t.Error("缓存目录建在了进程的当前工作目录里")
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("当前工作目录里多了 %d 个东西：%v", len(entries), entries)
	}
}

// TestTheDanmakuCacheGoesUnderTheUserDataDir 是上一条的另一半：目录**拿到**的时候，
// 缓存落在它底下（而不是当前工作目录，也不是别的什么地方）。
func TestTheDanmakuCacheGoesUnderTheUserDataDir(t *testing.T) {
	dir := t.TempDir()
	got := danmakuCacheDir(dir)
	want := filepath.Join(dir, "danmaku")
	if got != want {
		t.Fatalf("缓存目录是 %q，want %q", got, want)
	}
	c := danmaku.NewCache(got)
	if err := c.Save("BV1", "1", danmaku.Entry{Items: []bili.Danmaku{{Text: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("BV1", "1"); !ok {
		t.Error("落下去读不回来")
	}
}

// TestMountDanmakuIsWiredBeforeTheWindow 弹幕缓存挂在开窗口之前。
//
// `danmaku.NewCache` 要 `MkdirAll`，那是阻塞 IO；放到开窗口那条路上就是每启动
// 一次窗口白等一次。这一条钉住的是 `main.go` 里那一行还在——它不在编译能管到的
// 地方（`ui.Run` 之前那一段是手写的），而少了它弹幕每次都要重新拉一遍。
func TestMountDanmakuIsWiredBeforeTheWindow(t *testing.T) {
	mainSrc, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrapSrc, err := os.ReadFile(filepath.Join("..", "bootstrap", "bootstrap.go"))
	if err != nil {
		t.Fatal(err)
	}
	mainText := string(mainSrc)
	bootstrapText := string(bootstrapSrc)
	mount := strings.Index(bootstrapText, "MountDanmaku:         ui.MountDanmaku")
	if mount < 0 {
		t.Fatalf("bootstrap.go 里没有 ui.MountDanmaku 的默认装配")
	}
	assemble := strings.Index(mainText, "bootstrap.Assemble(bootstrap.DefaultDependencies())")
	if assemble < 0 {
		t.Fatalf("main.go 里没有通过 bootstrap.Assemble 启动")
	}
	if strings.Index(bootstrapText, "d.MountDanmaku(a)") < 0 {
		t.Fatalf("bootstrap.go 里没有在 Run 前执行弹幕挂载")
	}
	mountCall := strings.Index(bootstrapText, "d.MountDanmaku(a)")
	runCall := strings.Index(bootstrapText, "d.Run(a, sess)")
	if runCall < 0 {
		t.Fatalf("bootstrap.go 里没有执行 Run")
	}
	if mountCall > runCall {
		t.Errorf("弹幕挂载排在 Run 之后（第 %d 字节 vs 第 %d 字节）", mountCall, runCall)
	}
}
