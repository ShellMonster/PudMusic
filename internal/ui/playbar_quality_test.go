package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"

	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的音质徽章。
//
// 这几个断言本来没人写，于是徽章画没画对只有评审截一张图才知道。
// 写下来之后，「换流之后徽章跟着变」这条规格就不用人肉验了。

// qualityBarTester 只画播放条。
func qualityBarTester(t *testing.T, st app.State) *mygoui.Tester {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c)
		s.viewPlayBar(c, st)
	}, 760, coverWinH)
	tt.Frame()
	return tt
}

// TestQualityBadgeShowsForAnOnlineTrack 盯徽章画出来了。
func TestQualityBadgeShowsForAnOnlineTrack(t *testing.T) {
	for _, badge := range []string{"无损 FLAC", "320K", "128K", "64K", "杜比全景声"} {
		tt := qualityBarTester(t, app.State{HasTrack: true, Title: "在线的一首", Quality: badge})
		if !tt.HasText(badge) {
			t.Errorf("徽章 %q 没画出来，画面上的字：%q", badge, tt.Texts())
		}
	}
}

// TestQualityBadgeFollowsTheRefresh 盯换流之后徽章会变。
//
// 徽章是**每帧从 State 现读**的，不是开歌那一刻定下来的。把它缓存住的话，
// B 站把 320K 降成 128K 时用户看到的还是「320K」——无声地降级会让人以为
// 自己还在听原来那一档。
func TestQualityBadgeFollowsTheRefresh(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	st := app.State{HasTrack: true, Title: "在线的一首", Quality: "320K"}
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c)
		s.viewPlayBar(c, st) // ← 闭包读的是外面那个变量
	}, 760, coverWinH)

	tt.Frame()
	if !tt.HasText("320K") {
		t.Fatalf("一开始的徽章没画出来：%q", tt.Texts())
	}

	st.Quality = "128K"
	tt.Frame()
	if tt.HasText("320K") {
		t.Errorf("换流之后还在显示旧徽章：%q", tt.Texts())
	}
	if !tt.HasText("128K") {
		t.Errorf("新徽章没画出来：%q", tt.Texts())
	}
}

// TestNoQualityBadgeForALocalFile 盯本地文件不画徽章。
//
// 那是文件格式（FLAC / AAC），不是「音质」——拿格式凑一个徽章会让「无损」
// 这两个字在两个意思之间飘。
func TestNoQualityBadgeForALocalFile(t *testing.T) {
	tt := qualityBarTester(t, app.State{HasTrack: true, Title: "本地的一首"})
	for _, text := range tt.Texts() {
		if text == "FLAC" || text == "AAC" || text == "M4A" {
			t.Errorf("本地文件画出了 %q，那不是音质", text)
		}
	}
}

// TestNoQualityBadgeWhenThereIsNoTrack 盯没歌时不画。
func TestNoQualityBadgeWhenThereIsNoTrack(t *testing.T) {
	tt := qualityBarTester(t, app.State{})
	if tt.HasText("320K") {
		t.Errorf("还没有歌就画出了徽章：%q", tt.Texts())
	}
}

// TestErrorShowsNextToTheTitle 盯出错时播放条上有一句话。
//
// 「取流失败不是空白」这条规格的界面那一半：网络断了、解码失败、地址换不到，
// 都要有话可说，而且和歌名挨着（用户看歌名时自然会看到）。
func TestErrorShowsNextToTheTitle(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack: true,
		Title:    "在线的一首",
		Quality:  "320K",
		Err:      "连不上 B 站这条音频流了（网络或地址的问题，不是歌坏了）",
	})
	if !tt.HasText("打不开：连不上 B 站这条音频流了（网络或地址的问题，不是歌坏了）") {
		t.Errorf("播放条上没有那句错误：%q", tt.Texts())
	}
	if !tt.HasText("在线的一首") {
		t.Errorf("出错时歌名不见了：%q", tt.Texts())
	}
}
