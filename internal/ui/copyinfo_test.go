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

// 没有封面的歌：播放条封面和满屏封面的菜单一样——「复制歌曲信息」可选，「复制封面」
// 灰着且名字里说明原因；灰的那项选不中。键盘和读屏走系统原生菜单，认的就是这些标签。
func TestTheCoverMenuExplainsWhyCopyCoverIsGreyedOut(t *testing.T) {
	for _, full := range []bool{false, true} {
		_, tt, _ := fullTester(t)
		x, y := float32(40), float32(760) // 播放条封面
		if full {
			openFull(t, tt)
			tt.Advance(1500 * time.Millisecond)
			x, y = 370, 400 // 满屏封面
		}
		tt.RightClickAt(x, y)
		tt.Frame()
		got := tt.Menu()
		want := []string{"复制歌曲信息", "复制封面（这首歌没有封面）"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("满屏=%v 封面右键菜单 = %q，want %q", full, got, want)
		}
		if err := tt.ChooseMenuItem("复制封面（这首歌没有封面）"); err == nil {
			t.Errorf("满屏=%v 灰着的「复制封面」居然选得中", full)
		}
		if err := tt.ChooseMenuItem("复制歌曲信息"); err != nil {
			t.Fatalf("选「复制歌曲信息」：%v", err)
		}
		tt.Frame()
		if !tt.HasText("已复制歌曲信息") {
			t.Errorf("满屏=%v 复制完没有提示，文字有 %q", full, tt.Texts())
		}
	}
}
