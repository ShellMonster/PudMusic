package hover

import (
	"image"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// 这一族量**这一包自己画出来的那两圈影**（票 #179 的两处）。
//
//	网格卡片右下角那个主色圆播放钮	上游 `music-card/index.tsx:51` 的 `shadow-2xl`
//	「新歌速递」那张封面		上游 `new-music-top.tsx:234` 的 `shadow-md`
//
// ⚠️ **影是画在元素外面那一圈的**，而这一包画的东西浮在一张**图**上——所以
// 「有没有影」这件事在画面上就是「贴着它的那一圈有没有变暗」。判据是
// **加影 / 不加影两张画面的同一块地方**：别的什么都不差，差的那一点就是影。
//
// ⚠️ **那个播放钮的影会被封面裁掉一半**：它在封面那个 `overflow-hidden` 的框里
// （上游 `music-card/index.tsx:33` 也是这样），而 `shadow-2xl` 是往下偏 25 的
// ——所以看得见的是它**左边**那一圈。

// shadowBackdrop 是这一族那台机器的底色。
//
// ⚠️ **必须是浅色**：这一包的两处影都是**黑**的（`shadow-2xl` 25% 黑、`shadow-md`
// 10% 黑），压在深底上只差一个色阶——而这一包画的东西是浮在一张图上的，那张图
// 是什么颜色我们不知道，所以这里用一块**自己铺的浅灰底**。
const shadowBackdrop = 200

// coverShadowTester 造一台只画一块封面的画面，底是一块浅灰。
func coverShadowTester(t *testing.T, cache *imgcache.Cache, draw func(c *ui.Context)) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(200, 200).Padding(40).
			Background(ui.RGBA(shadowBackdrop, shadowBackdrop, shadowBackdrop, 255)).
			Children(func() { draw(c) })
	}, testerW, testerH)
	tt.Frame()
	return tt
}

// darker 报 with 那张图在 (x,y) 上比 without 那张图暗。
func darker(with, without *image.RGBA, x, y int) bool {
	a, b := with.RGBAAt(x, y), without.RGBAAt(x, y)
	return int(a.R) < int(b.R) || int(a.G) < int(b.G) || int(a.B) < int(b.B)
}

// TestTheGridPlayButtonHasAShadow 钉住**网格卡片右下角那个播放钮**有影。
//
// 上游是 `<div className="bg-primary rounded-full shadow-2xl">`
// （`components/music-card/index.tsx:51`）——`0 25px 50px -12px rgb(0 0 0/0.25)`，
// **只有一层，颜色是 25% 黑**。
//
// ⚠️ **看的是它左边那一圈**：这一枚是浮在封面里的一枚（`Attach`），而封面是裁着
// 的（`Clip`），所以影的右边和下边都被裁掉了——上游也一样（那一枚在
// `overflow-hidden` 的封面里）。
func TestTheGridPlayButtonHasAShadow(t *testing.T) {
	cache := newCache(t)

	// 两次渲染：指针不在 / 指针在。别的什么都不差。
	off := coverShadowTester(t, cache, func(c *ui.Context) {
		CardCover(c, cache, "", 80, 80, false, "", "", "甲", nil)
	})
	on := coverShadowTester(t, cache, func(c *ui.Context) {
		CardCover(c, cache, "", 80, 80, true, "", "", "甲", nil)
	})
	// 淡入要走完（200ms），不然影还在半路上。
	settle(on)

	btn, ok := on.Find(FromLabel("甲"))
	if !ok {
		t.Fatalf("指针移上去之后没有那个播放钮：%q", on.Texts())
	}
	// 影落在按钮**左边**：往左 6 像素那一列。
	x := int(btn.X) - 6
	y := int(btn.Y + btn.H/2)
	if !darker(on.Image(), off.Image(), x, y) {
		t.Errorf("播放钮左边 (%d,%d) 那个点没有变暗——`shadow-2xl` 没画出来", x, y)
	}
}

// TestTheNewMusicCoverHasAShadow 钉住**「新歌速递」那张封面**有影。
//
// 上游是 `<Image ... className="rounded-medium shadow-md">`
// （`pages/music-recommend/new-music-top.tsx:234`）——
// `0 4px 6px -1px rgb(0 0 0/0.1), 0 2px 4px -2px rgb(0 0 0/0.1)`。
//
// ⚠️ **只有这一处要影**：网格卡片那张封面**没有**（上游 `music-card/index.tsx:32`
// 的 `<Image>` 上没有任何 shadow 类），所以 `CardCover`（那一档）不带影，
// `CardCoverOne`（这一档）带。
func TestTheNewMusicCoverHasAShadow(t *testing.T) {
	cache := newCache(t)
	on := false
	tt := coverShadowTester(t, cache, func(c *ui.Context) {
		CardCoverOne(c, cache, "", 80, 80, on, "甲", nil)
	})
	// 封面自己**没有**标签（标签在那个播放钮上），所以它的位置是**从播放钮推
	// 出来的**：那一枚是贴在封面右下角、离右边和下边各 8 的。
	on = true
	tt.Frame()
	settle(tt)
	play, ok := tt.Find(PlayLabel("甲"))
	if !ok {
		t.Fatalf("指针移上去之后没有那个播放钮：%q", tt.Texts())
	}
	const coverSide = 80
	coverX := play.X - (coverSide - btnInset - btn)
	coverY := play.Y - (coverSide - btnInset - btn)

	img := tt.Image()
	// 影落在封面**下面**那一块（那一档影的纵向偏移是 +4）。
	x, y := int(coverX+coverSide/2), int(coverY+coverSide)+1
	ref := img.RGBAAt(x, y+30)
	cur := img.RGBAAt(x, y)
	if int(cur.R) >= int(ref.R) || int(cur.G) >= int(ref.G) || int(cur.B) >= int(ref.B) {
		t.Errorf("封面下面 (%d,%d) 是 %v，离它三十像素的同一样底色是 %v——"+
			"`shadow-md` 没画出来", x, y, cur, ref)
	}
}
