package platform

import (
	"log"
	"os"
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
	log.Printf("biu: 这一行是给排障看的")
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
	log.Printf("biu: 第一段")
	if _, err := OpenLogFileIn(dir); err != nil {
		t.Fatal(err)
	}
	log.Printf("biu: 第二段")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "第一段") || !strings.Contains(text, "第二段") {
		t.Errorf("第二段把第一段冲掉了：%q", text)
	}
}
