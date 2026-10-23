// 「把这一首放进哪些收藏夹」那个弹窗（Biu 的
// `components/favorites-select-modal`）。
//
// # 它和头图区那个星标不是一件事
//
// 歌单页上那个 ★ 收藏的是**这个收藏夹**（收藏别人的歌单）。这个弹窗是反过来
// 的：把**一条内容**放进我自己创建的那些收藏夹。两者请求的是完全不同的两个
// 接口（`folder/fav` 与 `resource/deal`），所以它们不共用任何状态。
//
// # 三个要点都是这一票的验收
//
//   - **分页拉全量**（`app.LoadCreatedFolders`，每页 100）。B 站这个接口一页
//     最多 100（见 `bili.CreatedFolderPageSize`），而用户的收藏夹通常十几个——
//     一页就够。翻页那一层在 app 层，因为铁律三不许界面线程发网络请求。
//   - **多选 + 全选**。初始勾选来自接口的 `fav_state`（这一条已经在哪些收藏夹
//     里），全选是**当前列表里的每一行**。
//   - **提交是异步的**。点确认之后弹窗**不关**、按钮转圈，等后台那一趟回来
//     才关（和「提交中不能被误关」是同一道闸的两半）。

package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// folderSelect 是这个弹窗此刻的全部状态。**只由界面线程读写**（铁律一）。
type folderSelect struct {
	open bool

	// rid 是要放进哪些收藏夹的那条内容的 id。
	rid int64
	// contentType 是 B 站的内容类型（2 视频 / 12 音频，见 `bili.Media`）。
	contentType int
	// title 是弹窗顶上写的那句「把《…》放到…」。
	title string

	// list 是拉回来的「我创建的收藏夹」。**列表在 background 换掉，而勾选
	// 不跟着换**（见 `onList`）。
	list []bili.CreatedFolder
	// loaded 是翻完了没有。
	loaded bool
	// picked 是勾上的那些收藏夹 id。
	//
	// ⚠️ **它是 map 不是切片**：勾选是「按一下翻一下」，而界面每一帧都要读它
	// （画勾）。切片每次翻一下都要重新分配，一帧一次。
	picked map[int64]bool
	// submitting 是「这一趟还在飞」。
	submitting bool

	// ls 是这个列表的 `ListState`。**每行一个 key**（见 `onList`）。
	ls ui.ListState
}

// openFolderSelect 是「把这一首放进哪些收藏夹」那个入口。
//
// 它自己**只调一次**解析（`askFavoriteIDThen`），而解析那一趟回来之后走的是
// `openFolderSelectNow`——**不是这里**。这两步分开不是洁癖，是下面那条命名的
// 唯一保证。
func (s *Shell) openFolderSelect(item app.Item) {
	// ⚠️ **没挂上就别开弹窗**：理由同 `openFolderEdit`（提交那一下会什么
	// 都不发生，而用户看到的是「点了没反应」）。
	if !s.app.CollectionsMounted() {
		// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）。
		s.addToast("收藏夹现在打不开", notice.Warning)
		return
	}
	if rid, typ, ok := favTarget(item); ok {
		s.openFolderSelectNow(item, rid, typ)
		return
	}
	s.askFavoriteIDThen(item)
}

// openFolderSelectNow 是真的把弹窗摆出来。**认不出身份的人不许调它**——
// 它没有兜底那一支，所以拿不到 rid 的人必须先在 `openFolderSelect` 里被拦住。
//
// 把它单独拎出来就是**为了让递归在结构上不成立**（理由见 `askFavoriteIDThen`）。
func (s *Shell) openFolderSelectNow(item app.Item, rid int64, typ int) {
	f := &folderSelect{
		open: true, rid: rid, contentType: typ,
		title:  "把《" + firstNonEmpty(item.Title, "这一首") + "》放到收藏夹",
		picked: map[int64]bool{},
	}
	// 先摆上上一份（打开过的话），这样第二次打开不是从空白开始——网络慢的时候
	// 一片空白比一份可能已经过期的列表好懂。
	if prev := s.folderSelect; prev != nil && prev.rid == rid {
		f.list, f.loaded = prev.list, prev.loaded
		f.picked = prev.picked
	}
	s.folderSelect = f
	s.app.LoadCreatedFolders(rid)
}

// askFavoriteIDThen 是「`favTarget` 认不出来」那一条：先在后台问一次稿件 id，
// 认得出来了就开弹窗。
//
// # 为什么不能直接说「这一首还没有可以收藏的身份」
//
// B 站的收藏接口收的是 **avid**，而**大多数列表接口都不给 avid**：搜索结果、
// 历史记录、UP 空间的投稿都只有 bvid（收藏夹那一页给的 `bili.Media.ID` 就是
// avid，所以那一页不走这里）。一句话打发掉的话，「收藏」在这些页面上就永远是
// 死的——而它其实只差一次 bvid → avid 的查询。
//
// ⚠️ **bvid → avid 是一次网络往返**，而界面线程上一次都不许有（铁律三）。所以
// 这里是后台 + 回调，弹窗在那之后才开。
//
// # 🔴 不许递归，而这条保证是**结构**给的，不是靠判据
//
// 回调里调的是 `openFolderSelectNow`，**不是 `openFolderSelect`**——所以这一趟
// 不可能再走回 `askFavoriteIDThen`，一环就到底了。
//
// 早一版回调里调的是 `openFolderSelect`，守卫只有 `item.Bvid == "" || isLocal`，
// 而 `favTarget` 真正卡人的是 **`item.Source == "online"`**。于是「Source 不是
// online、Bvid 非空、Sid 为空」的那一项会这样：
//
//	openFolderSelect → favTarget 失败 → askFavoriteIDThen → 问出 Sid 填上
//	  → openFolderSelect → **favTarget 还是失败（Source 没变）**
//	  → askFavoriteIDThen → 这一趟 Sid 已经有了，ResolveFavoriteID **同步**返回
//	    → openFolderSelect → ……
//
// 零网络开销的紧递归，几百毫秒内把 goroutine 栈撑到 1 GB，`fatal error: stack
// overflow`——**Go 里那个不可 recover，整个进程死掉**。今天所有 Item 都填了
// Source，所以够不着；那是运气，不是设计。
//
// 下面那道 `Source != "online"` 的守卫是第二道（它让上面那种条目当场就说清），
// 但**保证不靠它**——把它删掉也不会有递归。
func (s *Shell) askFavoriteIDThen(item app.Item) {
	if item.Source != "online" || item.Bvid == "" {
		s.addToast("这一首还没有可以收藏的身份", notice.Warning)
		return
	}
	s.app.ResolveFavoriteID(item, func(fixed app.Item, err error) {
		switch {
		case err != nil:
			s.addToast("收藏不了："+bili.Friendly(err), notice.Danger)
		default:
			rid, typ, ok := favTarget(fixed)
			if !ok {
				// 问回来了还是认不出（协议变了，或者 B 站给了一个空 id）。
				// **到这里就停**——再调 `openFolderSelect` 就是那条死循环。
				s.addToast("这一首还没有可以收藏的身份", notice.Warning)
				break
			}
			s.openFolderSelectNow(fixed, rid, typ)
		}
		s.wake.invalidate()
	})
}

// favTarget 从一个队列项上认出「B 站那边的 id 和类型」。
//
// ⚠️ **本地文件没有这一对**：本地歌不在 B 站上，收藏不了。真给它一个 0，
// B 站会回 -400，而用户看到的是「收藏失败」。
func favTarget(item app.Item) (rid int64, contentType int, ok bool) {
	if item.Source != "online" || item.Sid == "" {
		return 0, 0, false
	}
	rid, err := strconv.ParseInt(item.Sid, 10, 64)
	if err != nil || rid <= 0 {
		return 0, 0, false
	}
	// Type 是 "mv"（视频）还是 "audio"（见 `onlineItemOf`）。**认不出来的按
	// 视频走**：Sid 对这两类都带着，而 B 站的 `resource/deal` 只认这两个类型，
	// 填错了它不报错，只是把这一条放错地方。
	if item.Type == "audio" {
		return rid, bili.MediaAudio, true
	}
	return rid, bili.MediaVideo, true
}

func firstNonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// ── 视图 ───────────────────────────────────────────────────────────────────

// viewFolderSelect 是这个弹窗。
//
// 🔴 外面包着 Guard（铁律四）。它挂在 `viewMain` 的最后，整页的输入都被遮罩
// 吃掉，所以它自己坏了坏掉的是整个窗口。
func (s *Shell) viewFolderSelect(c *ui.Context) {
	f := s.folderSelect
	if f == nil || !f.open {
		return
	}
	t := c.Theme()

	modal := ui.Modal(c, &f.open, func() {
		// ⚠️ **字重不写**（400）：弹窗标题是 400（DESIGN.md §二）。
		ui.Text(c, f.title).FontSize(scale.Font(c, 14)).MaxLines(2).Ellipsis("…")

		// 「全选」那一行。⚠️ 判据是**当前列表里的每一行都勾上了**（严格长度
		// 相等），理由和批量操作条那一处一样：`collectionbatch.go` 的
		// `allSelected`。在这里换成「有没有一个没勾」的话，用户勾到一半时
		// 那一格会是「全不选」的样子，而不是「部分选中」。
		all := f.allSelected()
		ui.Row(c).Gap(space.Px10).Center().Children(func() {
			if ui.Checkbox(c, &all, "全选").Label("全选").Disabled(f.submitting).Changed() {
				f.toggleAll(!f.allSelected())
			}
			ui.Text(c, "已选 "+strconv.Itoa(len(f.picked))+" 个收藏夹").
				FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Text(c, selectListHint(f)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		})

		if !f.loaded && len(f.list) == 0 {
			ui.Text(c, "正在拉收藏夹…").FontSize(scale.Font(c, 13)).TextColor(t.TextMuted).Padding(space.Px12)
		} else if len(f.list) == 0 {
			ui.Text(c, "你还没有创建收藏夹").FontSize(scale.Font(c, 13)).TextColor(t.TextMuted).Padding(space.Px12)
		} else {
			pickScroll := ui.Scroll(c).Height(300).Children(func() {
				ui.List(c, &f.ls, len(f.list), func(i int) { s.viewFolderPickRow(c, f, i) })
			})
			s.trackScrollbar(pickScroll)
		}

		ui.Row(c).Gap(space.Px8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Disabled(f.submitting).Clicked() {
				s.closeFolderSelect()
			}
			// ⚠️ **提交中那一档是「转起来 + 灰下去」**（票 #178）：原来只是把字
			// 换成「提交中…」，而 Biu 那个 `<AsyncButton isLoading>` 画的是一枚
			// **转圈**的按钮——「在忙」这件事只靠字说，没有一个能一眼看出来的
			// 样子。共用的那一档见 `loading.go` 的 `busyPrimaryButton`。
			if f.submitting {
				busyPrimaryButton(c, confirmLabel(true))
				return
			}
			sub := ui.PrimaryButton(c, confirmLabel(false)).Disabled(!f.changed())
			if sub.Clicked() {
				s.submitFolderSelect(f)
			}
		})
	})

	// 🔴 提交中不许误关。理由和编辑弹窗那一道一样（见 folderedit.go）。
	if modal.Dismissed() && f.submitting {
		f.open = true
	}
}

func confirmLabel(busy bool) string {
	if busy {
		return "提交中…"
	}
	return "确认"
}

// selectListHint 是右上角那一小行。
//
// ⚠️ **有内容但没勾时要说清楚「已经收藏在哪儿」**：用户点开弹窗看到一片没勾
// 会以为这首歌没被收藏过，于是再收藏一遍——而 B 站那边接口的初始 `fav_state`
// 已经告诉他收藏在哪儿了，只是没勾出来（比如那一行需要往下滑）。
func selectListHint(f *folderSelect) string {
	if !f.loaded {
		return "正在拉全量…"
	}
	if n := len(f.picked); n > 0 {
		return "已收藏在 " + strconv.Itoa(n) + " 个收藏夹里"
	}
	return "还没有收藏在任何收藏夹里"
}

// viewFolderPickRow 是列表里的一行：一个勾 + 名字 + 几个内容。
//
// ⚠️ **点整行等于点那个勾**（`row.Clicked()`）：Biu 那边整行是一个 `role=button`
// 的 div，行内才是那个勾，而点行内不会连带外面那一次（外面那一次被 stop 了）。
// 少一行的话用户得瞄着那个小方框点，而弹窗里一列都是这种行——瞄错的次数
// 不少，症状是「有时候勾不上」。
func (s *Shell) viewFolderPickRow(c *ui.Context, f *folderSelect, i int) {
	t := c.Theme()
	item := f.list[i]

	row := ui.Column(c).Key(favSelectRowKey(item.ID)).Grow(1).
		Radius(6).Padding(space.Px6, space.Px8).Gap(space.Px2)
	if row.Hovered() {
		row = row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		on := f.picked[item.ID]
		ui.Row(c).Gap(space.Px8).Center().Children(func() {
			if ui.Checkbox(c, &on, item.Title).Disabled(f.submitting).Changed() {
				f.toggle(item.ID)
			}
			ui.Text(c, selectRowSubtitle(item)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		})
	})
	if row.Clicked() {
		f.toggle(item.ID)
	}
}

// selectRowSubtitle 是收藏夹名字底下那一小行。
//
// 照抄 Biu（`favorites-select-modal/index.tsx:206` 的「{n} 个内容」）。
// 私密的那几个标出来——用户同时有公开和私密的收藏夹时，界面上分不出哪几个
// 是私密的，而「我收藏到别人也能看见的地方去了」是一个要靠肉眼发现的错误。
func selectRowSubtitle(f bili.CreatedFolder) string {
	sub := strconv.Itoa(f.MediaCount) + " 个内容"
	if f.Private {
		sub += " · 私密"
	}
	return sub
}

// favSelectRowKey 是选择弹窗里一行的 key。
//
// 🔴 **用收藏夹的 id**（铁律四），不是序号。默认的元素身份是「父 ID + 兄弟
// 序号」，而这一列会变长（用户收藏到第 4 个收藏夹之后回到这一列，前面多了一
// 行）——序号整体错一位，于是**后面每一行的勾都串到别的收藏夹上**：勾了 A、
// 界面把 B 画成没勾，用户点确认，B 站那边收到的 del_media_ids 里就有 B。
//
// 和侧栏那两列的 key 不冲突：它们带的是组名（`favRowKey`），这里带的是
// `fav-sel-row:`。Tester 在同一个父底下撞 key 是直接 panic 的
// （`ui/diagnostics.go`），两个弹窗又是同一个窗口。
func favSelectRowKey(id int64) string {
	return "fav-sel-row:" + strconv.FormatInt(id, 10)
}

// ── 勾选 ───────────────────────────────────────────────────────────────────

// toggle 勾 / 取消勾一个收藏夹。
func (f *folderSelect) toggle(id int64) {
	if f.picked[id] {
		delete(f.picked, id)
		return
	}
	f.picked[id] = true
}

// toggleAll 全选 / 全不选（只作用在当前列表里的那些）。
func (f *folderSelect) toggleAll(on bool) {
	if !on {
		clear(f.picked)
		return
	}
	for _, it := range f.list {
		f.picked[it.ID] = true
	}
}

// allSelected 报是不是**当前列表里的每一个都勾上了**。
//
// 🔴 **严格长度相等**，不是集合比较——理由照抄 `collectionbatch.go` 的
// `allSelected`：集合里混进了一个已经不在列表里的 id（换了一次拉、或者以
// 前勾过、这次列表少了一条），长度可能和「全勾上」一样，而用户点确认会把
// 那一条也发出去。
func (f *folderSelect) allSelected() bool {
	if len(f.list) == 0 || len(f.picked) != len(f.list) {
		return false
	}
	for _, it := range f.list {
		if !f.picked[it.ID] {
			return false
		}
	}
	return true
}

// changed 报勾的和原来那一版有没有不一样。
//
// ⚠️ **这是「确认」按钮能不能点的判据**（Biu 的 `hasSameIds`，见
// `favorites-select-modal/index.tsx:222`）。少了它用户能对着一个什么都没改
// 的表单点确认，然后收到一句「收藏夹没有变化」——而他以为是自己收藏成功了。
func (f *folderSelect) changed() bool {
	return f.added() != nil || f.removed() != nil
}

// added 是这次要往哪些收藏夹里放。
func (f *folderSelect) added() []int64 {
	var out []int64
	for _, it := range f.list {
		if f.picked[it.ID] && !it.Faved {
			out = append(out, it.ID)
		}
	}
	return out
}

// removed 是这次要从哪些收藏夹里拿。
func (f *folderSelect) removed() []int64 {
	var out []int64
	for _, it := range f.list {
		if !f.picked[it.ID] && it.Faved {
			out = append(out, it.ID)
		}
	}
	return out
}

// ── 提交 ───────────────────────────────────────────────────────────────────

// submitFolderSelect 把勾上的提交出去。
//
// ⚠️ **立刻返回**：网络那一趟在 `app.DealFolderMedias` 的后台 goroutine 里，
// 而这里把「提交中」立起来、弹窗留着不关。
func (s *Shell) submitFolderSelect(f *folderSelect) {
	if f.submitting || !f.changed() {
		return
	}
	f.submitting = true
	s.app.DealFolderMedias(f.rid, f.contentType, f.added(), f.removed())
}

// closeFolderSelect 关掉弹窗。
//
// 提交中不许走到这里（`viewFolderSelect` 那一道闸）。
func (s *Shell) closeFolderSelect() {
	if s.folderSelect == nil {
		return
	}
	s.folderSelect.open = false
	s.folderSelect = nil
}

// syncFolderList 换掉列表，并**在换的时候重装 ListState**。
//
// ⚠️ **勾选不能跟着换**：接口回来的 `fav_state` 是「服务端此刻的样子」，
// 而用户可能已经勾了一部分（拉第二页的时候）。用服务端那份覆盖用户那份，
// 表现是「往下滚一页，刚勾的全没了」。
//
// 所以这里只更新 `list` 和初始勾选（**只在第一次**），`picked` 原样留着。
func (s *Shell) syncFolderList(f *folderSelect) {
	list, loaded := s.app.CreatedFolderChoices(f.rid)
	if !loaded && len(list) == 0 {
		return
	}
	first := len(f.list) == 0
	f.list, f.loaded = list, loaded
	if first {
		for _, it := range list {
			if it.Faved {
				f.picked[it.ID] = true
			}
		}
	}
	f.ls.Key = func(i int) any { return list[i].ID }
	f.ls.Label = func(i int) string { return list[i].Title }
}
