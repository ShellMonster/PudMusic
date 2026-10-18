package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// TestTheLyricsCacheNeverLandsInTheWorkingDirectory 钉住「拿不到应用数据目录时缓存
// 落哪儿」。
//
// 🔴 早一版写的是 `lyrics.NewCache(filepath.Join(dir, lyricsDirName))`，而空目录时
// `filepath.Join("", "lyrics")` 给的是**相对路径 `"lyrics"`**，不是空串。缓存那一层
// 只把**空串**当成「不落盘」，于是它在**进程的当前工作目录**里建出 `lyrics/` 并真的
// 写盘——用户数据目录没拿到，却往他碰巧启动应用的那个目录里写了一堆缓存文件。
//
// 那不是「缓存不落盘」，那是**缓存落到了没人预期的地方**，而它下一次启动还在那儿。
func TestTheLyricsCacheNeverLandsInTheWorkingDirectory(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work) // 假装应用是从这个目录启动的

	c := lyrics.NewCache(lyricsCacheDir(""))
	if got := c.Dir(); got != "" {
		t.Fatalf("拿不到数据目录时缓存目录是 %q，want 空串（空串 = 不落盘）", got)
	}
	if err := c.Save("bili-BV1-1", lyrics.Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	// 当前工作目录里一个文件都不许多出来。
	if _, err := os.Stat(filepath.Join(work, "lyrics")); err == nil {
		t.Error("缓存目录建在了进程的当前工作目录里")
	}
	if _, err := os.Stat(work); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("当前工作目录里多了 %d 个东西：%v", len(entries), entries)
	}
}

// TestTheLyricsCacheGoesUnderTheUserDataDir 是上一条的另一半：目录**拿到**的时候，
// 缓存落在它底下（而不是当前工作目录，也不是别的什么地方）。
func TestTheLyricsCacheGoesUnderTheUserDataDir(t *testing.T) {
	dir := t.TempDir()
	got := lyricsCacheDir(dir)
	want := filepath.Join(dir, "lyrics")
	if got != want {
		t.Fatalf("缓存目录是 %q，want %q", got, want)
	}
	c := lyrics.NewCache(got)
	if err := c.Save("bili-BV1-1", lyrics.Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("bili-BV1-1"); !ok {
		t.Error("落下去读不回来")
	}
}
