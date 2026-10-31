package lyrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	// ⚠️ **只比内容那一部分**：那两个时刻是**缓存层自己记的**（落盘那一刻、
	// 以及「刚刚用过」），而调用方给的那一份里它们是零值。
	if got.Lyrics != want.Lyrics || got.TLyrics != want.TLyrics ||
		got.OffsetMS != want.OffsetMS || got.FontSize != want.FontSize ||
		got.Origin != want.Origin {
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

// TestTheUserValuesSurviveTheExpiry 盯「歌词文本过期了，用户调过的值还在」。
//
// 🔴 **这是这一层最要紧的一条**（2026-10-30 的对抗性审查挖出来的 P0）。
//
// 那份缓存里除了歌词文本，还有**用户自己调过的偏移和字号**。而「歌词文本
// 过期了要重新拉一份」是**另一件事**——顺手把用户的数据清零的话，用户看到的
// 现象是「某天回来发现我调过的歌词偏移不见了，字号也变小了」，而且
// **永久丢失**（缓存里只有那一份）。
//
// ⇒ 过期时**只清歌词文本**，用户的那两栏留着。
func TestTheUserValuesSurviveTheExpiry(t *testing.T) {
	now := time.Now()
	clock := now
	c := NewCache(t.TempDir()).WithClock(func() time.Time { return clock })
	// 用户调过的：偏移 +250ms，字号 26。
	if err := c.Save("歌", Entry{
		Lyrics:   "[00:01.00]词",
		OffsetMS: 250,
		FontSize: 26,
	}); err != nil {
		t.Fatal(err)
	}
	// 第 8 天：歌词文本过期了。
	clock = now.Add(8 * 24 * time.Hour)
	e, ok := c.Load("歌")
	if !ok {
		t.Fatal("过期的那一份记录都不在了——用户自己的值就跟着没了")
	}
	if e.Lyrics != "" {
		t.Errorf("过期的那一份还带着歌词（%q）——它该重新拉一份", e.Lyrics)
	}
	if e.OffsetMS != 250 {
		t.Errorf("过期之后用户调过的偏移变成了 %d，want 250——"+
			"用户自己的数据不该跟着歌词文本一起过期", e.OffsetMS)
	}
	if e.FontSize != 26 {
		t.Errorf("过期之后用户调过的字号变成了 %v，want 26——"+
			"用户自己的数据不该跟着歌词文本一起过期", e.FontSize)
	}
}

// TestTheExpiryRollsWithEveryUse 盯「过期是滚动的」。
//
// 🔴 **歌词缓存以前根本没有 TTL**（2026-10-30）：它只增不减，于是用户数据目录
// 里的歌词文件只涨不落。而用户的要求是所有缓存都按「最后一次用到」滚动：
//
//	「过期7天应该会滚动刷新吧，比如第一天用户听的，应该第8天删；
//	  第三天用户又听了，这时候应该是第11天清的」
//
// ⇒ 判据是「**最后一次用到**」，不是「取回来的时刻」。
//
// ⚠️ **用假时钟**：真等 7 天没法测。
//
// ⚠️ **判据要落在「两种实现给出不同答案」的窗口里**（2026-10-30 的第二轮
// 对抗性审查发现的假绿）：这一条第一版挑的窗口，「写下来的时候也 Touch 一次」
// 的实现和「不 Touch」的实现给出**同一个答案**——于是它测不出任何东西。
//
// ⇒ 所以这里**直接量文件的修改时刻**：它被推到了当下，那就是「用过」的
// 证据；而一个不 Touch 的实现推不动它。
func TestTheExpiryRollsWithEveryUse(t *testing.T) {
	now := time.Now()
	clock := now
	dir := t.TempDir()
	c := NewCache(dir).WithClock(func() time.Time { return clock })
	if err := c.Save("歌", Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	// 第 3 天：用户又听了这一首。
	clock = now.Add(3 * 24 * time.Hour)
	e, ok := c.Load("歌")
	if !ok {
		t.Fatal("第 3 天读不到")
	}
	if e.Lyrics == "" {
		t.Fatal("第 3 天就过期了——它不该过期")
	}
	// 🔴 **文件的修改时刻被推到了当下**：那才是「用过」的证据。
	//
	// ⚠️ **这一条是这条用例的要害**：一个**不刷新「用过」**的实现推不动它
	// （文件还停在「存下来」那一刻）。
	info, err := os.Stat(c.Path("歌"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(clock) {
		t.Errorf("「最后用过」没有推到当下（文件修改时刻是 %v，want %v）——"+
			"过期没有跟着使用滚动，用户天天在听的那一首第七天就会消失",
			info.ModTime(), clock)
	}
	// 🔴 **第 9 天**：按「存下来」算早该删了（离存下来 9 天 > 7 天），
	// 但用户第 3 天听过 ⇒ 还该在。
	clock = now.Add(9 * 24 * time.Hour)
	e, _ = c.Load("歌")
	if e.Lyrics == "" {
		t.Error("第 3 天听过，第 9 天就过期了——过期没有跟着使用滚动")
	}
	// 第 17 天（离最后一次用到 8 天）⇒ 该删了。
	clock = now.Add(17 * 24 * time.Hour)
	e, _ = c.Load("歌")
	if e.Lyrics != "" {
		t.Error("离最后一次用到满 7 天了还不过期——用户不听了它就该被清掉")
	}
}
