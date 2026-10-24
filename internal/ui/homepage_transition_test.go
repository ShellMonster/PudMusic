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
	"net/http"
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

	tt.Advance(homeBodyDuration + 100*time.Millisecond)
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

// TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentOnScreen 钉住
// **「流行」切进去不闪**。
//
// 走的是真路子：假 B 站把「流行」那一档要的那一趟按在门口不放，用户点了
// 「流行」之后那一趟还没回来。
//
// 三件事：
//
//  1. **旧的内容留在画面上**（「音乐 推的第一首」还在）；
//  2. **「正在加载流行推荐」不出现**（那一格只在真的没内容可画时才有——
//     票 #368 的验收第 3 条）；
//  3. 那一趟回来之后，画面上换成「流行」自己的歌。
//
// ⚠️ **第 2 条是这一票的正题**：此前切过去的第一帧画的是「正在加载」那一格，
// 数据到了再换成列表——画面上就是「空 → 一屏内容」的一跳（票 #368 的原话）。
func TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentOnScreen(t *testing.T) {
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
	waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	homeFrame(tt)

	// 那一档还在拉：屏幕上留着的应该是**旧的内容**。
	if !showsText(tt, "音乐 推的第一首") {
		t.Errorf("切到「流行」而它还在拉的时候，旧的内容不见了，文字有 %q", tt.Texts())
	}
	if showsText(tt, "正在加载流行推荐") {
		t.Errorf("切到「流行」而它还在拉的时候画了「正在加载流行推荐」——" +
			"那一格只在真的没内容可画时才该出现（票 #368）")
	}
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("切到「流行」而它还在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	// 放它过去：那一屏自己换掉（这一次**有**过渡，见上面那一条）。
	close(gate)
	waitFrames(t, tt, "「流行」那一档拉回来了", func() bool {
		return showsText(tt, "流行 榜上的歌")
	})
	if showsText(tt, "音乐 推的第一首") {
		t.Errorf("「流行」那一档到货了，画面上还是旧那一档的歌，文字有 %q", tt.Texts())
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
// 第二个方向由上面那一条钉；这一条补的是第一个方向——少了它，一个「那一格
// 永远不出现」的实现在上面那一条里也是绿的。
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
	if !showsText(tt, "正在加载音乐推荐") {
		t.Errorf("启动时屏幕上什么都没有，却没说「正在加载」，文字有 %q", tt.Texts())
	}
	// 而空状态**不**该在（那是「B 站真的没有推荐」的说话方式）。
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("正在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	close(gate)
	waitFrames(t, tt, "拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })
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
	// 滑块的弹簧（`tabs.go` 的 `TabSlide`，实测 453ms 才收手，见 DESIGN.md
	// §五《主 Tab 滑块为什么是弹簧》）。要断的是「有没有东西永远转下去」，
	// 所以得等两个都停下来。
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

// 免得「这一组用不到」被当成「没写」：那个 `snapshot` 是满屏那一族留下的
// （`fullplayer_review_test.go`），这一族借它用。
var _ = mygoui.Smooth
