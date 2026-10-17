// 进度怎么算：这一趟下了多少、下到哪儿了、有多快、以及**什么时候算「回退全量」**。
//
// 它和 `update.go` 里那个相位机是分开的，因为它们变的原因不同：相位机跟「用户
// 做了什么」变（按检查、按下载、按退出），这一份跟**下载器怎么报数**变。而后者
// 有一处特别要盯住——框架的进度回调在增量包失败改下整个归档时，`total` 会从
// 「增量包多大」变成「整个安装包多大」，见 `updateState.observe`。

package app

import "time"

// reset 把一次下载的痕迹清掉。
func (s *updateState) reset() {
	s.transferred, s.total, s.fellBack = 0, 0, false
	s.lastDownloaded, s.sampled = 0, false
	s.err = ""
}

// observe 收一次进度采样。
//
// **回退是怎么认出来的**（见文件头 §增量）：框架下增量包时 `total` 是增量包
// 的大小，失败改下全量时 `total` 变成整个安装包的大小——于是 `total` 变了，
// 或者 `downloaded` 比上一次**小了**，都只可能是「这一趟重新开始了」。
//
// 第一个采样点是**建立基线**，不算回退：一个刚点「下载更新」的正常下载，它的
// 第一个采样点也有 `total`（框架拿清单里的 `Size` 当总量传下来），要是把它
// 也算成回退，那**每一次**下载都会弹一句「增量用不了，改下全量」——一句每次
// 都错的话比不说更糟，它会让用户对整个更新功能失去信任。
//
// ⚠️ 拿 u.mu 的时候调。见 check 的注释。
func (s *updateState) observe(now time.Time, downloaded, total int64) {
	if !s.sampled {
		// 这一趟的第一个采样点：记下它的总量当基线。
		s.sampled = true
	} else if (total > 0 && total != s.total) || downloaded < s.lastDownloaded {
		// 增量补丁没能把新版本拼出来，框架改下整个归档（`installUpdate` 里
		// `log.Printf("mygo: downloading the whole update…")` 那一句之后）。
		// 对用户来说那是**进度条往回走**，得说出来。
		s.fellBack = true
		s.speed.reset()
	}
	s.total = total
	s.transferred = downloaded
	s.lastDownloaded = downloaded
	s.speed.sample(now, downloaded)
}

// percentOf 算出 0~100 的百分比。
//
// 两个边界都要夹住：`total` 是 0（还没探到大小）时是 0 而不是 +Inf，
// `transferred` 超了 `total` 时夹到 100——夹住的理由不是「不可能发生」，而是
// 进度条画出去会满到框外面去，而那种一帧的畸形会被用户记成「这个应用有问题」。
func percentOf(transferred, total int64) float64 {
	if total <= 0 {
		return 0
	}
	p := float64(transferred) / float64(total) * 100
	switch {
	case p < 0:
		return 0
	case p > 100:
		return 100
	}
	return p
}

// speedWindow 是算速度时回看多少个采样点。
//
// 要**多于一个**采样点：拿相邻两帧算，每一帧的抖动就直接印在那个数字上，用户
// 会看见 `3.1 MB/s → 0.0 MB/s → 7.4 MB/s` 来回跳。5 个点（框架的进度回调最快
// 100ms 一个）是半秒多的窗口，够稳又不至于慢得跟不上真实速度的变化。
const speedWindow = 5

type speedSample struct {
	at    time.Time
	bytes int64
}

// speedMeter 按最近几个采样点算下载速度。
//
// 不是累计平均：用户看到的是「**现在**有多快」，而平均速度在下载快结束时一路
// 往下滑（最后那几秒只剩一点点，摊到全程里越摊越慢），那个下滑比抖动更让人
// 以为「是不是卡住了」。
type speedMeter struct {
	samples []speedSample
}

// sample 记一个采样点，丢掉窗口外的。
func (m *speedMeter) sample(at time.Time, n int64) {
	m.samples = append(m.samples, speedSample{at, n})
	if len(m.samples) > speedWindow {
		m.samples = m.samples[len(m.samples)-speedWindow:]
	}
}

// reset 清掉采样。下一次从头算，而不是接着上一次那趟的字节数接着除。
func (m *speedMeter) reset() { m.samples = nil }

// speed 返回此刻的字节/秒。采样点不足两个或者时间没走过时是 0。
func (m *speedMeter) speed(now time.Time) int64 {
	if len(m.samples) < 2 {
		return 0
	}
	first, last := m.samples[0], m.samples[len(m.samples)-1]
	d := last.at.Sub(first.at)
	if d <= 0 {
		return 0
	}
	if v := float64(last.bytes-first.bytes) / d.Seconds(); v > 0 {
		return int64(v)
	}
	return 0
}
