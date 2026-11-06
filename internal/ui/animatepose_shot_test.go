// 出图：动画换到 `AnimatePose` 那一档之后，这两处**画面上**长什么样（票 #276；
// 原来还有「赞-已赞」那一张，赞键在喜欢歌单原型里去掉了，那一段随之删掉）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAnimatePoseShots
//
// # 出哪几张
//
//	满屏-亮着    顶栏和底下那一条都在（淡入 + 滑回来之后的样子）
//	满屏-藏了    同一屏，界面藏起来之后那两条**滑出窗口**、淡到没有
//
// # 为什么深浅两套、两个主题方案都要出
//
// 满屏那两条是**压暗过的封面上的白字**，而背景层是**主色很淡的一盘**（18%）——
// 一套配色看不出「淡出量对不对」「那一圈是不是长满了」，两套也未必：主题方案
// 换的是主色和底色，而「淡出」这件事在两套里是**不同的数**。
//
// # ⚠️ **出图不是审查**
//
// 出图只是材料；**审查**（逐张打开比过五项：布局 / 间距 / 层级 / 密度 / 对齐）
// 才是那道门。差别写在收尾说明里。
package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// animatePoseShots 是要出图的那几套配色。
func animatePoseShots() []struct {
	name   string
	dark   bool
	preset app.Accent
} {
	return []struct {
		name   string
		dark   bool
		preset app.Accent
	}{
		{"浅色-暖纸", false, app.AccentCinnabar},
		{"深色-暖纸", true, app.AccentCinnabar},
		{"深色-暮光", true, app.AccentAmber},
		{"浅色-柔雾", false, app.AccentPine},
	}
}

func TestRenderAnimatePoseShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, tc := range animatePoseShots() {
		t.Run(tc.name, func(t *testing.T) {
			settings := app.DefaultSettings("")
			settings.Accent = tc.preset

			// ---- 满屏：亮着 / 藏了 ----
			fc := newFakeClock(time.Unix(1_700_000_000, 0))
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(tc.dark),
				withSettings(settings), withClock(fc))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("收起满屏播放器")
			})
			settleFull(t, b.tt)
			save(t, dir, "276-满屏-亮着-"+tc.name+".png", b.tt)

			// 藏起来：那两条要**滑出窗口**（不只是淡到没有）——藏着的按钮不该
			// 接得到点击。
			advanceFull(b.s, b.tt, fc, biuHideSeconds*time.Second+time.Second)
			settleFull(t, b.tt)
			save(t, dir, "276-满屏-藏了-"+tc.name+".png", b.tt)
			b.Close()
		})
	}
}
