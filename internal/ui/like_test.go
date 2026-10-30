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
	"strconv"
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
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
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

// TestTheRingHugsTheCoverBorder 钉住「环贴着封面外边框」。
//
// 旧的那版是外接椭圆：它只在中边那四个点摸到封面的边，四个角上**一辈子没有
// 像素**——在方形封面的角上，椭圆整段划进封面里头去。贴边的这一版跟着封面的
// 圆角矩形走：环走到右上角（约 750 毫秒）时，封面圆角外那一小块凹角必须亮起来。
//
// 凹角 = 封面外框以内、圆角以外的那一小块。悬停浮层的圆角把它让了出来，
// 基线里它是干净的。
func TestTheRingHugsTheCoverBorder(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	cover, before := holdCover(t, e)
	corner := mygoui.Rect{X: cover.X + cover.W - 4, Y: cover.Y, W: 4, H: 4}

	// 600 毫秒：环还在顶边右半上，没到角。
	advanceLike(e, biuLikeRingDelayMS*time.Millisecond+100*time.Millisecond)
	e.tt.Frame()
	if n := diffIn(corner, before, e.tt.Image()); n != 0 {
		t.Errorf("才按住 %d 毫秒角上就有 %d 个像素 —— 环走得太快",
			biuLikeRingDelayMS+100, n)
	}

	// 800 毫秒：环过了右上角。
	advanceLike(e, 200*time.Millisecond)
	e.tt.Frame()
	if n := diffIn(corner, before, e.tt.Image()); n == 0 {
		t.Error("环走到右上角了，圆角外的凹角却一个像素都没有 —— 它还是椭圆，不是贴着外边框")
	}
}

// TestLettingGoEarlyDoesNothing 钉住「没到 2.5 秒就不是三连」。
//
// ⚠️ **它不是「那一下还是普通点击」**：600 毫秒已经过了进度环的门槛（500 毫秒），
// 那一按**算长按**，补发的那一次 click 要被吞掉——封面不会开全屏
// （钉住它的是 `TestACoverHoldPastTheRingSwallowsItsClick`）。真正的短按（不到
// 500 毫秒）才照旧开全屏（`TestAQuickReleaseOnTheCoverStillOpensTheFullScreen`）。
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

	// 松手。⚠️ 环已经出来过，所以这一松是**长按的松手**（不是点击）——
	// 补发的那一次 click 会被吞掉，下面照旧可以拿画面比。
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

// TestACoverHoldPastTheRingSwallowsItsClick 钉住「环一出来，松手就不再是点击」。
//
// 🔴 用户报的「长按封面取消了还进播放器」：按住 600 / 700 毫秒（环已经画出来，
// 但还没到 2.5 秒的三连）再松手，框架会补发一次 click——漏吞就是**凭空弹出
// 全屏播放器**。用户手上只是按了一会儿又松开，预期是留在当前。
//
// ⚠️ **门槛是进度环那一个（500ms），不是三连那一个（2500ms）**：环一出来就说明
// 用户进入长按语义了（见 like.go 的 `likeGesture.engaged`）。
func TestACoverHoldPastTheRingSwallowsItsClick(t *testing.T) {
	for _, ms := range []int{biuLikeRingDelayMS + 100, biuLikeRingDelayMS + 200} {
		t.Run(strconv.Itoa(ms), func(t *testing.T) {
			e := newLikeEnv(t, false, false, false)
			holdCover(t, e)
			pt := coverPoint2(t, e.tt)

			advanceLike(e, time.Duration(ms)*time.Millisecond)
			e.tt.Release(pt[0], pt[1])
			e.tt.Frame()

			if e.s.full.open || e.tt.HasText("关闭全屏") {
				t.Errorf("封面按住 %d 毫秒松手，全屏播放器自己开出来了 —— "+
					"环一出来就算长按，补发的那一次 click 没吞掉", ms)
			}
			if got := e.f.triplePostCount(); got != 0 {
				t.Errorf("按住 %d 毫秒就三连了（%d 次），Biu 是 %d 毫秒",
					ms, got, biuLikeLongPressMS)
			}
			// ⚠️ **吞的是一次**：松手那一帧之后标记必须已经用掉，否则用户接下来
			// 点的第一下封面会被吃掉（「要点第二下才开」）。
			if e.s.like.engaged {
				t.Error("松手之后「这一按算长按」还留着 —— 它会吃掉下一次点击")
			}
			e.tt.ClickAt(pt[0], pt[1])
			if !e.s.full.open || !e.tt.HasText("关闭全屏") {
				t.Error("中途松手之后第一次点封面没开全屏 —— 吞标记用掉之后还在吞")
			}
		})
	}
}

// TestACoverHoldReleasedOffTheCoverKeepsTheNextKeyPress 钉住「松手落在封面外，
// 那一次长按就到松手为止」。
//
// 🔴 评审在票 #681 上跑出来的形态（`engaged` 的寿命问题）：
//
//	按住封面 700ms（环出来了）→ 指针移出封面 → 松手
//
// 补发的那一次 click **不落在封面上**（指针在封面外松手，框架根本不记这一下
// click），于是没有人问 `swallowClick`，`engaged` 就悬着。而键盘激活
// （Tab 之后按回车、或读屏软件的 press 动作）**没有指针按下那一帧**——
// `trackLike` 开头那个「下一次按下就清」的清零点永远进不去，于是第一条回车被
// 吞掉（全屏不开、赞不发），第二条才生效。
//
// ⚠️ **判据是「回车那一下就成了」**，不是「`engaged` 是不是 false」：后者是
// 实现细节，而用户看到的是「按了没反应」。也正因为这样，`settle()` 中间跑过
// 几帧都不影响这一条——它问的是最后那一次激活有没有被吃掉。
func TestACoverHoldReleasedOffTheCoverKeepsTheNextKeyPress(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdCover(t, e)

	advanceLike(e, time.Duration(biuLikeRingDelayMS+200)*time.Millisecond)

	// 指针移出封面，在封面外松手：这一下不是点封面，不该有任何 click 落到它上面。
	e.tt.Move(5, 5)
	e.tt.Release(5, 5)
	e.tt.Frame()

	if e.s.full.open || e.tt.HasText("关闭全屏") {
		t.Fatal("松手落在封面外，全屏却开了")
	}

	// 键盘那一下：封面在指针按下时已经拿到了焦点，回车就是「按它」。
	e.tt.Key(0, mygoui.KeyEnter)
	e.tt.Frame()

	if !e.s.full.open || !e.tt.HasText("关闭全屏") {
		t.Errorf("封面外松手之后按回车，全屏没开 —— 那一次长按的「松手已过」没到期，"+
			"把键盘的第一次激活吞掉了（文字有 %q）", e.tt.Texts())
	}
}

// TestALikeButtonHoldReleasedOffItKeepsTheNextKeyPress 是同一条病的赞键形态。
//
// 🔴 赞键上的后果和封面不同：封面上被吞的是「开全屏」，赞键上被吞的是**那一次
// 点赞**——用户按了回车，画面上什么都不变，也没有任何报错。
//
// 路径完全一样：按住 600 毫秒（抖/环已经出来）→ 指针移出 → 松手 → 回车。
func TestALikeButtonHoldReleasedOffItKeepsTheNextKeyPress(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdStar(t, e, "赞")

	advanceLike(e, time.Duration(biuLikeRingDelayMS+100)*time.Millisecond)

	// 移出赞键再松手：补发的那一次 click 不落在它上面。
	e.tt.Move(5, 5)
	e.tt.Release(5, 5)
	e.tt.Frame()

	if got := e.f.likePostsCount(); got != 0 {
		t.Fatalf("移出赞键松手之后发出去 %d 次赞（用户是在取消，不是点赞）", got)
	}

	// 回车就是「按这一键」。
	e.tt.Key(0, mygoui.KeyEnter)
	e.tt.Frame()

	if got := e.f.likePostsCount(); got != 1 {
		t.Errorf("赞键外松手之后按回车，赞出去了 %d 次（want 1）—— "+
			"那一次长按的标记没到期，把键盘的第一次激活吞掉了", got)
	}
}

// TestACoverTripleReleasedOffTheCoverKeepsTheNextKeyPress 是同一条病的第三连形态：
// **三连已经触发了，然后指针移出封面再松手**。
//
// 🔴 `swallow`（三连的尾巴）和 `engaged`（环出来过）是两个标记，先前它们都在
// 「下一次指针按下」时才兜底清。三连之后移出封面松手这一路里没人问
// `swallowClick`（补发的 click 不落在封面），键盘激活又没有指针按下那一帧——
// 于是回车被吞一次，第二次才开全屏。`released` 那一条把**两个**标记一起到期
// 之后，这里必须先红后绿。
func TestACoverTripleReleasedOffTheCoverKeepsTheNextKeyPress(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	holdCover(t, e)

	advanceLike(e, time.Duration(biuLikeLongPressMS+100)*time.Millisecond)
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })

	// 移出封面再松手：这一下不是点封面。
	e.tt.Move(5, 5)
	e.tt.Release(5, 5)
	e.tt.Frame()

	if e.s.full.open || e.tt.HasText("关闭全屏") {
		t.Fatal("三连之后移出封面松手，全屏却开了")
	}

	e.tt.Key(0, mygoui.KeyEnter)
	e.tt.Frame()

	if !e.s.full.open || !e.tt.HasText("关闭全屏") {
		t.Errorf("三连之后封面外松手，再按回车全屏没开 —— `swallow` 那个尾巴没到期，"+
			"把键盘的第一次激活吞掉了（文字有 %q）", e.tt.Texts())
	}
}

// TestAQuickReleaseOnTheCoverStillOpensTheFullScreen 是上一条的反面：**短按没被误伤**。
//
// 499 毫秒还没到进度环的门槛，按 Biu 的语义就是一次普通点击——封面照旧开全屏。
// 没有这一条的话，把「松手一律吞掉」写进去也能让上一条全绿，而封面就再也点不开了。
func TestAQuickReleaseOnTheCoverStillOpensTheFullScreen(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdCover(t, e)
	pt := coverPoint2(t, e.tt)

	advanceLike(e, time.Duration(biuLikeRingDelayMS-1)*time.Millisecond)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if !e.s.full.open || !e.tt.HasText("关闭全屏") {
		t.Errorf("封面按住 %d 毫秒松手，全屏没开 —— 短按被长按那一套误伤了",
			biuLikeRingDelayMS-1)
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

	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
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

	waitFrames(t, e.tt, "满屏上的长按三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
}

// TestTheFullScreenCoverHoldPastTheRingStaysPut 钉住满屏那张的「中途松手」。
//
// 🔴 同一个病在满屏上的形状**不是开全屏**（它本来就开着）：漏吞的话补发的那次
// click 落在满屏封面这一层，而这一层**没有自己的本命动作**——真正的问题是它会
// 把 `swallow` 标记留在**两张封面共用的那一份状态机**里，一路活到播放条，把用户
// 接下来第一次点封面吃掉（要点第二下才开全屏）。这一条量的是「松手之后满屏还在、
// 没三连、标记没留下」。
func TestTheFullScreenCoverHoldPastTheRingStaysPut(t *testing.T) {
	e := newLikeEnv(t, false, false, true)

	r := coverAt(t, e.tt)
	e.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Fatalf("满屏没开，文字有 %q", e.tt.Texts())
	}
	e.tt.Frame()

	big := fullCoverRect(t, e.tt)
	pt := [2]float32{big.X + big.W/2, big.Y + big.H/2}
	e.tt.Press(pt[0], pt[1])
	advanceLike(e, time.Duration(biuLikeRingDelayMS+200)*time.Millisecond)
	e.tt.Release(pt[0], pt[1])
	e.tt.Frame()

	if !e.s.full.open || !e.tt.HasText("关闭全屏") {
		t.Error("满屏封面上按住 700 毫秒松手，满屏却关掉了")
	}
	if got := e.f.triplePostCount(); got != 0 {
		t.Errorf("按住 %d 毫秒就三连了（%d 次），Biu 是 %d 毫秒",
			biuLikeRingDelayMS+200, got, biuLikeLongPressMS)
	}
	if e.s.like.swallow {
		t.Error("满屏那张松手之后还留着吞 click 的标记 —— 它会吃掉播放条上下一次点击")
	}
	// ⚠️ **`engaged` 也一样要消费掉**：它和 `swallow` 是两条独立判据，漏问它等于
	// 这一层什么都没做（`viewCover` 那边会替它吞，而那不是这一层的责任）。
	if e.s.like.engaged {
		t.Error("满屏那张松手之后「这一按算长按」还留着 —— 满屏这一层没消费它")
	}

	// 收手之后关掉满屏，点播放条封面：必须**一次**就开。
	closeFull(t, e)
	again := coverAt(t, e.tt)
	e.tt.ClickAt(again.X+again.W/2, again.Y+again.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Error("满屏封面中途松手之后，第一次点播放条封面没开全屏（要点第二下）—— " +
			"那一按的吞标记没人消费，一路活到了这里")
	}
}

// ---- 满屏三连之后的那一次点击 ----

// openFullAndTriple 开满屏、在满屏那张封面上按住 2.7 秒，把三连真的发出去，
// **不松手**。调用方接着决定松手还是先关掉满屏。
//
// ⚠️ 返回的 `big` 是松手要用的那个坐标，而**松手那一帧之后必须让共享元素转场
// 落回播放条那一张**（BMG-34 那一票留下的）：关掉满屏的那一帧播放条上的封面是
// 「新出现」的，于是它从满屏那个框长回来——刚转场那一帧它还画在窗口正中，而标签
// 量的是**画出来的**框。不等它落下来就按位置点，点的是空气（实测点完满屏没开）。
func openFullAndTriple(t *testing.T, e *likeEnv) mygoui.Rect {
	t.Helper()
	row := coverAt(t, e.tt)
	e.tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Fatalf("满屏没开，文字有 %q", e.tt.Texts())
	}
	e.tt.Frame()

	big := fullCoverRect(t, e.tt)
	e.tt.Press(big.X+big.W/2, big.Y+big.H/2)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	waitFrames(t, e.tt, "三连发出去了", func() bool { return e.f.triplePostCount() > 0 })
	// ⚠️ **等的是 App 那一栏，不是「已赞」那个字**：满屏开着的时候播放条压根没建
	// （BMG-34 的设计），星标根本不在画面上。要等的是「B 站的答案落地了、
	// likeArmed 因此变成 false」——那正是让清标记那一支再也进不去的状态。
	waitFrames(t, e.tt, "三连的答案落地了", func() bool {
		return e.s.app.LikeState().Liked && !likeArmed(e.s.app)
	})
	return big
}

// closeFull 按 Esc 关掉满屏并等转场落回播放条。
func closeFull(t *testing.T, e *likeEnv) {
	t.Helper()
	e.tt.Key(0, mygoui.KeyEscape)
	e.tt.Frame()
	if e.tt.HasText("关闭全屏") {
		t.Fatal("Esc 没关掉满屏")
	}
	advanceLike(e, 2*time.Second)
	e.tt.Frame()
}

// TestTheFullScreenTripleDoesNotEatTheNextClickOnThePlaybar 钉住
// 「长按三连之后，用户接下来的第一次点击还是他自己的」。
//
// 🔴 这是评审抓到的一条 critical：满屏那张封面只调了 `Pressed()`，**松手补发的那
// 一次 click 落在它上面而没有人问**，于是 `swallow` 那个标记一路活到播放条，把
// 用户接下来第一次点封面的那一下吃掉——**封面要点第二下才开全屏**。
//
// 而它自己不会好：三连成功之后 `Liked` 变真 → `likeArmed` 为 false → 清标记的
// 那一支（`case armed && pressed && !g.held`）永远进不去。
//
// 走的是评审给的那条路径：开满屏 → 长按 2.7 秒 → 三连成功 → 关掉满屏 → 点封面。
func TestTheFullScreenTripleDoesNotEatTheNextClickOnThePlaybar(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	big := openFullAndTriple(t, e)

	e.tt.Release(big.X+big.W/2, big.Y+big.H/2)
	e.tt.Frame()
	// 这一句单独钉住「满屏那一张消费了自己的那一次」——少了它，下面那句「点一次
	// 就开」还能靠下一次按下时的兜底蒙混过关（而那正是第一版的病根）。
	if e.s.like.swallow {
		t.Error("满屏那一张松手之后没有消费掉补发 click 的标记")
	}

	closeFull(t, e)

	// 用户接下来的第一次点击：必须**一次**就开全屏。
	again := coverAt(t, e.tt)
	e.tt.ClickAt(again.X+again.W/2, again.Y+again.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Errorf("满屏三连之后第一次点封面没开全屏（要点第二下）—— " +
			"补发 click 的标记没人消费，它一路活到了这里")
	}
}

// TestATripleLeftHoldingDoesNotStrandTheFlag 钉住那条兜底。
//
// 上一条里标记在松手那一帧就被消费掉了；这一条是**它没机会被消费**的那种：用户
// 按着封面等三连、等出来了直接按 Esc（手指还按着），然后才松手、再点封面。
//
// 那种情况下松手落下来的时候满屏早就不画了，于是没有任何长按目标会问那句
// `swallowClick`。**清标记的那一支也进不去**（三连成功之后 `likeArmed` 为 false）。
// 唯一挡住「吃掉用户下一次点击」的就是 `trackLike` 里「下一次按下先清标记」那一行——
// 它不看 armed，所以哪怕这一首已经赞过了也照清。
func TestATripleLeftHoldingDoesNotStrandTheFlag(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	big := openFullAndTriple(t, e)

	// 手指还按着就关掉满屏：松手落在一块谁也不是的地方。
	e.s.closeFullPlayer()
	e.tt.Frame()
	e.tt.Release(big.X+big.W/2, big.Y+big.H/2)
	e.tt.Frame()
	if !e.s.like.swallow {
		return // 标记已经被清掉了（某个长按目标问了）——那也满足这一条
	}
	closeFull(t, e)

	again := coverAt(t, e.tt)
	e.tt.ClickAt(again.X+again.W/2, again.Y+again.H/2)
	if !e.tt.HasText("关闭全屏") {
		t.Error("三连之后没被消费的那个标记，把用户接下来第一次点封面吃掉了")
	}
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
// ⚠️ 一个刚开始的环只有几个像素（起步那一截有 `likeRingMinSpan` 那么长、2px 宽），
// 所以「画出来了吗」判的是 `> 0` 而不是「够不够多」。
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
// 它是共享元素转场的**终点**：宽 `min(宽*0.7, 高*0.48)` 夹在 260~520（上游那个
// 式子），高是宽的 0.75。找元素不行——满屏上那个「当前封面」和播放条上那个同名，
// 而播放条这时候压根没建。
//
// ⚠️ **位置是我们自己的摆法**（票 #236）：歌词开着的时候满屏分两栏，封面在**左边
// 那一栏**里居中（右边那一栏让给歌词，见 `viewFullStage`），竖直方向居中在顶栏
// 与底栏之间那一段（底下那一段留给频谱）。三个数写死在这儿——布局和期望都用
// 生产代码那个常量的话，常量一改两边一起变，测试就发现不了了。
func fullCoverRect(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	winW, winH := float32(likeWinW), float32(likeWinH)
	w := min(winW*0.7, winH*0.48)
	w = min(max(w, 260), 520)
	h := w * 0.75
	const (
		coverColRatio = 0.42 // 左边那一栏占窗口宽的几成
		topBarH       = 64   // 顶上那一条的高
		controlsH     = 92   // 底下那一条的高
		spectrumBand  = 52   // 频谱那一条（40 + 12 的缝）
	)
	colW := winW * coverColRatio
	top := float32(topBarH)
	bottom := winH - float32(controlsH) - float32(spectrumBand)
	return mygoui.Rect{
		X: (colW - w) / 2,
		Y: top + (bottom-top-h)/2,
		W: w, H: h,
	}
}
