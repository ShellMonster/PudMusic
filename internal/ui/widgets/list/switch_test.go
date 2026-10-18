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
// 三档都要有行；**哨兵三档都要在**（末尾那一格 / 那一行是「滚到底就拉下一页」那个
// 触发器，没有它分页列表永远翻不了页）。
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

// TestTheGridCountsTheSentinelAsARow 钉住「网格把哨兵算成了一行」。
//
// 网格那一档原本**没有哨兵**：末尾没有那一行，于是分页列表的「滚到底就拉下一页」
// 永远触发不了——症状是「切到网格之后翻不了页」，一个只有开了这个设置才会撞上的
// 坏，而且不会让任何别的测试变红。
//
// ⚠️ 这里量的是**行数**而不是「哨兵那一格画在屏幕上」：`ui.List` 是虚拟的，只建
// 看得见的那几行（`mygo/ui/list.go` 的 listOverscan），四十首歌在网格里是二十几行、
// 一屏放不下，于是哨兵压根没被建过——「屏幕上没有哨兵」是真的，但和「哨兵不存在」
// 是两件事。行数是确定的量，而行为（真的翻到了第二页）由下一条钉。
func TestTheGridCountsTheSentinelAsARow(t *testing.T) {
	g := NewGrid(newCache(t), Actions{})
	g.SetSongs([]Music{{ID: "g1"}, {ID: "g2"}, {ID: "g3"}, {ID: "g4"}})

	tt := ui.NewTester(g.View, testerW, testerH)
	tt.Frame() // 画一帧：列数是量了容器宽才算出来的

	before := g.rows()
	g.SetFooter(func(c *ui.Context) { ui.Text(c, footerLabel) })
	tt.Frame()

	if got := g.rows(); got != before+1 {
		t.Errorf("装上哨兵之后网格的行数是 %d，之前是 %d——哨兵没有占一行，"+
			"滚到底就拉不到下一页", got, before)
	}
}

// TestTheGridKeepsPagingWorking 是上面那一条的后果：真的翻到了第二页。
//
// ⚠️ 判据是**行为**不是「哨兵在不在」：哨兵占了行、而三重保障问的又是**网格的行数**
// （一行几首，是拿列数除出来的），任何一处算错，翻页都不动。
func TestTheGridKeepsPagingWorking(t *testing.T) {
	st := newStub().with(1, 40, true).with(2, 20, false)
	p, tt := newSwitch(t, st.fetch, ViewCard)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 40 })

	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第二页落地", func() bool {
		return p.Paged().Loaded == 60 && !p.Paged().Loading
	})
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
