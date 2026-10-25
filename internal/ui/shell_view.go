// 这一块是**视图管线**：一帧是怎么从「MyGo 要画面」走到「画出来」的。
//
//   - `View` —— 每帧的入口（清状态、装可见性、崩了没崩、画提示、兑现焦点请求、
//     念播报）。
//   - `viewMain` —— 正常那一帧的画法（顶栏 + 侧栏 + 路由页 + 播放条 + 浮层）。
//   - `guardAt` / `onPanic` / `onPanicAt` / `newCrashInfo` —— 崩了以后怎么记现场。
//   - `pageJustShown` / `viewFolderDialogs` / `syncFolderDialogs` —— 画法里那几处
//     「要先问一次背景状态」的地方。
//
// ⚠️ `Guard`（页面级 panic recover 的自由函数）和错误页本身在 `shell_crash.go`
// ——那一块自成一体，见那个文件的头。
package ui

import (
	"log"
	"runtime/debug"

	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/egoist/mygo/ui"
)

// View 画一帧，MyGo 在需要新画面的时候调它。
func (s *Shell) View(c *ui.Context) {
	// 🔴 **冷启动那一段的「首帧」时刻**（票 #478）：用户「双击 → 窗口出来」
	// 等的就是这一帧，而它之前的那一大段（读盘、读钥匙串、建窗口）合起来
	// 是「应用慢」还是「等过人」，只有量出来才答得出。
	//
	// ⚠️ **它记在画之前**：这一帧画完还要一会儿（布局、绘制），而用户看到
	// 画面是在**画完之后**——差那点时间不影响「慢不慢」的判断，而记在
	// 画之前能保证**一帧都没画出来时也有一个数**（画崩了的时候那才是
	// 用户真正遇到的事）。
	s.app.NoteFirstFrame()

	// 提示的候选，每一遍 build 重新攒（理由见 tooltip.go 的 tipCandidate）。
	//
	// ⚠️ **清空是保险**：正常路径上 `drawTip` 画完就清了（那儿清才是准的——
	// 见 drawTip 的注释），这里再清一次是为了「这一遍 build 崩在中途」那种
	// 情况：崩了的那一遍不会走到 drawTip，候选就留在了壳上。
	s.tipCandidates = s.tipCandidates[:0]
	s.tipPanel = ui.Element{}

	// 「内容区里当前那一个列表」每帧重新登记：这一遍开头先清空，登记的那几
	// 个会在这一遍 build 里重新填（见 focus.go 的 `registerContentFocus`）。
	//
	// ⚠️ **清空是必须的**：不清的话，切到「这一页没有列表」之后，焦点会被
	// 交给一个**不在画面上**的列表——用户看不见任何指示，方向键也收不到。
	s.contentFocus = nil

	// 每帧重新登记：句柄内部是窗口的弱引用，窗口关了自动失效，关掉重开也不用管。
	s.wake.set(c)

	// 这一帧的窗口看不看得见，交给「每帧都要一帧」的动画自己去判断（见
	// hover.SetVisibleFlag 的注释）。**必须在 draw 之前**：波形是在 draw 里
	// 建出来的，而它建的那一刻就要认下这一扇窗的那一个开关。
	//
	// ⚠️ 这一句挡的是「自发动画」，不挡「有人在要帧」：看不见的时候之所以
	// 没有帧，是因为 `wakeHandle.invalidate` 不进来了；万一有别的路径送进来
	// 一帧（比如本地库扫完那一支直接调 `Services.Invalidate`），这一帧照样
	// 画得出来，只是它**不会再要下一帧**。两处合起来才停得下来。
	//
	// ⚠️ **顺带把答案刷成此刻的**：那个开关是**活的**（波形在画的时候读它），
	// 而可见性有可能是在两次 build 之间变的（`noteWindow` 那一条路也会改它，
	// 但这里再刷一次是保险——`presence` 是唯一的事实来源）。
	visible := s.visibleFlag()
	visible.Store(s.presence.seen())
	hover.SetVisibleFlag(visible)

	// 从看不见恢复过来的那一帧：让那些「有中间状态」的动画一次到位（见
	// snapScroll 的注释：同一时刻的两遍 view 都算这一帧）。
	if now := c.Now(); s.snapScroll && !now.Equal(s.snapAt) {
		s.snapScroll = false
	}
	if s.presence.takeResumed() {
		s.snapScroll, s.snapAt = true, c.Now()
	}

	if s.crash != nil {
		s.viewCrash(c)
		return
	}
	s.draw(c)

	// 提示：**这一遍 build 走完之后**才画（票 #420）。
	//
	// ⚠️ **排在 draw 之后**：提示是逐个元素登记的，而「指针刚离开的那一枚先被
	// build」——挑出该画哪一枚要看得见这一遍登记的**全部**候选（见 pickTip）。
	//
	// ⚠️ **崩了这一帧就不画**：崩的时候树是半截的，画上去的面板会挂在一个
	// 不知道在哪儿的锚点上。
	if s.crash == nil {
		s.drawTip(c)
	}

	// 兑现「把焦点交给内容区」这个请求（票 #245 的 E4）。
	//
	// ⚠️ **排在 draw 之后**：内容区在 draw 里画，各张列表的登记也在那儿——
	// 兑现的时候得已经有「当前那一个列表」可交。
	//
	// ⚠️ **每一遍 build 都会走到这里**：兑现一次就把请求清掉（见
	// `applyContentFocus`），所以只有第一遍真的兑现。
	s.applyContentFocus()

	// 把攒着的那些状态变化念给读屏软件听（无障碍，见 announce.go）。
	//
	// ⚠️ **排在最后**：这一句要念的是「刚刚发生了什么」，而刚刚发生的那些事
	// 有一部分是**这一遍建视图的时候才知道的**（按了下载、按了收藏——那些话
	// 是提示，提示也是要念的）。排在前面的话它们要等到下一帧才念出去，而下一帧
	// 未必来得快（界面是按需绘制的）。
	//
	// ⚠️ **一帧建好几遍视图，这里只念一遍**：`take` 是有破坏性的（见 announce.go）。
	s.flushAnnouncements(c)
}

// guardAt 给一层 view 包 Guard，并记住**它是界面的哪一块**。
//
// 界面上的每一道 Guard 都走它（`Guard` 那个自由函数留给不需要记位置的地方，
// 比如控件层自己包的那一道）。为什么每一处都要写一句「这一块是什么」：那是错误
// 页上「出错在哪一步」那一句的全部来源，而这正是这一票要的（issue 126：「出错时
// 能看到出错在哪一步」）。
//
// ⚠️ **写「是什么」而不是「在哪儿崩的」**：调用点在崩之前执行，它知道自己在
// 画哪一块 whereas 崩在它内部的第几行它不知道——那要等栈，而栈在错误页上对
// 用户没有意义（几百行 Go 调用，用户看不懂也用不上；它已经在日志里了）。
func (s *Shell) guardAt(where string, draw func(*ui.Context)) func(*ui.Context) {
	return guard.Wrap(draw, func(r any) { s.onPanicAt(where, r) })
}

// onPanic 记下崩掉的东西并要一帧，下一帧画错误页。
//
// ⚠️ 这里补的 Invalidate 不是多余的：recover 是在 build pass **中间**接住的，
// 那一帧剩下的部分没建，画出来是半张。要下一帧才完整（实测，文档没写）。
func (s *Shell) onPanic(r any) { s.onPanicAt("界面", r) }

// onPanicAt 是 `onPanic` 加上「崩在哪一块」。
func (s *Shell) onPanicAt(where string, r any) {
	s.crash = s.newCrashInfo(where, r)
	log.Printf("biu: 界面崩溃（%s / %s），已切到错误页\n%v\n%s",
		s.crash.where, s.crash.title, r, debug.Stack())
	s.wake.invalidate()
}

// newCrashInfo 收一次崩溃的现场。
//
// ⚠️ **在哪一页从 `router` 上读，而不是让调用点报上来**：崩的那一帧 `router`
// 上就是当时在看的页，而每一处调用点各自报一遍的话，早晚会有一处忘了报——而
// 「忘了报」在界面上看起来和「崩在首页」一模一样。
func (s *Shell) newCrashInfo(where string, r any) *crashInfo {
	path := s.router.Location()
	rt := lookup(path)
	return &crashInfo{
		where: where,
		path:  path,
		title: rt.Title,
		got:   r,
	}
}

// viewMain 是正常的那一帧：上导航 + 左边栏 + 路由页 + 播放条，浮层上是播放列表
// 抽屉（开着的话）和右下角的提示。
func (s *Shell) viewMain(c *ui.Context) {
	// 滚动条的悬停状态交接：上一帧量到的这一帧用，这一帧重新量（各容器在
	// `viewMain` 后半段把它们建出来）。必须排在 `applyFrameTheme` 之前。
	s.scrollbarHot, s.scrollbarHotNow = s.scrollbarHotNow, false
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头——SetTheme 只影响
	// 它之后创建的控件。不装的话 MyGo 在帧开头重置好的那份跟随系统的主题
	// 就是这一帧真正用的那份，用户选了深色也只在这一帧是对的。
	s.applyFrameTheme(c)
	// 封面缓存要知道「新的一帧开始了」——挤缓存时**画面上正画着**的那几张
	// 不许挤（票 #358：开队列抽屉时中间那一列的封面来回闪）。
	//
	// ⚠️ **给的是 `c.Now()` 而不是帧号**：一帧要跑好几遍 view，而同一帧的
	// 几遍给的是同一个时刻，缓存那边靠它认出「这是不是新的一帧」。
	s.images.BeginFrame(c.Now())
	// 应用内快捷键：登记这一帧窗口要处理哪些组合键，同时也就知道了这一帧有没有
	// 键打中（`Context.Shortcut` 命中就是「按了」）。见 shortcut_dispatch.go。
	//
	// 排在主题之后、其它每帧钩子之前：它自己不画任何东西，而放在最前面的话
	// 「按了快捷键 → 顺带这一帧才生效」这件事和主题那条铁律不会互相干扰。
	s.viewShortcuts(c)
	s.reportSettingsError()
	// 收「那个 500ms 防抖的钟要不要醒着」。**每帧一次**，所以用户切走时它一定
	// 会跟着停（见 syncSearchWatch）。
	s.syncSearchWatch()
	// 更新器留给界面的那一句提示（后台查到了新版本之类），取走就没了。
	s.reportUpdateNotice()
	// 「刷新数据」那一趟回来的话（成了几个 / 为什么没成 / 已经在刷了）。
	// 同样每帧取一次，取走就没了——理由见 reportFavoritesRefresh。
	s.reportFavoritesRefresh()
	// 启动时自动导入的结果，第一帧弹一条 toast 说一声。
	//
	// 为什么非弹不可：任务要求「凭据可以不迁，**要提示重新登录**」。那句提示只在
	// 设置页「配置」那一段里，而用户不会主动去那儿——他在启动时什么也没点，
	// 然后发现要重新登录，而界面上从头到尾没提过发生过一次导入。
	//
	// 排在 `syncSearchWatch` 之后：这一句会 `addToast`，而上面那一句收的是
	// 「搜索页那个钟」的状态。两句互不相干，先后无所谓，但**都必须在
	// `s.app.Snapshot()` 之前**——那一行拿的是这一帧要画的播放状态。
	s.announceMigration()

	// ⚠️ **这一帧只取一次快照**：底层那一层（`drawUnderlay`）和满屏播放器
	//（`viewFullPlayer`）看的是同一份播放状态——两次取的话两者之间播放状态可能
	// 变了，而那一帧画出来的是两份不同的状态。
	st := s.app.Snapshot()

	// 🔴 **满屏开着的时候底层那一层不画，但每帧的副作用照跑**（票 #161）。
	// 两件事拆开在 `underlay.go` 那一层：这一句跑副作用，下面那句画。
	//
	// ⚠️ **顺序不能反**：副作用在前、画在后——画的那一帧要看到副作用刚落下
	// 的状态（后台的结果、刚换好的查询）。反过来的话每一帧画的都是上一帧的
	// 状态，而用户在满屏里待久了之后关掉它，看到的是「慢了一帧」的那一份。
	s.syncUnderlay(c)
	s.drawUnderlay(c, st)

	// 抽屉是浮在侧栏和页面**上面**的一层，不是主布局里的另一块。Biu 那边它是
	// 右侧浮出来的 Drawer（`music-playlist-drawer/index.tsx` 的
	// `data-[placement=right]`），底下那一层页面照旧看得见、点得着；塞进上面
	// 那个 Row 里就成了「窗口窄一块」，页面被挤着走——那不是抽屉。
	// ⚠️ **全屏开着的时候这一处不建**：满屏底栏上那一枚「播放列表」要开的是
	// 同一个抽屉，而抽屉必须画在满屏**上面**（后建的浮层在上）。所以全屏那一支
	// 在 `viewFullPlayer` 末尾自己建（`fullplayer.go`），这里让开——两处都建的话
	// 同一个视图会在同一帧里建两遍，行 key 撞上（Tester 里当场 panic）。
	if !s.full.open {
		s.guardAt("播放列表抽屉", s.viewQueueDrawer)(c)
	}

	// 收藏夹管理那几个弹窗（新建 / 修改 / 选封面 / 裁剪 / 选收藏夹）。
	//
	// ⚠️ 排在**最后**：它们是模态的，要盖住整页；而外面这一层 Guard 不可省
	//（铁律四）——view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程。
	s.guardAt("收藏夹弹窗", s.viewFolderDialogs)(c)

	// 全屏播放器排在收藏夹弹窗**之后**、Toast **之前**：它盖住整个窗口，
	// 而提示要浮在最上面（右下角那句话在满屏里也得看得见）。
	//
	// ⚠️ **递的是这一帧开头取的那份快照**（`st`），不是满屏自己再取一份——
	// 见 `viewFullPlayer` 的注释。
	s.guardAt("全屏播放器", func(c *ui.Context) { s.viewFullPlayer(c, st) })(c)

	// 歌词搜索与预览那两个弹窗（lyricssearch.go）。**排在满屏之后**：它们是模态
	// 的、要盖住整页，而满屏盖住的是整个窗口——次序反过来的话用户在满屏里按到
	// 那个旋钮会看见弹窗开在满屏底下。Toast 仍然在最上面（采用歌词之后那句
	// 「已经换上这一版」要从预览弹窗上浮过去）。
	s.guardAt("歌词弹窗", s.viewLyricDialogs)(c)

	s.viewToasts(c)

	// 记下这一帧停在哪条路由上（下一帧的「刚进这一页」就是拿它比出来的）。
	//
	// ⚠️ **必须排在最后**：页面是在 `s.viewRoutes(c)` 里画的，而那一段要用
	// 「上一帧在哪条路由」来判断「这一帧是不是刚进这一页的第一帧」——所以
	// 这个数要等到整帧画完才更新。
	s.lastPath = s.router.Path()

	// ⚠️ **离开设置页就把「指着的那一行」收掉**：那根手指是给「刚从下载记录页
	// 跳过来」的人看的（见 goToDownloadDirSetting），留着的话，用户下次自己
	// 从侧栏进设置页，「下载目录」那一行的标题还是主色——他没跳过来过。
	//
	// 排在最后是安全的：这一帧的页面已经画完了（那一格已经被指着画过一次），
	// 而设置页自己那一格是**切走**才清的（见 syncSettings）。
	if s.lastPath != settingsPath {
		if s.settings.focus != settingsFocusNone {
			s.settings.focus = settingsFocusNone
		}
		// ⚠️ **那 16 像素的留白也别留着**：它是给「刚跳过来」这件事的——用户
		// 跳过来之后立刻走掉的话，下一次进设置页时它补在别的地方，页面就无故
		// 差了 16 像素（见 syncSettings）。
		s.settings.focusMargin = false
	}
}

// pageJustShown 报「这一帧是刚进这一页的第一帧」。
//
// 路由切走再切回来时，框架会把焦点还给「离开这一页时有焦点的那个元素」
// （`Router.focusPage`，`mygo@v0.6.3/ui/router.go:726`）。收藏夹/local 那一页
// 的「会展开的搜索」正好是那个元素——于是用户回来的时候，键盘焦点被塞进一个
// 他没打开过的框里，他在别处敲的字就落进那里（评审第 5 条）。
//
// 所以刚进这一页的那一帧，**那个输入框先不建**：没有它，框架就没有焦点可还
// （下一帧它照常展开，词还摆在那儿）。
func (s *Shell) pageJustShown() bool {
	return s.lastPath != s.router.Path()
}

// viewFolderDialogs 画收藏夹管理那几个弹窗，**并收掉后台回来的结果**。
//
// 每帧开头**先**收结果再画：`TakeFolderResult` 是「取走而不是读」，取过就不再
// 有，而收结果这一趟会改 `submitting`——先画后收的话，界面上会有一帧是
// 「提交完了但按钮还在转」，而用户看到的就是「点了没反应」。
func (s *Shell) viewFolderDialogs(c *ui.Context) {
	s.flushFolderResults()
	s.flushCoverPick()
	s.syncFolderDialogs()

	s.viewFolderEdit(c)
	// 裁剪弹窗画在编辑弹窗**外面**（不是里面）：`ui.Modal` 自己带一层遮罩，
	// 嵌在另一个 Modal 的面板里的话遮罩只有面板那么大，裁剪台周围那一圈点不
	// 到——而那一圈正是「点外面想关掉」的地方。
	if e := s.folderEdit; e != nil && e.crop.open {
		s.viewCoverCrop(c, &e.crop)
	}
	s.viewFolderSelect(c)
	// 侧栏收藏夹的「删除」二次确认（sidebar_favorites.go）。同样排在这里：
	// 它是模态的，要盖住整页，而侧栏是折叠的时候就不画东西的那一块。
	s.viewFavoriteDelete(c)
}

// syncFolderDialogs 是那些「每一帧都要问一次后台」的地方。
//
// ⚠️ **在 flushFolderResults 之后调**：列表换掉之后 `flushFolderResults` 才拿得到
// 刚回来的那一批勾选状态，反过来的话这一帧的勾是按旧列表算的。
func (s *Shell) syncFolderDialogs() {
	if f := s.folderSelect; f != nil {
		s.syncFolderList(f)
	}
}
