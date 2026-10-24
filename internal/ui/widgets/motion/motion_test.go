package motion

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组是**动效值表**的护栏：曲线和弹簧的值不许漂。
//
// ⚠️ **这两个值是全应用动效的唯一出处**：曲线散在五处调用点上，弹簧散在
// 主 Tab 那四处 `AnimateTo` 上——改了这里，所有用到的地方一起变。

// TestTheUnifiedCurveIsTheOneWeCopied 把统一曲线的值钉死。
//
// 出处：《回顾: 制作微信广告官网》——「统一选择 `0.8s` 与贝塞尔控制点
// `(0.16, 0.9, 0.21, 0.98)`」（https://wxad.design/abc/wxad）。
//
// ⚠️ **顺带钉住它的形状**：这条曲线是「前重后轻」的（前 50% 的时间走完
// 95.4%），而框架默认的 `EaseOut` 是 1-(1-t)³（前 50% 走完 87.5%）。
// 两者的差别肉眼看得出来——「是不是同一条曲线」这件事，靠的就是这几个数。
func TestTheUnifiedCurveIsTheOneWeCopied(t *testing.T) {
	// 端点必须是端点。
	if got := Unified(0); got != 0 {
		t.Errorf("曲线在 0 处是 %v，want 0", got)
	}
	if got := Unified(1); got != 1 {
		t.Errorf("曲线在 1 处是 %v，want 1", got)
	}
	// 中点：0.954（= 原文说的「前 50% 完成 95%」）。
	if got := Unified(0.5); got < 0.94 || got > 0.97 {
		t.Errorf("曲线在一半处是 %.4f，want 0.954（原文说前 50%% 的时间走完 95%%）", got)
	}
	// 前重后轻：四分之一处已经走了八成多（`EaseOut` 那时只有 57.8%）。
	if got := Unified(0.25); got < 0.80 || got > 0.83 {
		t.Errorf("曲线在四分之一处是 %.4f，want 0.815", got)
	}
	// 单调不减。
	prev := float32(0)
	for i := 1; i <= 100; i++ {
		got := Unified(float32(i) / 100)
		if got < prev {
			t.Fatalf("曲线在 %d%% 处回落了（%v < %v）——它不是一条能用的缓动", i, got, prev)
		}
		prev = got
	}
}

// TestTheTabSlideIsACriticalSpring 把主 Tab 滑块那一下的值钉死。
//
// ⚠️ **它是弹簧不是曲线**（`Duration` 必须是 0：Duration > 0 就是曲线那一支，
// 见 `mygo/ui/motion.go` 的 `retarget`）——曲线中途换目标会从当前值重新起速，
// 弹簧保留速度。
func TestTheTabSlideIsACriticalSpring(t *testing.T) {
	if TabSlide.Duration != 0 {
		t.Errorf("Tab 滑块有 Duration %v——那一条是曲线，不是弹簧", TabSlide.Duration)
	}
	want := ui.SpringOptions{Stiffness: 260, Damping: 32, Precision: 0.5}
	got := TabSlide.Spring
	if got.Stiffness != want.Stiffness {
		t.Errorf("刚度是 %v，want %v（出处：《使用 AI 制作交互动画的建议》的 demo）",
			got.Stiffness, want.Stiffness)
	}
	if got.Damping != want.Damping {
		t.Errorf("阻尼是 %v，want %v（demo 原值 26 会回弹 2%%，按手册⓪「not springy」上调）",
			got.Damping, want.Damping)
	}
	if got.Precision != want.Precision {
		t.Errorf("精度是 %v，want %v（框架默认的 0.001 会让弹簧多跑 400ms 才收手）",
			got.Precision, want.Precision)
	}
	if got.Mass != 0 {
		t.Errorf("质量是 %v，want 0（框架兜底 1，和 react-spring 一致）", got.Mass)
	}
}
