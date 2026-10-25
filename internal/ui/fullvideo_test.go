package ui

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/service/video"
	mygoui "github.com/egoist/mygo/ui"
)

func videoTester(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	if !video.Supported() {
		t.Skip("这个平台没有视频解码器")
	}
	s, tt := onlineTester(t, multiPages(), false)
	b, err := os.ReadFile("../service/video/testdata/frag.mp4")
	if err != nil {
		t.Fatal(err)
	}
	s.app.SetVideoOpener(func(_ context.Context, _ app.Item, pos time.Duration) (*video.Session, error) {
		return video.Start(video.Config{TempDir: t.TempDir(), Fetch: func(_ context.Context, from, to int64) ([]byte, error) {
			return b[from:min(to, int64(len(b)))], nil
		}}, pos), nil
	})
	s.openFullPlayer()
	tt.Frame()
	return s, tt
}

func TestAnOnlineVideoShowsAPlayBadgeOnTheCover(t *testing.T) {
	_, tt := videoTester(t)
	if _, ok := tt.Find("看视频"); !ok {
		t.Fatalf("封面上没有播放角标，文字有 %q", tt.Texts())
	}
}

func TestALocalFileHasNoPlayBadge(t *testing.T) {
	s, tt, _ := fullTester(t)
	s.openFullPlayer()
	tt.Frame()
	if _, ok := tt.Find("看视频"); ok {
		t.Fatal("本地文件没有视频，不该有角标")
	}
}

func TestClickingTheBadgeOpensAVideoWindowThatEntersAndLeavesTheTheater(t *testing.T) {
	s, tt := videoTester(t)
	if err := tt.Click("看视频"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "视频小窗出现", func() bool { _, ok := tt.Find("视频，点击全屏"); return ok })
	r, _ := tt.Find("视频，点击全屏")
	if got, want := r.H/r.W, float32(9)/16; got < want-0.01 || got > want+0.01 {
		t.Errorf("小窗 %.0f×%.0f，应当是 16:9", r.W, r.H)
	}
	if _, ok := tt.Find("看视频"); ok {
		t.Error("视频开着，角标还在")
	}
	waitFrames(t, tt, "第一帧到了", func() bool { return s.vv.bm != nil })

	if err := tt.Click("视频，点击全屏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tr, ok := tt.Find("视频，点击退出全屏")
	if !ok {
		t.Fatalf("没进影院，文字有 %q", tt.Texts())
	}
	if tr.W < fullWinW-1 || tr.H < fullWinH-1 {
		t.Errorf("影院只有 %.0f×%.0f，应当铺满整窗", tr.W, tr.H)
	}
	if err := tt.Click("视频，点击退出全屏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("视频，点击全屏"); !ok {
		t.Fatal("没退回小窗")
	}
}

func TestEscLeavesTheTheaterBeforeItClosesTheFullPlayer(t *testing.T) {
	s, tt := videoTester(t)
	if err := tt.Click("看视频"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "视频小窗出现", func() bool { _, ok := tt.Find("视频，点击全屏"); return ok })
	s.full.theater = true
	tt.Frame()
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.full.theater || !s.full.open {
		t.Fatalf("第一下 Esc 应该只退出影院：theater=%v open=%v", s.full.theater, s.full.open)
	}
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.full.open {
		t.Fatal("第二下 Esc 应该关掉满屏")
	}
}

func TestTheCloseKeyStopsTheVideoAndBringsTheBadgeBack(t *testing.T) {
	s, tt := videoTester(t)
	if err := tt.Click("看视频"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "视频小窗出现", func() bool { _, ok := tt.Find("关闭视频"); return ok })
	if err := tt.Click("关闭视频"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.app.VideoPhase() != app.VideoOff {
		t.Fatal("没关掉")
	}
	if _, ok := tt.Find("看视频"); !ok {
		t.Fatal("关掉之后角标没回来")
	}
}

func TestKaraokeSweepsAcrossTheLine(t *testing.T) {
	rows := []lyrics.Row{{At: 10 * time.Second, Text: "a"}, {At: 20 * time.Second, Text: "b"}}
	for _, c := range []struct {
		now  time.Duration
		want float32
	}{{9 * time.Second, 0}, {10 * time.Second, 0}, {14250 * time.Millisecond, 0.5}, {19 * time.Second, 1}} {
		if got := karaokeFrac(rows, 0, c.now); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("at %v: %.2f, want %.2f", c.now, got, c.want)
		}
	}
	if got := karaokeFrac(rows, 1, 22*time.Second); got <= 0 || got >= 1 {
		t.Errorf("last line at 2s: %.2f", got)
	}
}
