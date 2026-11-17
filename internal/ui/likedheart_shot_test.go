// 喜欢心形的视觉验收图：播放条上那一枚在深浅两套主题、实心 / 空心两档下的样子。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLikedHeartStates
//
// 画的就是播放条用的那一枚 `pud.IconBtn`（`heart` / `heart-on`，空心次要色、
// 实心主色），铺在页面底色上——浅色和深色下都要读得出来。
package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// heartShotView 是「只摆这一枚心」的那一帧。
func heartShotView(filled bool) func(*Shell) func(*mygoui.Context) {
	return func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			p := tok.Of(c.Theme())
			name, label := "heart", "喜欢"
			if filled {
				name, label = "heart-on", "取消喜欢"
			}
			mygoui.Column(c).Fill().Background(p.Bg).Center().Children(func() {
				pud.IconBtn(c, "shot-heart", name, label, pud.IconOpts{Row: true, On: filled})
			})
		}
	}
}

// TestRenderLikedHeartStates 出四张图：浅色 / 深色 × 空心 / 实心。
func TestRenderLikedHeartStates(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, filled := range []bool{false, true} {
			state := "outline"
			if filled {
				state = "filled"
			}
			t.Run(theme+"-"+state, func(t *testing.T) {
				b := newTestShell(t, at(160, 120), systemIsDark(dark), withView(heartShotView(filled)))
				b.tt.Frame()
				for range 30 {
					if !b.tt.FramePending() {
						break
					}
					b.tt.Advance(16e6)
				}
				save(t, dir, "liked-heart-"+theme+"-"+state+".png", b.tt)
			})
		}
	}
}

// TestRenderPlaybarHeartStates 出播放条上那颗心的实图：浅色 / 深色 ×
// 已喜欢 / 没喜欢。和上面那一组只有图标本身不同，这一组看的是它在真实
// 播放条里的位置、大小和配色——视觉验收要的是后者。
func TestRenderPlaybarHeartStates(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, filled := range []bool{false, true} {
			state := "outline"
			if filled {
				state = "filled"
			}
			t.Run(theme+"-"+state, func(t *testing.T) {
				_, tt := playbarHeartShotEnv(t, dark, filled)
				save(t, dir, "playbar-heart-"+theme+"-"+state+".png", tt)
				label := "喜欢"
				if filled {
					label = "取消喜欢"
				}
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("播放条上找不到「%s」，文字有 %q", label, tt.Texts())
				}
				saveRect(t, dir, "playbar-heart-"+theme+"-"+state+"-zoom.png", tt,
					int(r.X)-12, int(r.Y)-12, int(r.W)+24, int(r.H)+24)
			})
		}
	}
}

// playbarHeartShotEnv 把播放条摆成「绑着一格、这一首在 / 不在里面」。
func playbarHeartShotEnv(t *testing.T, dark, faved bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	env := newLikeEnv(t, false, false, false)
	s, tt := env.s, env.tt
	tt.SetDark(dark)
	if err := s.app.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	fav := 0
	if faved {
		fav = 1
	}
	fb := newFolderBili(t, nil)
	fb.pages = [][]map[string]any{{
		{"id": 42, "title": "我的喜欢", "attr": 2, "media_count": 1, "fav_state": fav},
	}}
	s.app.MountCollections(app.CollectionRefs{Client: fixtureClient(t, fb), Settle: 0})
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})
	tt.Frame()
	it, ok := s.app.PlayingItem()
	if !ok {
		t.Fatal("播放条上没有正在放的那一首")
	}
	waitFrames(t, tt, "喜欢状态回来", func() bool {
		_, known := s.app.FavoriteInBound(it)
		return known
	})
	return s, tt
}
