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
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// commentsButtonKey 是播放条上「评论」那一枚的身份。
//
// ⚠️ **要钉住身份**（理由同 `playbar_keys.go` 的文件头）：这一段是几枚并排，
// 而「分集」和「评论」都是**条件摆**的——它们一出现，兄弟序号就变了，而框架给
// 元素的默认身份是「父 ID + 兄弟序号」。于是这一枚会**继承**上一枚留下的动画
// 通道（画面上是「另一枚键莫名其妙缩着」）。
const commentsButtonKey = "playbar:comments"

// viewCommentsButton 是播放条上那一枚「评论」键。
//
// 🔴 **不摆的时候一个格子都不画**（理由见文件头第一条）：本地文件那一档
// 一个请求都不发。
func (s *Shell) viewCommentsButton(c *ui.Context, st app.State) {
	if !st.HasTrack || !s.app.CommentsWorthAsking() {
		return
	}
	c.Key(commentsButtonKey)
	if s.withTip(c, iconKey(c, iconComments, "评论", iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		"看这一首的评论").Clicked() {
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
