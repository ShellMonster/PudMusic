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

// homeTiles 是四个入口磁贴：指向 homeTabs 的哪一档、副标题、渐变两头。
// 颜色是固定的深色调（白字压得住），不跟主题走：磁贴本身就是「封面」。
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

// viewHero 是顶上那块轮播横幅：三张叠在一起，当前那张不透明、其余淡到看不见，
// 每 homeHeroEvery 换下一张（鼠标停在横幅上不换，系统开着「减少动态效果」时直接换）。
func (h *homePage) viewHero(c *ui.Context, w float32) {
	t := c.Theme()
	songs := h.heroSongs()
	ch := min(w*0.26, 260)
	if len(songs) == 0 {
		if !h.feed.newTab.loading && h.feed.newTab.started {
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
	card := ui.Box(c).Key("home:hero:"+m.ID).Size(w, hgt).Radius(t.Radius+4).Clip().
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
				imgcache.ViewPlaceholderTitled(c, h.newGrid.cache, m.Cover, sh.Title, cw, hgt, 0, nil)
				ui.Box(c).Key("home:hero:fade:"+m.ID).Size(cw*0.45, hgt).
					Gradient(ui.RGBA(28, 23, 20, 1), ui.RGBA(28, 23, 20, 0), 90).
					Attach(ui.AnchorTopLeft, ui.AnchorTopLeft)
			})
		ui.Column(c).Key("home:hero:txt:"+m.ID).Size(max(w-cw*0.7, w*0.3), hgt).
			Padding(space.Px24, space.Px24, space.Px24, space.Px28).Gap(space.Px8).Justify(ui.Center).AlignItems(ui.Start).
			Attach(ui.AnchorTopLeft, ui.AnchorTopLeft).Children(func() {
			ui.Text(c, "新歌速递").FontSize(scale.Font(c, scale.Size12)).Bold().TextColor(t.Accent)
			ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size24)).Bold().
				TextColor(ui.RGBA(255, 255, 255, 1)).MaxLines(2).Ellipsis("…")
			if sh.Owner != "" {
				ui.Text(c, sh.Owner).FontSize(scale.Font(c, scale.Size14)).
					TextColor(ui.RGBA(255, 255, 255, 0.75)).MaxLines(1).Ellipsis("…")
			}
			ui.Row(c).Key("home:hero:play:"+m.ID).Gap(space.Px6).Padding(space.Px8, space.Px16, space.Px8, space.Px12).
				Radius(18).Background(t.Accent).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon(iconPlayFill)).FontSize(16).TextColor(t.AccentText)
				ui.Text(c, "立即播放").FontSize(scale.Font(c, scale.Size13)).Bold().TextColor(t.AccentText)
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
			b := ui.Column(c).Key("home:tile:"+name).Size(cw, homeTileH).Radius(t.Radius+2).Clip().
				Gradient(tile.from, tile.to, 135).Padding(space.Px12, space.Px16).Justify(ui.Center).Gap(space.Px4).
				Focusable().Role(ui.RoleButton).Label(name).OnClick(func() { h.openTab(tile.tab) })
			b.Children(func() {
				// 磁贴右侧压一张该榜第一名的封面当「插画」，向左渐隐进磁贴底色；没有数据就只剩渐变。
				if items := h.lists[tile.tab].Items(); len(items) > 0 && items[0].Cover != "" && h.newGrid.cache.Get(items[0].Cover).Ready() {
					iw := cw * 0.5
					ui.Box(c).Key("home:tile:art:"+name).Size(iw, homeTileH).Attach(ui.AnchorTopRight, ui.AnchorTopRight).
						Children(func() {
							imgcache.ViewPlaceholderTitled(c, h.newGrid.cache, items[0].Cover, "", iw, homeTileH, 0, nil)
							ui.Box(c).Key("home:tile:fade:"+name).Size(iw, homeTileH).
								Gradient(tile.to, tile.to.Alpha(0), 90).Attach(ui.AnchorTopLeft, ui.AnchorTopLeft)
						})
				}
				ui.Text(c, name).FontSize(scale.Font(c, scale.Size18)).Bold().TextColor(ui.RGBA(255, 255, 255, 1)).MaxLines(1)
				ui.Text(c, tile.subtitle).FontSize(scale.Font(c, scale.Size12)).
					TextColor(ui.RGBA(255, 255, 255, 0.78)).MaxLines(1)
			})
		}
	})
}

// sectionHead 画一个分区标题：左边名字，右边「更多」。
func (h *homePage) sectionHead(c *ui.Context, key, title, more string, go_ func()) {
	t := c.Theme()
	ui.Row(c).Key(key).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, title).FontSize(scale.Font(c, scale.Size20)).Bold().TextColor(t.Text)
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
	// 三档分开：**在拉**摆卡片骨架；**拉失败**说失败（那一块自己有重试，
	// 见 `newMusicGrid.view`）；**真的没有**什么都不摆（那是结论，不用占版面）。
	if len(songs) == 0 {
		if !h.feed.newTab.loading && (h.feed.newTab.started || h.feed.newTab.err != nil) {
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
	// ⚠️ **只在「真的还在拉」时摆**：拉失败之后三列各自有一句「榜单还在路上」
	// （见 `viewMiniBoard`），那时再摆骨架是在说「马上就好」，而它不会好。
	if len(h.miniTop[0]) == 0 && len(h.miniTop[1]) == 0 && len(newTop) == 0 {
		if !h.boardLoading() {
			return
		}
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
			h.viewMiniBoard(c, homeTabs[homeHotTab], h.miniTop[0], homeHotTab, cw)
			h.viewMiniBoard(c, homeTabs[homeOriginalTab], h.miniTop[1], homeOriginalTab, cw)
			h.viewMiniBoard(c, "新歌榜", newTop, homePopTab, cw)
		})
	})
}

// boardLoading 报「那三列里至少还有一列在路上」。
//
// ⚠️ **看的是翻页列表自己的状态**（不是「有没有歌」）：空数组在三档里都出现
// （还在拉 / 拉失败 / 真的没有），只有第一种才该摆骨架。
func (h *homePage) boardLoading() bool {
	for _, tab := range [2]int{homeHotTab, homeOriginalTab} {
		if pg := h.lists[tab].Paged(); pg.Loading && pg.Loaded == 0 {
			return true
		}
	}
	st := &h.feed.newTab
	return st.loading && len(h.newGrid.songs) == 0
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

func (h *homePage) viewMiniBoard(c *ui.Context, name string, songs []list.Music, tab int, w float32) {
	t := c.Theme()
	ui.Column(c).Key("home:board:" + name).Width(w).Gap(space.Px4).Children(func() {
		ui.Row(c).Key("home:board:head:" + name).FillWidth().AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			ui.Text(c, name).FontSize(scale.Font(c, scale.Size20)).Bold().TextColor(t.Text).Grow(1)
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
			ui.Text(c, "榜单还在路上").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
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
					rank.Bold()
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
