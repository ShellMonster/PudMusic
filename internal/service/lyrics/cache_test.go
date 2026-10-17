package lyrics

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCacheRoundTrip 是缓存最基本的那个约定：写进去什么，读出来还是什么。
func TestCacheRoundTrip(t *testing.T) {
	c := NewCache(t.TempDir())
	want := Entry{
		Lyrics:   "[00:01.00]Hello",
		TLyrics:  "[00:01.00]你好",
		OffsetMS: -250,
		FontSize: 26,
	}
	if err := c.Save("bili-BV1xx411c7mD-112233", want); err != nil {
		t.Fatal(err)
	}

	got, ok := c.Load("bili-BV1xx411c7mD-112233")
	if !ok {
		t.Fatal("刚写进去就读不到")
	}
	if got != want {
		t.Errorf("读回来是 %+v，want %+v", got, want)
	}
}

// TestSongsDoNotShareAFile 是「一首歌一个文件」存在的理由：两首同名不同目录的
// 本地歌必须分开，而本地那一条的键是**完整路径**（含分隔符和 Windows 上的盘符
// 冒号）——文件名是键的哈希，不是键本身。
func TestSongsDoNotShareAFile(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	a := "local-/音乐/同名歌.mp3"
	b := `local-C:\音乐\同名歌.mp3`
	if err := c.Save(a, Entry{Lyrics: "[00:01.00]甲"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(b, Entry{Lyrics: "[00:01.00]乙"}); err != nil {
		t.Fatal(err)
	}

	if got, _ := c.Load(a); got.Lyrics != "[00:01.00]甲" {
		t.Errorf("甲被乙盖了：%+v", got)
	}
	if got, _ := c.Load(b); got.Lyrics != "[00:01.00]乙" {
		t.Errorf("乙被甲盖了：%+v", got)
	}

	// 两首歌 = 两个文件，而且文件名里不含任何路径分隔符。
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("want 2 个文件，有 %d 个：%v", len(names), names)
	}
	for _, n := range names {
		if filepath.Dir(n) != dir {
			t.Errorf("文件 %q 不在缓存目录里（键里的分隔符漏进文件名了）", n)
		}
	}
}

// TestAMissingOrBrokenFileIsJustNoLyrics：读不出来一律当成「没有」，绝不报错。
//
// 用户删缓存目录、清磁盘、换机器都会走到这里。报错的后果是应用开不起来，
// 而这件事本来只该让一首歌暂时没有歌词。
func TestAMissingOrBrokenFileIsJustNoLyrics(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	if _, ok := c.Load("没写过"); ok {
		t.Error("没写过的键读出了东西")
	}
	if _, ok := c.Load(""); ok {
		t.Error("空键读出了东西")
	}

	// 写坏了：半份 JSON。断电或进程被杀会留下这种文件。
	if err := c.Save("坏的那条", Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Path("坏的那条"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Load("坏的那条"); ok {
		t.Error("半份 JSON 读成了一首歌的歌词")
	}
}

// TestSaveIsAtomicAndLeavesNoRubbish 盯住「断电之后不能留下半份 JSON」：
// 临时文件走完 rename 之后目录里不能多出任何东西。
func TestSaveIsAtomicAndLeavesNoRubbish(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	for i := range 5 {
		if err := c.Save("同一首", Entry{Lyrics: "[00:01.00]词", OffsetMS: i * 50}); err != nil {
			t.Fatal(err)
		}
	}

	names, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		var got []string
		for _, n := range names {
			got = append(got, filepath.Base(n))
		}
		t.Fatalf("写了 5 次之后目录里应该有 1 个文件，有 %d 个：%v", len(names), got)
	}
	// 写到最后一次的值：rename 真的换掉了，不是每次都写进同一个临时文件。
	if got, _ := c.Load("同一首"); got.OffsetMS != 200 {
		t.Errorf("最后一次写的是 %d，want 200", got.OffsetMS)
	}
}

// TestACacheWithoutADirJustDoesNothing：没挂盘的时候（拿不到应用数据目录）歌词
// 照样看得见，只是调好的偏移关掉就没了。不许因为写不下去就报错。
func TestACacheWithoutADirJustDoesNothing(t *testing.T) {
	c := NewCache("")

	if err := c.Save("k", Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Errorf("没配盘写东西不该是错误：%v", err)
	}
	if _, ok := c.Load("k"); ok {
		t.Error("没配盘却读出了东西")
	}
	if c.Path("k") != "" {
		t.Errorf("没配盘时 Path 应该是空的，出了 %q", c.Path("k"))
	}
}
