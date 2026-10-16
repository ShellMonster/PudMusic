package ui

import (
	"image"
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

// TestSpecEmptyStates 是这一张的验收点：SPEC 点名的四个场景逐条对上。
//
// 四条各渲染一遍，断言标题和「怎么才能不空」两句都在画面上。两条都断言是有意
// 的：只有标题，用户看到「暂无内容」之后还是不知道该干什么，那一句就白说了。
//
// 「有宿主的」和「还没有宿主的」都走同一段：宿主在的用宿主那个外壳 navigate 过
// 去验，宿主还没到的（搜索结果、歌词——分别是阶段 5 和阶段 6 的功能）直接把同一
// 件空状态交给渲染器画。前者验的是「用户点得进去」，后者验的是「画出来就是这句
// 话」，两条合起来才是「这个场景被覆盖了」。
func TestSpecEmptyStates(t *testing.T) {
	cases := []struct {
		what string
		e    emptyState
		// host 是宿主路由；空串表示这一件还没有宿主（功能在后面的阶段）。
		host string
	}{
		{"没有收藏夹", noFavorites, "/collection/12345"},
		{"没有搜索结果", noSearchResults, ""},
		{"没有下载记录", noDownloads, "/download-list"},
		{"没有歌词", noLyrics, ""},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if c.e.Title == "" {
				t.Fatalf("%s 没有文案", c.what)
			}
			if c.e.Hint == "" {
				t.Fatalf("%s 没说「怎么才能不空」", c.what)
			}

			var texts []string
			var at mygoui.Rect
			var ok bool
			var img *image.RGBA
			if c.host != "" {
				s, tt := newShell(t)
				if got := lookup(c.host).Empty; got != c.e {
					t.Fatalf("%s 这一页挂的不是 %s 那一条（%q / %q）", c.host, c.what, got.Title, got.Hint)
				}
				s.navigate(c.host)
				tt.Frame()
				texts, ok = tt.Texts(), tt.HasText(c.e.Title)
				at, _ = tt.Find(c.e.Title)
				img = tt.Image()
			} else {
				// 还没有宿主：把同一件空状态交给渲染器，画面上真画出来。
				s, _ := newShell(t)
				tt := mygoui.NewTester(func(ctx *mygoui.Context) {
					s.viewEmpty(ctx, "spec:"+c.what, c.e)
				}, 900, 600)
				tt.Frame()
				texts, ok = tt.Texts(), tt.HasText(c.e.Title)
				at, _ = tt.Find(c.e.Title)
				img = tt.Image()
			}

			if !ok {
				t.Fatalf("%s 的 %q 没画出来，文字有 %q", c.what, c.e.Title, texts)
			}
			if !strings.Contains(strings.Join(texts, ""), c.e.Hint) {
				t.Errorf("%s 的「怎么才能不空」%q 没画出来，文字有 %q", c.what, c.e.Hint, texts)
			}
			// 画出来还得看得见：元素在、位置对、文字也在，可屏幕上什么也没有，
			// 只查元素框的测试发现不了这一类。
			if img.RGBAAt(int(at.X)+2, int(at.Y+at.H/2)) == img.RGBAAt(4, 4) {
				t.Errorf("%s 的字和窗口底色同色，画出来看不见", c.what)
			}
		})
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

// actionCues 是「怎么才能不空」这一句里必须出现的动作词。
//
// 为什么要有这张表：原来那条测试只查 Hint 非空、且不等于 Title，于是
// 「这个 UP 主还没有发过动态」「还没有歌在放」这种**把标题又说了一遍**的句子
// 照样过——用户看完还是不知道能干什么。「音乐榜和分区推荐还没有接进来」也是：
// 那是写给开发者看的实现进度，用户拿它办不了事。
//
// 判据只能到「这句话里有个动作词」这一层：文案没法用单元测试证明有用。但这一层
// 已经能把上面三句全揪出来（它们一个动作词都没有），比「非空」结实得多。
var actionCues = []string{
	"点", "打开", "输入", "搜", "登录", "选", "换", "切", "拖", "清空", "添加", "收藏", "去",
}

// TestEmptyStateCopySaysWhyAndHow 验每一条文案都把两件事说清了。
//
// 「这里为什么空」是上游那一句，「怎么才能不空」是我们加的第二句，而且第二句
// 必须给用户一个**能做的下一步**（见 actionCues）。
func TestEmptyStateCopySaysWhyAndHow(t *testing.T) {
	s, _ := newShell(t)
	check := func(what string, e emptyState) {
		t.Helper()
		if strings.TrimSpace(e.Title) == "" {
			t.Errorf("%s 没说「这里为什么空」", what)
		}
		if strings.TrimSpace(e.Hint) == "" {
			t.Errorf("%s 没说「怎么才能不空」", what)
			return
		}
		if e.Title == e.Hint {
			t.Errorf("%s 两句说的是同一件事：%q", what, e.Title)
		}
		if !hasActionCue(e.Hint) {
			t.Errorf("%s 的「怎么才能不空」%q 里没有动作词：它多半只是把「为什么空」"+
				"又说了一遍，用户看完还是不知道能干什么", what, e.Hint)
		}
	}

	for _, rt := range append(append([]Route{}, routes...), notFound) {
		if rt == notFound {
			// 兜底那一页有自己的 view，说的是「哪条路径不存在」和「几秒后回首页」。
			continue
		}
		if s.builtPage(rt) != nil {
			// 已经有内容的页（设置页）画的是它自己，用不到空状态。
			continue
		}
		check(rt.Match, rt.Empty)
	}
	// 四个点名场景也都得过这一关——其中 noSearchResults 和 noLyrics 还没有宿主，
	// 不在这里跑就没有任何测试看着它们。
	check("没有收藏夹", noFavorites)
	check("没有搜索结果", noSearchResults)
	check("没有下载记录", noDownloads)
	check("没有歌词", noLyrics)
}

// hasActionCue 报句子里有没有一个动作词。
func hasActionCue(s string) bool {
	for _, cue := range actionCues {
		if strings.Contains(s, cue) {
			return true
		}
	}
	return false
}

// TestEmptyStatesAreDistinct 验 13 条路由的文案不是同一句。
//
// 全用共享默认 `暂无内容` 在上游是常态，可在这儿 13 个页面并排摆着，全一样就
// 分不出谁是谁了。第二句（怎么才能不空）必须每页不同。
func TestEmptyStatesAreDistinct(t *testing.T) {
	s, _ := newShell(t)
	seen := map[string]string{}
	for _, rt := range routes {
		if s.builtPage(rt) != nil {
			continue
		}
		if prev, dup := seen[rt.Empty.Hint]; dup {
			t.Errorf("%s 和 %s 的「怎么才能不空」是同一句：%q", rt.Match, prev, rt.Empty.Hint)
		}
		seen[rt.Empty.Hint] = rt.Match
	}
}
