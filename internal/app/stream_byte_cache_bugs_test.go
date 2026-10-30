package app_test

// 审查（2026-10-30 的对抗性审查）挖出来的几个坑，各有一条用例钉着。
//
// 每一条都是**用户看得见的后果**，不是代码风格：
//
//	无限递归	点了歌完全没反应，CDN 一次都没被拉
//	恢复丢位置	重启之后那首歌从头开始而且自己出声
//	档位口径	「选回当前这一档」不再是操作，白下一遍
//	半截当完整	切回来听到放了一半的歌

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// newCacheApp 建一个挂了在线播放、并且有字节缓存的应用。
func newCacheApp(t *testing.T, f *streamFixture) (*app.App, string) {
	t.Helper()
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
	return a, cacheDir
}

// TestABrokenCacheDoesNotLoopForever 盯「盘上那份坏了不会死循环」。
//
// 🔴 **这是那一票最严重的一个**：盘上那份字节打不开（磁盘出过错、用户
// 手工改过、杀毒软件改写）时，如果只是「走网络那一路」而不先把这一份
// 从缓存里删掉，下一次查缓存**又命中同一份**——
//
//	打不开 → 走网络 → 缓存判定又命中 → 打不开 → 走网络 → ……
//
// 而用户看到的是「点了歌完全没反应，CDN 一次都没被拉」。
func TestABrokenCacheDoesNotLoopForever(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a, cacheDir := newCacheApp(t, f)

	// 先下一遍，让盘上有那份字节。
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首开始出声", func() bool {
		return a.Snapshot().Title == "A 首"
	})
	waitFor(t, 10*time.Second, "A 首下完", func() bool {
		return len(cacheFiles(t, cacheDir, ".bin")) > 0
	})

	// 🔴 **把盘上那份弄坏**：内容变成垃圾，而**大小不变**。
	//
	// ⚠️ **大小要不变**：只有大小变了的话，缓存的判据（大小对不上）就
	// 认出来它坏了 —— 而那测不到「判据没认出来」的那条路。
	bin := cacheFiles(t, cacheDir, ".bin")[0]
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, make([]byte, st.Size()), 0o644); err != nil {
		t.Fatal(err)
	}

	// 切走再切回。
	if err := a.PlayFrom(onlineItem("BV1", "B", "B 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "B 首开始出声", func() bool {
		return a.Snapshot().Title == "B 首"
	})
	hitsBefore := f.mediaHits.Load()
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}

	// 🔴 **必须真的重新走网络**：坏的那一份要被从缓存里删掉。
	waitFor(t, 10*time.Second, "A 首重新走网络", func() bool {
		return f.mediaHits.Load() > hitsBefore
	})

	// 🔴 **而且不能一直转**：转太多次就是死循环。
	// ⚠️ **判据是「短时间内转了很多次」**：用户看到的是「点了歌完全没反应」。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := f.mediaHits.Load(); got > hitsBefore+20 {
		t.Errorf("CDN 被拉了 %d 次（切回去那一趟只该 1 次）——"+
			"盘上那份坏了之后没有从缓存里删掉，于是「打不开 → 走网络 → 又命中」"+
			"转成了死循环，而用户看到的是「点了歌完全没反应」", got-hitsBefore)
	}
}

// TestTheRestoreKeepsItsPositionAndStaysSilent 盯「恢复那一趟不丢位置、不出声」。
//
// 🔴 **这是明确的产品规则**（见 `playOnlineFrom` 的注释）：启动时恢复上次
// 听到哪儿，那一首要**停在那个位置**而且**不出声**。
//
// ⚠️ **命中缓存的那一路丢掉了这两个参数的话**，用户重启应用之后那首歌
// 会**从头开始而且自己出声**——而那一首刚刚还在他耳朵里放着。
func TestTheRestoreKeepsItsPositionAndStaysSilent(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	settingsPath := filepath.Join(t.TempDir(), app.SettingsFileName)
	cacheDir := t.TempDir()

	// 第一次：放它，让它下完（盘上有了那份字节）。
	a := app.New()
	if err := a.MountSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client:   cli,
		Media:    f.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})
	if err := a.PlayFrom(onlineItem("BV1", "A", "A 首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首开始出声", func() bool {
		return a.Snapshot().Title == "A 首"
	})
	waitFor(t, 10*time.Second, "A 首下完", func() bool {
		return len(cacheFiles(t, cacheDir, ".bin")) > 0
	})
	// 拖到 2 秒，然后暂停（位置才停得住）。
	//
	// ⚠️ **用 `SeekTo` 而不是 `ScrubEnd`**：后者是**比例**
	// （`fraction * 时长`），而这份素材只有一秒多——比例算出来的位置是几十
	// 毫秒，量不出「位置有没有丢」。
	a.SeekTo(2 * time.Second)
	a.Toggle()
	waitFor(t, 5*time.Second, "位置停住了", func() bool {
		return a.Snapshot().Position > time.Second
	})
	// 落盘：队列、当前项、位置。
	a.Close()
	_ = a.CloseSettings()

	// 第二次：重启（**同一个缓存目录**），恢复。
	b := app.New()
	if err := b.MountSettings(settingsPath); err != nil {
		t.Fatal(err)
	}
	cli2 := bili.NewClient(bili.Options{})
	cli2.TestOnlyPointHostsAtFixture(f.URL)
	b.MountStreaming(app.Streaming{
		Client:   cli2,
		Media:    f.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})
	t.Cleanup(func() {
		b.Close()
		_ = b.CloseSettings()
	})

	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "A 首恢复出来", func() bool {
		return b.Snapshot().Title == "A 首"
	})

	st := b.Snapshot()
	// 🔴 **位置不许丢**：恢复出来的那一首要停在原来那个位置。
	if st.Position < 500*time.Millisecond {
		t.Errorf("恢复之后的位置 = %v，want ≥500ms——"+
			"命中缓存的那一路把「停到哪儿」丢了，那首歌从头开始放了", st.Position)
	}
	// 🔴 **不许出声**：启动时突然出声是最糟的一种。
	if st.Playing {
		t.Errorf("恢复之后在放（Playing = true）——" +
			"命中缓存的那一路把「别出声」丢了，用户刚打开应用房间里突然有声音")
	}
}

// cacheFiles 列出缓存目录里带某个后缀的文件。
func cacheFiles(t *testing.T, dir, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	var out []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == suffix {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}
