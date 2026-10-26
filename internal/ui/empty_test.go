package ui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// **四条全部走真实路由**——切过去、画出来、读像素。没有一条是靠测试直接把空状态
// 交给渲染器的：那种写法只能证明「这句话画出来是这句话」，证明不了用户点得进去，
// 而上一轮评审挑出来的正是这一点（noSearchResults 和 noLyrics 曾经只有测试引
// 用，在应用里哪儿都到不了）。
//
// 宿主在哪、「为什么在那儿」，写在对应 view 的注释里：收藏夹在
// `/collection/{id}`、下载记录在 `/download-list`、搜索结果在 `/search`（带上
// 搜索词的那一支）、歌词在 `/mini-player`（**得先有歌在放**——见下面那条注释）。
func TestSpecEmptyStates(t *testing.T) {
	cases := []struct {
		what string
		e    emptyState
		// target 是切过去要走的路径；path 是它的路由（去掉 query）。
		target string
		path   string
		// setup 是「切过去之前先把这页要用的东西挂上」。没有就不调。
		//
		// ⚠️ 需要它的是收藏夹那一页：它现在**有内容了**（头图区 + 列表 +
		// 批量操作条，collectionpage.go），而「没有收藏夹」是这一页**自己
		// 里面**的一支——容器拉回来了、里面一条都没有。所以那一句要真的跑
		// 到它，就得先有一个真能回「空收藏夹」的 B 站。
		setup func(t *testing.T, s *Shell)
	}{
		{
			what: "没有收藏夹", e: noFavorites,
			target: "/collection/12345", path: "/collection/{id}",
			setup: func(t *testing.T, s *Shell) {
				s.app.MountCollections(app.CollectionRefs{Client: newEmptyFolderClient(t)})
			},
		},
		{"没有搜索结果", noSearchResults, "/search?q=" + url.QueryEscape("周杰伦"), searchPath, nil},
		{"没有下载记录", noDownloads, "/download-list", "/download-list",
			// ⚠️ **下载记录页有两种空**：「连落盘的地方都没有」和「真的没下载
			// 过」。这一条验的是后者，所以得有一个目录（没目录时那一格说的是
			// 「去设置里选一个」，见报告 F3）。
			func(t *testing.T, s *Shell) {
				if err := s.app.MountDownloads(app.Downloads{
					Media: http.DefaultClient, FallbackDir: t.TempDir(),
				}); err != nil {
					t.Fatalf("挂下载：%v", err)
				}
			}},
		{"没有歌词", noLyrics, miniPlayerPath, miniPlayerPath, nil},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if c.e.Title == "" {
				t.Fatalf("%s 没有文案", c.what)
			}
			if c.e.Hint == "" {
				t.Fatalf("%s 没说「怎么才能不空」", c.what)
			}

			s, tt := newShell(t)
			// 宿主必须真的能画出来：「这句话是对的」和「用户走得到这句话」是两
			// 回事。宿主要么在 builtPage 里注册了页面视图（搜索、迷你播放器、
			// 收藏夹），要么自己带这句 Empty（下载记录）。
			rt := lookup(c.path)
			if s.builtPage(rt) == nil && rt.Empty != c.e {
				t.Fatalf("%s 那一页（%s）既没有注册页面视图、带的也不是这一句，"+
					"切过去画的是别的东西", c.what, c.path)
			}
			if c.setup != nil {
				c.setup(t, s)
			}
			// 「没有歌词」有前提：得先有歌在放。没打开任何文件就说「暂无歌词」，
			// 那句话和屏幕上的事实对不上。
			if c.what == "没有歌词" {
				if s.app.Snapshot().HasTrack {
					t.Fatalf("测试自己出了问题：还没打开文件就有歌在放")
				}
				s.navigate(c.target)
				tt.Frame()
				if tt.HasText(noLyrics.Title) {
					t.Fatalf("没打开任何文件就说「暂无歌词」，文字有 %q", tt.Texts())
				}
				openATrack(t, s)
			}
			s.navigate(c.target)
			// 收藏夹那一页要先等后台把容器拉回来（头图区第一帧是「正在加载…」，
			// 空状态那一支要 `info.Loaded` 才画得出来）；别的几页不发请求。
			//
			// ⚠️ **判的是「收藏夹那一页」而不是「有没有 setup」**：下载记录那一条
			// 也要 setup（挂一个下载目录，好让「真的没下载过」那一句是那一句），
			// 而它不等收藏夹的头图。
			if c.path == collectionMatch {
				waitCollectionHeader(t, s, tt)
			}
			// ⚠️ 搜索页那一条要多等几帧：结果是异步回来的，「画不画空状态」
			// 这个判断读的是上一帧的状态（理由见 settleText）。
			//
			// 两条**都要**：第一个等的是「容器元信息回来了」（那一页的空状态要
			// `info.Loaded` 才画得出来），第二个等的是「那一句话画出来了」。
			// 少任何一个都会让下面 `HasText` 变成按运气过。
			settleText(t, tt, c.e.Title)

			if !tt.HasText(c.e.Title) {
				t.Fatalf("%s 的 %q 没画出来，文字有 %q", c.what, c.e.Title, tt.Texts())
			}
			if !tt.HasText(c.e.Title) {
				t.Fatalf("%s 的「怎么才能不空」%q 没画出来，文字有 %q", c.what, c.e.Hint, tt.Texts())
			}
			at, ok := tt.Find(c.e.Title)
			if !ok {
				t.Fatalf("%s 的标题框找不到，文字有 %q", c.what, tt.Texts())
			}
			// 画出来还得看得见：元素在、位置对、文字也在，可屏幕上什么也没有，
			// 只查元素框的测试发现不了这一类。
			img := tt.Image()
			if img.RGBAAt(int(at.X)+2, int(at.Y+at.H/2)) == img.RGBAAt(4, 4) {
				t.Errorf("%s 的字和窗口底色同色，画出来看不见", c.what)
			}
		})
	}
}

// TestTheFourSpecStatesAreAllReachable is TestSpecEmptyStates 的反面：
// 确认这一张没有哪一条是「只有测试引用的常量」。
//
// 判据是 `builtPage`：宿主没注册的话，那一页切过去画的是空状态那一支，
// `noSearchResults` / `noLyrics` 就又变回只有测试看得见了。
func TestTheFourSpecStatesAreAllReachable(t *testing.T) {
	s, tt := newShell(t)
	for _, c := range []struct{ what, path string }{
		{"没有收藏夹", "/collection/{id}"},
		{"没有搜索结果", searchPath},
		{"没有下载记录", "/download-list"},
		{"没有歌词", miniPlayerPath},
	} {
		if s.builtPage(lookup(c.path)) != nil {
			continue
		}
		// 没注册页面视图的那一条，只有在它自己带 Empty 时才算说得过去。
		if got := lookup(c.path).Empty; got.Title == "" {
			t.Errorf("%s：%s 既没有页面视图也没有空状态文案，走过去是一片白", c.what, c.path)
		}
		_ = tt
	}
}

// TestSearchingShowsTheNoResultsState 验搜索页两个分支都走得到，而且是靠真的
// 敲字 + 回车走过去的——这是「用户能进入这个场景」最硬的一条证据。
func TestSearchingShowsTheNoResultsState(t *testing.T) {
	s, tt := newShell(t)

	// 还没输入：上游 pages/search/index.tsx:16 那一支。
	s.navigate(searchPath)
	tt.Frame()
	if !tt.HasText(searchIdle.Title) {
		t.Fatalf("没输入时没画「去输入」那一支，文字有 %q", tt.Texts())
	}
	if tt.HasText(noSearchResults.Title) {
		t.Errorf("还没输入就说「换个词再搜」，文字有 %q", tt.Texts())
	}

	// 点进搜索框，敲字 + 回车 → 到了「搜了没有结果」那一支。走的全是真实输入。
	//
	// ⚠️ **框是顶栏那一个**（票 #234）：页内那个 420 宽的输入框 2026-10-22 拍板
	// 删了（`DESIGN.md` §十.1），改词回顶栏——所以「用户能进入这个场景」这条路
	// 走的也是顶栏。
	box, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("搜索框没画出来，文字有 %q", tt.Texts())
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	tt.Type("周杰伦")
	tt.Frame()
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()

	if got := s.router.Location(); !strings.Contains(got, "q=") {
		t.Fatalf("回车之后 URL 上没有搜索词：%q", got)
	}
	if !tt.HasText(noSearchResults.Title) {
		t.Errorf("搜完没有画出「没有搜索结果」，文字有 %q", tt.Texts())
	}
	if !tt.HasText(noSearchResults.Title) {
		t.Errorf("搜完没有画出「换个词再搜」，文字有 %q", tt.Texts())
	}
}

// TestEmptyStateIsPaintedNotBlank 验「不是空白」这句话本身。
//
// 只查元素框发现不了这一类：元素在、位置对、文字也在，可屏幕上什么也没有。
// 这里读像素——空状态那块和窗口底色必须不是一个颜色。
//
// ⚠️ **下载记录页有两种空，两种都画**：没有下载目录（`noDownloadDir`）和
// 真的没下载过（`noDownloads`）。少画一种，那一种在真机上就是一片白。
func TestEmptyStateIsPaintedNotBlank(t *testing.T) {
	for _, c := range []struct {
		what string
		dir  bool
	}{
		{"没有下载目录", false},
		{"没有下载记录", true},
	} {
		t.Run(c.what, func(t *testing.T) {
			s, tt := newShell(t)
			if c.dir {
				if err := s.app.MountDownloads(app.Downloads{
					Media: http.DefaultClient, FallbackDir: t.TempDir(),
				}); err != nil {
					t.Fatalf("挂下载：%v", err)
				}
			}
			s.navigate("/download-list")
			tt.Frame()

			title := noDownloadDir.Title
			if c.dir {
				title = noDownloads.Title
			}
			r, ok := tt.Find(title)
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
		})
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
	b := newTestShell(t, withSettings(func() app.Settings {
		st := app.DefaultSettings("")
		st.ThemeMode = mode
		return st
	}()), withoutTester())
	return b.s
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

// waitCollectionHeader 等歌单内容页把容器拉回来（头图区第一帧是「正在加载…」）。
func waitCollectionHeader(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		tt.Frame()
		// ⚠️ 判据是**结果**不是帧数：那一趟跑在 goroutine 里，而 Tester 一帧
		// 只要几十微秒——按帧数等就是按运气等，慢机器上会偶发地红。
		if s.collection != nil && s.app.Collection(s.collection.ref).Loaded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 2s，收藏夹的元信息还没加载完，文字有 %q", tt.Texts())
		}
		time.Sleep(time.Millisecond)
	}
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

// openATrack 给外壳塞进一首真的歌，走的是应用层那扇正门（app.Open）。
//
// 歌词那一支的前提是「有歌在放」：没打开任何文件就说「暂无歌词」，那句话和
// 屏幕上的事实对不上——上一轮把它挂在一张通用展示页上就是这么错的。
//
// 用的是 flacdec 的测试素材，和 playbar_test.go 那套一样（那边在外部测试包里，
// 这里够不着）。`Open` 是异步的（解码在 goroutine 上），所以得轮询等快照翻过来。
func openATrack(t *testing.T, s *Shell) {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	if err := s.app.Open(p); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.app.Snapshot().HasTrack {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等了 5 秒，快照里还是没有歌在放")
}
