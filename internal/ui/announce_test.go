package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/notice"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组盯的是「状态变化被念出来」（无障碍，票 #321）。
//
// # 念出去的和看得见的是两件事
//
// 右下角弹出来的那张卡片**读屏软件用户看不见**——他们没有余光去扫右下角。
// 而「下载下完了」「这一首放起来了」「搜出结果了」这些事，在画面上**一个字都不占**
// （下载完成只是记录页上那一列多了一枚绿勾）。
//
// 所以这些事得被**念出来**：框架的 `ui.Context.Announce` 就是干这个的。
//
// ⚠️ **念和看是两套机制，不许混**：
//
//   - `.Label(...)` 给的是「**这一枚是什么**」（那一枚按钮叫什么、那一行是谁）；
//   - 播报给的是「**刚刚发生了什么**」。
//
// `internal/ui` 里有 102 处 `.Label`。把它们逐个改成播报的后果是：读屏软件把一屏
// 控件名字念一遍，而**一件正在发生的事一句也没有**——那正是这一票要消掉的毛病。
// `TestTheLabelsAreNotTurnedIntoAnnouncements` 钉住这件事。

// takeSaid 取走念出去的那些话（`Tester.Announcements` 取走之后就忘了）。
func takeSaid(tt *mygoui.Tester) []string { return tt.Announcements() }

// countSaid 数一数某一句话被念了几遍。
func countSaid(said []string, want string) int {
	n := 0
	for _, s := range said {
		if s == want {
			n++
		}
	}
	return n
}

// hasSaid 数一数念出去的那些话里有没有哪一句**包含** want。
func hasSaid(said []string, want string) bool {
	for _, s := range said {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// TestAToastIsAlsoReadOut 钉住「提示也念一遍」。
//
// 🔴 **提示是这一层唯一的「说一句话」通道**（140 来处调用点），而框架自带的
// 那条「toast 自动念一遍」的链路**在我们这儿是断的**——框架的 `AddToast` 挂在
// 未导出的 `*ui.context` 上，我们用的是自己那套（见 `toast.go` 的文件头）。
//
// 不念的话，那 140 来处「下载没成」「收藏不了」「快捷键没存上」在读屏软件那里
// **全是无声的**。
func TestAToastIsAlsoReadOut(t *testing.T) {
	sh := newTestShell(t)
	// 把开窗那一下念的（路由念的页标题）倒掉。
	takeSaid(sh.tt)

	sh.s.addToast("下载完成", notice.Success)
	sh.tt.Frame()

	said := takeSaid(sh.tt)
	if n := countSaid(said, "下载完成"); n != 1 {
		t.Fatalf("「下载完成」被念了 %d 遍，want 1 遍（念出去的是 %q）", n, said)
	}
}

// TestTheSameThingIsNotReadOutTwice 钉住「一句只念一遍」。
//
// ⚠️ **一帧会建好几遍视图**：框架一帧最多建三遍（`mygo/ui/runtime.go:524` 那个
// `for pass := 0; pass < 3`），而每一遍都会走一遍 `Shell.View`。取走必须是
// **有破坏性**的，不然一句会被念三遍——读屏软件把同一句话念三遍，是最吵的一种
// 吵。
//
// 而「一帧建一遍」还不够：点击吃了输入之后框架会**再画一遍**，于是同一帧里
// 走两遍 `Shell.View`。这一条把那种情况也钉住。
func TestTheSameThingIsNotReadOutTwice(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	sh.s.addToast("下载完成", notice.Success)

	// 连着画好几帧：一句只该念一遍。
	var said []string
	for i := 0; i < 3; i++ {
		sh.tt.Frame()
		said = append(said, takeSaid(sh.tt)...)
	}
	if n := countSaid(said, "下载完成"); n != 1 {
		t.Fatalf("「下载完成」被念了 %d 遍，want 1 遍（念出去的是 %q）", n, said)
	}
}

// TestAThingIsNotReadOutTwiceWhenTheFrameIsBuiltTwice 是上面那一条的**点击版**。
//
// 按一下侧栏那枚折叠键：这一帧框架吃了这个点击，于是**同一帧里建了两遍视图**。
// 取走不破坏的话，那一句会被念两遍。
func TestAThingIsNotReadOutTwiceWhenTheFrameIsBuiltTwice(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	sh.s.addToast("下载完成", notice.Success)

	if err := sh.tt.Click("折叠侧栏"); err != nil {
		t.Skipf("这一版画不出侧栏那枚折叠键（跳）：%v", err)
	}

	said := takeSaid(sh.tt)
	if n := countSaid(said, "下载完成"); n != 1 {
		t.Fatalf("「下载完成」被念了 %d 遍，want 1 遍（念出去的是 %q）", n, said)
	}
}

// TestASearchThatFoundSomethingIsReadOut 钉住「搜索完成」。
//
// 🔴 **搜索结果在画面上是一屏列表**：读屏软件用户得一行一行走才知道有什么。
// 而「搜完了、一共 N 个」这件事**不是画面上的任何一行**（页标题写的是
// 「搜索【X】的结果」，那说的是搜的是啥，不是搜到了几个）。
func TestASearchThatFoundSomethingIsReadOut(t *testing.T) {
	// ⚠️ **每页 24 条、一共 48 条**：断言的那个数必须是**总数**（48），而**不是**
	// 屏幕上那 24 行。拿「屏幕上那几行」冒充总数，用户就知道了个假数
	// （B 站一共 1000 个结果，念成「找到 24 个结果」）。
	//
	// ⚠️ **假拉页器要写 `t.total`**（见 `searchStub.fetch`）：不写的话那一句走
	// 「不知道总数」的回落分支，钉的就不是真总数那一半了。
	st := newSearchStub(24, 48)
	s, tt := newSearchPage(t, "周杰伦", st)
	takeSaid(tt)

	// 等结果真的落地（后台那一趟是并发的，判据走「跑到条件成立为止」）。
	waitFrames(t, tt, "搜索结果出来", func() bool {
		pg := pageOf(s, searchTabSong)
		return pg.Loaded > 0 && !pg.Loading
	})

	said := takeSaid(tt)
	if !hasSaid(said, "找到 48 个结果") {
		t.Fatalf("念出去的是 %q，want 一句「找到 48 个结果」（命中的总数，不是屏幕上那 24 行）", said)
	}
}

// TestTheSearchTotalIsTheOneThatIsAnnounced 钉住「找到 N 个结果」里的那个 N。
//
// 🔴 **这一条走真的那一支**（真的 B 站客户端 → 真的 `fetchSearchTab` → 真的
// `numResults`）：那一句里的 N 是 **B 站给的命中总数**，而**假拉页器把整个
// 拉页器替掉了**——它自己写一份 `t.total`，于是真实现里那一句
// （`fetchSearchTab` 的 `t.total = total`）**删掉了也照样绿**。
//
// ⚠️ **所以这里不用假拉页器**：假服务器回「一共 137 个」而只有**一行**结果，
// 于是「总数」和「屏幕上那几行」差得开——念出来的那个数必须是 137，而不是 1。
func TestTheSearchTotalIsTheOneThatIsAnnounced(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSearchTotal(137)
	s, tt := newShell(t)
	searchOnRealClient(s, f.client())

	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	waitFrames(t, tt, "搜索结果出来", func() bool {
		pg := pageOf(s, searchTabSong)
		return pg.Loaded > 0 && !pg.Loading
	})

	said := takeSaid(tt)
	if !hasSaid(said, "找到 137 个结果") {
		t.Fatalf("念出去的是 %q，want 一句「找到 137 个结果」（B 站给的命中总数，"+
			"不是屏幕上那几行）", said)
	}
}

// TestASearchThatFoundSomethingIsReadOutOnlyOnce 钉住「一场搜索只念一遍」。
//
// 翻页会一直加行，而每加一行都念一遍就成了报数。
func TestASearchThatFoundSomethingIsReadOutOnlyOnce(t *testing.T) {
	st := newSearchStub(24, 48)
	s, tt := newSearchPage(t, "周杰伦", st)
	takeSaid(tt)

	waitFrames(t, tt, "搜索结果出来", func() bool {
		pg := pageOf(s, searchTabSong)
		return pg.Loaded > 0 && !pg.Loading
	})
	// 再画几帧：结果已经在手上了，不该再念第二遍。
	for i := 0; i < 3; i++ {
		tt.Frame()
	}

	said := takeSaid(tt)
	if n := countSaid(said, "找到 48 个结果"); n != 1 {
		t.Fatalf("「找到 48 个结果」被念了 %d 遍，want 1 遍（念出去的是 %q）", n, said)
	}
}

// TestARecordedShortcutIsReadOut 钉住「录上了」。
//
// 🔴 **「录上了」这件事界面上一个字都没有**：那一格只是多了一串键，而读屏软件
// 用户按下去的时候听到的是「V」，不是「这一格现在录的是 ⌘⇧V」——于是他不知道
// 「录上了」还是「没录上」。
func TestARecordedShortcutIsReadOut(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	sh.s.applyShortcut(shortcutSlot{id: "togglePlay"}, "CommandOrControl+Shift+V")
	sh.tt.Frame()

	said := takeSaid(sh.tt)
	if !hasSaid(said, "快捷键已改成") {
		t.Fatalf("念出去的是 %q，want 一句「快捷键已改成 …」", said)
	}
}

// TestAPlayingFailureReachesTheScreenReaderThroughTheRealWiring 钉住**接线**。
//
// 🔴 **这一条钉的是那一行**：`s.announceRemove = a.AddAnnounce(s.announce)`
// （`internal/ui/shell.go`）——「应用层发生了一件事 → 界面层念出来」，中间就靠
// 这一行。删掉它，**编译照样过、本票的其它用例照样绿**：应用层那组用例自己装了
// 一个收集器当 sink，界面层那组直接调 `s.announce(...)`——两半各自测了，中间
// 这一行一次都没被测过。
//
// 所以这一条**从应用层那个口子进去**（`App.Open` 打开一个不存在的文件，它自己
// 念「播放失败：…」），一路走到读屏软件那里，中间一行都不绕。
func TestAPlayingFailureReachesTheScreenReaderThroughTheRealWiring(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	// 应用层那件事：本地文件打不开（`App.Open` 自己念，见 internal/app/app.go）。
	_ = sh.a.Open("/biu-go/这个文件不存在.m4a")

	sh.tt.Frame()
	said := takeSaid(sh.tt)
	if !hasSaid(said, "播放失败：") {
		t.Fatalf("念出去的是 %q，want 一句「播放失败：…」", said)
	}
}

// TestAnAnnouncementIsNotKeptWhileTheWindowIsInvisible 钉住「看不见的那一扇不攒」。
//
// 🔴 **看不见的那一扇窗攒着的话没人念**（它看不见的时候连帧都不画），等用户
// 回来时**一次性全念出来**：攒了多久就念多久之前的旧事。
//
// ⚠️ **用的是生产上同一份状态机**（`noteWindow` → `presence`，见 presence.go），
// 不是测试专用的旁路。
func TestAnAnnouncementIsNotKeptWhileTheWindowIsInvisible(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	// 用户切到别的应用去了：那一扇窗看不见了。
	sh.s.noteWindow(func(p *windowPresence) { p.focused = false })

	sh.s.addToast("下载完成", notice.Success)
	sh.tt.Frame()
	if said := takeSaid(sh.tt); len(said) != 0 {
		t.Fatalf("看不见的时候也念了：%q", said)
	}

	// 用户回来了：那一句**不该**这时候才念出来。
	sh.s.noteWindow(func(p *windowPresence) { p.focused = true })
	sh.tt.Frame()
	if said := takeSaid(sh.tt); len(said) != 0 {
		t.Fatalf("回来的时候把旧话念了一遍：%q", said)
	}
}

// TestAnOldAnnouncementIsNotReadOut 钉住「过期的不要」。
//
// ⚠️ **过期这一道兜的是迷你播放器**：它是「不聚焦也算看得见」的
// （`presence.freeWhileBlurred`），于是用户在别的应用里待着的时候它照样收得下
// ——那些话**回来时不该全部念一遍**。
func TestAnOldAnnouncementIsNotReadOut(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	// 攒一句，然后把它「攒了很久」。
	sh.s.announcements.push("很久以前的事", time.Now().Add(-2*announceTTL))
	sh.tt.Frame()
	if said := takeSaid(sh.tt); len(said) != 0 {
		t.Fatalf("过期的也念了：%q", said)
	}
}

// TestAtMostThreeAnnouncementsAreKept 钉住「攒着的那几摊有上限」。
//
// 一次念一屏是最吵的一种吵；而**新来的把旧的挤掉**——留住的正好是「刚刚发生
// 的那几件」。
func TestAtMostThreeAnnouncementsAreKept(t *testing.T) {
	sh := newTestShell(t)
	takeSaid(sh.tt)

	for i := 0; i < 5; i++ {
		sh.s.announce("第 " + strconv.Itoa(i) + " 句")
	}
	sh.tt.Frame()

	said := takeSaid(sh.tt)
	if len(said) != announceLimit {
		t.Fatalf("攒了 %d 句（%q），want %d 句", len(said), said, announceLimit)
	}
	// 留的是**最新**的那几句，不是最旧的那几句。
	if want := "第 4 句"; said[len(said)-1] != want {
		t.Fatalf("留下的是 %q，want %q（最新那几句）", said[len(said)-1], want)
	}
}

// TestTheLabelsAreNotTurnedIntoAnnouncements 钉住这一票的另一半。
//
// 播报和 `.Label(...)` 是两件事：
//
//   - `.Label` 给读屏软件的是「**这一枚是什么**」（那一枚按钮叫什么、那一行是谁）；
//   - 播报给的是「**刚刚发生了什么**」。
//
// 把那 95 处 `.Label` 逐个改成播报的后果是：读屏软件把一屏控件名字念一遍，
// 而**一件正在发生的事一句也没有**——那正是这一票要消掉的毛病。
//
// 这里钉两件事：
//
//  1. `.Label` 的调用点**一个都没少**（少一个就是有人把它删了，或者改成播报了）；
//  2. **没有哪一个 `.Label` 在播报的那条路上**（`announce` / `Announce` /
//     `flushAnnouncements` 那些函数里不许出现 `.Label`）。
func TestTheLabelsAreNotTurnedIntoAnnouncements(t *testing.T) {
	calls, inAnnounce := scanLabels(t)
	if len(calls) < 95 {
		t.Fatalf("给读屏软件的无障碍名字只剩 %d 处（至少要 95）："+
			"它们是「这一枚是什么」，不是「刚刚发生了什么」——"+
			"把其中任何一处改成播报，读屏软件用户听到的就是一屏控件名字",
			len(calls))
	}
	if len(inAnnounce) > 0 {
		t.Fatalf("播报的那条路上出现了 `.Label(...)`：%v", inAnnounce)
	}
}

// scanLabels 数出 `internal/ui`（不含测试）里 **给读屏软件的无障碍名字**
// （`.Label(...)`）的调用点，外加「哪些是在播报那条路上的」。
//
// ⚠️ **只数「带参数」的**：框架的无障碍名字是 `Element.Label(text string)`——
// 一定带一个字符串；而界面上另有几个**领域方法**也叫 `Label()`（`PlayMode` 的
// 档位名、`CollectionType` 的名字…），它们跟读屏软件一点关系都没有。
//
// ⚠️ **把它们算进来会让这条测试误报**：哪天有人把一个领域方法 `Label()` 删掉
// 或者改名，这个数就掉下来了，而无障碍的名字一个都没少——那正是「重构误报」。
// 那是本条测试第一版的错（它数的是全部 102 处——把领域方法也算进去了），
// 评审抓出来的。
func scanLabels(t *testing.T) (calls []string, inAnnounce []string) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		// 先找出这个文件里哪些函数是「播报那条路上的」。
		announcing := map[string]bool{}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if isAnnounceName(fn.Name.Name) {
				announcing[fn.Name.Name] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if ok && fn.Body != nil && announcing[fn.Name.Name] {
				ast.Inspect(fn.Body, func(m ast.Node) bool {
					call, ok := m.(*ast.CallExpr)
					if ok && isA11yLabel(call) {
						inAnnounce = append(inAnnounce, fset.Position(call.Pos()).String())
					}
					return true
				})
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if ok && isA11yLabel(call) {
				calls = append(calls, fset.Position(call.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫 `internal/ui` 失败：%v", err)
	}
	return calls, inAnnounce
}

// isA11yLabel 认得出「这一句是给读屏软件的无障碍名字」。
//
// 判据是**带参数**：框架的 `Element.Label(text string)` 一定带一个字符串，
// 而界面上那几个领域方法 `Label()` 一个参数都不带（`PlayMode` 的档位名、
// `CollectionType` 的名字…）——它们跟读屏软件一点关系都没有。
func isA11yLabel(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Label" {
		return false
	}
	return len(call.Args) > 0
}

// isAnnounceName 认得出「这个函数是播报那条路上的」。
func isAnnounceName(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "announce")
}
