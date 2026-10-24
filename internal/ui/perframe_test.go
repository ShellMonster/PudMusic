// 这一票（issue #314）钉住的三处「每帧重建」：
//
//   - **主题**（`theme.go`）：原先每一帧重建一份 `*ui.Theme`，并且每帧重新解析
//     用户设置里的颜色串。
//   - **满屏画布的键**（`fullplayer.go`）：原先每一帧 `fmt.Sprintf` 一次。
//   - **波形的本地存储键**（`widgets/hover/hover.go`）：在那个包里，那一条的
//     测试在 `hover_test.go` 里（它要的是「画出来的东西」，和这里是两件事）。
//
// # 为什么判据是「分配次数」而不是「时间」
//
// AGENTS §2：时间类的判据在这台机器上会被负载淹没，而**分配次数不会**——
// 它是「这一帧建了多少个对象」的计数。所以这里量的全是分配。
//
// # 为什么是「零分配」而不是「少一点」
//
// 这三处原先的写法都是**每帧必做**的（不是偶尔）：主题每帧重建、键每帧拼、
// 本地存储的键每帧拼。于是「少一点」说不清是修好了还是碰巧；而「稳态下零
// 分配」是一个**死数**——要么是零，要么不是。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFrameThemeIsNotRebuiltEveryFrame 钉住「主题不是每帧重建一份」。
//
// ## 为什么这件事值得钉
//
// 主题是**每帧开头装**的（铁律一），而「装」的那一份原先是**现拼**的：
// 每帧 `ui.LightTheme()` 建一份新的 `*ui.Theme`，并且每帧 `parseHexColor`
// 重新解析用户设置里的颜色串。
//
// 主题只在**设置变了 / 系统深浅变了 / 桌面偏好变了**的时候才变——那三样
// 都在缓存的键里，于是「键一样就复用」就等于「变了立刻生效」。
//
// ## 判据是**分配次数**，而且是零
//
// `AllocsPerRun` 量的是「这一次调用建了几个对象」。稳态下应当是 **0**：
// 键的比较是几个字符串和几个数的比较，不建任何东西。
func TestTheFrameThemeIsNotRebuiltEveryFrame(t *testing.T) {
	s := &Shell{}
	settings := app.DefaultSettings("")
	prefs := mygoui.Preferences{}

	// 第一帧：建一份出来。
	first := s.theme.of(settings, false, prefs)
	if first == nil {
		t.Fatal("主题没建出来")
	}
	if first.Accent != mygoui.Hex(app.DefaultPrimaryColor) {
		t.Errorf("主题的主色是 %v，want %v（设置里的主色没铺上去）",
			first.Accent, mygoui.Hex(app.DefaultPrimaryColor))
	}

	// 稳态：每帧零分配。
	allocs := testing.AllocsPerRun(200, func() {
		_ = s.theme.of(settings, false, prefs)
	})
	if allocs > 0 {
		t.Errorf("主题每帧重建了一次：稳态下每帧 %.1f 次分配——\n"+
			"主题只在设置变了、系统深浅变了、桌面偏好变了的时候才该重算"+
			"（那三样都在缓存的键里）", allocs)
	}

	// 而且拿到的必须是**同一份**，不是重建了一份新的（内容一样、指针不同）。
	if again := s.theme.of(settings, false, prefs); again != first {
		t.Error("稳态下拿到的不是同一份主题——它被重建了（内容一样，但指针不同，" +
			"于是每一帧都在堆上留一份）")
	}
}

// TestTheThemeFollowsTheSettingsTheMomentTheyChange 钉住「改了设置，下一帧就是新的」。
//
// ## 为什么这条比上一条更要紧
//
// 缓存最危险的失败模式是**该作废的时候没作废**：界面停在旧的主题上，而
// 「不报错、不崩溃、不打日志」——用户改了主色，界面没变。
//
// 所以这里走**真的外壳**：改设置（`UpdateSettings`）→ 画一帧 → 读那一帧
// 真正用的主题。中间没有一帧的延迟。
func TestTheThemeFollowsTheSettingsTheMomentTheyChange(t *testing.T) {
	var got themeProbe
	b := newTestShell(t, withSettings(app.DefaultSettings("")),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.applyFrameTheme(c)
				got = readTheme(c)
			}
		}))

	b.tt.Frame()
	if got.accent != mygoui.Hex(app.DefaultPrimaryColor) {
		t.Fatalf("起点就不对：主色是 %v，want %v", got.accent, mygoui.Hex(app.DefaultPrimaryColor))
	}

	// 用户在设置里挑了一个别的主色。
	if _, err := b.a.UpdateSettings(func(s *app.Settings) {
		s.PrimaryColor = "#ff0000"
	}); err != nil {
		t.Fatal(err)
	}
	b.tt.Frame()
	if got.accent != mygoui.Hex("#ff0000") {
		t.Errorf("改了主色之后下一帧还是 %v，want %v——\n"+
			"缓存没被设置变更作废（界面上会停在旧的主色，而那不报错）",
			got.accent, mygoui.Hex("#ff0000"))
	}

	// 档位也改：浅色 → 深色。
	if _, err := b.a.UpdateSettings(func(s *app.Settings) {
		s.ThemeMode = app.ThemeModeDark
	}); err != nil {
		t.Fatal(err)
	}
	b.tt.Frame()
	if !got.dark {
		t.Error("改了主题档位之后下一帧还是浅色——缓存没被设置变更作废")
	}

	// 底色也改：用户挑了底色，那一份也得跟着重算（分隔线是叠在底色上的）。
	if _, err := b.a.UpdateSettings(func(s *app.Settings) {
		s.BackgroundColor = "#101014"
	}); err != nil {
		t.Fatal(err)
	}
	b.tt.Frame()
	if got.bg != mygoui.Hex("#101014") {
		t.Errorf("改了底色之后下一帧的底色还是 %v，want %v", got.bg, mygoui.Hex("#101014"))
	}
}

// TestTheThemeIsNotRebuiltWhenTheViewRunsTwiceInAFrame 钉住「一帧跑两遍 view 时不重算」。
//
// ## 为什么这一条是缓存的**要害**
//
// MyGo 一帧要跑两遍 view（第一遍摆、第二遍留），而 `applyFrameTheme` 每遍都调。
// 第二遍读到的 `c.Theme()` 是**我们自己装上去的那一份**（第一遍装的），不是
// 框架那份——于是「系统深浅」在两遍里可能不一样：
//
//   - 档位是「跟随系统」、系统是深色：第一遍读到系统的深色，第二遍读到我们
//     自己的深色 → 一样。
//   - 档位是**浅色**、系统是深色：第一遍读到系统的深色，第二遍读到我们自己
//     装的浅色 → **不一样**。
//
// 而主题在第二种情形下是**同一份**（档位压过系统）。所以缓存的键要按
// 「实际要的那一档深浅」（`effectiveDark`）做，而不是按「系统深浅」——
// 按后者的话，这种情形下缓存每一帧都要重算，白做了。
func TestTheThemeIsNotRebuiltWhenTheViewRunsTwiceInAFrame(t *testing.T) {
	settings := app.DefaultSettings("")
	settings.ThemeMode = app.ThemeModeLight

	var first, second *mygoui.Theme
	b := newTestShell(t, withSettings(settings), systemIsDark(true),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.applyFrameTheme(c)
				first = c.Theme()
				// 第二遍：这一遍读到的 `c.Theme()` 已经是我们自己装的那一份。
				s.applyFrameTheme(c)
				second = c.Theme()
			}
		}))
	b.tt.Frame()

	if first == nil || second == nil {
		t.Fatal("主题没装上")
	}
	if first != second {
		t.Error("一帧里的第二遍 view 把主题重算了一遍——\n" +
			"键要按「实际要的那一档深浅」做（`effectiveDark`），" +
			"而不是按「系统深浅」：档位压过系统时，两遍读到的系统深浅不一样，" +
			"而主题是同一份")
	}
	if first.Dark {
		t.Error("档位是浅色，主题却是深色的")
	}
}

// TestTheFullPlayerBackdropKeyIsNotRebuiltEveryFrame 钉住「满屏画布的键不是每帧拼出来的」。
//
// ## 为什么这件事值得钉
//
// 满屏开着的时候，那个键是**每一帧**都要算的——而它原先是一句
// `fmt.Sprintf("%s|%p|%dx%d", …)`，每帧一次分配 ＋ 一次格式化。
//
// 判据是零分配：四个判据（地址、位图、宽、高）分开比，不建任何东西。
func TestTheFullPlayerBackdropKeyIsNotRebuiltEveryFrame(t *testing.T) {
	var f fullPlayer
	url := "http://example.test/cover.png"
	var bm *mygoui.Bitmap

	allocs := testing.AllocsPerRun(200, func() {
		_ = f.backdropChanged(url, bm, 300, 200)
	})
	if allocs > 0 {
		t.Errorf("满屏画布的键每帧拼一次字符串：每帧 %.1f 次分配——\n"+
			"四个判据分开比是零分配的（拼出来的那一版每帧要付一次格式化）", allocs)
	}
}

// TestTheBackdropIsRedrawnExactlyWhenItShouldBe 钉住「分开比之后，判据还是对的」。
//
// 换了封面、换了位图、换了尺寸 → 要重画；一样 → 不重画。
//
// ⚠️ **这一条是上一条的另一半**：零分配是手段，「该重画的时候重画」才是目的。
// 而这一条是**满屏那一张画布**的命门——不重画的话，背景停在上���张封面上
// （换了歌也不换），而画面上看着只是「背景没跟着换」。
func TestTheBackdropIsRedrawnExactlyWhenItShouldBe(t *testing.T) {
	var f fullPlayer
	url := "http://example.test/cover.png"
	// 两个**不同**的位图：同一个地址配不同的图，是「图换了」那一种。
	bm := &mygoui.Bitmap{}
	other := &mygoui.Bitmap{}

	// 还没画过：一切都是「变了」。
	if !f.backdropChanged(url, bm, 300, 200) {
		t.Fatal("还没画过那张画布，却说不用画")
	}

	f.bgURL, f.bgBitmap, f.bgW, f.bgH = url, bm, 300, 200
	if f.backdropChanged(url, bm, 300, 200) {
		t.Error("什么都没变，却说要重画——那会让毛玻璃每帧重渲一遍" +
			"（软件渲染上那是 80~100 毫秒一帧）")
	}

	// 换了封面。
	if !f.backdropChanged("http://example.test/other.png", bm, 300, 200) {
		t.Error("换了封面，却说不用重画")
	}
	// 换了位图（同一个地址，图换了）。
	if !f.backdropChanged(url, other, 300, 200) {
		t.Error("换了位图，却说不用重画")
	}
	// 换了尺寸（窗口改了大小）。
	if !f.backdropChanged(url, bm, 301, 200) {
		t.Error("改了画布的尺寸，却说不用重画")
	}
	if !f.backdropChanged(url, bm, 300, 201) {
		t.Error("改了画布的高度，却说不用重画")
	}
}
