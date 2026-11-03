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

// openEQPanel 点开设置页「均衡器」行的「自定义」键，把滑杆面板摆出来。
func openEQPanel(t *testing.T, tt *mygoui.Tester, s *Shell) {
	t.Helper()
	r := settingRect(t, tt, s, "自定义")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if _, ok := tt.Find("均衡器面板"); !ok {
		t.Fatalf("自定义均衡器面板没开，文字有 %q", tt.Texts())
	}
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
	tt, s, a, _ := shellOnSettings(t, want)
	tt.Frame()
	openEQPanel(t, tt, s)

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
	tt, s, a, path := shellOnSettings(t, app.DefaultSettings(""))
	tt.Frame()
	openEQPanel(t, tt, s)

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

// TestSwitchingBetweenEQPresets 验预设切换：选「摇滚」写 EQPreset 并把
// 自定义关掉；选一套存的自定义预设把增益换成那一套。
func TestSwitchingBetweenEQPresets(t *testing.T) {
	st := app.DefaultSettings("")
	st.EQCustomOn = true
	st.EQCustomName = "深夜"
	st.EQCustomGains = [10]float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	st.EQCustomPresets = []audio.EQPreset{
		{ID: "custom:深夜", Name: "深夜", Gains: st.EQCustomGains},
	}
	tt, s, a, _ := shellOnSettings(t, st)
	tt.Frame()

	settingRect(t, tt, s, "选择均衡器预设")
	openSelect(t, tt, "选择均衡器预设")
	if err := tt.Click("摇滚"); err != nil {
		t.Fatalf("下拉里没有摇滚：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := a.SettingsSnapshot(); got.EQPreset != "rock" || got.EQCustomOn {
		t.Errorf("选了摇滚之后是 preset=%q customOn=%v", got.EQPreset, got.EQCustomOn)
	}

	openSelect(t, tt, "选择均衡器预设")
	if err := tt.Click("深夜"); err != nil {
		t.Fatalf("下拉里没有存的预设：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	got := a.SettingsSnapshot()
	if !got.EQCustomOn || got.EQCustomName != "深夜" || got.EQCustomGains[0] != 1 {
		t.Errorf("选了存的预设之后是 on=%v name=%q gains=%v",
			got.EQCustomOn, got.EQCustomName, got.EQCustomGains)
	}
}

// TestTheFullPlayerEQEntryOpensTheSamePanel 验满屏播放器偏好面板里那枚
// 「自定义」弹的是同一个滑杆面板（滑杆身份带 "full:" 前缀，见
// eq_panel.go 文件头「键的分域」）。
func TestTheFullPlayerEQEntryOpensTheSamePanel(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)
	tt.Frame()
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("自定义"); err != nil {
		t.Fatalf("偏好面板里没有均衡器入口：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	if _, ok := tt.Find("均衡器面板"); !ok {
		t.Fatalf("满屏里没弹出均衡器面板，文字有 %q", tt.Texts())
	}
	// 满屏那份滑杆的身份带 "full:" 前缀；找到的是按 Label 命名的那枚
	//（"均衡器 31 Hz"）——key 只决定状态归属，Find 认的是 Label。
	if _, ok := tt.Find("均衡器 31 Hz"); !ok {
		t.Errorf("满屏里找不到第一段滑杆")
	}
}
