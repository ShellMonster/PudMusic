// Toast 是 Biu 全局浮层里最常用的一条。参数逐条抄自 Biu 的
// `src/app.tsx:166-172` 的 `<ToastProvider>`：
//
//	placement="bottom-right"   → 右下角
//	toastOffset={90}           → 抬过底部 90px（播放条）
//	maxVisibleToasts={3}       → 同时最多 3 条
//	toastProps={{ timeout: 2000, color: "primary" }} → 2 秒、主色卡片
//
// 这里自己管，不用 MyGo 内置的那一套：`AddToast` / `CloseToast` 挂在未导出的
// `*ui.context` 上，从我们这个包里根本调不到（`ui.Context` 上没有转发）。
// 而且「避开播放条、切路由不留上一页的提示」本来也得自己排版、自己管状态。
//
// # 票 #180：语义色、尺寸和入场
//
// 这一票把三件事一起改了（DESIGN.md §六「Toast」是那一档的目标值）：
//
//   - **按语义分色**：成功绿、失败红、警告橙、普通主色。上游那 164 处调用
//     是按调用给 `color` 的（danger 84 / success 58 / warning 22），而我们
//     此前**一律主色**——「下载失败」也是绿的。
//   - **尺寸与位置**：宽 356（上游 `@heroui/theme` 的 toast）、`p-3`（12）、
//     字 14/500、抬过底部 90、离右边 8。此前是 320、12/16、13/400、抬 106
//     （90+16）、离右边 16。
//   - **250ms 入场**（上游的 `appearance-in 250ms ease-out`）：淡入 + 从右边
//     滑进来。此前是「啪」地一下出现。
//
// # 票 #292：曲线与错开
//
// 时长没变（250ms），**曲线改成全应用的统一曲线**，而且**淡入和位移错开**
// （位移先走、淡入后到）。出处和理由在 DESIGN.md §五，代码在
// `internal/ui/widgets/motion`（曲线）与 `toastFadeDelay`（错开多少）。
package ui

import (
	"slices"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// toastLimit 是同时在屏幕上最多几条。多出来的把最旧的挤掉。
	toastLimit = 3
	// playbarClearance 是提示要抬过多高的那条底边。Biu 的 `toastOffset` 是 90。
	//
	// ⚠️ **90 就是 90**：此前这里是 106（90+16），多出来的那 16 是「右边也留
	// 16」时顺手一起加的——于是提示比上游高了 16（票 #180 的 G23）。
	playbarClearance = space.Px90

	// toastWidth 是卡片多宽（上游 `@heroui/theme` 的 toast：356）。
	//
	// ⚠️ 这是**外框宽**（框架的 `Width` 是 border-box：内边距算在里面）。
	toastWidth = 356

	// toastPad 是卡片自己的内边距（上游 `p-3` = 12，四边一样）。
	//
	// ⚠️ 此前是「上下 12、左右 16」——那是照着别处的卡片抄的，上游那个 toast
	// 四边都是 12。
	toastPad = space.Px12

	// toastRadius 是卡片的圆角。**这一条上游没写死**（HeroUI 那个 toast 用
	// 的是 `rounded-large`），而此前这一处是 10：不是本票要改的，就不动它。
	toastRadius = 10

	// toastRight 是卡片离窗口右边多远。
	//
	// ⚠️ 上游是「4~8」（`@heroui/theme` 的 toast 与 `app.tsx:167-175` 的参数
	// 一起算出来的那一档），取 **8**：宽一点的那一头，靠边一点更像上游。
	toastRight = space.Px8

	// toastGap 是两条提示之间的距离（上游那几条之间是 8）。
	toastGap = space.Px8

	// toastEnterTime 是入场动画的时长（DESIGN.md「动效」：Toast 入场 250ms；
	// 上游是 `appearance-in 250ms ease-out`）。
	toastEnterTime = 250 * time.Millisecond

	// toastFadeDelay 是**淡入比位移晚开始**的那一段，占整段入场的比例。
	//
	// ⚠️ **淡入和位移不是同一条时间线**：《Functional Motion》
	// （https://wxad.design/abc/functional-motion）——「动画的**透明度与位移应该
	// 是不同步的**」「**在添加时，透明度应该是后变化的**」。手册⓪ 也写着同一句
	// （「不透明度与位移应该是不同步的」）。合成一条线的话，位移到位时透明度
	// 早就满了——那一帧看起来是「啪地出现」，而那一段位移是白走的。
	//
	// **20% 是暂定的**：那两篇只给了原则（「后变化」），没给数字。取 20% 是因为
	// 统一曲线前重后轻（50% 的时间走完 95%），延迟再大就「滑进来」看不见了，
	// 再小就看不出错开。
	toastFadeDelay = 0.2

	// toastEnterShift 是入场时卡片从右边滑进来的距离（上游那一档是 100，
	// 但那是在屏幕中间弹的；我们这一条贴着右边，从右边滑 16 就够看出来）。
	//
	// ⚠️ **它不能大到压住播放条**：卡片底边本来就只比播放条高 2px（抬 90、
	// 播放条 76），往下挪一点就压上去了——所以这一段是**横着**滑的。
	toastEnterShift = 16
)

// toastLifetime 是一条提示待多久。Biu 的 timeout 是 2000ms。
//
// 它是 var 不是 const：测试要把它改短，不然每条都干等 2 秒。
var toastLifetime = 2 * time.Second

// toastHoverPoll 是「指针压着它」的时候隔多久问一次。
//
// ⚠️ **不用更长**：指针一离开，那条提示就该收走——问得越稀，「松手之后它还赖在
// 屏幕上」的时间越长。100ms 是「看不出来」的粒度。
const toastHoverPoll = 100 * time.Millisecond

// toast 是一条提示。id 用来在到点时认出「是自己那一条」——文字可能重复。
type toast struct {
	id   uint64
	text string
	// tone 是这句话的语义：卡片按它取底色（见 `internal/notice`）。
	tone notice.Tone
	// until 是它到点被收走的时刻（**真时钟**，见 `addToast`）。
	until time.Time
	// enterAt 是它开始入场的时刻（**引擎钟**）。零值 = 还没入过场。
	//
	// ⚠️ **两个钟不是同一个**：到期靠后台那条 goroutine 真睡（真时钟），
	// 而入场是画面上的动画（引擎钟——它才听「减少动态效果」，也才推得动）。
	// 见 `toastEnterAt`。
	enterAt time.Time
	// hovered 是「指针正压着这一条」。
	//
	// ⚠️ **主线程写、后台那条 goroutine 读**（`expire` 要靠它决定收不收）——
	// 所以它跟 `s.toasts` 一样在 `toastMu` 底下。
	hovered bool
}

// addToast 弹一条提示。多到 3 条时把最旧的挤掉。
//
// tone 是这句话的语义（成功 / 警告 / 失败 / 普通），**每个调用点都要给**：
// 上游那 164 处调用是按调用给 `color` 的，而「下载失败」也是绿的正是这一条
// 要收掉的毛病（票 #180）。
//
// 🔴 **它同时也是一句「刚刚发生了什么」的播报**（无障碍，见 announce.go）：右下角
// 弹出来的那张卡片，**读屏软件用户看不见**——他们没有余光去扫右下角。所以这一句
// 还要被**念出来**。
//
// ⚠️ **这一句是「发生了什么」，不是「这个元素是什么」**：卡片上写的是
// 「下载完成」「收藏不了：…」这样一句话，而 `.Label(...)` 给的是「这一枚是什么」
// （框架的 `Label` 是给读屏软件的名字，见 `flatbutton.go` 的警告）。两件事不许混。
//
// ⚠️ **后台那条线也调它**（比如 `download_mount.go` 的 `sayDownloadDone`），而播报
// 那一头是带锁的（`announceQueue` 自己有锁），所以这里不用管自己在哪条线上。
func (s *Shell) addToast(text string, tone notice.Tone) {
	s.addToastFor(text, tone, toastLifetime)
}

// addToastFor 是 `addToast` 加一个「待多久」。
//
// ⚠️ **为什么有些话要待得久一点**：一条提示默认两秒（上游 Biu 的
// `timeout: 2000`），而那两秒是给「用户正在看界面、按完一个键」准备的。
// 但**系统交给我们的文件被拒**那一类不是用户按出来的——他双击了访达里的
// 一个文件，然后**注意力已经回到别处了**（或者他正等着歌放起来）。两秒之后
// 那句话就没了，而「没看到」和「没有提示」在用户那里是同一件事。
//
// 那一类话走 `openFileErrorLifetime`（五秒，见 `openfile.go`）。
func (s *Shell) addToastFor(text string, tone notice.Tone, lifetime time.Duration) {
	s.toastMu.Lock()
	s.toastSeq++
	t := toast{
		id:    s.toastSeq,
		text:  text,
		tone:  tone,
		until: time.Now().Add(lifetime),
	}
	s.toasts = append(s.toasts, t)
	if len(s.toasts) > toastLimit {
		s.toasts = s.toasts[1:]
	}
	s.toastMu.Unlock()

	// 念出去（无障碍）。**排在解锁之后**：播报那一头有自己的锁，而这一段
	// 不该在握着 toast 那把锁的时候去拿另一把。
	s.announce(text)

	// 🔴 **要一帧**（铁律三：投递完状态必须叫醒界面）。
	//
	// ⚠️ **少了这一行，那句话在画面上不存在**（这一票第二轮评审抓到的那个
	// 静默故障）：`addToast` 只写状态，而**按需绘制的界面不会因为状态变了
	// 就自己画一帧**——`invalidate` 只在**收走**那一刻才调（`expire` 里那两
	// 句），于是新加的那一条要等到「下一个别的原因碰巧重画界面」时才出现。
	//
	// 而「下一个别的原因」可能一直没有（界面静止的时候一帧都不画）。
	// 症状是「用户双击了一个放不了的文件，什么都没发生」——而界面上**连
	// 状态都记着**（`s.toasts` 里那句话在），只有画面上没有。
	//
	// ⚠️ **用 `invalidate` 而不是 `invalidateNow`**：`invalidate` 会问「这一
	// 扇窗现在看不看得见」，而看不见的窗**不该**为一条提示重画（票 #156 的
	// 风扇问题）。`invalidateNow` 会绕过那道闸门。
	s.wake.invalidate()

	go s.expire(t)
}

// expire 等到点就把这一条收走，然后叫醒界面。
//
// # ⚠️ 指针压着它的时候**不收**
//
// 《Functional Motion》：「当鼠标移入时，所有 Message 将会展开」「悬停时不会
// 消失」——用户多半是照着这一条去按底下的那一行，它在指针还在上面的时候就消失
// 了，那句话就没用了。
//
// 所以这一条在**被指着**的时候一直留着，等指针走了才按原定的时刻收走（到点那一刻
// 剩下的时间照旧走完——「暂停」而不是「重来」）。
//
// ⚠️ **收走之后必须调 invalidate**。少了这一行，这条提示会一直挂在屏幕上，
// 直到别的原因碰巧重画了界面为止——「只投递不唤醒 = 0 帧刷新」实测踩过。
func (s *Shell) expire(t toast) {
	for {
		s.toastMu.Lock()
		cur, ok := s.toastByID(t.id)
		if !ok {
			// 已经被人收走了（切路由清空、或者别人先收的）——没什么可等的了。
			s.toastMu.Unlock()
			return
		}
		wait, hovered := time.Until(cur.until), cur.hovered
		s.toastMu.Unlock()

		if hovered {
			// 指针压着它：先不收，等下一轮再问。
			//
			// ⚠️ **这一轮要叫醒界面**：指针移走的时候，框架**不一定**给这一条发一帧
			//（卡片是 PassThrough 的，不在指针的命中链里——见 `viewToast`），所以
			// 「指针还压着吗」这个答案要靠我们自己 periodic 地问一次才拿得到。
			// 不问的话，那一条会一直赖在屏幕上（直到别的原因碰巧重画了界面）。
			//
			// ⚠️ **代价是「指着它的时候界面在 10 帧每秒地重画」**：所以这一轮只
			// 在**指着**的时候才问——指针一走，这一条就按原定的时刻收走。
			time.Sleep(toastHoverPoll)
			s.wake.invalidate()
			continue
		}
		if wait > 0 {
			time.Sleep(wait)
			continue
		}
		s.dropToast(t.id)
		s.wake.invalidate()
		return
	}
}

// toastByID 在锁底下找那一条。找不到就是没有了。
func (s *Shell) toastByID(id uint64) (toast, bool) {
	for _, t := range s.toasts {
		if t.id == id {
			return t, true
		}
	}
	return toast{}, false
}

// setToastHovered 记下「指针是不是压在这一条上」。
//
// ⚠️ **要加锁**：这个标记是**后台那条 goroutine** 也要读的（`expire` 靠它决定
// 收不收），而它是在主线程上写的——不加锁就是数据竞争。
func (s *Shell) setToastHovered(id uint64, hovered bool) {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	for i := range s.toasts {
		if s.toasts[i].id == id {
			s.toasts[i].hovered = hovered
			return
		}
	}
}

// dropToast 收走 id 那一条。
func (s *Shell) dropToast(id uint64) {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	for i := range s.toasts {
		if s.toasts[i].id == id {
			s.toasts = slices.Delete(s.toasts, i, i+1)
			return
		}
	}
}

// clearToasts 清空全部提示。切路由时调：上一页的提示不该留到下一页。
func (s *Shell) clearToasts() {
	s.toastMu.Lock()
	s.toasts = nil
	s.toastMu.Unlock()
}

// visibleToasts 是这一帧该画的几条，顺手把到点的收走。
func (s *Shell) visibleToasts() []toast {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	now := time.Now()
	kept := s.toasts[:0]
	for _, t := range s.toasts {
		// ⚠️ **指针压着的那一条留着**：它到点之后**不立刻消失**，要等指针走了
		// （见 `expire`）——这里要是按到期时刻把它藏了，那条提示会**凭空消失**
		// （后台还没收，屏幕上先没了），而用户正指着它。
		if t.hovered || t.until.After(now) {
			kept = append(kept, t)
		}
	}
	s.toasts = kept
	// 复制一份再给出去：s.toasts 的底层数组下一帧会被 reuse。
	return slices.Clone(kept)
}

// toastEnterAt 报这一条开始入场的时刻，并在它**第一次被画出来**时记下那一刻。
//
// ⚠️ **入场是从「第一次被画出来」那一刻算的，不是从「被加进来」那一刻**：
// 加进来的那一帧可能还没有画面（窗口看不见、或者这一帧还没轮到），从那一刻
// 算起的话，等这一帧真的画出来时动画早就走完了——用户看见的是「啪」地一下
// 出现，入场等于没有。
//
// ⚠️ **这个钟是引擎钟**（`Context.Now()`，也就是无头测试里那只推得动的钟）：
// 入场是画面上的动画，而动画要听「减少动态效果」、要能被推钟验中间态。
// 到点被收走用的是**真时钟**（后台那条 goroutine 真睡），两个钟各管各的。
func (s *Shell) toastEnterAt(id uint64, now time.Time) time.Time {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	for i := range s.toasts {
		if s.toasts[i].id != id {
			continue
		}
		if s.toasts[i].enterAt.IsZero() {
			s.toasts[i].enterAt = now
		}
		return s.toasts[i].enterAt
	}
	return now
}

// viewToasts 把提示浮在右下角、播放条上面。
//
// 用 Overlay 铺一层铺满窗口的空容器，而不是在主布局里塞一段留白——提示不该
// 把底下的内容顶开。
func (s *Shell) viewToasts(c *ui.Context) {
	list := s.visibleToasts()
	if len(list) == 0 {
		return
	}
	t := c.Theme()

	ui.Overlay(c, func() {
		// Key 要写在构造之前：同一帧里这个浮层只建一次，key 用来在下一帧
		// 认出是它。
		ui.Column(c).Key("toasts").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.End).AlignItems(ui.End).Gap(toastGap).
			// 底边留出播放条的高度（90），右边留 8（上游那一档是 4~8）。
			// Padding 的四个参数是上、右、下、左，别把 90 写成了右边距。
			Padding(space.None, toastRight, playbarClearance, space.None).
			// 空的地方让鼠标穿过去，不然会挡住底下侧栏和播放条的点击。
			PassThrough().
			Children(func() {
				for _, item := range list {
					s.viewToast(c, item, t)
				}
			})
	})
}

// viewToast 一条提示的样子。
//
// ⚠️ 卡片底色用**语义色**（`toastColors`），**不要用 Theme.Inverse**：
// 主题没设它的时候它是零值，等于 Background —— 白底白卡片，元素排版排得好好的，
// 屏幕上什么也看不见（头less Tester 只查元素框，查不到这个坑）。
//
// 🔴 **卡片本身也 `PassThrough`**——和外面那个铺满窗口的容器一样（见 viewToasts）。
// 一条提示**一个控件都没有**：它不可点、不可关、也不带任何动作。挡在它底下的东西
// 却是实打实的可点控件，而提示在右下角、抬过播放条之后正好压住页面右下角那一块
// （设置页「快捷键」那一页最后那行的 × 就在那儿）。
//
// 症状是「刚改完一个设置，右下角弹出一句提示，两秒之内那个 × 点不动」——而提示
// 两秒后自己没了，于是这件事看起来像是那个 × 偶尔失灵，最容易被当成界面坏了。
//
// 框架那一边也是同一个做法：它自己的 tooltip 也是 `Absolute().PassThrough()`
// （`mygo/ui/tooltip.go:80`）——同样的话，同样没有控件可点。
func (s *Shell) viewToast(c *ui.Context, t toast, theme *ui.Theme) {
	bg, fg := toastColors(t.tone, theme)

	card := ui.Column(c).Width(toastWidth).Radius(toastRadius).PassThrough().
		Background(bg).Padding(toastPad).
		Children(func() {
			ui.Text(c, t.text).FontSize(scale.Font(c, scale.ToastFont)).
				FontWeight(weightMedium).TextColor(fg).PassThrough()
		})

	// ⚠️ **悬停宽限**：指针压在这一条上时它不收走（见 `expire`）。
	//
	// 卡片是 **PassThrough** 的（不挡底下的点击），所以它拿不到悬停事件——只能
	// **按位置**判：`PointerPosition` 给的是指针相对这一条的位置和「在不在上面」。
	//
	// ⚠️ **必须在建元素的这一帧问**：框架的元素句柄**跨帧就作废**
	// （`mygo/ui/frame.go` 的 `expired`），帧外问会 panic。
	_, _, over := card.PointerPosition()
	s.setToastHovered(t.id, over)

	// 入场：250ms 的一段（DESIGN.md「动效」）。淡入 + 从右边滑进来。
	//
	// ⚠️ **淡入和位移错开**（位移先走、淡入后到）：两段进度分开算，
	// 理由见 `toastFadeDelay`。
	//
	// ⚠️ **卡片是「画」上去的，位置是「排」上去的**：滑动用的是 `Translate`
	// （视觉上的位移），而**布局不动**——于是「离右边 8、抬过底部 90」这一档
	// 在动画的每一帧都是真的（量元素框量到的是布局框）。
	move, fade := s.toastEnterProgress(c, t)
	if fade < 1 {
		card = card.Opacity(fade)
	}
	if move < 1 {
		card = card.Translate((1-move)*toastEnterShift, 0)
	}
	// ⚠️ **这一段换不了 `AnimatePose`**（票 #276 查过）。
	//
	// 表面上它是满屏那两条的同一种形状（淡出 + 位移，都能进一个 pose）——但**淡入
	// 和位移是错开的**（位移先走、淡入后到，两段进度分开算，见 `toastFadeDelay`），
	// 而**一个 pose 的五个通道共用一条 `Animation`**（一条曲线、一个延迟）。
	// ⇒ 错开的那一段表达不出来。
	//
	// 而只把淡入换成 pose、位移留在 `Translate` 又是**白换**：位移那一条还在
	// view 里被读，每帧照样重建整棵界面（判据：换完之后那个动画值还会不会被
	// view 读——会读就白换）。
	//
	// ⚠️ **顺带记一条**：这一段之所以要 `c.AnimationFrame()`（重建那一档）而不
	// 是 `p.AnimationFrame()`（只重绘），正是因为这两个属性是**建的时候**写死在
	// 节点上的（见下面那一段注释）——**而 pose 那条路恰好没有这个毛病**：
	// pose 的值是在 commit 里采的、在只重绘的那些帧里也会重新算
	// （`mygo/ui/pose.go` 的 `repose`）。所以哪天这一段能拆成一条时间线，
	// pose 就是它的答案。
}

// toastColors 是这一档语义的底色和压在这个底色上的字色。
//
// ⚠️ **字色不是白的**：白字压在成功绿（`#17c964`）上只有 2.19:1，压在警告
// 橙（`#f5a524`）上只有 2.04:1——都读不清（DESIGN.md §四 那张表里「成功 浅
// 2.19:1」「警告 浅 2.04:1」就是它们）。深色档 `#18181b` 压上去分别是
// **8.08:1** 和 **8.68:1**，读得清。
//
// ⚠️ **危险色上是白的**：白字压在 `#f31260` 上是 4.15:1，比改之前的深色档
// `#ef4444`（3.8:1）还高一点；而深色档是 4.27:1——两个几乎一样。选白是因为
// 确认框那颗红色的「确认」键早就是白字（`internal/ui/confirmmodal.go` 的
// `dialogOnDanger`），两处红底上的字色一样。
//
// ⚠️ **主色那一档的字色跟着主题走**（`theme.AccentText`）：用户把主色换成
// 深的就该配白字，换成亮的就该配深字——那是 `buildTheme` 里 `applyAccent`
// 算好的（浅色下压到 ≥4.5:1），这里不另算一份。
func toastColors(tone notice.Tone, theme *ui.Theme) (bg, fg ui.Color) {
	switch tone {
	case notice.Success:
		return theme.Success, readableOn(theme.Success, toastOnSemantic)
	case notice.Warning:
		return theme.Warning, readableOn(theme.Warning, toastOnSemantic)
	case notice.Danger:
		return theme.Danger, readableOn(theme.Danger, toastOnDanger)
	}
	return theme.Accent, theme.AccentText
}

// toastOnSemantic 是压在成功绿 / 警告橙上的字色。
//
// 深色档 `#18181b`（不是 `theme.Text`——深色主题下 `theme.Text` 是浅色的，
// 压在这两个饱和底色上读不清）。
var toastOnSemantic = ui.RGB(24, 24, 27)

// toastOnDanger 是压在危险红上的字色：白。
var toastOnDanger = ui.RGB(255, 255, 255)

// toastEnterProgress 是这一条入场走到哪儿了：**位移**和**淡入分开**算。
//
// 返回的两个数都是 0~1：move 是位移走到哪儿了，fade 是透明度走到哪儿了。
//
// ⚠️ **两个不是同一条线**：位移先走、淡入后到（理由见 `toastFadeDelay`）。
// 合成一条线的话，位移到位时透明度早就满了——那一帧看起来是「啪地出现」。
//
// ⚠️ **「减少动态效果」时直接到终点**（DESIGN.md §五）：系统开着那个偏好时
// 一点都不动——所以这一档返回 1，而且**不请求下一帧**（请求了就是白画）。
func (s *Shell) toastEnterProgress(c *ui.Context, t toast) (move, fade float32) {
	if c.Preferences().ReduceMotion {
		return 1, 1
	}
	now := c.Now()
	age := now.Sub(s.toastEnterAt(t.id, now))
	if age <= 0 {
		age = 0
	}
	if age >= toastEnterTime {
		return 1, 1
	}
	// 动画还没走完：这一帧画完还要再画一帧。
	//
	// ⚠️ **少了这一句，提示会停在半路**：即时模式是按需绘制，没有人在这个
	// 时候要帧，动画就停在那儿——而那一帧是「刚出现」（几乎全透明），于是
	// 提示看上去一直没出来。
	//
	// ⚠️ **这一句不能改成 `p.AnimationFrame()`**（票 #392 的那一批里，别处都
	// 改了，就这一处没改——**理由不是「忘了」，是它要的东西不同**）：局部重绘
	// （`mygo/ui/runtime.go` 的 `repaintFrame`）**只重画、不重建**，于是
	// **元素身上的属性不会变**——而这一条提示的入场是**节点自己的两个属性**
	// （`Opacity(fade)` 和 `Translate`，见 `viewToast`）：它们是**建的时候**
	// 写死在节点上的，局部重绘那一帧不会重新写一遍，于是提示会**停在它被
	// 重建那一次的位置上**（淡入到一半就停住，位移也停住）。
	//
	// 而**画的那一层**（`Painter`）没有「不透明度」这个入口——`p.Transform`
	// 只影响 `Draw` 回调自己画的东西，而这张卡片的底色和字都是框架画的。
	// 要让这一段走局部重绘，就得把整张卡片改成我们自己画（底色 + 字），那
	// 一条路会丢掉字重和读屏软件读到的文字——不值。
	//
	// 而它**也不值得**改：这一句是**每条提示 250ms**（约十五帧），省下来的
	// 是十五次整窗重建。
	c.AnimationFrame()
	p := float32(age) / float32(toastEnterTime)
	// 位移：全程 250ms，走统一曲线。
	move = motion.Unified(p)
	// 淡入：晚 `toastFadeDelay` 开始，但**和位移同时结束**（总时长不变）。
	q := (p - toastFadeDelay) / (1 - toastFadeDelay)
	if q < 0 {
		q = 0
	}
	fade = motion.Unified(q)
	return move, fade
}
