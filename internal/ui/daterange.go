// 日期范围筛选的状态：历史记录页的日期弹层（`library_pud.go` 的 `viewPudRange`）
// 把用户挑的区间写进这里，页面据此换查询条件。
//
// 旧的「四个预设按钮 + 双月日历」画法在 T7 换成 pud 的日期弹层之后没有任何生产调用，
// 已经连同它的测试一起删掉；这里只剩状态本身。
//
// # 换筛选条件一律回到第 1 页
//
// 上游两个页面都是 `refreshList()`：清空、回顶、从第 1 页重来。`viewPudRange` 返回
// 「变了」让页面去 `Reload`，那一步做的正是这件事。
package ui

import "time"

// presetAll 是「不按日期筛」那一档的下标。
const presetAll = 0

// dateFilter 是一个日期范围筛选条件。
//
// 零值不可用，请用 newDateFilter：两个端点里得先有**真的日期**——零值时刻
// 是「1/1/1」（公元元年），那不是「没选」，那是坏了。
type dateFilter struct {
	// from / to 是区间的两端。
	from, to time.Time
	// on 报现在有没有在按日期筛。
	on bool
}

// newDateFilter 建一个「不按日期筛」的筛选器。
//
// now 是用来把区间填成今天的那一下时钟——测试传假时钟，这一页上的
// 「今天」就永远是那一天，不会随测试跑的时间变。
func newDateFilter(now time.Time) *dateFilter {
	d := startOfDay(now)
	return &dateFilter{from: d, to: endOfDay(d)}
}

// Range 是这一帧生效的区间。on 为 false 时两端都返回零值（不筛）。
func (f *dateFilter) Range() (from, to time.Time, on bool) {
	if !f.on {
		return time.Time{}, time.Time{}, false
	}
	return f.from, f.to, true
}

// setPreset 换到「不按日期筛」（presetAll）：区间收成今天，免得两端停在公元元年。
func (f *dateFilter) setPreset(i int, now time.Time) {
	if i != presetAll {
		return
	}
	f.on = false
	d := startOfDay(now)
	f.from, f.to = d, endOfDay(d)
}

// setRange 把区间写进页面的筛选状态。
func (f *dateFilter) setRange(from, to time.Time, on bool) {
	f.on = on
	f.from, f.to = from, to
}

// startOfDay 是那一天 00:00:00（**本地时区**：用户挑的是他看日历上写的那些
// 日期，用 UTC 换算会让东八区的用户在晚上八点之后看到的是「昨天」）。
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// endOfDay 是那一天 23:59:59。用 23:59:59 而不是「第二天 00:00:00」：接口收
// 的是闭区间，第二天零点会把「今天 23:59:59.5 听的那一条」漏掉——而那正是
// 用户刚听完、正要加进稍后再看的那一条。
func endOfDay(t time.Time) time.Time {
	return startOfDay(t).Add(24*time.Hour - time.Second)
}

// sameDay 报 a 和 b 是不是同一天。
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
