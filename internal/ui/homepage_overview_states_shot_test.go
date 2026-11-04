package ui

// 首页「发现」总览那三块在**三种取数状态**下的出图：设 BIU_SHOT_DIR 才存。
//
// 要看的是三件事：
//
//	① 拉失败   那三块都还在画面上，而且说得清为什么 + 有重试（不是一片空白）
//	② 还在拉   那三块摆的是骨架（扫光 + 形状），不是空白、也不是转圈
//	③ 真的没有 那三块说的是「还没有」，**没有**重试
//
// ⚠️ **②和①是同一块地方的两种样子**：高度要一样（那一块在总览的最上面，
// 高度变了下面那些行会跳一下）。

import (
	"net/http"
	"os"
	"testing"
)

// TestRenderOverviewStateShots 出总览那三块的三种状态。
func TestRenderOverviewStateShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	startOnOverview(t)

	// ① 拉失败：三摊都失败。
	fail := homeDiscoverFake(t)
	fail.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	fail.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	fail.on(uiHomeToplistPeriods, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	_, tt, _ := newHomeShell(t, fail)
	waitFrames(t, tt, "拉失败了", func() bool { return showsText(tt, overviewFailText) })
	homeFrame(tt)
	save(t, dir, "160-首页总览-拉失败.png", tt)

	// ② 还在拉：三摊都不回来（骨架那一档）。
	loading := blockedHomeShell(t)
	waitFrames(t, loading.tt, "总览在拉", func() bool {
		return showsText(loading.tt, loadingBoardText) && showsText(loading.tt, loadingNewMusicText)
	})
	homeFrame(loading.tt)
	save(t, dir, "161-首页总览-还在拉.png", loading.tt)

	// ③ 真的没有：拉回来了，但是空的。
	empty := homeDiscoverFake(t)
	empty.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	empty.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	empty.on(uiHomeToplistMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	_, tt2, _ := newHomeShell(t, empty)
	waitFrames(t, tt2, "空的那几摊都回来了", func() bool {
		return showsText(tt2, overviewEmptyText)
	})
	homeFrame(tt2)
	save(t, dir, "162-首页总览-没有内容.png", tt2)
}
