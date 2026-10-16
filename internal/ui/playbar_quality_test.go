package ui

import (
	"fmt"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"

	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的音质徽章。
//
// 这几个断言本来没人写，于是徽章画没画对只有评审截一张图才知道。
// 写下来之后，「换流之后徽章跟着变」这条规格就不用人肉验了。

// qualityBarTester 只画播放条。
func qualityBarTester(t *testing.T, st app.State, width ...int) *mygoui.Tester {
	t.Helper()
	w := 760
	if len(width) > 0 {
		w = width[0]
	}
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c)
		s.viewPlayBar(c, st)
	}, w, coverWinH)
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

// TestTheErrorSitsUnderTheTitle 盯出错时歌名还是一整行。
//
// # 为什么这条不能只断「字在不在」
//
// 出错时界面变成什么样，只有**位置**能说清：「有一句话」在歌名被挤成
// 一字一行的竖条、又被 88 高的播放条裁掉的时候**照样成立**——字确实画了，
// 只是没人看得见。所以这里断的是两个 Rect 的关系：
//
//	错误那一行的上边在歌名那一行的下边之下（竖着堆，不是并排）
//	两行都还在播放条里面（没被裁）
//
// 曾经的写法就是把 `Column` 换成一个裸 `Row`，错误文字变成了外层那一行的
// 第三个孩子，跑到歌名右边去了。`TestErrorShowsNextToTheTitle` 那时候是绿的。
func TestTheErrorSitsUnderTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt := qualityBarTester(t, app.State{
				HasTrack: true,
				Title:    "这一首歌的名字",
				Quality:  "无损 FLAC",
				Err:      "连不上 B 站这条音频流了",
			}, width)

			title, okT := tt.Find("这一首歌的名字")
			errBox, okE := tt.Find("打不开：连不上 B 站这条音频流了")
			if !okT || !okE {
				t.Fatalf("宽度 %d：歌名在不在 %v，错误在不在 %v，画面的字：%q", width, okT, okE, tt.Texts())
			}

			if errBox.Y < title.Y+title.H {
				t.Errorf("宽度 %d：错误 {%.1f,%.1f %.1fx%.1f} 没有排在歌名 {%.1f,%.1f %.1fx%.1f} 的下面"+
					"——它跑到歌名右边去了，歌名会被挤成竖条", width,
					errBox.X, errBox.Y, errBox.W, errBox.H,
					title.X, title.Y, title.W, title.H)
			}
			// 两行都在播放条里面。播放条 88 高，歌名在行内垂直居中。
			if title.H > playbarHeight || errBox.Y+errBox.H > playbarHeight {
				t.Errorf("宽度 %d：内容被 88 高的播放条裁了（歌名 %.1f 高，错误下边在 y=%.1f）",
					width, title.H, errBox.Y+errBox.H)
			}
			// 歌名横向要占得开——被压到 40px 以下就说明已经是竖排了。
			if title.W < 40 {
				t.Errorf("宽度 %d：歌名只有 %.1f 宽（%.1f 高），已经被压成竖条了",
					width, title.W, title.H)
			}
		})
	}
}

// TestTheTitleAndBadgeShareOneLine 盯徽章不把歌名顶到下一行去。
func TestTheTitleAndBadgeShareOneLine(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack: true,
		Title:    "一首歌",
		Quality:  "无损 FLAC",
	})

	title, ok := tt.Find("一首歌")
	if !ok {
		t.Fatalf("歌名不在画面上：%q", tt.Texts())
	}
	badge, ok := tt.Find("无损 FLAC")
	if !ok {
		t.Fatalf("徽章不在画面上：%q", tt.Texts())
	}
	// 并排：徽章的左边在歌名右边之后，两者在同一行（垂直方向有重叠）。
	if badge.X < title.X+title.W {
		t.Errorf("徽章 {%.1f,%.1f} 压在歌名 {%.1f,%.1f %.1fx%.1f} 上面了",
			badge.X, badge.Y, title.X, title.Y, title.W, title.H)
	}
	if badge.Y >= title.Y+title.H || badge.Y+badge.H <= title.Y {
		t.Errorf("徽章 {%.1f,%.1f %.1fx%.1f} 和歌名 {%.1f,%.1f %.1fx%.1f} 不在同一行",
			badge.X, badge.Y, badge.W, badge.H, title.X, title.Y, title.W, title.H)
	}
	if badge.H > playbarHeight/2 {
		t.Errorf("徽章 %.1f 高，折行了（播放条才 %d 高）", badge.H, playbarHeight)
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
