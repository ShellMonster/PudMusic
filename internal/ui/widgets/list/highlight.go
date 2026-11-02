package list

import (
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// SetHighlight 设歌名里要标出的搜索词，传空串去掉。只有搜索结果用它。
func (l *List) SetHighlight(q string) { l.hl = strings.TrimSpace(q) }

// titleText 画歌名；hl 非空且在歌名里出现时，把命中的那几段染成主色并加粗，
// 其余照旧。大小写不敏感，只标第一处（歌名一行放不下更多）。没命中就是普通的 ui.Text。
func titleText(c *ui.Context, text, hl string, color ui.Color) ui.Element {
	if hl == "" {
		return ui.Text(c, text).TextColor(color)
	}
	i := indexFold(text, hl)
	if i < 0 {
		return ui.Text(c, text).TextColor(color)
	}
	j := i + len(hl)
	mark := ink.AccentOn(c.Theme())
	return ui.RichText(c,
		ui.Span{Text: text[:i], Color: color},
		ui.Span{Text: text[i:j], Color: mark, Weight: 700},
		ui.Span{Text: text[j:], Color: color},
	)
}

// indexFold 是大小写不敏感的 strings.Index，返回的是 s 里的字节下标。
// 只有大小写折叠后字节数不变的文本才能直接用下标切（ASCII 和中文都满足）；
// 遇到会变长度的字符就当没命中，不去冒切坏 UTF-8 的险。
func indexFold(s, sub string) int {
	ls, lsub := strings.ToLower(s), strings.ToLower(sub)
	if len(ls) != len(s) || len(lsub) != len(sub) {
		return -1
	}
	return strings.Index(ls, lsub)
}

// progressBarW 是那条进度条的宽。它是**定死的**：这一条画在歌名下面那一列的
// 最左边，宽度跟着内容走的话每一行都不一样（而它比歌名短一大截，跳起来更难看）。
const progressBarW = 120

// progressLabel 是这条进度条给无障碍（和 Tester）用的名字。
//
// ⚠️ **带上歌名**：读屏软件读的是名字而不是画出来的那几个像素，光说「播放进度」的
// 话，用户在一屏十几行里听到的是十几次同一句话——分不出是哪一首。写法照
// `operation.go` 的 `OperationLabel`（那一枚「⋯」也是这么起的）。
func progressLabel(title string) string { return "「" + title + "」的播放进度" }

// progressBar 在标题下面画「看到哪儿」：框架的 `ui.Progress`（灰轨 + 主色填充）。
// 没有进度或时长时一个元素都不建。
//
// # 为什么换成框架那一版（票 #703 的 C 项）
//
// 手搓的那一版（两个 `ui.Box`）**对辅助技术根本不存在**：它既没有角色，也没有
// 值，读屏软件读不出「这一首听到哪儿了」。框架那一版带 `RoleProgress` 和
// `Range(0, 1, value)`（`mygo/ui/widgets.go` 的 `coreProgress`），于是那一行
// 在无障碍树里是一个**值多少的进度条**。
//
// ⚠️ **这一版比手搓的那一版粗**：高度是 `t.Space(1.5)`（= 6，默认间距 4）、
// 轨道色是 `t.Border`（分隔线那一档）、圆角是 `t.Space(0.75)`（= 3）——而手搓的
// 是 3 高 + `t.Text.Alpha(0.1)` 的淡轨 + 1.5 的圆角。**这是故意的**：票面写明
// 「这一项要按视觉变更处理」。全应用另外两处进度条（更新弹窗、下载列表）早就是
// 框架那一版了，现在这三处是同一套外观。
//
// # 视觉审查量到的那几个数（票 #703 的 C 项）
//
// 尺子是**上游 Biu 的对应那一枚**：上游列表那一行**没有进度条**（`history/item.tsx`
// 把进度写成「0:30 / 1:00」那一句文字），所以**上游没有可抄的图**——比的是
// 上游**唯一一处同类的进度条**：`download-list/progress.tsx` 的 HeroUI
// `<Progress size="sm" radius="md">`。那一枚的轨道是 `h-1`（**4 像素**）、
// 圆角 `rounded-medium`；我们这一枚是框架的 `ui.Progress`（**6 像素**、
// 圆角 3）——**比上游粗 2 像素**，因为框架那一版的尺寸是它自己的
// （`t.Space(1.5)`），不是上游的 Tailwind 刻度。
//
// 量出来的差（浅色 / 深色两套量出来的一样，因为尺寸与主题无关）：
//
//	高度     3 → 6（上游 HeroUI `size="sm"` 是 4）
//	圆角     1.5 → 3（上游 `radius="md"` 约 6，HeroUI 的 medium = 0.5rem）
//	轨道色   t.Text.Alpha(0.1) → t.Border
//	          浅色 #e7e7e8 → #d9d9de（深色 #2f2f31 → #3f3f46）
//	填充色   t.Accent → t.Accent（**一样**）
//	宽度     120 → 120（**一样**，我们自己给的）
//
// ⚠️ **轨道色换了一档，视觉上更实**：浅色下 `t.Border`（#d9d9de）比
// `t.Text.Alpha(0.1)`（#e7e7e8）深一档——而这一条是**没填的那一段**，它在白底上
// 从「几乎看不见」变成「看得见的一道灰」。这是框架那一版的样子（全应用另外两处
// 进度条早就是它），所以是**故意**的，不是画歪了。
//
// ⚠️ **高度 6 吃掉了行底那 4 像素**：这一行是**定死 64 高**的（`card.go` 的
// `listRowHeight`），而这一列是「歌名 + UP 名 + 进度条」三段。改之前那一列的
// 墨迹落在 y=11..26 / 37..48 / 55..57，改之后落在 y=9..24 / 36..47 / 53..58
// ——**整列往上挪了 2 像素**（列是居中的，三段合起来更高就往上长），而**行高
// 64 没变**（`ui.List` 量出来还是 64，第二行还是从 y=64 开始）。所以**没有溢出**，
// 也没有挤掉下一行。
//
// ⚠️ **和上游的间距差在哪**：上游那一行是「封面 48 + `p-2`（上下各 8）」= 64，
// 歌名和 UP 名之间是 `space-y-1`（**4**）；我们是 `textGap = 4`——**一样**。
// 这一条不是上游有、我们没有的东西（上游那一行压根没有进度条），所以
// **这一项与上游的关系是「上游没有这一枚」**，比的是同类的下载进度条。
//
// ⚠️ **宽度要自己给**：框架那一版**只给高度**（`coreProgress` 里没有宽度），
// 不给的话它按内容算宽——而它没有内容，宽就是 0，**整条看不见**。
//
// ⚠️ **名字要自己给**：框架的进度条**没有文字**，读屏软件报出来的名字是空的
// （`mygo/ui/access.go` 的 `accessDetails`：名字要么来自 `Label`，要么来自里面
// 的文字）。所以这里给它一个 `Label`。
//
// ⚠️ **填充色不用临时换主题**：框架把填充色**写死**成建它那一刻主题的 `Accent`
// （`p.Fill(…, t.Accent, …)`），而我们要的就是主题的主色——所以直接用框架那一版
// 就行。（要换颜色才需要那套「临时换 `Theme.Accent`、建完换回来」的走法，理由见
// `internal/ui/downloadlist_status.go` 的 `statusBar`。）
func progressBar(c *ui.Context, m Music) {
	if m.Progress <= 0 || m.Duration <= 0 {
		return
	}
	f := float64(m.Progress) / float64(m.Duration)
	if f > 1 {
		f = 1
	}
	ui.Progress(c.Key("progress:"+m.ID), f).Width(progressBarW).
		Label(progressLabel(m.Title))
}
