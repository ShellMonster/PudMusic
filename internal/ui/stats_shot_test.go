package ui

// 听歌统计那一页的出图。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderStatsShots
//
// # 出四张：空状态 / 有数据 × 浅色 / 深色
//
// ⚠️ **空状态也要出**：那一块是这一页**自己**画的（见 stats_view.go 的
// `viewStatsEmpty`），而它有没有说清「怎么才能不空」只有图上看得出。
//
// ⚠️ **深浅两套都要出**：这一页的卡片底色和分隔线都跟着主题走，而浅色下
// 那一栏「时长」那一列的数字和正文色很接近——不出两套看不出它糊没糊。
//
// ⚠️ **出图本身不是判据**：同一份代码两次出图有几十张会不一样（字体 hinting、
// 抗锯齿的随机相位）。判据是 `stats_page_test.go` 里那几条（它们量的是文字），
// 出图是给人看的。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderStatsShots 把听歌统计页存成 PNG。
func TestRenderStatsShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		theme := "浅色"
		if dark {
			theme = "深色"
		}
		t.Run(theme, func(t *testing.T) {
			// 01 **空状态**：还没听过歌。
			empty := statsShotShell(t, dark, nil)
			save(t, dir, "01-统计页-空状态-"+theme+".png", empty)

			// 02 **有数据**：四枚磁贴 + 柱状图 + Top 歌 + Top 艺人。
			full := statsShotShell(t, dark, seedShotStats)
			save(t, dir, "02-统计页-有数据-"+theme+".png", full)
			mode := "light"
			if dark {
				mode = "dark"
			}
			save(t, dir, mode+"-stats.png", full)
		})
	}
}

// statsShotShell 造一个停在统计页的外壳。
func statsShotShell(t *testing.T, dark bool, seed func(*app.App)) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		if err := a.MountListeningStats(app.StatsPath(t.TempDir())); err != nil {
			t.Fatal(err)
		}
		if seed != nil {
			seed(a)
		}
	}))
	b.s.navigate(statsPath)
	b.tt.Frame()
	return b.tt
}

// seedShotStats 造一份有样子的统计。
//
// ⚠️ **它跨了好几天**：柱状图要有多根柱子才看得出形状，而「连续天数」也要
// 有好几天才有话说。
func seedShotStats(a *app.App) {
	now := time.Now()
	days := map[string]*app.StatsDayStat{}
	// ⚠️ **每一天都要带 `Tracks` 明细**：Top 那两张表是**从按天存的那一份里
	// 加起来**的（见 `statsPeriodRows`）——而明细是真实攒出来的东西，
	// 所以这份种子也得有它，否则那两张表是空的。
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		ms := int64(1_800_000 + i*420_000)
		// ⚠️ **各首之和 = 那一天的总时长，各首次数之和 = 那一天的起播次数**。
		//
		// 这是**真的攒出来的那一份**遵守的不变量（见
		// `TestTheDayTotalEqualsTheSumOfItsTracks`）——而这份种子是手写的，
		// 所以它也得守住：否则画面上「今天收听 30 分钟」和下面那四行加起来
		// 「34 分钟」摆在同一页上，而用户看到的���是「加起来比总数还多」。
		t1 := ms / 2
		t2 := ms / 3
		t3 := ms / 6
		t4 := ms - t1 - t2 - t3
		days[d] = &app.StatsDayStat{Ms: ms, Plays: 12 + int64(i), Completed: 8 + int64(i),
			Tracks: map[string]*app.StatsTrackStat{
				"mv:BV1|c1":             {Ms: t1, Plays: 6, Title: "夜曲", Artist: "林俊杰", ArtistMid: 1001},
				"mv:BV2|c2":             {Ms: t2, Plays: 4, Title: "晴天", Artist: "周杰伦", ArtistMid: 1002},
				"mv:BV3|c3":             {Ms: t3, Plays: 2, Title: "起风了", Artist: "买辣椒也用券", ArtistMid: 1003},
				"local:/music/song.mp3": {Ms: t4, Plays: 1, Title: "本地的一首歌", Local: true},
			}}
	}
	a.SeedListeningStatsForTest(app.ListeningStats{
		Days: days,
		Tracks: map[string]*app.StatsTrackStat{
			"mv:BV1|c1":             {Ms: 7_200_000, Plays: 34, Title: "夜曲", Artist: "林俊杰", ArtistMid: 1001, FirstHeard: time.Now().AddDate(0, 0, -2).Format("2006-01-02")},
			"mv:BV2|c2":             {Ms: 5_400_000, Plays: 26, Title: "晴天", Artist: "周杰伦", ArtistMid: 1002},
			"mv:BV3|c3":             {Ms: 3_100_000, Plays: 19, Title: "起风了", Artist: "买辣椒也用券", ArtistMid: 1003},
			"local:/music/song.mp3": {Ms: 2_400_000, Plays: 14, Title: "本地的一首歌", Local: true},
		},
		Artists: map[int64]*app.StatsArtistStat{
			1001: {Name: "林俊杰", Ms: 7_200_000, Plays: 34},
			1002: {Name: "周杰伦", Ms: 5_400_000, Plays: 26},
			1003: {Name: "买辣椒也用券", Ms: 3_100_000, Plays: 19},
		},
	})
}
