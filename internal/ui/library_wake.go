// 跨 goroutine 的那两小摊：后台投进来的信箱，和每个画过这个列表的窗口的唤醒
// 句柄。
//
// 🔴 铁律三在这里落地：**后台投递完状态必须唤醒界面，而且要逐窗口通知**。
// 即时模式是按需绘制的——不唤醒，界面永远停在旧帧（实测：只投递不唤醒 = 0 帧
// 刷新）；只唤醒主窗口，迷你窗口就停在旧内容。
//
// 单独一个文件是因为它们有**两条硬规矩**和其余那一层不一样：
//
//   - 信箱里的每一样东西都有一个 `apply(*pagedRows)`，**一律在界面线程上跑**
//     （adopt 在 view 开头调）。后台 goroutine 只造一个出来投进去，绝不碰界面
//     上的状态——那是铁律一，违反它的后果是 `-race` 报一个看起来毫不相关的字段。
//   - 认窗口靠 `ui.Services` 的相等规则（同一个窗口的两次 Services 相等，换一个
//     窗口就不相等），所以同一窗口每帧重登记只占一条。
package ui

import (
	"log"
	"slices"

	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// mailboxItem 是投进信箱的一样东西。
type mailboxItem interface{ apply(*pagedRows) }

// wakeEntry 是一个窗口的唤醒句柄。
type wakeEntry struct {
	// svc 是这个窗口的身份（认它用），wake 是叫醒它的动作。
	svc  ui.Services
	wake func()
	seen uint64
}

// libraryResult 是后台拉完一页之后投进信箱的东西。
type libraryResult struct {
	gen     uint64
	page    int
	rows    []libraryRow
	hasMore bool
	err     error
}

// apply 把一页结果落到界面的状态上（见 pagedRows.apply）。
func (r libraryResult) apply(p *pagedRows) { p.applyPage(r) }

// removeResult 是后台删完一行之后投进信箱的东西。
type removeResult struct {
	// gen 是发这一次删除时的代次。对不上就是换了查询之后回来的，作用在**新的
	// 一批行**上，删的会是同名的另一条——B 站那边已经删掉了，但界面上不该动它。
	gen uint64
	id  string
	err error
}

// apply 把「删掉了」落到界面上：成功就从列表里拿走那一行，失败说一句话。
//
// ⚠️ 这一段**只能跑在界面线程上**（adopt 在 view 开头调）：失败要说一句话，
// 成功要改列表的切片，都是界面状态。
func (r removeResult) apply(p *pagedRows) {
	if r.err != nil {
		if p.acts.Notify != nil {
			p.acts.Notify("删不掉："+r.err.Error(), notice.Danger)
		}
		return
	}
	if r.gen != p.gen {
		return // 这一行早就换掉了，别去动新的那一批。
	}
	p.Drop(r.id)
}

// clearViewedResult 是后台清完「已看完」的那些之后投进信箱的东西。
type clearViewedResult struct {
	// gen 是发这一次清除时的代次。对不上就是换了查询之后回来的——那一次清空
	// 动的是**另一个查询**的结果，不该拿它去动现在这一批行。
	gen uint64
	err error
}

// apply 把「清完了」落到界面上：重新拉一遍列表，说一句话。
//
// ⚠️ **要重新拉一遍**：清掉的是 **B 站那边**的清单，界面上的行还摆着——不重拉
// 的话用户看着那些已看完的歌还在列表里，而它们其实已经没了。
func (r clearViewedResult) apply(p *pagedRows) {
	if r.err != nil {
		if p.acts.Notify != nil {
			p.acts.Notify("清不掉："+r.err.Error(), notice.Danger)
		}
		return
	}
	if r.gen != p.gen {
		return // 换查询之前发出去的那一次，别去动新的那一批。
	}
	p.Reload()
	if p.acts.Notify != nil {
		p.acts.Notify("清掉了已看完的", notice.Success)
	}
}

// deliver 把一页结果投进信箱，然后叫醒界面。任何 goroutine 都能调。
//
// 🔴 铁律三：投递完**必须**唤醒（不唤醒实测 0 帧刷新）；只唤醒一个窗口就是
// 另一个窗口停在旧帧，所以是逐窗口通知。
func (p *pagedRows) deliver(r mailboxItem) {
	p.mu.Lock()
	p.box = append(p.box, r)
	p.mu.Unlock()

	p.invalidate()
}

// adopt 取走信箱里的结果，一条一条落到界面的状态上。只能由界面线程调。
func (p *pagedRows) adopt() {
	p.mu.Lock()
	box := p.box
	p.box = nil
	p.mu.Unlock()

	for _, r := range box {
		r.apply(p)
	}
}

// applyPage 落一页结果。
//
// 🔴 **锁在这一行解开，而且只有这一行解**（`p.loading = false`）：请求失败、
// 超时、返回不对、拉页器自己崩了，全走这一条。漏了它就是「界面永远转圈」。
func (p *pagedRows) applyPage(r libraryResult) {
	if r.gen != p.gen {
		return // 换查询之前发出去的那一页，作废。
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false

	if r.err != nil {
		// 解了锁，但**不再自动重试**：不然就是自己打自己（见 blocked）。
		p.err = r.err
		p.blocked = true
		log.Printf("biu: 第 %d 页拉失败（%s）：%v", r.page, p.why, r.err)
		if p.acts.Notify != nil {
			// 用 `bili.Friendly` 而不是 `err.Error()`：后者在界面上是
			// 「bili: /x/web-interface/history/search 返回 -101：账号未登录」，
			// 前者是一句「登录已经过期了，重新扫码登录一下」。同一个错，两种说法，
			// 用户只会觉得应用自己也不知道出了什么事。
			p.acts.Notify("加载更多失败："+bili.Friendly(r.err), notice.Danger)
		}
		return
	}

	p.err = nil
	p.blocked = false
	p.page = r.page
	p.hasMore = r.hasMore
	if len(r.rows) == 0 {
		// 一页空的还说「还有」：接口在骗人，或者已经到头了。再拉就是空转。
		p.hasMore = false
	}
	// 🔴 铁律四：追加是**尾部**插 key（换查询重建才是头部插，两条规则不一样）。
	// 尾部追加不动已有行的位置，所以「选中的那一行」翻完页还是它。
	for _, m := range r.rows {
		if p.seen[m.ID] {
			continue // 翻页重叠：两个一样的 ID 就是两行共用一份状态。
		}
		p.seen[m.ID] = true
		p.rows = append(p.rows, m)
	}
	p.rev++
}

// setWake 登记这一帧的窗口。只能由界面线程调。
func (p *pagedRows) setWake(svc ui.Services) {
	wake := svc.Invalidate // 方法值：一个窗口一条，所以每帧一个闭包不算多

	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	p.frame++
	for i := range p.wakes {
		// 认窗口靠 svc：同一个窗口的两次 Services 相等，换一个窗口就不相等。
		if p.wakes[i].svc == svc {
			p.wakes[i].wake, p.wakes[i].seen = wake, p.frame
			return
		}
	}
	if len(p.wakes) >= maxWakes {
		oldest := 0
		for i := range p.wakes {
			if p.wakes[i].seen < p.wakes[oldest].seen {
				oldest = i
			}
		}
		p.wakes = append(p.wakes[:oldest], p.wakes[oldest+1:]...)
	}
	p.wakes = append(p.wakes, wakeEntry{svc: svc, wake: wake, seen: p.frame})
}

// invalidate 叫醒所有画过这个列表的窗口。任何 goroutine 都能调。
func (p *pagedRows) invalidate() {
	p.wakeMu.Lock()
	wakes := slices.Clone(p.wakes)
	p.wakeMu.Unlock()

	for _, w := range wakes {
		if w.wake != nil {
			w.wake()
		}
	}
}
