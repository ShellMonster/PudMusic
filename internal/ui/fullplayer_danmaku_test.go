// 满屏播放器封面上的弹幕：画在哪儿、怎么动、能不能关。
//
// 走 MyGo 的 Tester：和真窗口一模一样的 layout 与 paint，只是画到内存里。
// 所以「弹幕压在封面上」「弹幕是滑过去的而不是瞬移的」这些都是**画面上的事**，
// 不是「调了哪个函数」。
//
// # 判据为什么大多是**像素**
//
// 位移是 `AnimatePose` 走的，而 pose 的中间帧是**只重绘**的——那一帧**不重建
// 视图**，于是 Tester 的查找索引（`tt.Find` / `tt.Texts` 那一套）停在上一帧的
// 位置上（`mygo/ui/runtime.go` 的 `rt.labels` 只在 commit 时重算）。
//
// ⇒ 量「弹幕现在在哪儿」只能量**画面上的像素**。
package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 满屏播放器封面那一块在 1200×800 的窗里的位置（报告 §3.2）。
//
// ⚠️ **这些数是写死的字面量**，不去用 `fullplayer.go` 里的同名常量——
// 布局和期望都用它的话，常量一改两边一起跟着变，测试就发现不了了。
const (
	danmakuCoverX = 192
	danmakuCoverY = 265
	danmakuCoverW = 360
	danmakuCoverH = 270
)

// danmakuRed 是测试里弹幕用的颜色：纯红。
//
// ⚠️ **用红是为了能从画面上认出来**：弹幕自己的颜色是 B 站给的十进制 RGB，
// 而测试要量的是「它在哪儿」，所以挑一个画面上别处不会有的颜色。
const danmakuRed = 0xFF0000

// danmakuEnv 是弹幕那一组测试的外壳。
type danmakuEnv struct {
	s  *Shell
	tt *mygoui.Tester
	// st 是喂给满屏播放器的那份状态（位置在它身上）。
	st app.State
}

// danmakuShell 造一个「满屏开着、位置在 env.st 上」的界面，并摆一份弹幕。
//
// ⚠️ **只画满屏那一块**（`withView`）：整窗的分配里混着首页、侧栏和播放条，
// 而这一组要量的是弹幕那一块。
func danmakuShell(t *testing.T, items []app.DanmakuItem, opts ...shellOpt) *danmakuEnv {
	t.Helper()
	env := &danmakuEnv{st: budgetState()}
	opts = append([]shellOpt{withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.viewFullPlayer(c, env.st)
		}
	})}, opts...)
	env.s, env.tt = newShell(t, opts...)
	env.s.full.open = true
	env.s.app.SetDanmakuForTest(items)
	return env
}

// danmakuItem 造一条弹幕（省得每处都写全五个字段）。
func danmakuItem(id string, timeMS int64, text string) app.DanmakuItem {
	return app.DanmakuItem{
		ID: id, TimeMS: timeMS, Mode: 1, FontSize: 25, Color: danmakuRed, Text: text,
	}
}

// danmakuArea 是封面那一块（测试里量像素的范围）。
func danmakuArea() mygoui.Rect {
	return mygoui.Rect{
		X: danmakuCoverX, Y: danmakuCoverY, W: danmakuCoverW, H: danmakuCoverH,
	}
}

// redSpan 报画面上某一条轨道里那些红像素的范围（最左 / 最右）。
//
// ⚠️ **它扫的是画面，不是树**：弹幕的位置是 pose 走的，而只重绘的那一帧不重建
// 视图 ⇒ Tester 的查找索引停在上一帧（见文件头那一段）。像素是唯一说真话的。
func redSpan(tt *mygoui.Tester, area mygoui.Rect, lane int) (int, int) {
	img := tt.Image()
	top := int(area.Y) + lane*int(danmakuTrackHeight)
	bottom := top + int(danmakuTrackHeight)
	left, right := -1, -1
	for y := top; y < bottom && y < img.Rect.Max.Y; y++ {
		for x := int(area.X); x < int(area.X+area.W) && x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 120 && p.G < 90 && p.B < 90 {
				if left < 0 {
					left = x
				}
				right = x
			}
		}
	}
	return left, right
}

// TestTheDanmakuIsDrawnOnTheCoverOfTheFullPlayer 钉住「弹幕画在满屏左列的封面
// 那一块上」。
//
// # 判据是像素，而且量的是**那一块**
//
// 弹幕在浮层里（`ui.Overlay`），而浮层是压在主树之上的——所以「它建在树上」
// 不等于「它看得见」（见下一条）。这一条量的是**画面上有没有那些红像素**，
// 以及它们落在不在封面那一块里（x=192..552、y=265..535）。
func TestTheDanmakuIsDrawnOnTheCoverOfTheFullPlayer(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()

	left, right := redSpan(env.tt, danmakuArea(), 0)
	if left < 0 {
		t.Fatalf("画面上没有弹幕（封面那一块里一个红像素都没有），文字有 %q", env.tt.Texts())
	}
	if left < danmakuCoverX || right > danmakuCoverX+danmakuCoverW {
		t.Errorf("弹幕画在 x=%d..%d，而封面那一块是 x=%d..%d——它不在封面上",
			left, right, danmakuCoverX, danmakuCoverX+danmakuCoverW)
	}
	// 纵向：第一条轨道的顶就是封面那一块的顶（轨道高 26，字 18）。
	if got := env.s.full.coverRect; got.Y != danmakuCoverY || got.X != danmakuCoverX {
		t.Errorf("封面那一块是 %v，want x=%d y=%d", got, danmakuCoverX, danmakuCoverY)
	}
}

// TestTheDanmakuUnderTheFullPlayerWouldNotBeSeen 是上一条的**反证**。
//
// # 它钉的是哪一个坑
//
// 满屏播放器整个是浮层，而浮层画在主树**之上**——弹幕画在主树里的话，满屏那层
// 不透明的底会把它整个盖住。
//
// ⚠️ **那个症状是静默的**：文字**在树里**（`tt.Texts()` 查得到），而画面上一个
// 像素也没有。所以只验「树上有没有那几行字」是验不出这个坑的——得看画面。
//
// ⇒ 这里**故意**把弹幕画在主树里（不走 `ui.Overlay`），然后断言画面上**没有**
// 那些红像素。
func TestTheDanmakuUnderTheFullPlayerWouldNotBeSeen(t *testing.T) {
	env := &danmakuEnv{st: budgetState()}
	env.s, env.tt = newShell(t, systemIsDark(true), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.viewFullPlayer(c, env.st)
			// ⚠️ **故意画在主树里**：满屏那一层是浮层，压在上面。
			s.drawFullDanmaku(c, env.st, s.full.coverRect)
		}
	}))
	env.s.full.open = true
	// ⚠️ **把「正确的那一份」关掉**：不然浮层里那份弹幕也会画出来，画面上的
	// 红像素就分不清是**主树里那一份**还是浮层里那一份了——这一条要证的正是
	// 「主树里那一份看不见」。
	prefs := env.s.app.FullPlayerPrefs()
	prefs.ShowDanmaku = false
	env.s.app.SetFullPlayerPrefs(prefs)
	env.s.app.SetDanmakuForTest([]app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	})
	env.tt.Frame()

	// 树上**有**那行字（所以「它没画」不是「它压根没建」）。
	if !env.tt.HasText("第一条弹幕") {
		t.Fatalf("弹幕压根没建，文字有 %q", env.tt.Texts())
	}
	// 而画面上**没有**它——被满屏那层不透明的底盖住了。
	left, _ := redSpan(env.tt, danmakuArea(), 0)
	if left >= 0 {
		t.Errorf("画在主树里的弹幕在画面上看得见（最左的红像素在 x=%d）——"+
			"它应该被满屏那一层不透明的底盖住", left)
	}
}

// TestTheDanmakuStartsFromTheCurrentPosition 钉住「按当前位置滚」。
//
// # 为什么这条要紧
//
// 弹幕的时间是**视频时间**，我们放的就是这个视频的音轨 ⇒ 位置就是弹幕的时间轴，
// 没有额外 offset（报告 §2）。
//
// ⇒ 「从第 3 分钟开始听」就从第 3 分钟那一条开始滚——**不是**从「播放开始后的
// 第几分钟」滚。这一条包括应用自己的「恢复上次听到哪儿」（那时位置就是一个非零
// 数）。
func TestTheDanmakuStartsFromTheCurrentPosition(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("near", 180000, "第三分钟那一条"),
		danmakuItem("far", 5000, "开头那一条"),
	}, systemIsDark(true))
	// 位置在第 3 分钟。
	env.st.Position = 3 * time.Minute
	env.tt.Frame()

	if !env.tt.HasText("第三分钟那一条") {
		t.Errorf("第 3 分钟那一条没有画出来，文字有 %q", env.tt.Texts())
	}
	if env.tt.HasText("开头那一条") {
		t.Error("开头那一条还在画面上——它是按「播放了多久」而不是按当前位置滚的")
	}
}

// TestTheDanmakuGlidesInsteadOfTeleporting 钉住「位移走 `AnimatePose`」。
//
// # 它量的是「跳一下之后它怎么过去」
//
// 位置要是**算在 view 里**（`Absolute().Left(x)`），它就只会在重建的时候更新
// ——而满屏开着的时候界面 20 帧/秒重建，于是弹幕是 20fps 跳着走的。
//
// ⇒ 位移走 `AnimatePose`（「只重绘」那一族）：view 只给**目标**，元素自己在只重绘
// 的帧里走到那儿。
//
// # 判据：跳一下之后**它没有瞬移**
//
// 位置跳 500 毫秒 ⇒ 目标挪 30 像素。位移走 pose 的话，重建那一帧元素**还在老的
// 位置**（曲线刚起步），推过 100 毫秒才到新的位置；位移算在 view 里的话，
// 重建那一帧它就已经在新的位置上了。
//
// ⚠️ **量的是像素**：只重绘的那一帧**不重建视图**，Tester 的查找索引停在上一帧
// （见文件头那一段）。
func TestTheDanmakuGlidesInsteadOfTeleporting(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}

	// 位置往前跳 500 毫秒（像用户拖了进度条）。
	env.st.Position += 500 * time.Millisecond
	env.tt.Frame()
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if after != before {
		t.Errorf("位置跳了 500 毫秒之后弹幕**瞬移**了（左边沿 %d → %d）——"+
			"位移是算在 view 里的，于是它 20fps 跳着走；"+
			"它应该走 `AnimatePose`，滑过去", before, after)
	}

	// 推过那一段动画（100 毫秒）之后，它到新位置了。
	env.tt.Advance(150 * time.Millisecond)
	settled, _ := redSpan(env.tt, danmakuArea(), 0)
	if settled >= before {
		t.Errorf("推过 150 毫秒之后弹幕还在 x=%d（原来 x=%d）——它没有滑到新位置上去",
			settled, before)
	}
}

// TestTheDanmakuJumpsWhenMotionIsReduced 钉住「减少动态效果开着的时候按 20fps
// 跳着走」。
//
// # 为什么不用另写一套
//
// 框架在那个偏好下让 pose 的 X/Y/Scale/Rotate **直接到终点**
// （`mygo/ui/pose.go` 的 `still && i < 4`）⇒ 弹幕变成 20fps 跳着走（实测与「位置
// 算在 view 里」那一档一模一样）。
//
// ⇒ 判据就是上一条的反过来：位置跳一下之后，它**立刻**就在新的位置上了。
func TestTheDanmakuJumpsWhenMotionIsReduced(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true), reduceMotion())
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}

	env.st.Position += 500 * time.Millisecond
	env.tt.Frame()
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if after >= before {
		t.Errorf("「减少动态效果」开着的时候弹幕没有直接跳到新位置"+
			"（左边沿 %d → %d）——它应该一步到位", before, after)
	}
}

// TestEveryDanmakuHasItsOwnKey 钉住「每条弹幕一个稳定且唯一的 key」。
//
// # 它钉的是哪一个坏
//
// pose 的状态是**按元素的 key 记着**的 ⇒ 同一个 key 换一批弹幕会继承上一批的位移，
// 元素直接跑到屏幕外、**一个字都画不出来**（报告 §3.1 实测）。
//
// ⇒ key 用**弹幕自己的 rowid**（`app.DanmakuItem.ID`）。
//
// # 怎么验「key 是 rowid 而不是下标」
//
// 先摆一条**滚了 3 秒**的弹幕（位移 -180），再把整份换成一条**滚了 1 秒**的
// （位移 -60）。
//
//   - key 是 rowid ⇒ 新来的那条是**新元素**，直接落在它自己的目标上（-60），
//     两者的左边沿差 120 像素。
//   - key 是下标   ⇒ 新来的那条**继承了**上一条的位移（-180），还在老位置——
//     左边沿一点不差。
//
// ⚠️ **两条的字一样**：量的是**像素的左边沿**，而字形左边那一撇和字框左沿差
// 几像素（实测 10）——字不一样的话那几像素的差就混进判据里了。
//
// ⚠️ **量的是「换完那一帧」**：位移走 pose 的话，新元素在它自己的第一帧就落在
// 目标上（`mygo/ui/pose.go` 的 `samplePose`：没有上一帧的位姿就「shows there at
// once」）；而继承下来的位姿会**从老位置起步**滑过去——那正是「不瞬移」。
func TestEveryDanmakuHasItsOwnKey(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("old", 39000, "同一条字"),
	}, systemIsDark(true))
	env.tt.Frame()
	old, _ := redSpan(env.tt, danmakuArea(), 0)
	if old < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}

	// 换成一份新的：一条滚了 1 秒的（时间 = 当前位置 - 1 秒）。
	env.s.app.SetDanmakuForTest([]app.DanmakuItem{
		danmakuItem("new", 41000, "同一条字"),
	})
	env.tt.Frame()
	fresh, _ := redSpan(env.tt, danmakuArea(), 0)
	if fresh < 0 {
		t.Fatalf("换了一份之后画面上没有弹幕，文字有 %q", env.tt.Texts())
	}
	// 差 120 像素（滚了 3 秒 vs 滚了 1 秒 ⇒ 2 秒 × 60 像素/秒）。
	if got := fresh - old; got < 117 || got > 123 {
		t.Errorf("换了一份之后新弹幕的左边沿在 x=%d，原来那条在 x=%d（差 %d，"+
			"want 120 上下）——差 0 说明它继承了上一条的位移，"+
			"说明 key 不是弹幕自己的 rowid", fresh, old, got)
	}
}

// TestTheDanmakuCanBeTurnedOff 钉住「设置里能整个关掉」。
//
// ⚠️ **关掉的时候一个字都不建**：不是「画了但是透明」——那一层浮层整个不建。
func TestTheDanmakuCanBeTurnedOff(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	if left, _ := redSpan(env.tt, danmakuArea(), 0); left < 0 {
		t.Fatalf("默认没有弹幕，文字有 %q", env.tt.Texts())
	}

	prefs := env.s.app.FullPlayerPrefs()
	if !prefs.ShowDanmaku {
		t.Error("弹幕默认不是开着的——产品要求的是默认开")
	}
	prefs.ShowDanmaku = false
	env.s.app.SetFullPlayerPrefs(prefs)
	env.tt.Frame()
	if left, _ := redSpan(env.tt, danmakuArea(), 0); left >= 0 {
		t.Errorf("关掉之后弹幕还在画面上（最左的红像素在 x=%d）", left)
	}
	if env.tt.HasText("第一条弹幕") {
		t.Errorf("关掉之后那行字还在树上，文字有 %q", env.tt.Texts())
	}
}

// TestTheFullPlayerSettingsHasTheDanmakuSwitch 钉住设置里那一格。
func TestTheFullPlayerSettingsHasTheDanmakuSwitch(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("显示弹幕") {
		t.Fatalf("偏好面板里没有「显示弹幕」那一格，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("开关：显示弹幕"); !ok {
		t.Fatalf("找不到那个开关，文字有 %q", tt.Texts())
	}
}
