package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 空状态这一组盯的是两件事：
//
//  1. SPEC 点名的四个场景（没有收藏夹、没有搜索结果、没有下载记录、没有歌词）
//     各自的文案是什么——文案抄 Biu 上游，抄错一个字就是这一页和 Biu 对不上。
//  2. 空状态**真的画出来了**，不是一片白、也不是一句转圈。只查元素框查不到
//     「画出来看不见」，所以有的测试读到像素。

// TestSpecEmptyStates 是这一张的验收点：SPEC 要求的空状态场景逐条对上。
//
// 每条都断言标题和「怎么才能不空」两句都在画面上。两句都断言是有意的：只有
// 标题，用户看到「暂无内容」之后还是不知道该干什么，那一句就白说了。
func TestSpecEmptyStates(t *testing.T) {
	cases := []struct {
		what  string
		route string
		// want 是上游那一句，出处写在 routes 表里那一条的注释上。
		want string
	}{
		{"没有收藏夹", "/collection/12345", "暂无内容"},
		{"没有搜索结果", "/search", "暂无内容"},
		{"没有下载记录", "/download-list", "暂无内容"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			s, tt := newShell(t)
			s.navigate(c.route)
			tt.Frame()

			rt := lookup(c.route)
			if rt.Empty.Title != c.want {
				t.Errorf("%s 的标题是 %q，want %q（抄错上游了）", c.what, rt.Empty.Title, c.want)
			}
			if !tt.HasText(c.want) {
				t.Errorf("%s 的 %q 没画出来，文字有 %q", c.what, c.want, tt.Texts())
			}
			if !tt.HasText(rt.Empty.Hint) {
				t.Errorf("%s 没说「怎么才能不空」，文字有 %q", c.what, tt.Texts())
			}
		})
	}
}

// TestLyricsEmptySaysSo 验第四个场景：没有歌词。
//
// 这一条现在**在应用里真的画得出来**：它挂在 `/empty` 那张空状态落地页上
// （routes 表里那一条）。歌词面板本身是阶段 6 的东西（PLAN.md 的 S-08），
// 在那之前，这一页就是它能落脚的地方——而不是一个只有测试引用的常量。
func TestLyricsEmptySaysSo(t *testing.T) {
	if lyricsEmpty.Title != "暂无歌词" {
		t.Fatalf("歌词空状态的标题是 %q，want 暂无歌词（上游 components/lyrics/index.tsx:375）", lyricsEmpty.Title)
	}
	// 「怎么才能不空」也要有：和其余 13 条一个规矩（上游那一句是全部，第二句
	// 是我们补的，见 empty.go）。
	if lyricsEmpty.Hint == "" {
		t.Error("歌词空状态没说「怎么才能不空」")
	}

	s, tt := newShell(t)
	s.navigate("/empty")
	tt.Frame()

	if !tt.HasText("暂无歌词") {
		t.Errorf("歌词空状态没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText(lyricsEmpty.Hint) {
		t.Errorf("歌词空状态的「怎么才能不空」没画出来，文字有 %q", tt.Texts())
	}
}

// TestEmptyStateIsPaintedNotBlank 验「不是空白」这句话本身。
//
// 只查元素框发现不了这一类：元素在、位置对、文字也在，可屏幕上什么也没有。
// 这里读像素——空状态那块和窗口底色必须不是一个颜色。
func TestEmptyStateIsPaintedNotBlank(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/download-list")
	tt.Frame()

	r, ok := tt.Find(lookup("/download-list").Empty.Title)
	if !ok {
		t.Fatalf("下载记录的空状态没画出来，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	// 窗口左上角是窗口底色，空状态那块是次要文字色。
	window := img.RGBAAt(4, 4)
	text := img.RGBAAt(int(r.X)+2, int(r.Y+r.H/2))
	if text == window {
		t.Errorf("空状态的字和窗口底色同色（%v），画出来看不见", text)
	}
}

// TestPageViewAppliesTheThemeItself 验 🔴 铁律二在**页面 view 上**也成立。
//
// 这条是这一张新加的页面 view 会不会用系统色画出来的判据：故意让**系统外观和
// 设置要的相反**（系统深色 / 设置浅色，或者反过来），而这里只画那一件空状态
// ——外层没有 viewMain 替它装主题。画面上跟系统那份一样，就说明页面 view 忘了
// 在开头装主题：它会用 MyGo 每帧重置好的系统色画出来，跟旁边的界面对不上。
func TestPageViewAppliesTheThemeItself(t *testing.T) {
	for _, c := range []struct {
		mode     app.ThemeMode
		sysDark  bool
		wantDark bool
	}{
		{app.ThemeModeLight, true, false},
		{app.ThemeModeDark, false, true},
	} {
		s := shellOn(t, c.mode)
		tt := mygoui.NewTester(func(c *mygoui.Context) {
			s.viewEmpty(c, "page", emptyState{Title: "这一页还没有内容", Hint: "空状态"})
		}, 600, 400)
		tt.SetDark(c.sysDark)
		tt.Frame()

		// 期望的底色是「设置要的那一档」，系统那份另算一份——两者正好相反。
		want := BuildTheme(s.app.SettingsSnapshot(), c.sysDark).Background
		got := tt.Image().RGBAAt(300, 200)
		w := mygoui.Color{R: got.R, G: got.G, B: got.B, A: got.A}
		if w != want {
			t.Errorf("设置 %v + 系统深色 %v：画面上却是 %v，want %v（页面 view 没装主题）",
				c.mode, c.sysDark, w, want)
		}
		if got := BuildTheme(s.app.SettingsSnapshot(), c.sysDark).Dark; got != c.wantDark {
			t.Fatalf("测试本身有问题：拼出来的是 %v 深色，want %v", got, c.wantDark)
		}
	}
}

// TestApplyThemeRepeatedlyStillFollowsTheSystem 钉住 theme.go 顶上写的那条
// 性质：**一帧里调几次结果都一样**。
//
// 页面 view 在自己开头也要调一次（SetTheme 只影响它之后创建的控件），所以这
// 一条天天在跑。以前有人在这里加过一个 `s.themed` 守卫，理由是「第二次读到的
// 是自己刚设的那份，会把跟随系统搞反」——那个失败模式不存在，守卫删掉之后这条
// 照样全绿。现在没有守卫，靠的就是下面这个性质本身。
func TestApplyThemeRepeatedlyStillFollowsTheSystem(t *testing.T) {
	s := shellOn(t, app.ThemeModeSystem)

	var after [3]bool
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		for i := range after {
			s.applyFrameTheme(c)
			after[i] = c.Theme().Dark
		}
	}, 300, 200)

	for _, sysDark := range []bool{true, false, true} {
		tt.SetDark(sysDark)
		tt.Frame()
		for i, got := range after {
			if got != sysDark {
				t.Errorf("系统深色 %v 时，第 %d 次装完读到的是 %v", sysDark, i+1, got)
			}
		}
	}
}

// shellOn 造一个真挂上设置的外壳（走「启动时读盘」那条路），返回它。
//
// newShell 造的那一个没挂设置：App 拿不到设置时给的是零值 Settings，
// ThemeMode 是空串，`BuildTheme` 把它当「跟随系统」。要验某**一档**主题的
// 接线，就得先把那份设置真的写上盘再挂上。
func shellOn(t *testing.T, mode app.ThemeMode) *Shell {
	t.Helper()

	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *app.Settings) { s.ThemeMode = mode }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	s := New(a)
	t.Cleanup(s.images.Close)
	return s
}

// TestEmptyStateCopySaysWhyAndHow 验每一条文案都把两件事说清了。
//
// 「这里为什么空」是上游那一句，「怎么才能不空」是我们加的第二句。两句都要在
// 画面上——这正是这一张要的「说得清的交代」。
func TestEmptyStateCopySaysWhyAndHow(t *testing.T) {
	for _, rt := range append(append([]Route{}, routes...), notFound) {
		if rt == notFound {
			// 兜底那一页有自己的 view，说的是「哪条路径不存在」和「几秒后回首页」。
			continue
		}
		if strings.TrimSpace(rt.Empty.Title) == "" {
			t.Errorf("%s 没说「这里为什么空」", rt.Match)
		}
		if strings.TrimSpace(rt.Empty.Hint) == "" {
			t.Errorf("%s 没说「怎么才能不空」", rt.Match)
		}
		if rt.Empty.Title == rt.Empty.Hint {
			t.Errorf("%s 两句说的是同一件事：%q", rt.Match, rt.Empty.Title)
		}
	}
}

// TestEmptyStatesAreDistinct 验 13 条路由的文案不是同一句。
//
// 全用共享默认 `暂无内容` 在上游是常态，可在这儿 13 个页面并排摆着，全一样就
// 分不出谁是谁了。第二句（怎么才能不空）必须每页不同。
func TestEmptyStatesAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, rt := range routes {
		if prev, dup := seen[rt.Empty.Hint]; dup {
			t.Errorf("%s 和 %s 的「怎么才能不空」是同一句：%q", rt.Match, prev, rt.Empty.Hint)
		}
		seen[rt.Empty.Hint] = rt.Match
	}
}
