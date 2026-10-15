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
		if el != l.order.Front() {
			l.order.MoveToFront(el)
		}
		return
	}
	l.byURL[e.url] = l.order.PushFront(e)
}

// resize 改一次 e 的记账值（解码完才知道它多大），并返回该被挤掉的那些。
func (l *lru) resize(e *entry, bytes int64) (evicted []*entry) {
	l.used += bytes - e.bytes
	e.bytes = bytes
	for el := l.order.Back(); el != nil && l.used > l.max; el = l.order.Back() {
		victim := el.Value.(*entry)
		l.order.Remove(el)
		delete(l.byURL, victim.url)
		l.used -= victim.bytes
		victim.bytes = 0
		evicted = append(evicted, victim)
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
