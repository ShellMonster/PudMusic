// 票 #244 的判据：设置页两端对齐（D1）、迷你窗音量滑块有悬停提示（B5）、
// 「全部播放」两处画法统一（B6）。
//
// 三条都对着**画面**断言：量的都是画面上那一块的位置和大小，不是「调了哪个
// 函数」。
//
// ⚠️ **深浅两套都要过**：设置页那一段空档在两套主题下是同一组数，而迷你窗那枚
// 提示是**反色底**（深浅两套反过来的），浅色下那一枚浅底深字、深色下深底浅字。
package ui

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// D1（设置页说明和控件之间的空档）已经不存在：设置页的每一行不再带说明文字，
// 自解释的标题加控件就够了。

// ─────────────────────────────────────────────────────────────────────────────
// B5：迷你窗那根音量滑块要有一枚看得见的悬停提示
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniVolumeSliderSaysWhatItIs 钉住「迷你窗那根音量滑块有悬停提示」。
//
// # 为什么这一条不是「加一句标签」就完了
//
// 那一扇窗只有 **320×100**，而音量滑块在**第一行**（y=14..30）。框架默认把提示
// 摆在锚点的**上面**——摆在上面的话整个提示落在窗口之外，**一个字都看不见**，
// 而「看不见的提示」和「没有提示」在画面上是一回事。
//
// # 判据是**提示在窗内**
//
// 光断言「画面上有『音量 80%』这几个字」不够：提示画在窗口外时，Tester 里的
// 文字列表照样有它（它是被建出来了的），而用户看不见。所以这一条同时断它的
// **位置**：上下左右都得在 320×100 之内。
func TestTheMiniVolumeSliderSaysWhatItIs(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.8)
	tt := miniTester(s)
	settleMini(t, tt, "音量读回来了", func() bool { return a.Snapshot().Volume > 0 })

	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	// 把指针停在那根滑块上。
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()

	want := "音量 80%"
	tip, ok := tt.Find(want)
	if !ok {
		t.Fatalf("音量滑块上没有 %q 那枚提示，文字有 %q", want, tt.Texts())
	}
	// ⚠️ **提示得在窗内**：这一扇窗只有 320×100，而音量滑块在第一行——摆在上面
	// 的话整个提示落在窗口外，用户一个字也看不见。
	if tip.X < 0 || tip.Y < 0 || tip.X+tip.W > miniWidth || tip.Y+tip.H > miniHeight {
		t.Errorf("那枚提示画在 %v，而迷你窗只有 %d×%d——它落在窗外，用户看不见",
			tip, miniWidth, miniHeight)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B6：「全部播放」两处画法统一
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayAllButtonIsTheSameOnBothPages 钉住「『全部播放』在两个页面上是
// 同一枚」。
//
// # 同一个动作，两个页面
//
// 搜索结果页有「全部播放」，此前**首页没有**——用户在搜索页能按、在首页找不到，
// 会以为是两个不同的功能（UX 报告 §2 的 B6）。
//
// # 判据是**两枚按钮的盒子一样**
//
// 同一个字、同一颗实心主色、同一枚控件 ⇒ 两处的盒子一模一样。而「一个页面有、
// 另一个没有」这件事本身也由这一条断掉：少一个就量不成一对。
//
// ⚠️ **不判语义**：队列语义由票 #337 的行为测试钉住，这一票只统一画法。
func TestThePlayAllButtonIsTheSameOnBothPages(t *testing.T) {
	// 首页：摆三首歌，那一枚按钮才是可按的。
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: playableSongsOf("a", 3)},
	})
	homeFrame(home.tt)
	homeBtn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}

	// 搜索结果页：同一个动作，那一枚按钮在排序那一排右边。
	st := newSearchStub(10, 20)
	_, searchTT := newSearchPage(t, "周杰伦", st)
	waitFor(t, "搜索结果出来了", func() bool {
		searchTT.Frame()
		return searchTT.HasText(playAllLabel)
	})
	searchBtn, ok := searchTT.Find(playAllLabel)
	if !ok {
		t.Fatalf("搜索页上没有 %q 那一枚，文字有 %q", playAllLabel, searchTT.Texts())
	}

	if homeBtn.W != searchBtn.W || homeBtn.H != searchBtn.H {
		t.Errorf("两处的 %q 不是同一枚：首页是 %v，搜索页是 %v——"+
			"同一个动作在两个页面上长得不一样，用户会以为是两个功能",
			playAllLabel, homeBtn, searchBtn)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B6 的另一半：两枚按钮按下去做的是同一件事
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayAllButtonReplacesQueueOnBothPages 验两个页面共用批量替换语义。
// 在线搜索走本地 HTTP fixture，起播与队列都检查。
func TestThePlayAllButtonReplacesQueueOnBothPages(t *testing.T) {
	t.Run("首页", func(t *testing.T) {
		home := newTestShell(t, withSettings(app.DefaultSettings("")))
		seedThreeSongs(t, home.a)
		page := localSongs(t, 3)
		home.s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: page}})
		homeFrame(home.tt)
		if err := home.tt.Click(playAllLabel); err != nil {
			t.Fatal(err)
		}
		assertQueueAndPlaying(t, home.a, []app.Item{page[0].Item, page[1].Item, page[2].Item}, 0)
	})
	t.Run("搜索", func(t *testing.T) {
		st := newSearchStub(24, 24)
		search, tt := newSearchPage(t, "周杰伦", st)
		mountQueueStreaming(t, search.app)
		settleText(t, tt, stubTitle(1, 0))
		seedThreeSongs(t, search.app)
		if err := tt.Click(playAllLabel); err != nil {
			t.Fatal(err)
		}
		want, _ := playAllItems(st.rows(searchTabSong, 1))
		assertQueueAndPlaying(t, search.app, want, 0)
	})
}

// playableSongsOf 造 n 首**放得了**的歌（首页那一屏）。
//
// 🔴 **Item 得是放得了的**：票 #244 之后「全部播放」会跳过放不了的那些
// （判据 `app.CanPlay`），而一个空的 `app.Item`（`Type` 是空串）在那个判据眼里
// 就是「没有可播的稿件」——拿它当夹具，这一枚按钮按下去什么也不会加。
func playableSongsOf(prefix string, n int) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		title := prefix + " 的歌 " + strconv.Itoa(i+1)
		out[i] = list.Music{
			ID:    prefix + "-" + strconv.Itoa(i),
			Title: title,
			Owner: prefix + " 的 UP 主",
			Item: app.Item{
				Source: app.SourceOnline,
				Type:   "mv",
				Bvid:   "BV1" + prefix + strconv.Itoa(i),
				Title:  title,
			},
		}
	}
	return out
}

// TestThePlayAllButtonSkipsTheUnplayableOnes 钉住「放不了的不要塞进队列」。
//
// # 为什么这一条要紧
//
// 首页「流行」那一档走音乐综合榜，而榜上**有些条目没有稿件**（纯音乐条目，
// `related_archive` 是空的，`internal/app/recommend.go`）——那种行**画得出来**，
// 但放不了。而同一页每一行的右键菜单对那种行是**灰**的（「立即播放 /
// 从这一首往后听 / 下一首播放 / 添加到队列」四个入口都按不动，末尾还补一句
// 为什么）。
//
// 「全部播放」是**整页一起换**的：它要是把放不了的也换进去，用户就会在队列里
// 撞上几首按不动的歌，而界面上一个字的解释都没有。
//
// # 判据是**队列里那几项**
//
// 放得了的那一首进去了，放不了的那一首没进去；而且**跳过了几首要说一声**
// （不然用户数不出「这一页有 2 首，而队列里换过去的是 1 首」）。
func TestThePlayAllButtonSkipsTheUnplayableOnes(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	// 队列先放一首。
	if err := home.s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatal(err)
	}
	// 本地音频夹具可实际起播；不可播条目必须被跳过并说明。
	playable := writeCopy(t, t.TempDir(), "放得了的.flac")
	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: {
			{ID: "a", Title: "放得了的", Item: app.LocalItem(playable)},
			// 🔴 **这一首放不了**：`Type` 是 mv 而 `Bvid` 是空串——榜上没有稿件
			// 的纯音乐条目就是这个样子（`app.playableOnline` 的最后那一支）。
			{ID: "b", Title: "放不了的", Item: app.Item{
				Source: app.SourceOnline, Type: "mv", Title: "放不了的",
			}},
		}},
	})
	homeFrame(home.tt)
	btn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}
	home.tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	home.tt.Frame()

	items := home.s.app.Queue().Items()
	if len(items) != 1 {
		t.Fatalf("队列里有 %d 项，want 1（只剩放得了的那一首）——"+
			"放不了的那一首不该被塞进队列", len(items))
	}
	if items[0].Title != "放得了的" {
		t.Errorf("队列第 1 项是 %q，want 「放得了的」", items[0].Title)
	}
	// 跳过了几首要说一声。
	if !home.tt.HasText("有 1 首放不了") {
		t.Errorf("没有说跳过了几首，文字有 %q", home.tt.Texts())
	}
}
