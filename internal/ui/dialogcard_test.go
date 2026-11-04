package ui

import (
	mygoui "github.com/egoist/mygo/ui"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestDialogCardIsTheSurfaceToken：弹窗面板是 `--surface` 底、12 圆角，和页面
// 底色分得开（深浅两份）。
func TestDialogCardIsTheSurfaceToken(t *testing.T) {
	for _, dark := range []bool{false, true} {
		st := app.DefaultSettings("")
		th := BuildTheme(st, dark)
		got := dialogCard(th)
		if want := tok.Of(th).Surface; got.bg != want || got.radius != 12 {
			t.Errorf("深色=%v：弹窗面板是 %+v，该是 surface %v / 12", dark, got, want)
		}
		if got.bg == th.Background {
			t.Errorf("深色=%v：弹窗面板和页面同色，没有层次", dark)
		}
	}
}

// TestEnterDoesNotConfirmADangerDialog：删除类弹窗一出来，直接按回车**不会**删
// （设计规范 §7.19：默认焦点在安全选项上，不得默认聚焦确认键）。
func TestEnterDoesNotConfirmADangerDialog(t *testing.T) {
	b, s, tt := collectionShellOpts(t, []shellOpt{at(1200, 1200)}, "1", "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
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
