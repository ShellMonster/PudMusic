// UP 空间头图横幅的出图（票 #630 的视觉审查）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSpaceBannerShots
//
// # ⚠️ **要出两张**：有头图 / 没头图
//
// 单看第一张的话，「画不出来」和「画对了」长得一样——因为「画不出来」的那
// 一张上，头图区是一块**空白的渐变**，而「没有头图」的 UP 主本来就是那块
// 渐变。**两张并排才看得出「这一块是不是被图铺满了」**。
//
// # 跟什么比
//
// 上游 Biu 那一侧的头图画法（`src/pages/user-profile/space-info.tsx`）：
//
//	`background: linear-gradient(蒙版), url(头图) center/cover no-repeat`
//
// 即**居中 + cover（铺满裁切）** + 一层黑蒙版。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。头图、头像
// 都从**假图床**取——那台机器给一张现画的图（`bannerShotPNG`），不这么做
// 的话画面上全是占位色块，头图区那一块根本看不出横幅有没有画对。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderSpaceBannerShots 把 UP 空间那一页存成 PNG：有头图 / 没头图各一张。
func TestRenderSpaceBannerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		shade := "浅色"
		if dark {
			shade = "深色"
		}
		t.Run(shade, func(t *testing.T) {
			shotSpaceWithBanner(t, dir, shade, dark)
			shotSpaceWithoutBanner(t, dir, shade, dark)
		})
	}
}

// bannerShotShell 造一个 UP 空间的壳，那台假 B 站给的横幅地址指向假图床。
//
// ⚠️ **图是假图床给的**：头图那一张要**能看出是横幅**（一条横向的渐变带），
// 而头像要是**另一种颜色**——否则「数横幅的像素」会把头像也算进去（那是
// 这一票的测试踩过的坑，见 `upspace_banner_test.go` 的 `bannerBlue`）。
func bannerShotShell(t *testing.T, dark bool, banner string, pics *bannerPicServer) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newUIExplore(t)
	b.on("/shot/face.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bannerShotPNG(240, 240, color.RGBA{64, 96, 128, 255}, color.RGBA{32, 56, 88, 255}))
	})
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "枫叶_Official", "sign": "每周三更新，记得点关注。", "level": 6,
			"face":         b.URL + "/shot/face.png",
			"top_photo_v2": map[string]string{"l_200h_img": banner},
			"official":     map[string]any{"role": 1, "title": "哔哩哔哩音乐认证"},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"follower": 21294500, "following": 12})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": 0},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, nil)
	})
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{
				uiDynamicArchive("v1", "BV1upBiu"),
				uiDynamicArchive("v2", "BV1upBiu2"),
			},
			"offset": "", "has_more": false,
		})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
	installBannerImages(t, s, pics)
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	return s, tt
}

// shotSpaceWithBanner 存「有头图」的那一张。
func shotSpaceWithBanner(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	// 横幅那一张给一条**横向的渐变带**：一眼看得出是横幅，而且**左右两端颜色
	// 不同**——中心裁剪把两端切掉的话，画面上就看出来了。
	pics := &bannerPicServer{}
	pics.bannerImg = bannerShotPNG(1200, 400,
		color.RGBA{92, 62, 44, 255}, color.RGBA{214, 132, 78, 255})
	_, tt := bannerShotShell(t, dark, "https://i0.hdslb.com/bfs/archive/banner.png", pics)
	// ⚠️ **等的是「横幅那一带有图」**，不是「有那个红色」——出图这一组给的
	// 横幅是**渐变带**，不是纯红色（纯色看不出「这是横幅」）。
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerDistinctPixels(t, tt, bannerBox(t, tt), bannerPageBackdrop(t, tt)) > 1000
	})
	tt.Move(1180, 300)
	homeFrame(tt)
	save(t, dir, "空间横幅-有头图-"+name+".png", tt)
}

// shotSpaceWithoutBanner 存「没头图」的那一张。
func shotSpaceWithoutBanner(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	// 头图地址是空串：B 站对大多数 UP 主就是给空对象。
	pics := &bannerPicServer{}
	_, tt := bannerShotShell(t, dark, "", pics)
	tt.Move(1180, 300)
	homeFrame(tt)
	save(t, dir, "空间横幅-没头图-"+name+".png", tt)
}

// bannerShotPNG 画一张斜着渐变的图（当头图 / 头像用）。
func bannerShotPNG(w, h int, a, b color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float32(x+y) / float32(w+h)
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(float32(a.R) + t*float32(b.R-a.R)),
				G: uint8(float32(a.G) + t*float32(b.G-a.G)),
				B: uint8(float32(a.B) + t*float32(b.B-a.B)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
