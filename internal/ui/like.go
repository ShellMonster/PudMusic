// 点赞与一键三连：Biu 的 `components/music-thumb`（`index.tsx` + `index.css`）。
//
// # 手势：长按 2.5 秒 = 三连，短按 = 它本来的那一按
//
// 长按三连有**三个入口**：
//
//	播放条上的「赞」键  照 B 站的样式：按住键上抖，2.5 秒一到三连发出去。
//	                    没有环，抖就是「正在进行」的全部（见 likeShakeDX）。
//	播放条那张封面      SPEC 需求 69「长按封面 2.5 秒一键三连」。环贴着封面
//	                    外边框转，跑满一圈 = 触发。
//	满屏那张封面        同上——满屏上没有别的赞入口，SPEC 说的就是它。
//
// # 三个入口是**三份状态机**，不是共用一份
//
// `trackLike` 对「这一帧没按住」的处理是把 held 清掉。赞键和播放条封面**同帧
// 常驻**，共用一份的话，按住哪一个都会被另一个目标每帧喂进来的「没按」归零——
// 2.5 秒永远到不了，长按彻底没反应。所以赞键自己一份（`s.likeBtn`），两张封面
// 共用一份（`s.like`）：满屏开着的时候播放条整个不建（见 shell.go 那一栏），
// 任一帧只有一张封面在喂它，互斥的前提还成立。
//
// 跟 Biu 那边的**故意的差别**也变了：Biu 的长按目标只有播放条上那个「赞」键
// （`IconButton` 在封面右边），我们先是照 SPEC 接了封面，现在赞键也接上——
// 两边的形状就一样了。而病根是同一个：**长按的松手和普通点击的松手在框架看来
// 没有区别**，所以补发的那一次 click 必须由**每一个长按目标自己**吞掉
// （`likeGesture.swallowClick`，三连触发时置上、松手那一帧消费）。谁漏了它，
// 一次长按就多出一次自己的本命动作：赞键上是「又赞/取消赞一次」，封面上是
// 「凭空弹出全屏播放器」——后者更难看：进度环刚转满，界面就换成满屏那一屏。
//
// # 两个数是死的（照抄 Biu）
//
//	按下 500ms  之后开始画进度环/抖    index.tsx:100 的 progressDelayTimerRef
//	按住 2500ms 触发一键三连           index.tsx:104 的 longPressTimerRef
//
// 进度环 2 秒转满（`index.css` 的 `animation: … 2s linear forwards`）——500 + 2000
// 正好是 2500，所以环转满的那一刻就是三连触发的那一刻。**这两个数不是巧合**，
// 把环的时长单独改了就对不上了（见 `likeRingSpan`）。赞键没有环，但 500ms 这个
// 门槛是同一条：短按和长按要先分得开，抖才能开始。
package ui

import (
	"math"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// 长按三连的两个数，和进度环的时长。
//
// ⚠️ 三个数是**一个式子**：环从 500ms 画起、2s 转满，而 500+2000 = 2500。
// 环转满 = 三连触发。Biu 那边这三样在两个文件里（`index.tsx` 的两个
// `setTimeout` 和 `index.css` 的 `animation`），形状一样但**没有互相引用**。
const (
	// likeLongPress 是按住多久触发一键三连（`index.tsx:104` 的 2500）。
	likeLongPress = 2500 * time.Millisecond
	// likeRingDelay 是按住多久开始画进度环（`index.tsx:100` 的 500）。
	likeRingDelay = 500 * time.Millisecond
	// likeRingSpan 是环画多久转满（`index.css` 的 `2s linear forwards`）。
	//
	// 它必须是 `likeLongPress - likeRingDelay`——见文件头那一段。
	likeRingSpan = likeLongPress - likeRingDelay
)

// 进度环那几个数，意图照抄 `music-thumb/index.css`：
//
//	inset: -2px                          环套在按钮外面 2px
//	mask: … calc(100% - 2px)             环的粗是 2px
//	background: conic-gradient(primary)  颜色跟主题的主色走
//
// 形状不一样了：环是**贴着目标外边框**的圆角矩形，不是外接椭圆——椭圆只在中边
// 那四个点摸到封面的边，四个角上整段划进封面里头去；贴边的那版跟着封面圆角走。
const (
	// likeRingWidth 是环的粗（Biu 的 2px）。
	likeRingWidth = 2
	// likeRingArcStepDeg 是四段圆角弧每隔多少度落一个点。
	//
	// 12° 在半径 7（播放条封面圆角 6 + 环外扩 1）的圆角上弦高不到 0.1 像素；
	// 直边只要端点。一圈的点数因此是固定的（不到 70 个），跟封面多大无关——
	// 520 宽的满屏封面和 56 的播放条封面一样多，再多只是白烧 CPU。
	likeRingArcStepDeg = 12.0
	// likeRingMinSpan 是起步那一帧至少画出的一小截（像素）。
	//
	// 进度刚过 0 的那一帧按比例走出来不到一个像素——一个点什么都画不出来，
	// 「500ms 一过环立刻看得见」就断了。旧椭圆版第一段是 7.5° 的一条弦，
	// 天生看得见；这里把那个下限补回来。
	likeRingMinSpan = 4
)

// 赞键长按那一档的抖：按住 500ms 之后开始，2500ms 触发那一停回到原位。
// B 站长按赞键就是这一下高频的小碎抖——键上没有环，「还在按」全靠它说。
const (
	// likeShakeAmp 是抖的幅度（DIP）。18 的图标每边有 7 的空余，1.5 抖出去
	// 不裁边；再小就在 32 见方的键上看不出「在抖」了。
	likeShakeAmp = 1.5
	// likeShakePeriod 是一个来回多久。90ms ≈ 每秒 11 个来回、22 次换向：
	// 再慢像是在晃，再快糊成一团。
	likeShakePeriod = 90 * time.Millisecond
)

// 赞键那一下的动效：图标层先弹，**背景层晚 0.1s 才长出来**。
//
// # 0.1s 这个数从哪来
//
// 《制作点赞与投票动画》榜单 v3：「爱心与圆形背景分别做 0.7s 的 scale 动画，
// **背景晚 0.1s 开始**」；手册里也有一处同样的说法（2024 年的点赞按钮：背景爱心
// 比按钮圆形层延迟 0.1s）。
//
// ⚠️ **晚 0.1s 是靠两条通道 + `Animation.Delay` 做的**（框架的 `Delay` 只在
// **目标变了**的时候才生效，见 `mygo/ui/motion.go`）——所以图标层和背景层是
// **两个元素、两条通道**，不是一条通道两个数。
const (
	// likePopScale 是图标层弹到多大（弹完回到 1）。
	likePopScale = 1.25
	// likePopUp 是弹上去要多久。
	likePopUp = 100 * time.Millisecond
	// likePopDown 是弹回来要多久。
	//
	// ⚠️ **比弹上去慢**：弹回去是「余韵」，快一点会显得像弹了一下就断了。
	likePopDown = 180 * time.Millisecond
	// likeBgDelay 是**背景层比图标层晚多久开始**。
	likeBgDelay = 100 * time.Millisecond
	// likeBgTime 是背景层长出来要多久。
	//
	// ⚠️ **比图标层慢**：两层的时长一样的话，晚 0.1s 才开始的那一层会「追」上来，
	// 错落就白错了。
	likeBgTime = 260 * time.Millisecond
	// likeBgRest 是背景层在「没赞」那一档缩到多少。
	//
	// 🔴 **不是 0**：手册 ⓪ ②「伴随不透明度的缩放动画…不要以 0 作为开始和结束的
	// 值，一般保持在 0.5 以上」——用户很容易注意到元素从一个点出现。所以背景层在
	// 「没赞」那一档是**半大、半透明**（不透明度那一档是 0，所以看不见），长出来
	// 的时候从 0.5 起，而不是从一个点里钻出来。
	likeBgRest = 0.5
	// likeBgAlpha 是背景层那一圈的颜色透明度。
	//
	// 0.18：主色压一层很淡的底，图标（主色）压在上面读得出来（对照 DESIGN.md
	// §四那张对比度表——主色当字色是 1.9:1，所以底要淡到不干扰它）。
	likeBgAlpha = 0.18
)

// 赞键那几条动画通道的键。
//
// ⚠️ **必须是常量**：框架把动画的值存在元素状态的一个 map 里，键是 `any`——
// 传一个每次都要装箱的字符串，每一帧就多一次分配（见 `widgets/flat/press.go`）。
//
// ⚠️ **背景层那两条通道没有了**：它现在走 `AnimatePose`（缩放和不透明度合成一个
// pose，见 `likeButton` 那一段），而 pose 的值**没有公开的读法**——所以「背景层
// 此刻缩到多少」只能从**画面上**量（见 `like_anim_test.go`）。
const likeIconKey = "like:icon"

// likeGesture 是长按那一套状态机。
//
// ⚠️ **只由 view 在主线程上读写**（铁律一）。它挂在 `Shell` 上而不是每帧新建：
// 即时模式下一帧一新建就等于「按了 2.5 秒」这件事每次都从头记。
type likeGesture struct {
	// held 是「手指还按着」。
	held bool
	// start 是按下去那一刻（`s.clock` 的钟，见 clock.go）。
	start time.Time
	// fired 是这一按已经三连过。再按住多久都不再发第二次（Biu 的
	// `longPressTimerRef` 是一次性的）。
	fired bool
	// swallow 是「下一次 click 要吞掉」。三连触发时置上，长按的松手那一帧被
	// `swallowClick` 消费掉。理由见文件头。
	swallow bool
	// progress 是环走到哪（0~1），0 = 不画。每一帧重算（不用动画），
	// 所以它只是这一帧的答案，**不是**跨帧记着的进度。
	progress float32

	// popUntil 是图标层那一弹什么时候结束（**引擎钟**，见 `viewLikeStar`）。
	//
	// ⚠️ **是引擎钟，不是 `s.clock`**：这一档是画面上的动画，引擎钟才听「减少
	// 动态效果」、才推得动（无头测试推的就是它）。长按那 2.5 秒是真时钟的事
	// （`start` 那一栏），两件事各用各的钟。
	//
	// ⚠️ 只有**赞键那一份**（`s.likeBtn`）用得到：弹的是赞键的图标，封面没有
	// 图标可弹。
	popUntil time.Time

	// 下面是**这一帧图标层缩到多少**（0 ~ 1 或 1.25）。
	//
	// ⚠️ **它是这一帧的答案，不是跨帧记着的进度**——跟上面那个 `progress` 一
	// 样（每一帧重算）。留着是因为「图标层先动、背景层晚 0.1s」这件事**只有
	// 从它身上看得见**：画面上那一圈淡主色太浅，量像素量不出来（背景层那
	// 一半现在只能从画面上量，见 `like_anim_test.go`）。
	//
	// iconScale 是图标层（赞的那一下弹到 1.25，弹完回到 1）。
	iconScale float32

	// pts 是环的顶点，每帧复用（稳态一帧不分配，和频谱那一块同一条）。
	// ⚠️ 只有**画环的那一份**（`s.like`，两张封面）用得到；赞键没有环。
	pts [][2]float32
}

// likeArmed 判「这一首现在能不能长按三连」。
//
// ⚠️ **问过之后才开**：换歌之后头半秒界面上的「没赞」是我们**猜**的（B 站那一趟
// 还在路上），而猜错的方向是「其实赞过了还让长按」——那一次三连会白花两枚币。
// Biu 那边是 `isThumb` 的初值 false，等 `refreshIsFav` 回来，中间那半秒同样开着；
// 这里宁可少开半秒。
func likeArmed(a *app.App) bool {
	return a.Likeable() && a.LikeAsked() && !a.LikeState().Liked
}

// trackLike 把「这一帧按着没有」喂进状态机，认出进度环走到哪，走到点就发三连。
//
// 🔴 **armed（`likeArmed`）是 false 时不计时**——Biu 那边是 `handleMouseDown` 的
// `if (isThumb) return`：连计时都不开始，于是既没有进度环也没有三连，而那一次
// click 仍然是普通的 click（封面照旧开全屏）。照抄的就是这个，不是「照旧三连但
// 换个颜色」。
//
// ⚠️ **每帧只喂一次**，而且喂的就是**长按目标自己**报的那个 `Pressed()`。
// 喂两次（一次 true 一次 false）会把这一按立刻取消掉：这一帧末尾那个 false
// 看到的是「手指已经松开了」。这正是「每个目标一份状态机」的原因——见文件头。
//
// ⚠️ **它管的是「按着没有」和「什么时候发三连」，环画到哪是**画的时候**算的**
// （`likeRingProgress`）——理由见那一段（局部重绘不重建，票 #392）。
func (s *Shell) trackLike(g *likeGesture, c *ui.Context, pressed, armed bool) {
	now := s.clock.Now()

	// 这一次按下开始了：从这一刻起计时。
	//
	// 🔴 **清掉 `swallow` 不看 armed**：那个标记是**某一个长按目标**三连时置上的，
	// 而只要有一个目标忘了在松手那一帧问一句 `swallowClick`，它就一路活到下一次
	// 按下——**把用户接下来的第一次正常点击吃掉**（第一版的满屏那一张就是这样：
	// 关掉满屏之后点封面要点第二下才开全屏，而它自己永远好不了，因为三连成功之后
	// `Liked` 变真、`likeArmed` 变 false，清标记的那一支再也进不去）。
	//
	// 所以每个长按目标**都要**在松手那一帧消费它自己那一次（见 `viewFullCover`），
	// 而这里再兜一层：漏掉一个消费者的后果最多是「标记到下一次按下就被清掉」，
	// 而不是用户看到的多点一下。
	if pressed && !g.held {
		g.held, g.start, g.fired, g.swallow = true, now, false, false
	}

	g.progress = 0
	if g.held && armed {
		held := now.Sub(g.start)
		if !g.fired && held >= likeLongPress {
			// 到点了。Biu 那边这一帧把进度环收掉（`setIsProgressing(false)`）
			// 并把补发的那一次 click 标成要吞的（`ignoreNextPressRef = true`）。
			g.fired, g.swallow = true, true
			s.tripleNow()
		}
		// 环：500ms 之后才开始画，画到三连那一刻正好转满。
		//
		// ⚠️ **三连发出去之后就不再画环**：Biu 那边是同一句
		// `setIsProgressing(false)`。环留着不动会让人以为「还没触发」。
		if !g.fired && held > likeRingDelay {
			g.progress = min(float32(held-likeRingDelay)/float32(likeRingSpan), 1)
		}
	}

	if !pressed {
		g.held = false
	}

	// ⚠️ **按住的时候必须一直要帧**：指针不动时框架不会自己出帧，于是进度环
	// 停在 0、那 2.5 秒也永远不会到（不报错、不崩溃，就是长按没反应）。
	//
	// ⚠️ **但只有「环还没出来」的那一段要靠重建**（`c.AnimationFrame()`）：
	// 环那个元素此刻还不存在，于是**没有东西能去要「只重画」的那一帧**——
	// 而局部重绘那一帧**不重建**（`mygo/ui/runtime.go` 的 `repaintFrame`），
	// 靠它量不到「按了多久」。
	//
	// 环出来之后（`g.progress > 0`）就换成**画的时候**要帧
	// （`likeRingProgress` 里的 `p.AnimationFrame()`），那一段走的是局部
	// 重绘：整棵元素树不必每一帧重建一遍（票 #392）。
	if armed && pressed && g.progress <= 0 {
		c.AnimationFrame()
	}
}

// swallowClick 消费掉长按之后补发的那一次 click。返回 true = 这一次要吞掉。
//
// ⚠️ **谁来问**：长按目标自己在处理 `Clicked()` 的那一帧问。问的人只能是那个
// 元素——补发的那一次 click 落在长按目标上，别人问了也问不到（而别人问了就
// 会把标记吃掉，然后真正要吞的那一次没吞）。
//
// 它是**一次性的**：消费掉就清掉（和 Biu 的 `ignoreNextPressRef` 一样）。漏了
// 消费也不要紧——下一次按下会把标记清掉（见 `trackLike`），所以一个漏掉的标记
// 最多吃掉一次正常点击的资格，而不会一直卡着。
func (g *likeGesture) swallowClick() bool {
	if !g.swallow {
		return false
	}
	g.swallow = false
	return true
}

// tripleNow 发那一次一键三连。**立刻返回**，结果在后台那一趟。
//
// ⚠️ 回调跑在后台 goroutine 上（铁律一）。它只做两件事：`addToast`（自己带锁，
// 安全）和 `App` 那一层已经叫过唤醒。**不许在这里碰 Shell 的字段**。
func (s *Shell) tripleNow() {
	s.app.TripleLike(func(res bili.TripleResult, err error) {
		if err != nil {
			s.addToast("一键三连没成功："+err.Error(), notice.Danger)
			return
		}
		// ⚠️ **三样要分开说**：B 站可能只做成一部分（硬币不够、这一首投过了、
		// 风控拦了）。一句话「三连成功」会骗用户说「投出去了」——而他手上没有
		// 那一枚币，那是他花了钱的。
		if msg := tripleSaysWhatHappened(res); msg != "" {
			s.addToast(msg, notice.Danger)
		}
	})
}

// tripleSaysWhatHappened 是三连做完之后那句话。
//
// 三样都成了就**不说话**：星标亮起来、收藏那一键也亮了，那就是答复，Biu 那边
// 成功时也不弹提示（`music-thumb` 只在失败时 `addToast`）。
//
// 差一样就说差的那一样——「赞和收藏成功了，投币没成功」。
func tripleSaysWhatHappened(res bili.TripleResult) string {
	yes, no := []string{}, []string{}
	for _, p := range []struct {
		name string
		ok   bool
	}{{"赞", res.Liked}, {"投币", res.Coined}, {"收藏", res.Favorited}} {
		if p.ok {
			yes = append(yes, p.name)
		} else {
			no = append(no, p.name)
		}
	}
	if len(no) == 0 {
		return ""
	}
	msg := "三连里" + strings.Join(no, "、") + "没成功"
	if len(yes) > 0 {
		msg = strings.Join(yes, "和") + "成功了，" + msg
	}
	return msg
}

// viewLikeRing 画长按进度环，并把这一帧该发的三连发出去。
//
// g 是这个长按目标**自己那份**状态机（两张封面共用 `s.like`，赞键那份见
// `likeButton`），radius 是目标的圆角——环跟着它拐弯才贴得住边。
//
// ⚠️ **状态机和画环必须在同一个元素里面、同一次调用里**：环画的是「这一帧按了
// 多久」，而按下多久是 `Pressed()` 刚问出来的。分成两处的话环永远比手慢一帧
// （而一帧是 16 毫秒——用户看得见环起得晚）。
//
// ⚠️ 环是**封面外面**的一圈（Biu 的 `inset: -2px`），所以它摆成铺满封面的一层
// `Absolute`，画的时候把半径往外推一点。它 `PassThrough`：环自己不接指针——
// 否则指针停在环上时报的是「停在空气上」，悬停那一层（⤢）就不亮了。
//
// ⚠️ **环画到哪是**画的时候**算的**（`likeRingProgress`），而「环这个元素要不要
// 建」是**建的时候**看的（`g.progress`）——那一段（按住的头 500ms）还没有
// 环可画，只能靠重建（见 `trackLike` 那一段）。
func (s *Shell) viewLikeRing(g *likeGesture, c *ui.Context, pressed bool, radius float32) {
	armed := likeArmed(s.app)
	s.trackLike(g, c, pressed, armed)
	if g.progress <= 0 {
		return
	}
	ring := ui.Box(c).Key("like-ring").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		PassThrough().
		Role(ui.RoleProgress).Label("点赞进度")
	ring.Draw(func(p *ui.Painter, r ui.Rect) {
		progress := s.likeRingProgress(g, armed)
		if progress <= 0 {
			// 三连刚发出去（或者这一段本来就不画）：环收掉。
			//
			// ⚠️ **这一帧不再要下一帧**：环已经没有了，而三连那一趟是后台
			// 线程在跑（`App.TripleLike` 自己起 goroutine），它回来时会叫醒
			// 界面（铁律三）。
			return
		}
		p.StrokePolyline(s.ringPoints(g, r, radius, progress), likeRingWidth, p.Theme().Accent)
		// 环在动 → 要下一帧。而那一帧走的是**局部重绘**
		// （`p.AnimationFrame()`，不是 `c.AnimationFrame()`）：环是画出来的，
		// 而局部重绘那一帧**只重画、不重建整棵元素树**（票 #392）。
		p.AnimationFrame()
	})
}

// likeRingProgress 是环走到哪（0~1），0 = 不画。
//
// ⚠️ **它是在画的时候算的**（`viewLikeRing` 的 `Draw` 回调里），不是建的时候：
// 环走的是**局部重绘**（`p.AnimationFrame()`），而局部重绘那一帧**不重建**
// ——建的时候算出来的进度会被**冻结**在闭包里，环于是不走了（票 #392）。
//
// ⚠️ **用的是 `s.clock` 那只钟，不是 `p.Now()`**：这一段的判据是「按了多久到
// 了 2500ms」——那是一个**绝对**的时刻，而 `p.Now()` 是这一帧的显示时刻，
// 比墙钟早一帧（十几毫秒）。用早一点的时刻判「到没到 2500ms」会让三连晚
// 触发一点，而 Biu 那三个数（500 / 2500 / 2s）是死的。
//
// ⚠️ **三连也是在这里发出去的**（而不是等到下一次重建）：局部重绘那一帧**不
// 重建**，而「按了多久」只有这一层知道——等下一次重建才发现就晚了。
func (s *Shell) likeRingProgress(g *likeGesture, armed bool) float32 {
	if !g.held {
		return 0
	}
	held := s.clock.Now().Sub(g.start)
	if armed && !g.fired && held >= likeLongPress {
		// 到点了（同 `trackLike` 那一支）：Biu 那边这一帧把进度环收掉
		// （`setIsProgressing(false)`）并把补发的那一次 click 标成要吞的
		// （`ignoreNextPressRef = true`）。
		g.fired, g.swallow = true, true
		s.tripleNow()
	}
	if !armed || g.fired || held <= likeRingDelay {
		return 0
	}
	return min(float32(held-likeRingDelay)/float32(likeRingSpan), 1)
}

// ringPoints 算出这一帧进度环的那些点。
//
// 环是**贴着目标外边框**的圆角矩形：环心线外扩 1px（= likeRingWidth/2），圆角
// 比目标的圆角同样大 1——两个圆角**同心**、环心半径大 1，描边才和外边平行。
// 从正上方起、顺时针（和 CSS 的 `conic-gradient` 一个方向），画到 `progress`
// 转过的那一段为止——Biu 那边就是
// `conic-gradient(primary var(--progress), transparent 0deg)`。
//
// ⚠️ **每帧复用 `g.pts`**（稳态一帧不分配）；一圈的点数是固定的：直边只落
// 端点，弧段按 `likeRingArcStepDeg` 落点——跟目标多大无关。
func (s *Shell) ringPoints(g *likeGesture, r ui.Rect, radius float32, progress float32) [][2]float32 {
	const out = likeRingWidth / 2
	x0, y0 := r.X-out, r.Y-out
	w, h := r.W+likeRingWidth, r.H+likeRingWidth
	rad := min(radius+out, min(w, h)/2)
	cx := x0 + w/2

	// 从顶心起顺时针的九段：顶边右半、右上角、右边、右下角、底边、左下角、
	// 左边、左上角、顶边左半。下标偶数是直段（一个终点就够——前一段的终点
	// 就是它的起点），奇数是弧段。
	straight := w/2 - rad
	arc := float32(math.Pi/2) * rad
	var lengths [9]float32
	lengths[0], lengths[1], lengths[2], lengths[3] = straight, arc, h-2*rad, arc
	lengths[4], lengths[5], lengths[6], lengths[7] = w-2*rad, arc, h-2*rad, arc
	lengths[8] = straight

	d := progress * (2*w + 2*h - 8*rad + float32(2*math.Pi)*rad)
	if d > 0 && d < likeRingMinSpan {
		d = likeRingMinSpan
	}

	// pointAt 算第 k 段上走出 t 远的那个点。弧段的角从各自的起角起、按屏幕
	// 坐标（y 朝下）转——顺时针就是角变大。
	pointAt := func(k int, t float32) [2]float32 {
		switch k {
		case 0: // 顶边右半：向右
			return [2]float32{cx + t, y0}
		case 1: // 右上角：-90° → 0°
			ang := -math.Pi/2 + float64(t/rad)
			return [2]float32{x0 + w - rad + rad*float32(math.Cos(ang)),
				y0 + rad + rad*float32(math.Sin(ang))}
		case 2: // 右边：向下
			return [2]float32{x0 + w, y0 + rad + t}
		case 3: // 右下角：0° → 90°
			ang := float64(t / rad)
			return [2]float32{x0 + w - rad + rad*float32(math.Cos(ang)),
				y0 + h - rad + rad*float32(math.Sin(ang))}
		case 4: // 底边：向左
			return [2]float32{x0 + w - rad - t, y0 + h}
		case 5: // 左下角：90° → 180°
			ang := math.Pi/2 + float64(t/rad)
			return [2]float32{x0 + rad + rad*float32(math.Cos(ang)),
				y0 + h - rad + rad*float32(math.Sin(ang))}
		case 6: // 左边：向上
			return [2]float32{x0, y0 + h - rad - t}
		case 7: // 左上角：180° → 270°
			ang := math.Pi + float64(t/rad)
			return [2]float32{x0 + rad + rad*float32(math.Cos(ang)),
				y0 + rad + rad*float32(math.Sin(ang))}
		default: // 8：顶边左半，走完正好回到顶心
			return [2]float32{x0 + rad + t, y0}
		}
	}

	pts := g.pts[:0]
	pts = append(pts, pointAt(0, 0))
	left := d
	step := rad * (likeRingArcStepDeg * math.Pi / 180)
	for k := 0; k < 9 && left > 0; k++ {
		take := min(lengths[k], left)
		if take <= 0 {
			continue // 圆角钳到半边长时直段可以是 0
		}
		if k%2 == 0 {
			pts = append(pts, pointAt(k, take))
		} else {
			// 弧段：按固定步长落点，末一段对齐到 take。
			n := max(int(math.Ceil(float64(take/step))), 1)
			for i := 1; i <= n; i++ {
				pts = append(pts, pointAt(k, take*float32(i)/float32(n)))
			}
		}
		left -= take
	}
	g.pts = pts
	return pts
}

// likeShakeDX 是赞键这一帧该横着挪多远（DIP）。
//
// 抖是正弦：amp·sin(2π·已按住/周期)。500ms 之前是 0——那段是点 / 按消歧，
// 界面不该有动静；触发之后也是 0——抖是「还在按」的信号，时间到了就该停，
// 那一停正好接上图标那一弹（`likeButton` 里触发时置上的 `popUntil`）。
// armed 为 false（已赞 / 还没问到）时同样不抖：长按整个关着。
func likeShakeDX(g *likeGesture, now time.Time, armed bool) float32 {
	if !g.held || !armed || g.fired {
		return 0
	}
	held := now.Sub(g.start)
	if held <= likeRingDelay {
		return 0
	}
	return likeShakeAmp * float32(math.Sin(2*math.Pi*float64(held)/float64(likeShakePeriod)))
}

// viewLikeStar 是播放条左边第三段那个「赞 / 已赞」键。
//
// 点一下切换（`App.SetLike`，**乐观**：那一帧就亮），失败改回去并说一句人话。
// 按住 2.5 秒是长按三连——照 B 站的样子键上抖着，到点发出去（见 `likeButton`，
// 那一台状态机是赞键自己的 `s.likeBtn`）。
//
// ⚠️ **只有 B 站的 mv 才摆**（`App.Likeable`）：本地文件和音乐库条目在 B 站那边
// 没有可赞的东西（`bili/like.go` 文件头），摆一个点了没反应的点不动的键，
// 比不摆更让人以为它坏了。Biu 那边也是 `playItem.source !== "local"` 才摆
// （`playbar/left/index.tsx:89`）。
//
// 🔴 **图标 18、点击区 32**（票 #219，报告 §3.6）：上游那一枚是
// `RiThumbUpFill/Line size={18}`（`components/music-thumb/index.tsx:152-153`），
// 而**播放条上全部图标键都是 18**。改之前是 14（`iconSm`）——比上游小 4 像素。
//
// # 两层：图标层 + 背景层
//
// 底下一圈**背景**（主色很淡的一盘），上面是**拇指图形**。赞的那一下：图标层
// 先弹，**背景层晚 0.1s 才长出来**（`likeBgDelay`）——错落来自手册和那份
// 点赞动画榜单（见上面那几个数的注释）。
//
// 🔴 **两层是两个元素、两条通道**：框架的 `Animation.Delay` 只在**目标变了**的
// 时候才生效（`mygo/ui/motion.go`），一条通道没法让两层错开 0.1s。
func (s *Shell) viewLikeStar(c *ui.Context, st app.State) {
	if !s.app.Likeable() {
		return
	}
	rel := s.app.LikeState()
	label := "赞"
	color := c.Theme().TextMuted
	if rel.Liked {
		// 主色是染出来的，和收藏那一键同一种做法。
		//
		// ⚠️ **名字那一半一个字不能少**：实心/空心是画面上的事，读屏软件看不见，
		// 所以「赞」和「已赞」这两句得留在无障碍名字里。
		label = "已赞"
		color = c.Theme().Accent
	}
	// ⚠️ **图标只有 `likeIcon` 这一个出口**（不是这里 `if` 一下）——理由见
	// icons.go 那四个函数的头一段：调用点一旦自己也能决定画哪张，「哪一档配哪
	// 一张」就没有钉住的地方了。
	name := likeIcon(rel.Liked)

	// 🔴 **身份要钉死**：这一段是「分集 / 收藏 / 赞」三枚并排，而「分集」是逐帧
	// 问出来的（单 P 的稿子不摆，见 `viewPartButton`）——它一出现，后面几枚的
	// **兄弟序号**就都变了，而**动画通道是存在身份里的**（理由见
	// `playbar_keys.go`）。所以这里给这一枚钉一个 key。
	c.Key(likeButtonKey)

	btn := s.withTip(c, s.likeButton(c, name, label, color, rel.Liked),
		likeStarTooltip(rel.Liked, s.app.LikeErr() != nil && !s.app.LikeAsked()))
	if btn.Clicked() {
		// 🔴 **长按三连之后松手补发的那一次 click 必须吞掉**，否则一次长按变成
		// 「三连 + 一次多余的取消赞」（同 `viewCover`；见文件头）。而吞标记是
		// 一次性的：接下来的第一次普通点击必须照旧是点赞。
		if s.likeBtn.swallowClick() {
			return
		}
		// 赞的那一下：图标层弹起来，背景层晚 0.1s（那条通道的 `Delay` 自己算）。
		//
		// ⚠️ **这一弹是从下一帧才开始的**：框架的 `Children` 是**同步跑回调**的
		// （`mygo/ui/element.go` 的 `func (e *node) Children(fn func())` 里就是
		// `fn()`），所以图标层在**上面那一句**里就已经建好了——它问到的
		// `popUntil` 还是上一次的值。于是这一帧按完，这一弹要到下一帧才起步
		// （下一帧由 `SetLike` 叫醒，所以墙钟上几乎看不出）。
		s.likeBtn.popUntil = c.Now().Add(likePopUp)
		s.app.SetLike(!rel.Liked, func(err error) {
			if err != nil {
				s.addToast("赞不了："+err.Error(), notice.Danger)
			}
		})
	}
}

// likeButton 是赞键那一枚按钮：底下那一圈背景，上面是拇指图形。
// 它同时是长按三连的目标：这一帧的按住喂进 `s.likeBtn`，抖动也是这一帧算的。
//
// ⚠️ **两层都是绝对定位、居中挂在按钮上**：绝对定位的元素绘制时排在普通子元素
// **之后**（`mygo/ui/paint.go` 的两趟循环），而同一趟里是按**建元素的先后**画的
// ——所以背景层先建、图标层后建，图标才落在背景上面。
//
// ⚠️ **位置用 `Attach(AnchorCenter, AnchorCenter)`**：绝对定位是相对按钮的
// **padding box**（`mygo/ui/element.go` 的 `Absolute` 文档：「Top(0) puts the
// element just below the border, whatever the padding」），居中挂上去正好在按钮
// 正中间，和内边距无关。
//
// ⚠️ **抖要抖在内层（背景 + 图标），不能抖在按钮上**：框架的 Transform 连
// 命中框一起挪（`mygo/ui/transform.go`：「changes drawing and interaction」）
// ——抖按钮本体，命中框、悬停反馈和 tooltip 的锚点就都跟着晃；抖内层只有
// 画面动，按钮自己站得稳稳的。
//
// ⚠️ **按下时是「放大」到 1.04**（不是别的图标键那种缩到 0.96）：赞是往外给的
// 一个动作，按下去的时候让它**涨一点**，比缩下去更像「按到了」（手册 ⓪ ①：
// 「如果是放大，一般 1.04 就够了」）。
func (s *Shell) likeButton(c *ui.Context, name, label string, color ui.Color, liked bool) ui.Element {
	b := flat.Button(c, "").Label(label).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7).TextColor(color)

	// 长按三连：这一帧的按住喂进赞键**自己那一份**状态机。
	//
	// ⚠️ **不能和封面共用 `s.like`**：赞键和播放条封面同帧常驻，而 trackLike
	// 对「这一帧没按住」的处理是把 held 清掉——共用的话按住哪一个都会被另一个
	// 每帧归零，2.5 秒永远到不了（为什么两张封面可以共用：满屏开着的时候播放条
	// 整个不建，见 shell.go 那一栏）。
	//
	// ⚠️ **喂和算抖都要在 `Children` 之前**：抖的偏移是「这一帧按了多久」算出来
	// 的，而按下多久是 `b.Pressed()` 刚问出来的——放到 Children 后面，抖就比手
	// 慢一帧。
	armed := likeArmed(s.app)
	fired := s.likeBtn.fired
	s.trackLike(&s.likeBtn, c, b.Pressed(), armed)
	if !fired && s.likeBtn.fired {
		// 三连触发的这一下，图标弹一下（和点击那一弹同一个动效）：时间到的
		// 「成了」就是这一停一弹，星标等 B 站的答案落地再亮。
		s.likeBtn.popUntil = c.Now().Add(likePopUp)
	}
	dx := likeShakeDX(&s.likeBtn, s.clock.Now(), armed)

	b.Children(func() {
		// 背景层（先建 → 画在下面）
		bg := ui.Box(c).Attach(ui.AnchorCenter, ui.AnchorCenter).
			Size(iconHitMd, iconHitMd).Radius(iconHitMd/2).
			Background(c.Theme().Accent.Alpha(likeBgAlpha)).Translate(dx, 0)
		// ⚠️ **缩放和不透明度合成一个 pose**（原来是两条通道）：
		//
		//	缩放管「多大」   不透明度管「多实」
		//
		// 不透明度那一档在「没赞」时是 0（所以看不见），而缩放那一档是 0.5 而不是
		// 0（手册 ⓪ ②：带透明度的缩放不要以 0 作为开始和结束的值）。
		//
		// 🔴 **两条必须一起进 pose**：判据是「换完之后那个动画值还会不会被 view
		// 读」——`AnimateTo` 的返回值是**被 view 读**的（`Scale(…AnimateTo(…))`），
		// 所以每帧都要重建整棵界面；而 `AnimatePose` 的值**元素自己走**，中间那些
		// 帧只重绘、不重建（报告 BMG-264 §7 A1）。
		//
		// ⚠️ **`Fade` 的方向和 `Opacity` 相反**：`Opacity(v)` 是 alpha（1 = 不透明），
		// 而 `Pose{Fade: f}` 是**淡出的量**（0 = 不透明，1 = 全没）⇒ 等价的是
		// **`Fade: 1 - v`**。写反了会让「赞了」的时候那圈背景整个不见。
		//
		// ⚠️ **延迟（`likeBgDelay`）对两个通道是一样长的**：pose 的五个通道各自
		// 走同一条 `Animation`，延迟也是同一条（`mygo/ui/motion.go` 的 `headFor`），
		// 所以「背景层晚 0.1s 才长出来」这件事**还在**。
		bg = bg.AnimatePose(ui.Pose{
			Scale: likeBgScaleTarget(liked),
			Fade:  1 - likeBgAlphaTarget(liked),
		}, ui.Animation{Duration: likeBgTime, Delay: likeBgDelay, Ease: motion.Unified})

		// 图标层（后建 → 画在上面）
		ic := ui.Icon(c, icon(name)).FontSize(iconMd).Attach(ui.AnchorCenter, ui.AnchorCenter)
		if col, ok := flat.IconAccentOnHover(c, false, b.Hovered()); ok {
			ic = ic.TextColor(col)
		}
		// ⚠️ **两个缩放要相乘**：框架的 `Scale` 本来就是「在已有变换之后再缩」
		// （`mygo/ui/transform.go` 的 `e.transform.Then(Scaling(x, y))`），两次
		// `Scale` 也就是乘在一起的——这里自己乘一次，是为了让「乘的是哪两个」在
		// 一行里看得见（而且只留一份变换）。抖的偏移接在最后：变换按「先缩、
		// 再平移」复合，dx 是外层坐标上的 1.5px，不会被缩放放大缩小。
		v := flat.PressScaleValue(ic, b.Pressed(), true) * s.likeIconScale(c, ic)
		ic = ic.Scale(v, v).Translate(dx, 0)
	})
	return b
}

// likeIconScale 是图标层这一帧该缩到多大：赞的那一下弹一下，弹完回到 1。
//
// ⚠️ **弹上去和弹回来是两档时长**：目标变了的时候框架才用这一帧给的时长重画
// 曲线（`mygo/ui/motion.go`），所以「弹上去 100ms、弹回来 180ms」是这么给的。
func (s *Shell) likeIconScale(c *ui.Context, ic ui.Element) float32 {
	popping := c.Now().Before(s.likeBtn.popUntil)
	target, d := float32(1), likePopDown
	if popping {
		target, d = likePopScale, likePopUp
	}
	v := ic.AnimateTo(likeIconKey, target, ui.Animation{Duration: d, Ease: motion.Unified})
	s.likeBtn.iconScale = v
	return v
}

// likeBgScaleTarget 是背景层这一帧该缩到多大。
func likeBgScaleTarget(liked bool) float32 {
	if liked {
		return 1
	}
	return likeBgRest
}

// likeBgAlphaTarget 是背景层这一帧该多实。
//
// ⚠️ **「没赞」那一档是 0**：背景层在没赞的时候要**看不见**（半大的一盘淡主色
// 挂在赞键底下，看着像没点着但颜色不对）。0.5 那个下限管的是**缩放**那一档
// （`likeBgRest`），不是这一档。
func likeBgAlphaTarget(liked bool) float32 {
	if liked {
		return 1
	}
	return 0
}

// likeStarTooltip 是星标悬停时那句话。
//
// 没赞的那一句把两个入口都说上：长按**这一键**是正路，封面是同一个动作的另一处
// ——用户盯着这个键找三连的时候，按住就行，不用知道还有个隐藏手势。
//
// 🔴 **问不出「赞没赞」时要说出来**：那一次问失败之后长按是关着的（`likeArmed`
// 要 `LikeAsked`），而**关着的时候什么都不说**就是「按住两秒半，界面一动
// 不动」——用户不会知道那是网络的事，以为是自己按错了。
//
// 🔴 **已赞那一档要说「长按用不了」**：`likeArmed` 要没赞过，已赞时长按是整个
// 关着的——照旧说「可以三连」就是把做不到的事说给人听。
func likeStarTooltip(liked, askFailed bool) string {
	switch {
	case askFailed:
		return "暂时问不出赞没赞（长按三连先关着，赞还是能点）"
	case liked:
		return "已经赞过了（取消赞；长按三连要在没赞过的歌上用）"
	}
	return "给这一首点赞（长按这一键或封面 2.5 秒一键三连）"
}
