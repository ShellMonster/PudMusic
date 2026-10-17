package app

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// lyricsApp 建一个挂好了歌词缓存的空应用。缓存挂在 `t.TempDir()` 上，和真应用
// 一样落盘：这一票要验的就是「关掉再打开还记得」，用内存里的一份就把它绕过去了。
//
// ⚠️ `t.TempDir()` **必须**在 `t.Cleanup(a.Close)` 之前调：cleanup 是后进先出，反过来
// 的话临时目录先被删掉，`a.Close()` 里那次「退出前把欠着的偏移落完」就往一个已经不
// 存在的目录里写，日志里全是它。
func lyricsApp(t *testing.T) (*App, *lyrics.Cache) {
	t.Helper()
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	c := lyrics.NewCache(filepath.Join(dir, "lyrics"))
	a.MountLyrics(c)
	return a, c
}

// waitLyrics 轮询到 cond 成立。缓存是后台读的，只能这样等。
func waitLyrics(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s", d, what)
}

// onlineSong 是测试里用的一个在线条目：在线的歌按 bvid+cid 做缓存键（上游
// `lyrics/index.tsx:90`），所以这两个字段得填。
//
// ⚠️ 叫 `onlineSong` 不叫 `song`：`localmusic_test.go` 里已经有一个叫 `song` 的
// 辅助函数（造一个 .mp3 文件），同一个包里重名会直接编译不过。
func onlineSong(bvid, cid string) Item {
	return Item{Source: "online", Type: "mv", Bvid: bvid, Cid: cid, Title: bvid}
}

// TestSwitchingSongsResetsTheOffsetBeforeRestoringIt 是这一票点名的「切歌时先重置为
// 默认值，再从缓存恢复」。
//
// 断的是**顺序**不是结果：顺序反了的话最后状态可能也对（恢复会把值盖回去），只有中间
// 那一帧露馅——而那一帧正是用户换歌时看见的那一帧。
func TestSwitchingSongsResetsTheOffsetBeforeRestoringIt(t *testing.T) {
	a, c := lyricsApp(t)
	if err := c.Save(lyricKeyOf(onlineSong("BV1", "1")), lyrics.Entry{
		Lyrics: "[00:01.00]甲", OffsetMS: -1500, FontSize: 32,
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(lyricKeyOf(onlineSong("BV2", "2")), lyrics.Entry{
		Lyrics: "[00:01.00]乙", OffsetMS: 2000, FontSize: 14,
	}); err != nil {
		t.Fatal(err)
	}

	a.setLyricTrack(onlineSong("BV1", "1"))
	waitLyrics(t, 2*time.Second, "甲的歌词读回来", func() bool {
		return a.Lyrics().HasLyrics
	})
	if got := a.Lyrics().OffsetMS; got != -1500 {
		t.Fatalf("甲的偏移是 %d，want -1500", got)
	}

	// 换歌。读盘是异步的，所以换完歌这一刻读到的应该是**默认值**，
	// 而不是甲留下的 -1500。
	a.setLyricTrack(onlineSong("BV2", "2"))
	if got := a.Lyrics(); got.OffsetMS != 0 || got.FontSize != DefaultLyricsFontSize {
		t.Errorf("换歌那一刻读到的是 %+v，want 偏移 0、字号 %v——"+
			"带着上一首的值换歌，用户会以为新歌的词对不上", got, DefaultLyricsFontSize)
	}

	// 然后才恢复成这一首自己的值。
	waitLyrics(t, 2*time.Second, "乙的歌词读回来", func() bool {
		return a.Lyrics().OffsetMS == 2000
	})
	ls := a.Lyrics()
	if ls.FontSize != 14 {
		t.Errorf("乙的字号是 %v，want 14", ls.FontSize)
	}
	if ls.Rows[0].Text != "乙" {
		t.Errorf("歌词是 %q，want 乙（读到的是甲的缓存？）", ls.Rows[0].Text)
	}
}

// TestPutLyricsRacesTheDebouncedSave 钉住那条数据竞争：`PutLyrics` 在**锁外**读
// `a.lyricEntry`，而防抖落盘跑在 `time.AfterFunc` 自己的 goroutine 上、它在**锁里**写
// 同一个字段。`lyrics.Entry` 里装的是 `string`，撕裂读出来就是一个野指针。
//
// 复现它要真的把那个 500ms 的定时器排上（所以这里不换假钟）——竞态窗口很窄，
// 去掉修复之后 `-race -count=5` 能抓到一次（`-count=50` 必抓）。
func TestPutLyricsRacesTheDebouncedSave(t *testing.T) {
	a, _ := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))
	a.PutLyrics("[00:01.00]词", "")
	a.SetLyricsOffset(-1000) // ← 排上**真的** 500ms 定时器

	// 这边连着换歌词，那边 500ms 后准时来落盘。
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			a.PutLyrics(fmt.Sprintf("[00:0%d.00]换了第 %d 遍", i%9+1, i), "")
		}
	}()
	time.Sleep(lyricsSaveDelay + 200*time.Millisecond)

	// ⚠️ **必须等它跑完再返回**。`PutLyrics` 是往盘上写文件的，而 `t.TempDir()`
	// 的清理是在测试返回**之后**去删那个目录的：这条 goroutine 还在写的时候清理就
	// 动手，于是报 `TempDir RemoveAll cleanup: directory not empty`——
	// 机器一忙 goroutine 活得比测试久，整仓跑时约三次红一次（实测）。
	// 一个自己会红的测试比一条 flaky 更糟：它把整张票卡住。
	<-done
}

// TestAnAdjustmentIsNotLostWhenTheSongChangesMidDebounce 盯住「防抖窗口内换歌」。
//
// 那道窗口是 500ms，用户在这半秒里换歌的话，窗口到期时读到的是**新歌**那条（歌词还
// 空着），上一首的那一次调整就被静默丢掉——界面上看不出任何异样：拖完偏移、换歌、
// 回头再放上一首，偏移是 0。上游不是这样（`debounce` 把 playItem 当参数传进去）。
func TestAnAdjustmentIsNotLostWhenTheSongChangesMidDebounce(t *testing.T) {
	a, c, clock := lyricsAppOnFakeClock(t)
	first := onlineSong("BV1", "1")
	a.setLyricTrack(first)
	a.PutLyrics("[00:01.00]第一首的词", "")

	a.SetLyricsOffset(-1000)
	clock.advance(lyricsSaveDelay / 3) // 窗口只走了三分之一

	a.setLyricTrack(onlineSong("BV2", "2")) // 就在这半秒里换歌
	clock.advance(lyricsSaveDelay)          // 让原来的窗口到期

	if got, _ := c.Load(lyricKeyOf(first)); got.OffsetMS != -1000 {
		t.Errorf("上一首的偏移落盘成了 %d，want -1000——那一次调整被换歌吞掉了",
			got.OffsetMS)
	}
}

// TestALateCacheReadDoesNotWipeTheLyricsYouJustAdopted 是代次守卫的回归测试。
//
// 时序：换歌 → 后台开始读缓存（还没回来）→ 用户从歌词搜索里换了一份（#21 接上之后走
// `PutLyrics`）→ 那次迟到的读回来了。少了守卫的话最后一步会把刚换的那份**盖成缓存
// 里原来的（多半是空的）**：症状是「刚找到的歌词一眨眼就没了」，而且按多少次搜索都
// 一样——用户没法重试，因为真正的原因在一次他看不见的后台读盘里。
//
// 这一条**不需要**注入慢缓存：换歌之后立刻 PutLyrics，读回来的 goroutine 大概率落在
// 后面（实测去掉守卫后 5 次全红）。
func TestALateCacheReadDoesNotWipeTheLyricsYouJustAdopted(t *testing.T) {
	a, _ := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))

	// 缓存里什么都没有（正是「这首歌还没找过歌词」的情形），紧接着换一份。
	a.PutLyrics("[00:01.00]刚找到的词", "")

	// 等足够久，让那次迟到的读盘有充足的机会回来盖掉它。
	time.Sleep(200 * time.Millisecond)

	rows := a.Lyrics().Rows
	if len(rows) != 1 || rows[0].Text != "刚找到的词" {
		t.Fatalf("刚换上的歌词被那次迟到的读盘盖掉了：%+v", rows)
	}
}

// TestTheDebounceWindowHoldsTheWrite 用假钟验那 500ms 的防抖窗口
// （`PLAN.md` 的验收项：「假时钟测 500ms 防抖，不靠 time.Sleep」）。
//
// 断的是两件事：
//
//   - 改完 499ms 时**还没写**（窗口没到）；
//   - 窗口里又改了一次，那一次要**从改的时候重新算**（拖滑块时每秒 20 次，
//     每一次都按原计划 500ms 后落盘的话，最后一次改的值会被前一次那下盖掉）。
func TestTheDebounceWindowHoldsTheWrite(t *testing.T) {
	// 500 是抄上游 `debounce(…, 500)`（`lyrics/index.tsx:191`）的那个数，写死在这儿
	// 不读 `lyricsSaveDelay`：读常量就等于拿生产代码的常量核对生产代码的行为，常量
	// 一改它跟着改，永远绿。
	const window = 500 * time.Millisecond

	a, c, clock := lyricsAppOnFakeClock(t)
	it := onlineSong("BV1", "1")
	a.setLyricTrack(it)
	// 采用歌词**当场就写**（它要记住换来的这一版），所以下面的判据是缓存里的
	// **偏移**，不是「有没有这条」。
	a.PutLyrics("[00:01.00]词", "")
	cached := func() int { e, _ := c.Load(lyricKeyOf(it)); return e.OffsetMS }

	a.SetLyricsOffset(100)
	clock.advance(window - time.Millisecond)
	if got := cached(); got != 0 {
		t.Fatalf("改完 499ms 就写下去了（缓存里的偏移 %d）——防抖窗口没起作用", got)
	}

	// 改到 499ms 时再改一次：窗口从**这一次**重新算。
	a.SetLyricsOffset(200)
	clock.advance(window - 2*time.Millisecond)
	if got := cached(); got != 0 {
		t.Fatalf("第二次改之后的 500ms 里写下去了（缓存里的偏移 %d）——窗口没有重新算", got)
	}

	clock.advance(4 * time.Millisecond)
	if got := cached(); got != 200 {
		t.Errorf("落盘的是 %d，want 200（最后一次改的值）", got)
	}
	if got := clock.runs; got != 1 {
		t.Errorf("窗口到期之后跑了 %d 次，want 1——防抖要把上一次排的那个定时器取消掉，"+
			"否则改了三次就落三次盘", got)
	}
}

// TestTheSameSongIsNotAReset：换流之后程序会把同一首再报一次歌词。那不是换歌，
// 重置会把用户刚调好的偏移抹掉，而用户那一下什么都没做。
func TestTheSameSongIsNotAReset(t *testing.T) {
	a, c := lyricsApp(t)
	it := onlineSong("BV1", "1")
	if err := c.Save(lyricKeyOf(it), lyrics.Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	a.setLyricTrack(it)
	waitLyrics(t, 2*time.Second, "歌词读回来", func() bool { return a.Lyrics().HasLyrics })

	a.SetLyricsOffset(800)
	a.setLyricTrack(it) // 换流：还是这一首

	if got := a.Lyrics().OffsetMS; got != 800 {
		t.Errorf("同一首被重报了，偏移被抹成 %d，want 800", got)
	}
}

// TestEachSongKeepsItsOwnOffset：两首歌的偏移各存各的。切回去还得是原来那个。
func TestEachSongKeepsItsOwnOffset(t *testing.T) {
	a, _ := lyricsApp(t)
	first := onlineSong("BV1", "1")
	a.setLyricTrack(first)
	a.PutLyrics("[00:01.00]甲的词", "") // 歌词有了，偏移才有地方可附

	a.SetLyricsOffset(-1200)
	a.SetLyricsFontSize(40)
	flushLyricsNow(t, a)

	second := onlineSong("BV2", "2")
	a.setLyricTrack(second)
	a.PutLyrics("[00:01.00]乙的词", "")
	a.SetLyricsOffset(700)
	flushLyricsNow(t, a)

	a.setLyricTrack(first)
	waitLyrics(t, 2*time.Second, "切回甲，甲的偏移恢复", func() bool {
		return a.Lyrics().OffsetMS == -1200
	})
	if got := a.Lyrics().FontSize; got != 40 {
		t.Errorf("切回甲的字号是 %v，want 40", got)
	}
	if got := a.Lyrics().Rows[0].Text; got != "甲的词" {
		t.Errorf("切回甲读到的是 %q，want 甲的词（乙的缓存串了？）", got)
	}
}

// flushLyricsNow 跳过那 500ms 的防抖窗口直接落盘。那道窗口本身由
// `TestTheDebounceWindowHoldsTheWrite` 用假钟单独验，这里只关心落下去的是什么。
func flushLyricsNow(t *testing.T, a *App) {
	t.Helper()
	a.flushLyrics()
}

// fakeAfter 是一只手动推进的钟。
//
// 用它是为了 `PLAN.md` 那条验收：「假时钟测 500ms 防抖，不靠 time.Sleep」。
//
// ⚠️ 每一次 `after` 都是**一个独立的定时器**，停掉只会把**那一个**作废——和
// `time.AfterFunc` 一个意思。只留「最后一次排的那个」的那只假钟是看不出问题的：防抖
// 窗口该取消上一次定时器这件事，只有「每一次排的都各自记着」才验得到（去掉那一句取
// 消，三次连改就落三次盘）。
type fakeAfter struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
	// runs 是跑到过的次数。防抖窗口要的就是「改了三次只落一次」。
	runs int
}

type fakeTimer struct {
	at      time.Duration // 到点的时刻（排的时候的现在 + d，和 Timer.Reset 一个意思）
	fn      func()
	stopped bool
}

func (c *fakeAfter) after(d time.Duration, fn func()) func() {
	c.mu.Lock()
	tm := &fakeTimer{at: c.now + d, fn: fn}
	c.timers = append(c.timers, tm)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		tm.stopped = true
		c.mu.Unlock()
	}
}

// advance 把钟往前推 d，到点的那些跑了（被停掉的除外）。
func (c *fakeAfter) advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var run []func()
	kept := c.timers[:0]
	for _, t := range c.timers {
		switch {
		case t.stopped:
		case c.now >= t.at:
			run = append(run, t.fn)
		default:
			kept = append(kept, t)
		}
	}
	c.timers, c.runs = kept, c.runs+len(run)
	c.mu.Unlock()
	for _, fn := range run {
		fn()
	}
}

// lyricsAppOnFakeClock 建一个**用假钟**的应用：防抖窗口只能由测试自己推进。
func lyricsAppOnFakeClock(t *testing.T) (*App, *lyrics.Cache, *fakeAfter) {
	t.Helper()
	dir := t.TempDir()
	a := New()
	t.Cleanup(a.Close)
	c := lyrics.NewCache(filepath.Join(dir, "lyrics"))
	a.MountLyrics(c)
	fake := &fakeAfter{}
	a.after = fake.after
	return a, c, fake
}

// TestTheAdjustedValuesSurviveARestart 是「切歌重置」的另一半：重置到默认只发生在**换歌**
// 那一刻，落在盘上的值下次打开还在。不落盘的话用户为了对上词调了半分钟，关掉再打开
// 又要从头调一遍——而调偏移恰恰是「一次调好、永远不管」的那种操作。
func TestTheAdjustedValuesSurviveARestart(t *testing.T) {
	a, c := lyricsApp(t)
	it := onlineSong("BV1", "1")
	if err := c.Save(lyricKeyOf(it), lyrics.Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}
	a.setLyricTrack(it)
	waitLyrics(t, 2*time.Second, "歌词读回来", func() bool { return a.Lyrics().HasLyrics })

	a.SetLyricsOffset(-950)
	a.SetLyricsFontSize(45)
	flushLyricsNow(t, a)

	// 「重开应用」：新一个 App，同一个缓存目录。
	dir := c.Dir()
	fresh := New()
	t.Cleanup(fresh.Close)
	fresh.MountLyrics(lyrics.NewCache(dir))
	fresh.setLyricTrack(it)
	waitLyrics(t, 2*time.Second, "重开后歌词读回来", func() bool {
		return fresh.Lyrics().HasLyrics
	})
	if got := fresh.Lyrics().OffsetMS; got != -950 {
		t.Errorf("重开之后偏移是 %d，want -950（没落盘？）", got)
	}
	if got := fresh.Lyrics().FontSize; got != 45 {
		t.Errorf("重开之后字号是 %v，want 45", got)
	}
}

// TestNothingIsWrittenForASongWithoutLyrics：偏移和字号是**附着在歌词上的**，
// 一首歌都没有歌词时它们无处可附，不该在用户数据目录里留下一个只有
// `{offset, fontSize}` 的空壳文件——每听一首积一个，界面上那个值还没有出处。
func TestNothingIsWrittenForASongWithoutLyrics(t *testing.T) {
	a, c := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))

	a.SetLyricsOffset(500)
	a.SetLyricsFontSize(28)
	flushLyricsNow(t, a)

	entries, err := filepath.Glob(filepath.Join(c.Dir(), "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("没有歌词却写下了 %d 个文件：%v", len(entries), entries)
	}
}

// TestSwapLyricsKeepsTheUsersAdjustments 是换歌词源那条路（#21 接上之后走它）：
// 换了原文和译文，**用户调过的偏移和字号不动**。上游换源只换歌词本身
// （index.tsx:269-278 的 handleLyricsAdopted），不动这两个。
func TestSwapLyricsKeepsTheUsersAdjustments(t *testing.T) {
	a, _ := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))
	a.SetLyricsOffset(300)
	a.SetLyricsFontSize(30)

	a.PutLyrics("[00:01.00]新的原文", "[00:01.00]新的译文")

	ls := a.Lyrics()
	if len(ls.Rows) != 1 || ls.Rows[0].Text != "新的原文" || ls.Rows[0].Translation != "新的译文" {
		t.Fatalf("换了之后是 %+v", ls.Rows)
	}
	if ls.OffsetMS != 300 || ls.FontSize != 30 {
		t.Errorf("换歌词把用户调的值带走了：%+v", ls)
	}
}

// TestSwapLyricsDropsTheOldTranslation：译文是**整个**换掉的。留着旧译文的话界面上是
// 「新的原文 + 旧的译文」——两行对不上的字比没有译文更难懂。
func TestSwapLyricsDropsTheOldTranslation(t *testing.T) {
	a, _ := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))
	a.PutLyrics("[00:01.00]有译文的歌", "[00:01.00]旧译文")

	a.PutLyrics("[00:01.00]没译文的歌", "")

	rows := a.Lyrics().Rows
	if len(rows) != 1 {
		t.Fatalf("want 1 行，出了 %d 行", len(rows))
	}
	if rows[0].Translation != "" {
		t.Errorf("译文还挂着旧的：%q", rows[0].Translation)
	}
}

// TestTheTwoKnobsAreClampedToTheUpstreamRanges 钉住那两个「死的」数：偏移 ±5000 步长
// 50、字号 12~48（上游 offset-control.tsx:18 和 font-size-control.tsx:16）。
//
// 越界夹住而不是照单全收：缓存文件是用户能直接编辑的，一个 `offset: 999999` 会让
// 歌词整体飞到歌外面去，而界面上看不出是哪儿错了。
func TestTheTwoKnobsAreClampedToTheUpstreamRanges(t *testing.T) {
	a, _ := lyricsApp(t)
	a.setLyricTrack(onlineSong("BV1", "1"))

	for _, c := range []struct {
		set          func()
		wantOffset   int
		wantFontSize float32
		why          string
	}{
		{func() { a.SetLyricsOffset(99999) }, MaxLyricsOffsetMS, 0, "越界的偏移夹到上界"},
		{func() { a.SetLyricsOffset(-99999) }, MinLyricsOffsetMS, 0, "越界的偏移夹到下界"},
		{func() { a.SetLyricsFontSize(500) }, 0, MaxLyricsFontSize, "越界的字号夹到上界"},
		{func() { a.SetLyricsFontSize(1) }, 0, MinLyricsFontSize, "越界的字号夹到下界"},
		{func() { a.SetLyricsFontSize(0) }, 0, DefaultLyricsFontSize, "0 是「没调过」，不是字号 0"},
	} {
		c.set()
		ls := a.Lyrics()
		if c.wantOffset != 0 && ls.OffsetMS != c.wantOffset {
			t.Errorf("%s：偏移是 %d，want %d", c.why, ls.OffsetMS, c.wantOffset)
		}
		if c.wantFontSize != 0 && ls.FontSize != c.wantFontSize {
			t.Errorf("%s：字号是 %v，want %v", c.why, ls.FontSize, c.wantFontSize)
		}
	}
}

// TestNoCacheMeansNoLyricsAndNoCrash：没挂缓存也得能跑（拿不到应用数据目录时就是
// 这样）。那时的表现是「暂无歌词」，不是空指针。
func TestNoCacheMeansNoLyricsAndNoCrash(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	a.setLyricTrack(onlineSong("BV1", "1"))
	if ls := a.Lyrics(); ls.HasLyrics {
		t.Errorf("没挂缓存却读出了歌词：%+v", ls)
	}

	a.SetLyricsOffset(500)
	a.SetLyricsFontSize(30)
	a.PutLyrics("[00:01.00]词", "")

	ls := a.Lyrics()
	if !ls.HasLyrics || ls.OffsetMS != 500 || ls.FontSize != 30 {
		t.Errorf("没挂缓存时调的值没留在内存里：%+v", ls)
	}
}

// TestLocalSongsKeyOnTheirFullPath：同名不同目录的两首歌必须是两条歌词。判重认的是
// **完整路径**（Biu 的 `isSame` 对本地项也认路径），挪了目录就是一个新文件。
func TestLocalSongsKeyOnTheirFullPath(t *testing.T) {
	a, c := lyricsApp(t)
	if err := c.Save(lyricKeyOf(LocalItem("/音乐/同名.mp3")),
		lyrics.Entry{Lyrics: "[00:01.00]第一份"}); err != nil {
		t.Fatal(err)
	}

	a.setLyricTrack(LocalItem("/音乐/同名.mp3"))
	waitLyrics(t, 2*time.Second, "第一份歌词读回来", func() bool { return a.Lyrics().HasLyrics })
	a.SetLyricsOffset(1000)

	a.setLyricTrack(LocalItem("/别处/同名.mp3"))
	if a.Lyrics().HasLyrics {
		t.Error("换了个目录却读到了另一首歌的歌词")
	}
	if got := a.Lyrics().OffsetMS; got != 0 {
		t.Errorf("换歌那一刻偏移是 %d，want 0", got)
	}
}
