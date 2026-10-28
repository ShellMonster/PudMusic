package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestDialogCardFollowsThePreset：套了方案，弹窗面板用方案的 surface1 / radiusLg；
// 经典不变（底色 = Background）。
func TestDialogCardFollowsThePreset(t *testing.T) {
	for _, name := range allPresets {
		st := app.DefaultSettings("")
		st.ThemePreset = name
		st.ThemeMode = app.ThemeModeLight
		b := newTestShell(t, withSettings(st))
		b.tt.Frame()
		th := BuildTheme(st, false)
		sk, shape, _ := presetSkinFor(name, false)
		got := dialogCard(th)
		if got.bg != mygoui.Hex(sk.surface1) || got.radius != shape.radiusLg {
			t.Errorf("%s：弹窗面板是 %+v，该是 surface1 %s / radiusLg %v", name, got, sk.surface1, shape.radiusLg)
		}
		if got.bg == th.Background {
			t.Errorf("%s：弹窗面板和页面同色，没有层次", name)
		}
	}

	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight
	b := newTestShell(t, withSettings(st))
	b.tt.Frame()
	th := BuildTheme(st, false)
	if got := dialogCard(th); got.bg != th.Background || got.radius != th.Space(2.5) {
		t.Errorf("经典：弹窗面板是 %+v，该保持原样 {Background, 2.5 单位}", got)
	}
}

// TestEnterDoesNotConfirmADangerDialog：删除类弹窗一出来，直接按回车**不会**删
// （设计规范 §7.19：默认焦点在安全选项上，不得默认聚焦确认键）。
func TestEnterDoesNotConfirmADangerDialog(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")
	wantText(t, tt, "确认删除选中的 2 首吗？")

	tt.Key(0, mygoui.KeyEnter)
	for range 5 {
		tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(b.removedBatches()); n != 0 {
		t.Fatalf("弹窗一出来按回车就删了 %d 批——默认焦点落在了确认键上", n)
	}
}
