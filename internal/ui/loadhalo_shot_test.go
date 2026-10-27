package ui

// 「正在取流」时绕着播放键转的那一圈光：出图。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLoadHaloShots
//
// 这一族出的是**播放条那一格**（放大 4 倍）、**整条播放条**（看第三行那句话
// 和光圈是不是吵）、还有**满屏播放器那一枚**，深浅各一套，外加转到的几个
// 不同位置——转动的光圈**位置**是要看的（有一段在正上方和有一段在正下方是
// 两回事）。
//
// ⚠️ **装的是应用自己的主题**：不装的话画的是框架默认那套（蓝的），而这一
// 族要看的正是应用实际的样子（主色那一档）。

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// shotTester 造一个只画播放条的外壳（装应用的主题），喂一份手摆的状态。
func shotTester(t *testing.T, st app.State, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	return shotTesterPreset(t, st, dark, app.ThemePresetWarmPaper)
}

// shotTesterPreset 造一个只画播放条的外壳，用**指定的主题方案**。
func shotTesterPreset(t *testing.T, st app.State, dark bool, preset app.ThemePreset) (*Shell, *mygoui.Tester) {
	t.Helper()
	settings := app.DefaultSettings("")
	settings.ThemePreset = preset
	b := newTestShell(t, at(1200, coverWinH), systemIsDark(dark), withSettings(settings),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.viewPlayBar(c, st)
			}
		}))
	tt := b.tt
	tt.Frame()
	return b.s, tt
}

// shotFullTester 造一个只画满屏播放器的外壳（装应用的主题）。
func shotFullTester(t *testing.T, st app.State, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, at(1200, 800), systemIsDark(dark),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.full.open = true
				s.viewFullPlayer(c, st)
			}
		}))
	tt := b.tt
	tt.Frame()
	return b.s, tt
}

func TestRenderLoadHaloShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := app.State{
				HasTrack: true, Title: "正在取的那一首", Source: app.SourceOnline,
				Duration: 3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeSequence,
				Loading: true,
			}
			_, tt := shotTester(t, st, dark)
			// 转到的几个位置：0（12 点）、1/4 圈、1/2 圈（6 点）。
			for i, d := range []time.Duration{0, 350 * time.Millisecond, 700 * time.Millisecond} {
				if d > 0 {
					tt.Advance(d)
					tt.Frame()
				}
				saveHaloCrop(t, dir, "01-播放条-取流光圈-"+name+"-"+strconv.Itoa(i)+".png", tt, 540, 0, 120, 76)
			}
			// 整条播放条（看第三行那句话和光圈是不是吵）。
			saveCrop(t, dir, "02-播放条-取流整条-"+name+".png", tt, 0, 76)

			// 满屏播放器那一枚（64×64 的实心圆，光圈比播放条那一枚大一圈）。
			_, full := shotFullTester(t, st, dark)
			full.Advance(350 * time.Millisecond)
			full.Frame()
			saveHaloCrop(t, dir, "03-满屏-取流光圈-"+name+".png", full, 500, 640, 200, 160)

			// 迷你播放器那一枚（24 的实心圆，最小的一枚）。
			saveHaloCrop(t, dir, "04-迷你窗-取流光圈-"+name+".png", miniHaloTester(t, st), 0, 0, 80, 60)

			// **另外两套主题方案**各看一眼（暖纸·赤陶是默认的那一套，
			// 上面几张就是它）：光圈是 `t.Accent`，而强调色**逐套不同**——
			// 所以每一套都要看得出「那一圈光还在」。
			for _, preset := range []app.ThemePreset{app.ThemePresetDuskAmber, app.ThemePresetClassic} {
				_, pt := shotTesterPreset(t, st, dark, preset)
				pt.Advance(350 * time.Millisecond)
				pt.Frame()
				saveHaloCrop(t, dir, "05-播放条-取流光圈-"+string(preset)+"-"+name+".png", pt, 540, 0, 120, 76)
			}
		})
	}
}

// miniHaloTester 造一个只画迷你播放器那一枚播放键的外壳（装应用的主题）。
func miniHaloTester(t *testing.T, st app.State) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, at(320, 84), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewMiniPlay(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	tt.Advance(350 * time.Millisecond)
	tt.Frame()
	return tt
}

// saveHaloCrop 存画面上 (x0,y0) 起 w×h 那一块（放大 4 倍，看得清那一圈光）。
func saveHaloCrop(t *testing.T, dir, name string, tt *mygoui.Tester, x0, y0, w, h int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := tt.Image()
	b := img.Bounds()
	if x0+w > b.Dx() {
		w = b.Dx() - x0
	}
	if y0+h > b.Dy() {
		h = b.Dy() - y0
	}
	const zoom = 4
	sub := image.NewRGBA(image.Rect(0, 0, w*zoom, h*zoom))
	for y := 0; y < h*zoom; y++ {
		for x := 0; x < w*zoom; x++ {
			sub.Set(x, y, img.RGBAAt(x0+x/zoom, y0+y/zoom))
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", filepath.Join(dir, name))
}
