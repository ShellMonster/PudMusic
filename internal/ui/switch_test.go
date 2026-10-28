package ui

// 设置开关：旋钮拨过去会**弹一下**，效果看不见的那几格会说一句短提示。
//
// # 判据是**数出来的**，不是「等了多少帧」
//
// 帧数那种判据在机器忙的时候会飘（AGENTS.md §2），而这里量的是「旋钮那颗白圆
// 在轨道哪一半」——一个像素一个像素数出来的死数。
//
// # ⚠️ **深色底下量**
//
// `knobHalves`（`fullplayer_glass_test.go`）数的是**够白**的像素（门槛 190）：
// 旋钮是纯白，而**关态的轨道在浅色底下是浅灰**（≈208）——浅色底下连轨道都算成
// 「白旋钮」了，两个半边都非零，这一族断言在浅色底下**量不出东西**。
// 深色底下关态轨道是深灰（≈90），只有旋钮过得了那个门槛。

import (
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// switchKnobAt 是旋钮这一帧在轨道哪一半：「左」「右」，或者「半路」。
func switchKnobAt(t *testing.T, tt *mygoui.Tester, box mygoui.Rect) string {
	t.Helper()
	left, right := knobHalves(tt.Image(), box)
	switch {
	case left > 0 && right == 0:
		return "左"
	case right > 0 && left == 0:
		return "右"
	case left > 0 && right > 0:
		return "半路"
	default:
		return "没有"
	}
}

// TestTheSettingsSwitchSpringsItsKnobOver 钉「旋钮是**弹**过去的，不是啪一下跳过去」。
//
// 三段：
//
//  1. 按下去那一帧，旋钮**还在左半边**——它要飞过去，不是瞬移；
//  2. 推 100ms，旋钮在**半路**上（两头都有白像素）——它在动；
//  3. 推到底，旋钮在**右半边**——它到了。
//
// ⚠️ **第二段是这一条的关键**：如果旋钮每帧从头开始（也就是「每帧换一个新节点」
// 那个坑），它会一直待在左半边；而如果它是瞬移的，第二段会变成「右」。
// 只有「真的在飞」才是两头都有白像素。
func TestTheSettingsSwitchSpringsItsKnobOver(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(true))

	// ⚠️ **量的是「封面取色」那一颗**，不是「音量均衡」：音量均衡拨开之后会弹
	// 一句提示，而**提示就浮在右下角**（356 宽、抬过播放条 90）——设置页的控件列
	// 正在右边，于是**提示把开关盖住了**（实测：提示那一块 y ∈ [666, 710]，
	// 开关在 y=686）。「封面取色」的效果看得见，**不弹提示**，旋钮量得出来。
	//
	// ⚠️ **它默认是关的**（`CoverAccent` 默认关），而「显示搜索历史」默认是开的
	// ——量一个默认就开着的开关，起点在右边。
	sw := settingRect(t, tt, s, "开关：封面取色")
	if at := switchKnobAt(t, tt, sw); at != "左" {
		t.Fatalf("开关关着的时候旋钮在%s（want 左）", at)
	}

	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()
	if at := switchKnobAt(t, tt, sw); at != "左" {
		t.Fatalf("按下去那一帧旋钮已经在%s了——它是啪一下跳过去的，不是弹过去的", at)
	}

	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if at := switchKnobAt(t, tt, sw); at != "半路" {
		t.Fatalf("推了 100ms 之后旋钮在%s（want 半路）——它没有在动", at)
	}

	tt.Advance(600 * time.Millisecond)
	tt.Frame()
	if at := switchKnobAt(t, tt, sw); at != "右" {
		t.Fatalf("弹完了旋钮在%s（want 右）", at)
	}
}

// TestTurningOnAnInvisibleEffectSaysOneShortLine 钉那一句提示**逐字**是什么。
//
// ⚠️ **逐字**：提示是用户唯一能看到的「这一格开了会怎样」，改一个字就是另一句
// 承诺了。
func TestTurningOnAnInvisibleEffectSaysOneShortLine(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	sw := settingRect(t, tt, s, "开关：音量均衡")
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()

	want := "音量均衡：太响的歌会压下来"
	if !hasExactText(tt, want) {
		t.Errorf("拨开「音量均衡」之后没有那句提示（want %q），文字有 %q", want, tt.Texts())
	}
	// ⚠️ **一句，不是一段**：提示里不许出现第二个句号/问号/叹号。
	if n := strings.Count(want, "。") + strings.Count(want, "？") + strings.Count(want, "！"); n != 0 {
		t.Errorf("那一句提示 %q 里有 %d 个句号——它该是一句短句，不是一段话", want, n)
	}
}

// TestAVisibleEffectSaysNothing 钉「看得见的那些格不弹」。
//
// 判据见 `switch_hint.go`：这一格的效果用户自己看得见，弹一句就是噪音。
func TestAVisibleEffectSaysNothing(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	sw := settingRect(t, tt, s, "开关：封面取色")
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()

	if n := toastCount(s); n != 0 {
		t.Errorf("拨开「封面取色」（效果看得见）之后弹了 %d 条提示，want 0", n)
	}
}

// TestTurningOffSaysNothing 钉「只有打开的时候才说」。
//
// 这一句说的是「开了它会怎样」——关掉的时候说同一句就是错的。
func TestTurningOffSaysNothing(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	sw := settingRect(t, tt, s, "开关：音量均衡")
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()
	if n := toastCount(s); n != 1 {
		t.Fatalf("拨开「音量均衡」之后有 %d 条提示，want 1", n)
	}

	// 再拨一次 = 关掉。
	tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
	tt.Frame()
	if n := toastCount(s); n != 1 {
		t.Errorf("关掉「音量均衡」之后有 %d 条提示，want 还是 1（关掉的时候不该说）", n)
	}
}

// toastCount 是屏幕上现在有几条提示。
//
// ⚠️ **要拿锁**：`s.toasts` 是后台那条 goroutine 也要读的（`expire` 靠它决定收
// 不收），不锁着读就是数据竞争。
func toastCount(s *Shell) int {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	return len(s.toasts)
}
