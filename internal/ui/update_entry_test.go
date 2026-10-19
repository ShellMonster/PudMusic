// 设置页上那个「检查更新」按钮：它**在忙**的时候是什么样。
//
// 这一族断的是**画面上的三件事**：灰了、转了、按不动。
//
// ⚠️ 判据一律是「画面上看见了什么」和「按下去有没有真的去查」，不是「调了哪个
// 函数」——点一个关着的按钮不会报错，所以「按不动」只能靠「状态没变」来断。
package ui

import (
	"context"
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// gatedUpdate 是一份**卡住不回的**假更新源：查的那一趟要等 gate 放行才回来。
//
// 为什么要卡住：查得那么快的话，「正在查」那一档在画面上根本留不住（按钮转起来
// 的那一帧就已经查完了），而这一档正是要断的东西。
type gatedUpdate struct {
	fakeUpdate
	gate chan struct{}
}

func (g *gatedUpdate) Check(ctx context.Context) (*app.UpdateRelease, error) {
	g.mu.Lock()
	g.checks++
	g.mu.Unlock()
	<-g.gate
	return nil, nil
}

// TestTheCheckUpdateButtonTurnsAndGreysOut 断「点『检查更新』时按钮转起来、也灰
// 下去、还按不动」。
//
// Biu 那个按钮是 `<AsyncButton isLoading={…}>`：查的时候它转起来、也灰下去。
// 我们原来只有「灰下去」和「字换成『正在检查…』」，**没有转的那一下**。
//
// 三条判据：
//
//  1. **灰**：按钮的底色不再是那枚主色绿（`Disabled` 让整枚按钮半透明）。
//  2. **转**：推钟的时候按钮上的像素在变（那是转动的辐条在转）。
//  3. **按不动**：查的时候再按一下，不会又去查一次。
func TestTheCheckUpdateButtonTurnsAndGreysOut(t *testing.T) {
	gu := &gatedUpdate{gate: make(chan struct{})}
	gu.enabled = true
	gu.version = "0.1.0"
	b := newTestShell(t,
		withSettings(app.DefaultSettings("")),
		mounted(func(_ *testing.T, a *app.App) {
			a.MountUpdates(gu, filepath.Join(t.TempDir(), "app-update.json"))
		}))
	tt := b.tt
	tt.Frame()
	openSettings(t, tt)

	f := &updFixture{t: t, tt: tt, s: b.s, a: b.a, be: &gu.fakeUpdate}
	f.pressEntry(t, "检查更新")

	// 等它真的进到「正在查」那一档。
	waitFor(t, "查起来了", func() bool { return b.a.UpdateSnapshot().Phase == app.UpdateChecking })
	tt.Frame()

	busy, ok := tt.Find("正在检查…")
	if !ok {
		t.Fatalf("按钮没有变成忙的那一档，文字有 %q", tt.Texts())
	}

	// ① 灰：按钮的底色不是主色绿了。
	//
	// 取按钮框里最靠边上的一个像素（那儿是按钮的底，不是字）。
	face := tt.Image().RGBAAt(int(busy.X)+1, int(busy.Y+busy.H/2))
	if face.R == 30 && face.G == 215 && face.B == 96 {
		t.Errorf("查的时候按钮还是那枚主色绿（%v）——它没有灰下去", face)
	}

	// ② 转：推钟的时候按钮上的像素在变。
	before := tt.Image()
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	after := tt.Image()
	if !changedIn(before, after, busy) {
		t.Error("推了 120ms 的钟，按钮上一个像素都没变——它没有转")
	}

	// ③ 按不动：再按一下不会又去查一次。
	before2 := gu.checksDone()
	tt.ClickAt(busy.X+busy.W/2, busy.Y+busy.H/2)
	tt.Frame()
	if got := gu.checksDone(); got != before2 {
		t.Errorf("查的时候按钮还能按（又查了 %d 次）——它是关着的", got-before2)
	}

	close(gu.gate)
	waitFor(t, "查完", func() bool { return b.a.UpdateSnapshot().Phase != app.UpdateChecking })
}

// changedIn 是两帧之间在 box 里变了的像素个数。
func changedIn(a, b *image.RGBA, box mygoui.Rect) bool {
	for y := max(int(box.Y), a.Rect.Min.Y); y < min(int(box.Y+box.H), a.Rect.Max.Y); y++ {
		for x := max(int(box.X), a.Rect.Min.X); x < min(int(box.X+box.W), a.Rect.Max.X); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return true
			}
		}
	}
	return false
}
