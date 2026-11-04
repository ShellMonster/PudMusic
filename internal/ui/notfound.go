// 404 页：路径谁都不匹配时的那一页（原型 `PAGES.notfound`）。
//
// 搜索插画、一句说明和一枚「回到推荐音乐」。没有倒计时，也不自动跳走。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

// syncRoute 是「换页了」这件事唯一的收口：换页要一起收掉的浮层由它收（`closeOverlaysForNav`）。
//
// **收在这里而不是 navigate**：后退/前进按钮（viewNav 里那对 BackButton /
// ForwardButton）走的是 Router 自己的历史，不经过 navigate。漏了这一处，
// 评论抽屉、队列抽屉、分集浮层、倍速菜单就会在换页之后还悬在画面上。
//
// 这一段跑在主线程的 view 里，所以改这些字段不违反铁律一；后台那一跳只调
// wakeHandle.invalidate，不碰它们（见 clock.go 顶上那条硬规矩）。
func (s *Shell) syncRoute() {
	path := s.router.Path()
	if path == s.shownPath {
		return
	}
	s.shownPath = path
	// 换页了这一拍把浮层一起收掉（票：评论抽屉换页不收）。收在这里的理由同
	// 上面那段注释——**换页不止 navigate 一条道**，而这里是唯一都过的那一处。
	s.closeOverlaysForNav()
	// 原型的 404 没有倒计时、也不自动跳走（DESIGN.md §0′.T7）：只有一枚「回到推荐音乐」。
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
	s.full.partsOpen = false
	s.lyric.offsetOpen = false
	s.lyric.fontOpen = false
	s.fullLyric.offsetOpen = false
	s.fullLyric.fontOpen = false
}

// viewNotFound 是 404 页：原型只有一枚主色按钮「回到推荐音乐」，路径不画。
func (s *Shell) viewNotFound(c *ui.Context, path string) {
	// 和其它页面 view 一样，头一件正事是装主题（铁律二，见 viewEmpty）。
	s.applyFrameTheme(c)

	// 原型 `PAGES.notfound`：搜索插画 + 「这一页走丢了」+ 「链接可能已经失效，或者这一页还没有内容。」
	// + 主色按钮「回到推荐音乐」。没有倒计时、不自动跳走；路径也不画（原型没有）。
	_ = path
	s.pudPage(c, "page:not-found", func(_, _ float32) {
		pud.Empty(c, pud.ArtSearch, "这一页走丢了", "链接可能已经失效，或者这一页还没有内容。", func() {
			if pud.Button(c, "notfound:home", pud.Primary, pud.Std, "回到推荐音乐", "").Clicked() {
				s.navigate("/")
			}
		})
	})
}
