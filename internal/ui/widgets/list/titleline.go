package list

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	inkpkg "github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/egoist/mygo/ui"
)

// Shown 是一首歌在列表 / 网格里**真正显示**的几样字：清洗过的歌名、副标题（从标题方括号里
// 挪出来的歌手 / 专辑名拼在 UP 名前面）、是不是现场版。清洗规则见 `widgets/title`。
// 它只管显示：`Music.Title` 不变，勾选框 / 菜单 / 无障碍名字仍用原标题。
//
// ⚠️ **导出的**：界面层里有几处自己画行 / 画卡片（首页的新歌速递那一块，见
// `internal/ui/homepage_newmusic.go`），它们要显示的字**必须和列表里的一样**——
// 两处各写一份清洗算法的话，同一首歌在两个地方会显示成两个名字。
type Shown struct {
	// Title 是清洗过的歌名。
	Title string
	// Owner 是副标题那一行：UP 名（前面可能缀着从标题方括号里挪出来的歌手 / 专辑名）。
	Owner string
	// Live 是「这是现场版」。
	Live bool
}

// ShowOf 清洗一首歌的显示字。
func ShowOf(m Music) Shown {
	c := title.Clean(m.Title)
	owner := m.Owner
	switch {
	case c.Extra != "" && owner != "":
		owner = c.Extra + " · " + owner
	case c.Extra != "":
		owner = c.Extra
	}
	return Shown{Title: c.Title, Owner: owner, Live: c.Live}
}

// liveTagRadius 是「现场」小标签的圆角：刻意不跟主题圆角走（标签是个小胶囊，
// 用户把全局圆角拖到 24 它也不该变成圆球）。
const liveTagRadius = 4

// LiveTag 画那枚「现场」小标签：caption 号、强调色字、强调色 14% 的淡底（规范 §7.3）。
//
// ⚠️ **导出的**：界面层里自己画行 / 画卡片的那几处要画同一枚标签（见 `Shown` 的
// 注释）——各画各的的话，同一首歌在一处有「现场」标签、在另一处没有。
func LiveTag(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "现场").FontSize(scale.Font(c, scale.Size11)).TextColor(liveTagText(t)).
		Background(t.Background.Mix(t.Accent, 0.14)).Radius(liveTagRadius).
		Padding(space.Px2, space.Px6).SingleLine()
}

// InvalidLabel 是那枚「已失效」小标签上的字。
//
// ⚠️ **导出的**：界面层那一组 E2E 测试要认得这一枚（Tester 认元素的唯一凭据
// 就是文字和 Label），而标签的画法在这一处——两处各写一遍字的话，改一边
// 另一边对不上。
const InvalidLabel = "已失效"

// invalidTagRadius 是「已失效」那枚小标签的圆角：和「现场」那枚同一档
// （两个都是小胶囊，不跟主题圆角走）。
const invalidTagRadius = 4

// InvalidTag 画那枚「已失效」小标签。
//
// # 为什么它是**次要色**而不是主色
//
// 主色在本项目里是「正在放」那一行（`rowColors`）——用户随时要认的那一件。
// 而失效是「这一行放不了了」：**它是常态**（收藏夹里躺着一个失效条目很常见），
// 不该抢主色。抢了的话用户会以为那一行是「正在放」。
//
// ⚠️ **底比字浅**（不是反过来）：这一枚要和那一行的**灰字**分开——那行字是
// 次要色，这一枚要是「底深字浅」，那两个字就糊在底里读不出来（实测：那
// 样裁剪放大看，标签像一块脏斑而不是一枚标签）。
//
// 所以：底 = 次要色和背景混出来的**淡灰**，字 = **次要色**（比底深）。
//
// invalidTagColors 是那枚标签的**字色和底色**。
//
// 🔴 **它是一个函数**：画法和测试**共用这一份**——测试自己算一遍的话，
// 算的就不是画面上那个颜色了（而「测试算的和画的不是同一个」正是那种
// 一路绿过的错）。
//
// ⚠️ **字用 `ink.Ensure` 压到 4.5:1**（WCAG AA，正文那一档）：那两个字是
// **11px** 的小字，而「次要色配淡灰底」在浅色下只有 **3.95:1**——达不到
// 要求（`TestTheInvalidTagKeepsItsContrast` 钉着这一条）。`Ensure` 按底色
// 的亮度往黑或往白混，达标就原样返回，所以**深色那一档它不动**（本来就够）。
func invalidTagColors(t *ui.Theme) (text, bg ui.Color) {
	bg = t.Background.Mix(t.TextMuted, 0.16)
	text = inkpkg.Ensure(t.TextMuted, bg, inkpkg.TextContrast)
	return text, bg
}

// InvalidTag 画那枚「已失效」小标签。
//
// ⚠️ **底比字浅**（不是反过来）：这一枚要和那一行的**灰字**分开——那行字是
// 次要色，这一枚要是「底深字浅」，那两个字就糊在底里读不出来（实测：那
// 样裁剪放大看，标签像一块脏斑而不是一枚标签）。
//
// ⚠️ **淡灰那一档不是「纯背景」**：纯背景的话标签就没有边界，看着就是一行
// 字里多了两个字，而不是「一枚标签」。
func InvalidTag(c *ui.Context) {
	t := c.Theme()
	text, bg := invalidTagColors(t)
	ui.Text(c, InvalidLabel).FontSize(scale.Font(c, scale.Size11)).
		TextColor(text).Background(bg).Radius(invalidTagRadius).
		Padding(space.Px2, space.Px6).SingleLine()
}

// ownerRow 是「画副标题那一行」要的那些东西。
//
// # ⚠ 「画出来的那一句」和「那个人」是两样
//
// shown.Owner 是**清洗过**的那一句（可能是「歌手 · UP 名」，见 `ShowOf`），
// 而 name 是**那个人**的名字——提示里说的是他（「打开 X 的空间」），不是
// 那一行拼出来的字。
type ownerRow struct {
	// shown 是要画的那一份（清洗过的）。
	shown Shown
	// name 是那个 UP 主的名字（提示里用的那个）。
	name string
	// mid 是那个 UP 主的编号。
	//
	// ⚠ **0 = 这一页没给**：那种情况下 UP 名照旧显示，只是点不动——**把名字
	// 藏起来是另一种撒谎**（用户看到的是「这一首没有 UP 主」，而真实原因是
	// 那一页的接口没给编号；见 `app.Item.ArtistMid`）。
	mid int64
	// tip 是那句提示（**记好的那一份**，见 `rowkey.Memo`）。
	tip string
	// size 是字的大小，lh 是行高，color 是字的颜色（次要色那一档）。
	size, lh float32
	color    ui.Color
	// maxW 是这一行的宽度上限（0 = 不限），grow 是「吃满剩下的宽度」。
	maxW float32
	grow bool
	// acts 是这个列表会做的操作（`OpenOwner` 在里面）。
	acts Actions
	// invalid 是「这一条已经失效」（票 #624）。
	//
	// ⚠️ **它画在 UP 名前面**：它是这一行的**状态**，而 UP 名是**这一首是谁**
	// ——状态在前，读起来是「这一首已经失效 · 某某」。
	//
	// ⚠️ **它自己也会让那一行出现**：失效的条目常常既没有 UP 名也不是
	// 「现场」，而判据在外一层（`card.go`）——那里看的是 `m.Invalid`。
	invalid bool
}

// OwnerTipText 是那句提示：和播放条上那一枚**一个说法**（`internal/ui/playbar.go`
// 的 `viewArtist` 用的就是它）。
//
// ⚠ **说的是那个人，不是画出来的那一句**：画出来的那一句可能是清洗过的
// （「歌手 · UP 名」），而提示要的是「打开 X 的空间」——X 是那个 UP 主。
//
// 🔴 **这句话只有这一处**（票 #360）：列表里的 UP 名和播放条上的 UP 名是同
// 一件事的两处画法，各写一遍的话，改了一处另一处就安静地变成另一句话——而
// 那种错在画面上只是「提示里那句话怪怪的」。
func OwnerTipText(name string) string { return "打开 " + name + " 的空间" }

// ownerLineShown 报副标题那一行**要不要画**。
//
// 🔴 **全仓只有这一处判**（票 #624）：列表那一档和紧凑那一档都调它。
//
// ⚠️ **失效的条目也算「要画」**：「已失效」是那一行**唯一**告诉用户
// 「这一首放不了」的东西，而失效的条目常常既没有 UP 名也不是「现场」——
// 于是原来那个「有 UP 名或者有现场标签才画」的判据会让它什么都不画，
// 用户看到的是一行灰字（灰是灰了，但**没说是为什么**）。
//
// ⚠️ 而两处各判一次的话，改判据就会漏一处——漏掉的那一处症状是
// 「紧凑那一档不标失效」：用户切到紧凑就看不出哪一首坏了。
func ownerLineShown(sh Shown, invalid bool) bool {
	return sh.Owner != "" || sh.Live || invalid
}

// ownerLine 画副标题那一行：现场标签（有就画）+ 副标题文字。
//
// 🔴 **有编号时 UP 名是一枚可点的名字型链接**（票 #360）：点了进那个 UP 的空间。
// **没有编号时照旧画成普通字**——名字照样显示，只是点不动（B 站有些接口只给
// 名字不给编号，见 `app.Item.ArtistMid`）。
func ownerLine(c *ui.Context, o ownerRow) {
	// ⚠️ **缝是 8 不是 6**（票 #624）：「已失效」那枚标签和 UP 名之间，
	// 6 像素（`space.Px6`）太挤——裁剪放大看，标签的圆角几乎贴着 UP 名那个
	// 「U」，看着像连成一片。而「现场」那枚标签和 UP 名之间也是 6，那一枚
	// 是主色淡底（边界清楚），这一枚是淡灰（边界弱），需要的缝更多。
	row := ui.Row(c).Gap(space.Px8).AlignItems(ui.Center)
	if o.maxW > 0 {
		row = row.MaxWidth(o.maxW)
	}
	if o.grow {
		row = row.Grow(1)
	}
	row.Children(func() {
		// 🔴 **失效的标签在最前面**（票 #624）：它是这一行的**状态**，
		// 而 UP 名是**这一首是谁**——状态在前，读起来是「这一首已经失效 ·
		// 某某」。
		if o.invalid {
			InvalidTag(c)
		}
		if o.shown.Live {
			LiveTag(c)
		}
		if o.shown.Owner == "" {
			return
		}
		OwnerName(c, o.shown.Owner, o.name, o.tip, o.mid, o.size, o.lh, o.color, o.acts)
	})
}

// OwnerName 画那一行的 UP 名：有编号时是一枚**可点的名字型链接**（点了走
// `acts.OpenOwner`）；没有时是**普通字**。
//
// # ⚠ 「画出来的那一句」和「那个人」是两样
//
// text 是**画出来**的那一句（可能是清洗过的「歌手 · UP 名」，见 `ShowOf`），
// 而 name 是**那个人**的名字——提示里说的是他（「打开 X 的空间」），不是
// 那一行拼出来的字。
//
// # 🔴 **name 为空时不是可点的**（票 #360 的评审）
//
// 那一行画的有时**只是标题里挪过来的那个歌手名**（「周杰倫」这种），而那不是
// UP 主：那一页压根没有 UP 名（UP 空间「投稿」那一档每一行都是同一个 UP，
// 名字在页头；首页「音乐榜」那一支也可能只给编号不给名字）。
//
// 把它画成可点的，提示就成了「打开 的空间」（名字空着），点它还会跳到
// **编号指的那个人**——而那一页就是那个人的空间：**标签和目的地对不上**。
//
// # ⚠ tip 是记好的那一份
//
// 现算的话每帧每行一次字符串拼接（票 #313 那一类）——见 `rowkey.Memo`。
//
// # ⚠ lh 不给就是「不设行高」
//
// 那一列的字本来就没有行高的时候（首页「新歌速递」那一块的卡片），传 0 ——
// `LineHeight(0)` 是**真的把行高设成 0**，那会把那一行压扁。
func OwnerName(c *ui.Context, text, name, tip string, mid int64,
	size, lh float32, color ui.Color, acts Actions) ui.Element {
	// 没有编号、没接上导航、**或者压根没有那个人**：**普通字**。
	//
	// ⚠ **不许因为「点不动」就把名字藏起来**（AGENTS.md §4）：用户看到的
	// 是「这一行没有 UP 名」，而真实原因是这一页没给编号。
	if name == "" || mid <= 0 || acts.OpenOwner == nil {
		t := ui.Text(c, text).FontSize(size).TextColor(color).MaxLines(1).Ellipsis("…")
		if lh > 0 {
			t = t.LineHeight(lh)
		}
		return t
	}
	// 可点的那一枚：扁平、无描边、字色还是次要色（理由见 `ownerButton`）。
	b := ownerButton(c, text, tip, size, lh, color)
	if b.Clicked() {
		acts.OpenOwner(mid, name)
	}
	return b
}

// ownerButton 画那一枚**可点的** UP 名。
//
// # 它是「名字型链接」，不是一枚按钮（票 #417）
//
// **没有按钮壳、没有底色、没有胶囊**——指针落上去时名字底下长出一道下划线
// （上游那一处是 `hover:underline`）。
//
// 改之前这里画的是扁平按钮（`flat.Button`）：左右各留一段内边距 + 圆角，
// 于是那个名字被一个**胶囊**包着，而指针落上去时胶囊才显出来——像一块底牌。
// 列表里一屏十几行，每一行都顶着那么一块底牌，一眼就是「另一种控件库」。
//
// ⚠️ **照播放条上那一枚画**
//
// `internal/ui/playbar.go` 的 `viewArtist` 是同一件事的另一个地方：有编号时
// 播放条上那个 UP 名也是一枚**名字型链接**。**别为了「可点」就把它画成主色
// 实心块**——上游那一枚是 `text-sm` 的弱化灰字（见 playbar.go 顶上那一段），
// 而列表里每一行都多一块绿的话，一屏就是十几块。
//
// # ⚠ 内边距清零、内容左对齐
//
// 扁平按钮那份配方默认给 6/14（`widgets/flat` 的 `flat.Button`）：留着的话
// 那句话会比同列的字**右移 14 像素**，而这一列的字是从左沿起的（上游那一列是
// `items-start`）。框架的按钮内容默认居中，所以还要 `Justify(ui.Start)`——
// 理由和播放条上那一枚一模一样（见 playbar.go 的 `viewArtist` 顶上）。
//
// 这两样现在由**「名字型链接」那一档的配方**（`widgets/flat` 的 `Link`）给死了。
//
// # ⚠ 宽度上限**不在这里**
//
// 那一列的字有一个上限（`listTitleMaxW`，见 card.go 的注释），而那个上限**加在
// 副标题那一行上**（`ownerLine` 的 `row.MaxWidth`）——按钮是那一行的一个孩子，
// 它跟着那一行收。所以这里**不再给按钮一份上限**：两份上限各写一遍的话，改了
// 一处另一处就安静地变成另一个数，而那种错在画面上只是「这一列的字忽然长了」。
//
// （量过：普通字和按钮这两种画法在列表 / 紧凑 / 网格三档、有没有「现场」标签
// 四种情形下画出来的盒子**一模一样**。）
func ownerButton(c *ui.Context, text, tip string, size, lh float32, color ui.Color) ui.Element {
	// ⚠ **空标签**：那份配方默认给它建一个字，而这里要的是**自己**建那个字
	// （字号 / 行高 / 字色 / 省略号都跟这一列的字一模一样，见 `ownerLine`）。
	b := flat.Link(c, "")
	b = b.Children(func() {
		t := ui.Text(c, text).FontSize(size).TextColor(color).MaxLines(1).Ellipsis("…")
		// ⚠ **行高是 0 就不设**（理由同 `OwnerName` 的那一句）。
		if lh > 0 {
			t = t.LineHeight(lh)

		}
	})
	return b.Tooltip(tip)
}

// liveTagText 是标签字色：强调色压在它自己的 14% 淡底上，浅色下不够 4.5:1 就压深。
//
// ⚠️ **用的是 `ink.TextContrast` 那个数**（不是一个写死的 4.5）：那个数是
// 「正文」那一档的要求（WCAG AA，1.4.3），写死的话改了标准这一处不跟着改。
func liveTagText(t *ui.Theme) ui.Color {
	return inkpkg.Ensure(t.Accent, t.Background.Mix(t.Accent, 0.14), inkpkg.TextContrast)
}

// OwnerLine 画副标题那一行（**导出**给界面层里自己画行 / 画卡片的那几处用）。
//
// ⚠️ **它和列表里那一行是同一套**：现场标签（有就画）+ UP 名（有编号时是一枚**可点
// 的**扁平按钮，点了进那个 UP 的空间，见 `OwnerName`）——两处各画各的的话，同一个
// UP 名在一处点得动、在另一处点不动。
//
// ⚠️ **tip 由调用方给**：列表那边是**记好的那一份**（`rowkey.Memo`，见
// `List.ownerTip`）——现算的话每帧每一行一次字符串拼接。
//
// ⚠️ **maxW / grow 不在这一层**：自己画行 / 画卡片的那几处（首页的新歌速递那一块）
// 那一行不限宽、也不吃满剩下的宽度。
func OwnerLine(c *ui.Context, s Shown, m Music, tip string,
	size, lh float32, color ui.Color, acts Actions) {
	ownerLine(c, ownerRow{
		shown: s,
		name:  m.Owner,
		mid:   m.Item.ArtistMid,
		tip:   tip,
		size:  size,
		lh:    lh,
		color: color,
		acts:  acts,
	})
}
