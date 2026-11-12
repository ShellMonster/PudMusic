package app

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

// 这一组测「开一首歌时顺带问网易云要逐字歌词」（lyrics_prefer.go）。
//
// 判据都走 App.Lyrics()：用户看到的是那一行行，而不是哪个 goroutine 先跑到哪。

// staysFor 盯一段时间内条件一直成立。给「不该发生」的异步行为用：不能等它发生，
// 只能给它足够的窗口去发生、然后在每次轮询时立刻判红。
func staysFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !cond() {
			t.Fatalf("%s 在 %v 内变了", what, d)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestANonStandardTitleGetsNeteaseWordTiming 钉住这一条路的两半：
//
//   - 发给网易云的是 `cleanLyricTitle` 洗过的歌名（视频标题里那些【】《》和
//     Hi-Res 会把搜索带偏）；
//   - 带 YRC 的候选装上去之后，第一行是**逐字**的（Words 非空），而不是整句。
func TestANonStandardTitleGetsNeteaseWordTiming(t *testing.T) {
	const (
		videoTitle = "【交响】你没听过的《千秋岁引》版本【国家宝藏】【Hi-Res】"
		wantQuery  = "你没听过的 千秋岁引 版本"
	)
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引", Artist: "某某"})
	src.doc = lyrics.Doc{
		LRC: "[00:01.00]千秋岁引",
		YRC: "[1000,2000](1000,1000,0)千(2000,1000,0)秋",
	}
	a, _ := searchApp(t, src)

	it := onlineSong("BV1", "1")
	it.Title = videoTitle
	a.setLyricTrack(it)

	waitSearch(t, 2*time.Second, "逐字歌词上屏", func() bool {
		rows := a.Lyrics().Rows
		return len(rows) > 0 && len(rows[0].Words) > 0
	})
	if got := a.Lyrics().Origin; got != lyrics.OriginAuto {
		t.Errorf("来路 = %q，want %q", got, lyrics.OriginAuto)
	}
	qs := src.seenQueries()
	if len(qs) == 0 || qs[0] != wantQuery {
		t.Errorf("网易云收到的搜索词 = %q，want 第一句是 %q", qs, wantQuery)
	}
}

// TestAYRCLessCandidateDoesNotReplaceTheBiliSubtitle 钉住「没有逐字就不动」：
//
// 候选本身（或者拉回来的那一份）没有 YRC 时，B 站已经装上的那份歌词原样留着。
func TestAYRCLessCandidateDoesNotReplaceTheBiliSubtitle(t *testing.T) {
	gate := make(chan struct{})
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引"})
	src.gate = gate
	src.doc = lyrics.Doc{LRC: "[00:01.00]搜来的整句"}
	a, _ := searchApp(t, src)

	it := onlineSong("BV1", "1")
	it.Title = "千秋岁引"
	a.setLyricTrack(it)
	waitSearch(t, 2*time.Second, "网易云那一趟真的发出去了", func() bool {
		n, _ := src.count()
		return n == 1
	})

	// B 站那份先在（真实时序里它比搜索早得多）。
	a.PutLyricsFrom(lyrics.OriginBili, "[00:01.00]B 站那份", "")
	close(gate)
	waitSearch(t, 2*time.Second, "候选拉回来", func() bool {
		_, p := src.count()
		return p == 1
	})

	staysFor(t, 300*time.Millisecond, "B 站那份", func() bool {
		ls := a.Lyrics()
		return ls.HasLyrics && ls.Rows[0].Text == "B 站那份" && ls.Origin == lyrics.OriginBili
	})
}

// TestWordTimingReplacesASubtitleThatArrivedFirst 钉住这条策略正对的那场竞速：
//
// B 站字幕先装上（它只有整句），随后那份带 YRC 的搜索结果回来，必须把它换掉。
// 代次已经因为那次安装推进过，守卫要放这一份过去。
func TestWordTimingReplacesASubtitleThatArrivedFirst(t *testing.T) {
	gate := make(chan struct{})
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引"})
	src.gate = gate
	src.doc = lyrics.Doc{
		LRC: "[00:01.00]千秋岁引",
		YRC: "[1000,2000](1000,1000,0)千(2000,1000,0)秋",
	}
	a, _ := searchApp(t, src)

	it := onlineSong("BV1", "1")
	it.Title = "千秋岁引"
	a.setLyricTrack(it)
	waitSearch(t, 2*time.Second, "网易云那一趟真的发出去了", func() bool {
		n, _ := src.count()
		return n == 1
	})

	a.PutLyricsFrom(lyrics.OriginBili, "[00:01.00]B 站整句", "")
	close(gate)

	waitSearch(t, 2*time.Second, "逐字那份顶掉了字幕", func() bool {
		rows := a.Lyrics().Rows
		return len(rows) > 0 && len(rows[0].Words) > 0
	})
	if got := a.Lyrics().Origin; got != lyrics.OriginAuto {
		t.Errorf("来路 = %q，want %q", got, lyrics.OriginAuto)
	}
}

// TestWordTimingReplacesAnAutomaticallyFoundLyric 盯「兜底自动搜先装上的整句
// 歌词也要被逐字那份顶掉」。这两条搜索是并行的，兜底那条可能在逐字那条之前
// 落盘；它装出来的也是 `OriginSearch` 的话，用户挑的词和自动搜来的词就分不开，
// 「优先逐字」会在最需要它的那一场上输掉。
func TestWordTimingReplacesAnAutomaticallyFoundLyric(t *testing.T) {
	gate := make(chan struct{})
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引"})
	src.gate = gate
	src.doc = lyrics.Doc{
		LRC: "[00:01.00]千秋岁引",
		YRC: "[1000,2000](1000,1000,0)千(2000,1000,0)秋",
	}
	a, _ := searchApp(t, src)

	it := onlineSong("BV1", "1")
	it.Title = "千秋岁引"
	a.setLyricTrack(it)
	waitSearch(t, 2*time.Second, "网易云那一趟真的发出去了", func() bool {
		n, _ := src.count()
		return n == 1
	})

	a.PutLyricsFrom(lyrics.OriginAuto, "[00:01.00]兜底搜来的整句", "")
	close(gate)

	waitSearch(t, 2*time.Second, "逐字那份顶掉了兜底那份", func() bool {
		rows := a.Lyrics().Rows
		return len(rows) > 0 && len(rows[0].Words) > 0
	})
	if got := a.Lyrics().Origin; got != lyrics.OriginAuto {
		t.Errorf("来路 = %q，want %q", got, lyrics.OriginAuto)
	}
}

// TestACachedUserAdoptedLyricIsNotReplacedBeforeItLoads 盯同一件事的另一半时序：
//
// 用户挑的那份在**盘上**，读盘还在后台。搜索结果要是抢在它前面装，读盘那趟会
// 因为代次变了被丢掉——界面上就变成「搜来的词顶掉了用户挑的词」。所以装之前
// 要先看盘上那份的来路。
func TestACachedUserAdoptedLyricIsNotReplacedBeforeItLoads(t *testing.T) {
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引"})
	src.doc = lyrics.Doc{
		LRC: "[00:01.00]搜来的",
		YRC: "[1000,2000](1000,1000,0)搜(2000,1000,0)来",
	}
	a, c := searchApp(t, src)
	it := onlineSong("BV1", "1")
	it.Title = "千秋岁引"
	if err := c.Save(lyricKeyOf(it), lyrics.Entry{
		Lyrics: "[00:01.00]我挑的", Origin: lyrics.OriginSearch,
	}); err != nil {
		t.Fatal(err)
	}

	a.setLyricTrack(it)
	waitSearch(t, 2*time.Second, "缓存里的用户歌词读回来", func() bool {
		return a.Lyrics().HasLyrics
	})
	waitSearch(t, 2*time.Second, "网易云那一趟试过了", func() bool {
		_, p := src.count()
		return p == 1
	})
	staysFor(t, 300*time.Millisecond, "缓存里的用户歌词", func() bool {
		ls := a.Lyrics()
		return ls.HasLyrics && ls.Rows[0].Text == "我挑的" && ls.Origin == lyrics.OriginSearch
	})
}

// TestNeteaseYRCNeverReplacesTheLyricTheUserAdopted 钉住「用户挑的那份不换」：
//
// 哪怕网易云这一份确实有逐字，当前歌词的来路是 `OriginSearch` 时也不许顶掉。
func TestNeteaseYRCNeverReplacesTheLyricTheUserAdopted(t *testing.T) {
	gate := make(chan struct{})
	src := sourceOf(lyrics.SourceNetease, lyrics.Candidate{ID: "1", Title: "千秋岁引"})
	src.gate = gate
	src.doc = lyrics.Doc{
		LRC: "[00:01.00]搜来的",
		YRC: "[1000,2000](1000,1000,0)搜(2000,1000,0)来",
	}
	a, _ := searchApp(t, src)

	it := onlineSong("BV1", "1")
	it.Title = "千秋岁引"
	a.setLyricTrack(it)
	waitSearch(t, 2*time.Second, "网易云那一趟真的发出去了", func() bool {
		n, _ := src.count()
		return n == 1
	})

	// 用户在搜索弹窗里挑了一份。
	a.PutLyricsFrom(lyrics.OriginSearch, "[00:01.00]我挑的", "")
	close(gate)
	waitSearch(t, 2*time.Second, "候选拉回来", func() bool {
		_, p := src.count()
		return p == 1
	})

	staysFor(t, 300*time.Millisecond, "用户挑的那份", func() bool {
		ls := a.Lyrics()
		return ls.HasLyrics && ls.Rows[0].Text == "我挑的" && ls.Origin == lyrics.OriginSearch
	})
}
