package pud

import (
	"fmt"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// ArtKind 是空状态插画右上角那一枚小标（原型 `emptyArt(kind)`）。
type ArtKind string

const (
	// ArtPlain 只有唱片 + 封套。
	ArtPlain ArtKind = ""
	// ArtSearch 加一枚放大镜（搜不到）。
	ArtSearch ArtKind = "search"
	// ArtDownload 加一枚主色下箭头（下载记录空）。
	ArtDownload ArtKind = "download"
	// ArtClock 加一枚钟（稍后再看 / 历史记录空）。
	ArtClock ArtKind = "clock"
	// ArtLock 加一把锁（要登录）。
	ArtLock ArtKind = "lock"
)

// 插画 160×120（原型 `.empty svg.art`）。
const (
	ArtW = 160
	ArtH = 120
)

// artSVG 是原型 `emptyArt` 那张图：线条 currentColor（text-3），唱片中心主色，
// 封套和小标的底是页面底色。
func artSVG(kind ArtKind, accent, bg string) string {
	extra := ""
	switch kind {
	case ArtSearch:
		extra = `<circle cx="118" cy="44" r="14" fill="` + bg + `" stroke="currentColor" stroke-width="2"/><path d="m128 54 10 10" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>`
	case ArtDownload:
		extra = `<path d="M120 30v26m-9-9 9 9 9-9" stroke="` + accent + `" stroke-width="2.4" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`
	case ArtClock:
		extra = `<circle cx="120" cy="42" r="13" fill="` + bg + `" stroke="currentColor" stroke-width="2"/><path d="M120 35v7l5 3" stroke="` + accent + `" stroke-width="2" fill="none" stroke-linecap="round"/>`
	case ArtLock:
		extra = `<rect x="108" y="38" width="24" height="20" rx="3" fill="` + bg + `" stroke="currentColor" stroke-width="2"/><path d="M113 38v-5a7 7 0 0 1 14 0v5" stroke="currentColor" stroke-width="2" fill="none"/><circle cx="120" cy="48" r="2.5" fill="` + accent + `"/>`
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 120" width="160" height="120">` +
		`<circle cx="88" cy="62" r="40" fill="none" stroke="currentColor" stroke-width="2"/>` +
		`<circle cx="88" cy="62" r="30" fill="none" stroke="currentColor" stroke-opacity=".45"/>` +
		`<circle cx="88" cy="62" r="22" fill="none" stroke="currentColor" stroke-opacity=".45"/>` +
		`<circle cx="88" cy="62" r="9" fill="` + accent + `"/><circle cx="88" cy="62" r="2" fill="` + bg + `"/>` +
		`<rect x="18" y="20" width="78" height="84" rx="4" fill="` + bg + `" stroke="currentColor" stroke-width="2"/>` +
		`<path d="M30 88h40M30 80h26" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>` +
		extra + `</svg>`
}

var artCache sync.Map // key → *ui.SVG

func hex(c ui.Color) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// EmptyArt 画空状态那张插画（160×120）。颜色跟着主题和强调色走；解析结果按
// （kind, 主色, 底色）缓存，不会每帧解析。
func EmptyArt(c *ui.Context, kind ArtKind) ui.Element {
	p := pal(c)
	a, b := hex(p.Accent), hex(p.Bg)
	k := string(kind) + a + b
	v, ok := artCache.Load(k)
	if !ok {
		v, _ = artCache.LoadOrStore(k, ui.MustParseSVG([]byte(artSVG(kind, a, b))))
	}
	return ui.Image(c, v.(*ui.SVG)).Size(ArtW, ArtH).TextColor(p.Text3)
}

// Empty 是空状态（原型 `.empty`）：插画（下面空 16）+ 24 号宋体标题 + 次要色说明
// （最宽约 40 字，下面空 16）+ 可选的操作区，整块居中，上下 72、左右 24。
//
//	pud.Empty(c, pud.ArtClock, "还没有稍后再看", "在视频卡片上点「稍后再看」……", func() {
//		pud.Button(c, "go-home", pud.Primary, pud.Std, "去首页逛逛", "")
//	})
func Empty(c *ui.Context, kind ArtKind, title, body string, action func()) {
	p := pal(c)
	ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(space.Px72, space.Px24).Gap(space.Px8).
		Children(func() {
			EmptyArt(c, kind).Margin(space.None, space.None, space.Px16, space.None)
			typo.Display(ui.Text(c, title), scale.Font(c, scale.SectionTitle)).TextAlign(ui.Center)
			if body != "" {
				text(c, body, scale.Size14, p.Text2).MaxWidth(360).TextAlign(ui.Center).
					Margin(space.None, space.None, space.Px16, space.None)
			}
			if action != nil {
				ui.Row(c).Gap(space.Px12).Children(action)
			}
		})
}

// LoginPrompt 是「登录后查看」（原型 `loginPrompt`）：锁的插画 + 「登录后查看{what}」
// + 「{what}保存在你的 B 站账号里，登录之后会自动同步过来。」+ 主色「扫码登录」键；
// 1 像素 line-strong 虚线框、圆角 12、上下 64 左右 24、离上面 32。返回登录键被点了没有。
func LoginPrompt(c *ui.Context, key, what string) (login bool) {
	p := pal(c)
	ui.Column(c).Key(key).FillWidth().AlignItems(ui.Center).Padding(space.Px64, space.Px24).Gap(space.Px8).
		Margin(space.Px32, space.None, space.None, space.None).
		Draw(func(pp *ui.Painter, r ui.Rect) { pp.StrokeDashed(r, p.LineStrong, tok.R12, 1) }).
		Children(func() {
			EmptyArt(c, ArtLock).Margin(space.None, space.None, space.Px16, space.None)
			typo.Display(ui.Text(c, "登录后查看"+what), scale.Font(c, scale.SectionTitle)).TextAlign(ui.Center)
			text(c, what+"保存在你的 B 站账号里，登录之后会自动同步过来。", scale.Size14, p.Text2).
				MaxWidth(420).TextAlign(ui.Center).Margin(space.None, space.None, space.Px16, space.None)
			login = Button(c, key+":login", Primary, Std, "扫码登录", "user").Clicked()
		})
	return login
}

// Skeleton 是一块骨架（原型 `.sk`）：surface-2 底、圆角 radius（0 = 8），一道 hover
// 色的光带 1.2 秒一趟从左扫到右；系统开了「减少动态效果」时光带不动。
func Skeleton(c *ui.Context, key string, w, h, radius float32) ui.Element {
	p := pal(c)
	if radius == 0 {
		radius = tok.R8
	}
	e := ui.Box(c).Key(key).Size(w, h)
	visible := shimmer.VisibleFlag()
	return e.Draw(func(pp *ui.Painter, r ui.Rect) {
		shimmer.Paint(pp, r, radius, p.Surface2, p.Hover, visible)
	})
}

// SkeletonRow 是曲目行的骨架（原型 `.sk-row`：高 60、上下各空 4）。
func SkeletonRow(c *ui.Context, key string, w float32) ui.Element {
	return Skeleton(c, key, w, tok.RowH, tok.R8).Margin(space.Px4, space.None)
}

// composite 把半透明的 fg 合到不透明的 bg 上。
func composite(bg, fg ui.Color) ui.Color {
	return bg.Mix(ui.Color{R: fg.R, G: fg.G, B: fg.B, A: 255}, float32(fg.A)/255)
}
