// 票 #242 的画面验收：登录入口、登录页、未登录那一屏、404、顶栏那两枚图标。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLoginShots
//
// **深浅两套都出**：图标是单色的、跟着祖先的文字色走（`mygo/ui/svg.go` 的
// drawIcon 收的就是文字色）——浅色下是深灰、深色下是亮白；侧栏那一句提示是
// 12px 的淡灰，深浅两套差得更远。只出浅色等于没验这一条。
//
// ⚠️ **图是给人看的，判据在各自的测试里**（`login_test.go` / `notfound_test.go` /
// `sidebar_login_test.go` / `icons_test.go`）：这里只负责把画面摆出来。
package ui

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestRenderLoginShots 把这一票动的五处画面存成 PNG，深浅各一套。
func TestRenderLoginShots(t *testing.T) {
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
			// ① 侧栏那一句「登录后可见」（未登录），和它悬停时的样子。
			//
			// ⚠️ **两个外壳**：悬停要靠指针的位置，而指针一动画面就变了——
			// 同一个外壳上连着存两张的话，第二张里第一张的悬停态还挂着。
			shotLoggedOut(t, dir, name, dark, false)
			shotLoggedOut(t, dir, name, dark, true)

			// ② 登录页：说了收益。
			shotLoginPage(t, dir, name, dark)

			// ③ 未登录那一屏：有一枚专属图标。
			shotNeedsLogin(t, dir, name, dark)

			// ④ 404：多一条立刻能走的路。
			shotNotFound(t, dir, name, dark)

			// ⑤ 顶栏右端那两枚：未登录是二维码（登录），登录了是圆圈箭头（退出）。
			shotNavCorner(t, dir, name, dark, false)
			shotNavCorner(t, dir, name, dark, true)
		})
	}
}

// shotLoggedOut 存未登录时那一屏（侧栏那句提示 + 那一页）。
//
// hover 为真时把指针停在那句提示上——那一句的「点得动」全靠悬停才看得出
// （变主色 + 下划线），而平时它就是一行淡灰。
func shotLoggedOut(t *testing.T, dir, name string, dark, hover bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{})
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountExplore(cli)
	}))
	s, tt := b.s, b.tt
	s.navigate(followPath)
	waitFrames(t, tt, "「还没登录」那一支画出来了", func() bool {
		return tt.HasText("还没登录")
	})
	if hover {
		r, ok := tt.Find(loggedOutHint(3))
		if !ok {
			t.Fatalf("侧栏上没有那句提示，文字有 %q", tt.Texts())
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
	}
	file := "110-侧栏-登录后可见"
	if hover {
		file = "111-侧栏-登录后可见-悬停"
	}
	save(t, dir, fmt.Sprintf("%s-%s.png", file, name), tt)
	b.Close()
}

// shotLoginPage 存扫码登录那一页。
func shotLoginPage(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{})
		a.MountLogin(app.NewMemoryStore("test"), cli)
	}))
	s, tt := b.s, b.tt
	s.navigate(loginPath)
	waitFrames(t, tt, "登录页画出来了", func() bool {
		return tt.HasText("扫码登录")
	})
	save(t, dir, fmt.Sprintf("112-登录页-说收益-%s.png", name), tt)
	b.Close()
}

// shotNeedsLogin 存「还没登录」那一屏（我的关注）。
func shotNeedsLogin(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{})
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountExplore(cli)
	}))
	s, tt := b.s, b.tt
	s.navigate(followPath)
	waitFrames(t, tt, "「还没登录」那一支画出来了", func() bool {
		return tt.HasText("还没登录")
	})
	save(t, dir, fmt.Sprintf("113-未登录屏-专属图标-%s.png", name), tt)
	b.Close()
}

// shotNotFound 存 404 那一页。
func shotNotFound(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark))
	s, tt := b.s, b.tt
	s.navigate("/没有这一页")
	waitFrames(t, tt, "404 画出来了", func() bool {
		return tt.HasText("404")
	})
	save(t, dir, fmt.Sprintf("114-404-回到主页-%s.png", name), tt)
	b.Close()
}

// shotNavCorner 存顶栏右端那一块（头像 + 登录 / 退出那一枚）。
//
// loggedIn 为真时那一枚是「退出登录」（圆圈 + 一支朝外的箭头），为假时是
// 「登录」（二维码）——两枚挨着看，才看得出「一眼分得清」。
func shotNavCorner(t *testing.T, dir, name string, dark, loggedIn bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{})
		a.MountLogin(app.NewMemoryStore("test"), cli)
		if loggedIn {
			a.TestOnlyAdoptLogin("7")
		}
	}))
	tt := b.tt
	tt.Frame()
	img := tt.Image()
	bounds := img.Bounds()
	x0 := bounds.Dx() - 220
	if x0 < 0 {
		x0 = 0
	}
	h := 64
	if bounds.Dy() < h {
		h = bounds.Dy()
	}
	sub := image.NewRGBA(image.Rect(0, 0, bounds.Dx()-x0, h))
	draw.Draw(sub, sub.Bounds(), img, image.Pt(x0, 0), draw.Src)

	file := "115-顶栏-登录图标"
	if loggedIn {
		file = "116-顶栏-退出图标"
	}
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.png", file, name))
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", out)
	b.Close()
}
