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
	// Hint 怎么才能不空。
	//
	// ⚠️ 这一句要给用户**能做的下一步**，不是把 Title 换个说法，也不是讲实现进度
	// 到哪了。「这个 UP 主还没有发过动态」和「还没有歌在放」都只是把标题又说
	// 了一遍；「音乐榜和分区推荐还没有接进来」是写给开发者看的，用户拿它没法
	// 办任何事。合格的写法长这样：「回首页听歌，或者换一个 UP 主的空间看看」。
	//
	// 空串就只画上面那一句（上游没有第二行）。判据在测试里：
	// `TestEmptyStateCopySaysWhyAndHow` 要求每一句都带一个动作词。
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

// SPEC 点名的四个空状态场景（PLAN.md 的 C-02 / S-01）。四条各有各的一句上游
// 文案和一句「怎么才能不空」，`TestSpecEmptyStates` 逐条渲染出来验。
//
// **四条全部有宿主**，`TestSpecEmptyStates` 一条一条切过去验，没有一条是只有
// 测试引用的常量：
//
//	没有收藏夹   /collection/{id}   路由表里的 Empty
//	没有搜索结果 /search（带上 q）  searchpage.go 的 viewSearch
//	没有下载记录 /download-list     路由表里的 Empty
//	没有歌词     /mini-player（有歌在放时）  nowplaying.go 的 viewNowPlaying
//
// 「没有歌词」这一条的前提是**有歌在放**——没打开任何文件的时候不该看见「暂无
// 歌词」，那句话和屏幕上的事实对不上。宿主是迷你播放器：它是 14 条路由里唯一
// 一条表示「正在放什么」的页面，而歌词是「正在放的这首歌」的属性。理由写在
// nowplaying.go 顶上。
//
// 「怎么才能不空」那一句必须给用户一个能做的下一步，见 emptyState.Hint 和
// TestEmptyStateCopySaysWhyAndHow。

// noFavorites 是「没有收藏夹」。
//
// 标题抄上游：`pages/video-collection/favorites/` 那两个文件一个字都没有，
// 空的时候落到 `components/virtual-page-list` 的兜底（index.tsx:146）。
var noFavorites = emptyState{
	Title: "暂无内容",
	Hint:  "在歌上点星标，挑一个收藏夹存进去，就能在这里看到",
}

// noSearchResults 是「搜了，但没有结果」。
//
// ⚠️ 别和「还没输入搜索词」混成一条：上游那是**两个**分支，画出来都是同一个
// `<Empty />`（所以标题一样），但接下来该做的事不一样——
//   - 还没输入：`pages/search/index.tsx:16` 的分支，叫用户去输入；
//   - 搜了没有：`pages/search/video-list/index.tsx:176` 的分支，叫用户换个词。
//
// 现在 `/search` 那一页处在第一个分支（它还没有搜索框，见 routes 表里那一条），
// 所以路由表上填的不是这一条。这一条跟着搜索功能一起落地。
var noSearchResults = emptyState{
	Title: "暂无内容",
	Hint:  "换个词再搜一次，或者把关键词缩短一点",
}

// noDownloads 是「没有下载记录」。
//
// 标题抄上游 `pages/download-list/index.tsx:155` 的 `emptyContent`。
var noDownloads = emptyState{
	Title: "暂无内容",
	Hint:  "搜一首歌，在它旁边点「下载」，下完的会出现在这里",
}

// noLyrics 是「没有歌词」。
//
// 标题抄上游 `src/components/lyrics/index.tsx:375`：那里是一句三选一，
// 「歌词加载中...」/「暂无歌词」/ 歌词本身，这里取空的那一支。
//
// Hint 是我们写的（上游那一句就是全部）：Biu 的歌词不是从 B 站拿的，是歌词
// 搜索按歌名去两个源找的（`components/lyrics-search-modal/` 下的
// `netease-tab` 和 `lrclib-tab`），没有上传这一说。所以「怎么才能不空」就是
// 换个源再找一次——这不是替上游许诺，是说清这件事有哪几条路。
var noLyrics = emptyState{
	Title: "暂无歌词",
	Hint:  "在歌词搜索里换个源（网易云 / LRCLIB）再找一次",
}

// viewLoadError 是「这一块拉失败了」的提示 + 一个重试按钮。
//
// # 为什么它不能是 `viewEmpty` 的一个空状态
//
// 三个理由，每一个都够单独一票：
//
//  1. **文案不能混**。「暂无动态」和「加载失败」在用户那边是两件完全不同
//     的事，而闸门一失败就把第一支挡住了（不重试），界面上只剩「暂无」——
//     用户会以为那个人真的什么都没发。
//  2. **必须有重试按钮**。空状态那一支没有按钮，于是失败之后唯一能
//     继续的路是人点一下。
//  3. **切走再切回来不会自己好**。这一页的状态按 mid 缓存着，翻回来
//     还是那份失败，而 `want()` 因为 `err != nil` 一直挡着。
//
// ⚠️ 它自己**不碰闸门**：`retry` 是调用方给的闭包，闸门那一摊在
// `upspace_tabs.go` / `dynamicfeed.go` 那边，离得近才好改。
//
// ⚠️ **每个重试按钮要带自己的名字**（`retryLabel`），不能全叫「重试」：
// 动态流那一页同时有两块会失败（左边的关注的人、右边的动态），两个同名
// 按钮摆在一起，用户分不清按的是哪个——按错了的那一块没反应，另一块还在
// 转圈，看起来就像「点了没用」。
func (s *Shell) viewLoadError(c *ui.Context, key, retryLabel string, err error, retry func()) {
	// 🔴 铁律二：和 viewEmpty 一样，每一帧开头装主题。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key(key).Grow(1).Center().Gap(8).Padding(20, 28).Children(func() {
		ui.Text(c, "这一块没拉出来").FontSize(15).TextColor(t.Text)
		ui.Text(c, err.Error()).FontSize(12).TextColor(t.TextMuted).MaxLines(3)
		if retry != nil && ui.Button(c, retryLabel).Clicked() {
			retry()
		}
	})
}
