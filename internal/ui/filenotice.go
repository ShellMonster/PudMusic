// 「系统交给我们的那个文件，收下了没有」——那句话的状态。
//
// # 为什么它不只是一条 toast
//
// 🔴 **这一票第二轮评审抓到的那个静默故障就在这儿**：
//
//   - toast 画在**主窗口**的右下角（`viewToasts` 只在主窗口那一支调，
//     `shell_view.go`），而**迷你播放器那一扇根本不画 toast**；
//   - 而迷你开着的时候主窗口是**收着的**——于是「说了话但用户眼前那一扇
//     什么也没有」就发生了，症状和「双击没反应」一模一样。
//
// 所以这一句**还有一份**：**迷你那一扇也画它**（迷你画在自己那一小块里），
// 而**迷你那一份和主窗口那一份是同一个对象**（迷你建外壳时接主窗口那一份，
// 见 `adoptShared`）。
//
// ⚠️ **主窗口那一半仍然是一条 toast**（`addToastFor`）：用户看惯的是右下角
// 那张卡片，而这一句在别的页面上的位置也该和别的提示一样。而**迷你那一半**
// 是**另画一遍**——因为迷你只有 320×84，而且它**不画 toast**。
//
// # 它和 toast 的差别只有两处
//
//  1. **它待得久**（`fileNoticeLifetime`）：系统交给我们的文件被拒那一类
//     不是用户按出来的，他双击了访达里的一个文件然后注意力已经回到别处了。
//  2. **它要一帧**（`Shell.repaint`）：状态写进去了，而按需绘制的界面停在
//     旧帧上——那句话就还不在画面上。
//
// ⚠️ **不要把它接成 `addToast`**：那一层每条提示都要一帧，而无头环境里
// 那一帧要画二十遍（入场动画一直要下一帧，而钟站着不动）。
package ui

import (
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// fileNoticeLifetime 是那句话待多久。
//
// ⚠️ **比普通提示长**：普通那一条两秒（`toastLifetime`），而那两秒是给
// 「用户正在看界面、按完一个键」准备的。系统交给我们的文件被拒不是用户按
// 出来的——两秒之后那句话就没了，而「没看到」和「没有提示」在用户那里是
// 同一件事。
const fileNoticeLifetime = 5 * time.Second

// fileNotice 是「系统交给我们的那个文件」那一句**出错的那一句**。
//
// ⚠️ **它只装错误，不装「正在放」**：成功那一句是主窗口右下角的一条 toast，
// 而迷你那一扇在成功那一档是**让开**的（歌要在主窗口上放）——迷你那一扇上
// 冒出一句「正在放《…》」，会在用户五分钟之后重新开迷你时**突然出现**，
// 而那件事早就过去了。
//
// ⚠️ **而错误那一档迷你不让开**：迷你常常就是用户眼前那一扇，而迷你**不画
// toast**（`viewToasts` 只在主窗口那一支调）——于是「说了话但用户眼前什么
// 也没有」，症状和「双击没反应」一样。
type fileNotice struct {
	mu sync.Mutex
	// text 是那句话本身。
	text string
	// tone 是它的语义（成功 / 失败）。
	tone notice.Tone
	// until 是它到点被收走的时刻（**真时钟**）。
	until time.Time
}

// newFileNotice 建一份空的那一句。
func newFileNotice() *fileNotice { return &fileNotice{} }

// say 挂上那一句。
//
// ⚠️ **它不碰界面**：界面那一半在 `Shell` 上（`Shell.sayFileNotice`），
// 而这一份状态是**两扇窗共用的**——所以「说完之后要一帧」那一件事必须
// 由拿到 `Shell` 的那一半去做（迷你那一扇要的是**它自己**那一帧）。
func (n *fileNotice) say(text string, tone notice.Tone, lifetime time.Duration) {
	n.mu.Lock()
	n.text = text
	n.tone = tone
	n.until = time.Now().Add(lifetime)
	n.mu.Unlock()
}

// current 报此刻该画的那一句（没有就报 false）。
//
// ⚠️ **到点的那一条在这里被收走**：画它的那一帧顺带把它收掉——和 toast 那边
// `visibleToasts` 做的事一样（那里还有一条后台 goroutine 收，见 `expire`）。
// 这一份没有那条 goroutine：它是**两扇窗共用的**，而收它的那个 goroutine
// 该挂在谁身上是个麻烦（迷你关掉之后那条 goroutine 就没人管了）。所以它
// **被画到的时候才收**——而两扇窗里至少有一扇是看得见的（否则用户也不会
// 看见它），所以它一定收得掉。
func (n *fileNotice) current() (text string, tone notice.Tone, ok bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.text == "" {
		return "", 0, false
	}
	if !n.until.After(time.Now()) {
		n.text = ""
		return "", 0, false
	}
	return n.text, n.tone, true
}
