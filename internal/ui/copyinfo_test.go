package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

func TestCurrentSongInfoHasNoEmptyLines(t *testing.T) {
	online := currentSongInfo(app.State{Title: "晴天", Duration: 185 * time.Second},
		app.Item{Source: "online", Type: "mv", Bvid: "BV1xx", Artist: "周杰伦"})
	want := "晴天 - 周杰伦\n时长 3:05\nBV1xx\n" + app.BiliLink(app.Item{Source: "online", Type: "mv", Bvid: "BV1xx"})
	if online != want {
		t.Errorf("在线歌曲信息 = %q，want %q", online, want)
	}
	local := currentSongInfo(app.State{Title: "本地歌", Path: "/music/a.flac"}, app.Item{Source: "local"})
	if local != "本地歌\n/music/a.flac" {
		t.Errorf("本地歌曲信息 = %q", local)
	}
}
