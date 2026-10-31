package ui

// 切 Tab 的过渡（票 #368）。
//
// 这一组钉三件事：
//
//  1. **切 Tab 有过渡**（不是硬切）；
//  2. **「流行」切进去不闪**：那一档还在拉的时候，**旧的内容留在画面上**，
//     「正在加载」那一格**不出现**；
//  3. **过渡走完之后，画面上就是新那一档**（过渡没有把内容停在半路上）。
//
// 判据是**画面**（像素）而不是状态字段：这一票动的是「画面上是什么」，
// 而「状态字段对了」不等于「画面对了」。
//
// # 为什么第一帧 ≠ 稳定帧就是「有过渡」的证据
//
// 硬切的话，新内容一上来就是最终样子——第一帧和稳定之后**一模一样**。
// 所以「两者不同」正是「中间有过一段」的证据；反过来，一个「切过去之后画面
// 变了、但变了两次」的实现（先空一下再换）也是绿的——那一条由下面「不闪」
// 那一组钉。

import (
	"image"
	"image/color"
	"math"
	"net/http"
	"runtime"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// 内容那一块在窗口里的位置（1200×800 的默认窗口）。
//
// ⚠️ **只比这一块**：点一下 Tab 会让主 Tab 那个滑块滑过去、选中的那一格换颜色
// ——那是**另一个动画**（`tabs.go` 的弹簧），它动不动的都跟「内容有没有过渡」
// 无关。整个窗口比的话，一个硬切的实现在这里也是绿的。
const (
	homeContentLeft = 224 // 侧栏右边那条沿（侧栏 200 + 内容区左边距）
	homeContentTop  = 120 // Tab 那一排下面
)

// contentChanged 是两帧在**内容那一块**里颜色变了的像素数。
func contentChanged(a, b *image.RGBA) int {
	var n int
	for y := homeContentTop; y < a.Rect.Max.Y; y++ {
		for x := homeContentLeft; x < a.Rect.Max.X; x++ {
			if color.RGBAModel.Convert(a.RGBAAt(x, y)) != color.RGBAModel.Convert(b.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// TestSwitchingTabsIsNotAHardCut 钉住「切 Tab 有过渡」。
//
// 四步：记下切换前的画面 → 点另一个 Tab → 记下**切换后第一帧**的画面 →
// 推钟让过渡走完，再记一张。
//
// ⚠️ **推钟而不是数帧**（AGENTS.md §2）：Tester 的钟站着不动，「画了几帧」
// 数的不是时间；而过渡是按钟走的，不推钟它永远走不完。
func TestSwitchingTabsIsNotAHardCut(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	before := snapshot(tt)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	tt.Frame()
	during := snapshot(tt)

	tt.Advance(homePagerDuration + 100*time.Millisecond)
	tt.Frame()
	after := snapshot(tt)

	// 硬切的话这两张一模一样。
	if n := contentChanged(during, after); n == 0 {
		t.Errorf("切换之后的第一帧和过渡走完之后**一模一样**（内容那一块一个像素都没变）：" +
			"这是硬切，没有过渡")
	} else if n < 500 {
		t.Errorf("切换之后的第一帧和过渡走完之后只差 %d 个像素——"+
			"那一段过渡短得看不出（实测这一块在换的像素有两万多个：两档的行一样多，"+
			"每一行的字和那一格底色都在换）", n)
	}

	// 走完之后画面上是**新那一档**的歌。
	if !shows(tt, songName(homeTabs[1], 1)) {
		t.Errorf("过渡走完之后画面上没有新那一档的歌，文字有 %q", tt.Texts())
	}
	// 旧那一档的歌不在了。
	if shows(tt, songName(homeTabs[0], 1)) {
		t.Errorf("过渡走完之后旧那一档的歌还在画面上，文字有 %q", tt.Texts())
	}
	// 切之前那张画面和走完之后那张**不一样**（不然上面那条就是白断的）。
	if contentChanged(before, after) == 0 {
		t.Errorf("切换前后画面上一模一样：切过去之后画的还是原来那一档")
	}
}

// TestSwitchingToATabThatIsStillLoadingShowsASkeleton 钉住**「流行」切进去画
// 骨架屏**。
//
// # 这一条**推翻了票 #368 的那一条**
//
// 票 #368 说的是「切到一档而它还在拉的时候，**旧的内容留在画面上**」——那是
// 靠「**不换**」兑现的（等那一档有着落了才换过去）。
//
// **这一票换了做法**（票 #706）：切 Tab 走**横向滑动**，而新的那一档**滑进来**
// ——它**不等数据**。数据没到的时候它画的是**骨架屏**（内容的形状已知：一屏
// 歌曲行）。
//
// ⇒ **「不闪」那一条还是兑现了**，只是兑现的方式换了：**从「留着旧内容」换成
// 「新内容以骨架屏的样子进来」**——用户看得见**自己到了哪一档**，而版面不跳。
//
// # 三件事
//
//  1. **那一档出现了**（它在画面上，不是还留着上一档）；
//  2. **画的是骨架屏**（「正在加载流行推荐」这句话在）；
//  3. 那一趟回来之后，画面上换成「流行」自己的歌。
//
// ⚠️ **第 2 条是这一票的正题**：骨架屏和转圈是**两档**（DESIGN.md §六《加载
// 态》那张表）——这一档是**形状已知**（一屏歌曲行），所以是骨架屏。
func TestSwitchingToATabThatIsStillLoadingShowsASkeleton(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「流行」那一档（音乐榜）要的那一趟按住在门口。
	gate := make(chan struct{})
	b.mu.Lock()
	rank := b.handlers[uiHomeMusicRank]
	b.mu.Unlock()
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		rank(w, r)
	})
	t.Cleanup(func() {
		// 用例在断言完之前就 return 了的话，别把收尾卡在这一趟上。
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return shows(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	homeFrame(tt)

	// 那一档还在拉：它**已经在画面上**了（滑进来了），画的是骨架屏。
	if !homeSkeletonShown(tt, homeTabNouns[homePopTab]) {
		t.Errorf("切到「流行」而它还在拉的时候，画面上没有说它在加载，文字有 %q\n"+
			"（这一票要的是**骨架屏**：内容的形状已知，用骨架屏而不是转圈）",
			tt.Texts())
	}
	// 而**空状态不许出现**：那是「B 站真的没有推荐」的说话方式，而那一档还在拉。
	if shows(tt, lookup(homePath).Empty.Title) {
		t.Errorf("切到「流行」而它还在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	// 放它过去：那一屏自己换掉（这一次**有**过渡，见上面那一条）。
	close(gate)
	waitFramesClock(t, tt, "「流行」那一档拉回来了", func() bool {
		return shows(tt, "流行 榜上的歌")
	})
	// ⚠️ **要推完那一段横滑再看「旧的那一档还在不在」**（票 #706）：切 Tab 是
	// **一次横滑**，而**滑的那一段里两档都在画面上**（一档滑出、一档滑进）——
	// 所以「新的一档出现了」**不等于**「旧的一档没了」。
	//
	// ⇒ 断言「旧的那一档没了」要等**滑完**（那一段走完，旧的才整个滑出容器）。
	waitFramesClock(t, tt, "横滑走完了", func() bool {
		return !shows(tt, "音乐 推的第一首")
	})
	if shows(tt, "音乐 推的第一首") {
		t.Errorf("「流行」那一档到货了，画面上还是旧那一档的歌，文字有 %q", tt.Texts())
	}
}

// TestSwitchingToATabThatIsStillLoadingStillShowsTheSkeletonAfterTheClockMoves
// 钉住**「切到一档而它还在拉」时，钟走了之后骨架屏还在**（票 #706）。
//
// # 这一条和上一条差在哪儿
//
// 上一条**只画帧不推钟**（Tester 的钟站着不动，AGENTS.md §2），于是它看不见
// 「过了 200ms 之后」的样子。而真窗口里钟**是走的**——横滑走完之后内容就换
// 过去了，而那一档的歌**还没到**，屏幕上于是**应该**是骨架屏（而**不是**空、
// 也不是转圈）。
//
// # 这一条断的是「走完之后那一档还是骨架屏」
//
// ⚠️ **推着钟走 400ms**：真窗口里钟是走的，而那一段横滑是按钟走的（200ms），
// 400ms 足够走完一整段——**走完之后画面上就只剩新那一档了**，而它还在拉。
func TestSwitchingToATabThatIsStillLoadingStillShowsTheSkeletonAfterTheClockMoves(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「流行」那一档（音乐榜）要的那一趟按住在门口。
	gate := make(chan struct{})
	b.mu.Lock()
	rank := b.handlers[uiHomeMusicRank]
	b.mu.Unlock()
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		rank(w, r)
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return shows(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	// ⚠️ **推着钟走 400ms**（真窗口里钟是走的）：横滑那一段是 200ms，400ms
	// 足够走完一整段——而那一档的歌**还没到**。
	for range 25 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}

	// 那一档还在拉：画面上的是**那一档自己的骨架屏**（横滑已经走完了，画面
	// 上就只剩新那一档）。
	if !homeSkeletonShown(tt, homeTabNouns[homePopTab]) {
		t.Errorf("切到「流行」而它还在拉的时候（推着钟走了 400ms），画面上没有骨架屏，文字有 %q",
			tt.Texts())
	}
	// 而**旧那一档不该还在**（横滑走完了，它已经滑出去了）。
	if shows(tt, "音乐 推的第一首") {
		t.Errorf("横滑走完了，画面上还留着上一档的歌，文字有 %q", tt.Texts())
	}
}

// TestTheLoadingCellOnlyShowsWhenThereIsNothingToShow 钉住「正在加载」那一格
// **只在真的没内容可画时**出现。
//
// 两个方向都要：
//
//   - **启动时**（屏幕上确实一首歌都没有）→ 那一格出现；
//   - **切到一档而它还在拉**（屏幕上还有上一档）→ 那一格**不**出现。
//
// ⚠️ **第二条方向变了**（票 #706）：切 Tab 走**横向滑动**，而新的那一档**不等
// 数据**——它滑进来，画的是**骨架屏**（形状已知）。所以「那一格不出现」这一
// 条**不再成立**：切到一档而它还在拉的时候，屏幕上**就是**那一档的骨架屏。
// 而骨架屏的**无障碍名字是同一句话**（「正在加载音乐推荐」）——所以上面那两条
// 断言（启动时那句在、空状态不在）**照旧成立**。
//
// 这一条补的是第一个方向——少了它，一个「那一格永远不出现」的实现在上面那
// 一条里也是绿的。
func TestTheLoadingCellOnlyShowsWhenThereIsNothingToShow(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	gate := make(chan struct{})
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		uiExploreJSON(w, 0, homeArchiveData("音乐"))
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "那一趟已经发出去了", func() bool { return b.hit(uiHomeRegionFeed) })
	homeFrame(tt)

	// 屏幕上确实一首歌都没有 → 那一格该在。
	if !homeSkeletonShown(tt, homeTabNouns[0]) {
		t.Errorf("启动时屏幕上什么都没有，却没说「正在加载」，文字有 %q", tt.Texts())
	}
	// 而空状态**不**该在（那是「B 站真的没有推荐」的说话方式）。
	if shows(tt, lookup(homePath).Empty.Title) {
		t.Errorf("正在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	close(gate)
	waitFrames(t, tt, "拉回来了", func() bool { return shows(tt, "音乐 推的第一首") })
}

// TestTheTransitionDoesNotCostAFrameEveryFrame 钉住「过渡没有让稳态多花」。
//
// ⚠️ **判据是「稳态下不再要帧」**：过渡那 200ms 里界面当然要一帧一帧地画，
// 而**过渡走完之后**它必须安静下来——一个「过渡一直转着」的实现在这里会红
// （而那种实现每帧都在占 CPU）。
//
// ⚠️ **推钟再断**：Tester 的钟站着不动，不推钟的话过渡永远走不完，
// 「安静下来」这件事就永远不成立（AGENTS.md §2）。
func TestTheTransitionDoesNotCostAFrameEveryFrame(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	// ⚠️ **推 800ms，不是 200ms**：这一档还有**另一个**动画在跑——主 Tab 那个
	// 滑块（`tabs.go`）。而首页那一排现在走的是「和底下同步」那一档
	// （`tabSlideSynced`，和内容同时长 200ms），所以 200ms 就够——但推 800ms
	// 更稳（它要断的是「有没有东西永远转下去」）。
	tt.Advance(800 * time.Millisecond)
	tt.Frame()

	// 过渡走完之后：界面不再要帧。
	if tt.FramePending() {
		t.Error("过渡走完之后界面还在一帧一帧地要——那一段动画没有停下来")
	}
	// 再画一帧也不该再要（稳态）。
	tt.Frame()
	if tt.FramePending() {
		t.Error("稳态下还在要帧")
	}
}

// TestBothPagesAreOnScreenWhileTheContentSlides 钉住**滑动那一段两档都在
// 画面上**（票 #706）。
//
// # 这一条**推翻了票 #238 的裁决**
//
// 票 #238 的 F1 判的是「**中间不同时画两层**」——它把「同一时刻画面上有两层
// 内容」判为**重影缺陷**，所以旧的那一档淡完之前新的一档不建。
//
// **现在推翻那个裁决**（用户明确同意），理由是：
//
//	淡入淡出时两层**叠在同一位置**（新的一层盖在旧的一层上面）——那是重影。
//	横向滑动时两层**在空间上不重叠**——那不是重影，是 ViewPager。
//
// ⇒ 这一条现在断的是「两档同时在，而且**不重叠**」。
//
// ⚠️ **「不重叠」由另一条断**（`TestSwitchingTabsKeepsBothPagesApart`，在
// `homepage_pager_test.go` 里）：它量的是两个盒子的几何位置。这一条只断
// 「两档都在」。
//
// ⚠️ **一路推到过渡走完**：每一帧都查一遍「两档一起在 → 然后只剩一档」。
// 判据是**状态**（哪一档的字在画面上），不是时间。
//
// # 「在画面上」有两种，判据要分开
//
//   - **旧的那一档**（滑出去的那一档）：**滑的那一段里它一直有可见宽度**
//     （它从容器右边一路缩到左边）——所以判据是**可见宽度大于 0**；
//   - **新的那一一档**（滑进来的那一一档）：**第一帧上它正好在容器的右边沿**
//     （可见宽度是 **0**——它还没进来）——那是 ViewPager 的样子（Android 的
//     ViewPager 就是这样：新宿页先在屏外等着）。所以**第一帧**判据是
//     **它在树上**（内容建出来了、位置算对了），而**往后每一帧**判据是
//     **可见宽度大于 0**。
//
// ⚠️ **「它在树上」用 `HasText` 量，不用 `shows`**：`shows` 要求盒子有正宽
// （`tt.Find` 报的是**裁切之后**的盒子，见 `mygo/ui/runtime.go` 的
// `commitElement`），而第一帧上新的那一档**恰好在容器的右边沿**——可见宽度
// 正好是 0。仓库里别处也是这么分的（见 `homepage_pager_test.go` 的
// `homeSkeletonShown`：那一档在滑进来的过程中是**被裁掉的**，所以那里量的是
// **名字在不在**）。
func TestBothPagesAreOnScreenWhileTheContentSlides(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	old := songName(homeTabs[0], 1)
	fresh := songName(homeTabs[1], 1)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	// 切之后的第一帧：两档**都在**（旧的在往左走，新的在从右边进来）。
	tt.Frame()
	if !shows(tt, old) {
		t.Errorf("切 Tab 之后的第一帧上旧的那一块（%q）不在画面上了——\n"+
			"这一票是**横滑**：旧的那一档要滑出去，它得**在画面上**才滑得出去", old)
	}
	// ⚠️ **新的那一档在第一帧上「在树上」而不是「有可见宽度」**：它正好在容器
	// 的右边沿（可见宽度 0）——**它得被建出来**（否则用户点不动它），而位置由
	// 别的那几条断（`homepage_pager_test.go` 的
	// `TestSwitchingTabsKeepsBothPagesApart` 量的是两个盒子的几何位置）。
	if !tt.HasText(fresh) {
		t.Errorf("切 Tab 之后的第一帧上新的那一块（%q）不在树上了——\n"+
			"这一票是**横滑**：新的那一档从右边滑进来，它得**在树上**才滑得进来",
			fresh)
	}

	// 一路推到过渡走完：先两档一起在，然后只剩新的一档。
	//
	// ⚠️ **这一段断的是「两档**都**有可见宽度」**（比原来那条强）：滑的那一段
	// 里旧的那一档正在缩窄、新的那一档正在变宽——**两档各占一段，中间不空**
	// （那正是「横向滑动真的发生了」的判据；曾经那个 `Left(width)` 的几何算错
	// 会让新的那一档**整个在容器外面**，于是这一段就红了）。
	for i := range 40 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		oldShown, newShown := shows(tt, old), shows(tt, fresh)
		if !oldShown && newShown {
			// 换完了：旧的那一块没了，新的那一块在。
			return
		}
		if !newShown {
			t.Fatalf("第 %d 帧上新的那一块（%q）不在画面上了——\n"+
				"横滑那一段**两档都在**（一档滑出、一档滑入）", i, fresh)
		}
		if !oldShown {
			t.Fatalf("第 %d 帧上旧的那一块（%q）已经不在画面上了——\n"+
				"横滑那一段**两档都在**：旧的那一档是**滑出去**的，不是**一下子没了**\n"+
				"（一下子没了就是票 #238 说的那种「先走旧的、再进新的」，而这一票要的是"+
				"**同起同落**）", i, old)
		}
	}
	t.Fatalf("推了 40 帧（640ms），过渡还没走完：旧的那一块 %q 和新的那一块 %q "+
		"都不在画面上，文字有 %q", old, fresh, tt.Texts())
}

// TestThePopTabAppearsWhileTheNewMusicIsStillLoading 钉住**「流行」那一档在
// 「新歌速递」还在拉的时候就出现**（票 #238 的 F3）。
//
// # 走的是真路子
//
// 假 B 站把**新歌速递**那一趟按在门口不放，而**音乐榜**那一趟放行——于是「流行」
// 自己的歌到了，新歌速递还在拉。此前这一档**整档都不出现**（新歌速递也算进了
// 「这一档齐了没有」），屏幕上一直是上一档的内容，而**全程没有任何反馈**。
//
// # 两件事
//
//  1. **「流行」那一档出现了**（它自己的歌在画面上）；
//  2. **新歌速递那一块自己说它在拉**（"正在加载新歌速递"）——用户看得见**哪一
//     块在等**。
func TestThePopTabAppearsWhileTheNewMusicIsStillLoading(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「新歌速递」那一趟按住在门口（**不是**音乐榜那一趟——那一趟到了，「流行」
	// 自己的歌也就到了）。
	gate := make(chan struct{})
	b.mu.Lock()
	newMusic := b.handlers[uiHomeNewMusic]
	b.mu.Unlock()
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		newMusic(w, r)
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return shows(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	// ⚠️ **推着钟等**：切 Tab 那一段过渡是按钟走的（旧的先淡出、新的后淡进），
	// 而新歌速递还在拉的时候那一档**照样出现**——它不等新歌速递。
	waitFramesClock(t, tt, "「流行」那一档出现了", func() bool {
		return shows(tt, "流行 榜上的歌")
	})
	// 而新歌速递那一块**自己说它在拉**——用户看得见哪一块在等。
	if !shows(tt, "正在加载新歌速递") {
		t.Errorf("切到「流行」而新歌速递还在拉的时候，界面上没有说哪一块在等，文字有 %q",
			tt.Texts())
	}
	// ⚠️ **旧的那一档不许还在**：这一档已经出现了，屏幕上不该还有上一档的歌。
	//
	// ⚠️ **要推完那一段横滑再看**（票 #706）：切 Tab 是**一次横滑**，而**滑的
	// 那一段里两档都在画面上**（一档滑出、一档滑进）——所以「新的一档出现了」
	// **不等于**「旧的一档没了」。断言「旧的那一档没了」要等**滑完**（那一段
	// 走完，旧的才整个滑出容器）。
	waitFramesClock(t, tt, "横滑走完了", func() bool {
		return !shows(tt, "音乐 推的第一首")
	})
	if shows(tt, "音乐 推的第一首") {
		t.Errorf("「流行」那一档已经出现了，画面上还留着上一档的歌，文字有 %q", tt.Texts())
	}
}

// TestTheFramesWhileSwitchingTabsCostNoMoreThanTwoPages 钉住**切 Tab 那一段
// 每一帧的分配**（票 #706 改了「切 Tab 的第一帧没有多分配一笔」那条）。
//
// # 这一条改的是**量法**，不是**结论**
//
// 此前那一条（票 #238 的 F4）量的是「点一下那一下」摊到**帧**上的分配，而那个
// 量法在这一票之后**量不到该量的东西**：
//
//   - `tt.Click` 的 settle 会在**同一个钟**上一次画二十帧（`mygo/ui/headless.go`
//     的 `settle`），而切 Tab 那一段是**按钟走的**——于是那二十帧**全都停在
//     同一刻**；
//   - 而**每一档的内容是第一次被画**（那一档的列表从零建起）——所以那二十帧
//     里**每一帧**都在建那一档的列表，而摊到每一帧上是「建一整档」的数。
//
// ⇒ 那个数（实测每帧 114 KB）量的是「**建一整档列表**要多少」，不是「切 Tab
// 多花多少」。
//
// # 这一条量的是**真的**那个数
//
// **推着钟**一帧一帧地画（每一帧是**不同的时刻**，而那一段是按钟走的），量每
// 一帧的分配——然后比**两档并存的稳态**。
//
// ⚠️ **判据是「不超过两档并存的稳态的一个零头」**：切 Tab 那一段**本来就该**
// 是两档并存（那一档在滑出去、新的在滑进来），所以它的成本**就该**是两档的
// 那些，而不是一档的那些。
//
// ⚠️ **推钟而不是数帧**（AGENTS.md §2）：Tester 的钟站着不动，不推钟的话那一
// 段永远走不完，而每一帧都停在同一刻（那正是上面说的那个量法的毛病）。
func TestTheFramesWhileSwitchingTabsCostNoMoreThanTwoPages(t *testing.T) {
	s, tt := newHome(t, fullSongs())
	// 先把两档都画过一遍（那一档的列表就建好了）：下面量的是「切 Tab 那一段
	// 每帧多花多少」，而**第一次**建一档的列表是**另一件事**（见下面那条）。
	for _, tab := range []int{1, 2} {
		if err := tt.Click(homeTabs[tab]); err != nil {
			t.Fatalf("点不到 %q 那一档：%v", homeTabs[tab], err)
		}
		tt.Advance(homePagerDuration + 50*time.Millisecond)
		tt.Frame()
	}
	// 回到第一档。
	if err := tt.Click(homeTabs[0]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[0], err)
	}
	tt.Advance(homePagerDuration + 50*time.Millisecond)
	tt.Frame()
	for range 8 {
		tt.Frame()
	}

	// 稳态（一档）：先量几帧，把「只有头几帧才有」的一次性开销付掉。
	steady, _ := measureHomeFrameCost(t, s, tt, 10, false)

	// ⚠️ **量的是「滑动那一段每一帧」**：Tester 的 `Click` 自己的 settle 会在
	// **同一个钟**上一次画二十帧（`mygo/ui/headless.go` 的 `settle`）——而那一
	// 段是**按钟走的**，所以那二十帧**全都停在同一刻**（那一档一动不动）。
	//
	// ⇒ 所以**推着钟一帧一帧地量**（`measureHomeFrameCost` 的 `advance` 那一支）：
	// 每一帧是**不同的时刻**，量到的就是「滑动那一段每一帧」的成本。
	//
	// ⚠️ **实测（这一票，2026-10-31 重新量过）**：一档的稳态是 **134 次 /
	// 4704 字节**，而**滑动那一段的每一帧**是 **166 次 / 5232 字节**——**+32 次 /
	// +528 字节**（稳态的 **1.24 倍 / 1.11 倍**）。
	//
	// ⚠️ **那不是翻倍**：多出来的那部分是**第二档的壳**（表头 + 列表 + 它的
	// 滚动容器 + 它的行），不是它的全部行。
	//
	// ⚠️ **量之前要把两档都画过一遍**（下面那三句）：否则**第一帧**要**从零建
	// 那一档的列表**，而那个数（实测每帧 114 KB）量的是「建一整档」，不是「滑动
	// 多花多少」。
	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	worst := homeFrameCost{allocs: 0, bytes: 0}
	for range 12 {
		cost, frames := measureHomeFrameCost(t, s, tt, 1, true)
		if frames <= 0 {
			// 那一段走完了（界面不再要帧）——后面就没有帧可量了。
			break
		}
		if cost.allocs > worst.allocs {
			worst.allocs = cost.allocs
		}
		if cost.bytes > worst.bytes {
			worst.bytes = cost.bytes
		}
	}
	if worst.allocs == 0 {
		t.Fatal("滑动那一段一帧都没量到")
	}

	// ⚠️ **上限是 1.5 倍**：那一段**本来就要建两档**（一档滑出、一档滑进），而
	// 实测是 +32 次 / +528 字节（稳态的 **1.24 倍 / 1.11 倍**）——1.5 倍留着
	// 余量，而**翻倍**（2.0 倍）那个上限**抓不住任何回归**（实测离它还有一半远）。
	//
	// ⚠️ **上限从 2.0 收到 1.5**（2026-10-31）：2.0 倍是**照着旧的那个错数**
	// （195 次 / 5854 字节，稳态的 1.46 倍）定的——而那个数是错的。重新量过之后
	// 实测只有 **1.24 倍 / 1.11 倍**，2.0 倍那个上限**什么回归都抓不住**。
	const allocsFactor = 1.5
	const bytesFactor = 1.5
	if worst.allocs > steady.allocs*allocsFactor {
		t.Errorf("切 Tab 那一段最贵的一帧分配了 %.0f 次，一档的稳态是 %.0f 次"+
			"（上限 %.1f 倍）——\n"+
			"那一段**本来就要建两档**（一档滑出、一档滑入），而实测是 166 次 /\n"+
			"5232 字节（一档的稳态是 134 次 / 4704 字节）",
			worst.allocs, steady.allocs, allocsFactor)
	}
	if worst.bytes > steady.bytes*bytesFactor {
		t.Errorf("切 Tab 那一段最贵的一帧分配了 %.0f 字节，一档的稳态是 %.0f 字节"+
			"（上限 %.1f 倍）——\n"+
			"那一段**本来就要建两档**（一档滑出、一档滑入），而实测是 166 次 /\n"+
			"5232 字节（一档的稳态是 134 次 / 4704 字节）",
			worst.bytes, steady.bytes, bytesFactor)
	}
}

// TestTheSlideStartsWithTheOldPageInPlace 钉住**滑的那一段的第一帧，旧的���一
// 档还在原地**（票 #706）。
//
// # 这一条**推翻了票 #238 的裁决**
//
// 票 #238 的对应那一条（`TestTheFadeOutStartsAtFullOpacity`）断的是「退场那一
// 段的第一帧是满亮的」——那是**淡出**那一套的判据（不透明度是 0 的话整棵子
// 树不画，屏幕上只剩底色，用户看见的是一下「闪」）。
//
// **横滑没有那个问题**：位置是 0 意味着「它还在原地」，而原地就是**画得好好的**
// ——没有「不透明度是 0 ⇒ 整棵子树不画」那一条。
//
// ⇒ 所以这一条断的是**位置**：滑出去那一段的第一帧，旧的那一档**在 x=0**
// （原地），而新的那一档**在容器右边**（width）——两者并排，不重叠。
//
// ⚠️ **Tester 里看不见那一帧**：`tt.Click` 的 settle 在**同一个钟**上一次画
// 二十帧（`mygo/ui/headless.go`），后面十九帧把第一帧盖掉了——所以这里断的
// 是**算式**，不是画面。
func TestTheSlideStartsWithTheOldPageInPlace(t *testing.T) {
	now := time.Unix(2026, 0)
	width := float32(960)
	s := homePager{phase: homePagerSlide, to: 1, at: now}
	oldX, newX := homePagerOffsets(s, now, width)
	if oldX != 0 {
		t.Errorf("滑的那一段的第一帧，旧的那一档在 x=%v（want 0，在原地）——\n"+
			"它要从**原地**滑出去，不是从别处冒出来", oldX)
	}
	if math.Abs(float64(newX-width)) > 0.5 {
		t.Errorf("滑的那一段的第一帧，新的那一档在 x=%v（want %v，在容器右边）——\n"+
			"它要从**右边**滑进来", newX, width)
	}
}

// TestThePagerDoesNotCopyTheOldContent 钉住**滑动那一段不复制旧内容**
// （票 #238 的 F4 的根因）。
//
// ⚠️ **这一条现在是白盒的替代品**：滑动那一段**根本不用框架的 `Transition`**——
// 两档的内容是**两棵真的树**（各挂各的 key，见 `homeBodyKeys`），位置是我们
// 自己按曲线算的（`homePagerOffsets`）。
//
// 所以「不复制旧内容」这一条由**下面那个纯函数**保证：位置一律**由曲线算**，
// 不是「上一帧是多少」——而框架的 `Exit` 那一半（`mygo/ui/transition.go` 的
// `ghostOf`）**根本不在这一条路上**。
//
// ⚠️ **那一帧不闪**：横滑的第一帧旧的那一档在**原地**（满好的），而新的那一
// 档在**容器右边**（还没进来，但**它在树上**）——所以内容那一块**始终有东西**。
//
// # 🔴 **这一条以前是一条**恒真断言**（票 #706 的一致性修复）
//
// 以前这里是 `var homePagerUsesFrameworkTransition = false`（一个**编译期常量
// false**）加上 `if homePagerUsesFrameworkTransition { t.Error(...) }` ⇒ **这一条
// 永远绿**，无论实现怎么改。
//
// ⇒ 而它号称是票 #238 的 F4（切 Tab 第一帧多分配 204 KB）的**白盒替代品**——
// 真正的实现（`homePagerOffsets` 确实纯按曲线算）如果哪天被人改成「读上一帧的
// 位置」，那条测试**照样绿**。
//
// ⇒ 所以现在断的是**实现本身**：位置**只由「过了多久」决定**，与「那一段什么时
// 候开始的」和「上一帧画在哪儿」**都无关**。
func TestThePagerDoesNotCopyTheOldContent(t *testing.T) {
	now := time.Unix(2026, 0)
	width := float32(960)

	// ⚠️ **同一个「过了多久」，两个不同的起点 ⇒ 位置必须一样**。
	//
	// ⚠️ **为什么这一条断得住**：位置要是「读上一帧的值」，那它的值会**依赖
	// 于上一帧画在哪儿**——而「上一帧画在哪儿」只可能来自**上一次调用**（那个
	// 就是「复制上一帧的样子」）。而 `homePagerOffsets` 是**纯函数**：它只拿
	// 「时刻」和「这一段的起点」，算不出「上一帧在哪儿」——所以两个不同的起点
	// 必然给出同一个结果。
	//
	// ⚠️ **而这一条**真的**会红**：如果有人把 `homePagerOffsets` 改成读某个
	// 「上一帧位置」的字段，那两次调用的结果就不再只由「过了多久」决定了。
	for _, ms := range []int{0, 25, 50, 100, 150, 199} {
		elapsed := time.Duration(ms) * time.Millisecond
		s1 := homePager{phase: homePagerSlide, to: 1, at: now}
		s2 := homePager{phase: homePagerSlide, to: 1, at: now.Add(-37 * time.Second)}
		old1, new1 := homePagerOffsets(s1, now.Add(elapsed), width)
		old2, new2 := homePagerOffsets(s2, now.Add(elapsed), width)
		if old1 != old2 || new1 != new2 {
			t.Errorf("%dms 时两档的位置取决于「这一段什么时候开始的」"+
				"（起点 A：旧 %.1f / 新 %.1f；起点 B：旧 %.1f / 新 %.1f）——"+
				"位置必须**只由曲线算**（由「过了多久」决定），"+
				"不是「上一帧是多少」（那正是框架 `Exit` 那一半干的事，"+
				"它要复制一份上一帧的样子重新排版，实测 204 KB）",
				ms, old1, new1, old2, new2)
		}
	}
}

// homeClickCost 量「点一下那一下」的分配（按画了几帧摊平成每帧），取几次里
// **最小**的那一个。
//
// ⚠️ **量的是 `tt.Click` 自己**：**第一帧就在它的 settle 里**（Tester 的
// `Click` 会画帧，见 `mygo/ui/headless.go`）。量 settle **之后**那一帧量不到
// 第一帧——而框架那份复制品正是在第一帧建的。
//
// ⚠️ **按「画了几遍首页」摊平**：那一次 settle 在界面还在要帧的时候会一次画
// 二十帧（`mygo/ui/headless.go` 的 `settle`），而切 Tab 那一段过渡正在要帧——
// 不摊平的话「每帧」这个单位就不是一帧了。
//
// ⚠️ **取最小**：测量窗口里可能有别的线程的一次性开销，而那种开销**只会加不
// 会减**。
func homeClickCost(t *testing.T, s *Shell, tt *mygoui.Tester, tabs []int) homeFrameCost {
	t.Helper()
	best := homeFrameCost{allocs: math.MaxFloat64, bytes: math.MaxFloat64}
	for _, tab := range tabs {
		old := runtime.GOMAXPROCS(1)
		s.builds = buildTally{}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if err := tt.Click(homeTabs[tab]); err != nil {
			t.Fatalf("点不到 %q 那一档：%v", homeTabs[tab], err)
		}
		runtime.ReadMemStats(&after)
		runtime.GOMAXPROCS(old)
		frames := float64(s.builds.home)
		if frames <= 0 {
			t.Fatalf("点 %q 那一下首页一帧都没建——量不到东西", homeTabs[tab])
		}
		cost := homeFrameCost{
			allocs: float64(after.Mallocs-before.Mallocs) / frames,
			bytes:  float64(after.TotalAlloc-before.TotalAlloc) / frames,
		}
		if cost.allocs < best.allocs {
			best.allocs = cost.allocs
		}
		if cost.bytes < best.bytes {
			best.bytes = cost.bytes
		}
		// 把那一段过渡走完，再切下一档。
		tt.Advance(homePagerDuration + 100*time.Millisecond)
		tt.Frame()
	}
	return best
}

// homeFrameCost 是量出来的一帧的分配。
type homeFrameCost struct {
	allocs float64
	bytes  float64
}

// measureHomeFrameCost 量 n 帧的平均分配，返回这一批画了几帧。
//
// ⚠️ **单核**（`GOMAXPROCS=1`）：免得后台线程在测量窗口里被调度到，把**别的
// 线程**的分配记到界面这一帧的账上（同 framebudget_test.go 的那三条约定）。
//
// ⚠️ **按「画了几遍首页」摊平**：Tester 的 `Frame()` 在界面还在要帧的时候会
// **一次画二十帧**（`mygo/ui/headless.go` 的 `settle`）——稳态下界面不要帧，
// 那是「一帧就是一帧」，而切 Tab 那一段过渡正在要帧。
//
// ⚠️ **advance 是「推钟画一帧」**：Tester 的钟站着不动，而 `Advance` 要等钟走到
// **下一个刷新时刻**才画那一帧（`mygo/ui/headless.go` 的 `due`）——推 20ms 正好
// 画一帧（刷新是 60Hz，也就是 16.7ms 一帧）。
//
// ⚠️ **advance 那一支不额外调 `tt.Frame()`**：`Advance` **自己就画了那一帧**
// （`mygo/ui/headless.go` 的 `Advance`：钟走到刷新时刻且界面要了帧，就画一帧），
// 再调一次 `tt.Frame()` 是**多画一帧**——而那一帧是**同一刻**的（钟又站住了），
// 量进去就把它算成两帧。
func measureHomeFrameCost(t *testing.T, s *Shell, tt *mygoui.Tester, n int, advance bool) (homeFrameCost, float64) {
	t.Helper()
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	s.builds = buildTally{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		if advance {
			tt.Advance(20 * time.Millisecond)
		} else {
			tt.Frame()
		}
	}
	runtime.ReadMemStats(&after)
	frames := float64(s.builds.home)
	if frames <= 0 {
		// 这一批里首页一帧都没建——**没有帧可量**。
		//
		// ⚠️ **这不是错误**：切 Tab 那一段走完之后界面**不再要帧**，而那一批
		// 正好落在那一段之后（见上面那个循环的注释）。调用方自己判（那一帧
		// 是「没有」而不是「测到了 0」）。
		return homeFrameCost{}, 0
	}
	return homeFrameCost{
		allocs: float64(after.Mallocs-before.Mallocs) / frames,
		bytes:  float64(after.TotalAlloc-before.TotalAlloc) / frames,
	}, frames
}

// 免得「这一组用不到」被当成「没写」：那个 `snapshot` 是满屏那一族留下的
// （`fullplayer_review_test.go`），这一族借它用。
var _ = mygoui.Smooth
