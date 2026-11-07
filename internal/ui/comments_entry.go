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

// 评论键那个数字的形状（票 #682 起带数，本票起收成**角标**）。
//
// ⚠️ **角标不占布局**：数字折成一个小字，挂在图形盒子的右上角——所以这一枚键
// 的宽是 **32**（图形 18 + 两侧各 7），**带不带数字都一样**。宽度账和结构都在
// `playbar_counts.go`（三枚键共用一份）。

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
	return countKeyTip("评论", count, "评论")
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
	// 而这一枚要的是「图形盒子 + 右上角一枚角标」——壳、悬停淡底、圆角都和
	// `iconKey` 同一份（`flat.button`）。
	//
	// 🔴 **内边距回到纯图标键那一档（每边 7）**：角标不占布局（它 `Attach` 在
	// 图形盒子的右上角），所以这一枚的宽和没有数字时**一模一样**——32。
	btn := pbKey(c, commentsKeyLabel(count))
	btn.Children(func() {
		// ⚠️ **结构是共享的那一份**（`countKeyIcon`）：一个 18×18 的图形盒子，
		// 缺口块挂在盒子的右上角。缺口 / 字号 / 挂点在 `playbar_counts.go`。
		//
		// ⚠️ **缺口色 = 按钮底下那块底**：播放条那一列的底是
		// `coverWash(t.Background)`（和这一列自己同一个来源，`playbar.go`）。
		countKeyIcon(c, iconComments, btn.Hovered(), btn.Pressed(), count,
			s.coverWash(c.Theme(), c.Theme().Background))
	})

	if s.withTip(c, btn, commentsKeyTip(count)).Clicked() {
		// ⚠️ **开抽屉，不是跳路由**（票 #706）：评论内容改成**右侧抽屉**了——
		// 独立页会把整个窗口顶掉，而评论是「当前这一首的附属信息」，它是**叠在
		// 底下那一页上面**的东西。
		s.openCommentsDrawer()
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
// 🔴 **满屏这一枚不带数字**（票 #682 的有意取舍）：满屏底下那一行是**居中的
// flex**（`viewFullControls` 的 `Justify(ui.Center)`）——这一行里任何一枚变宽，
// **播放键就离开窗口的竖直中轴**（「三键落在中轴上」正是满屏底栏那条设计，
// `fullplayer_layout_test.go` 的 `TestTheFullPlayerBottomBarPaddingAndMaxWidth`
// 量着它；那里用的是本地文件，所以评论键不在，量到的中轴是准的）。
//
// ⚠️ **本票之后播放条那一枚也不再「加宽」了**（角标不占布局，它的宽是 32、
// 和没有数字时一样）——但满屏那一枚**照旧不带**：播放条上角标挂得住，是因为
// 那一枚键的图形右边是空气；满屏底栏那一枚**左右都是别的键**，角标会盖到
// 邻居身上（那一行的缝只有几像素，而角标比它宽得多）。要补的话是另一票的账
// （比如给那一枚单独留一格、或者把角标改成压在图标底下那种）。
//
// ⚠️ **但提示句可以带上那个数**（票 #701 之后的对齐）：数字**不进布局**——
// 它只在那一句提示里，而提示是**浮在画面上的气泡**（不进那一行的 flex）——
// ⇒ 所以「不带数字」这条取舍**管不到提示句**，而「提示句里说清是整个稿件的
// 多少」是播放条那一枚已经有的（`countKeyTip`）。⇒ 两枚键的提示句现在
// **说同样的话**。
func (s *Shell) viewFullCommentsButton(c *ui.Context, st app.State, base flat.Base) {
	if !st.HasTrack || !s.app.CommentsWorthAsking() {
		return
	}
	c.Key("full:comments")
	if s.withTip(c, iconKeyOnBase(c, iconComments, "评论", iconMd, false, base).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		countKeyTip("评论", s.app.CommentCount(), "评论")).Clicked() {
		// ⚠️ **先把满屏收掉**（票 #706）：抽屉是浮在**主窗口**上面的一层，而
		// 满屏是**压在主窗口上面**的另一层——不收掉的话，抽屉会开在满屏底下
		// （那一层不透明，抽屉一格都看不见，而用户按了「评论」什么反应都没有）。
		//
		// ⚠️ **和原来跳路由那一支的差别**：跳路由那一支必须收掉满屏，是因为
		// 路由页是主窗口里的一页；而抽屉这一支**也**必须收掉——两层都是浮层，
		// 后建的在上，而满屏那一层建得更晚（它在 `viewMain` 的最后），所以
		// 满屏会盖住抽屉。
		s.closeFullPlayer()
		s.openCommentsDrawer()
	}
}
