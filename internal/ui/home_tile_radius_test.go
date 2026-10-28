package ui

// 首页总览上两张「插画封面」的圆角（票：首页有一点圆角什么的问题）。
//
// # 问题是什么
//
// 磁贴（热榜 / 原创榜那一排）右侧和新歌速递横幅右侧各压着一张插画封面，都
// `Attach(AnchorTopRight)` 贴在容器右上角。它们原来调 `imgcache.ViewPlaceholderTitled`
// 时把**圆角传了 0**——画出来的图片自己是**直角**，「看起来是圆的」全靠外层
// 容器的 `Clip()` 兜底。
//
// 而 `Attach` 是**绝对定位**：绝对定位的孩子挂在这一层的根上，外层容器的
// 裁剪**罩不住它**——用户撞上的就是那个：封面的右上 / 右下两个角是方的，
// 凸出在磁贴的圆角外面。
//
// # 钉法
//
// 🔴 **插画自己的圆角必须等于所在容器的圆角**——两个数写两遍的话，改了一处
// 另一处就悄悄对不上（磁贴的圆角和插画不一致，用户看到的是「插画比容器方
// 一圈」）。所以两个半径各有一个具名出口（`homeTileRadius` / `homeHeroRadius`），
// 容器和插画**共用同一个函数**。
//
// 第二条在**独立渲染**下量像素：不套容器、不靠任何外层裁剪，插画那一格自己
// 的右上角就该是圆的——那才是「自己有圆角」的证明。

import (
	"image/color"
	"net/http"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheHomeTileRadiusMatchesItsContainer 断**磁贴 / 横幅的圆角出口算出来的
// 就是「圆角令牌 + 那一档的偏移」**。
//
// 拦的是有人把它改回写死的 0——那正是原来那个缺陷：插画传 0，图片画成直角。
func TestTheHomeTileRadiusMatchesItsContainer(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			th := BuildTheme(app.DefaultSettings(""), dark)
			if got := homeTileRadius(th); got != th.Radius+2 {
				t.Errorf("磁贴圆角 = %.1f，want %.1f（圆角令牌 + 2）——"+
					"插画那一格要和容器同一个数，传 0 的话图片画成直角", got, th.Radius+2)
			}
			if got := homeHeroRadius(th); got != th.Radius+4 {
				t.Errorf("横幅圆角 = %.1f，want %.1f（圆角令牌 + 4）", got, th.Radius+4)
			}
		})
	}
}

// TestTheHomeTileArtIsRoundedOnItsOwn 盯**插画那一格自己有圆角**。
//
// 🔴 **判据是「右上角的角是圆的」，而且渲染时**不套**磁贴那一层**：插画挂在
// 容器右上角靠的是 `Attach`（绝对定位），外层容器的裁剪罩不住绝对定位的孩子
// ——「外层有 Clip 所以看起来是圆的」在真机上不成立（用户撞上的就是它）。
// 所以插画那一格**自己**得有圆角。
//
// ⚠️ **量的是右上角**：磁贴的插画贴在右上角，凸出的就是那两个角（右上 / 右下）；
// 左侧两个角在渐变底下看不见，量它没有意义。
func TestTheHomeTileArtIsRoundedOnItsOwn(t *testing.T) {
	const (
		w = 220.0
		h = homeTileH
	)
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: redImageTransport{}}})
	cache.AddWake(func() {})
	t.Cleanup(cache.Close)

	th := BuildTheme(app.DefaultSettings(""), false)
	isRed := func(c color.RGBA) bool { return c.R > 180 && c.G < 120 && c.B < 120 }

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Column(c).Fill().Background(th.Background).Children(func() {
			mygoui.Box(c).Size(w, h).Background(th.Background).Children(func() {
				imgcache.ViewPlaceholderTitled(c, cache, "https://img/cover.jpg", "",
					w, h, homeTileRadius(th), nil)
			})
		})
	}, int(w)+2*int(space.Px8), int(h)+2*int(space.Px8))

	// 图在后台那一趟拉，画出来要等几帧；中心是封面色才算就绪。
	ready := false
	for range 300 {
		tt.Frame()
		im := tt.Image()
		if isRed(im.RGBAAt(im.Bounds().Dx()/2, im.Bounds().Dy()/2)) {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("插画那一格没画出封面")
	}

	im := tt.Image()
	// 右上角内 1~3px：圆角外面 → 背景色；直角 → 封面色。
	for _, d := range []int{1, 2, 3} {
		c := im.RGBAAt(im.Bounds().Dx()-1-d, 1+d)
		if isRed(c) {
			t.Errorf("右上角内 %dpx 处量到封面色 %v——插画那一格是直角"+
				"（圆角传了 0，靠外层裁剪兜底，而绝对定位的孩子罩不住）", d, c)
		}
	}
	// 中心照旧是封面色（证明量的就是这一格）。
	if !isRed(im.RGBAAt(im.Bounds().Dx()/2, im.Bounds().Dy()/2)) {
		t.Fatalf("中心不是封面色——测试自己摆错了")
	}
}
