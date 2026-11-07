package ui

// 视觉审查出图（默认不跑）：
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAlignWidgetsShots
//
// 对照原型 index.html 的 `.pop` / `.empty` / `.icon-btn:disabled` / `.btn:disabled`、app.js:638/639/699。

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

func TestRenderAlignWidgetsShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			// ① 组件：禁用键（悬停在上面）+ 弹层面板（小标题 + 选中项带说明）。
			s, _ := newShell(t, systemIsDark(dark))
			tt := mygoui.NewTester(func(c *mygoui.Context) {
				s.applyFrameTheme(c)
				p := tok.Of(c.Theme())
				mygoui.Column(c).Size(720, 420).Background(p.Bg).Padding(32).Gap(24).Children(func() {
					mygoui.Row(c).Gap(12).AlignItems(mygoui.Center).Children(func() {
						pud.IconBtn(c, "b1", "left", "后退（禁用）", pud.IconOpts{Disabled: true})
						pud.IconBtn(c, "b2", "right", "前进", pud.IconOpts{})
						pud.ButtonDisabled(c, "b3", pud.Ghost, pud.Std, "幽灵（禁用）", "", true)
						pud.ButtonDisabled(c, "b4", pud.Primary, pud.Std, "主色（禁用）", "", true)
						pud.Button(c, "b5", pud.Primary, pud.Std, "主色", "")
					})
					// 全屏顶栏的评论键角标（透明底，app.js:1145）压在封面取色的底上，旁边是普通的（side 底）。
					mygoui.Row(c).Gap(24).Padding(12).Background(mygoui.RGB(0x4a, 0x1c, 0x1e)).Radius(8).Children(func() {
						pud.WithFull(func() {
							mygoui.Row(c).Gap(8).AlignItems(mygoui.Center).Children(func() {
								pud.IconBtn(c, "cnt-full", "comment", "评论（全屏）", pud.IconOpts{Count: "1.2万"})
								pud.IconBtn(c, "cnt-full-q", "queue", "队列（全屏）", pud.IconOpts{})
							})
							mygoui.Row(c).Gap(8).AlignItems(mygoui.Center).Children(func() {
								pud.IconBtn(c, "cnt-full-999", "comment", "评论 999+（全屏）", pud.IconOpts{Count: "999+"})
								pud.IconBtn(c, "cnt-full-q2", "queue", "队列 2（全屏）", pud.IconOpts{})
							})
						})
						pud.IconBtn(c, "cnt-plain", "comment", "评论（普通）", pud.IconOpts{Count: "999+"})
					})
					pud.PopPanel(c, "pop").Children(func() {
						pud.PopHeading(c, "音质")
						pud.PopOpt(c, "o1", "无损", "FLAC 16bit", true)
						pud.PopOpt(c, "o2", "高品质", "AAC 192k", false)
					})
				})
			}, 720, 420)
			tt.SetDark(dark)
			tt.Frame()
			if r, ok := tt.Find("幽灵（禁用）"); ok {
				tt.Move(r.X-10, r.Y+r.H/2)
				for i := 0; i < 6; i++ {
					tt.Advance(100 * time.Millisecond)
					tt.Frame()
				}
			}
			save(t, dir, name+"-align-components.png", tt)

			// ② 失败块。
			tt = mygoui.NewTester(func(c *mygoui.Context) {
				s.viewLoadError(c, "shot:err", "关注的 UP 主", "重试加载关注的人", errors.New("网络超时"), func() {})
			}, 900, 600)
			tt.SetDark(dark)
			tt.Frame()
			save(t, dir, name+"-align-load-error.png", tt)

			// ③ 空收藏夹 / 搜索无结果（整页，1200×800）。
			_, cs, ctt := collectionShellOpts(t, []shellOpt{systemIsDark(dark)}, "", "12345")
			ctt.SetDark(dark)
			waitSongs(t, ctt, cs, 0)
			ctt.Frame()
			save(t, dir, name+"-align-collection-empty.png", ctt)

			// ④ 稍后再看未登录（页头 + 登录提示）。
			f := newLibFake(t)
			b := newTestShell(t, systemIsDark(dark), mounted(func(t *testing.T, a *app.App) {
				a.MountLibrary(biliForTest(t, f))
			}))
			b.tt.SetDark(dark)
			b.tt.Frame()
			b.s.navigate(laterPath)
			b.tt.Frame()
			b.tt.Frame()
			save(t, dir, name+"-align-later-logged-out.png", b.tt)
		})
	}
}
