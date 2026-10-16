package ui

// 这一组跑的是搜索页那三个反直觉的行为，加上「三种类型各有一套排序」和
// 「行的身份稳定且唯一」。
//
// 全是 headless 的（MyGo 的 Tester + 假拉页器）：真的点 Tab、真的点排序、
// 真的滚到底翻页、真的按「全部播放」。后台拉页是并发的，断言走「跑到条件
// 成立为止」而不是死等——见 settleSearch。

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 假拉页器
// ─────────────────────────────────────────────────────────────────────────────

// searchCall 是一次拉页请求：谁拉的、第几页、搜的什么、按哪一档排序。
type searchCall struct {
	tab     int
	page    int
	keyword string
	sort    int
}

// searchStub 是搜索页的假拉页器。不碰网络；翻页、去重、「全部播放」那份
// 副本全都走真的那条路——它替掉的只有网络那一段。
type searchStub struct {
	shell *Shell

	// total 是结果总数，perPage 是每一页几条。24 条一行摆得下一屏多，所以
	// **不会**触发「容器没填满自动续拉」——那三重保障在 `widgets/list` 的
	// 用例里验，那边不需要搜索页这一层。
	total, perPage int
	// overlap 让第 2 页的头一行重复第 1 页的最后一行，模拟 B 站翻页重叠
	// （真会这样：同一首歌挪了名次就落进两页里）。
	overlap bool

	mu    sync.Mutex
	calls []searchCall
}

// newSearchStub 造一份假拉页器。**所有字段都要在它接上之前定好**：接上之后
// 第一趟请求可能已经在后台跑了，那时候再改字段就是竞态。
func newSearchStub(perPage, total int) *searchStub {
	return &searchStub{perPage: perPage, total: total}
}

// install 把这一份接到搜索页上，替掉真的那一份。
func (st *searchStub) install(s *Shell) {
	st.shell = s
	s.search.fetch = st.fetch
}

func (st *searchStub) fetch(tab int, _ context.Context, page int) (list.Page, error) {
	t := &st.shell.search.lists[tab]
	t.mu.Lock()
	q := t.query
	t.mu.Unlock()

	st.mu.Lock()
	st.calls = append(st.calls, searchCall{tab: tab, page: page, keyword: q.keyword, sort: q.sort})
	rows := st.rows(tab, page)
	st.mu.Unlock()

	// 「全部播放」那份副本：和真的那一支走同一段逻辑（见 fetchSearchTab）。
	t.mu.Lock()
	t.loaded = append(t.loaded, rows...)
	t.mu.Unlock()

	has := len(rows) > 0 && page*st.perPage < st.total
	return list.Page{Songs: rows, HasMore: has}, nil
}

// rows 是这一页的行。**走真的映射**（searchVideoMusics / searchUserMusics），
// 这样行身份那一格也在测。
func (st *searchStub) rows(tab, page int) []list.Music {
	if page < 1 {
		return nil
	}
	n := st.perPage
	if left := st.total - (page-1)*st.perPage; left < n {
		n = left
	}
	if n <= 0 {
		return nil
	}
	if tab == searchTabUser {
		hits := make([]bili.UserHit, n)
		for i := range hits {
			hits[i] = bili.UserHit{Mid: stubMid(page, i), Name: stubTitle(page, i), Sign: "签名"}
		}
		return searchUserMusics(hits)
	}
	hits := make([]bili.VideoHit, n)
	for i := range hits {
		hits[i] = bili.VideoHit{Bvid: stubBvid(page, i), Title: stubTitle(page, i), Owner: "某UP"}
	}
	if st.overlap && page > 1 {
		// 第 2 页的头一行重复上一页的最后一行。
		hits[0].Bvid, hits[0].Title = stubBvid(page-1, st.perPage-1), stubTitle(page-1, st.perPage-1)
	}
	return searchVideoMusics(hits)
}

// stubTitle 是一行的标题。**带页号**是故意的：翻页是在尾部追加，两页标题一样
// 就看不出是哪一页的那一行。
func stubTitle(page, i int) string { return fmt.Sprintf("第 %d 页 第 %d 首", page, i+1) }

func stubBvid(page, i int) string { return fmt.Sprintf("BV%03d%03d", page, i) }

func stubMid(page, i int) int64 { return int64(page*1000 + i) }

// callsOf 是某一 Tab 发出去的那些请求。
func (st *searchStub) callsOf(tab int) []searchCall {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []searchCall
	for _, c := range st.calls {
		if c.tab == tab {
			out = append(out, c)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 脚手架
// ─────────────────────────────────────────────────────────────────────────────

// newSearchPage 开一个跑到搜索页的外壳：假拉页器接上，然后搜 keyword。
func newSearchPage(t *testing.T, keyword string, st *searchStub) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	st.install(s)
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape(keyword))
	return s, tt
}

// settleSearch 跑到 cond 成立为止。每一轮走一帧，顺带让后台那一支有机会跑。
func settleSearch(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了两秒还没%s，文字有 %q", what, tt.Texts())
		}
		time.Sleep(time.Millisecond)
	}
}

// loadedTo 是「某一 Tab 加载到了第 n 页、并且那一页进来了」。
//
// ⚠️ **只看「≥ n 页」会误判**：换查询之后 `Reload` 要先把页码清 0 再拉，
// 清 0 和新的一页落地之间隔着一次网络往返，而这一段里 `Page` 读到的还是
// 换查询之前那个数。判据里必须带上「正在拉」这一条（`!Loading`），否则
// 测试会在**重置还没发生**的时候先判成「好了」。
func loadedTo(s *Shell, tab, page int) func() bool {
	return func() bool {
		pg := pageOf(s, tab)
		return pg.Page >= page && !pg.Loading
	}
}

// pageOf 是某一 Tab 此刻的分页状态。
func pageOf(s *Shell, tab int) list.Paged {
	return s.search.lists[tab].list.Paged()
}

// 滚搜索结果那一块用的坐标。
//
// ⚠️ 要落在**页面区中间**：窗口左边有侧栏、下面有播放条，滚到别处滚的是别的东西。
const searchScrollX, searchScrollY float32 = 700, 400

// scrollSearchList 把结果那一块滚到底（正 DY 是往下滚，见 `platform/surface.go`
// 的 PointerScroll 注释）。
func scrollSearchList(tt *mygoui.Tester) { tt.Scroll(searchScrollX, searchScrollY, 0, 100000) }

// queueOneSong 先往队列里放一首**真的能放**的歌，这样「只追加」才有
// 「正在放的那首」可对照。
func queueOneSong(t *testing.T, s *Shell, tt *mygoui.Tester) app.Item {
	t.Helper()
	src := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	dst := filepath.Join(t.TempDir(), "正在放.flac")
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.app.Enqueue(app.LocalItem(dst)); err != nil {
		t.Fatal(err)
	}
	settleSearch(t, tt, "第一首开始播", func() bool { return s.app.Snapshot().HasTrack })
	return s.app.Queue().Items()[0]
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个反直觉行为
// ─────────────────────────────────────────────────────────────────────────────

// TestPlayAllOnlyAppendsToTheQueue 是验收第 1 条，也是最容易被做反的一条：
// 「全部播放」**不替换**当前队列。
//
// 上游的原话就在那儿（`pages/search/video-list/index.tsx:89-101`）：它把当前
// 已加载的列表全量映射之后调 `addList`，**不是 `playList`**。做成了
// `PlayFrom`（换掉整个队列、从第一首起播）的表现是：用户正听着歌，按一下
// 搜索页上的按钮，正在放的那首立刻被换掉，队列里前面放的那些全没了。
func TestPlayAllOnlyAppendsToTheQueue(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	queueOneSong(t, s, tt)
	playing := s.app.Snapshot().Title
	before := s.app.Queue().Items()
	if len(before) != 1 {
		t.Fatalf("测试自己出了问题：队列里有 %d 项", len(before))
	}

	if err := tt.Click("全部播放"); err != nil {
		t.Fatalf("点不到「全部播放」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()

	// 🔴 正在放的那一首不能换。
	if got := s.app.Snapshot().Title; got != playing {
		t.Errorf("按了「全部播放」之后正在放的是 %q，want 还是 %q", got, playing)
	}
	after := s.app.Queue().Items()
	if len(after) != len(before)+24 {
		t.Fatalf("队列里有 %d 项，want %d（原来 1 + 追加 24）", len(after), len(before)+24)
	}
	// 前面那些一个字都不许动。
	for i, it := range before {
		if after[i] != it {
			t.Errorf("队列第 %d 项被换了：%+v，want %+v", i, after[i], it)
		}
	}
	// 话照抄上游（`video-list/index.tsx:100`）。
	if !tt.HasText("已添加 24 首到播放列表") {
		t.Errorf("没有说加了几首，文字有 %q", tt.Texts())
	}
}

// TestPlayAllIsDisabledWhenThereIsNothingToPlay 盯那个按钮的禁用条件。
//
// Biu 是 `isDisabled={initialLoading || list.length === 0}`（同文件 :174）。
// 一首都没有还让按，用户点完什么反应都没有，还以为搜索坏了。
func TestPlayAllIsDisabledWhenThereIsNothingToPlay(t *testing.T) {
	s, tt := newSearchPage(t, "不存在的歌", newSearchStub(24, 0))
	settleText(t, tt, noSearchResults.Hint)

	if err := tt.Click("全部播放"); err != nil {
		t.Fatalf("画面上连「全部播放」都没有：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()
	if n := s.app.Queue().Len(); n != 0 {
		t.Errorf("一首都没有还往队列里塞了 %d 项", n)
	}
}

// TestChangingTheSortGoesBackToPageOne 是验收第 2 条的前半句：换排序回到第 1 页。
//
// 上游是 `fetchPage` 依赖 `[keyword, musicOnly, order]`，任一变化就把 `page`
// 重置为 1 并重拉（`video-list/index.tsx:32-47` 与 `:85-87`）。不做的话用户点
// 一下排序，界面停在第 2 页那一带，而新排序的第 1 页根本没见过。
//
// 判据是**请求**（新排序发出去的那一趟是第 1 页）加**画面**（第 2 页那些行不
// 在屏幕上了）。判据里没有「一共加载了几行」——理由见 searchTab.current 的
// 注释：换查询那一帧列表是空的，分页列表的「容器没填满自动续拉」有概率顺手再
// 拉一页，那一页是它那一层的判断，不是这一页的。
func TestChangingTheSortGoesBackToPageOne(t *testing.T) {
	st := newSearchStub(24, 72)
	s, tt := newSearchPage(t, "周杰伦", st)
	settleText(t, tt, "第 1 页 第 1 首")

	scrollSearchList(tt)
	settleSearch(t, tt, "第 2 页 加载进来", loadedTo(s, searchTabSong, 2))
	tt.Frame()
	if _, ok := tt.Find("第 1 页 第 1 首"); ok {
		t.Fatalf("测试自己出了问题：明明滚到第 2 页了，第 1 页的第 1 首还在屏幕上")
	}
	before := len(st.callsOf(searchTabSong))

	if err := tt.Click("最多播放"); err != nil {
		t.Fatalf("点不到「最多播放」：%v，文字有 %q", err, tt.Texts())
	}
	settleSearch(t, tt, "新排序发出去的那一趟", func() bool {
		c := st.callsOf(searchTabSong)
		return len(c) > before && c[len(c)-1].sort == 1
	})

	// 新排序发出去的第一趟是第 1 页。
	calls := st.callsOf(searchTabSong)
	if got := calls[before]; got.page != 1 {
		t.Errorf("换排序之后发出去的第一趟是第 %d 页，want 第 1 页", got.page)
	}
	// 画面回到了那一页的头：第 1 页的第 1 首重新在屏幕上。
	if _, ok := tt.Find("第 1 页 第 1 首"); !ok {
		t.Errorf("换排序之后没有回到第 1 页的头，文字有 %q", tt.Texts())
	}
}

// TestChangingTheKeywordGoesBackToPageOne 是验收第 2 条的后半句。
//
// 「任一筛选条件」里的另一个：搜索词变了。走的是同一条路（判据是「画着的
// 这些结果不是你要的那一批了」，见 searchTab.current）。
func TestChangingTheKeywordGoesBackToPageOne(t *testing.T) {
	st := newSearchStub(24, 72)
	s, tt := newSearchPage(t, "周杰伦", st)
	settleText(t, tt, "第 1 页 第 1 首")

	scrollSearchList(tt)
	settleSearch(t, tt, "第 2 页 加载进来", loadedTo(s, searchTabSong, 2))
	before := len(st.callsOf(searchTabSong))

	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("林俊杰"))
	settleSearch(t, tt, "新词发出去的那一趟", func() bool {
		c := st.callsOf(searchTabSong)
		return len(c) > before && c[len(c)-1].keyword == "林俊杰"
	})

	calls := st.callsOf(searchTabSong)
	if got := calls[before]; got.page != 1 {
		t.Errorf("换词之后发出去的第一趟是第 %d 页，want 第 1 页", got.page)
	}
	if _, ok := tt.Find("第 1 页 第 1 首"); !ok {
		t.Errorf("换词之后没有回到第 1 页的头，文字有 %q", tt.Texts())
	}
}

// TestSwitchingTabsKeepsWhereYouWere 是验收第 3 条：切 Tab **不重置滚动位置**。
//
// Biu 那一行的理由写在 `BIU-UI-INVENTORY.md`：「只改条件渲染，外层
// ScrollContainer 也没传 resetOnChange」。本项目的做法不同但更严：每个 Tab 各
// 有自己的分页列表，也各自记着滚到哪儿——切走再切回来落回切走前那一行，
// 而不是「像素没动、内容换了」。
//
// 反向验：把 Tab 算进那个「查询变了」的判据里（见 searchSpecKey 的注释），
// 每次切都重新拉并且回到顶上，这一条立刻红。
func TestSwitchingTabsKeepsWhereYouWere(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 72))
	settleText(t, tt, "第 1 页 第 1 首")

	// 在「歌曲」Tab 上翻到第 2 页并滚下去。
	scrollSearchList(tt)
	settleSearch(t, tt, "第 2 页 加载进来", loadedTo(s, searchTabSong, 2))
	tt.Frame()

	// 记下这一刻「第 2 页 第 1 首」在画面上的位置。
	const anchor = "第 2 页 第 1 首"
	before, ok := tt.Find(anchor)
	if !ok {
		t.Fatalf("滚下去之后画面上没有 %q，文字有 %q", anchor, tt.Texts())
	}

	// 切到「视频」再切回「歌曲」。
	for _, tab := range []string{"视频", "歌曲"} {
		if err := tt.Click(tab); err != nil {
			t.Fatalf("点不到 %q 那个 Tab：%v，文字有 %q", tab, err, tt.Texts())
		}
		settleSearch(t, tt, tab+" 那一页画出来", func() bool {
			return s.search.tab == tabOf(tab) && !pageOf(s, tabOf(tab)).Loading
		})
	}

	// 位置没动：那一行还在画面上同一个地方。
	after, ok := tt.Find(anchor)
	if !ok {
		t.Fatalf("切回「歌曲」之后画面上没有 %q，文字有 %q", anchor, tt.Texts())
	}
	if after.Y != before.Y {
		t.Errorf("%q 的位置从 y=%v 变成 y=%v：切 Tab 重置了滚动位置", anchor, before.Y, after.Y)
	}
	// 而且**没有重新拉一遍**——重新拉的话加载行数会从 48 掉回 24。
	if got := pageOf(s, searchTabSong).Loaded; got != 48 {
		t.Errorf("切回「歌曲」之后加载了 %d 行，want 48（说明被重新拉了一遍）", got)
	}
}

// tabOf 是那个 Tab 名字的下标。
func tabOf(name string) int {
	for i, n := range searchTabNames {
		if n == name {
			return i
		}
	}
	return -1
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个类型、三套排序
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeTypesAndTheirSorts 盯「搜索结果分三种类型，每种都有对应排序」。
func TestTheThreeTypesAndTheirSorts(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	for _, name := range searchTabNames {
		if !tt.HasText(name) {
			t.Errorf("画面上没有 %q 那个 Tab，文字有 %q", name, tt.Texts())
		}
	}
	// 「歌曲」/「视频」那一档是上游的五档（`video-list/search-header.tsx:21-27`）。
	for _, o := range searchVideoSorts {
		if !tt.HasText(o.Label) {
			t.Errorf("画面上没有排序 %q，文字有 %q", o.Label, tt.Texts())
		}
	}

	// 切到「用户」：换成**它自己那一套**五档（`user-list/search-header.tsx:12-18`）。
	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v，文字有 %q", err, tt.Texts())
	}
	settleSearch(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	for _, o := range searchUserSorts {
		if !tt.HasText(o.Label) {
			t.Errorf("用户 Tab 上没有排序 %q，文字有 %q", o.Label, tt.Texts())
		}
	}
	// 两档排序**不相同**：用户那五档是「粉丝数」「Lv 等级」，视频那五档是
	// 「播放」「弹幕」。
	if tt.HasText("最多弹幕") {
		t.Errorf("用户 Tab 上还留着视频那一档的排序，文字有 %q", tt.Texts())
	}
	// 用户不是歌，没有「全部播放」。
	if tt.HasText("全部播放") {
		t.Errorf("用户 Tab 上还有「全部播放」，文字有 %q", tt.Texts())
	}
}

// TestEachSortStaysItsOwn 盯「排序是每个 Tab 各自的」。
//
// Biu 的两个列表各有一份排序状态（`video-list` 一个 `order`、`user-list` 一个
// `sortKey`）。做成一个全局值的话，在「用户」里按过排序再切回「视频」，视频
// 那边的排序被改了——用户回去发现列表顺序变了，而他自己刚点的明明是用户那边。
func TestEachSortStaysItsOwn(t *testing.T) {
	st := newSearchStub(24, 24)
	s, tt := newSearchPage(t, "周杰伦", st)
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("最新发布"); err != nil {
		t.Fatalf("点不到「最新发布」：%v，文字有 %q", err, tt.Texts())
	}
	settleSearch(t, tt, "「歌曲」那一档换了排序", func() bool {
		c := st.callsOf(searchTabSong)
		return len(c) >= 2 && c[len(c)-1].sort == 2
	})

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	settleSearch(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))
	if err := tt.Click("Lv等级由高到低"); err != nil {
		t.Fatalf("点不到「Lv等级由高到低」：%v，文字有 %q", err, tt.Texts())
	}
	settleSearch(t, tt, "用户那一档换了排序", func() bool {
		c := st.callsOf(searchTabUser)
		return len(c) >= 2 && c[len(c)-1].sort == 3
	})

	// 切回「歌曲」：排序**还在**第 3 档（最新发布），没有被用户那档的
	// 第 4 档（Lv等级由高到低）顶掉。
	if err := tt.Click("歌曲"); err != nil {
		t.Fatalf("点不到「歌曲」那个 Tab：%v", err)
	}
	tt.Frame()
	tt.Frame()
	if got := s.search.sorts[searchTabSong]; got != 2 {
		t.Errorf("切回来之后「歌曲」的排序变成了第 %d 档，want 第 3 档（最新发布）", got+1)
	}
	// 而且切回来没有重新拉（排序没变）。
	if got := pageOf(s, searchTabSong).Loaded; got != 24 {
		t.Errorf("切回「歌曲」之后加载了 %d 行，want 24（排序没变不该重拉）", got)
	}
}

// TestTheSongTabAsksForTheMusicZoneAndTheVideoTabDoesNot 盯「三种类型」里
// 真正发到 B 站的那一点差别。
//
// 上游那个默认开着的「仅音乐分区」开关发的是 `tids=3`（`video-list/index.tsx:40`）。
// 本项目把它做成第一个 Tab——**Tab 的差别必须真的落到请求上**，不然「歌曲」和
// 「视频」搜出来是同一份东西，而界面上看不出任何异样。
func TestTheSongTabAsksForTheMusicZoneAndTheVideoTabDoesNot(t *testing.T) {
	q := searchQuerySpec{keyword: "周杰伦", sort: 0}
	if !searchVideoQueryOf(searchTabSong, q).MusicOnly {
		t.Errorf("「歌曲」Tab 没限定音乐分区")
	}
	if searchVideoQueryOf(searchTabVideo, q).MusicOnly {
		t.Errorf("「视频」Tab 限定了音乐分区")
	}
	// 排序要真的落到请求上。
	if got := searchVideoQueryOf(searchTabSong, q).Order; got != bili.VideoOrderTotalRank {
		t.Errorf("零值排序变成了 %q，want %q", got, bili.VideoOrderTotalRank)
	}
	if got := searchVideoQueryOf(searchTabVideo, searchQuerySpec{sort: 3}).Order; got != bili.VideoOrderDanmaku {
		t.Errorf("第 4 档排序变成了 %q，want %q", got, bili.VideoOrderDanmaku)
	}
	// 用户那一档是另一套参数（粉丝数 + 升降序），不是视频那一套。
	if got := searchUserQueryOf(searchQuerySpec{sort: 1}); got.Sort != bili.UserSortFansDesc {
		t.Errorf("用户第 2 档排序变成了 %+v，want %+v", got.Sort, bili.UserSortFansDesc)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 行的身份
// ─────────────────────────────────────────────────────────────────────────────

// TestARowThatLandsOnTwoPagesIsStillOneRow 盯铁律四那一半：**唯一**。
//
// B 站翻页会重叠，同一个 bvid 落进两页里是常事。两个一样的 ID 就是两行共用
// 一份状态——选中一个另一个也亮着，而界面上看不出任何异样。分页列表自己去重，
// 这一条钉的是「我们给出去的 ID 确实相同」：去重之后剩几行、选中的还是不是
// 那一首。
func TestARowThatLandsOnTwoPagesIsStillOneRow(t *testing.T) {
	st := newSearchStub(24, 72)
	st.overlap = true
	s, tt := newSearchPage(t, "周杰伦", st)
	settleText(t, tt, "第 1 页 第 1 首")

	// 先选中第 1 页的一行：翻页追加之后它还得是选中那一首。
	if err := tt.Click("第 1 页 第 3 首"); err != nil {
		t.Fatalf("点不到「第 1 页 第 3 首」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	scrollSearchList(tt)
	settleSearch(t, tt, "第 2 页 加载进来", loadedTo(s, searchTabSong, 2))

	// 第 2 页 24 条里有一条和第 1 页重复 → 只该有 47 行。
	if got := pageOf(s, searchTabSong).Loaded; got != 47 {
		t.Errorf("两页重叠之后有 %d 行，want 47（重叠的那一条只算一次）", got)
	}
	// 选中项还是那同一首（追加在尾部不会动到上面那些行的身份）。
	m, ok := s.search.lists[searchTabSong].list.List().Selected()
	if !ok {
		t.Fatalf("翻完页之后没有选中了")
	}
	if m.Title != "第 1 页 第 3 首" {
		t.Errorf("翻完页之后选中的是 %q，want 还是「第 1 页 第 3 首」", m.Title)
	}
}

// TestSearchRowsCarryStableIDs 盯铁律四那一半：**稳定**。
//
// 行的身份是 B 站的 bvid / mid 本身，不是序号。B 站认的就是这个，它不随页面
// 不随排序变——拿序号当身份的话，头部插一行会让后面所有行的状态整体错配。
func TestSearchRowsCarryStableIDs(t *testing.T) {
	v := searchVideoMusic(bili.VideoHit{Bvid: "BV1MbznYoEk1", Title: "歌名", Owner: "某UP"})
	if v.ID != "BV1MbznYoEk1" {
		t.Errorf("视频行的 ID 是 %q，want bvid 本身", v.ID)
	}
	u := searchUserMusic(bili.UserHit{Mid: 9527, Name: "某人"})
	if u.ID != searchUserIDPrefix+"9527" {
		t.Errorf("用户行的 ID 是 %q，want %q", u.ID, searchUserIDPrefix+"9527")
	}
	// 两个类型撞不上：一个用户的 ID 和一个 bvid 绝不会一样。
	if v.ID == u.ID {
		t.Errorf("视频行和用户行的 ID 撞上了：%q", v.ID)
	}
	// 菜单那一层只递 app.Item 过来，所以 mid 得能从它答得出来。
	if mid, ok := searchUserMid(u.Item); !ok || mid != "9527" {
		t.Errorf("从用户那一项读不出 mid：%q / %v", mid, ok)
	}
	if _, ok := searchUserMid(v.Item); ok {
		t.Errorf("把一首视频误认成了用户")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用户 Tab
// ─────────────────────────────────────────────────────────────────────────────

// TestAMenuOnAUserGoesToTheirSpace 盯用户那一行按下去是什么。
//
// 用户不是歌：右键菜单里的「播放」是进他的 UP 主空间（上游
// `user-card.tsx:51` 的 `onPress={() => navigate("/user/" + u.mid)}`）。
func TestAMenuOnAUserGoesToTheirSpace(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v，文字有 %q", err, tt.Texts())
	}
	settleSearch(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	if err := tt.RightClick("第 1 页 第 2 首"); err != nil {
		t.Fatalf("右键点不到那一行：%v，文字有 %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatalf("菜单里点不了「播放」：%v", err)
	}
	tt.Frame()
	tt.Frame()

	want := userPathPrefix + strconv.FormatInt(stubMid(1, 1), 10)
	if got := s.router.Path(); got != want {
		t.Errorf("点了用户那一行的「播放」之后在 %q，want %q", got, want)
	}
}

// TestAddingAUserToTheQueueSaysWhyNot 盯「按了没反应最难解释」。
func TestAddingAUserToTheQueueSaysWhyNot(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	settleSearch(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	if err := tt.RightClick("第 1 页 第 2 首"); err != nil {
		t.Fatalf("右键点不到那一行：%v", err)
	}
	if err := tt.ChooseMenuItem("添加到队列"); err != nil {
		t.Fatalf("菜单里点不了「添加到队列」：%v", err)
	}
	tt.Frame()
	tt.Frame()

	if n := s.app.Queue().Len(); n != 0 {
		t.Errorf("用户被塞进队列了（%d 项）", n)
	}
	if !tt.HasText("UP 主不是歌") {
		t.Errorf("没有说为什么加不进去，文字有 %q", tt.Texts())
	}
}

// TestTheResultRowsArePaintedNotBlank 盯「有结果时那一块不是一片白」。
//
// 只查元素框发现不了这一类：元素在、位置对、文字也在，可屏幕上什么也没有。
// 空状态那一支有同类的用例（`TestEmptyStateIsPaintedNotBlank`），这一支是它的
// 对面——结果区才是这一页九成时间在显示的东西。
func TestTheResultRowsArePaintedNotBlank(t *testing.T) {
	_, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 72))
	settleText(t, tt, "第 1 页 第 1 首")

	r, ok := tt.Find("第 1 页 第 1 首")
	if !ok {
		t.Fatalf("结果那一行没画出来，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	bg := img.RGBAAt(4, 4)
	// 在那一行的盒子里扫一遍：**要有一个像素**和底色不同。只查一个点是不行的
	//——落在两个字之间的空白上，它和底色一样，而那一行其实画得好好的。
	painted := false
	for dy := 0; dy < int(r.H) && !painted; dy++ {
		for dx := 0; dx < int(r.W); dx++ {
			if img.RGBAAt(int(r.X)+dx, int(r.Y)+dy) != bg {
				painted = true
				break
			}
		}
	}
	if !painted {
		t.Errorf("那一行的盒子里全是底色 %v：画出来一片白", bg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 没接上 / 没有结果
// ─────────────────────────────────────────────────────────────────────────────

// TestNoClientSaysSoInsteadOfPretending 盯「没挂 B 站客户端」别静默。
//
// 生产上 `main.go` 一定先挂（`ui.MountLogin`），这一条是给「万一没挂」留的。
// 表现是「搜了没有结果」那一支 + 一条说清原因的提示，而不是一片空白让人以为
// 搜过了就是没有。
func TestNoClientSaysSoInsteadOfPretending(t *testing.T) {
	s, tt := newShell(t) // 没有换掉 fetch：走真的那一支，而它没有客户端
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))

	settleText(t, tt, noSearchResults.Hint)
	if !tt.HasText(errSearchNotMounted.Error()) {
		t.Errorf("没有说清是「没接上」，文字有 %q", tt.Texts())
	}
	_ = s
}
