// 「同一份歌，列表 / 网格按设置换着摆」的那一对控件。
//
// # 为什么不干脆让 `List` 自己画网格
//
// 因为网格那一档有三样东西是列表没有的，而它们住在 `grid.go` 里：列数按容器宽
// **分档**换（不是连续算出来的）、换列数时**保住滚动锚点**（`keepAnchor`）、格子
// 的单击 / 双击 / 右键三件手势。把它们搬进 `List` 就是把一个已经落进主干的类型
// 重写一遍——而这一票的边界写得很清楚：不重排界面、不顺手重构刚做完的功能。
//
// 所以是**一对**：两份控件共用同一份歌（歌只存一份进两边，见 SetSongs），每一帧
// 画哪一个由设置决定。`PagedList` 和首页那几个列表页都建在它上面。
//
// # 为什么两份都存歌，而不是只给正在画的那一份
//
// 页面上问的是「这些歌里有哪些」——收藏夹那一页的多选、全选、批量删除都走
// `List().At(i)` / `Len()`（`internal/ui/collectionbatch.go`），而那些代码**不
// 知道此刻画的是哪一档**。只给正在画的那一份存的话，切到网格时那些调用会当场读到
// 空列表——症状是「按了全选，一首都没勾上」。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// SwitchView 是同一份歌的两种摆法，按设置分发。
//
// **建一次，存进页面自己的状态里，每一帧调 View**。理由和 `List`/`Grid` 各自那条
// 一样：新建一个等于每帧跳回顶上（`ui.ListState` 记着滚动位置和选中项）。
type SwitchView struct {
	list *List
	grid *Grid

	// view 是此刻画哪一个。零值是列表，和设置的默认值一致。
	view View
}

// NewSwitchView 建一对（列表 + 网格）。
//
// ⚠️ cache / acts 的要求和 `New` 一样：封面走 imgcache（nil 会让**带封面的每一
// 行 / 每一格**画不出来），操作走右键菜单和双击。
func NewSwitchView(cache *imgcache.Cache, acts Actions) *SwitchView {
	return &SwitchView{list: New(cache, acts), grid: NewGrid(cache, acts)}
}

// List 是底下那个列表。页面要选中项、要滚到某一行、要接多选时用它。
//
// ⚠️ 网格那一档画的时候**它照样是有歌的**（见文件头），所以 `At` / `Len` 在两档
// 里都对。
func (s *SwitchView) List() *List { return s.list }

// Grid 是底下那个网格。翻页那一层要问它「末尾那行进没进画面」。
func (s *SwitchView) Grid() *Grid { return s.grid }

// SetView 换摆法。**每帧调**（设置随时会变），理由同 `List.SetView`。
func (s *SwitchView) SetView(v View) {
	if v == s.view {
		return
	}
	s.view = v
	// 两边都要装：网格那一档在 `Grid` 上，退回列表时 `List` 得知道自己该紧凑还是
	// 该正常。
	s.list.SetView(v)
}

// Layout 返回此刻的摆法。
func (s *SwitchView) Layout() View { return s.view }

// SetColumns 换这一页多画哪两列。**只装在列表那半边**：网格卡片没有列，
// 播放量走封面底部那一条渐变，而时间那一列网格里本来就不画。
func (s *SwitchView) SetColumns(c Columns) { s.list.SetColumns(c) }

// SetSongs 换掉这份歌。**两份控件都换**（见文件头）。
func (s *SwitchView) SetSongs(songs []Music) {
	s.list.SetSongs(songs)
	// ⚠️ 别调 `Grid.SetSongs`：它会再查一遍重复 ID，于是同一个问题在一帧里打两行
	// 一样的日志。查重在 List 那一次已经做完了（两份拿到的是同一个切片）。
	s.grid.setSongs(songs, false)
}

// SetEmpty 换掉「没有歌时说的那句话」，两份都换。
func (s *SwitchView) SetEmpty(text string) {
	s.list.SetEmpty(text)
	s.grid.SetEmpty(text)
}

// SetEmptyView 换掉「没有歌时**那一格**」的画法，两份都换。理由见
// `List.SetEmptyView`。
//
// ⚠️ **传 nil 是退回那句话**（`SetEmpty` 那一句，或默认那句）。
func (s *SwitchView) SetEmptyView(fn func(c *ui.Context)) {
	s.list.SetEmptyView(fn)
	s.grid.SetEmptyView(fn)
}

// SetFooter 装末尾哨兵的画法，两份都装。理由见 `Grid.footer` 那一段——**两份都要装**
// 是因为网格那半边没有哨兵就没有失败之后的重试按钮（`blocked` 闩住的是自动重试）。
//
// ⚠️ 别把理由写成「网格没有哨兵就翻不了页」：翻页的判据是「末尾那一行进没进画面」，
// 而网格里最后一行就是歌的行。那句话说错了半年，是靠把 `SetFooter` 改成只装列表
// 那半边、发现上面那几条测试照样绿才查出来的。
//
// ⚠️ 只装一半的另一个后果：`TestTheGridCountsTheSentinelAsARow` 直接对 `*Grid`
// 调 SetFooter，**绕过了这一层**，所以它抓不到这一种漏。
func (s *SwitchView) SetFooter(fn func(c *ui.Context)) {
	s.list.SetFooter(fn)
	s.grid.SetFooter(fn)
}

// SetFooterLabel 换末尾那一行给无障碍和「打字选中」用的名字，两份都换。
// 理由同 `SetFooter`（末尾那一行在两档里各有一份）。
func (s *SwitchView) SetFooterLabel(name string) {
	s.list.SetFooterLabel(name)
	s.grid.SetFooterLabel(name)
}

// SetScroll 让两份都把滚动位置写进 s。同一份 `ScrollState` 只给正在画的那一个用
// （没在画的那个压根不调 TrackScroll，见 `List.View` 末尾），所以不打架。
func (s *SwitchView) SetScroll(st *ui.ScrollState) {
	s.list.SetScroll(st)
	s.grid.SetScroll(st)
}

// SetSelection 装多选那一摊接线，两份都装（理由见 `Grid.sel2`）。
func (s *SwitchView) SetSelection(sel Selection) {
	s.list.SetSelection(sel)
	s.grid.SetSelection(sel)
}

// Len 是这份歌有几首。
func (s *SwitchView) Len() int { return len(s.list.songs) }

// At 返回第 i 首那一首，没有是 ok=false。
//
// ⚠️ 收的是**歌的下标**（两档里都是同一个意思，因为两份控件拿到的是同一份歌），
// 不是「第几格」——格子是布局，列数一变同一首歌就换了格子（铁律四）。
func (s *SwitchView) At(i int) (Music, bool) { return s.list.At(i) }

// View 画这一份歌。摆法是列表 / 紧凑时走 `List`，网格那一档走 `Grid`。
//
// ⚠️ **不要在这一层调 `SetSongs`**：两份控件各有各的选中项和滚动位置，而 `SetSongs`
// 会顺手重挑选中项（`Grid.SetSongs` 里那个 `indexOfID`）。每一帧换一次歌的话，
// 用户点的那一格每帧都被重挑一次——看起来像是「点不中」。
func (s *SwitchView) View(c *ui.Context) {
	if s.view == ViewCard {
		s.grid.View(c)
		return
	}
	s.list.View(c)
}

// TakeFocus 把键盘焦点交给**此刻画的那一档**（票 #245 的 E4）。
//
// ⚠️ **只交给此刻画的那一档**：另一档的元素这一帧根本没建，焦点给它等于
// 给一个不在画面上的东西焦点。
func (s *SwitchView) TakeFocus() {
	if s.view == ViewCard {
		s.grid.TakeFocus()
		return
	}
	s.list.TakeFocus()
}

// Hot 报指针是不是压在底下这一对控件的滚动条上（见 `List.Hot`）。
//
// 两档各有一份滚动容器（同一份歌、两种摆法），所以问的是**这一帧画的那一档**。
func (s *SwitchView) Hot() bool {
	if s.view == ViewCard {
		return s.grid.Hot()
	}
	return s.list.Hot()
}

// rows 是此刻画出来的**行**数，含末尾哨兵。
//
// 网格那一档的行数是按列数算出来的，所以切档时它会变——`PagedList` 的三重保障
// 问的「末尾那行进没进画面」用的是它，而问的时机在画完之后（`Visible` 报的是
// 上一帧），所以列数换档那一帧读到的是旧列数下的行数，下一帧自己对上。
func (s *SwitchView) rows() int {
	if s.view == ViewCard {
		return s.grid.rows()
	}
	return s.list.rows()
}

// visible 是**上一帧**画出来的那几行。
func (s *SwitchView) visible() (first, last int) {
	if s.view == ViewCard {
		return s.grid.visible()
	}
	return s.list.state.Visible()
}

// scrollTop 回到顶上。**两份都回**：它们各有各的 `ui.ListState`，只回一份的话，
// 另一份下次被画出来时还停在上次那个位置，而用户以为「回顶」是回到整页的顶上。
//
// ⚠️ **换档本身不回顶**（`SetView` 不调它）：两份各自记着自己停在哪，切回去时回到
// 原处——和同一个 Tab 切走再切回来是同一条规矩。
func (s *SwitchView) scrollTop() {
	s.list.ScrollTop()
	s.grid.scrollTop()
}

// Selected 返回选中的那首。没有选中是 ok=false。
//
// ⚠️ 认的是**正在画的那一份**：用户能看见的光标在它上面，而另一份的 `sel` 是上
// 一回画它的时候留下的——拿它说「你选的是这一首」会给出一个用户看不见的东西。
func (s *SwitchView) Selected() (Music, bool) {
	if s.view == ViewCard {
		return s.grid.Selected()
	}
	return s.list.Selected()
}

// ScrollTo 把这一份滚到某一行。align 见 `List.ScrollTo`。
//
// ⚠️ 网格那一档收的是**行号**，而网格的行和列表的行不是一回事（网格一行几首）——
// 翻页那一层用的是 `PagedList.ScrollTo`，那里已经处理过了。
func (s *SwitchView) ScrollTo(row int, align ui.Align) {
	if s.view == ViewCard {
		g := s.grid
		g.state.ScrollTo(row, align)
		return
	}
	s.list.ScrollTo(row, align)
}

// ScrollOffset 是这一屏滚过了多少像素。回顶按钮判断「滚过 400 像素没有」用它。
//
// ⚠️ **认的是正在画的那一份**：用户看得见的是它，别的档停在哪儿与它无关。
func (s *SwitchView) ScrollOffset() float32 {
	if s.view == ViewCard {
		return s.grid.ScrollOffset()
	}
	return s.list.ScrollOffset()
}
