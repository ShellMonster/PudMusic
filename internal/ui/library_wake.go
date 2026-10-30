// 跨 goroutine 的操作信箱。
//
// 翻页结果和逐窗口唤醒由 widgets/list.Pager 统一处理；这里仅保留删除与清除操作的
// 结果，因为它们改变 libraryRows 的本地模型而不是分页生命周期。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/notice"
)

// mailboxItem 是投进信箱的一样东西。
type mailboxItem interface{ apply(*pagedRows) }

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
	if r.gen != p.pager.Generation() {
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
	if r.gen != p.pager.Generation() {
		return // 换查询之前发出去的那一次，别去动新的那一批。
	}
	p.Reload()
	if p.acts.Notify != nil {
		p.acts.Notify("清掉了已看完的", notice.Success)
	}
}

// deliver 把操作结果投进信箱，然后叫醒共享 pager 登记过的窗口。任何 goroutine 都能调。
func (p *pagedRows) deliver(r mailboxItem) {
	p.mu.Lock()
	p.box = append(p.box, r)
	p.mu.Unlock()

	p.pager.Invalidate()
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
