package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
	mygoui "github.com/egoist/mygo/ui"
)

// 均衡器自定义这一组：拖一段 → 设置里那份增益变了且 EQCustomOn 开了；
// 存预设 → 下拉里多一套、重启还在；预设切换 → 生效的增益跟着换。

// shellWithEQPanel 造一个只摆着均衡器滑杆面板的无头窗口。
//
// 设置页里的「均衡器」入口已经删了（原型里 EQ 在满屏播放器，归满屏那一边接），面板本身
// （`eqPanelContent`）和它写的设置不变，所以这里直接把它摆在一个小窗口里验。
func shellWithEQPanel(t *testing.T, st app.Settings) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()
	b := newTestShell(t, withSettings(st))
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		b.s.applyFrameTheme(c)
		mygoui.Column(c).Width(560).Padding(16).Children(func() {
			b.s.eqPanelContent(c, b.a.SettingsSnapshot(), "", false)
		})
	}, 640, 560)
	tt.Frame()
	if _, ok := tt.Find("均衡器 31 Hz"); !ok {
		t.Fatalf("均衡器面板没画出来，文字有 %q", tt.Texts())
	}
	return tt, b.s, b.a, b.settingsPath
}

// dragEQSlider 把第 band 段滑杆拖到「从底往上 f 的比例」（0 = -12，1 = +12）。
//
// 上下各留半个钮（`PaddingY(thumb/2)`），和 `dragFullLyricSlider` 同一个算法
// ——那是同一个控件（`lyricSlider`）。
func dragEQSlider(t *testing.T, tt *mygoui.Tester, band int, f float32) {
	t.Helper()
	label := "均衡器 " + eqBandFreq(band) + " Hz"
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("第 %d 段滑杆 %q 不在画面上，文字有 %q", band, label, tt.Texts())
	}
	y := r.Y + r.H - float32(lyricSliderThumb)/2 - (r.H-float32(lyricSliderThumb))*f
	x := r.X + r.W/2
	tt.Press(x, y)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	tt.Release(x, y)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
}

// eqBandFreq 是滑杆 Label 里那个频率数（和 eq_panel.go 里
// `strconv.FormatFloat(..., 'f', -1)` 同一个串）。
func eqBandFreq(band int) string {
	return fmt.Sprintf("%d", int(audio.EQBands[band]))
}

// TestDraggingAnEQBandTurnsOnCustom 验「拖任何一段 → EQCustomOn 开了，
// EQCustomGains 里那一段是拖到的值，其余段是原来那套预设的」。
func TestDraggingAnEQBandTurnsOnCustom(t *testing.T) {
	want := app.DefaultSettings("")
	want.EQPreset = "bass"
	tt, _, a, _ := shellWithEQPanel(t, want)

	// 第 0 段（31 Hz）拖到顶（+12）。bass 预设的第 0 段是 6。
	dragEQSlider(t, tt, 0, 1.0)
	st := a.SettingsSnapshot()
	if !st.EQCustomOn {
		t.Fatal("拖了一段，自定义没开")
	}
	if st.EQCustomGains[0] != 12 {
		t.Errorf("第 0 段该是 12：%v", st.EQCustomGains[0])
	}
	if st.EQCustomGains[1] != 5 {
		t.Errorf("第 1 段该是 bass 的 5：%v", st.EQCustomGains[1])
	}
}

// TestSavingACustomEQPreset 验「存为预设」把当前增益记成一套命名预设，
// 重启之后还在下拉里。
func TestSavingACustomEQPreset(t *testing.T) {
	tt, s, a, path := shellWithEQPanel(t, app.DefaultSettings(""))

	dragEQSlider(t, tt, 0, 1.0)
	r, ok := tt.Find("均衡器预设名称")
	if !ok {
		t.Fatalf("找不到预设名输入框，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Type("深夜")
	tt.Frame()
	if err := tt.Click("存为预设"); err != nil {
		t.Fatalf("存不了预设：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()

	st := a.SettingsSnapshot()
	if len(st.EQCustomPresets) != 1 || st.EQCustomPresets[0].Name != "深夜" {
		t.Fatalf("存的预设是 %v", st.EQCustomPresets)
	}
	if st.EQCustomName != "深夜" || st.EQCustomGains[0] != 12 {
		t.Errorf("存完之后该选中「深夜」：name=%q gains=%v", st.EQCustomName, st.EQCustomGains)
	}
	if !hasToast(s, "已保存预设「深夜」") {
		t.Errorf("存了一套预设没说一句，提示有 %q", dropToastTexts(s))
	}

	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	b := app.New()
	t.Cleanup(func() { b.Close(); _ = b.CloseSettings() })
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	st = b.SettingsSnapshot()
	if len(st.EQCustomPresets) != 1 || st.EQCustomPresets[0].Name != "深夜" {
		t.Errorf("重启之后预设没了：%v", st.EQCustomPresets)
	}
}

// TestSwitchingBetweenEQPresets 验预设切换（`applyEQSelect`）：选「摇滚」写 EQPreset 并把自定义关掉；
// 选一套存的自定义预设把增益换成那一套。预设的下拉入口归满屏播放器，这里验写设置的那一笔。
func TestSwitchingBetweenEQPresets(t *testing.T) {
	st := app.DefaultSettings("")
	st.EQCustomOn = true
	st.EQCustomName = "深夜"
	st.EQCustomGains = [10]float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	st.EQCustomPresets = []audio.EQPreset{
		{ID: "custom:深夜", Name: "深夜", Gains: st.EQCustomGains},
	}
	b := newTestShell(t, withSettings(st))
	b.tt.Frame()

	b.s.applyEQSelect(b.a.SettingsSnapshot(), "rock")
	if got := b.a.SettingsSnapshot(); got.EQPreset != "rock" || got.EQCustomOn {
		t.Errorf("选了摇滚之后是 preset=%q customOn=%v", got.EQPreset, got.EQCustomOn)
	}

	b.s.applyEQSelect(b.a.SettingsSnapshot(), eqCustomPresetValue("深夜"))
	got := b.a.SettingsSnapshot()
	if !got.EQCustomOn || got.EQCustomName != "深夜" || got.EQCustomGains[0] != 1 {
		t.Errorf("选了存的预设之后是 on=%v name=%q gains=%v", got.EQCustomOn, got.EQCustomName, got.EQCustomGains)
	}
}

// TestTheFullPlayerEQEntryOpensItsOwnPop 验满屏底栏右边那枚「均衡器」开出来的弹层：预设下拉 +
// 十段竖滑杆（滑杆身份带 "full:" 前缀，见 eq_panel.go 文件头「键的分域」）。
func TestTheFullPlayerEQEntryOpensItsOwnPop(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	tt.Frame()
	if err := tt.Click("均衡器"); err != nil {
		t.Fatalf("点不了均衡器：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	settleFull(t, tt)
	if !s.full.popEQ {
		t.Fatalf("满屏里没弹出均衡器弹层，文字有 %q", tt.Texts())
	}
	for _, want := range []string{"31Hz 频段", "16000Hz 频段"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("均衡器弹层里找不到 %q，文字 %q", want, tt.Texts())
		}
	}
	if !tt.HasText("恢复平直") {
		t.Errorf("均衡器弹层里没有「恢复平直」：%q", tt.Texts())
	}
}
