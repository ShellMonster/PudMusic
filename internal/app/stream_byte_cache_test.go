package app_test

// 字节落盘缓存：**下过的字节留在盘上**，来回切歌不再走网络。
//
// # 这条盯的是真问题（2026-10-30 用户提出来的）
//
// 用户的原话：「前面比如取到了的，那应该是在本地电脑缓存上啊，
// 为啥还要再去跑网络请求；应该是没有才跑网络请求啊」。
//
// 地址缓存（PR #691）只解决了「**地址**不要再去问一遍」。但**字节**那一层
// 原来是没有的：`StreamSource` 用 `os.CreateTemp` 建一个随机名的临时文件，
// `Close` 把它删掉。于是：
//
//	A 第一次播 → 整首下到盘上 → 切走 → **文件删了**
//	A 第二次播 → 又从零下一个整首 → CDN 又被拉一遍
//
// 用户来回切歌时，每切一下都白下一整首。
//
// # 判据
//
// 1. 切走之后，缓存文件**还在盘上**（没被删）。
// 2. 切回那一首时，**CDN 不该再被拉**（`mediaHits` 不涨）。
// 3. 切回那一首时，**连取流接口都不该再问**（`playCalls` 不涨）——
//    这一条是「整首下完就完全本地播，连地址都不要」。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestSwitchingBackAndForthDoesNotDownloadTwice 盯「来回切歌不重复下」。
func TestSwitchingBackAndForthDoesNotDownloadTwice(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 🔴 **每一首有自己的 CDN 路径**：判「切回去那一趟有没有白下」要看的是
	// **同一首**那条路径被打了多少次。
	f.onPlayURLForCid("A", dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURLForCid("B", dashFor(cdnURL(f.URL, "b.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	cacheDir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client:   cli,
		Media:    f.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})
	// 🔴 **先关应用，再让 `t.TempDir()` 收尾**：清理是**倒序**跑的，
	// 而 `t.TempDir()` 的清理是在它被调用的那一刻登记的——比这里登记的
	// `a.Close` **早**。于是缓存目录会在字节源还开着那个文件的时候被删，
	// 收尾报「directory not empty」（Windows 上那更是删不掉）。
	t.Cleanup(a.Close)

	// A → B → A：A 第二次播的时候不该再从 CDN 拉一遍。
	for i, who := range []string{"A", "B", "A"} {
		if err := a.PlayFrom(onlineItem("BV1", who, "第 "+who+" 首")); err != nil {
			t.Fatalf("第 %d 次切到 %s：%v", i+1, who, err)
		}
		waitFor(t, 10*time.Second, "「第 "+who+" 首」开始出声", func() bool {
			return a.Snapshot().Title == "第 "+who+" 首"
		})
	}

	// 🔴 **A 那一条 CDN 路径只该被拉一次**（第一次播 A 那次）。
	//
	// ⚠️ **判据是「同一条路径被打了多少次」，不是「总数」**：总数是 2
	// （A 一次、B 一次）——**每一首各一次才是对的**，而 A 被拉两次才是错的。
	if got := f.mediaHitsFor("/cdn/A.flac"); got > 1 {
		t.Errorf("A 的 CDN 路径被打了 %d 次，want ≤1——"+
			"下过的字节没有留在盘上，用户每切回去一次就白下一整首", got)
	}

	// 🔴 **连取流接口都不该再问**：「整首下完就完全本地播，连地址都不要」。
	//
	// ⚠️ **判据同样是「A 被问了几次」**：A → B → A 问两次是正常的
	// （A 一次、B 一次），而 A 被问两次才是错的。
	aCalls := 0
	for _, q := range f.playQueries {
		if q.Get("cid") == "A" {
			aCalls++
		}
	}
	if aCalls > 1 {
		t.Errorf("A 被问取流接口 %d 次，want ≤1——"+
			"整首已经下完了，切回去的时候不该再去问 B 站要地址", aCalls)
	}

	// 缓存文件留在盘上。
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	if len(entries) == 0 {
		t.Errorf("缓存目录 %s 是空的——下过的字节没留在盘上", cacheDir)
	}
	// 至少有一个是完整的（A 那一首）。
	var biggest int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > biggest {
			biggest = info.Size()
		}
	}
	if biggest != int64(len(f.media)) {
		t.Errorf("最大的缓存文件 = %d 字节，want %d（整首）", biggest, len(f.media))
	}
}

// TestTheCacheIsUsedOnlyForTheSameTrack 盯「缓存不能串歌」。
//
// 🔴 **这是这一票最危险的地方**：缓存键算错一次，用户点 A 放出来的却是 B
// 的声音——而界面上什么都看不出来（歌名是 A，声音是 B）。
//
// 所以这里要验：同一份缓存目录，两首**不同的**歌，各下各的，
// 切回去的时候各自拿到自己的字节。
func TestTheCacheIsUsedOnlyForTheSameTrack(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	cacheDir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client:   cli,
		Media:    f.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})
	// 🔴 **先关应用，再让 `t.TempDir()` 收尾**：清理是**倒序**跑的，
	// 而 `t.TempDir()` 的清理是在它被调用的那一刻登记的——比这里登记的
	// `a.Close` **早**。于是缓存目录会在字节源还开着那个文件的时候被删，
	// 收尾报「directory not empty」（Windows 上那更是删不掉）。
	t.Cleanup(a.Close)

	// 先放 A，让它下完。
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首开始出声", func() bool {
		return a.Snapshot().Title == "A 首"
	})
	// 等它真的下完（字节数到了整首）。
	// 🔴 **等的是「缓存记上了『下完了』」**：`mediaHits > 0` 只是「第一个字节
	// 到了」——而界面上的「开始出声」也是那一刻。慢网下测试会在只下了几 KB
	// 的时候就往下走，于是这一条验的是「下到一半的缓存」。
	//
	// ⚠️ **「下完了」的唯一外部证据是那份记录里的 `complete: true`**。
	waitFor(t, 10*time.Second, "A 首下完", func() bool {
		return cacheHasComplete(t, cacheDir, "online|mv|BV1|A|")
	})
	// 切到 B（不同的一首）再切回 A。
	if err := a.PlayFrom(onlineItem("BV1", "B", "B 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "B 首开始出声", func() bool {
		return a.Snapshot().Title == "B 首"
	})
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首回来", func() bool {
		return a.Snapshot().Title == "A 首"
	})

	// 缓存目录里要有**两份**不同的文件（A 和 B 各一份）。
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	var sizes []int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > 0 {
			sizes = append(sizes, info.Size())
		}
	}
	if len(sizes) < 2 {
		t.Errorf("缓存目录里有 %d 个非空文件，want ≥2——"+
			"两首不同的歌不该共用同一个缓存文件", len(sizes))
	}
}

// TestAPartialDownloadIsNotMistakenForACompleteOne 盯「没下完的不能当缓存用」。
//
// 🔴 **这是这一票最危险的一种错**：字节源的文件布局是「顺序下载的前沿 +
// 并行取回来的末尾那一段 + 中间可能的洞」（见 `service/audio` 的 `availAt`）。
// 一个**下到一半**的文件，大小可能和整首**一样**（末尾那一段先到了），
// 而它的中间是**洞**——洞里的字节是零，解出来是噪声。
//
// 所以「下完了」必须靠**旁挂的元数据**（`Complete` 那一栏），不能靠文件大小。
//
// 判据：下到一半就被切走的那一首，**切回去的时候要重新走网络**
// （用盘上那份半截的字节放出来 = 放噪声）。
func TestAPartialDownloadIsNotMistakenForACompleteOne(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 🔴 **CDN 永远只发 4096 字节**：这一首下不完。
	//
	// ⚠️ **要「永远」那一条**：断流之后源会重试，而重试那一次就下完了
	// （`failMediaAfter` 只掐第一次）——那样这一首最终还是完整的，
	// 而这一条用例就测不到「下了一半」。
	f.alwaysTruncateAt = 4096
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	cacheDir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client:   cli,
		Media:    f.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})
	// 🔴 **先关应用，再让 `t.TempDir()` 收尾**：清理是**倒序**跑的，
	// 而 `t.TempDir()` 的清理是在它被调用的那一刻登记的——比这里登记的
	// `a.Close` **早**。于是缓存目录会在字节源还开着那个文件的时候被删，
	// 收尾报「directory not empty」（Windows 上那更是删不掉）。
	t.Cleanup(a.Close)

	// 放它，然后等它开始下（但下不完）。
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首开始取流", func() bool {
		return f.mediaHits.Load() > 0
	})
	// 让它下到那个断点。
	waitFor(t, 10*time.Second, "A 首下到断点", func() bool {
		return f.mediaHits.Load() > 0
	})
	// 切走：这一趟的源会被掐掉。
	if err := a.PlayFrom(onlineItem("BV1", "B", "B 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "B 首开始出声", func() bool {
		return a.Snapshot().Title == "B 首"
	})

	// 🔴 **盘上那一份不能被当成「可以直接播的缓存」**。
	//
	// ⚠️ **判据是「缓存目录里没有那一份的记录」**：这一首永远下不完，
	// 而「下不完」的那一份**不能**被标成完整——用户切回来的时候，
	// 用半截的字节放出来 = 放噪声。
	//
	// ⚠️ **不是「CDN 又被拉了一次」**：这一首压根放不出来（CDN 永远只发
	// 4096 字节），所以「切回去」那一趟根本走不到 CDN。要盯的是**缓存
	// 有没有把这一份记成完整**。
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), `"complete":true`) &&
			strings.Contains(string(raw), `"track":"online|mv|BV1|A|"`) {
			t.Errorf("没下完的那一份被记成完整了：%s——"+
				"用户切回来听到的是放了一半的歌", raw)
		}
	}
}

// TestWithoutACacheDirNothingIsKept 盯「没给缓存目录时行为不变」。
//
// 🔴 **底线**：在线播放的临时文件是「切歌不遗留」那道门
// （见 audio 包里的 `TestCloseRemovesTheCacheFileAndJoinsEveryGoroutine`）。
// 没给缓存目录时不能变成「什么都被留下」。
func TestWithoutACacheDirNothingIsKept(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	tempDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client:  cli,
		Media:   f.Client(),
		TempDir: tempDir,
	})

	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首开始出声", func() bool {
		return a.Snapshot().Title == "A 首"
	})

	// 临时目录里不该留下缓存。
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != "" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		t.Errorf("临时目录里留下了 %s（%d 字节）——没给缓存目录时不该留东西", e.Name(), info.Size())
	}
}
