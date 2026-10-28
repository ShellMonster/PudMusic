// 歌词搜索与预览那两个弹窗（internal/ui/lyricssearch.go）的验收。
//
// 这些测试跑在 MyGo 的 headless Tester 上：真布局、真绘制、真读像素，和真窗口
// 走的是同一套 layout 与 paint。
//
// # 三样东西是这一票的判据，而它们都不是「画出来了」
//
//   - **防抖**：用户每敲一个字那个源都被叫到一次，B 站之外的两个源也经不起这个。
//     判据是**源被叫了几次**（数调用次数），不是「界面上出现了那句话」——
//     防抖开着和关着，界面上那句话一个像素都不差。
//   - **失败之后不许每帧重发**：那一支自己养自己（生产里每次投递都叫醒下一帧），
//     症状是「拉不到」两个字来回闪。同样的判据。
//   - **行身份**：铁律四那条「行键是候选的 ID、不是下标」。这一条在界面上
//     **看不出任何异样**（每一行的字都是对的），所以只能量「把同一批候选打乱
//     之后重新搜，某首的行键还是不是它自己那一个」——按下标认的话它换了位置
//     行键就跟着换。
package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 假源
// ─────────────────────────────────────────────────────────────────────────────

// fakeLyricSource 是一只可编排的歌词源：它按次序记下自己被搜过哪些词、数着预览
// 发了多少次，能按需要卡住、按需要失败。
//
// ⚠️ **必须数调用次数**：这一票最要紧的两条（「防抖」「失败之后不许每帧重发」）
// 判据都是「那个源被叫了几次」。只判「界面上出现了一样的候选」抓不住它们。
type fakeLyricSource struct {
	id, name string

	mu sync.Mutex
	// words 是每次 Search 的词，按发生的次序。
	words []string
	// previews 是 Preview 被调了几次。
	previews int
	// cands 是 Search 每次都返回的这份。
	cands []lyrics.Candidate
	// searchErr / previewErr 是那两次调用的返回值。nil 时正常。
	searchErr error
	// previewDoc / previewErr 是 Preview 的返回值。⚠️ **previewDoc 留空而
	// previewErr 是 nil 是一种要专门试的情况**（源返回「成功」而里面什么都没有）。
	previewDoc lyrics.Doc
	previewErr error
	// hold 是 Search 卡住多久。0 = 不卡（用来把界面停在「正在搜」那一支）。
	hold time.Duration
}

func (f *fakeLyricSource) ID() string   { return f.id }
func (f *fakeLyricSource) Name() string { return f.name }

func (f *fakeLyricSource) Search(_ context.Context, q string) ([]lyrics.Candidate, error) {
	f.mu.Lock()
	f.words = append(f.words, q)
	hold, err, cands := f.hold, f.searchErr, f.cands
	f.mu.Unlock()

	if hold > 0 {
		time.Sleep(hold)
	}
	if err != nil {
		return nil, err
	}
	return cands, nil
}

func (f *fakeLyricSource) Preview(_ context.Context, _ lyrics.Candidate) (lyrics.Doc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews++
	return f.previewDoc, f.previewErr
}

func (f *fakeLyricSource) searched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.words...)
}

func (f *fakeLyricSource) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.words)
}

func (f *fakeLyricSource) previewsOf() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previews
}

// setCands 换掉 Search 每次返回的那一批（界面线程改，后台读，读的时候拿锁）。
func (f *fakeLyricSource) setCands(cands []lyrics.Candidate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cands = cands
}

// neteaseSource 是一只「搜 → 只有歌名、要另拉歌词」的源（网易云那种）。
func neteaseSource(cands ...lyrics.Candidate) *fakeLyricSource {
	return &fakeLyricSource{id: lyrics.SourceNetease, name: "网易云", cands: cands}
}

// lrclibSource 是一只「歌词就在搜索结果里」的源（LRCLib 那种）。
func lrclibSource(cands ...lyrics.Candidate) *fakeLyricSource {
	return &fakeLyricSource{id: lyrics.SourceLRCLib, name: "LrcLib", cands: cands}
}

// cand 造一条候选，省得每处都写全五个字段。
func cand(id, title string) lyrics.Candidate {
	return lyrics.Candidate{
		ID: id, Title: title, Artist: "歌手" + id, Album: "专辑" + id,
		DurationMS: 214_000,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 宿主
// ─────────────────────────────────────────────────────────────────────────────

// lyricSearchShell 造一个停在迷你播放器上、正放着一首歌、挂着假源的外壳 +
// Tester。
//
// ⚠️ **假时钟是这里的关键**：防抖那 500ms 要是真等，一条测试就干等半秒，而
// 「敲完字等 500ms」用 `advance` 是一行。
func lyricSearchShell(t *testing.T, srcs ...lyrics.Source) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	s.clock = newFakeClock(now)
	s.app.SetWake(s.wake.invalidate)
	// 缓存挂在真的临时目录上：采用那一下要真的写盘，这一票才验得到「写进缓存」。
	s.app.MountLyrics(lyrics.NewCache(writableTempDir(t, "lyrics")))
	s.app.TestOnlyNoAutoLyrics()
	s.app.MountLyricSources(srcs...)
	openATrack(t, s)
	s.app.Pause()
	s.navigate(miniPlayerPath)
	tt.Frame()
	// 收掉那个 500ms 防抖的钟（理由同 librarypage_test.go 里的那一处）。
	t.Cleanup(func() {
		if s.searchWatchStop != nil {
			s.searchWatchStop()
			s.searchWatchStop = nil
		}
	})
	return s, tt
}

// openTheSearchDialog 从右下角那个旋钮进搜索弹窗——**走真的那一下点击**，
// 而不是直接调 `openLyricSearch`。
//
// ⚠️ 「没有歌词的时候那个旋钮也在」本身就是这一票要验的一条：用户点它的时机
// 恰恰是没有歌词的时候。
func openTheSearchDialog(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click(lyricSearchKnobLabel); err != nil {
		t.Fatalf("右下角没有 %q 那个旋钮：%v（文字有 %q）",
			lyricSearchKnobLabel, err, tt.Texts())
	}
	tt.Frame()
}

// focusLyricKeyword 点一下搜索框把它放到焦点上（不敲字）。
//
// ⚠️ 回车/提交只发给**有焦点**的那个输入框，所以「按一下回车提交」那几条必须先
// 点一下它——`Tester.TypeKey` 不挑接收方。
func focusLyricKeyword(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	box, ok := tt.Find(lyricSearchBoxLabel)
	if !ok {
		t.Fatalf("搜索框没画出来，文字有 %q", tt.Texts())
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
}

// typeLyricKeyword 在搜索弹窗的关键词框里敲字（走真实的点击 + 真实输入）。
func typeLyricKeyword(t *testing.T, tt *mygoui.Tester, text string) {
	t.Helper()
	focusLyricKeyword(t, tt)
	tt.Type(text)
	tt.Frame()
}

// ─────────────────────────────────────────────────────────────────────────────
// 一眼看得到的那几条
// ─────────────────────────────────────────────────────────────────────────────

// TestNoSourcesMeansTheSearchDialogNeverOpens 钉住「一个源都没挂时不许开弹窗」。
//
// 开着的弹窗里用户输完词、按下搜索，看到的会是一句「没有歌词源」——而那不是他
// 做了什么导致的。这跟「提交那一下什么都不会发生」是同一件。
func TestNoSourcesMeansTheSearchDialogNeverOpens(t *testing.T) {
	s, tt := lyricSearchShell(t)

	openTheSearchDialog(t, tt)

	if tt.HasText(lyricSearchHeading) {
		t.Fatalf("一个源都没挂，搜索弹窗还是开了：文字有 %q", tt.Texts())
	}
	if s.lyricSearch.open {
		t.Error("壳上那一摊还是开着的")
	}
	// 也不能默默吞掉：用户按了那个旋钮，界面上得有一句话告诉他为什么。
	if !tt.HasText("歌词源") {
		t.Errorf("没有一句交代为什么开不了，文字有 %q", tt.Texts())
	}
}

// TestTheSearchDialogListsBothSourcesAndStartsFromTheSongOnScreen 钉住弹窗本身。
//
// 上游（`lyrics-search-modal/index.tsx`）的三件事：顶上写着「搜索歌词」、Tab 上
// 摆着两个源、**框里先填屏幕上正在放的那首歌**（`setKeyword(playItem.title)`）并
// 立刻搜它一次——用户多半就是冲着「正在放的这首没有歌词」才点进来的。
func TestTheSearchDialogListsBothSourcesAndStartsFromTheSongOnScreen(t *testing.T) {
	netease, lrclib := neteaseSource(cand("1", "候选甲")), lrclibSource()
	s, tt := lyricSearchShell(t, netease, lrclib)

	// 先钉前半句：没有歌词时那个旋钮也在（此刻屏幕上正是「暂无歌词」）。
	if !tt.HasText(noLyrics.Title) {
		t.Fatalf("这一支该是「暂无歌词」，文字有 %q", tt.Texts())
	}
	openTheSearchDialog(t, tt)

	if !tt.HasText(lyricSearchHeading) {
		t.Errorf("弹窗顶上没有 %q，文字有 %q", lyricSearchHeading, tt.Texts())
	}
	for _, name := range []string{"网易云", "LrcLib"} {
		if !tt.HasText(name) {
			t.Errorf("Tab 上没有 %q，文字有 %q", name, tt.Texts())
		}
	}
	// 框里填的是屏幕上那一首。
	want := s.app.Snapshot().Title
	if want == "" {
		t.Fatal("测试素材那一首没有歌名，这一票验不了「从屏幕上的歌开始」")
	}
	if got := s.lyricSearch.draft; got != want {
		t.Errorf("框里是 %q，want 屏幕上那一首 %q", got, want)
	}
	// 而且**立刻**按它搜了一次（上游那个 useEffect([isOpen]) → handleSubmit）。
	waitFrames(t, tt, "把开机那一次搜索发出去", func() bool { return netease.calls() == 1 })
	if got := netease.searched(); got[0] != want {
		t.Errorf("第一次搜的是 %q，want 屏幕上那一首 %q", got[0], want)
	}
}

// TestSwitchingTheTabSearchesThatSourceOnItsOwn 钉住「切 Tab 立刻用同一个词
// 重搜那个源」。
//
// ⚠️ 少了它用户切到另一个源看到的是**上一个源的候选**——而两个源的候选连 id 的
// 取值范围都不一样（网易云是歌曲 id，LRCLib 是它的数字 id），界面上一个字也
// 看不出来那是哪来的。
func TestSwitchingTheTabSearchesThatSourceOnItsOwn(t *testing.T) {
	netease, lrclib := neteaseSource(cand("1", "候选甲")), lrclibSource(cand("9001", "候选乙"))
	_, tt := lyricSearchShell(t, netease, lrclib)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "第一个源搜完", func() bool { return netease.calls() == 1 })
	before := lrclib.calls()

	if err := tt.Click("LrcLib"); err != nil {
		t.Fatalf("切不到 LrcLib 那个 Tab：%v（文字有 %q）", err, tt.Texts())
	}
	waitFrames(t, tt, "第二个源自己搜一遍", func() bool { return lrclib.calls() == before+1 })

	if got := lrclib.searched(); got[len(got)-1] != netease.searched()[0] {
		t.Errorf("切过去搜的是 %q，want 框里那个词 %q", got[len(got)-1], netease.searched()[0])
	}
	if n := netease.calls(); n != 1 {
		t.Errorf("切 Tab 又把第一个源叫了一遍（共 %d 次）", n)
	}
	// 候选表摆的是**这一个源**的。
	if !tt.HasText("候选乙") || tt.HasText("候选甲") {
		t.Errorf("切过去之后摆的不是这个源的候选，文字有 %q", tt.Texts())
	}
}

// TestPreviewThenAdoptPutsTheLyricsOnScreen 是这一票的主线（走通一遍）。
//
// 上游那一串是 `handleSelect` → 预览 → `onAdoptLyrics` → 写进
// `${bvid}-${cid}` 那一条 → 界面上换上去。我们这边最后两段是
// `app.AdoptLyricPreview`，判据是**歌词真的上了屏幕**和**两个弹窗一起关了**。
func TestPreviewThenAdoptPutsTheLyricsOnScreen(t *testing.T) {
	const (
		lrc   = "[00:00.00]第一句\n[00:05.00]第二句"
		trans = "[00:00.00]first line\n[00:05.00]second line"
	)
	netease := neteaseSource(cand("1", "候选甲"))
	netease.previewDoc = lyrics.Doc{LRC: lrc, Translated: trans}
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把候选搜出来", func() bool { return tt.HasText("候选甲") })

	// 点一条候选 → 预览弹窗，看得到完整歌词。
	if err := tt.Click("候选甲"); err != nil {
		t.Fatalf("点不到那一条候选：%v（文字有 %q）", err, tt.Texts())
	}
	waitFrames(t, tt, "把这一条的歌词拉到预览里", func() bool {
		return tt.HasText("第一句") && tt.HasText("第二句")
	})
	if netease.previewsOf() != 1 {
		t.Errorf("预览发了 %d 次请求，want 1", netease.previewsOf())
	}
	// 译文那一个 Tab 在，因为这一条有译文。
	if !tt.HasText(lyricPreviewTransTab) {
		t.Errorf("有译文却没有 %q 那个 Tab，文字有 %q", lyricPreviewTransTab, tt.Texts())
	}

	// 采用 → 两个弹窗一起关，歌词上了屏幕。
	if err := tt.Click(lyricPreviewAdopt); err != nil {
		t.Fatalf("点不到 %q：%v（文字有 %q）", lyricPreviewAdopt, err, tt.Texts())
	}
	waitFrames(t, tt, "两个弹窗一起关掉", func() bool {
		return !tt.HasText(lyricSearchHeading) && !tt.HasText("候选甲")
	})

	rows := s.app.Lyrics().Rows
	if len(rows) != 2 {
		t.Fatalf("采用之后歌词区有 %d 句，want 2（%+v）", len(rows), rows)
	}
	if rows[0].Text != "第一句" || rows[1].Text != "第二句" {
		t.Errorf("上去的是 %q / %q，want 第一句 / 第二句", rows[0].Text, rows[1].Text)
	}
	// 译文也一起上去了（`LyricsSnapshot.Rows` 是叠好的）。
	if rows[0].Translation != "first line" {
		t.Errorf("译文没跟着上：%q", rows[0].Translation)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 假时钟：500ms 防抖
// ─────────────────────────────────────────────────────────────────────────────

// TestTheKeywordIsDebouncedBy500ms 钉住「别在输入时每一笔都发」。
//
// ⚠️ 判据是**源被叫了几次**：防抖开着和关着，界面上那句话一个像素都不差。
//
// ## 两个字之间**必须隔开时间**，否则这一条验不到它要验的那件事
//
// 它要钉的是「每敲一个字都**重新起算**那 500ms」（上游 `useDebounceFn(wait: 500)`
// 就是每个输入都重置那个钟）。而「从第一个字算起」和「从第二个字算起」**只有在
// 两个字之间过了时间时才分得开**：紧挨着敲两下的时候，两种钟都在同一刻起算，
// 后面量到的 400ms / 600ms 对它们一模一样——一个不重新起算的实现也照样绿。
//
// 从第一个字算起的症状正是这一条要挡的那一个：用户慢慢打的时候，会在**中途**就
// 发出去一个还没输完的词。所以下面是 300ms → 敲 → 300ms → 200ms 这个排法：
// 第二次量的时候离**第一个**字已经 600ms（早就该发了），离**第二个**字才 300ms。
//
// ⚠️ **正的等、负的也等，但等的是两样不同的东西**（理由在 `callsStayAt` 上）：
// 这一发是后台 goroutine 发的，而一帧画完不等于那一趟已经跑到 `src.Search`。
// 正的用 `waitFor`（赌「还没跑到」），负的用 `callsStayAt` 给一个真实的窗口
// （赌「来不及发」）。两处都写成「画完那一帧立刻读」的话，这一条既会在满载的
// 机器上偶发红，**又**在防抖真的坏掉时照样绿——而后者是更要紧的那一半。
func TestTheKeywordIsDebouncedBy500ms(t *testing.T) {
	netease := neteaseSource()
	s, tt := lyricSearchShell(t, netease)
	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把开机那一次搜索发出去", func() bool { return netease.calls() == 1 })

	// 第一个字起算。300ms 之内不许发。
	typeLyricKeyword(t, tt, "周")
	if n := netease.calls(); n != 1 {
		t.Fatalf("敲完第一个字就发了（共 %d 次）", n)
	}
	advance(s, 300*time.Millisecond)
	tt.Frame()
	callsStayAt(t, netease, 1, lyricSettle)

	// 第二个字：**重新起算**。从这一刻起 300ms 之内仍然不许发——而从第一个字算
	// 起的话，此刻已经过去 600ms，那一趟早该发出去了。
	tt.Type("杰")
	tt.Frame()
	advance(s, 300*time.Millisecond)
	tt.Frame()
	// ⚠️ 这一步是整条测试的**判据本体**，所以不能只是「读一眼没变」：提前发的那
	// 一趟是后台发出去的，机器一忙它就还没被调度，于是读出来还是 1，防抖坏没坏
	// 根本判不出来。见 `callsStayAt`。
	callsStayAt(t, netease, 1, lyricSettle)

	// 离第二个字满 500ms 了，它**必须**已经发出去。
	advance(s, 200*time.Millisecond)
	tt.Frame()
	waitFor(t, "离第二个字满 500ms 真的发了那一次", func() bool { return netease.calls() == 2 })
	// 框里此刻是「屏幕上的那首歌 + 周杰」（开弹窗时框里已经填了那首歌），所以
	// 判据是**后缀**：这一条验的是「重新起算」，不是搜的是哪几个字。
	if got := netease.searched()[1]; !strings.HasSuffix(got, "周杰") {
		t.Errorf("发出去的是 %q，want 以「周杰」结尾（框里之前那首歌名还在前面）", got)
	}
}

// lyricSettle 是「等一个后台 goroutine 跑一下」的窗口。
//
// ⚠️ 它**只用在负的断言上**（「这 N 毫秒之内不许发生」），而那一类断言越慢的
// 机器越准——所以它在满载的 CI 上不会引入新的红。方向和基座禁掉的那一类正相反：
// 那一类赌的是「N 毫秒之内**必须**发生」，慢机器必红。
//
// 100ms 足够一次 goroutine 被调度 + 走到 `src.Search`（那之后就只差一次加锁）。
const lyricSettle = 100 * time.Millisecond

// callsStayAt 等一小段真实时间，确认源**没有**被多叫。
//
// # 为什么负的断言要等，而正的断言用 `waitFor`
//
// 请求是**后台 goroutine** 发的（`app.SearchLyrics` 立刻返回，见
// `applyLyricsSearch` 的注释），而一帧画完不等于那一趟已经跑到 `src.Search`。
// 于是两种「立刻读」各自错在一边，而且**都是往「绿」那边错**：
//
//	「必须已经发了」  立刻读 = 赌调度已经跑到了。满载时那一趟还在排队，于是红成
//	                「防抖没生效」，而防抖好好的（这是偶发红的那一半）
//	「还不许发」     立刻读 = 赌它**来不及**发。真提前发了而那一趟还没被调度，
//	                这一条照样绿 —— 判据是空的，防抖坏掉根本测不出来（这是
//	                「测了等于没测」的那一半，比偶发红更坏）
//
// 所以：正的用基座的 `waitFor`（它等的就是「后台 goroutine 直接写的状态」），
// 负的在这里给一个真实的窗口。真没发的话多等一会儿还是没有；发过了就一定被看见。
func callsStayAt(t *testing.T, src *fakeLyricSource, want int, window time.Duration) {
	t.Helper()
	deadline := time.Now().Add(window)
	for {
		if got := src.calls(); got != want {
			t.Fatalf("这 %v 之内源被叫了 %d 次，want %d", window, got, want)
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// TestEnterDoesNotWaitForTheDebounce 钉住「回车不等那 500ms」。
//
// 上游也是 `onKeyDown Enter` 直接调 `handleSubmit`：用户按下回车就是想现在搜，
// 让他等半秒是另一回事。
//
// ⚠️ **判据是「这一次搜索真的发出去了」，不是「按回车之后跑一帧」**：请求是
// 后台发的（铁律三），而 `waitFrames` 的注释写明了「同一帧里画出来的还是上一帧
// 的样子」。原来那一句 `tt.Frame()` + 立刻断言，在机器一满载时搜索请求还在路上，
// 测试就红——而功能是好的：这一条实测在整包连跑里飘过一次，**在未改动的 main
// 上也飘**。判据一个字没放宽：要的还是「第二次搜索发出去了」，只是等到了再断。
func TestEnterDoesNotWaitForTheDebounce(t *testing.T) {
	netease := neteaseSource()
	_, tt := lyricSearchShell(t, netease)
	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把开机那一次搜索发出去", func() bool { return netease.calls() == 1 })

	typeLyricKeyword(t, tt, "林俊杰")
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	// ⚠️ 取 main 那一份（`waitFrames`），不是本票写的 `waitFor`：两边等的是**同一个
	// 条件**（第二次搜索发出去了），而上游已经因为同一个原因改过这一条了
	//（`app.SearchLyrics` 立刻返回，请求是后台发的，而画完那一帧不等于它已经到源那儿）。
	// 留上游那一份，将来上游再动这个文件时这里不会又撞一次。
	waitFrames(t, tt, "回车之后不等防抖就发出去了", func() bool {
		return netease.calls() == 2
	})
	if got := netease.searched()[1]; !strings.HasSuffix(got, "林俊杰") {
		t.Errorf("回车之后发出去的是 %q，want 以「林俊杰」结尾", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 负路径：失败不许静默，也不许每帧重发
// ─────────────────────────────────────────────────────────────────────────────

// TestSubmittingTheSameWordAgainWhileItIsStillRunningStillShowsTheCandidates 是
// 「慢网络下用户再按一下试试」那一条的界面那一半。
//
// 🔴 少了它，界面上那个症状是**无解**的：一条候选、一个错误、一个重试按钮都没有，
// 用户能做的只有关掉弹窗重开一次（而重开时如果网络还慢，就再卡一次）。
//
// 根因在 `app.SearchLyrics`（`TestSubmittingTheSameWordAgainWhileItIsStillRunningDoesNotStrandIt`
// 钉的是那一条），这一条钉的是「界面上不会卡成那样」——两半都要有：只有那一条，
// 界面上坏成什么样没人知道；只有这一条，根因改了没有也说不清。
//
// ⚠️ 按的是**回车**。那一条走的是 `lyricsQueryChanged(now, true)`；「搜索」按钮是
// 同一个入口（`lyricsQueryChanged(now, true)` 里 `submitted` 的另一个来源），所以
// 两条路共用同一个根因。
func TestSubmittingTheSameWordAgainWhileItIsStillRunningStillShowsTheCandidates(t *testing.T) {
	netease := neteaseSource(cand("1", "候选甲"))
	netease.hold = 600 * time.Millisecond
	s, tt := lyricSearchShell(t, netease)

	// 弹窗一打开就自动按屏幕上那首歌搜一次（上游 `useEffect([isOpen])`）。
	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把开机那一次搜索发出去", func() bool { return netease.calls() == 1 })

	// 慢网络下用户「再按一下试试」：框里一个字没改，提交的还是同一个词、同一个源。
	focusLyricKeyword(t, tt)
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()
	if n := netease.calls(); n != 1 {
		t.Fatalf("同一个词重复提交发了 %d 次请求（want 1）", n)
	}

	// 等那一趟回来，**再多走十帧**——卡住的话这一段就是它露馅的地方。
	waitFrames(t, tt, "这一趟落地", func() bool { return !s.app.LyricSearch().Loading })
	for range 10 {
		tt.Frame()
	}

	if s.app.LyricSearch().Loading {
		t.Fatalf("又按了一次回车之后卡在「正在搜…」：%+v，文字有 %q",
			s.app.LyricSearch(), tt.Texts())
	}
	if !tt.HasText("候选甲") {
		t.Errorf("候选没有出来，文字有 %q", tt.Texts())
	}
	if tt.HasText("正在搜") {
		t.Errorf("状态行还停在「正在搜…」，文字有 %q", tt.Texts())
	}
}

// TestAFailedSearchSaysItIsAFailureAndDoesNotFireAgain 是这一票最容易踩的一个坑。
//
// `app.failLocked` 失败时**留着** `Query`，界面这边的 `draft == applied` 那一支
// 就每一帧都判它「已经搜过了」。少了它：`now - 零值`（一大截）恒大于防抖窗口，
// 于是下一帧立刻又发一次、再失败、再发——而生产里每次投递都叫醒下一帧，那个
// 循环**自己养自己**（实测 20 帧打了 22 次），界面上只剩「拉不到」两个字来回闪。
func TestAFailedSearchSaysItIsAFailureAndDoesNotFireAgain(t *testing.T) {
	netease := neteaseSource()
	netease.searchErr = errors.New("dial tcp: 网络不通")
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把那次搜索发出去并看到失败", func() bool {
		return s.app.LyricSearch().Err != ""
	})

	for range 20 {
		tt.Frame()
	}
	if n := netease.calls(); n != 1 {
		t.Fatalf("失败之后又发了 %d 次（want 1）——那个循环自己养自己", n)
	}
	if s.lyricSearch.pendingSince.IsZero() == false {
		t.Error("失败之后 `pendingSince` 还留着，防抖的钟还醒着")
	}
	// 而且界面上得有一句交代（不是一片空白）。
	if !tt.HasText("网络不通") {
		t.Errorf("失败之后界面上没有交代，文字有 %q", tt.Texts())
	}
}

// TestAFailedSearchOffersARetryAndSaysItIsAFailure 钉住「失败那一支带一个按钮」。
//
// 不带的话用户能做的只有把窗口关掉再打开一次，而那和「拉不到」是一件事。
func TestAFailedSearchOffersARetryAndSaysItIsAFailure(t *testing.T) {
	netease := neteaseSource()
	netease.searchErr = errors.New("dial tcp: 网络不通")
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "看到那一次失败", func() bool { return s.app.LyricSearch().Err != "" })

	if _, ok := tt.Find(lyricSearchRetryLabel); !ok {
		t.Fatalf("失败之后没有 %q 那个按钮，文字有 %q", lyricSearchRetryLabel, tt.Texts())
	}
	// 而且要点名是哪个源：面板上此刻摆着两个 Tab，只说「搜不到」的话用户不知道
	// 该换哪一个再试。
	if !tt.HasText("网易云") {
		t.Errorf("失败那句话没有点名是哪个源，文字有 %q", tt.Texts())
	}
	// 🔴 **状态行不许说「没搜到这一首」**：那件事要用户换个词或者换个源，而这一件要他
	// 再试一次。两句话挨在屏幕上互相打架，用户读到的是界面自己前后不一致。
	if tt.HasText("没搜到这一首") {
		t.Errorf("失败时候选区顶上说的是「没搜到这一首」，和下面那句「网络不通」打架：文字有 %q",
			tt.Texts())
	}
	if !tt.HasText("结果拉不到") {
		t.Errorf("失败时状态行没有说「结果拉不到」，文字有 %q", tt.Texts())
	}
}

// TestTheRetryButtonSearchesThatWordAgain 钉住「重试按钮真的再发一次」。
//
// ⚠️ 重搜的是 `app` 留着的那个 `Query`，**不是框里此刻的字**——用户敲到一半按
// 重试就是搜一个他还没输完的词。
func TestTheRetryButtonSearchesThatWordAgain(t *testing.T) {
	netease := neteaseSource()
	netease.searchErr = errors.New("dial tcp: 网络不通")
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "看到那一次失败", func() bool { return s.app.LyricSearch().Err != "" })
	want := s.app.LyricSearch().Query

	if err := tt.Click(lyricSearchRetryLabel); err != nil {
		t.Fatalf("点不到 %q：%v（文字有 %q）", lyricSearchRetryLabel, err, tt.Texts())
	}
	waitFrames(t, tt, "重试真的又发了一次", func() bool { return netease.calls() == 2 })
	if got := netease.searched()[1]; got != want {
		t.Errorf("重试搜的是 %q，want 刚才失败的那个词 %q", got, want)
	}
}

// TestThePreviewSaysItIsNotAFetchFailure 钉住预览里那**第三种**「没有」。
//
// 「源返回了成功而正文是空的」既不是「拉不到」也不是「这一条没有歌词」。摆成
// 「网络不通」的话用户会去反复重试，而这一条重试一万次也不会有歌词；摆成一片
// 空白的话那句话和「加载中」一样是在搪塞。
func TestThePreviewSaysItIsNotAFetchFailure(t *testing.T) {
	netease := neteaseSource(cand("1", "候选甲"))
	// ⚠️ **err 是 nil、正文是空的**：那正是源返回「成功」而里面什么都没有。
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把候选搜出来", func() bool { return tt.HasText("候选甲") })
	if err := tt.Click("候选甲"); err != nil {
		t.Fatalf("点不到那一条候选：%v", err)
	}
	waitFrames(t, tt, "看到预览里那一句", func() bool { return s.app.LyricPreview().Err != "" })

	if !tt.HasText("未找到歌词内容") {
		t.Errorf("预览里没有「未找到歌词内容」，文字有 %q", tt.Texts())
	}
	if tt.HasText("网络不通") {
		t.Errorf("这一条被说成了「网络不通」，而它重试一万次也不会有歌词：文字有 %q", tt.Texts())
	}
	// 「采用歌词」得是禁着的：按下去不会有任何事发生，而那种按钮在界面上和坏了
	// 的按钮长得一模一样。
	if err := tt.Click(lyricPreviewAdopt); err != nil {
		t.Fatalf("点不到 %q：%v（文字有 %q）", lyricPreviewAdopt, err, tt.Texts())
	}
	tt.Frame()
	if got := s.app.Lyrics().Rows; len(got) != 0 {
		t.Errorf("禁着的「采用歌词」还是被按下去了，歌词区有 %d 句", len(got))
	}
}

// TestAdoptingSaysSoWhenTheSongChanged 钉住「按了没成功要有一句交代」。
//
// 「采用」按下去有反应、但不是用户想要的那个反应（歌换了），是这一票里少数几个
// 这种情形之一。不说清楚的话用户看到的是「按了，什么也没发生」。
func TestAdoptingSaysSoWhenTheSongChanged(t *testing.T) {
	netease := neteaseSource(cand("1", "候选甲"))
	netease.previewDoc = lyrics.Doc{LRC: "[00:00.00]只有一句"}
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把候选搜出来", func() bool { return tt.HasText("候选甲") })
	if err := tt.Click("候选甲"); err != nil {
		t.Fatalf("点不到那一条候选：%v", err)
	}
	waitFrames(t, tt, "把预览拉出来", func() bool { return s.app.LyricPreview().Adoptable })

	// 换歌（底下那一层按缓存键挡着，采用会被拒）。
	openAnotherTrack(t, s)

	if err := tt.Click(lyricPreviewAdopt); err != nil {
		t.Fatalf("点不到 %q：%v", lyricPreviewAdopt, err)
	}
	tt.Frame()
	if !tt.HasText("歌已经换了") {
		t.Errorf("换歌之后采用没有交代，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 铁律四：行身份
// ─────────────────────────────────────────────────────────────────────────────

// TestClosingTheSearchDialogTakesThePreviewWithIt 钉住「关掉搜索弹窗时预览一起收」。
//
// 预览开着这件事住在应用层，而搜索弹窗关掉之后界面上已经没有一个弹窗罩着它了
// ——下一次打开搜索弹窗就会看到上一次那一份没看完的歌词，而用户完全不知道它是
// 从哪来的。
func TestClosingTheSearchDialogTakesThePreviewWithIt(t *testing.T) {
	netease := neteaseSource(cand("1", "候选甲"))
	netease.previewDoc = lyrics.Doc{LRC: "[00:00.00]预览里的那一句"}
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把候选搜出来", func() bool { return tt.HasText("候选甲") })
	if err := tt.Click("候选甲"); err != nil {
		t.Fatalf("点不到那一条候选：%v", err)
	}
	waitFrames(t, tt, "把预览拉出来", func() bool { return s.app.LyricPreview().Open })

	// Esc 关掉最上面那个（预览）。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.app.LyricPreview().Open {
		t.Errorf("Esc 之后预览还开着，文字有 %q", tt.Texts())
	}
	// 再 Esc 关掉搜索弹窗；这一次预览本来就没开。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.lyricSearch.open {
		t.Errorf("Esc 之后搜索弹窗还开着，文字有 %q", tt.Texts())
	}

	// 重开一次：不能看到上一次那一份预览。
	openTheSearchDialog(t, tt)
	if s.app.LyricPreview().Open {
		t.Error("重新打开搜索弹窗时，上一次那一份预览还开着")
	}
	if tt.HasText("预览里的那一句") {
		t.Errorf("重新打开时看到了上一次的预览，文字有 %q", tt.Texts())
	}
}

// TestTheRowKeyFollowsTheSongAndNotItsPlace 钉住候选表的行键是**候选自己的 ID**。
//
// 🔴 这一条在界面上**看不出任何异样**（每一行的字都是对的），所以只能直接量
// `ListState.Key`。而判据也不能写成「键等于某个拼出来的字符串」——那一条对任何
// 一种一致的拼法都绿，包括拼法里其实用的是下标的那种。
//
// 所以这里量的是**它跟不跟歌走**：把同一批候选打乱之后重新搜一遍，某一首的行键
// 必须还是它自己那一个。按下标认的话，它换了位置行键就跟着换，于是滚动位置和
// 选中全部串到**别的歌**上，而用户永远查不出来。
func TestTheRowKeyFollowsTheSongAndNotItsPlace(t *testing.T) {
	first := []lyrics.Candidate{cand("11", "候选甲"), cand("22", "候选乙"), cand("33", "候选丙")}
	// 同一批歌，位置全换了一个。
	shuffled := []lyrics.Candidate{first[1], first[2], first[0]}
	netease := neteaseSource(first...)
	s, tt := lyricSearchShell(t, netease)

	openTheSearchDialog(t, tt)
	waitFrames(t, tt, "把第一批候选搜出来", func() bool { return tt.HasText("候选丙") })

	key := s.lyricSearch.ls.Key
	if key == nil {
		t.Fatal("候选表没有装行身份（ListState.Key 是 nil）")
	}
	before := []any{key(0), key(1), key(2)}
	for i, got := range before {
		if got == i {
			t.Fatalf("第 %d 行的行键就是它的下标 %d", i, i)
		}
		if w := lyricCandidateRowKey(lyrics.SourceNetease, first[i].ID); got != w {
			t.Errorf("第一批第 %d 行的行键 = %v，want %v", i, got, w)
		}
	}
	for i, got := range before {
		for j, other := range before {
			if i != j && got == other {
				t.Errorf("第一批第 %d 行和第 %d 行的行键重了：%v", i, j, got)
			}
		}
	}

	// 换一批位置（同一个源、换一个词 → 缓存不命中，真发一次请求）。
	netease.setCands(shuffled)
	s.applyLyricsSearch("另一首")
	waitFrames(t, tt, "把打乱之后的那一批搜出来", func() bool {
		return len(s.app.LyricSearch().Candidates) == 3 && !s.app.LyricSearch().Loading
	})

	// ⚠️ **要重新取一次**：`ls.Key` 每一帧都被重装成一个闭包，包的是**那一帧**的
	// 候选。拿着第一次那个不放，量的还是第一批——而那样这条断言就成了空转。
	key = s.lyricSearch.ls.Key

	// shuffled 的第 i 位是 first 的第 want 位，所以它的行键必须是 first[want] 的。
	want := []int{1, 2, 0}
	for i, w := range want {
		if got := key(i); got != before[w] {
			t.Errorf("打乱之后第 %d 行摆的是 %q，它的行键是 %v，want %q 自己的那一个 %v",
				i, shuffled[i].Title, got, first[w].Title, before[w])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个状态
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSearchBoxSaysThreeDifferentThings 钉住候选区那三句话。
//
// 「没搜过」「正在搜」「搜到了但是空的」是用户完全不同的三种处境。合成一句之后
// 用户看到的是「我明明还没搜，它就说我搜不到」，而空白和「加载中」在用户那边
// 也只是同一句话（都是「这个界面没在回答我」）。
func TestTheSearchBoxSaysThreeDifferentThings(t *testing.T) {
	netease := neteaseSource()
	s, tt := lyricSearchShell(t, netease)

	// 一、还没搜过。框被清干净、又没有搜过一次。
	s.lyricSearch.open = true
	s.applyLyricsSearch("")
	tt.Frame()
	if !tt.HasText(lyricSearchIdle) {
		t.Fatalf("没搜过时没有说 %q，文字有 %q", lyricSearchIdle, tt.Texts())
	}
	if tt.HasText(lyricSearchEmpty) {
		t.Errorf("没搜过却说 %q——那是两句完全不同的事", lyricSearchEmpty)
	}

	// 二、正在搜。源卡住，界面停在「正在搜」那一支。
	netease.hold = 300 * time.Millisecond
	s.lyricSearch.open = true
	s.applyLyricsSearch("晴天")
	tt.Frame()
	if !tt.HasText("正在") {
		t.Errorf("搜索中没有说「正在…」，文字有 %q", tt.Texts())
	}

	// 三、搜到了，但是空的。
	netease.mu.Lock()
	netease.hold, netease.searchErr = 0, nil
	netease.mu.Unlock()
	waitFrames(t, tt, "把这一次搜完", func() bool { return !s.app.LyricSearch().Loading })
	if !tt.HasText(lyricSearchEmpty) {
		t.Fatalf("搜到了空的没有说 %q，文字有 %q", lyricSearchEmpty, tt.Texts())
	}
	// 搜不到要给交代：不能是一片空白，也不能只是一句「没搜到」。
	if !tt.HasText(lyricNoHitsHint) {
		t.Errorf("「没搜到」没有给下一步，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────
