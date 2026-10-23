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
// # 刻度分两族
//
// **正文刻度**（8 档）：12 / 13 / 14 / 16 / 18 / 20 / 24。
//
// 这一族是**通用的**：任何一处正文、副题、标题都从这一族里取。
// `13` 是 DESIGN.md 承认的一档（票 #238 / 报告 F-18 选的路子是
// 「**承认它**」而不是「把 61 处 13 归到 12 或 14」——后者会把副题和正文
// 压成一样大，层次就没了）。
//
// **角色专属档**（10 / 11 / 15 / 28 / 30）：每一档**只有一个用途**，
// 不是通用档：
//
//	10 —— 歌词面板里那个数值（上游 `text-[10px] font-bold`）
//	11 —— 极次要的读数（图标旁边那一行小字：8px、日期格、倍速读数）
//	15 —— 空状态 / 弹窗的标题（空状态那一行、404 说明、裁剪标题）
//	28 —— 404 那一页的大字
//	30 —— 合集 / 收藏夹头图区的大标题（上游 `text-3xl`）
//
// ⚠️ **别拿它们当通用档用**：一个「标题」用了 15、另一个用了 16，
// 差的那 1 像素在画面上看不出来，但它正是「一套体系」被破坏的样子
// （报告 F-18 的原话）。**新写一处文字，先问它属于哪一族。**
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
	// Size15 是空状态 / 弹窗的标题。
	Size15 = 15
	// Size16 是列表 / 网格 / 队列的歌名（上游 `text-base`）。
	Size16 = 16
	// Size18 是队列抽屉的标题（上游 `h3` = `text-lg`）。
	Size18 = 18
	// Size20 是设置页的分组标题（上游 `h2` = `text-xl`）。
	Size20 = 20
	// Size24 是页标题（上游 `h1` = `text-2xl`）。
	Size24 = 24
	// Size28 是 404 那一页的大字。
	Size28 = 28
	// Size30 是合集 / 收藏夹头图区的大标题（上游 `text-3xl`）。
	Size30 = 30
)

// 角色别名：同一个档位在不同语境里有不同的名字。
//
// ⚠️ **别名不产生第二份值**——它们就是上面那些刻度名的另一个写法。
// 角色成立的地方用它（`scale.PageTitle = scale.PageTitle`），
// 讲的是「这一处要哪一档」而不是「这一处是几」。
const (
	// PageTitle 是页标题（上游 `h1` = `text-2xl`）。
	PageTitle = Size24
	// CollectionTitle 是合集 / 收藏夹头图区的大标题（上游 `text-3xl`）。
	CollectionTitle = Size30
	// NotFoundTitle 是 404 那一页的大字。
	NotFoundTitle = Size28
	// NewMusicTitle 是首页「新歌速递」那一块的大标题。
	NewMusicTitle = Size24
	// BrandName 是侧栏里那个品牌字。
	BrandName = Size24
	// ListTitle 是列表 / 网格 / 队列那一行的歌名。
	ListTitle = Size16
	// ListOwner 是列表那一行的 UP 名 / 副题。
	ListOwner = Size14
	// CollectionCopy 是合集头图区下面那几行说明（简介 / 类型 / 创建者）。
	CollectionCopy = Size14
	// LyricsTranslation 是歌词的译文那一行。
	LyricsTranslation = Size14
	// EmptyTitle 是空状态那一行「这里为什么空」。
	EmptyTitle = Size15
	// EmptyHint 是空状态第二句「怎么才能不空」。
	EmptyHint = Size12
	// EmptyPageTitle 是页面级空状态（整页只有一句）那个标题。
	EmptyPageTitle = Size20
	// LyricValue 是歌词面板里那个数值。
	LyricValue = Size10
	// HeaderFont 是列表表头那一行的字。
	HeaderFont = Size14
	// ToastFont 是提示条那一行的字。
	ToastFont = Size14
	// FavPanelItem 是收藏夹浮层里那一项的字。
	FavPanelItem = Size13
	// RateMenuFont 是倍速菜单里那一项的字。
	RateMenuFont = Size12
	// VolPercentFont 是音量浮层里那个百分比的字。
	VolPercentFont = Size12
)
