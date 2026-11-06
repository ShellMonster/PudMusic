// 「喜欢歌单绑定」这一摊弹窗：第一次点喜欢时的三选一、挑已有歌单、
// 新建歌单，加上设置页里那一行「喜欢歌单」。
//
// # 它和「添加到收藏夹」那个多选框是两条路
//
// 列表行上那颗心是「喜欢」——进**绑定的那一个**收藏夹，不弹多选框；
// 右键菜单里的「收藏」照旧弹多选框（`folderselect.go`）。两条路最后都
// 落在 `resource/deal`，区别只在「放进哪一个格子」。
//
// # 状态机
//
// `likeBind.step` 只有三档：
//
//	choose  三选一：创建新的 / 选择已有歌单 / 暂时不用（→ 收藏夹多选框）
//	pick    挑一个已有歌单（绑定 + 把这一首放进去；从设置进来时只换绑定）
//	create  起名 + 公开/私密 → 建出来、绑上、把这一首放进去
//
// 三个档是**同一个弹窗**换内容，不是三个弹窗叠起来——叠起来的话「返回」
// 那个按钮要解开两层模态，而原型里它是「回到三选一」。
//
// # 挂在哪
//
// `s.likeBind` 是弹窗状态（只由界面线程读写，铁律一）；`s.likePending`
// 是「正在替哪一首办这件事」——绑定三档走完都要把这一首放进新绑的格子，
// 而「从设置进来换绑定」那一档是**只换绑定不收歌**（likePending 为 nil）。
//
// 后台结果从 `TakeLikedResult` 回来（`flushLikedResults`，在
// `viewFolderDialogs` 里每帧开头收一次）。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/rowkey"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// likeBindStep 是弹窗画到第几档。
type likeBindStep int

const (
	likeBindChoose likeBindStep = iota // 三选一
	likeBindPick                       // 挑已有歌单
	likeBindCreate                     // 新建
)

// likeBind 是「给喜欢找一个专属歌单」这个弹窗此刻的全部状态。
type likeBind struct {
	open bool
	step likeBindStep

	// rid / contentType 是 pending 那一首在 B 站那边的身份（和选择弹窗
	// 同一对）。pending 为 nil 时是「从设置进来换绑定」，不收歌。
	rid         int64
	contentType int

	// list / loaded 是「我创建的」那份列表（pick 档用）。
	list   []bili.CreatedFolder
	loaded bool
	// loadErr 是 pick 档拉列表失败要说的那句（`FolderOpListLoad`
	// 回来的结果）。非空时画「拉不到 + 重试」，不许永远停在
	// 「正在拉收藏夹…」。
	loadErr string
	// picked 是挑中的那一个 id。
	picked int64

	// name / public 是 create 档的表单。
	name   string
	public bool
	// touched 是名称框被碰过没有（和 folderEdit 同一个约定：没碰过不画
	// 「必填」那句红的）。
	touched bool

	// submitting 是「这一趟还在飞」（建 / 绑 + 收）。
	submitting bool

	// ls / rowKeys 是 pick 档那个列表的 ListState 和预先装箱好的行身份
	// （理由同 folderSelect 那两个字段）。
	ls      ui.ListState
	rowKeys rowkey.Memo[int64, any]
}

// likePending 是「正在替哪一首办绑定」。
//
// ⚠️ **它只记这一首是谁**：rid 已经问出来（绑定三档走完要用它收歌），
// Item 留着是为了「绑定失效要回到多选框」那一次还认得是哪一首；
// token 是 `FavoriteIntoBound` 发出去那一趟领的号——回来的结果得拿它
// 对号，多首快速喜欢 / 旧结果乱序时不能拿「最新一条 pending」去处理
// 一条早发出的回执。
type likePending struct {
	item  app.Item
	rid   int64
	typ   int
	token uint64
}

// ── 入口 ─────────────────────────────────────────────────────────────────

// likeItem 是列表行上那颗「喜欢」心做的事。
//
//   - **绑定了** → 直接收进绑定的那一格（后台 `FavoriteIntoBound`，
//     结果走 `TakeLikedResult`）。
//   - **没绑过** → 开三选一那个弹窗。
//
// ⚠️ 和 `openFolderSelect` 一样要先把稿件 id 问出来（收藏接口要 avid，
// 大多数列表只给 bvid）——那一趟是后台的，见 `askFavoriteIDThen`。
func (s *Shell) likeItem(item app.Item) {
	if !s.app.CollectionsMounted() {
		// ⚠️ **不许说「还没接接口」**（票 #242）。
		s.addToast("收藏夹现在打不开", notice.Warning)
		return
	}
	if rid, typ, ok := favTarget(item); ok {
		s.likeItemResolved(item, rid, typ)
		return
	}
	s.askLikeIDThen(item)
}

// resolveFavorite 是「问 avid」那一趟在界面这一层的样子：
// 发出去（`app.ResolveFavoriteID`），把回调按**那一趟的号**存起来，
// 等帧循环（`flushFavoriteResolutions`）取走结果时对号去跑。
//
// 🔴 **回调不许交给后台那条线程**：`app.ResolveFavoriteID` 的 done 改的是
// UI-owned 状态（`s.likePending` / `s.likeBind` / `s.toasts`），后台直接调
// 它就是一次数据竞争——所以结果只入队，应用结果由帧循环来做。token
// 是 0 的那些（Sid 已有 / 没挂上 / Bvid 为空）是**同步**那一半：它们就在
// 这一帧上，done 照旧交给 app 层当场跑（`likeDone`）。
//
// ⚠️ token 必须进 key：多首在飞、旧结果乱序回来时，「最新那次回调」
// 不能拿去处理一条早发出的结果——和 `likePending.token` 是同一个理由。
func (s *Shell) resolveFavorite(item app.Item, done func(app.Item, error)) {
	token := s.app.ResolveFavoriteID(item, done)
	if token == 0 {
		return // 同步那一半已经跑完了
	}
	if s.resolveCallbacks == nil {
		s.resolveCallbacks = map[uint64]func(app.Item, error){}
	}
	s.resolveCallbacks[token] = done
}

// flushFavoriteResolutions 取走解析结果邮箱里的那一条，对号去跑回调。
//
// 每帧开头调一次（在 `viewFolderDialogs` 里，和 `flushLikedResults` 并排）。
//
// ⚠️ **回调不存在的也照样取走**：token 对不上的那一条属于「已经收场的
// pending」——留在邮箱里下一次还会被取出来、还是对不上（和
// `likedResultMatchesPending` 的对号逻辑是同一个判据：认不出就丢）。
func (s *Shell) flushFavoriteResolutions() {
	for {
		res, ok := s.app.TakeFavoriteResolution()
		if !ok {
			return
		}
		done := s.resolveCallbacks[res.Token]
		delete(s.resolveCallbacks, res.Token)
		if done != nil {
			done(res.Item, res.Err)
		}
	}
}

// likeCurrent 是播放器上那颗「喜欢」心做的事：满屏、迷你窗、刘海上的
// 同一枚心形都走它——和列表行上那颗心是**同一条绑定流程**
// （`likeItem`），不是「添加到收藏夹」那个多选框（`favoriteCurrent`）。
//
// # 它和 `favoriteCurrent` 是两条路
//
//   - `likeCurrent`（这颗心）→ **喜欢歌单绑定流程**：未绑三选一、已绑
//     直加、绑定失效回落多选框；
//   - `favoriteCurrent`（菜单「添加到收藏夹…」/ 快捷键）→ **收藏夹多选框**。
//
// 早一版这颗心走的是 `favoriteCurrent`——播放器上的心按下去弹的是多选
// 框，绑定流程压根进不来，「绑定一个专属歌单」这件事只有列表行办得到。
// 那一版是本票要拦的病：同一颗心形在五个入口两种行为。
//
// ⚠️ **弹窗要落在主壳上**：`viewFolderDialogs` 只在主窗口那一支调——从
// 迷你 / 刘海那一扇直接 `s.likeItem` 会把 `likeBind` 写进副壳自己那一份，
// 那一扇永远不画它（静默失败）。所以下面路由到主壳（`s.main`，主壳自己
// 调时 `s.main` 是 nil、落回 `s`），理由同 `favoriteCurrent` 那段。
//
// ⚠️ **提示也要落在画 toast 的那一扇上**（同 `favoriteCurrent`）：迷你窗
// 和刘海窗都不画 toast，「还没有取回来」那句得写进主壳的 toasts。
func (s *Shell) likeCurrent() {
	target := s
	if s.main != nil {
		target = s.main
	}
	it, ok := s.app.PlayingItem()
	if !ok {
		target.addToast("这一首还没有取回来，收藏不了", notice.Warning)
		return
	}
	// ⚠️ **主窗的满屏要收掉**：绑定弹窗排在 `viewFullPlayer` **之前**建，
	// 满屏盖在它上面——满屏开着时这个弹窗用户看不见（票 #706 的坑）。
	if target.full.open {
		target.closeFullPlayer()
	}
	target.likeItem(it)
}

// likeItemResolved 是「身份已经有了」之后那一半：绑定了直接写，没绑先问。
//
// 单独一个函数和 `openFolderSelectNow` 同一条理由：回调里调的必须是它，
// 递归在结构上就不成立（见 `askFavoriteIDThen` 文件头那段）。
func (s *Shell) likeItemResolved(item app.Item, rid int64, typ int) {
	if _, bound := s.app.LikedFolder(); bound {
		s.likePending = &likePending{item: item, rid: rid, typ: typ,
			token: s.app.FavoriteIntoBound(rid, typ)}
		return
	}
	s.openLikeBind(&likePending{item: item, rid: rid, typ: typ})
}

// askLikeIDThen 是「favTarget 认不出来」那一条：先在后台问一次稿件 id，
// 认得出来了再走 `likeItemResolved`。
//
// ⚠️ **回调里不许调 `likeItem`**——同 `askFavoriteIDThen` 那段：递归的
// 保证是**结构**给的（这里调的是不带兜底的 `likeItemResolved`），不是靠
// 判据。死循环的教训见 `askFavoriteIDThen` 的文件头。
func (s *Shell) askLikeIDThen(item app.Item) {
	if item.Source != "online" || item.Bvid == "" {
		s.addToast("这一首还没有可以收藏的身份", notice.Warning)
		return
	}
	s.resolveFavorite(item, func(fixed app.Item, err error) {
		switch {
		case err != nil:
			s.addToast("收藏不了："+bili.Friendly(err), notice.Danger)
		default:
			rid, typ, ok := favTarget(fixed)
			if !ok {
				s.addToast("这一首还没有可以收藏的身份", notice.Warning)
				break
			}
			s.likeItemResolved(fixed, rid, typ)
		}
		s.wake.invalidate()
	})
}

// openLikeBind 把弹窗摆出来，停在「三选一」那一档。
//
// pending 为 nil 是「从设置进来换绑定」：那时候没有歌要收，pick 那一档
// 选完只改设置。
func (s *Shell) openLikeBind(pending *likePending) {
	b := &likeBind{open: true, step: likeBindChoose, name: "我的喜欢"}
	if pending != nil {
		b.rid, b.contentType = pending.rid, pending.typ
	}
	s.likeBind = b
	s.likePending = pending
}

// openLikePick 直接停在「挑已有歌单」那一档（三选一的「选择已有歌单」
// 和设置里的「更换」都走它）。
//
// ⚠️ **rid=0 拉列表**：选择弹窗那份列表带「这一首在不在里面」的勾选，
// 换绑定要的是「有哪些格子」，不带 fav_state。
func (s *Shell) openLikePick(b *likeBind) {
	b.step = likeBindPick
	b.picked = 0
	b.loadErr = ""
	s.app.LoadCreatedFolders(0)
}

// ── 每一帧的同步 ─────────────────────────────────────────────────────────

// syncLikeBind 换掉 pick 档那份列表（created/list 拉回来之后）。
//
// ⚠️ **勾选不跟着换**：用户可能已经挑了一个，接口回来的那份列表不能
// 把那一挑盖掉（同 `syncFolderList`）。
func (s *Shell) syncLikeBind(b *likeBind) {
	list, loaded := s.app.CreatedFolderChoices(0)
	if !loaded && len(list) == 0 {
		return
	}
	b.list, b.loaded = list, loaded
	b.ls.Key = func(i int) any { return b.rowKeys.At(i, list[i].ID, rowkey.Identity64) }
	b.ls.Label = func(i int) string { return list[i].Title }
}

// ── 视图 ─────────────────────────────────────────────────────────────────

// viewLikeBind 是这个弹窗。
//
// 🔴 外面包着 Guard（铁律四），挂在 `viewFolderDialogs` 里。
func (s *Shell) viewLikeBind(c *ui.Context) {
	b := s.likeBind
	if b == nil || !b.open {
		return
	}

	modal := ui.DialogBase(c, &b.open, func(backdrop, panel ui.Element) {
		switch b.step {
		case likeBindPick:
			s.viewLikeBindPick(c, b, backdrop, panel)
		case likeBindCreate:
			s.viewLikeBindCreate(c, b, backdrop, panel)
		default:
			s.viewLikeBindChoose(c, b, backdrop, panel)
		}
	})

	// 🔴 提交中不许误关（同 folderselect.go 那一道闸）。
	if modal.Dismissed() && b.submitting {
		b.open = true
	}
}

// viewLikeBindChoose 是三选一那一档（原型 `firstDialog`）。
func (s *Shell) viewLikeBindChoose(c *ui.Context, b *likeBind, backdrop, panel ui.Element) {
	dlgPanel(c, backdrop, panel, "给喜欢找一个专属歌单", dlgSheetW)
	dlgBody(c, "给喜欢找一个专属歌单", "喜欢的歌曲会自动加入，之后可在设置里更换。", func() {})
	dlgFoot(c, nil, func() {
		if pud.Button(c, "likebind:skip", pud.Quiet, pud.Std, "暂时不用", "").Clicked() {
			s.skipLikeBind(b)
		}
		if pud.Button(c, "likebind:existing", pud.Ghost, pud.Std, "选择已有歌单", "").Clicked() {
			s.openLikePick(b)
		}
		if pud.Button(c, "likebind:create", pud.Primary, pud.Std, "创建新的", "").Clicked() {
			b.step = likeBindCreate
		}
	})
}

// viewLikeBindPick 是「挑已有歌单」那一档。
//
// ⚠️ **从设置进来时标题是「更换喜欢歌单」**（原型同一个弹窗两种语境），
// 脚下也只有「绑定」——那时候没有歌要收，不画「并加入」。
func (s *Shell) viewLikeBindPick(c *ui.Context, b *likeBind, backdrop, panel ui.Element) {
	pending := s.likePending != nil
	title := "选择喜欢歌单"
	if !pending {
		title = "更换喜欢歌单"
	}
	dlgPanel(c, backdrop, panel, title, dlgSheetW)
	dlgBody(c, title, "喜欢的歌曲会加入所选歌单。", func() {
		pl := tok.Of(c.Theme())
		switch {
		case b.loadErr != "":
			ui.Row(c).AlignItems(ui.Center).Gap(space.Px8).
				Margin(space.Px16, space.None, space.None, space.None).Children(func() {
				ui.Text(c, b.loadErr).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Err).Shrink(1)
				if pud.Button(c, "likebind:retry", pud.Ghost, pud.Sm, "重试", "").Clicked() {
					b.loadErr = ""
					s.app.LoadCreatedFolders(0)
				}
			})
		case !b.loaded && len(b.list) == 0:
			ui.Text(c, "正在拉收藏夹…").FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text3).
				Margin(space.Px16, space.None, space.None, space.None)
		case len(b.list) == 0:
			ui.Text(c, "你还没有创建收藏夹").FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text3).
				Margin(space.Px16, space.None, space.None, space.None)
		default:
			pickScroll := ui.Scroll(c).FillWidth().MaxHeight(280).Margin(space.Px16, space.None, space.None, space.None).
				Children(func() {
					ui.List(c, &b.ls, len(b.list), func(i int) { s.viewLikeBindRow(c, b, i) }).GapY(space.Px4)
				})
			s.trackScrollbar(pickScroll)
		}
	})
	dlgFoot(c, nil, func() {
		if pending {
			if pud.Button(c, "likebind:back", pud.Quiet, pud.Std, "返回", "").Clicked() {
				b.step = likeBindChoose
			}
		} else if pud.Button(c, "likebind:cancel", pud.Ghost, pud.Std, "取消", "").Clicked() {
			s.closeLikeBind()
		}
		confirm := "绑定并加入"
		if !pending {
			confirm = "绑定"
		}
		if b.submitting {
			confirm = "提交中…"
		}
		if pud.Button(c, "likebind:ok", pud.Primary, pud.Std, confirm, "").Disabled(b.picked <= 0 || b.submitting).Clicked() {
			s.submitLikePick(b)
		}
	})
}

// viewLikeBindRow 是挑歌单那一档里的一行：封面 + 名字 + 几首 · 公开/私密。
// 选中行是 accent-soft 底 + 尾巴上一个勾。
func (s *Shell) viewLikeBindRow(c *ui.Context, b *likeBind, i int) {
	pl := tok.Of(c.Theme())
	item := b.list[i]
	on := b.picked == item.ID

	row := ui.Row(c).Key(likePickRowKey(item.ID)).FillWidth().AlignItems(ui.Center).Gap(space.Px12).
		Padding(space.Px8).Radius(tok.R8)
	if on {
		row = row.Background(pl.AccentSoft)
	} else if row.Hovered() {
		row = row.Background(pl.Hover)
	}
	row.Children(func() {
		ui.Box(c).Size(32, 32).Radius(tok.R4).Clip().Background(pl.Surface2).Shrink(0).
			AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if item.Cover != "" {
				s.collCoverDraw(item.Cover)(c, 32, 32)
			} else {
				ui.Icon(c, pud.Icon("folder")).FontSize(16).TextColor(pl.Text3)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, item.Title).FontSize(scale.Font(c, scale.Size14)).FontWeight(500).TextColor(pl.Text).
				SingleLine().Ellipsis("…")
			ui.Text(c, selectRowSubtitle(item)).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).SingleLine()
		})
		if on {
			ui.Text(c, "✓").FontSize(scale.Font(c, scale.Size14)).TextColor(pl.Accent)
		}
	})
	if row.Clicked() {
		b.picked = item.ID
	}
}

// likePickRowKey 是挑歌单那一档里一行的 key（铁律四：用收藏夹 id）。
func likePickRowKey(id int64) string {
	return "liked-pick-row:" + strconv.FormatInt(id, 10)
}

// viewLikeBindCreate 是「新建喜欢歌单」那一档：名称 + 公开/私密。
func (s *Shell) viewLikeBindCreate(c *ui.Context, b *likeBind, backdrop, panel ui.Element) {
	pl := tok.Of(c.Theme())
	dlgPanel(c, backdrop, panel, "新建喜欢歌单", dlgSheetW)
	dlgBody(c, "新建喜欢歌单", "名称可改，绑定会保留。", func() {
		ui.Column(c).Gap(space.Px16).Margin(space.Px16, space.None, space.None, space.None).Children(func() {
			ui.Column(c).Gap(space.Px6).Children(func() {
				ui.Row(c).Gap(space.Px4).Children(func() {
					ui.Text(c, "名称").FontSize(scale.Font(c, scale.Size13)).FontWeight(500).TextColor(pl.Text)
					ui.Text(c, "*").FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Err)
				})
				name := ui.TextInput(c, &b.name).Placeholder("我的喜欢").Disabled(b.submitting).
					FillWidth().Height(40)
				name.Label("名称")
				if name.Changed() {
					b.touched = true
				}
				if strings.TrimSpace(b.name) == "" && !b.submitting && b.touched {
					ui.Text(c, "名称为必填项").FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Err)
				}
			})
			ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
				priv := !b.public
				if pud.Switch(c, "likebind:priv", &priv, "私密歌单") {
					b.public = !priv
				}
				ui.Column(c).Children(func() {
					ui.Text(c, "私密歌单").FontSize(scale.Font(c, scale.Size14)).FontWeight(500).TextColor(pl.Text)
					hint := "公开：别人能在你的空间看到"
					if priv {
						hint = "只有你自己能看到"
					}
					ui.Text(c, hint).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3)
				})
			})
		})
	})
	dlgFoot(c, nil, func() {
		if pud.Button(c, "likebind:create-cancel", pud.Ghost, pud.Std, "取消", "").Disabled(b.submitting).Clicked() {
			if s.likePending != nil {
				b.step = likeBindChoose
			} else {
				s.closeLikeBind()
			}
		}
		label := "创建并加入"
		if b.submitting {
			label = "提交中…"
		}
		if dlgPrimary(c, "likebind:create-ok", label, b.submitting,
			strings.TrimSpace(b.name) == "") {
			s.submitLikeCreate(b)
		}
	})
}

// ── 三档各自的提交 ────────────────────────────────────────────────────────

// skipLikeBind 是「暂时不用」：回到原来的收藏夹多选框，不建立绑定。
//
// ⚠️ **弹的是多选框不是直接关掉**：用户点的是「喜欢」这颗心，「暂时不用」
// 的意思是「这次不绑定」——不是「这次不喜欢」。
func (s *Shell) skipLikeBind(b *likeBind) {
	pending := s.likePending
	s.closeLikeBind()
	if pending != nil {
		s.openFolderSelectNow(pending.item, pending.rid, pending.typ)
	}
}

// submitLikePick 把挑中的那个歌单绑上；有 pending 时顺手把这一首放进去。
//
// ⚠️ **先绑再收**：绑定是本地设置，写成功是立刻的；收歌是后台那一趟
// （`FavoriteIntoBound` 自己读绑定），所以它排在绑定之后。
func (s *Shell) submitLikePick(b *likeBind) {
	if b.submitting || b.picked <= 0 {
		return
	}
	var chosen *bili.CreatedFolder
	for i := range b.list {
		if b.list[i].ID == b.picked {
			chosen = &b.list[i]
			break
		}
	}
	if chosen == nil {
		return
	}
	s.app.BindLikedFolder(app.LikedFolder{ID: chosen.ID, Title: chosen.Title, Private: chosen.Private})
	if s.likePending != nil {
		b.submitting = true
		s.likePending.token = s.app.FavoriteIntoBound(b.rid, b.contentType)
		return
	}
	s.closeLikeBind()
	s.addToast("已绑定「"+chosen.Title+"」", notice.Success)
}

// submitLikeCreate 建一个新歌单：名字 + 公开/私密 → `app.CreateFolder`。
//
// 建成之后（`flushFolderResults` 里 likeBind 那一支）绑上它、再把 pending
// 那一首放进去。
func (s *Shell) submitLikeCreate(b *likeBind) {
	name := strings.TrimSpace(b.name)
	if b.submitting || name == "" {
		return
	}
	b.submitting = true
	privacy := 1 // B 站原值：0 公开、1 私密（FolderDraft 注释）
	if b.public {
		privacy = 0
	}
	s.app.CreateFolder(bili.FolderDraft{Title: name, Privacy: privacy})
}

// closeLikeBind 关掉弹窗并清掉 pending。
func (s *Shell) closeLikeBind() {
	s.likeBind = nil
	s.likePending = nil
}

// ── 后台结果 ─────────────────────────────────────────────────────────────

// flushLikedResults 取走后台攒下的喜欢结果。
//
// 每帧开头调一次（在 `viewFolderDialogs` 里，和 `flushFolderResults` 并排）。
// 三种结果：成了 → 一句话；普通失败 → 一句话 + 解除弹窗的「提交中」；
// **绑定失效** → 解绑 + 回到收藏夹多选框（那一档和普通失败是两件事，
// 见 `app/likedfolder.go` 的文件头）。
//
// 🔴 **结果必须对回发出那一趟的 pending**：多首快速喜欢时几条结果会
// 排着队回来，而 `s.likePending` 只存**最新**那一首——拿「最新的
// pending」去处理一条早发出的 Missing，会拿错的 item / rid 开多选框
// （把歌 B 的结果当成歌 A 的判决）。所以每条结果带发出那一趟的
// token + rid，**对不上就丢**，留给真正属于它的那次 flush 去处理。
func (s *Shell) flushLikedResults() {
	res, ok := s.app.TakeLikedResult()
	if !ok {
		return
	}
	b := s.likeBind
	if b != nil && b.submitting {
		b.submitting = false
	}
	switch {
	case res.OK:
		if !s.likedResultMatchesPending(res) {
			return
		}
		s.addToast(res.Text, notice.Success)
		s.closeLikeBind()
	case res.Missing:
		if !s.likedResultMatchesPending(res) {
			return
		}
		// 绑定的那一格已经没了：解绑，回到多选框把这一首收进去。
		s.app.UnbindLikedFolder()
		s.addToast(res.Text, notice.Warning)
		if s.likePending != nil {
			p := s.likePending
			s.likePending = nil
			s.openFolderSelectNow(p.item, p.rid, p.typ)
		}
		s.likeBind = nil
	default:
		// 普通失败也要对号：弹窗那一帧的「提交中」解除不解除看它，
		// 但提示本身是逐条的、发到就是发到。
		if !s.likedResultMatchesPending(res) {
			return
		}
		s.addToast(res.Text, notice.Danger)
	}
}

// likedResultMatchesPending 判这条回执是不是属于**此刻** pending 那一趟。
//
// 两个判据都要对上：token 是发出那一趟领的号（最快区分「哪一趟」），
// rid 是替哪一首（token 为 0 的旧路径 / 没 pending 时靠它兜底）。
// pending 为 nil（从设置进来换绑、或失败支刚清掉）时：token 对不上
// 就丢——那条结果属于「上一首」或者「已经收场的 pending」。
func (s *Shell) likedResultMatchesPending(res app.LikedResult) bool {
	p := s.likePending
	if p == nil {
		return res.Token == 0
	}
	if res.Token != 0 && p.token != 0 {
		return res.Token == p.token
	}
	return res.Rid == p.rid
}
