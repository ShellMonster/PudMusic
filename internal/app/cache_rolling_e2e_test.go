package app

// 缓存过期**滚动**的端到端测试：走**真的应用层**（真的取流那一趟、真的盘上的
// 缓存文件、真的应用装配），确认「用户还在用的那一份不会过期」。
//
// # 为什么必须有一条 E2E
//
// 2026-10-30 用户提出来的：
//
//	「过期7天应该会滚动刷新吧，比如第一天用户听的，应该第8天删；
//	  第三天用户又听了，这时候应该是第11天清的」
//
// 而「滚动」这件事**恰恰是应用层的事**：缓存那一层只管「这一份过没过期」，
// 而「用户又听了」这个动作是**应用层**发起的（切歌 → 查缓存 → 命中）。
//
// ⇒ 只测缓存包的话，测的是「缓存包在被人告知『用过』之后会滚动」——
// 而**应用层有没有真的去告诉它**，那一条测不到。
//
// 🔴 **这一条测的正是那一件事**：应用层走过一遍之后，盘上那份记录的
// 「最后用过」是不是真的往前推了。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/danmaku"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

// danmakuRecord 是盘上那份弹幕记录的样子（`danmaku.Entry`）。
//
// ⚠️ **自己解 JSON 而不是用 `danmaku.Entry`**：这一条要验的是**盘上那个文件
// 里写的是什么**，而用同一个结构体去解就变成了「用被测代码的读法去验被测代码
// 的写法」——写错了读法也跟着错，那这条用例就白跑了。
type danmakuRecord struct {
	FetchedAt time.Time `json:"fetchedAt"`
}

// readDanmakuRecord 读盘上那一份弹幕记录。
func readDanmakuRecord(t *testing.T, dir, bvid, cid string) (danmakuRecord, bool) {
	t.Helper()
	path := filepath.Join(dir, bvid+"-"+cid+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return danmakuRecord{}, false
	}
	var rec danmakuRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("盘上那份弹幕记录解析不了：%v（内容：%s）", err, raw)
	}
	return rec, true
}

// danmakuModTime 读盘上那一份弹幕文件的**修改时刻**。
//
// 🔴 **那才是「最后一次用过」的时刻**（见 `service/diskcache` 的文件头）：
// 过期的判据是它，而每次读到它都会把它推到当下。
func danmakuModTime(t *testing.T, dir, bvid, cid string) (time.Time, bool) {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, bvid+"-"+cid+".json"))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// rollingDanmakuApp 装一个带假时钟的弹幕缓存应用。
//
// ⚠️ **假时钟装在缓存上**：应用层那一侧的时间（退避、序号那些）用的是真时钟，
// 而**过期判据**走的是缓存那个时钟——所以只有「过期」这一件事被推进了。
func rollingDanmakuApp(t *testing.T) (*App, *danmaku.Cache, string, *time.Time) {
	t.Helper()
	now := time.Now()
	clock := now
	cacheDir := t.TempDir()
	c := danmaku.NewCache(cacheDir).WithClock(func() time.Time { return clock })

	a := New()
	t.Cleanup(a.Close)
	a.MountDanmaku(c)
	return a, c, cacheDir, &clock
}

// TestTheDanmakuCacheExpiryRollsWhenTheAppReusesIt 盯「应用层用过之后，
// 过期窗口真的往前推了」。
//
// 🔴 **判据是「盘上那份记录的 `lastUsedAt` 往前推了」**，不是「缓存有没有
// 命中」——命中与否在别的用例里已经量过了，而这里要量的是**滚动**本身。
func TestTheDanmakuCacheExpiryRollsWhenTheAppReusesIt(t *testing.T) {
	a, _, cacheDir, clock := rollingDanmakuApp(t)

	// 取弹幕那一趟：数它被问了几次。
	var asked int
	a.danmakuFetch = func(context.Context, string) ([]bili.Danmaku, error) {
		asked++
		return []bili.Danmaku{{Text: "第一条"}}, nil
	}

	// 第 1 天：第一次进这一集 ⇒ 走网络。
	putDanmakuCurrent(a, "BV1", "1")
	a.Danmaku()
	waitRolling(t, "弹幕装上了", func() bool {
		return len(a.Danmaku().Items) > 0
	})
	waitRolling(t, "缓存记上了", func() bool {
		_, ok := readDanmakuRecord(t, cacheDir, "BV1", "1")
		return ok
	})
	if asked != 1 {
		t.Fatalf("第一次问了 %d 次，want 1", asked)
	}
	first, _ := readDanmakuRecord(t, cacheDir, "BV1", "1")
	firstMod, ok := danmakuModTime(t, cacheDir, "BV1", "1")
	if !ok {
		t.Fatal("盘上那份缓存文件不见了")
	}

	// 🔴 **第 3 天**：用户又进了这一集。
	*clock = first.FetchedAt.Add(3 * 24 * time.Hour)
	// 换到另一集再换回来（那一趟会重新查缓存）。
	putDanmakuCurrent(a, "BV1", "2")
	a.Danmaku()
	waitRolling(t, "换到第二集", func() bool {
		return a.danmakuKey == danmakuKeyOf("BV1", "2")
	})
	putDanmakuCurrent(a, "BV1", "1")
	a.Danmaku()
	// 等缓存命中那一趟真的走过（它是在后台的）。
	waitRolling(t, "缓存命中那一趟走完了", func() bool {
		mod, ok := danmakuModTime(t, cacheDir, "BV1", "1")
		return ok && mod.After(firstMod)
	})

	secondMod, _ := danmakuModTime(t, cacheDir, "BV1", "1")
	if !secondMod.After(firstMod) {
		t.Errorf("「最后用过」没有往前推（还是 %v）——"+
			"过期没有跟着使用滚动，用户天天在听的这一集第七天就会消失",
			secondMod)
	}
	// ⚠️ **「取回来的时刻」不许动**：那是另一个事实（这一份是什么时候拿到的）。
	second, _ := readDanmakuRecord(t, cacheDir, "BV1", "1")
	if !second.FetchedAt.Equal(first.FetchedAt) {
		t.Errorf("「取回来的时刻」被改了（%v → %v）——"+
			"应用层重写缓存时把整个记录盖掉了", first.FetchedAt, second.FetchedAt)
	}
}

// TestTheDanmakuCacheIsNotRefetchedWhileItIsStillFresh 盯「还在有效期内的
// 那一份不会被重拉」。
//
// ⚠️ **这是「滚动」的另一半**：滚动不是为了「一直不过期」，而是为了
// 「用户不用它之后，它确实会被清掉」。而「不用它之后会被清掉」的前提是
// **「还在用的时候别去重拉」**——不然滚动就是「每隔 7 天重下一次」。
func TestTheDanmakuCacheIsNotRefetchedWhileItIsStillFresh(t *testing.T) {
	a, _, cacheDir, clock := rollingDanmakuApp(t)

	// 🔴 **只数 cid "1" 的**：换到第二集那一趟**本来就该问**（那是另一集的
	// 弹幕，缓存键不一样），而这里要验的是「**这一集**不该被重拉」。
	var asked int
	a.danmakuFetch = func(_ context.Context, cid string) ([]bili.Danmaku, error) {
		if cid == "1" {
			asked++
		}
		return []bili.Danmaku{{Text: "第一条"}}, nil
	}

	putDanmakuCurrent(a, "BV1", "1")
	a.Danmaku()
	waitRolling(t, "弹幕装上了", func() bool {
		return len(a.Danmaku().Items) > 0
	})
	waitRolling(t, "缓存记上了", func() bool {
		_, ok := readDanmakuRecord(t, cacheDir, "BV1", "1")
		return ok
	})
	first, _ := readDanmakuRecord(t, cacheDir, "BV1", "1")

	// 第 3 天：换集再换回来。**这一集还在被用** ⇒ 不该重拉。
	*clock = first.FetchedAt.Add(3 * 24 * time.Hour)
	putDanmakuCurrent(a, "BV1", "2")
	a.Danmaku()
	waitRolling(t, "换到第二集", func() bool {
		return a.danmakuKey == danmakuKeyOf("BV1", "2")
	})
	putDanmakuCurrent(a, "BV1", "1")
	a.Danmaku()
	waitRolling(t, "换回第一集", func() bool {
		return a.danmakuKey == danmakuKeyOf("BV1", "1")
	})
	// 给后台那一趟一点时间（它要真的查一次缓存才算数）。
	time.Sleep(500 * time.Millisecond)

	if asked != 1 {
		t.Errorf("「第一集」的弹幕被问了 %d 次，want 1——"+
			"用户第 3 天又听了这一集，而它还在被用，不该重拉", asked)
	}
}

// waitRolling 等一个条件成立（最多 10 秒）。
//
// ⚠️ **自己起一个名字**：这个包里已经有两三个 `waitFor` 了（签名还不一样），
// 而它们分属不同的包（`app` / `app_test`）——撞名会编译不过。
func waitRolling(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等了 10 秒，%s 还没发生", what)
}

// TestTheUserLyricsValuesSurviveTheCacheExpiry 盯「歌词缓存过期了，用户调过
// 的偏移和字号还在」。
//
// 🔴 **这是这一层最要紧的一条**（2026-10-30 的对抗性审查挖出来的 P0）。
//
// 用户把歌词偏移拖到 +250ms、字号调到 22。缓存 7 天不用就过期——而
// **过期不该把用户自己的设置清零**：用户看到的现象是「某天回来发现我调过的
// 歌词偏移不见了，字号也变小了」，而且**永久丢失**（缓存里只有那一份）。
//
// 🔴 **走真的应用层**：这条用例要验的是「应用层把用户值恢复回来了」——
// 而那件事只有走一遍应用层才算验过（缓存那一层只管「歌词文本过没过期」）。
func TestTheUserLyricsValuesSurviveTheCacheExpiry(t *testing.T) {
	now := time.Now()
	clock := now
	cacheDir := t.TempDir()
	c := lyrics.NewCache(cacheDir).WithClock(func() time.Time { return clock })

	a := New()
	t.Cleanup(a.Close)
	a.MountLyrics(c)

	// ⚠️ **先认下这一首**：`PutLyrics` 要一个当前键（空键它不写）。
	a.setLyricTrack(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})
	// 再放一份歌词（相当于搜来的那一份）。
	a.PutLyrics("[00:01.00]词", "")
	// 用户调了偏移和字号。
	a.SetLyricsOffset(250)
	a.SetLyricsFontSize(22)
	// 让防抖落盘那一趟真的写下去。
	a.flushLyrics()

	// 盘上得有那一份缓存了。
	waitRolling(t, "歌词缓存写下去了", func() bool {
		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			return false
		}
		return len(entries) > 0
	})

	// 🔴 **第 8 天**：歌词文本过期了。
	clock = now.Add(8 * 24 * time.Hour)

	// 🔴 **换一个应用实例**（模拟「重启之后又听了这一首」）。
	//
	// ⚠️ **必须换一个实例**：同一个实例里那份歌词还在**内存**中，而切歌那一趟
	// 会把内存里的那份**重新写回盘上**（于是修改时刻被刷新，那一份又「新鲜」
	// 了）——那样这条用例就测不到「过期」这一件事了。
	//
	// ⚠️ **而「重启之后」正是用户遇到这个问题的场景**：他过了八天再打开
	// 应用，这一首的歌词缓存过期了，而他要的偏移和字号**不该跟着没**。
	a2 := New()
	t.Cleanup(a2.Close)
	a2.MountLyrics(c)
	a2.setLyricTrack(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"})

	// 等读盘回来（那一趟在后台）。
	//
	// ⚠️ **等的是「用户值被恢复」而不是「加载态收掉」**：过期之后没有歌词文本，
	// 而「没有歌词」那一档的加载态要等别的东西（B 站字幕、自动搜）才收掉——
	// 那两条在这个测试里都没有挂上。
	waitRolling(t, "歌词读盘回来了", func() bool {
		return a2.Lyrics().OffsetMS == 250
	})
	snap := a2.Lyrics()
	if snap.OffsetMS != 250 {
		t.Errorf("过期之后用户调过的偏移变成了 %d，want 250——"+
			"用户自己的数据不该跟着歌词文本一起过期", snap.OffsetMS)
	}
	if snap.FontSize != 22 {
		t.Errorf("过期之后用户调过的字号变成了 %v，want 22——"+
			"用户自己的数据不该跟着歌词文本一起过期", snap.FontSize)
	}
}

// TestTheLocalLyricsAreSearchedAgainAfterTheCacheExpires 盯「本地文件的歌词
// 过期之后，自动找歌词还会被触发」。
//
// 🔴 **这一条盯的是一个功能回归**（2026-10-30 的第二轮对抗性审查挖出来的
// P1）：缓存层的 `Load` 在**过期**的时候返回的是「记录在、歌词没了」
// （ok = true，而 `Lyrics` 是空串）——因为用户调过的偏移和字号还在那份记录里。
//
// 而**本地文件那一支的「自动找歌词」是挂在 `!ok` 上的**：本地文件没有
// B 站字幕那一路，歌词全靠自动搜。所以判据要是「ok」，本地文件的那份歌词
// 过期之后就**永远搜不回来了**——用户看到的现象是「这一首的歌词永久消失，
// 除非他自己开搜索弹窗」。
func TestTheLocalLyricsAreSearchedAgainAfterTheCacheExpires(t *testing.T) {
	now := time.Now()
	clock := now
	cacheDir := t.TempDir()
	c := lyrics.NewCache(cacheDir).WithClock(func() time.Time { return clock })

	// 本地文件那一支：键是 `local-<完整路径>`。
	songPath := filepath.Join(t.TempDir(), "本地那首.mp3")
	key := "local-" + songPath
	it := Item{Path: songPath, Title: "本地那首"}

	// 先放一份歌词（相当于自动搜到的那一份）。
	if err := c.Save(key, lyrics.Entry{Lyrics: "[00:01.00]词"}); err != nil {
		t.Fatal(err)
	}

	a := New()
	t.Cleanup(a.Close)
	a.MountLyrics(c)
	// 自动找歌词要有一个源——用假的那个（它会记下自己被问过）。
	var searched atomic.Int32
	a.lyricSearch.sources = []lyrics.Source{countingLyricSource{searched: &searched}}

	// 🔴 **第 8 天**：歌词缓存过期了。
	clock = now.Add(8 * 24 * time.Hour)
	a.setLyricTrack(it)

	// 等自动搜那一趟真的被触发了。
	waitRolling(t, "自动找歌词被触发了", func() bool {
		return searched.Load() > 0
	})
}

// countingLyricSource 是一份「只数自己被问了几次」的歌词源。
type countingLyricSource struct {
	searched *atomic.Int32
}

func (s countingLyricSource) ID() string { return "counting" }

func (s countingLyricSource) Name() string { return "counting" }

func (s countingLyricSource) Search(_ context.Context, _ string) ([]lyrics.Candidate, error) {
	s.searched.Add(1)
	return nil, nil
}

func (s countingLyricSource) Preview(context.Context, lyrics.Candidate) (lyrics.Doc, error) {
	return lyrics.Doc{}, nil
}
