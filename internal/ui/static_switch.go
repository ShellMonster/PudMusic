// 设置页上的开关：**不带动画**的那一个。
//
// ## 为什么自己画，而不是用 `ui.Switch`
//
// Biu 那一页的每一个开关都写了 `disableAnimation`
// （`biu/src/pages/settings/system-settings.tsx` 的三处 `<Switch disableAnimation …>`），
// 而 MyGo 的 `ui.Switch` 把旋钮动画写死在 140ms 上、**没有关掉的开关**
// （`mygo/ui/widgets.go` 的 coreSwitch：`sw.Animate("knob", …, 140*time.Millisecond)`）。
// 照 Biu 就是关掉它。
//
// ## 关掉它顺带解决了一件很贵的事
//
// 旋钮动画每一帧都要一帧新的画面，而无头 Tester 的钟**只有 `Advance(d)` 才走**。
// 于是「点一下开关」之后的 `Tester.settle()` 跑满它那 20 帧的上限——动画在不动
// 的钟上永远走不完。实测（`-race` 下）**点一下开关 31.7 秒**，而点一下下拉 0.3 秒。
//
// 设置页有四个开关，每一个都被测试点过（点一下就断言设置变了），于是整包测试在
// `-race` 下多花好几百秒——验收命令 `go test -race ./...` 要跑 7 分半。去掉这个
// 动画之后，它既和 Biu 一致，又把验收命令拉回能忍受的时长。
//
// 外观照 `coreSwitch` 逐字抄（`Size(Space(9), Space(5))`、`Radius(Space(2.5))`、
// 关态底色 `Border.Mix(Text, 0.15)` 往主色混、旋钮是带一圈影子的白圆），所以静
// 态看起来和 `ui.Switch` 一模一样，只是不再滑过去。
package ui

import (
	"github.com/egoist/mygo/ui"
)

// staticSwitch 是 `ui.Switch` 去掉旋钮动画的那一个。
//
// 值和 `ui.Switch` 一样：一帧一读的局部 `*bool`，用户点了就写进去，`Changed()`
// 报这一次点击。`Disabled` 照旧由调用方给。
func staticSwitch(c *ui.Context, on *bool) ui.Element {
	t := c.Theme()
	sw := ui.SwitchBase(c, on).
		Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5))

	// 关态的底色往主色混：开 = 全主色，关 = 全灰。和 coreSwitch 同一条式子。
	off := t.Border.Mix(t.Text, 0.15)
	pos := float32(0)
	if *on {
		pos = 1
	}
	sw.Background(off.Mix(t.Accent, pos))
	// 钉死：别让排版在这一帧里改出别的宽高（`ui.Switch` 也是这么钉的）。
	sw.Shrink(0)

	sw.Draw(func(p *ui.Painter, r ui.Rect) {
		in := t.Space(0.5)
		d := r.H - 2*in
		// 开着时旋钮在末端。本项目不摆「从右到左」的布局（那一条是 MyGo 的
		// Direction，界面上读不到），所以这一支照 coreSwitch 的默认分支抄。
		knob := ui.Rect{X: r.X + in + pos*(r.W-r.H), Y: r.Y + in, W: d, H: d}
		p.Shadow(knob, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.25))
		p.Fill(knob, ui.RGBA(255, 255, 255, 1), d/2)
	})
	return sw
}
