// 评论页的**入口**：播放条上那一枚键 + 满屏播放器里那一枚。
//
// # 两个入口，两条规矩
//
//  1. **本地文件一个入口都不摆**（`CommentsWorthAsking`）：本地音乐在 B 站上
//     压根没有对应的对象，而问一个认不出来的 oid 的症状是「静默拿到空列表」——
//     于是界面上会摆一个永远空着的评论页。
//     ⚠️ **不是摆一个点不动的键**：给一个按了没用的键比不给更糟（用户按两次
//     发现还是那样，就不再信这个应用了）。这一条和「下载」那一枚同一理由
//     （见 `viewDownloadButton`）。
//
//  2. **播放条上只放键，不放内容**（票 #573 的原话）：正文在 `/comments` 那一页，
//     而播放条那一行只有 140 像素宽的文字列——**一行正文塞不进去**，硬塞进去的
//     后果是「一行字被截成半句」。
//
// # 那一页为什么是「独立路由」而不是抽屉
//
// 研究票 #537 的三个候选：A 独立路由页（推荐）/ B 右侧抽屉 / C 满屏播放器加
// 第三栏。
//
//   - **B（抽屉）**：正文一折行就四五行，读不了——而评论正文是**会折行**的
//     （一句「太短了」还好，一段吐槽就是五六行）。
//   - **C（满屏加第三栏）**：会顶破满屏那一档的每帧分配上限（票 #407 刚收过
//     余量）。
//   - **A（独立路由页）**：正文有地方放，而且**它有地址**（`/comments`）——
//     用户可以把它当成「当前这一首」的一个附属页来来回看。
//
// ⇒ 选 A。入口两处（播放条 + 满屏），内容只有一处。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// commentsButtonKey 是播放条上「评论」那一枚的身份。
//
// ⚠️ **要钉住身份**（理由同 `playbar_keys.go` 的文件头）：这一段是几枚并排，
// 而「分集」和「评论」都是**条件摆**的——它们一出现，兄弟序号就变了，而框架给
// 元素的默认身份是「父 ID + 兄弟序号」。于是这一枚会**继承**上一枚留下的动画
// 通道（画面上是「另一枚键莫名其妙缩着」）。
const commentsButtonKey = "playbar:comments"

// 播放条上评论键那个数字的几档。
//
// # 🔴 宽度账（票 #682）
//
// 改之前这一枚是**纯图标键**：盒子 32（`iconHitMd`），图形 18（`iconMd`）。
// 加了数字之后它的宽 = 7（左内边距）+ 18（图形）+ 2（缝）+ 数字 + 7（右内边距）
// = **67**（数字吃到上限 33 时）。**没有数字时仍然是 32**（数字那一格不画，
// 缝也不占，于是那一档的画法和改之前一模一样）。
//
//	commentCountGap    2  图形和数字之间的缝（比 `space.Px4` 窄：它们是一格里的两半）
//	commentCountMaxW   33 数字那一格最宽多少（见下）
//
// 🔴 **33 是量出来的上限，不是随手取的**：左边那一列在 900 宽下**撑出轨道**
// （`playbarLeftMinW` 比 1/5 的轨道宽），而它右端离滑块只有 **42.7**（实测）。
// 评论键每宽 1 像素，那一截就长 1 像素——所以数字最多只能再吃 33：左列
// 272 → **307**，右端 323，离滑块 330.7 还有 **7.7**。
//
// ⚠️ **33 装得下「99.9万」（32.95）**，装不下「100.0万」（37.5）：`PlayCountText`
// 对一亿以下按万折、保留一位小数，所以 ≥ 100 万条的稿子会**截成省略号**。
// 那是有意的取舍——为了一百万条以上的评论把进度条挤掉不值；数字本身在那几档
// 已经只是「很多」的意思，而完整的数在评论页页头（「共 N 条」）。
//
// ⚠️ **字号是 `Size10`**：这是播放条上最小的一档，而数字是**附属信息**
// （图标才是那一枚键的身份）。再大一号（Size11）上面那个 33 就不够了。
const (
	commentCountGap  = space.Px2
	commentCountMaxW = 33
)

// playbarCommentsW 是评论键**摆满**那一档的宽：32 + 2 + 33 = **67**。
//
// ⚠️ **它和 `iconHitMd` 不是同一个数**，所以 `playbarActionsW` 不能再用
// 「四枚 × 32」算——那一条是票 #573 的账（四枚纯图标键），这一票之后评论
// 那一枚比别的宽 35（缝 2 + 数字 33）。
const playbarCommentsW = iconHitMd + commentCountGap + commentCountMaxW

// commentsKeyLabel / commentsKeyTip 是评论键的无障碍名字和悬停提示。
//
// 🔴 **数字不许只靠画面传达**（读屏软件看不见它）：拿到数的时候名字里带上它，
// 提示句里也说一遍。
//
// ⚠️ **没有数字时**名字就是「评论」两个字（一字不多）：本地文件不摆这一枚，
// 而拿不到数（离线 / 还没回来）的时候画面上也没有数字——那时候多说一句
// 「评论 0」是在撒谎。
func commentsKeyLabel(count int64) string {
	if t := list.PlayCountText(count); t != "" {
		return "评论 " + t
	}
	return "评论"
}

func commentsKeyTip(count int64) string {
	if t := list.PlayCountText(count); t != "" {
		return "看这一首的评论（" + t + "条）"
	}
	return "看这一首的评论"
}

// viewCommentsButton 是播放条上那一枚「评论」键（票 #573，票 #682 起带数字）。
//
// 🔴 **不摆的时候一个格子都不画**（理由见文件头第一条）：本地文件那一档
// 一个请求都不发。
//
// # 数字从哪儿来（票 #682）
//
// `App.CommentCount()`——稿件详情的 `stat.reply`，和收藏要的 avid 是**同一个
// 回包**（见 `bili.ArchiveStats`）。**它和评论页那一趟无关**：这一枚在用户
// 打开评论页之前就要显示数字。
//
// ⚠️ **音乐库音频（`type=audio`）这一档没有数字**：`stat.reply` 挂在稿件详情
// 上，而音频没有 bvid——它的评论数在评论接口那一侧（`type=14`），要等评论页
// 拉起来才知道。那一档照旧摆键、只是没有数字：**「不显示」不是「零条评论」**。
// 要补的话是另一票的账（每个音频多一趟网络往返，而这一枚键本来只是一条路标）。
//
// 🔴 **0 = 不显示，不是「零条评论」**：拿不到（还没回来 / 失败 / 本地文件 /
// 音频）的时候**一个数字都不画**，而那一枚键**照旧能点**（失败不该让它变成
// 死键）。
func (s *Shell) viewCommentsButton(c *ui.Context, st app.State) {
	if !st.HasTrack || !s.app.CommentsWorthAsking() {
		return
	}
	count := s.app.CommentCount()
	countText := list.PlayCountText(count)
	c.Key(commentsButtonKey)

	// ⚠️ **壳走 `flat.Button`，内容自己拼**：`iconKey` 那一档只会画一个图标，
	// 而这一枚要有「图形 + 数字」两半。壳、悬停淡底、圆角都和 `iconKey` 同一份
	// （`flat.button`），只是内边距和缝按「一格里的两半」摆。
	btn := flat.Button(c, "").Label(commentsKeyLabel(count)).
		Height(iconHitMd).Padding(space.None, space.Px7).Gap(commentCountGap)
	btn.Children(func() {
		// ⚠️ **图形那一半仍然「悬停变主色」**（和相邻几枚一样）：`iconKey` 的
		// 悬停反馈是图形自己换色（上游 `hover:text-primary`），而 `Button` 只会
		// 铺一层淡底。数字跟着图标一起换色没有意义（它是读数，不是控件）。
		ic := ui.Icon(c, icon(iconComments)).FontSize(iconMd)
		if col, ok := flat.IconAccentOnHover(c, false, btn.Hovered()); ok {
			ic = ic.TextColor(col)
		}
		ic = flat.PressScale(ic, btn.Pressed(), false)

		if countText != "" {
			// ⚠️ **`MaxWidth` + `MaxLines(1)` + 省略号**：宽度账见上面那几个
			// 常量——超过 33 的那几档（≥ 100 万条）截成「100…」，绝不把左列
			// 撑得更宽（那样 900 宽下进度条就被压了）。
			ui.Text(c, countText).FontSize(scale.Font(c, scale.Size10)).
				TextColor(c.Theme().TextMuted).
				MaxWidth(commentCountMaxW).MaxLines(1).Ellipsis("…")
		}
	})

	if s.withTip(c, btn, commentsKeyTip(count)).Clicked() {
		s.navigate(commentsPath)
	}
}

// viewFullCommentsButton 是满屏播放器里那一枚「评论」键。
//
// ⚠️ **满屏开着的时候主窗口整个被盖住了**（`viewFullPlayer` 底下那一整层一个
// 像素都看不见），所以播放条上那一枚在满屏里**点不到**——这一枚是满屏里唯一
// 的入口。
//
// ⚠️ **本地文件不摆**（同 `viewCommentsButton`）。
//
// 🔴 **满屏这一枚不带数字**（票 #682 的有意取舍）：播放条那一枚能带，是因为
// 左列是**三段定宽**的（`playbarLeftMinW` 钉着，数字只吃自己那 33，歌名照旧
// 76）。满屏底下那一行是**居中的 flex**（`viewFullControls` 的
// `Justify(ui.Center)`）：这一行里任何一枚变宽，**播放键就离开窗口的竖直中轴**
// ——而「三键落在中轴上」正是满屏底栏那条设计（`fullplayer_layout_test.go` 的
// `TestTheFullPlayerBottomBarPaddingAndMaxWidth` 量着它；那里用的是本地文件，
// 所以评论键不在，量到的中轴是准的）。给评论键加 33 会让播放键偏 16.5，而
// 满屏那一行没有播放条左列那样的余量可以重排。
//
// ⚠️ **同一份数据、同一个画法**：真要加的时候把那 33 从别处让出来（比如把
// 这一行改成「左右两组 + 中间一组」的 Grid），不能像播放条那样直接撑宽。
func (s *Shell) viewFullCommentsButton(c *ui.Context, st app.State, base flat.Base) {
	if !st.HasTrack || !s.app.CommentsWorthAsking() {
		return
	}
	c.Key("full:comments")
	if s.withTip(c, iconKeyOnBase(c, iconComments, "评论", iconMd, false, base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		"看这一首的评论").Clicked() {
		// ⚠️ **先把满屏收掉**：这一页是主窗口里的一页，而满屏是**压在主窗口
		// 上面**的一层——不收掉的话 navigate 之后画面上还是满屏（那几枚键在
		// 满屏底下点不着）。
		s.closeFullPlayer()
		s.navigate(commentsPath)
	}
}
