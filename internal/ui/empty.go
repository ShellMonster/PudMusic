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
// 有宿主的直接被路由表引用（`/collection/{id}` 用 noFavorites、`/download-list`
// 用 noDownloads）；还没有宿主的（noSearchResults、noLyrics）跟着它们该在的
// 功能一起落地——搜索结果列表是阶段 5，歌词面板是阶段 6。在那之前它们由
// `TestSpecEmptyStates` 直接驱动渲染器验，画面上是真画出来的，不是只对了个
// 常量。

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
//
// 歌词面板是阶段 6 的东西（PLAN.md 的 S-08），到那时它才有地方落脚。
var noLyrics = emptyState{
	Title: "暂无歌词",
	Hint:  "在歌词搜索里换个源（网易云 / LRCLIB）再找一次",
}
