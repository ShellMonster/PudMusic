// 404 页：路径谁都不匹配时的那一页（Biu 的 P-30）。
//
// Biu 的 `src/pages/not-found/index.tsx` 就是一个 `<Empty />` 加一个从 3 数到
// 0 的倒计时（`useState(3)` + 每秒 `useInterval(…, 1000)` 减 1），到 0 跳 `/`。
// 我们这一页比上游多两样：**说清楚是哪条路径不存在**（上游只知道「没匹配上」），
// 和**一枚立刻能按的「回到主页」**（上游只有那个倒计时，见 viewNotFound）。
//
// 倒计时那一行的文案用上游删掉之前那一版（a6c48e1）的 `{count} 秒后回到主页...`：
// 三个是 ASCII 的点，不是 `…`，别顺手改成省略号。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// notFoundRedirect 是 404 页停多久就自己回首页。上游是 3 秒。
	notFoundRedirect = 3 * time.Second
	// notFoundTick 是倒计时每一跳的间隔，也就是上游那个每秒减 1 的
	// `useInterval(…, 1000)`。每一跳都要叫醒界面，否则数字会卡在 3 不动。
	notFoundTick = time.Second
)

// syncRoute 是「换页了」这件事唯一的收口：那个倒计时由它驱动，换页要一起收掉
// 的浮层也由它收（`closeOverlaysForNav`）。
//
// **收在这里而不是 navigate**：后退/前进按钮（viewNav 里那对 BackButton /
// ForwardButton）走的是 Router 自己的历史，不经过 navigate。漏了这一处，
// 「404 页 3 秒后跳首页」就会在用户已经退回别的页之后照样把他拽回首页；
// 而评论抽屉、队列抽屉、分集浮层、倍速菜单就会在换页之后还悬在画面上。
//
// 这一段跑在主线程的 view 里，所以改这些字段不违反铁律一；后台那一跳只调
// wakeHandle.invalidate，不碰它们（见 clock.go 顶上那条硬规矩）。
func (s *Shell) syncRoute() {
	// 到点了就回首页。**必须在这一帧的主线程上做**：`ui.Router.Push` 不是线程
	// 安全的（ui/router.go 的注释就写着只能从主线程改状态），后台那一跳因此
	// 只负责叫醒界面，跳页这件事留给这儿。
	if !s.notFoundAt.IsZero() && s.clock.Now().Sub(s.notFoundAt) >= notFoundRedirect {
		s.disarmNotFound()
		s.navigate("/")
	}

	path := s.router.Path()
	if path == s.shownPath {
		return
	}
	s.shownPath = path
	// 换页了这一拍把浮层一起收掉（票：评论抽屉换页不收）。收在这里的理由同
	// 上面那段注释——**换页不止 navigate 一条道**，而这里是唯一都过的那一处。
	s.closeOverlaysForNav()
	if lookup(path) == notFound {
		s.armNotFound()
	} else {
		s.disarmNotFound()
	}
}

// closeOverlaysForNav 换页的时候把浮在页面上的那几层收掉。
//
// # 收哪几层、为什么
//
// 换页了旧页的浮层都不该留着——它们盖的是**上一页**的东西，下一页上悬着一块
// 上一页的浮层，用户第一眼看到的是「这页坏了」：
//
//   - `commentsOpen`（评论抽屉，`ui.Overlay`）；
//   - `drawerOpen`（队列抽屉，同款 Overlay）；
//   - `queueClearOpen`（抽屉里那道「清空播放列表」二次确认）：**它不是抽屉的
//     一部分，是自己活着的一层模态**——抽屉收了它状态还在，下次开抽屉那个
//     没回答过的问题直接复活（这是对抗性审查点名的状态泄漏）；
//   - `partOpen`（分集浮层，框架 `Popover`）；
//   - `rateOpen`（倍速菜单，框架 `Popover`）；
//   - `sleepOpen`（睡眠定时，`Popover`）；
//   - `volOpen`（音量，`Popover`）；
//   - `errDetailOpen`（播放条「错误详情」，`Popover`）；
//   - `searchSuggest`（搜索联想面板，`PopoverBase`）；
//   - `avatarMenuOpen`（头像下拉，`PopoverBase`）；
//   - `dynMenuOpen`（动态卡片「⋯」菜单，`PopoverBase`）；
//   - `favPanelOpen`（折叠侧栏的收藏夹浮层，`PopoverBase`）；
//   - `full.partsOpen`（满屏播放器里的分集面板）；
//   - `lyric.offsetOpen` / `lyric.fontOpen`（播放条歌词区右下角的两个浮层，
//     `PopoverBase`）；
//   - `fullLyric.offsetOpen` / `fullLyric.fontOpen`（满屏歌词区同款两个）。
//
// ⚠️ **这些 Popover 都得显式收**：它们的「点外面自己关」那一条对**换页**
// 无效——换页不是「点外面」，那一层框架自己不会收。
//
// # 收的是「意图」（`open = false`），不是「阶段」
//
// `commentsPhase` / `drawerPhase` 是各自的阶段机（`syncCommentsDrawerPhase` /
// `syncDrawerPhase`）推的——置 open=false 之后它们自己走 `drawerOut`，面板
// **滑出去**那 200ms 照旧走。把阶段机也硬掰成 `drawerGone` 的话是「啪」地
// 没了，不是滑出去。
//
// ⚠️ **不存在「抽屉内部发起的换页」**：评论抽屉里能点的只有「展开/收起/楼中楼/
// 刷新」（`comments.go` / `comments_body.go`），队列抽屉里是队列行和清空——
// **没有一处调 `s.navigate`**。所以「先收抽屉、换页这一帧才发生」的顺序里
// 没有「收早了把正在导航的那下关动作弄闪烁」的问题。
//
// # 明确不收的，以及为什么
//
// 下面这几层**也是浮层**，但换页时**故意留着**——取舍写在这里，不是漏掉：
//
//   - **收藏夹管理的几个弹窗**（`folderEdit.open`、`folderEdit.crop.open`、
//     `folderSelect.open`、`folderDelete.open`，见 `viewFolderDialogs`）：
//     它们是**装着用户输入的表单**——敲了一半的收藏夹名、刚裁好的封面、
//     刚勾好的几个夹。换页收掉等于**静默丢掉用户没存的工作**；而它们是居中的
//     模态，压在新页上读起来是「在等你回答」，不是「这页坏了」。
//   - **各页的破坏性确认框**（`laterClearOpen`、`collection.confirm`、
//     `downloads.ask`、`local.rmOpen`）：同上——那是一个**没回答完的问题**，
//     `doIt` 闭包里还攥着页面上下文；换页替用户把「删除吗」按成「算了」是
//     替他做决定。
//   - **歌词搜索/预览**（`lyricSearch.open` / `lyricSearch.preview`）：
//     搜索框里的词和挑到一半的候选同理。
//   - **关于面板**（`viewAbout` 那一层）：应用级信息，从菜单开得着、跟当前
//     页没有上下文，不属于「上一页的东西」。
//   - **满屏播放器**（`full.open`）：它是**播放面本身**不是「压在页上的
//     浮层」——换页把播放器收了是拿导航当停止用，明显不对。
func (s *Shell) closeOverlaysForNav() {
	s.commentsOpen = false
	s.drawerOpen = false
	s.queueClearOpen = false
	s.partOpen = false
	s.rateOpen = false
	s.sleepOpen = false
	s.volOpen = false
	s.errDetailOpen = false
	s.searchSuggest = false
	s.avatarMenuOpen = false
	s.dynMenuOpen = false
	s.favPanelOpen = false
	s.full.partsOpen = false
	s.lyric.offsetOpen = false
	s.lyric.fontOpen = false
	s.fullLyric.offsetOpen = false
	s.fullLyric.fontOpen = false
}

// armNotFound 记下进 404 的那一刻，起那个每秒一跳的倒计时。
//
// 先收旧的：404 → 另一个 404（`/没有这条` → `/也没有这条`）也算换页，会走到
// 这里来。少了这一行，上一次那个每秒一跳的定时器就没人收了，它会一直醒到
// 窗口关掉。
//
// 那一跳回调是 `s.wake.invalidate`——只叫醒，不改任何状态。要真等到 3 秒、
// 要算还剩几秒，都是下一帧的 syncRoute / viewNotFound 在主线程上拿时钟现算。
func (s *Shell) armNotFound() {
	// ⚠️ 关掉之后不许再装（理由见 navbar_feed.go 里 `s.feed.arm` 那一句）：这一句
	// 是「画到 404 那一页」就会到的，而 `Close` 收掉的是**一个**已经装上的定时器。
	if s.closed {
		return
	}
	s.disarmNotFound()
	s.notFoundAt = s.clock.Now()
	s.notFoundStop = s.clock.EveryFunc(notFoundTick, s.wake.invalidate)
}

// disarmNotFound 收掉倒计时。离开 404、或者到点跳首页时调。
func (s *Shell) disarmNotFound() {
	if s.notFoundStop != nil {
		s.notFoundStop()
		s.notFoundStop = nil
	}
	s.notFoundAt = time.Time{}
}

// notFoundLeft 是倒计时上还剩几秒，从 3 数到 0。
//
// 按经过的时间现算，不记着「已经减了几次」：真时钟上那一跳和这一帧之间差几毫秒
// 是随机的，记次数会出现「数字写着 2、实际只剩 0.4 秒」。
func (s *Shell) notFoundLeft() int {
	left := notFoundRedirect - s.clock.Now().Sub(s.notFoundAt)
	if left <= 0 {
		return 0
	}
	// 向上取整：刚进来 0.1 秒就该写着 3，而不是 2。
	n := int(left / time.Second)
	if left%time.Second != 0 {
		n++
	}
	if n > int(notFoundRedirect/time.Second) {
		n = int(notFoundRedirect / time.Second)
	}
	return n
}

// viewNotFound 是 404 页。
//
// 四样：哪一页不存在（就是刚才敲进去的那个路径）、上游那句空状态、还剩几秒
// 回首页、以及**一枚立刻能按的「回到主页」**。第一行是这里唯一非上游的字：上游
// 那一页只知道「没匹配上」，不知道用户敲的是什么，404 不说清楚是哪一条就不成立。
//
// # 为什么多一枚按钮（票 #242，UX 报告 §3-A 的 A4）
//
// 原来这一页**只有一条出路**：等 3 秒自动跳首页。而 404 一般是**用户点错了链接**
// ——他多半想去别的页，不是主页。于是那个倒计时把他**丢到主页**，他刚才想找的
// 东西没了；而如果那个钟没推进（框架的钟在某些路径下站着不动），他就**永远停
// 在这一页**——页面上又没有按钮可以自救。
//
// 现在倒计时照旧走（上游那个行为不改），但**多一条立刻能走的路**。
//
// ⚠️ **按下去要先把倒计时收掉**：不收的话，那一秒恰好跨过 3 秒的话，倒计时会
// 在用户已经回首页之后**再把他拽一次**（虽然拽去的还是首页，但那是第二次跳转）。
func (s *Shell) viewNotFound(c *ui.Context, path string) {
	// 和其它页面 view 一样，头一件正事是装主题（铁律二，见 viewEmpty）。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key("page:not-found").Grow(1).Center().Gap(space.Px8).Padding(space.Px20, space.Px28).
		Children(func() {
			ui.Text(c, "404").FontSize(scale.Font(c, scale.Size28)).Bold().TextColor(t.TextMuted)
			// ⚠️ **限宽**：路径是用户敲进来的，长度不封顶——不限宽的话一条长
			// 路径会把这一行撑出窗口，而 404 这一页上又没有别的东西能提示
			// 「刚才敲的那一长串是个地址」。
			ui.Text(c, "没有「"+path+"」这一页").FontSize(scale.Font(c, scale.Size15)).MaxWidth(520)
			ui.Text(c, notFoundCountdown(s.notFoundLeft())).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "回到主页").Clicked() {
				s.disarmNotFound()
				s.navigate("/")
			}
		})
}

// notFoundCountdown 是倒计时那一行。字面照上游 a6c48e1 抄的，只是把数换成参数。
func notFoundCountdown(left int) string {
	return strconv.Itoa(left) + " 秒后回到主页..."
}
