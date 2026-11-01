package list

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 列表行下面那条进度条的出图（票 #703 的 C 项的视觉审查）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/widgets/list/ -run TestRenderProgressBarShots
//
// # 为什么出图
//
// 这一项要按**视觉变更**处理（票面写明「这一项要按视觉变更处理：走
// `docs/agents/visual-review.md` 的视觉审查，深浅两套都要看」）。而框架那一版
// 和手搓那一版**长得不一样**：
//
//	              手搓（改之前）        框架 ui.Progress（改之后）
//	轨道色        t.Text.Alpha(0.1)     t.Border
//	填充色        t.Accent              t.Accent（一样）
//	高度          3                     t.Space(1.5) = 6
//	圆角          1.5                   t.Space(0.75) = 3
//	宽度          120（写死）           **不给就没有**（框架只给高度）
//
// ⇒ 所以宽度是我们给死的（120，和改之前一样），其余四项跟着框架走。
//
// ⚠️ **出图本身不是判据**：同一份代码两次出图有几十张会不一样（字体 hinting、
// 抗锯齿的随机相位，见 `internal/ui/ticket319_shot_test.go` 顶上那段）。判据是
// `progress_test.go` 里那几条（它们量的是宽度和填充比例），出图是给人看的。
//
// ⚠️ **深浅两套都要看**：轨道色是 `t.Border`——深色下它是「叠在当前底色上的
// 15% 白」，浅色下是 `#dbdbdb`，两套的对比度不一样（所以浅色下那一档更显眼）。

// TestRenderProgressBarShots 出图：浅色 + 深色，各一张。
//
// 三行：一行**没有进度**（对照：那一行下面什么都没有）、一行**听了一半**、
// 一行**听完了**（进度条画满）。
func TestRenderProgressBarShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			th := ui.LightTheme()
			if dark {
				th = ui.DarkTheme()
			}
			th.Accent = ui.Hex("#1ed760") // Biu 的绿（应用的默认主色）
			l := New(newCache(t), Actions{})
			l.SetSongs([]Music{
				{ID: "a", Title: "没有进度的那一首", Owner: "甲 UP", Duration: 60 * time.Second},
				{ID: "b", Title: "听到一半的那一首", Owner: "乙 UP",
					Duration: 60 * time.Second, Progress: 30 * time.Second},
				{ID: "c", Title: "听完了的那一首", Owner: "丙 UP",
					Duration: 60 * time.Second, Progress: 90 * time.Second},
			})
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(th)
				l.View(c)
			}, testerW, 200)
			tt.Frame()
			saveShot(t, dir, "01-列表进度条-"+name+".png", tt)
		})
	}
}

// saveShot 把那一帧存成 PNG。
//
// ⚠️ **自己写一份，不 import `internal/ui` 的那个 `save`**：那个在 `package ui`
// 里，而这里是 `package list`——而 `internal/ui` import `internal/ui/widgets/list`
// （`internal/ui/collectionbatch.go`），反向 import 就是**循环依赖**（`go test`
// 直接报 "import cycle not allowed in test"）。
func saveShot(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", filepath.Join(dir, name))
}
