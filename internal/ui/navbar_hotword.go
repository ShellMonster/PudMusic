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
//     本来可以一帧都不画。
//   - **换词那一帧之外零分配**：占位词直接取词池里的那个字符串（`hotwordPlaceholder`
//     只做下标取模和一次 TrimSpace），不现拼。
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
// 定时器回调只调 `wake.invalidate`（clock.go 的硬规矩），「到没到点」由 view
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
}

// syncHotwords 收「这一帧搜索框的热词占位怎么办」：定时器装不装、到没到点换词。
// **每帧调一次**（`viewSearchBox` 里）；满屏开着的时候顶栏不画（票 #161），它不跑
// ——那一支的收尾在 `syncUnderlay` 里（满屏一开就把表收掉，不为看不见的东西醒着）。
func (s *Shell) syncHotwords(now time.Time) {
	if s.closed {
		s.stopHotword()
		return
	}
	hw := s.app.HotwordsOf()
	// 框里有字：占位词根本看不见，不续也不换。Refresh 是每帧可调的（TTL 在
	// 业务层），但为一个看不见的东西白问是白问——框空了它自然会续上。
	if strings.TrimSpace(s.searchKeyword) != "" {
		s.stopHotword()
		return
	}
	hw.Refresh(s.wake.invalidate)
	words := hw.Words()
	// 没词、或者焦点在框里（用户正要打字）：不装表也不换（理由见文件头）。
	if len(words) == 0 || s.searchFocused {
		s.stopHotword()
		return
	}
	if s.hotword.stop == nil {
		s.hotword.stop = s.clock.EveryFunc(searchHotWordRotate, s.wake.invalidate)
		s.hotword.lastRotate = now
	}
	if now.Sub(s.hotword.lastRotate) >= searchHotWordRotate {
		s.hotword.idx = (s.hotword.idx + 1) % len(words)
		s.hotword.lastRotate = now
	}
}

// stopHotword 收掉轮换的定时器。幂等（`closePerWindow` 和每帧的收条件都调它）。
func (s *Shell) stopHotword() {
	if s.hotword.stop != nil {
		s.hotword.stop()
		s.hotword.stop = nil
	}
}

// hotwordPlaceholder 是这一帧搜索框该显示的占位词：框空着且有词池时是轮到的
// 那一个热词，否则是「搜索」。测试也读它——占位词画在框架的输入框里，无头
// Tester 的 `Find`/`Texts` 看不见它（那是 a11y 的 Label，不是文字节点），
// 所以「画出来的是哪个词」靠像素比对，「该是哪个词」靠这一个纯函数。
func (s *Shell) hotwordPlaceholder() string {
	if strings.TrimSpace(s.searchKeyword) != "" {
		return searchLabel
	}
	words := s.app.HotwordsOf().Words()
	if len(words) == 0 {
		return searchLabel
	}
	return words[s.hotword.idx%len(words)]
}
