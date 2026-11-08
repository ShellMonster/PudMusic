package platform

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 日志文件这一层（票 #282）。
//
// 应用以前只往 stderr 打日志——从一个 `.app` 里启动的进程，stderr 落在**系统
// 日志**里，用户和维护者都找不到。于是「点了歌不播」这类问题**完全查不了**：
// 应用自己什么都没留下。

// TestTheLogFileActuallyReceivesWhatIsLogged 盯日志**真的落在文件里**。
//
// 这一条不是「调了 `log.SetOutput` 就算数」——它断的是文件里真的有那几行。
// 而这一点以前没人验过：应用里几十处 `log.Printf` 写的都是 stderr。
func TestTheLogFileActuallyReceivesWhatIsLogged(t *testing.T) {
	dir := t.TempDir()
	old := log.Writer()
	t.Cleanup(func() { log.SetOutput(old) })

	path, err := OpenLogFileIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "main.log" {
		t.Errorf("日志文件叫 %q，不是 main.log", filepath.Base(path))
	}
	log.Printf("pudmusic: 这一行是给排障看的")
	// ⚠️ **日志是同步写的**（标准 logger 直接写文件），所以这里不用等。
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "这一行是给排障看的") {
		t.Errorf("日志文件里没有那一行：%q", string(b))
	}
}

// TestTheLogFileIsAppendedTo 盯**追加**，不是覆盖。
//
// 覆盖的话，应用每重启一次就把上一次的日志抹掉——而「上一次的崩溃」正是
// 下次启动要查的东西。
func TestTheLogFileIsAppendedTo(t *testing.T) {
	dir := t.TempDir()
	old := log.Writer()
	t.Cleanup(func() { log.SetOutput(old) })

	path, err := OpenLogFileIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("pudmusic: 第一段")
	if _, err := OpenLogFileIn(dir); err != nil {
		t.Fatal(err)
	}
	log.Printf("pudmusic: 第二段")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "第一段") || !strings.Contains(text, "第二段") {
		t.Errorf("第二段把第一段冲掉了：%q", text)
	}
}

// TestTheLogFileRotatesOnceWhenASessionWritesPastTheLimit 盯运行中写过上限时
// 只换一份：旧内容进 `main.log.1`，最新那行留在 `main.log`，活文件不超过上限。
func TestTheLogFileRotatesOnceWhenASessionWritesPastTheLimit(t *testing.T) {
	dir := t.TempDir()
	old := log.Writer()
	t.Cleanup(func() { log.SetOutput(old) })

	path, err := OpenLogFileIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("pudmusic: 最早那一行")
	filler := strings.Repeat("x", 1000)
	for written := 0; written <= maxLogBytes; written += len(filler) {
		log.Print(filler)
	}
	log.Printf("pudmusic: 最新那一行")

	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("写过上限却没换出 main.log.1：%v", err)
	}
	if !strings.Contains(string(live), "最新那一行") {
		t.Error("最新那一行不在 main.log 里")
	}
	if strings.Contains(string(live), "最早那一行") {
		t.Error("main.log 没换：最早那一行还在活文件里")
	}
	// 换了两次的话，最早那一行会跟着第一份备份一起被删掉。
	if !strings.Contains(string(backup), "最早那一行") {
		t.Error("main.log.1 里没有最早那一行：换了不止一次")
	}
	if len(live) > maxLogBytes {
		t.Errorf("main.log 有 %d 字节，超过上限 %d", len(live), maxLogBytes)
	}
}

// TestAFatalPanicInAGoroutineLandsInTheCrashLog 盯一个没接住的 goroutine panic
// 把堆栈写进 `crash.log`：子进程接上崩溃日志后崩掉，父进程去读那个文件。
func TestAFatalPanicInAGoroutineLandsInTheCrashLog(t *testing.T) {
	if dir := os.Getenv("PUDMUSIC_CRASH_CHILD_DIR"); dir != "" {
		if _, err := OpenCrashLogIn(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		go panic("pudmusic-crash-marker")
		select {}
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAFatalPanicInAGoroutineLandsInTheCrashLog$")
	cmd.Env = append(os.Environ(), "PUDMUSIC_CRASH_CHILD_DIR="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("子进程该以 panic 的退出码 2 结束，实际 %v；输出：%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "crash.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "panic: pudmusic-crash-marker") || !strings.Contains(string(b), "goroutine") {
		t.Errorf("crash.log 里没有那次 panic 的堆栈：%q", b)
	}
}
