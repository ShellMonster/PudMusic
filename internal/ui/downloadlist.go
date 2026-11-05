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
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// downloadListPath 是下载记录页那条路由，和 router.go 里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[5]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——这一页退回空状态页，没人看得出来（和 `homePath` 同一个
// 理由）。
const downloadListPath = "/download-list"

// downloadsPage 是下载记录页自己那一摊状态。建一次，每一帧调 view。
//
// 和 widgets/list 的 `List` 同一个道理：**建一次存着**：`ui.ListState` 记着滚动
// 位置，新建的等于每帧跳回顶上（`ui.ListState` 的注释：「Give each List a
// ListState of its own」）。
type downloadsPage struct {
	// scroll 是整页那个滚动区的状态（回顶按钮读它的 Y）。
	scroll ui.ScrollState

	// filter 是那一排「全部 / 下载中 / 已完成 / 失败」现在选的是哪一档。见 downloadFilter。
	//
	// ⚠️ 它长在这个对象上，而 `Shell.downloads` 是建一次、跨路由不换的
	// （`Shell.New`）——所以「切走再切回来，选中的还是原来那一档」是白捡的，
	// 而验收项点名要它。别把它挪进 view 的局部变量：即时模式每一帧都重画，
	// 局部变量下一帧就没了（和 `confirmAsk` 同一个理由）。
	filter downloadFilter

	// all 是这一帧队列里的**全部**记录（`rowsFn` 给的那一份），`rows` 是它
	// 筛完之后的那一份，画表格用的是 `rows`。
	//
	// ⚠️ 两个都留着，是因为有一处必须按**全部**来算：「清空记录」清的是整个
	// 队列（`App.ClearDownloads`），它那颗按钮灰不灰、确认弹窗里报几条，用
	// 的是 `len(all)` 而不是 `len(rows)`。按筛完的那份报的话，用户在「视频」
	// 那一档看到「确认清空这 1 条」，点下去删掉的是 21 条——那一句话是在骗
	// 用户，而它骗掉的正是不可撤销的那部分（还在下的半成品会被删掉）。
	//
	// 进度那边要每帧重读（见下面 `rows` 的注释），而筛选是纯内存的一遍循环，
	// 它的开销和页面读一次队列快照没法比。
	//
	// ⚠️ 「全部」那一档上这两个**是同一份切片**（`filterDownloads` 原样返回）。
	// 两边都不改，所以眼下没有事；而真要有一天在 `rows` 上动手脚，记得那是
	// `rowsFn()` 下一帧就要换掉的一份拷贝，不是跨帧活着的状态。
	all  []download.Snapshot
	rows []download.Snapshot

	// rowsFn 是 `rows` 的来源。`Shell.New` 里接的是 `app.DownloadSnapshots`。
	//
	// 为什么不直接调那一句：「排队中」和「交织中」这两格，各自有它自己的理由要
	// 单测（前者是刚入队还没轮到，后者是字节 100% 而交织没做完），而直接调
	// 应用层的话那一格就跟着真实时序跑了——测试没法稳定地把它停在那个状态上。
	// 所以这里收的是一个函数：生产环境接 `app.DownloadSnapshots`（`Shell.New`），
	// 测试接一份给定的快照（`TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone`）。
	rowsFn func() []download.Snapshot

	// ask 是「你确定吗」那一弹窗，非 nil 时开着。见 downloadlist_ops.go。
	ask *confirmAsk

	// openDir 是「打开下载目录」，reveal 是「在访达里显示这一首」，openLink
	// 是「在浏览器里打开这一首的 B 站页面」（文件那一列 hover 出来的外链图标）。
	//
	// ⚠️ 三者都是**接缝**：真实现要去系统里开访达 / 浏览器（`mygo.Shell`），而
	// 那一条在无头 Tester 里会 panic（`mygo.App.Run` 没跑过，见 mygo 的
	// needsApp）。测试里换成记动作的桩，于是「点『打开文件夹』打开的是不是歌
	// 真正落下来的那个目录」这条能自动验。
	openDir  func(path string) error
	reveal   func(path string)
	openLink func(url string) error

	// tip 是带延迟的悬停提示（tooltip.go），由 Shell 在建这一页时接上。
	// 为零时（单独造这一页的测试）退回框架那枚。
	tip tipFunc
}

// newDownloadsPage 建这一摊状态。
//
// rows 是这一帧要画的那些记录的来源（`Shell.New` 接的是 `app.DownloadSnapshots`）；
// 三个接缝（打开目录 / 在访达里显示 / 打开 B 站页面）在 `Shell.New` 里接真的
// 系统动作，测试建完外壳换成桩。
func newDownloadsPage(rows func() []download.Snapshot, openDir func(string) error,
	reveal func(string), openLink func(string) error) *downloadsPage {
	return &downloadsPage{rowsFn: rows, openDir: openDir, reveal: reveal, openLink: openLink}
}

// sync 把这一帧要画的行换掉（顺便筛一遍）。
func (d *downloadsPage) sync(all []download.Snapshot) {
	d.all = all
	d.rows = filterDownloads(all, d.filter)
}

// filterDownloads 按 f 筛出这一帧要画的那些行。
//
// ⚠️ 它**什么都不碰**：不发请求、不动队列、不写设置。数据早就在内存里了
// （`app.DownloadSnapshots` 就是队列快照），所以这一档换一次就是切一个切片。
// 判据是状态（`downloadFilter.matches`）。
func filterDownloads(all []download.Snapshot, f downloadFilter) []download.Snapshot {
	if f == dlFilterAll || f < 0 || int(f) >= dlFilterCount {
		return all
	}
	out := make([]download.Snapshot, 0, len(all))
	for _, r := range all {
		if f.matches(r) {
			out = append(out, r)
		}
	}
	return out
}

// viewDownloadList 画下载记录页（原型 `PAGES.downloads`）。
//
// 🔴 铁律二：页面 view 的头一件正事就是装主题，而且必须在最开头——SetTheme 只影响它之后
// 创建的控件。
func (s *Shell) viewDownloadList(c *ui.Context, _ *ui.Route) {
	s.applyFrameTheme(c)
	d := s.downloads

	dir := s.app.DownloadDir()
	var content func()
	s.pudPage(c, "page:"+downloadListPath, func(winW, listW float32) {
		scroll := ui.Scroll(c).Key("downloads:scroll").Grow(1).TrackScroll(&d.scroll)
		d.trackScrollbar(s, scroll)
		scroll.Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Stretch).Padding(space.None, space.None, space.PageBottom, space.None).
				Children(func() {
					p := tok.Of(c.Theme())
					pud.PageHeadFn(c, "Downloads", "下载记录", winW, func() {
						// 导语：「保存到 <路径>」，路径是一枚等宽链接，点了去设置里改。
						ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).
							Margin(space.Px8, space.None, space.None, space.None).Children(func() {
							if dir == "" {
								if d.tipWith(c, flatLink(c, "还没有下载目录"), "去设置里选一个下载目录").Clicked() {
									s.goToDownloadDirSetting()
								}
								return
							}
							ui.Text(c, "保存到").FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2)
							link := ui.ButtonBase(c).Key("downloads:dir").Label("保存到 " + dir)
							fg := p.Text2
							if link.Hovered() {
								fg = p.Text
							}
							link.Children(func() {
								typo.Mono(ui.Text(c, dir).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Copy)).TextColor(fg).
									SingleLine().Ellipsis("…")
							})
							if link.Clicked() {
								s.goToDownloadDirSetting()
							}
						})
					}, func() {
						if len(d.all) == 0 {
							return
						}
						if dir != "" && pud.Button(c, "downloads:open-dir", pud.Ghost, pud.Std, "打开文件夹", "folder").Clicked() {
							d.openDownloadDir(dir, s)
						}
						if pud.Button(c, "downloads:clear", pud.Quiet, pud.Std, "清空全部", "trash").Clicked() {
							d.askClear(s)
						}
					})

					d.viewFilter(c)
					ui.Box(c).Height(space.Px16)
					if len(d.rows) == 0 {
						pudEmptyState(c, s.downloadsEmptyState(d))
						return
					}
					for _, r := range d.rows {
						d.viewRow(c, s, r, listW, winW)
					}
				})
		})
	})
	_ = content

	// 弹窗画在页面外面：它盖的是整个窗口，一个 `Grow(1)` 的页面列里放它的话面板会跟着页面走位。
	d.viewConfirm(c, s)

	// 「回到顶部」那一枚。滚过 400 像素才出现（上游的数，见 `widgets/list` 的 `BackToTop`）。
	list.BackToTop(c, d.scroll.Y, d.scrollTop)
}

// pudEmptyState 把一个 `emptyState`（标题 / 说明 / 图标 / 可选动作）画成原型的空状态。
func pudEmptyState(c *ui.Context, e emptyState) { emptyContent(c, e) }

// syncDownloads 是下载记录这一页每帧的副作用：取一次队列快照、按当前那一档筛出
// 这一帧要画的那些行。
//
// ⚠️ **它原来在 `viewDownloadList` 的最开头**，而这一页在满屏开着的时候不画
// （票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，下载队列的变化
// 一直看不见（而下载是**后台**在跑的事，满屏期间照样在下载）。
func (s *Shell) syncDownloads(c *ui.Context) {
	d := s.downloads
	d.sync(d.rowsFn())
}

// scrollTop 回到顶上。
func (d *downloadsPage) scrollTop() { d.scroll.Y = 0 }

// trackScrollbar 把滚动条的悬停转告给壳（主题下一帧据此换深一档）。
func (d *downloadsPage) trackScrollbar(s *Shell, e ui.Element) { s.trackScrollbar(e) }

// downloadsEmptyState 是这一页空着的时候说的那两句话。
//
// **两句话必须分得开**：真的没下载是「怎么才能不空」（`noDownloads`），筛完了
// 没有是「你在看哪一档、换一档看看」（`dlEmptyAfterFilter`）。混成一句的话，用户
// 在「视频」那一档看到「暂无内容」，会以为自己一首歌都没下过——而盘上明明躺着
// 一整列表的音频。验收项点名要的就是这个分别。
//
// ⚠️ **还有第三种**：**连落盘的地方都没有**（`App.DownloadDir()` 是空串）。那一档
// 摆「搜一首歌…下完的会出现在这里」是骗人——用户照着做，下完发现没地方放，
// 而页头那一行还写着「还没有下载目录」（报告 F3：两句提示互相打架）。那一档
// 给的是**一条路**：去设置里选一个目录。
func (s *Shell) downloadsEmptyState(d *downloadsPage) emptyState {
	// 「全部」那一档空 = 真的没下载；队列整个是空的也是同一句（那时候筛选那一排
	// 压根没摆，用户手上没有第三档可选，说「换一档看看」是骗他）。
	if d.filter != dlFilterAll && len(d.all) > 0 {
		return dlEmptyAfterFilter(d.filter)
	}
	// ⚠️ **先看有没有落盘的地方**：没有的话「搜一首歌」这一句是骗人的。
	if s.app.DownloadDir() == "" {
		e := noDownloadDir
		e.Action = &emptyAction{Label: "去设置", Do: func() { s.goToDownloadDirSetting() }}
		return e
	}
	return lookup(downloadListPath).Empty
}
