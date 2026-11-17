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

// TestRenderMiniHeartStates 出迷你窗那颗心的实图（浅色 / 深色 × 实心 / 空心）。
func TestRenderMiniHeartStates(t *testing.T) {
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
				s, main, fb := miniLikeEnv(t)
				fav := 0
				if filled {
					fav = 1
				}
				fb.pages = [][]map[string]any{{
					{"id": 42, "title": "我的喜欢", "attr": 2, "media_count": 1, "fav_state": fav},
				}}
				main.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})
				it, ok := main.app.PlayingItem()
				if !ok {
					t.Fatal("迷你没有正在放的那一首")
				}
				tt := miniTester(s)
				tt.SetDark(dark)
				waitFor(t, "喜欢状态回来", func() bool {
					tt.Frame()
					_, known := main.app.FavoriteInBound(it)
					return known
				})
				save(t, dir, "mini-heart-"+theme+"-"+state+".png", tt)
			})
		}
	}
}

// TestRenderNotchHeartStates 出刘海展开卡里那颗心的实图。刘海永远是黑底，所以
// 只有实心 / 空心两张，不按主题分。
func TestRenderNotchHeartStates(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, filled := range []bool{false, true} {
		state := "outline"
		if filled {
			state = "filled"
		}
		t.Run(state, func(t *testing.T) {
			n, main := newNotchShell(t)
			f := newBiliFixture(t, 1)
			fb := newFolderBili(t, nil)
			fav := 0
			if filled {
				fav = 1
			}
			fb.pages = [][]map[string]any{{
				{"id": 42, "title": "我的喜欢", "attr": 2, "media_count": 1, "fav_state": fav},
			}}
			main.app.MountStreaming(app.Streaming{
				Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
			})
			main.app.MountCollections(app.CollectionRefs{Client: fixtureClient(t, fb), Settle: 0})
			main.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})
			if err := main.app.Enqueue(likeItem()); err != nil {
				t.Fatalf("把在线的那首放进队列：%v", err)
			}
			waitFor(t, "在线那首开始播", func() bool { return main.app.Snapshot().HasTrack })
			// 暂停之后再拍：频谱 / 波形一直在请求帧，刘海形状动画永远停不下来，
			// 拍出来是一张随机相位的图。
			main.app.Toggle()
			it, ok := main.app.PlayingItem()
			if !ok {
				t.Fatal("刘海没有正在放的那一首")
			}
			n.notchSt.open = true
			tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
			n.repaint = tt.Frame
			settleNotch(t, tt)
			waitFor(t, "喜欢状态回来", func() bool {
				tt.Frame()
				_, known := main.app.FavoriteInBound(it)
				return known
			})
			save(t, dir, "notch-heart-"+state+".png", tt)
		})
	}
}
