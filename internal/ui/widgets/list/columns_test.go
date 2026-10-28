package list

// 这一组钉「播放量」和「投稿时间」那两列（票 #142）。
//
// 判据是**画面上有没有那个字、表头和行对不对得齐**，不是「调了哪个函数」：
// 这两列最典型的坏法是静默的——字都在、位置也在，只是列和列错开一格，或者
// 这一页明明有数据却没画。所以每一页的列定义（`Columns`）和行、表头一起断。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// columnsNow 是这一组固定的「现在」。**不取墙钟**：一列「3天前」的断言
// 跟着真实时间走的话，过一天就红，而红的时候和被测的逻辑毫无关系。
var columnsNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)

// newColumnsList 造一个带表头的列表，并装上那两列。
func newColumnsList(t *testing.T, v View, cols Columns, songs []Music) (*List, *ui.Tester) {
	t.Helper()
	l := New(newCache(t), (&recorded{}).actions())
	l.SetView(v)
	l.SetColumns(cols)
	l.SetSongs(songs)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			l.Header(c, "hdr")
			l.View(c)
		})
	}, 900, testerH)
	tt.Frame()
	return l, tt
}

// columnsSongs 是五首歌：
//
//	第 1 首	播放量 55，投稿时间「今天」
//	第 2 首	播放量 123400（→「12.3万」），投稿时间「3天前」
//	第 3~5 首	两样都是零值——那一格必须是**空的**，不是「0」
//
// 时长里故意不带 0（1:05 起），这样「画面上有没有一个字符串 0」能当判据。
func columnsSongs() []Music {
	songs := onlineSongs(5)
	for i := range songs {
		songs[i].Duration = time.Duration(65+i) * time.Second
		songs[i].Owner = "UP " + string(rune('A'+i))
	}
	songs[0].View = 55
	songs[0].PubTime = columnsNow
	songs[1].View = 123_400
	songs[1].PubTime = columnsNow.AddDate(0, 0, -3)
	return songs
}

// TestTheRowsAndTheHeaderCarryThePlayCountAndTheTime 钉住两列真的画出来，
// 而且**表头和行是同一个位置、同一个宽**。
//
// ⚠️ 量的两列都要量：播放量在标题后面、投稿时间在它后面，任何一列的宽不对，
// 右边那几列就整体错开一格——而画面上只是「表头歪了一点」。
func TestTheRowsAndTheHeaderCarryThePlayCountAndTheTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    View
	}{
		{"列表", ViewList},
		{"紧凑", ViewCompact},
	} {
		v := tc.v
		t.Run(tc.name, func(t *testing.T) {
			cols := Columns{PlayCount: true, PubTime: true, Now: func() time.Time { return columnsNow }}
			_, tt := newColumnsList(t, v, cols, columnsSongs())

			for _, want := range []string{"#", "标题", "播放量", "投稿时间", "时长"} {
				if !shows(tt, want) {
					t.Errorf("%v：表头上没有「%s」，文字有 %q", v, want, tt.Texts())
				}
			}

			// 播放量：折过的字（12.3万）和第一列的那个数都要在。
			for _, n := range []int64{55, 123_400} {
				row, ok := tt.Find(PlayCountText(n))
				if !ok {
					t.Fatalf("%v：行里没有播放量 %q，文字有 %q", v, PlayCountText(n), tt.Texts())
				}
				head, ok := tt.Find("播放量")
				if !ok {
					t.Fatalf("%v：表头上没有「播放量」，文字有 %q", v, tt.Texts())
				}
				if row.X != head.X || row.W != head.W {
					t.Errorf("%v：播放量那一格在 %.1f、宽 %.1f，表头在 %.1f、宽 %.1f"+
						"——列没有对齐", v, row.X, row.W, head.X, head.W)
				}
			}

			// 投稿时间：「今天」和「3天前」两档都要在，位置也对。
			for _, want := range []string{"今天", "3天前"} {
				row, ok := tt.Find(want)
				if !ok {
					t.Fatalf("%v：行里没有 %q，文字有 %q", v, want, tt.Texts())
				}
				head, ok := tt.Find("投稿时间")
				if !ok {
					t.Fatalf("%v：表头上没有「投稿时间」，文字有 %q", v, tt.Texts())
				}
				if row.X != head.X || row.W != head.W {
					t.Errorf("%v：投稿时间那一格在 %.1f、宽 %.1f，表头在 %.1f、宽 %.1f"+
						"——列没有对齐", v, row.X, row.W, head.X, head.W)
				}
			}

			// 没有这个数的那三行：那一格是空的，**不是「0」**。
			if shows(tt, "0") {
				t.Errorf("%v：画面上有一个「0」——没有播放量的那一行被画成了零播放：%q",
					v, tt.Texts())
			}
		})
	}
}

// TestTheColumnsStayOffWhenThePageDoesNotAskForThem 钉住零值 `Columns` 真的
// 一列都不画：本地音乐、搜索的「用户」那一档、历史记录都是这样。
//
// ⚠️ **歌上有这两个数也一样不画**：画不画是**这一页的列定义**说的，不是
// 数据碰巧带没带。反过来（有数据就画）会让本地音乐那种页上一会儿有一会儿没有
// 列，列表的宽度跟着数据跳。
func TestTheColumnsStayOffWhenThePageDoesNotAskForThem(t *testing.T) {
	_, tt := newColumnsList(t, ViewList, Columns{}, columnsSongs())

	for _, unwanted := range []string{"播放量", "投稿时间"} {
		if shows(tt, unwanted) {
			t.Errorf("没装这两列，表头上却摆着「%s」：%q", unwanted, tt.Texts())
		}
	}
	if shows(tt, PlayCountText(123_400)) {
		t.Errorf("没装播放量那一列，行里却画了 %q：%q", PlayCountText(123_400), tt.Texts())
	}
}

// TestTheTimeColumnTitleCanBeChangedPerPage 钉住收藏夹那一页的表头写的是
// 「收藏时间」——那一页画的是 `fav_time`，标题跟着它走。
func TestTheTimeColumnTitleCanBeChangedPerPage(t *testing.T) {
	cols := Columns{PlayCount: true, PubTime: true, PubTimeTitle: "收藏时间",
		Now: func() time.Time { return columnsNow }}
	_, tt := newColumnsList(t, ViewList, cols, columnsSongs())

	if !shows(tt, "收藏时间") {
		t.Errorf("表头上没有「收藏时间」：%q", tt.Texts())
	}
	if shows(tt, "投稿时间") {
		t.Errorf("这一页的时间列标题是「收藏时间」，表头上却还有「投稿时间」：%q", tt.Texts())
	}
}

// TestTheColumnsGiveWayToTheTitleWhenTheListIsNarrow 钉住窄窗口下的让位顺序。
//
// 票面加的两列是**额外交出来的宽度**，而列表的宽是窗口减侧栏和页边距来的。
// 最小窗口 900 配最宽侧栏 480 时只剩 380：全画的话标题那一列是负数，歌名和表头
// 「标题」都只剩「…」（评审量到过）。所以（列间距 16、序号 32 之后的那几个数）：
//
//	宽 520		两列都在，标题 104
//	宽 460（约 900 + 侧栏 400）	先收「投稿时间」，标题 140
//	宽 380（约 900 + 侧栏 480）	播放量也收掉，标题 140
//
// ⚠️ **判据是「标题至少留 80」**（`minTitleWidth`），不是某个具体侧栏宽：
// 侧栏是用户可以拖的，拿侧栏当判据会在中间某几档错位。
func TestTheColumnsGiveWayToTheTitleWhenTheListIsNarrow(t *testing.T) {
	l := New(newCache(t), (&recorded{}).actions())
	l.SetView(ViewList)
	l.SetColumns(Columns{PlayCount: true, PubTime: true})

	for _, c := range []struct {
		width      float32
		play, time bool
		why        string
	}{
		{0, true, true, "还没量到宽：第一帧照全画（下一帧再收）"},
		{520, true, true, "两列都放得下，标题还有 104"},
		{460, true, false, "先收投稿时间，标题回到 140"},
		{380, false, false, "再窄：播放量也收掉，标题 140"},
	} {
		l.width = c.width
		got := l.Columns()
		if got.PlayCount != c.play || got.PubTime != c.time {
			t.Errorf("宽 %.0f（%s）：画的是 播放量=%v 时间=%v，want 播放量=%v 时间=%v",
				c.width, c.why, got.PlayCount, got.PubTime, c.play, c.time)
		}
	}

	// 紧凑档用同一套判据（`MetricsOf` 取的是紧凑那一组尺寸）：宽 380 时
	// 两列合计 348，标题只剩 32——先收「投稿时间」，播放量还在。
	l.SetView(ViewCompact)
	l.width = 380
	if got := l.Columns(); !got.PlayCount || got.PubTime {
		t.Errorf("紧凑档宽 380：画的是 播放量=%v 时间=%v，want 播放量=true 时间=false",
			got.PlayCount, got.PubTime)
	}
}

// TestTheCompactPlayCountColumnFitsTheLongestFold 钉住播放量那一列装得下
// `PlayCountText` 能产出的最长那个字。
//
// ⚠️ **最长不是「9999.9万」**：一亿以下按万折，`99999999/1e4` 再 `FormatFloat`
// 一位小数会进位成「10000.0万」（font 12 下约 60 宽）。紧凑档原来只有 56 宽，
// 最后一位被裁掉，读数变成「10000....」——评审量到过。
func TestTheCompactPlayCountColumnFitsTheLongestFold(t *testing.T) {
	longest := PlayCountText(99_999_999)
	if longest != "10000.0万" {
		t.Fatalf("99,999,999 折出来是 %q，want 10000.0万（这条测试量的就是它）", longest)
	}

	var measured float32
	tt := ui.NewTester(func(c *ui.Context) {
		measured, _ = c.MeasureText(0, ui.Span{Text: longest, Size: scale.Font(c, 12)})
		ui.Text(c, longest).FontSize(scale.Font(c, 12))
	}, 300, 60)
	tt.Frame()

	if measured > compactPlayCountWidth {
		t.Errorf("「%s」宽 %.1f，紧凑档那一列只有 %d——最后一位会被裁掉",
			longest, measured, compactPlayCountWidth)
	}
	if measured > playCountWidth {
		t.Errorf("「%s」宽 %.1f，普通档那一列只有 %d", longest, measured, playCountWidth)
	}
}

// TestPubTimeTextFoldsLikeTheTicket 钉住那一列的显示格式（票 #142 要求把格式
// 定下来，见 `pubtime.go` 的文件头）。
func TestPubTimeTextFoldsLikeTheTicket(t *testing.T) {
	now := columnsNow
	for _, c := range []struct {
		name string
		t    time.Time
		want string
	}{
		{"零值", time.Time{}, ""},
		{"今天", now.Add(-2 * time.Hour), "今天"},
		{"未来也算今天", now.Add(2 * time.Hour), "今天"},
		{"昨天", now.AddDate(0, 0, -1), "昨天"},
		{"几天前", now.AddDate(0, 0, -3), "3天前"},
		{"一周前换月-日", now.AddDate(0, 0, -10), "10-11"},
		{"同一年", time.Date(2026, 1, 5, 9, 0, 0, 0, time.Local), "01-05"},
		{"跨年", time.Date(2025, 12, 31, 9, 0, 0, 0, time.Local), "2025-12-31"},
	} {
		if got := PubTimeText(c.t, now); got != c.want {
			t.Errorf("%s：%v 折出来是 %q，want %q", c.name, c.t, got, c.want)
		}
	}
}

// TestPubTimeTextCountsCalendarDays 钉住「按日历日算，不是按 24 小时」。
//
// 23:59 和第二天 00:01 只隔两分钟，但它们是两天——按 `now.Sub(t)` 除 24 小时
// 算的话，跨越午夜的那一条会显示「今天」，而用户明明记得是昨天看的/发的。
func TestPubTimeTextCountsCalendarDays(t *testing.T) {
	late := time.Date(2026, 10, 20, 23, 59, 0, 0, time.Local)
	early := time.Date(2026, 10, 21, 0, 1, 0, 0, time.Local)
	if got := PubTimeText(late, early); got != "昨天" {
		t.Errorf("20 日 23:59 对 21 日 00:01 折出来是 %q，want 昨天", got)
	}
}

// TestTheRankColumnMakesTheTopThreeBiggerThanTheRest 钉榜单页的名次列：前三名的序号
// 比第四名大（主色加粗放大），不开 Rank 的列表所有序号一样大。
func TestTheRankColumnMakesTheTopThreeBiggerThanTheRest(t *testing.T) {
	size := func(cols Columns) (first, fourth float32) {
		_, tt := newColumnsList(t, ViewList, cols, columnsSongs())
		a, ok1 := tt.Find("1")
		d, ok2 := tt.Find("4")
		if !ok1 || !ok2 {
			t.Fatalf("找不到序号 1 / 4：%q", tt.Texts())
		}
		return a.H, d.H
	}
	if f, d := size(Columns{Rank: true}); f <= d {
		t.Errorf("榜单页第一名序号高 %.1f，应该比第四名（%.1f）大", f, d)
	}
	if f, d := size(Columns{}); f != d {
		t.Errorf("普通列表第一名序号高 %.1f，应该和第四名（%.1f）一样", f, d)
	}
}
