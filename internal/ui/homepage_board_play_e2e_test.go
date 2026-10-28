package ui

// 「点首页榜单上的一首歌，它真的放出来了」的端到端（票：首页榜单点不动）。
//
// # 为什么这一份是 E2E
//
// 症状是「点了没反应，播放条上写着『B 站拒绝了这次请求』」。而这一条路上
// **每一层单独看都是对的**：
//
//	榜单接口返回正常 → 页面画出来了（那一行有歌名、有封面）
//	队列也收进去了 → 点一下确实加进了队列
//	取流那一层判的是「cid 不为空」→ 而它拿到的是字符串 "0"，**不是空串**
//
// ⇒ 断在哪一层，只有把「点 → 放出来」整条路走一遍才看得见。
//
// # 这一份盯两件事
//
//  1. **点榜单上的一首歌，它放出来了**（当前项是它，播放条上是它）；
//  2. **取流请求发出去的 cid 是真的分 P id**，不是 "0"。
//
// ⚠️ **全部走内存里的假 B 站**（AGENTS.md §6）：一个真接口都不打。

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// boardPlayable 让假 B 站**每一次取流都成功**，并且记下它收到的 cid。
//
// ⚠️ **它要回真音频**：只回一段 JSON 的话，播放器开不起来（"没有音频流"），
// 那一类是**不可重试**的，`autoSkipDead` 会一路往后跳——于是「当前项是点的
// 那一首」这条断言会被自动跳过盖掉（理由见 `upSpacePlayable` 那段）。
//
// ⚠️ **记下 cid 是这一份的另一半**：断言「放出来了」只能证明整条路通了，
// 而「发出去的 cid 是真的那个」才钉得住**是哪一个数**错了——cid 错成别的
// 时候，歌也许照样放得出来（多 P 投稿放的是第一集）。
//
// ⚠️ **分 P 列表也要挂**（`/x/player/pagelist`）：榜单那一档有的是条目**不带**
// 分 P id（`creation_first_cid` 是后加的字段，老的榜没有），那一条只能靠
// **问一次**分 P 才放得出来。而 `data` **是数组**（上游
// `player-pagelist.ts:18`）——fixture 的形状必须照着真接口抄，理由见
// `serveVideoPages` 那段。
func boardPlayable(t *testing.T, b *uiExplore) *string {
	t.Helper()
	media, err := os.ReadFile(flac(t))
	if err != nil {
		t.Skipf("读不到 FLAC 测试素材：%v", err)
	}
	b.on("/media/a.flac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp4")
		_, _ = w.Write(media)
	})
	b.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, []any{map[string]any{
			"cid": 777, "page": 1, "part": "第一集", "duration": 260,
		}})
	})
	got := new(string)
	b.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		*got = r.URL.Query().Get("cid")
		uiExploreJSON(w, 0, map[string]any{
			"dash": map[string]any{
				"duration": 1,
				"audio": []any{map[string]any{
					"id": 30280, "bandwidth": 110836, "codecs": "flac",
					"mimeType": "audio/mp4", "baseUrl": b.URL + "/media/a.flac",
				}},
			},
		})
	})
	return got
}

// doubleClickText 双击画面上那个字。
//
// ⚠️ **榜单那一列是双击才播**（Biu 的 `onDoubleClick`，见
// `widgets/list/list.go` 的 `RowDoubleClicked`）——单击是选中。所以这里点两下。
//
// ⚠️ **要点在盒子里**：虚拟列表对没进画面的行也给一句文字，但盒子是 0×0，
// 点一个 0×0 的盒子点不到任何东西（理由见 `doubleClickSongRow`）。
func doubleClickText(t *testing.T, tt *mygoui.Tester, s string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		tt.Frame()
		r, ok := tt.Find(s)
		if ok && r.W > 0 && r.H > 0 {
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			tt.Frame()
			return
		}
	}
	t.Fatalf("画面上找不到可点的「%s」（画面：%q）", s, tt.Texts())
}

// TestClickingASongOnTheHomeBoardPlaysIt 是这一票的 E2E 钉子。
//
// 复现用户报的那条路：
//
//  1. 打开首页，切到「热榜」；
//  2. 点榜上的第一首；
//  3. 它**要放出来**（当前项是它，播放条上是它），而不是播放条上挂一句红字。
//
// ⚠️ **榜单接口要带 `creation_first_cid`**：B 站真的给了这个字段（真网实测：
// 热榜 20/20 条都有），而我们原来没解析它——于是取流那一层拿到的是字符串
// "0"，B 站回 -400「请求错误」。
func TestClickingASongOnTheHomeBoardPlaysIt(t *testing.T) {
	b := homeDiscoverFake(t)
	gotCid := boardPlayable(t, b)
	_, tt, a := newHomeShell(t, b)

	// 切到「热榜」那一档。
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeHotTab]); err != nil {
		t.Fatalf("点不到「%s」：%v，文字有 %q", homeTabs[homeHotTab], err, tt.Texts())
	}
	waitFramesClock(t, tt, "热榜那一档", func() bool { return showsText(tt, "热榜第一名") })

	// 双击榜上那一首（榜单那一列是双击才播）。
	doubleClickText(t, tt, "热榜第一名")

	// 它要放出来：当前项是它，播放条上是它。
	waitFrames(t, tt, "这一首放出来了", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Title == "热榜第一名" && st.Err == ""
	})
	st := a.Snapshot()
	if st.Err != "" {
		t.Fatalf("播放条上挂着错：%q", st.Err)
	}
	if !st.HasTrack {
		t.Fatalf("没有歌：HasTrack=%v，Err=%q", st.HasTrack, st.Err)
	}

	// ⚠️ **发出去的 cid 要是真的分 P id**。
	//
	// 假 B 站给的榜条目没带 `creation_first_cid`（`homeDiscoverFake` 那一份），
	// 所以这里预期的是**空串**——空串会让取流那一层去问一次分 P（问不到就
	// 报错），而**不是**把 "0" 当成真的分 P id 发出去。
	//
	// 这条断言盯的是**没有 "0"**：它是那个 bug 的形状。
	if *gotCid == "0" {
		t.Errorf("取流发出去的 cid = %q——那是「没有」被当成一个真的分 P id 传下去"+
			"（B 站回 -400，界面上写「B 站拒绝了这次请求」）", *gotCid)
	}
}

// TestTheBoardEntryCarriesTheFirstPartId 是上面那一条的**更直接**的一支：
// 榜单接口**带了**分 P id 的时候，那一条要**带着它**进队列。
//
// ⚠️ **为什么不并进上面那一条**：上面那一条走的是「接口不给分 P id → 去问一次」
// 那条路；这一条走的是「接口给了 → 直接用」那条路。**两条路都得通**，而
// 上面那一条用的假数据本来就没有分 P id，钉不到第二条。
func TestTheBoardEntryCarriesTheFirstPartId(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeToplistPeriods, func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": map[string]any{
			"2024": []map[string]any{{"ID": 8, "priod": 3, "publish_time": 200}},
		}})
	})
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"music_id": "MA1", "music_title": "榜上那首", "singer": "榜上歌手",
			"creation_bvid": "BV1board", "creation_aid": 1,
			"creation_first_cid": 27922599802,
			"creation_cover":     "https://f/board.jpg", "creation_duration": 120, "creation_play": 5,
		}}})
	})
	gotCid := boardPlayable(t, b)
	_, tt, a := newHomeShell(t, b)

	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeHotTab]); err != nil {
		t.Fatalf("点不到「%s」：%v，文字有 %q", homeTabs[homeHotTab], err, tt.Texts())
	}
	waitFramesClock(t, tt, "热榜那一档", func() bool { return showsText(tt, "榜上那首") })
	doubleClickText(t, tt, "榜上那首")
	waitFrames(t, tt, "这一首放出来了", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Title == "榜上那首" && st.Err == ""
	})

	// 🔴 **cid 要是榜单给的那个**，不是别的、也不是空串。
	//
	// ⚠️ **这一条比上面那条更严**：上面那条只断「不是 0」，而这一条断「就是
	// 那个数」——少了分 P id 而去问一次也能放出来，但那是**多一次网络往返**，
	// 而且榜单本来就知道答案。
	if *gotCid != "27922599802" {
		t.Errorf("取流发出去的 cid = %q，want \"27922599802\"（榜单给的"+
			"creation_first_cid 要直接用上，不该白问一次分 P）", *gotCid)
	}
}

// TestTheFailedBoardSongSaysSoOnThePlayBar 盯**失败时播放条上说的是什么**。
//
// 这一条是另一半：上两条盯「放出来了」，这一条盯「放不出来的时候用户看得
// 懂发生了什么」。
//
// ⚠️ **假 B 站这一趟回 -400**（B 站真的会给这个：cid 不对的时候它就是回
// -400「请求错误」）。那一句在 `bili.Friendly` 里是「B 站拒绝了这次请求」。
func TestTheFailedBoardSongSaysSoOnThePlayBar(t *testing.T) {
	b := homeDiscoverFake(t)
	b.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -400, nil)
	})
	_, tt, a := newHomeShell(t, b)

	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeHotTab]); err != nil {
		t.Fatalf("点不到「%s」：%v，文字有 %q", homeTabs[homeHotTab], err, tt.Texts())
	}
	waitFramesClock(t, tt, "热榜那一档", func() bool { return showsText(tt, "热榜第一名") })
	doubleClickText(t, tt, "热榜第一名")
	waitFrames(t, tt, "播放条上挂出错", func() bool { return a.Snapshot().Err != "" })
	if got := a.Snapshot().Err; got == "" {
		t.Fatal("放不出来却什么都没说")
	}
	_ = app.SourceOnline
}
