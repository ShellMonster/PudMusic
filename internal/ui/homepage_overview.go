// 首页「发现」总览：打开应用看到的第一屏（设计稿 docs/design-mockups/home-overview.png）。
//
// 从音乐播放器的角度重排首页：封面是主角，入口摆在第一屏，列表页退到二级。
//
//	① 大封面横幅：新歌速递的头三首，封面铺满，歌名压在底部渐变上，点一下就播。
//	② 四个入口磁贴：热榜 / 原创榜 / 最近播放 / 为你推荐，点进去是原来的那一档列表页。
//	③ 新歌速递：一排方形封面，右上角「查看更多」进「流行」那一档。
//	④ 排行榜：热榜、原创榜各取前 5 名的迷你榜，点一行就播，「查看全部」进整张榜。
//
// 数据都是首页原来就拉的那几摊（新歌速递、热榜、原创榜），没有新接口：总览只是
// 换了一种摆法。「为你推荐」「鬼畜」「流行」「最近播放」仍是 `homeTabs` 里的那几
// 档列表，总览通过 `openTab` 切过去；列表页左上角有一枚「返回发现」回到这里。
//
// ⚠️ **迷你榜的歌是缓存的**（`miniTop`）：`PagedList.Items()` 每次是一份拷贝，
// 每帧取一次就是每帧一次分配（这一屏有逐帧分配预算，见 framebudget_test.go），
// 所以只在榜上歌数变了的时候重取。
//
// ⚠️ **横幅 / 磁贴 / 封面行的列数与宽度是按上一帧量出来的容器宽算的**（`ovWidth`），
// 和新歌速递那一块同一个做法：窗口改宽之后晚一帧换。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/skeleton"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// homeHeroCount 是横幅摆几张；homeMiniRows 是迷你榜取前几名。
	homeHeroCount = 3
	homeHeroEvery = 6 * time.Second
	homeMiniRows  = 5

	// homeOverviewCellGap 是横幅、磁贴、封面行的横向缝（16）。算格宽用它；
	// 元素上的缝写 space.Px16（间距 lint 不认常量），块与块之间的竖向缝是 space.Px28。
	homeOverviewCellGap = 16

	homeTileH      = 96
	homeBoardHeroH = 112
	homeMiniRowH   = 64
	homeMiniCover  = 48
)

// homeStartsOnOverview 说应用打开时首页是不是先停在总览。生产里恒为 true；
// 列表页那一批旧测试在 init 里关掉它，直接从列表页起手。
var homeStartsOnOverview = true

// homeTileRadius 是**磁贴**（和它右侧那张插画封面）的圆角。
//
// 🔴 **插画和容器必须同一个数**（票：首页有一点圆角什么的问题）：插画调
// `imgcache.ViewPlaceholderTitled` 时把圆角传给它——原来传的是 **0**，图片
// 自己画成直角，「看起来是圆的」全靠磁贴的 `Clip()` 兜底。而插画是
// `Attach(AnchorTopRight)` **绝对定位**的孩子，外层容器的裁剪罩不住绝对定位
// 的孩子——用户撞上的就是那个：封面的右上 / 右下两个角凸出在磁贴圆角外面。
//
// ⚠️ **容器和插画共用这一个出口**：两个数写两遍的话，改了一处另一处就悄悄
// 对不上（插画比容器方一圈，或者反过来）。
func homeTileRadius(t *ui.Theme) float32 { return t.Radius + 2 }

// homeHeroRadius 是**新歌速递横幅**（和它右侧那张封面）的圆角。
//
// 理由同 `homeTileRadius`：容器和插画共用这一个出口。
func homeHeroRadius(t *ui.Theme) float32 { return t.Radius + 4 }

func homeTilePalette(t *ui.Theme, tile struct {
	tab      int
	subtitle string
	from, to ui.Color
}) (from, to, text ui.Color) {
	index := 0
	for i, candidate := range homeTiles {
		if candidate.tab == tile.tab {
			index = i
			break
		}
	}
	step := float32(0.22) + float32(0.04)*float32(index)
	if t.Dark {
		from = t.Background.Mix(t.Accent, step+0.08)
	} else {
		from = t.Text.Mix(t.Accent, step)
	}
	to = from.Mix(t.Text, 0.12)
	contrastBg := from
	if ink.Ratio(t.Text, to) < ink.Ratio(t.Text, from) {
		contrastBg = to
	}
	return from, to, ink.Ensure(t.Text, contrastBg, ink.TextContrast)
}

var homeTiles = [4]struct {
	tab      int
	subtitle string
	from, to ui.Color
}{
	{homeHotTab, "本周最热", ui.RGBA(196, 82, 44, 1), ui.RGBA(150, 52, 40, 1)},
	{homeOriginalTab, "原创音乐榜", ui.RGBA(112, 74, 150, 1), ui.RGBA(72, 56, 124, 1)},
	{homeRecentTab, "继续你的音乐旅程", ui.RGBA(40, 116, 122, 1), ui.RGBA(34, 82, 104, 1)},
	{0, "按口味为你挑选", ui.RGBA(170, 60, 92, 1), ui.RGBA(122, 44, 82, 1)},
}

// openTab 从总览进第 tab 档的列表页。
//
// ⚠️ tab / shown / want 三个一起落到 tab：不然 `syncSwap` 会把它当成一次切 Tab，
// 先淡出一段「上一档」的列表再换。
func (h *homePage) openTab(tab int) {
	h.tab, h.shown, h.want, h.wantReady = tab, tab, tab, true
	h.overview = false
}

// backToOverview 回到总览。
func (h *homePage) backToOverview() { h.overview = true }

// refreshMini 在两张榜上的歌数变了时重取前 homeMiniRows 名。每帧在 sync 层调。
func (h *homePage) refreshMini() {
	for i, tab := range [2]int{homeHotTab, homeOriginalTab} {
		n := h.lists[tab].Paged().Loaded
		if n == h.miniLoaded[i] {
			continue
		}
		h.miniLoaded[i] = n
		top := h.lists[tab].Items()
		if len(top) > homeMiniRows {
			top = top[:homeMiniRows]
		}
		h.miniTop[i] = top
	}
}

// viewOverview 画总览。
func (h *homePage) viewOverview(c *ui.Context, s *Shell) {
	t := c.Theme()
	ww, _ := c.Size()
	w := h.ovWidth
	if w <= 0 {
		w = ww - 280
	}

	scroll := ui.Scroll(c).Key("home:overview").Grow(1).TrackScroll(&h.ovScroll)
	scroll.Children(func() {
		col := ui.Column(c).Key("home:overview:body").FillWidth().Gap(space.Px28).
			Padding(space.Px4, scrollbar.HotZone, space.Px24, space.None)
		col.Children(func() {
			h.viewHero(c, w)
			h.viewTiles(c, w)
			h.viewNewRow(c, w, ww)
			h.viewMiniBoards(c, w)
		})
		if bw := col.Bounds().W; bw > 0 {
			h.ovWidth = bw
		}
	})
	_, _ = t, s
}

// cellW 是 n 列、缝为 gap 时一格的宽。
func cellW(w float32, n int, gap float32) float32 {
	if n < 1 || w <= 0 {
		return w
	}
	return (w - float32(n-1)*gap) / float32(n)
}

// heroSongs 是横幅用的歌：新歌速递里有封面的头三首。
func (h *homePage) heroSongs() []list.Music {
	var out []list.Music
	for _, m := range h.newGrid.songs {
		if m.Cover != "" {
			out = append(out, m)
			if len(out) == homeHeroCount {
				break
			}
		}
	}
	return out
}

// overviewState 是总览上那一块此刻处在哪一档。
//
// 🔴 **三档是三件不同的事，不许合成两档**（和 `viewTabBody` 那四种状态同一个
// 口径，见 `homepage.go` 那段「没拉到和没有内容是两件事」）：
//
//	overviewLoading  还在拉     → 摆骨架（形状已知）
//	overviewFailed   拉失败了   → 说清为什么 + 给一个重试
//	overviewEmpty    拉到了但是空的 → 说一句「还没有」，**不给重试**
//
// ⚠️ **「拉失败」和「还在拉」混起来就是这一票的那个空白**（2026-10-31）：
// 三块各自有一句早退，而那句早退只认「还在不在拉」——于是失败那一刻（
// `loading` 变 false、`err` 有了）它判成「不在拉」，早退 ⇒ 整块消失，
// 用户看到的是首页内容区一片空白（连标题都没有）。
type overviewState int

const (
	// overviewLoading 是「还在拉」。
	overviewLoading overviewState = iota
	// overviewFailed 是「拉失败了」。
	overviewFailed
	// overviewEmpty 是「拉到了，但是空的」。
	overviewEmpty
)

// heroState 是横幅那一块此刻的状态。
//
// ⚠️ **它和「有没有歌」是两件事**：横幅只要**有封面的**那几首（`heroSongs`），
// 而新歌速递那一摊可能有歌、只是那些歌没有封面地址——那一档仍然是「横幅没有
// 内容」，而**不是**「这一摊拉失败了」。
//
// ⚠️ **「还没拉过」是「还在拉」那一档**（`!started`）：请求都还没发出去，
// 说「还没有内容」是在说一个还没问过的结论。
func (h *homePage) heroState() overviewState {
	st := &h.feed.newTab
	if st.err != nil {
		return overviewFailed
	}
	if st.loading || !st.started {
		return overviewLoading
	}
	return overviewEmpty
}

// boardState 是榜那一档此刻的状态（`homeHotTab` / `homeOriginalTab`）。
//
// ⚠️ **它问的是那一档自己的翻页列表**（`list.PagedList.Paged()`）：翻页、防重入、
// 失败都在那一层里，而「失败」在那儿是 `Err` 有值、`Loading` 是 false。
//
// ⚠️ **「一页都还没拿到」（`Page == 0`）是「还在拉」**：那一档的第一趟还在
// 路上（或者还没发出去）。把它读成「拉到了但是空的」，用户看到的是一句
// 「还没有内容」——而那是**一个还没问过的结论**。
//
// ⚠️ **「拉到空的」那一档和「失败」是两件事**：榜是「一期一次给完」的一整张
// （见 `app/recommend.go` 的 `toplist`），B 站给了一张空榜就是「这一期没有歌」，
// 不是拉不到——说成「拉不到」会让用户去按一个按多少次都是空的重试。
func (h *homePage) boardState(tab int) overviewState {
	pg := h.lists[tab].Paged()
	if pg.Err != nil {
		return overviewFailed
	}
	if pg.Loading || pg.Page == 0 {
		return overviewLoading
	}
	return overviewEmpty
}

// newMusicState 是新歌速递那一摊此刻的状态。
//
// ⚠️ **它和「有没有歌」是两件事**：这一摊拉回来了空的 ≠ 失败。
//
// ⚠️ **「还没拉过」（`!started`）是「还在拉」那一档**：同 `heroState` 那一条
// ——请求都还没发出去，说「还没有内容」是在说一个还没问过的结论。
//
// ⚠️ **「拉回来了、但一首都没有」是「拉到了但是空的」那一档**：那是 B 站
// 给的结论，不是失败。
func (h *homePage) newMusicState() overviewState {
	st := &h.feed.newTab
	if st.err != nil {
		return overviewFailed
	}
	if st.loading || !st.started {
		return overviewLoading
	}
	return overviewEmpty
}

// overviewFailText 是总览上「这一块没拉出来」那一句。
//
// ⚠️ **和整页那一档说的是同一件事**（`empty.go` 的 `viewLoadError`）：那边是
// 「这一块没拉出来」，这儿是同一句——用户在首页看到的那一句话不该和他在别的
// 页面看到的不一样。
const overviewFailText = "这一块没拉出来"

// overviewEmptyText 是总览上「拉到了，但是空的」那一句。
//
// ⚠️ **不许和失败那句混**：空的那一档按多少次重试都是空的（说成「没拉出来」
// 会让用户去按一个按多少次都没用的按钮）；而失败的那一档**必须**有重试
// （B 站的风控、断网、登录态过期全都会走到那一支）。
//
// ⚠️ **和 `homepage_newmusic.go` 那一档说的是同一件事**：那边是「暂无数据」
// （上游 `new-music-top.tsx:211-214` 的原话），这儿是同一档的另一种说法——
// 它在总览上不是一整块卡片，而是几列小榜，所以那句更短。
const overviewEmptyText = "还没有内容"

// viewOverviewFail 是总览上「这一块没拉出来」那一档：一句话 + 一个重试。
//
// ⚠️ **它不是 `viewEmpty` 的一个空状态**：失败必须有重试按钮，而空状态那一支
// 没有按钮——理由在 `empty.go` 那段「为什么它不能是 viewEmpty 的一个空状态」。
//
// ⚠️ **它也不是 `viewLoadError`**：那一支是**整页**那一格（居中、一枚插画、
// 上下留白），而这里是**总览上的其中一块**——它有邻居（别的块还在），把它
// 撑成一整页的失败格会把旁边那些块挤走。
//
// ⚠️ **三样和 `viewLoadError` 同一个口径**：一句「这一块没拉出来」、一行
// 「没拉出来的是：X」（说清是**哪一块**——总览上可能有三四块同时失败）、
// 一句原始原因，最后一个重试。
//
// where 和 retryLabel 是**两件不同的事**：where 说「是哪一块」（给用户复现用），
// retryLabel 说「按的是哪一趟」（按钮上的字）。两处都带是因为三块可以同时
// 失败，而两个同名按钮摆在一起用户分不清按的是哪个（理由同 `viewLoadError`）。
func (h *homePage) viewOverviewFail(c *ui.Context, key, where, retryLabel string, err error, retry func()) {
	t := c.Theme()
	// ⚠️ **要 `AlignItems(ui.Start)`**：框架的列默认把内容**居中**（横向也是），
	// 而这一块是「一段话 + 一枚按钮」——居中之后按钮飘到那一块的右边，而话在
	// 左边，两者对不上（出图看出来的）。
	ui.Column(c).Key(key).FillWidth().Gap(space.Px4).AlignItems(ui.Start).Children(func() {
		ui.Text(c, overviewFailText).FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text)
		if where != "" {
			ui.Text(c, "没拉出来的是："+where).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
		}
		if err != nil {
			ui.Text(c, err.Error()).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).
				MaxLines(2).Ellipsis("…")
		}
		if retry != nil && retryLabel != "" {
			if flatButton(c, retryLabel).FontSize(scale.Font(c, scale.Size13)).Clicked() {
				retry()
			}
		}
	})
}

// viewOverviewEmpty 是总览上「拉到了，但是空的」那一档：一句话，**没有重试**。
//
// ⚠️ **它和失败那一档必须说不同的话**：空的那一档按多少次重试都是空的，
// 而失败的那一档不给人重试就出不来（理由同 `viewTabBody` 那四种状态）。
func (h *homePage) viewOverviewEmpty(c *ui.Context, key, text string) {
	t := c.Theme()
	ui.Text(c, text).Key(key).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
}

// viewHeroFailed 是横幅那一块在「拉失败」那一档的样子。
//
// ⚠️ **高度照骨架那一档**（`ch`）：失败和「还在拉」是同一块地方的两档，
// 高度不一样的话下面那些行会跟着跳一下——而这一块上面就是磁贴那一排。
//
// ⚠️ **这里不放重试按钮**：横幅和歌那一排是**同一摊数据**（`heroSongs` 读的是
// `h.newGrid.songs`），而重试在下面那个「新歌速递」分区里（那一处的按钮上写着
// 「重试加载新歌速递」，说清按的是哪一趟）。两处各摆一枚一模一样的按钮，
// 用户会以为那是两件事——而它们确实是同一件事。
func (h *homePage) viewHeroFailed(c *ui.Context, w, ch float32) {
	t := c.Theme()
	ui.Column(c).Key("home:hero:fail").Size(w, ch).Radius(t.Radius + 4).
		Center().Gap(space.Px4).Children(func() {
		ui.Text(c, overviewFailText).FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text)
		if err := h.feed.newTab.err; err != nil {
			ui.Text(c, err.Error()).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).
				MaxLines(2).Ellipsis("…")
		}
	})
}

// viewHero 是顶上那块轮播横幅：三张叠在一起，当前那张不透明、其余淡到看不见，
// 每 homeHeroEvery 换下一张（鼠标停在横幅上不换，系统开着「减少动态效果」时直接换）。
func (h *homePage) viewHero(c *ui.Context, w float32) {
	t := c.Theme()
	songs := h.heroSongs()
	ch := min(w*0.26, 260)
	if len(songs) == 0 {
		// 🔴 **失败那一档不许整块消失**（2026-10-31）：原先是「不在拉了就
		// return」——而失败时正是「不在拉了」，于是这一块连标题带内容一起不见，
		// 首页内容区一片空白（用户报的那一张）。
		//
		// 三档分开：**在拉**摆骨架；**拉失败**说清为什么 + 给一个重试；
		// **真的没有**说一句话（那是结论，不用占版面，也**不给重试**——
		// 按多少次都是空的）。
		switch h.heroState() {
		case overviewFailed:
			h.viewHeroFailed(c, w, ch)
			return
		case overviewEmpty:
			// ⚠️ **拉回来了、只是没有能上横幅的封面**：那不是失败，说一句就够。
			// 而**不给重试**：这一摊拉到了，重试也是同一批。
			ui.Column(c).Key("home:hero:empty").Size(w, ch).Radius(t.Radius + 4).
				Center().Children(func() {
				ui.Text(c, "还没有能上横幅的新歌").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			})
			return
		}
		// ⚠️ **骨架要扫光、要有形状**（2026-10-27）：原先是一块 `Text` 6% 的
		// 平色——比空白好一点，但和列表首屏那八行（有扫光、有封面块 + 字条）
		// 明显不是一个东西，同一屏上两种「在等」的样子。
		//
		// 这里照着它将来要变的形状摆：**右边一张封面、左边两行字 + 一条按钮**，
		// 和 `viewHeroCard` 的布局对齐，数据到了只换内容、版面不跳。
		ui.Column(c).Key("home:hero:skel").Size(w, ch).Radius(t.Radius + 4).Clip().
			Label(loadingNewMusicText).Children(func() {
			heroArt := min(ch*16/9, w*0.7)
			ui.Box(c).Key("home:hero:skel:art").Size(heroArt, ch).
				Attach(ui.AnchorTopRight, ui.AnchorTopRight).Children(func() {
				skeleton.Block(c, "home:hero:skel:art:fill", heroArt, ch, 0)
			})
			ui.Column(c).Key("home:hero:skel:txt").Size(max(w-heroArt*0.7, w*0.3), ch).
				Padding(space.Px24, space.Px24, space.Px24, space.Px28).Gap(space.Px8).
				Justify(ui.Center).AlignItems(ui.Start).Children(func() {
				skeleton.Block(c, "home:hero:skel:k", 72, 12, 4)
				skeleton.Block(c, "home:hero:skel:t", min(w*0.45, 360), 24, 6)
				skeleton.Block(c, "home:hero:skel:o", min(w*0.3, 200), 14, 4)
				skeleton.Block(c, "home:hero:skel:btn", 96, 32, 16)
			})
		})
		return
	}
	now := c.Now()
	if h.heroAt.IsZero() {
		h.heroAt = now
	}
	if h.heroIdx >= len(songs) {
		h.heroIdx = 0
	}
	stage := ui.Box(c).Key("home:hero").Size(w, ch).Radius(t.Radius + 4).Clip()
	if len(songs) > 1 {
		if stage.Hovered() {
			h.heroAt = now
		} else if now.Sub(h.heroAt) >= homeHeroEvery {
			h.heroIdx, h.heroAt = (h.heroIdx+1)%len(songs), now
		}
		c.After(max(homeHeroEvery-now.Sub(h.heroAt), time.Millisecond))
	}
	anim := fullBarAnim(c)
	stage.Children(func() {
		for i, m := range songs {
			h.viewHeroCard(c, m, w, ch, i == h.heroIdx, anim)
		}
	})
	if len(songs) > 1 {
		ui.Row(c).Key("home:hero:dots").Gap(space.Px8).Padding(space.Px8, space.None, space.None, space.None).
			Justify(ui.Center).FillWidth().Children(func() {
			for i, m := range songs {
				on := i == h.heroIdx
				dw := float32(8)
				bg := t.Text.Alpha(0.2)
				if on {
					dw, bg = 20, t.Accent
				}
				ui.Box(c).Key("home:hero:dot:"+m.ID).Size(dw, 8).Radius(4).Background(bg).
					Focusable().Role(ui.RoleButton).Label("第 " + strconv.Itoa(i+1) + " 张").
					OnClick(func() { h.heroIdx, h.heroAt = i, c.Now() })
			}
		})
	}
}

func (h *homePage) viewHeroCard(c *ui.Context, m list.Music, w, hgt float32, cur bool, anim ui.Animation) {
	t := c.Theme()
	sh := list.ShowOf(m)
	card := ui.Box(c).Key("home:hero:"+m.ID).Size(w, hgt).Radius(homeHeroRadius(t)).Clip().
		Attach(ui.AnchorTopLeft, ui.AnchorTopLeft)
	fade := float32(1)
	if cur {
		fade = 0
		card = card.Focusable().Role(ui.RoleButton).Label(sh.Title).OnClick(func() { h.playNow(m) })
	}
	card = card.AnimatePose(ui.Pose{Fade: fade}, anim)
	card = card.Background(ui.RGBA(28, 23, 20, 1))
	card.Children(func() {
		// 封面多是 16:9：按原比例贴右，不再拉满整条横幅（拉满会被放大裁掉大半，还糊）；
		// 左边一段渐变把它融进深色底，文字落在深色底上。
		cw := min(hgt*16/9, w*0.7)
		ui.Box(c).Key("home:hero:img:"+m.ID).Size(cw, hgt).Attach(ui.AnchorTopRight, ui.AnchorTopRight).
			Children(func() {
				imgcache.ViewPlaceholderTitled(c, h.newGrid.cache, m.Cover, sh.Title, cw, hgt, homeHeroRadius(t), nil)
				ui.Box(c).Key("home:hero:fade:"+m.ID).Size(cw*0.45, hgt).
					Gradient(ui.RGBA(28, 23, 20, 1), ui.RGBA(28, 23, 20, 0), 90).
					Attach(ui.AnchorTopLeft, ui.AnchorTopLeft)
			})
		ui.Column(c).Key("home:hero:txt:"+m.ID).Size(max(w-cw*0.7, w*0.3), hgt).
			Padding(space.Px24, space.Px24, space.Px24, space.Px28).Gap(space.Px8).Justify(ui.Center).AlignItems(ui.Start).
			Attach(ui.AnchorTopLeft, ui.AnchorTopLeft).Children(func() {
			ui.Text(c, "新歌速递").FontSize(scale.Font(c, scale.Size12)).
				TextColor(ink.Ensure(t.Accent, ui.RGBA(28, 23, 20, 1), ink.TextContrast))
			ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size24)).
				TextColor(ink.Ensure(ui.RGBA(255, 255, 255, 1), ui.RGBA(28, 23, 20, 1), ink.TextContrast)).MaxLines(2).Ellipsis("…")
			if sh.Owner != "" {
				ui.Text(c, sh.Owner).FontSize(scale.Font(c, scale.Size14)).
					TextColor(ink.Ensure(ui.RGBA(255, 255, 255, 1), ui.RGBA(28, 23, 20, 1), ink.TextContrast)).MaxLines(1).Ellipsis("…")
			}
			ui.Row(c).Key("home:hero:play:"+m.ID).Gap(space.Px6).Padding(space.Px8, space.Px16, space.Px8, space.Px12).
				Radius(18).Background(t.Accent).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon(iconPlayFill)).FontSize(16).TextColor(t.AccentText)
				ui.Text(c, "立即播放").FontSize(scale.Font(c, scale.Size13)).TextColor(t.AccentText)
			})
		})
	})
}

func (h *homePage) playNow(m list.Music) {
	if h.acts.PlayNow != nil {
		h.acts.PlayNow(m.Item)
	}
}

func (h *homePage) viewTiles(c *ui.Context, w float32) {
	t := c.Theme()
	cw := cellW(w, len(homeTiles), homeOverviewCellGap)
	ui.Row(c).Key("home:tiles").FillWidth().Gap(space.Px16).Children(func() {
		for _, tile := range homeTiles {
			name := homeTabs[tile.tab]
			from, to, tileText := homeTilePalette(t, tile)
			b := ui.Column(c).Key("home:tile:"+name).Size(cw, homeTileH).Radius(homeTileRadius(t)).Clip().
				Gradient(from, to, 135).Padding(space.Px12, space.Px16).Justify(ui.Center).Gap(space.Px4).
				Focusable().Role(ui.RoleButton).Label(name).OnClick(func() { h.openTab(tile.tab) })
			b.Children(func() {
				// 磁贴只需要榜单第一项；不要用 Items() 为一张插画复制整页分页列表。
				iw := cw * 0.5
				if first, ok := h.lists[tile.tab].First(); ok && first.Cover != "" &&
					h.newGrid.cache.GetSized(first.Cover, iw, homeTileH).Ready() {
					ui.Box(c).Key("home:tile:art:"+name).Size(iw, homeTileH).Attach(ui.AnchorTopRight, ui.AnchorTopRight).
						Children(func() {
							imgcache.ViewPlaceholderTitled(c, h.newGrid.cache, first.Cover, "", iw, homeTileH, homeTileRadius(t), nil)
							ui.Box(c).Key("home:tile:fade:"+name).Size(iw, homeTileH).
								Gradient(to, to.Alpha(0), 90).Attach(ui.AnchorTopLeft, ui.AnchorTopLeft)
						})
				}

				ui.Text(c, name).FontSize(scale.Font(c, scale.Size18)).TextColor(tileText).MaxLines(1)
				ui.Text(c, tile.subtitle).FontSize(scale.Font(c, scale.Size13)).
					TextColor(tileText).MaxLines(1)
			})
		}
	})
}

// sectionHead 画一个分区标题：左边名字，右边「更多」。
func (h *homePage) sectionHead(c *ui.Context, key, title, more string, go_ func()) {
	t := c.Theme()
	ui.Row(c).Key(key).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, title).FontSize(scale.Font(c, scale.Size20)).TextColor(t.Text)
		if more != "" && flatLink(c, more).Clicked() {
			go_()
		}
	})
}

// loadingNewMusicText 是总览上「新歌速递」那一块在拉时那句（骨架屏的无障碍名字）。
//
// ⚠️ **和「流行」Tab 那一档用同一句话**（`homepage_newmusic.go` 的骨架块）：
// 同一块内容在两个地方等，读屏软件该读到同一句。
const loadingNewMusicText = "正在加载新歌速递"

// loadingBoardText 是总览上「排行榜」在拉时那句。
const loadingBoardText = "正在加载排行榜"

// viewNewRow 在「发现」总览上画新歌速递那一排。
func (h *homePage) viewNewRow(c *ui.Context, w, ww float32) {
	t := c.Theme()
	songs := h.newGrid.songs
	if len(songs) > homeHeroCount {
		songs = songs[homeHeroCount:]
	}
	cols := newMusicColsFor(ww)
	if cols > 6 {
		cols = 6
	}
	cw := cellW(w, cols, homeOverviewCellGap)
	// 🔴 **一首都没有的时候摆骨架，不要整块消失**（2026-10-27）：原先这里
	// `len(songs) == 0` 直接 return——连「新歌速递」那个标题一起不见，数据到了
	// 才整块冒出来，下面的磁贴/榜单跟着往下跳一大截（用户看到的是「点进首页，
	// 版面自己动了一下」）。
	//
	// 三档分开：**在拉**摆卡片骨架；**拉失败**说清为什么 + 给一个重试；
	// **真的没有**说一句「还没有」（那是结论，不用占版面，也**不给重试**）。
	//
	// 🔴 **失败那一档不许整块消失**（2026-10-31）：原先那句早退是
	// `!loading && (started || err != nil)` ——而失败时正是「不在拉、也没歌」，
	// 于是它早退 ⇒ 连「新歌速递」那个标题一起不见，首页内容区一片空白。
	if len(songs) == 0 {
		switch h.newMusicState() {
		case overviewFailed:
			ui.Column(c).Key("home:new").FillWidth().Gap(space.Px12).Children(func() {
				h.sectionHead(c, "home:new:head", "新歌速递", "", nil)
				h.viewOverviewFail(c, "home:new:fail", "新歌速递", "重试加载新歌速递",
					h.feed.newTab.err, func() { h.retryNew() })
			})
			return
		case overviewEmpty:
			ui.Column(c).Key("home:new").FillWidth().Gap(space.Px12).Children(func() {
				h.sectionHead(c, "home:new:head", "新歌速递", "", nil)
				h.viewOverviewEmpty(c, "home:new:empty", overviewEmptyText)
			})
			return
		}
		ui.Column(c).Key("home:new").FillWidth().Gap(space.Px12).Children(func() {
			h.sectionHead(c, "home:new:head", "新歌速递", "", nil)
			ui.Row(c).Key("home:new:skel").FillWidth().Gap(space.Px16).
				Label(loadingNewMusicText).Children(func() {
				for i := range min(cols, 6) {
					skeleton.Card(c, "home:new:skel:"+strconv.Itoa(i), cw)
				}
			})
		})
		return
	}
	total := len(songs)
	if h.newOff+cols > total {
		h.newOff = max(total-cols, 0)
	}
	songs = songs[h.newOff:min(h.newOff+cols, total)]
	ui.Column(c).Key("home:new").FillWidth().Gap(space.Px12).Children(func() {
		h.sectionHead(c, "home:new:head", "新歌速递", "查看更多", func() { h.openTab(homePopTab) })
		ui.Box(c).Key("home:new:stage").FillWidth().Children(func() {
			ui.Row(c).Key("home:new:row").FillWidth().Gap(space.Px16).Children(func() {
				for _, m := range songs {
					sh := list.ShowOf(m)
					card := ui.Column(c).Key("home:new:" + m.ID).Width(cw).Gap(space.Px6).
						Focusable().Role(ui.RoleButton).Label(sh.Title).OnClick(func() { h.playNow(m) })
					on := card.Hovered()
					card.Children(func() {
						imgcache.ViewTitled(c, h.newGrid.cache, m.Cover, sh.Title, cw, cw)
						col := t.Text
						if on || (h.acts.IsPlaying != nil && h.acts.IsPlaying(m.Item)) {
							col = ink.AccentOn(t)
						}
						ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size14)).TextColor(col).MaxLines(1).Ellipsis("…")
						if sh.Owner != "" {
							ui.Text(c, sh.Owner).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
						}
					})
				}
			})
			arrow := func(key, label, glyph string, anchor ui.Anchor, to int) {
				b := ui.Box(c).Key(key).Size(36, 36).Radius(18).Center().Background(t.Surface).Border(1, t.Border).
					Attach(anchor, anchor).Top(cw/2 - 18).Focusable().Role(ui.RoleButton).Label(label).
					OnClick(func() { h.newOff = to })
				b.Children(func() { ui.Text(c, glyph).FontSize(scale.Font(c, scale.Size20)).TextColor(t.Text) })
			}
			if h.newOff > 0 {
				arrow("home:new:prev", "上一页", "‹", ui.AnchorTopLeft, max(h.newOff-cols, 0))
			}
			if h.newOff+cols < total {
				arrow("home:new:next", "下一页", "›", ui.AnchorTopRight, min(h.newOff+cols, total-cols))
			}
		})
	})
}

func (h *homePage) viewMiniBoards(c *ui.Context, w float32) {
	newTop := h.newGrid.songs
	if len(newTop) > homeMiniRows {
		newTop = newTop[:homeMiniRows]
	}
	cw := (w - 2*32) / 3
	// 🔴 **三块都空的时候摆骨架，不要整块消失**（2026-10-27）：原先这里直接
	// return——「排行榜」那个标题连同三列一起不见，数据到了整块冒出来，
	// 下面的内容跟着往下跳。
	//
	// 🔴 **这一块不许整块消失**（2026-10-31）：原先这里是
	// `if !h.boardLoading() { return }` ——而 `boardLoading()` 只在「还在拉」
	// 时为真（它只看 `pg.Loading`），于是**拉失败**那一刻它判成「不在拉」
	// ⇒ 整块（含「排行榜」那个标题）不见了 ⇒ 首页内容区一片空白
	// （用户报的那一张）。
	//
	// 三档分开：**在拉**摆骨架；**拉失败**说清为什么 + 给一个重试；
	// **真的没有**说一句「还没有」（**不给重试**——按多少次都是空的）。
	//
	// ⚠️ **三列各自判各自的**：热榜和原创榜是两次请求，而「新歌榜」是第三次
	// （新歌速递那一摊）——三列的状态**可以不一样**（一列失败、另两列有歌）。
	// 所以那一列失败时只有它自己那一列说失败，别的两列照常摆歌。
	allEmpty := len(h.miniTop[0]) == 0 && len(h.miniTop[1]) == 0 && len(newTop) == 0
	if allEmpty && h.boardsLoading() {
		// 三列都还在拉：摆**三列**的骨架（照着它们将来的样子：榜头 + 四行
		// 「封面 + 字条」），数据到了只换内容、版面不跳。
		ui.Column(c).Key("home:boards").FillWidth().Gap(space.Px12).Children(func() {
			h.sectionHead(c, "home:boards:head", "排行榜", "", nil)
			ui.Row(c).Key("home:boards:skel").FillWidth().Gap(space.Px32).
				AlignItems(ui.Start).Label(loadingBoardText).Children(func() {
				for i := range 3 {
					ui.Column(c).Key("home:boards:skel:" + strconv.Itoa(i)).Width(cw).
						Gap(space.Px6).Children(func() {
						skeleton.Block(c, "home:boards:skel:h:"+strconv.Itoa(i), 96, 20, 4)
						for row := range 4 {
							ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
								skeleton.Block(c, "home:boards:skel:c:"+strconv.Itoa(i)+":"+strconv.Itoa(row),
									homeMiniCover, homeMiniCover, 6)
								skeleton.Block(c, "home:boards:skel:t:"+strconv.Itoa(i)+":"+strconv.Itoa(row),
									0, 12, 4)
							})
						}
					})
				}
			})
		})
		return
	}
	ui.Column(c).Key("home:boards").FillWidth().Gap(space.Px12).Children(func() {
		h.sectionHead(c, "home:boards:head", "排行榜", "", nil)
		ui.Row(c).Key("home:boards:row").FillWidth().Gap(space.Px32).AlignItems(ui.Start).Children(func() {
			h.viewMiniBoard(c, homeTabs[homeHotTab], h.miniTop[0], homeHotTab, cw,
				h.boardState(homeHotTab))
			h.viewMiniBoard(c, homeTabs[homeOriginalTab], h.miniTop[1], homeOriginalTab, cw,
				h.boardState(homeOriginalTab))
			h.viewMiniBoard(c, "新歌榜", newTop, homePopTab, cw,
				h.newMusicState())
		})
	})
}

// boardsLoading 报「那三列全都还在拉」。
//
// ⚠️ **三列都空**才轮到它（外层那一支）：一列有歌的时候另外两列各自判各自
// 的（见 `viewMiniBoard`）——那一列有歌，这一块就该摆出那个「有歌」的形状。
//
// ⚠️ **看的是每一列自己的取数状态**（不是「有没有歌」）：空数组在三档里都出现
// （还在拉 / 拉失败 / 真的没有），只有第一种才该摆骨架。
func (h *homePage) boardsLoading() bool {
	for _, tab := range [2]int{homeHotTab, homeOriginalTab} {
		if h.boardState(tab) != overviewLoading {
			return false
		}
	}
	return h.newMusicState() == overviewLoading
}

// boardErrOf 是那一列失败的原因（已经翻成人话了）。
//
// ⚠️ **两张榜的错是翻过的**（`homepage_feed.go` 的 `tabFetcher` 里那一行
// `homeFailureOf(err)`），而「新歌榜」那一列的错是新歌速递那一摊的
// （`h.feed.newTab.err`，也是翻过的）。两边都已经是能直接画给人看的话。
func (h *homePage) boardErrOf(tab int) error {
	if tab == homePopTab {
		return h.feed.newTab.err
	}
	return h.lists[tab].Paged().Err
}

// retryBoard 让那一列再拉一次。
//
// ⚠️ **两张榜走 `retryTab`**（翻页列表的 `Reload`：清空、回顶、从第一页重来），
// 而**新歌榜那一列走 `retryNew`**（新歌速递那一摊自己的那一个）——
// 三列是三次不同的请求，重试要重发的是**那一列**自己的那一次。
func (h *homePage) retryBoard(tab int) {
	if tab == homePopTab {
		h.retryNew()
		return
	}
	h.retryTab(tab)
}

// playAllOf 把 songs 整排放进播放：第一首立刻播，其余排进队尾。
func (h *homePage) playAllOf(songs []list.Music) {
	if len(songs) == 0 {
		return
	}
	h.playNow(songs[0])
	if h.acts.Add == nil || len(songs) < 2 {
		return
	}
	its := make([]app.Item, 0, len(songs)-1)
	for _, m := range songs[1:] {
		its = append(its, m.Item)
	}
	h.acts.Add(its)
}

// viewMiniBoard 画总览上那一列小榜。
//
// ⚠️ **三档是三件不同的事**（2026-10-31，和 `viewTabBody` 那四种状态同一个
// 口径）：**在拉**说「还在路上」；**拉失败**说清为什么 + 给一个重试；
// **真的没有**说「还没有内容」（**不给重试**——按多少次都是空的）。
//
// ⚠️ **三列各自判各自的**（state 是传进来的）：热榜、原创榜、新歌榜是三次
// 请求，它们可以一个失败、另两个有歌——那一列失败时只有它自己那一列说失败，
// 别的两列照常摆歌。
//
// ⚠️ **重试的名字要说清是哪一趟**：三列同时失败时三个「重试」摆在一起，
// 用户分不清按的是哪个（理由同 `viewLoadError` 的那一条）。
func (h *homePage) viewMiniBoard(c *ui.Context, name string, songs []list.Music, tab int, w float32, state overviewState) {
	t := c.Theme()
	ui.Column(c).Key("home:board:" + name).Width(w).Gap(space.Px4).Children(func() {
		ui.Row(c).Key("home:board:head:" + name).FillWidth().AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			ui.Text(c, name).FontSize(scale.Font(c, scale.Size20)).TextColor(t.Text).Grow(1)
			if len(songs) > 0 {
				pa := ui.Row(c).Key("home:board:all:"+name).AlignItems(ui.Center).Gap(space.Px4).
					Padding(space.Px4, space.Px10).Radius(14).Background(t.Text.Alpha(0.06)).
					Focusable().Role(ui.RoleButton).Label("播放全部 " + name).OnClick(func() { h.playAllOf(songs) })
				if pa.Hovered() {
					pa = pa.Background(t.Text.Alpha(0.12))
				}
				pa.Children(func() {
					ui.Icon(c, icon(iconPlayFill)).FontSize(12).TextColor(t.Text)
					ui.Text(c, "播放全部").FontSize(scale.Font(c, scale.Size12)).TextColor(t.Text)
				})
			}
			if flatLink(c, "全部").Clicked() {
				h.openTab(tab)
			}
		})
		if len(songs) == 0 {
			switch state {
			case overviewFailed:
				// 🔴 **失败那一档要有重试**（2026-10-31）：B 站的风控、断网、
				// 登录态过期全都会走到这一支，而不给重试用户就没法重来。
				h.viewOverviewFail(c, "home:board:fail:"+name, name, "重试加载"+name,
					h.boardErrOf(tab), func() { h.retryBoard(tab) })
			case overviewEmpty:
				// ⚠️ **和失败那句不同**：这一档是「B 站给了一张空榜」——按多少次
				// 重试都是空的，所以**不给重试**（理由同 `viewTabBody`）。
				h.viewOverviewEmpty(c, "home:board:empty:"+name, overviewEmptyText)
			default:
				// 还在拉：说「还在路上」。外层那一支三列都在拉时摆的是骨架，
				// 走到这里的是「别的列有歌、这一列还在拉」那种混合情形。
				ui.Text(c, "榜单还在路上").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			}
			return
		}
		for i, m := range songs {
			sh := list.ShowOf(m)
			row := ui.Row(c).Key("home:board:row:"+name+":"+m.ID).FillWidth().Height(homeMiniRowH).
				AlignItems(ui.Center).Gap(space.Px12).Padding(space.None, space.Px8).Radius(t.Radius).
				Focusable().Role(ui.RoleButton).Label(sh.Title).OnClick(func() { h.playNow(m) })
			if row.Hovered() {
				row = row.Background(t.Text.Alpha(0.06))
			}
			row.Children(func() {
				rc, bold := t.TextMuted, false
				if i < 3 {
					rc, bold = ink.AccentOn(t), true
				}
				rank := ui.Text(c, strconv.Itoa(i+1)).Width(24).TextAlign(ui.Center).
					FontSize(scale.Font(c, scale.Size16)).TextColor(rc)
				if bold {
					rank = rank.FontWeight(weightMedium)
				}
				imgcache.ViewTitled(c, h.newGrid.cache, m.Cover, sh.Title, homeMiniCover, homeMiniCover)
				ui.Column(c).Grow(1).Gap(space.Px2).Children(func() {
					ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size14)).TextColor(t.Text).MaxLines(1).Ellipsis("…")
					if sh.Owner != "" {
						ui.Text(c, sh.Owner).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
					}
				})
				if m.Duration > 0 {
					ui.Text(c, clockText(m.Duration)).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
				}
			})
		}
	})
}
