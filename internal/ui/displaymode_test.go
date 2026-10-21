// 「数据显示样式」在**每一页**上都生效——这是 issue #101 的核心验收。
//
// # 为什么每一页都要有一条
//
// 这个设置以前存得进设置文件却没有一页读它，而修的时候最容易犯的错是**只接一页**：
// 搜索页接上了，其余几页忘了，于是「改了设置只有搜索页变了」。用户看到的和
// 「这个设置根本没用」一样糟——他还得先自己排除掉一个页面才想得到是设置的问题。
//
// 所以下面这个表是**逐页列出来**的：少接一页，这一组少一条子测试，而少的那一页
// 恰恰是最容易漏的（本地音乐有自己的一档、UP 空间只有一张 Tab 是歌、稍后再看和
// 历史记录自己画行不走 `widgets/list`）。
//
// # 每条量的是什么
//
// **量画面，不量函数**。两条判据都是渲染结果：
//
//	紧凑：相邻两首歌之间的纵向距离变小了（行高变了）
//	网格：前两首歌的 y 一样（并排了）
//
// 判据都是「变了 / 没变」，不是某个具体的像素数——字体、主题、缩放一改，具体
// 数值就全变了，而「紧凑比列表矮」这件事不会。

package ui

import (
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// mountDisplay 给一个**已经建好**的外壳挂上设置文件。
//
// ⚠️ 为什么不是用基座那个 `withSettings`：那个是启动时读盘，而这一组要量的正是
// **改了设置之后**页面跟着变的那一下。挂在建好之后，就可以走 `a.UpdateSettings`
// ——设置页那十几项改设置走的是同一条路（`applySetting`），于是这里量的是真实
// 用户的改动，不是「启动参数恰好是这一档」。
func mountDisplay(t *testing.T, a *app.App) {
	t.Helper()
	// ⚠️ **照着此刻那一份写出去**，不是 `DefaultSettings`：有些测试（本地音乐）
	// 已经挂过一次设置，那一份里带着它们要的东西（本地音乐目录）。写一份默认值
	// 出去会把那些冲掉，而症状是「挂完设置页面空了」——和「挂设置」本身毫无关系。
	st := a.SettingsSnapshot()
	if err := a.MountSettings(writeTempSettings(t, st)); err != nil {
		t.Fatalf("挂不上设置：%v", err)
	}
}

// setDisplay 改设置里的「数据显示样式」并要一帧。
//
// 走 `UpdateSettings` 而不是直接改 `s.app` 上的字段：那份设置在 `SettingsStore`
// 里面，而 `a.invalidate` 是设置改完之后叫醒界面的那一手（`settings_mount.go`）。
// 少了它，页面要等下一次别的东西碰它才重画——而那种绿是运气。
func setDisplay(t *testing.T, a *app.App, tt *mygoui.Tester, m app.DisplayMode) {
	t.Helper()
	if _, err := a.UpdateSettings(func(cur *app.Settings) { cur.DisplayMode = m }); err != nil {
		t.Fatalf("改不了显示样式：%v", err)
	}
	// 画几帧：切视图不只是换一个控件，还要把滚动位置和列数量一遍，而行高是量出来
	// 的，量完才知道落在哪儿（`homepage_test.go` 的 homeFrame 用的是同一个数）。
	for range 3 {
		tt.Frame()
	}
}

// spot 是画面上量出来的一个点：某一首在哪儿。
type spot struct {
	title string
	x, y  float32
	ok    bool
}

// findSpot 找某一首歌在画面上的位置。
//
// ⚠️ **盒子是空的（宽或高 0）当没找到**：虚拟列表在视口上下各多建两行给 Tab 和读屏
// 用（`mygo/ui/list.go` 的 listOverscan），那些行建在**视口之外**，它们仍然有
// y——只判「找没找到」会把视口外那一行也量进来，于是「前两首并排」永远为真
// （`homepage_test.go` 的 listPos 顶上写的是同一件事）。
func findSpot(tt *mygoui.Tester, title string) spot {
	r, ok := tt.Find(title)
	if !ok || r.W <= 0 || r.H <= 0 {
		return spot{title: title}
	}
	return spot{title: title, x: r.X, y: r.Y, ok: true}
}

// rowPitch 是相邻两首歌之间的纵向距离（列表 / 紧凑两档）。
//
// 量不出来是 0。**调用方必须自己判 0**：一屏放不下两首时 pitch 当然是 0，而那不是
// 「紧凑没生效」。
func rowPitch(tt *mygoui.Tester, a, b string) float32 {
	sa, sb := findSpot(tt, a), findSpot(tt, b)
	if !sa.ok || !sb.ok {
		return 0
	}
	d := sb.y - sa.y
	if d < 0 {
		return -d
	}
	return d
}

// sameRowTol 是「同一个网格行」判据的容差（像素）。
//
// ⚠️ **不许用 `==`**：同一行里相邻两格的那几个字，y 量出来是 243.31055 和
// 243.31067——浮点排版算出来的，同一行差不到千分之一像素，而**差一点点就判成不在
// 同一行**，于是这条测试会随机红。
const sameRowTol = 2

// beside 报这几首里**有没有两首并排**（网格那一档：同一个网格行里左右挨着）。
//
// 判据是 **y 一样**而不是「x 不一样」：x 不一样的话，列表里两行首尾相接也是
// 「x 不一样」，而判据必须是「换了视图」这件事本身。
//
// ⚠️ **为什么是「有没有两首」而不是「固定量第 2、3 首」**。`ui.ListState` 给每一行
// 挂一个无障碍标签（`List.View` / `Grid.View` 里的 `state.Label`，值是这一行
// **第一首**的歌名），而无头 Tester 的 `Find` 认的正是这些标签——于是网格里第一
// 首歌命中的是**整行**的盒子（y 是行顶、宽是整页），另外几首命中的才是格子里的
// 那几个字（y 是行顶往下 148 像素）。**哪一首在第一格取决于这一页的排序**（本地
// 音乐那一页把歌名按路径排，结果是倒着的），所以问「第 2、3 首」在有的页面上量到
// 的一个是行、一个是格子，判据会永远为假。改成「三首里有两首同 y」就与哪一首
// 排在第一无关了。
func beside(tt *mygoui.Tester, titles [3]string) bool {
	for i := range titles {
		for j := i + 1; j < len(titles); j++ {
			sa, sb := findSpot(tt, titles[i]), findSpot(tt, titles[j])
			if !sa.ok || !sb.ok {
				continue
			}
			d := sa.y - sb.y
			if d < 0 {
				d = -d
			}
			if d < sameRowTol {
				return true
			}
		}
	}
	return false
}

// pageCase 是「一页」：怎么把它打开、打开之后画面上那几首歌叫什么。
type pageCase struct {
	name string
	// open 打开那一页，返回外壳、Tester 和画在画面上的前三首歌名。
	open func(t *testing.T) (*Shell, *mygoui.Tester, [3]string)
}

// displayPages 是**接了这个设置的每一页**。
//
// ⚠️ 这张表是本票的覆盖面清单，少一行就是那一页没接。哪些页**故意不在**里，
// 理由写在 `displayPagesMissAndWhy`。
var displayPages = []pageCase{
	{name: "首页", open: openHomeForDisplay},
	{name: "搜索结果", open: openSearchForDisplay},
	{name: "收藏夹", open: openCollectionForDisplay},
	{name: "UP 空间的投稿", open: openUpSpaceForDisplay},
	{name: "稍后再看", open: openLaterForDisplay},
	{name: "历史记录", open: openHistoryForDisplay},
	{name: "本地音乐", open: openLocalForDisplay},
}

// TestEveryPageFollowsTheDisplayStyleSetting 是本票的核心验收。
//
// 每一页切到「紧凑」行高都要变小、切到「网格」前两首都要并排。两条判据缺一不可：
// 只有紧凑的话，一个「永远画列表」的页面全绿；只有网格的话，一个「紧凑没做」的
// 页面全绿——而那个页面上用户在设置里选「紧凑」看到的还是老样子。
func TestEveryPageFollowsTheDisplayStyleSetting(t *testing.T) {
	for _, p := range displayPages {
		t.Run(p.name, func(t *testing.T) {
			s, tt, titles := p.open(t)
			a := s.app
			if got := a.SettingsSnapshot().DisplayMode; got != app.DisplayModeList {
				t.Fatalf("起点就不是列表那一档（%q）——后面量的是两档之间", got)
			}

			listPitch := rowPitch(tt, titles[0], titles[1])
			if listPitch == 0 {
				t.Fatalf("列表那一档量不出前两首的位置（%q / %q），量的是画出来的东西"+
					"不是变量", titles[0], titles[1])
			}
			if beside(tt, titles) {
				t.Fatalf("列表那一档已经有两首并排了：起点不对，后面全都量不出来")
			}

			setDisplay(t, a, tt, app.DisplayModeCompact)
			if !haveBoth(tt, titles[0], titles[1]) {
				t.Fatalf("切到紧凑之后歌不见了：%q", tt.Texts())
			}
			compactPitch := rowPitch(tt, titles[0], titles[1])
			if compactPitch == 0 {
				t.Fatalf("紧凑那一档量不出前两首的位置（%q / %q）", titles[0], titles[1])
			}
			if compactPitch >= listPitch {
				t.Errorf("紧凑没有变矮：列表 %.0f → 紧凑 %.0f——设置里的这一档在"+
					"这一页没生效", listPitch, compactPitch)
			}

			setDisplay(t, a, tt, app.DisplayModeCard)
			if !haveBoth(tt, titles[0], titles[1]) {
				t.Fatalf("切到网格之后歌不见了：%q", tt.Texts())
			}
			if !beside(tt, titles) {
				t.Errorf("切到网格之后没有两首并排——这一页没跟着设置换视图。%s",
					spotted(tt, titles))
			}
		})
	}
}

// haveBoth 报两首歌此刻是不是都画在画面上（有盒子）。
func haveBoth(tt *mygoui.Tester, a, b string) bool {
	return findSpot(tt, a).ok && findSpot(tt, b).ok
}

// spotted 是「它们都在哪儿」，只给报错用。
//
// ⚠️ 为什么不用 `tt.Texts()`：那一长串里混着侧栏、播放条、窗口标题，量出来的 y
// 全是别的东西的 y，而报错的人要看的恰恰是「那几首歌自己摆成什么样」。
func spotted(tt *mygoui.Tester, titles [3]string) string {
	var b strings.Builder
	for _, n := range titles {
		s := findSpot(tt, n)
		note := ""
		if !s.ok {
			note = "（不在画面上）"
		}
		fmt.Fprintf(&b, "\n  %s 在 (%.0f, %.0f)%s", n, s.x, s.y, note)
	}
	return b.String()
}

// TestThePagesThatDeliberatelyDoNotFollowIt 钉住「哪些页故意不跟」。
//
// 上面那张表是覆盖面清单，而**没在表上的页**同样要有一份说清楚为什么的清单：不然
// 下一个人读代码时，「这一页没接」和「这一页不该接」长得一模一样，而分不清就会去
// 「补上」——把一个不该动的控件改了（比如那个行高定死、因为「定位当前播放」按行号
// 乘 64 的抽屉）。
//
// 判据是**这些页真的画着它们自己的那种东西**（抽屉里有一行队列、动态那一页有动态、
// 关注那一页有用户卡）：页还在，才谈得上「它为什么不跟」。
func TestThePagesThatDeliberatelyDoNotFollowIt(t *testing.T) {
	for _, c := range []struct {
		what string
		// 摆到那一页，判据是画面上有一句只有那一页才有的字。
		open    func(t *testing.T) (*mygoui.Tester, *Shell)
		why     string
		witness string
	}{
		{
			what: "播放队列抽屉",
			open: func(t *testing.T) (*mygoui.Tester, *Shell) {
				s, tt := newShell(t)
				s.drawerOpen = true
				for range 3 {
					tt.Frame()
				}
				return tt, s
			},
			why:     "它不是一页，是播放时拉出来的一个抽屉；而且它的**行高定死在 64**（Biu 的 RowHeight）——「定位当前播放」是 `targetIndex*64`，改了行高那个按钮每次都停在差几像素的地方",
			witness: "队列",
		},
		{
			what: "用户动态",
			open: func(t *testing.T) (*mygoui.Tester, *Shell) {
				s, tt := newShell(t)
				s.navigate(dynamicFeedPath)
				for range 3 {
					tt.Frame()
				}
				return tt, s
			},
			why:     "那一页画的是**动态卡片**（一段文字 + 视频缩略图），不是歌的列表——网格 / 紧凑那三档说的全是「一行几首歌」",
			witness: "用户动态",
		},
		{
			what: "我的关注",
			open: func(t *testing.T) (*mygoui.Tester, *Shell) {
				s, tt := newShell(t)
				s.navigate(followPath)
				for range 3 {
					tt.Frame()
				}
				return tt, s
			},
			why:     "那一页画的是**用户卡**（头像 + 昵称 + 关注按钮），上游从一开始就只有网格一种摆法（`follow-list/index.tsx` 把它整个转给 `virtual-grid-page-list`）",
			witness: "我的关注",
		},
		{
			what: "下载记录",
			open: func(t *testing.T) (*mygoui.Tester, *Shell) {
				s, tt := newShell(t)
				s.navigate("/download-list")
				for range 3 {
					tt.Frame()
				}
				return tt, s
			},
			why:     "它是**下载任务表**（三段进度 + 每条五种操作），没有封面也没有「正在放」——它不是内容页",
			witness: "下载记录",
		},
		{
			what: "首页的新歌速递",
			open: func(t *testing.T) (*mygoui.Tester, *Shell) {
				s, tt := newShell(t)
				s.home.SetSongs(homeSongs{new: homeSongsOf("新歌", 12)})
				s.home.tab = homePopTab
				for range 3 {
					tt.Frame()
				}
				return tt, s
			},
			why:     "它是首页顶部的一块**横向推荐位**，行数是按窗口高算出来的（`newMusicRowsAt`），翻页是「上一批 / 下一批」两个按钮——它有自己的几何，跟「一屏能看到更多歌」没关系",
			witness: "新歌",
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			tt, s := c.open(t)
			_ = s
			if !tt.HasText(c.witness) {
				t.Errorf("这一页没画出来（找 %q），那「它为什么不跟这个设置」就成了"+
					"一句没人核对过的话。文字有 %q", c.witness, tt.Texts())
			}
			if c.why == "" {
				t.Error("没写为什么——那一条就只剩一个名字")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 每一页怎么打开
// ─────────────────────────────────────────────────────────────────────────────

// 三首歌名。三条都要**短**：歌名一折行行高就变了，而「紧凑比列表矮」这条判据
// 量的是行高——歌名折行的页面会把两档之间的差别吃掉一部分。
var displayTitles = [3]string{"甲曲", "乙曲", "丙曲"}

// displayWindow 是这一组量布局用的窗口尺寸。
//
// ⚠️ **要够宽**：网格那一档量的是「第 2 首和第 3 首并排」，而它们在同一个网格行
// 里要求列数 ≥ 3（Biu 那四档是 1536→6 / 1280→5 / 1024→4 / 768→3）。1200 的默认
// 窗口扣掉侧栏和页内边距只剩 ~980 → 3 列，勉强够；而每一页的内容区宽度还各不相同
// （搜索页有排序和搜索框、UP 空间有头图），留一条余量。
//
// 窗口不够宽时这一组会红，而那不是产品的错——所以宁可一开始就摆一个够宽的。
func wideEnough(tt *mygoui.Tester) {
	tt.SetSize(1600, 900)
	// SetSize 自己只跑一遍 settle，而列数是量**容器**的宽算出来的（`Bounds` 量的是
	// 上一帧的盒子），所以还要再画两帧。
	for range 3 {
		tt.Frame()
	}
}

// openHomeForDisplay 打开首页（三个 Tab 的第一档）。
func openHomeForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	song := func(i int) list.Music {
		return list.Music{ID: displayTitles[i], Title: displayTitles[i], Duration: 100}
	}
	s, tt := newHome(t, homeSongs{
		tabs: [homeTabCount][]list.Music{
			{song(0), song(1), song(2)},
			homeSongsOf(homeTabs[1], 3), homeSongsOf(homeTabs[2], 3),
		},
	})
	mountDisplay(t, s.app)
	wideEnough(tt)
	return s, tt, displayTitles
}

// openSearchForDisplay 打开搜索结果（歌曲那一档）。
func openSearchForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	s, tt := newShell(t)
	st := newSearchStub(20, 3)
	st.install(s)
	mountDisplay(t, s.app)
	wideEnough(tt)
	s.navigate(searchPath + "?" + searchQuery + "=" + displayTitles[0])
	waitFrames(t, tt, "搜索结果落地", func() bool {
		return pageOf(s, searchTabSong).Loaded >= 3
	})
	for range 3 {
		tt.Frame()
	}
	// 桩的行标题是它自己给的（「第 1 页 第 1 首」那种），量就用那三个。
	return s, tt, [3]string{stubTitle(1, 0), stubTitle(1, 1), stubTitle(1, 2)}
}

// openCollectionForDisplay 打开一个收藏夹。
func openCollectionForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	medias := make([]map[string]any, 0, len(displayTitles))
	for i, title := range displayTitles {
		medias = append(medias, mediaOf(900+i, title))
	}
	_, s, tt := collectionShell(t, "77", medias...)
	mountDisplay(t, s.app)
	wideEnough(tt)
	waitSongs(t, tt, s, len(displayTitles))
	for range 3 {
		tt.Frame()
	}
	return s, tt, displayTitles
}

// openUpSpaceForDisplay 打开一个 UP 空间的「投稿」那一档。
func openUpSpaceForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "投稿页的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "offset": "", "has_more": false})
	})
	vlist := make([]any, 0, len(displayTitles))
	for i, title := range displayTitles {
		vlist = append(vlist, map[string]any{
			"bvid": fmt.Sprintf("BVspace%d", i), "title": title, "length": "3:20", "cid": "9",
		})
	}
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": vlist}, "page": map[string]any{"count": len(vlist)},
		})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{}})
	})
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{"seasons_list": []any{}}})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	mountDisplay(t, s.app)
	sp := openSpace(t, s, tt, 42)
	sp.tab = upTabVideo
	waitFrames(t, tt, "投稿落地", func() bool { return sp.videos.Paged().Loaded >= len(displayTitles) })
	for range 3 {
		tt.Frame()
	}
	return s, tt, displayTitles
}

// openLaterForDisplay 打开稍后再看。
func openLaterForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	f := newLibFake(t).withLater(1, 3, displayTitles[:]...)
	tt, s := libraryShell(t, f, laterPath)
	mountDisplay(t, s.app)
	wideEnough(tt)
	waitFrames(t, tt, "稍后再看落地", func() bool { return rowsShown(&s.laterPage) == len(displayTitles) })
	for range 3 {
		tt.Frame()
	}
	return s, tt, displayTitles
}

// openHistoryForDisplay 打开历史记录。
func openHistoryForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	f := newLibFake(t).withHistory(1, 30, 185, displayTitles[:]...)
	tt, s := libraryShell(t, f, historyPath)
	mountDisplay(t, s.app)
	wideEnough(tt)
	waitFrames(t, tt, "历史记录落地", func() bool { return rowsShown(&s.historyPage) == len(displayTitles) })
	for range 3 {
		tt.Frame()
	}
	return s, tt, displayTitles
}

// openLocalForDisplay 打开本地音乐，并切到「歌曲」那一档。
//
// ⚠️ **必须先切到「歌曲」**：这一页自己的开关第一档是「表格」，而表格里画的是文件
// （大小 / 格式 / 创建时间），全局设置对它没有话说——分工写在 `localmusic.go` 的
// 文件头上。只接了「歌曲」那一档才是这一页响应设置的地方。
func openLocalForDisplay(t *testing.T) (*Shell, *mygoui.Tester, [3]string) {
	t.Helper()
	root := t.TempDir()
	for _, title := range displayTitles {
		libSong(t, root, title+".mp3")
	}
	tt, s, _, _ := shellOnLocalMusic(t, root)
	if err := tt.Click(localStyles[localStyleSong]); err != nil {
		t.Fatalf("切不到「%s」那一档：%v，文字有 %q", localStyles[localStyleSong], err, tt.Texts())
	}
	tt.Frame()
	mountDisplay(t, s.app)
	wideEnough(tt)
	return s, tt, displayTitles
}

// ─────────────────────────────────────────────────────────────────────────────
// 设置页那一格
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDisplayStyleSettingShowsAPreviewOfEachStyle 钉住「设置项旁边能预览效果」。
//
// 三张缩略图都要**真的画出来**（有盒子，而且不是一片空白）。三张一模一样的话，预览
// 回答不了「我选了哪个」，而这一条正是那一段验收要的东西。
//
// ⚠️ 判据是「盒子里头有内容」而不是「盒子的颜色」：缩略图里那几块封面 / 字条画的是
// 主题的 `SurfaceHover`，主题一换它们就换颜色，而「三张各画各的形状」这件事不换。
func TestTheDisplayStyleSettingShowsAPreviewOfEachStyle(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	for v := list.View(0); v < list.ViewCount; v++ {
		r, ok := tt.Find("预览：" + v.String())
		if !ok || r.W <= 0 || r.H <= 0 {
			t.Fatalf("「预览：%s」没画出来（%v），三张预览里少了一张", v.String(), r)
		}
		if ink := inkIn(tt, r); ink == 0 {
			t.Errorf("「预览：%s」那一格是空白的——它没告诉用户这一档长什么样", v.String())
		}
	}
}

// TestThePreviewHighlightsTheChosenStyle 钉住预览上「哪一张是现在这一档」。
//
// ⚠️ **不数「主色像素有几个」**：边框那一圈是圆角 + 抗锯齿画出来的，量出来的是
// {87 225 136} 这种混了白边的颜色，一个像素都对不上主色（实测三档都是 0 个）——
// 那是判据不对，不是没画。改成「这一格的边框里离主色最近的像素，比离普通边框色
// 更近」：抗锯齿只影响「有几个」，不影响「哪边更近」。
func TestThePreviewHighlightsTheChosenStyle(t *testing.T) {
	for _, mode := range []app.DisplayMode{
		app.DisplayModeList, app.DisplayModeCard, app.DisplayModeCompact,
	} {
		t.Run(string(mode), func(t *testing.T) {
			tt, _, a, _ := shellOnSettings(t, func() app.Settings {
				st := app.DefaultSettings("")
				st.DisplayMode = mode
				return st
			}())
			th := BuildTheme(a.SettingsSnapshot(), false)
			chosen := list.ViewOf(mode)

			for v := list.View(0); v < list.ViewCount; v++ {
				r, ok := tt.Find("预览：" + v.String())
				if !ok || r.W <= 0 {
					t.Fatalf("「预览：%s」没画出来", v.String())
				}
				got := borderLeansToAccent(tt, r, th.Accent, th.Border)
				if v == chosen && !got {
					t.Errorf("选中的是「%s」，而那一张的描边不是主色——预览指着错的",
						chosen.String())
				}
				if v != chosen && got {
					t.Errorf("没选中的「%s」描边也是主色——三张分不出哪一档是现在这一档",
						v.String())
				}
			}
		})
	}
}

// previewBorderRows 是量描边时往下看几行。
//
// ⚠️ **只量最上面这一小条**：缩略图的内容（封面 / 字条，`SurfaceHover`）从往下 6
// 像素才开始（`Padding(6)`），而描边只有 1 像素、再加圆角那两行抗锯齿。量整个盒子
// 的话，内容里有一块恰好接近主色就会误判。
const previewBorderRows = 4

// inkIn 是这一格里画了东西的像素数（和背景不一样的那种）。
func inkIn(tt *mygoui.Tester, r mygoui.Rect) int {
	img := tt.Image()
	bg := img.RGBAAt(int(r.X+r.W/2), int(r.Y+r.H)-2) // 底下那一行是空白
	n := 0
	for y := int(r.Y) + 2; y < int(r.Y+r.H)-2; y++ {
		for x := int(r.X) + 2; x < int(r.X+r.W)-2; x++ {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

// borderLeansToAccent 报这一格的描边更像主色还是更像普通边框色。
//
// 判据是「盒子里离主色最近的那个像素」和「离边框色最近的那个像素」谁更近。抗锯齿
// 改的是像素本身，改不了哪个更近——所以这一条不依赖「边框是纯粹的某一个颜色」。
func borderLeansToAccent(tt *mygoui.Tester, r mygoui.Rect, accent, border mygoui.Color) bool {
	img := tt.Image()
	bestAccent, bestBorder := 1<<30, 1<<30
	for dy := range previewBorderRows {
		for x := int(r.X) + 1; x < int(r.X+r.W)-1; x++ {
			c := img.RGBAAt(x, int(r.Y)+dy)
			if d := distRGBA(c, accent); d < bestAccent {
				bestAccent = d
			}
			if d := distRGBA(c, border); d < bestBorder {
				bestBorder = d
			}
		}
	}
	return bestAccent < bestBorder
}

// distRGBA 是两个颜色差多远（各通道差的绝对值加起来）。
//
// 用和而不是欧氏距离：只有「谁更近」这一个问法，而三通道相加更便宜也更好读。
func distRGBA(c color.RGBA, want mygoui.Color) int {
	return abs(int(c.R)-int(want.R)) + abs(int(c.G)-int(want.G)) +
		abs(int(c.B)-int(want.B))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheGridStillPagesOnARealPage 钉住「切到网格之后翻页链路整条是通的」。
//
// 这一条量的是**整页那一层**：切档 → 收页 → 画出来 → 滚到底 → 续拉。网格是另一个
// 控件，而 `PagedList` 的行数、可见行、三重保障全都要按摆法换算法——任何一处没换，
// 症状就是「切到网格之后停在第一页」。
//
// ## ⚠️ 这一条**不是**靠末尾哨兵成立的（这句话我写错过一次）
//
// 翻页的触发判据是 `PagedList.afterFrame` 里那句「末尾那一行进没进画面」（`last < n-1`
// 就什么都不做），而网格里**最后一行本身就是歌的行**，滚到底照样命中——把网格那半边
// 的哨兵拿掉（`SwitchView.SetFooter` 只装列表那半边），**这一条照样绿**。变异的证据：
// 拿掉哨兵后 `internal/ui` 整包 ok，只有 `widgets/list` 里那两条红。
//
// 哨兵真正负责的是「还有就拉时的那一句」和**失败之后的「点此重试」按钮**（`blocked`
// 闩住的是自动重试，解开它的只有那个按钮）——钉住的是
// `TestTheGridOffersTheRetryThatTheListOffers` 和同一层的
// `TestTheGridStillOffersARetryOnARealPage`。**把「翻得动」和「失败之后还有出路」
// 合成一句话，就会又写错一次。**
//
// 判据是**真的加载到第二页**，不是「哨兵画在屏幕上」。
func TestTheGridStillPagesOnARealPage(t *testing.T) {
	s, tt := newShell(t)
	// ⚠️ **一页要够高**（40 首 ≈ 2560 像素 > 900 的窗口）：一屏摆得下时三重保障的
	// 「容器没填满」会自己把第二页拉上来，于是「滚到底之后才续拉」这一条一次都走
	// 不到——那条测试就变成在验另一件事了（`library_paging_test.go` 顶上写的是
	// 同一个坑）。
	st := newSearchStub(40, 80)
	st.install(s)
	mountDisplay(t, s.app)
	wideEnough(tt)
	s.navigate(searchPath + "?" + searchQuery + "=" + displayTitles[0])

	waitFrames(t, tt, "第一页落地", func() bool {
		return pageOf(s, searchTabSong).Loaded == 40 && !pageOf(s, searchTabSong).Loading
	})

	setDisplay(t, s.app, tt, app.DisplayModeCard)
	if !beside(tt, [3]string{stubTitle(1, 1), stubTitle(1, 2), stubTitle(1, 3)}) {
		t.Fatalf("切到网格之后没有并排，起点就不对：%s",
			spotted(tt, [3]string{stubTitle(1, 1), stubTitle(1, 2), stubTitle(1, 3)}))
	}

	scrollSearchList(tt)
	waitFrames(t, tt, "网格那一档滚到底之后自动续拉", func() bool {
		return pageOf(s, searchTabSong).Loaded == 80
	})
}

// TestEveryViewSurvivesTheNarrowestWindow 钉住「三档在最窄窗口下都画得出来」。
//
// ⚠️ **为什么单独一条**：`displayPages` 那一组量的是宽窗口（宽到网格有五列），而
// 窗口能拖到 900（`WindowOptions.MinWidth`）——那时候网格只剩两列，封面按格子宽
// 画（`list.CellWidth`），而格子里的歌名 / UP 名 / 时长要挤进剩下那点宽度。
// 「挤坏了」和「画崩了」在这里是两种病，而它们在同一个开关后面。
//
// 判据有两条：**头两首还在画面上** + **画面上没有「画不出来」**。
//
// ⚠️ **只要求头两首，不是三首**：`ui.List` 是虚拟的，只建看得见的那几行，而网格在
// 最窄窗口是**两列**——三首歌占两行，第二行在 560 高的窗口外面。三首里少一首是
// 对的，不是坏了。
//
// ⚠️ **网格那一档只要求头一首**：歌名在卡片的最下面（封面 140 在上面），而主 Tab
// 对齐上游 40 高之后（票 #171），最窄窗口里第二张卡的歌名那一行正好落到可视区
// 底下——卡片和封面都还在，裁掉的只是那一行字。这不是「画崩了」，两列这件事由
// 宽窗口的 `TestEveryPageFollowsTheDisplayStyleSetting` 钉着；这里管的是「挤到
// 最窄时每一档都还画得出来」。
//
// ⚠️ 「画不出来」那一句是**兜住 panic 的占位**（`widgets/list` 的 `guardFailed`）：
// 一格画崩了就是它。不崩就没有那一句——所以它是「没崩」的直接判据，比「有几首可见」
// 灵敏得多。
func TestEveryViewSurvivesTheNarrowestWindow(t *testing.T) {
	for _, p := range displayPages {
		t.Run(p.name, func(t *testing.T) {
			s, tt, titles := p.open(t)
			tt.SetSize(minWindowW, minWindowH)
			for range 3 {
				tt.Frame()
			}
			for _, m := range []app.DisplayMode{
				app.DisplayModeCompact, app.DisplayModeCard, app.DisplayModeList,
			} {
				setDisplay(t, s.app, tt, m)
				if tt.HasText("画不出来") {
					t.Errorf("%s 那一档下画面上有「画不出来」：%q", m, tt.Texts())
				}
				want := titles[:2]
				if m == app.DisplayModeCard {
					want = titles[:1] // 理由见函数头上那一段
				}
				for _, n := range want {
					if r, ok := tt.Find(n); !ok || r.W <= 0 || r.H <= 0 {
						t.Errorf("%s 那一档下 %q 不在画面上（%v）", m, n, r)
						break
					}
				}
			}
		})
	}
}

// minWindowW / minWindowH 是窗口能拖到的最小尺寸，**照抄** `WindowOptions` 里那
// 两个（`shell.go`：`MinWidth: 900` / `MinHeight: 560`）。
//
// ⚠️ **别改小、也别改大**：这一组量的是「挤到最窄时有没有崩、有没有整行消失」。
// 改小是在量一个用户到不了的尺寸，改大就永远量不到最窄那一档。
const (
	minWindowW = 900
	minWindowH = 560
)

// TestTheBatchOpsStillWorkInEveryView 钉住「多选在每一档都点得到」。
//
// 这是「数据显示样式」新打开的一条路：切到网格之后画的是**另一半控件**，而多选
// 接线装在列表那半边——格子上一格勾选框都没有，于是**逐首勾完全做不到**，只剩全选
// / 不选。
//
// ⚠️ **那个症状特别难自己发现**：全选 / 不选走的是 `Len()` / `At()`，不经过画法，
// 所以它在网格那一档照样能用；批量操作条上「已选 0 首」会跟着变，用户点几下
// 「全选 → 批量删除」一切正常。而「一格一格地勾」这件事在网格里没有入口——
// 也就是九成用法没了。
//
// 三档各点一次：每一档都要有 6 个勾选框、逐首点得动、点完「已选 N 首」跟着变。
func TestTheBatchOpsStillWorkInEveryView(t *testing.T) {
	medias := make([]map[string]any, 0, len(displayTitles))
	for i, title := range displayTitles {
		medias = append(medias, mediaOf(900+i, title))
	}

	for _, m := range []app.DisplayMode{
		app.DisplayModeList, app.DisplayModeCard, app.DisplayModeCompact,
	} {
		t.Run(string(m), func(t *testing.T) {
			_, s, tt := collectionShell(t, "77", medias...)
			mountDisplay(t, s.app)
			wideEnough(tt)
			waitSongs(t, tt, s, len(displayTitles))
			setDisplay(t, s.app, tt, m)

			if err := tt.Click("多选"); err != nil {
				t.Fatalf("点不到「多选」：%v，文字有 %q", err, tt.Texts())
			}
			for range 3 {
				tt.Frame()
			}

			// 每一首都要有勾选框。名字是「选择 <歌名>」（`viewCheckbox` /
			//			// `viewCellCheckbox` 都用同一个），无头 Tester 的 `Click` 按它找。
			for i, title := range displayTitles {
				box := "选择 " + title
				if _, ok := tt.Find(box); !ok {
					t.Fatalf("%s 那一档下 %q 那个勾选框没画出来（共 %d 首）",
						m, box, len(displayTitles))
				}
				// 只点第一首和第三首：点完要看到「已选 2 首」。
				if i != 0 && i != 2 {
					continue
				}
				if err := tt.Click(box); err != nil {
					t.Fatalf("%s 那一档下点不动 %q：%v，文字有 %q", m, box, err, tt.Texts())
				}
				for range 3 {
					tt.Frame()
				}
			}
			if !tt.HasText("已选 2 首") {
				t.Errorf("%s 那一档下逐首勾完显示的是 %q，want「已选 2 首」",
					m, tt.Texts())
			}
		})
	}
}

// TestTheGridStillOffersARetryOnARealPage 是 `TestTheGridStillPagesOnARealPage` 的
// 另一半，也是它真正该在的那个地方。
//
// ## 为什么要有这一条
//
// 把网格那半边的哨兵拿掉之后，`internal/ui` 整包是绿的——上一条的翻页照样翻得了，
// 而 `widgets/list` 里钉「重试」的那两条在另一个包里。**整页这一层当时没有任何一条
// 会红**，于是「网格那一档翻页失败之后卡死」这件事在页面这一层是没有证据的。
//
// 这一条把它补上：第二页装成一次业务错误（B 站的错都在 HTTP 200 里面，`refused`），
// 切到网格，滚到底，然后要那个「点此重试」**出现在画面上**。拿掉哨兵之后这一条立刻
// 红——症状是页面上什么都不剩，而 `blocked` 闩住的是自动重试，用户能做的只有切路由
// 或者把视图切回列表。
func TestTheGridStillOffersARetryOnARealPage(t *testing.T) {
	f := newLibFake(t).withLater(1, 60, manyTitles(1, 40)...)
	f.withLater(2, 60, manyTitles(2, 20)...)
	f.mu.Lock()
	f.later[2] = refused
	f.mu.Unlock()

	tt, s := libraryShell(t, f, laterPath)
	mountDisplay(t, s.app)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 40 })

	setDisplay(t, s.app, tt, app.DisplayModeCard)
	scrollLibraryToEnd(t, tt, &s.laterPage)
	waitFrames(t, tt, "页面停在那一个重试按钮上", func() bool {
		return tt.HasText("加载失败，点此重试")
	})
	if s.laterPage.rows.Status().Loading {
		t.Error("失败之后还在转圈")
	}
}
