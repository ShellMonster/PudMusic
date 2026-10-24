// Package rowkey 记着「算好的一份值」，按「这一次的原料」对上号。
//
// # 为什么要有它
//
// 列表的行身份（`ui.ListState.Key`）和几处显示文字是**每帧每一行**都要算一遍的。
// 而「把一个 string 装进 `any`」是要分配的（`runtime.convTstring`），
// 「把一个时长拼成「3:07」」也是（`fmt.Sprintf`）。
//
// 于是那种「现算」的写法每帧每行多一到三次分配——一屏十来行就是几十次
// （实测见票 #313；调研报告在 `reports/Biu_MyGo/163/report.md` §4.2.2）。
//
// # 它怎么知道「数据换了」
//
// **不靠调用方记得重建**——那是最容易漏的一步，漏了就是铁律四的回归
// （前面插一行，后面所有行的状态整体错配；见 AGENTS.md §4 第 4 条）。
// 每次问的时候先取一次**原料**（取它不分配），跟上次记的比：一样就把算好的
// 那一份直接返回（**零分配**）；不一样就重算一次。
//
// 所以「数据换了」是自动跟上的，而**快的那条路上没有任何分配**。
//
// # 原料必须可比较，而且取得它不能分配
//
// 所以原料得是**已经存在**的东西（一个 ID、一个 mid、一个时长），
// 不能是现拼的字符串——现拼的那一步本身就要分配，而它恰恰是要省掉的那一步。
package rowkey

// Memo 记着一批算好的值。
//
// K 是**原料**（必须可比较——比较一次不分配），V 是**算好的那一份**。
//
// **零值就能用**（不用建）：里面那个切片是空的，第一次问的时候才长出来。
type Memo[K comparable, V any] struct {
	// rows 是每一行记着的那一份。
	rows []memoRow[K, V]
}

// memoRow 是第 i 行记着的那一份。
//
// ⚠️ **set 是「这一格算过没有」**：光靠「val 是不是零值」判不出来——
// 零值**可能**就是算对了的那一份（比如时长的零值写着空串，见
// `widgets/list` 的 `formatDuration`）。所以这一格得单独记一笔。
type memoRow[K comparable, V any] struct {
	raw K
	val V
	set bool
}

// At 返回第 i 行算好的那一份。
//
// raw 是这一行**此刻**的原料；pack 是「原料 → 算好的那一份」的那一步
// （它会分配——所以它只在原料变了的时候才走）。
//
// 原料跟上一次一样就把算好的那一份直接返回（**零分配**）；不一样就重算一次。
//
// ⚠️ **pack 必须是纯函数**：同样的原料必须给出同样的结果。理由是「拿原料当
// 身份」——身份变了才重算，而身份就是原料本身。
func (m *Memo[K, V]) At(i int, raw K, pack func(K) V) V {
	if i < len(m.rows) && m.rows[i].set && m.rows[i].raw == raw {
		return m.rows[i].val
	}
	// 补齐到 i。中间那些格子**不算**（它们还没被问过，算一遍就是白分配一次）——
	// 「算过没有」由 `set` 记着，所以真问起它们的时候算的是它自己那一份。
	for len(m.rows) <= i {
		m.rows = append(m.rows, memoRow[K, V]{})
	}
	m.rows[i] = memoRow[K, V]{raw: raw, val: pack(raw), set: true}
	return m.rows[i].val
}

// Identity 把一个行身份（ID）装进 `any`。
//
// ⚠️ **这一步是分配的**（`runtime.convTstring`）——所以它只在身份变了的时候
// 才走（见 `Memo`）。而框架那一边是零分配的（`mygo/ui/context.go` 的
// `keyedID` 对 string 走 `maphash.String`），所以这一笔**全在我们这一侧**。
func Identity(id string) any { return id }

// Identity64 把一个行身份（数字 ID）装进 `any`。
//
// ⚠️ 同 `Identity`：这一步是分配的（`runtime.convT64`）——所以它只在身份变了
// 的时候才走。而 256 以内的数走的是运行时那张静态表（不分配）。
func Identity64(id int64) any { return id }
