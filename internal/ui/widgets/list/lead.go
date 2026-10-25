// 页内头部（lead）：这一份歌**上面**那一块。
//
// # 它和框架那个 `ListState.Header` 是两件相反的事
//
// 框架那个是「分节的表头」：**钉在列表顶上不动**，下面那一节的行从它底下滚过去
// （`mygo/ui/list.go` 的 `pinHeader`）。这里要的是**反过来的**那一件：上面那一块
// 跟着内容一起滚走，滚一段就没了——
//
// 上游那一页就是这么摆的（`music-recommend/index.tsx` 的
// `{activeTab === "pop" && <NewMusicTop … />}` 和下面的列表在**同一个滚动容器**
// 里，顶图跟着滚走）。我们此前把那一块摆在列表**外面**（它是列表的兄弟），于是
// 滚动列表时它一动不动——票 #369 修的就是这个。
//
// # ⚠️ **它占第 0 行**
//
// 所以下面那些歌在框架那一层的**行号**整体挪了一格。挪动的量全仓只有一处算它
// （`leadRows`），而**对外那些 API 收的仍然是「歌的下标」**——见 `List.Selected`、
// `List.At`、`List.ScrollTo`、`Grid.ScrollToItem`。
//
// # 为什么不给别的页面用
//
// 它是**按需装**的（`SetLead`）：没装的那一份（其余六页）一个字节都不多画，
// 行号也不挪（`leadRows` 是 0）。
package list

import "github.com/egoist/mygo/ui"

// Lead 是「这一份歌上面那一块」。
type Lead struct {
	// Draw 画这一块。
	//
	// ⚠️ **它画在列表的滚动区里**：所以它跟着内容一起滚走（这正是这一块要的事）。
	//
	// ⚠️ **它可能被框架刷上那一块实心主色底**（点它一下就是选中了第 0 行）——
	// 所以这一块自己刷一层页面底色把它盖掉（见 `List.viewLead`）。
	Draw func(c *ui.Context)

	// Label 是这一行给无障碍和「打字选中」用的名字。
	//
	// ⚠️ 空串是「没有名字」：那一行在读屏软件里念不出是什么，而它装的东西
	// （卡片）各自有自己的名字。
	Label string

	// Height 是这一块有多高（像素）。
	//
	// ⚠️ **它是估的**：这一块的高度跟着窗口的宽高走（封面是方的），而回顶按钮
	// 判断「滚过 400 像素没有」靠它（`List.ScrollOffset`）。差几十像素的后果只是
	// 那枚按钮早一点或晚一点出现——而它本来就是估的（网格那一档的行高也是估的，
	// 见 `Grid.ScrollOffset`）。
	Height func() float32
}

// leadRows 是「页内头部」占几行：0（没有）或 1。
//
// ⚠️ **全仓只有这一处算它**：行号挪动的地方（`Key` / `Label` / `viewRow` /
// `ScrollTo` / `ScrollOffset` / `keepAnchor`）全都要用到它，而每一处自己再算一遍
// 就是一处可能算错的地方。
func leadRows(l *Lead) int {
	if l == nil {
		return 0
	}
	return 1
}

// leadHeight 是「页内头部」有多高。**没装是 0**。
func leadHeight(l *Lead) float32 {
	if l == nil || l.Height == nil {
		return 0
	}
	return l.Height()
}

// leadKey 是页内头部那一行的身份。
//
// ⚠️ **不用字符串**：歌的 ID 是 bvid / 完整路径之类的用户数据，理论上能撞上一个
// 叫 "lead" 的；类型撞不上（和 `footerKey` 同一个理由）。
type leadKey struct{}

// leadFailed 是页内头部那一行在「画崩了」那张表里的键。
//
// ⚠️ **它和歌 / 哨兵的 ID（用户数据，bvid 或完整路径）不是一个来源**，所以带一个
// 前缀——撞上了的话「崩过的那一块」会把某一首歌也一起短路掉。
const leadFailed = "biu:lead"
