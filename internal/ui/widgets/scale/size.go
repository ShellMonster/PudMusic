// 字号刻度。
//
// # 为什么字号的出处是这张表
//
// 审计（票 #212 的 F-18）数过：界面上有 **12 档字号在用**，其中 6 档不在
// DESIGN.md §二的刻度上（`13` 61 处、`11` 16 处、`15` 7 处、`28` / `30` / `10`
// 各 1 处），而 **17 个命名的字号常量散在 14 个文件**里——没有一张字号表。
//
// 这张表把每一档的值写在**一处**，调用处只说「要哪一档」——
// 跟 `widgets/space`（间距）、`widgets/ink`（颜色）、`widgets/shadow`（阴影）
// 同一个路数。
//
// # 刻度（「深夜唱片行」§0′，2026-11）
//
// **正文段**：11 / 12 / 13 / 14 / 16（系统字体；11 是等宽的 Eyebrow / kbd）。
// **展示段**：20 / 24 / 32 / 48 / 64（宋体 700，走 `widgets/typo` 的 `Display`）。
// **角色专属**：10（音质角标）、28（全屏歌词当前句）。
//
// 15 / 18 / 30 三档已退役：引用迁到相邻档（15 → 16、18 → 20 或分区标题 24、
// 30 → 32），见 DESIGN.md §0′。
//
// # 为什么表里也放这些「不在刻度上」的档
//
// 因为它们**在用**：值不写在这里，就散回各文件的字面量里去了。
// 归到别的档（把 15 改成 16、把 28 改成 24）是**改画面**，
// 那是另一票的事（`widgets/space` 那张表也是这么写的：
// 「碎步也在表里钉死——归一是另一票的事」）。
//
// # 数值为什么是这些
//
// 每一档的用途和上游出处见 DESIGN.md §二 的表（那里是目标值，
// 这张表是它的代码形态；两边对不上以 DESIGN.md 为准）。

package scale

const (
	// Size10 是歌词面板里那个数值（上游 `text-[10px] font-bold`）。
	Size10 = 10
	// Size11 是极次要的读数（上游 `text-[11px]` 一类的地方）。
	Size11 = 11
	// Size12 是次要信息（上游 `text-xs`）——列表里的播放量、投稿时间。
	Size12 = 12
	// Size13 是副题（比正文的 14 小一档）。
	Size13 = 13
	// Size14 是正文 / 说明（上游 `text-sm`）。
	Size14 = 14
	// Size16 是列表 / 网格 / 队列的歌名（上游 `text-base`）。
	Size16 = 16
	// Size20 是 Subhead：品牌字、设置分组标题、历史日期头（宋体 700）。
	Size20 = 20
	// Size24 是 Section：分区标题、抽屉 / 对话框标题、空状态标题（宋体 700）。
	Size24 = 24
	// Size28 是全屏歌词的当前句（原型 24 × 1.18）。
	Size28 = 28
	// Size32 是 Title：UP 主名、榜单序号大字、窄窗下的页标题 / 统计大数字（宋体 700）。
	Size32 = 32
	// Size40 是登录页的「扫码登录」标题（原型 `.h-page` 上的行内 `font-size:40px`，宋体 700）。
	Size40 = 40
	// Size48 是 Display：页标题、问候语、统计大数字（宋体 700）。
	Size48 = 48
	// Size64 是 Hero：「今日一张」专辑名、收藏夹 / 合集标题（宋体 700）。
	Size64 = 64
)

// 「深夜唱片行」的标题角色（DESIGN.md §0′，原型 `.h-page` / `.h-sec` / `.hero-title`）。
//
// 20 号及以上的标题一律用宋体 700：`typo.Display(ui.Text(c, s), scale.Font(c, scale.PageTitle))`。
// 内容区窄于 1024 时页标题和 Hero 降一档，用 `PageTitleFor` / `HeroTitleFor`。
const (
	// PageTitle 是页标题：48、宋体 700（`TestPageTitlesAreDisplayBold` 钉着）。
	PageTitle = Size48
	// SectionTitle 是分区头 / 抽屉 / 对话框 / 空状态标题：24、宋体 700。
	SectionTitle = Size24
	// HeroTitle 是封面级大字：64、宋体 700。
	HeroTitle = Size64
	// Subhead 是品牌字、设置分组标题、历史日期头：20、宋体 700。
	Subhead = Size20
)

// NarrowContent 是标题降档的分界：内容区宽度小于它时，48 → 32、64 → 48
// （design-tokens.html 3.1：原型 `clamp(32px, 3.4vw, 48px)` 的桌面落法）。
const NarrowContent = 1024

// PageTitleFor 是这一宽度的内容区里页标题该用的字号（还没乘系统缩放）。
func PageTitleFor(contentW float32) float32 {
	if contentW > 0 && contentW < NarrowContent {
		return Size32
	}
	return PageTitle
}

// HeroTitleFor 是这一宽度的内容区里 Hero 标题该用的字号（还没乘系统缩放）。
func HeroTitleFor(contentW float32) float32 {
	if contentW > 0 && contentW < NarrowContent {
		return Size48
	}
	return HeroTitle
}

// 角色别名：同一个档位在不同语境里有不同的名字。
//
// ⚠️ **别名不产生第二份值**——它们就是上面那些刻度名的另一个写法。
// 角色成立的地方用它（`scale.PageTitle = scale.PageTitle`），
// 讲的是「这一处要哪一档」而不是「这一处是几」。
const (
	// CollectionTitle 是合集 / 收藏夹头图区的大标题。30 那一档退役后先归到
	// 相邻的 32；T3 按原型换成 `HeroTitle`（64）。
	CollectionTitle = Size32
	// NotFoundTitle 是 404 那一页的大字：它是那一页上最大的一个字。
	NotFoundTitle = Size28
	// NewMusicTitle 是「新歌速递」那一行标题的字号。
	//
	// 上游那一处是整页唯一的 `<h1>`（`new-music-top.tsx:178`），字号照
	// DESIGN.md 的「页标题 24/400」：24 号、**不加粗**（权重由 Preflight 重置成
	// inherit）。这一块要「立得住」靠的是字号，不是字重。
	NewMusicTitle = Size24
	// BrandName 是品牌字（应用名）的字号。上游 `text-2xl` = 24
	// （DESIGN.md 的排版表：品牌字 = 24 / 粗体 / 主色）。
	BrandName = Size24
	// PlayerTitle 是播放器（满屏 / 迷你播放器）那一页的歌名（上游 `h2` =
	// `text-xl`）。
	//
	// ⚠️ **满屏和迷你播放器必须是同一个数**：它们是同一首歌的两个地方，
	// 字号不一样的话，从迷你窗点开满屏会看见那行字忽然变大。
	PlayerTitle = Size20
	// ListTitle 是列表 / 网格 / 队列那一行的歌名（DESIGN.md 的「列表歌名 16」）。
	ListTitle = Size16
	// ListOwner 是列表那一行的 UP 名 / 副题（DESIGN.md 的「列表 UP 名 14」）。
	ListOwner = Size14
	// CollectionCopy 是合集头图区下面那三行字（简介 / 类型 + 条数 / 创建者），
	// 都是 `text-sm` = **14**（上游同一个文件 :53-113）。
	CollectionCopy = Size14
	// LyricsTranslation 是译文那一行的字号，抄 `index.tsx:344` 的 `text-sm`
	// （0.875rem = 14px）。**是死的**，不跟着原词那个字号走——上游的
	// `text-sm` 就是绝对值，12px 的歌词配 14px 的翻译是 Biu 自己的样子。
	LyricsTranslation = Size14
	// EmptyTitle 是空状态那一行「这里为什么空」的字号。
	//
	// ⚠️ **「正在加载」那一档不用这个数**（票 #178）：那一档画的是一枚转圈，
	// 没有字。改之前那一档是一行同样大小的淡字，而它现在是一个图形。
	//
	// 15 那一档退役（§0′）：原型的空状态标题是 24 宋体（`SectionTitle`），由
	// `emptyTitle` 套上 `typo.Display`。
	EmptyTitle = SectionTitle
	// EmptyHint 是空状态第二句「怎么才能不空」的字号。
	EmptyHint = Size12
	// EmptyPageTitle 是上面那一页**页面名**的字号。
	//
	// 它比 `emptyCopy` 的标题大一号，因为它是**这一页叫什么**（一直都在），而
	// `emptyCopy` 那一句是**这里为什么空**（只在空着的时候有）。两句混成一个字号的话，
	// 用户分不出哪一句是导航、哪一句是状态。
	EmptyPageTitle = Size20
	// LyricValue 是浮层里那个数值的字号，抄两个 control 里
	// `text-[10px] font-bold`。
	LyricValue = Size10
	// HeaderFont 是列表表头那一行的字。**14** 是上游那一行表头的数
	// （`music-list-item/header.tsx:22` 的 `text-sm`）。
	//
	// ⚠️ 原来是 12：那是行里**次要文字**（UP 名 / 时长 / 播放量）的号。列名用着和
	// 次要文字一样的号、一样的灰，竖着扫下去时它比下面那一列数字还弱——而数字列本来
	// 就是靠表头才对得齐的。上游那边列名 14、次要文字 14、播放量 / 时长 12，列名站在
	// 它们上面。
	HeaderFont = Size14
	// ToastFont 是卡片上那行字的字号（上游 `text-sm` = 14）。
	//
	// 字重是 `weightMedium`（500，上游的 `font-medium`）——此前是 13/400。
	ToastFont = Size14
	// FavPanelItem 是收藏夹浮层里那一项的字。
	//
	// ⚠️ **14（票 #371）**：这一层是折叠侧栏时的收藏夹入口，而它列的是**收藏
	// 夹名**——和侧栏里那几行是同一样东西，就得是同一个号（正文 14）。原来是
	// 13（副题那一档），于是同一个名字在两个地方是两种大小。
	FavPanelItem = Size14
	// RateMenuFont 是浮层里那七行的字号：**12**。
	//
	// 上游那一列的按钮是 `size="sm"`，HeroUI 那个档的字是 `text-tiny`（12）。
	// 字大一号，44 宽就装不下「0.75x」了（见 playbar_rate.go 那段）。
	RateMenuFont = Size12
	// VolPercentFont 是浮层底下那个百分比的字号：**12**。
	//
	// 上游是 `endContent` 的 `text-xs`（12，`music-volume/index.tsx:126-149`），
	// 我们原来写的是 11（票 #220 / 报告 §3.9 对齐上游）。
	VolPercentFont = Size12
)
