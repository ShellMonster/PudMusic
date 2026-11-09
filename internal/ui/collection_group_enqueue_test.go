// 合集页**行级**入队也要带组标（GroupKey 那四栏）——不然那一首进队列时
// 是「散歌」：抽屉没有组头、随机档也不接「同合集的下一集」。
//
// 出事的形状是「批量入队有组、行级入队没组」：批量那三条（播放全部 /
// 加入队列 / 随机播放）走 `queueItemsOfMedias` 打标，而行级（双击一行、
// 右键「下一首播放」、页头双击）走 `list.Actions`——那几条手里是裸
// `Item`，打标发生在 `collectionActions` 包的那一层。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestTheCollectionRowsCollapseIntoAGroupHeader 是端到端那一条：合集页
// **行级**把三集都放进队列（一首首点「下一首播放」/双击），抽屉里它们
// 收成**一个组头**——这正是「播放列表看不到合集标识」修掉之后的样子。
func TestTheCollectionRowsCollapseIntoAGroupHeader(t *testing.T) {
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"), mediaOf(3, "第三集"))
	waitSongs(t, tt, s, 3)

	acts := s.collectionActions(s.collection)
	for row := 0; row < 3; row++ {
		m, ok := s.collection.list.List().At(row)
		if !ok {
			t.Fatalf("合集页第 %d 行不在", row+1)
		}
		acts.Add([]app.Item{m.Item})
	}
	tt.Frame()

	// 抽屉里的那一份：三集归成了一个组（组头行 + 三行带着组的集行）。
	// `s.queueRows` 是归拢完的那一份（`queueRowsForSnapshot`）：组头行
	// `group != nil && item.GroupKey == ""`，集行 `group != nil`。
	s.openQueueDrawer()
	settleDrawer(t, s, tt)
	var header, episodes int
	for _, r := range s.queueRows {
		switch {
		case r.group != nil && r.item.GroupKey == "":
			header++
		case r.group != nil:
			episodes++
		}
	}
	if header != 1 || episodes != 3 {
		t.Errorf("队列行归拢 = %d 组头 / %d 集行, want 1/3——行级入队的合集没认作一组", header, episodes)
	}
	// 收着的时候（默认）：画出来的只有组头和没有组的行，三集不画。
	visible := queueGroupRows(s.queueRows, s.queueGroupOpen)
	var drawnEp int
	for _, r := range visible {
		if r.group != nil && r.item.GroupKey != "" {
			drawnEp++
		}
	}
	if drawnEp != 0 {
		t.Errorf("收着的合集画出了 %d 行集行——它们该收在组头里", drawnEp)
	}
}

// TestTheCollectionEnqueueMergesTheGroupOntoAnExistingRow 钉 CodeAnt 提的
// 那一支：同一首歌**先**从散歌来源（没组标）进过队列，再从合集页点它——
// `EnqueueAndPlayNow` 定位到队列里那一份（播它，不追加），而定位之后那一份
// 该**带上组标**：不然它永远没有组，抽屉画不出组头、随机档也不接下一集。
func TestTheCollectionEnqueueMergesTheGroupOntoAnExistingRow(t *testing.T) {
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"))
	waitSongs(t, tt, s, 2)

	// 先入队一份**没组标**的同一首（模拟它早前从搜索/收藏夹进来过）。
	m, ok := s.collection.list.List().At(0)
	if !ok {
		t.Fatal("合集页第 1 行不在")
	}
	loose := m.Item
	loose.GroupKey, loose.GroupIndex, loose.GroupCount, loose.GroupTitle = "", 0, 0, ""
	// 空队列的第一首会顺手起播（`Enqueue` 的 `wasEmpty` 分支），而这个壳里
	// 没挂取流——起播失败也把项留在队列里（`Enqueue` 先 Append 再 play），
	// 错可以忍。
	_ = s.app.Enqueue(loose)

	// 再从合集页点它（行级入口那一趟带着组标来定位）。
	acts := s.collectionActions(s.collection)
	acts.PlayNow(m.Item)

	// 队列里那份（下标 0 是刚才那首散歌）该带上组标了。
	it, _ := s.app.Queue().Items()[0], true
	if it.GroupKey == "" {
		t.Errorf("已在队列里的那首被合集页点过之后 GroupKey 还是空——它该是合集的一集")
	}
}

// TestTheCollectionRowEnqueueCarriesTheGroup 钉「双击合集里的一行，
// 进队列的那一项带着组标」——之前它走的是 `Actions.PlayNow` 那条路，
// 裸 `Item` 进去，抽屉里就是一首散歌（没有组头、也没有「合集」标识）。
func TestTheCollectionRowEnqueueCarriesTheGroup(t *testing.T) {
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"), mediaOf(3, "第三集"))
	waitSongs(t, tt, s, 3)

	m, ok := s.collection.list.List().At(1)
	if !ok {
		t.Fatal("合集页第 2 行不在")
	}
	acts := s.collectionActions(s.collection)
	acts.PlayNow(m.Item)

	q := s.app.Queue()
	found := false
	for _, it := range q.Items() {
		if it.Bvid != m.Item.Bvid {
			continue
		}
		found = true
		if it.GroupKey == "" {
			t.Errorf("双击入队的第 2 集 GroupKey 是空的——抽屉里它没有组头")
		}
		if it.GroupIndex != 2 || it.GroupCount != 3 {
			t.Errorf("第 2 集的组标 = %d/%d, want 2/3", it.GroupIndex, it.GroupCount)
		}
	}
	if !found {
		t.Fatal("双击之后队列里没有那一首")
	}
}

// TestTheCollectionLeadEnqueueCarriesTheGroup 钉「页头双击播第一首」也带
// 组标——那一颗绕开了 `list.Actions`，自己调的 `EnqueueAndPlayNow`。
func TestTheCollectionLeadEnqueueCarriesTheGroup(t *testing.T) {
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"))
	waitSongs(t, tt, s, 2)

	title, ok := tt.Find("一个合集")
	if !ok {
		t.Fatalf("合集页头标题不在：%q", tt.Texts())
	}
	// 页头文字列内的空白也属于页头双击区域，走真实的 Lead.OnDoubleClick。
	x, y := title.X+title.W+200, title.Y+title.H/2
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	for range 8 {
		tt.Frame()
	}

	queueItems := s.app.Queue().Items()
	last := queueItems[len(queueItems)-1]
	if last.GroupKey == "" {
		t.Errorf("页头双击真实回调入队的第一集 GroupKey = %q，队列里它该是合集的一集", last.GroupKey)
	}
}
