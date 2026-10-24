// 状态变化的播报（无障碍）。
//
// # 这一层只攒句子，不碰界面
//
// 应用层不认识界面（`internal/app` 的包注释第一条），所以它不碰任何界面的
// 东西：它只把「后台发生了一件事」翻译成一句**人话**，交给界面层去念给读屏
// 软件听。
//
// ⚠️ **它和 `internal/notice` 是两件事**：`notice` 说的是「这句话是成功还是
// 失败」，而那一档是要**画成一张卡片**的（颜色是界面层的事，见
// `internal/ui/toast.go` 的 `toastColors`）；这里说的是「这一句要被念出来」。
// 两者碰巧都带一句话，但去处不一样——所以这里不共用一个类型。
//
// # 为什么是逐窗口送，而不是攒一份让界面来取
//
// 两个窗口共用同一个 `App`（主窗口和迷你播放器，见 `miniplayer.go`），而
// 「取走」是**有破坏性**的：谁先画一帧谁就取走了，于是迷你开着的时候那一句
// 可能被收起来的主窗口吃掉，迷你窗口永远听不到。
//
// 挨个送就没有这个问题：两扇窗都拿到同一句，而只有**看得见**的那一扇会把它
// 念出来（看不见的窗口连帧都不画，见 `internal/ui/wake.go` 的规则三）。
//
// # 念出来的是「发生了什么」，不是「这个元素是什么」
//
// 「播放失败」「下载完成」是**状态变化**；「播放」「下载」那一栏叫什么、那枚
// 按钮叫什么是**元素的名字**，那是 `.Label(...)` 的事（框架的 `Label` 给读屏
// 软件的是「这一枚是什么」，见 `internal/ui/flatbutton.go` 的警告）。两件事
// 混起来的后果是读屏软件把一屏控件名字念一遍，而**一件正在发生的事**一句
// 也没有——那正是这一票要消掉的毛病。
package app

import "slices"

// announceSink 是一扇窗的播报句柄：界面层装一个，App 往里送要念的句子。
type announceSink func(text string)

// announceEntry 是一个句柄，外加一个用来摘掉它的号。
//
// 号是必要的：函数值不能比（只能和 nil 比），所以「关窗时摘掉自己那一条」
// 只能靠一个能比较的东西认（同 `wakeEntry` 的做法，见 `app.go`）。
type announceEntry struct {
	tok uint64
	fn  announceSink
}

// announce 把一句话送给每一扇窗，让它们念给读屏软件听。
//
// ⚠️ **空串不送**：框架的 `Context.Announce` 自己也会挡空串，但这里更早挡掉——
// 一句空话在读屏软件那里是「什么也没说」，而在别处它却像一句说过的话。
//
// ⚠️ **只有应用层自己用**：界面层那一边的话走它自己的那一栏
// （`internal/ui/announce.go`）——提示是**这一扇窗**的事，而这里的话是
// **整件事**的事（两扇窗都该听见）。
func (a *App) announce(text string) {
	if text == "" {
		return
	}
	// 拷贝一份再送：**不许拿着锁去调界面层的东西**。那是别的包的代码，
	// 而它回头再调 App 的话就是当场死锁——死锁在这里的表现是「整个应用冻住」。
	a.announceMu.Lock()
	sinks := slices.Clone(a.announces)
	a.announceMu.Unlock()
	for _, e := range sinks {
		e.fn(text)
	}
}

// AddAnnounce 登记一扇窗的播报句柄，返回摘掉它的那一个。
//
// **每个窗口一个**：主窗口一个、迷你播放器那一个（见 `miniplayer.go`）。
// 摘掉的那一支由界面层在关窗时调（`internal/ui/shell.go` 的 `closePerWindow`）——
// 不摘的话「进迷你 / 退出迷你」来回十次之后那个列表就有十条，每发生一件事
// 就多走十趟空转。
func (a *App) AddAnnounce(fn func(text string)) (remove func()) {
	a.announceMu.Lock()
	a.announceSeq++
	e := announceEntry{tok: a.announceSeq, fn: fn}
	a.announces = append(a.announces, e)
	a.announceMu.Unlock()
	return func() {
		a.announceMu.Lock()
		defer a.announceMu.Unlock()
		for i := range a.announces {
			if a.announces[i].tok == e.tok {
				a.announces = slices.Delete(a.announces, i, i+1)
				return
			}
		}
	}
}
