// 「状态」那一列：三段进度，和这一列上每一种状态写什么字、涂什么色。
//
// 文案**逐句照上游** `biu/src/pages/download-list/status-desc.ts` 的 `StatusDesc`，
// 出处标在每一行上面。两处上游自己就不一样，抄的时候按**这一列真正画的那一句**
// 来：
//
//	`status-desc.ts:8` completed 是「已完成」，而 `progress.tsx:17-25` 的完成那一支
//	画的是一枚对勾 +「下载完成」。这一列是 `StageProgress`，所以是「下载完成」。
//
// 「交织中」是这一版自己加的（上游那边是 `merging`「下载完成，合并分块中」），
// 理由写在 `downloadlist.go` 的文件头。
//
// # 这一列的两处颜色（票 #176 / G46）
//
//	进度条   蓝（`#3b82f6`，上游 `progress.tsx:49` 的 `bg-blue-500`），
//	         出错的那一条是红（上游同一个 `indicator` 的另一支）。
//	完成     绿勾 +「下载完成」，两样都是成功色（上游 `progress.tsx:17-25`）。
//
// 蓝**不是**主色：上游那一格是蓝的，用户把主色换成红的它也还是蓝的。

package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// statusCell 是「状态」那一格要画的三样。
//
// 三样分开存是因为它们**不是同一条规则推出来的**：条画不画由状态决定（上游的
// waiting 和 completed 两支就不画条），百分比画不画由「长度探到了没有」决定
// （`Snapshot.Percent` 是 -1 时是「还没探到」，不是 0%），而文字是状态自己的
// 那一句。合成一个字符串的话「排队中」会被画成「排队中 0%」——而那一刻根本
// 不知道会有多少。
type statusCell struct {
	// text 是状态文字。出错时这一句是错误原因（上游 index.tsx:53-60）。
	text string
	// c 是 text 的颜色。
	c ui.Color
	// bar 是进度条的值。0~1 是确定的进度；**负数是「长度未知」**（见
	// `coreProgress`），条会自己走。
	bar float64
	// showBar 那一段画不画。showPct 是第三段画不画。
	showBar, showPct bool
	// danger 让进度条变红（上游 `progress.tsx:49` 的 `indicator: failed ?
	// "bg-danger"`）。
	danger bool
	// done 让这一格前面多一枚对勾（上游 `progress.tsx:17-25` 的
	// `<RiCheckboxCircleLine size={16} />`）。
	done bool
}

// statusOf 把一条下载记录翻译成那一格要画的东西。
//
// 纯函数：状态 → 画法。界面上的每一个数都能在这一处对着查，而不用点开一张
// 表看（`TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone` 就是直接调它）。
func statusOf(r download.Snapshot, t *ui.Theme) statusCell {
	// `Snapshot.Percent` 已经是 0~100 的整数，没探到长度时是 -1。÷100 之后
	// 负数仍然是负数，于是「还没探到」自动落进 MyGo 的「长度未知」那一支，
	// 不必在这里分一次情况。
	fill := float64(r.Percent) / 100

	switch r.Status {
	case download.StatusQueued:
		// 上游 waiting 那一支只画一句「等待下载...」，**不画进度条**
		// （progress.tsx:12-14）。照它：排着队的时候还没探到长度，画一条 0%
		// 的条只会让人以为卡住了。
		return statusCell{text: "等待中", c: t.TextMuted}

	case download.StatusRunning:
		// 下载中（`status-desc.ts:4` 的 downloading）。
		return statusCell{text: "下载中", c: t.TextMuted, bar: fill, showBar: true, showPct: r.Percent >= 0}

	case download.StatusPaused:
		// 下载暂停（`status-desc.ts:5` 的 downloadPaused）。条停在它停下的
		// 那个数上——用户要知道回来之后还差多少。
		return statusCell{text: "下载暂停", c: t.TextMuted, bar: fill, showBar: true, showPct: r.Percent >= 0}

	case download.StatusDone:
		// 上游画的是一枚对勾 +「下载完成」，**不画进度条**
		// （progress.tsx:17-25）。对勾是 `RiCheckboxCircleLine`——我们图标集里
		// 有同一张（`System/checkbox-circle-line.svg`），所以照画。
		//
		// ⚠️ **那几个字取成功色「当字色」的那一档**（票 #241 / 报告 F-22）：
		// 浅色下上游那个成功绿 `#17c964` 压在白底上只有 **2.19:1**，读不出来。
		// 色相不变，只压深到 ≥4.5:1（`successText` 的那一层，理由见
		// `internal/ui/semantic.go`）。
		//
		// ⚠️ **那枚绿勾不动**：它是**图形**，不受 4.5:1 约束（3:1 即可），而改它
		// 就是改上游那一枚图标的颜色（`TestTheFinishedCellHasAGreenCheck` 钉的
		// 就是 `#17c964`）。
		return statusCell{text: "下载完成", c: successText(t), done: true}

	case download.StatusMuxing:
		// 交织中：字节已经全下完了，这一格说的是**交织**而不是字节，而交织
		// 的进度这一版报不出来（#32 才有）。所以画一条长度未知的条
		// （coreProgress 的负值那一支，它自己会走）。
		//
		// ⚠️ 千万别改成 100%：那一格要是画成 100%，用户看到的是「下完了」，
		// 然后盯着一个永远不变成「下载完成」的「下载完成」等下去。
		return statusCell{text: "交织中", c: t.TextMuted, bar: -1, showBar: true}

	default: // download.StatusError，以及将来还不认识的状态
		// 上游出错时**不写「任务出错」**（`status-desc.ts:10` 那一句用在这一列
		// 之外的别处），而是把错误原因整句摆在这儿，两行、危险色
		// （progress.tsx:53-60）。原因空着才退回那句标签——那说明状态是从
		// 队列文件里读回来的一条坏记录。
		text := r.Err
		if text == "" {
			text = "任务出错"
		}
		// ⚠️ **错误原因当字色用，取危险色「当字色」的那一档**（票 #241 / 报告
		// F-22）：浅色下上游那个 `#f31260` 压在白底上是 4.14:1，不到正文那一档。
		// 色相不变，只压深（理由见 `internal/ui/semantic.go`）。
		return statusCell{text: text, c: dangerText(t), bar: fill, showBar: true,
			showPct: r.Percent >= 0, danger: true}
	}
}

// viewStatus 画「状态」那一格：三段。
//
// 上游 `progress.tsx:38-65` 是一个 `flex flex-col` 的两层：上面进度条，下面一
// 个 `justify-between` 的行，左边状态文字、右边百分比。这里的结构照它，所以
// 「三段」在一列里是竖着摞的，不是横着排的。
func (d *downloadsPage) viewStatus(c *ui.Context, r download.Snapshot) {
	cell := statusOf(r, c.Theme())

	// 上游的 waiting / completed 两支没有条也没有百分比：一个只有一句话的
	// 居中块。这里让它**顶格靠左**：一列里所有格子左对齐，「等待中」和
	// 「下载完成」才在同一处，读起来是一列而不是两列。
	//
	// ⚠️ **「完成」那一支前面还有一枚对勾**（上游 `progress.tsx:17-25`）：
	// 一枚绿勾 +「下载完成」，两样都是成功色。
	//
	// ⚠️ **那枚对勾用主题里那一个成功色，那几个字用「当字色」的那一档**
	// （票 #241 / 报告 F-22）：图标是**图形**（3:1 即可），字是**正文**（4.5:1），
	// 而上游那个 `#17c964` 当字色只有 2.19:1。理由见 `internal/ui/semantic.go`。
	if cell.done {
		ui.Row(c).Gap(dlDoneGap).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconChecked)).FontSize(dlDoneIconSize).TextColor(c.Theme().Success)
			ui.Text(c, cell.text).FontSize(scale.Font(c, scale.Size13)).TextColor(cell.c).MaxLines(2)
		})
		return
	}
	if !cell.showBar {
		ui.Text(c, cell.text).FontSize(scale.Font(c, scale.Size13)).TextColor(cell.c).MaxLines(2)
		return
	}

	// ⚠️ **出错那一支是另一套排法**（票 #241 / 报告 F-19）：错误原因**独占一行、
	// 整宽**，而百分比上到条那一行去。
	//
	// 原来错误原因和百分比**并排**（「这一条流有多…」那一截就是这么没的）：状态列
	// 150 宽，扣掉单元格左右各 10 的内边距是 130；再扣掉百分比那一档 40 和中间
	// 那道 8 的缝，错误原因只剩 **82**——12px 下每行六个字，「服务器不肯说这一条
	// 流有多长」这 13 个字折两行还多两个字，于是画面上只剩「这一条流有多…」。
	// **截掉的是信息本身**，而这一句是用户唯一能知道「哪儿坏了」的地方。
	//
	// 现在这一句拿整宽（130）：12px 下每行十个字，13 个字折成两行（十 + 三），
	// 整句都在。
	if cell.danger {
		ui.Column(c).FillWidth().Gap(space.Px4).Children(func() {
			ui.Row(c).FillWidth().Gap(space.Px8).AlignItems(ui.Center).Children(func() {
				// 条在这一行里**让出百分比那一档**（`Grow(1)` 而不是 `FillWidth`）：
				// 两样挤在一行，条就短一截，而短的那一截是「下载到哪儿了」。
				statusBar(c, cell.bar, c.Theme().Danger).Grow(1)
				if cell.showPct {
					dlPercent(c, r)
				}
			})
			// 三行是留给更长的原因的兜底：这一句**通常**两行就够（13 个字），
			// 而省略号只在它真的超过三行时才出现。
			ui.Text(c, cell.text).FillWidth().FontSize(scale.Font(c, scale.Size12)).TextColor(cell.c).
				MaxLines(3).Ellipsis("…")
		})
		return
	}

	ui.Column(c).FillWidth().Gap(space.Px4).Children(func() {
		statusBar(c, cell.bar, ui.Hex(dlBarColor)).FillWidth()
		ui.Row(c).FillWidth().Gap(space.Px8).Children(func() {
			// ⚠️ **12 和省略号都照原样**（这一支是「下载中 / 下载暂停 / 交织中」
			// 那几句短的，省略号实际上不出现）——这一票只动**出错**那一支的排法，
			// 不顺手改这里的字号。
			ui.Text(c, cell.text).Grow(1).FontSize(scale.Font(c, scale.Size12)).TextColor(cell.c).
				MaxLines(2).Ellipsis("…")
			if cell.showPct {
				dlPercent(c, r)
			}
		})
	})
}

// dlPercent 是状态列里那个百分比。
//
// 定宽 + 靠右：Biu 用的是 tabular-nums 那一列，不定宽的话 9% 和 100% 两帧之间
// 整个右边会跳一下。
func dlPercent(c *ui.Context, r download.Snapshot) ui.Element {
	return ui.Text(c, strconv.Itoa(r.Percent)+"%").Width(dlPctW).
		TextAlign(ui.End).FontSize(scale.Font(c, scale.Size12)).TextColor(c.Theme().TextMuted).
		MaxLines(1)
}

// dlPctW 是百分比那一段的宽度。
//
// 40 是按「100%」在 12px 下约 26 加一点余量挑的，而它是从**状态列**里扣的
// （进度条和它共用那一行，见 `downloadlist.go` 里 `dlStatusW` 那段算式）——
// 这里多给一个像素，状态列的条就少一个。
const dlPctW = 40

// 「完成」那一支的尺寸。
//
// 上游 `progress.tsx:17-25`：`<RiCheckboxCircleLine size={16} />` 加上
// `space-x-1`（4px）的缝。
const (
	dlDoneIconSize = 16
	dlDoneGap      = space.Px4
)

// dlBarColor 是进度条涂的那个颜色。
//
// 上游 `download-list/progress.tsx:49` 的 `indicator` 是
// `data.status === "failed" ? "bg-danger" : "bg-blue-500"`——
// **蓝的，不是主色**。而 Tailwind 的 `blue-500` 是 `#3b82f6`。
//
// ⚠️ **它不是主题里的一个语义色**：DESIGN.md §四 那一组语义色是
// 危险 / 警告 / 成功，主色是 Biu 的绿。这一条是上游那一枚进度条自己的颜色，
// 所以**不跟用户挑的主色走**——用户把主色换成红的，进度条还是蓝的（那正是
// 上游的样子）。
//
// ⚠️ 深浅两套是**同一个值**：这一条是「这一格是蓝的」而不是「这一格比底色
// 亮一点」，深色下它照样是 `#3b82f6`。
const dlBarColor = "#3b82f6"

// statusBar 画一条进度条，填充色是给定的那一个。
//
// ⚠️ MyGo 的 `Progress` 把填充色**写死**成建它那一刻主题的 Accent
// （mygo/ui/widgets.go 的 `coreProgress` 里 `p.Fill(…, t.Accent, …)`），元素
// 上没有换色的方法。所以「这一条要涂蓝 / 涂红」只能在这里临时把主题的 Accent
// 换成那个颜色建这一条控件，**建完立刻换回来**。
//
// 后半句是这一段能安全存在的前提：`SetTheme` 只影响**它之后创建**的控件
// （铁律二，见 theme.go），所以这一条之后的任何东西都还是原来的颜色。忘了
// 换回来，那一帧剩下的整个页面就都是蓝的了——而那种错不报错、不崩溃。
//
// ⚠️ **宽度由调用方给**（`FillWidth()` / `Grow(1)`）：出错那一支的条要和百分比
// 并排（`Grow(1)` 让出那一档），别的分支的条是整宽的——所以这里不写死。
func statusBar(c *ui.Context, value float64, color ui.Color) ui.Element {
	orig := c.Theme()
	themed := *orig
	themed.Accent = color
	c.SetTheme(&themed)
	e := ui.Progress(c, value)
	c.SetTheme(orig)
	return e
}

// fileSize 把字节数写成大小。0（还没探到长度）是上游那个 `-`。
//
// ⚠️ 上游这一列是 `filesize(item.totalBytes)`（index.tsx:190），走的是 npm 的
// `filesize` 包。这里自己写一份：那是个 JS 依赖，而我们这 0 和 1 都用不到它，
// 而一个只有一行、纯粹为了格式化一个整数的外部依赖不值得为它引一整条工具链。
// 算法是 1024 进制（和 `widgets/list` 里其它列写时长/写序号的取舍一致）。
func fileSize(n int64) string {
	if n <= 0 {
		return "-"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	// 整数那一档不写小数：「1 KB」比「1.0 KB」干净，而「1023 B」不能写成
	// 「1023.0 B」。
	if i == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + units[i]
}

// addedOn 把入队时刻写成日期。0 是上游那个 `-`。
//
// `YYYY-MM-DD`，照 `formatMillisecond`
// （biu/src/common/utils/time.ts:20 的 `moment(s).format("YYYY-MM-DD")`）：上游
// 那一列没有时刻，也没有「几天前」，所以这里不加。
//
// ⚠️ 用**本机时区**：那正是上游 `moment(s)` 的默认（moment 不给时区就用本地）。
// 入队时刻记的是 Unix 毫秒（`Task.AddedAt`），换算回来必须还是用户自己那一天。
func addedOn(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format("2006-01-02")
}
