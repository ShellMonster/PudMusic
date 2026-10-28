package ui

// 「歌单页的头跟着列表一起滚走」（票 #625）。
//
// # 为什么要有这一组
//
// 歌单页（收藏夹 / 合集 / 系列）原先把头图区（封面、标题、简介、创建者）和
// 下面那排操作条摆在列表**外面**——于是滚列表时它们**钉在顶上不动**，一屏
// 只剩半屏能看歌。
//
// 首页的热榜 / 原创榜那一档**已经**改过了（#615 / 提交 `3df4eefd`）：榜单头图
// 和表头现在是列表的第 0 行（`list.Lead`），跟着内容一起滚走。UP 空间也是
// 同一个做法（`upspace_tabs.go` 的 `SetLead`）。
//
// 这一组钉的是**歌单页也这样**。
//
// # 上游是怎么摆的
//
// `_research/biu/src/pages/video-collection/favorites/index.tsx:436`：整页是
// **一个** `ScrollContainer`，`<Header/>` + `<Operations/>` + 列表都在里面——
// 所以那边头图本来就会滚走。

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// collectionMedias 造 n 条内容（够滚好几屏）。
func collectionMedias(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := range n {
		out = append(out, mediaOf(i+1, "歌曲 "+strconv.Itoa(i+1)))
	}
	return out
}

// TestTheCollectionHeaderScrollsAwayWithTheList 钉住「头图区跟着滚走」。
//
// ⚠️ **判据是滚过之后那一块的位置**：钉住的会在原地，跟着滚的会往上
// （甚至出画面）。
//
// ⚠️ **「不在画面上」单独不算过**：那一块要是压根没画出来，`Find` 同样拿不到
// ——那也是「不在画面上」，但页头并没有跟着滚。所以先断「滚之前它确实在」，
// 再断「滚之后它不在了或者往上」。
func TestTheCollectionHeaderScrollsAwayWithTheList(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMedias(40)...)
	waitSongs(t, tt, s, 40)
	tt.Frame()

	// 锚点是歌单标题（头图区里那个大标题）。
	before, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("画面上找不到歌单标题：文字有 %q", tt.Texts())
	}
	if before.Y <= 0 || before.Y > 1000 {
		t.Fatalf("滚之前标题的位置就不在窗口里（y=%.0f）——这一条量不下去", before.Y)
	}

	tt.Scroll(600, 500, 0, 1500)
	for range 40 {
		tt.Frame()
	}

	after, stillThere := tt.Find("通勤路上")
	if stillThere && after.Y >= before.Y-1 {
		t.Errorf("滚了 1500 之后标题还在 y=%.0f（滚之前 y=%.0f）——"+
			"它是钉在顶上的，不是跟着列表滚走", after.Y, before.Y)
	}
	// ⚠️ **顺带把「滚动真的发生了」钉一下**：列表报的滚动量得往前走过。
	// 不然上面那条会在「压根没滚动」的情况下也通过。
	if off := s.collection.list.ScrollOffset(); off <= 0 {
		t.Errorf("滚了 1500，列表报的滚动量还是 %.0f——滚动压根没发生", off)
	}
}

// TestTheCollectionOpsRowScrollsAwayWithTheList 钉住「操作条跟着滚走」。
//
// ⚠️ **判据是那一枚主按钮**：它是操作条上最显眼的一样（上游 `Button` 的
// md 档，40 高）。
func TestTheCollectionOpsRowScrollsAwayWithTheList(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMedias(40)...)
	waitSongs(t, tt, s, 40)
	tt.Frame()

	before, ok := tt.Find("播放全部")
	if !ok {
		t.Fatalf("画面上找不到「播放全部」：文字有 %q", tt.Texts())
	}

	tt.Scroll(600, 500, 0, 1500)
	for range 40 {
		tt.Frame()
	}

	after, stillThere := tt.Find("播放全部")
	if stillThere && after.Y >= before.Y-1 {
		t.Errorf("滚了 1500 之后「播放全部」还在 y=%.0f（滚之前 y=%.0f）——"+
			"操作条钉在顶上没跟着滚", after.Y, before.Y)
	}
}

// TestTheCollectionHeaderIsNotLeftBehindAsAnOverlay 钉住「它不是浮在上面的」。
//
// 🔴 **这一条防的是另一种改法**：把头图区做成一个**浮层**（`ui.Overlay`，浮在
// 整窗之上、不占布局）——那样表头会跟着列表滚走（这一条断的就是那个），
// 但**头图区本身会一直压在内容上面**，用户看到的还是一屏只剩半屏。
//
// ⚠️ **两条判据**（票 #624 的评审第 6 条：原来只有一条，而注释说的是另一条）：
//
//	表头滚走了	它**跟着列表走**，不是浮层
//	列表内容没被盖住	它**不压着内容**，不是浮层
//
// ⚠️ **第二条量的是「滚到底之后最后那一首歌还在画面上」**：浮层会盖住它
// （或者把它推到画面外面），而它现在**跟着内容走**，所以它在。
func TestTheCollectionHeaderIsNotLeftBehindAsAnOverlay(t *testing.T) {
	medias := collectionMedias(40)
	_, s, tt := collectionShell(t, "12345", medias...)
	waitSongs(t, tt, s, 40)
	tt.Frame()

	// 滚到底：末尾那一行（哨兵）会在画面上。
	tt.Scroll(600, 500, 0, 100000)
	for range 40 {
		tt.Frame()
	}

	// ① 表头是**跟着列表走**的那一块的证明：它在列表的第 0 行里，
	// 所以滚到底之后它应该不在画面上——而如果它是一个浮层，它会**一直在**。
	//
	// ⚠️ **表头的名字是「收藏时间」**（收藏夹那一页的时间列是 `fav_time`，
	// 见 `collectionTimeTitle`）。
	if _, stillThere := tt.Find("收藏时间"); stillThere {
		t.Error("滚到底之后表头还在画面上——它不是跟着列表走的（是个浮层？）")
	}

	// ② **内容没被盖住**：最后那一首歌还在画面上。
	//
	// ⚠️ **判据是「最后那一首」而不是「第一首」**：滚到底之后第一首早滚出
	// 画面了（「不在画面上」和「被盖住了」在它身上分不开——而分不开就不能当
	// 判据）。
	last := "歌曲 40"
	if _, ok := tt.Find(last); !ok {
		t.Errorf("滚到底之后最后一首歌（%q）不在画面上——头图区盖住了内容？文字有 %q",
			last, tt.Texts())
	}
}

// TestTheCollectionHeaderSurvivesSorting 钉住「换排序之后头图区还在」。
//
// 🔴 **这一条防的是 `reloadCollection` 那一步**：它把整个列表换掉了，而页内
// 头部是装在**列表**上的——不重装的话，新列表就没有那块，于是**头图区整个
// 不见**（症状是「收藏夹页没有封面、没有标题、也没有那一排操作」）。
func TestTheCollectionHeaderSurvivesSorting(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMedias(3)...)
	waitSongs(t, tt, s, 3)
	tt.Frame()

	if _, ok := tt.Find("通勤路上"); !ok {
		t.Fatalf("一开始就没有头图区：文字有 %q", tt.Texts())
	}

	// 换排序（那一枚下拉）：选「最多播放」（不是默认那一档「最近收藏」——
	// 默认那一档的 `listSort` 本来就是它，等不到变化）。
	if err := tt.Click("排序：最近收藏"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("最多播放"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "换排序之后内容重新加载", func() bool {
		return s.collection.listSort == bili.SortByPlay
	})
	tt.Frame()

	if _, ok := tt.Find("通勤路上"); !ok {
		t.Errorf("换排序之后头图区不见了：文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("播放全部"); !ok {
		t.Errorf("换排序之后操作条不见了：文字有 %q", tt.Texts())
	}
}

// TestTheCollectionHeaderIsStillThereWhenTheFolderIsEmpty 钉住「空的时候头图区还在」。
//
// 🔴 **这一条防的是页内头部的一个真副作用**：它占列表的**第 0 行**，而列表
// 一行都没有的时候（空收藏夹 / 搜了没搜到），那个第 0 行**压根不存在**——
// 于是头图区整个不见，页面变成一句「暂无内容」。
//
// ⚠️ 而头图区是**这一页的身份**（封面、标题、创建者）——它没了，用户看到的
// 是一句没有上下文的「暂无内容」。
func TestTheCollectionHeaderIsStillThereWhenTheFolderIsEmpty(t *testing.T) {
	// ⚠️ **一条都没有**：`collectionShell` 的 fixture 会把 `media_count` 设成
	// 条数，所以这里是零条。
	_, s, tt := collectionShell(t, "12345")
	waitFrames(t, tt, "收藏夹加载完", func() bool {
		return s.app.Collection(s.collection.ref).Loaded
	})
	tt.Frame()

	if _, ok := tt.Find("通勤路上"); !ok {
		t.Errorf("空收藏夹的头图区不见了：文字有 %q", tt.Texts())
	}
}

// TestTheCollectionHeaderScrollsAwayInTheGridViewToo 钉住**网格那一档**。
//
// 🔴 **两档都要有**：`SetLead` 在 `SwitchView` 上两份都装（`switch.go`），
// 而设置里的「数据显示样式」随时会换档——只在一档装的话，用户切到网格就
// 看到头图区又钉在顶上了（而那一档压根没有表头）。
func TestTheCollectionHeaderScrollsAwayInTheGridViewToo(t *testing.T) {
	st := app.DefaultSettings("")
	st.DisplayMode = app.DisplayModeCard

	_, s, tt := collectionShellOpts(t,
		[]shellOpt{withSettings(st)},
		"", "12345", collectionMedias(40)...)
	waitSongs(t, tt, s, 40)
	tt.Frame()

	before, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("画面上找不到歌单标题：文字有 %q", tt.Texts())
	}

	tt.Scroll(600, 500, 0, 1500)
	for range 40 {
		tt.Frame()
	}

	after, stillThere := tt.Find("通勤路上")
	if stillThere && after.Y >= before.Y-1 {
		t.Errorf("网格那一档：滚了 1500 之后标题还在 y=%.0f（滚之前 y=%.0f）——"+
			"它钉在顶上了", after.Y, before.Y)
	}
}

// TestTheCollectionFirstFrameSaysItIsLoading 钉住**首屏的「正在加载…」**。
//
// 🔴 **这一条是票 #625 的评审补上的（第 3 条）**：头图区搬进列表之后，列表
// **至少有一行**（那第 0 行就是头图区自己），而框架那一格（`Children`，列表
// **外面**的内容）**只在列表一行都没有的时候才画**（`mygo/ui/list.go` 的
// `layoutList` 里 `if n == 0` 那一支）。于是那一格**永远不画**——
// 「正在加载…」那个骨架屏整个消失。
//
// ⚠️ **症状是首屏空白**：用户第一次打开收藏夹看到的是「头图区 + 一片空白 +
// 不知道在不在加载」。而那一格是**专门**做这件事的（票 #138：它和
// 「没有结果」那一格是同一个形状）。
//
// ⚠️ **判据是「内容还在路上」**：夹具默认秒回，而那一档只在内容还在路上
// 的时候出现——所以这一条要**卡住内容列表那一趟**。
func TestTheCollectionFirstFrameSaysItIsLoading(t *testing.T) {
	b := newCollectionBili(t)
	hold := make(chan struct{})
	b.holdList = hold
	t.Cleanup(func() { close(hold) })

	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	tt.Frame()

	if !tt.HasText("正在加载…") {
		t.Errorf("首屏没有「正在加载…」：文字有 %q", tt.Texts())
	}
	// ⚠️ **顺带钉住「头图区还在」**：那一格和头图区是**两样**——骨架屏没了
	// 而头图区还在，用户至少还知道自己在哪个收藏夹里。
	if _, ok := tt.Find("通勤路上"); !ok {
		t.Errorf("首屏的头图区不见了：文字有 %q", tt.Texts())
	}
	_ = s
}
