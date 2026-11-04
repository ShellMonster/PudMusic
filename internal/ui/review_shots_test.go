package ui

// 视觉审查出图：设 BIU_SHOT_DIR 才存。首页六个 Tab、满屏播放器、收藏夹头图、设置页、骨架屏，深浅各一套。

import (
	"os"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

func reviewDir(t *testing.T) string {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	return dir
}

func themeName(dark bool) string {
	if dark {
		return "深色"
	}
	return "浅色"
}

func TestRenderReviewFullPlayer(t *testing.T) {
	dir := reviewDir(t)
	for _, dark := range []bool{false, true} {
		_, tt, _ := fullTester(t)
		tt.SetDark(dark)
		openFull(t, tt)
		for range 5 {
			tt.Advance(300 * time.Millisecond)
		}
		save(t, dir, "30-满屏播放器-"+themeName(dark)+".png", tt)
	}
}

func TestRenderReviewCollectionAndSettings(t *testing.T) {
	dir := reviewDir(t)
	for _, dark := range []bool{false, true} {
		_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
		tt.SetDark(dark)
		waitSongs(t, tt, s, 3)
		tt.Advance(500 * time.Millisecond)
		tt.Frame()
		save(t, dir, "40-收藏夹头图-"+themeName(dark)+".png", tt)

		b := newTestShell(t, at(1200, 800), systemIsDark(dark))
		b.s.navigate(settingsPath)
		b.tt.Frame()
		b.tt.Frame()
		save(t, dir, "50-设置-"+themeName(dark)+".png", b.tt)
		b.Close()

		for _, p := range []struct{ n, path string }{{"稍后再看", laterPath}, {"用户动态", dynamicFeedPath}} {
			k := newTestShell(t, append(blockedBiliOpts(t, true), at(1200, 800), systemIsDark(dark))...)
			k.s.navigate(p.path)
			k.tt.Frame()
			k.tt.Frame()
			k.tt.Frame()
			save(t, dir, "60-骨架屏-"+p.n+"-"+themeName(dark)+".png", k.tt)
			k.Close()
		}
	}
}

var _ = mygoui.Color{}
