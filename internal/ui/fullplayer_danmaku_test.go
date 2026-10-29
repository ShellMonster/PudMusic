// 满屏播放器封面上的弹幕：画在哪儿、怎么动、能不能关。
//
// 走 MyGo 的 Tester：和真窗口一模一样的 layout 与 paint，只是画到内存里。
// 所以「弹幕压在封面上」「弹幕是滑过去的而不是瞬移的」这些都是**画面上的事**，
// 不是「调了哪个函数」。
//
// # 判据为什么**全是像素和模型状态**
//
// 弹幕是一层**自绘的墨迹**（票 #669）：位置和字都在 `Draw` 回调里按
// `Painter.Now()` 算，因为只重绘的那些帧**不重建视图**。于是
//
//   - 「弹幕现在在哪儿」只能量**画面上的像素**（`redSpan` / `greenSpan`）；
//   - 「它该在哪条轨道上」量模型层（`danmakuLayer.slots` / `lanes`）；
//   - **`tt.Texts()` 里没有弹幕的正文**——那不是一个缺陷，弹幕本来就是装饰
//     （`RoleNone`），读屏软件也不念它。
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// danmakuGreen 是测试里第二条弹幕用的颜色：纯绿。
//
// ⚠️ **用它是为了分开量两条弹幕**：同一条轨道上两条弹幕**不能叠字**（那是
// 本票评审提的 critical），而「叠没叠」要能在画面上看出来 ⇒ 两条得是两种
// 颜色。
//
// ⚠️ **画面上别处没有绿色的像素**：封面那一块在测试里是默认封面图，实测
// 那个区域没有「绿得起来」的像素（否则这条测试量的是背景，不是弹幕）。
const danmakuGreen = 0x00FF00

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
	return danmakuItemColor(id, timeMS, text, danmakuRed)
}

// danmakuItemColor 造一条**指定颜色**的弹幕。
func danmakuItemColor(id string, timeMS int64, text string, color uint32) app.DanmakuItem {
	return app.DanmakuItem{
		ID: id, TimeMS: timeMS, Mode: 1, FontSize: 25, Color: color, Text: text,
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

// greenSpan 报画面上某一条轨道里那些**绿色**像素的范围（最左 / 最右）。
//
// ⚠️ **和 `redSpan` 一样是扫画面**：弹幕的位置是 pose 走的，只重绘的那一帧
// 不重建视图 ⇒ 树上的位置是过期的（见文件头那一段）。
func greenSpan(tt *mygoui.Tester, area mygoui.Rect, lane int) (int, int) {
	img := tt.Image()
	top := int(area.Y) + lane*int(danmakuTrackHeight)
	bottom := top + int(danmakuTrackHeight)
	left, right := -1, -1
	for y := top; y < bottom && y < img.Rect.Max.Y; y++ {
		for x := int(area.X); x < int(area.X+area.W) && x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.G > 120 && p.R < 90 && p.B < 90 {
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
// ⚠️ **那个症状是静默的**：画面上一个像素也没有，而没有任何报错。
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
			//
			// 设置里关着弹幕 ⇒ `viewFullPlayer` 那一遍把状态 reset 了；这里
			// 按生产同一条路重建一份，只是**不套 `ui.Overlay`**。
			s.full.danmaku.sync(c, env.st, s.app.Danmaku().Items, s.full.coverRect)
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

	// 这一层真的建了（所以「它没画」不是「它压根没建」）。
	if !env.s.full.danmaku.ready {
		t.Fatal("弹幕层压根没建起来——这一条要验的是「画了但被盖住」")
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
		// ⚠️ **比位置早半秒**：年龄 0 的那条正好压在右边沿上（裁剪之后看不见），
		// 而这一条要验的是「它在画面上」。
		danmakuItem("near", 179500, "第三分钟那一条"),
		danmakuItem("far", 5000, "开头那一条"),
	}, systemIsDark(true))
	// 位置在第 3 分钟。
	env.st.Position = 3 * time.Minute
	env.tt.Frame()

	if !redSeen(env.tt, danmakuArea()) {
		t.Errorf("第 3 分钟那一条没有画出来（封面那一块里一个红像素都没有）")
	}
	// 「开头那一条」要是画了，它会和这一条**同一条轨道**（同一时刻只有它在滚动
	// 窗口里），而画面上那一条墨迹的左沿会跑到封面外面去 —— 所以判据是画面上
	// 只有一条墨迹、而且在封面里。
	for lane := range danmakuTracks {
		if lane == 0 {
			continue
		}
		if l, _ := redSpan(env.tt, danmakuArea(), lane); l >= 0 {
			t.Errorf("第 %d 条轨道上也有墨迹——「开头那一条」还在画面上"+
				"（它是按「播放了多久」而不是按当前位置滚的）", lane)
		}
	}
}

// TestACloserSampleDoesNotJerkTheDanmaku 钉住「几十毫秒的采样误差不会让弹幕
// 抖一下」（票 #669）。
//
// # 它量的是「读数变了，但位置不动」
//
// 弹幕的位置由绘制帧按 `Painter.Now()` 推，而整窗重建时拿到的
// `State.Position` 是播放器 50ms 一拍采回来的。两者差几十毫秒是**正常**的
// ——拿那个差去重钉锚点，弹幕就会在每次整窗重建时抖一下。
//
// ⇒ 判据：把读数往前挪 500 毫秒（远小于 `danmakuSeekSlop` 的 2 秒）、钟不动，
// 弹幕**一动不动**。
func TestACloserSampleDoesNotJerkTheDanmaku(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕")
	}

	// 钟不动，只有采样值往前走 500 毫秒。
	env.st.Position += 500 * time.Millisecond
	env.tt.Frame()
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if after != before {
		t.Errorf("读数差了 500 毫秒（采样误差）就把弹幕挪了 %d 像素（x=%d → x=%d）"+
			"——它会因此每帧抖一下；这么小的差该被当成采样误差", after-before, before, after)
	}

	// 而钟往前推，它就该跟着走（位置由它自己的时钟推）。
	env.tt.Advance(500 * time.Millisecond)
	settled, _ := redSpan(env.tt, danmakuArea(), 0)
	if got := before - settled; got < 26 || got > 34 {
		t.Errorf("推了 500 毫秒之后弹幕走了 %d 像素（want 30）——它没有跟着自己的时钟走", got)
	}
}

// TestTheDanmakuFollowsThePositionWhenMotionIsReduced 钉住「减少动态效果开着
// 的时候弹幕还是跟着位置走，只是不逐帧重画」。
//
// 弹幕的运动**本身就是动效**：那个偏好开着的时候不该按显示器刷新率一直重画
// （`TestReduceMotionDoesNotAskForContinuousFrames` 钉着那一半）。但位置仍然
// 得跟着播放走——否则用户看到的是一秒跳一格的「卡住的弹幕」。
//
// ⇒ 判据：推钟（没有绘制帧）它不动；位置变了（整窗重建）它到新位置。
func TestTheDanmakuFollowsThePositionWhenMotionIsReduced(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true), reduceMotion())
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕")
	}

	// 推钟但不改位置 ⇒ 不动（那个偏好下位置只跟着整窗重建走）。
	env.tt.Advance(500 * time.Millisecond)
	env.tt.Frame()
	if still, _ := redSpan(env.tt, danmakuArea(), 0); still != before {
		t.Errorf("「减少动态效果」开着时推钟就把弹幕挪了（x=%d → x=%d）——"+
			"那个偏好下它不该逐帧重画", before, still)
	}

	// 位置变了 ⇒ 到新位置（60 像素/秒 × 0.5 秒 = 30 像素）。
	env.st.Position += 500 * time.Millisecond
	env.tt.Frame()
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if got := before - after; got < 26 || got > 34 {
		t.Errorf("位置往前 500 毫秒之后弹幕走了 %d 像素（want 30）——"+
			"偏好的只是刷新频率，位置还是得跟着位置走", got)
	}
}

// TestEveryDanmakuHasItsOwnKey 钉住「每条弹幕一个稳定且唯一的 key」。
//
// # 它钉的是哪一个坏
//
// 每条弹幕的 key 必须是**它自己的 rowid**（`app.DanmakuItem.ID`），不是它在
// 列表里的下标：列表一换（换歌、或者 B 站补发/刷新一份新的），下标会整体
// 错配——上一条的状态被下一条继承，位置、轨道全跟着错。
//
// ⚠️ **判据已经从「画面上量位移」改成「模型层的槽位」**：票 #669 之后弹幕是
// 一层自绘的墨迹，位置由绘制帧现算，元素上不再有逐条的 Text/pose 可量。稳定
// key 现在的事实就是 `danmakuLayer.slots`——它按 rowid 记着每一条占的那条
// 轨道（见 `slots` 字段的注释：键是 rowid，不是下标）。
//
// # 怎么验「key 是 rowid 而不是下标」
//
// 摆两条弹幕（一红一绿），让它们各占一条轨道；记下各自的槽位。之后只推时间、
// 重建几帧（位置往前走，但这一份弹幕没换）：
//
//   - 每条弹幕的槽位（轨道、入场时间、存活时长）**逐字段不变**——说明它跨帧
//     仍是同一条，没有因为列表重排或新条目插到前面而换槽位；
//   - 两条的槽位**互不相同**（各占一条轨道）——说明键不是「列表里第几个」。
func TestEveryDanmakuHasItsOwnKey(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItemColor("d1", 40000, "同一条字", danmakuRed),
		danmakuItemColor("d2", 40100, "另一条字", danmakuGreen),
	}, systemIsDark(true))
	env.st.Position = 42 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	first, ok := d.slots["d1"]
	if !ok {
		t.Fatalf("第一条弹幕没有进槽位：%v", d.slots)
	}
	second, ok := d.slots["d2"]
	if !ok {
		t.Fatalf("第二条弹幕没有进槽位：%v", d.slots)
	}
	if first == second {
		t.Fatalf("两条弹幕拿到了一模一样的槽位 %+v——键不是各自的 rowid", first)
	}
	if first.timeMS != 40000 || second.timeMS != 40100 {
		t.Fatalf("槽位记的入场时间不是各自的：d1=%+v d2=%+v", first, second)
	}

	// 只推时间、重建几帧：这一份弹幕没换，每条弹幕的槽位必须原样保留。
	for i := range 3 {
		env.st.Position += 500 * time.Millisecond
		env.tt.Frame()
		if got := d.slots["d1"]; got != first {
			t.Fatalf("第 %d 帧之后 d1 的槽位从 %+v 变成了 %+v——同一条弹幕跨帧换了槽位", i+1, first, got)
		}
		if got := d.slots["d2"]; got != second {
			t.Fatalf("第 %d 帧之后 d2 的槽位从 %+v 变成了 %+v——同一条弹幕跨帧换了槽位", i+1, second, got)
		}
	}
	// 画面上那两条都还在（否则上面那个「槽位没变」可能只是什么都没画）。
	if !redSeen(env.tt, danmakuArea()) || !greenSeen(env.tt, danmakuArea()) {
		t.Errorf("推进之后画面上少了一条弹幕（红=%v 绿=%v）",
			redSeen(env.tt, danmakuArea()), greenSeen(env.tt, danmakuArea()))
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
	if env.s.full.danmaku.ready {
		t.Error("关掉之后弹幕那一层的状态还留着（ready 还是真）——" +
			"设置关掉时要整个复位")
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

// TestTwoDanmakuOnTheSameLaneNeverOverlap 钉住「同一条轨道上的两条弹幕不能
// 叠字」。
//
// # 它钉的是哪一个坏
//
// 报告 §3.3 那条轨道规则是「前一条走出 30% 的宽度才放下一条」（360 × 30% ÷
// 60 像素/秒 = 1.8 秒）。⚠️ **那条规则对长字不够**：一条 144 像素宽的字滚了
// 1.8 秒（走了 108 像素）之后，它的**尾巴还在封面里**，而下一条已经从右边沿
// 进来了 ⇒ **两句话叠在一起**，没法读（评审提的 critical：实测重叠 53.5
// 像素）。
//
// ⇒ 现在多一条**几何**判据：同一条轨道上，前一条的尾巴必须已经过了右边沿
// 那条入场线（两条同速 ⇒ 不叠字当且仅当 Δt × 60 ≥ 前一条的字宽）。
//
// # 判据是像素，而且是两种颜色
//
// 两条弹幕一条红一条绿 ⇒ 画面上能分开量：同一条轨道上**不该**同时出现红色
// 和绿色的墨迹。
func TestTwoDanmakuOnTheSameLaneNeverOverlap(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		// 前一条：**二十个字**（实测墨迹 230 像素上下），40 秒入场。
		//
		// ⚠️ **它得比 108 像素宽**（360 × 30%）：报告 §3.3 那条规则只等
		// 「前一条走出 30% 宽度」= 1.8 秒 = 108 像素 ⇒ 前一条比这宽的话，
		// 它的尾巴还在封面里，后一条就进来了 ⇒ 叠字。
		danmakuItemColor("long", 40000, strings.Repeat("哈", 20), danmakuRed),
		// 后一条：4 个字，**1.9 秒后**入场——报告 §3.3 那条时间门槛放它进来
		//（1.9 > 1.8），而它跟前一条**重叠一百多像素**。
		danmakuItemColor("short", 41900, "哈哈哈哈", danmakuGreen),
	}, systemIsDark(true))
	// 位置 44 秒：前一条滚了 4 秒（左边沿 312），后一条滚了 2.1 秒
	//（左边沿 426 ⇒ 压着前一条那一大片）。
	env.st.Position = 44 * time.Second
	env.tt.Frame()

	// ⚠️ **先验条件**：前一条得**真的比 108 像素宽**，不然这个场景压根不复现
	// 那个坏（测试就白绿了）。量的是画面上的墨迹宽度。
	rl, rr := redSpan(env.tt, danmakuArea(), 0)
	if rl < 0 {
		t.Fatalf("画面上没有前一条弹幕（文字有 %q）", env.tt.Texts())
	}
	if rr-rl <= 108 {
		t.Fatalf("前一条弹幕只有 %d 像素宽，而这一条要复现的坏"+
			"（叠字）要它比 108 像素（360 宽的 30%%）宽——"+
			"换一条更长的字再测", rr-rl)
	}

	// 后一条**得在画面上**（这一条不许靠「把它丢掉」来躲开叠字）。
	greenSeen := false
	for lane := range danmakuTracks {
		rl, rr := redSpan(env.tt, danmakuArea(), lane)
		gl, gr := greenSpan(env.tt, danmakuArea(), lane)
		if gl >= 0 {
			greenSeen = true
		}
		if rl < 0 || gl < 0 {
			continue
		}
		if rl <= gr && gl <= rr {
			t.Errorf("第 %d 条轨道上红、绿两条弹幕**叠在一起**了"+
				"（红 %d..%d，绿 %d..%d）——同轨的两条不能叠字", lane, rl, rr, gl, gr)
		}
	}
	if !greenSeen {
		t.Errorf("后一条弹幕压根没画出来（文字有 %q）——它该换一条轨道，而不是被丢掉",
			env.tt.Texts())
	}
}

// TestTheSameLaneWaitsForThePreviousDanmakuToClearTheEdge 钉住轨道规则**本身**
// （不看画面，只看模型层那条判据）。
//
// # 为什么要两条
//
// 上一条是**画面上的**判据（像素），这一条是**规则上的**：它量的是「两条
// 挨得那么近的时候，能不能同轨」，而那正是评审复现里出的事（lane 0/0，
// 前一条宽 196.3、后一条宽 53.5，1.8 秒后就进来了）。
func TestTheSameLaneWaitsForThePreviousDanmakuToClearTheEdge(t *testing.T) {
	var d danmakuLayer
	// 前一条：196.3 像素宽，40 秒入场。
	a := d.assign(app.DanmakuItem{ID: "a", TimeMS: 40000}, 196.3, 1800, 360)
	if a.lane < 0 {
		t.Fatal("前一条没有轨道（空轨道都放不下它？）")
	}
	// 后一条：53.5 像素宽，**1.8 秒后**入场——报告 §3.3 那条时间门槛放它
	// 进来，而 1.8 秒 × 60 像素/秒 = 108 < 196.3 ⇒ 它俩会叠 88 像素。
	b := d.assign(app.DanmakuItem{ID: "b", TimeMS: 41800}, 53.5, 1800, 360)
	if b.lane < 0 {
		t.Fatal("后一条没有轨道")
	}
	if b.lane == a.lane {
		t.Errorf("两条挨得太近却进了同一条轨道（lane %d）："+
			"1.8 秒里前一条只走了 108 像素，而它自己宽 196.3 ⇒ 叠 %.1f 像素",
			a.lane, 196.3-108)
	}
	// 而「同轨」本身不是错——**同轨的前提是前一条的尾巴过了入场线**。
	// ⇒ 两条**短**的（36 像素宽）隔 1.8 秒就该能同轨（108 > 36）。
	var d2 danmakuLayer
	short := d2.assign(app.DanmakuItem{ID: "s", TimeMS: 40000}, 36, 1800, 360)
	same := d2.assign(app.DanmakuItem{ID: "n", TimeMS: 41800}, 36, 1800, 360)
	if short.lane != same.lane {
		t.Errorf("两条短弹幕（36 像素宽、隔了 1.8 秒）没有进同一条轨道"+
			"（lane %d 与 %d）——它们本该能同轨（1.8 秒 × 60 = 108 > 36）",
			short.lane, same.lane)
	}
}

// TestALongDanmakuStaysUntilItHasLeftTheCover 钉住「长弹幕不会提前消失」。
//
// # 它钉的是哪一个坏
//
// 之前有一个 **15 秒的硬上限**在按字宽判断出界**之前**先滤掉了长弹幕：
// 40 字（713.7 像素宽）的那条在第 17 秒整条不见了——而按 60 像素/秒，它的
// 右缘（245.7）**还在封面里**（评审提的 major）。
//
// ⇒ 现在粗筛只是**上界**（按字数估的「早就出界了没有」），真正的判据是量
// 出来的字宽：(封面宽 + 字宽) ÷ 速度。
func TestALongDanmakuStaysUntilItHasLeftTheCover(t *testing.T) {
	long := strings.Repeat("长弹幕", 13) + "啊" // 40 个字
	env := danmakuShell(t, []app.DanmakuItem{
		danmakuItem("long", 40000, long),
	}, systemIsDark(true))

	// 第 17 秒：右缘（245.7）还在封面里 ⇒ 它得看得见。
	env.st.Position = 57 * time.Second
	env.tt.Frame()
	left, right := redSpan(env.tt, danmakuArea(), 0)
	if left < 0 {
		t.Fatalf("第 17 秒时长弹幕不见了（文字有 %q）——它的右缘还在封面里"+
			"（按 60 像素/秒算），不该这么早消失", env.tt.Texts())
	}
	if right <= danmakuCoverX {
		t.Errorf("第 17 秒时长弹幕的墨迹在 x=%d..%d，而封面是 x=%d..%d——"+
			"它已经整个走出去了？", left, right, danmakuCoverX, danmakuCoverX+danmakuCoverW)
	}

	// 第 19 秒：它整个出界了 ⇒ 不该再看得到。
	env.st.Position = 59 * time.Second
	env.tt.Frame()
	if l, _ := redSpan(env.tt, danmakuArea(), 0); l >= 0 {
		t.Errorf("第 19 秒时长弹幕还在画面上（最左的红像素在 x=%d）——"+
			"它早就整个走出封面了", l)
	}
}
