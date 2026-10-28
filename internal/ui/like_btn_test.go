// 播放条上那枚「赞」键自己的长按：按住 2.5 秒一键三连（键上像 B 站那样抖）、
// 已点赞时长按整个禁用、长按之后松手补发的那一次 click 必须吞掉。
//
// 和 like_test.go（封面那边）同一套脚手架：假的 B 站 → App → 界面 → 真的 POST，
// 全程假时钟。⚠️ 赞键和播放条封面**同帧常驻**——所以这批用例同时就是
// 「两个长按目标互不干扰」的回归：封面那一台状态机每帧都看到「没按」，
// 赞键这一台照样要能按满 2.5 秒。
package ui

import (
	"image"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// starAt 是播放条上那枚赞键在画面上的框。label 是「赞」或「已赞」。
func starAt(t *testing.T, tt *mygoui.Tester, label string) mygoui.Rect {
	t.Helper()
	if r, ok := tt.Find(label); ok {
		return r
	}
	t.Fatalf("画面上找不到「%s」那一键，文字有 %q", label, tt.Texts())
	return mygoui.Rect{}
}

// holdStar 按住赞键**不松手**。返回它的框和按下那一刻的画面（基线）。
//
// ⚠️ 一毫秒都不推进（同 holdCover）：推进一下就等于按下时刻往前挪，
// 2500 / 500 两条断言量到的就不是那两个数了。
func holdStar(t *testing.T, e *likeEnv, label string) (mygoui.Rect, *image.RGBA) {
	t.Helper()
	r := starAt(t, e.tt, label)
	e.tt.Press(r.X+r.W/2, r.Y+r.H/2)
	return r, cloneImage(e.tt.Image())
}

// TestHoldingTheLikeButtonTriplesTheSong 钉住「赞键自己就是长按三连的目标」。
//
// 照 B 站的样子：按住赞键，键上抖着，2.5 秒一到三连发出去——不用挪去封面上
// 长按。⚠️ 封面那台状态机这一路每帧都被喂「没按」，三连照样要到点才发：
// 两台状态机各记各的，谁也不许把谁清零。
func TestHoldingTheLikeButtonTriplesTheSong(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdStar(t, e, "赞")

	// 2499 毫秒：还没有三连。
	advanceLike(e, (biuLikeLongPressMS-1)*time.Millisecond)
	if got := e.f.triplePostCount(); got != 0 {
		t.Fatalf("按住 %d 毫秒就三连了，B 站是 %d 毫秒",
			biuLikeLongPressMS-1, biuLikeLongPressMS)
	}

	// 再过 2 毫秒：三连发出去。
	advanceLike(e, 2*time.Millisecond)
	waitFrames(t, e.tt, "赞键的长按三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
}

// TestALongPressOnTheLikeButtonNeverTogglesTheLike 钉住「长按之后松手补发的那一次
// click 必须吞掉」。
//
// 赞键的 click 本来是「点赞 / 取消赞」——漏吞的话一次长按 = 三连 + 一次多余的
// 取消赞（Biu 那边漏了它就是「赞两次」，viewCover 那边同一个坑）。
// 而吞标记是一次性的：接下来的第一次普通点击必须照旧是点赞。
func TestALongPressOnTheLikeButtonNeverTogglesTheLike(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	btn, _ := holdStar(t, e, "赞")

	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(btn.X+btn.W/2, btn.Y+btn.H/2)
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })

	if got := e.f.likePostsCount(); got != 0 {
		t.Errorf("长按三连多发了 %d 次赞 —— 松手补发的 click 没吞掉", got)
	}

	// 用户接下来的第一次点击照旧是他自己的：吞标记不能一直吞下去。
	// ⚠️ 三连已经把这一首**赞上了**（B 站回包 like=true），所以这一次点击
	// 是「取消赞」（up=2）——断的是「点击照常工作」，不是「还赞一次」。
	e.tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	waitFrames(t, e.tt, "赞发出去了", func() bool { return e.f.likePostsCount() > 0 })
	if got := e.f.lastLikeUp(); got != "2" {
		t.Errorf("长按之后第一次点击发出去的 up 是 %q，want 2（三连赞上了，这一次是取消赞）", got)
	}
}

// TestTheButtonLongPressIsOffOnceTheSongIsLiked 钉住「已点赞时赞键的长按整个禁用」。
//
// 和封面那一条（TestTheLongPressIsOffOnceTheSongIsLiked）同一口径：不进状态机、
// 不抖、不发三连；而松手那一下照旧是普通点击（取消赞）——禁用长按不是禁用按钮。
func TestTheButtonLongPressIsOffOnceTheSongIsLiked(t *testing.T) {
	e := newLikeEnv(t, true, false, false)
	btn, _ := holdStar(t, e, "已赞")

	// 等按下那一弹落定再取基线，往后按多久画面都不该动。
	advanceLike(e, 300*time.Millisecond)
	e.tt.Frame()
	base := cloneImage(e.tt.Image())

	advanceLike(e, (biuLikeLongPressMS+500-300)*time.Millisecond)
	e.tt.Frame()

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("已经赞过了还发出去 %d 次三连 —— 长按该整个不进状态机", got)
	}
	if n := diffIn(btn, base, e.tt.Image()); n != 0 {
		t.Errorf("已经赞过了按住还动了 %d 个像素 —— 长按关着就不该抖", n)
	}

	e.tt.Release(btn.X+btn.W/2, btn.Y+btn.H/2)
	waitFrames(t, e.tt, "取消赞发出去了", func() bool { return e.f.likePostsCount() > 0 })
	if got := e.f.lastLikeUp(); got != "2" {
		t.Errorf("发出去的 up 是 %q，want 2（2 是取消赞）", got)
	}
}

// TestTheLikeButtonShakesWhileHeld 钉住「B 站那种抖」的两端：头 500 毫秒一动不动
// （那是点 / 按消歧的一段），之后抖起来；触发三连那一停，键回到原位。
//
// 量的是赞键框**里面**的像素：tooltip 和别处的动静都在框外，混不进来。
func TestTheLikeButtonShakesWhileHeld(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	btn, _ := holdStar(t, e, "赞")

	// 按下那一弹落定之后取基线。
	advanceLike(e, 300*time.Millisecond)
	e.tt.Frame()
	base := cloneImage(e.tt.Image())

	// 300 → 400 毫秒：还没到 500，键该一动不动。
	advanceLike(e, 100*time.Millisecond)
	e.tt.Frame()
	if n := diffIn(btn, base, e.tt.Image()); n != 0 {
		t.Fatalf("按住 %d 毫秒键上就动了 %d 个像素 —— 500 毫秒之前不该有动静",
			biuLikeRingDelayMS-100, n)
	}

	// 500 → 600 → 645 毫秒：抖起来了，隔半个周期（45ms）的两帧必须不一样。
	// ⚠️ 采样差要跨换向点：隔 100ms 的话相位只差 40°，图标偏移差 0.18px——
	// 抗锯齿的余量能不能显出来全看光栅化心情；隔半周期最大差 3px，断得实。
	advanceLike(e, 200*time.Millisecond) // 400 + 200 = 600 毫秒
	e.tt.Frame()
	mid := cloneImage(e.tt.Image())
	advanceLike(e, 45*time.Millisecond)
	e.tt.Frame()
	if n := diffIn(btn, mid, e.tt.Image()); n == 0 {
		t.Error("按住半秒之后键上的图形一动不动 —— 抖动没接上")
	}

	// 触发之后抖要停：把钟推过 2500ms（⚠️ waitFrames 不推钟，触发要靠
	// advanceLike 把「按了多久」喂够），等三连发出去、B 站的答案落地、
	// 那一弹走完，再隔 100 毫秒的两帧又该一模一样（还在按着，也不许有东西动）。
	advanceLike(e, biuLikeLongPressMS*time.Millisecond-700*time.Millisecond+200*time.Millisecond)
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	waitFrames(t, e.tt, "赞的答案落地了", func() bool { return e.s.app.LikeState().Liked })
	advanceLike(e, 400*time.Millisecond)
	e.tt.Frame()
	still1 := cloneImage(e.tt.Image())
	advanceLike(e, 100*time.Millisecond)
	e.tt.Frame()
	if n := diffIn(btn, still1, e.tt.Image()); n != 0 {
		t.Errorf("三连都发过了键还在动（%d 个像素）—— 抖没停", n)
	}
}

// TestTheButtonLongPressKeepsAskingForFrames 钉住「没环的目标整段按住都要自己要帧」。
//
// 赞键没有环、没有 Draw 回调：抖动的偏移和「到没到 2500ms」全靠**重建帧**算。
// 要是照有环那一路在 500ms 后停手（环出来之后有局部重绘自驱，赞键没有），暂停时
// 没有 tick、指针不动没有事件——长按冻在半路：抖停了、三连永远发不出去。
//
// ⚠️ 这一条量的是**帧经济**：推钟不强制出帧。Tester 的 `Frame()` 会无条件出
// 一帧，`waitFrames` 也一样——别的用例里那一笔会把「冻住」盖住，只有数重建
// 次数才量得出来。
func TestTheButtonLongPressKeepsAskingForFrames(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdStar(t, e, "赞")

	// 推到 700ms（抖动已经开始、进度 > 0 的那一段），数接下来 100ms 里
	// 播放条建了几次帧。上限给 2 是给后台 tick 的：窗口里真时的唤醒撞进来
	// 算它一帧，但抖动的帧是**每一刷新一帧**，量级对不上。
	advanceLike(e, 700*time.Millisecond)
	e.s.builds = buildTally{}
	advanceLike(e, 100*time.Millisecond)
	if got := e.s.builds.playbar; got < 2 {
		t.Errorf("按住 700–800 毫秒之间播放条只建了 %d 次帧 —— 没环的目标漏要帧，"+
			"暂停时长按会冻在半路（抖停了、三连发不出去）", got)
	}

	// 要帧要得到位，三连就还是到点就发（这一段依旧不强制出帧）。
	advanceLike(e, biuLikeLongPressMS*time.Millisecond-800*time.Millisecond+200*time.Millisecond)
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
}

// TestTheButtonLongPressWaitsForTheServerAnswer 钉住「没问就不许长按」——
// 和封面那一条同一口径：猜错的方向是白花两枚币，宁可按不动。
func TestTheButtonLongPressWaitsForTheServerAnswer(t *testing.T) {
	e := newLikeEnvSlowRelation(t)
	btn, _ := holdStar(t, e, "赞")

	advanceLike(e, biuLikeLongPressMS*time.Millisecond+500*time.Millisecond)
	e.tt.Release(btn.X+btn.W/2, btn.Y+btn.H/2)
	e.tt.Frame()

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("还没问「赞没赞」就发了 %d 次三连 —— 猜错的方向是白花两枚币", got)
	}
	e.f.releasePath(pathArchiveRelation)
}
