package app_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/media"
)

// 系统媒体会话在业务这一侧的接线：什么时候说一声、说什么、以及收到命令之后
// 到底做了什么。
//
// 真机那一半（Objective-C、MediaPlayer.framework）在这里一个字都不出现——
// 那一半在 `internal/media` 的真机用例里。这里的断言全是「App 交出去的那份
// 快照对不对」和「命令落到哪个动作上」。

// spy 记下 App 交出去的所有快照。
type spy struct {
	mu   sync.Mutex
	all  []media.NowPlaying
	want int // 收到几条之后返回
	got  chan struct{}
}

func newSpy() *spy {
	return &spy{got: make(chan struct{}, 64)}
}

func (s *spy) publish(n media.NowPlaying) {
	s.mu.Lock()
	s.all = append(s.all, n)
	s.mu.Unlock()
	select {
	case s.got <- struct{}{}:
	default:
	}
}

func (s *spy) last() media.NowPlaying {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.all) == 0 {
		return media.NowPlaying{}
	}
	return s.all[len(s.all)-1]
}

func (s *spy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.all)
}

// await 等到收到第 n 份快照。
func (s *spy) await(t *testing.T, n int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.count() >= n {
			return
		}
		select {
		case <-s.got:
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("等了 5 秒，%s（要第 %d 份，只收到 %d 份）", what, n, s.count())
}

// mountedApp 建一个装了媒体会话的 App。
func mountedApp(t *testing.T) (*app.App, *spy) {
	t.Helper()
	a := newApp(t)
	s := newSpy()
	a.SetMediaSink(s.publish)
	return a, s
}

// ─────────────────────────────────────────────────────────────────────────────
// 说了什么
// ─────────────────────────────────────────────────────────────────────────────

// TestOpeningATrackReachesTheSystem 盯「打开一首歌要说一声」。
//
// 不说的话，锁屏上永远没有歌名——而这一票要的就是那个。
func TestOpeningATrackReachesTheSystem(t *testing.T) {
	a, s := mountedApp(t)
	s.await(t, 1, "装上会话之后应该先报一次现状")

	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	n := s.last()
	if n.State != media.StatePlaying {
		t.Errorf("状态是 %v，want playing", n.State)
	}
	want := filepath.Base(flac(t))
	if want = trimExt(want); n.Title != want {
		t.Errorf("歌名是 %q，want %q", n.Title, want)
	}
	if n.Duration <= 0 {
		t.Error("时长是 0——锁屏上的进度条会没有刻度")
	}
}

// trimExt 去掉扩展名，和 App 给本地文件取歌名的算法一致。
func trimExt(name string) string {
	if i := len(name) - len(filepath.Ext(name)); i > 0 {
		return name[:i]
	}
	return name
}

// TestALocalFileHasNoArtist 盯本地文件没有艺人。
//
// 这是**有意留空的**：读音频内嵌标签那一套没有接，Biu 上游也没有。
// 拿文件夹名去凑一个的话，锁屏上会一直显示一个猜出来的名字，而用户
// 看不出它是猜的。
func TestALocalFileHasNoArtist(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")
	if got := s.last().Artist; got != "" {
		t.Errorf("本地文件的艺人 = %q，应该是空的", got)
	}
	if got := s.last().ArtworkURL; got != "" {
		t.Errorf("本地文件的封面地址 = %q，应该是空的（见 State.CoverURL）", got)
	}
}

// TestTogglingReachesTheSystem 盯播放/暂停要说一声。
//
// 系统那边是「必设」的播放状态：不报的话控制中心上的按钮会一直显示
// 「播放中」，而歌是停着的。
func TestTogglingReachesTheSystem(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.Toggle()
	s.await(t, 3, "暂停")
	if got := s.last().State; got != media.StatePaused {
		t.Errorf("暂停之后状态是 %v，want paused", got)
	}

	a.Toggle()
	s.await(t, 4, "接着放")
	if got := s.last().State; got != media.StatePlaying {
		t.Errorf("恢复之后状态是 %v，want playing", got)
	}
}

// TestScrubbingReachesTheSystem 盯拖进度条要说一声。
//
// 锁屏上那根进度条和界面上这根是同一根：不报的话它停在原地，直到下一次
// 语义变化才跟上。
func TestScrubbingReachesTheSystem(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.ScrubEnd(0.5)
	s.await(t, 3, "拖到中间")
	if got := s.last().Position; got <= 0 {
		t.Errorf("拖完之后报的位置是 %v，want 大于 0", got)
	}
}

// TestClosingClearsTheSystem 盯退出时把系统清干净。
//
// 不发的话退出去之后锁屏上还留着一首已经不存在的歌，而控制中心的播放键
// 按下去还能收到命令。
func TestClosingClearsTheSystem(t *testing.T) {
	a := newApp(t)
	s := newSpy()
	a.SetMediaSink(s.publish)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.Close()
	s.await(t, 3, "退出")
	if got := s.last().State; got != media.StateNone {
		t.Errorf("退出之后状态是 %v，want none", got)
	}
	if got := s.last().Title; got != "" {
		t.Errorf("退出之后还带着歌名 %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 收到命令之后做了什么
// ─────────────────────────────────────────────────────────────────────────────

// TestThePauseCommandPauses 盯「暂停」这条命令真的暂停。
func TestThePauseCommandPauses(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.MediaCommand(media.CommandPause, 0)
	waitFor(t, 5*time.Second, "歌停下来", func() bool { return !a.Snapshot().Playing })
	if got := s.last().State; got != media.StatePaused {
		t.Errorf("命令之后状态是 %v，want paused", got)
	}
}

// TestThePlayCommandDoesNotToggle 盯「播放」这条命令**不**是切换。
//
// 这是这一组里最容易写错的一条：用户在控制中心点了播放而歌本来就在放的
// 时候，按切换会把它按停——用户看到的是「按了播放反而停了」。
func TestThePlayCommandDoesNotToggle(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")
	waitFor(t, 5*time.Second, "歌开始走", func() bool { return a.Snapshot().Position > 0 })

	a.MediaCommand(media.CommandPlay, 0)
	if !a.Snapshot().Playing {
		t.Error("歌本来在放，「播放」命令之后变成停了——这条命令被当成切换了")
	}
}

// TestThePauseCommandOnAPausedTrackIsHarmless 盯重复的暂停不动手。
func TestThePauseCommandOnAPausedTrackIsHarmless(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.MediaCommand(media.CommandPause, 0)
	waitFor(t, 5*time.Second, "歌停下来", func() bool { return !a.Snapshot().Playing })
	before := s.count()

	a.MediaCommand(media.CommandPause, 0)
	if got := s.last().State; got != media.StatePaused {
		t.Errorf("再按一次暂停之后状态是 %v，want paused", got)
	}
	_ = before
}

// TestTheStopCommandGoesBackToTheStart 盯「停止」回到开头。
//
// 停止和暂停对系统是两件事：暂停的进度条停在原地，停止的进度条回到开头。
func TestTheStopCommandGoesBackToTheStart(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")
	waitFor(t, 5*time.Second, "歌开始走", func() bool { return a.Snapshot().Position > 0 })

	a.MediaCommand(media.CommandStop, 0)
	if got := s.last().State; got != media.StateStopped {
		t.Errorf("停止之后状态是 %v，want stopped", got)
	}
	if got := s.last().Position; got != 0 {
		t.Errorf("停止之后位置是 %v，want 0", got)
	}
	if a.Snapshot().Playing {
		t.Error("停止之后歌还在放")
	}
}

// TestTheStopThenPlayCommandResumes 盯「停止之后再按播放」从头开始放。
//
// 停在 0 秒而不是接着刚才那一句——「停止」的意思是「这首到此为止」。
func TestTheStopThenPlayCommandResumes(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.MediaCommand(media.CommandStop, 0)
	a.MediaCommand(media.CommandPlay, 0)
	waitFor(t, 5*time.Second, "重新开始放", func() bool { return a.Snapshot().Playing })
	if got := s.last().State; got != media.StatePlaying {
		t.Errorf("重新播放之后状态是 %v，want playing", got)
	}
}

// TestTheSeekCommandJumps 盯锁屏上拖进度条能跳。
//
// 这是遥控器上唯一**没法自我诊断**的一条：拖不动的时候用户只会以为耳机
// 坏了，不会想到是播放器的问题。
func TestTheSeekCommandJumps(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")
	dur := a.Snapshot().Duration
	if dur < 3*time.Second {
		t.Skipf("素材只有 %v，跳不动", dur)
	}

	a.MediaCommand(media.CommandSeek, dur/2)
	waitFor(t, 5*time.Second, "跳到中间", func() bool {
		p := a.Snapshot().Position
		return p > dur/4 && p < 3*dur/4
	})
	if got := s.last().Position; got < dur/4 {
		t.Errorf("跳完之后报给系统的位置是 %v，太靠前了（大概 %v）", got, dur/4)
	}
}

// TestASeekPastTheEndStopsAtTheEnd 盯跳过头被夹在末尾。
//
// 夹住的理由不是「怕溢出」：进度比时长还长的话，系统那根进度条的最后一格
// 是空的，而拖到末尾本来就是合法操作（放完这一首）。
func TestASeekPastTheEndStopsAtTheEnd(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")
	dur := a.Snapshot().Duration

	a.MediaCommand(media.CommandSeek, dur+time.Hour)
	waitFor(t, 5*time.Second, "停在末尾", func() bool {
		return a.Snapshot().Position >= dur-time.Second
	})
	if got := s.last().Position; got > dur {
		t.Errorf("报给系统的位置是 %v，比时长 %v 还长", got, dur)
	}
}

// TestANegativeSeekIsIgnored 盯往回拖过头不动。
func TestANegativeSeekIsIgnored(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	a.MediaCommand(media.CommandSeek, -time.Minute)
	if got := a.Snapshot().Position; got < 0 {
		t.Errorf("往回拖了负数之后位置是 %v", got)
	}
}

// TestTheStepCommandsMoveInTheQueue 盯上一首/下一首走的是队列。
//
// 用两份**不同的**素材，才能看出确实换了一首——`Open` 是从路径取歌名的，
// 同一份文件排两次的话两首同名，怎么看都像没动。
func TestTheStepCommandsMoveInTheQueue(t *testing.T) {
	a, s := mountedApp(t)
	first, second := flac(t), otherFlac(t)
	if first == second {
		t.Skip("只有一个素材，切歌看不出来")
	}
	if err := a.Enqueue(app.LocalItem(first), app.LocalItem(second)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "开始放第一首")
	if got := a.Snapshot().Title; got != trimBase(first) {
		t.Fatalf("一开始放的是 %q，want %q", got, trimBase(first))
	}

	a.MediaCommand(media.CommandNext, 0)
	waitFor(t, 5*time.Second, "切到第二首", func() bool {
		return a.Snapshot().Title == trimBase(second)
	})

	a.MediaCommand(media.CommandPrev, 0)
	waitFor(t, 5*time.Second, "切回第一首", func() bool {
		return a.Snapshot().Title == trimBase(first)
	})
}

// otherFlac 是另一份 FLAC 素材（换歌用）。
func otherFlac(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "mono16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到第二份素材：%v", err)
	}
	return p
}

// trimBase 是文件名去掉扩展名（和 App 给本地文件取歌名的算法一致）。
func trimBase(path string) string { return trimExt(filepath.Base(path)) }

// TestCommandsOnAnEmptyAppDoNothing 盯没有歌的时候命令不乱来。
//
// 系统那边还可能递命令过来（用户点的是上一个用过的应用留下的按钮），
// 这时候不能 panic、不能把状态改坏。
func TestCommandsOnAnEmptyAppDoNothing(t *testing.T) {
	a, s := mountedApp(t)
	for _, c := range []media.Command{
		media.CommandPlay, media.CommandPause, media.CommandToggle,
		media.CommandNext, media.CommandPrev, media.CommandStop, media.CommandSeek,
	} {
		a.MediaCommand(c, time.Second)
	}
	if a.Snapshot().HasTrack {
		t.Error("没有歌的时候按命令，凭空多出来一首")
	}
	if got := s.last().State; got != media.StateNone {
		t.Errorf("没有歌的时候报给系统的是 %v，want none", got)
	}
}

// TestASongFinishingReachesTheSystem 盯「歌自然放完了要说一声」。
//
// 这一条曾经**漏了**，而漏掉的样子和「代码没跑」完全不同：歌放完了、界面上的
// 进度条走到底，一切正常，只有**锁屏和控制中心**还停在「正在播放」——进度条
// 卡在最后一格，播放键画的是一个已经在响的按钮。
//
// 为什么容易漏：App 每 50ms 写一次播放位置，而「位置」这一项按规矩**不**
// 一变化就报（系统自己外推）。于是「在放 → 不在放」这个变化，恰好被那条
// 「位置不动就不报」的规则一起吃掉了——它只在**播放中**才校正，歌一停校正
// 也跟着停了。
func TestASongFinishingReachesTheSystem(t *testing.T) {
	a, s := mountedApp(t)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	s.await(t, 2, "打开一首歌")

	// 等它真的放到头。
	waitFor(t, 30*time.Second, "歌放到头", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Duration > 0 && st.Position >= st.Duration
	})
	waitFor(t, 5*time.Second, "播放器自己报「不在放」", func() bool {
		return !a.Snapshot().Playing
	})

	// 再给系统一点时间，然后看它收到的那一份。
	time.Sleep(300 * time.Millisecond)
	got := s.last()
	if got.State == media.StatePlaying {
		t.Errorf("歌已经放完了（位置 %v / %v），系统那边还是 playing",
			a.Snapshot().Position, a.Snapshot().Duration)
	}
}
