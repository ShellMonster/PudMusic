package ui

import (
	"github.com/egoist/mygo/ui"
)

// 队列抽屉那一列的**拖拽重排**（票 #322 的 B5）。
//
// # 为什么不用框架的 ListState.Reorder
//
// 框架那一套（`mygo/ui/list.go` 的 reorder）做得比我这整套好：插入线画在行的
// 边界上、拖着的那一行半透明、拖到边上会自动滚、落在原处等于没动——它都替我
// 想好了。
//
// 但它**每一行每帧都要新建一个闭包**（`w.dragFrom(func() any {…})`），而队列
// 抽屉一屏有十来行：那是**每帧十四次分配**（实测那一档 45 → 59），而每帧分配
// 是本项目唯一的性能判据（见 framebudget_test.go，队列抽屉那一档的上限是
// 50 次）。
//
// 所以这里自己接，三条规矩：
//
//   - **拖动值传指针**。框架把拖动值装进 `any`，装指针不分配（装字符串或下标
//     都分配——而每一行每帧都要传一次）。
//   - **每一行的拖动值记着**（rowkey.Memo）：指针要稳，稳了才不用每帧新建。
//   - **框架的 Drop 每帧只问一次**（问这一列，不问每一行）：它每问一次都要
//     新建一个闭包（它要抓住类型），每行每帧问一次就是每帧二十几次分配。
//   - **框架的 DragOver 一句都不问**：「有东西正被拖着」由**每一行自己报**
//     （`rowEl.Dragging()`，那一行本来就在问它——拖着的那一行要画成半透明），
//     而 DragOver 每问一次同样要新建一个闭包（见 queue_drawer.go 里那两句）。
//
// # 松手才提交
//
// 拖动的过程中队列一个字节都不动。落点那一帧才动队列——拖一下就改一次队列的
// 话，用户每帧都在被改，而「正在放的那首」会跟着乱跳。那个跳动是**静默**的：
// 播放条上写着甲，抽屉里高亮的是乙，响着的还是甲。
//
// # 「挪到哪一首歌之前」而不是「挪到第几行」
//
// 抽屉里同一首只画一行（见 queueRowsFor 的去重），所以行号和队列的下标不是
// 一回事。认的是**判重标识**——队列认的是「哪一首歌」，抽屉画的也是「哪一首歌」
// （铁律四）。

// queueDragValue 是拖动时传的东西：被拖的是**哪一首歌**。
//
// ⚠️ **只放判重标识**：下标和队列的下标对不上（见上面那段），判重标识才是两边
// 都认的那个键。
type queueDragValue struct {
	id string
}

// newQueueDragValue 造一个拖动值。
//
// ⚠️ **不能有捕获**：它被当成 rowkey.Memo 的 pack 传（每帧每一行都问一次），
// 有捕获的话那个闭包每次都是新的一份——那就是每帧每行一次分配。
func newQueueDragValue(id string) *queueDragValue { return &queueDragValue{id: id} }

// queueDragFor 是第 i 行那个拖动值。
//
// ⚠️ **指针要稳**：它被装进拖动值里带着走（拖动开始那一刻取的就是它），所以
// 不能每帧新建一个。rowkey.Memo 记着它——身份没变就是同一份（见 rowkey 的
// 说明：快的那条路上零分配）。
func (s *Shell) queueDragFor(i int, row queueRow) *queueDragValue {
	return s.queueDrags.At(i, row.id, newQueueDragValue)
}

// queueDrop 是「松手」那一下：把被拖的那首挪到落点。
//
// 落点是哪一行、那一行的哪一半，是**各行自己记的**（见 viewQueueRow 里那几句
// PointerPosition）：每一行都知道指针是不是在它身上、在它的哪一半。
func (s *Shell) queueDrop(v *queueDragValue) {
	if v == nil || v.id == "" {
		// 认不出是哪一首：不动（挪了也对不上任何一行）。
		return
	}
	at := s.queueDropAt
	if at < 0 || at >= len(s.queueRows) {
		return
	}
	before := ""
	if s.queueDropUpper {
		// ⚠️ **用行上记好的那一份**（`row.id`）：它是同一首歌的判重标识，而现算
		// 的话要拼一次字符串（理由同 `currentRow` 里那一处，票 #339）。
		before = s.queueRows[at].id
	} else if at+1 < len(s.queueRows) {
		before = s.queueRows[at+1].id
	}
	// before 是空串 = 挪到队尾（落在最后一行的下半）。
	if before == v.id {
		// 挪到自己前面：不动（那本来就是它待着的地方）。
		return
	}
	if !s.app.Queue().MoveSongBefore(v.id, before) {
		return
	}
	// 队列变了，抽屉要重画。
	//
	// ⚠️ **必须叫醒**：这一帧的行已经建好了（画的是旧顺序），队列是在这一帧的
	// 中途改的——不叫醒的话画面会停在旧顺序，直到下一次别的什么东西碰巧刷新
	//（而即时模式下那种刷新可能一直不来）。
	s.wake.invalidate()
}

// queueDropLine 是拖动时画的那条插入线：落在哪一行、哪一边，就画在哪一行的
// 边界上。
//
// ⚠️ **只在拖动悬在这一行上时才画**：那个闭包是每帧一次分配（它要抓住「上半还
// 是下半」），而拖动悬在别的行上的时候它没有用。
func (s *Shell) queueDropLine(at int, rowEl ui.Element, accent ui.Color) {
	upper := s.queueDropUpper
	rowEl.DrawOver(func(p *ui.Painter, r ui.Rect) {
		y := r.Y
		if !upper {
			y = r.Y + r.H
		}
		// 一条两像素的主色线（框架那一套画的是同一条，见 mygo/ui/list.go 的
		// reorder）。
		p.Fill(ui.Rect{X: r.X + 4, Y: y - 1, W: r.W - 8, H: 2}, accent, 1)
	})
}
