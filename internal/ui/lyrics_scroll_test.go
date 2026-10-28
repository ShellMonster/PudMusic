// 歌词「滚到中间」那一下（`internal/ui/lyrics.go` 的 `lyricScrollAnim`）。
//
// 这一组钉两件事：
//
//  1. **时长是 200ms**（票 #470，2026-10-25 人拍板）。原来那 500ms **不是抄
//     上游来的**——上游 Biu 那个 `scrollTo({behavior:"smooth"})` 根本没给时长，
//     我们当时是拿框架 `ui.Smooth` 的默认值（500ms）顶上的。
//  2. **它是弹簧**，而弹簧的那一条性质是「打断时接着走」（速度连续）——换成
//     曲线的话，打断会「速度归零重新起跑」，用户看到的是一下顿挫。
//
// ⚠️ **量的是假时钟**（`tt.Advance`），不是墙钟：框架的钟站着不动，而弹簧按
// 帧与帧之间的**时间**推进（`mygo/ui/motion.go` 的 `step`：dt = 0 就一步都不
// 走）。数帧那种等法会**静默失效**——不报错、不超时，直接放行。
package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
)

// TestTheLyricScrollIsTwoHundredMilliseconds 钉住那一下的时长与形状。
//
// ⚠️ **那几个数一个都不许动**：
//
//	Bounce    0    不过冲（回弹只留给「奖励动量」，滚到中间不是那种）
//	Precision 0.05 一根头发丝的误差，到点就收手
//
// 而 `lyricLineAnim`（行内渐变 300ms，逐字抄上游 Biu 的
// `transition-all duration-300 ease-out`）与 `lyricSnapAnim`（窗口刚恢复那一帧
// 一次到位）**一个字都不许跟着动**——所以这一条把它们也钉上。
func TestTheLyricScrollIsTwoHundredMilliseconds(t *testing.T) {
	// 留空 `Duration` 才是走弹簧那一支（`mygo/ui/motion.go`）。
	if got := lyricScrollAnim.Duration; got != 0 {
		t.Errorf("滚到中间那一段的 Duration 是 %v，want 0（留空才是弹簧那一支）", got)
	}
	if got, want := lyricScrollAnim.Spring.Duration, 200*time.Millisecond; got != want {
		t.Errorf("滚到中间那一段的弹簧时长是 %v，want %v（#470 拍板的）", got, want)
	}
	if got := lyricScrollAnim.Spring.Bounce; got != 0 {
		t.Errorf("滚到中间那一段的 Bounce 是 %v，want 0（不过冲）", got)
	}
	if got, want := lyricScrollAnim.Spring.Precision, 0.05; got != want {
		t.Errorf("滚到中间那一段的 Precision 是 %v，want %v（它决定弹簧什么时候收手）",
			got, want)
	}
	// 邻居不许跟着动。
	if got, want := lyricLineAnim.Duration, 300*time.Millisecond; got != want {
		t.Errorf("行内渐变的时长是 %v，want %v（那是逐字抄上游 Biu 的）", got, want)
	}
	if got, want := motion.Snap.Duration, time.Nanosecond; got != want {
		t.Errorf("窗口刚恢复那一帧的时长是 %v，want %v（一次到位）", got, want)
	}
}

// TestTheLyricScrollArrivesInTwoHundredMilliseconds 换句之后 200ms 就滚到中间。
//
// ⚠️ **判据是「200ms 时走完了多少」，不是「第几帧到位」**：弹簧是按**时间**推进
// 的，而帧间隔是测试自己定的（数帧那种等法会静默失效）。
//
// ⚠️ **为什么这条会红**：那条 500ms 的弹簧在 200ms 时只走完**七成**
// （实算 71.5%，界面上量到 70.1%——见 `internal/ui/lyrics.go` 里
// `lyricScrollDuration` 那段注释），而这条要求九成以上——所以「改回 500ms」
// 这一件事在这里是**看得见的**。
//
// ⚠️ **走完 100% 出头是正常的**：目标每一帧按当下的几何重新对一次（见
// `lyricsView.centre`），而几何是上一帧量到的，于是落点会略微越过终点一点。
func TestTheLyricScrollArrivesInTwoHundredMilliseconds(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	from := s.lyric.scroll.Y
	// 换到第 15 句（离得够远，那一下才看得出快慢），不等它落定。
	s.app.SeekTo(15 * lyricStepMS * time.Millisecond)
	for range 4 {
		tt.Advance(50 * time.Millisecond)
		tt.Frame()
	}
	mid := s.lyric.scroll.Y
	// 让它落定，量终点。
	settleLyrics(t, s, tt)
	to := s.lyric.scroll.Y

	if to == from {
		t.Fatalf("这一趟压根没滚（Y=%v → %v），后面那一步就验不到", from, to)
	}
	covered := float64(mid-from) / float64(to-from)
	t.Logf("200ms 时走完 %.1f%%（Y %.2f → %.2f，终点 %.2f）", 100*covered, from, mid, to)
	if covered < 0.9 {
		t.Errorf("200ms 只走完 %.1f%%，want ≥ 90%%（500ms 那条弹簧在 200ms 时只有七成）",
			100*covered)
	}
}

// TestTheLyricScrollKeepsGoingWhenInterrupted 打断的时候它**接着走**。
//
// # 这一条钉的是弹簧的那一条性质
//
// 弹簧保留速度（`mygo/ui/motion.go` 的 `channel.retarget`：弹簧那一支不动
// `ch.velocity`），而定时长的曲线打断会「速度归零重新起跑」——用户看到的是
// 一下顿挫（俗称「甩」）。
//
// # 怎么量
//
// 先让它**往上**飞（滚到第 15 句），飞到一半把目标换成**下面**那一句（第 2 句）：
//
//	弹簧   还在往上冲一小段（速度是它自己的），然后才折回来
//	曲线   立刻掉头往回走
//
// 所以判据是「打断之后的第一帧，它还在往上走」。
func TestTheLyricScrollKeepsGoingWhenInterrupted(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	// 往上飞。
	s.app.SeekTo(15 * lyricStepMS * time.Millisecond)
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	before := s.lyric.scroll.Y

	// 打断：目标换成下面那一句。
	s.app.SeekTo(2 * lyricStepMS * time.Millisecond)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	after := s.lyric.scroll.Y

	t.Logf("打断前 Y=%.2f，打断后第一帧 Y=%.2f（%+.2f）", before, after, after-before)
	if after <= before {
		t.Errorf("打断之后它立刻掉头了（%.2f → %.2f）——速度被清零了，那是曲线的性质，"+
			"不是弹簧的", before, after)
	}
	// 它总得回来：这一句不许变成「一路往上不回头」。
	settleLyrics(t, s, tt)
	if s.lyric.scroll.Y >= before {
		t.Errorf("打断之后它停在 %.2f（比打断前的 %.2f 还靠下）——没回到第 2 句那儿",
			s.lyric.scroll.Y, before)
	}
}
