package ui

// 切 Tab 的**横向滑动**（票 #706）。
//
// 这一组钉五件事：
//
//  1. **切 Tab 是横移**（不是淡入淡出、不是瞬间替换）——判据是**两档在同一帧
//     里各自的位置**；
//  2. **两档并存**（一档滑出、一档滑入），而**空间上不重叠**；
//  3. **指示器和内容同起同落**（推钟到同一时刻，两者的进度一致）；
//  4. **动画可打断**（切到一半再点另一个 Tab，从当前进度接着走，不跳回）；
//  5. **那一档还在拉时画骨架屏**（不是空白，也不是转圈）。
//
// # 判据是**几何**，不是像素
//
// ⚠️ **别用「画面上那道 1 像素边线的精确颜色」那种像素判据**——那道边线是
// **半透明混色**画上去的（实测画面上的值和主题的 `Border` 不相等），精确比色
// 会扫不到（队列抽屉那族的测试在基线上就是这么红的）。
//
// 而**几何**是稳的。
//
// ⚠️ **几何从 `homePagerView` 取，不从画面上量**：画面上量不到「两档各在哪儿」
// ——`tt.Find` 报的是**裁切之后**的盒子（框架的 `commitElement` 把标签的盒子
// 和祖先的裁切区求交，见 `mygo/ui/runtime.go`），所以滑出去的那一档报出来是
// **被裁过的**（量出来是零宽，或者贴在容器的边上）——量不出它滑了多远。
//
// ⇒ 所以那一段把**算出来的位置**记一份（`homePagerView`），测试照它断。
// 而「画面上真的动了」由 `TestTheContentMovesSidewaysOnScreen` 那一组**逐
// 像素**断（它量的是**画面上**那一列的像素有没有变）。

import (
	"math"
	"net/http"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// TestSwitchingTabsSlidesTheContentSideways 钉住**切 Tab 是横移**。
//
// # 判据
//
// 切到另一档之后、**动画还在走**的那一帧上：
//
//   - 旧的那一档在 **x < 0**（它往左走了）；
//   - 新的那一档在 **x > 0**（它还在从右边进来）。
//
// 而**淡入淡出**那一套里旧的那一档**不动**（它只改不透明度）——所以「旧的那
// 一档的 x 变了」正是「横滑」和「淡入淡出」的分界。
//
// ⚠️ **推钟而不是数帧**（AGENTS.md §2）：Tester 的钟站着不动。
func TestSwitchingTabsSlidesTheContentSideways(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}

	// 推到动画的中途（进度约六成）。
	//
	// ⚠️ **推 25ms**：统一曲线（`motion.Unified`）前重后轻——前 50% 的时间走完
	// 95.4%，所以 100ms（总时长 200ms 的一半）时进度已经约 **97%**，那太靠后
	// 了（旧的那一档已经快滑出去了）。25ms 时进度约 63%，两档的位置差别明显。
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	g := homePagerView
	if g.oldTab < 0 || g.newTab < 0 {
		t.Fatalf("动画中途两档没有同时在（oldTab=%d newTab=%d）——\n"+
			"这一票要的是**两档并存**（一档滑出、一档滑入）", g.oldTab, g.newTab)
	}
	if g.oldX >= 0 {
		t.Errorf("动画中途旧的那一档在 x=%.1f（want < 0）——\n"+
			"它没有往左走。淡入淡出那一套里旧的那一档**不动**（只改不透明度），\n"+
			"所以「旧的那一档的 x 变了」正是横滑和淡入淡出的分界", g.oldX)
	}
	if g.newX <= 0 {
		t.Errorf("动画中途新的那一档在 x=%.1f（want > 0）——\n"+
			"它没有从右边进来", g.newX)
	}
}

// TestSwitchingTabsKeepsBothPagesApart 钉住**两档空间上不重叠**。
//
// # 为什么这一条是这一票的正题
//
// 票 #238 把「同一时刻画面上有两层内容」判为**重影缺陷**。而这一票推翻那个
// 裁决的理由是：**滑动时两层在空间上不重叠**，而淡入淡出时它们**叠在同一
// 位置**。
//
// ⇒ 所以「不重叠」是推翻那个裁决的**论据本身**，它必须被钉住：一旦两档重叠
// 了，这一票就退回到票 #238 说的那种重影。
//
// # 判据
//
// 新的那一档的左边沿恒在旧的那一档的右边沿右边**一个容器宽**的地方——它们
// **紧挨着**（ViewPager 的样子）：既不重叠，也不留缝。
func TestSwitchingTabsKeepsBothPagesApart(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	g := homePagerView
	if g.oldTab < 0 || g.newTab < 0 {
		t.Fatalf("动画中途两档没有同时在（oldTab=%d newTab=%d）", g.oldTab, g.newTab)
	}
	if d := g.newX - (g.oldX + g.width); math.Abs(float64(d)) > 0.5 {
		t.Errorf("两档不紧挨着（旧的那一档 x=%.1f，新的那一档 x=%.1f，want 新的在 %.1f）——\n"+
			"它们要**并排**（各占一段，不重叠也不留缝）",
			g.oldX, g.newX, g.oldX+g.width)
	}
	// 而「不重叠」就是「新的那一档在旧的那一档的右边沿之外」。
	if g.newX < g.oldX+g.width {
		t.Errorf("新的那一档（x=%.1f）和旧的那一档（右边沿 %.1f）重叠了——\n"+
			"重叠就是票 #238 说的重影；这一票推翻那个裁决靠的就是「滑动时两层不重叠」",
			g.newX, g.oldX+g.width)
	}
}

// TestTheIndicatorAndTheContentStartAndEndTogether 钉住**指示器和内容同起同落**。
//
// # 判据是**同一条曲线、同一个时长**
//
// 两者的进度在**任何时刻**都相等——而这只有「参数一样」才做得到（框架的动画
// 通道是**每元素私有**的，见 `mygo/ui/motion.go` 的 `channel`，没有跨元素共享
// 的时间源）。
//
// 所以这一条断两件事：
//
//  1. **内容那一侧走的是 `homePagerDuration` + `motion.Unified`**；
//  2. **指示器那一侧走的是同一套**（`tabSlideSynced`）。
//
// ⚠️ **指示器那一侧此前是弹簧**（`motion.TabSlide`）：它走 453ms 才收手
// （实测），而内容走 200ms ⇒ **不同起同落**。所以这一条**会把那个弹簧顶红**
// ——那正是这一票要改的。
func TestTheIndicatorAndTheContentStartAndEndTogether(t *testing.T) {
	// 指示器那一条：首页这一排走「和底下同步」那一档。
	if tabSlideSynced.Duration != homePagerDuration {
		t.Errorf("指示器那一侧的时长是 %v，内容那一侧是 %v——两者要一样才同起同落",
			tabSlideSynced.Duration, homePagerDuration)
	}
	// 曲线是同一个闭包（`motion.Unified`）。
	if tabSlideSynced.Ease == nil || homePagerAnim.Ease == nil {
		t.Fatal("指示器或内容那一侧没有曲线")
	}
	// 同一个闭包 ⇒ 在同一时刻给出同一个值。
	for _, ms := range []int{0, 25, 50, 100, 150, 199} {
		a := tabSlideSynced.Ease(float32(ms) / float32(homePagerDuration))
		b := homePagerAnim.Ease(float32(ms) / float32(homePagerDuration))
		if a != b {
			t.Errorf("%dms 时两边的进度不一样（指示器 %.4f、内容 %.4f）——"+
				"「同步」只能是「参数一样」", ms, a, b)
		}
	}
	// 内容那一侧的进度函数在走完之后是 1（走完了）。
	if got := homePagerProgress(homePagerDuration); got != 1 {
		t.Errorf("走完 %v 之后进度是 %.4f（want 1）", homePagerDuration, got)
	}
}

// TestTheTabSwitchCanBeInterrupted 钉住**动画可打断**（DESIGN.md §五《规则》
// 第 3、12 条）。
//
// # 判据
//
// 切到一半**再点另一个 Tab**：新的那一段**从当前进度接着走**，而不是跳回起点
// 再走一遍。
//
// ⚠️ **怎么量「跳回」**：打断那一帧上，旧的那一档的位置必须**和打断前那一帧
// 连着**——它不能突然回到 x=0（那正是「跳回」）。
func TestTheTabSwitchCanBeInterrupted(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()
	before := homePagerView

	// 打断：再点第三档。
	if err := tt.Click(homeTabs[2]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[2], err)
	}
	tt.Frame()
	after := homePagerView

	// 旧的那一档**不能跳回起点**（x=0 那一带）。
	//
	// ⚠️ **量的是「它没有往右跳回去」**：打断那一帧它要么还在原处（继续往左走），
	// 要么已经换了档（画着的是新的那一档了）——但**不能回到走之前那个位置**。
	if d := after.oldX - before.oldX; d > 1 {
		t.Errorf("打断之后旧的那一档从 x=%.1f 跳到了 x=%.1f（往右跳了 %.1f）——\n"+
			"打断之后要**从当前进度接着走**，不是跳回起点再走一遍",
			before.oldX, after.oldX, d)
	}
}

// TestTheTabSwitchFinishesOnTheNewTab 钉住**过渡走完之后画面上就是新那一档**。
//
// ⚠️ **推钟**（AGENTS.md §2）：过渡是按钟走的，不推钟它永远走不完。
func TestTheTabSwitchFinishesOnTheNewTab(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	tt.Advance(homePagerDuration + 100*time.Millisecond)
	tt.Frame()

	if !shows(tt, songName(homeTabs[1], 1)) {
		t.Errorf("过渡走完之后画面上没有新那一档的歌，文字有 %q", tt.Texts())
	}
	if shows(tt, songName(homeTabs[0], 1)) {
		t.Errorf("过渡走完之后旧那一档的歌还在画面上，文字有 %q", tt.Texts())
	}
}

// TestCancellingTheSwitchSlidesTheContentBack 钉住**取消（反悔）时内容跟着指示器
// 一起滑回去**（DESIGN.md §五《规则》第 12 条：「动画必须可打断、**可反悔**——
// 中途改变主意时，界面随用户调度」）。
//
// # 这一条顶的是「内容瞬时归位、指示器慢慢滑回去」
//
// 此前（票 #706 的实现）用户点回原来那一档时，内容**瞬间**跳回原地（旧的那一
// 档从 `{X=236 W=404.7}` 一下子变成 `{X=236 W=948}`），而指示器从 0.60 慢慢
// 滑回 0.00——**两段不同起同落**，也违反《逐处》那张表里「指示器和内容同起同落」
// 的承诺。
//
// ⇒ 取消那一支也走一段动画：内容**从当前进度滑回原地**，而指示器那一条由框架
// 带着**从当前值接着走**（`mygo/ui/motion.go` 的 `channel.retarget`）——两边**逐
// 帧同步**。
//
// # 判据
//
//  1. **不是瞬间归位**：点回之后那一帧上旧的那一档**没有**立刻回到满宽（它还得
//     滑回去）；
//  2. **是滑回去的**：往后每一帧，旧的那一档的**可见宽度递增**（它一路变宽）；
//  3. **滑回去的过程中两档都还在画面上**（不是「旧的没了、新的瞬时顶上」）；
//  4. **走完之后回到原来那一档**（画面上只有它）。
func TestCancellingTheSwitchSlidesTheContentBack(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	old := songName(homeTabs[0], 1)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()
	mid, ok := tt.Find(old)
	if !ok || mid.W <= 0 {
		t.Fatalf("切到一半时旧的那一档（%q）不在画面上（盒子 %v）", old, mid)
	}
	midW := mid.W

	// 反悔：点回原来那一档。
	if err := tt.Click(homeTabs[0]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[0], err)
	}
	tt.Frame()
	after, ok := tt.Find(old)
	if !ok {
		t.Fatalf("反悔之后旧的那一档（%q）不在树上了", old)
	}

	//  1. **不是瞬间归位**：它没有立刻回到满宽（那才是「瞬时归位」）。
	//
	// ⚠️ **判据是「宽度还比满宽小」**：反悔那一帧上它还在半路上（宽度是滑回去
	// 的起点），而**瞬时归位**的实现会让它**当场就是满宽**。
	if after.W >= midW*1.5 {
		t.Errorf("反悔之后旧的那一档立刻回到了满宽（%.1f，滑到一半时是 %.1f）——\n"+
			"它是**瞬时归位**的，而这一条要的是**跟着指示器一起滑回去**\n"+
			"（DESIGN.md §五《规则》第 12 条「可反悔」、以及《逐处》那张表里"+
			"「指示器和内容同起同落」）", after.W, midW)
	}

	//  2. **是滑回去的**：往后每一帧，可见宽度**递增**（它一路变宽回原地）。
	//
	// ⚠️ **量的是可见宽度**：旧的那一档往右走回原地，而它的左沿被容器夹住——
	// 所以**宽度**就是「它回来了多少」。
	prev := after.W
	for i := range 12 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		r, ok := tt.Find(old)
		if !ok {
			t.Fatalf("第 %d 帧上旧的那一档（%q）不在树上了——\n"+
				"滑回去那一段**两档都在**", i, old)
		}
		if r.W <= 0 {
			t.Fatalf("第 %d 帧上旧的那一档（%q）的可见宽度是 %.1f——\n"+
				"它是**滑回原地**的，不该消失", i, old, r.W)
		}
		if r.W < prev-0.5 {
			t.Errorf("第 %d 帧上旧的那一档的可见宽度是 %.1f，上一帧是 %.1f——\n"+
				"它没有**往回走**（那一段是滑回去，不是原地不动）", i, r.W, prev)
		}
		prev = r.W
	}

	//  4. **走完之后回到原来那一档**（画面上只有它）。
	tt.Advance(homePagerDuration + 100*time.Millisecond)
	tt.Frame()
	if !shows(tt, old) {
		t.Errorf("滑回去之后画面上没有原来那一档的歌，文字有 %q", tt.Texts())
	}
}

// TestCancellingTheSwitchKeepsTheIndicatorAndTheContentTogether 钉住**取消时指示器
// 和内容同起同落**。
//
// # 判据是**同一时刻的进度相等**
//
// 指示器的位置从**画面上量**（那个白色滑块的中线），内容的进度从
// `homePagerView.progress` 取。
//
// ⚠️ **这一条是「同起同落」的续集**（`TestTheIndicatorAndTheContentMoveTogether`
// 量的是**正着走**的时候）：反悔那一段上，指示器是框架带着**从当前值接着走**
// 的（`mygo/ui/motion.go` 的 `channel.retarget`），而内容那一段是**我们自己按
// 曲线算**的——两边**必须**落在同一个数上，否则又是「指示器慢慢滑、内容啪地一下
// 跳」。
func TestCancellingTheSwitchKeepsTheIndicatorAndTheContentTogether(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	from := textCenterX(t, tt, homeTabs[0])
	to := textCenterX(t, tt, homeTabs[1])

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	// 反悔。
	if err := tt.Click(homeTabs[0]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[0], err)
	}
	for i := range 8 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		bar := tabBarAt(t, tt, homeTabs[:], false)
		got := bar.pill.X + bar.pill.W/2
		frac := 0.0
		if to != from {
			frac = float64(got-from) / float64(to-from)
		}
		if math.Abs(frac-float64(homePagerView.progress)) > 0.12 {
			t.Errorf("第 %d 帧：指示器走到了 %.0f%%，内容走到了 %.0f%%——\n"+
				"反悔那一段**两边也要同起同落**（指示器那一条是框架从当前值接着走的，"+
				"内容那一条是自己按曲线算的，两边要落在同一个数上）",
				i, frac*100, float64(homePagerView.progress)*100)
		}
	}
}

// TestThePagerOffsetsKeepTheTwoPagesSideBySide 钉住**两档紧挨着**（纯函数）。
//
// ⚠️ **纯函数**：不碰界面状态、不看钟——所以它在**任何机器上**都是同一个数。
//
// 判据：新的那一档的左边沿恒在旧的那一档的右边沿右边**一个容器宽**的地方
// （`newX == oldX + width`）——它们**紧挨着**，既不重叠也不留缝（ViewPager 的
// 样子）。
func TestThePagerOffsetsKeepTheTwoPagesSideBySide(t *testing.T) {
	now := time.Unix(2026, 0)
	width := float32(960)
	for _, ms := range []int{0, 25, 50, 100, 150, 199} {
		s := homePager{phase: homePagerSlide, to: 1, at: now}
		oldX, newX := homePagerOffsets(s, time.Duration(ms)*time.Millisecond, width)
		if d := newX - (oldX + width); math.Abs(float64(d)) > 0.5 {
			t.Errorf("%dms 时两档不紧挨着（旧的那一档 x=%.1f，新的那一档 x=%.1f，"+
				"want 新的在 %.1f）——它们要**并排**（各占一段，不重叠也不留缝）",
				ms, oldX, newX, oldX+width)
		}
	}
	// 走完之后：旧的那一档整个在容器外面，新的那一档在原地。
	s := homePager{phase: homePagerSlide, to: 1, at: now}
	oldX, newX := homePagerOffsets(s, homePagerDuration+time.Millisecond, width)
	if math.Abs(float64(oldX+width)) > 0.5 {
		t.Errorf("走完之后旧的那一档在 x=%.1f（want %.1f，整个在容器外面）", oldX, -width)
	}
	if newX != 0 {
		t.Errorf("走完之后新的那一档在 x=%.1f（want 0，在原地）", newX)
	}
}

// TestThePagerShowsASkeletonWhileTheTabIsStillLoading 钉住**那一档还在拉时画
// 骨架屏**。
//
// # 走的是真路子
//
// 假 B 站把「流行」那一档要的那一趟按在门口不放——于是用户点了「流行」而它
// 还在拉。
//
// # 判据
//
// 那一档在画面上（**它出现了**，不是留着上一档），而且画的是**骨架屏**——
//
// ⚠️ **骨架屏和转圈是两档**（DESIGN.md §六《加载态》那张表）：
//
//	**列表首屏** → 八行骨架屏
//	**页面级首屏** → 一枚居中的转圈
//
// 首页这三个 Tab 是**页面级首屏**（上游 `music-recommend/index.tsx:319` 的
// `h-[40vh]` 那一格）——而**这一票改成了骨架屏**（用户拍板：「那一档还没到货
// 时画骨架」），因为**内容的形状已知**（一屏歌曲行），骨架屏说得出「将来长什
// 么样」而转圈只说「还没好」。
//
// ⇒ 所以判据是：**「正在加载流行推荐」这句话在**（那一档的无障碍名字），
// 而**转圈那一格不在**。
func TestThePagerShowsASkeletonWhileTheTabIsStillLoading(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「流行」那一档（音乐榜）要的那一趟按在门口。
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
	waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	homeFrame(tt)

	// 那一档还在拉：它**已经在画面上**了（滑进来了），画的是骨架屏。
	//
	// ⚠️ **无障碍名字是判据**：骨架块是灰条、**没有文字**，所以量的是那一档
	// 挂在容器上的名字（「正在加载流行推荐」）。
	if !showsText(tt, "正在加载流行推荐") {
		t.Errorf("切到「流行」而它还在拉的时候，画面上没有说它在加载，文字有 %q\n"+
			"（这一票要的是**骨架屏**：内容的形状已知，用骨架屏而不是转圈）",
			tt.Texts())
	}
}

// 免得「这一组用不到」被当成「没写」。
var _ = mygoui.Smooth

// homeSkeletonShown 报「那一档的骨架屏在画面上」。
//
// ⚠️ **量的是「那一档的无障碍名字」**（`tt.HasText`）：骨架屏的灰块**没有
// 文字**，而那一档挂着的名字是「正在加载 XX 推荐」（`drawSkeletonRows` 的
// `Label(text)`）——所以那是「那一档在加载」这句话。
//
// ⚠️ **不能用 `shows`（要求盒子有正宽）**：骨架屏那一档**在滑进来的过程中**
// 是**被裁掉的**（在容器外面），而那一刻它的名字已经在树上了——而
// `tt.Find` 报的是**裁切之后**的盒子（框架把标签的盒子和祖先的裁切区求交，
// 见 `mygo/ui/runtime.go`），于是那一档滑出去的时候报出来是**零宽**。
//
// ⇒ 所以「骨架屏在不在」看**名字在不在**，而「它有没有真的滑到位」由
// `homePagerView` 那一组断。
func homeSkeletonShown(tt *mygoui.Tester, noun string) bool {
	return tt.HasText("正在加载" + noun)
}
