package ui

// 这一组钉的是一批**「有加载态、但画得看不见」**的地方（2026-10-27）。
//
// 每一条的错法都是静默的：不报错、不崩、不写日志，界面只是「看着不对劲」——
// 一屏封面在等的时候是一排空白、拉失败之后那块灰永远不变、切歌那一瞬间就写
// 「暂无歌词」。所以判据全是**画面上**的东西（颜色、有没有字、是不是动）。

import (
	"image"
	"net/http"
	"sort"
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheCoverLoadingFillIsVisibleOnALightPage 钉住「封面加载中」那一档的底色
// **在浅色页面上看得出来**。
//
// 🔴 **判据是「实打实画出来的像素」**：那一档原先是一个 `Surface` 往
// `Background` 混一半的平色（#fafafa），压在 #ffffff 的页面上**每通道只差 4**
// ——一屏 48 像素的封面在浅色下仍然是一排空白。现在它是**前景色 8%**
// （约 #ebebeb），每通道差 20。
//
// ⚠️ **取样点必须在「高光带扫不到」的位置，而且要真的画一遍**：上一版这一条
// 是拿 `shimmer.BaseAlpha` 现算一遍数学——把实现改回旧的混色它照样绿（实测过），
// 那是**假测试**。现在画一格、读像素。
//
// ⚠️ **怎么保证取样点不在高光带上**：`shimmer` 的高光是一道 35% 宽的竖带，
// 按钟从左扫到右。取**左上角往里 10 像素**那个点，然后在若干个相位上各取一次——
// 只要**有一点**落在带外，那一点就是纯底色。判据用「最暗的那一次」（带外的
// 那一次），因为高光只会把它提亮。
func TestTheCoverLoadingFillIsVisibleOnALightPage(t *testing.T) {
	b := newTestShell(t, at(400, 300), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			imgcache.Loading(c, 200, 200, imgcache.Shape{})
		}
	}))
	// 🔴 **判据不认主题色，只认「这一格和它旁边的空白页差多少」**：
	//
	// 上一版拿 `BuildTheme(设置, 浅色)` 去算页面底色——而画面上那一份是
	// `applyFrameTheme` 装的（默认主题方案把底色换成了 #f8f4ed），两者差 4~8 个
	// 通道值，于是在门槛边上翻脸（实测量到 27，真实值 48）。
	//
	// 现在两边都从**同一张图**上取：那一格里的取样点 vs 它右边那块空白页。
	b.tt.Frame()

	// ⚠️ **多取几个点、每个点扫几个相位、最后取最小值**：高光是一道 35% 宽的
	// 竖带按钟扫过，单点单次很可能正落在带上（那就量成了「带里最亮的色」）。
	// 取最小值等于「这个格子最暗的那一处、最暗的那一次」——也就是**纯底色**。
	//
	// 往里 8 稳住：避开抗锯齿的边，也避开正中那枚音符图标（它是 `TextMuted`
	// 再往背景色混 25%，比底色深，会把「差多少」量成图标和底色的差）。
	// 那一格是 200×200，画在 (0,0)。页面底从**窗口里**取——(250,150) 是那一格
	// 右边那块空白页（别取到 300,300：那已经在窗口外，读回来是全透明，差值算出
	// 来是个假数）。
	const pageX, pageY = 250, 150

	// 🔴 **判据是「整格像素里和页面底色的距离的中位数」**：
	//
	// 扫光是一道 35% 宽的竖带在走，**任何单点都可能正落在带上**（实测最小差
	// 21 = 带上最亮那一处）；而正中那枚音符又把中间那一块拉深。取**中位数**
	// 等于「这个格子里占面积最大的那个色」，也就是**底色本身**——它不随相位变
	// （实测六个相位都是 48）。
	b.tt.Frame()
	base := b.tt.Image().RGBAAt(pageX, pageY)
	page := [3]uint8{base.R, base.G, base.B}

	img := b.tt.Image()
	var ds []int
	for y := 4; y < 196; y++ {
		for x := 4; x < 196; x++ {
			// 跳过正中那枚音符（`placeholderIconRatio * 200 = 80` 见方那一块）。
			if x > 60 && x < 140 && y > 60 && y < 140 {
				continue
			}
			p := img.RGBAAt(x, y)
			ds = append(ds, absChannel(p.R, page[0])+
				absChannel(p.G, page[1])+absChannel(p.B, page[2]))
		}
	}
	sort.Ints(ds)
	worst := ds[len(ds)/2]
	if worst < 40 {
		t.Fatalf("封面加载中那一档压在页面底色上**最暗**只差 %d（门槛 40）——"+
			"浅色下那等于一块空白，用户分不出「还在加载」和「压根没有图」", worst)
	}
}

// absChannel 是两个通道值的绝对差。
func absChannel(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// TestTheCoverLoadingCellShimmers 钉住「封面加载中」那一档**会动**。
//
// 🔴 **判据是两帧的像素**：扫光是一道从左往右走的高光，同一格隔一段时间取两次
// 样，颜色必须不同。不闪的话那一档只是一块灰——而一屏几十块灰看起来就是
// 「这一页坏了」。
func TestTheCoverLoadingCellShimmers(t *testing.T) {
	b := newTestShell(t, at(400, 300), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			imgcache.Loading(c, 200, 200, imgcache.Shape{})
		}
	}))
	b.tt.Frame()
	before := b.tt.Image()
	b.tt.Advance(shimmer.Period / 2)
	b.tt.Frame()
	after := b.tt.Image()

	if changedPixelsIn(before, after, 200, 200) == 0 {
		t.Fatal("隔了半个周期那一格一个像素都没变——「加载中」没有扫光，" +
			"一屏几十张封面在等的时候看起来是一排不动的灰块")
	}
}

// changedPixelsIn 数两张图里左上角 w×h 那一块有多少像素不一样。
func changedPixelsIn(a, b *image.RGBA, w, h int) int {
	n := 0
	bounds := a.Bounds()
	for y := range h {
		for x := range w {
			if x >= bounds.Dx() || y >= bounds.Dy() {
				continue
			}
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// TestTheAuthorPaneSaysItIsLoading 钉住动态流左栏第一页在途时**说的是「正在加载」**，
// 而不是画成「我一个都没关注」。
//
// 🔴 **判据是那句话在不在**：这一栏原先只有「没登录 / 失败 / 正常」三支，
// `auth.started` 还是 false 时直接画「全部」那一行——和「他真的一个都没关注」
// 一模一样（左栏 256 宽，量不出别的东西来）。
//
// ⚠️ **走的是真登录态那条路**：作者栏只在 `App.LoggedIn()` 为真时发请求。
// 这一条用假 B 站把登录态灌进去（`uiFollowLoggedIn` 那一套），然后让动态流
// 那个接口永远不回——于是作者栏一定停在「第一页在途」。
func TestTheAuthorPaneSaysItIsLoading(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	// 永远不回：作者栏那一趟卡在路上（等到测试结束那条连接才放）。
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	tt.Frame()
	tt.Frame()

	if !tt.HasText(loadingAuthorsText) {
		t.Fatalf("作者栏第一页在途，画面上却没有「%s」——用户会以为他真的一个都没关注；"+
			"文字有 %q", loadingAuthorsText, tt.Texts())
	}
}

// TestTheBoardHeroScrollsAwayWithTheList 钉住热榜 / 原创榜那一档的**页头跟着列表滚走**。
//
// 🔴 **2026-10-27 用户点名要的**：原先榜单那张头图（榜名 + 期数）和表头都在列表
// **外面**，滚列表时它们钉在顶上不动——一屏只剩半屏能看歌。现在它们是列表的
// 第 0 行（`list.Lead`），跟着内容一起滚走。
//
// 判据是**滚过之后那一块的位置**：钉住的会在原地，跟着滚的会往上（甚至出画面）。
func TestTheBoardHeroScrollsAwayWithTheList(t *testing.T) {
	startOnOverview(t)
	b := homeDiscoverFake(t)
	// ⚠️ **要多给几首**：`homeDiscoverFake` 只给一条，一屏装得下就压根滚不动——
	// 「滚了 200 之后它还在原地」会被读成「钉住的」，而那其实是「没滚动」。
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, r *http.Request) {
		title := "热榜第一名"
		if r.URL.Query().Get("list_id") == "9" {
			title = "原创榜第一名"
		}
		list := make([]map[string]any, 0, 40)
		for i := range 40 {
			list = append(list, map[string]any{
				"music_id": "MA" + strconv.Itoa(i), "music_title": title, "singer": "榜上歌手",
				"creation_bvid": "BV" + strconv.Itoa(i), "creation_aid": i,
				"creation_cover": "https://f/board.jpg", "creation_duration": 120, "creation_play": 5,
			})
		}
		uiExploreJSON(w, 0, map[string]any{"list": list})
	})
	s_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜列表", func() bool { return showsText(tt, "鬼畜") })
	// ⚠️ **锚点是页内头部那一行的 Label**（"榜单头图"），不是 `home:board:hero`
	// 那个 Key：后者是头图自己的键，而 `Tester.Find` 认的是**文字和 Label**。
	// 这一条要量的正是那一行（它跟着滚），所以锚在行上。
	waitFrames(t, tt, "榜单头图", func() bool { _, ok := tt.Find("榜单头图"); return ok })

	before, _ := tt.Find("榜单头图")
	// 往下滚一大段（头图 112 高，滚 200 一定扫过它）。
	//
	// ⚠️ **多画几帧**：滚轮是**带滑动**的（`Tester.Scroll` 那一段注释：
	// "a scroll container glides there over a moment"），只画一帧的话位置还在
	// 原地——那一帧读到的 y 会让人以为「它是钉住的」。
	tt.Scroll(homeW/2, homeH/2, 0, 1200)
	homeFrame(tt)

	// 🔴 **判据是「滚过之后那一块要么往上走了、要么已经不在画面上」**。
	//
	// ⚠️ **「不在画面上」单独不算过**：那一块要是**压根没画出来**（或者被别处
	// 一起删了），`Find` 同样拿不到——那也是「不在画面上」，但页头并没有跟着滚。
	// 所以先断「滚之前它确实在、而且位置合理」，再断「滚之后它不在了或者往上」。
	//
	// ⚠️ **`before.Y` 那一句不是在断「它在顶上」**（它本来就在列表第 0 行、
	// Tab 条下面），而是留一个能核对的数：出问题时那句话里带着两个 y。
	if before.Y <= 0 || before.Y > homeH {
		t.Fatalf("滚之前页头的位置就不在窗口里（y=%.0f）——这一条量不下去", before.Y)
	}
	after, stillThere := tt.Find("榜单头图")
	if stillThere && after.Y >= before.Y-1 {
		t.Errorf("滚了 1200 之后头图还在 y=%.0f（滚之前 y=%.0f）——"+
			"它是钉在顶上的，不是跟着列表滚走", after.Y, before.Y)
	}
	// 顺带把「滚动真的发生了」钉一下：`ScrollOffset` 得往前走过。
	if off := s_.home.lists[homeHotTab].ScrollOffset(); off <= 0 {
		t.Errorf("滚了 1200，列表报的滚动量还是 %.0f——滚动压根没发生，"+
			"上面那条「位置上移」就不成立", off)
	}
}

// TestTheNewMusicRowShowsSkeletonsInsteadOfVanishing 钉住总览上「新歌速递」在拉
// 的时候**摆骨架**，而不是整块消失。
//
// 🔴 **判据是「那一块还在不在」**：原先 `len(songs) == 0` 直接 return——连标题
// 一起不见，数据到了整块冒出来，下面的磁贴和排行榜跟着往下跳一截。
func TestTheNewMusicRowShowsSkeletonsInsteadOfVanishing(t *testing.T) {
	startOnOverview(t)
	// 一台永远不回的假 B 站：新歌速递那一趟卡在路上。
	b := newTestShell(t, append(blockedBiliOpts(t, true), at(1200, 800))...)
	tt := b.tt
	tt.Frame()
	tt.Frame()

	if !showsText(tt, "新歌速递") {
		t.Fatalf("总览在拉的时候「新歌速递」整块都不见了（文字有 %q）——"+
			"数据到了它才冒出来，下面的内容要往上跳一截", tt.Texts())
	}
	if !tt.HasText(loadingNewMusicText) {
		t.Errorf("那一块没有骨架（找不到 %q），文字有 %q", loadingNewMusicText, tt.Texts())
	}
}

// TestTheCollectionHeaderDoesNotClaimZeroMedia 钉住收藏夹页头**元信息没回来时
// 不写「0 条视频」**。
//
// 🔴 **那个 0 是假数**：`CollectionInfo` 在 `!Loaded` 时是零值，照写就是
// 「公开收藏夹 • 0 条视频」——而真实值可能是几百。用户会相信它，它看起来也
// 完全正常。和 UP 空间那边「粉丝数 0」是同一种错。
func TestTheCollectionHeaderDoesNotClaimZeroMedia(t *testing.T) {
	b := newTestShell(t, blockedBiliOpts(t, true)...)
	s, tt := b.s, b.tt
	s.navigate(collectionPathPrefix + "12345")
	tt.Frame()
	tt.Frame()

	for _, line := range tt.Texts() {
		if hasSuffix(line, "• 0 条视频") {
			t.Fatalf("元信息还没回来，页头就写了「%s」——那是个假数"+
				"（真实条数可能是几百），用户会相信它", line)
		}
	}
}

// hasSuffix 是 strings.HasSuffix 的本地版（这一组只 import 少数几个包）。
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestTheCollectionOwnerRowKeepsItsSlot 钉住收藏夹页头的创建者那一行在元信息
// 没回来时**留位**（摆骨架），而不是整行不画。
//
// 🔴 **不留位的后果**：数据到了那一行凭空冒出来，头图区高度跳一截，下面的
// 操作条和列表跟着往下走——用户看到的是「这一页自己动了一下」。
func TestTheCollectionOwnerRowKeepsItsSlot(t *testing.T) {
	b := newTestShell(t, blockedBiliOpts(t, true)...)
	s, tt := b.s, b.tt
	s.navigate(collectionPathPrefix + "12345")
	tt.Frame()
	tt.Frame()

	if !tt.HasText(collectionOwnerLoadingText) {
		t.Fatalf("创建者那一行没留位（找不到 %q）——数据到了它会凭空冒出来，"+
			"把下面的内容顶下去；文字有 %q", collectionOwnerLoadingText, tt.Texts())
	}
}

// TestTheShimmerFlagIsTheWindowsOwnFlag 钉住「看不见的窗口不要扫光帧」那道开关
// **两个包装的是同一个指针**。
//
// 🔴 **各装一份的后果**：波形停了、骨架屏还在每帧要帧——一个看不见的窗口
// 照样在烧电（票 #156 治的就是那个）。
func TestTheShimmerFlagIsTheWindowsOwnFlag(t *testing.T) {
	b := newTestShell(t, at(800, 600))
	if got, want := shimmer.VisibleFlag(), b.s.visibleFlag(); got != want {
		t.Fatal("`shimmer` 装的不是这一扇窗那一个「看不看得见」开关——" +
			"两个包各拿一份的话，失焦之后一边停一边还在烧电")
	}
}
