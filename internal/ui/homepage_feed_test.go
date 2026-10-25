package ui

// 首页那一摊**取数**的测试。
//
// 这一组盯的是首页从「一句暂无内容」变成「有歌」之后新长出来的那几条：
//
//  1. **只拉正在看的那一档**，新歌速递只在「流行」上拉；
//  2. **三种状态是三种样子**：正在拉 / 拉不到（有原因、有重试）/ 真的没有；
//  3. **失败之后只有人能让它再来一次**（不是每帧重发）；
//  4. **换账号重来一遍**（推荐是按账号的）。
//
// 取数本身（参数、签名、字段怎么落）由服务层和 app 层那两组钉
// （`internal/service/bili/recommend_test.go`、`internal/app/recommend_test.go`）。
// 断言面是**画面上有什么字**，和这一包其它用例一个路子。
//
// ⚠️ 假 B 站复用空间/动态那一组的那台（`newUIExplore`）：它已经带 WBI 密钥那
// 一段（分区推荐要签名，签名之前先打 nav 拿 key）。自己再造一台的话这里要多
// 抄一遍那二十行，而抄出来的那份迟早和那边分叉。

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// 首页那几个接口的路径（写字符串是为了测试自己看得见自己断言的是什么；出处是
// app 层那一份和 `service/bili/recommend.go`）。
const (
	uiHomeRegionFeed = "/x/web-interface/region/feed/rcmd"
	uiHomeMusicRank  = "/x/centralization/interface/music/comprehensive/web/rank"
	uiHomeNewMusic   = "/x/centralization/interface/new/music"
	uiHomeNewBanner  = "/x/centralization/interface/new/music/banner"
)

// newHomeShell 装一个指着假 B 站的首页。
//
// ⚠️ 和 `newSpaceShell` 同一个形状，所以也走同一个基座：`MountLogin` 建**那一个**
// 客户端，然后把它递给 `MountRecommend`（建第二个客户端的话登录态、WBI 密钥、
// 请求头都不是同一份，而症状是「首页一直 -101」）。而 Mount* 都要在建外壳
// **之前**挂上，收尾（四个定时器、封面缓存、应用、设置/历史/快捷键的落盘）
// 由 `newTestShell` 无条件挂上——少挂一次就是一组挂到测试二进制结束的 goroutine。
//
// ⚠️ **opts 排在最后**：出图的那几组要换深浅（`systemIsDark`），而换主题
// 预设要挂一份带方案名的设置（`withSettings`）——那两样都是基座的活。
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

// homeArchiveData 造一份分区推荐的 data 段。region 是「音乐」还是「鬼畜」。
//
// ⚠️ 两档给的歌**不一样**：同一个实现回同一批的话「切到鬼畜还是那一批」是绿的。
//
// ⚠️ **给的是一整页（15 首）**，不是一首两首：分区推荐一页就是 15 条（上游的
// `REGION_PAGE_SIZE = 15`，见 `service/bili/recommend.go`），而**首页的列表
// 滚到底会自动拉下一段**（票 #370）——一页填不满一屏的话它会接着拉，于是
// 「这一档只发了一个请求」那种断法就数不准了（它数的是请求，而请求有几回取决
// 于屏幕装不装得下）。
const homePageSize = 15

func homeArchiveData(region string) map[string]any {
	archives := make([]map[string]any, 0, homePageSize)
	for i := range homePageSize {
		cover := fmt.Sprintf("https://f/home%d.jpg", i+1)
		if i == 0 {
			cover = "https://f/one.jpg"
		}
		if i == 1 {
			cover = "https://f/two.jpg"
		}
		archives = append(archives, map[string]any{
			"aid": 11 + i, "bvid": fmt.Sprintf("BV1home%d", i+1), "cid": 101 + i,
			"duration": 185,
			"title":    fmt.Sprintf("%s 推的第%s首", region, homeOrdinal(i+1)),
			"cover":    cover,
			"author": map[string]any{
				"mid": 7, "name": region + " 的 UP 主",
			},
			"stat": map[string]any{"view": 1000},
		})
	}
	return map[string]any{"archives": archives}
}

// homeOrdinal 是第 n 个汉字数词（首页那几首的歌名用它）。
func homeOrdinal(n int) string {
	switch n {
	case 1:
		return "一"
	case 2:
		return "二"
	default:
		return strconv.Itoa(n)
	}
}

// homeArchives 给一台假 B 站装上首页那几个接口：分区推荐按 `from_region` 回不同
// 的歌，音乐榜和两个新歌速递各给一首。
func homeArchives(b *uiExplore) {
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, r *http.Request) {
		region := "音乐"
		if r.URL.Query().Get("from_region") == "1007" {
			region = "鬼畜"
		}
		uiExploreJSON(w, 0, homeArchiveData(region))
	})
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"id": 31, "music_title": "榜上那首", "bvid": "BV1top", "aid": 31, "cid": 31,
			"related_archive": map[string]any{
				"bvid": "BV1rank", "cid": 310, "title": "流行 榜上的歌",
				"uid": 9, "username": "榜上的 UP", "vv_count": 4242, "duration": 190,
			},
		}}})
	})
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"id": 5, "music_title": "新歌速递里那首", "bvid": "BV1new", "aid": 5, "cid": 5,
			"cover": "https://f/new.jpg", "author": "新歌的 UP 主",
		}}})
	})
	b.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"music_id": "m1", "archive_title": "新歌速递的头图", "bvid": "BV1ban",
			"aid": 6, "cid": 6, "cover": "https://f/ban.jpg", "author": "头图的 UP 主",
		}}})
	})
}

// TestTheHomePagePullsTheTabYouAreLookingAt 盯「只拉正在看的那一档」。
//
// 三档各用各的接口（分区推荐的两个分区号 + 音乐榜），而**没用上的那两档一个
// 请求都不该有**。启动就把三摊都拉一遍的话，用户还没点开就等着两个用不上的
// 往返——而 B 站的推荐接口不快。
func TestTheHomePagePullsTheTabYouAreLookingAt(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	s, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "首页第一档的推荐", func() bool {
		return showsText(tt, "音乐 推的第一首")
	})

	if n := b.countHit(uiHomeRegionFeed); n != 1 {
		t.Errorf("分区推荐发了 %d 次，want 1", n)
	}
	if got := b.lastQueryOf(uiHomeRegionFeed).Get("from_region"); got != "1003" {
		t.Errorf("第一档的 from_region = %q，want 1003", got)
	}
	// 另外两档一个都不该被碰过。
	if b.hit(uiHomeMusicRank) {
		t.Error("还停着第一档就去拉音乐榜了")
	}
	if b.hit(uiHomeNewMusic) || b.hit(uiHomeNewBanner) {
		t.Error("还停着第一档就去拉新歌速递了——它只画在「流行」上")
	}

	// 卡片上那三样：标题、UP 名、封面（🔴 铁律四的身份也在这行里）。
	//
	// ⚠️ **走 `List().At(0)`**：三个 Tab 现在是三个翻页列表（`list.PagedList`），
	// 而行在底下那个 `list.List` 里（翻页那一层只管「拉页、翻页、哨兵」）。
	m, ok := s.home.lists[0].List().At(0)
	if !ok {
		t.Fatal("第一档一行歌都没有")
	}
	if m.ID != "BV1home1" {
		t.Errorf("行身份 = %q，want BV1home1（拿 bvid 当身份，见 app 层 recommendID）", m.ID)
	}
	if m.Owner != "音乐 的 UP 主" {
		t.Errorf("UP 名 = %q", m.Owner)
	}
	if m.Cover != "https://f/one.jpg" {
		t.Errorf("封面 = %q", m.Cover)
	}
	if m.Item.Bvid != "BV1home1" || m.Item.Artist != "音乐 的 UP 主" {
		t.Errorf("播放项 = %+v", m.Item)
	}
}

// TestEachTabPullsItsOwnList 每一档拉的是**自己的**那一份请求。
//
// ⚠️ **判据要等被断的那两样本身，而不是等「画面上出现了某句话」**：`from_region`
// 和「打没打音乐榜那个接口」都是 fixture 那边记的**请求**，而画面是这些请求回来
// **之后**画的；而 `waitFrames` 的注释写明了「同一帧里画出来的还是上一帧的样子」。
// 只等画面就断请求，机器一忙（或者上一次那一档的请求还在路上）那条断言就红——
// 而功能是好的：这一条在**未改动的 main 上单跑 30 次就红过**。
//
// ⚠️ **断的是「点了这一档之后打出去的那一趟」，不是「最后一次」**：两档的请求同时
// 在路上，而 fixture 记的是**到达**先后——满载时上一档那一趟可能后到（探针量过：
// 同一个进程塞满 CPU 跑 200 轮，5 轮读到的是上一档的 region）。问「最后一次」
// 的话那个条件永远不成立，10 秒之后保险丝断掉，症状是「等了 10s」，离真因隔着
// 三层。理由见 `askedRegionAfter`。
func TestEachTabPullsItsOwnList(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	_, tt, _ := newHomeShell(t, b)

	before := b.countHit(uiHomeRegionFeed)
	clickTab(t, tt, 1)
	waitFrames(t, tt, "鬼畜那一档拉的是自己那份", func() bool {
		return showsText(tt, "鬼畜 推的第一首") &&
			b.askedRegionAfter(uiHomeRegionFeed, before, "1007")
	})
	if !b.askedRegionAfter(uiHomeRegionFeed, before, "1007") {
		t.Errorf("鬼畜那一档没有用 from_region=1007 拉过，区域 %v", b.lastQueryOf(uiHomeRegionFeed))
	}

	clickTab(t, tt, 2)
	waitFrames(t, tt, "流行那一档打的是音乐榜那个接口", func() bool {
		return showsText(tt, "流行 榜上的歌") && b.hit(uiHomeMusicRank)
	})
	if !b.hit(uiHomeMusicRank) {
		t.Error("「流行」那一档没有打音乐榜那个接口")
	}
}

// TestNewMusicIsOnlyPulledOnThePopTab 盯新歌速递的取数也在「流行」上。
//
// ⚠️ **连请求都不该有**：上游那块组件只在 `activeTab === "pop"` 时才挂载
// （`index.tsx:295`），它的 `useEffect` 也就跟着只在那时候跑。我们这四摊是
// 各拉各的，所以这一条钉的是「什么时候发」，不是「画不画」——
// 「画不画」由 homepage_test.go 那一条钉。
func TestNewMusicIsOnlyPulledOnThePopTab(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "第一档的推荐", func() bool { return showsText(tt, "音乐 推的第一首") })
	for range 3 {
		clickTab(t, tt, 0)
		homeFrame(tt)
	}
	if b.hit(uiHomeNewMusic) || b.hit(uiHomeNewBanner) {
		t.Error("一直停着「音乐」那一档，却把新歌速递拉了")
	}

	clickTab(t, tt, homePopTab)
	waitFrames(t, tt, "新歌速递", func() bool { return showsText(tt, "新歌速递的头图") })
	if !b.hit(uiHomeNewMusic) {
		t.Error("切到「流行」之后没拉新歌速递的列表")
	}
	if !b.hit(uiHomeNewBanner) {
		t.Error("切到「流行」之后没拉新歌速递的头图位")
	}
	// ⚠️ 切走之后**不许重来**：上游每切一次都重拉，我们不——用户盯着的那一首
	// 不能因为切了个 Tab 就换成另一批的同一首。
	before := b.countHit(uiHomeNewMusic)
	clickTab(t, tt, 0)
	clickTab(t, tt, homePopTab)
	waitFrames(t, tt, "切回「流行」", func() bool { return showsText(tt, "新歌速递的头图") })
	if after := b.countHit(uiHomeNewMusic); after != before {
		t.Errorf("切走再切回来重拉了新歌速递（%d → %d）", before, after)
	}
}

// TestTheHomePageSaysItIsLoadingUntilTheFirstPageIsBack 盯「正在拉」那一支。
//
// ⚠️ 拉的那一段**不许**画成「暂无内容」：那是「B 站真的没有推荐」的说话方式，
// 而用户读到它只会以为这一页是空的。
func TestTheHomePageSaysItIsLoadingUntilTheFirstPageIsBack(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	gate := make(chan struct{})
	// 把那一趟按住在门口，直到用例放它过去。
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		region := "音乐"
		if r.URL.Query().Get("from_region") == "1007" {
			region = "鬼畜"
		}
		uiExploreJSON(w, 0, homeArchiveData(region))
	})
	t.Cleanup(func() {
		// 用例在断言完之前就 return 了的话，别把收尾卡在这一趟上。
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "那一趟已经发出去了", func() bool { return b.hit(uiHomeRegionFeed) })
	homeFrame(tt)

	if !showsText(tt, "正在加载音乐推荐") {
		t.Fatalf("没在说「正在加载」，文字有 %q", tt.Texts())
	}
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("正在拉的时候画了「%s」，文字有 %q", lookup(homePath).Empty.Title, tt.Texts())
	}

	// 放它过去：那一屏自己换掉。
	close(gate)
	waitFrames(t, tt, "拉回来之后换成歌", func() bool { return showsText(tt, "音乐 推的第一首") })
}

// TestAFailedRecommendSaysSoAndOffersARetry 盯失败那一支的三件事。
//
//  1. **说的话是「拉不到」而不是「没有」**，而且带一个重试；
//  2. 失败之后**每帧不许重发**（那是自己打自己，实测 300 毫秒能发 138 次）；
//  3. 修好了点「重试」能重来。
//
// 断的是 -403：风控在真实环境里最常见（见 recommend.go 里「哪几个要签名」那段）。
func TestAFailedRecommendSaysSoAndOffersARetry(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "拉失败", func() bool { return showsText(tt, "这一块没拉出来") })

	// 一句人话，不是 B 站的 code。
	if !showsText(tt, "没有权限看这一项") {
		t.Errorf("没说清楚为什么失败，文字有 %q", tt.Texts())
	}
	// ⚠️ 「没有内容」那一句**不许**出现：用户读到它会以为 B 站没有推荐给他。
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("失败了却画「%s」，文字有 %q", lookup(homePath).Empty.Title, tt.Texts())
	}
	// 重试按钮必须量得出来、点得到（0×0 的按钮 `Find` 找得到文字，点下去什么都不会发生）。
	r, ok := tt.Find("重试加载音乐推荐")
	if !ok {
		t.Fatalf("没有重试按钮，文字有 %q", tt.Texts())
	}
	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("「重试加载音乐推荐」是 %.0fx%.0f，点不到", r.W, r.H)
	}

	// 失败之后 30 帧不许再发请求。
	for range 30 {
		tt.Frame()
	}
	if n := b.countHit(uiHomeRegionFeed); n > 1 {
		t.Errorf("失败之后还请求了 %d 次（>1）：`viewHome` 每帧都在 pull", n)
	}

	// 修好假 B 站再点一次：它必须能重来。
	homeArchives(b)
	if err := tt.Click("重试加载音乐推荐"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "重试之后拉回来", func() bool { return showsText(tt, "音乐 推的第一首") })
	if showsText(tt, "这一块没拉出来") {
		t.Errorf("重试成功了还画着失败，文字有 %q", tt.Texts())
	}
}

// TestAnEmptyRecommendListIsNotAFailure 盯「真的没有」那一支。
//
// ⚠️ B 站给了空的**不是失败**：界面上该是「暂无内容」那一对（它说了「怎么才能
// 不空」），**不许**出现那句「拉不到」和一个重试按钮。画错了的话用户会去按
// 一个按多少次都是空的重试。
func TestAnEmptyRecommendListIsNotAFailure(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"archives": []map[string]any{}})
	})
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "拉回来（空的）", func() bool {
		return showsText(tt, lookup(homePath).Empty.Title)
	})
	homeFrame(tt)

	if showsText(tt, "这一块没拉出来") {
		t.Errorf("空的推荐单被说成了「拉不到」，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("重试加载音乐推荐"); ok {
		t.Error("空的推荐单上摆了一个重试按钮——按多少次都是空的")
	}
}

// TestTheNewMusicBlockFailsOnItsOwn 盯新歌速递那一块的失败不牵连下面那档。
//
// ⚠️ 新歌速递是**另一块**（另一对接口）。它挂了的时候下面那个列表照常显示，
// 而那一块自己说「没拉出来」+ 给一个重试。合成一句「这一页拉不到」的话，
// 用户会以为三档的推荐全没了。
//
// ⚠️ **判据要等这三件本身，而不是只等第一件**：「新歌速递没拉出来」和下面那一档
// 是**两个请求、两个时间点**落地的（而 `waitFrames` 的注释写明了「同一帧里画
// 出来的还是上一帧的样子」）。只等第一件就断言第二件，机器一忙那条断言就红——
// 而功能是好的：这一条实测在整包连跑里飘过两次，**在未改动的 main 上也飘**。
// 判据放宽了吗？没有——三件都要求成立，一件不少，只是**等到了再断言**。
func TestTheNewMusicBlockFailsOnItsOwn(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -500, nil)
	})
	b.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -500, nil)
	})
	_, tt, _ := newHomeShell(t, b)

	clickTab(t, tt, homePopTab)
	waitFrames(t, tt, "新歌速递挂了、下面那一档照画", func() bool {
		_, retry := tt.Find("重试加载新歌速递")
		return showsText(tt, "新歌速递没拉出来") &&
			showsText(tt, "流行 榜上的歌") && retry
	})
	// 下面那一档的推荐照画。
	if !showsText(tt, "流行 榜上的歌") {
		t.Errorf("新歌速递挂了把下面那一档也带走了，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("重试加载新歌速递"); !ok {
		t.Errorf("新歌速递那一块没有重试按钮，文字有 %q", tt.Texts())
	}
}

// TestThePopTabReadsThePayloadBiliActuallySends 盯**真实形态**的返回能从
// 「流行」那一档一路走到画面上。
//
// ⚠️ 这一条是补一个真出过事的洞：这一族接口的 `aid` / `cid` / `uid` 真会发成
// **JSON 字符串**（Biu 上游 `src/service/service.md` 里音乐榜那份原样返回的参数），
// 而服务层最早按 `int64` 解，于是「流行」整档解码失败 → 界面上是「这一块没拉出来
// / 推荐接口的返回读不懂 / 重试加载流行推荐」。那时候这一包 26 条用例全绿——**它们
// 喂的全是自己编的数字**。
//
// 所以这里不自己编：直接照抄那份真实样件（歌名/UP 名/id 都用它给的）。
func TestThePopTabReadsThePayloadBiliActuallySends(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	// 照抄 Biu 上游 `src/service/service.md` 里 `music/comprehensive/web/rank`
	// 那一份返回（只改了封面地址，免得测试去连真网）。
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"music_title": "I Met A Boy",
			"music_id":    "MA584087591096195920",
			"cid":         "32447137298",
			"aid":         "115182114246511",
			"bvid":        "BV1DFHdzjEkK",
			"author":      "Mimi Webb",
			"cover":       "https://f/rank.jpg",
			"id":          int64(3593581),
			"score":       int64(212),
			"related_archive": map[string]any{
				"bvid": "BV1Nhp4z4EVa", "cid": "115219862986158", "aid": "115219862986158",
				"cover": "https://f/arc.jpg", "title": "【MV】「I Met A Boy」",
				"uid": int64(384580074), "username": "fallinforjulia",
				"vv_count": int64(75), "duration": int64(179), "fname": "MV",
			},
		}}})
	})
	_, tt, _ := newHomeShell(t, b)

	clickTab(t, tt, homePopTab)
	waitFrames(t, tt, "「流行」那一档画出歌来", func() bool {
		return showsText(tt, "【MV】「I Met A Boy」")
	})
	// 失败那一支一个字都不许有：它就是当时看到的那个症状。
	if showsText(tt, "这一块没拉出来") {
		t.Errorf("真实形态的返回没能读出来，文字有 %q", tt.Texts())
	}
	if !showsText(tt, "fallinforjulia") {
		t.Errorf("挂着的稿件的 UP 名没画出来，文字有 %q", tt.Texts())
	}
}

// TestLoggingInReloadsTheRecommendations 盯换账号重来。
//
// ⚠️ 推荐是**按账号**的：B 站拿 mid 算口味。带着没登录时那一批继续给登录后的
// 用户看，界面上没有任何东西提示这一点——而那正是用户以为「B 站的推荐就是
// 这样的」的那一刻。
func TestLoggingInReloadsTheRecommendations(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	_, tt, a := newHomeShell(t, b)

	waitFrames(t, tt, "没登录时那一批", func() bool { return showsText(tt, "音乐 推的第一首") })
	if n := b.countHit(uiHomeRegionFeed); n != 1 {
		t.Fatalf("登录之前就拉了 %d 次，want 1", n)
	}

	// 登录（换了一个 mid）之后必须重来一遍。
	a.TestOnlyAdoptLogin("77")
	waitFrames(t, tt, "登录之后重来一遍", func() bool {
		return b.countHit(uiHomeRegionFeed) >= 2
	})
}
