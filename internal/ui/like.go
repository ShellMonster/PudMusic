// 点赞与一键三连：Biu 的 `components/music-thumb`（`index.tsx` + `index.css`）。
//
// # 手势：长按封面 2.5 秒 = 三连，单击封面 = 它本来的那一按
//
// SPEC 需求 69「长按封面 2.5 秒一键三连」和 Biu 那边有一个**故意的差别**：
// Biu 的长按目标是播放条上那个「赞」键（`IconButton` 在封面右边），我们的是
// **封面本身**（SPEC 就是这么写的）。于是两处补发的那一次 click 落在**不同的**
// 控件上：
//
//	Biu      长按赞键 → 三连 → 松手补发的那一次 click 落在赞键上 → **又赞/取消赞一次**
//	我们     长按封面 → 三连 → 松手补发的那一次 click 落在封面上 → **凭空弹出全屏播放器**
//
// 病根是同一个：**长按的松手和普通点击的松手在框架看来没有区别**，所以补发的那
// 一次必须由这一层自己吞掉（`likeGesture.swallowClick`）。Biu 那边漏了这个的
// 后果是「赞两次」，我们这边漏了的后果是「长按三连完，全屏自己开了」——而后者
// 更难看：进度环刚转满，界面就换成满屏那一屏。
//
// # 两个数是死的（照抄 Biu）
//
//	按下 500ms  之后开始画进度环    index.tsx:100 的 progressDelayTimerRef
//	按住 2500ms 触发一键三连         index.tsx:104 的 longPressTimerRef
//
// 进度环 2 秒转满（`index.css` 的 `animation: … 2s linear forwards`）——500 + 2000
// 正好是 2500，所以环转满的那一刻就是三连触发的那一刻。**这两个数不是巧合**，
// 把环的时长单独改了就对不上了（见 `likeRingSpan`）。
package ui

import (
	"math"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
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

// 进度环那三个数，同样照抄 `music-thumb/index.css`：
//
//	inset: -2px                          环套在按钮外面 2px
//	mask: … calc(100% - 2px)             环的粗是 2px
//	background: conic-gradient(primary)  颜色跟主题的主色走
const (
	// likeRingWidth 是环的粗（Biu 的 2px）。
	likeRingWidth = 2
	// likeRingSteps 是环画多少段。48 段转一圈是每段 7.5°，在 56 见方的那一圈上
	// 拐角误差不到 0.5 像素——再多只是白烧 CPU。
	likeRingSteps = 48
)

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

	// pts 是环的顶点，每帧复用（稳态一帧不分配，和频谱那一块同一条）。
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
// 看到的是「手指已经松开了」。
func (s *Shell) trackLike(c *ui.Context, pressed, armed bool) float32 {
	g := &s.like
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
	// `AnimationFrame` 是框架自己的那一条（`Loop` / `Animate` 用的也是它）。
	if armed && pressed {
		c.AnimationFrame()
	}
	return g.progress
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
			s.addToast("一键三连没成功：" + err.Error())
			return
		}
		// ⚠️ **三样要分开说**：B 站可能只做成一部分（硬币不够、这一首投过了、
		// 风控拦了）。一句话「三连成功」会骗用户说「投出去了」——而他手上没有
		// 那一枚币，那是他花了钱的。
		if msg := tripleSaysWhatHappened(res); msg != "" {
			s.addToast(msg)
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
// ⚠️ **状态机和画环必须在同一个元素里面、同一次调用里**：环画的是「这一帧按了
// 多久」，而按下多久是 `Pressed()` 刚问出来的。分成两处的话环永远比手慢一帧
// （而一帧是 16 毫秒——用户看得见环起得晚）。
//
// ⚠️ 环是**封面外面**的一圈（Biu 的 `inset: -2px`），所以它摆成铺满封面的一层
// `Absolute`，画的时候把半径往外推一点。它 `PassThrough`：环自己不接指针——
// 否则指针停在环上时报的是「停在空气上」，悬停那一层（⤢）就不亮了。
func (s *Shell) viewLikeRing(c *ui.Context, pressed bool) {
	progress := s.trackLike(c, pressed, likeArmed(s.app))
	if progress <= 0 {
		return
	}
	ring := ui.Box(c).Key("like-ring").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		PassThrough().
		Role(ui.RoleProgress).Label("点赞进度")
	ring.Draw(func(p *ui.Painter, r ui.Rect) {
		p.StrokePolyline(s.ringPoints(r, progress), likeRingWidth, c.Theme().Accent)
	})
}

// ringPoints 算出这一帧进度环的那些点。
//
// 从正上方起、顺时针（和 CSS 的 `conic-gradient` 一个方向），终点是 `progress`
// 转过的那一段——Biu 那边就是 `conic-gradient(primary var(--progress), transparent 0deg)`。
//
// ⚠️ **环心线离封面边 1 像素**（=`likeRingWidth/2`）：Biu 的环是「外扩 2px 的那个
// 圆环、只留最外 2px」，所以它的中线正好在封面边上凸出 1px 的地方。
func (s *Shell) ringPoints(r ui.Rect, progress float32) [][2]float32 {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rx, ry := r.W/2+likeRingWidth/2, r.H/2+likeRingWidth/2

	steps := max(int(likeRingSteps*progress), 1)
	pts := s.like.pts[:0]
	for i := 0; i <= steps; i++ {
		// 从正上方（12 点）起顺时针：sin 给横、-cos 给纵。
		ang := float64(i) / float64(steps) * float64(progress) * 2 * math.Pi
		pts = append(pts, [2]float32{
			cx + rx*float32(math.Sin(ang)),
			cy - ry*float32(math.Cos(ang)),
		})
	}
	s.like.pts = pts
	return pts
}

// viewLikeStar 是播放条第二行那个「赞 / 已赞」键。
//
// 点一下切换（`App.SetLike`，**乐观**：那一帧就亮），失败改回去并说一句人话。
//
// ⚠️ **只有 B 站的 mv 才摆**（`App.Likeable`）：本地文件和音乐库条目在 B 站那边
// 没有可赞的东西（`bili/like.go` 文件头），摆一个点了没反应的点不动的键，
// 比不摆更让人以为它坏了。Biu 那边也是 `playItem.source !== "local"` 才摆
// （`playbar/left/index.tsx:89`）。
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
	btn := s.withTip(c, iconButton(c, name, label, iconSm).
		Size(iconHit, iconHit).Padding(0, 9).TextColor(color),
		likeStarTooltip(rel.Liked, s.app.LikeErr() != nil && !s.app.LikeAsked()))
	if btn.Clicked() {
		s.app.SetLike(!rel.Liked, func(err error) {
			if err != nil {
				s.addToast("赞不了：" + err.Error())
			}
		})
	}
}

// likeStarTooltip 是星标悬停时那句话。
//
// 两句话都提「长按封面三连」，因为那是同一个动作的另一个入口——用户盯着这个键
// 找三连的时候，指针往封面那边挪一下就行，不用知道有个隐藏手势。
//
// 🔴 **问不出「赞没赞」时要说出来**：那一次问失败之后长按是关着的（`likeArmed`
// 要 `LikeAsked`），而**关着的时候什么都不说**就是「按住封面两秒半，界面一动
// 不动」——用户不会知道那是网络的事，以为是自己按错了。
func likeStarTooltip(liked, askFailed bool) string {
	switch {
	case askFailed:
		return "暂时问不出赞没赞（长按封面三连先关着，赞还是能点）"
	case liked:
		return "已经赞过了（取消赞；长按封面可以三连）"
	}
	return "给这一首点赞（长按封面 2.5 秒一键三连）"
}
