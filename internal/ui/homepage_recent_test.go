package ui

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func recentShell(t *testing.T) *testShell {
	t.Helper()
	b := homeDiscoverFake(t)
	now := time.Now()
	b.on(uiHomeHistory, func(w http.ResponseWriter, _ *http.Request) {
		rows := []map[string]any{}
		for i, ago := range []time.Duration{2 * time.Hour, 30 * time.Hour, 24 * 5 * time.Hour} {
			rows = append(rows, map[string]any{
				"title": "听过的歌" + string(rune('甲'+i)), "cover": "https://f/h.jpg",
				"author_name": "某UP", "author_mid": 900 + i,
				"view_at": now.Add(-ago).Unix(), "progress": 75 * (i + 1), "duration": 300,
				"kid": 1000 + i, "history": map[string]any{"bvid": "BV1h" + string(rune('a'+i)), "cid": 1, "business": "archive"},
			})
		}
		uiExploreJSON(w, 0, map[string]any{"has_more": false, "list": rows})
	})
	store := app.NewMemoryStore("home-test")
	if err := store.Save(context.Background(), app.Session{SESSDATA: "sess", DedeUserID: "42"}); err != nil {
		t.Fatal(err)
	}
	return newTestShell(t, at(homeW, homeH), mounted(func(_ *testing.T, a *app.App) {
		transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountLogin(store, cli)
		a.MountRecommend(cli)
	}))
}

func TestRecentPlaysShowWhenTheyWereWatched(t *testing.T) {
	sh := recentShell(t)
	tt := sh.tt
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeRecentTab]); err != nil {
		t.Fatal(err)
	}
	waitFramesClock(t, tt, "最近播放", func() bool { return showsText(tt, "听过的歌甲") })
	for _, want := range []string{"观看时间", "今天", "昨天", "5天前", "上次听到这里", "今天 1 首  ·  昨天 1 首  ·  更早 1 首"} {
		if !showsText(tt, want) {
			t.Errorf("最近播放上没有「%s」：%q", want, tt.Texts())
		}
	}
}

func TestRenderHomeRecentShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	sh := recentShell(t)
	tt := sh.tt
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeRecentTab]); err != nil {
		t.Fatal(err)
	}
	waitFramesClock(t, tt, "最近播放", func() bool { return showsText(tt, "听过的歌甲") })
	save(t, dir, "160-最近播放.png", tt)
}
