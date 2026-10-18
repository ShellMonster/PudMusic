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
//   - **批量下载没挂上时不假装成功**。没挂上的话（正常路径不会，测试和没装
//     下载服务的那一次启动会）按钮按成不可用并说清楚——假装的话界面上会弹一句
//     「下载任务已添加」，而用户等一小时什么也不会发生。挂上了之后走的是
//     `app.Downloader`，实现是 App 自己（见 `collection_mount.go`）。
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
	"sync"

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
			dl = dl.Disabled(true).Tooltip("这个应用没把下载服务挂上")
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
	s.askConfirmAbout(p, kind, count, only, "")
}

// askConfirmAbout 是上面那一个，额外带一句「动的是哪一个收藏夹」。
func (s *Shell) askConfirmAbout(p *collectionPage, kind confirmKind, count int, only bool, name string) {
	p.confirm = confirmRequest{kind: kind, count: count, only: only, open: true, name: name}
}

// confirmText 是一个确认框的三样字：标题、正文、确认按钮上写的那两个字。
//
// 🔴 **三个都按「这一下有多不可逆」分档**，而分档的规矩是规格点名的（issue
// #100）：「删 B 站那边的」和「只删本地记录」必须让用户一眼分得出来。靠一个
// 共用的动词（「删掉」）加一个「吗」是分不出来的——两个框长得一模一样，而
// 按错的那一个撤不回来。
//
//   - **删 B 站上的收藏夹**：最重。正文要点名它是 B 站上那个、要点名里面
//     有多少首、要说撤不回来。三样缺一样，用户会以为和下面那个一样轻。
//   - **只从本地记录里拿掉**：明确说 **B 站上的那个还在**。这一条一个字节都
//     不发过去，而用户正是靠这句话才知道自己没做不可逆的事。
func confirmText(req confirmRequest, mediaCount int) (title, what, button string) {
	switch req.kind {
	case confirmDownload:
		return "确认下载选中的 " + strconv.Itoa(req.count) + " 首吗？",
			"下载会占用磁盘和带宽。", "下载"
	case confirmDelete:
		return "确认删除选中的 " + strconv.Itoa(req.count) + " 首吗？",
			"这一下撤不回来。", "删除"
	case confirmClearInvalid:
		return "清掉「" + req.name + "」里的失效内容吗？",
			"已经失效的条目会从这个收藏夹里删掉，撤不回来。", "清除"
	case confirmDeleteFolder:
		return "删掉「" + req.name + "」吗？",
			"B 站上的这个收藏夹和里面的 " + strconv.Itoa(mediaCount) + " 首都会没，撤不回来。",
			"删除"
	case confirmForgetFolder:
		return "从侧栏移除「" + req.name + "」吗？",
			"B 站上的这个收藏夹不会动，只是在这儿不再显示它了。", "移除"
	}
	return "确认吗？", "", "确定"
}

// viewCollectionConfirm 是批量删除 / 批量下载、以及 ⋯ 菜单里那三件的二次确认。
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
	title, what, button := confirmText(p.confirm, s.app.Collection(p.ref).MediaCount)
	switch ui.AlertDialog(c, &p.confirm.open, title, what, "取消", button) {
	case 1:
		// ⚠️ 先把整份抄下来再干活：干活的过程中 `p.confirm` 会被重画的那一帧
		// 改掉，而旧的那一份才是「用户在确认的是哪一件事」。
		req := p.confirm
		// ⚠️ 再清掉 kind：干活的过程中列表可能整个换掉（删除会），
		// 而 `p.confirm` 挂在旧的那一份状态上——不清的话它会被带进新列表。
		p.confirm.kind = confirmNone
		s.runConfirmed(p, req)
	case 0, ui.AlertDismissed:
		// 不。**两个来源都要在这儿**：点「取消」按钮（`AlertDialog` 返回它的下标
		// 0）和按 Esc（返回 `AlertDismissed`）——后者才是原来那一支。
		//
		// ⚠️ 别把这一行说成「修了一个 bug」。**两个后果都验过，它都不成立**：
		// 框不会重画（`coreAlertDialog` 在 `*open` 为 false 时第一句就 return -1，
		// 而「取消」按下去的时候 `open` 已经被 `AlertDialog` 自己关掉了）；而残留
		// 的 `kind` 也不会连着把事情做掉——`askConfirm` / `askConfirmAbout` 是
		// **整份替换** `confirmRequest`，下一次有人按菜单时它早就被覆盖了
		// （评审的变异验证也确认了这一点：拿掉 `case 0` 之后全部测试照样绿）。
		//
		// 它真正值着的只有一件小事：**清掉状态是本分**。不唯一的代价是这一页
		// 每一帧白跑一次 `confirmText` 加一次立刻返回 -1 的 `AlertDialog`——
		// 便宜，但不该白花，而且「这一页没在等任何确认」和「它在等一个没人看得见
		// 的确认」在以后加新动作时会长出不一样的行为。
		p.confirm.kind = confirmNone
	}
}

// runConfirmed 是用户在确认框上点了那一下之后做的事。
//
// ⚠️ **取消不落到这里**（`AlertDismissed` 那一支），而这一层**一个网络请求
// 都不发**：所有写操作都在后面的后台 goroutine 里。点错一次的代价就是白发一
// 次请求，而白发一次「删收藏夹」在 B 站那边是删掉了——所以这里必须真的什么都不做。
func (s *Shell) runConfirmed(p *collectionPage, req confirmRequest) {
	switch req.kind {
	case confirmDelete:
		s.startCollectionDelete(p)
	case confirmDownload:
		s.startCollectionDownload(p, req.only)
	case confirmClearInvalid:
		s.startCollectionClearInvalid(p)
	case confirmDeleteFolder:
		s.startCollectionDeleteFolder(p)
	case confirmForgetFolder:
		s.startCollectionForgetFolder(p)
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
		p.list.SetSelection(list.Selection{})
		return
	}
	s.attachSelection(p)
}

// attachSelection 把多选那一摊接线装到当前列表上。
//
// 每次**换列表**都要重装（`list.Selection` 装在底下那一对控件上，而换排序 /
// 刷新会整个换掉那个 `PagedList`）——漏了这一次的后果是「刚换了排序，多选态的勾
// 全没了而且点勾没反应」，而界面上看不出哪里错了。
//
// 🔴 而且**两份控件都要装**：设置的「数据显示样式」切到网格之后画的是另一半，
// 只装列表那半边的话网格里一格勾选框都没有（`PagedList.SetSelection` 的注释）。
func (s *Shell) attachSelection(p *collectionPage) {
	// ⚠️ 走 `PagedList.SetSelection` 而不是 `p.list.List().SetSelection`：后者只装到
	// 列表那半边，而用户可能正开着「数据显示样式 = 网格」，那半边压根不画。漏了它
	// 的症状是「切到网格之后一格勾选框都没有，逐首勾完全做不到」——而全选走
	// `Len()` / `At()` 不经过画法，照样能用，于是界面上看着「多选还是好的」。
	p.list.SetSelection(list.Selection{
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

// startCollectionClearInvalid 让 B 站把这个收藏夹里失效的条目清掉。
//
// ⚠️ **这里不调 `reloadCollection`**：清还没做完，而 `Reload` 发出去的那一趟
// 会拉回**还带着失效条目**的列表——刷新确实发生了，只是发生得太早，症状是
// 「点完什么都没变」。真正那一次刷新由 `app.ClearInvalidMedias` 做完之后
// 递一个 `NoticeCollectionChanged` 回来，`flushCollectionNotice` 在界面线程
// 那一帧 reload（理由见那个常量）。
//
// 一句「正在清除…」是给用户的进度：清一个几千首的收藏夹要走完一整个请求，
// 十几秒里界面毫无动静的话多数人会再按一次。
func (s *Shell) startCollectionClearInvalid(p *collectionPage) {
	s.addToast("正在清除…")
	s.app.ClearInvalidMedias(p.ref)
}

// startCollectionDeleteFolder 把 B 站上这个收藏夹删掉（连同里面的歌）。
//
// 🔴 **只有成功了才离开这一页**：B 站拒绝的时候收藏夹还在那儿、侧栏里也还在，
// 而跳走的后果是用户停在**一个空页面**上、那句「删不掉」也被 `navigate` 的
// `clearToasts` 一起擦掉——零提示加一个空页面，而东西好好地在那儿。
//
// ⚠️ **走开这件事过了一手**（`leave` 那儿）：那一趟跑在后台 goroutine 里，而
// `navigate` 和 `s.collection = nil` 都是界面状态（铁律一）。直接在回调里调，
// `-race` 会报，而真机上的症状是「有时候跳、有时候不跳」。
func (s *Shell) startCollectionDeleteFolder(p *collectionPage) {
	ref, title := p.ref, collectionTitleText(s.app.Collection(p.ref))
	s.app.DeleteCollectionFolder(ref, title, func(msg string, ok bool) {
		s.sayFolderDone(msg)
		if !ok {
			return // 收藏夹还在，这一页留着，提示也留着
		}
		s.leave.put(ref)
		s.wake.invalidate()
	})
}

// startCollectionForgetFolder 只把这一行从本地记录里拿掉。
//
// 🔴 **一个字节都不往 B 站发**（`app.ForgetCollectionFolder` 不碰网络），
// 所以这里既没有失败也没有「正在…」——说完那句话事情就结束了。
//
// ⚠️ 但**这一页还留着**：收藏夹在 B 站上还在、刚才的内容也还在，只是侧栏
// 不再列它了。页面继续停着是对的，用户刚才看的东西还在。
func (s *Shell) startCollectionForgetFolder(p *collectionPage) {
	s.app.ForgetCollectionFolder(p.ref, collectionTitleText(s.app.Collection(p.ref)), s.sayFolderDone)
}

// pendingLeave 是「刚被删掉的那个容器，等下一帧走开」那个交接槽。
//
// ⚠️ **带锁**：写它的是后台 goroutine（删收藏夹那一趟），读它的是界面线程
// （`takeLeave`），而中间没有别的同步——`-race` 会报，真机上是「有时候跳、有时候
// 不跳」。这一小摊的形状同 `coverURL` / `followPage.box`。
type pendingLeave struct {
	mu   sync.Mutex
	ref  app.CollectionRef
	have bool
}

// put 记下「该走开了」。同一时刻只有一个（收藏夹页一次只处理一个确认框）。
func (l *pendingLeave) put(ref app.CollectionRef) {
	l.mu.Lock()
	l.ref, l.have = ref, true
	l.mu.Unlock()
}

// take 取走。取过就没有了——不走第二遍（理由同 `collectionNotice`）。
func (l *pendingLeave) take() (app.CollectionRef, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ref, have := l.ref, l.have
	l.have = false
	return ref, have
}

// takeLeave 是「刚被删掉的那个容器，该走开了」那一趟的出口。
//
// **每帧开头调一次，而且取走**：取过就不会再说第二遍（理由同
// `flushCollectionNotice`）。
func (s *Shell) takeLeave() {
	gone, ok := s.leave.take()
	if !ok {
		return
	}
	// 这一页的状态整个换掉：容器已经不存在了，留着一份只会让 `collectionFor`
	// 在用户从历史里回去时拿回一个打不开的东西。
	if s.collection != nil && s.collection.ref == gone {
		s.collection = nil
	}
	// ⚠️ **判的是「当前停在这一页上」**：确认框开着的时候用户可能已经切走了，
	// 而那时候再把人家从别的页面踢走，是这一层最不该干的那种事。
	if s.router.Path() == collectionPath(gone.ID) {
		s.navigate("/empty")
	}
}

// startCollectionDownload 批量下载。
//
// 交过去的那一趟在后台（`app.EnqueueDownload` 要补分 P id，那是一次网络往返，
// 界面线程上一次都不许有）。**结果一句话说全**——那一层编好的话里带着「几首
// 进去了、几首没进去」，所以这里不再往前加任何前缀。
//
// only 决定范围：批量操作条传 true（只下勾上的），⋯ 菜单的「下载全部」传
// false（把已经加载出来的全部）。
func (s *Shell) startCollectionDownload(p *collectionPage, only bool) {
	if !s.app.DownloaderMounted() {
		s.addToast("下载还没挂上：这个应用没把下载服务装进来")
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
//
// ⚠️ **err 不是空的时候原样说出来**，前面什么都不加：批量下载那一层编好的
// 错已经是一整句（「已加进 3 首的下载队列，另有 2 首没加进去：…」），而加上
// 一句「下载不了：」会把它读成「一首都没进去」。
func downloadDoneText(err error) string {
	switch {
	case err == nil:
		return "下载任务已添加"
	case app.DownloaderMissing(err):
		return "下载还没挂上：这个应用没把下载服务装进来"
	}
	return err.Error()
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
