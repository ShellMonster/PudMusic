// 歌单内容页的多选那一半：批量操作条、勾选、以及它们背后那几个动作。
//
// 拆出来单独一个文件是因为它和「头图区 + 排序」是两件事：
// `collectionpage.go` 管**这一页长什么样**（拉什么、画在哪、什么排序），
// 这个文件管**用户一次动了几首**（勾选、批量、确认）。
//
// # 两条在这里才成立的规矩
//
//   - 🔴 **全选判定是严格长度相等**（`allSelected`），不是集合比较。PLAN 的
//     C-50 点名的就是这一条：改成集合比较的话，「三首里选了两首」在某个歌单
//     上会被画成全选，而用户点「批量删除」会删掉他没勾的那一首。
//   - **批量下载现在接不上**（issue #29）。留的是接口（`app.Downloader`），
//     没挂上时按钮按成不可用并说清楚，**不假装成功**——假装的话界面上会弹一句
//     「下载任务已添加」，而用户等一小时什么也不会发生。
//
// # 这一层外面包着 Guard
//
// 页级那一道在 `router.go` 的 `Shell.page`（整页崩了换错误页）；列表控件自己
// 还有一道（`list.PagedList` 的 guard.Wrap）。两道不重复。
package ui

import (
	"context"
	"log"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// ── 批量操作条 ─────────────────────────────────────────────────────────────

// viewCollectionBatchBar 是多选态下面那一条：全选、已选几首、批量下载、批量删除。
//
// ⚠️ **「全选」的判定是严格长度相等**（`allSelected`），不是集合比较。照抄
// `src/components/select-all-checkbox-group/index.tsx:22`：
// `groupKeys.length > 0 && selectedKeys.length === groupKeys.length`。改成
// 集合比较的话，「三首里选了两首」在某个子集大小也等于 2 的歌单上会被画成
// 全选，而用户点「批量删除」会删掉他没勾的那一首。
func (s *Shell) viewCollectionBatchBar(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	chosen := len(p.picked)

	// `all` 是这一帧算出来的那一份。Checkbox 要一个能一直绑着的 `*bool`，
	// 而全选的答案每一帧都可能变（翻一页就变）。所以喂它这一帧的答案，
	// 框架点它时 `Changed()` 响，我们再照总数重算一次该全选还是全不选。
	all := p.allSelected()

	ui.Row(c).Key("collection-batch-bar").Center().Gap(12).Padding(10, 24).
		Background(t.Surface).Children(func() {
		if ui.Checkbox(c, &all, "全选").Label("全选").Changed() {
			s.toggleSelectAll(p, !p.allSelected())
		}
		ui.Text(c, "已选 "+strconv.Itoa(chosen)+" 首").FontSize(13).
			TextColor(t.TextMuted)

		dl := ui.Button(c, "批量下载")
		if !s.app.DownloaderMounted() {
			dl = dl.Disabled(true).Tooltip("下载服务还没接上（issue #29）")
		}
		if dl.Clicked() {
			s.askConfirm(p, confirmDownload, chosen, true)
		}

		if ui.Button(c, "批量删除").Clicked() {
			s.askConfirm(p, confirmDelete, chosen, true)
		}

		ui.Spacer(c)

		if ui.Button(c, "退出多选").Clicked() {
			s.toggleCollectionMulti(p)
		}
	})
}

// askConfirm 记下「这件事要用户点一下确定」。
//
// ⚠️ **这里只记，不做**。真正那一下在 `viewCollectionConfirm` 里——确认框是
// 系统模态，在它上面点任何别的控件都不该生效，所以两步必须分开。
func (s *Shell) askConfirm(p *collectionPage, kind confirmKind, count int, only bool) {
	p.confirm = confirmRequest{kind: kind, count: count, only: only, open: true}
}

// viewCollectionConfirm 是批量删除 / 批量下载的二次确认。
//
// 用 MyGo 的 `AlertDialog`（系统原生确认框）而不是自绘浮层：Biu 那边的
// `components/confirm-modal` 就是模态框，而系统这一份免费给到「点外面不关」
// 和「Esc 当取消」——**点外面不关正是二次确认要的**（自绘浮层很容易漏掉，
// 而漏掉的后果是误删一整批）。
//
// ⚠️ 确认框画在**批量操作条外面**（`viewCollection` 的下一层）才有遮罩盖住
// 整页；塞进那条 Row 里的话遮罩只有那一条高。
func (s *Shell) viewCollectionConfirm(c *ui.Context, p *collectionPage) {
	if p.confirm.kind == confirmNone {
		return
	}
	verb, what := "删除", "这一下撤不回来。"
	if p.confirm.kind == confirmDownload {
		verb, what = "下载", "下载会占用磁盘和带宽。"
	}
	title := "确认" + verb + "选中的 " + strconv.Itoa(p.confirm.count) + " 首吗？"
	switch ui.AlertDialog(c, &p.confirm.open, title, what, "取消", verb) {
	case 1:
		kind, only := p.confirm.kind, p.confirm.only
		// ⚠️ 先清掉 kind 再干活：干活的过程中列表可能整个换掉（删除会），
		// 而 `p.confirm` 挂在旧的那一份状态上——不清的话它会被带进新列表。
		p.confirm.kind = confirmNone
		switch kind {
		case confirmDelete:
			s.startCollectionDelete(p)
		case confirmDownload:
			s.startCollectionDownload(p, only)
		}
	case ui.AlertDismissed:
		// Esc = 取消。**要把 kind 清掉**：不清的话下一帧还会把同一个确认框
		// 画出来，而用户已经明确说了「不」。
		p.confirm.kind = confirmNone
	}
}

// ── 多选 ───────────────────────────────────────────────────────────────────

// toggleCollectionMulti 开 / 关多选态。
//
// 关掉的时候**顺手清掉勾选**：留着那些勾，下次开多选时会出现「刚进来就有一堆
// 被勾上的行」，而用户上一次明明说了退出。
func (s *Shell) toggleCollectionMulti(p *collectionPage) {
	p.multi = !p.multi
	if !p.multi {
		clear(p.picked)
		p.list.List().SetSelection(list.Selection{})
		return
	}
	s.attachSelection(p)
}

// attachSelection 把多选那一摊接线装到当前列表上。
//
// 每次**换列表**都要重装（`list.Selection` 装在 `List` 上，而换排序 / 刷新
// 会整个换掉那个 `List`）——漏了这一次的后果是「刚换了排序，多选态的勾全没了
// 而且点勾没反应」，而界面上看不出哪里错了。
func (s *Shell) attachSelection(p *collectionPage) {
	p.list.List().SetSelection(list.Selection{
		On:     func(i int) bool { return p.pickedAt(i) },
		Toggle: func(i int) { s.pickRow(p, i) },
	})
}

// pickedAt 报第 i 行勾上没有。
//
// ⚠️ 越界是**正常**的：`list.Selection.On` 问的是「列表的第 i 行」，而末尾
// 那一行哨兵不在 `picked` 的世界里（它不是歌）。判一下，省得在 Tester 里
// 撞一个下标越界。
func (p *collectionPage) pickedAt(i int) bool {
	m, ok := p.list.List().At(i)
	if !ok {
		return false
	}
	return p.picked[m.ID]
}

// pickRow 点第 i 行的勾。
func (s *Shell) pickRow(p *collectionPage, i int) {
	m, ok := p.list.List().At(i)
	if !ok {
		return
	}
	if p.picked[m.ID] {
		delete(p.picked, m.ID)
	} else {
		p.picked[m.ID] = true
	}
}

// toggleSelectAll 全选 / 全不选。
//
// ⚠️ 判据是 `allSelected()`（严格长度相等），不是「有没有全选上」这个反问。
// 翻页之后总数会变，长度相等这一条自己就重新算出来了，不用维护一个
// 「已加载过的所有行」的集合。
func (s *Shell) toggleSelectAll(p *collectionPage, on bool) {
	l := p.list.List()
	if !on {
		clear(p.picked)
		return
	}
	for i := range l.Len() {
		if m, ok := l.At(i); ok {
			p.picked[m.ID] = true
		}
	}
}

// allSelected 报是不是**当前列表里的每一行都勾上了**。
//
// 🔴 **严格长度相等**，不是集合比较（见 viewCollectionBatchBar 的注释）。
// 空列表不是全选——一个收藏夹里的歌一条都还没加载出来时，「全选」画成勾上
// 的话，用户会以为接下来点一下删的是全部，而实际是零条。
//
// ⚠️ 长度相等**之后还要扫一遍列表**，确认每一个勾都落在还在列表里的行上。
// 这一遍只在「数量凑上了」的时候才跑（平时一次点击才会到），而它挡的是这样
// 一种情形：`picked` 里混进了一个已经不在列表里的行（换排序 / 刷新时漏清，
// 或者以后接了新的勾选来源），于是「两首已勾 + 一个陈旧 id」和「三首已勾」
// 长度一样——只比长度就会把前者显示成全选，而用户点「批量删除」删掉的是
// 他没勾的那两首里的别的。
func (p *collectionPage) allSelected() bool {
	l := p.list.List()
	n := l.Len()
	if n == 0 || len(p.picked) != n {
		return false
	}
	for i := range n {
		m, ok := l.At(i)
		if !ok || !p.picked[m.ID] {
			return false
		}
	}
	return true
}

// ── 动作 ───────────────────────────────────────────────────────────────────

// setCollectionFav 收藏 / 取消收藏。
//
// ⚠️ 立刻返回：网络往返在 `app.SetFavorite` 的后台 goroutine 里，而那一层
// 会在 B 站那边改完之后（500ms）刷新元信息并叫醒界面——**这一页不用等，也
// 不用自己定时器**（定时器跑在界面线程之外，改界面状态就违反铁律一了）。
func (s *Shell) setCollectionFav(p *collectionPage, fav bool) {
	what := "正在收藏…"
	if !fav {
		what = "正在取消收藏…"
	}
	s.addToast(what)
	s.app.SetFavorite(p.ref, fav)
}

// startCollectionDelete 批量删除勾上的那些。
//
// 立刻返回：真正删的那一趟在后台，删完那一层会重新拉元信息（见
// `app.RemoveMedias`），这里自己把列表重来一遍。
func (s *Shell) startCollectionDelete(p *collectionPage) {
	medias := s.pickedMedias(p)

	// ⚠️ **「勾了 5 首却只找齐 3 条」要说出来，不能闷头删 3 条**。
	//
	// `pickedMedias` 拿不到的那几条（完整信息在 `media` 表里找不到）只会被
	// 静默跳过——而界面上写的是「已选 5 首」。用户看着删除成功、刷新之后那两首
	// 还在，而界面上从头到尾没有一句话说过「有两首没删掉」。那种「少做了一半
	// 还说做完了」比报错难查得多。
	//
	// ⚠️ 这一句**排在「一条都没有」的前面**：一条都找不齐时它才是真正发生的那
	// 一件事（「先勾几首再删」是句假话——用户明明勾了）。
	missing := len(p.picked) - len(medias)
	if missing > 0 {
		log.Printf("biu: 勾了 %d 首却只找齐 %d 条完整信息，%d 首会被跳过",
			len(p.picked), len(medias), missing)
		s.addToast("有 " + strconv.Itoa(missing) + " 首查不到完整信息，这次先删其余的")
	}
	if len(medias) == 0 {
		if missing == 0 {
			s.addToast("先勾几首再删")
		}
		return
	}

	// 列表当场重来（它顺带清掉了勾选）：那几首已经不在收藏夹里了，勾着没有
	// 意义——留着的话「全选」那一格会在重拉之后对不上新的总数。
	s.reloadCollection(p)

	ref := p.ref
	go func() {
		if err := s.app.RemoveMedias(context.Background(), ref, medias); err != nil {
			log.Printf("biu: 从收藏夹里删内容失败：%v", err)
			s.addToast("删不掉：" + bili.Friendly(err))
			s.wake.invalidate()
		}
	}()
}

// startCollectionDownload 批量下载。
//
// 🔴 **下载服务还没接上**（issue #29），所以这里现在只有两件事：没挂上就如实
// 说，挂上了就把这一批交过去。**不假装成功**——假装的话界面上会弹一句
// 「下载任务已添加」，而用户等一小时什么也不会发生。
//
// only 决定范围：批量操作条传 true（只下勾上的），⋯ 菜单的「下载全部」传
// false（把已经加载出来的全部）。
func (s *Shell) startCollectionDownload(p *collectionPage, only bool) {
	if !s.app.DownloaderMounted() {
		s.addToast("下载还没接上：下载服务（issue #29）还没做")
		return
	}
	medias := s.pickedMedias(p)
	if !only {
		medias = s.allMedias(p)
	}
	if len(medias) == 0 {
		what := "勾上的"
		if !only {
			what = "这里"
		}
		s.addToast(what + "还没有可以下载的内容")
		return
	}
	ref := p.ref
	go func() {
		err := s.app.DownloadMedias(context.Background(), ref, medias)
		s.addToast(downloadDoneText(err))
		s.wake.invalidate()
	}()
}

// downloadDoneText 是批量下载跑完之后说的那句话。
func downloadDoneText(err error) string {
	switch {
	case err == nil:
		return "下载任务已添加"
	case app.DownloaderMissing(err):
		return "下载还没接上：下载服务（issue #29）还没做"
	}
	return "下载不了：" + bili.Friendly(err)
}

// pickedMedias 是当前勾上的那些条目的完整信息，**按列表里的顺序**。
//
// ⚠️ 按列表顺序而不是按勾的顺序（`picked` 是 map，key 无序）：批量删除的
// 请求里条目顺序不变，同一个操作跑两次发出去的报文才一样——排障时对着日志
// 看才有意义。
func (s *Shell) pickedMedias(p *collectionPage) []bili.Media {
	l := p.list.List()
	out := make([]bili.Media, 0, len(p.picked))
	for i := range l.Len() {
		m, ok := l.At(i)
		if !ok || !p.picked[m.ID] {
			continue
		}
		if raw, found := p.media.get(m.ID); found {
			out = append(out, raw)
		}
	}
	return out
}

// allMedias 是当前已经加载出来的全部条目。给「播放全部 / 下载全部」用。
//
// ⚠️ **只含加载出来的那几页**。B 站的「全部 id」接口一给几千条，一个几千首
// 的收藏夹要等它好几秒——用户等的这段时间里界面上什么反应都没有。等拉列表
// 那一层做完再说（见 `bili` 里还没有的那个接口）。
func (s *Shell) allMedias(p *collectionPage) []bili.Media {
	l := p.list.List()
	out := make([]bili.Media, 0, l.Len())
	for i := range l.Len() {
		if m, ok := l.At(i); ok {
			if raw, found := p.media.get(m.ID); found {
				out = append(out, raw)
			}
		}
	}
	return out
}

// playWholeCollection 播放全部：从第一首起，把**已经加载出来的**都接进队列。
func (s *Shell) playWholeCollection(p *collectionPage) {
	items := itemsOfMedias(s.allMedias(p))
	if len(items) == 0 {
		s.addToast("这里还没有可以播的歌")
		return
	}
	if err := s.app.PlayFrom(items...); err != nil {
		s.addToast("放不了：" + err.Error())
	}
}

// addWholeCollectionToQueue 把已经加载出来的都追加到队列末尾。
func (s *Shell) addWholeCollectionToQueue(p *collectionPage) {
	items := itemsOfMedias(s.allMedias(p))
	if len(items) == 0 {
		s.addToast("这里还没有可以加的歌")
		return
	}
	if err := s.app.Enqueue(items...); err != nil {
		s.addToast("加不进队列：" + err.Error())
	}
}

// itemsOfMedias 把一批 B 站内容翻成队列项（跳过放不了的那种）。
//
// 合集 / 电影那一类（type 21 / 24）在这里就被跳过，所以「播放全部」不会
// 因为收藏夹里躺着两个合集就整条失败。
func itemsOfMedias(medias []bili.Media) []app.Item {
	items := make([]app.Item, 0, len(medias))
	for _, m := range medias {
		if !m.Playable() {
			continue
		}
		items = append(items, onlineItemOf(m))
	}
	return items
}

// reloadCollection 重新拉这一页：清掉勾选、清掉旧内容、从第一页重来。
func (s *Shell) reloadCollection(p *collectionPage) {
	clear(p.picked)
	p.media.reset()
	p.list = s.newCollectionList(p)
	p.listSort = p.sort
	p.list.Reload()
	if p.multi {
		s.attachSelection(p)
	}
}
