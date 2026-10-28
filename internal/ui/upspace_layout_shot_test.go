// UP 空间整页的画面验收（票 #268 / BMG-268）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderUpSpaceLayoutShots
//
// # 四套方案 × 深浅两套都要出
//
// 这一票改的是**底色、圆角、边框、留白**——而这几样每一套方案都不一样。
// 只出浅色的话深色下那一层根本看不出来（浅色和深色是两套值）。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
//
// # 图也是假接口给的
//
// 头图、头像、封面都从**假 B 站**那个服务器上取——那台服务器给一张现画的
// PNG（`upspaceShotPNG`）。不这么做的话画面上全是占位色块，**头图区那一块
// 根本看不出横幅有没有画对**（占位色是「控件那一层」的灰，和横幅的渐变
// 差得很远）。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestRenderUpSpaceLayoutShots 把 UP 空间那一页存成 PNG。
func TestRenderUpSpaceLayoutShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, preset := range []app.ThemePreset{
		app.ThemePresetWarmPaper, app.ThemePresetDuskAmber,
		app.ThemePresetSoftPeach, app.ThemePresetClassic,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			name := string(preset) + "-" + shade
			t.Run(name, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}

				b := newUIExplore(t)
				// 三张图都从假服务器上取（横幅 / 头像 / 封面）。
				b.on("/shot/banner.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(1200, 400, color.RGBA{92, 62, 44, 255}, color.RGBA{168, 85, 46, 255}))
				})
				b.on("/shot/face.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(240, 240, color.RGBA{64, 96, 128, 255}, color.RGBA{32, 56, 88, 255}))
				})
				b.on("/shot/cover.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(480, 270, color.RGBA{72, 84, 96, 255}, color.RGBA{40, 48, 60, 255}))
				})

				// 头 / 关系 / 粉丝数三个接口：这一份自己写，不复用 `uiSpaceData`
				// （那一份的头图地址是 `https://f/banner.jpg`，取不到）。
				b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{
						"mid": 42, "name": "枫叶_Official", "sign": "每周三更新，记得点关注。", "level": 6,
						"face":         b.URL + "/shot/face.png",
						"top_photo_v2": map[string]string{"l_200h_img": b.URL + "/shot/banner.png"},
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
							upspaceShotArchive(b.URL+"/shot/cover.png", "v1", "BV1upBiu"),
							upspaceShotText("t1", "今天更新了一期视频，记得来看。"),
						},
						"offset": "", "has_more": false,
					})
				})

				// 登录的是**别人**（42），这样「+ 关注」那一枚才会画出来。
				s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark), withSettings(st))
				openSpace(t, s, tt, 42)
				sp := s.upSpaces[42]
				waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
				// 等图下完（头图、头像、封面三张）。
				waitFrames(t, tt, "图下完", func() bool {
					return s.images.Get(b.URL+"/shot/banner.png").Ready() &&
						s.images.Get(b.URL+"/shot/face.png").Ready() &&
						s.images.Get(b.URL+"/shot/cover.png").Ready()
				})
				tt.Move(1180, 300)
				homeFrame(tt)
				save(t, dir, "UP空间-"+name+".png", tt)

				// 动态流那一页：**同一个组件**（`viewDynamicCard`），所以它也得
				// 一起看——改坏了不会只坏在 UP 空间这一页。
				ex := newUIExplore(t)
				ex.on("/shot/face.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(240, 240, color.RGBA{64, 96, 128, 255}, color.RGBA{32, 56, 88, 255}))
				})
				ex.on("/shot/cover.png", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(480, 270, color.RGBA{72, 84, 96, 255}, color.RGBA{40, 48, 60, 255}))
				})
				ex.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{
						"items": []any{
							upspaceShotArchive(ex.URL+"/shot/cover.png", "v1", "BV1upBiu"),
							upspaceShotText("t1", "今天更新了一期视频，记得来看。"),
						},
						"offset": "", "has_more": false,
					})
				})
				s2, tt2, _ := newSpaceShell(t, ex, true, "77", systemIsDark(dark), withSettings(st))
				s2.navigate(dynamicFeedPath)
				tt2.Frame()
				waitFrames(t, tt2, "动态流加载完", func() bool {
					return tt2.HasText(dynMoreLabel)
				})
				waitFrames(t, tt2, "图下完", func() bool {
					return s2.images.Get(ex.URL + "/shot/cover.png").Ready()
				})
				tt2.Move(1180, 300)
				homeFrame(tt2)
				save(t, dir, "动态流-"+name+".png", tt2)
			})
		}
	}
}

// upspaceShotPNG 画一张斜着渐变的图（当头图 / 头像 / 封面用）。
func upspaceShotPNG(w, h int, a, b color.RGBA) []byte {
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

// upspaceShotArchive 造一条投稿型动态（封面地址给真的那张图）。
func upspaceShotArchive(cover, id, bvid string) map[string]any {
	m := uiDynamicArchive(id, bvid)
	mods := m["modules"].(map[string]any)
	arch := mods["module_dynamic"].(map[string]any)["major"].(map[string]any)["archive"].(map[string]any)
	arch["cover"] = cover
	arch["title"] = "【纯音乐】雨后的街道 · 循环了一整天的钢琴曲"
	arch["desc"] = "这一段是我自己录的，副歌那里换了一次和声。"
	arch["duration_text"] = "3:20"
	arch["stat"] = map[string]any{"play": "2129.45万"}
	return m
}

// upspaceShotText 造一条纯文字动态（作者名与时间照参考图那一套）。
func upspaceShotText(id, text string) map[string]any {
	m := uiDynamic(id, text)
	mods := m["modules"].(map[string]any)
	author := mods["module_author"].(map[string]any)
	author["name"] = "枫叶_Official"
	author["pub_time"] = "50 分钟前"
	author["pub_action"] = "投稿了视频"
	return m
}
