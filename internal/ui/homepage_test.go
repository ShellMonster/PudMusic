package ui

// 首页（问候区 + 今日一张 + 推荐 / 榜单 / 新歌三档）的测试。
//
// 取数本身（参数、签名、字段怎么落）由服务层和 app 层那两组钉
// （`internal/service/bili/recommend_test.go`、`internal/app/recommend_test.go`）。这一组
// 盯的是首页自己的：什么时候去要、三种状态各是什么样子、点了会做什么。断言面是画面上
// 有什么字。
//
// ⚠️ 假 B 站复用空间 / 动态那一组的那台（`newUIExplore`）：它已经带 WBI 密钥那一段
// （分区推荐要签名，签名之前先打 nav 拿 key）。

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// 首页那几个接口的路径（写字符串是为了测试自己看得见自己断言的是什么）。
const (
	uiHomeRegionFeed   = "/x/web-interface/region/feed/rcmd"
	uiHomeToplistList  = "/x/copyright-music-publicity/toplist/all_period"
	uiHomeToplistMusic = "/x/copyright-music-publicity/toplist/music_list"
	uiHomeNewMusic     = "/x/centralization/interface/new/music"
	uiHomeNewBanner    = "/x/centralization/interface/new/music/banner"
)

// newHomeShell 装一个指着假 B 站的首页：`MountLogin` 建那一个客户端，再递给
// `MountRecommend`（建第二个客户端的话登录态、WBI 密钥、请求头都不是同一份）。
func newHomeShell(t *testing.T, b *uiExplore, opts ...shellOpt) (*Shell, *mygoui.Tester, *app.App) {
	t.Helper()
	sh := newTestShell(t, append([]shellOpt{at(homeW, homeH), mounted(func(_ *testing.T, a *app.App) {
		transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountLogin(app.NewMemoryStore("home-test"), cli)
		a.MountRecommend(cli)
		a.MountStreaming(app.Streaming{Client: cli, Media: b.Server.Client()})
	})}, opts...)...)
	return sh.s, sh.tt, sh.a
}

// homePageSize 是分区推荐一页的条数（上游 `REGION_PAGE_SIZE = 15`）。
const homePageSize = 15

// homeArchiveData 造一份分区推荐的 data 段。歌名是「音乐 推的第 N 首」。
func homeArchiveData() map[string]any {
	archives := make([]map[string]any, 0, homePageSize)
	for i := range homePageSize {
		archives = append(archives, map[string]any{
			"aid": 11 + i, "bvid": fmt.Sprintf("BV1home%d", i+1), "cid": 101 + i,
			"duration": 185,
			"title":    fmt.Sprintf("每日推荐 %d", i+1),
			"cover":    fmt.Sprintf("https://f/home%d.jpg", i+1),
			"author":   map[string]any{"mid": 7, "name": "推荐 UP"},
			"stat":     map[string]any{"view": 1000},
		})
	}
	return map[string]any{"archives": archives}
}

// homeChartData 造热榜那一期的 music_list：n 首，歌名「榜上第 N 名」。
func homeChartData(n int) map[string]any {
	list := make([]map[string]any, 0, n)
	for i := range n {
		list = append(list, map[string]any{
			"music_id": fmt.Sprintf("m%d", i+1), "music_title": fmt.Sprintf("榜上第 %d 名", i+1),
			"singer":        fmt.Sprintf("歌手%d", i+1),
			"creation_bvid": fmt.Sprintf("BV1chart%d", i+1), "creation_aid": 500 + i,
			"creation_first_cid": 900 + i, "creation_cover": fmt.Sprintf("https://f/chart%d.jpg", i+1),
			"creation_duration": 200, "creation_play": 100000 - i*1000,
		})
	}
	return map[string]any{"list": list}
}

// homeArchives 给一台假 B 站装上首页那几个接口。
func homeArchives(b *uiExplore) {
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, homeArchiveData())
	})
	b.on(uiHomeToplistList, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": map[string]any{"2026": []map[string]any{
			{"ID": 77, "priod": 12, "publish_time": time.Now().Add(-48 * time.Hour).Unix()},
		}}})
	})
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, homeChartData(24))
	})
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"id": 5, "music_title": "新歌速递里那首", "bvid": "BV1new", "aid": 5, "cid": 5,
			"cover": "https://f/new.jpg", "author": "新歌手", "publish_time": "1-1", "total_vv": 12345,
		}}})
	})
	b.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
}

// clickHomeTab 点「推荐 / 榜单 / 新歌」里的一个，再画几帧。
func clickHomeTab(t *testing.T, tt *mygoui.Tester, tab int) {
	t.Helper()
	if err := tt.Click(homeTabLabels[tab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabLabels[tab], err, tt.Texts())
	}
	homeFrame(tt)
	settleHome(tt)
}

// TestTheHomePagePullsWhatItNeedsOnOpen 盯「什么时候发请求」：
// 一进首页就拉每日推荐（音乐分区）和本周最热（热榜），新歌速递等点到「新歌」才拉。
func TestTheHomePagePullsWhatItNeedsOnOpen(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	s, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "每日推荐拉回来了", func() bool { return showsText(tt, "每日推荐 1") })
	waitFrames(t, tt, "热榜拉回来了", func() bool { return b.hit(uiHomeToplistMusic) })

	if n := b.countHit(uiHomeRegionFeed); n != 1 {
		t.Errorf("分区推荐发了 %d 次，want 1", n)
	}
	if got := b.lastQueryOf(uiHomeRegionFeed).Get("from_region"); got != "1003" {
		t.Errorf("每日推荐的 from_region = %q，want 1003（音乐分区）", got)
	}
	if b.hit(uiHomeNewMusic) || b.hit(uiHomeNewBanner) {
		t.Error("还停着「推荐」就把新歌速递拉了——它等点到「新歌」才拉")
	}

	// 每日推荐只摆 6 张，哪怕 B 站给了 15 条；行身份是 bvid（铁律四）。
	if showsText(tt, "每日推荐 7") {
		t.Error("每日推荐摆了第 7 张，原型只有 6 张")
	}
	songs := s.home.feed.sec[homeDaily].songs
	if len(songs) == 0 || songs[0].ID != "BV1home1" {
		t.Errorf("行身份 = %+v，want 第一首是 BV1home1", songs)
	}
}

// TestTheNewTabPullsNewMusicOnceAndShowsTheUploadAge 盯「新歌」档：点到才拉、只拉一次、
// 那一列写「x 天前上传」。
func TestTheNewTabPullsNewMusicOnceAndShowsTheUploadAge(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "首页起来了", func() bool { return showsText(tt, "每日推荐 1") })

	clickHomeTab(t, tt, homeTabNew)
	waitFrames(t, tt, "新歌速递拉回来了", func() bool { return showsText(tt, "新歌速递里那首") })
	if !b.hit(uiHomeNewMusic) || !b.hit(uiHomeNewBanner) {
		t.Error("点到「新歌」之后没拉新歌速递的列表 / 头图位")
	}
	if !tt.HasText("上传") {
		t.Errorf("「专辑」那一列没写「x 天前上传」，文字有 %q", tt.Texts())
	}

	// 切走再切回来不重拉。
	before := b.countHit(uiHomeNewMusic)
	clickHomeTab(t, tt, homeTabRec)
	clickHomeTab(t, tt, homeTabNew)
	if after := b.countHit(uiHomeNewMusic); after != before {
		t.Errorf("切走再切回来重拉了新歌速递（%d → %d）", before, after)
	}
}

// TestTheChartTabListsTheTopTwentyWithATrendColumn 盯「榜单」档：只画前 20 名，每一行有升降
// 那一格（没有上一期记录时是「—」）。
func TestTheChartTabListsTheTopTwentyWithATrendColumn(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "热榜拉回来了", func() bool { return b.hit(uiHomeToplistMusic) })

	clickHomeTab(t, tt, homeTabChart)
	waitFrames(t, tt, "榜上第一名画出来了", func() bool { return showsText(tt, "榜上第 1 名") })
	if !tt.HasText("本周最热") || !tt.HasText("播放前 20") {
		t.Errorf("没有「本周最热」和「播放前 20」，文字有 %q", tt.Texts())
	}
	if showsText(tt, "榜上第 21 名") {
		t.Error("榜单画了第 21 名，原型只有前 20")
	}
	if !showsText(tt, "—") {
		t.Errorf("没有上一期记录时升降应该是「—」，文字有 %q", tt.Texts())
	}
}

// TestAFailedSectionSaysSoAndOnlyRetriesWhenAsked 盯失败那一支：说清为什么、带重试、
// 失败之后每帧不许重发（那是自己打自己）、点了「重试」才再来一次。
func TestAFailedSectionSaysSoAndOnlyRetriesWhenAsked(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) { uiExploreJSON(w, -403, nil) })
	// 窗口拉高：「重试」在「今日一张」下面，默认 800 高的窗口里它在折线以下。
	_, tt, _ := newHomeShell(t, b, at(homeW, 1600))

	waitFrames(t, tt, "每日推荐拉失败", func() bool { return showsText(tt, "这一块没拉出来") })
	hits := b.countHit(uiHomeRegionFeed)
	for range 30 {
		tt.Frame()
	}
	if after := b.countHit(uiHomeRegionFeed); after != hits {
		t.Fatalf("失败之后每帧在重发：%d → %d 次", hits, after)
	}

	// 修好，点「重试」。
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) { uiExploreJSON(w, 0, homeArchiveData()) })
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("点不到「重试」：%v，文字有 %q", err, tt.Texts())
	}
	waitFrames(t, tt, "重试之后拉回来了", func() bool { return showsText(tt, "每日推荐 1") })
	if after := b.countHit(uiHomeRegionFeed); after != hits+1 {
		t.Errorf("点一次「重试」应该多发一趟：%d → %d", hits, after)
	}
}

// TestTheHeroPlayButtonQueuesTheTwoTracks 盯「今日一张」：「播放」把那两首换进队列从第一首
// 起播，「全部加入队列」把它们追加到队尾。
func TestTheHeroPlayButtonQueuesTheTwoTracks(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	s, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "热榜拉回来了", func() bool { return len(s.home.feed.sec[homeChart].songs) > 0 })
	homeFrame(tt)

	var played, added [][]app.Item
	s.home.acts.Play = func(items []app.Item) { played = append(played, items) }
	s.home.acts.Add = func(items []app.Item) { added = append(added, items) }

	if err := tt.Click("播放"); err != nil {
		t.Fatalf("点不到「播放」：%v，文字有 %q", err, tt.Texts())
	}
	if len(played) != 1 || len(played[0]) != heroTrackCount {
		t.Fatalf("「播放」换进队列的是 %v，want 一次、%d 首", played, heroTrackCount)
	}
	if err := tt.Click("全部加入队列"); err != nil {
		t.Fatalf("点不到「全部加入队列」：%v", err)
	}
	if len(added) != 1 || len(added[0]) != heroTrackCount {
		t.Fatalf("「全部加入队列」追加的是 %v，want 一次、%d 首", added, heroTrackCount)
	}
	want := heroPick(s.home.feed.sec[homeChart].songs, s.home.day)
	if played[0][0].Bvid != want[0].Item.Bvid {
		t.Errorf("起播的第一首 = %q，want 今日一张 %q", played[0][0].Bvid, want[0].Item.Bvid)
	}
}

// TestHeroPickIsStableWithinADay 盯「今日一张」的取法：同一天不变，第二天换一首，
// 不会选到没封面或放不了的条目。
func TestHeroPickIsStableWithinADay(t *testing.T) {
	songs := make([]list.Music, 0, 12)
	for i := range 12 {
		m := list.Music{ID: "id" + strconv.Itoa(i), Title: "歌" + strconv.Itoa(i), Cover: "https://f/c.jpg",
			Item: app.Item{Bvid: "BV" + strconv.Itoa(i)}}
		if i%3 == 0 {
			m.Cover = "" // 没封面
		}
		songs = append(songs, m)
	}
	day := time.Date(2026, 11, 4, 21, 0, 0, 0, time.Local)
	a := heroPick(songs, day)
	b := heroPick(songs, day.Add(2*time.Hour))
	if len(a) != heroTrackCount || a[0].ID != b[0].ID || a[1].ID != b[1].ID {
		t.Fatalf("同一天选出的不一样：%v vs %v", a, b)
	}
	for _, m := range a {
		if m.Cover == "" || m.Item.Bvid == "" {
			t.Errorf("选到了没封面 / 放不了的条目 %+v", m)
		}
	}
	next := heroPick(songs, day.AddDate(0, 0, 1))
	if next[0].ID == a[0].ID {
		t.Errorf("第二天还是同一首 %q", a[0].ID)
	}
	if got := heroPick(nil, day); got != nil {
		t.Errorf("没有歌时选出了 %v", got)
	}
}

// TestTheSmallTextHelpers 钉问候语、日期、时长、上传时间、升降那几个纯函数的口径。
func TestTheSmallTextHelpers(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 4, h, 30, 0, 0, time.Local) }
	for h, want := range map[int]string{0: "夜深了", 4: "夜深了", 5: "早上好", 10: "早上好", 11: "中午好",
		13: "中午好", 14: "下午好", 17: "下午好", 18: "晚上好", 23: "晚上好"} {
		if got := homeGreeting(at(h)); got != want {
			t.Errorf("%d 点问候 = %q，want %q", h, got, want)
		}
	}
	if got := homeDateText(at(9)); got != "11 月 4 日 · 星期三" {
		t.Errorf("日期 = %q", got)
	}
	if got := homeClock(12*time.Hour + 40*time.Minute); got != "12:40" {
		t.Errorf("本周已听 = %q", got)
	}
	if got := homeClock(0); got != "0:00" {
		t.Errorf("零时长 = %q", got)
	}
	now := at(12)
	for _, c := range []struct {
		d    time.Time
		want string
	}{
		{time.Time{}, "最近上传"},
		{now, "今天 上传"},
		{now.AddDate(0, 0, -1), "昨天 上传"},
		{now.AddDate(0, 0, -3), "3 天前 上传"},
	} {
		if got := homeDaysAgo(c.d, now); got != c.want {
			t.Errorf("homeDaysAgo(%v) = %q，want %q", c.d, got, c.want)
		}
	}
	for _, c := range []struct {
		delta int
		ok    bool
		want  string
		up    bool
	}{{3, true, "↑ 3", true}, {-2, true, "↓ 2", false}, {0, true, "—", false}, {5, false, "—", false}} {
		if got, up := homeTrendText(c.delta, c.ok); got != c.want || up != c.up {
			t.Errorf("homeTrendText(%d,%v) = %q,%v want %q,%v", c.delta, c.ok, got, up, c.want, c.up)
		}
	}
}

// TestACardThatCannotBePlayedSaysSoInsteadOfDoingNothing 盯「放不了的歌点了要说一声」：榜上有些
// 条目没有稿件（`Type` 是 mv 而 `Bvid` 是空串），按下去不能没反应，也不能把它塞进队列。
func TestACardThatCannotBePlayedSaysSoInsteadOfDoingNothing(t *testing.T) {
	b := newTestShell(t, at(homeW, 1600))
	s, tt := b.s, b.tt
	daily := homeSongsOf("推荐", 3)
	daily[0].Item = app.Item{Source: app.SourceOnline, Type: "mv", Title: daily[0].Title}
	// 本地文件总是放得了的（走开文件那一路），拿它当「放得了」的那一首。
	daily[1].Item = app.LocalItem(flacPath(t))
	chart := homeSongsOf("榜上", 3)
	for i := range chart {
		chart[i].Cover, chart[i].Item.Bvid = "x", "BV1chart"+strconv.Itoa(i) // 让「今日一张」从榜里选，不和卡片抢字
	}
	s.home.SetSongs(daily, chart, nil, nil)
	var played [][]app.Item
	s.home.acts.Play = func(items []app.Item) { played = append(played, items) }
	homeFrame(tt)

	// 点封面：封面那枚按钮的无障碍名字就是歌名（卡片标题那行字不是按钮）。
	if err := tt.Click("推荐 1"); err != nil {
		t.Fatalf("点不到第一张卡：%v，文字有 %q", err, tt.Texts())
	}
	homeFrame(tt)
	if len(played) != 1 || len(played[0]) != 1 || !played[0][0].IsLocal() {
		t.Fatalf("点第一张（放不了）之后换进队列的是 %+v，want 只有往后第一首放得了的那一首", played)
	}
}

// TestTheHomePageUsesThePrototypePageMargins 钉首页的页边距（原型 `.page { padding: 8px 40px 64px }`
// 和两条媒体查询）：窗口 > 1024 是 40，> 760 是 32，再窄 16；顶上是页顶 8 + 问候区 24。
//
// 数是**窗口宽**判的（媒体查询看视口），不是内容区——见 `homeLayoutFor`。
func TestTheHomePageUsesThePrototypePageMargins(t *testing.T) {
	for win, want := range map[float32]float32{1600: 40, 1200: 40, 1025: 40, 1024: 32, 800: 32, 761: 32, 760: 16, 600: 16} {
		if got := homeLayoutFor(win).pad; got != want {
			t.Errorf("窗口 %v 的页边距 = %v，want %v", win, got, want)
		}
	}

	// 画面上：1200 的窗口里，最靠左的墨（Eyebrow / 标题的左缘）在侧栏右边 40。
	b := newTestShell(t, at(1200, 800))
	b.s.home.SetSongs(homeSongsOf("推荐", 6), homeSongsOf("榜上", 20), nil, nil)
	for range 3 {
		b.tt.Frame()
	}
	sideW := app.DefaultSideMenuWidth
	area := contentArea(sideW)
	if got, want := leftmostInk(b.tt.Image(), area), int(sideW)+40; got < want-2 || got > want+2 {
		t.Errorf("首页的内容从 x=%d 开始，want %d（侧栏 %d + 页边距 40）", got, want, int(sideW))
	}
	// 顶上：页顶 8 + 问候区 24 = 32，再加 Eyebrow 字形自己的上沿留白（几像素）。
	lo, _, ok := firstBand(b.tt.Image(), area)
	if !ok {
		t.Fatalf("首页顶上一点墨都没有，文字有 %q", b.tt.Texts())
	}
	if top := lo - navMinHeight; top < 28 || top > 42 {
		t.Errorf("首页的第一段墨从顶栏下缘往下 %d 像素开始，want 32 附近（页顶 8 + 问候区 24）", top)
	}
}
