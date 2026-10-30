package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/rowkey"
)

func TestQueueRowsShowEveryQueuedCopy(t *testing.T) {
	item := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "同一首"}
	rows := queueRowsFor([]app.Item{item, item}, &rowkey.Memo[queueRowRaw, queueRowIdentity]{})
	if len(rows) != 2 {
		t.Fatalf("抽屉展示了 %d 行，want 2：队列里的重复项必须逐项可见", len(rows))
	}
	if rows[0].at != 0 || rows[1].at != 1 {
		t.Fatalf("重复项的位置是 %d / %d，want 0 / 1", rows[0].at, rows[1].at)
	}
}

func TestQueueDrawerHasVisibleCloseButton(t *testing.T) {
	s, tt := newShell(t)
	s.app.Queue().Reset(app.Item{Source: "local", Type: "audio", Title: "第一首"})
	s.drawerOpen = true
	settleDrawer(t, s, tt)
	if _, ok := tt.Find("关闭播放列表"); !ok {
		t.Fatalf("播放列表抽屉没有可见关闭按钮，文字有 %q", tt.Texts())
	}
	if err := tt.Click("关闭播放列表"); err != nil {
		t.Fatalf("点不了关闭播放列表：%v", err)
	}
	settleDrawer(t, s, tt)
	if tt.HasText("关闭播放列表") {
		t.Fatal("点关闭按钮后，播放列表抽屉仍然存在")
	}
}
