package media

import (
	"sync"
	"testing"
	"time"
)

// fakePlatform 记下这一轮平台那一半被要求做了什么。
type fakePlatform struct {
	mu       sync.Mutex
	applied  []NowPlaying
	handler  func(Command, time.Duration)
	detaches int
}

func (f *fakePlatform) apply(n NowPlaying) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, n)
}

func (f *fakePlatform) attach(fn func(Command, time.Duration)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = fn
}

func (f *fakePlatform) detach() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detaches++
	f.handler = nil
}

// published 返回这一轮一共往下发了几份、发的是什么。
func (f *fakePlatform) published() []NowPlaying {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]NowPlaying(nil), f.applied...)
}

// count 是发了几次。
func (f *fakePlatform) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

// ─────────────────────────────────────────────────────────────────────────────
// 什么时候发
//
// 那一半是这一层最需要钉住的地方：发多了系统在白重建字典，发少了锁屏上的
// 进度条停在原地，而两边的症状**都是「看着没动静」**，人眼分不出是哪一种。
// ─────────────────────────────────────────────────────────────────────────────

// playing 是一份「正在放」的快照，省得每条用例都写一遍。
func playing() NowPlaying {
	return NowPlaying{
		State:    StatePlaying,
		Title:    "一首歌",
		Duration: 3 * time.Minute,
	}
}

// TestUnchangedIsNotSentAgain 盯「语义没变就别发」。
//
// 这一条是 Apple 那句 "updating this property frequently is not required
// (or recommended)" 的落点。去掉这条判断，这个包仍然全部测试通过，
// 而真机上的表现是「控制中心的进度条偶尔要卡一下才跟上」——
// 那种症状会被当成系统慢，谁都不会去查自己这边多发了几百次。
func TestUnchangedIsNotSentAgain(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	s.Publish(playing())
	s.Publish(playing())
	s.Publish(playing())

	if got := f.count(); got != 1 {
		t.Errorf("同一份状态发了 %d 次，want 1", got)
	}
}

// TestTheFirstOneAlwaysGoesOut 盯第一次一定发。
//
// 「跟上一份比」在还没有上一份的时候无从比起。不发的话，挂上媒体会话之后
// 锁屏上会一直空着——而挂的那一刻用户根本没在放新歌，没有别的东西会触发它。
func TestTheFirstOneAlwaysGoesOut(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	s.Publish(playing())
	if got := f.count(); got != 1 {
		t.Fatalf("第一次 Publish 发了 %d 次，want 1", got)
	}
}

// TestEveryVisibleFieldCountsAsAChange 盯歌名 / 艺人 / 封面 / 时长 / 状态
// 任何一样变了都要重发。
//
// 它们是锁屏上**看得见**的字段。这里逐个改一遍：漏掉哪一个，那一栏就一直是
// 旧的——而且不会报错，只是「界面上没更新」。
func TestEveryVisibleFieldCountsAsAChange(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(*NowPlaying)
	}{
		{"歌名", func(n *NowPlaying) { n.Title = "另一首" }},
		{"艺人", func(n *NowPlaying) { n.Artist = "某某" }},
		{"封面", func(n *NowPlaying) { n.ArtworkURL = "https://example.invalid/c.jpg" }},
		{"时长", func(n *NowPlaying) { n.Duration = 4 * time.Minute }},
		{"在放 → 暂停", func(n *NowPlaying) { n.State = StatePaused }},
		{"暂停 → 停止", func(n *NowPlaying) { n.State = StateStopped }},
		{"有歌 → 没歌", func(n *NowPlaying) { *n = NowPlaying{State: StateNone} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakePlatform{}
			s := newSession(f)
			t.Cleanup(s.Close)

			first := playing()
			s.Publish(first)
			c.tweak(&first)
			s.Publish(first)

			if got := f.count(); got != 2 {
				t.Errorf("改了%s之后一共发了 %d 次，want 2", c.name, got)
			}
		})
	}
}

// TestSmallPositionMovesAreLeftToTheSystem 盯「位置往前挪一点点不���」。
//
// 系统会从上一次报的位置和播放速率**自己外推**进度（Apple 头文件原话）。
// App 每 50ms 写一次位置，全报过去就是在白重建字典。
func TestSmallPositionMovesAreLeftToTheSystem(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	n := playing()
	s.Publish(n)
	n.Position = 50 * time.Millisecond
	s.Publish(n)
	n.Position = 100 * time.Millisecond
	s.Publish(n)

	if got := f.count(); got != 1 {
		t.Errorf("位置往前挪了两次却发了 %d 次，want 1（进度该由系统外推）", got)
	}
}

// TestAPositionJumpIsSent 盯「跳了位置要报」。
//
// 用户拖了进度条、或者 App 每 5 秒做的那次校正，位置一下跳过两秒以上。
// 不报的话锁屏上的进度条要等到下一次语义变化才跟上，而语义变化可能
// 一直不来（用户就让它放着一首）。
func TestAPositionJumpIsSent(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	n := playing()
	s.Publish(n)
	n.Position = 30 * time.Second
	s.Publish(n)

	if got := f.count(); got != 2 {
		t.Errorf("跳了 30 秒却只发了 %d 次，want 2", got)
	}
}

// TestAPositionJumpBackwardsAlsoCounts 盯「往回拖也要报」。
//
// 绝对值差，不是「变大了才报」。往回拖之后不报的话，锁屏上的进度条会
// 停在刚才的位置继续走，而界面上那一根已经回去了——两根进度条对不上。
func TestAPositionJumpBackwardsAlsoCounts(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	n := playing()
	n.Position = 90 * time.Second
	s.Publish(n)
	n.Position = 10 * time.Second
	s.Publish(n)

	if got := f.count(); got != 2 {
		t.Errorf("往回拖了 80 秒却只发了 %d 次，want 2", got)
	}
}

// TestPausedPositionMovesAreSent 盯「暂停中挪了位置要报」。
//
// 系统**不会**给暂停状态外推进度（速率是 0）。所以这里必须自己报，
// 否则锁屏上的进度条永远停在暂停那一刻的位置——而用户在界面上拖一下
// 就看见它动了，两边对不上。
func TestPausedPositionMovesAreSent(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	n := playing()
	n.State = StatePaused
	s.Publish(n)
	n.Position = 50 * time.Millisecond
	s.Publish(n)

	if got := f.count(); got != 2 {
		t.Errorf("暂停中挪了位置却发了 %d 次，want 2", got)
	}
}

// TestSeekThresholdBoundary 盯那道线本身。
//
// 一秒差恰好**不**报（严格大于才算跳），两秒差报。写这条是因为把
// `>=` 写成 `>` 之后症状是「每次校正都多发一次」，而校正本来就 5 秒一次，
// 看不出来。
func TestSeekThresholdBoundary(t *testing.T) {
	cases := []struct {
		name  string
		shift time.Duration
		want  int
	}{
		{"差一秒", time.Second, 1},
		{"差两秒", 2 * time.Second, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakePlatform{}
			s := newSession(f)
			t.Cleanup(s.Close)

			n := playing()
			s.Publish(n)
			n.Position = c.shift
			s.Publish(n)

			if got := f.count(); got != c.want {
				t.Errorf("位置移了 %v 之后发了 %d 次，want %d", c.shift, got, c.want)
			}
		})
	}
}

// TestNoTrackClearsTheSystem 盯「没有歌了要发出去」。
//
// 这一份就是「把系统里那份曲目信息收掉」。不发的话退出去之后锁屏上还留着
// 一首已经不存在的歌，而控制中心的播放键按下去还能收到命令。
func TestNoTrackClearsTheSystem(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	s.Publish(playing())
	s.Publish(NowPlaying{State: StateNone})

	got := f.published()
	if len(got) != 2 {
		t.Fatalf("发了 %d 次，want 2", len(got))
	}
	if got[1].State != StateNone {
		t.Errorf("最后一份的状态是 %v，want none", got[1].State)
	}
	if got[1].Title != "" {
		t.Errorf("没有歌的时候还带着歌名 %q——清空不是这么清的", got[1].Title)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 命令：收到什么、什么时候执行、丢不丢
// ─────────────────────────────────────────────────────────────────────────────

// waitCommand 等到处理器收到一条命令。命令是在工作协程上跑的，所以要等。
func waitCommand(t *testing.T, got chan queued) queued {
	t.Helper()
	select {
	case q := <-got:
		return q
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒内没有收到任何远程命令")
		return queued{}
	}
}

// TestACommandReachesTheHandler 盯「系统发来的命令到得了处理器」。
func TestACommandReachesTheHandler(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	got := make(chan queued, 4)
	s.SetHandler(func(c Command, at time.Duration) { got <- queued{cmd: c, at: at} })

	f.handler(CommandNext, 0)
	if q := waitCommand(t, got); q.cmd != CommandNext {
		t.Errorf("处理器收到的是 %v，want next", q.cmd)
	}
}

// TestTheSeekPositionTravels 盯跳转带的位置有没有送到。
//
// 锁屏上拖进度条走的就是这条命令，位置只在这一个地方。丢了它的症状是
// 「拖了没反应」——而遥控器上拖不动是没法自我诊断的那种问题。
func TestTheSeekPositionTravels(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	got := make(chan queued, 4)
	s.SetHandler(func(c Command, at time.Duration) { got <- queued{cmd: c, at: at} })

	f.handler(CommandSeek, 42*time.Second)
	q := waitCommand(t, got)
	if q.cmd != CommandSeek {
		t.Fatalf("处理器收到的是 %v，want seek", q.cmd)
	}
	if q.at != 42*time.Second {
		t.Errorf("跳转位置是 %v，want 42s", q.at)
	}
}

// TestCommandsArriveInOrder 盯命令的执行顺序。
//
// 连按两下「下一首」时处理得反序，用户听到的是「第二首 → 第一首」。
// 队列只有一个消费者就是为了这个。
func TestCommandsArriveInOrder(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	t.Cleanup(s.Close)

	got := make(chan queued, 8)
	s.SetHandler(func(c Command, at time.Duration) {
		got <- queued{cmd: c, at: at}
	})

	// 先等消费者起来，否则连发会撞在「还没人取」上面，那不叫顺序问题。
	f.handler(CommandPause, 0)
	waitCommand(t, got)

	for _, c := range []Command{CommandPlay, CommandNext, CommandPrev, CommandStop} {
		f.handler(c, 0)
	}
	for _, want := range []Command{CommandPlay, CommandNext, CommandPrev, CommandStop} {
		if q := waitCommand(t, got); q.cmd != want {
			t.Errorf("第 %v 条执行的是 %v，顺序错了", want, q.cmd)
		}
	}
}

// TestAClosedSessionStopsTakingCommands 盯关掉之后不再执行。
//
// 退出流程走完之后系统还可能递一条命令过来（用户正好这时候按了耳机）。
// 那时候执行下去是往一个正在拆的东西上动手。
func TestAClosedSessionStopsTakingCommands(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)

	var mu sync.Mutex
	ran := 0
	s.SetHandler(func(Command, time.Duration) {
		mu.Lock()
		ran++
		mu.Unlock()
	})
	f.handler(CommandNext, 0)
	s.Close()

	// 关掉之后系统那边仍可能递命令过来（平台那一半摘得慢，或者摘之前
	// 已经有一条在飞）。都不该再执行。
	s.dispatch(CommandNext, 0)
	s.dispatch(CommandPause, 0)
	time.Sleep(50 * time.Millisecond) // 等那个消费协程真的退干净

	mu.Lock()
	defer mu.Unlock()
	if ran != 0 {
		t.Errorf("关掉之后仍然执行了 %d 条命令，want 0", ran)
	}
}

// TestCloseClearsTheSystem 盯退出时把系统里那份收掉。
func TestCloseClearsTheSystem(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	s.Publish(playing())
	s.Close()

	if f.detaches == 0 {
		t.Error("关掉的时候没有摘掉平台那一半")
	}
	got := f.published()
	if len(got) == 0 || got[len(got)-1].State != StateNone {
		t.Errorf("最后一份不是「没有歌」，是 %+v", got)
	}
}

// TestCloseIsIdempotent 盯关掉可以重复调。
//
// 退出流程里 OnWillQuit 和关窗那一条都会走到（见 ui/media_mount.go）。
func TestCloseIsIdempotent(t *testing.T) {
	f := &fakePlatform{}
	s := newSession(f)
	s.Close()
	s.Close()
	s.Close()
}

// ─────────────────────────────────────────────────────────────────────────────
// 翻成系统的话
// ─────────────────────────────────────────────────────────────────────────────

// TestPlaybackStateMapping 盯状态翻对了。
//
// 错一个的症状都是「控制中心上的按钮显示的东西和实际不符」，而且不会
// 报错：系统只是按你给的数画。StateNone 翻成 stopped 而不是 unknown 是
// 特意定的——unknown 在控制中心上留着一个不知道该显示什么的按钮。
func TestPlaybackStateMapping(t *testing.T) {
	cases := []struct {
		in   State
		want int
	}{
		{StateNone, 3},
		{StatePlaying, 1},
		{StatePaused, 2},
		{StateStopped, 3},
	}
	for _, c := range cases {
		if got := playbackStateNumber(c.in); got != c.want {
			t.Errorf("%v 翻成了 %d，want %d", c.in, got, c.want)
		}
	}
}

// TestSuccessIsZero 盯「成功」是 0。
//
// 写错成 1 的话系统把每一次成功的处理都当成失败，而症状是「按了没反应」
// 连日志都不会有——它是这一层最容易写错、也最难查的一个常数。
func TestSuccessIsZero(t *testing.T) {
	if statusSuccess != 0 {
		t.Errorf("statusSuccess = %d，want 0（MPRemoteCommandHandlerStatus）", statusSuccess)
	}
}

// TestCommandNamesAreDistinct 盯命令的名字不会重名。
//
// 日志里靠它区分「那次按的是哪条路过来的」。重名了看日志也白看。
func TestCommandNamesAreDistinct(t *testing.T) {
	seen := map[string]Command{}
	for _, c := range []Command{
		CommandPlay, CommandPause, CommandToggle, CommandNext,
		CommandPrev, CommandStop, CommandSeek,
	} {
		name := c.String()
		if name == "?" {
			t.Fatalf("命令 %d 没有名字", int(c))
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("命令 %d 和 %d 同名（%q）", int(c), int(prev), name)
		}
		seen[name] = c
	}
}
