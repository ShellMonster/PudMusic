package ui

import (
	"context"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一族把**焦点与提示**那两件事存成 PNG（票 #245）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFocusShots
//
//	01 键盘走到顶栏那几枚图标键上：屏幕右上角**只有一条**提示（E3）
//	02 焦点在侧栏的第一项上（E4 的「进」之前）
//	03 焦点在内容区的列表上（E4 的「进」之后）
//
// ⚠️ **深浅两套都要出**：提示那一块是反色底（`Inverse`），深浅两套的底色
// 完全不同；而焦点环是半透明主色，压在浅底和深底上深浅不一——只看一套就只
// 验了一半。
//
// ⚠️ **一个字都不许打真 B 站接口**：这一族用的是内存里的假 B 站
// （`newLibFake`）。
func TestRenderFocusShots(t *testing.T) {
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
			// 01 键盘走位之后，提示只有一条。
			//
			// ⚠️ **等条件，不数次数**：Tab 停在顶栏哪一枚上，取决于焦点顺序，
			// 而焦点顺序会变（顶栏加一枚图标就变）。这里等的是「顶栏那枚
			// 『打开文件』的提示出来了」这个**条件**。
			{
				b := newTestShell(t, systemIsDark(dark),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				tt := b.tt
				tt.SetFocused(true)
				tt.Frame()
				waitFrames(t, tt, "键盘走到顶栏那枚带提示的图标键上",
					func() bool {
						// ⚠️ **按 Tab 在条件里**：焦点走到哪一枚上，取决于焦点
						// 顺序，而焦点顺序会变（顶栏加一枚图标就变）。所以这里
						// 「一直按 Tab，按到那枚的提示出来为止」——不数次数
						// （waitFrames 的保险丝兜底）。
						tt.Key(0, mygoui.KeyTab)
						return tt.HasText("缩成 320×84 的小窗贴边悬浮，音乐不停")
					})
				save(t, dir, "01-键盘走位-只有一条提示-"+name+".png", tt)
				b.Close()
			}

			// 02 焦点在侧栏的第一项上 → 03 按 ↓ 之后焦点在内容区的列表上。
			//
			// ⚠️ 用**有内容**的那一页（稍后再看 + 内存夹具）：一页没有内容的时候
			// 画的是空状态，列表压根不建，也就没有「走进内容区」这一说。
			{
				f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
				fc := &focusCtx{}
				b := newTestShell(t, systemIsDark(dark),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
					fc.opt(),
					mounted(func(t *testing.T, a *app.App) {
						cli := biliForTest(t, f)
						store := app.NewMemoryStore("test")
						if err := store.Save(context.Background(), app.Session{SESSDATA: "sess"}); err != nil {
							t.Fatal(err)
						}
						a.MountLogin(store, cli)
						a.MountLibrary(cli)
					}),
					withClock(newFakeClock(now)))
				tt := b.tt
				tt.SetFocused(true)
				tt.Frame()
				b.s.navigate(laterPath)
				tt.Frame()
				waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&b.s.laterPage) == 3 })

				// 焦点交给侧栏的第一项（框架的句柄请求焦点）。
				b.s.sideAnchor.Focus()
				tt.Frame()
				waitFrames(t, tt, "焦点落到侧栏的第一项上",
					func() bool { return b.s.sideAnchor.Focused(fc.c) })
				save(t, dir, "02-焦点在侧栏-"+name+".png", tt)

				// 按 ↓：焦点走进内容区的列表。
				tt.Key(0, mygoui.KeyDown)
				tt.Frame()
				waitFrames(t, tt, "焦点走进内容区的列表",
					func() bool { return b.s.laterPage.rows.state.Handle.Focused(fc.c) })
				save(t, dir, "03-焦点在内容区-"+name+".png", tt)
				b.Close()
			}
		})
	}
}
