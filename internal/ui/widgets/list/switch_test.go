// 「数据显示样式」在 `widgets/list` 这一层的钉子：换摆法、网格那一份也有哨兵、
// 紧凑真的更矮。
//
// 这一层的测试量的是**控件自己的行为**（几列、行多高、哨兵在不在）；「每一页都
// 跟着设置变」那一条在 `internal/ui/displaymode_test.go`，量的是整页。
//
// 为什么要分开：这一层量得细（行高差几十像素、哨兵占第几格），而整页那一层要
// 穿过取数、路由、翻页——两件事各自的失败长得不一样，而混在一起的话报出来的错
// 永远是「这一页没跟着变」，追不到是哪一层的问题。

package list

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// newSwitch 造一对（列表 / 网格）并把 fetch 给分页那一层。
func newSwitch(t *testing.T, fetch Fetcher, v View) (*PagedList, *ui.Tester) {
	t.Helper()
	p := NewPaged(newCache(t), Actions{Notify: func(string) {}}, fetch)
	p.SetView(v)
	tt := ui.NewTester(p.View, testerW, testerH)
	tt.Frame()
	return p, tt
}

// spotOf 找某个元素在画面上的盒子。
func spotOf(tt *ui.Tester, s string) (ui.Rect, bool) {
	r, ok := tt.Find(s)
	if !ok || r.W <= 0 || r.H <= 0 {
		return ui.Rect{}, false
	}
	return r, true
}

// TestTheCompactRowsAreShorterThanTheNormalOnes 钉住「紧凑行高」这个档位本身。
//
// 量的是 **UP 名那一块的 x**（它在封面右边，封面小一号它就跟着往左靠），不是行与行
// 之间的距离：行距是框架量出来的平均值，会随歌名折行和主题字号动，而封面边长是这一
// 档里唯一一个「写了死的数」。
//
// ⚠️ **量 UP 名而不是歌名**：`ui.ListState` 给每一行挂一个无障碍标签（`List.View`
// 里的 `state.Label`，值就是歌名），而无头 Tester 的 `Find` 认这些标签——于是
// `Find(歌名)` 命中的是**整行**的盒子，x 永远是 0，两档量出来一模一样。UP 名只在
// 行内画，没有标签，不会撞上。
func TestTheCompactRowsAreShorterThanTheNormalOnes(t *testing.T) {
	songs := []Music{
		{ID: "c1", Title: "短歌名一", Owner: "甲 UP"},
		{ID: "c2", Title: "短歌名二", Owner: "乙 UP"},
	}
	const owner = "甲 UP"

	normal := New(newCache(t), Actions{})
	normal.SetView(ViewList)
	ttn := ui.NewTester(normal.View, testerW, testerH)

	compact := New(newCache(t), Actions{})
	compact.SetView(ViewCompact)
	ttc := ui.NewTester(compact.View, testerW, testerH)

	normal.SetSongs(songs)
	compact.SetSongs(songs)
	ttn.Frame()
	ttc.Frame()

	an, ok1 := spotOf(ttn, owner)
	ac, ok2 := spotOf(ttc, owner)
	if !ok1 || !ok2 {
		t.Fatalf("两档都得画出来：列表 %v 紧凑 %v", ok1, ok2)
	}
	if ac.X >= an.X {
		t.Fatalf("紧凑那一档的 UP 名没有往左靠（列表 x=%.0f、紧凑 x=%.0f）——"+
			"封面没缩，这一档等于没做", an.X, ac.X)
	}
	// 封面 48 → 32 是这一档省下来的高度里最大的一块，判据就是它：UP 名至少要往左
	// 让出这么多像素（实际让出的更多——序号和间距在紧凑那一档也一起收窄了）。
	if want := float32(coverSize - compactCoverSize); an.X-ac.X < want {
		t.Errorf("封面只缩了 %.0f 像素，want 至少 %d（%d → %d）",
			an.X-ac.X, int(want), coverSize, compactCoverSize)
	}
}

// TestEveryViewKeepsTheRowsAndThePagingWorking 钉住「换视图不丢东西」。
//
// 三档都要把六首歌画出来；**拉到底之后哨兵要收起来**（那是 `PagedList.syncList` 的
// 规矩：留着就是一行空白）。
//
// ⚠️ 这里曾经写着「哨兵是『滚到底就拉下一页』那个触发器，没有它分页列表永远翻不了
// 页」——**那句话是错的**（`afterFrame` 判的是「末尾那一行进没进画面」，而网格里最后
// 一行本身就是歌的行，滚到底照样命中）。哨兵真正负责的是「还有就拉时的那一句」和
// **失败之后那个「点此重试」按钮**（`blocked` 闩住的是自动重试，解开它的只有这个
// 按钮），钉住的是 `TestTheGridOffersTheRetryThatTheListOffers`。
//
// ⚠️ 而且这条的哨兵那一半**曾经是空判**：哨兵压根没画过时它也绿。另一条
// （`TestEveryViewShowsTheSentinelWhileThereIsMore`）钉的是反方向——哨兵**该**在的
// 时候三档都在。
func TestEveryViewKeepsTheRowsAndThePagingWorking(t *testing.T) {
	for _, v := range []View{ViewList, ViewCard, ViewCompact} {
		t.Run(v.String(), func(t *testing.T) {
			st := newStub().with(1, 6, false)
			p, tt := newSwitch(t, st.fetch, v)

			pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 6 })

			for i := range 6 {
				title := pageSongs(1, 6)[i].Title
				if _, ok := spotOf(tt, title); !ok {
					t.Fatalf("第 %d 首没画出来：%q", i+1, tt.Texts())
				}
			}
			// 拉到底了（`with(1, 6, false)`），所以哨兵**不该**在——那是
			// `syncList` 的规矩，留着就是一行空白。
			if _, ok := spotOf(tt, footerLabel); ok {
				t.Errorf("拉到底之后末尾哨兵还在：%q", tt.Texts())
			}
		})
	}
}

// TestEveryViewShowsTheSentinelWhileThereIsMore 钉住「哨兵该在的时候三档都在」。
//
// `SwitchView.SetFooter` 是**扇出到两份**的那一处，漏了哪一半只有这一条抓得到：
// 下面那些要么只量列表那半边（`PagedList` 的三重保障问的行数是按摆法算的，哨兵在
// 不在都翻得了），要么直接对 `*Grid` 调 `SetFooter` 绕过了这一层。
//
// ⚠️ 这是评审那一轮查出来的：**把 `SetFooter` 改成只装列表那半边，整包 18 个包全
// 绿**——多选（`TestTheBatchOpsStillWorkInEveryView` 走的是另一条出口）没红，翻页
// 没红，重试那条红的是 `Grid` 那半边而不是这里。
func TestEveryViewShowsTheSentinelWhileThereIsMore(t *testing.T) {
	const marker = "还有更多"
	for _, v := range []View{ViewList, ViewCard, ViewCompact} {
		t.Run(v.String(), func(t *testing.T) {
			sv := NewSwitchView(newCache(t), Actions{})
			sv.SetView(v)
			sv.SetSongs(pageSongs(1, 4))
			// 直接装一个自己的哨兵，绕开 `PagedList.syncList` 的「还有没有」——这一条
			// 量的是**扇出**（三档是不是都收到了），不是翻页列表的规矩。
			sv.SetFooter(func(c *ui.Context) { ui.Text(c, marker) })

			tt := ui.NewTester(sv.View, testerW, testerH)
			tt.Frame()

			if _, ok := spotOf(tt, marker); !ok {
				t.Errorf("%s 那一档看不见哨兵（%q）——`SwitchView.SetFooter` 没有扇出到"+
					"这一半。文字有 %q", v, marker, tt.Texts())
			}
		})
	}
}

// TestTheGridCountsTheSentinelAsARow 钉住「网格把哨兵算成了一行」。
//
// ⚠️ 这里量的是**行数**而不是「哨兵那一格画在屏幕上」：`ui.List` 是虚拟的，只建
// 看得见的那几行（`mygo/ui/list.go` 的 listOverscan），四十首歌在网格里是二十几行、
// 一屏放不下，于是哨兵压根没被建过——「屏幕上没有哨兵」是真的，但和「哨兵不存在」
// 是两件事。
func TestTheGridCountsTheSentinelAsARow(t *testing.T) {
	g := NewGrid(newCache(t), Actions{})
	g.SetSongs([]Music{{ID: "g1"}, {ID: "g2"}, {ID: "g3"}, {ID: "g4"}})

	tt := ui.NewTester(g.View, testerW, testerH)
	tt.Frame() // 画一帧：列数是量了容器宽才算出来的

	before := g.rows()
	g.SetFooter(func(c *ui.Context) { ui.Text(c, footerLabel) })
	tt.Frame()

	if got := g.rows(); got != before+1 {
		t.Errorf("装上哨兵之后网格的行数是 %d，之前是 %d——哨兵没有占一行",
			got, before)
	}
}

// TestTheGridKeepsPagingWorking 钉住「切到网格之后照样翻得了页」。
//
// ## 这一条**不是**靠哨兵成立的（这一点量过，别写成靠它）
//
// 翻页的触发判据是 `afterFrame` 里那句「末尾那一行进没进画面」（`last < n-1` 就
// 什么都不做）。网格里**最后一行本身就是歌的行**，滚到底照样命中——把网格的哨兵
// 拿掉（`SwitchView.SetFooter` 只装列表那半边），这一条和整页那一层
// （`TestTheGridStillPagesOnARealPage`）都照样绿。所以哨兵**不是**翻页的前提，
// 早先那句「没有哨兵就永远翻不了页」是错的。
//
// 哨兵真正负责的是下一条（失败之后的那条出路）和「正在加载…」那一句。
func TestTheGridKeepsPagingWorking(t *testing.T) {
	st := newStub().with(1, 40, true).with(2, 20, false)
	p, tt := newSwitch(t, st.fetch, ViewCard)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 40 })

	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第二页落地", func() bool {
		return p.Paged().Loaded == 60 && !p.Paged().Loading
	})
}

// TestTheSentinelShowsEvenWhenItLandsMidRow 钉住「哨兵落在某一行的中间格时也画」。
//
// 哨兵排在第 `len(songs)` 位，而行是按列数切出来的——**歌数除不尽列数时哨兵就在某一
// 行的中间格**，不是第一格。第一版只判了「整行从哨兵开始」那一支，中间那一支漏了，
// 于是网格里看不见「正在加载 / 点此重试」：翻页失败之后页面上什么都不剩，而 `blocked`
// 闩住的是自动重试。
//
// ⚠️ **两首都量**：520 宽是**两列**，奇数首歌的哨兵落在最后一行的第 2 格（这一支），
// 偶数首歌的哨兵独占一行（另一支）。少量一首的话最后一行就没排满，而那一行**后面还
// 有格子**——那一格还必须空着，见下面 `TestAnUnfilledLastCellStaysEmpty`。
func TestTheSentinelShowsEvenWhenItLandsMidRow(t *testing.T) {
	for _, n := range []int{1, 2, 5} {
		songs := pageSongs(1, n)
		for _, v := range []View{ViewList, ViewCard} {
			t.Run(fmt.Sprintf("%d首-%s", n, v), func(t *testing.T) {
				sv := NewSwitchView(newCache(t), Actions{})
				sv.SetView(v)
				sv.SetSongs(songs)
				sv.SetFooter(func(c *ui.Context) { ui.Text(c, sentinelMarker) })

				tt := ui.NewTester(sv.View, testerW, testerH)
				tt.Frame()
				for range 3 {
					tt.Frame()
				}

				if _, ok := spotOf(tt, sentinelMarker); !ok {
					t.Errorf("%d 首歌、%s 那一档看不见哨兵——它落在最后一行的中间格，"+
						"而那一支没画。文字有 %q", n, v, tt.Texts())
				}
			})
		}
	}
}

// TestAnUnfilledLastCellStaysEmpty 钉住「最后一行没排满的那几格是空的」。
//
// ⚠️ 这是上一条的**另一半**，而且是它引出来的：哨兵那一格判在「下标 == 歌数」上，
// 而最后一行没排满时**后面还有格子**也是同一个下标——那时候根本没有哨兵（`rows()`
// 不把它算成一行，`PagedList.syncList` 早就 `SetFooter(nil)` 了）。不挡的话它会去越界
// 取 `songs[i]`，当场崩在画这一行上，屏幕上留下「这一行画不出来」。
//
// 判据是**没有崩溃**（`TestEveryViewSurvivesTheNarrowestWindow` 在整页那一层量的是
// 同一件事），加上最后那首歌**画出来了**——崩了的话那一格会被换成占位。
func TestAnUnfilledLastCellStaysEmpty(t *testing.T) {
	// 三首歌、两列 → 最后一行有一格是空的（没有哨兵那一档）。
	for _, v := range []View{ViewList, ViewCard} {
		t.Run(v.String(), func(t *testing.T) {
			songs := pageSongs(1, 3)
			sv := NewSwitchView(newCache(t), Actions{})
			sv.SetView(v)
			sv.SetSongs(songs)

			tt := ui.NewTester(sv.View, testerW, testerH)
			tt.Frame()
			for range 3 {
				tt.Frame()
			}

			for i, s := range songs {
				if _, ok := spotOf(tt, s.Title); !ok {
					t.Errorf("第 %d 首没画出来：%q", i+1, tt.Texts())
				}
			}
			if tt.HasText("画不出来") {
				t.Errorf("有一格画崩了：%q", tt.Texts())
			}
		})
	}
}

// sentinelMarker 是上面那几条自己画的哨兵上的字。
//
// 用它而不是 `footerLabel`：那个是 `PagedList` 自己的哨兵（转圈那一句）上的标签，
// 而这两条量的是「哨兵画不画得出来」本身，值写死一个更好认。
const sentinelMarker = "这一格是哨兵"

// TestTheGridOffersTheRetryThatTheListOffers 是网格哨兵**真正的**用处。
//
// ## 为什么这一条是网格那半边必须有自己的哨兵
//
// 翻页失败时 `apply` 会把 `blocked` 闩上，**闩住的是自动重试**（理由是「失败 →
// 解锁 → 下一帧看见容器还没填满 → 又拉 → 又失败」那个死循环）。闩上之后能解开它
// 的地方**只有哨兵上那个「点此重试」按钮**（`footerView` 的 `trigRetry`）。
//
// 所以网格那半边没有哨兵时：**翻页还是能翻**，但一旦某一页失败就**卡住不动**了——
// 没有转圈、没有重试按钮、连「已加载多少」都不会再变，用户能做的只有切路由或者把
// 视图切回列表。而症状是静默的：歌还在、封面还在、点得动，只是**再也翻不动了**。
//
// 这一条正是把哨兵从网格上拿掉之后会红的那一条（上面那两条不会）。
func TestTheGridOffersTheRetryThatTheListOffers(t *testing.T) {
	boom := errors.New("网络炸了")
	st := newStub().with(1, 40, true).fails(2, boom).with(3, 10, false)
	p, tt := newSwitch(t, st.fetch, ViewCard)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 40 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页失败", func() bool { return p.Paged().Err != nil })

	if p.Paged().Loading {
		t.Fatalf("失败之后还在 Loading，界面会永远转圈")
	}
	if !tt.HasText("加载失败，点此重试") {
		t.Fatalf("网格那一档失败了却没给人一条重试的路——`blocked` 闩住的是自动重试，"+
			"而解开它的只有哨兵上这个按钮。文字有 %q", tt.Texts())
	}

	// 自动补屏就此打住（那是列表那一档也有的规矩，三档一致）。
	frame(tt)
	frame(tt)
	if c := st.callsOf(); len(c) != 2 {
		t.Errorf("失败之后又自己拉了（叫了 %v），want 停在 [1 2]", c)
	}

	// 用户点一下就继续。
	st.serve(2, Page{Songs: pageSongs(2, 10), HasMore: true}, nil)
	if err := tt.Click("加载失败，点此重试"); err != nil {
		t.Fatalf("点不到重试：%v", err)
	}
	pump(t, tt, "重试之后第二页落地", func() bool {
		return p.Paged().Loaded == 50 && !p.Paged().Loading
	})
	if p.Paged().Err != nil {
		t.Errorf("重试成功了 Err 还没清掉：%v", p.Paged().Err)
	}
}

// TestSwitchingViewsKeepsTheSongs 是 `SwitchView` 存在的那条理由。
//
// 两份控件共用同一份歌：页面问的是「这些歌里有哪些」（收藏夹那一页的多选 / 全选
// / 批量删除都走 `List().At(i)`），而那些代码**不知道此刻画的是哪一档**。只给正在
// 画的那一份存的话，切到网格时那些调用会读到空列表——症状是「按了全选，一首都没
// 勾上」。
func TestSwitchingViewsKeepsTheSongs(t *testing.T) {
	st := newStub().with(1, 4, false)
	p, tt := newSwitch(t, st.fetch, ViewList)
	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 4 })

	for _, v := range []View{ViewCard, ViewCompact, ViewList} {
		p.SetView(v)
		tt.Frame()
		if got := p.List().Len(); got != 4 {
			t.Fatalf("切到%s之后 List().Len() 是 %d，want 4——两份控件没有共用同一份歌",
				v.String(), got)
		}
		if m, ok := p.List().At(1); !ok || m.ID != "p1-1" {
			t.Errorf("切到%s之后 At(1) 是 %q，want p1-1", v.String(), m.ID)
		}
		if p.Layout() != v {
			t.Errorf("Layout() 是 %v，want %v", p.Layout(), v)
		}
	}
}

// TestTheLayoutIsOnlyMappedInOnePlace 钉住「档位只有一个来源」。
//
// `ViewOf` / `ModeOf` 是设置里那三串和这里三档之间的**唯一**对照。一旦有人在页面里
// 自己 `switch` 那三个字符串，就多了一个会对不上的来源——症状是「改了设置某一页没
// 变」，而追不到是哪一处多写的那份。
//
// 所以这一条把三个方向都对一遍，且用的是**逐档**而不是「有一个默认值就行」。
func TestTheLayoutIsOnlyMappedInOnePlace(t *testing.T) {
	for _, c := range []struct {
		mode app.DisplayMode
		want View
	}{
		{app.DisplayModeList, ViewList},
		{app.DisplayModeCard, ViewCard},
		{app.DisplayModeCompact, ViewCompact},
		// 认不出的档位退回列表（设置那边 `app.pickEnum` 也退回默认）：
		// 一个拼错的值落到界面上应该是「和默认一样」，而不是「什么都不画」。
		{app.DisplayMode("grid"), ViewList},
		{app.DisplayMode(""), ViewList},
	} {
		if got := ViewOf(c.mode); got != c.want {
			t.Errorf("ViewOf(%q) 是 %v，want %v", c.mode, got, c.want)
		}
	}

	for _, v := range []View{ViewList, ViewCard, ViewCompact} {
		if got := ViewOf(ModeOf(v)); got != v {
			t.Errorf("ViewOf(ModeOf(%v)) 是 %v——往返对不上", v, got)
		}
		if v.String() == "" {
			t.Errorf("%v 没有名字，报错时读不出来", v)
		}
	}
}

// TestAListAskedForTheGridFallsBackToTheList 钉住「忘了分发」的后果。
//
// `List` 收到 `ViewCard` 时**按列表画**：网格那一档走的是另一个控件（`SwitchView` 在
// 两者之间分发）。这里钉住的是「退回旧的、看得见的样子」，而不是画半张网格。
func TestAListAskedForTheGridFallsBackToTheList(t *testing.T) {
	l := New(newCache(t), Actions{})
	l.SetView(ViewCard)
	if l.Layout() != ViewList {
		t.Errorf("List 收到网格那一档之后是 %v，want 退回列表", l.Layout())
	}
	l.SetView(ViewCompact)
	if l.Layout() != ViewCompact {
		t.Errorf("List 收到紧凑那一档之后是 %v，want 紧凑", l.Layout())
	}
}

// TestTheGridDrawsItsOwnCheckboxes 钉住「多选在网格那一档也在」。
//
// 切漏一边的后果是「在列表里能批量删，切成网格就没了」——同一个功能两个答案。
func TestTheGridDrawsItsOwnCheckboxes(t *testing.T) {
	picked := map[string]bool{}
	acts := Actions{}
	g := NewGrid(newCache(t), acts)
	g.SetSongs([]Music{{ID: "g1", Title: "格子一"}, {ID: "g2", Title: "格子二"}})
	g.SetSelection(Selection{
		On:     func(i int) bool { return i == 0 },
		Toggle: func(int) { picked["toggled"] = true },
	})

	tt := ui.NewTester(g.View, testerW, testerH)
	tt.Frame()

	if _, ok := spotOf(tt, "选择 格子一"); !ok {
		t.Fatalf("装了多选接线而第一格没有勾选框：%q", tt.Texts())
	}
	if _, ok := spotOf(tt, "选择 格子二"); !ok {
		t.Errorf("第二格没有勾选框：%q", tt.Texts())
	}
}
