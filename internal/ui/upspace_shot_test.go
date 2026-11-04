// UP 主空间的画面验收（T3，原型 `PAGES.user`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderUpSpaceShots
//
// 深浅两套 × 投稿 / 动态 两个 Tab，图都是假接口现画的 PNG（占位色块看不出取色带对不对）。
// 对照 `ref/{dark,light}-user-works.png`、`*-user-posts.png`，见收尾报告。
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

// TestRenderUpSpaceShots 把 UP 空间存成 PNG。
func TestRenderUpSpaceShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			st := app.DefaultSettings("")
			st.ThemeMode = app.ThemeModeLight
			if dark {
				st.ThemeMode = app.ThemeModeDark
			}
			b := newUIExplore(t)
			png := func(path string, a, c color.RGBA) {
				b.on(path, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(upspaceShotPNG(480, 270, a, c))
				})
			}
			png("/shot/face.png", color.RGBA{64, 84, 140, 255}, color.RGBA{50, 66, 120, 255})
			png("/shot/cover.png", color.RGBA{190, 60, 50, 255}, color.RGBA{120, 30, 30, 255})
			b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"mid": 42, "name": "夜航电台", "sign": "每晚十一点，放一张老唱片。", "level": 6,
					"face": b.URL + "/shot/face.png",
				})
			})
			b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"follower": 124000, "following": 86})
			})
			b.on("/x/space/upstat", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{"likes": 1830000})
			})
			b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"relation": map[string]any{"attribute": 2}, "be_relation": map[string]any{"attribute": 0},
				})
			})
			b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
				vs := []any{}
				for i, t := range []string{"说到爱 · 蔡健雅", "渺小 · 田馥甄", "寓言 · 王菲", "新房客 · 王菲"} {
					vs = append(vs, map[string]any{
						"bvid": "BV1up" + string(rune('a'+i)), "title": t, "length": "4:12", "cid": "9",
						"pic": b.URL + "/shot/cover.png", "play": 1915000, "created": 1700000000,
					})
				}
				uiExploreJSON(w, 0, map[string]any{
					"list": map[string]any{"vlist": vs}, "page": map[string]any{"count": 5},
				})
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
			s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark), withSettings(st))
			openSpace(t, s, tt, 42)
			sp := s.upSpaces[42]
			waitFrames(t, tt, "投稿加载完", func() bool { return sp.videos.Paged().Loaded > 0 })
			waitFrames(t, tt, "图下完", func() bool {
				return s.images.Get(b.URL+"/shot/face.png").Ready() && s.images.Get(b.URL+"/shot/cover.png").Ready()
			})
			waitFrames(t, tt, "取色", func() bool { _, sat := s.tint.sync(b.URL+"/shot/face.png", func() {}); return sat > 0 })
			tt.Move(1180, 300)
			homeFrame(tt)
			save(t, dir, name+"-user-works.png", tt)

			sp.tab = upTabDynamic
			tt.Frame()
			tt.Frame()
			waitFrames(t, tt, "动态请求发出", func() bool { return b.hit("/x/polymer/web-dynamic/v1/feed/space") })
			waitFrames(t, tt, "动态加载完", func() bool { return len(sp.dynItems) > 0 })
			waitFrames(t, tt, "图下完", func() bool { return s.images.Get(b.URL + "/shot/cover.png").Ready() })
			homeFrame(tt)
			save(t, dir, name+"-user-posts.png", tt)
		})
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
