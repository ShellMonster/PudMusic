package ui

// 切 Tab **横向滑动**的 E2E（票 #706）。
//
// 这一组走的是**真路子**：真外壳、真 Tester、真点击、真推钟。
//
// # 判据是**画面上的几何**，不是状态字段
//
// 这一票动的是「画面上是什么」，而「状态字段对了」不等于「画面对了」。
//
// ⚠️ **`tt.Find` 报的是**裁切之后**的盒子**：框架把标签的盒子和祖先的裁切区
// 求交（`mygo/ui/runtime.go` 的 `commitElement`）——所以
//
//   - **滑出去的那一档**（往左走）的盒子**左沿被夹在容器边上**，而它的**可见
//     宽度越来越窄**（最后是 0）；
//   - **滑进来的那一档**（从右边来）的盒子**左沿从容器右边往左挪**。
//
// ⇒ 那两个数（**可见宽度**和**左沿**）就是这一票真正动的东西，而且它们**在画
// 面上量得到**。
//
// ⚠️ **不用「精确比色」那种像素判据**：那道边线是**半透明混色**画上去的（实
// 测画面上的值和主题的 `Border` 不相等），精确比色会扫不到（队列抽屉那族的测
// 试在基线上就是这么红的）。

import (
	"math"
	"testing"
	"time"
)

// TestTheContentSlidesSidewaysOnScreen 钉住**画面上真的横向滑了**。
//
// # 四件事
//
//  1. **新的那一档从右边进来**：它的**左沿**在动画中途比刚开始时**更靠左**；
//  2. **旧的那一档往左走**：它的**可见宽度**在动画中途比刚开始时**更窄**；
//  3. **旧的那一档还在画面上**（它滑出去，不是一下子没了）；
//  4. **走完之后**：新的那一档**在原地**，而旧的**不在画面上**了。
//
// ⚠️ **推钟**（AGENTS.md §2）：过渡是按钟走的，不推钟它永远走不完。
func TestTheContentSlidesSidewaysOnScreen(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	oldSong := songName(homeTabs[0], 1)
	newSong := songName(homeTabs[1], 1)

	// 切之前：新的那一档**不在画面上**（它没建）。
	if r, ok := tt.Find(newSong); ok && r.W > 0 {
		t.Fatalf("切之前新的那一档（%q）已经在画面上了（盒子 %v）", newSong, r)
	}

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	// 动画的第一帧：新的那一档**已经在树上了**，但它在容器外面（看不见）。
	tt.Frame()
	start, ok := tt.Find(newSong)
	if !ok {
		t.Fatalf("切 Tab 之后的第一帧上新的那一档（%q）不在树上了", newSong)
	}
	oldStart, ok := tt.Find(oldSong)
	if !ok || oldStart.W <= 0 {
		t.Fatalf("切 Tab 之后的第一帧上旧的那一档（%q）不在画面上了（盒子 %v）", oldSong, oldStart)
	}

	// 推到动画的中途（旧的那一档已经走了一半，新的那一档刚进来一半）。
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	mid, ok := tt.Find(newSong)
	if !ok {
		t.Fatalf("动画中途新的那一档（%q）不在树上了", newSong)
	}
	oldMid, ok := tt.Find(oldSong)
	if !ok {
		t.Fatalf("动画中途旧的那一档（%q）不在树上了——它应该**往左走**，"+
			"不是一下子没了", oldSong)
	}

	//  1. 新的那一档**从右边进来**：它的左沿比第一帧更靠左。
	//
	// ⚠️ **量的是「可见盒子的左沿」**：它被容器裁过，所以滑进来的过程里它一
	// 步一步往左挪。
	if mid.X >= start.X {
		t.Errorf("动画中途新的那一档的左沿在 x=%.1f，第一帧时在 x=%.1f——\n"+
			"它没有**从右边进来**（横滑是新的那一档往左走）", mid.X, start.X)
	}
	//  2. 旧的那一档**往左走**：它的可见宽度比第一帧更窄。
	if oldMid.W >= oldStart.W {
		t.Errorf("动画中途旧的那一档的可见宽度是 %.1f，第一帧时是 %.1f——\n"+
			"它没有**往左走**（左沿被容器夹住了，所以看的是**宽度**：往左走的时\n"+
			"候它的一部分滑出了容器，可见的那一段就窄了）",
			oldMid.W, oldStart.W)
	}
	//  3. 而它**还在画面上**（不是一下子没了）。
	if oldMid.W <= 0 {
		t.Errorf("动画中途旧的那一档（%q）的可见宽度是 %.1f——它应该**还在画面\n"+
			"上**（横滑是它滑出去，不是一下子没了）", oldSong, oldMid.W)
	}

	//  4. 走完之后：新的那一档在原地，旧的没了。
	tt.Advance(homePagerDuration + 100*time.Millisecond)
	tt.Frame()
	end, ok := tt.Find(newSong)
	if !ok || end.W <= 0 {
		t.Errorf("过渡走完之后新的那一档（%q）不在画面上（盒子 %v）", newSong, end)
	}
	if r, ok := tt.Find(oldSong); ok && r.W > 0 {
		t.Errorf("过渡走完之后旧的那一档（%q）还在画面上（盒子 %v）", oldSong, r)
	}
}

// TestTheIndicatorAndTheContentMoveTogether 钉住**指示器和内容同起同落**。
//
// # 判据
//
// **指示器**（Tab 那一排的白色滑块）的位置和**内容**的横向偏移，在**同一个
// 时刻**的**进度相等**。
//
// ⚠️ **指示器的位置从画面上量**（滑块是画出来的，有颜色——扫法见 `tabs_test.go`
// 的 `tabBarAt`），而**内容的进度**从 `homePagerView.progress` 取（它和指示
// 器走的是**同一套参数**，所以两者在任何时刻的进度相等）。
//
// ⇒ 判据是「指示器此刻走到了目标的几分之几」≈「内容此刻走到了目标的几分之
// 几」。
func TestTheIndicatorAndTheContentMoveTogether(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	// 指示器的起点和终点：那两个 Tab 的文字中线（滑块停在那上面）。
	from := textCenterX(t, tt, homeTabs[0])
	to := textCenterX(t, tt, homeTabs[1])

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	// 指示器此刻在哪儿（画面上量那个白色滑块的中线）。
	bar := tabBarAt(t, tt, homeTabs[:], false)
	got := bar.pill.X + bar.pill.W/2
	// 它走了目标的几分之几。
	frac := 0.0
	if to != from {
		frac = float64(got-from) / float64(to-from)
	}
	// 内容那一侧此刻的进度。
	want := float64(homePagerView.progress)

	if math.Abs(frac-want) > 0.12 {
		t.Errorf("指示器走到了 %.0f%%，内容走到了 %.0f%%——两者要**同起同落**\n"+
			"（指示器那一条和内容那一条走的是**同一套参数**：同一曲线、同一时长）",
			frac*100, want*100)
	}
}
