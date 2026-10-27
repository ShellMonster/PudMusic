package ui

// 「每一页至少要有一条真数据」。
//
// BMG-47 那份对照审计查出的交付模式是：**页面壳子做完了、数据源没排票**，于是
// 交付时一片绿，用户打开是空的。它逐页查了一遍，七个页面命中。
//
// 首页是最典型的那一个：它有 `TestTheHomePageStillSaysWhyItIsEmpty`，而那条验的是
// **空状态文案对不对**——空状态正是当时的真实状态，所以它**必然通过**。票（#13）
// 面上的四条验收全是交互和布局，没有一条要求有数据，于是首页永久为空，而证据台账
// 里也没有一条钉「每一页至少有一条真数据」。
//
// 这一份就是那张票早该有的钉子。它的规矩只有一条：
//
//	**每一页都要在表上有一行，而那一行必须说清它是「有数据」还是「欠着」。**
//
// 「欠着」不许白欠，三样都要齐：
//
//  1. 点名到工单号（或者审计编号）——「还没排票」本身就是一个要写下来的事实；
//  2. 写清差的是什么（缺哪个接口、缺哪一页）；
//  3. **当场验一次「它现在真的是空的」**——不然这张表就退化成一个万能借口：
//     一页数据早就接上了，欠条还赖在上面，谁也看不出来。
//
// 反过来，有数据的那几页除了「真数据出现在画面上」，还钉一句**这一页的空状态
// 文案不许出现**。那正是 #13 的教训：空状态文案对不对，和这一页有没有数据，是
// 两件事，而验收只验了前一件。

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
	mygoui "github.com/egoist/mygo/ui"
)

// realDataPage 是「这一页至少要有一条真数据」那张表的一行。
type realDataPage struct {
	// Route 是路由（`/`、`/later`…），兜底那一条写 `*`。
	Route string
	// Title 是这一页的名字，只用来把报错说成人话。
	Title string

	// Open 建一个停在这一页、并且**已经喂了真数据**的外壳。
	//
	// 「喂了数据」是这一份的核心：每一页的假 B 站都给了至少一条**内容**，于是
	// 「画面上有没有它」才是「这一页有没有真数据」的答案。没有 Open 而 Debt
	// 也空着的话，那一行就是白写的。
	Open func(t *testing.T) (*Shell, *mygoui.Tester)
	// Want 是画面上必定出现的真数据（fixture 给的那一句）。
	Want []string

	// Debt 是还欠着的：点名到工单号或者审计编号，外加差的是什么。
	// 空串 = 这一页真有数据。
	Debt string
	// Why 是 Debt 非空时补的一句：差的是什么、为什么现在交不出来。
	Why string
	// ByDesign 是「这一页按设计就该是空的」（只有空状态落地页 `/empty` 算数）。
	// 它和 Debt 分开是因为两者的性质不同：欠着的是欠债，按设计的是这个页面的
	// **名字**。
	ByDesign string

	// NowEmpty 是 Debt / ByDesign 非空时要断言的那一句空状态。**必填**：
	// 少了它，「欠着」就永远不用证明自己真的还欠着。
	NowEmpty string
}

// realDataLedger 是那张表本体。加一条路由就要在这里加一行。
var realDataLedger = []realDataPage{
	{
		Route: homePath, Title: "推荐音乐",
		// 🔴 全表**只有这一行**必须走真接口，理由就是审计 A1 / #13 的原教训：
		// 「画得出来」和「真去 B 站取回来了」是两件事，而当年只钉了前一件
		//（`homePage.SetSongs` 一路绿，`SetSongs` 的调用者一个也没有）。所以这一
		// 行不塞歌，而是拿一台假 B 站 + `homeArchives` 的真接口跑一遍，并且把三档
		// 各走一遍——每一档都必须**从 B 站拿到它自己那一批**，而不是换了档还是那一批。
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			b := newUIExplore(t)
			homeArchives(b)
			s, tt, _ := newHomeShell(t, b)

			// 先停在第一档：那是用户落地看到的那一屏，也是 Want 断言的那一屏。
			realDataWait(t, tt, "第一档的推荐拉回来了", func() bool { return tt.HasText("音乐 推的第一首") })

			clickTab(t, tt, 1)
			realDataWait(t, tt, "鬼畜那一档拉回来了", func() bool { return tt.HasText("鬼畜 推的第一首") })
			clickTab(t, tt, 2)
			realDataWait(t, tt, "流行那一档（榜 + 新歌速递）拉回来了", func() bool {
				return tt.HasText("流行 榜上的歌") && tt.HasText("新歌速递里那首")
			})
			// 「流行」走的是音乐榜而不是第三个分区——那一档拿别的档的歌来顶，
			// 界面上看着有内容，而这一条断言的是「它打的是哪个接口」。
			if !b.hit(uiHomeMusicRank) {
				t.Fatal("「流行」那一档没有打音乐榜那个接口（它拿的是别的那一批）")
			}
			clickTab(t, tt, 0)
			realDataWait(t, tt, "切回第一档", func() bool { return tt.HasText("音乐 推的第一首") })
			return s, tt
		},
		Want: []string{"音乐 推的第一首"},
	},
	{
		Route: laterPath, Title: "稍后再看",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			f := newLibFake(t).withLater(1, 1, "真数据一首")
			tt, s := libraryShell(t, f, laterPath)
			waitFrames(t, tt, "那一首拉回来了", func() bool { return rowsShown(&s.laterPage) == 1 })
			return s, tt
		},
		Want: []string{"真数据一首"},
	},
	{
		Route: historyPath, Title: "历史记录",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			f := newLibFake(t).withHistory(1, 30, 200, "真数据一首")
			tt, s := libraryShell(t, f, historyPath)
			waitFrames(t, tt, "那一首拉回来了", func() bool { return rowsShown(&s.historyPage) == 1 })
			return s, tt
		},
		Want: []string{"真数据一首"},
	},
	{
		Route: localMusicPath, Title: "本地音乐",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			root := t.TempDir()
			libSong(t, root, "真数据一首.mp3")
			tt, s, a, _ := shellOnLocalMusic(t, root)
			settleLocal(t, a, tt)
			realDataWait(t, tt, "本地音乐扫出了那一首", func() bool {
				return len(s.local.rows) == 1
			})
			return s, tt
		},
		Want: []string{"真数据一首"},
	},
	{
		Route: downloadListPath, Title: "下载记录",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newRows(t, download.Snapshot{
				ID: "dl-1", Title: "真数据一首", Label: "音频",
				Dest: "/tmp/真数据一首.m4a", Status: download.StatusDone,
				Done: 1024, Total: 1024, Percent: 100,
			})
			return s, tt
		},
		Want: []string{"真数据一首"},
	},
	{
		Route: settingsPath, Title: "设置",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newShell(t)
			s.navigate(settingsPath)
			realDataFrames(tt, 3)
			return s, tt
		},
		// 这一页的内容不是 B 站给的，是它自己那一页设置项。判据是「有一整排真实
		// 的设置项在画面上」——不是一句空状态。
		Want: []string{"数据显示样式", "开机自启"},
	},
	{
		Route: searchPath, Title: "搜索",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newSearchPage(t, "真数据", newSearchStub(24, 24))
			waitFrames(t, tt, "结果拉回来了", loadedTo(s, 0, 1))
			realDataFrames(tt, 2)
			return s, tt
		},
		Want: []string{stubTitle(1, 0)},
	},
	{
		Route: dynamicFeedPath, Title: "动态",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			b := newUIExplore(t)
			uiSpaceData(b, "我自己", 0, 1)
			b.on("/x/relation/followings", func(w http.ResponseWriter, _ *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"total": 1, "list": []any{
					map[string]any{"mid": 1, "uname": "真数据作者"},
				}})
			})
			b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, _ *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"items": []any{uiDynamic("d1", "真数据一条动态")}, "offset": "", "has_more": false})
			})
			s, tt, _ := newSpaceShell(t, b, true, "77")
			s.navigate(dynamicFeedPath)
			realDataWait(t, tt, "动态拉回来了", func() bool {
				return len(s.dynamicFeedOf().items) == 1
			})
			return s, tt
		},
		Want: []string{"真数据一条动态"},
	},
	{
		Route: upPath, Title: "UP 主空间",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			b := newUIExplore(t)
			uiSpaceData(b, "真数据 UP 主", 2, 1234)
			// 四张 Tab 各自接接口，所以「这一页有数据」得**每张 Tab** 都喂：
			// 只喂头图区的话，「动态」那一档仍然是空的，界面上照样挂着「暂无动态」。
			b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, _ *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"items": []any{uiDynamic("d1", "真数据一条动态")}, "offset": "", "has_more": false})
			})
			b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, _ *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"list": map[string]any{"vlist": []any{}}})
			})
			s, tt, _ := newSpaceShell(t, b, true, "77")
			s.navigate(upPathOf(42))
			realDataWait(t, tt, "空间信息拉回来了", func() bool { return tt.HasText("真数据 UP 主") })
			realDataWait(t, tt, "动态那一档拉回来了", func() bool { return tt.HasText("真数据一条动态") })
			return s, tt
		},
		Want: []string{"真数据 UP 主", "真数据一条动态"},
	},
	{
		Route: collectionMatch, Title: "收藏夹 / 合集 / 系列",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			_, s, tt := collectionShell(t, "12345", mediaOf(1, "真数据一首"))
			waitSongs(t, tt, s, 1)
			realDataFrames(tt, 2)
			return s, tt
		},
		Want: []string{"真数据一首"},
	},
	{
		Route: miniPlayerPath, Title: "迷你播放器",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newShell(t)
			// 这一页的内容不是 B 站给的，是**正在放的那一首**（歌词那一块另有
			// 缺口，票 #20/#21，那一页不许靠歌词撑）。
			openATrack(t, s)
			s.navigate(miniPlayerPath)
			realDataWait(t, tt, "在放的那一首画出来了", func() bool { return tt.HasText("stereo16") })
			return s, tt
		},
		Want: []string{"stereo16"},
	},
	{
		Route: loginPath, Title: "扫码登录",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newShell(t)
			s.navigate(loginPath)
			realDataFrames(tt, 3)
			return s, tt
		},
		Want: []string{"扫码登录"},
	},
	{
		Route: commentsPath, Title: "评论",
		// 🔴 **这一页走的是真接口的假 B 站**：评论是 B 站给的，形状照着真
		// 接口录下来的 fixture（`internal/service/bili/testdata/comment/`）——
		// 而**页头那句话**是这一页最要紧的东西（用户得知道自己在读谁的评论），
		// 所以它也在 Want 里面。
		//
		// ⚠️ **「这一条评论」在画面上**：正文是**自己排版自己画**的（表情要成图，
		// 框架的文本控件只给 inline object 留位置、不画它），而**给辅助技术和
		// 无头 Tester 的那一份**是 `.Label(...)`——所以它找得到（理由见
		// `comments.go` 的文件头）。
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			b := newCommentBili(t, "reply-main.json")
			s, tt := newCommentsShell(t, b, mvItem(), 1200, 900)
			realDataWait(t, tt, "评论画出来了", func() bool {
				return tt.HasText("大圆镜科普")
			})
			return s, tt
		},
		Want: []string{"大圆镜科普", "若将地球的四十六亿年浓缩为一天", "这个视频的评论"},
	},
	{
		Route: followPath, Title: "我的关注",
		// ✅ **BMG-47 §3 A2 的欠条已经还清**（BMG-49）。这一页现在有自己的画法
		//（`follow.go`：分组 Tab + 虚拟网格 + 页内搜索）和自己的四个接口
		//（`internal/service/bili/follow.go`），走的是一台**真接口的假 B 站**
		// 而不是往模型里塞数据——和首页那一行同一个道理（A1 的原教训：画得出来
		// 那一半被验了、取数那一半没人管）。
		//
		// ⚠️ **欠条必须撤掉，而且撤掉它本身就是被这两条钉着的**：
		//   - `TestEveryRouteShowsARealRowOrSaysWhichTicketIsMissing` 的「欠着」
		//     那一支会要求 `Open == nil`，而数据接上之后 `Open` 非空 → 它自己红；
		//   - `TestEveryRouteHasAPageViewOrAnOpenDebt` 在 `builtPage != nil` 时会
		//     `continue`，于是第二道钉子**不响**——所以第三道（这张表自己的规矩
		//     「已经能打开却有欠条：数据接上了就把欠条撤掉」）是唯一还醒着的那道。
		//     留着一条过期欠条的后果是：这一行继续走 `realDataShellOnEmpty`，
		//     而那个空壳画的是「还没接上 B 站接口」那一支，它**顺带**含有
		//     `NowEmpty` 那一句，于是欠条「当场证明自己还欠着」也照样通过。
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			b := newUIExplore(t)
			b.on("/x/relation/tags", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, []any{
					map[string]any{"tagid": -10, "name": "特别关注", "count": 0, "tip": ""},
					map[string]any{"tagid": 0, "name": "默认分组", "count": 1, "tip": ""},
				})
			})
			b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"total": 1,
					"list": []any{map[string]any{
						"mid": 4242, "uname": "关注页那位 UP 主",
						"sign": "这一位是接口给的", "face": "https://f/a.jpg",
						"official_verify": map[string]any{"type": 0, "desc": "接口给的认证"},
					}},
				})
			})
			s, tt, _ := newSpaceShell(t, b, true, "77")
			s.navigate(followPath)
			realDataWait(t, tt, "关注的 UP 主画出来了",
				func() bool { return tt.HasText("关注页那位 UP 主") })
			return s, tt
		},
		// ⚠️ 两条都要：一条是**接口给的真内容**，一条是「全部关注(N)」那个 N——
		// 后者是 B 站的**总数**画上来的，少了它的话一个「每一格都画了但列表是空的」
		// 实现照样能过第一条。
		Want: []string{"关注页那位 UP 主", "接口给的认证", "全部关注(1)", "特别关注"},
	},
	{
		Route: "/empty", Title: "空状态落地页",
		// 这一页的**名字**就是「空状态长什么样」（`empty.go` 是拿它做样板的）。
		// 它永远空才是对的——所以它不算「壳子没接数据」，而是这一页按设计就该这样。
		ByDesign: "这一页就是「空状态落地页」：它是样板，给别的页面看空状态长什么样",
		NowEmpty: lookup("/empty").Empty.Title,
	},
	{
		Route: "*", Title: "页面不存在",
		Open: func(t *testing.T) (*Shell, *mygoui.Tester) {
			s, tt := newShell(t)
			s.navigate("/没有这一条")
			settleText(t, tt, lookup("/没有这一条").Title)
			return s, tt
		},
		// 404 自己那一页的内容：说清是哪条路径不存在、几秒后回首页。它不是空状态。
		Want: []string{lookup("/没有这一条").Title, "/没有这一条"},
	},
}

// TestEveryRouteShowsARealRowOrSaysWhichTicketIsMissing 是那张表的主线。
//
// 三种结局，每一种都必须说清自己是谁：
//
//	有数据 → 真数据在画面上，且**这一页的空状态文案不在**（那是 #13 的教训）
//	欠着  → 点名到票 + 差什么 + **当场证明它现在真的是空的**
//	按设计 → 同样的证明，只是它不是欠债
func TestEveryRouteShowsARealRowOrSaysWhichTicketIsMissing(t *testing.T) {
	for _, pg := range realDataLedger {
		t.Run(pg.Route, func(t *testing.T) {
			switch {
			case pg.Debt != "" && pg.ByDesign != "":
				t.Fatalf("%s 既写着欠条又写着按设计——那是两件事，只能占一个", pg.Title)
			case pg.Debt != "":
				// 「欠着」这一支：欠条必须具体，且这一页必须**真的还是空的**。
				// 少了后半句，这张表就变成了万能借口：数据早就接上了，欠条还赖在
				// 上面，谁也看不出来（这正是审计里那五条说谎注释的同一个形状）。
				if pg.Why == "" {
					t.Errorf("%s 写着欠条却没说差的是什么", pg.Title)
				}
				if pg.NowEmpty == "" {
					t.Fatalf("%s 写着欠条却没有 NowEmpty——没有它，「欠着」不用证明自己还欠着", pg.Title)
				}
				if pg.Open != nil {
					t.Errorf("%s 已经能打开却有欠条：数据接上了就把欠条撤掉", pg.Title)
				}
				_, tt := realDataShellOnEmpty(t, pg)
				if !tt.HasText(pg.NowEmpty) {
					t.Fatalf("%s 欠着 %s，可是画面上现在没有那句空状态 %q——"+
						"欠条已经过期了，撤掉它，文字有 %q", pg.Title, pg.Debt, pg.NowEmpty, tt.Texts())
				}

			case pg.ByDesign != "":
				if pg.NowEmpty == "" {
					t.Fatalf("%s 按设计该空，却没有 NowEmpty", pg.Title)
				}
				_, tt := realDataShellOnEmpty(t, pg)
				if !tt.HasText(pg.NowEmpty) {
					t.Errorf("%s 按设计该是空的，可画面上没有 %q，文字有 %q", pg.Title, pg.NowEmpty, tt.Texts())
				}

			default:
				if pg.Open == nil {
					t.Fatalf("%s 既没有 Open 也没有 Debt/ByDesign：那一行什么都没说", pg.Title)
				}
				if len(pg.Want) == 0 {
					t.Fatalf("%s 没有 Want：真数据在画面上长什么样，这一行得写出来", pg.Title)
				}
				_, tt := pg.Open(t)
				for _, w := range pg.Want {
					if !tt.HasText(w) {
						t.Errorf("%s 有数据，可画面上没有 %q——这一页现在给用户看的是空的，"+
							"文字有 %q", pg.Title, w, tt.Texts())
					}
				}
				// ⚠️ 这一条才是 #13 的正题：真数据出现了，而**这一页的空状态
				// 文案不许同时出现**。只验「真数据在」的话，一个「有歌也画一句
				// 『暂无内容』」的实现照样绿。
				if empty := lookup(pg.Route).Empty; strings.TrimSpace(empty.Title) != "" {
					if tt.HasText(empty.Title) {
						t.Errorf("%s 已经有数据了，画面上却还挂着空状态 %q，文字有 %q",
							pg.Title, empty.Title, tt.Texts())
					}
				}
			}
		})
	}
}

// TestTheRealDataLedgerCoversEveryRoute 盯「加了一条路由，表里忘了加一行」。
//
// 少一行是**静默**的：那张表读起来仍然是一份完整的清单，而新那一页从那天起
// 就没人验了。所以「表必须盖住全部路由」是一条单独的断言，而它靠的是**路由表
// 是另一份东西**——两份对不上，红的是这一条。
func TestTheRealDataLedgerCoversEveryRoute(t *testing.T) {
	seen := map[string]bool{}
	for _, pg := range realDataLedger {
		if seen[pg.Route] {
			t.Errorf("表里 %s 登记了两次", pg.Route)
		}
		seen[pg.Route] = true
	}
	for _, r := range routes {
		if !seen[r.Match] {
			t.Errorf("路由表里有 %s（%s），「每一页至少有一条真数据」那张表上没有它——"+
				"加一行：它是真有数据，还是欠着", r.Match, r.Title)
		}
	}
	if !seen["*"] {
		t.Error("兜底那一页（*，404）不在表上")
	}
}

// TestEveryRouteHasAPageViewOrAnOpenDebt 是同一张表的结构那半。
//
// 「有真数据」的前置条件是**这一页有自己的画法**（`builtPage`）。没有画法的
// 路由切过去画的就是那一句空状态——那正是审计里说的「页面壳子做完了、数据源没
// 排票」的另一种形状：这一页还连壳子都没有，而它照样能通过一遍全绿的测试。
func TestEveryRouteHasAPageViewOrAnOpenDebt(t *testing.T) {
	s, _ := newShell(t)
	byRoute := map[string]realDataPage{}
	for _, pg := range realDataLedger {
		byRoute[pg.Route] = pg
	}
	for _, r := range routes {
		if s.builtPage(r) != nil {
			continue
		}
		pg, ok := byRoute[r.Match]
		if !ok {
			t.Errorf("%s（%s）既没有自己的画法，也不在「每一页至少一条真数据」那张表上",
				r.Match, r.Title)
			continue
		}
		if pg.Debt == "" && pg.ByDesign == "" {
			t.Errorf("%s（%s）没有自己的画法，表上却没写它欠着——"+
				"「用户打开是空的」这一条被漏掉了", r.Match, r.Title)
		}
	}
}

// realDataShellOnEmpty 造一个停在 pg.Route 上的空壳子，**不喂任何数据**。
//
// 欠条和「按设计」这两支都要用它：那句话得是这一页**真的**画出来的，否则
// 「欠着」就只是表上的一行字。
func realDataShellOnEmpty(t *testing.T, pg realDataPage) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	s.navigate(pg.Route)
	realDataWait(t, tt, "这一页画出来了", func() bool { return len(tt.Texts()) > 0 })
	return s, tt
}

// realDataWait 边跑帧边等 cond。判据一律是**结果**，不是帧数——后台那一趟跑在
// goroutine 上，按帧数等就是按运气等。
func realDataWait(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了三秒还没「%s」，文字有 %q", what, tt.Texts())
		}
		time.Sleep(time.Millisecond)
	}
}

// realDataFrames 连跑几帧，让这一帧的样子画完。
func realDataFrames(tt *mygoui.Tester, n int) {
	for range n {
		tt.Frame()
	}
}

// TestTheCountsInTheProseMatchTheCountsInTheCode 钉住「说明里的那个数」不是人记得的。
//
// # 为什么要有这一条
//
// 上一轮把 `/follow` 的画法和接口接上之后，「/follow 还没排票」这句假话**只在台账
// 和 `evidence.go` 里撤掉了**，`main.go` 的抬头、README、`router.go` 的 `navigate`
// **还有三处一字不差**地说着它，顺带那个「14 条里 12 条有画法」也该是 13。
//
// 而**全仓没有任何一条测试读这三处**——`TestTheRealDataLedgerCoversEveryRoute` 只
// 遍历 `routes` 表，说服力全在代码里，于是这三句假话不会被任何一遍绿盖住。
//
// 这一条就把那个洞补上：数一遍真值，回头核这三处。
//
// # 为什么只核这三处、不扫全仓
//
// 「还没排票」「连壳子」这些词在**讲规矩**的地方是正当的（`realdata_test.go`
// 文件头那三条规则举的例子就是），全仓扫会把那些一起判死，测出来的红没有信息。
// 所以这里核的是**三处会陈述现状的散文**，且只断言三件具体的事：那个数对得上、
// 没有过期的那句、没有把已经有画法的路由说成没有。
func TestTheCountsInTheProseMatchTheCountsInTheCode(t *testing.T) {
	s, _ := newShell(t)
	withView := 0
	var withoutView []Route
	for _, r := range routes {
		if s.builtPage(r) != nil {
			withView++
		} else {
			withoutView = append(withoutView, r)
		}
	}
	if withView == 0 || len(withoutView) == 0 {
		t.Fatalf("数出来 %d 条有画法、%d 条没有——这两个数本身就不像话，先修 `builtPage`",
			withView, len(withoutView))
	}

	// want 是这三处都要写着的那个数（「N 条有画法/有内容/接上了真数据」）。
	want := fmt.Sprintf("%d 条", withView)
	for _, f := range proseAboutRoutes {
		body := readRepoFile(t, f)
		if !strings.Contains(body, want) {
			t.Errorf("%s 没有说「%s」——现在有画法的是 %d 条，"+
				"而这一句是给读它的人看的（真值见 `routes` + `builtPage`）", f, want, withView)
		}
		// ⚠️ 过期的那两句：上一次更新时它们正是漏掉的那部分（说 /follow 没有画法）。
		//
		// **不查「12 条」**：那一串在 `router.go` 顶上说的是**上游 Biu** 的
		// `src/routes.tsx`（「12 条在 Layout 里」），那是**另一件事、而且是真的**，
		// 和「我们 14 条里有几条有画法」无关。而数字漂移已经被上面那条正向断言
		// 盖住了——加一页忘了改散文，`%s` 就不在文件里；反之一页没了，`%s` 就不对。
		for _, stale := range []string{"还没排票", "连壳子"} {
			if strings.Contains(body, stale) {
				t.Errorf("%s 里还有「%s」——它说的是 %v 的画法做出来之前的事，"+
					"现在那几页都有画法了", f, stale, withoutView)
			}
		}
		// 没有画法的那几条必须被点名：只有说得出「哪几条还没有」，那个数才有意义。
		for _, r := range withoutView {
			if !strings.Contains(body, r.Match) {
				t.Errorf("%s 没有点名 %s（%s）——「还有 N 条没做」不说清是哪几条，"+
					"读的人就得自己回去数", f, r.Match, r.Title)
			}
		}
	}
}

// proseAboutRoutes 是那三处**陈述现状**的散文。
//
// ⚠️ **路径是相对包目录 `internal/ui` 的**（`go test` 把工作目录设成包目录）。
var proseAboutRoutes = []string{"../../README.md", "../../main.go", "router.go"}

// readRepoFile 读那三处散文之一。读不到就当场红——
// **「读不到」绝不能当成「那句话不存在」**：那正是这个测试要防的那类错，
// 而一个悄悄什么都没验的测试比没有更糟（上一轮就是这样：台账撤了，三处散文没撤，
// 而全仓没有一条测试读它们）。
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读不到 %s：%v（路径写错了的话，这个测试就成了一句空话）", rel, err)
	}
	return string(b)
}
