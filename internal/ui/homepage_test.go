package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

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
func clickTab(t *testing.T, tt *ui.Tester, tab int) {
	t.Helper()
	if err := tt.Click(homeTabs[tab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[tab], err, tt.Texts())
	}
	homeFrame(tt)
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
