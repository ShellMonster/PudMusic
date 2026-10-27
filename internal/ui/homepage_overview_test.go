package ui

import (
	"os"
	"testing"
	"time"
)

func startOnOverview(t *testing.T) {
	t.Helper()
	homeStartsOnOverview = true
	t.Cleanup(func() { homeStartsOnOverview = false })
}

func TestTheOverviewShowsTilesAndLeadsIntoTheListPages(t *testing.T) {
	startOnOverview(t)
	_, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "新歌速递") && showsText(tt, "排行榜") })
	for _, name := range []string{"热榜", "原创榜", "最近播放", "为你推荐"} {
		if !showsText(tt, name) {
			t.Fatalf("总览上没有「%s」入口", name)
		}
	}
	waitFrames(t, tt, "迷你榜", func() bool { return showsText(tt, "热榜第一名") && showsText(tt, "原创榜第一名") })
	if showsText(tt, "鬼畜") {
		t.Fatal("总览上不该有 Tab 那一排")
	}
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜列表", func() bool { return showsText(tt, "鬼畜") })
	if err := tt.Click("返回发现"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "回到总览", func() bool { return showsText(tt, "排行榜") && !showsText(tt, "鬼畜") })
}

func TestRenderHomeOverviewShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	startOnOverview(t)
	_, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })
	save(t, dir, "140-首页总览.png", tt)
	tt.Scroll(700, 400, 0, 600)
	waitFrames(t, tt, "迷你榜", func() bool { return showsText(tt, "查看全部") })
	save(t, dir, "141-首页总览-滚到榜单.png", tt)
}

func TestTheHeroBannerAdvancesByItselfAndStopsWhenHovered(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "横幅", func() bool { return len(s.home.heroSongs()) > 1 })
	waitFrames(t, tt, "横幅起了表", func() bool { return !s.home.heroAt.IsZero() })
	if s.home.heroIdx != 0 {
		t.Fatalf("一开始应停在第 1 张，实际 %d", s.home.heroIdx)
	}
	tt.Advance(homeHeroEvery + time.Second)
	waitFrames(t, tt, "自动换到下一张", func() bool { return s.home.heroIdx == 1 })
}
