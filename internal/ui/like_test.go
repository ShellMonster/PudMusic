// 封面上的长按：2.5 秒一键三连、500 毫秒起进度环、已点赞时长按被完全禁用，
// 以及**长按之后补发的那一次 click 必须吞掉**。
//
// 走的是整条链：真的假 B 站（`biliFixture`）→ `App` 那一摊缓存 → 封面 → 长按 →
// 请求真的发出去。所以这些断的是「用户按得住的那一下」，不是「某个函数被调到了」。
//
// ⚠️ **全程假时钟**：2.5 秒和 500 毫秒都是 `s.clock` 上算的（`advanceLike` 把
// `s.clock` 和 Tester 自己那只一起推）。真等就是一条测试两秒半。
package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// ---- 长按与进度环 ----

// TestTheRingStartsAfterHalfASecond 钉住「500 毫秒开始显示进度环」这个数。
//
// ⚠️ 断的是**画面**：环画出来了没有。数一个「点赞进度」那个元素在不在是不够的
// ——那一个只要被建出来就在（而它建不建和环画不画是两件事）。
func TestTheRingStartsAfterHalfASecond(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	// 499 毫秒：还没有环。封面外面那一圈一个像素都不许多出来。
	advanceLike(e, biuLikeRingDelayMS*time.Millisecond-time.Millisecond)
	if ringBuilt(e.tt) {
		t.Errorf("按住 %d 毫秒就开始画环了，Biu 是 %d 毫秒才开始画",
			biuLikeRingDelayMS-1, biuLikeRingDelayMS)
	}
	if n := ringPainted(e.tt, cover, before); n != 0 {
		t.Errorf("按住 %d 毫秒封面外面就多了 %d 个像素", biuLikeRingDelayMS-1, n)
	}

	// 再过 2 毫秒：环出来了（还差得远，走不完一圈）。
	advanceLike(e, 2*time.Millisecond)
	if !ringBuilt(e.tt) {
		t.Fatalf("按住 %d 毫秒了环还没出来，文字有 %q", biuLikeRingDelayMS, e.tt.Texts())
	}
	if n := ringPainted(e.tt, cover, before); n == 0 {
		t.Error("盒建出来了，封面外面却一个像素都没多 —— 环没真的画上去")
	}
	if e.f.triplePostCount() != 0 {
		t.Errorf("按住半秒就三连了（%d 次）", e.f.triplePostCount())
	}
}

// TestTheRingIsFullJustAsTheTripleGoesOut 钉住「环转满 = 三连触发」。
//
// Biu 那三个数是互相咬着的：环从 500ms 画起、2s 转满，而 500+2000 = 2500。
// 把环的时长单独改了就对不上——环转满那一刻用户以为「成了」，而请求还没发出去。
func TestTheRingIsFullJustAsTheTripleGoesOut(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	// 2499 毫秒：环几乎满了，**但还没发**。
	advanceLike(e, (biuLikeLongPressMS-1)*time.Millisecond)
	if e.f.triplePostCount() != 0 {
		t.Fatalf("按住 %d 毫秒就三连了，Biu 是 %d 毫秒",
			biuLikeLongPressMS-1, biuLikeLongPressMS)
	}
	if !ringBuilt(e.tt) {
		t.Fatal("快到点了环却不见了")
	}
	full := ringPainted(e.tt, cover, before)

	// 再过 2 毫秒：满了，而且**同时**三连已经发出去了。
	advanceLike(e, 2*time.Millisecond)
	settleSearch(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	e.tt.Frame()
	if ringBuilt(e.tt) {
		t.Error("三连都发出去了环还在 —— Biu 那边是同一句 setIsProgressing(false)")
	}
	// 而 2499 毫秒那一刻环真的转满了：转满那一刻用户以为「成了」，而那时候请求
	// 还**没**发出去（上面已经断过了）。
	if full == 0 {
		t.Error("快到点时封面外面一个像素都没多 —— 环一直没画上去")
	}
}

// TestTheRingGrowsWhileTheCoverIsHeld 钉住「环是跟着手指走的」，不是一步到位。
//
// 把进度写成常数 1 的话上面那两条照样全绿，而用户看到的是「按下去立刻满环」，
// 然后干等两秒半。
func TestTheRingGrowsWhileTheCoverIsHeld(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	advanceLike(e, (biuLikeRingDelayMS+biuLikeRingSpanMS/4)*time.Millisecond)
	quarter := ringPainted(e.tt, cover, before)

	advanceLike(e, biuLikeRingSpanMS/4*time.Millisecond)
	half := ringPainted(e.tt, cover, before)

	if quarter <= 0 || half <= quarter {
		t.Errorf("环画了 %d 个像素然后 %d 个——它没有随着按住的时间变长", quarter, half)
	}
}

// TestLettingGoEarlyDoesNothing 钉住「短按不是三连」。
//
// 而那一次短按**照旧是普通点击**（封面照旧开全屏）：Biu 那边短按赞键是「赞一下」，
// 我们这边短按封面是「开全屏」，两边都不该被长按那一套吞掉。
func TestLettingGoEarlyDoesNothing(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)

	advanceLike(e, biuLikeRingDelayMS*time.Millisecond+100*time.Millisecond)
	if !ringBuilt(e.tt) {
		t.Fatal("按住 600 毫秒环还没出来，前一条已经量过这一点了")
	}
	if ringPainted(e.tt, cover, before) == 0 {
		t.Fatal("按住 600 毫秒环建出来了却没画上去")
	}

	// 松手。⚠️ **这一松是一次普通点击**，封面照旧开全屏（Biu 那边短按赞键是赞一下，
	// 我们这边短按封面是开全屏）——所以下面**不能**拿画面比了：满屏一开整窗都变了。
	pt := coverPoint2(t, e.tt)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("按住 %d 毫秒就三连了（%d 次），Biu 是 %d 毫秒",
			biuLikeRingDelayMS+100, got, biuLikeLongPressMS)
	}
	if ringBuilt(e.tt) {
		t.Error("手已经松开了环还在")
	}
}

// TestALongPressNeverOpensTheFullScreen 钉住那一条真机上踩过的坑。
//
// 🔴 **长按之后浏览器补发的那一次 click 必须吞掉**。Biu 那边漏了它，一次长按 =
// 三连 + 一次多余的赞；我们这边长按目标是封面，于是漏了它 = **凭空弹出全屏
// 播放器**——进度环刚转满，界面就换成满屏那一屏，而用户以为自己只是赞了一下。
//
// 这一条量的是**画面**：松手之后「关闭全屏」那个键必须不在。
func TestALongPressNeverOpensTheFullScreen(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdCover(t, e)

	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(coverPoint(t, e.tt))
	e.tt.Frame()

	settleSearch(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	if e.tt.HasText("关闭全屏") {
		t.Error("长按三连完，全屏播放器自己弹出来了 —— 补发的那一次 click 没吞掉")
	}
	if e.s.full.open {
		t.Error("外壳上记着全屏开着")
	}
	// 而**下一次**普通点击必须照旧开全屏：吞标记是一次性的，
	// 一直吞下去的话封面就再也点不开了。
	e.tt.ClickAt(coverPoint(t, e.tt))
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("吞标记把后面那一次正常点击也吞了，文字有 %q", e.tt.Texts())
	}
}

// TestAShortPressStillOpensTheFullScreen 是上一条的反面：确认吞的是**那一次**。
//
// 没有这一条的话，把 `swallowClick` 写成「永远吞」上面那条照样全绿——而界面上
// 封面就再也点不开了（一个看着好好的键，什么也不发生）。
func TestAShortPressStillOpensTheFullScreen(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	x, y := coverPoint(t, e.tt)

	e.tt.ClickAt(x, y)
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("点一下封面全屏没开，文字有 %q", e.tt.Texts())
	}
}

// TestTheLongPressIsOffOnceTheSongIsLiked 钉住「已点赞时长按被完全禁用」。
//
// 照抄 Biu 的 `handleMouseDown`：`if (isThumb) return`——**整个不进状态机**。
// 于是既没有进度环（用户不会以为还能长按），也没有那一次三连（那一次要花两枚
// 币，而 B 站压根不会再给）。
//
// ⚠️ 顺带断「那一次 click 仍然是普通的 click」：禁用长按**不是**禁用封面。
func TestTheLongPressIsOffOnceTheSongIsLiked(t *testing.T) {
	e := newLikeEnv(t, true, false, false)
	cover, before := holdCover(t, e)

	// 按住三秒（比三连那一把还长半秒）：没有三连，也没有环。
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+500*time.Millisecond)

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("已经赞过了还发出去 %d 次三连 —— Biu 那边是整个不进状态机", got)
	}
	if ringBuilt(e.tt) {
		t.Error("已经赞过了还建进度环 —— 用户会以为还能长按")
	}
	if n := ringPainted(e.tt, cover, before); n != 0 {
		t.Errorf("已经赞过了封面外面还多了 %d 个像素", n)
	}

	// ⚠️ **松手那一下仍然是普通点击**：禁用长按不是禁用封面。
	pt := coverPoint2(t, e.tt)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("禁用长按把普通点击也禁用了 —— 封面照旧该开全屏，文字有 %q", e.tt.Texts())
	}
}

// TestTheLongPressIsOffUntilTheServerAnswers 钉住「没问就不许长按」。
//
// 换歌之后头半秒界面上「没赞」是我们**猜**的，而猜错的方向是「其实赞过了还让
// 长按」——那一次三连白花两枚币。宁可这半秒按不动。
func TestTheLongPressIsOffUntilTheServerAnswers(t *testing.T) {
	e := newLikeEnvSlowRelation(t)
	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+500*time.Millisecond)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("还没问「赞没赞」就发了 %d 次三连 —— 猜错的方向是白花两枚币", got)
	}
	e.f.releasePath(pathArchiveRelation)
}

// ---- 满屏上那张封面 ----

// TestTheFullScreenCoverTakesTheLongPressToo 钉住「接进播放条**与**全屏播放器」。
//
// 满屏上那一张是**另一个元素**（BMG-34 的共享元素转场让它有自己的框），所以
// 「两边都接上了」不是把同一个函数调两次就成立——满屏那张外面套了一层新的 Box，
// 长按要落在**它**上面。
func TestTheFullScreenCoverTakesTheLongPressToo(t *testing.T) {
	e := newLikeEnv(t, false, false, true)

	r := coverAt(t, e.tt)
	e.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Fatalf("满屏没开，文字有 %q", e.tt.Texts())
	}
	e.tt.Frame()

	// 满屏上那张封面：找「点赞进度」那个元素是不行的（它要按住 500ms 才建），
	// 找的是**画着**的那一张——共享元素转场结束之后它落在满屏中央。
	big := fullCoverRect(t, e.tt)
	e.tt.Press(big.X+big.W/2, big.Y+big.H/2)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)

	settleSearch(t, e.tt, "满屏上的长按三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
}

// ---- 小工具 ----

// ringBuilt 问「那个叫「点赞进度」的盒子建出来没有」。
//
// ⚠️ **这不等于「环画出来了」**（`TestTheRingIsReallyPainted` 量的是那个），
// 但它对「500ms / 2500ms 这两个数」是够用的：`viewLikeRing` 在 `progress <= 0`
// 时**连盒子都不建**（它第一句就 return），所以「盒子在」严格蕴含「这一帧要画环」。
// 拿它去量时间是准的。
func ringBuilt(tt *mygoui.Tester) bool {
	_, ok := tt.Find("点赞进度")
	return ok
}

// ringPainted 是「封面**外面那一圈**里比基线多出来多少像素」。
//
// ⚠️ **只数封面外面，不数封面里面，也不数整张画面**。两样都量不得：
//
//	封面里面  按下去会多出悬停浮层（半透明黑 + ⤢），松手它又消失——
//	          整块封面变一次就是四千多个像素，而那和环一点关系没有
//	整张画面  播放条上别的控件会跟着抖（抗锯齿、焦点圈）
//
// 环是画在封面**外面**的（Biu 的 `inset: -2px`），所以外面这一圈是干净的：
// 没有环的时候数出来**正好是 0**。
//
// ⚠️ 一个刚开始的环只有 4~5 个像素（进度 0.1% 是 1 段，一段 2×2），所以
// 「画出来了吗」判的是 `> 0` 而不是「够不够多」。
func ringPainted(tt *mygoui.Tester, cover mygoui.Rect, before *image.RGBA) int {
	now := tt.Image()
	band := inflate(cover, likeRingWidth*2+2)
	n := 0
	for y := max(int(band.Y), now.Rect.Min.Y); y < min(int(band.Y+band.H), now.Rect.Max.Y); y++ {
		for x := max(int(band.X), now.Rect.Min.X); x < min(int(band.X+band.W), now.Rect.Max.X); x++ {
			// 封面里面那一块不算：那儿归悬停浮层管。
			if x >= int(cover.X) && x < int(cover.X+cover.W) &&
				y >= int(cover.Y) && y < int(cover.Y+cover.H) {
				continue
			}
			if !samePixel(before.RGBAAt(x, y), now.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// inflate 是一个框往外长 d。
func inflate(r mygoui.Rect, d float32) mygoui.Rect {
	return mygoui.Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}

// diffIn 是 box 那个框里有多少个像素不一样。
func diffIn(box mygoui.Rect, a, b *image.RGBA) int {
	img := a
	x0 := max(int(box.X), img.Rect.Min.X)
	y0 := max(int(box.Y), img.Rect.Min.Y)
	x1 := min(int(box.X+box.W), img.Rect.Max.X)
	y1 := min(int(box.Y+box.H), img.Rect.Max.Y)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !samePixel(a.RGBAAt(x, y), b.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// samePixel 留一点余量（光栅化会有舍入）。
func samePixel(a, b color.RGBA) bool {
	const slack = 8
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= slack && d(a.G, b.G) <= slack && d(a.B, b.B) <= slack
}

// fullCoverRect 是满屏上那张封面在画面上的框。
//
// 它是共享元素转场的**终点**，所以按满屏那几个数算：宽 `min(宽*0.7, 高*0.48)`
// 夹在 260~520，高是宽的 0.75，居中。找元素不行——满屏上那个「当前封面」和
// 播放条上那个同名，而播放条这时候压根没建。
func fullCoverRect(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	winW, winH := float32(likeWinW), float32(likeWinH)
	w := min(winW*0.7, winH*0.48)
	w = min(max(w, 260), 520)
	h := w * 0.75
	return mygoui.Rect{X: (winW - w) / 2, Y: (winH - h) / 2, W: w, H: h}
}
