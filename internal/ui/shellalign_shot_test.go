package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 外壳对齐原型那一票的自查图（顶栏、播放条、设置、登录；宽窗口 1280 和窄窗口 960 各一套）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderShellAlignShots
//
// 只用内存夹具和本地 FLAC 素材，不碰真 B 站。
func TestRenderShellAlignShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, w := range []int{1280, 960} {
		for _, dark := range []bool{true, false} {
			name := "浅色"
			if dark {
				name = "深色"
			}
			suffix := name + "-" + itoa(w)
			t.Run(suffix, func(t *testing.T) {
				b := newTestShell(t, systemIsDark(dark), at(w, 800), withSettings(app.DefaultSettings("")))
				s, tt := b.s, b.tt
				openATrack(t, s)
				s.navigate("/settings")
				waitFrames(t, tt, "设置页", func() bool { return tt.HasText("常规") })
				tt.Move(float32(w)-40, 400)
				homeFrame(tt)
				save(t, dir, "shell-settings-"+suffix+".png", tt)

				tt.Scroll(float32(w)/2, 400, 0, 300)
				homeFrame(tt)
				save(t, dir, "shell-settings-scrolled-"+suffix+".png", tt)

				s.navigate(loginPath)
				waitFrames(t, tt, "登录页", func() bool { return tt.HasText("扫码登录") })
				settleHome(tt)
				homeFrame(tt)
				save(t, dir, "shell-login-"+suffix+".png", tt)
			})
		}
	}
}

// TestRenderShellAlignAvatarShots 存头像外环和头像菜单头部（40 头像 + 昵称 + UID）。
func TestRenderShellAlignAvatarShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		f := newAvatarFixture(t)
		f.withALoginIn()
		_, tt := newAvatarShellIn(t, f, "2", dark)
		waitFrames(t, tt, "头像图到了", func() bool { return avatarShowsThePicture(tt) })
		openAvatarMenu(t, tt)
		tt.Move(600, 400)
		settleHome(tt)
		homeFrame(tt)
		save(t, dir, "shell-avatar-menu-"+name+".png", tt)
	}
}
