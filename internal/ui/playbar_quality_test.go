package ui

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"

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
	// 只画播放条那一块，不走整窗：`withView` 就是为「绕过整窗、单独画一块」留的。
	b := newTestShell(t, at(w, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)

		}
	}))
	tt := b.tt
	tt.Frame()
	return tt
}

// TestQualityBadgeShowsForAnOnlineTrack 盯徽章画出来了。
//
// 🔴 **画面上的是**短版**（`bili.ChipLabel`），不是全称**（票 #219）：那一列
// （歌名 + UP 名）在多 P 那一档只有 76 宽，而全称（52~60 宽）和歌名挤在一行
// 放不下——框架按基宽分摊，于是**徽章自己被摊扁**（实测「杜比全景声」只剩
// 13.8：一个字，点击区只剩 14 像素）。短版就是**上游那一枚 chip 的形状**
// （「无损」/「杜比」，`playbar/left/index.tsx:50-56`），全称在悬停提示里。
func TestQualityBadgeShowsForAnOnlineTrack(t *testing.T) {
	for _, badge := range []string{"无损 FLAC", "320K", "128K", "64K", "杜比全景声"} {
		chip := bili.ChipLabel(badge)
		tt := qualityBarTester(t, app.State{HasTrack: true, Title: "在线的一首", Quality: badge})
		if !tt.HasText(chip) {
			t.Errorf("徽章 %q（短版 %q）没画出来，画面上的字：%q", badge, chip, tt.Texts())
		}
	}
}

// TestQualityBadgeFollowsTheRefresh 盯换流之后徽章会变。
//
// 徽章是**每帧从 State 现读**的，不是开歌那一刻定下来的。把它缓存住的话，
// B 站把 320K 降成 128K 时用户看到的还是「320K」——无声地降级会让人以为
// 自己还在听原来那一档。
func TestQualityBadgeFollowsTheRefresh(t *testing.T) {
	st := app.State{HasTrack: true, Title: "在线的一首", Quality: "320K"}
	b := newTestShell(t, at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st) // ← 闭包读的是外面那个变量

		}
	}))
	tt := b.tt

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
	// ⚠️ **画面上的是短版**（票 #219，理由见 `TestQualityBadgeShowsForAnOnlineTrack`）。
	badge, ok := tt.Find(bili.ChipLabel("无损 FLAC"))
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

// ─────────────────────────────────────────────────────────────────────────────
// 徽章就是换音质的入口
// ─────────────────────────────────────────────────────────────────────────────

// TestTheBadgeMenuSwitchesQuality 盯「点徽章能换音质」。
//
// 这一条是「音质切换后立即生效，无需重启」的**入口**：用户在播放条上点一下
// 徽章、选一档，偏好就换了。没有这个入口的话，换音质只能去设置页——而那里
// 改的是「下一首」，改完当前这首纹丝不动，用户会以为没生效。
//
// 这条断的是「点了菜单那一项，App 的偏好真的变了」。换流、跳回原位置那些
// 是 `internal/app` 那边的用例（那边能造出「B 站只给了 320K」这种界面造不
// 出来的 fixture）。
func TestTheBadgeMenuSwitchesQuality(t *testing.T) {
	a := streamingApp(t)

	// 偏好每帧从 App 现读，和界面别的地方一样。徽章那一行固定住（这一条测的
	// 是菜单，不是换流），变的只有偏好那一项。
	st := app.State{
		HasTrack:    true,
		Title:       "在线的一首",
		Quality:     "320K",
		QualityPref: app.AudioQualityAuto,
	}
	b := newTestShell(t, onApp(a), at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			st.QualityPref = a.Snapshot().QualityPref
			s.viewPlayBar(c, st)

		}
	}))
	tt := b.tt
	tt.Frame()

	if err := tt.Click("320K"); err != nil {
		t.Fatalf("点不动徽章：%v（画面上的字：%q）", err, tt.Texts())
	}
	want := []string{"自动", "无损", "高音质", "标准", "省流量"}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Fatalf("菜单里的项 = %q，want %q", got, want)
	}
	if err := tt.ChooseMenuItem("无损"); err != nil {
		t.Fatalf("选不了「无损」：%v", err)
	}
	if got := a.AudioQuality(); got != bili.AudioLossless {
		t.Errorf("选完之后偏好 = %v，want AudioLossless", got)
	}
	if got := a.Snapshot().QualityPref; got != app.AudioQualityLossless {
		t.Errorf("状态里的偏好 = %q，want %q", got, app.AudioQualityLossless)
	}

	// 菜单不是一次性的：换完一档之后还能接着换。菜单里勾中的那一项由这一帧
	// 的状态说了算，所以它得是现读的——缓存住的话勾永远停在换之前那一档。
	if err := tt.Click("320K"); err != nil {
		t.Fatalf("第二次点不动徽章：%v", err)
	}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Fatalf("第二次的菜单 = %q，want %q", got, want)
	}
	if err := tt.ChooseMenuItem("省流量"); err != nil {
		t.Fatalf("选不了「省流量」：%v", err)
	}
	if got := a.AudioQuality(); got != bili.AudioLow {
		t.Errorf("第二次选完偏好 = %v，want AudioLow", got)
	}
}

// TestNoQualityMenuWithoutABadge 盯本地文件不弹这个菜单。
//
// 徽章说的是「B 站给了哪一档」，本地文件没有这一档，也就没有档可换。点歌名
// 不该弹出音质菜单——那会让用户以为点哪儿都能换音质。
func TestNoQualityMenuWithoutABadge(t *testing.T) {
	tt := qualityBarTester(t, app.State{HasTrack: true, Title: "本地的一首"})
	if err := tt.Click("本地的一首"); err != nil {
		t.Fatal(err)
	}
	if items := tt.Menu(); items != nil {
		t.Errorf("本地文件也弹出了音质菜单：%q", items)
	}
}

// streamingApp 建一个挂了在线播放的 App。
//
// ⚠️ 菜单里的选择最终要落到 `App.SetAudioQuality` 上，而它一看「没挂取流」
// 就直接走人。所以这一条要的是**真挂了**，不是摆一个空壳：菜单项点了没反应
// 和菜单项根本没接上是两回事。
//
// 这里挂的客户端不会发任何请求——`SetAudioQuality` 在没有在线流的时候只改
// 偏好，不取流。
func streamingApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	a.MountStreaming(app.Streaming{
		Client: bili.NewClient(bili.Options{}),
		Media:  http.DefaultClient,
	})
	return a
}
