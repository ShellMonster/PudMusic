package ui

// 视觉审查出图（本轮新 UI 三件：⌘K 命令面板、设置页均衡器自定义、满屏均衡器）。
//
//	BIU_SHOT_DIR=/tmp/pud-shots go test ./internal/ui -run TestRenderNewUIShots
//
// 这三张图是给人看的材料，不是断言：五项审查（布局/间距/层级/密度/对齐）
// 由人对着图核（docs/agents/visual-review.md）。
//
// 标尺：命令面板是首创（上游 Biu 没有），对照「VS Code/Raycast 顶上 1/6 处
// 的命令框」；EQ 面板对照「设置页行内弹层」的既有 Popover 样式；满屏 EQ
// 对照同一面板在偏好浮层里的嵌套层级。

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// dumpGeom 把 label 命中的元素的框打出来（视觉审查的「量，不目测」那一半）。
func dumpGeom(t *testing.T, tt *mygoui.Tester, labels ...string) {
	t.Helper()
	for _, l := range labels {
		if r, ok := tt.Find(l); ok {
			t.Logf("%-24q X=%.0f Y=%.0f W=%.0f H=%.0f", l, r.X, r.Y, r.W, r.H)
		} else {
			t.Logf("%-24q <未命中>", l)
		}
	}
}

func TestRenderNewUIShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	// 1. ⌘K 命令面板（空词，列全部命令+页面）。
	t.Run("command-palette", func(t *testing.T) {
		b := newTestShell(t)
		tt := b.tt
		tt.Frame()
		tt.Key(mygoui.Cmd, mygoui.KeyK)
		tt.Frame()
		tt.Advance(300 * time.Millisecond) // 进场动画走完
		tt.Frame()
		save(t, dir, "palette-empty.png", tt)
		img := tt.Image()
		t.Logf("窗口 %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
		dumpGeom(t, tt, paletteLabel, paletteInputLabel, "播放/暂停", "去设置")
		b.Close()
	})

	// 2. ⌘K 命令面板（敲词收窄后）。
	t.Run("command-palette-filtered", func(t *testing.T) {
		b := newTestShell(t)
		s, tt := b.s, b.tt
		tt.Frame()
		tt.Key(mygoui.Cmd, mygoui.KeyK)
		tt.Frame()
		if !s.paletteOpen {
			t.Fatal("面板没开")
		}
		tt.Type("播放")
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		save(t, dir, "palette-filtered.png", tt)
		dumpGeom(t, tt, paletteLabel, paletteInputLabel, "播放/暂停")
		var visible []string
		for _, txt := range tt.Texts() {
			if strings.HasPrefix(txt, "去") || strings.Contains(txt, "播放") {
				visible = append(visible, txt)
			}
		}
		t.Logf("过滤后可见条目 %v", visible)
		b.Close()
	})

	// 3. 设置页「均衡器」自定义弹层（十段滑杆）。
	t.Run("eq-panel-settings", func(t *testing.T) {
		want := app.DefaultSettings("")
		want.EQPreset = "bass"
		tt, s, _, _ := shellOnSettings(t, want)
		tt.Frame()
		openEQPanel(t, tt, s)
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		save(t, dir, "eq-panel-settings.png", tt)
		dumpGeom(t, tt, "均衡器面板", "均衡器 31 Hz", "均衡器 16000 Hz", "均衡器预设名称", "重置")
	})

	// 4. 满屏播放器偏好里的「均衡器」弹层。
	t.Run("eq-panel-fullplayer", func(t *testing.T) {
		s, tt := onlineTester(t, nil, false)
		s.openFullPlayer()
		tt.Frame()
		s.full.popEQ = true
		tt.Frame()
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		save(t, dir, "eq-panel-fullplayer.png", tt)
		dumpGeom(t, tt, "full-pop-eq", "31Hz 频段", "16000Hz 频段")
	})
}
