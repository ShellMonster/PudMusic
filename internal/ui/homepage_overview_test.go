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
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "新歌速递") && showsText(tt, "本周热榜") })
	for _, name := range []string{"接着听", "历史记录", "本周热榜", "新歌速递"} {
		if !showsText(tt, name) {
			t.Fatalf("总览上没有「%s」区块", name)
		}
	}
	if showsText(tt, "鬼畜") {
		t.Fatal("总览上不该有旧的分类 Tab")
	}
}

func TestRenderHomeOverviewShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	startOnOverview(t)
	_, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "首页原型首屏", func() bool {
		return showsText(tt, "今晚，") && showsText(tt, "慢一点") && showsText(tt, "听。") && showsText(tt, "本周热榜")
	})
	save(t, dir, "140-首页总览.png", tt)
	tt.Scroll(700, 400, 0, 600)
	waitFrames(t, tt, "首页原型下半屏", func() bool { return showsText(tt, "新歌速递") })
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

func TestTheSidebarRecommendEntryLeadsBackToTheOverview(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "本周热榜") })
	s.navigate(homePath)
	waitFrames(t, tt, "回到总览", func() bool { return showsText(tt, "本周热榜") })
}
