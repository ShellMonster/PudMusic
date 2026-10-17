// 下载记录页（Biu 的 `/download-list`，`src/pages/download-list/`）。
//
// ## 这一版摆什么
//
// 一张表格，五列：**文件 / 状态 / 大小 / 下载时间 / 操作**（列名和顺序照
// `download-list/index.tsx:140-152` 的 `TableHeader`），外加五种操作里落在页面
// 上的那一个（清空）、一行四到五个按钮，和删除前那道确认弹窗。
//
// 「状态」那一列是**三段**，照 `download-list/progress.tsx` 的 `StageProgress`
// 一段一段数出来的：
//
//	① 进度条      `coreProgress` 那个条（负值 = 长度未知，它自己会走）
//	② 状态文字    出错时这一段换成错误原因（上游 index.tsx:53-60）
//	③ 百分比      右边对齐，和 Biu 的 `{progressValue || 0}%` 同一处
//
// ## 交织中的那一格
//
// 视频那一路下完字节之后还要交织成一个能播的文件（PLAN 8.9 / #32），交织期间
// 记录页上多出「交织中」这一格。
//
// 这一格是**先占好、后接上**的：记录页那一票（#77）把状态位、文案和画法写好
// 了，而交织器（#39）把 `download.StatusMuxing` 真的置上了——于是这一格现在
// 会走到。`TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone` 钉的是画法
// （它说「交织中」，而且**不假装自己下完了**：字节是 100% 而交织没做完，
// 那一格画的是一条不确定长度的条）。
//
// ## 「复用 widgets/list」是什么意思
//
// 能复用的是它那几条约定，一条不少地照着来：
//
//   - 🔴 铁律四（`ListState.Key`）：行身份是 `Task.ID`（`<bvid>|<cid>`），
//     不是序号。`TestDeletingARowAboveKeepsTheTopRowWhereItIs` 钉住它。
//   - 一格画崩了只废掉那一格，下一帧不重试（`guardCell`，同 `widgets/list` 的
//     `guardFailed`）。
//   - 封面走 `widgets/imgcache`，四种状态不在这儿重画。
//   - 会做的操作是一摊函数（`openDir` / `reveal`），测试里换成记动作的桩。
//
// 不能复用的是它那一份 `Music` 卡片：`viewCard` 摆的是「序号 / 封面 / 歌名 /
// UP 名 / 时长」五个格子的一首**歌**，而这里是五列**任务**的表格——文件那一列
// 是封面 + 歌名 + 音质徽章，状态那一列是三段进度，操作那一列是按钮。把任务塞进
// `Music` 只能扭曲它（`Owner` 装错误原因？`Duration` 装百分比？），而那份卡片是
// 首页 / 收藏夹 / 搜索结果在用的。所以这里另写一张表格，约定继承，实现不硬凑。
//
// 🔴 铁律二：`viewDownloadList` 的头一件正事是装主题（见它的注释）。
// 🔴 页面级的 recover 在 `Shell.page` 上（router.go），这里不用再包一层。
package ui

import (
	"fmt"
	"log"
	"runtime/debug"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// downloadListPath 是下载记录页那条路由，和 router.go 里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[5]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——这一页退回空状态页，没人看得出来（和 `homePath` 同一个
// 理由）。
const downloadListPath = "/download-list"

// 五个列的次序。`downloadColumns` 和 `viewCell` 的 switch 都对着这几个数。
const (
	colFile = iota
	colStatus
	colSize
	colTime
	colOps
)

// 列宽。Biu 的 `TableColumn` 给的是 350 / 自适应 / 120 / 120 / 120，这里照那个
// 意思取：文件那一列吃掉剩下的，其余四列定住，免得窗口一拖数字就跳。
//
// # 四个定宽是按**最窄那一档窗口**倒着算出来的，不是按 1200 那档挑的
//
// 这一页最难看的尺寸是窗口自己定的最窄那一档（`WindowOptions.MinWidth: 900`）：
// 减去侧栏（约 200）和页面内边距，表格只剩约 660。而 MyGo 的 `ui.Table` 把没给
// `Width` 的列当「吃剩下的」，四个定宽吃掉多少，文件那一列就只剩多少——
// 定宽合计 560 时它只剩 100，而封面就占掉 48，剩下的歌名**一行放得下两个字**。
// 那一版这里写的注释还说「再宽一点就摆不下封面 + 歌名」，而实测摆不下的是歌名
// 本身：四行普通歌名全被折成两行、尾巴全是「…」。那是错的。
//
// 所以四个数是照着 660 那一条倒着配的，每一列各自的下限写在下面（单元格左右
// 各留 10 的内边距算进去了，MyGo 的 `t.Space(2.5)`）：
//
//	状态     150 = 进度条 ~84 + 缝 8 + 百分比 40 + 内边距 20（少一列就先砍它）
//	大小      76 = 「120.0 MB」在 12px 下约 52 + 内边距 20
//	时间      88 = 「2023-11-15」在 12px 下约 66 + 内边距 20
//	操作     138 = 两颗 56 的按钮 + 缝 6 + 内边距 20（同时最多亮两颗）
//	合计     452 → 900 那一档文件列剩 208，装得下封面 + 歌名 + 徽章
//
// ⚠️ 再窄就不够了：**文件那一列另有一个下限**（`dlFileMin`），而 MyGo 的
// `clampWidth` 只认 `TableColumn.MinWidth`（`mygo/ui/table.go`）。低于那个下限
// 时表格横向滚动（上游那边是 `overflow-x-auto`，同一件事）——滚一下能看见歌名，
// 永远看不见的是操作按钮那半屏。侧栏拖到最宽（`SideMenuWidth` 的上限）时就是
// 这条路；所以取舍是「宁可滚，不把歌名压成两个字」。
const (
	dlStatusW = 150
	dlSizeW   = 76
	dlTimeW   = 88
	dlOpsW    = 138
)

// dlFileMin 是「文件」那一列的下限。
//
// 200 是这么来的：封面 48 + 缝 8 + 单元格内边距 20 = 76，剩下的 124 留给歌名
// ——14px 下八个多汉字，也就是「歌名很长很长很长的一…」这种程度，而不是两个字。
//
// 歌名那一块下面还有一行音质徽章（「无损 FLAC」在 11px 下约 55），124 也装得下。
const dlFileMin = 200

// dlOpW 是操作列里每个按钮的宽度。
//
// ⚠️ **56 是 MyGo 按钮的最小值，别再往下压**：按钮左右各留 `t.Space(3.5)`
// （`mygo/ui/widgets.go` 的 `styleButton`），14px 的两个汉字正好 28，加起来
// 56。再窄一个字都放不下，画出来是「暂…」。
const dlOpW = 56

// dlCover 是封面边长。Biu 那一行是 48（`index.tsx:164` 的 width/height）。
const dlCover = 48

// downloadColumns 是那五列。
//
// ⚠️ **ID 用列名**（`TableColumn.ID` 空着时 `id()` 回落成 Title）：MyGo 的
// Table 按列的身份记状态——用户拖过的列宽、点过的排序都挂在列上（
// `ListState.Columns`）。这里没开 `Sortable`，所以不会有排序状态，可列的身份
// 还是要给稳的：改一次列名等于把用户拖好的列宽扔掉。
var downloadColumns = []ui.TableColumn{
	{Title: "文件", MinWidth: dlFileMin},
	{Title: "状态", Width: dlStatusW},
	{Title: "大小", Width: dlSizeW, Align: ui.Center},
	{Title: "下载时间", Width: dlTimeW, Align: ui.Center},
	{Title: "操作", Width: dlOpsW, Align: ui.Center},
}

// downloadsPage 是下载记录页自己那一摊状态。建一次，每一帧调 view。
//
// 和 widgets/list 的 `List` 同一个道理：**建一次存着**：`ui.ListState` 记着滚动
// 位置，新建的等于每帧跳回顶上（`ui.ListState` 的注释：「Give each List a
// ListState of its own」）。
type downloadsPage struct {
	// state 是那张表格的状态：滚动位置、行身份、无障碍名字。
	state ui.ListState

	// rows 是这一帧要画的任务。**每一帧从应用状态重读一遍**（`sync`），
	// 不缓存：下载进度是后台线程一直在改的，缓存下来的那份一定过期，而这一页
	// 上每一个数都是用户拿来判断「下完了没有」的。
	//
	// ⚠️ 它跟着 `state.Key` / `state.Label` 一起用，所以刷新要发生在建表格
	// **之前**：闭包读的是这个切片。
	rows []download.Snapshot

	// rowsFn 是 `rows` 的来源。`Shell.New` 里接的是 `app.DownloadSnapshots`。
	//
	// 为什么不直接调那一句：「排队中」和「交织中」这两格，各自有它自己的理由要
	// 单测（前者是刚入队还没轮到，后者是字节 100% 而交织没做完），而直接调
	// 应用层的话那一格就跟着真实时序跑了——测试没法稳定地把它停在那个状态上。
	// 所以这里收的是一个函数：生产环境接 `app.DownloadSnapshots`（`Shell.New`），
	// 测试接一份给定的快照（`TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone`）。
	rowsFn func() []download.Snapshot

	// failed 是画崩了的格子（按「行 id + 列号」记），下一帧不重试。理由和
	// `widgets/list` 的 failed 一样：MyGo 的事件循环没有 recover（页级那道在
	// `Shell.page`，兜的是整页），不记的话这一格每帧崩一次，日志被刷爆、
	// 帧率拖垮——比画不出来糟得多。
	failed map[string]bool

	// ask 是「你确定吗」那一弹窗，非 nil 时开着。见 downloadlist_ops.go。
	ask *confirmAsk

	// openDir 是「打开下载目录」，reveal 是「在访达里显示这一首」。
	//
	// ⚠️ 两者都是**接缝**：真实现要去系统里开访达（`mygo.Shell`），而那一条在
	// 无头 Tester 里会 panic（`mygo.App.Run` 没跑过，见 mygo 的 needsApp）。
	// 测试里换成记动作的桩，于是「点『打开文件夹』打开的是不是歌真正落下来的
	// 那个目录」这条能自动验。
	openDir func(path string) error
	reveal  func(path string)
}

// newDownloadsPage 建这一摊状态。
//
// rows 是这一帧要画的那些记录的来源（`Shell.New` 接的是 `app.DownloadSnapshots`）；
// 两个接缝在 `Shell.New` 里接真的系统动作，测试建完外壳换成桩。
func newDownloadsPage(rows func() []download.Snapshot, openDir func(string) error, reveal func(string)) *downloadsPage {
	return &downloadsPage{rowsFn: rows, openDir: openDir, reveal: reveal}
}

// sync 把这一帧要画的行换掉。
func (d *downloadsPage) sync(rows []download.Snapshot) { d.rows = rows }

// viewDownloadList 画下载记录页。
//
// 🔴 铁律二：页面 view 的头一件正事就是装主题，而且必须在最开头——SetTheme 只
// 影响它之后创建的控件。不装的话 MyGo 在帧开头重置好的跟随系统那份就是这一帧
// 真正用的那份，用户选了深色也只在这一帧是对的。
func (s *Shell) viewDownloadList(c *ui.Context, _ *ui.Route) {
	s.applyFrameTheme(c)

	d := s.downloads
	d.sync(d.rowsFn())

	ui.Column(c).Key("page:" + downloadListPath).Grow(1).Padding(20).Gap(12).
		AlignItems(ui.Stretch).Children(func() {
		d.viewToolbar(c, s)

		// 一条记录都没有时画的是**这一页**的空状态（routes 表里
		// `/download-list` 那一条，出处和其余 13 条写在一起）。不用表格自己的
		// 空行：那一行只有一句「暂无内容」，说不了「怎么才能不空」。
		//
		// ⚠️ 这一支是 return，不是少画表格：`ui.Table` 哪怕 n=0 也会画表头，
		// 于是空着的时候页面上还杵着五个列名 —— 比空状态更让人以为这一页坏了。
		if len(d.rows) == 0 {
			s.viewEmpty(c, "downloads:empty", lookup(downloadListPath).Empty)
			return
		}
		d.viewTable(c, s)
	})

	// 弹窗画在这一列**外面**：它盖的是整个窗口，一个 `Grow(1)` 的页面列里
	// 放它的话面板会跟着页面走位。
	d.viewConfirm(c, s)
}

// viewTable 画那张表格。
//
// 🔴 铁律四：这一行是这张表存在的理由。`Key` 每一帧都重装，装的是 `Task.ID`
// 而不是序号——不装的话行的身份是「父 ID + 兄弟序号」，上面少一行、下面所有行
// 的身份就整体错配一格（widgets/list 的 `TestInsertingARowAtTheHead…` 量的是
// 同一件事的另一半：选中项会变成旁边那一首）。
//
// 这一张表上它落成的是**滚动锚点**：用户滚到下面、在上面删掉一条，屏幕上
// 还是那几首歌。`TestDeletingARowAboveKeepsTheTopRowWhereItIs` 钉的就是它。
func (d *downloadsPage) viewTable(c *ui.Context, s *Shell) {
	// Label 是这一行给无障碍和「打字选中」用的名字。
	d.state.Label = func(i int) string {
		if i < 0 || i >= len(d.rows) {
			return ""
		}
		return d.rows[i].Title
	}
	d.state.Key = func(i int) any {
		if i < 0 || i >= len(d.rows) {
			// 不该发生（表格是按 len(d.rows) 建的）。给一个类型撞不上的值，
			// 比返回下标安全：返回下标会让它看起来像「行 0」。
			return dlRowKey{}
		}
		return d.rows[i].ID
	}

	ui.Table(c, &d.state, downloadColumns, len(d.rows), func(row, col int) {
		d.viewCell(c, s, row, col)
	}).Grow(1)
}

// dlRowKey 是「下标越界」时给的行身份。一个歌不可能撞上的类型。
type dlRowKey struct{}

// viewCell 画一格，外包一层 panic 兜底。
//
// 页级那道 Guard（`Shell.page`）兜的是**整页**：一格画崩了，用户看到的是整个
// 下载记录页变成一张错误页，而出错的地方在一格进度条上。这一层只废掉那一格。
func (d *downloadsPage) viewCell(c *ui.Context, s *Shell, row, col int) {
	id := fmt.Sprintf("%s#%d", d.rows[row].ID, col)
	if d.failed[id] {
		ui.Text(c, "画不出来").FontSize(11).TextColor(c.Theme().TextMuted)
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			if d.failed == nil {
				d.failed = map[string]bool{}
			}
			d.failed[id] = true
			log.Printf("biu: 下载记录的一格画崩了（%s）：%v\n%s", id, rec, debug.Stack())
		}
	}()
	d.drawCell(c, s, row, col)
}

// drawCell 是真的画一格。
func (d *downloadsPage) drawCell(c *ui.Context, s *Shell, row, col int) {
	t := c.Theme()
	r := d.rows[row]
	switch col {
	case colFile:
		ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
			// 四态（没有地址 / 载入中 / 就绪 / 失败）由 imgcache.View 画，
			// 这里不重复它的判断。
			imgcache.View(c, s.images, r.Cover, dlCover, dlCover)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, r.Title).FontSize(14).MaxLines(2).Ellipsis("…")
				// 音质徽章，照上游 `index.tsx:179-183` 的 `Chip`。空串不画。
				if r.Label != "" {
					ui.Text(c, r.Label).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
				}
			})
		})
	case colStatus:
		d.viewStatus(c, r)
	case colSize:
		// 上游是 `item.totalBytes ? filesize(item.totalBytes) : "-"`，只写
		// 总大小（`index.tsx:190`）。已下了多少在状态那一列的进度条上。
		ui.Text(c, fileSize(r.Total)).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
	case colTime:
		// `formatMillisecond` 是 `moment(s).format("YYYY-MM-DD")`
		// （biu/src/common/utils/time.ts:20），没有时刻也没有「几天前」。
		ui.Text(c, addedOn(r.AddedAt)).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
	case colOps:
		d.viewOps(c, s, r)
	}
}
