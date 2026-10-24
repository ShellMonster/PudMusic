package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 这一组跑的是 MyGo 的 Tester：和真窗口一模一样的布局、点击和滚动，只是画到内存里。
// 所以「三个 Tab 切得动」「切回来还在原位」「新歌速递只在流行上」都能自动验，
// 不用人守在窗口前面拖。

// homeW / homeH 是无头窗口的大小。
//
// ⚠️ 宽度不能随便改小：新歌速递分列看的是**容器**的宽，窗口左边还有侧栏，窗口
// 一窄容器更窄（Biu 那几个断点是在容器上分的，见 homepage_newmusic.go 的文件头）。
const (
	homeW = 1200
	homeH = 800
)

// songName 是第 i 首（1 起）的歌名。
func songName(prefix string, i int) string { return fmt.Sprintf("%s %d", prefix, i) }

// homeSongsOf 造 n 行歌：歌名是「<prefix> 1」到「<prefix> n」，ID 带着 prefix，
// 所以三个 Tab 的歌不串、也不会撞。
func homeSongsOf(prefix string, n int) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		out[i] = list.Music{
			ID:    prefix + "-" + strconv.Itoa(i),
			Title: songName(prefix, i+1),
			Owner: prefix + " 的 UP 主",
			Item:  app.Item{Title: songName(prefix, i+1)},
		}
	}
	return out
}

// homeTestCache 造一个封面缓存。
//
// AddWake 不能省：图下完之后不唤醒，界面会永远停在占位（imgcache/view.go 顶上那条
// 约定）。这一组里这些歌都不带封面地址，走的是「没有地址画空框」那一支，但留着
// 的理由和真窗口一样，也免得哪天测试里加一个带地址的歌就踩上。
func homeTestCache(t *testing.T) *imgcache.Cache {
	t.Helper()
	c := imgcache.New(imgcache.Options{})
	c.AddWake(func() {})
	t.Cleanup(c.Close)
	return c
}

// homeFrame 连跑几帧。
//
// 一帧不够：切 Tab 之后列表要重新排版，而行高是量出来的，量完才知道落在哪儿
// （`mygo/ui/list.go` 的 ScrollTo 注释）。三帧是 widgets/list 那边用的同一个数。
func homeFrame(tt *ui.Tester) {
	for range 3 {
		tt.Frame()
	}
}

// settleHome 推着钟走一段，让**切 Tab 的那一段过渡**走完。
//
// ⚠️ **推钟是必须的**（AGENTS.md §2）：切 Tab 有过渡（票 #368），而 Tester 的
// 钟站着不动——不推钟的话画面停在过渡的**第一帧**上（内容还在淡入、还差着
// 那 8 DIP），而逐像素的断言量到的就是那一帧（一个「切过去就换内容」的硬切
// 实现也是那种画面，所以那样的断言断不出「有没有过渡」）。
//
// ⚠️ **推 250ms**：过渡本身是 200ms（`homeBodyDuration`），留 50ms 余量。
// 主 Tab 那个滑块的弹簧（`tabs.go` 的 `TabSlide`，实测 453ms 才收手）**不在
// 这条的范围里**——它在 Tab 那一排上，而这一组要量的是内容那一块。
func settleHome(tt *ui.Tester) {
	tt.Advance(homeBodyDuration + 50*time.Millisecond)
	tt.Frame()
}

// newHome 给首页塞上歌，返回外壳和画着它的 Tester。
//
// ⚠️ 走的是 `newShell`（**完整的窗口**：顶栏 + 侧栏 + 首页 + 播放条），不是
// 「只画首页」：侧栏上那一项「推荐音乐」和首页的 Tab 挨得很近，只画一半的话
// 「点侧栏回首页」那两条根本走不到。
func newHome(t *testing.T, songs homeSongs) (*Shell, *ui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	s.home.SetSongs(songs)
	homeFrame(tt)
	return s, tt
}

// fullSongs 是「三个 Tab 各四十首 + 新歌速递十二首」的一屏，够滚动也够分页。
func fullSongs() homeSongs {
	return homeSongs{
		tabs: [homeTabCount][]list.Music{
			homeSongsOf(homeTabs[0], 40),
			homeSongsOf(homeTabs[1], 40),
			homeSongsOf(homeTabs[2], 40),
		},
		new: homeSongsOf("新歌", 12),
	}
}

// shows 报 s 此刻在不在画面上。
//
// ⚠️ **不能用 `tt.HasText`**：它是**子串**匹配（`ui/headless.go` 的
// `strings.Contains`），而这几个 Tab 的名字互为子串——「本地音乐」里有「音乐」，
// 首页上「音乐」那一档的 Tab 还在不在就永远为真。`tt.Find` 是**全等**匹配。
func shows(tt *ui.Tester, s string) bool {
	r, ok := tt.Find(s)
	return ok && r.W > 0 && r.H > 0
}

// rowPos 是画面上画着的某一行：它是哪一首、上面离窗口顶多远。
type rowPos struct {
	title string
	y     float32
}

// listPos 是某一档此刻画着的那些行，从上往下排。
//
// 用它判「还在不在原来的位置」而不是判「顶上那一行是哪一首」：虚拟列表在上下各
// 多建两行给 Tab 和读屏用（`mygo/ui/list.go` 的 listOverscan），那些建在**视口
// 之外**的行盒子是空的（宽 0），但它们仍然有 y——按 y 找「最上面那一行」会挑到
// 视口上面那一行去（实测挑到的是真正顶上那首的前两首）。整屏的（歌，y）一起比，
// 就没这个歧义了。
//
// ⚠️ 盒子是空的要**跳过**而不是停在那儿：只看「找没找到」会把画面外的那几行也算
// 进来，断言就永远为真。
func listPos(tt *ui.Tester, prefix string) []rowPos {
	var out []rowPos
	for i := 1; i <= 200; i++ {
		r, ok := tt.Find(songName(prefix, i))
		if !ok || r.W == 0 || r.H == 0 {
			continue
		}
		out = append(out, rowPos{songName(prefix, i), r.Y})
	}
	slices.SortFunc(out, func(a, b rowPos) int {
		if a.y != b.y {
			return cmp.Compare(a.y, b.y)
		}
		return cmp.Compare(a.title, b.title)
	})
	return out
}

// titles 是给报错用的一行摘要：这一屏里画着的头几首和最后一首。
func titles(pos []rowPos) string {
	if len(pos) == 0 {
		return "（一首歌都没画出来）"
	}
	head := make([]string, 0, 4)
	for i, p := range pos {
		if i == 3 {
			head = append(head, "…")
			break
		}
		head = append(head, p.title)
	}
	return fmt.Sprintf("%v 一直到 %s", head, pos[len(pos)-1].title)
}

// clickTab 点那三个 Tab 里的一个。
//
// ⚠️ **推钟把过渡走完**（`settleHome`）：切 Tab 有过渡（票 #368），而 Tester
// 的钟站着不动——不推钟的话画面停在过渡的第一帧上。
func clickTab(t *testing.T, tt *ui.Tester, tab int) {
	t.Helper()
	if err := tt.Click(homeTabs[tab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[tab], err, tt.Texts())
	}
	homeFrame(tt)
	settleHome(tt)
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个 Tab 都能切
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeTabsAllSwitch 是这一票的头一条：三个 Tab 都能切，切过去画的是
// **那一档自己的歌**。
//
// 判据是画面上的字：切到「鬼畜」之后「音乐 1」必须从画面上消失、「鬼畜 1」必须
// 出现。只断言 `s.home.tab` 变了的话，一个换了档但忘了换内容的实现照样绿。
func TestTheThreeTabsAllSwitch(t *testing.T) {
	s, tt := newHome(t, fullSongs())

	for i, name := range homeTabs {
		clickTab(t, tt, i)
		if s.home.tab != i {
			t.Errorf("点了 %q，落在第 %d 档，want %d", name, s.home.tab, i)
		}
		if !shows(tt, songName(name, 1)) {
			t.Errorf("切到 %q，画面上没有它自己的第一首，文字有 %q", name, tt.Texts())
		}
		// 另外两档的歌一个都不该还在画面上。
		for j, other := range homeTabs {
			if j == i {
				continue
			}
			if shows(tt, songName(other, 1)) {
				t.Errorf("切到 %q，画面上还有 %q 的歌，文字有 %q", name, other, tt.Texts())
			}
		}
	}
}

// TestTheHomeRouteShowsItsOwnPage 验**接线**：从别的页面点回首页，画的是首页自己
// （那一排 Tab + 那一屏的歌），不是 router.go 兜底的那张空状态页。
//
// ⚠️ 这一条不能省：首页整个接在 `builtPage` 上，而接错了的话首页画的是
// `pageContent` 的兜底分支——那一支照样画得出一屏东西，上面几条却全在往
// `s.home` 的字段上读，读的是没被画过的那一份，全绿。
func TestTheHomeRouteShowsItsOwnPage(t *testing.T) {
	s, tt := newHome(t, fullSongs())

	if err := tt.Click("本地音乐"); err != nil {
		t.Fatal(err)
	}
	homeFrame(tt)
	if shows(tt, homeTabs[0]) {
		t.Fatalf("切到本地音乐之后首页的 Tab 还在画面上，文字有 %q", tt.Texts())
	}

	if err := tt.Click("推荐音乐"); err != nil {
		t.Fatal(err)
	}
	homeFrame(tt)
	if got := s.router.Path(); got != homePath {
		t.Fatalf("点回首页停在 %q，want %q", got, homePath)
	}
	for _, name := range homeTabs {
		if !shows(tt, name) {
			t.Errorf("首页没有画出 %q 那一档的 Tab，文字有 %q", name, tt.Texts())
		}
	}
	if !shows(tt, songName(homeTabs[0], 1)) {
		t.Errorf("首页没有画出它自己的歌，文字有 %q", tt.Texts())
	}
	if tt.HasText(lookup("/local-music").Empty.Title) {
		t.Errorf("首页画的是兜底那张空状态页，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 切 Tab 的滚动位置
// ─────────────────────────────────────────────────────────────────────────────

// TestSwitchingBackRestoresThatTabsScrollPosition 是这一票的第二条：**切回某个
// Tab 时它还在自己原来的位置**。
//
// 怎么算「还在原来的位置」：比**这一屏画着的是同一批歌、同样的上下位置**
// （`listPos`），不判「顶上那一行是第几首」也不判像素。像素是**布局的产物**
// （那一档的行高是定死的还是量出来的，看「数据显示样式」），而用户记住的是
// 「我刚才在看的那一屏」。
//
// 四步都要走：记下滚之前的那一屏 → 在「音乐」滚一段 → 切到「鬼畜」滚一段 → 切回
// 来比。**两条都断**（「音乐」和「鬼畜」各自的位置）：少断一条的话，「只有当前
// 这一档记位置、其余的都回顶」那种实现也绿。
func TestSwitchingBackRestoresThatTabsScrollPosition(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	scrollTo := func(dy float32) {
		// mygo 的 PointerScroll：**正的 DY 是往下滚**（platform/surface.go）。
		tt.Scroll(homeW/2, homeH/2, 0, dy)
		homeFrame(tt)
	}

	// 滚之前先记一份：没有它，「滚动之后这一屏 == 回到顶上那一屏」会全绿。
	atStart := listPos(tt, homeTabs[0])

	scrollTo(1200)
	music := listPos(tt, homeTabs[0])
	if len(music) == 0 {
		t.Fatalf("滚过之后「%s」那一档一首歌都没画出来", homeTabs[0])
	}
	if slices.Equal(music, atStart) {
		t.Fatalf("滚了 1200 像素之后画面上还是 %s：滚动没生效，后面的断言都白断",
			titles(music))
	}

	clickTab(t, tt, 1)
	scrollTo(2400)
	guichu := listPos(tt, homeTabs[1])
	if len(guichu) == 0 {
		t.Fatalf("滚过之后「%s」那一档一首歌都没画出来", homeTabs[1])
	}

	clickTab(t, tt, 0)
	if got := listPos(tt, homeTabs[0]); !slices.Equal(got, music) {
		t.Errorf("切回「%s」，画面上是 %s，want 切走之前的 %s",
			homeTabs[0], titles(got), titles(music))
	}

	clickTab(t, tt, 1)
	if got := listPos(tt, homeTabs[1]); !slices.Equal(got, guichu) {
		t.Errorf("切回「%s」，画面上是 %s，want 切走之前的 %s",
			homeTabs[1], titles(got), titles(guichu))
	}
}

// TestLeavingTheHomePageAndComingBackKeepsWhereYouWere 验**换路由**也不丢位置。
//
// 这是「切 Tab 恢复位置」那一条在另一条路上的同一个要求：用户不是在侧栏里换来
// 换去、就是在这一页内部切 Tab，两条都要在原位。首页的状态长在 Shell 上（不是
// view 的局部变量——即时模式每一帧都重画一遍，局部变量下一帧就没了）。
func TestLeavingTheHomePageAndComingBackKeepsWhereYouWere(t *testing.T) {
	s, tt := newHome(t, fullSongs())

	tt.Scroll(homeW/2, homeH/2, 0, 1200)
	homeFrame(tt)
	before := listPos(tt, homeTabs[0])
	if len(before) == 0 {
		t.Fatalf("滚过之后「%s」那一档一首歌都没画出来", homeTabs[0])
	}

	s.navigate("/later")
	homeFrame(tt)
	if !tt.HasText(lookup("/later").Empty.Title) {
		t.Fatalf("没切到稍后再看，文字有 %q", tt.Texts())
	}

	if err := tt.Click("推荐音乐"); err != nil {
		t.Fatal(err)
	}
	homeFrame(tt)
	if got := listPos(tt, homeTabs[0]); !slices.Equal(got, before) {
		t.Errorf("从别的页面切回首页，画面上是 %s，want 切走之前的 %s",
			titles(got), titles(before))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 新歌速递只在「流行」上
// ─────────────────────────────────────────────────────────────────────────────

// TestNewMusicShowsOnlyOnThePopTab 是这一票的第三条：**新歌速递只在「流行」Tab
// 顶部显示**。
//
// 逐个 Tab 走一遍：另外两个上面「新歌速递」一个都不许出现，「流行」上必须出现。
// 只钉「流行上有」的话，一个三个 Tab 全都显示的实现也绿——那正是这条要消掉的。
//
// 顺带钉住「顶部」那半句：新歌速递的 y 必须**小于**那一档第一首的 y（它画在选中
// 那一屏的列表之前，见 viewHome）。
//
// 上游出处：`music-recommend/index.tsx:295` 的
// `{activeTab === "pop" && <NewMusicTop onLayoutChange={handlePopLayoutChange} />}`。
func TestNewMusicShowsOnlyOnThePopTab(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	for i, name := range homeTabs {
		clickTab(t, tt, i)
		shown := shows(tt, "新歌速递")
		if want := i == homePopTab; shown != want {
			if shown {
				t.Errorf("「%s」上画了新歌速递，want 不画（上游只在 pop 上）", name)
			} else {
				t.Errorf("「%s」上没有新歌速递，文字有 %q", name, tt.Texts())
			}
			continue
		}
		// 「流行」上才有：接着钉「顶部」那半句——它在列表上面。
		if !shown {
			continue
		}
		rNew, okNew := tt.Find("新歌速递")
		rSong, okSong := tt.Find(songName(name, 1))
		if !okNew || !okSong {
			t.Fatalf("找不到新歌速递或那一档的第一首，文字有 %q", tt.Texts())
		}
		if rNew.Y >= rSong.Y {
			t.Errorf("新歌速递在 y=%v、%q 的第一首在 y=%v：它不在列表上面",
				rNew.Y, name, rSong.Y)
		}
	}

	// 再走一圈回到第一档：换 Tab 之后那一块跟着「流行」走，没有粘在别处。
	clickTab(t, tt, homePopTab)
	if !shows(tt, "新歌速递") {
		t.Errorf("切回「流行」之后新歌速递不见了，文字有 %q", tt.Texts())
	}
	clickTab(t, tt, 0)
	if shows(tt, "新歌速递") {
		t.Errorf("从「流行」切走之后新歌速递还挂在画面上，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 顶图跟着滚走（票 #369）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTopImagesScrollAwayWithTheList 钉住这一票的头一条：**滚动时顶图被滑出
// 屏幕**。
//
// 判据是画面：滚一段之后新歌速递那几张图不在画面上了。
//
// ⚠️ **为什么它此前是红的**：新歌速递那一块是列表的**兄弟**（在列表的滚动容器
// **外面**），所以滚动列表时它一动不动。上游那一页是
// `{activeTab === "pop" && <NewMusicTop … />}` 和下面的列表在**同一个滚动容器**
// 里（`music-recommend/index.tsx`），顶图跟着滚走。
//
// 怎么滚：滚轮要落在**列表**那一块上（`ui.Tester.Scroll` 给的是指针位置）。
func TestTheTopImagesScrollAwayWithTheList(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	clickTab(t, tt, homePopTab)

	// 滚之前：顶图在画面上。
	if !shows(tt, "新歌速递") {
		t.Fatalf("切到「流行」之后新歌速递不在画面上，文字有 %q", tt.Texts())
	}
	if !shows(tt, songName("新歌", 1)) {
		t.Fatalf("新歌速递那一块没有画出它的卡片，文字有 %q", tt.Texts())
	}

	// 滚一段。指针落在下面那一档的歌上（列表那一块）。
	tt.Scroll(homeW/2, homeH-150, 0, 1200)
	homeFrame(tt)

	// 顶图不见了。
	if shows(tt, "新歌速递") {
		t.Errorf("滚了 1200 像素之后新歌速递还在画面上——它没有跟着滚走，文字有 %q", tt.Texts())
	}
	if shows(tt, songName("新歌", 1)) {
		t.Errorf("滚了 1200 像素之后新歌速递的卡片还在画面上，文字有 %q", tt.Texts())
	}
	// 而下面那一档的歌还在（滚的是列表，不是把整页滚没了）。
	if len(listPos(tt, homeTabs[homePopTab])) == 0 {
		t.Errorf("滚了 1200 像素之后「%s」那一档一首歌都没画出来，文字有 %q",
			homeTabs[homePopTab], tt.Texts())
	}
}

// TestTheTabRowStaysPutWhileTheListScrolls 钉住这一票的第二条：**Tab 那一排留在
// 滚动容器外面**（滚列表时它不动）。
//
// ⚠️ **它和上一条是一对**：顶图要跟着滚走，Tab 那一排不能跟着走——上游也是
// （Tab 那一排在滚动容器外面，只有顶图和列表在里面）。
//
// 判据是**位置**：Tab 那一排的 y 在滚动前后要一模一样。判「还在不在」不够——
// 它一直在，只是位置变了。
func TestTheTabRowStaysPutWhileTheListScrolls(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	clickTab(t, tt, homePopTab)

	before, ok := tt.Find(homeTabs[homePopTab])
	if !ok || before.H == 0 {
		t.Fatalf("找不到「%s」那一档的 Tab，文字有 %q", homeTabs[homePopTab], tt.Texts())
	}

	tt.Scroll(homeW/2, homeH-150, 0, 1200)
	homeFrame(tt)

	after, ok := tt.Find(homeTabs[homePopTab])
	if !ok || after.H == 0 {
		t.Fatalf("滚完之后找不到「%s」那一档的 Tab，文字有 %q", homeTabs[homePopTab], tt.Texts())
	}
	if after.Y != before.Y {
		t.Errorf("滚列表的时候 Tab 那一排动了：y 从 %v 变成 %v——它应该留在滚动容器外面",
			before.Y, after.Y)
	}
}

// TestTheTopImagesScrollAwayInGridModeToo 验「数据显示样式 = 网格」那一档**也一样**。
//
// ⚠️ **这一档是另一个控件**：网格那一档不是 `list.List`，是 `list.Grid`（它自己
// 一份 `ui.ListState`、自己一套列数分档）。页内头部要**两档都装**
// （`SwitchView.SetLead`），漏了网格那一档的症状是「用户在设置里切成网格，新歌速递
// 那一块就没了」——而它是这一屏的内容，不是列表的装饰。
func TestTheTopImagesScrollAwayInGridModeToo(t *testing.T) {
	// 「数据显示样式」是**设置**里的一格，所以要先把那份设置挂上（走的是
	// 「先写盘、再挂上去」那条生产路，见 `withSettings`）。
	st := app.DefaultSettings("")
	st.DisplayMode = app.DisplayModeCard
	s, tt := newShell(t, withSettings(st))
	s.home.SetSongs(fullSongs())
	homeFrame(tt)
	clickTab(t, tt, homePopTab)

	if !shows(tt, "新歌速递") {
		t.Fatalf("网格那一档上没有新歌速递，文字有 %q", tt.Texts())
	}

	tt.Scroll(homeW/2, homeH-150, 0, 1200)
	homeFrame(tt)

	if shows(tt, "新歌速递") {
		t.Errorf("网格那一档滚了 1200 像素之后新歌速递还在画面上，文字有 %q", tt.Texts())
	}
	if len(listPos(tt, homeTabs[homePopTab])) == 0 {
		t.Errorf("网格那一档滚了 1200 像素之后一首歌都没画出来，文字有 %q", tt.Texts())
	}
}

// TestTheTopImagesStayWhenTheListIsEmpty 验「下面那一档没歌的时候，顶图还在」。
//
// ⚠️ **顶图是这一屏的内容，不是列表的装饰**：新歌速递那一块和下面那一档的推荐单
// 是两摊各拉各的（见 `homepage_feed.go` 的「四摊，各自独立」）。下面那一档没拉到
// 的时候，上游那一页照样摆着 `<NewMusicTop/>`（`index.tsx:295`）。
//
// ⚠️ 而这一档**没有列表可挂**（一根歌都没有 → 画的是页面那一档的空状态，不是
// 列表），所以顶图得自己站在上面——它平时是列表的第 0 行（页内头部）。
func TestTheTopImagesStayWhenTheListIsEmpty(t *testing.T) {
	songs := fullSongs()
	songs.tabs[homePopTab] = nil
	_, tt := newHome(t, songs)

	clickTab(t, tt, homePopTab)
	if !shows(tt, "新歌速递") {
		t.Errorf("下面那一档没歌的时候顶图不见了，文字有 %q", tt.Texts())
	}
	if !shows(tt, lookup(homePath).Empty.Title) {
		t.Errorf("下面那一档没歌的时候空状态不见了，文字有 %q", tt.Texts())
	}
}

// TestTheMusicAndGuichuTabsAreNotTouched 钉住「另外两档没动」这一条。
//
// 两条判据：另外两档上**没有**新歌速递（上游本来就没有），而它们**照样能滚**
// （这一票没把滚动容器挪到别处去）。
//
// ⚠️ 第二条是这一票真正可能踩坏的地方：顶图要滚走，实现就动了列表的滚动容器
// ——而另外两档的列表是**共用同一套**的（`list.SwitchView`）。动坏了的症状是
// 「另外两档滚不动了」。
func TestTheMusicAndGuichuTabsAreNotTouched(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	for _, tab := range []int{0, 1} {
		clickTab(t, tt, tab)
		if shows(tt, "新歌速递") {
			t.Errorf("「%s」那一档上画了新歌速递（上游只有 pop 有）", homeTabs[tab])
		}
		atStart := listPos(tt, homeTabs[tab])
		tt.Scroll(homeW/2, homeH-150, 0, 1200)
		homeFrame(tt)
		after := listPos(tt, homeTabs[tab])
		if len(after) == 0 {
			t.Errorf("「%s」那一档滚过去之后一首歌都没画出来", homeTabs[tab])
			continue
		}
		if slices.Equal(after, atStart) {
			t.Errorf("「%s」那一档滚了 1200 像素之后画面没变——它滚不动了", homeTabs[tab])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 一行歌都没有时说的话
// ─────────────────────────────────────────────────────────────────────────────

// TestTheHomePageStillSaysWhyItIsEmpty 钉住首页的空状态**没被「接了内容」这一下
// 弄丢**。
//
// 首页接进 `builtPage` 之后，`routes` 表里 `/` 那一条的 `Empty` 就不走
// `pageContent` 的兜底分支了——而兜底那一支会把整页换掉，连上面那排 Tab 一起没。
// 所以 `Empty` 换了个地方活：首页自己在 `viewHome` 里画它。
//
// 逐个 Tab 走一遍（三个 Tab 一行歌都没有时画的是同一句，所以三档都该有），再验
// **两句话都在**：标题说「这里为什么空」，提示说「怎么才能不空」。
func TestTheHomePageStillSaysWhyItIsEmpty(t *testing.T) {
	want := lookup(homePath).Empty
	if strings.TrimSpace(want.Title) == "" || strings.TrimSpace(want.Hint) == "" {
		t.Fatalf("routes 表里 %s 的空状态自己就不全：%+v", homePath, want)
	}

	_, tt := newHome(t, homeSongs{})
	for i, name := range homeTabs {
		clickTab(t, tt, i)
		if !shows(tt, want.Title) {
			t.Errorf("「%s」上没有画出 %q，文字有 %q", name, want.Title, tt.Texts())
		}
		if !shows(tt, want.Hint) {
			t.Errorf("「%s」上没有画出「怎么才能不空」%q，文字有 %q", name, want.Hint, tt.Texts())
		}
		// Tab 那一排在空状态之上还看得见——不然用户连换一档都做不到。
		if !shows(tt, name) {
			t.Errorf("空着的时候 %q 那一档的 Tab 不见了，文字有 %q", name, tt.Texts())
		}
	}
}

// TestOneEmptyTabDoesNotBlankOutTheOthers 验「空状态是**那一档**的事」：三个 Tab
// 各自有各自的歌，某一档是空的时候，另外两档照画自己的内容。
//
// 不然一个「只要有一档是空就整页画空状态」的实现会绿，而用户看到的是：点了一下
// 别的 Tab，整页的歌没了。
func TestOneEmptyTabDoesNotBlankOutTheOthers(t *testing.T) {
	songs := fullSongs()
	songs.tabs[1] = nil // 只有「鬼畜」是空的
	_, tt := newHome(t, songs)

	clickTab(t, tt, 1)
	if !shows(tt, lookup(homePath).Empty.Title) {
		t.Errorf("空着的那一档没有画空状态，文字有 %q", tt.Texts())
	}

	for _, i := range []int{0, 2} {
		clickTab(t, tt, i)
		if shows(tt, lookup(homePath).Empty.Title) {
			t.Errorf("切到有歌的 %q，却画了空状态，文字有 %q", homeTabs[i], tt.Texts())
		}
		if !shows(tt, songName(homeTabs[i], 1)) {
			t.Errorf("切到有歌的 %q，却没画出它的歌，文字有 %q", homeTabs[i], tt.Texts())
		}
	}
}

// TestTheLeadRowIsNeverTheCursor 钉住「页内头部那一行不是歌」。
//
// ⚠️ **基线上 Home 和 ↑ 都停在第一首歌上**（三档都一样）：页内头部占着框架的
// **第 0 行**，而框架的游标记的是**行号**——于是 Home / ↑ 会把光标停在那一行上，
// 而那一行不是歌：**选中项变成空**、**按 Enter 不放歌**、焦点环从「跟着那一行走」
// 退回「画在整张列表上」，而票 #245 的 E4「↑ 在顶上一行时把焦点交回侧栏」在
// 「流行」这一档也失效了（↑ 从第一首歌往上走会停在那一行上，而不是交回侧栏）。
//
// 所以逐键量：三档各按一次 Home 和一次 ↑，**光标都得停在第一首歌上**。
func TestTheLeadRowIsNeverTheCursor(t *testing.T) {
	// ⚠️ **窗口要有焦点**，否则框架的 `Focused()` 恒为假，「焦点在哪」这一整套
	// 判据就全是假的（见 `focus_test.go` 那一条的注释）。
	fc := &focusCtx{}
	s, tt := newShell(t, fc.opt())
	tt.SetFocused(true)
	s.home.SetSongs(fullSongs())

	for tab := range homeTabs {
		clickTab(t, tt, tab)
		// 把焦点交给这一档的列表，光标停在第一首歌上（和用户走进来时一样）。
		s.home.lists[tab].TakeFocus()
		homeFrame(tt)

		// Home：光标该停在第一首歌上。
		tt.Key(0, ui.KeyHome)
		homeFrame(tt)
		assertCursorOnFirstSong(t, s, tab, "Home")

		// Enter：光标停在歌上时，它放的是**那一首**。
		//
		// ⚠️ **这里量的是「选中项」而不是队列**：测试里那些歌放不了（不是真的
		// 音频），所以队列两种情况下都是空的 ✗ —— 区别在**光标停在哪一行**：
		// 停在页内头部那一行时，`OnSubmit` 走 `playFrom(-1)`，**什么也不放**。
		tt.Key(0, ui.KeyEnter)
		homeFrame(tt)
		assertCursorOnFirstSong(t, s, tab, "Enter")

		// ↑：光标该**留在**第一首歌上（基线上 ↑ 在顶上一行时把焦点交回侧栏，
		// 选中项不动；页内头部那一行不是歌，光标停上去的话选中项会变成空）。
		s.home.lists[tab].TakeFocus()
		homeFrame(tt)
		tt.Key(0, ui.KeyUp)
		homeFrame(tt)
		assertCursorOnFirstSong(t, s, tab, "↑")

		// ⚠️ **↑ 在顶上一行时把焦点交回侧栏**（票 #245 的 E4）：页内头部那一行
		// 不是歌，光标停上去的话这一半就失效了——焦点既没交回侧栏，光标也空了。
		if !s.sideAnchor.Focused(fc.c) {
			t.Errorf("「%s」这一档在顶上一行按 ↑ 之后焦点没交回侧栏——"+
				"光标大概停在页内头部那一行上了", homeTabs[tab])
		}
	}
}

// assertCursorOnFirstSong 断言这一档的光标停在**第一首歌**上。
func assertCursorOnFirstSong(t *testing.T, s *Shell, tab int, key string) {
	t.Helper()
	m, ok := s.home.lists[tab].Selected()
	want := songName(homeTabs[tab], 1)
	if !ok || m.Title != want {
		t.Errorf("「%s」这一档按 %s 之后选中的是 %q（ok=%v），want %q",
			homeTabs[tab], key, m.Title, ok, want)
	}
}

// TestTheNewMusicPagerIsClickableInsideTheList 钉住「翻页箭头点得动」。
//
// ⚠️ **这一块在列表的滚动区里**（它是「流行」那一档列表的第 0 行），而列表的浮动
// 滚动条就贴在它的右边：指针一悬停滚动条就出现，**按下被滚动条抢走**（框架的
// `pointerDown` 先问 `scrollbarPress`，滚动条那一帧画出来了就把这一次按下拿走），
// 于是那两枚翻页箭头按了没反应——**用户翻不到第三页**（新歌 9~12 那四首看不到）。
//
// ⚠️ **单独造这一块的测试量不到**（`newGridTester` 那一块不在列表里，没有滚动条），
// 所以这一条要**在首页上**跑。
//
// ⚠️ **两条都量**：直接点，和「先把指针悬停上去再点」——后者是评审探针里更狠的那条
// （悬停让滚动条出现，之后每一次按下都被它拿走）。
func TestTheNewMusicPagerIsClickableInsideTheList(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	clickTab(t, tt, homePopTab)

	// 一路翻到第三页：1 / 3 → 2 / 3 → 3 / 3。
	for _, want := range []string{"2 / 3", "3 / 3"} {
		if err := tt.Click("下一页"); err != nil {
			t.Fatalf("点不到「下一页」：%v，文字有 %q", err, tt.Texts())
		}
		homeFrame(tt)
		if got := readout(tt); got != want {
			t.Fatalf("翻页之后读数是 %q，want %q（箭头点不动：列表的浮动滚动条压在它上面）", got, want)
		}
	}

	// 翻回第一页，再来一遍「先悬停再点」——悬停会让滚动条出现。
	for range 2 {
		if err := tt.Click("上一页"); err != nil {
			t.Fatalf("点不到「上一页」：%v，文字有 %q", err, tt.Texts())
		}
		homeFrame(tt)
	}
	if got := readout(tt); got != "1 / 3" {
		t.Fatalf("翻回第一页读数是 %q，want 1 / 3", got)
	}

	// 把指针悬停在「下一页」上（滚动条于是出现），再点它。
	r, ok := tt.Find("下一页")
	if !ok || r.W == 0 {
		t.Fatalf("画面上找不到「下一页」，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	homeFrame(tt)

	if err := tt.Click("下一页"); err != nil {
		t.Fatalf("点不到「下一页」：%v，文字有 %q", err, tt.Texts())
	}
	homeFrame(tt)
	if got := readout(tt); got != "2 / 3" {
		t.Errorf("悬停之后点「下一页」，读数是 %q，want 2 / 3（滚动条把这一次按下抢走了）", got)
	}
}
