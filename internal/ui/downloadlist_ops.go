// 下载记录页上的那些操作：暂停 / 继续 / 重试 / 删除 / 清空，加删除前那道
// 确认弹窗，和「打开文件所在文件夹」。
//
// 五种操作的名字和亮不亮的条件**逐条照** `biu/src/pages/download-list/actions.tsx`
// 的那份 `actions` 数组（`show` 那一栏）：
//
//	暂停   只在 downloading        继续  只在 downloadPaused
//	重试   只在 failed             删除  一直在
//	打开文件 只在 completed        清空  页头（上游是表格右上角那个垃圾桶）
//
// 上游每个按钮按下之后会锁 2000ms（`lockAction`，因为它每个操作都是
// `await window.electron.*`）。这边**不锁**：五种操作全是同步改一份内存里的
// 状态，改完这一帧就画出来了，没有等第二下的那个窗口期；锁上只会让用户按第二
// 下时按钮是灰的，而它灰得没有道理。

package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 按钮上的字，和上游 `actions.tsx:71-136` 那一组 label 一个字不差。
const (
	opPause  = "暂停"
	opResume = "继续"
	opRetry  = "重试"
	opDelete = "删除"
	opOpen   = "打开"
)

// opIcons 是每一颗操作按钮画哪一张图。
//
// 上游那几颗是 `isIconOnly` 的 `AsyncButton`（`actions.tsx:150-176`，每个按钮的
// 悬停提示就是上面那个名字）。我们照它画图标键——`Label` 仍然给名字，读屏软件
// 念的和无头 Tester 认的都还是这一句。
//
// ⚠️ 播放 / 暂停那两张走 `transportIcon`（`icons.go` 顶上那一段：状态图标只准
// 从那个函数里出），不在调用点自己挑。
var opIcons = map[string]string{
	opOpen:   iconFolder,
	opPause:  transportIcon(true),
	opResume: transportIcon(false),
	opRetry:  iconRefresh,
	opDelete: iconDeleteBin5,
}

// viewToolbar 是页头那一行：标题 + 打开下载目录 + 清空记录。
//
// 上游是 `<h1>下载记录</h1>` 加右边一个写着下载路径的按钮（`index.tsx:99-103`）。
//
// ⚠️ **路径不在按钮上**：上游那样写，路径一长按钮就被截成半句话，用户看到
// "/Users/daoz…" 得先猜一下点下去会发生什么。这里路径单独立一段（超长打
// 省略号），按钮只说它做的那件事——两边说的都是同一件事，而这个更好读。
func (d *downloadsPage) viewToolbar(c *ui.Context, s *Shell) {
	t := c.Theme()
	dir := s.app.DownloadDir()

	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "下载记录").FontSize(scale.Font(c, pageTitleSize))

		// 路径。空串是「还没有下载目录」（设置里没选、系统目录也没拿到，
		// 见 `app.Downloads.FallbackDir`）——那时候要说出来，不能画一段空白
		// 让用户以为按钮坏了。
		path := dir
		if path == "" {
			path = "还没有下载目录"
		}
		// 超长被截掉的时候完整路径在 tooltip 里：用户不必去设置页才看得到它落在
		// 哪儿。没有目录时那一截是空的（提示为空就不画东西），没有可补充的。
		d.tipWith(c, ui.Text(c, path).Grow(1).FontSize(scale.Font(c, 12)).
			LineHeight(smallCopyLeading).TextColor(t.TextMuted).MaxLines(1).
			Ellipsis("…"), dir)

		// ⚠️ 没有目录时不摆这个按钮：按下去是打不开的。一个只会说「不行」的
		// 按钮不如不摆（和首页不摆「全部播放」是同一个取舍）。
		if dir != "" && flatButton(c, "打开文件夹").Width(dlOpenDirW).Clicked() {
			d.openDownloadDir(dir, s)
		}
		// ⚠️ 灰不灰看的是**整个队列**（`d.all`），不是筛完屏幕上那几行：
		// 这一颗清的是整个队列，而在「视频」那一档筛出空来的时候它还摆得动，
		// 就该还能按——否则用户看着一排有记录的表格之外的地方，想清却没得清。
		//
		// 上游是表格右上角一个 `isIconOnly` 的垃圾桶（`index.tsx:128-132`），
		// 我们把它留在页头——表格那一块是共用的（widgets/list），页头才是这一页
		// 自己的地方。
		if d.tipWith(c, flatIconButton(c, iconDeleteBin5, "清空记录", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 7).
			Disabled(len(d.all) == 0), "清空记录").Clicked() {
			d.askClear(s)
		}
	})
}

// dlOpenDirW 是页头「打开文件夹」那个文字键的宽度。
//
// 清空记录不再有宽度：它是一枚图标键（`iconHitMd`），宽由图标和它自己的内边距
// 撑出来——写死一个宽度只会和旁边那个框对不上。
const dlOpenDirW = 96

// viewOps 是「操作」那一列。
//
// 哪一个亮着见文件头那张表。这里不判断「点了会怎样」——判断在 `download.Manager`
// 那一侧（`Status.CanPause` / `CanResume` 就在状态类型上），这里只判断**摆不摆**
// 这颗按钮。摆一颗按了没反应的按钮比不摆更糟。
func (d *downloadsPage) viewOps(c *ui.Context, s *Shell, r download.Snapshot) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		// 打开文件所在文件夹：只在真的下完、文件已经在盘上的时候摆
		// （上游 `index.tsx:153-173` 的 `show: data.status === "completed"`）。
		// 半成品和失败的那两支不摆——`Dest` 上要么是空的，要么是一个 `.part`，
		// 在访达里指着它只会让用户以为那就是那首歌。
		if r.Status == download.StatusDone {
			d.opButton(c, opOpen, func() { d.revealFile(r, s) })
		}
		if r.Status.CanPause() {
			d.opButton(c, opPause, func() { d.pause(r, s) })
		}
		if r.Status == download.StatusPaused {
			d.opButton(c, opResume, func() { d.resume(r, s) })
		}
		if r.Status == download.StatusError {
			d.opButton(c, opRetry, func() { d.resume(r, s) })
		}
		d.opButton(c, opDelete, func() { d.askDeleteOne(r, s) })
	})
}

// opButton 是操作列里的一颗按钮。按钮一多就宽过列宽，所以列宽是按**最多同时亮
// 的那几颗**挑的（完成时是「打开」+「删除」，出错的行是「重试」+「删除」，
// 最多两颗，见 `dlOpsW`）。
func (d *downloadsPage) opButton(c *ui.Context, label string, fn func()) {
	if d.tipWith(c, flatIconButton(c, opIcons[label], label, iconSm).
		Size(dlOpW, dlOpW).Padding(0, 7), label).Clicked() {
		fn()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 五种操作
// ─────────────────────────────────────────────────────────────────────────────

// pause 停下一项。
func (d *downloadsPage) pause(r download.Snapshot, s *Shell) {
	if err := s.app.PauseDownload(r.ID); err != nil {
		s.addToast("暂停不了："+trimDownloadErr(err), notice.Danger)
	}
}

// resume 接着下一项。暂停和出错走的是同一个动作（`download.Manager.Resume`：
// 「继续」和「重试」是同一个动作，只是文案和亮着的条件不同），所以这两个按钮
// 共用它。
//
// 引擎那一侧会把 `Err` 清掉并重新排上队（见 `Manager.Resume`），所以按了「重试」
// 之后那一格画出来是「等待中」/「下载中」而不是原来那句红字——**重试必须让人
// 看得出已经重来了**。
func (d *downloadsPage) resume(r download.Snapshot, s *Shell) {
	if err := s.app.ResumeDownload(r.ID); err != nil {
		s.addToast("继续不了："+trimDownloadErr(err), notice.Danger)
	}
}

// askDeleteOne 按「删除」之后弹出来的那一道。
//
// ⚠️ **一律问**，包括已经下完的那一条——这一票的验收项写的是「删除走确认弹窗」，
// 而第一版照上游（`actions.tsx:137-146`）只对「还没下完」的才问，比验收项的字面
// 窄了一格。评审把它挑出来是对的：这一页上「删除」不是一个只有一处的动作，
// 用户在弹窗里看到的是这一行的歌名和它下面的状态，而删除是不可撤销的（
// 队列里那一项没了就是没了）——多按一下换来一个统一的预期。
//
// 正文按状态分，因为两件事的后果完全不同，说错一句就是在骗用户：
//
//	没下完  上游那一句：「当前任务未下载完成，确认删除后将无法恢复」——半成品会
//	         被删掉，那首歌就没了
//	已下完  文件在 `Dest` 上、**不会**被动（`Manager.Remove` 只删 `.part`），
//	         所以正文说的是「删除的只是这条记录，下载目录里的歌留着」
func (d *downloadsPage) askDeleteOne(r download.Snapshot, s *Shell) {
	body := "当前任务未下载完成，确认删除后将无法恢复。"
	if r.Status == download.StatusDone {
		body = "这一条已经下完，删除的只是这条记录，下载目录里的歌留着。"
	}
	d.ask = &confirmAsk{
		title: "确认删除吗？",
		body:  body,
		ok:    opDelete,
		doIt:  func() { d.deleteOne(r, s) },
		open:  true,
	}
}

// deleteOne 真的删掉一条。记录拿掉，半成品由引擎删（`Manager.Remove`）。
func (d *downloadsPage) deleteOne(r download.Snapshot, s *Shell) {
	if err := s.app.RemoveDownload(r.ID); err != nil {
		s.addToast("删不掉："+trimDownloadErr(err), notice.Danger)
	}
}

// askClear 按「清空记录」之后弹出来的那一道。
//
// ⚠️ 上游的「清空」是直接清的（`index.tsx:65-67` 就一句 `clearMediaDownloadTaskList`，
// 没有确认弹窗）。而**这一边的清空比上游狠**：它会停掉所有正在下的，并把每一首
// 的半成品删掉（`Manager.Clear`）——上游那边停不掉正在下的，因为它是每个任务
// 一个 goroutine。所以这里加一道确认，说的也是这一边真正会发生的事。
//
// 「删一行」和「清空」是同一类动作（都不可恢复），所以走同一道弹窗，不另写一
// 个。一条记录都没有时按钮是灰的，这一支走不到。
//
// ⚠️ **报的是整个队列的条数，不是筛选之后屏幕上那几行**（用的是 `d.all`）。
// `App.ClearDownloads` 清的是整个队列：用户在「视频」那一档上看到「确认清空这
// 1 条」而点下去删掉 21 条，那一句是在骗用户，而且骗掉的正是不可撤销的那部分
// （还在下的半成品会被删掉，见上面那段）。
func (d *downloadsPage) askClear(s *Shell) {
	if len(d.all) == 0 {
		return
	}
	n := len(d.all)
	half := 0
	for _, r := range d.all {
		if r.Status != download.StatusDone {
			half++
		}
	}
	body := "确认清空这 " + strconv.Itoa(n) + " 条下载记录吗？下完的歌留在盘上，删不掉。"
	if half > 0 {
		body += "其中 " + strconv.Itoa(half) + " 条还没下完，清空之后那些半成品也会被删掉。"
	}
	d.ask = &confirmAsk{
		title: "确认清空吗？",
		body:  body,
		ok:    "清空",
		doIt: func() {
			s.app.ClearDownloads()
			s.addToast("清空了 "+strconv.Itoa(n)+" 条下载记录", notice.Success)
		},
		open: true,
	}
}

// openDownloadDir 在系统里打开下载目录。
func (d *downloadsPage) openDownloadDir(dir string, s *Shell) {
	if d.openDir == nil {
		return
	}
	if err := d.openDir(dir); err != nil {
		s.addToast("打不开那个目录："+trimDownloadErr(err), notice.Danger)
	}
}

// systemOpenDir 是「打开下载目录」在真窗口里的那一份：在访达里开一个目录。
//
// 走 `mygo.Shell.OpenPath` 而不是 `OpenExternal`：后者是给网址用的（交给默认
// 浏览器），而目录要交给系统的文件管理器。
//
// ⚠️ 这一句在**无头 Tester 里会 panic**：MyGo 的 `needsApp` 见到
// `App.Run` 没跑过就直接 panic（mygo/app.go:128）。所以它只能挂在
// `downloadsPage` 的接缝上（`newDownloadsPage`），测试建完外壳换成记动作的桩。
func systemOpenDir(path string) error { return mygo.Shell.OpenPath(path) }

// systemOpenLink 是「文件那一列的外链」在真窗口里的那一份：交给默认浏览器。
//
// 和 `systemOpenDir` 同一个理由走接缝（无头 Tester 里 `mygo.Shell` 会 panic）。
func systemOpenLink(url string) error { return mygo.Shell.OpenExternal(url) }

// revealFile 在访达里显示这一首歌。
//
// 显出来的是 `Dest`（那首歌），不是目录（`ShowItemInFolder` 天然会把它所在
// 的目录也打开，文件自己被选中）。
// dlOpenBiliLabel 是文件那一列 hover 出来的外链图标的名字。
//
// 图标只在 hover 那一帧画，所以这个名字也是「这一下 hover 生效了没有」的
// 测试凭据（`Find` 找得到它 = 图标在画面上）。
const dlOpenBiliLabel = "在 B 站打开"

// openBiliPage 点文件名时打开这一首在 B 站的页面。
//
// 上游是 `openBiliVideoLink({type: item.sid ? "audio" : "mv", bvid, sid})`
// （`download-list/index.tsx:168-175`）。队列里的身份是 `TaskID`（`bvid|cid`，
// 视频再带一个 `|video`），bvid 就是第一个 `|` 前面那一段——**不拿整个 ID 拼
// URL**：那样拼出来的是一个 404，而浏览器打开 404 不会回到这里报错。
//
// 认不出 bvid（队列里有一条来历不明的记录）时说一句，别按了没反应。
func (d *downloadsPage) openBiliPage(r download.Snapshot, s *Shell) {
	bvid := r.ID
	if i := strings.IndexByte(bvid, '|'); i >= 0 {
		bvid = bvid[:i]
	}
	if bvid == "" {
		s.addToast("这一条没有 B 站地址", notice.Warning)
		return
	}
	if d.openLink == nil {
		return
	}
	if err := d.openLink("https://www.bilibili.com/video/" + bvid); err != nil {
		s.addToast("打不开那个链接："+trimDownloadErr(err), notice.Danger)
	}
}

func (d *downloadsPage) revealFile(r download.Snapshot, s *Shell) {
	if r.Dest == "" {
		// 队列里没有落盘路径 = 这一条是认不出位置的。回顶按钮已经在页头给出
		// 了方向，这里说一句，别按了没反应。
		s.addToast("这一条没有文件路径", notice.Warning)
		return
	}
	if d.reveal == nil {
		return
	}
	d.reveal(r.Dest)
}

// ─────────────────────────────────────────────────────────────────────────────
// 确认弹窗
// ─────────────────────────────────────────────────────────────────────────────

// confirmAsk 是「你确定吗」那一弹窗：正在等用户点头的那一次操作。
//
// 状态长在页面上（不是 view 的局部变量）——即时模式每一帧都重画一遍，局部变
// 量下一帧就没了（和 `Shell.searchKeyword` 同一个理由）。
type confirmAsk struct {
	// title 是标题，body 是正文，ok 是确认按钮上的字。三样都摆出来，不靠
	// 「你确定吗？」那一句话让用户猜自己在删什么。
	title, body, ok string
	// doIt 是点了确认之后要做的事。**闭包**（不是 id）：确认的时候队列可能
	// 已经变了，按 id 回去找一条已经不在的记录是另一种错。
	doIt func()
	// open 是 `ui.Modal` 的那个开关，建的时候置 true（`ui.Modal` 在它是 false
	// 的时候一个节点都不建），之后写它的是框架（点背景、按 Esc）。
	open bool
}

// viewConfirm 画那道弹窗。
//
// ⚠️ 弹窗画在**整页画完之后**：删除按钮在表格里，按下它就把 `d.ask` 填上，同一
// 帧的这一段就把弹窗画出来。放反了的话用户要等下一次重画才看见弹窗，而这一帧
// 的重画不一定有（按钮按下之后没有任何东西会要求再画一帧）。
//
// Escape 和点背景都关掉它，**并且这一次删除不算数**——上游也是「取消」什么
// 都不做。
//
// ⚠️ **danger 那一档是红的**（上游 `download-list/actions.tsx:141-146` 那一处
// 没有写 `type`，而那个弹窗的默认值就是 `danger`）：删掉的是一条下载任务，
// 而任务没下完的时候删掉就再也下不回来了。
func (d *downloadsPage) viewConfirm(c *ui.Context, s *Shell) {
	if d.ask == nil {
		return
	}
	ask := d.ask
	switch confirmModal(c, &ask.open, confirmSpec{
		tone:    toneDanger,
		title:   ask.title,
		body:    ask.body,
		confirm: ask.ok,
		cancel:  "取消",
	}) {
	case confirmAccepted:
		// 先关弹窗再做那件事：做的时候可能弹提示（addToast），
		// 而弹窗还开着的话用户看到的是提示压在弹窗底下。
		d.ask = nil
		ask.doIt()
	case confirmCancelled:
		d.ask = nil
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// tipWith 是这一页的悬停提示入口：状态挂在 Shell 上（tooltip.go），这里只是
// 把这一页拿到的那份递过去。
func (d *downloadsPage) tipWith(c *ui.Context, e ui.Element, text string) ui.Element {
	return tipFrom(d.tip, c, e, text, tipDefault)
}

// trimDownloadErr 把错误里那截前缀去掉。
//
// 错误都带一个包名前缀（`download: ` / `app: `，见 `internal/app/downloads.go`
// 里那几个 `errDownloadsMissing` 和 `service/download` 里各个 `fmt.Errorf`），
// 弹给用户看的时候那截前缀是白占地方——尤其在一条只有一格宽的提示里。
//
// ⚠️ 两截都要去：`app: ` 那一层是给日志认得出哪一层的，界面上用户只关心
// 「删不掉」还是「打不开」。
func trimDownloadErr(err error) string {
	msg := err.Error()
	for _, p := range []string{"download: ", "app: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	return msg
}
