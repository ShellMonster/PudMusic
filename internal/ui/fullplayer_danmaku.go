// 满屏播放器左边那张封面上的弹幕：**画**的那一层。
//
// # 这一层必须走 `ui.Overlay`
//
// 满屏播放器整个是浮层（`viewFullPlayer` 里那个 `ui.Overlay`），而浮层画在
// 主树**之上**——弹幕画在主树里的话，满屏那层不透明的底会把它整个盖住。
//
// ⚠️ **那个症状是静默的**：画面上一个像素也没有，而没有任何报错。所以这里
// 专门有一条测试钉住它（见 `TestTheDanmakuUnderTheFullPlayerWouldNotBeSeen`）。
//
// # 它建在满屏那一层之后
//
// 框架的浮层都挂在同一个 overlay root 上，**后建的在上**
// （`mygo/ui/context.go` 的 `overlayRoot`）——弹幕于是压在封面上面，而不是被
// 满屏那一层压住。
//
// # 位移走**绘制帧**，不走 view（票 #669）
//
// 整窗的唤醒被降到约 1Hz（`internal/app/tick_wake.go` 的 `readoutMoved`）：
// 位置要是算在 view 里（`Absolute().Left(x)`），弹幕就只会在整窗重建那一帧
// 动一下——**「动一下停一下」**，那正是本票要修的那件事。
//
// ⇒ 位置在**画的时候**算：这一层的 `Draw` 回调拿 `Painter.Now()`，从
// `danmakuLayer` 的锚点推出现在该在哪儿（见 `danmaku.go` 的文件头），然后
// 自己把字画上去。
//
// ⚠️ **不能用 `Context.AnimationFrame()`**：那一位是 `rt.animating`，框架会
// **重建整棵 view**——#611 好不容易降下来的整窗重建又被抬回 60Hz。这里用
// `Painter.AnimationFrame()`（`rt.repainting`）：那一帧只重画、不重建。
//
// # 为什么不再一条弹幕一个 `ui.Text`
//
// 每个 `ui.Text` 是一个元素，而它的位置只有在**建树**时才能改。要让位移跟着
// 绘制帧走，字就得在这一层的 `Draw` 里自己画。
//
// 代价是**弹幕的正文不在 `Tester.Texts()` 里**了。所以这一组的判据从「树上有
// 那行字」改成**画面上的像素**（`redSpan` / `greenSpan`）和**模型层的轨道
// 状态**——那两样才是用户能看到的东西。弹幕本来就是装饰（`RoleNone`），读屏
// 软件不念它。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// danmakuLayerKey 是弹幕那一层盒子的 key。
//
// ⚠️ **它是一个固定的串**：弹幕的 rowid 只是模型层的键（`danmakuLayer.slots`），
// 这一层盒子是浮层里唯一的一个，所以它只需要在兄弟里唯一。
const danmakuLayerKey = "full-danmaku"

// viewFullDanmaku 画满屏播放器左边那张封面上的弹幕。
func (s *Shell) viewFullDanmaku(c *ui.Context, st app.State) {
	d := &s.full.danmaku
	if !s.app.FullPlayerPrefs().ShowDanmaku {
		// 设置里关掉了 ⇒ **一个字都不建**（连那个盒子都不建）。
		//
		// ⚠️ **顺手把这一摊清掉**：关掉再打开的时候轨道那一摊不该留着上一次
		// 的分配（那是「换歌要清」那一条的同一件事），绘制帧的请求也随盒子
		// 一起消失。
		d.reset()
		return
	}
	area := s.full.coverRect
	if area.W <= 0 || area.H <= 0 {
		// 没有封面可放（封面关着 / 视频铺满整窗 / 满屏里没有歌）⇒ 不画。
		d.reset()
		return
	}
	items := s.app.Danmaku().Items
	if len(items) == 0 {
		// 这一集没有弹幕（或者还在问）⇒ 把上一份的痕迹收掉。
		d.reset()
		return
	}
	// 建树这一帧要做的三件事：认这份弹幕的身份（换歌就整份作废）、量字宽
	// 用的字号、把时钟锚点对上这一帧的位置。
	d.sync(c, st, items, area)
	ui.Overlay(c, func() {
		s.drawFullDanmaku(c, st, area)
	})
}

// drawFullDanmaku 把弹幕那一层建进**当前这一层**。
//
// ⚠️ **它不自己建浮层**：调用方（`viewFullDanmaku`）建，因为「画在浮层里」
// 还是「画在主树里」是**两件不同的事**——后者是那个静默失败的坑，而要证明
// 它就得能故意画错一次（见那条测试）。
func (s *Shell) drawFullDanmaku(c *ui.Context, st app.State, area ui.Rect) {
	d := &s.full.danmaku
	if !d.ready {
		// 这一帧没有可画的弹幕（设置关着、没有封面、没有歌）。盒子都不建——
		// 建了也没有人往里面画。
		return
	}
	ui.Box(c).Key(danmakuLayerKey).
		Absolute().Left(area.X).Top(area.Y).Size(area.W, area.H).
		// ⚠️ **不吃指针**：这一层压在封面上面，而封面要接长按三连和点开满屏
		// （见 `viewFullCover`）。吃了指针的话，用户点封面会点不动。
		PassThrough().
		// ⚠️ **弹幕是装饰**：读屏软件不该念它（报告 §4）。
		Role(ui.RoleNone).
		// 🔴 **位移和字都在画的时候算**（见文件头那一段）：这一帧之后只重绘
		// 的那些帧**不会重建 view**，所以回调里从 `d` 现算才是唯一的事实来源。
		Draw(s.paintDanmaku)
}

// paintDanmaku 画这一帧该在屏幕上的那几条弹幕。
//
// ⚠️ **它每一帧都从 `d` 现算**（包括窗口推进和轨道分配）：只重绘的那些帧用的
// 是**建树那一遍记下的回调**，闭包里存着的任何「那一帧的可见列表」到这里都
// 是过期的。
//
// # 每帧的工作量（票 #670）
//
//   - 窗口：`advance` 只把**新进入窗口**的条目过一遍（游标只前进），稳态帧里
//     游标不动——一次 `sort.Search` 都不做；
//   - 活动集合：最多同屏 12 条（`danmakuMaxOnScreen`），`expire` 原地压缩；
//   - 绘制：就那 12 条，逐条算位置、画字。
//
// 时间索引和窗口二分仍然是 #647 交付的那一份（`d.itemsByTime` + `sort.Search`，
// 见 `danmakuLayer.rewind`）——这一票没有重新实现它，只是不再每帧都二分。
func (s *Shell) paintDanmaku(p *ui.Painter, r ui.Rect) {
	d := &s.full.danmaku
	if !d.ready || len(d.itemsByTime) == 0 {
		return
	}
	d.paints++
	posMS := d.posAt(p.Now())

	// 「前一条走出 30% 的宽度」换算成时间（毫秒）。
	spacingMS := int64(float64(r.W) * danmakuTrackSpacingRatio * 1000 / danmakuSpeed)

	// 把时间窗口推到当前位置：新进入的条目在这里分轨道、量字宽（只做一次），
	// 出界的那些在这里收掉。
	d.advance(p, posMS, r.W, spacingMS)

	// 画。⚠️ **裁到封面那一块**：走出左边沿的弹幕直接出界（报告 §3.3：不淡出）。
	// 不裁的话它们会飘到满屏的别处去。
	//
	// ⚠️ **裁剪必须写在这个回调里**：元素的 `Clip()` 只裁它的**孩子**，而这一层
	// 没有孩子（字是这里画的）。
	//
	// ⚠️ **宽 0 = 不折行**：原来那条 `.SingleLine()` 要的就是这个；而这一层
	// 只有一张画布那么大，位置是算出来的，折行会把字叠到别的轨道上去。
	customFont := p.Theme().Font != ""
	p.Clip(r, 0, func() {
		for i := range d.active {
			v := &d.active[i]
			// 入场那一点是封面的右沿；走了多远由它自己的年龄算。
			x := r.X + r.W - float32(posMS-v.slot.timeMS)*danmakuSpeed/1000
			y := r.Y + float32(v.slot.lane)*d.trackH
			color := danmakuColorOf(v.item.Color)
			if customFont {
				// ⚠️ **用户换了字体才走这一支**：那一档下 `p.Text` 用的是
				// 系统默认字体，弹幕会和界面上别的字不一样。`RichText` 每一帧
				// 会比 `Text` 多几次分配（span 的样式缓存），所以默认那一档
				// （主题的 `Font` 是空串 = 系统字体）不走它。
				p.RichText(x, y, 0, ui.Span{Text: v.item.Text, Size: d.fontSize, Color: color})
				continue
			}
			p.Text(x, y, v.item.Text, d.fontSize, color)
		}
	})

	// 播放中要下一帧：那一帧**只重绘**（`p.AnimationFrame` 是
	// `rt.repainting`，不是 `Context.AnimationFrame` 的 `rt.animating`）。
	//
	// 三个闸门：
	//
	//	播放中      暂停时位置不动，一直画是白烧 CPU（用户能感到的是风扇）
	//	没开减动效   那个偏好下不逐帧重画（位置仍旧跟着整窗重建走）
	//	窗口看得见  失焦/最小化时框架自己不管，链子会在绘制路径里续下去
	//	            （票 #156，和波形、频谱、播放条那根线同一条纪律）
	if d.playing && d.animate && s.presence.seen() {
		d.requests++
		p.AnimationFrame()
	}
}
