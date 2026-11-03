// 听歌统计页的界面测试。
//
// 这一组钉的是**「这一页画出来是什么」**：四档在那儿、数在那儿、空状态
// 说清了为什么空。
//
// ⚠️ **数据是摆进去的**：这一页拿的是 `App.ListeningStats()`，而测试里
// 造一份统计比造一个真播放器容易得多。

package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// statsShell 造一个带着统计的外壳。
func statsShell(t *testing.T, seed func(a *app.App)) (*Shell, *ui.Tester) {
	t.Helper()
	b := newTestShell(t, mounted(func(_ *testing.T, a *app.App) {
		if err := a.MountListeningStats(app.StatsPath(t.TempDir())); err != nil {
			t.Fatal(err)
		}
		if seed != nil {
			seed(a)
		}
	}))
	return b.s, b.tt
}

// TestTheStatsPageSaysItHasNoRecordsYet 钉住「没听过歌时的那一句」。
//
// ⚠️ **这一页的空状态画在自己里面**（不在路由表里），理由同 follow.go。
func TestTheStatsPageSaysItHasNoRecordsYet(t *testing.T) {
	s, tt := statsShell(t, nil)
	s.navigate(statsPath)
	tt.Frame()

	if !tt.HasText("听歌统计") {
		t.Errorf("页面标题没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("还没有听歌记录") {
		t.Errorf("空状态没画出来，文字有 %q", tt.Texts())
	}
}

// TestTheStatsPageShowsTheFourTabs 钉住「四档都在」。
func TestTheStatsPageShowsTheFourTabs(t *testing.T) {
	s, tt := statsShell(t, nil)
	s.navigate(statsPath)
	tt.Frame()

	for _, name := range statsTabNames {
		if !tt.HasText(name) {
			t.Errorf("「%s」那一档没画出来，文字有 %q", name, tt.Texts())
		}
	}
}

// TestTheStatsPageShowsTheNumbers 钉住「有数据时那四枚磁贴的数字」。
func TestTheStatsPageShowsTheNumbers(t *testing.T) {
	s, tt := statsShell(t, func(a *app.App) {
		seedStats(a)
	})
	s.navigate(statsPath)
	tt.Frame()

	// 90 分钟 = 1.5 小时。
	if !tt.HasText("1.5") {
		t.Errorf("时长那一枚没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("1 小时 30 分钟") {
		t.Errorf("时长的说法没画出来，文字有 %q", tt.Texts())
	}
	// ⚠️ **「12」和「首」是两个元素**：单位挂在数字右边（不是同一个字符串），
	// 而 `HasText` 断的是**每一个元素的字**——所以这里断两个。
	if !tt.HasText("12") {
		t.Errorf("起播那一枚的数字没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("首") {
		t.Errorf("起播那一枚的单位没画出来，文字有 %q", tt.Texts())
	}
}

// TestTheStatsPageShowsTheTopSongs 钉住「Top 歌那一块」。
func TestTheStatsPageShowsTheTopSongs(t *testing.T) {
	s, tt := statsShell(t, func(a *app.App) {
		seedStats(a)
	})
	s.navigate(statsPath)
	tt.Frame()

	if !tt.HasText("最爱听的歌") {
		t.Errorf("Top 歌那一块没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("一首歌") {
		t.Errorf("歌名没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("最爱听的 UP 主") {
		t.Errorf("Top 艺人那一块没画出来，文字有 %q", tt.Texts())
	}
}

// TestTheStatsPageIsReachableFromTheNavbar 钉住「顶栏那枚图标能进这一页」。
//
// 🔴 **这是这一票唯一的入口**：它不在侧栏里，所以那一枚是**唯一**的路。
// 而「按钮在但点了没反应」是界面层最常见的坏法之一。
func TestTheStatsPageIsReachableFromTheNavbar(t *testing.T) {
	s, tt := statsShell(t, nil)
	s.navigate("/")
	tt.Frame()

	if err := tt.Click("听歌统计"); err != nil {
		t.Fatalf("点顶栏那枚「听歌统计」：%v", err)
	}
	tt.Frame()

	if s.router.Path() != statsPath {
		t.Errorf("点顶栏那枚没进统计页，停在 %q", s.router.Path())
	}
}

// TestTheStatsEntryLightsUpOnHover 钉住「顶栏那枚听歌统计键，指针落上去要亮」。
//
// ## 它修的是什么
//
// 那一枚原来把 `.Key("nav-stats")` 链在 `iconKey(...)` **返回之后**——而
// `Element.Key` 是**事后 rekey**（`mygo/ui/context.go` 的 `rekey`：换 `e.id`
// 和 `e.st`）。悬停反馈是 `iconKey` → `flat.iconButtonAccent` / `feel` 在**构造
// 时**按当时的位置 id 去问 `Hovered()` 的（`mygo/ui/input.go:1188`：命中链按
// id 记），而 rekey 之后命中链上挂的是**新**的 keyed id——于是指针明明落在它
// 上面，`Hovered()` 拿旧 id 去对却永远对不上：**没有主色图形、没有悬停淡底、
// 提示也挂错 id**。
//
// 修法是把 Key 提前：构造**之前**给 `c.Key("nav-stats")`（Context 级 Key，给下
// 一个控件定身份），那枚按钮从落生那一刻起就是 keyed id——和 `comments_entry.go`
// 的 `c.Key(commentsButtonKey)` 同一个写法。Key 不能删：真窗口那一族 E2E 靠
// automation `act` 按 `key:"nav-stats"` 找它（`internal/e2e/stats_test.go`）。
//
// 判据和 `TestTheTopBarIconKeysTurnAccentOnHover` 同一把尺子：悬停之后那一枚的
// 框里必须出现主色像素（`hover:text-primary`），悬停之前一个都不许有。
//
// ⚠️ **断「它自己」，不再借「迷你」/「打开文件」那两枚捎带**：它们没链 `.Key`
// 所以一直是对的——拿它们绿着当「这一排都好」的证据，这一条根本抓不到它。
func TestTheStatsEntryLightsUpOnHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := navBarShell(t, app.DefaultThemePreset, dark)
			want := rgb(ink.AccentOn(BuildTheme(s.app.SettingsSnapshot(), dark)))

			btn, ok := tt.Find("听歌统计")
			if !ok || btn.W == 0 {
				t.Fatalf("顶栏上找不到「听歌统计」那一枚，文字有 %q", tt.Texts())
			}
			// 指针先离开：那时候画面上一个主色像素都不该有。
			tt.Move(600, 400)
			tt.Frame()
			if n := nearPixels(tt.Image(), btn, want); n != 0 {
				t.Errorf("没悬停的时候画面上已经有 %d 个主色像素——它平时是前景色的", n)
			}
			// 指针落到这一枚上。
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			if n := nearPixels(tt.Image(), btn, want); n < 4 {
				t.Errorf("悬停之后画面上只有 %d 个主色像素（want ≥ 4）——"+
					"图形没有变主色，悬停对它不生效（`.Key` 在 `iconKey` 返回之后才调，"+
					"rekey 换了 id，构造时按旧 id 问的 `Hovered()` 永远对不上）", n)
			}
		})
	}
}

// TestTheStatsRouteIsRegistered 钉住「这一条路由在表里」。
func TestTheStatsRouteIsRegistered(t *testing.T) {
	if got := lookup(statsPath).Match; got != statsPath {
		t.Errorf("lookup(%q) 认出的是 %q", statsPath, got)
	}
	// ⚠️ **它不进侧栏**：它的入口是顶栏那枚图标。
	if lookup(statsPath).InMenu {
		t.Errorf("统计那一页进了侧栏菜单（它的入口是顶栏那枚图标）")
	}
}

// seedStats 造一份统计：90 分钟、12 首起播、Top 歌叫「一首歌」。
//
// ⚠️ **它直接写进 App 的统计里**：这是这一层要验的东西（画出来是什么样），
// 而「怎么攒出来的」是应用层那一族的事。
//
// ⚠️ **那一天是「今天」**：这一页默认看的是「日」那一档，而它按**本地
// 日期**算——写死一个日期的话，磁贴上的数全是 0（而测试要验的正是那些数）。
func seedStats(a *app.App) {
	today := time.Now().Format("2006-01-02")
	a.SeedListeningStatsForTest(app.ListeningStats{
		Days: map[string]*app.StatsDayStat{
			// ⚠️ **按天存的那一份要带 `Tracks` 明细**：Top 那两张表是**从
			// 按天存的那一份里加起来**的（见 `statsPeriodRows`）——而明细是
			// 真实攒出来的东西，所以这份种子也得有它。
			today: {Ms: 5_400_000, Plays: 12, Completed: 8,
				Tracks: map[string]*app.StatsTrackStat{
					"mv:BV1|c1": {Ms: 5_400_000, Plays: 12, Title: "一首歌", Artist: "某个UP", ArtistMid: 42},
				}},
		},
		Tracks: map[string]*app.StatsTrackStat{
			"mv:BV1|c1": {Ms: 5_400_000, Plays: 12, Title: "一首歌", Artist: "某个UP", ArtistMid: 42},
		},
		Artists: map[int64]*app.StatsArtistStat{
			42: {Name: "某个UP", Ms: 5_400_000, Plays: 12},
		},
	})
}

// TestSwitchingTheTabChangesWhatIsShown 钉住「换档真的会换」。
//
// 🔴 **这一条验的是 `syncStats` 那一支**：换档的时候 `mainTabs` 把新下标写
// 回 `tabIndex`，而 `syncStats` 认的就是它——不认的话点「周」什么都不会变。
func TestSwitchingTheTabChangesWhatIsShown(t *testing.T) {
	s, tt := statsShell(t, func(a *app.App) {
		seedStats(a)
	})
	s.navigate(statsPath)
	tt.Frame()

	// 「日」那一档写的是「今天收听」。
	if !tt.HasText("今天收听") {
		t.Errorf("「日」那一档的标签没画出来，文字有 %q", tt.Texts())
	}
	// 切到「周」。
	if err := tt.Click("周"); err != nil {
		t.Fatalf("点「周」那一档：%v", err)
	}
	tt.Frame()

	if !tt.HasText("本周收听") {
		t.Errorf("切到「周」之后标签没换，文字有 %q", tt.Texts())
	}
	if tt.HasText("今天收听") {
		t.Errorf("切到「周」之后「日」那一档的标签还在，文字有 %q", tt.Texts())
	}
}

// TestTheStatsPageRefreshesWhenComingBack 钉住「切走再切回来会重新拿一次」。
//
// ⚠️ **这一条验的是「用户在看这一页的时候数字是最新的」**：用户可能在别的页
// 听了一首歌，切回来时那些数要跟着变。
func TestTheStatsPageRefreshesWhenComingBack(t *testing.T) {
	s, tt := statsShell(t, nil)
	// 先造一份数据，切过去。
	seedStats(s.app)
	s.navigate(statsPath)
	tt.Frame()
	if !tt.HasText("12") {
		t.Fatalf("第一次没画出来，文字有 %q", tt.Texts())
	}

	// 🔴 **切走，再造一份不同的数据，切回来**。
	//
	// ⚠️ **这一条必须换掉数据再断**：只断「那首歌还在不在」的话，
	// 「刷新了」和「没刷新」都成立（那份数据没变）——而上一版就是那么写的，
	// 于是「刷新」这件事压根没被验到（而它**真的**没发生）。
	seedStatsWithPlays(s.app, 99)
	s.navigate("/")
	tt.Frame()
	s.navigate(statsPath)
	tt.Frame()

	if !tt.HasText("99") {
		t.Errorf("切回来之后还是老的数（没刷新），文字有 %q", tt.Texts())
	}
}

// TestTheTopTableFollowsTheSelectedTab 钉住「Top 表跟那一档走」。
//
// 🔴 **这一条挡的是一个真实的自相矛盾**：磁贴看的是「今天」（30 分钟），
// 而 Top 表看的是「所有时间」（2 小时）——两个数摆在同一页上，而它们**根本
// 不是一段时间**。用户读到的是「我今天听了半小时，可最爱听的那首听了
// 两小时」——那是两件事混在一页上。
func TestTheTopTableFollowsTheSelectedTab(t *testing.T) {
	s, tt := statsShell(t, func(a *app.App) {
		seedStatsPeriods(a)
	})
	s.navigate(statsPath)
	tt.Frame()

	// 「日」那一档：只有今天那首歌。
	if !tt.HasText("今天听的那首") {
		t.Errorf("「日」那一档的 Top 歌没画出来，文字有 %q", tt.Texts())
	}
	if tt.HasText("以前听的那首") {
		t.Errorf("「日」那一档里混进了别的时段，文字有 %q", tt.Texts())
	}

	// 切到「全部」：两首都在。
	if err := tt.Click("全部"); err != nil {
		t.Fatalf("点「全部」那一档：%v", err)
	}
	tt.Frame()

	if !tt.HasText("今天听的那首") || !tt.HasText("以前听的那首") {
		t.Errorf("「全部」那一档没有两首都在，文字有 %q", tt.Texts())
	}
}

// seedStatsWithPlays 造一份「起播次数是 n」的统计。
func seedStatsWithPlays(a *app.App, n int64) {
	today := time.Now().Format("2006-01-02")
	a.SeedListeningStatsForTest(app.ListeningStats{
		Days: map[string]*app.StatsDayStat{
			today: {Ms: 5_400_000, Plays: n, Completed: 8,
				Tracks: map[string]*app.StatsTrackStat{
					"mv:BV1|c1": {Ms: 5_400_000, Plays: n, Title: "一首歌", Artist: "某个UP", ArtistMid: 42},
				}},
		},
		Tracks: map[string]*app.StatsTrackStat{
			"mv:BV1|c1": {Ms: 5_400_000, Plays: n, Title: "一首歌", Artist: "某个UP", ArtistMid: 42},
		},
		Artists: map[int64]*app.StatsArtistStat{
			42: {Name: "某个UP", Ms: 5_400_000, Plays: n},
		},
	})
}

// seedStatsPeriods 造一份跨两天的统计：今天一首，以前一天一首。
func seedStatsPeriods(a *app.App) {
	today := time.Now().Format("2006-01-02")
	old := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	a.SeedListeningStatsForTest(app.ListeningStats{
		Days: map[string]*app.StatsDayStat{
			today: {Ms: 1_800_000, Plays: 12, Completed: 8, Tracks: map[string]*app.StatsTrackStat{
				"mv:BV1|c1": {Ms: 1_800_000, Plays: 12, Title: "今天听的那首", Artist: "UP甲", ArtistMid: 1},
			}},
			old: {Ms: 7_200_000, Plays: 34, Completed: 20, Tracks: map[string]*app.StatsTrackStat{
				"mv:BV2|c2": {Ms: 7_200_000, Plays: 34, Title: "以前听的那首", Artist: "UP乙", ArtistMid: 2},
			}},
		},
		Tracks: map[string]*app.StatsTrackStat{
			"mv:BV1|c1": {Ms: 1_800_000, Plays: 12, Title: "今天听的那首", Artist: "UP甲", ArtistMid: 1},
			"mv:BV2|c2": {Ms: 7_200_000, Plays: 34, Title: "以前听的那首", Artist: "UP乙", ArtistMid: 2},
		},
		Artists: map[int64]*app.StatsArtistStat{
			1: {Name: "UP甲", Ms: 1_800_000, Plays: 12},
			2: {Name: "UP乙", Ms: 7_200_000, Plays: 34},
		},
	})
}
