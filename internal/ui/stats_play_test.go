package ui

// 「本周单曲循环」点一首歌。
//
// 那些行画的是**统计快照**（歌名 + 听了多久），不是曲目——行底下没有
// `app.Item`。按下去要把聚合键（`mv:` / `audio:` / `local:`）**拆回**一项，
// 走和其他列表同一条路：`list.AppActions` 的 `PlayNow`（`EnqueueAndPlayNow`）。
//
// 拆不回来 / 放不了的那几档要有一句提示，不许「按了没反应」。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	mygoui "github.com/egoist/mygo/ui"
)

// statsPlayShell 造一个窗口够高（单曲循环那一栏在页尾）、停在统计页的外壳。
func statsPlayShell(t *testing.T, seed func(*app.App)) (*testShell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, at(1200, 1600), mounted(func(_ *testing.T, a *app.App) {
		if err := a.MountListeningStats(app.StatsPath(t.TempDir())); err != nil {
			t.Fatal(err)
		}
		if seed != nil {
			seed(a)
		}
	}))
	b.s.navigate(statsPath)
	b.tt.Frame()
	return b, b.tt
}

// statsPlaySeed 是三行的种子：一首本地文件（真的放得了）、一首在线 mv、
// 一条认不出身份的坏记录。
//
// ⚠️ **本地那一行用真实的 FLAC 素材**：这一组里有「点下去真的放起来」
// 那一条——伪造路径会走「打不开」那一支，和要钉的不是一回事。
func statsPlaySeed(t *testing.T) func(*app.App) {
	t.Helper()
	local := flacPath(t)
	today := time.Now().Format("2006-01-02")
	return func(a *app.App) {
		a.SeedListeningStatsForTest(app.ListeningStats{
			Days: map[string]*app.StatsDayStat{
				today: {Ms: 3_600_000, Plays: 7, Tracks: map[string]*app.StatsTrackStat{
					"local:" + local: {Ms: 2_000_000, Plays: 4, Title: "本地的一首", Local: true},
					"mv:BVstat|c9":   {Ms: 1_000_000, Plays: 2, Title: "在线的一首", Artist: "某 UP", ArtistMid: 1001},
					"bogus:key":      {Ms: 600_000, Plays: 1, Title: "坏掉的那条"},
				}},
			},
			Tracks: map[string]*app.StatsTrackStat{
				"local:" + local: {Ms: 2_000_000, Plays: 4, Title: "本地的一首", Local: true},
				"mv:BVstat|c9":   {Ms: 1_000_000, Plays: 2, Title: "在线的一首", Artist: "某 UP", ArtistMid: 1001},
				"bogus:key":      {Ms: 600_000, Plays: 1, Title: "坏掉的那条"},
			},
		})
	}
}

// statsRowPlay 悬停到那一行上、再按序号位置那枚「播放 <歌名>」。
//
// ⚠️ **先悬停**：那枚播放钮只在指针压在行上时才换上来（序号变成播放键，
// 见 `pud.TrackRow`），直接 `Click` 会找不到它。
func statsRowPlay(t *testing.T, tt *mygoui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("找不到歌名 %q 那一行，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("播放 " + title); err != nil {
		t.Fatalf("悬停之后按不到「播放 %s」：%v，文字有 %q", title, err, tt.Texts())
	}
}

// TestClickingAStatsLoopRowPlaysThatSong 钉住验收第一条的正路：
// 点本地那一行 → 走 `PlayNow`（`EnqueueAndPlayNow`）→ 真的放起来。
//
// ⚠️ **断言的是「进队列并起播的那一项」**：`EnqueueAndPlayNow` 是「追加到
// 队尾并立即播」——所以队列里必须多出这一首、当前项必须指着它、而它必须
// 在响。`Snapshot().Title` 用的是播放器报的名字（本地文件按文件名，不是
// 快照里的歌名），所以队列那一项才是「点的是谁」的判据。
func TestClickingAStatsLoopRowPlaysThatSong(t *testing.T) {
	b, tt := statsPlayShell(t, statsPlaySeed(t))
	statsRowPlay(t, tt, "本地的一首")
	waitFor(t, "本地那首进队列并开始播", func() bool {
		if !b.s.app.Snapshot().Playing {
			return false
		}
		cur, ok := b.s.app.Queue().Current()
		return ok && cur.IsLocal() && cur.Path == flacPath(t) &&
			cur.Title == "本地的一首"
	})
}

// TestClickingAnOnlineStatsLoopRowPlaysItThroughTheQueue 钉住 mv 那一档走的是
// 队列那条路（`EnqueueAndPlayNow`：追加到队尾并立即播），不是「换队列」也不是
// 没反应——就算起播失败，**队列里必须有它**，而且界面上要有一句话。
func TestClickingAnOnlineStatsLoopRowPlaysItThroughTheQueue(t *testing.T) {
	b, tt := statsPlayShell(t, statsPlaySeed(t))
	statsRowPlay(t, tt, "在线的一首")
	waitFor(t, "在线那首进队列", func() bool {
		for _, it := range b.s.app.Queue().Items() {
			if it.Bvid == "BVstat" && it.Cid == "c9" && it.Title == "在线的一首" {
				return true
			}
		}
		return false
	})
}

// TestClickingABrokenStatsLoopRowSaysItCannotPlay 钉住「认不出身份的那一行按
// 下去要有一句话」：坏记录不能塞进队列（那就是一条永远放不了的死项），
// 也不能按了没反应。
func TestClickingABrokenStatsLoopRowSaysItCannotPlay(t *testing.T) {
	b, tt := statsPlayShell(t, statsPlaySeed(t))
	statsRowPlay(t, tt, "坏掉的那条")
	waitFrames(t, tt, "提示放不了", func() bool {
		return tt.HasText("放不了")
	})
	for _, it := range b.s.app.Queue().Items() {
		if it.Title == "坏掉的那条" {
			t.Errorf("认不出身份的那一条进了队列（%+v）", it)
		}
	}
}

// TestTheStatsArtistMinutesStayClearOfTheScrollbar 钉住验收第二条：
// 「最常听的歌手」行尾那列「N 分钟」不许压在 overlay 滚动条底下。
//
// 滚动条画在滚动容器右缘往里 16 像素那条竖带上（`scrollbarGutter`：
// thumb 10 + 两侧各 3）。行原来左右零内边距，分钟数
// 右对齐到容器右缘——指针一压上去，最后那几个像素就被盖住。
//
// 判据是**分钟数字形离容器右缘至少一条热区**：用窗口几何算容器右缘
// （侧栏 + 居中内容 + 页边距），不拿另一行当参照——两行一起错的时候
// 「对不齐」就变成「对齐了」。
func TestTheStatsArtistMinutesStayClearOfTheScrollbar(t *testing.T) {
	_, tt := statsPlayShell(t, seedShotStats)
	r, ok := tt.Find("179 分钟")
	if !ok {
		t.Fatalf("找不到分钟数那一格，文字有 %q", tt.Texts())
	}
	// 内容区在窗口里居中：右缘 = 窗口宽 − 页边距（`pudPage` 的布局）。
	edge := float32(1200) - space.PageHorizontalForWindow(1200)
	if got, limit := r.X+r.W, edge-scrollbarGutter; got > limit {
		t.Errorf("分钟数右缘 x=%.1f，压在滚动条带里（容器右缘 %.1f，热区 %d）——"+
			"指针一悬停它就看不见", got, edge, scrollbarGutter)
	}
}

// TestTheStatsLoopRowLightsUpWhileItsSongPlays 钉住「点了播放之后那一行要像
// 别的列表一样亮起来」：行底下没有 `app.Item`，正在放的判据是把聚合键
// 拆回一项再比 `Identity`——不标的话按了播放那一行什么变化都没有，
// 像没按上。
//
// ⚠️ **判据是无障碍标签**（`hover.PlayingLabel`：「<歌名> 正在播放」）：
// eq-bars 那几个像素读屏软件看不见，那一行能拿的标记就是它。
func TestTheStatsLoopRowLightsUpWhileItsSongPlays(t *testing.T) {
	b, tt := statsPlayShell(t, statsPlaySeed(t))
	statsRowPlay(t, tt, "本地的一首")
	waitFor(t, "本地那首放起来", func() bool { return b.s.app.Snapshot().Playing })
	// ⚠️ **先把指针挪开**：悬停时序号位置是「播放 X」那枚键而不是
	// eq-bars——eq-bars（和它的「正在播放」标签）只在**不悬停**时画。
	tt.Move(2, 2)
	tt.Frame()
	waitFrames(t, tt, "那一行亮起来", func() bool {
		return tt.HasText("本地的一首 " + hover.PlayingMark)
	})
}

// TestTheBrokenStatsLoopRowNeverLooksPlaying 钉住另一半：认不出身份的那一行
// **永远不该**被标成正在放——拆不出来就不是「在放」，亮起它等于说「正在放
// 的是它」。
func TestTheBrokenStatsLoopRowNeverLooksPlaying(t *testing.T) {
	b, tt := statsPlayShell(t, statsPlaySeed(t))
	statsRowPlay(t, tt, "本地的一首")
	waitFor(t, "本地那首放起来", func() bool { return b.s.app.Snapshot().Playing })
	tt.Move(2, 2)
	tt.Frame()
	if tt.HasText("坏掉的那条 " + hover.PlayingMark) {
		t.Error("认不出身份的那一行被标成了正在放")
	}
}
