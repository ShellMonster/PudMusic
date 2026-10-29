// 底层那一层：满屏播放器底下那些东西。
//
// # 这一层为什么存在
//
// 满屏播放器是一层铺满整窗的浮层（`fullplayer.go` 的 `viewFullPlayer`），底下那一
// 整页（侧栏 + 顶栏 + 路由页）**一个像素都看不见**——而它自己还在每一帧重新布局、
// 重新绘制。频谱、封面、歌词在动，机器却在给一页看不见的东西干活（票 #161）。
//
// # 拆成两半
//
// 这一层把那两件事分开：
//
//   - **副作用**：收后台投回来的结果、起该起的那一拉、翻页、同步搜索框、起定时器
//     ……**每一帧都要跑**，跟画不画没关系。跳过它们的话，后台的结果一直落在信箱里
//     没人取，用户关掉满屏看到的是「打开那一刻的样子」。
//   - **绘制**：画出来给人看的那一部分。满屏开着的时候**一个字都不建**。
//
// # 契约
//
// `syncUnderlay` 这一层**不许调 `ui.*`**：`ui.*` 那些是「建控件」，而这一层要的
// 是「不建」。唯一可以的读是 `c.Services()`——唤醒句柄要靠它登记，后台的结果
// 回来了才知道该叫醒哪一扇窗。
//
// # 哪些副作用挪了、哪些没挪
//
// 判据是**它依不依赖这一帧刚建出来的控件**：
//
//   - 不依赖的（收结果、起拉、翻页、换账号就重置、起定时器）→ **挪到这一层**。
//   - 依赖的（用户在搜索框里按了回车、点了日期预设、点了排序 Tab）→ **留在画的那
//     一半里**。它们只能由用户在被画出来的东西上操作触发，而满屏开着的时候那一页
//     压根不画，用户碰不到它；把它们挪过来反而会让「按了回车」晚一帧才生效。
//
// 每一页自己的副作用在它自己的文件里（`syncHome` / `syncLater` / …），由
// `pageSyncs` 按当前路由分派。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// drawUnderlay 画底层那一层：侧栏 + 顶栏 + 路由页 + 播放条。
//
// # 排法：**侧栏全高，顶栏只盖内容区**（上游 `biu/src/layout/index.tsx:36-47`）
//
// 侧栏从窗口最上头贯到底，播放条在底下横贯整窗；顶栏在**侧栏右边那一列**里，
// 只盖内容区那一段。票 #286 / 报告 C-01 改掉了原来那三层（顶栏盖满整窗、
// 侧栏从顶栏下面才开始）。
//
// 🔴 **满屏开着的时候一个字都不建**（票 #161）：满屏那层是铺满整窗的不透明浮层，
// 底下这一层一个像素都看不见，而它自己每帧还要重新布局、重新绘制。
//
// ⚠️ **路由那一套机器不跑**（`s.router.View`）：满屏开着的时候路由动不了（满屏
// 那一层是模态的，底下的按钮点不着），而关掉满屏之后 `Router.View` 照旧从它
// 记着的那一页接着走——`Router` 的 `view.shown` 记着「上一次画的是哪一页」，
// 中间跳过多少帧都认得出来，于是**不会**把这一页当成「刚换过来的」再走一遍
// 换页（我们本来也不做换页动画，见 `router.go` 的 `newRouter`；这一层记的是
// 「画的是哪一页」，不只服务于动画）。
func (s *Shell) drawUnderlay(c *ui.Context, st app.State) {
	if s.underlayHidden() {
		return
	}

	root := ui.Column(c).Fill()
	// 空格 = 播放/暂停（见 shortcut_dispatch.go 的 spaceTogglesPlay）。
	//
	// ⚠️ **登记在这一层根上，而不是只登记在窗口上**：点过一个按钮之后焦点就留在
	// 那枚按钮上，而框架让「有焦点的按钮」先于窗口级快捷键拿空格（`input.go`
	// 的 `claimedBy(k, false)`）——于是空格会再点一次那枚按钮（比如侧栏的首页）。
	// 根在焦点链上，登记在它身上框架才知道「这个键有人要」。
	if s.recording == (shortcutSlot{}) && root.Shortcut(0, ui.KeySpace) {
		s.spaceTogglesPlay(c)
	}
	root.Children(func() {
		// ⚠️ **侧栏和「顶栏 + 内容区」那一列是兄弟**（上游
		// `biu/src/layout/index.tsx:36-47`）：侧栏从窗口最上头一直贯到底，
		// 顶栏**只画在内容列上**（侧栏右边那一段）。
		//
		// 我们原来是「顶栏盖满整窗 / 侧栏从顶栏下面才开始」那三层（票 #286 /
		// 报告 C-01 改成了现在这样）：左上角那一块（上游放 Logo 的地方）放着
		// 后退 / 前进 / 搜索框，而侧栏从 y=64 才开始——于是每一页的第一眼是
		// 「一条横贯整窗的顶栏」，不是 macOS 应用那种「左边一条全高栏 + 内容区
		// 右上角一条导航」。
		//
		// ⚠️ **「这一帧侧栏画出来的那一档」要在这里归零**（票 #630 的对抗性
		// 审查）：它由 `viewSide` 记下，而侧栏排在下面那个 Row 的**第一个**
		// 孩子——所以归零必须排在**建侧栏之前**，否则那一格还是上一帧的值。
		//
		// 归零是**必须的**：MyGo 一帧要跑 3 遍 view，而 `viewSide` 在其中
		// 某一遍里可能被替掉（`sideBody`）或压根没被调到（满屏播放器开着
		// 时底层那一层压根不画）——不归零的话，内容区会拿**上一帧**的侧栏
		// 宽去算，而那一帧的侧栏和这一帧的不是同一件事。
		s.sideDrawnW = 0
		ui.Row(c).Grow(1).Children(func() {
			// 侧栏是**一整块** view，外面包一层 Guard（铁律三）。它坏了的
			// 表现是整个窗口空一大块，连报错的地方都没有。
			s.guardAt("侧栏", s.sideBody)(c)
			// 内容区那一列：顶栏 + 路由页。
			//
			// ⚠️ **它要拉满高**（`AlignSelf(Stretch)`）：Row 是把子元素**垂直
			// 居中**摆的（`mygo/docs/ui/layout.md`：「Row places them from left
			// to right and centers them vertically」），不拉满高的话这一列按内容
			// 高度浮在中间，而里面的路由页（框架的 `Router` 自己 `Grow(1)`）就
			// 没有高度可分——底下空一大块。
			//
			// ⚠️ **`MinWidth(0)` 不能省**：这一列的**内容**（顶栏那一排控件）有
			// 一个自己的最小宽度（搜索框 280 + 两个箭头 + 右边五枚键，五百多），
			// 而 flex 默认的最小宽度是「内容的最小宽度」——不钉成 0 的话，窗口
			// 900 宽加上侧栏拖到 480（内容区只剩 420）时，这一列会被顶到五百多
			// 宽，**把内容区撑出窗口**：收藏夹页那一行操作条于是排到了窗口右边
			// 外面（`TestTheCollectionOpsRowStillFitsAtTheMinWindow` 量的就是
			// 那个）。钉成 0 之后，这一列拿到的是「窗口宽 − 侧栏宽」那个数，
			// 顶栏在它里面自己收缩（搜索框先让位）。
			ui.Column(c).Grow(1).AlignSelf(ui.Stretch).MinWidth(0).Children(func() {
				// 顶栏同样外面包一层 Guard（铁律三）：它坏了坏掉的是整条顶栏，
				// 用户连窗口都拖不动了，而出错的地方不在页面上。
				s.guardAt("顶栏", s.navBody)(c)
				s.viewRoutes(c)
			})
		})
		// ⚠️ **播放条在满屏开着的时候也不建**（理由见 fullplayer.go 顶上那一段：
		// 共享元素转场要的是「上一帧没被摆过的那个」，播放条那个封面下一帧
		// 还会被摆一次，满屏的封面就找不到「上一个」了——封面会「啪」地一下
		// 就位而不是长过去。它反正被一层不透明的背景盖着，少建它看不出异样）。
		// 满屏开着的时候这一整个函数已经提前返回了，所以这里只管画。
		s.viewPlayBar(c, st)
	})
}

// underlayHidden 报底层那一层此刻是不是被满屏盖住了。
//
// 判据是**满屏开着没有**，不是「窗口看得见没有」：窗口看不见的时候整个窗口
// 都不画（见 presence.go），那是另一件事。
func (s *Shell) underlayHidden() bool { return s.full.open }

// syncUnderlay 跑底层那一层的**每帧副作用**。
//
// ⚠️ **每一帧都调**，不管底层画不画（票 #161）。满屏开着的时候它照跑：后台的结果
// 照落、该起的拉照起、该翻的页照翻——只是**不画**。
func (s *Shell) syncUnderlay(c *ui.Context) {
	// 顶栏那一摊：联想词那个防抖（`syncSuggestPending`）、红点那个轮询
	//（`syncFeedDot`）、头像那个「登录态变了吗」（`avatarSync`）。它们原来在
	// 顶栏的 view 里，而顶栏在满屏开着的时候也不画——挪到这里，它们才不至于
	// 跟着一起停。
	s.syncSuggestPending(s.clock.Now())
	s.syncFeedDot()
	s.avatarSync()

	// 热词轮换的表：满屏开着的时候顶栏不画，占位词没人看得见，把表收掉——
	// 不然它每 5 秒叫醒一次窗口画一帧满屏（`syncHotwords` 自己跟着顶栏的 view
	// 跑，满屏时它没机会跑自己的收条件；见 navbar_hotword.go）。
	if s.full.open {
		s.stopHotword()
	}

	// 路由那一摊：404 那个倒计时（`syncRoute`）。它原来在 `viewRoutes` 的
	// 最前面，而满屏开着的时候 `viewRoutes` 整个不跑——不挪的话，用户在满屏
	// 里待着的那几秒里，那个倒计时停在半路。
	s.syncRoute()

	// 「**刚刚**登录成功那一下」（票 #277）：说一句提示、回该去的页。
	//
	// ⚠️ **它在这一层，不在 `pageSyncs` 里**：登录成功的时候用户**可能已经
	// 不在登录页上了**（他扫完码就自己走了），而登录页自己的 `syncLogin`
	// 只在当前路由是 `/login` 时才跑——放那儿就漏掉了那一半。
	s.syncLoginSuccess()

	// 当前这一页自己那一份。
	s.syncCurrentPage(c)
}

// syncCurrentPage 跑当前这一页自己的副作用。
//
// ⚠️ **按当前路由分派**，不是按「刚才画过哪一页」——满屏开着的时候路由不会变
// （满屏那层是模态的，底下的路由动不了），而关掉满屏之后的第一帧要画的就是
// 当前这一页，它的状态必须是**此刻**的。
func (s *Shell) syncCurrentPage(c *ui.Context) {
	fn, ok := pageSyncs[lookup(s.router.Path()).Match]
	if !ok {
		return
	}
	fn(s, c)
}

// pageSyncs 是「每一页每帧的副作用」，按 `Route.Match` 索引。
//
// ⚠️ **这张表和 `builtPage` 是同一件事的两面**：表里加一行就要在 `builtPage` 里
// 也加一页（反了也一样）。漏了的话那一页的副作用就停更——症状是「满屏开着
// 的时候那一页不更新了」，而界面上一个字都不提。
// `TestEveryPageWithContentHasASync`（underlay_test.go）钉住这件事。
//
// ⚠️ **有一页没有副作用的也在表里**（登录页、迷你播放器那一页）：留一个空函数
// 好过让表和 `builtPage` 对不上——对不上的话，加新页的人根本不知道该往哪儿加。
var pageSyncs = map[string]func(*Shell, *ui.Context){
	homePath:         (*Shell).syncHome,
	settingsPath:     (*Shell).syncSettings,
	searchPath:       (*Shell).syncSearch,
	miniPlayerPath:   (*Shell).syncNowPlaying,
	downloadListPath: (*Shell).syncDownloads,
	loginPath:        (*Shell).syncLogin,
	collectionMatch:  (*Shell).syncCollection,
	upPath:           (*Shell).syncUpSpace,
	dynamicFeedPath:  (*Shell).syncDynamicFeed,
	laterPath:        (*Shell).syncLater,
	historyPath:      (*Shell).syncHistory,
	localMusicPath:   (*Shell).syncLocalMusic,
	followPath:       (*Shell).syncFollow,
	commentsPath:     (*Shell).syncComments,
}
