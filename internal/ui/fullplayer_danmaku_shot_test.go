// 出图：满屏播放器左边那张封面上的弹幕长什么样（BMG-306）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDanmakuShots
//
// # 出哪几张
//
//	满屏-弹幕-深色   整窗（1200×800），满屏开着，封面上有十二条弹幕
//	满屏-弹幕-浅色   同一副样子，浅色主题
//	封面特写-深色     封面那一块（360×270）放大四倍——量轨道、字号、缝
//	封面特写-浅色     同上
//
// # ⚠️ **出图不是审查**
//
// 出图只是材料；**审查**（逐张打开比过五项：布局 / 间距 / 层级 / 密度 / 对齐）
// 才是那道门。差别写在收尾说明里。
//
// ⚠️ **参考是研究票 #538 的报告 §3.3**，不是上游 Biu——**上游 Biu 没有弹幕**
// （报告 §6 第 6 条），所以没有可对照的东西。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// shotDanmaku 是出图时摆的那十二条弹幕。
//
// ⚠️ **时间是排好的**（理由同 `budgetDanmaku`）：前八条各占一条轨道，后四条
// 等 1.8 秒之后复用前四条轨道——于是十二条**同时**在屏幕上（这正是「同屏
// 上限十二条」那一档）。
//
// ⚠️ **长度和颜色是故意不一样的**：短的看字号，长的看「会不会被折行」，
// 几种颜色看「颜色保不保留 B 站那一份」（报告 §3.3：颜色照 B 站，统一乘
// alpha 0.85）。
func shotDanmaku() []app.DanmakuItem {
	rows := []struct {
		text  string
		color uint32
	}{
		{"前奏好好听", 0xFFFFFF},
		{"这个循环了一晚上", 0xFF0000},
		{"副歌进来那一下", 0x00FF00},
		{"弹幕护体", 0x0000FF},
		{"哈哈哈哈哈哈哈哈哈哈哈哈", 0xFFFFFF},
		{"收藏了", 0xFFFF00},
		{"前排", 0xFF00FF},
		{"打卡 2026-10-27", 0x00FFFF},
		{"这个音色是真的好", 0xFFFFFF},
		{"单曲循环中", 0xFF0000},
		{"好家伙", 0x00FF00},
		{"这一段直接封神", 0xFFFFFF},
	}
	items := make([]app.DanmakuItem, 0, len(rows))
	for i, r := range rows {
		// 前八条：39500 起每条隔 0.2 秒；后四条：41300 起（隔了 1.8 秒，
		// 「前一条走出 30% 宽度」的条件满足了 ⇒ 复用前四条轨道）。
		t := int64(39500 + i*200)
		if i >= 8 {
			t = int64(41300 + (i-8)*200)
		}
		items = append(items, app.DanmakuItem{
			ID: "shot-" + r.text, TimeMS: t, Mode: 1, Color: r.color, Text: r.text,
		})
	}
	return items
}

// grayCoverPNG 造一张**中灰**的封面。
//
// ⚠️ **不用 `splitCoverPNG`**：那一张是「一半黑一半白」，而弹幕里有几条是白色
// 的——白字压在白的那一半上，**出图里一个字都看不见**，于是那张图量不出任何
// 东西。中灰上白字和彩字都看得见。
//
// ⚠️ **上面压一块深色**：弹幕要从右边进=right 沿那儿有一条**竖着的深色带**
// （封面图自己的），而白字压在深色上才看得出「半透明」——那是这一档要看的
// 东西之一（报告 §3.3：统一乘 alpha 0.85）。
func grayCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// 底：深灰；右边三分之一压一条更深的（模拟封面图里的暗部）。
			c := color.RGBA{R: 58, G: 58, B: 64, A: 255}
			if x > w*2/3 {
				c = color.RGBA{R: 26, G: 26, B: 30, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRenderDanmakuShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := grayCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name string
		mode app.ThemeMode
	}{
		{"深色", app.ThemeModeDark},
		{"浅色", app.ThemeModeLight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// ⚠️ **主题要自己摆**：界面用的是**应用设置里的那个主题**（不是
			// Tester 的偏好），而默认是「跟着系统」——于是两套出图会长得一模一样。
			settings := app.DefaultSettings("")
			settings.ThemeMode = tc.mode
			env := danmakuShell(t, shotDanmaku(),
				withSettings(settings),
				systemIsDark(tc.mode == app.ThemeModeDark))
			// ⚠️ **给一张真封面**：出图要看的是「弹幕压在封面上」——压在一块
			// 平色占位上，看不出半透明对不对。
			env.st.CoverURL = srv.URL + "/cover.png"
			waitFrames(t, env.tt, "封面下回来了", func() bool {
				return env.s.images.Get(env.st.CoverURL).Ready()
			})
			env.tt.Frame()

			save(t, dir, "306-满屏-弹幕-"+tc.name+".png", env.tt)
			// 封面那一块（360×270）放大四倍：轨道、字号、缝都量得出来。
			saveHaloCrop(t, dir, "306-封面特写-"+tc.name+".png", env.tt,
				danmakuCoverX, danmakuCoverY, danmakuCoverW, danmakuCoverH)
		})
	}
}
