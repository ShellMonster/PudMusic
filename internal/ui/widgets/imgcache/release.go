package imgcache

// 票 #673：把长期没人要的封面像素放掉，需要的时候再解回来。
//
// # 为什么不是 `ui.Bitmap.ReleasePixels`
//
// `mygo/ui/bitmap.go` 的 `ReleasePixels` **只对 `ui.DecodeBitmap` 造的 bitmap
// 有效**：它靠 `decode` 闭包把像素再造出来，而 `DecodeBitmap` 为了那个闭包会把
// **整份压缩字节**留在 bitmap 上（一张封面几百 KB）。本包**故意不用**
// `DecodeBitmap`（见 `scale.go`），走的是 `ui.NewBitmap` —— 它没有 `decode`，
// `ReleasePixels` 在它上面**什么都不做**（bitmap.go 原话："A bitmap NewBitmap
// made … has nothing else to make them from and keeps them"）。
//
// 所以这里的「释放」是：**把条目上那个 `*ui.Bitmap` 整个放掉**。没人再引用它，
// 像素和 mipmap 链跟着被回收；条目自己留着（`entries` 里那一条），记着「这一张
// 要重新解」。重新解走的是**原来那条加载链路**：先问磁盘缓存（生产上
// `Options.Dir` 一直开着，一张 672×378 的 WebP 才几十 KB），盘上没有才重新
// 下载——尺寸档位、DPR、缩放、EXIF 摆正全都不变，因为走的是同一个 `fetch` /
// `decodeStored`。
//
// # GPU texture 那一半
//
// 放掉 Bitmap 之后，框架的渲染器（`mygo/internal/gpu/*`）在**连续 240 帧没画
// 过**之后自己 `sweep` 掉那张 texture（每 120 帧扫一次，见 metal.go 的
// `sweep`）。我们这一层管的是 CPU 那份 RGBA（一张 768×432 是 1.33MB，加上
// mipmap 约 1.77MB——那才是 32MiB 预算记的东西）；GPU 那份框架自己会放。
//
// # 判据用的是帧号，不是墙钟
//
// 阈值是「**连续多少帧没被 `Get`**」，和 `visibleFrames` / `onScreen` 用的是
// 同一个帧号（`Cache.BeginFrame`）。为什么不用墙钟：帧号在测试里是确定的
// （假钟推一帧就是一帧），而墙钟会让「什么时候放」取决于机器负载——本仓库的
// 判据纪律里那条「不用固定毫秒」说的就是这个。
//
// ⚠️ **两扇窗共用一个帧号**（主窗口和迷你播放器）：迷你窗口放歌时每 50ms 醒
// 一次，它数的帧也推进这个号。所以同一个阈值在两边的时间尺度不一样——
// 主窗口滚动时（60fps）120 帧约 2 秒，只有迷你窗口在醒（20Hz）时约 6 秒，
// 正在播放的主窗口（1Hz 唤醒）约 2 分钟。
//
// 这个差别不要紧：**画面上正画着的那几张每一帧都会被 `Get` 一次**（view 里
// 每一格都问自己的图），所以两扇窗里看得见的那几张 `drawn` 一直是新的，永远
// 轮不到释放；会被释放的只有「已经滚出视口、而且两个窗口都不再要」的那些。
// 而「窗口收起来、一帧都不画」的时候这个清扫根本不跑（没有 `BeginFrame`），
// 也就不会因为「没人画」而把谁放掉。
//
// # 释放之后界面上是什么样
//
// 那一格退回**正在加载的占位**（`State.Loading`），下一帧开始从盘上重新解，
// 解完恢复成图。这和「第一次见到这张封面」是同一档，不是失败态——失败态只有
// 重新解真的失败了才会出现（404 / 字节坏了）。

const (
	// DefaultReleaseAfterFrames 是「连续多少帧没被要过就把像素放掉」的默认阈值。
	//
	// 120 帧：主窗口滚动时（60fps）约 2 秒，只有迷你播放器在醒时（20Hz）约
	// 6 秒。这个数要**远大于** `visibleFrames`（4）——那几张是画面上正画着的，
	// 一帧都不能放；也要**远小于**「用户已经滚到别处去了」的时间尺度，否则
	// 32MiB 预算里的那些像素会一直躺着。
	//
	// ⚠️ **调小它换来的是更频繁的重新解码**：每次回看一张放掉的封面都要
	// 读盘 + 解码 + 缩放（实测一张 672×378 的 WebP 约 15~37ms，见 `scale.go`
	// 与 `jpegstream.go` 的注释），全在后台解码 worker 上，但界面上会先出现
	// 一两帧占位。调大它换来的就是更久的常驻——这一票要压的正是后者。
	DefaultReleaseAfterFrames = 120

	// defaultMaxReleased 是「已经放掉像素、还留着记录」的条目**条数**上限。
	//
	// 这些条目没有字节数（像素已经放了），所以 `lru` 的字节上限挤不掉它们。
	// 不设上限的话，滚过几千张封面就留下几千个条目——一个条目约 200 字节
	// （地址串、锁、几个整数字段），几千个就是几百 KB 到 1MB，属于「不报错、
	// 只是慢慢变大」的那一类。512 条约 100KB，相对 32MiB 的像素预算可以忽略。
	//
	// 到上限之后**最老的那一条整个忘掉**（从 `entries` / `released` 里删掉）：
	// 下次再要它就是一次干净的首次加载，行为和「从没缓存过」一样。
	//
	// ⚠️ 比 `defaultMaxFailed`（256）大：成功的封面远多于失败的，而放掉像素
	// 是常态，不是异常。
	defaultMaxReleased = 512
)

// releaseIdle 把「连续 releaseAfter 帧没被要过」的封面像素放掉。
//
// 由 `Cache.BeginFrame` 每帧调一次。调用方保证 frame 是**这一帧**的号（>= 1）。
//
// # 为什么整表扫一遍，而不是只看队尾
//
// `lru.order` 近似按「最近一次被要」排，但**不是严格序**：`lru.resize`（刚
// 解码完的那一条）会把条目**提到队头**而不动 `drawn`——于是一条很早就被要过、
// 刚解出来的条目可能排在「更近被要过」的那几条前面。所以不能「碰到第一条没
// 到阈值的就停」，得逐条按它自己的 `drawn` 判。
//
// 代价是每帧一次全表扫描（生产上一份缓存二三十条、每次两个锁），和 32MiB
// 像素预算相比可以忽略；而它换掉的是「漏放」那一类不报错、只慢慢变大的问题。
//
// 放掉之后这一条**从 LRU 表里拿掉**（`lru.drop`）：它的字节已经归零，留着只会
// 让下一次挤的循环白走一遍；而 `entries` 里那一条还在，所以同一个地址再来要
// 的时候仍然只有一条记录（不会重复下载）。
func (c *Cache) releaseIdle(frame uint64) {
	if c == nil || c.releaseAfter <= 0 || frame == 0 || c.closed.Load() {
		return
	}
	var forgotten []*entry
	c.mu.Lock()
	for cur := c.lru.order.Back(); cur != nil; {
		prev := cur.Prev()
		e := cur.Value.(*entry)
		// 画面上正画着的一律不动（阈值比 `visibleFrames` 小时，这一道是唯一
		// 的保护——见 `TestCoversOnScreenAreNeverReleased`）。
		if !e.onScreen(frame) && e.idleFrames(frame) >= uint64(c.releaseAfter) {
			if c.takePixelsLocked(e) {
				c.lru.drop(e)
				forgotten = c.noteReleasedLocked(e, forgotten)
			}
		}
		cur = prev
	}
	c.mu.Unlock()

	// `discard` 拿 entry 自己的锁，放在 c.mu 之外（锁序：c.mu → entry.mu）。
	for _, e := range forgotten {
		e.discard()
	}
}

// takePixelsLocked 放掉 e 的像素。调用方持 c.mu（见 releaseIdle 的锁序说明）。
//
// 返回 true 表示真的放掉了；false 表示这一条没有像素可放（还在下、下失败了、
// 或者已经被别人放掉了）。
func (c *Cache) takePixelsLocked(e *entry) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bm == nil || e.dropped || e.fetching {
		return false
	}
	e.bm = nil
	e.err = nil
	e.done = false
	e.released = true
	c.releases.Add(1)
	return true
}

// noteReleasedLocked 把 e 记进「已经放掉像素」的那本账，并在超上限时把最老的
// 一条整个忘掉（返回给调用方去 `discard`）。调用方持 c.mu。
//
// ⚠️ **正在重新解码的那一条要跳过**：`reloadReleased` 是先置 `fetching`、再
// 把这一条从本账里摘掉，中间有一个极短的窗口它同时「在账上」和「正在解」。
// 这时候把它当牺牲品丢掉会把一次正在跑的加载掐掉（`discard`）——虽然下一次
// `Get` 会重新建一条，但那一帧白等。所以看到 `fetching` 就只从账上摘掉、不动它。
func (c *Cache) noteReleasedLocked(e *entry, forgotten []*entry) []*entry {
	if _, ok := c.released[e]; !ok {
		c.released[e] = c.relOrder.PushBack(e)
	}
	for len(c.released) > c.maxReleased {
		el := c.relOrder.Front()
		if el == nil {
			break
		}
		victim := el.Value.(*entry)
		c.relOrder.Remove(el)
		delete(c.released, victim)

		victim.mu.Lock()
		fetching := victim.fetching
		victim.mu.Unlock()
		if fetching {
			continue // 已经被重新要了：它不再是「放掉的那一批」
		}
		c.entries.CompareAndDelete(victim.url, victim)
		c.lru.drop(victim) // 已经不在表里了，这一句是幂等的
		forgotten = append(forgotten, victim)
	}
	return forgotten
}

// forgetReleased 把 e 从「已经放掉像素」那本账里摘掉（取消的时候用）。
func (c *Cache) forgetReleased(e *entry) {
	c.mu.Lock()
	if el, ok := c.released[e]; ok {
		c.relOrder.Remove(el)
		delete(c.released, e)
	}
	c.mu.Unlock()
}

// reloadReleased 在一条「像素已经放掉」的条目被重新要的时候，起一次后台加载。
//
// 它只做一件事：把这一条从「放掉」状态**换回加载中**，然后交给原来那条
// `start` → `fetch`（先盘后网）→ `decodeWorker` → `finish` 的链路。所以尺寸
// 档位、DPR、磁盘缓存、失败重试全都是同一套代码，没有第二条加载路径。
//
// ⚠️ **并发的两次 Get 只会起一次**：`fetching` 在 entry 的锁里判并置，
// 第二个人看到它就不再起（主窗口和迷你窗口共用一个缓存，同一个地址被两处
// 同时要过是常事）。
//
// ⚠️ **缓存已经关掉时落成 `ErrClosed`**：不然那一格会永远停在「加载中」——
// 下载永远不会来（`start` 拒绝了），而失败态才是「拿不到」的正确表达。
// ⚠️ **失败之后不会反复起加载**：`done` 置上之后 `state()` 报的是失败态，而
// `reloadReleased` 的判据里有 `e.done`——重试由退避那条路管（和第一次加载
// 失败完全一样），不是每次 `Get` 再打一遍图床。
func (c *Cache) reloadReleased(e *entry) {
	e.mu.Lock()
	if !e.released || e.fetching || e.dropped || e.done {
		e.mu.Unlock()
		return
	}
	e.fetching = true
	e.mu.Unlock()

	c.forgetReleased(e)

	if !c.start(e) {
		e.mu.Lock()
		e.fetching = false
		e.released = false
		e.done = true
		e.err = ErrClosed
		e.mu.Unlock()
	}
}

// idleFrames 报这一张**连续多少帧没被要过**（frame 是当前帧号）。
//
// `drawn` 是最后一次被要的帧号（见 `entry.drawn`）。它可能是 0——那是「在
// `BeginFrame` 之前建的」，那时候帧号还没开始数；这种条目按**建出来的那一帧**
// （`born`）算，于是「启动时先建、第一帧才开始画」的那几张不会被立刻放掉。
func (e *entry) idleFrames(frame uint64) uint64 {
	last := e.drawn
	if last == 0 {
		last = e.born
	}
	if frame <= last {
		return 0
	}
	return frame - last
}
