package ui

// 榜单页出图：设 BIU_SHOT_DIR 才存。看前三名次的主色放大和顶上「第几期 · 更新」那一行。

import (
	"net/http"
	"os"
	"strconv"
	"testing"
)

func TestRenderHomeBoardShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := homeDiscoverFake(t)
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, _ *http.Request) {
		rows := make([]map[string]any, 0, 6)
		for i := 1; i <= 6; i++ {
			rows = append(rows, map[string]any{
				"music_id": "M" + strconv.Itoa(i), "music_title": "榜上第" + strconv.Itoa(i) + "首", "singer": "榜上歌手",
				"creation_bvid": "BV1board" + strconv.Itoa(i), "creation_aid": i,
				"creation_cover": "https://f/board.jpg", "creation_duration": 120 + i, "creation_play": 1000 * i,
			})
		}
		uiExploreJSON(w, 0, map[string]any{"list": rows})
	})
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeHotTab]); err != nil {
		t.Fatal(err)
	}
	waitFramesClock(t, tt, "热榜", func() bool { return showsText(tt, "榜上第6首") })
	save(t, dir, "130-热榜.png", tt)
}

func TestRenderHomeRecentEmptyAndSearchHotShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := homeDiscoverFake(t)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(searchLabel); err != nil {
		t.Fatal(err)
	}
	waitFramesClock(t, tt, "大家在听", func() bool { return showsText(tt, suggestHotHeading) })
	tt.Move(900, 500)
	settleSuggestEnter(t, tt)
	save(t, dir, "131-搜索框空着-大家在听.png", tt)
}
