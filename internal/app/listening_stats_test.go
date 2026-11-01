// 听歌统计这一摊的业务层测试。
//
// 这一组钉的是**「什么算听了一分钟」**——也就是统计的全部判断都在这儿：
// 位置增量怎么算、拖进度条怎么排除、暂停怎么排除、什么时候算「听完」。
//
// # 为什么这里不真的放歌
//
// 这一层把「在放哪一首、放到哪儿了、过了多久」当作**输入**摆进去。
//
// ⚠️ **真的从播放链路走到这里的那一条**（真播放器 + 真位置 + 真墙钟）由
// `internal/e2e/stats_test.go` 那一族在**真窗口**上验——而它验的是「顶栏
// 那一枚图标能进这一页」，不是累加逻辑。
//
// ⇒ 所以累加逻辑的判据**全在这一层**：每一拍摆进去的输入都是「位置 + 墙钟 +
// 身份」，而生产那一路（`statsTick`）把它��装配好。**两者之间的接线由
// `TestTheCompletedCountNeedsTheTrackDuration` 和
// `TestTheTrackerRecordsTheTrackMetadataInTheDayDetail` 挡住**——那两条走的是
// 「时长和快照是外面给进来的」那一条路。
//
// ⚠️ **时间全是摆进去的**：这一层没有一个 `time.Now()`——墙钟由调用方给。
// 而这不只是为了测试方便：统计的每一个判断都建立在「这一拍和上一拍差在哪」
// 上，墙钟从外面进来才有那一拍。

package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// ─────────────────────────────────────────────────────────────────────────────
// 输入的形状
// ─────────────────────────────────────────────────────────────────────────────

// withPos 填上位置和时长（并且说这一拍读到了位置）。
func (in statsTickIn) withPos(pos, dur time.Duration) statsTickIn {
	in.Pos = pos
	in.Dur = dur
	in.FreshPos = true
	return in
}

// paused 说「这一拍没有在放，而且位置没读到」。
func (in statsTickIn) paused() statsTickIn {
	in.FreshPos = false
	return in
}

// at 填上墙钟时刻。
func (in statsTickIn) at(t time.Time) statsTickIn {
	in.At = t
	return in
}

// finished 说「这一首真的放完了」。
func (in statsTickIn) finished() statsTickIn {
	in.Finished = true
	return in
}

// onPlayer 说「这一拍的那台播放器是 p」。
func (in statsTickIn) onPlayer(p *audio.Player) statsTickIn {
	in.Player = p
	return in
}

// newStart 说「又装上了一台新的播放器，而且位置回到 0」。
func (in statsTickIn) newStart() statsTickIn {
	in.Player = &audio.Player{}
	return in
}

// mvIn 是「一首 B 站的 mv」的输入。
func mvIn(bvid, cid, title, artist string, mid int64) statsTickIn {
	return statsTickIn{Kind: "mv", Bvid: bvid, Cid: cid, Title: title, Artist: artist, ArtistMid: mid}
}

// localIn 是「一个本地文件」的输入。
func localIn(path, title string) statsTickIn {
	return statsTickIn{Kind: "local", Path: path, Title: title}
}

// newStatsFixture 开一个统计器。
func newStatsFixture() *listeningTracker { return newListeningTracker() }

// ─────────────────────────────────────────────────────────────────────────────
// 一、时长怎么算
// ─────────────────────────────────────────────────────────────────────────────

// TestListeningTimeCountsThePositionDelta 钉住最基本的那个：在放的时候，
// 过了多少时间就记多少。
func TestListeningTimeCountsThePositionDelta(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(30 * time.Second)))

	got := tr.Snapshot()
	if got.DayMs("2026-11-01") != 30_000 {
		t.Errorf("当天时长 = %d 毫秒，want 30000", got.DayMs("2026-11-01"))
	}
}

// TestListeningTimeDoesNotCountWhilePaused 钉住「暂停不算时间」。
//
// ⚠️ **判据是「位置没动」**：暂停时位置不动，于是增量是 0。
// 写这一条是因为另一种写法（按 Playing 判）会在「暂停后拖进度条」那一种
// 上算错。
func TestListeningTimeDoesNotCountWhilePaused(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
	// 暂停：位置不动，墙钟走了 10 分钟。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(10 * time.Minute)).paused())
	// 恢复：位置继续往前走。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(60*time.Second, 3*time.Minute).
		at(base.Add(10*time.Minute + 30*time.Second)))

	got := tr.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 60_000 {
		t.Errorf("当天时长 = %d 毫秒，want 60000（暂停那 10 分钟不算）", v)
	}
}

// TestAPausedTickDoesNotMoveTheBaseline 钉住「暂停的那一拍不挪位置基线」。
//
// 🔴 **这是「暂停不算时间」能成立的前提**：暂停时 tick 读不到位置（读出来
// 是零值），而如果那一拍把基线挪到 0，恢复之后的第一拍就会看到
// 「位置从 0 跳到 30 秒」——那一下子会被算成听了 30 秒。
func TestAPausedTickDoesNotMoveTheBaseline(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).at(base))
	// 暂停：位置读不到（FreshPos 假）。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).paused().at(base.Add(10 * time.Minute)))
	// 恢复：位置还是 30 秒。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(10*time.Minute + time.Second)))

	got := tr.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 0 {
		t.Errorf("当天时长 = %d 毫秒，want 0（暂停前后位置没动过）", v)
	}
}

// TestDraggingTheProgressBarDoesNotInflateTheTime 钉住「拖进度条不虚增时长」。
//
// 🔴 **这是最容易做错的一条**：用户把进度从 0 拖到 3 分钟，播放器的位置
// 一下子跳了 180 秒——而用户实际只听了 5 秒。按位置增量算就是 3 分钟。
func TestDraggingTheProgressBarDoesNotInflateTheTime(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 5*time.Minute).at(base))
	// 拖到 3 分钟：墙钟只过了 5 秒。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(3*time.Minute, 5*time.Minute).
		at(base.Add(5 * time.Second)))

	got := tr.Snapshot()
	// 🔴 **钳制之后是 5 秒 × 2.25 = 11.25 秒**（上限比最大倍率宽一点，
	// 理由见 `statsMaxRateNum` 那段）。
	//
	// ⚠️ **这一条要断的是「不虚增」**，不是精确值：拖了 3 分钟而只记 11 秒
	// ——那个「3 分钟」才是要挡住的东西。
	if v := got.DayMs("2026-11-01"); v > 12_000 {
		t.Errorf("拖进度条之后时长 = %d 毫秒，want ≤ 12000（墙钟 5 秒 × 2.25）", v)
	}
}

// TestPlayingFasterThanRealTimeStillCounts 钉住「倍速播放不被钳制吃掉」。
//
// ⚠️ 2 倍速时，30 秒的墙钟对应 60 秒的播放位置——位置增量是墙钟的两倍。
// 那一条钳制（2 倍）刚好放过它；钳到 1.5 就会把 2 倍速的时间砍掉三分之一。
func TestPlayingFasterThanRealTimeStillCounts(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 10*time.Minute).at(base))
	// 2 倍速：墙钟 30 秒，位置走了 60 秒。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(60*time.Second, 10*time.Minute).
		at(base.Add(30 * time.Second)))

	got := tr.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 60_000 {
		t.Errorf("2 倍速下时长 = %d 毫秒，want 60000（位置增量 60 秒）", v)
	}
}

// TestTheLastSecondOfATrackIsNotLost 钉住「歌放完那一刻，时长不丢」。
//
// 🔴 **放完的那一拍，上层把位置归零**（见 app.go 的 `setPositionLocked(0)`）——
// 而**播放器自己读到的位置是全长**。于是这一拍的位置增量是「全长 - 上一次
// 读到的位置」，那正是最后那一段。
func TestTheLastSecondOfATrackIsNotLost(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 30*time.Second).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(25*time.Second, 30*time.Second).
		at(base.Add(25 * time.Second)))
	// 放完：这一拍读到的是全长（30 秒）。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 30*time.Second).
		at(base.Add(30 * time.Second)).finished())

	got := tr.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 30_000 {
		t.Errorf("放完那一刻时长 = %d 毫秒，want 30000（整首）", v)
	}
}

// TestSwitchingTracksKeepsWhatWasAlreadyListened 钉住「换歌不丢已听的时长」。
func TestSwitchingTracksKeepsWhatWasAlreadyListened(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "第一首", "UP甲", 1).withPos(0, 3*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "第一首", "UP甲", 1).withPos(40*time.Second, 3*time.Minute).
		at(base.Add(40 * time.Second)))
	// 换歌：第一首结算，第二首从头开始。
	tr.Tick(mvIn("2", "c2", "第二首", "UP乙", 2).withPos(0, 3*time.Minute).at(base.Add(41 * time.Second)))
	tr.Tick(mvIn("2", "c2", "第二首", "UP乙", 2).withPos(20*time.Second, 3*time.Minute).
		at(base.Add(61 * time.Second)))

	got := tr.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 60_000 {
		t.Errorf("当天时长 = %d 毫秒，want 60000（40 + 20）", v)
	}
	if v := got.TrackMs("mv:1|c1"); v != 40_000 {
		t.Errorf("第一首时长 = %d 毫秒，want 40000", v)
	}
	if v := got.TrackMs("mv:2|c2"); v != 20_000 {
		t.Errorf("第二首时长 = %d 毫秒，want 20000", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 二、起播次数与「听完」
// ─────────────────────────────────────────────────────────────────────────────

// TestEachStartCountsOnce 钉住「起播次数」。
//
// ⚠️ **换歌算一次起播，同一首暂停后恢复不算**：那不是「又听了一遍」，
// 那还是同一遍。
func TestEachStartCountsOnce(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 3*time.Minute).at(base))
	// 暂停。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(10*time.Second, 3*time.Minute).
		at(base.Add(10 * time.Second)).paused())
	// 恢复。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(10*time.Second, 3*time.Minute).
		at(base.Add(20 * time.Second)))
	// 换歌：第二次起播。
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, 3*time.Minute).at(base.Add(21 * time.Second)))

	got := tr.Snapshot()
	if v := got.DayPlays("2026-11-01"); v != 2 {
		t.Errorf("当天起播次数 = %d，want 2", v)
	}
}

// TestReplayingTheSameTrackCountsAnotherStart 钉住「同一首重听算又一次起播」。
//
// 🔴 **这是「单曲循环」那一档需要的**：身份不变，而身份不变就认不出
// 「又听了一遍」——于是那一栏会一直显示 1。
func TestReplayingTheSameTrackCountsAnotherStart(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	p1 := &audio.Player{}
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).at(base).onPlayer(p1))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(10*time.Second, time.Minute).
		at(base.Add(10 * time.Second)).onPlayer(p1))
	// 同一首重听：装上了新的一台播放器，位置回到 0。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).
		at(base.Add(20 * time.Second)).newStart())

	got := tr.Snapshot()
	if v := got.DayPlays("2026-11-01"); v != 2 {
		t.Errorf("当天起播次数 = %d，want 2（重听了一遍）", v)
	}
}

// TestAShortTrackNeverCountsAsCompleted 钉住「短于 30 秒的歌不算听完」。
//
// 🔴 **这是 Last.fm 的口径**（一首歌要长于 30 秒才算一次 scrobble）。
// 没有这条闸，一个 10 秒的短歌反复播就会刷出一堆假的「听完」。
func TestAShortTrackNeverCountsAsCompleted(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 一首 20 秒的歌，完整放完。
	tr.Tick(mvIn("s", "c1", "短歌", "UP甲", 1).withPos(0, 20*time.Second).at(base))
	tr.Tick(mvIn("s", "c1", "短歌", "UP甲", 1).withPos(20*time.Second, 20*time.Second).
		at(base.Add(20 * time.Second)).finished())

	got := tr.Snapshot()
	if v := got.DayCompleted("2026-11-01"); v != 0 {
		t.Errorf("20 秒的歌算成了听完 %d 次，want 0", v)
	}
}

// TestHalfTheTrackCountsAsCompleted 钉住「听完一半就算听完」。
//
// ⚠️ **判据是「这一遍累计净时长」，不是「当前播放位置」**：暂停着停在
// 2 分钟处、实际只听了 30 秒，那不算听完一半。
func TestHalfTheTrackCountsAsCompleted(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 一首 4 分钟的歌。播到一半（2 分钟）就切走。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 4*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(2*time.Minute, 4*time.Minute).
		at(base.Add(2 * time.Minute)))
	// 切走：结算。
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, 4*time.Minute).
		at(base.Add(2*time.Minute + time.Second)))

	got := tr.Snapshot()
	if v := got.DayCompleted("2026-11-01"); v != 1 {
		t.Errorf("听完一半算成了 %d 次，want 1", v)
	}
}

// TestFourMinutesCountsEvenOnALongTrack 钉住「长歌听满 4 分钟就算听完」。
//
// 🔴 Last.fm 的口径是「一半 **或** 4 分钟，取先到的」——一首 20 分钟的歌，
// 听 4 分钟就算一次 scrobble。
func TestFourMinutesCountsEvenOnALongTrack(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 一首 20 分钟的歌，听了 4 分钟。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 20*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(4*time.Minute, 20*time.Minute).
		at(base.Add(4 * time.Minute)))
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, 20*time.Minute).
		at(base.Add(4*time.Minute + time.Second)))

	got := tr.Snapshot()
	if v := got.DayCompleted("2026-11-01"); v != 1 {
		t.Errorf("听满 4 分钟算成了 %d 次，want 1", v)
	}
}

// TestALittleWayThroughDoesNotCount 钉住「没到一半就不算听完」。
func TestALittleWayThroughDoesNotCount(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 一首 4 分钟的歌，只听了 30 秒。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 4*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(30*time.Second, 4*time.Minute).
		at(base.Add(30 * time.Second)))
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, 4*time.Minute).
		at(base.Add(31 * time.Second)))

	got := tr.Snapshot()
	if v := got.DayCompleted("2026-11-01"); v != 0 {
		t.Errorf("听了 30 秒算成了听完 %d 次，want 0", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三、连续天数
// ─────────────────────────────────────────────────────────────────────────────

// TestStreakCountsConsecutiveDays 钉住「连续天数」。
func TestStreakCountsConsecutiveDays(t *testing.T) {
	tr := newStatsFixture()
	d := func(n int) time.Time {
		return time.Date(2026, 11, n, 12, 0, 0, 0, time.Local)
	}
	for n := 1; n <= 3; n++ {
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).at(d(n)))
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(time.Minute, time.Minute).
			at(d(n).Add(time.Minute)))
	}
	// 换歌，结算前几天的时长（每天都要落进 map）。
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, time.Minute).at(d(4)))

	got := tr.Snapshot()
	if v := got.Streak(d(3)); v != 3 {
		t.Errorf("连续天数 = %d，want 3", v)
	}
}

// TestStreakBreaksOnAGap 钉住「中间断一天就断签」。
func TestStreakBreaksOnAGap(t *testing.T) {
	tr := newStatsFixture()
	d := func(day int) time.Time {
		return time.Date(2026, 11, day, 12, 0, 0, 0, time.Local)
	}
	// 1 号、2 号有，3 号没有，4 号有。
	for _, day := range []int{1, 2, 4} {
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).at(d(day)))
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(time.Minute, time.Minute).
			at(d(day).Add(time.Minute)))
	}
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, time.Minute).at(d(5)))

	got := tr.Snapshot()
	// 从 4 号往回数：4 号有，3 号没有 ⇒ 连续 1 天。
	if v := got.Streak(d(4)); v != 1 {
		t.Errorf("连续天数 = %d，want 1", v)
	}
}

// TestStreakFromYesterdayCounts 钉住「今天还没听，昨天往前数」。
//
// ⚠️ **这是连续天数的常见口径**：今天还没开始听不算断签——昨天的连续
// 记录还算数（用户是「今天还没听」而不是「昨天听了一天然后断了」）。
//
// ⚠️ **「今天还没听」的判据是「今天没有净时长」**，不是「今天没有起播」——
// 装上一首歌又立刻切走，那不算「今天听了歌」。
func TestStreakFromYesterdayCounts(t *testing.T) {
	tr := newStatsFixture()
	d := func(day int) time.Time {
		return time.Date(2026, 11, day, 12, 0, 0, 0, time.Local)
	}
	for _, day := range []int{1, 2, 3} {
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).at(d(day)))
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(time.Minute, time.Minute).
			at(d(day).Add(time.Minute)))
	}
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, time.Minute).at(d(4)))

	got := tr.Snapshot()
	// 今天（4 号）还没有时长，但 1~3 号连着 ⇒ 连续 3 天。
	if v := got.Streak(d(4)); v != 3 {
		t.Errorf("连续天数 = %d，want 3（今天还没听，昨天往前数）", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四、聚合：Top 歌 / Top 艺人
// ─────────────────────────────────────────────────────────────────────────────

// TestTopSongsAreOrderedByListeningTime 钉住「Top 歌按时长排，不按次数排」。
//
// 🔴 **这是「按累计净时长」这条口径的意义**：一首听了 30 分钟的歌和一首
// 听了 3 秒的歌，起播次数可能更多，但后者不该排在前面。
func TestTopSongsAreOrderedByListeningTime(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 「短的那首」反复起播 3 次，每次 1 秒。
	for i := 0; i < 3; i++ {
		a := base.Add(time.Duration(i) * 5 * time.Second)
		tr.Tick(mvIn("short", "c1", "短的那首", "UP甲", 1).withPos(0, time.Minute).at(a))
		tr.Tick(mvIn("short", "c1", "短的那首", "UP甲", 1).withPos(time.Second, time.Minute).
			at(a.Add(time.Second)))
		if i < 2 {
			tr.Tick(mvIn("short", "c1", "短的那首", "UP甲", 1).withPos(0, time.Minute).
				at(a.Add(2 * time.Second)).newStart())
		}
	}
	// 「长的那首」一次，听了 30 秒。
	tr.Tick(mvIn("long", "c2", "长的那首", "UP乙", 2).withPos(0, 3*time.Minute).at(base.Add(10 * time.Second)))
	tr.Tick(mvIn("long", "c2", "长的那首", "UP乙", 2).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(40 * time.Second)))

	got := tr.Snapshot()
	top := got.TopTracks(10)
	if len(top) == 0 {
		t.Fatalf("Top 歌是空的")
	}
	if top[0].Key != "mv:long|c2" {
		t.Errorf("Top 第一是 %q，want mv:long|c2（听得更久）", top[0].Key)
	}
}

// TestLocalFilesCountAsSongsButNotAsArtists 钉住「本地文件算歌，不算艺人」。
//
// ⚠️ **本地文件没有 UP 主**（见 app.go 的 `a.artist = ""`）——所以它进
// 「Top 歌」，不进「Top 艺人」。
func TestLocalFilesCountAsSongsButNotAsArtists(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(localIn("/music/a.mp3", "本地歌").withPos(0, 3*time.Minute).at(base))
	tr.Tick(localIn("/music/a.mp3", "本地歌").withPos(time.Minute, 3*time.Minute).
		at(base.Add(time.Minute)))

	got := tr.Snapshot()
	if v := got.TrackMs("local:/music/a.mp3"); v != 60_000 {
		t.Errorf("本地歌时长 = %d 毫秒，want 60000", v)
	}
	if len(got.TopArtists(10)) != 0 {
		t.Errorf("本地文件进了 Top 艺人：%+v", got.TopArtists(10))
	}
	if len(got.TopTracks(10)) != 1 {
		t.Errorf("本地文件没进 Top 歌：%+v", got.TopTracks(10))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 五、性能：tick 上不许有堆分配
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTrackerDoesNotAllocateOnTick 钉住「每一拍不许有堆分配」。
//
// 🔴 **这是这一票最要紧的一条性能判据**：tick 是全局心跳，20Hz 一拍，
// 而界面每帧的唤醒都挂在它身上。**一次分配都不许有**——不是「少一点」，
// 是零。
//
// ⚠️ **判据用 `AllocsPerRun` 而不是 benchmark**：benchmark 报的是平均值，
// 而「平均 0.1 次分配」在 20Hz 下就是每秒 2 次分配、每天 17 万次——
// 那正是会让 GC 慢慢涨起来的那种量。AllocsPerRun 断的是「有没有」。
func TestTheTrackerDoesNotAllocateOnTick(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 先跑一拍把「上一拍」的状态铺好（第一拍要认身份，必然有一次性开销）。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))

	allocs := testing.AllocsPerRun(200, func() {
		tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(time.Second, 3*time.Minute).
			at(base.Add(time.Second)))
	})
	if allocs != 0 {
		t.Errorf("tick 一次分配了 %.2f 次，want 0", allocs)
	}
}

// TestChangingTheQualityIsNotAReplay 钉住「换音质不算重听」。
//
// 🔴 **这是「重听」那条判据最容易错的地方**：换音质会装上新的一台播放器
// （见 `restreamOnline`），而位置还在原来那儿——那是**同一遍**。
//
// 把它算成重听的话：换一次音质就多一次起播，而「起播次数」那一栏会
// 跟着用户换音质的频率虚高。
func TestChangingTheQualityIsNotAReplay(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	p1 := &audio.Player{}
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 3*time.Minute).at(base).onPlayer(p1))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(30 * time.Second)).onPlayer(p1))
	// 换音质：新的一台播放器，但**位置继续**（不是 0）。
	p2 := &audio.Player{}
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(31 * time.Second)).onPlayer(p2))
	// 换完之后照常往前走。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(31*time.Second, 3*time.Minute).
		at(base.Add(32 * time.Second)).onPlayer(p2))

	got := tr.Snapshot()
	if v := got.DayPlays("2026-11-01"); v != 1 {
		t.Errorf("当天起播次数 = %d，want 1（换音质不是重听）", v)
	}
	// 🔴 **时长也不能断**：换音质那一拍位置没变，那 1 秒还在。
	if v := got.DayMs("2026-11-01"); v != 31_000 {
		t.Errorf("当天时长 = %d 毫秒，want 31000（换音质不打断计时）", v)
	}
}

// TestTheAllTabStaysFastWithManyTracks 钉住「几千首歌时聚合不慢」。
//
// 🔴 **这是「全部」那一档不卡的那个前提**：Top 歌是**每次进页面时算一遍**，
// 而那一算是 O(n log n)（排序）。而用户可能听过几千首——所以这一条钉住
// 「几千首的聚合在几十毫秒之内」。
//
// ⚠️ **判据给得松**：CI 机器不快，而这一条防的是「O(n²) 那种病」——
// 几千首慢到 100ms 以上就说明算法不对了。
func TestTheAllTabStaysFastWithManyTracks(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 3000 首，每首 1 秒。
	const n = 3000
	for i := 0; i < n; i++ {
		at := base.Add(time.Duration(i) * 2 * time.Millisecond)
		tr.Tick(mvIn(fmt.Sprintf("BV%d", i), "c1", fmt.Sprintf("歌 %d", i), "UP", 1).
			withPos(0, time.Minute).at(at))
		tr.Tick(mvIn(fmt.Sprintf("BV%d", i), "c1", fmt.Sprintf("歌 %d", i), "UP", 1).
			withPos(time.Second, time.Minute).at(at.Add(time.Second)))
	}

	start := time.Now()
	snap := tr.Snapshot()
	top := snap.TopTracks(100)
	elapsed := time.Since(start)

	if len(top) != 100 {
		t.Fatalf("Top 歌取回了 %d 首，want 100", len(top))
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("3000 首的快照 + 排序花了 %v，want ≤ 200ms", elapsed)
	}
}

// TestTheDayTotalEqualsTheSumOfItsTracks 钉住「一天的总时长 = 那一档各首之和」。
//
// 🔴 **这一条钉的是一个不变量**：界面上「今天收听 30 分钟」和下面那张表
// 「15 分钟 + 10 分钟 + 5 分钟」是**同一份数据的两种说法**——而如果各首之和
// 大过那一天的总数，用户看到的���是「加起来比总数还多」。
//
// ⚠️ **它成立是因为那一份是同一处攒出来的**：`d.Ms` 和每一首的 `Ms` 记的
// 是**同一个增量**（见 listening_stats.go 里那几行）。所以这不是巧合，是
// 结构决定的——而结构会被改，所以要钉住。
//
// ⚠️ **次数也一样**：`d.Plays` 是那一天起的次数，而各首的 `Plays` 加起来
// 也得等于它。
func TestTheDayTotalEqualsTheSumOfItsTracks(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 三首，各听一会儿。
	for i, key := range []string{"mv:1|c1", "mv:2|c2", "mv:3|c3"} {
		tr.Tick(mvIn(key[:len(key)-2], key[len(key)-2:], "歌"+key, "UP", 1).
			withPos(0, time.Minute).at(base.Add(time.Duration(i) * 10 * time.Second)))
		tr.Tick(mvIn(key[:len(key)-2], key[len(key)-2:], "歌"+key, "UP", 1).
			withPos(10*time.Second, time.Minute).at(base.Add(time.Duration(i)*10*time.Second + 10*time.Second)))
	}

	got := tr.Snapshot()
	day := got.Days["2026-11-01"]
	if day == nil {
		t.Fatalf("没有 2026-11-01 那一档")
	}
	var sumMs, sumPlays int64
	for _, t := range day.Tracks {
		sumMs += t.Ms
		sumPlays += t.Plays
	}
	if sumMs != day.Ms {
		t.Errorf("各首时长之和 = %d 毫秒，而那一天的总时长 = %d 毫秒", sumMs, day.Ms)
	}
	if sumPlays != day.Plays {
		t.Errorf("各首次数之和 = %d，而那一天的起播次数 = %d", sumPlays, day.Plays)
	}
}

// TestTheTrackerRecordsTheTrackMetadataInTheDayDetail 钉住「按天存的那一份
// 也带着那几个快照字段」。
//
// 🔴 **这一条挡住的是一个「界面上看不出坏了」的 bug**：界面层的「Top 艺人」
// 那一档是**从按天存的那一份里加起来**的，而它靠的是每一首的 `ArtistMid`。
// 那一栏要是空的，那张表**永远是空的**——而界面上只显示「这一段时间还没有
// 听过 B 站的歌」，用户以为是自己没听过。
//
// ⚠️ **上一版漏了它**：那几个快照字段只写进了全局那一份，而界面层读的是
// 按天存的那一份。
func TestTheTrackerRecordsTheTrackMetadataInTheDayDetail(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(30 * time.Second)))

	got := tr.Snapshot()
	day := got.Days["2026-11-01"]
	if day == nil {
		t.Fatalf("没有 2026-11-01 那一档")
	}
	trk, ok := day.Tracks["mv:1|c1"]
	if !ok {
		t.Fatalf("按天存的那一份里没有这一首：%+v", day.Tracks)
	}
	if trk.Title != "一首歌" {
		t.Errorf("按天那一份的歌名 = %q，want 「一首歌」", trk.Title)
	}
	if trk.Artist != "某个UP" {
		t.Errorf("按天那一份的 UP 名 = %q，want 「某个UP」", trk.Artist)
	}
	if trk.ArtistMid != 42 {
		t.Errorf("按天那一份的 UP 编号 = %d，want 42（界面层的 Top 艺人靠它）", trk.ArtistMid)
	}
}

// TestTheCompletedCountNeedsTheTrackDuration 钉住「听完」判据要拿得到时长。
//
// 🔴 **这一条挡住的是一个「永远显示 0」的 bug**：`settleLocked` 判「听完」
// 要用这一首多长（`tr.st.dur`），而那个时长是**外面给进来的**——
//
// ⚠️ **上一版漏了它**：生产那一路（`statsTick`）压根没填 `Dur`，所以
// `dur` 永远是 0，而 0 <= 30 秒 ⇒ **「听完」永远是 0**。
//
// 而单元测试是**直接构造 `statsTickIn`** 的（自己填了 `Dur`），所以那条测试
// 全绿而生产是坏的——**这就是这一条存在的理由**：它走的是「时长是外面
// 给的」那一条路。
func TestTheCompletedCountNeedsTheTrackDuration(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	// 一首 4 分钟的歌，播到一半。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 4*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(2*time.Minute, 4*time.Minute).
		at(base.Add(2 * time.Minute)))
	tr.Tick(mvIn("2", "c2", "另一首", "UP乙", 2).withPos(0, 4*time.Minute).
		at(base.Add(2*time.Minute + time.Second)))

	got := tr.Snapshot()
	if v := got.DayCompleted("2026-11-01"); v != 1 {
		t.Errorf("听完 = %d 次，want 1", v)
	}
}

// TestAFinishedTrackIsNotCountedAsPlayedTwice 钉住「放完一遍不是起了两次」。
//
// 🔴 **这一条挡住的是一个「起播次数虚高」的 bug**：
//
// 一首放完之后，队列常常停在队尾（或者下一首还在取流，而 `a.online` 还指着
// 旧的这一首）——于是**下一拍还是同一首**。而那一拍如果被当成「换了一首」，
// 就会**再记一次起播**，于是「听了一遍」算成「起了两次」。
//
// ⚠️ **而更糟的是时长**：那一段时间会被算成「用户还在听旧那首」——旧那首
// 在取流那一段时间里**连时长都虚增了**。
func TestAFinishedTrackIsNotCountedAsPlayedTwice(t *testing.T) {
	tr := newStatsFixture()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)

	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 30*time.Second).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(30*time.Second, 30*time.Second).
		at(base.Add(30 * time.Second)).finished())
	// ⚠️ **放完之后的那几拍还是同一首**（队列停在队尾 / 下一首还在取流）。
	for i := 0; i < 5; i++ {
		tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(30*time.Second, 30*time.Second).
			at(base.Add(time.Duration(31+i) * time.Second)))
	}

	got := tr.Snapshot()
	if v := got.DayPlays("2026-11-01"); v != 1 {
		t.Errorf("起播次数 = %d，want 1（放完一遍不是起了两次）", v)
	}
	// 🔴 **时长也不能虚增**：放完之后那几拍**不听了**。
	if v := got.DayMs("2026-11-01"); v > 31_000 {
		t.Errorf("当天时长 = %d 毫秒，want ≤ 31000（放完之后那几拍不该记时长）", v)
	}
}

// TestTheCompletedCountLandsOnTheDayTheListeningHappened 钉住「听完记在
// 听的那一天」。
//
// 🔴 **这一条挡住的是一个「跨零点时数记错日子」的 bug**：
//
// 一首歌跨了零点（前半段昨天、后半段今天），而「听完」是一个整件事，它只能
// 落在一个日子里——**落在这首歌时长主要在的那一天**。
//
// ⚠️ **它不是「最后那一拍是哪一天」**：那样的话，一首 23:50 开始听、00:10
// 放完的歌，「听完」会记到第二天，而**第二天用户一分钟都没听**（时长已经
// 按拍记在昨天了）。
func TestTheCompletedCountLandsOnTheDayTheListeningHappened(t *testing.T) {
	tr := newStatsFixture()
	// 🔴 **23:50 开始，听 15 分钟**——那么 23:50→00:00 是 11-01 的
	// **十分钟**，00:00→00:05 是 11-02 的**五分钟**。
	//
	// ⚠️ **别写成 23:50 起听 10 分钟**：那就是正好跨零点，前一天那一截
	// 是 0 分钟——而「时长主要在哪一天」就成了平手（而平手归先来的那天）。
	start := time.Date(2026, 11, 1, 23, 50, 0, 0, time.Local)
	midnight := time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)

	// 23:50 → 00:00（11-01 的十分钟）。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, 20*time.Minute).at(start))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(10*time.Minute, 20*time.Minute).
		at(midnight))
	// 00:00 → 00:05（11-02 的五分钟）——**比前一天少**。
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(10*time.Minute, 20*time.Minute).
		at(midnight))
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(15*time.Minute, 20*time.Minute).
		at(midnight.Add(5 * time.Minute)).finished())

	got := tr.Snapshot()
	// 两天各 10 分钟，而「听完」记在**时长更多的那一天**（这里是 11-01，
	// 因为先开始的那一段先记）。
	// ⚠️ **而关键的是：不能记在「时长是 0 的那一天」**——那是用户一分钟
	// 都没听的那天（只放了一部分）。
	if got.DayCompleted("2026-11-02") != 0 {
		t.Errorf("11-02 那一天记了 %d 次「听完」，want 0（时长主要在 11-01）",
			got.DayCompleted("2026-11-02"))
	}
	if got.DayCompleted("2026-11-01") != 1 {
		t.Errorf("11-01 那一天记了 %d 次「听完」，want 1", got.DayCompleted("2026-11-01"))
	}
}
