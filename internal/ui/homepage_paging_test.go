package ui

// 这一组验首页那三张 Tab 的「无限下滑」（票 #370）。
//
// 三件事：
//
//  1. **滚到底自动拉下一段**：一直滑一直有（到 B 站没有更多为止）；
//  2. **到头了说清楚**：「没有更多了」那一行（不是无限转圈、不是空白）；
//  3. **「回到顶部」那一枚真的出现**，按了回的是**当前这一档**自己的位置。
//
// 取数走假 B 站（`uiExplore`），不打真接口（AGENTS.md §6）。

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// homePagedSong 是「第 page 页的第 i 首」那首歌的名字。
//
// ⚠️ **页号要进名字和身份**：两页给同一首歌的话，列表去重之后第二页就白拉了
// （而那一页带来的新行是 0，翻页那一层会当成「到底了」——见 `list.PagedList`
// 的 `apply`）。
func homePagedSong(region, page string, i int) string {
	return fmt.Sprintf("%s 第%s页 第%d首", region, page, i)
}

// homePagedData 造一页分区推荐的歌（`display_id` 是页码）。
func homePagedData(region, page string, n int) map[string]any {
	archives := make([]map[string]any, 0, n)
	for i := range n {
		archives = append(archives, map[string]any{
			"aid": 1000*atoiOr(page, 1) + i, "bvid": fmt.Sprintf("BV1p%s%d", page, i),
			"cid": 1000*atoiOr(page, 1) + i, "duration": 180,
			"title":  homePagedSong(region, page, i+1),
			"cover":  "https://f/p.jpg",
			"author": map[string]any{"mid": 7, "name": region + " 的 UP 主"},
			"stat":   map[string]any{"view": 1000},
		})
	}
	return map[string]any{"archives": archives}
}

// atoiOr 把页码那个字符串变成数字（认不出来当第 1 页）。
func atoiOr(s string, def int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

// homePagedArchives 给一台假 B 站装上「一页一页给不同的歌」的分区推荐。
//
// pages 是它有几页：第 pages+1 页给**空的**（那就是「到底了」）。
func homePagedArchives(b *uiExplore, pages int) {
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, r *http.Request) {
		region := "音乐"
		if r.URL.Query().Get("from_region") == "1007" {
			region = "鬼畜"
		}
		page := r.URL.Query().Get("display_id")
		n := homePageSize
		if atoiOr(page, 1) > pages {
			n = 0
		}
		uiExploreJSON(w, 0, homePagedData(region, page, n))
	})
}

// homePagedPos 是画面上画着的那几首（名字 + 上下位置），从上面往下排。
//
// ⚠️ 这是 `homepage_test.go` 里 `listPos` 的另一副样子：那一副找的是「音乐 1」
// 那种名字（测试里塞进去的歌就叫那个），而这里假 B 站给的是「第几页 第几首」。
func homePagedPos(tt *ui.Tester, prefix string) []rowPos {
	var out []rowPos
	for page := 1; page <= 9; page++ {
		for i := 1; i <= homePageSize; i++ {
			name := homePagedSong(prefix, strconv.Itoa(page), i)
			r, ok := tt.Find(name)
			if !ok || r.W == 0 || r.H == 0 {
				continue
			}
			out = append(out, rowPos{name, r.Y})
		}
	}
	slices.SortFunc(out, func(a, b rowPos) int {
		if a.y != b.y {
			return cmpFloat(a.y, b.y)
		}
		return cmpFloat(a.y, b.y)
	})
	return out
}

// cmpFloat 比两个数（小的在前）。
func cmpFloat(a, b float32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// TestTheHomeKeepsLoadingMoreWhenYouScrollToTheEnd 是这一票的头一条：
// **滚到底自动拉下一段**。
//
// 断的是三件事：下一段真的来了（画面上有了新一页的歌）、请求带的是**第二页**
// （`display_id=2`）、而**拉的是同一档**（`from_region` 还是音乐那个）。
func TestTheHomeKeepsLoadingMoreWhenYouScrollToTheEnd(t *testing.T) {
	b := newUIExplore(t)
	homePagedArchives(b, 3)
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})

	// 滚到底：末尾那一行（哨兵）进了画面，下一页就该被拉起来。
	tt.Scroll(homeW/2, homeH/2, 0, 100000)
	waitFrames(t, tt, "第二页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "2", 1))
	})
	if got := b.lastQueryOf(uiHomeRegionFeed).Get("display_id"); got != "2" {
		t.Errorf("最后一次拉的是第 %q 页，want 2", got)
	}
	if got := b.lastQueryOf(uiHomeRegionFeed).Get("from_region"); got != "1003" {
		t.Errorf("最后一次拉的是分区 %q，want 1003（还是「音乐」那一档）", got)
	}

	// 再滚到底：第三页。
	tt.Scroll(homeW/2, homeH/2, 0, 100000)
	waitFrames(t, tt, "第三页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "3", 1))
	})
}

// TestTheHomeSaysThereIsNoMoreWhenThereIsNoMore 是验收第 2 条：
// **到头了说清楚**。
//
// ⚠️ **不许是无限转圈**：B 站给了一页空的之后就该停手，而末尾那一行要有一句
// 「没有更多了」——不然用户只看到列表停在半截，分不清是加载坏了还是真的到头了。
func TestTheHomeSaysThereIsNoMoreWhenThereIsNoMore(t *testing.T) {
	b := newUIExplore(t)
	homePagedArchives(b, 1) // 只有一页：第二页是空的
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})

	tt.Scroll(homeW/2, homeH/2, 0, 100000)
	waitFrames(t, tt, "说到头了", func() bool {
		return showsText(tt, list.EndLabel)
	})
	// ⚠️ **转圈的那一句不许留着**：那是「还在拉」，而这里已经拉完了。
	if showsText(tt, "正在加载…") {
		t.Errorf("到头了还在说「正在加载…」，文字有 %q", tt.Texts())
	}
}

// TestTheHomeBackToTopGoesBackToThatTab 是验收第 4 条：**回顶按钮**。
//
// 滚过 400 像素它要出现（上游的数），按了回到顶上。
//
// ⚠️ **要先滑出好几屏**：那一枚是「滚过 400 像素」才出现的，而 400 像素要
// 列表**够长**才滚得到——一页 15 首滚到底也不够（这正是这一票要消掉的那个
// 「按钮从来不出现」：列表只有 15 条的时候，滚到底也才 400 出头一点点）。
func TestTheHomeBackToTopGoesBackToThatTab(t *testing.T) {
	b := newUIExplore(t)
	homePagedArchives(b, 3)
	s, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})

	// 还没滚：那一枚不该在。
	if showsText(tt, backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// 先滑出好几屏：每一屏到底都拉下一段，列表就长起来了。
	//
	// ⚠️ **等条件，不数次数**（同这一包里的规矩）：数次数赌的是「滚完那几下
	// 的时候下一页已经回来了」，而那是后台一趟 HTTP，满载时回来不了那么快。
	scrollToEnd(t, tt, homeW/2, homeH/2, "第三页到了", func() bool {
		return s.home.lists[0].Paged().Loaded >= 3*homePageSize
	})

	// 滚 1200 像素：按钮出现。
	tt.Scroll(homeW/2, homeH/2, 0, 1200)
	waitFrames(t, tt, "滚下去之后回顶按钮出现", func() bool {
		return showsText(tt, backTopLabel)
	})

	// 按它：回到顶上。
	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	waitFrames(t, tt, "回到顶上之后按钮消失", func() bool {
		return !showsText(tt, backTopLabel)
	})
	pos := homePagedPos(tt, "音乐")
	if len(pos) == 0 || pos[0].title != homePagedSong("音乐", "1", 1) {
		t.Errorf("回到顶上之后画着的是 %v，want 从第一首开始", titles(pos))
	}
}

// TestEachHomeTabKeepsItsOwnScrollAndItsOwnBackToTop 是「回的是**当前这一档**
// 自己的位置」那半句。
//
// 两档各滚一段，然后来回切：每一档都要停在自己那一段。
func TestEachHomeTabKeepsItsOwnScrollAndItsOwnBackToTop(t *testing.T) {
	b := newUIExplore(t)
	homePagedArchives(b, 3)
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "第一页到了", func() bool {
		return showsText(tt, homePagedSong("音乐", "1", 1))
	})

	// 「音乐」滚一段。
	tt.Scroll(homeW/2, homeH/2, 0, 1200)
	homeFrame(tt)
	music := homePagedPos(tt, "音乐")
	if len(music) == 0 {
		t.Fatalf("滚过之后「%s」那一档一首歌都没画出来", homeTabs[0])
	}

	// 切到「鬼畜」，它自己从顶上开始。
	clickTab(t, tt, 1)
	waitFrames(t, tt, "鬼畜那一档到了", func() bool {
		return showsText(tt, homePagedSong("鬼畜", "1", 1))
	})
	guichuTop := homePagedPos(tt, "鬼畜")
	if len(guichuTop) == 0 || guichuTop[0].title != homePagedSong("鬼畜", "1", 1) {
		t.Errorf("切到「%s」画着的是 %v，want 从第一首开始", homeTabs[1], titles(guichuTop))
	}

	// 切回「音乐」：还在它自己那一段。
	clickTab(t, tt, 0)
	if got := homePagedPos(tt, "音乐"); !slices.Equal(got, music) {
		t.Errorf("切回「%s」，画面上是 %v，want 切走之前的 %v",
			homeTabs[0], titles(got), titles(music))
	}
}
