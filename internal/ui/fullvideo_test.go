package ui

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/ShellMonster/PudMusic/internal/service/video"
	mygoui "github.com/egoist/mygo/ui"
)

func videoTester(t *testing.T) (*Shell, *mygoui.Tester) {
	s, tt, _ := videoTesterQN(t)
	return s, tt
}

// videoTesterQN 另外给出一份「每次开视频用了哪一档」的记录。
func videoTesterQN(t *testing.T) (*Shell, *mygoui.Tester, *[]bili.VideoQuality) {
	t.Helper()
	var qns []bili.VideoQuality
	if !video.Supported() {
		t.Skip("这个平台没有视频解码器")
	}
	s, tt := onlineTester(t, multiPages(), false)
	b, err := os.ReadFile("../service/video/testdata/frag.mp4")
	if err != nil {
		t.Fatal(err)
	}
	s.app.SetVideoOpener(func(_ context.Context, _ app.Item, pos time.Duration, qn bili.VideoQuality) (*video.Session, error) {
		qns = append(qns, qn)
		return video.Start(video.Config{TempDir: t.TempDir(), Fetch: func(_ context.Context, from, to int64) ([]byte, error) {
			return b[from:min(to, int64(len(b)))], nil
		}}, pos), nil
	})
	s.openFullPlayer()
	tt.Frame()
	return s, tt, &qns
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

func TestTheTwoFullscreenKeysAreDistinctAndOnlyTheWindowOneEntersTheTheater(t *testing.T) {
	s, tt := videoTester(t)
	if err := tt.Click("看视频"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "视频小窗出现", func() bool { _, ok := tt.Find("视频，点击全屏"); return ok })
	in, ok1 := tt.Find("窗口内全屏")
	scr, ok2 := tt.Find("屏幕全屏")
	if !ok1 || !ok2 {
		t.Fatalf("两枚全屏键没都在，文字有 %q", tt.Texts())
	}
	if in.X == scr.X && in.Y == scr.Y {
		t.Fatal("两枚键叠在一起")
	}
	if g1, g2 := scanGlyph(tt, "窗口内全屏"), scanGlyph(tt, "屏幕全屏"); g1.ink == g2.ink {
		t.Error("两枚键画的是同一个图标，分不出来")
	}
	if err := tt.Click("窗口内全屏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !s.full.theater {
		t.Fatal("窗口内全屏没进影院")
	}
	if _, ok := tt.Find("退出窗口内全屏"); !ok {
		t.Fatalf("影院里没有退出键，文字有 %q", tt.Texts())
	}
	if err := tt.Click("退出窗口内全屏"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.full.theater {
		t.Fatal("没退出影院")
	}
}

func TestTheQualityMenuListsTheAvailableTiersAndRestartsInPlace(t *testing.T) {
	s, tt, qns := videoTesterQN(t)
	if err := tt.Click("看视频"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "视频小窗出现", func() bool { _, ok := tt.Find("视频，点击全屏"); return ok })
	if _, ok := tt.Find("清晰度"); ok {
		t.Fatal("只有一档（或不知道有几档）时不该画画质键")
	}
	s.app.SetVideoQualitiesForTest([]bili.VideoQuality{bili.VideoQuality360, bili.VideoQuality480, bili.VideoQuality720, bili.VideoQuality1080}, bili.VideoQuality720)
	tt.Frame()
	if err := tt.Click("清晰度"); err != nil {
		t.Fatalf("没有画质键：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	for _, q := range []string{"360P", "480P", "720P", "1080P"} {
		if _, ok := tt.Find(q); !ok {
			t.Fatalf("菜单里没有 %s，文字有 %q", q, tt.Texts())
		}
	}
	s.full.theater = true
	tt.Frame()
	if err := tt.Click("1080P"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "按新画质重开", func() bool { return len(*qns) == 2 })
	if (*qns)[1] != bili.VideoQuality1080 {
		t.Errorf("重开用的画质 %v，应当是 1080P", (*qns)[1])
	}
	if !s.full.theater || s.app.VideoPhase() == app.VideoOff {
		t.Error("换画质不该退出影院或关掉视频")
	}
}
