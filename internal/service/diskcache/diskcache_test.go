package diskcache

// 公用层的判据钉死。
//
// # 为什么这个包必须有自己的测试
//
// 它是**五个缓存包**共用的那一层——而其中一条判据错了，五个包一起错，
// 而且症状是「用户天天在听的歌第七天消失」这种没法一眼看出根因的东西。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile 造一份「盘上的缓存文件」，并把它的修改时刻设成 mod。
func writeFile(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// TestTheExpiryRollsWithEveryUse 盯「过期是滚动的」。
//
// 🔴 **这是这一层存在的全部理由**（2026-10-30 用户提出来的）：
//
//	「过期7天应该会滚动刷新吧，比如第一天用户听的，应该第8天删；
//	  第三天用户又听了，这时候应该是第11天清的」
//
// ⇒ 判据是「**最后一次用到**」，不是「取回来的时刻」。
//
// ⚠️ **断言要落在「两种判据会给出不同答案」的窗口里**：否则这条用例在
// 旧实现下也会绿（第一版就栽在这儿——它挑的窗口两种判据答案一样）。
func TestTheExpiryRollsWithEveryUse(t *testing.T) {
	day0 := time.Now()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	ttl := 7 * 24 * time.Hour

	// 第 1 天：刚写下来。
	writeFile(t, path, day0)
	if !Fresh(path, day0, ttl) {
		t.Fatal("刚写下来就过期了")
	}

	// 🔴 **第 3 天用户又用了它** ⇒ 「最后用过」推到第 3 天。
	Touch(path, day0.Add(3*24*time.Hour))

	// 🔴 **第 8 天**：按「写下来」算早该删了（离写下来 8 天 > 7 天），
	// 但用户第 3 天用过 ⇒ **还该在**。
	//
	// ⚠️ **这一条断言是「滚动」的全部要害**：旧实现（判据 = 写下来的时刻）
	// 在这里会红。
	if !Fresh(path, day0.Add(8*24*time.Hour), ttl) {
		t.Error("第 3 天用过，第 8 天就过期了——过期没有跟着使用滚动")
	}
	// 第 11 天（离最后一次用到 8 天）⇒ 该删了。
	if Fresh(path, day0.Add(11*24*time.Hour), ttl) {
		t.Error("离最后一次用到满 7 天了还不过期——用户不听了它就该被清掉")
	}
}

// TestTouchDoesNotChangeTheContent 盯「刷新「用过」不碰内容」。
//
// 🔴 **这是「读路径不变成写路径」的关键**：第一版是把这个时刻**记在记录
// 里面**，于是刷新它就得**把整份记录写回去**——那有两个坏处：
//
//  1. 读路径变成写路径（一次 fsync + rename + 目录 fsync）；
//  2. 写回整份记录会把并发写进去的新内容盖成旧内容（丢更新）。
//
// ⇒ 现在只改元数据（`os.Chtimes`），内容一个字节都不动。
func TestTouchDoesNotChangeTheContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	content := []byte(`{"lyrics":"词"}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	Touch(path, time.Now())

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("内容被改了：%q，want %q", got, content)
	}
}

// TestTouchIsNotAWritePath 盯「刷新「用过」不产生新的文件」。
//
// 🔴 **写路径会攒残骸**：临时文件 + rename 那一套每次都建一个临时文件，
// 而它要 fsync、要改目录项。一张封面读 100 次就是 100 次那套动作。
func TestTouchIsNotAWritePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	writeFile(t, path, time.Now())

	for range 20 {
		Touch(path, time.Now())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("刷新了 20 次之后目录里留下了 %d 个文件（want 1）：%v", len(entries), names)
	}
}

// TestNoTTLMeansNeverExpires 盯「ttl <= 0 = 永不过期」。
//
// 歌词缓存以前走的就是这一档：歌词不会变，而「该更新了」对它没有意义。
func TestNoTTLMeansNeverExpires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	// 一百年前写的。
	writeFile(t, path, time.Now().Add(-1000*24*time.Hour))
	if !Fresh(path, time.Now(), 0) {
		t.Error("ttl=0 却算过期了——那应该是「永不过期」")
	}
	// ⚠️ **但文件不在就不算有**：不然「永不过期」会变成「永远有一份不存在的缓存」。
	if Fresh(filepath.Join(dir, "没有这个.json"), time.Now(), 0) {
		t.Error("文件都不在了还说它新鲜")
	}
}

// TestAMissingFileIsNotFresh 盯「文件不在 = 没有缓存」。
func TestAMissingFileIsNotFresh(t *testing.T) {
	dir := t.TempDir()
	if Fresh(filepath.Join(dir, "没有这个.json"), time.Now(), TTL) {
		t.Error("文件都不在还说它新鲜")
	}
	if Fresh("", time.Now(), TTL) {
		t.Error("空路径还说它新鲜")
	}
}

// TestAnOldFileWithoutATimestampStillCounts 盯「老缓存文件不需要迁移」。
//
// ⚠️ **老文件没有「最后用过」那一栏**——而它们的 mtime 就是当初写下去的时刻。
// 所以它们按 mtime 判过期，**和当初的行为一致**（不会一升级就全部作废）。
func TestAnOldFileWithoutATimestampStillCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	day0 := time.Now()
	// 一份老格式的缓存（内容里没有时间戳），写下去的时刻是 3 天前。
	writeFile(t, path, day0.Add(-3*24*time.Hour))
	if !Fresh(path, day0, TTL) {
		t.Error("3 天前写的老缓存被判过期了——老文件不该一升级就作废")
	}
}

// TestWriteAtomicLeavesNoDebris 盯「写完不留残骸」。
//
// 🔴 **临时文件留在目录里会一直攒**：它们占地方，而用户从访达里看那个目录
// 只看到一堆莫名其妙的文件。
func TestWriteAtomicLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := WriteAtomic(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("目录里留下了 %d 个文件（want 1）：%v", len(entries), names)
	}
}

// TestWriteAtomicReplacesTheOldOne 盯「写是替换，不是追加」。
func TestWriteAtomicReplacesTheOldOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := WriteAtomic(path, []byte("第一次")); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("第二次")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "第二次" {
		t.Errorf("读回来是 %q，want 第二次", b)
	}
}

// TestWriteAtomicFailsCleanlyOnABadPath 盯「路径不对时不留残骸」。
func TestWriteAtomicFailsCleanlyOnABadPath(t *testing.T) {
	dir := t.TempDir()
	// 目标在一个不存在的目录里 ⇒ 建临时文件那一步就失败。
	err := WriteAtomic(filepath.Join(dir, "nope", "a.json"), []byte("{}"))
	if err == nil {
		t.Fatal("写到一个不存在的目录里居然成功了")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("失败之后目录里留下了 %d 个文件", len(entries))
	}
}
