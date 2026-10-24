// 票 #286（板子任务 BMG-165）的逐页出图：**外壳布局（侧栏通栏 + 顶栏只盖内容区）**在每一页上
// 长什么样，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket286Shots
//
// # 为什么逐页都要出
//
// 这一票改的是**每一页的第一眼**：侧栏从窗口顶端贯到底、顶栏只盖内容区那
// 一段（上游 `biu/src/layout/index.tsx:36-47`，报告 C-01）。而「第一眼」在
// 每一页上都被这一页自己的内容挡着——首页那一栏 Tab、设置页那一片设置项、
// 列表页那一张表头。所以只有把每一页都出出来，才能看出「外壳那一层」在每一
// 页上是不是同一副样子。
//
// # 深浅两套都要出
//
// 深色下侧栏、内容区、播放条是**同一个** `t.Background`（票 #127 统一过的），
// 三块之间只靠那条 1px 的分隔线分界——而分隔线在深色下是 15% 的白叠出来的
// `#3b3b3d`。所以「侧栏通栏」这件事在深色下才看得出真假。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 假 B 站（`httptest`）。
package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderTicket286Shots 把每一页存成 PNG，深浅各一套。
func TestRenderTicket286Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			shotShell := func(tag string, s *Shell, tt *mygoui.Tester) {
				tt.Move(600, 400)
				tt.Frame()
				save(t, dir, tag+"-"+name+".png", tt)
			}

			// 01 首页：三档 Tab 那一行 + 「全部播放」。
			//
			// ⚠️ **得有歌**：没有歌时「全部播放」是禁用的（框架给禁用控件乘
			// 0.5 不透明度），画面上是一片淡绿。
			{
				b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
				b.s.home.SetSongs(homeSongs{
					tabs: [homeTabCount][]list.Music{0: homeSongsOf("音乐", 12)},
				})
				homeFrame(b.tt)
				shotShell("01-首页", b.s, b.tt)
				b.Close()
			}

			// 02 搜索页：结果列表 + 那一行的排序和「全部播放」。
			{
				_, tt := newSearchPage(t, "周杰伦", newSearchStub(10, 20), systemIsDark(dark))
				waitFor(t, "搜索结果出来了", func() bool {
					tt.Frame()
					return tt.HasText(stubTitle(1, 0))
				})
				shotShell("02-搜索页", nil, tt)
			}

			// 03 设置页：内容收在中间一段宽度里。
			{
				b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
				b.s.navigate("/settings")
				waitFrames(t, b.tt, "设置页", func() bool { return b.tt.HasText("常规设置") })
				shotShell("03-设置页", b.s, b.tt)
				b.Close()
			}

			// 04 稍后再看 / 05 历史记录：两页都是列表页（自己的表头）。
			{
				f := newLibFake(t).withLater(1, 1, "真数据一首").withHistory(1, 30, 200, "真数据一首")
				tt, s := libraryShellOpts(t, f, laterPath, systemIsDark(dark))
				waitFrames(t, tt, "那一首拉回来了", func() bool { return rowsShown(&s.laterPage) == 1 })
				shotShell("04-稍后再看", s, tt)
			}
			{
				f := newLibFake(t).withLater(1, 1, "真数据一首").withHistory(1, 30, 200, "真数据一首")
				tt, s := libraryShellOpts(t, f, historyPath, systemIsDark(dark))
				waitFrames(t, tt, "那一首拉回来了", func() bool { return rowsShown(&s.historyPage) == 1 })
				shotShell("05-历史记录", s, tt)
			}

			// 06 我的关注 / 07 动态 / 11 UP 主空间：三页共用一台假 B 站。
			{
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
				b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{
						"items": []any{uiDynamic("d1", "真数据一条动态")}, "offset": "", "has_more": false})
				})
				b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{
						"items": []any{uiDynamic("d1", "真数据一条动态")}, "offset": "", "has_more": false})
				})
				b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{"list": map[string]any{"vlist": []any{}}})
				})
				uiSpaceData(b, "真数据 UP 主", 2, 1234)

				{
					s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
					s.navigate(followPath)
					waitFrames(t, tt, "关注的 UP 主画出来了", func() bool {
						return tt.HasText("关注页那位 UP 主")
					})
					shotShell("06-我的关注", s, tt)
				}
				{
					s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
					s.navigate(dynamicFeedPath)
					waitFrames(t, tt, "动态拉回来了", func() bool {
						return tt.HasText("真数据一条动态")
					})
					shotShell("07-动态", s, tt)
				}
				{
					s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
					s.navigate(upPathOf(42))
					waitFrames(t, tt, "空间信息拉回来了", func() bool {
						return tt.HasText("真数据 UP 主")
					})
					shotShell("11-UP主空间", s, tt)
				}
			}

			// 08 下载记录：自己的表头 + 一行。
			{
				b := newTestShell(t, systemIsDark(dark), stubbed(func(_ *testing.T, s *Shell) {
					s.downloads.rowsFn = func() []download.Snapshot {
						return []download.Snapshot{{
							ID: "dl-1", Title: "真数据一首", Label: "音频",
							Dest: "/tmp/真数据一首.m4a", Status: download.StatusDone,
							Done: 1024, Total: 1024, Percent: 100,
						}}
					}
				}))
				b.s.navigate(downloadListPath)
				dlFrame(b.tt)
				shotShell("08-下载记录", b.s, b.tt)
				b.Close()
			}

			// 09 本地音乐：自己扫出来的库。
			{
				root := t.TempDir()
				libSong(t, root, "真数据一首.mp3")
				tt, s, a, _ := shellOnLocalMusicIn(t, dark, root)
				settleLocal(t, a, tt)
				waitFrames(t, tt, "本地音乐扫出了那一首", func() bool {
					return len(s.local.rows) == 1
				})
				shotShell("09-本地音乐", s, tt)
			}

			// 10 收藏夹：一张卡 + 操作条。
			{
				_, s, tt := collectionShellOpts(t, []shellOpt{systemIsDark(dark)}, "12345", "12345",
					mediaOf(1, "真数据一首"))
				waitSongs(t, tt, s, 1)
				shotShell("10-收藏夹", s, tt)
			}

			// 12 扫码登录：内容区那一页自己的画法。
			{
				b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
				b.s.navigate("/login")
				waitFrames(t, b.tt, "登录页", func() bool { return b.tt.HasText("扫码登录") })
				shotShell("12-扫码登录", b.s, b.tt)
				b.Close()
			}

			// 13 404：兜底那一页。
			{
				b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
				b.s.navigate("/没有这一条")
				waitFrames(t, b.tt, "404", func() bool {
					return b.tt.HasText(lookup("/没有这一条").Title)
				})
				shotShell("13-404", b.s, b.tt)
				b.Close()
			}

			// 14 **最窄窗口 + 最宽侧栏**：外壳布局最坏的一档。
			//
			// ⚠️ **这一档是这一票改出来的**：顶栏现在只盖内容区，而窗口最小
			// （900）加上侧栏最大（480）之后内容区只剩 420——顶栏那一排控件
			// （搜索框 280 + 两个箭头 + 右边五枚键）在 420 里放不下，**搜索框
			// 先让位**（`underlay.go` 那一处 `MinWidth(0)` 的注释写着为什么）。
			// 不出这一张的话，那一档的顶栏长什么样没人知道。
			{
				st := app.DefaultSettings("")
				st.SideMenuWidth = 480
				b := newTestShell(t, systemIsDark(dark), withSettings(st),
					at(minWindowW, minWindowH),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				b.s.navigate("/")
				waitFrames(t, b.tt, "首页画出来了", func() bool { return b.tt.HasText("播放") })
				b.tt.Move(500, 300)
				b.tt.Frame()
				save(t, dir, "14-最窄窗口最宽侧栏-"+name+".png", b.tt)
				b.Close()
			}
		})
	}
}
