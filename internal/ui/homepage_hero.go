// 首页「今日一张」主视觉（原型 `.hero`，CSS 366–376 行，`app.js` 567–579 行）。
//
// # 真实数据怎么取（原型里是写死的《叶惠美》）
//
// B 站给不出「专辑」，所以「今日一张」取的是**本周最热那张榜里的一首歌**：
//
//   - **哪一首**：把榜里「有封面、放得了」的条目排成一列，按**日期**确定性地选一个——
//     同一天不管开几次、换不换账号都是同一首，第二天换下一首（`heroPick`）。
//     没有热榜时退回每日推荐。
//   - **标题**：清洗过的歌名（`widgets/title`），艺人名是它的 UP / 歌手名。
//   - **两首曲目**：被选中的那一首 + 同一张榜里排在它后面的那一首。原型这里是专辑的前
//     两个曲目，我们没有分 P 的数据（榜单接口不给分 P，逐个去问会多发请求），所以取同
//     一来源里相邻的两首。
//   - **「播放」**从第一首起把这两首换进队列；**「全部加入队列」**把这两首追加到队尾。
//   - **背景**：封面取色（`covertint`）按 22%（`tok.TintHero`）混进底色；还没取到色时
//     用原型的默认 #333。
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// heroTrackCount 是主视觉上摆几首曲目（原型 `heroIds = ['t1', 't2']`）。
const heroTrackCount = 2

// heroPlayable 报一条能当「今日一张」的候选：有封面、放得了。
func heroPlayable(m list.Music) bool { return m.Cover != "" && m.Item.Bvid != "" }

// heroPick 从 songs 里按日期选出「今日一张」和后面那一首（最多 heroTrackCount 首）。
//
// 候选是有封面、放得了的条目；一个都没有就退回全部。同一天选到的永远是同一首。
//
// ⚠️ 不拷候选：`list.Music` 是个大结构，拷一遍二十行就是几十 KB，而首页每帧都问它——
// 数一遍候选、再走到第 idx 个就够了（调用方还会把结果记一份，见 `heroSongs`）。
func heroPick(songs []list.Music, day time.Time) []list.Music {
	n := 0
	for _, m := range songs {
		if heroPlayable(m) {
			n++
		}
	}
	all := n == 0
	if all {
		n = len(songs)
	}
	if n == 0 {
		return nil
	}
	y, mo, d := day.Date()
	idx := (y*372 + int(mo)*31 + d) % n
	at := func(i int) list.Music {
		for _, m := range songs {
			if all || heroPlayable(m) {
				if i == 0 {
					return m
				}
				i--
			}
		}
		return list.Music{}
	}
	out := []list.Music{at(idx)}
	if n > 1 && heroTrackCount > 1 {
		out = append(out, at((idx+1)%n))
	}
	return out
}

// heroKey 是「今日一张」那一选的依据：哪一天、从哪一摊选、那一摊的头一首和条数。三样都没变，
// 选出来的就是同一首，不用每帧再选。
type heroKey struct {
	day   int
	chart bool
	n     int
	first string
}

// heroSongs 是此刻主视觉上的曲目：热榜优先，没有就用每日推荐。选出来的记一份，依据没变
// 就直接给。
func (h *homePage) heroSongs() []list.Music {
	src, chart := h.feed.sec[homeChart].songs, true
	if len(src) == 0 {
		src, chart = h.feed.sec[homeDaily].songs, false
	}
	y, mo, d := h.day.Date()
	k := heroKey{day: y*10000 + int(mo)*100 + d, chart: chart, n: len(src)}
	if len(src) > 0 {
		k.first = src[0].ID
	}
	if h.heroOK && h.heroFor == k {
		return h.hero
	}
	h.hero, h.heroFor, h.heroOK = heroPick(src, h.day), k, true
	return h.hero
}

// heroMetrics 是这一窗口宽度下的主视觉尺寸（原型 `.hero` 的三档，媒体查询看窗口宽）：
//
//	> 1024  封面 320、缝 48、内边距 32
//	> 760   封面 200、缝 32、内边距 24
//	更窄    上下叠放，封面最宽 220，内边距 24
func heroMetrics(win float32) (coverW, gap, pad float32, stacked bool) {
	switch {
	case win > 1024:
		return 320, 48, 32, false
	case win > 760:
		return 200, 32, 24, false
	}
	return 220, 24, 24, true
}

// viewHero 画「今日一张」。
func (h *homePage) viewHero(c *ui.Context, s *Shell, inner float32, hero []list.Music) {
	p := tok.Of(c.Theme())
	cw := h.contentW
	if cw <= 0 {
		cw = inner
	}
	coverW, gap, pad, stacked := heroMetrics(h.win)

	if len(hero) == 0 {
		// 还在拉：摆一块和成品一样高的骨架，免得拉到之后整页往下跳。
		if h.waiting(homeChart) || h.waiting(homeDaily) {
			pud.Skeleton(c, "home:hero:sk", inner, coverW+2*pad, tok.R12).
				Margin(space.Px32, space.None, space.None, space.None)
		}
		return
	}
	top := hero[0]

	// 背景：封面取色按 22% 混进底色；取不到色时用 #333（和收藏夹头部同一个混色入口）。
	bg := s.tintBackgroundOr(c, imgcache.FormatURLProtocol(top.Cover), tok.TintHero, ui.RGB(0x33, 0x33, 0x33))

	box := ui.Row(c).Key("home:hero").FillWidth().Radius(tok.R12).Background(bg).Clip().
		Padding(pad).Gap(gap).AlignItems(ui.Center)
	if stacked {
		box = ui.Column(c).Key("home:hero").FillWidth().Radius(tok.R12).Background(bg).Clip().
			Padding(pad).Gap(gap).AlignItems(ui.Start)
	}
	box = box.Margin(space.Px32, space.None, space.None, space.None)
	box.Children(func() {
		cover := ui.Box(c).Key("home:hero:cover").Size(coverW, coverW).Radius(tok.R8).Background(p.Surface2).Shrink(0)
		cover = shadow.LargeCover(cover, c.Theme())
		cover.Clip().Children(func() {
			imgcache.ViewRadius(c, h.cache, imgcache.FormatURLProtocol(top.Cover), coverW, coverW, 0)
		})

		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			typo.Eyebrow(c, "今日一张 · Album of the day")
			name := title.Clean(top.Title).Title
			// 原型 `.hero-title { line-height: 1.05; margin: 12px 0 8px }`（补回行高见 `typo.DisplayNatural`）。
			hs := scale.Font(c, homeHeroSize(h.win))
			trim := typo.DisplayTrim(hs, 1.05)
			typo.Display(ui.Text(c, name), hs).LetterSpacing(hs*0.02).MaxLines(2).
				Margin(space.Px12-trim, space.None, space.Px8-trim, space.None)
			if top.Owner != "" {
				ui.Text(c, top.Owner).FontSize(scale.Font(c, scale.Size16)).LineHeight(leading.Body).TextColor(p.Text2).SingleLine().Ellipsis("…")
			}
			// 曲目。
			ui.Column(c).FillWidth().MaxWidth(520).Gap(space.Px2).
				Margin(space.Px24, space.None, space.Px28, space.None).Children(func() {
				for i, m := range hero {
					h.viewHeroTrack(c, s, hero, i, m)
				}
			})
			ui.Row(c).Gap(space.Px12).Children(func() {
				if pud.Button(c, "home:hero:play", pud.Primary, pud.Std, "播放", "play").Clicked() {
					h.playFrom(s, hero, 0)
				}
				if pud.Button(c, "home:hero:enqueue", pud.Ghost, pud.Std, "全部加入队列", "plus").Clicked() {
					h.enqueue(hero)
				}
			})
		})
	})
}

// viewHeroTrack 画主视觉上的一行曲目（原型 `.hero-track`：序号 28 | 歌名 | 播放量 · 时长，
// 缝 12、内边距 8、圆角 8，悬停 hover 底，正在放的歌名换 accent-text）。
func (h *homePage) viewHeroTrack(c *ui.Context, s *Shell, hero []list.Music, i int, m list.Music) {
	p := tok.Of(c.Theme())
	b := ui.ButtonBase(c).Key("home:hero:track:" + m.ID).Label("播放 " + title.Clean(m.Title).Title).
		FillWidth().Padding(space.Px8).Radius(tok.R8).Gap(space.Px12).AlignItems(ui.Center)
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b.Children(func() {
		typo.Mono(ui.Text(c, fmt.Sprintf("%02d", i+1))).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).
			TextColor(p.Text3).Width(28).SingleLine()
		fg := p.Text
		if h.isCur(m) {
			fg = p.AccentText
		}
		ui.Text(c, title.Clean(m.Title).Title).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(fg).
			SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		var meta []string
		if t := homePlays(m.View); t != "" {
			meta = append(meta, t)
		}
		if t := clockText(m.Duration); t != "" {
			meta = append(meta, t)
		}
		if len(meta) > 0 {
			typo.Mono(ui.Text(c, strings.Join(meta, " · "))).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).
				TextColor(p.Text3).SingleLine().Shrink(0)
		}
	})
	if b.Clicked() {
		h.playFrom(s, hero, i)
	}
}
