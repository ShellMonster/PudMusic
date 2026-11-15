// 满屏播放器的封面墙：把播放队列铺成一面错缝的封面墙，盖在满屏中间那一块（封面 + 歌词）上。
//
// 墙比视口大时，拖它、甩它、在触控板上两指推它都能两个方向走；点一张封面就从那一首放起。
// 打开时正在放的那一首在视口正中；之后换歌，只有新那一首不在视口里才滑过去。墙的四边是
// 柔的：封面越靠近视口边越淡，切在边上的那一截不会是一刀硬边。
//
// # 一首一格
//
// 合集（`Item.GroupKey`）在墙上**不并成一格**：队列本身就是一首一项，`App.PlayAt` 认的也是
// 队列下标，并成一格的话第 2 集起就点不到了。「这几项是一组」是抽屉画列表时归拢出来的
// 界面概念（见 `queueGroup`），墙不需要它。
//
// # 只画看得见的
//
// 布局是纯函数（`layWall`：项数 + 视口 → 每一格的矩形），每一帧只建落在视口里的那几格，
// 每一格的 key 是队列项的判重标识加它是第几份（和抽屉的行 key 同一个口径），换歌、重排
// 都不会让悬停 / 图片状态错到隔壁那一格。
//
// # 静止不要帧
//
// 拖动和甩出去之后的滑行都交给框架的 `Draggable`（弹簧、橡皮筋、按住接住）；触控板的
// 位移在输入事件里直接落到 `pan`。两样都停下来之后，墙不再要帧。
package ui

import (
	"math"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

const (
	// wallGap 是两格之间的缝。
	wallGap = space.Px16
	// wallPad 是墙停下来时最外一圈封面离视口边的距离。
	wallPad = space.Px40
	// wallFade 是柔边的宽：封面离视口边不到这么远就开始变淡。
	wallFade = space.Px40
	// wallCaptionH 是墙下面那一行字（悬停的那一首 / 正在放的那一首）占的高。
	wallCaptionH = 36
)

// wallGlide 是甩出去之后的滑行和「滑到正在放的那一首」那一段。
var wallGlide = ui.Animation{Spring: ui.SpringOptions{Duration: 700 * time.Millisecond}}

// wallHoverAnim 是悬停那一格放大 / 复原那一下。
var wallHoverAnim = ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut}

// wallTileFor 是一格的边长：随视口宽分三档。
func wallTileFor(viewW float32) float32 {
	switch {
	case viewW <= 760:
		return 96
	case viewW <= 1180:
		return 120
	}
	return 136
}

// wallLayout 是 n 格封面在 viewW×viewH 的视口里怎么排。坐标都在「墙」自己的坐标系里
// （左上角那一格的左上角是原点）；墙在视口里的位置是 offset（墙原点在视口里的坐标）。
type wallLayout struct {
	n, cols, rows int
	tile, pitch   float32
	viewW, viewH  float32
	// w, h 是墙本身的宽高（多行时含奇数行错出去的那半格）。
	w, h float32
	// bounds 是 offset 能停的范围：放得下的那一轴宽 / 高为 0，墙在那一轴上居中不动。
	bounds ui.Rect
}

// layWall 排 n 格封面。
//
// 一屏放得下就按放得下排：一行放得下就一行，否则行数取最少、每行尽量一样多，整面墙居中、
// 拖不动。放不下就排成一面比视口更扁的墙（列数约为 √(n × 视口宽高比 × 1.5)），两个方向
// 都要走。奇数行往右错半格，像砌砖。
func layWall(n int, viewW, viewH float32) wallLayout {
	l := wallLayout{n: n, viewW: viewW, viewH: viewH, tile: wallTileFor(viewW)}
	l.pitch = l.tile + wallGap
	if n <= 0 {
		return l
	}
	inner := func(view float32) float32 { return view - 2*wallPad + wallGap }
	oneRow := max(1, int(inner(viewW)/l.pitch))
	perRow := max(1, int((inner(viewW)-l.pitch/2)/l.pitch))
	rowsFit := max(1, int(inner(viewH)/l.pitch))
	switch {
	case n <= oneRow:
		l.cols = n
	case n <= perRow*rowsFit:
		rows := (n + perRow - 1) / perRow
		l.cols = (n + rows - 1) / rows
	default:
		aspect := float64(max(viewW/max(viewH, 1), 1)) * 1.5
		l.cols = min(n, max(perRow, int(math.Ceil(math.Sqrt(float64(n)*aspect)))))
	}
	l.rows = (n + l.cols - 1) / l.cols
	l.w = float32(l.cols)*l.pitch - wallGap
	if l.rows > 1 {
		l.w += l.pitch / 2
	}
	l.h = float32(l.rows)*l.pitch - wallGap
	l.bounds.X, l.bounds.W = wallSpan(l.w, viewW)
	l.bounds.Y, l.bounds.H = wallSpan(l.h, viewH)
	return l
}

// wallSpan 是一条轴上 offset 能停的范围 [lo, lo+span]：放得下就居中、span 为 0；放不下就从
// 「最后一格离对边 wallPad」到「第一格离这边 wallPad」。
func wallSpan(size, view float32) (lo, span float32) {
	if size+2*wallPad <= view {
		return (view - size) / 2, 0
	}
	return view - wallPad - size, size + 2*wallPad - view
}

// stagger 是第 row 行往右错出去多少。
func (l wallLayout) stagger(row int) float32 {
	if l.rows > 1 && row%2 == 1 {
		return l.pitch / 2
	}
	return 0
}

// cell 是第 i 格在墙上的矩形。
func (l wallLayout) cell(i int) ui.Rect {
	row, col := i/l.cols, i%l.cols
	return ui.Rect{X: float32(col)*l.pitch + l.stagger(row), Y: float32(row) * l.pitch, W: l.tile, H: l.tile}
}

// clamp 把 offset 收进 bounds。
func (l wallLayout) clamp(p ui.Point) ui.Point {
	b := l.bounds
	return ui.Point{X: max(b.X, min(p.X, b.X+b.W)), Y: max(b.Y, min(p.Y, b.Y+b.H))}
}

// centre 是把第 i 格摆到视口正中的 offset（墙走不了那么远就停在边上）。
func (l wallLayout) centre(i int) ui.Point {
	r := l.cell(i)
	return l.clamp(ui.Point{X: l.viewW/2 - r.X - r.W/2, Y: l.viewH/2 - r.Y - r.H/2})
}

// shows 报墙在 at 时第 i 格是不是整个在视口里。
func (l wallLayout) shows(i int, at ui.Point) bool {
	r := l.cell(i)
	x, y := r.X+at.X, r.Y+at.Y
	return x >= 0 && y >= 0 && x+r.W <= l.viewW && y+r.H <= l.viewH
}

// visible 是墙在 at 时落在视口里（哪怕一角）的行、列范围，闭区间；没有就 r0 > r1。
// 列范围按错开的半格多放一列。
func (l wallLayout) visible(at ui.Point) (r0, r1, c0, c1 int) {
	if l.n == 0 {
		return 0, -1, 0, -1
	}
	floor := func(v float32) int { return int(math.Floor(float64(v))) }
	r0 = max(0, floor((-at.Y+wallGap)/l.pitch))
	r1 = min(l.rows-1, floor((l.viewH-at.Y)/l.pitch))
	c0 = max(0, floor((-at.X-l.pitch/2+wallGap)/l.pitch))
	c1 = min(l.cols-1, floor((l.viewW-at.X)/l.pitch))
	return
}

// hit 是墙上 (x, y) 那一点落在哪一格；落在缝里或墙外是 -1。
func (l wallLayout) hit(x, y float32) int {
	if l.n == 0 || x < 0 || y < 0 {
		return -1
	}
	row := int(y / l.pitch)
	if row >= l.rows || y-float32(row)*l.pitch > l.tile {
		return -1
	}
	x -= l.stagger(row)
	if x < 0 {
		return -1
	}
	col := int(x / l.pitch)
	if col >= l.cols || x-float32(col)*l.pitch > l.tile {
		return -1
	}
	if i := row*l.cols + col; i < l.n {
		return i
	}
	return -1
}

// wallEdgeFade 是视口里矩形 r 那一格的不透明度：离视口四边都超过 wallFade 是 1，中心
// 压到边上是 0，中间按 smoothstep 过渡。
func wallEdgeFade(r ui.Rect, viewW, viewH float32) float32 {
	d := min(r.X, r.Y, viewW-r.X-r.W, viewH-r.Y-r.H)
	half := min(r.W, r.H) / 2
	t := max(0, min(1, (d+half)/(wallFade+half)))
	return t * t * (3 - 2*t)
}

// wallPan 是墙在视口里的位置，跨帧留着。
//
// 墙此刻在 shown + pan：shown 是 `Draggable` 这一帧把它摆在哪（拖着、滑着或停着），off 是
// 它要停在哪（拖动 / 甩出去 / 「滑到正在放的那一首」都改它）；pan 是触控板推出来的那一截，
// 叠在上面、不经过弹簧——手指推多少墙就走多少。
type wallPan struct {
	off, pan, shown ui.Point
	// centred 是上一次照顾到的正在放的那一首（格子的 key）；空串 = 刚打开、还没摆过。
	centred string
	// moving 是上一次建墙时它还在走（滑行 / 回弹）没有；caught 是这一次按下去的时候
	// 它还在走——那一按是「接住」，不是「点这一首」。
	moving, caught bool
}

// at 是墙此刻在视口里的位置。
func (p *wallPan) at() ui.Point {
	return ui.Point{X: p.shown.X + p.pan.X, Y: p.shown.Y + p.pan.Y}
}

// follow 照顾正在放的那一首：刚打开时直接把它摆在正中；之后它换了，而新那一首不整个在视口
// 里，墙滑过去把它带到正中。key 没变就什么都不做（一帧建三遍也只动一次）。
func (p *wallPan) follow(l wallLayout, cur int, key string) {
	if cur < 0 || cur >= l.n || key == p.centred {
		return
	}
	switch {
	case p.centred == "":
		p.pan = ui.Point{}
		p.off = l.centre(cur)
		p.shown = p.off
	case !l.shows(cur, p.at()):
		to := l.centre(cur)
		p.off = ui.Point{X: to.X - p.pan.X, Y: to.Y - p.pan.Y}
	}
	p.centred = key
}

// scroll 是触控板 / 滚轮推了 (dx, dy)：滑行停在此刻的位置，墙跟着手指走，到边为止。
func (p *wallPan) scroll(l wallLayout, dx, dy float32) {
	p.off = p.shown
	to := l.clamp(ui.Point{X: p.shown.X + p.pan.X - dx, Y: p.shown.Y + p.pan.Y - dy})
	p.pan = ui.Point{X: to.X - p.shown.X, Y: to.Y - p.shown.Y}
}

// queueWall 是满屏里封面墙那一摊：开着没有、墙的位置、各格的 key。
type queueWall struct {
	open bool
	pan  wallPan
	// keys 是各格的 key，按队列的 revision 记着（每帧现拼是每帧每格一次分配）。
	keys    []string
	keysRev uint64
	keysN   int
}

// toggle 开 / 关封面墙。每次打开都重新把正在放的那一首摆到正中。
func (w *queueWall) toggle() {
	w.open = !w.open
	w.pan = wallPan{}
}

// wallKeyLabel 是顶栏上封面墙那枚键的名字。
func wallKeyLabel(on bool) string {
	if on {
		return "收起封面墙"
	}
	return "以封面墙查看播放队列"
}

// keysFor 是这一份快照里各格的 key：判重标识 + 它是第几份（同一首重复入队时各占一格）。
func (w *queueWall) keysFor(snap app.QueueSnapshot) []string {
	if w.keys != nil && w.keysRev == snap.Revision() && w.keysN == snap.Len() {
		return w.keys
	}
	n := snap.Len()
	keys := make([]string, n)
	seen := make(map[string]int, n)
	for i := range n {
		it, _ := snap.At(i)
		id := it.Identity()
		keys[i] = "wall:" + queueRowKey(id, seen[id], i)
		seen[id]++
	}
	w.keys, w.keysRev, w.keysN = keys, snap.Revision(), n
	return keys
}

// viewQueueWall 画封面墙：满屏中间那一块（顶栏下沿到底栏上沿），下面一行字。
func (s *Shell) viewQueueWall(c *ui.Context, st app.State, g fullGeo) {
	w := &s.full.wall
	p := tok.OfFull(c.Theme())
	snap := s.app.Queue().Snapshot()
	keys := w.keysFor(snap)
	viewW := g.winW
	viewH := max(0, g.mainBottom-g.mainTop-wallCaptionH)
	l := layWall(snap.Len(), viewW, viewH)
	cur := -1
	if st.HasTrack {
		cur = snap.CurrentIndex()
	}
	if cur >= 0 && cur < len(keys) {
		w.pan.follow(l, cur, keys[cur])
	}

	hover := -1
	area := ui.Box(c).Key("full-wall").Absolute().Left(0).Top(g.mainTop).Width(viewW).Height(viewH).Clip().
		Role(ui.RoleGroup).Label("播放队列封面墙")
	area.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputScroll:
			if ev.Mods&ui.Ctrl != 0 {
				return false
			}
			w.pan.scroll(l, ev.DX, ev.DY)
			return true
		case ui.InputPointerDown:
			w.pan.caught = w.pan.moving
		}
		return false
	})
	area.Children(func() {
		bounds := l.bounds
		bounds.X -= w.pan.pan.X
		bounds.Y -= w.pan.pan.Y
		plane := ui.Box(c).Key("full-wall-plane").Absolute().Left(w.pan.pan.X).Top(w.pan.pan.Y).Size(l.w, l.h).
			Draggable(&w.pan.off, ui.DraggableOptions{
				Bounds:    &bounds,
				Snap:      func(p ui.Point) ui.Point { return p },
				Animation: wallGlide,
			})
		w.pan.shown = plane.DragOffset()
		w.pan.moving = plane.Transitioning()
		at := w.pan.at()
		if px, py, over := plane.PointerPosition(); over && !plane.Dragging() {
			if hover = l.hit(px, py); hover >= 0 {
				plane.Cursor(ui.CursorPointer)
			}
		}
		if plane.Clicked() && hover >= 0 && !w.pan.caught {
			if err := s.app.PlayAt(hover); err != nil {
				s.addToast("放不了这一首："+err.Error(), notice.Danger)
			}
		}
		r0, r1, c0, c1 := l.visible(at)
		plane.Children(func() {
			for row := r0; row <= r1; row++ {
				for col := c0; col <= c1; col++ {
					i := row*l.cols + col
					if i >= l.n || i >= len(keys) {
						break
					}
					it, _ := snap.At(i)
					s.viewWallCell(c, l, i, keys[i], it, at, i == cur, i == hover, p.Accent)
				}
			}
		})
	})
	// 墙整面放得下（竖向不动）时字贴在墙下面；放不下时在视口下沿。
	capTop := g.mainTop + viewH
	if l.n > 0 && l.bounds.H == 0 {
		capTop = g.mainTop + l.bounds.Y + l.h + wallGap/2
	}
	s.viewWallCaption(c, snap, cur, hover, capTop, viewW)
}

// viewWallCell 画墙上的一格：封面（没有就是一枚唱片图标的占位），正在放的那一格一圈主色，
// 悬停的那一格放大一点；离视口边越近越淡。
func (s *Shell) viewWallCell(c *ui.Context, l wallLayout, i int, key string, it app.Item, at ui.Point,
	current, hovered bool, accent ui.Color) {
	r := l.cell(i)
	fade := wallEdgeFade(ui.Rect{X: r.X + at.X, Y: r.Y + at.Y, W: r.W, H: r.H}, l.viewW, l.viewH)
	lift := float32(1)
	if hovered {
		lift = 1.05
	}
	cell := ui.Box(c).Key(key).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(tok.R8).
		Background(tok.Full.Surface2).Opacity(fade).Label(it.Title).
		AnimatePose(ui.Pose{Scale: lift}, wallHoverAnim)
	cell.Children(func() {
		img := s.pageImages()
		if it.Cover != "" && img.GetSized(it.Cover, r.W, r.H).Ready() {
			imgcache.ViewFlat(c, img, it.Cover, r.W, r.H, tok.R8)
		} else {
			ui.Box(c).Size(r.W, r.H).Center().Children(func() {
				ui.Icon(c, pud.Icon("disc")).FontSize(r.W * 0.3).TextColor(tok.Full.Text3)
			})
		}
		if current {
			ui.Box(c).Absolute().Left(0).Top(0).Size(r.W, r.H).Radius(tok.R8).Border(2, accent)
		}
	})
}

// viewWallCaption 是墙下面那一行字：悬停在哪一格就写哪一首，否则写正在放的那一首。
func (s *Shell) viewWallCaption(c *ui.Context, snap app.QueueSnapshot, cur, hover int, top, w float32) {
	at := cur
	if hover >= 0 {
		at = hover
	}
	it, ok := snap.At(at)
	if !ok {
		return
	}
	text := it.Title
	if it.Artist != "" {
		text += " · " + it.Artist
	}
	col := tok.Full.Text2
	if at == cur {
		col = tok.Full.Text
	}
	ui.Row(c).Key("full-wall-caption").Absolute().Left(fullPadX(w)).Top(top).Width(w - 2*fullPadX(w)).Height(wallCaptionH).
		Justify(ui.Center).AlignItems(ui.Center).PassThrough().Children(func() {
		ui.Text(c, text).FontSize(scale.Font(c, scale.Size14)).TextColor(col).SingleLine().Ellipsis("…")
	})
}
