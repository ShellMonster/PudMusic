// 评论的**入口**：播放条上那一枚键 + 满屏播放器里那一枚。
//
// # 两个入口，两条规矩
//
//  1. **本地文件一个入口都不摆**（`CommentsWorthAsking`）：本地音乐在 B 站上
//     压根没有对应的对象，而问一个认不出来的 oid 的症状是「静默拿到空列表」——
//     于是界面上会摆一个永远空着的评论面板。
//     ⚠️ **不是摆一个点不动的键**：给一个按了没用的键比不给更糟（用户按两次
//     发现还是那样，就不再信这个应用了）。这一条和「下载」那一枚同一理由
//     （见 `viewDownloadButton`）。
//
//  2. **播放条上只放键，不放内容**（票 #573 的原话）：正文在**抽屉**里，
//     而播放条那一行只有 140 像素宽的文字列——**一行正文塞不进去**，硬塞进去的
//     后果是「一行字被截成半句」。
//
// # 内容在**右侧抽屉**里，不在独立路由页上（票 #706）
//
// 原来它是 `/comments` 那一条独立路由（研究票 #537 的三个候选里选的 A）。
// 现在它是**右侧抽屉**——理由很直接：**独立页把整个窗口顶掉**，用户看完评论
// 要再点一次「返回」才回得去播放条；而评论是「当前这一首的附属信息」，它是
// **叠在底下那一页上面**的东西。
//
// ⚠️ **抽屉的骨架照队列抽屉**（`comments_drawer.go`）：同样浮在主布局外面、
// 同样从右边滑进来、同样动**布局位置**而不是 transform。
//
// ⚠️ **而且它比队列抽屉宽**（400 对 320）：这一份面板里装的是**会折行的
// 正文**——一段吐槽五六行，而队列那一列是「封面 + 歌名 + 来源」的定宽三段。
//
// # 两枚入口的差别：**满屏那一枚不带数字**
//
// 播放条那一枚**继续显示评论数**（票 #682 的两级逻辑照旧）；满屏那一枚**不带**
// ——满屏底栏是**居中的 flex**，任何一枚变宽播放键就离开竖直中轴了（理由见
// `viewFullCommentsButton` 那一段）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
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
// # 🔴 宽度账（票 #682 / #701）
//
// 改之前这一枚是**纯图标键**：盒子 32（`iconHitMd`），图形 18（`iconMd`）。
// 票 #682 给它加过数字（那一版是 32 + 2 + **33** = **67**，数字格的上限是
// **33**）。
//
// ⚠️ **票 #701 把那个折法换掉了**（`list.CountText` 那一族：「1k+ / 10k+ /
// 99k+ / 1亿+」），而那一族的宽由**数字格的上限**（**24.5**）说了算——比旧的
// 33 还窄 8.5。而**这一枚键的形状也变了**：带数字时图形**紧贴**左沿（左右
// 内边距 0），所以它的宽是 18 + 2 + 数字，**不是** 7 + 18 + 2 + 数字 + 7。
//
//	commentCountGap  2  图形和数字之间的缝（比 `space.Px4` 窄：它们是一格里的两半）
//
// ⚠️ **数字格的上限现在是 `playbarCountMaxW`（24.5）**，不是旧的 33——
// **三个键共用一个上限**（理由见 `playbar_counts.go` 那个常量的注释：它挡的是
// 「数字本身被拉出那一格」——而**左列的宽本来就是常数**，所以碰撞不可能发生）。
// 旧的 33 是票 #682 的账，那时的折法最宽的一档是「99.9万」（32.95），而新折法
// 最宽的是「99k+」（**24.49**）。
//
// ⚠️ **字号是 `Size10`**：这是播放条上最小的一档，而数字是**附属信息**
// （图标才是那一枚键的身份）。再大一号（Size11）上面那个上限就不够了。
const (
	commentCountGap = playbarCountGap
)

// playbarCommentsW 是评论键**摆满**那一档的宽：18 + 2 + **24.5** = **44.5**。
//
// ⚠️ **它和 `iconHitMd` 不是同一个数**，所以 `playbarActionsW` 不能再用
// 「四枚 × 32」算——那一条是票 #573 的账（四枚纯图标键），票 #682 之后评论
// 那一枚比别的宽 35，票 #701 之后是三枚带数字的键（见 `playbar.go` 的
// `playbarActionsW`）。
const playbarCommentsW = iconMd + commentCountGap + playbarCountMaxW

// commentsKeyLabel / commentsKeyTip 是评论键的无障碍名字和悬停提示。
//
// 🔴 **数字不许只靠画面传达**（读屏软件看不见它）：拿到数的时候名字里带上它，
// 提示句里也说一遍。
//
// ⚠️ **没有数字时**名字就是「评论」两个字（一字不多）：本地文件不摆这一枚，
// 而拿不到数（离线 / 还没回来）的时候画面上也没有数字——那时候多说一句
// 「评论 0」是在撒谎。
//
// ⚠️ **折法是 `list.CountText`**（不是 `list.PlayCountText`）：这一族（票 #701）
// 走「1k+ / 10k+ / 99k+ / 1亿+」那一套，因为它的**宽度上界必须钉死**（理由见
// `widgets/list/count.go` 的文件头）。
func commentsKeyLabel(count int64) string {
	return countKeyLabel("评论", count)
}

func commentsKeyTip(count int64) string {
	return countKeyTip("评论", count, "看这一首的评论")
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
	c.Key(commentsButtonKey)

	// ⚠️ **壳走 `flat.Button`，内容自己拼**：`iconKey` 那一档只会画一个图标，
	// 而这一枚要有「图形 + 数字」两半。壳、悬停淡底、圆角都和 `iconKey` 同一份
	// （`flat.button`），只是内边距和缝按「一格里的两半」摆。
	//
	// 🔴 **左右内边距跟着数走**（`countKeyPadX`）：带数字时是 0（不然会宽出
	// 14），没有数字时是 7（那时的形状和改之前一模一样）。
	btn := flat.Button(c, "").Label(commentsKeyLabel(count)).
		Height(iconHitMd).Padding(space.None, countKeyPadX(count)).Gap(commentCountGap)
	btn.Children(func() {
		// ⚠️ **结构是共享的那一份**（`countKeyRow`）：「图形 │ 缝 2 │ 数字」，
		// 而图形紧贴那一枚键的左沿（宽度账见 `playbar_counts.go` 的文件头）。
		//
		// ⚠️ **数字那一格的上限是 `playbarCountMaxW`（24.5）**：宽度账见上面
		// 那几个常量——它是一道**保险**（折法改了也不撞滑块），而当前那个折法
		// （`list.CountText`）最宽的一档是「99k+」（**24.49**），**刚好装满**
		// 24.5。
		//
		// ⚠️ **当前折法一档都不截**：上限 24.5 是**保险**，不是「有一档会被
		// 切掉」——「100…」那一档在当前折法里**压根不存在**（最宽的「99k+」
		// 24.49 ≤ 24.5）。
		countKeyRow(c, iconComments, count, btn.Hovered(), btn.Pressed(),
			func(c *ui.Context) { countIcon(c, iconComments, btn.Hovered(), btn.Pressed()) })
	})

	if s.withTip(c, btn, commentsKeyTip(count)).Clicked() {
		// ⚠️ **开抽屉，不是跳路由**（票 #706）：评论内容改成**右侧抽屉**了——
		// 独立页会把整个窗口顶掉，而评论是「当前这一首的附属信息」，它是**叠在
		// 底下那一页上面**的东西。
		s.commentsOpen = true
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
// 左列是**三段定宽**的（`playbarLeftMinW` 钉着，数字只吃自己那 24.5，歌名照旧
// 76）。满屏底下那一行是**居中的 flex**（`viewFullControls` 的
// `Justify(ui.Center)`）：这一行里任何一枚变宽，**播放键就离开窗口的竖直中轴**
// ——而「三键落在中轴上」正是满屏底栏那条设计（`fullplayer_layout_test.go` 的
// `TestTheFullPlayerBottomBarPaddingAndMaxWidth` 量着它；那里用的是本地文件，
// 所以评论键不在，量到的中轴是准的）。给评论键加 24.5 会让播放键偏 12.25，而
// 满屏那一行没有播放条左列那样的余量可以重排。
//
// ⚠️ **但提示句可以带上那个数**（票 #701 之后的对齐）：数字**不进布局**——
// 它只在那一句提示里，而提示是**浮在画面上的气泡**（不进那一行的 flex）——
// ⇒ 所以「不带数字」这条取舍**管不到提示句**，而「提示句里说清是整个稿件的
// 多少」是播放条那一枚已经有的（`countKeyTip`）。⇒ 两枚键的提示句现在
// **说同样的话**。
//
// ⚠️ **同一份数据、同一个画法**：真要加数字的时候把那 24.5 从别处让出来
// （比如把这一行改成「左右两组 + 中间一组」的 Grid），不能像播放条那样直接
// 撑宽。
func (s *Shell) viewFullCommentsButton(c *ui.Context, st app.State, base flat.Base) {
	if !st.HasTrack || !s.app.CommentsWorthAsking() {
		return
	}
	c.Key("full:comments")
	if s.withTip(c, iconKeyOnBase(c, iconComments, "评论", iconMd, false, base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		countKeyTip("评论", s.app.CommentCount(), "看这一首的评论")).Clicked() {
		// ⚠️ **先把满屏收掉**（票 #706）：抽屉是浮在**主窗口**上面的一层，而
		// 满屏是**压在主窗口上面**的另一层——不收掉的话，抽屉会开在满屏底下
		// （那一层不透明，抽屉一格都看不见，而用户按了「评论」什么反应都没有）。
		//
		// ⚠️ **和原来跳路由那一支的差别**：跳路由那一支必须收掉满屏，是因为
		// 路由页是主窗口里的一页；而抽屉这一支**也**必须收掉——两层都是浮层，
		// 后建的在上，而满屏那一层建得更晚（它在 `viewMain` 的最后），所以
		// 满屏会盖住抽屉。
		s.closeFullPlayer()
		s.commentsOpen = true
	}
}
