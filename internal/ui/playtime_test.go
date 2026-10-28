package ui

// 这一组钉「播放量」和「投稿时间」两列从接口一路走到画面上（票 #142）。
//
// # 为什么要逐页问一遍
//
// 两列的数据源**每一页都不一样**（票面点名：逐页核，别拿一个页的字段套另一个页）：
//
//	收藏夹	`cnt_info.play` + `fav_time`（表头写「收藏时间」）
//	搜索		`play` + `pubdate`
//	UP 空间投稿	`play` + `created`
//	稍后再看	`stat.view` + `pubdate`（另外还有筛选用的 `add_at`）
//	首页		`stat.view`，**没有投稿时间**（上游就是 hidePubTime）
//
// 所以一页一页问「画面上有没有那个字」，而不是对着某一个映射函数断。
// 「收藏夹那一路也有它」是这一票最关键的一条（票面原话）。

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// playtimeNow 是这一组固定的「现在」。「3天前」这类断言不能跟着墙钟走：
// 过一天就红，而红的时候和被测的逻辑毫无关系。
var playtimeNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)

// collectionMediaWithNumbers 造一条收藏夹内容，带着播放量和两个时间。
//
// favAge / pubAge 是「几天前」，负数就是那一天之后。
func collectionMediaWithNumbers(id int, title string, play, favAge, pubAge int) map[string]any {
	m := mediaOf(id, title)
	m["cnt_info"] = map[string]any{"collect": 1, "play": play, "danmaku": 0}
	m["fav_time"] = playtimeNow.AddDate(0, 0, -favAge).Unix()
	m["pubtime"] = playtimeNow.AddDate(0, 0, -pubAge).Unix()
	return m
}

// TestTheCollectionPageShowsThePlayCountAndTheCollectionTime 是这一票最关键的那条：
// 收藏夹那一路的播放量真的画出来了，时间那一列画的是**收藏时间**。
//
// ⚠️ **收藏夹那一列不叫「投稿时间」**：上游 `favorites/list.tsx:63` 画的是
// `fav_time`，表头也写「收藏时间」。拿 `pubtime` 套上去的话，用户读到的是
// 一个和标题对不上的时间。
func TestTheCollectionPageShowsThePlayCountAndTheCollectionTime(t *testing.T) {
	m := collectionMediaWithNumbers(1, "有数的歌", 123_400, 3, 100)
	_, s, tt := collectionShellOpts(t, []shellOpt{withClock(newFakeClock(playtimeNow))},
		"", "77", m)
	waitSongs(t, tt, s, 1)

	// 表头：两列都在，时间那列叫「收藏时间」。
	for _, want := range []string{"播放量", "收藏时间"} {
		if !uiShows(tt, want) {
			t.Errorf("收藏夹的表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	if uiShows(tt, "投稿时间") {
		t.Errorf("收藏夹那一页画的是 fav_time，表头却写着「投稿时间」：%q", tt.Texts())
	}

	// 行里：播放量折过之后是「12.3万」；时间那一格是收藏时间的「3天前」，
	// **不是**发布时间（100 天前 → 「07-13」）。
	if !uiShows(tt, list.PlayCountText(123_400)) {
		t.Errorf("行里没有播放量 %q：%q", list.PlayCountText(123_400), tt.Texts())
	}
	if want := list.PubTimeText(playtimeNow.AddDate(0, 0, -3), playtimeNow); !uiShows(tt, want) {
		t.Errorf("行里没有收藏时间 %q：%q", want, tt.Texts())
	}
	if unwanted := list.PubTimeText(playtimeNow.AddDate(0, 0, -100), playtimeNow); uiShows(tt, unwanted) {
		t.Errorf("行里画的是发布时间 %q，收藏夹那一列该画 fav_time：%q", unwanted, tt.Texts())
	}
}

// TestTheCollectionGridCarriesThePlayCountIntoTheGradient 钉住**网格卡片
// 封面底部那一条渐变的左半边**也有收藏夹的播放量。
//
// 那一半画的是 `Music.View`（`hover.CardCover`），和列表那一列是同一个数——
// 数据没从 `bili.Media.View` 翻过去的话，两处一起空着。
func TestTheCollectionGridCarriesThePlayCountIntoTheGradient(t *testing.T) {
	m := collectionMediaWithNumbers(1, "有数的歌", 123_400, 3, 100)
	_, s, tt := collectionShellOpts(t, []shellOpt{withClock(newFakeClock(playtimeNow))},
		"", "77", m)
	waitSongs(t, tt, s, 1)

	setDisplay(t, s.app, tt, app.DisplayModeCard)
	wideEnough(tt)
	// ⚠️ **指针压在格子右上角那枚「⋯」上，不是压在歌名上**：`tt.Find(歌名)`
	// 拿到的是网格**整行**的盒子（`Grid.state.Label` 是这一行第一首的歌名），
	// 一行有几格时它的中心可能落在别的格子上。那一枚「⋯」在格子里面，压上去
	// 一定落在这一格。
	op, ok := tt.Find(list.OperationLabel("有数的歌"))
	if !ok {
		t.Fatalf("画面上找不到那一格的「⋯」，文字有 %q", tt.Texts())
	}
	tt.Move(op.X+op.W/2, op.Y+op.H/2)
	tt.Frame()
	tt.Frame()
	tt.Advance(hover.Fade)
	tt.Frame()

	if !uiShows(tt, list.PlayCountText(123_400)) {
		t.Errorf("网格卡片封面底部的渐变上没有播放量 %q：%q",
			list.PlayCountText(123_400), tt.Texts())
	}
}

// TestTheSearchVideoTabShowsThePlayCountAndTheUploadTime 钉住搜索页那一档。
//
// 这里用一份**返回真映射结果**的假拉页器：`searchVideoMusic` 是数据源到
// 行的那一段，拿真的它才验得到 `VideoHit.PubDate` 有没有翻过去。
func TestTheSearchVideoTabShowsThePlayCountAndTheUploadTime(t *testing.T) {
	s, tt := newShell(t, withClock(newFakeClock(playtimeNow)))
	s.search.fetch = func(tab int, _ context.Context, page int) (list.Page, error) {
		if tab != searchTabSong || page > 1 {
			return list.Page{}, nil
		}
		return list.Page{Songs: []list.Music{
			searchVideoMusic(bili.VideoHit{
				Bvid: "BV1time", Title: "搜到的歌", Owner: "某UP",
				Play: 123_400, PubDate: playtimeNow.AddDate(0, 0, -3),
			}),
		}}, nil
	}
	s.navigate(searchPath + "?" + searchQuery + "=歌")
	waitFrames(t, tt, "搜索结果落地", func() bool {
		return pageOf(s, searchTabSong).Loaded == 1
	})

	for _, want := range []string{"播放量", "投稿时间"} {
		if !uiShows(tt, want) {
			t.Errorf("搜索页的表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	if !uiShows(tt, list.PlayCountText(123_400)) {
		t.Errorf("行里没有播放量 %q：%q", list.PlayCountText(123_400), tt.Texts())
	}
	if want := list.PubTimeText(playtimeNow.AddDate(0, 0, -3), playtimeNow); !uiShows(tt, want) {
		t.Errorf("行里没有投稿时间 %q：%q", want, tt.Texts())
	}
}

// TestTheHomeTabShowsThePlayCountButNoUploadTime 钉住首页那一档。
//
// 上游首页是 `hidePubTime`（`pages/music-recommend/list.tsx:49`），而且它那一层
// 的 `RecommendItem` 也没带 `pubdate`——所以表头上**不该**有「投稿时间」。
func TestTheHomeTabShowsThePlayCountButNoUploadTime(t *testing.T) {
	_, tt := newHome(t, homeSongs{
		tabs: [homeTabCount][]list.Music{{
			{ID: "a", Title: "首页的歌", Duration: time.Minute, View: 123_400},
		}},
	})

	if !uiShows(tt, "播放量") {
		t.Errorf("首页的表头上没有「播放量」：%q", tt.Texts())
	}
	if uiShows(tt, "投稿时间") {
		t.Errorf("首页的表头上摆着「投稿时间」，而这一页没有那个数：%q", tt.Texts())
	}
	if !uiShows(tt, list.PlayCountText(123_400)) {
		t.Errorf("首页行里没有播放量 %q：%q", list.PlayCountText(123_400), tt.Texts())
	}
}

// TestTheCollectionTimeTitleFollowsTheContainer 钉住时间那一列的标题和取数
// **随容器类型一起变**：收藏夹是 fav_time / 「收藏时间」，合集和系列是
// pubtime / 「投稿时间」。
//
// 两半是同一件事，一起断——只改一边的话，收藏夹页会写着「投稿时间」而画的是
// 收藏时间（或者反过来），用户按它判断新旧，读到的是错的。
func TestTheCollectionTimeTitleFollowsTheContainer(t *testing.T) {
	m := bili.Media{
		FavTime: playtimeNow.AddDate(0, 0, -3),
		PubTime: playtimeNow.AddDate(0, 0, -100),
	}
	if got := collectionTimeTitle(bili.CollectionFavorite); got != "收藏时间" {
		t.Errorf("收藏夹的时间列标题是 %q，want 收藏时间", got)
	}
	if got := collectionTimeOf(bili.CollectionFavorite, m); !got.Equal(m.FavTime) {
		t.Errorf("收藏夹取到的时间是 %v，want fav_time（%v）", got, m.FavTime)
	}
	for _, typ := range []bili.CollectionType{bili.CollectionAlbum, bili.CollectionSeries} {
		if got := collectionTimeTitle(typ); got != "投稿时间" {
			t.Errorf("%v 的时间列标题是 %q，want 投稿时间", typ, got)
		}
		if got := collectionTimeOf(typ, m); !got.Equal(m.PubTime) {
			t.Errorf("%v 取到的时间是 %v，want pubtime（%v）", typ, got, m.PubTime)
		}
	}
}

// TestRenderPlayTimeShots 把「收藏夹的两列」和「稍后再看的两列时间」存成 PNG。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayTimeShots
//
// 深浅两套都出（理由和 `TestRenderHoverShots` 顶上那一段一样）。这一族抓的是
// **量不出来的东西**：两列挤在一起时字有没有被裁掉、数字列竖着扫不扫得下来。
//
// ⚠️ 全部是内存夹具 + 无头渲染，一个真 B 站接口都没打。
func TestRenderPlayTimeShots(t *testing.T) {
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
			medias := []map[string]any{
				collectionMediaWithNumbers(1, "播放最多的一首", 1_234_000, 2, 300),
				collectionMediaWithNumbers(2, "第二首", 120_000, 10, 800),
				collectionMediaWithNumbers(3, "第三首", 9_800, 400, 40),
			}
			_, s, tt := collectionShellOpts(t,
				[]shellOpt{systemIsDark(dark), withClock(newFakeClock(playtimeNow))},
				"", "77", medias...)
			waitSongs(t, tt, s, len(medias))
			tt.Move(1180, 300)
			settleFrame(tt)
			save(t, dir, "40-"+name+"-收藏夹-播放量与收藏时间.png", tt)

			// 网格那一档：封面底部渐变的左半边。
			setDisplay(t, s.app, tt, app.DisplayModeCard)
			wideEnough(tt)
			op, ok := tt.Find(list.OperationLabel("播放最多的一首"))
			if !ok {
				t.Fatalf("画面上找不到第一格的「⋯」，文字有 %q", tt.Texts())
			}
			tt.Move(op.X+op.W/2, op.Y+op.H/2)
			tt.Frame()
			tt.Frame()
			tt.Advance(hover.Fade)
			tt.Frame()
			save(t, dir, "41-"+name+"-收藏夹-网格渐变.png", tt)
		})

		t.Run(name+"-稍后再看", func(t *testing.T) {
			f := newLibFake(t).withLater(1, 4, libTitles...)
			tt, s := libraryShellOpts(t, f, laterPath, systemIsDark(dark))
			waitFrames(t, tt, "稍后再看第一页", func() bool {
				return rowsShown(&s.laterPage) == 4
			})
			tt.Move(1180, 300)
			settleFrame(tt)
			save(t, dir, "42-"+name+"-稍后再看-两列时间.png", tt)
		})

		// 窄窗口：最小窗口 900 + 侧栏 480。这一档看的是「标题和「⋯」还在不在」
		//（评审抓到的回归就在这儿）。
		t.Run(name+"-窄窗口", func(t *testing.T) {
			st := app.DefaultSettings("")
			st.SideMenuWidth = 480
			m := collectionMediaWithNumbers(1, "播放最多的一首", 1_234_000, 2, 300)
			_, s, tt := collectionShellOpts(t, []shellOpt{
				withSettings(st), at(minWindowW, minWindowH),
				withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}),
				systemIsDark(dark), withClock(newFakeClock(playtimeNow)),
			}, "", "77", m)
			waitSongs(t, tt, s, 1)
			for range 3 {
				tt.Frame()
			}
			tt.Move(880, 300)
			settleFrame(tt)
			save(t, dir, "43-"+name+"-收藏夹-窄窗口480.png", tt)
		})

		t.Run(name+"-稍后再看-窄窗口", func(t *testing.T) {
			for _, side := range []float64{300} {
				st := app.DefaultSettings("")
				st.SideMenuWidth = side
				f := newLibFake(t).withLater(1, 4, libTitles...)
				tt, s := libraryShellOpts(t, f, laterPath,
					withSettings(st), at(minWindowW, minWindowH),
					withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}),
					systemIsDark(dark))
				waitFrames(t, tt, "稍后再看第一页", func() bool {
					return rowsShown(&s.laterPage) == 4
				})
				for range 3 {
					tt.Frame()
				}
				tt.Move(880, 300)
				settleFrame(tt)
				save(t, dir, "44-"+name+"-稍后再看-窄"+strconv.FormatInt(int64(side), 10)+".png", tt)
			}
		})
	}
}

// TestTheLaterPageShowsTheUploadTimeAsWellAsTheAddTime 钉住稍后再看那一页
// **两列时间都在**。
//
// 上游画的是 `pubdate`（「投稿时间」），而这一页的日期筛选筛的是 `add_at`
// （「加入时间」）——两列各回答一半问题，拿一个顶另一个总有一半没答案。
func TestTheLaterPageShowsTheUploadTimeAsWellAsTheAddTime(t *testing.T) {
	tt, _ := laterHoverShell(t)

	for _, want := range []string{"加入时间", "投稿时间"} {
		if !uiShows(tt, want) {
			t.Errorf("稍后再看的表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	// 夹具里每一条的 pubdate 都是 1700000000（2023-11-14），在假时钟
	// （2026-10-16）眼里是跨年，所以那一格写 `YYYY-MM-DD`。
	if want := list.PubTimeText(time.Unix(1700000000, 0), now); !uiShows(tt, want) {
		t.Errorf("行里没有投稿时间 %q：%q", want, tt.Texts())
	}
}

// TestTheColumnsLeaveTheTitleVisibleInTheNarrowestWindow 钉住窄窗口下的让位。
//
// 评审量到过：最小窗口 900 + 侧栏 480 时，本票新加的两列把标题列挤成 0，
// 收藏夹 / 搜索页的歌名与表头「标题」都只剩「…」，而同尺寸的基线是好的。
// 现在 `List.Columns()` 会先收「投稿时间」、再不行才收「播放量」——标题
// 始终留得出字，行尾的「⋯」也不会被挤出画面。
//
// ⚠️ 列间距改成 16、序号收成 32 之后，右边那几列一共吃掉 416（原来 376）：
// 侧栏 400 时只放得下「播放量」，侧栏 480 时**两列都放不下**。让位的顺序没变，
// 变的是哪一档先让到位。
func TestTheColumnsLeaveTheTitleVisibleInTheNarrowestWindow(t *testing.T) {
	for _, tc := range []struct {
		side              float64
		wantPlay, wantPub bool
	}{
		{300, true, true}, // 设计规范把侧栏上限收到 300：最小窗口下列表宽约 560，两列都放得下，标题当然读得出
	} {
		t.Run(strconv.FormatInt(int64(tc.side), 10), func(t *testing.T) {
			st := app.DefaultSettings("")
			st.SideMenuWidth = tc.side
			m := collectionMediaWithNumbers(1, "歌 一", 123_400, 3, 100)
			_, s, tt := collectionShellOpts(t, []shellOpt{
				withSettings(st), at(minWindowW, minWindowH),
				withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}),
				withClock(newFakeClock(playtimeNow)),
			}, "", "12345", m)
			waitSongs(t, tt, s, 1)
			// 列定义按**上一帧**量到的宽算（`List.Columns`），多画两帧让它落定。
			for range 3 {
				tt.Frame()
			}

			head, ok := tt.Find("标题")
			if !ok {
				t.Fatalf("侧栏 %.0f：表头上找不到「标题」，文字有 %q", tc.side, tt.Texts())
			}
			if head.W < 40 {
				t.Errorf("侧栏 %.0f：表头「标题」只剩 %.1f 宽——被新加的两列挤没了",
					tc.side, head.W)
			}
			if _, ok := tt.Find("UP 1"); !ok {
				t.Errorf("侧栏 %.0f：行里没有 UP 名（标题列被挤没了）：%q",
					tc.side, tt.Texts())
			}
			// 收藏夹那一列的表头写的是「收藏时间」（`collectionTimeTitle`），
			// 不是「投稿时间」——查错字的话会以为它在、其实没有。
			if got := uiShows(tt, "收藏时间"); got != tc.wantPub {
				t.Errorf("侧栏 %.0f：「收藏时间」那一列在不在=%v，want %v",
					tc.side, got, tc.wantPub)
			}
			if got := uiShows(tt, "播放量"); got != tc.wantPlay {
				t.Errorf("侧栏 %.0f：播放量那一列在不在=%v，want %v", tc.side, got, tc.wantPlay)
			}

			// 表头和行里那一格必须对得上（评审量到过错开 9.9 像素）。
			if tc.wantPlay {
				headCount, ok := tt.Find("播放量")
				if !ok {
					t.Fatalf("侧栏 %.0f：表头上没有「播放量」，文字有 %q", tc.side, tt.Texts())
				}
				rowCount, ok := tt.Find(list.PlayCountText(123_400))
				if !ok {
					t.Fatalf("侧栏 %.0f：行里没有播放量 %q：%q",
						tc.side, list.PlayCountText(123_400), tt.Texts())
				}
				if rowCount.X != headCount.X || rowCount.W != headCount.W {
					t.Errorf("侧栏 %.0f：播放量那一格 X=%.1f W=%.1f，表头 X=%.1f W=%.1f——错开了",
						tc.side, rowCount.X, rowCount.W, headCount.X, headCount.W)
				}
			}
			// 行尾的「⋯」也要在（它被挤出画面之后，这一行就只剩右键能开菜单了）。
			op, ok := tt.Find(list.OperationLabel("歌 一"))
			if !ok || op.W <= 0 {
				t.Errorf("侧栏 %.0f：行尾的「⋯」被挤出画面（%v）", tc.side, op)
			}
		})
	}
}

// TestTheSearchListGivesWayToTheTitleAtTheWidestSidebar 钉住搜索页也走同一套
// 让位：评审点名了「搜索歌曲页 900/480 同样复现（表头「标题」=「…」，行标题 0）」。
//
// 搜索页的表头是**自己给规格**的（`searchHeaderSpec`），所以它要读
// `List.Columns()`（这一帧真的会画哪几列），不能读配置的那一份。
func TestTheSearchListGivesWayToTheTitleAtTheWidestSidebar(t *testing.T) {
	st := app.DefaultSettings("")
	st.SideMenuWidth = 300
	s, tt := newShell(t,
		withSettings(st), at(minWindowW, minWindowH),
		withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}),
		withClock(newFakeClock(playtimeNow)))
	stub := newSearchStub(20, 1)
	stub.install(s)
	s.navigate(searchPath + "?" + searchQuery + "=甲")
	waitFrames(t, tt, "搜索结果落地", func() bool {
		return pageOf(s, searchTabSong).Loaded == 1
	})
	for range 3 {
		tt.Frame()
	}

	head, ok := tt.Find("标题")
	if !ok || head.W < 40 {
		t.Errorf("表头「标题」只剩 %v 宽（want ≥ 40）——被新加的两列挤没了", head)
	}
	// 侧栏上限现在是 300，最小窗口下搜索列表放得下全部列；这一条只守「标题不被挤成…」。
	if !uiShows(tt, stubTitle(1, 0)) {
		t.Errorf("行里的歌名不在画面上：%q", tt.Texts())
	}
}

// TestTheLaterRowsKeepTheirOpButtonAtNarrowWidths 钉住稍后再看那一页的让位。
//
// 评审量到过：加了「投稿时间」之后，900 宽 + 侧栏 400 起行尾「⋯」被挤成 0；
// 侧栏 480 时连新的时间列本身都被裁。现在 `laterTimeCols` 两级让位：
//
//	400：投稿时间放不下 → 收它；基线五列还是留不出标题 → 「加入时间」也收
//	480：两列都收
//
// 两档都要验「标题读得出几个字、行尾『⋯』还在」——那两个数随列间距 / 序号宽度
// 变（16 / 32 这一档改过一次），所以判据是拿得到的宽，不是某个具体像素。
func TestTheLaterRowsKeepTheirOpButtonAtNarrowWidths(t *testing.T) {
	for _, side := range []float64{300} {
		t.Run(strconv.FormatInt(int64(side), 10), func(t *testing.T) {
			st := app.DefaultSettings("")
			st.SideMenuWidth = side
			f := newLibFake(t).withLater(1, 4, libTitles...)
			tt, s := libraryShellOpts(t, f, laterPath, withSettings(st), at(minWindowW, minWindowH),
				withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}))
			waitFrames(t, tt, "稍后再看第一页", func() bool {
				return rowsShown(&s.laterPage) == 4
			})
			for range 3 {
				tt.Frame()
			}

			// 侧栏上限收到 300 之后最小窗口下两列时间都放得下，不再有「放不下」的那一档；
			// 剩下要守的是：标题读得出、行尾「⋯」不被挤出画面（下面两条）。
			op, ok := tt.Find(list.OperationLabel(libTitles[0]))
			if !ok || op.W <= 0 {
				t.Errorf("侧栏 %.0f：行尾的「⋯」被挤出画面（%v）：%q", side, op, tt.Texts())
			}
			head, ok := tt.Find("标题")
			if !ok || head.W < 40 {
				t.Errorf("侧栏 %.0f：表头「标题」只剩 %v 宽（want ≥ 40）", side, head)
			}
		})
	}
}
