// 满屏播放器左边那张封面上的弹幕：**画**的那一层。
//
// # 这一层必须走 `ui.Overlay`
//
// 满屏播放器整个是浮层（`viewFullPlayer` 里那个 `ui.Overlay`），而浮层画在
// 主树**之上**——弹幕画在主树里的话，满屏那层不透明的底会把它整个盖住。
//
// ⚠️ **那个症状是静默的**：文字在树里（`tt.Texts()` 查得到），而画面上一个
// 像素也没有。所以这里专门有一条测试钉住它（见
// `TestTheDanmakuIsDrawnOverTheFullPlayerAndNotUnderIt`）。
//
// # 它建在满屏那一层之后
//
// 框架的浮层都挂在同一个 overlay root 上，**后建的在上**
// （`mygo/ui/context.go` 的 `overlayRoot`）——弹幕于是压在封面上面，而不是被
// 满屏那一层压住。
//
// # 位移由 pose 走，不由 view 走
//
// 位置一旦算在 view 里（`Absolute().Left(x)`），它就只会在**重建**的时候更新
// ——而满屏开着的时候界面 20 帧/秒重建（`app.tickInterval` = 50ms），于是弹幕
// 是 20fps 跳着走的。
//
// ⇒ 位移走 `AnimatePose`（「只重绘」那一族）：view 只给**目标**，元素自己在
// 只重绘的帧里走到那儿（60fps 平滑，每帧 1 像素）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// danmakuLayerKey 是弹幕那一层盒子的 key。
//
// ⚠️ **它是一个固定的串**：弹幕的 key 是**弹幕自己的 rowid**，而它们在
// 满屏那一层的**兄弟**里（这一层盒子的孩子）——所以这一层的 key 只需要在
// 兄弟里唯一，而这一层盒子是浮层里唯一的一个。
const danmakuLayerKey = "full-danmaku"

// viewFullDanmaku 画满屏播放器左边那张封面上的弹幕。
func (s *Shell) viewFullDanmaku(c *ui.Context, st app.State) {
	if !s.app.FullPlayerPrefs().ShowDanmaku {
		// 设置里关掉了 ⇒ **一个字都不建**（连那个盒子都不建）。
		//
		// ⚠️ **顺手把这一摊清掉**：关掉再打开的时候轨道那一摊不该留着上一次
		// 的分配（那是「换歌要清」那一条的同一件事）。
		s.full.danmaku.reset()
		return
	}
	area := s.full.coverRect
	if area.W <= 0 || area.H <= 0 {
		// 没有封面可放（封面关着 / 视频铺满整窗 / 满屏里没有歌）⇒ 不画。
		s.full.danmaku.reset()
		return
	}
	ui.Overlay(c, func() {
		s.drawFullDanmaku(c, st, area)
	})
}

// drawFullDanmaku 把该在屏幕上的那几条弹幕画进**当前这一层**。
//
// ⚠️ **它不自己建浮层**：调用方（`viewFullDanmaku`）建，因为「画在浮层里」
// 还是「画在主树里」是**两件不同的事**——后者是那个静默失败的坑，而要证明
// 它就得能故意画错一次（见那条测试）。
func (s *Shell) drawFullDanmaku(c *ui.Context, st app.State, area ui.Rect) {
	d := &s.full.danmaku
	snap := s.app.Danmaku()
	items := snap.Items
	if len(items) == 0 {
		// 这一集没有弹幕（或者还在问）⇒ 把上一份的痕迹收掉。
		d.reset()
		return
	}
	if d.count != len(items) || d.first != items[0].ID {
		// **换了一份**（换歌了）⇒ 上一份的轨道与字宽全部作废。
		//
		// ⚠️ **判据是「条数 + 第一条的 rowid」**：那一份弹幕的顺序是 B 站给的
		// 顺序（不变），所以这两样变了就是换了一份。
		d.reset()
		d.count, d.first = len(items), items[0].ID
	}

	fontSize := scale.Font(c, danmakuFontSize)
	trackH := danmakuTrackHeight
	if h := fontSize + danmakuTrackGap; h > trackH {
		// 系统字号调大了 ⇒ 轨道跟着长（否则两行字压在一起）。
		trackH = h
	}
	if maxH := area.H / float32(danmakuTracks); trackH > maxH {
		// 而轨道总数是死的（8 条）⇒ 太高的话只能压回去。
		trackH = maxH
	}
	posMS := st.Position.Milliseconds()
	if d.lastPos != 0 && posMS < d.lastPos {
		// ⚠️ **位置往回跳了**（用户把进度条拖回前面）：那八条轨道上记着的
		// 「下一条最早能进」还是**将来**的时刻，于是新弹幕一条都放不进去
		//（屏幕上会一条弹幕都没有）。
		//
		// ⇒ 轨道那一摊清掉重来（字宽那份留着——它跟位置无关）。
		d.lanes = [danmakuTracks]danmakuLane{}
		d.slots = nil
		d.live = 0
	}
	d.lastPos = posMS
	// 「前一条走出 30% 的宽度」换算成时间（毫秒）。
	spacingMS := int64(float64(area.W) * danmakuTrackSpacingRatio * 1000 / danmakuSpeed)

	// 收掉已经整个出界的那些（它们占的轨道要让给后来的）。
	d.expire(posMS)

	// 此刻该在屏幕上的那几条。
	//
	// ⚠️ **窗口是「从它自己的时间到当前位置」**：还没到的（age < 0）不画，
	// 已经走完的（age 超过「(封面宽 + 字宽) / 速度」）也不画。
	vis := s.full.danmakuBuf[:0]
	for _, it := range items {
		if it.ID == "" {
			// 没有 rowid ⇒ 认不出它 ⇒ 不画。
			//
			// ⚠️ **不是「兜底给它一个 key」**：key 必须是**稳定且唯一**的，
			// 而这里没有能当身份的东西（生产上 rowid 一定有，见
			// `internal/service/bili` 的 `Danmaku.ID`）。
			continue
		}
		age := posMS - it.TimeMS
		if age < 0 {
			continue
		}
		// ⚠️ **粗筛只是「上界」**：早该出界的那些不排版（量字宽要排一次版）。
		// 它**不是**一个硬上限——本票评审提过一个 major：一条 15 秒的硬上限
		// 把 40 字的长弹幕在第 17 秒整条滤掉了，而那时它的右缘还在封面里
		//（按 60 px/s 算）。见 `danmakuAgeBoundMS`。
		if age > danmakuAgeBoundMS(it.Text, fontSize, area.W) {
			continue
		}
		w := d.widthOf(c, it.ID, it.Text, fontSize)
		if age > int64((float64(area.W)+float64(w))*1000/danmakuSpeed) {
			continue
		}
		vis = append(vis, danmakuVisible{item: it, ageMS: age, width: w})
	}
	// ⚠️ **按时间排**：轨道是按「谁先入场」定的（B 站那样），所以顺序不能乱。
	danmakuSortByTime(vis)
	s.full.danmakuBuf = vis

	// 给新出现的那些找轨道（老的那几条的位置不动）。
	for i := range vis {
		v := &vis[i]
		slot, known := d.slots[v.item.ID]
		if !known {
			slot = d.assign(v.item, v.width, spacingMS, area.W)
		}
		v.lane = slot.lane
	}

	// 画。
	ui.Box(c).Key(danmakuLayerKey).
		Absolute().Left(area.X).Top(area.Y).Size(area.W, area.H).
		// ⚠️ **裁到封面那一块**：走出左边沿的弹幕直接出界（报告 §3.3：
		// 不淡出）。不裁的话它们会飘到满屏的别处去。
		Clip().
		// ⚠️ **不吃指针**：这一层压在封面上面，而封面要接长按三连和点开满屏
		// （见 `viewFullCover`）。吃了指针的话，用户点封面会点不动。
		PassThrough().
		Children(func() {
			for i := range vis {
				v := &vis[i]
				if v.lane < 0 {
					// 没有轨道可放 ⇒ **丢掉这一条**（报告 §3.3：不排队）。
					continue
				}
				ui.Text(c, v.item.Text).
					// ⚠️ **key 用弹幕自己的 rowid**：稳定且唯一。
					Key(v.item.ID).
					FontSize(fontSize).
					TextColor(danmakuColorOf(v.item.Color)).
					// ⚠️ **不给 Role / Label**：弹幕是**装饰**，读屏软件不该念它
					// （报告 §4）。`RoleNone` 让它不进无障碍树，而 `Tester` 的
					// `Texts()` 照样列得出来（那是 Tester 的查找索引，不是
					// 无障碍树）——测试正是靠它找到弹幕的框的。
					Role(ui.RoleNone).
					// ⚠️ **这一层不吃指针**：弹幕压在封面上，而封面要接长按
					// 三连和点开满屏。
					PassThrough().
					// ⚠️ **宽度给成量出来的那一档**：绝对定位的元素默认按
					// 「父框里放得下的宽」算，而父框是封面那一块（360）——于是
					// 一条长弹幕会被折成两行。给死宽度就不会折。
					Width(v.width).
					SingleLine().
					// ⚠️ **位置是固定的**：入场那一点（封面的右沿）。
					//
					// 位移**全部**交给 pose（见文件头那一段）——位置要是每帧
					// 算在 view 里，弹幕就是 20fps 跳着走的。
					Absolute().Left(area.W).Top(float32(v.lane)*trackH).
					// 🔴 **位移走 pose**：目标是「此刻它该在哪儿」，
					// 中间的帧由框架自己插（60fps 平滑，每帧 1 像素）。
					AnimatePose(ui.Pose{
						X: -float32(v.ageMS) * danmakuSpeed / 1000,
					}, danmakuAnim)
			}
		})
}
