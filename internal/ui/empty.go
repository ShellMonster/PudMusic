// 空状态：一页没有内容时画的东西（Biu 的 P-29 / C-02，PLAN.md 里的 S-01）。
//
// **标题逐条抄 Biu 上游**，出处标在 routes 表里每一条的上面。上游的空状态只有
// 一个 title prop（`src/components/empty/index.tsx`），没有第二行，所以这里的
// Hint 是我们自己写的一句话，只回答「怎么才能不空」，不复述标题、也不改上游
// 那句的意思。上游没有专属文案、只落到共享默认 `暂无内容` 的页，这里也照抄
// `暂无内容`：编一句更好听的，等于让这一页和 Biu 对不上，而对不上正是这个
// 项目要避免的。
//
// # 一套绘制入口，七个页面各画各的
//
// **画法只有一个**（`drawEmpty`），而「哪个页面」体现在每一条 `emptyState` 自
// 己带的那张插画上。上游所有页面共用同一枚 `RiCloseCircleLine`，于是十三条路由
// 的空状态一模一样——用户看到「暂无内容」既不知道是哪一页，也无法把这一格和
// 别的空状态区分开。这一票给每一页配一枚说得清「这一页是干什么的」的图标，
// 而画法（居中、留白、字号、主次色）仍然是共享的那一份：改一次留白，七个页面
// 一起变，而不是七个页面各改一次然后慢慢漂。
//
// 接入点只有两个：`viewEmpty`（页面级）和 `pagedRows.viewEmptyState`（列表 /
// 网格那一块里的），两个都调 `drawEmpty`。
package ui

import "github.com/egoist/mygo/ui"

// emptyState 是一页空着时说的两句话，外加那一格插画位上的图标。
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
	// Icon 是插画位上那一枚图标（上游的「分类/文件名」，见 icons.go）。
	//
	// ⚠️ **给的是「这一页是干什么的」，不是随便一枚好看的**：首页推荐配音乐、
	// 稍后再看配钟、UP 主空间配一个人，而**不是**七页都配上游那一枚
	// `close-circle-line`——那正是「七个页面共用一张」这个差评的由来。
	//
	// 空串就**不画图标**（那一格只剩两行字）。这是「正在加载…」那一支的形状
	// （见 dynamicfeed.go / follow.go），那一支说的是「还没好」而不是「没有」，
	// 摆一枚插画反而像在说「这里什么都没有」。
	Icon string
}

// 空状态那一格插画的大小。抄上游 `components/empty/index.tsx:16` 的 `size={32}`。
//
// 为什么固定成 32 而不跟主题字号：图标的大小要和**别的**图标（播放条那批 14 / 18）
// 分得开——同一个数在两处含义不同，一处跟着主题走、一处写死，测试才量得出
// 「换主题之后它没变」。
const emptyIconSize = 32

// emptyGap 是图标、标题、提示三样之间的间距。上游是 `space-y-2`（8），
// 这里多一样东西（上游只有两样），照抄 8 的话三样会挤成一团。
const emptyGap = 8

// drawEmpty 是**唯一**的空状态画法。页面级（`viewEmpty`）和列表 / 网格那一块
// （`pagedRows.viewEmptyState`）都走它——七页共用一套绘制入口就是指这个。
//
// 它是**一个自由函数**而不是 `Shell` 的方法：这一格也会被列表 / 网格画
// （`pagedRows` 那一支），而 `pagedRows` 身上没有 `Shell`——硬挂一个字段进去
// 只为了让一个不碰外壳的画法能写成 `s.` 而已。
//
// key 给这一块（空串 = 不给，那一格不是动态列表里的行，不受铁律四约束）。
// pad 是上下留白（上游 `py-20` 的意思）。列表里那一块比整页矮，留白要小一档，
// 否则上下那两行字在列表里飘在半空；所以它是参数而不是写死的数。
func drawEmpty(c *ui.Context, key string, e emptyState, pad float32) {
	col := ui.Column(c)
	if key != "" {
		// ⚠️ Key 写在构造**之前**（铁律四的坑）：构造之后再 Key 等于没给。
		col = col.Key(key)
	}
	col.Grow(1).Center().Gap(emptyGap).Padding(pad, 28).Children(func() {
		emptyContent(c, e)
	})
}

// emptyContent 把「一枚插画 + 标题 + 提示」这三样画进**当前这一个容器**。
//
// 它是 `drawEmpty` 的内层，单独拎出来是因为有一处要在这三样**下面**再摆一句
// 别的：`viewLibraryMissing`（「还没接上 B 站接口」这一句）。那一处要的是同一
// 枚插画、同两行字，而外层还要自己加一句——所以拆成「内容」和「外壳」两层，
// 免得那一处把三样的字号和主次色再抄一遍（抄一遍就是会漂开的那份）。
func emptyContent(c *ui.Context, e emptyState) {
	t := c.Theme()
	if e.Icon != "" {
		// 次要色：图标是修饰，不是这一页要说的那句话。上游那枚也是
		// `text-default-400`（`index.tsx:16` 整个空状态一个色）。
		ui.Icon(c, icon(e.Icon)).FontSize(emptyIconSize).TextColor(t.TextMuted)
	}
	ui.Text(c, e.Title).FontSize(15).TextColor(t.Text)
	if e.Hint != "" {
		ui.Text(c, e.Hint).FontSize(12).TextColor(t.TextMuted)
	}
}

// viewEmpty 画一页的空状态。key 给这一页的根元素（铁律四）。
//
// 照 Biu 的 `src/components/empty/index.tsx`：整体居中（`items-center
// justify-center`）、上下留白（`py-20`）、一枚 32px 的图标在上。
//
// 颜色上上游两行都是 `text-default-400`（同一个灰），这里分成主次两档：标题
// 用正文色，第二句用次要色。15px 的浅灰在浅色底上已经偏淡，两行一起淡就分不
// 出主次，而这一页要说的恰恰是「标题是结论、第二句是办法」。
func (s *Shell) viewEmpty(c *ui.Context, key string, e emptyState) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。SetTheme 只影响它之后
	// 创建的控件，不装的话这里会用 MyGo 在帧开头重置好的系统色画出来，跟旁边
	// 的界面对不上。多调一次没有副作用——applyFrameTheme 认得出「这一帧已经
	// 装过了」，见它上面的注释。
	s.applyFrameTheme(c)
	drawEmpty(c, key, e, emptyPagePad)
}

// emptyPagePad 是整页那一支的上下留白。20 ≈ 上游 `py-20` 的一半（上游那个
// Tailwind 类是 5rem = 80，这里是紧凑桌面应用的窗口，取 20 上下留白就够了，
// 再多就只把两行字推离窗口中心）。
const emptyPagePad = 20

// emptyRowPad 是列表 / 网格那一块里的上下留白。比整页小一档：整页上下有筛选
// 条和播放条，中间本来就空；列表里那一块四周全是行，留白大了就只剩字飘在当中。
const emptyRowPad = 12

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
	Icon:  iconEmptyFolder,
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
	Icon:  iconEmptySearch,
}

// noDownloads 是「没有下载记录」。
//
// 标题抄上游 `pages/download-list/index.tsx:155` 的 `emptyContent`。
var noDownloads = emptyState{
	Title: "暂无内容",
	Hint:  "搜一首歌，在它旁边点「下载」，下完的会出现在这里",
	Icon:  iconEmptyDownload,
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
	Icon:  iconEmptyLyrics,
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
//
// ⚠️ **where 是「哪一趟请求」**，而 `err.Error()` 往往只说得清「失败了」说
// 不清「哪一趟」：B 站返回 -101 和网络超时在界面上都是一句 `-101` 或者一句
// `context deadline exceeded`，而用户要给开发者复现时需要的是「关注的人那一
// 趟」还是「动态那一趟」。所以每一处调用方都要写清楚是哪一趟，界面上单独一行
// 摆在错误上面——这是「出错在哪一步」在请求这一层的落法（页面那一层是 `viewCrash`）。
func (s *Shell) viewLoadError(c *ui.Context, key, where, retryLabel string, err error, retry func()) {
	// 🔴 铁律二：和 viewEmpty 一样，每一帧开头装主题。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key(key).Grow(1).Center().Gap(emptyGap).Padding(emptyPagePad, 28).Children(func() {
		// 插画用警告那一枚而**不是**空状态那一批：这一格说的是「没拉出来」，
		// 不是「本来就没有」。
		ui.Icon(c, icon(iconFailed)).FontSize(emptyIconSize).TextColor(t.Warning)
		ui.Text(c, "这一块没拉出来").FontSize(15).TextColor(t.Text)
		if where != "" {
			// 主次和错误本身再分一档：这一行是给用户复现用的（说给开发者听），
			// 而下面那行是原始错误。两者都别比标题更抢眼。
			ui.Text(c, "没拉出来的是："+where).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Text(c, err.Error()).FontSize(12).TextColor(t.TextMuted).MaxLines(3)
		if retry != nil && ui.Button(c, retryLabel).Clicked() {
			retry()
		}
	})
}
