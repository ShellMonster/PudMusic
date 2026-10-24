package imgcache

import "container/list"

// lru 是**按字节**算的最近最少用表，不是按张数。
//
// 为什么不能按张数：Biu 的封面尺寸从 56×56 的播放条小图到 672×378 的大图
// 都有，单张解码后的 RGBA 加 mipmap 在 0.15MB ~ 1.3MB 之间（`verify-image-and-window.md`
// 实测 672×378 WebP ≈ 0.97MB + 0.33MB mipmap）。「最多 64 张」在 56×56 上
// 只有 9MB，在原图上就是几百 MB —— 这个数字约束不了任何东西。字节上限才
// 是真的内存上限。
//
// ⚠️ **上限是多少，取决于「存下来的那一张图有多大」**（见 `scale.go`）：
// 存的是 1920×1080 的原图时一张就是 11MB，一屏十几张就是 170MB，缓存
// 装不下 → LRU 只能把**正在显示着的**挤掉 → 界面上封面反复闪（issue #208）。
//
// 它只管记账：谁最近用过、谁该被挤掉。挤掉之后做什么是 Cache 的事。
type lru struct {
	max   int64
	used  int64
	order *list.List // 元素是 *entry，最近用的在前面
	byURL map[string]*list.Element
}

func newLRU(maxBytes int64) *lru {
	return &lru{max: maxBytes, order: list.New(), byURL: make(map[string]*list.Element)}
}

// touch 记一次使用，把 e 提到最前面。
func (l *lru) touch(e *entry) {
	if el, ok := l.byURL[e.url]; ok {
		if el.Value.(*entry) == e {
			if el != l.order.Front() {
				l.order.MoveToFront(el)
			}
			return
		}
		// 表里是同一个 URL 的**旧条目**（失败表到上限挤掉的那一个，或者
		// 被取消的那一个）。不能只把旧的提到前面：那样新的这一条永远不在
		// 账上，它的字节数记进 used 却挤不掉，而旧的会一直占着 URL 表。
		// 把旧条目归零拿掉，换新的。
		l.order.Remove(el)
		old := el.Value.(*entry)
		l.used -= old.bytes
		old.bytes = 0
		delete(l.byURL, e.url)
	}
	l.byURL[e.url] = l.order.PushFront(e)
}

// resize 改一次 e 的记账值（解码完才知道它多大），并返回该被挤掉的那些。
//
// ⚠️ **e 可能已经不在表里了**：从「有人要它」到「解完码」之间隔着几百毫秒
// （一次下载 + 一次解码），这中间它可能被挤掉（内存一紧就会），而挤掉之后
// 它已经没人要了。这时候再给它记一笔账，`used` 就比表里真正占着的多——
// 而**那一笔再也挤不掉**（表里已经没有它了，没有一个「挤掉它」的动作能把它
// 从 used 里减掉）。
//
// 后果不是「内存多占一点」：挤的循环最后会把整张表清空还挤不下（表空了
// `used` 还是正的），之后**每来一张图就把表里剩下的全挤掉**——一张封面刚
// 解出来就被挤掉、退回占位、重新下一次（实测 16 张封面下了 7000 多次，
// 界面上就是封面反复闪，issue #208）。
//
// 所以这里只给**还在表里**的那一条记账。
//
// frame 是**这一帧的号**（见 `Cache.BeginFrame`）：它用来认出**画面上正画着**
// 的那几张——挤的时候不许碰它们（票 #358，理由见 `visibleFrames`）。
func (l *lru) resize(e *entry, bytes int64, frame uint64) (evicted []*entry) {
	el, ok := l.byURL[e.url]
	if !ok || el.Value.(*entry) != e {
		// 已经不在表里了：这一张没人要了，它解码出来的那份内存会跟着条目
		// 一起被回收（条目已经不在 `entries` 里了），不记账。
		return nil
	}
	l.used += bytes - e.bytes
	e.bytes = bytes

	// ⚠️ **刚解出来的这一张要排到最前面**：LRU 挤掉的是最久没用的那一张，
	// 而「刚解码完」并不比「刚画过」更久——它下载的这几百毫秒里，屏幕上
	// 别的封面被画了几十遍。不排到前面的话，它会被当成最久没用的那一张
	// 挤掉（实测：它刚解出来，就被自己那一次记账挤掉了）。
	if el != l.order.Front() {
		l.order.MoveToFront(el)
	}

	// 挤：从最久没用的那一头开始。
	for cur := l.order.Back(); cur != nil && l.used > l.max; {
		victim := cur.Value.(*entry)
		if victim.bytes == 0 {
			// 这一张**不占预算**（还在下，或者下失败了）：把它挤掉对预算
			// 一分钱都不省，只是把正在下的一次扔了——下一次有人要它，它又
			// 从零开始，而「从零开始」在界面上就是「退回占位」。
			//
			// 所以 0 字节的条目不参与挤。它们留在表里（`used` 不算它们，
			// 挤的循环自然跳过），等它们自己有了字节、或者被取消。
			cur = cur.Prev()
			continue
		}
		if victim.onScreen(frame) {
			// 🔴 **画面上正画着的那一张不许挤**（票 #358）。
			//
			// 挤掉它之后，它下一帧就退回「正在加载」的占位——于是那一格
			// **闪一下**，然后重新下一次、再解一次码。用户看到的就是「开
			// 队列抽屉的时候，中间那一列的封面来回闪」。
			//
			// 为什么抽屉一开就会撞上：抽屉里那十几行**第一次露出来**，
			// 一下子全都要封面（加上别的页面已经攒下的那几十张），字节
			// 上限立刻超了；挤的循环从最久没用的那头一路挤过来，
			// **画面上正画着的那几张也在里面**（它们只是「这一帧刚被画
			// 过」，在表里排得靠前，但前面还堵着刚要下来的那十几张 0 字
			// 节的——那十几张不占预算，挤的循环跳过它们继续往前，于是就
			// 挤到了画面上那几张）。
			//
			// 所以这里**跳过**它们：宁可缓存超一点预算（超的只是屏幕上
			// 看得见的那几张），也不要让用户看见封面闪。
			cur = cur.Prev()
			continue
		}
		l.order.Remove(cur)
		delete(l.byURL, victim.url)
		l.used -= victim.bytes
		victim.bytes = 0
		evicted = append(evicted, victim)
		cur = l.order.Back()
	}
	return evicted
}

// drop 把 e 从表里拿掉，记账归零。它已经不在表里时是个空操作。
func (l *lru) drop(e *entry) {
	if el, ok := l.byURL[e.url]; ok && el.Value.(*entry) == e {
		l.order.Remove(el)
		delete(l.byURL, e.url)
	}
	l.used -= e.bytes
	e.bytes = 0
}
