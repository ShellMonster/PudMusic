// 空状态：一页没有内容时画的东西（Biu 的 P-29 / C-02，PLAN.md 里的 S-01）。
//
// **标题逐条抄 Biu 上游**，出处标在 routes 表里每一条的上面。上游的空状态只有
// 一个 title prop（`src/components/empty/index.tsx`），没有第二行，所以这里的
// Hint 是我们自己写的一句话，只回答「怎么才能不空」，不复述标题、也不改上游
// 那句的意思。上游没有专属文案、只落到共享默认 `暂无内容` 的页，这里也照抄
// `暂无内容`：编一句更好听的，等于让这一页和 Biu 对不上，而对不上正是这个
// 项目要避免的。
package ui

import "github.com/egoist/mygo/ui"

// emptyState 是一页空着时说的两句话。
type emptyState struct {
	// Title 是上游那句：这里为什么空。
	Title string
	// Hint 怎么才能不空。空串就只画上面那一句（上游没有第二行）。
	Hint string
}

// viewEmpty 画一页的空状态。key 给这一页的根元素（铁律四）。
//
// 照 Biu 的 `src/components/empty/index.tsx`：整体居中（`items-center
// justify-center`）、上下留白（`py-20`）。上游那枚 `<RiCloseCircleLine
// size={32}/>` 是 Remix 图标字体里的图标，MyGo 没有图标字体，这里不画——
// 少一个图标好过画一个不像的。
//
// 颜色上上游两行都是 `text-default-400`（同一个灰），这里分成主次两档：标题
// 用正文色，第二句用次要色。15px 的浅灰在浅色底上已经偏淡，两行一起淡就分不
// 出主次，而这一页要说的恰恰是「标题是结论、第二句是办法」。
//
// ⚠️ Key 写在构造之前（铁律四的坑）：构造之后再 Key 等于没给。
func (s *Shell) viewEmpty(c *ui.Context, key string, e emptyState) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。SetTheme 只影响它之后
	// 创建的控件，不装的话这里会用 MyGo 在帧开头重置好的系统色画出来，跟旁边
	// 的界面对不上。多调一次没有副作用——applyFrameTheme 认得出「这一帧已经
	// 装过了」，见它上面的注释。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key(key).Grow(1).Center().Gap(6).Padding(20, 28).Children(func() {
		ui.Text(c, e.Title).FontSize(15).TextColor(t.Text)
		if e.Hint != "" {
			ui.Text(c, e.Hint).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// lyricsEmpty 是歌词面板空着时说的话。
//
// 上游 `src/components/lyrics/index.tsx:375` 是一句三选一：「歌词加载中...」
// /「暂无歌词」/ 歌词本身，这里取空的那一支。Hint 留空：上游就是只有这一句，
// 补一句「去哪儿找歌词」等于替上游许一个它没有的承诺。
//
// 歌词面板本身是阶段 6 的东西（PLAN.md 的 C-40 一带），这一张先把文案和画法
// 定下来，等它接上来的时候直接用。
var lyricsEmpty = emptyState{Title: "暂无歌词"}
