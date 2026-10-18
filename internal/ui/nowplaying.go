// 迷你播放器页（Biu 的 `/mini-player`，`src/pages/mini-player/`）：正在放什么，
// 以及它的歌词。
//
// ## 为什么歌词长在这一页，而不是 `/empty`
//
// 「没有歌词」是一个**有前提**的状态：得先有歌在放，才谈得上「这首歌没有歌词」。
// 挂在一张跟播放无关的通用展示页上（之前是这么做的），等于没打开任何歌的时候也
// 跟你说「暂无歌词、去换个歌词源」——那句话和屏幕上的事实对不上。
//
// Biu 的歌词住在全屏播放器里，那是阶段 6 的东西（PLAN.md 的 S-08）。这一版能落地的
// 只有迷你播放器：它是 Biu 那 14 条路由里**唯一一条表示「正在放什么」**的页面，
// 而歌词正是「正在放的这首歌」的属性。所以歌词区摆在这儿，等全屏播放器来了再
// 搬过去。
//
// 两个状态都从真实的播放状态推出来，不是无条件画出来的：
//
//	没有歌在放   上游 pages/mini-player/index.tsx:128 的「暂无播放内容」
//	有歌在放     歌名 + 歌词区（lyrics.go）。
//
// 歌词区**只从缓存来**（`app/lyrics.go`）：缓存里没有就是那一句「暂无歌词」
// ——照实说，不画一个转圈（SPEC 的四条铁律之外那条：空状态不能拿「加载中」搪塞）。
//
// 「去找一份歌词」的那一条路（两个搜索/预览弹窗）**不在这一票里**：歌词搜索的
// **服务层和应用层**已经接上（`service/lyrics/search.go`、`app/lyrics_search.go`，
// 两个源和结果缓存都在，`lyrics_mount.go` 把源挂上了），界面上那一个入口是拆出去的
// **下一票 BMG-66**「歌词搜索与缓存：搜索与预览两个弹窗」。
// ⚠️ 在它落地之前 `noLyrics.Hint` 那句「在歌词搜索里换个源再找一次」是一句兑现
// 不了的承诺（那是人的判断，见这一票的收尾说明，没有替它改）。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够（铁律二，
// 见 theme.go）。
package ui

import "github.com/egoist/mygo/ui"

// miniPlayerPath 是迷你播放器那条路由，和 routes 表里登记的是同一个字符串。
const miniPlayerPath = "/mini-player"

// nothingPlaying 是「没有歌在放」。
//
// 标题抄上游 `pages/mini-player/index.tsx:128`；Hint 是我们写的。
var nothingPlaying = emptyState{
	Title: "暂无播放内容",
	Hint:  "在主窗口里点「打开文件」，这边就会显示正在放什么",
}

// viewNowPlaying 是迷你播放器：没歌时一句话，有歌时歌名 + 歌词区。
func (s *Shell) viewNowPlaying(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	st := s.app.Snapshot()
	if !st.HasTrack {
		s.viewEmpty(c, "page:mini-player:nothing", nothingPlaying)
		return
	}

	// 正在放：歌名 + 歌词区。
	//
	// 歌词区本体在 lyrics.go：逐行高亮、滚到中间、上下渐隐、译文叠在原词下面，
	// 右下角浮着偏移与字号两个旋钮。底下那一层（`service/lyrics` 的解析与缓存、
	// `app/lyrics` 的切歌先重置 / 偏移字号夹界 / 防抖落盘）负责「哪一句正在唱」
	// 和「那两个旋钮能走到哪儿」，这一层只画。
	ui.Column(c).Key("page:mini-player").Grow(1).Padding(28).Gap(12).Children(func() {
		ui.Text(c, st.Title).FontSize(20).Bold()
		s.viewLyrics(c, s.app.Lyrics(), st.Position)
	})
}
