// 搜索框那个轮换的热词占位：框空着的时候，占位词每隔几秒换成网易云热搜词池里
// 的下一个。业务层在 `app/hotwords.go`，接口在 `service/hotwords`。
//
// # 为什么占位词要轮换，而换的节奏是几秒一个
//
// B 站网页的搜索框就是这么做的：占位词按热搜列表轮着换（它自己是定时器直接改
// `input.placeholder`）。间隔取 **5 秒**——B 站网页同档；再快了读不完一个词，
// 再慢了它看起来就不像「在换」。
//
// # 性能：这一块的账只有三笔
//
//   - **定时器只在占位词看得见的时候醒着**（框空、顶栏在画、焦点不在框里、
//     没进满屏）。挂进那个 125ms 一跳的共用防抖钟（`syncSearchWatch`）是行不通的：
//     轮换要的是「每几秒醒一次」，125ms 一跳是每秒白醒八次——这个应用在空闲时
//     本来可以一帧都不画。收表的口子有四个：`syncHotwords` 自己的每帧收条件、
//     满屏（`syncUnderlay`，顶栏不画的那一帧它照跑）、崩溃页（`Shell.View` 的
//     viewCrash 那一支）、关窗（`closePerWindow`）；进迷你播放器时主窗口被藏起来，
//     那一下也收（`openMiniPlayer`），回来重画首帧时照常装回。⌘H 那类系统级隐藏
//     收不到——那时候 `wakeHandle` 自己会把帧丢掉（wake.go 规则三），代价只是一次
//     空醒，和红点那几枚既有定时器同档。
//   - **换词那一帧之外零分配**：唤醒函数在 `New` 里绑一次存着（方法值现取就是
//     每帧一次堆分配——`Refresh` 每帧都调，方法值逃逸进后台闭包）；占位词直接取
//     词池里的那一个字符串（`syncHotwords` 算好存进 `shown`，画的那一半只读它），
//     不现拼。
//   - **网络有 TTL 挡着**（`hotWordsTTL`，10 分钟一份）：每帧都调的 `Refresh`
//     在业务层自带闸门，缓存期内一个请求都不发。
//
// # 换词的判据和装表的判据是同一支
//
// 焦点在框里的时候**不换**：占位词此刻还看得见（框是空的），而词在正要打字的
// 人眼皮底下换掉是打扰。聚焦时把定时器也收掉——不为一个暂时不换的东西醒着。
package ui

import (
	"strings"
	"time"
)

// searchHotWordRotate 是占位热词多久换一个。
const searchHotWordRotate = 5 * time.Second

// hotwordState 是轮换热词的跨帧状态。只由 view 在主线程读写（铁律一）；
// 定时器回调只调 `wakeHandle.invalidate`（clock.go 的硬规矩），「到没到点」由 view
// 拿时钟现算——和红点（`navbar_feed.go`）是同一套「帧驱动 + 到点叫醒」。
type hotwordState struct {
	// idx 是轮到词池的第几个。词池重新拉过之后长度可能变，用的时候永远
	// `idx % len(words)` 取模，不另管「池子缩了」。
	idx int
	// lastRotate 是上一次换词（或最近一次装表）的时刻。装表时也置上——
	// 不然词池刚到的第一帧 `now.Sub(零值)` 巨大，第一个词还没显示满一档就被换掉。
	lastRotate time.Time
	// stop 是那个每 `searchHotWordRotate` 一跳的定时器；nil 是没装。
	stop func()
	// wake 是绑好的一次唤醒函数（`New` 里绑的 `s.wake.invalidate`）。⚠️ **每帧
	// 现取 `s.wake.invalidate` 就是每帧一次堆分配**——方法值逃逸进 `Refresh`
	// 的后台闭包，而 `Refresh` 是每帧都调的；绑一次存着，传它就不分配了。
	wake func()
	// shown 是这一帧 `syncHotwords` 算出来的占位词（空串 = 还没算过，按「搜索」
	// 算）。画的那一半（`viewSearchBox`）只读它，不自己再算一遍。
	shown string
}

// syncHotwords 收「这一帧搜索框的热词占位怎么办」：定时器装不装、到没到点换词、
// 占位词该是哪个。**每帧调一次**（`viewSearchBox` 里）；满屏开着的时候顶栏不画
// （票 #161），它不跑——那一支的收尾在 `syncUnderlay` 里（满屏一开就把表收掉，
// 不为看不见的东西醒着）。
func (s *Shell) syncHotwords(now time.Time) {
	// 默认「搜索」；下面每一支要么留着它，要么换成词池里轮到的那一个。
	s.hotword.shown = ""
	if s.closed {
		s.stopHotword()
		return
	}
	// 框里有字：占位词根本看不见，不续也不换。Refresh 是每帧可调的（TTL 在
	// 业务层），但为一个看不见的东西白问是白问——框空了它自然会续上。
	if strings.TrimSpace(s.searchKeyword) != "" {
		s.stopHotword()
		return
	}
	hw := s.app.HotwordsOf()
	words := hw.Words()
	// **「显示」和「推进」是两个判据**：框空且有词就显示轮到的那一个——聚焦时
	// 它冻着不动（下面不推进），但**不闪回「搜索」**；词在正要打字的人眼皮底下
	// 消失又回来，比它停着不动更打扰。
	if len(words) > 0 {
		s.hotword.shown = words[s.hotword.idx%len(words)]
	}
	hw.Refresh(s.hotword.wake)
	// 没词、或者焦点在框里（用户正要打字）：不装表也不推进（理由见文件头）。
	if len(words) == 0 || s.searchFocused {
		s.stopHotword()
		return
	}
	if s.hotword.stop == nil {
		s.hotword.stop = s.clock.EveryFunc(searchHotWordRotate, s.hotword.wake)
		s.hotword.lastRotate = now
	}
	if now.Sub(s.hotword.lastRotate) >= searchHotWordRotate {
		s.hotword.idx = (s.hotword.idx + 1) % len(words)
		s.hotword.lastRotate = now
	}
	s.hotword.shown = words[s.hotword.idx%len(words)]
}

// stopHotword 收掉轮换的定时器。幂等（`closePerWindow`、满屏、崩溃页、进迷你
// 和每帧的收条件都调它）。
func (s *Shell) stopHotword() {
	if s.hotword.stop != nil {
		s.hotword.stop()
		s.hotword.stop = nil
	}
}

// hotwordPlaceholder 是这一帧搜索框该显示的占位词。测试也读它——占位词画在
// 框架的输入框里，无头 Tester 的 `Find`/`Texts` 看不见它（那是 a11y 的 Label，
// 不是文字节点），所以「画出来的是哪个词」靠像素比对，「该是哪个词」靠这一个
// 纯读。
func (s *Shell) hotwordPlaceholder() string {
	if s.hotword.shown == "" {
		return searchLabel
	}
	return s.hotword.shown
}
