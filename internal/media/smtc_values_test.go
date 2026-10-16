package media

import (
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Windows 系统媒体会话的词汇表
//
// 断的是「我们的话 ↔ 系统的话」这张对照表。表错了**不会报错**：SMTC 收到一个
// 认不出来的播放状态只会照单全收，于是浮窗上显示「暂停」而歌在放；而这一票
// 在 Windows 上没法真机验，这一组用例是唯一能自动抓到它的东西。
// ─────────────────────────────────────────────────────────────────────────────

// TestSmtcStatusIsNotTheMacNumber 钉住 Windows 的这套数字和 macOS 的不是同一条。
//
// 两边差一位（Windows Closed=0 起、Playing=3；macOS Unknown=0 起、Playing=1）。
// 照着另一边的数字抄过来是这个包里最容易犯、也最难在真机上发现的错：歌在放，
// 浮窗上写着暂停，而**应用这边完全看不出发生过什么**。
func TestSmtcStatusIsNotTheMacNumber(t *testing.T) {
	if smtcPlaying == 1 {
		t.Error("Windows 的 Playing 写成了 1，那是 macOS 的数字")
	}
	if smtcPlaying != 3 {
		t.Errorf("Windows 的 Playing 是 %d，want 3", smtcPlaying)
	}
}

// TestSmtcPlaybackStatusMapping 断四个状态各翻成什么。
func TestSmtcPlaybackStatusMapping(t *testing.T) {
	for _, c := range []struct {
		in   State
		want smtcStatus
	}{
		{StatePlaying, smtcPlaying},
		{StatePaused, smtcPaused},
		{StateStopped, smtcStopped},
		// ⚠️ None → Closed 而不是 Stopped：Windows 上 Closed 的意思是
		// 「根本没有会话」，系统会把它整个从浮窗里拿掉；Stopped 会留下一条
		// 永远不动的记录，用户看到的是一个已经关掉的播放器还在。
		{StateNone, smtcClosed},
	} {
		if got := smtcPlaybackStatus(c.in); got != c.want {
			t.Errorf("%v 翻成了 %d，want %d", c.in, got, c.want)
		}
	}
}

// TestNoStateMapsToChanging 断「换源中」不会被我们用上。
//
// `Changing` 是给「正在换流」用的。这一版换流是业务层自己的事（BMG-14），
// 系统那边看不到也用不着——把它当成一个状态发过去的话，浮窗上的进度条会在
// 换流那几秒里停住，而那几秒本来应该照常走。
func TestNoStateMapsToChanging(t *testing.T) {
	for _, s := range []State{StateNone, StatePlaying, StatePaused, StateStopped} {
		if got := smtcPlaybackStatus(s); got == smtcChanging {
			t.Errorf("%v 翻成了 Changing", s)
		}
	}
}

// TestSmtcCommandMapping 断系统按下的每一个键翻成哪条命令。
func TestSmtcCommandMapping(t *testing.T) {
	for _, c := range []struct {
		in   smtcButton
		want Command
	}{
		{smtcButtonPlay, CommandPlay},
		{smtcButtonPause, CommandPause},
		{smtcButtonStop, CommandStop},
		{smtcButtonNext, CommandNext},
		{smtcButtonPrev, CommandPrev},
	} {
		got, ok := smtcCommand(c.in)
		if !ok {
			t.Errorf("键 %d 被判成接不住，而它明明接得住", c.in)
			continue
		}
		if got != c.want {
			t.Errorf("键 %d 翻成了 %v，want %v", c.in, got, c.want)
		}
	}
}

// TestPlayAndPauseStayTwoCommands 钉住「播放和暂停不合成 toggle」。
//
// 合成 Toggle 的症状是「按了播放反而把歌按停」：用户在浮窗上点播放的那一刻，
// 歌本来就在放（只是浮窗上的状态慢了一帧），Toggle 就把它按停了。
//
// 这和耳机线控不一样——耳机上那一个物理键**本来就是**切换，所以它走 Toggle
// 是对的（见 darwin 那一半）。这里必须保持两条命令分开。
func TestPlayAndPauseStayTwoCommands(t *testing.T) {
	play, _ := smtcCommand(smtcButtonPlay)
	pause, _ := smtcCommand(smtcButtonPause)
	if play == CommandToggle || pause == CommandToggle {
		t.Errorf("play/pause 有一条变成了 toggle（%v / %v）", play, pause)
	}
	if play == pause {
		t.Errorf("播放和暂停翻成了同一条命令 %v", play)
	}
}

// TestUnhandledButtonsAreRejected 断「接不住的键」被认出来。
//
// 这三个（快进 / 倒带 / 录音）Windows 的浮窗上本来就不画，我们不去碰它们
// 就不会出现「按了没反应」的键。返回值是 false 就是给调用点一个交代：
// 那边要把对应的 `IsXxxEnabled` 设成 false。
func TestUnhandledButtonsAreRejected(t *testing.T) {
	// 快进 4、倒带 5、录音 3 —— Windows.Media.SystemMediaTransportControlsButton
	// 的实际取值。它们**不是**我们内部 Command 的数字，别照抄。
	for _, b := range []smtcButton{3, 4, 5, 8, 9, 99} {
		if _, ok := smtcCommand(b); ok {
			t.Errorf("键 %d 被判成接得住，可我们没有它的意思", b)
		}
	}
}

// TestNoSmtcButtonEverSeeks 断 SMTC 上不存在 seek。
//
// Windows 的 SMTC **不发 seek 请求**：浮窗上的进度条走的是另一条事件
// （PlaybackPositionChangeRequested），走的是另一个口子（smtc_windows.go 里
// 的时间轴那一片），不是这张表。所以「有个键能跳位置」这件事在这一层是
// 不成立的——把它写进这张表的话，代码看着像能跳，实际上那个键不存在。
func TestNoSmtcButtonEverSeeks(t *testing.T) {
	for b := smtcButton(0); b < 10; b++ {
		cmd, ok := smtcCommand(b)
		if ok && cmd == CommandSeek {
			t.Errorf("键 %d 翻成了 seek", b)
		}
	}
}

// TestSmtcStatusValuesAreDistinct 断五个状态互不相同。
//
// 抄错成两个一样的值时，上面的映射测试仍然全绿（映射对了，只是有两个状态
// 落到同一个数上），而真机上那两种状态在浮窗里长得一模一样。
func TestSmtcStatusValuesAreDistinct(t *testing.T) {
	seen := map[smtcStatus]string{}
	for name, v := range map[string]smtcStatus{
		"closed":   smtcClosed,
		"changing": smtcChanging,
		"stopped":  smtcStopped,
		"playing":  smtcPlaying,
		"paused":   smtcPaused,
	} {
		if prev, dup := seen[v]; dup {
			t.Errorf("%s 和 %s 是同一个值 %d", prev, name, v)
		}
		seen[v] = name
	}
}

// TestSmtcButtonValuesAreNotCommandValues 断两套数字没有混。
//
// Command 是我们自己的（CommandPlay=0 起），smtcButton 是系统的（也是 0 起）。
// 两边的顺序不一样（Command 是 play/pause/toggle/next/prev/stop/seek，
// smtcButton 是 play/pause/stop/record/ff/rewind/next/prev），所以**直接互传
// 是错的**——只是错得很有迷惑性：0 传过去是 play（对），1 是 pause（对），
// 而 2 在 Command 里是 toggle、在系统那边是 stop（错，且症状是「按停止变成
// 播放暂停切换」）。这条把那条错路直接钉死。
func TestSmtcButtonValuesAreNotCommandValues(t *testing.T) {
	// ⚠️ 2 在两边**碰巧是同一个数**，而意思完全不同：系统那边是「停止」，
	// 我们这边是「切换」。所以「收到的整数直接当 Command 用」这种写法在
	// 0 和 1 上都对（play、pause）、在 2 上错、往后的每一个都错——这种
	// 错法在小规模测试下极难发现，而真机上的症状是「按停止变成了播放暂停
	// 切换」。
	//
	// 这条测试存在的意义就是把那句「碰巧一样」钉住：哪天上游改了任一张表，
	// 它提醒有人回来重看一遍这里。
	if int(smtcButtonStop) != 2 || int(CommandToggle) != 2 {
		t.Logf("注意：2 在两边不再碰巧一样了（系统 %d / 我们 %d），",
			int(smtcButtonStop), int(CommandToggle))
	}

	if cmd, ok := smtcCommand(smtcButtonStop); !ok || cmd != CommandStop {
		t.Errorf("系统的停止键翻成了 %v，want stop", cmd)
	}
	// 系统的 3 是 record，我们自己的 3 是 next。直传的话，一个根本不存在的
	// 键会变成「下一首」。
	if _, ok := smtcCommand(smtcButton(3)); ok {
		t.Error("系统的 3（record）被当成我们接得住的键了")
	}
	// 系统的 6/7 是 next/prev，我们自己的是 3/4。顺序不同，直传一样会错。
	if got, _ := smtcCommand(smtcButton(6)); got != CommandNext {
		t.Errorf("系统的 6 翻成了 %v，want next", got)
	}
	if got, _ := smtcCommand(smtcButton(7)); got != CommandPrev {
		t.Errorf("系统的 7 翻成了 %v，want prev", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 时间
// ─────────────────────────────────────────────────────────────────────────────

// TestTicks 断「时长 ↔ Windows 的 100 纳秒」这个换算。
//
// ⚠️ **这一条是补上去的，因为 `windowsTicks` 一开始写成了 `10 * time.Millisecond`**
// ——那是 1000 万纳秒，而 SMTC 的单位是 **100 纳秒**，**差十万倍**。
//
// 症状是「歌才播了两秒，进度条走完了整首」，而歌名、按键、播放状态全都正常。
// 只坏一根进度条，加上一个错得离谱的常数，人在真机上盯着看只会以为是
// 「Windows 的进度条算得不对」。
func TestTicks(t *testing.T) {
	if windowsTicks != 100*time.Nanosecond {
		t.Fatalf("windowsTicks 是 %v，那边是 100ns（= 100 * time.Nanosecond）", windowsTicks)
	}
	for _, c := range []struct {
		in   time.Duration
		want int64
	}{
		{0, 0},
		{100 * time.Nanosecond, 1},
		{time.Microsecond, 10},
		{time.Millisecond, 10_000},
		{time.Second, 10_000_000},
		// 225 秒 × 每秒一千万刻
		{3*time.Minute + 45*time.Second, 2_250_000_000},
	} {
		if got := ticksOf(c.in); got != c.want {
			t.Errorf("ticksOf(%v) = %d，want %d", c.in, got, c.want)
		}
	}
}

// TestTicksClampsToZero 断负的时长落成 0。
//
// 一个负的 EndTime 在 SMTC 上没有意义，而它会变成「整根进度条画不出来」。
func TestTicksClampsToZero(t *testing.T) {
	for _, d := range []time.Duration{-1, -time.Second, -time.Hour} {
		if got := ticksOf(d); got != 0 {
			t.Errorf("ticksOf(%v) = %d，want 0", d, got)
		}
	}
}
