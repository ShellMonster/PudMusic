package imgcache

import "sync"

// 这一块是「**按显示尺寸拼出来的那个地址**」的备忘录（票 #634）。
//
// # 为什么要有它
//
// `sizedURL` 每帧每一格都要拼一次 `@<宽>w_<高>h_1c.webp` 这个后缀——**一屏十二张
// 封面 = 每帧十二次分配**，60 帧每秒就是**每秒 720 次纯垃圾**。而它的结果只
// 取决于三样东西（地址、宽、高），完全可以记好一份。
//
// # 键的粒度和现有的档位机制是对齐的
//
// 键是 **(地址, 宽, 高)**，而那三个宽高**已经是吸过档的**——`coverPixelSize`
// 做的事：长边吸到 `sizeTiers` 的某一档、短边跟比例量化到 1/32。所以拖动窗口
// 时短边只每隔几十像素才变一档，**不会**每拖一个像素就多一个条目。
//
// ⚠️ **别在这份备忘录里再吸一次档**：那会让两个不同的输出共用一个键，于是缓存
// 里存的是**另一个尺寸**的地址——图会拉错（画出来是另一个大小，或者干脆拉不
// 到）。这一层只认「给什么就是什么」。
//
// # 🔴 **改了 `sizedURL` 的输出格式，这张表要作废**
//
// 这一层的键是 **(地址, 宽, 高)**——而 `sizedURL` 是**纯函数**，键是它的**全部
// 输入**。所以它一旦改了输出格式（改后缀、改分隔符、改那一段的写法…），
// **同一个键就会对应两个不同的地址**。
//
// 而 `store` 里那一句 `if _, dup := m.items[k]; dup { return }`——**同一个键
// 不会被覆盖**——于是备忘录里会**留着旧的那一个**，而新的那个**再也存不进去**。
// ⇒ 后果是：图会拉到**旧尺寸**那一档（新格式那个地址只在这一格第一次用到
// 的时候算出来过一次，之后一直用旧的那个）。
//
// ⚠️ **它不报错、不崩溃**：拉到的还是一张图，只是尺寸不对——在「按显示尺寸
// 要图」那个机制里，那意味着**糊了**或者**多要了几倍内存**。
//
// ⇒ 所以：**改了 `sizedURL` 的输出格式，要让这张表作废**——要么把键改掉
// （把「格式的版本」也算进键里），要么清掉这张表（它是**内存**里的，每
// 个进程一份，重启就没了）。
//
// # 上限
//
// 不许无限长：地址那一维是跟着「这一屏见过多少张不同的图」走的，滚一晚上就能
// 攒出几万个条目。所以有一道上限，满了从最老的开始忘（FIFO）。

// sizedKey 是这份备忘录的键。
//
// ⚠️ **宽高必须是「决定那个输出串」的那两个数**——它们得原样进 `sizedURL`，
// 所以这里不吸档（理由见文件头那一段）。
type sizedKey struct {
	src string
	w   int
	h   int
}

// maxSizedURLCache 是这份备忘录的条目上限。
//
// 256 够一屏几十张图加上拖窗口时的那几档；每个条目是一份地址（百来字节），
// 满上也不过几十 KB——和图片缓存本身那几百 MB 相比是舍入误差。
//
// ⚠️ **它必须是有限的**：地址那一维跟着「见过多少张不同的图」走，而那一维
// 没有天花板。
const maxSizedURLCache = 256

// sizedURLCache 是「(地址, 宽, 高) → 拼好的地址」那张表。
//
// ⚠️ **它带自己的锁**：`GetSized` 是界面线程调的，但这份缓存是**一个缓存对象**
// 共享的（迷你窗借的是主窗那一份），所以不能假设只有一个读者。
type sizedURLCache struct {
	mu    sync.Mutex
	items map[sizedKey]string
	// order 是键的**进入顺序**，最老的在前面——满了从那一头挤掉。
	order []sizedKey
}

// lookup 查一份拼好的地址。
//
// 命中时**不产生分配**：那个串已经在表里了，直接拿走它的头（字符串是不可变的，
// 共享同一份底子是对的）。
func (m *sizedURLCache) lookup(k sizedKey) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[k]
	return v, ok
}

// store 记一份拼好的地址。满了就挤掉最老的那几条。
func (m *sizedURLCache) store(k sizedKey, v string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = make(map[sizedKey]string, maxSizedURLCache)
	}
	if _, dup := m.items[k]; dup {
		return
	}
	m.items[k] = v
	m.order = append(m.order, k)
	if len(m.order) > maxSizedURLCache {
		// 挤掉最老的那几条（FIFO）。这一档是「拖窗口时那些一次性条目」的
		// 兜底——它们本来就不该有那么多（见文件头「键的粒度」那一段），而
		// 这一道是最后一道闸：它保证**这张表不会无限长**。
		n := len(m.order) - maxSizedURLCache
		for _, old := range m.order[:n] {
			delete(m.items, old)
		}
		m.order = append(m.order[:0], m.order[n:]...)
	}
}

// lenOf 是这份表现在有几条（测试用）。
func (m *sizedURLCache) lenOf() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}
