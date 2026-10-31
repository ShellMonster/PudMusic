package ui

// 「播放条左下角那一行写的是**认出来的歌名**」那一族的 E2E。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestThePlayBarShowsTheRecognizedSongName
//
// # 这一族为什么必须存在
//
// B 站的标题是**视频的标题**，不是歌名：`【4K修复】周杰伦 - 晴天 官方MV
// 1080P 高音质`——而本项目是**纯音乐 App**，界面上要写的是**歌名**。
//
// ⚠️ **旧实现只处理括号里面的东西**，而「官方MV 1080P 高音质」挂在括号**外
// 面** ⇒ 播放条左下角那一行写的是「周杰伦 - 晴天 官方MV 1080P 高音质」——
// 用户看到的是一串视频痕迹，而不是歌名。
//
// ⇒ 这一族从**一次真的渲染**出发，断「播放条上那一行写的是认出来的那一个
// 歌名」。
//
// ⚠️ **判据用文字内容，不用像素比色**：那一行旁边那道 1 像素的边线是**半透明
// 混色**画上去的（画面上的值和主题的 `Border` 不相等），精确比色会扫不到。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// TestThePlayBarShowsTheRecognizedSongName 断「播放条上写的是**认出来的歌
// 名**」。
//
// 🔴 **这是「显示那一档」**：播放条左下角那一行是用户盯着看的地方——它写着
// 「【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质」的话，用户看到的是一串视频
// 痕迹，而不是歌名。
//
// ⚠️ **判据是「画面上有没有那一句」**（`HasText`）：字**在不在**画面上，而不
// 是「那一行的颜色是不是某个数」——后者要写死一个平台相关的值，而这一族要
// 断的是**内容**。
func TestThePlayBarShowsTheRecognizedSongName(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  string
		notGo []string
	}{
		{
			name:  "方括号 + 破折号 + 括号外的宣传词",
			raw:   "【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质",
			want:  "晴天",
			notGo: []string{"官方MV", "1080P", "4K修复", "周杰伦 - 晴天"},
		},
		{
			name:  "整条标题就是那对括号",
			raw:   "【晴天】",
			want:  "晴天",
			notGo: []string{"【晴天】"},
		},
		{
			name:  "书名号",
			raw:   "歌手《晴天》",
			want:  "晴天",
			notGo: []string{"《晴天》"},
		},
		{
			name:  "破折号",
			raw:   "周杰伦 - 晴天",
			want:  "晴天",
			notGo: []string{"周杰伦 - 晴天"},
		},
		{
			name:  "紧凑的连字符",
			raw:   "周杰伦-晴天",
			want:  "晴天",
			notGo: []string{"周杰伦-晴天"},
		},
		{
			name:  "冒号 + 说明",
			raw:   "晴天：电影主题曲",
			want:  "晴天",
			notGo: []string{"电影主题曲"},
		},
		{
			name:  "竖线",
			raw:   "起风了 | 吴青峰 | 官方MV",
			want:  "起风了",
			notGo: []string{"吴青峰", "官方MV"},
		},
		{
			name:  "三段 + 歌手",
			raw:   "孤勇者 - 陈奕迅 - 官方完整版",
			want:  "孤勇者",
			notGo: []string{"陈奕迅", "官方完整版"},
		},
		{
			name:  "括号里的版本说明",
			raw:   "起风了(钢琴版)",
			want:  "起风了",
			notGo: []string{"钢琴版"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tt, _ := metaBarTester(t, app.State{
				HasTrack: true, Title: c.raw,
				Artist: "某个 UP 主", ArtistMid: 12345,
			}, 1200)

			if !tt.HasText(c.want) {
				t.Errorf("播放条上没有认出来的歌名 %q，文字有 %q",
					c.want, tt.Texts())
			}
			for _, bad := range c.notGo {
				if tt.HasText(bad) {
					t.Errorf("播放条上还有认不出来的 %q，文字有 %q", bad, tt.Texts())
				}
			}
		})
	}
}

// TestThePlayBarFallsBackToTheRawTitle 断「**认不出就退回原标题**」。
//
// 🔴 **宁可显示脏标题，也不显示空白**：认不出的时候播放条上要是空的，用户看
// 到的是「这首歌没有名字」，而真实原因是「不知道这首歌叫什么」——那是**另一
// 件事**，而那一件事界面上一句话都说不清。
//
// ⚠️ **这一档跟「匹配」那一档的语义不一样**（`songname`：认不出给**空表**
// ——空表不筛，否则会把这一首的评论全筛掉）。两个语义在代码和注释里都写清楚
// 了（见 `internal/ui/songmeta` 的文件头）。
func TestThePlayBarFallsBackToTheRawTitle(t *testing.T) {
	const raw = "【官方MV】"
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: raw,
		Artist: "某个 UP 主", ArtistMid: 12345,
	}, 1200)
	if !tt.HasText(raw) {
		t.Errorf("认不出的时候播放条上写的是 %q，want 原标题 %q"+
			"（宁可显示脏标题，也不显示空白）", tt.Texts(), raw)
	}
}

// TestTheListRowShowsTheRecognizedSongName 断「**列表里那一行**也是认出来的
// 歌名」。
//
// ⚠️ **同一个歌名在两处显示成两个名字**是那种「界面上看不出坏了」的错——用户
// 点的和放出来的不一样。
//
// 🔴 **判据是「那一行**画出来**的字」**（`Find` 找**完全相同**的那一句），而
// 不是「画面上有没有那几个字」**——**无障碍名字**里用的是**原标题**（那一行
// 右端的「操作」按钮带着它），而那是**故意**的：屏幕阅读器要念的是这一首的
// **身份**，不是显示用的歌名。
//
// ⚠️ **不用像素比色**（理由见文件头）。
func TestTheListRowShowsTheRecognizedSongName(t *testing.T) {
	const raw = "【4K修复】周杰伦 - 晴天 官方MV 1080P 高音质"
	tt := listRowTester(t, raw)

	// ⚠️ **找的是**画出来的那一行字**：它必须是认出来的那个歌名。
	box, ok := tt.Find("晴天")
	if !ok {
		t.Fatalf("列表里那一行没有认出来的歌名「晴天」，文字有 %q", tt.Texts())
	}
	if box.W <= 0 || box.H <= 0 {
		t.Errorf("「晴天」那个盒子是 %.1fx%.1f——它没画出来", box.W, box.H)
	}
	// ⚠️ **那一行**画出来**的**不该是原标题。
	//
	// 🔴 **判据是**几何**（宽度），不是「画面上有没有那几个字」**：那一行的
	// **无障碍名字**里用的是**原标题**（右端那个「操作」按钮带着它），而那是
	// **故意**的——屏幕阅读器要念的是这一首的**身份**，不是显示用的歌名。
	//
	// ⚠️ **原标题那一行是 27 个字**，而「晴天」只有 2 个 ⇒ 宽度差一个数量级
	// ⇒ 量宽度就能分辨画的是哪一个（而 `Find` 分不出：两者都是「文字」。
	//
	// ⚠️ **不用像素比色**（理由见文件头）。
	const titleWidthMax = 80
	if box.W > titleWidthMax {
		t.Errorf("「晴天」那个盒子是 %.1f 宽——那不是认出来的那个歌名"+
			"（原标题那一行会是这么宽）", box.W)
	}
}

// listRowTester 造一个**只画首页**的外壳，首页那一页摆一行给定标题的歌。
//
// ⚠️ **它摆的是 `list.ShowOf` 那一档**（`widgets/list/titleline.go`）——而那一
// 档和播放条用的是**同一份**清洗判据（`widgets/title`）。
func listRowTester(t *testing.T, raw string) *mygoui.Tester {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(1200, 900), withView(func(s *Shell) func(*mygoui.Context) {
		s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: {{
			ID: "1", Title: raw, Owner: "某个 UP 主",
		}}}})
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewHome(c, nil)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt
}
