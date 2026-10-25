package app

// 队列的**item 级重排**：把队列里的一项挪到另一项的前面。
//
// 这是「所有同类都有、我们连 API 都没有」的那一条（报告 §3.3 的 B5）：我们原来
// 只有 `Move`——它挪的是**播放指针**，不是列表的顺序。
//
// 两条规矩抄自成熟播放器：
//
//   - **松手才提交、整批原子替换**（foobar2000 的 `sdk/queue.html`）。拖动的
//     过程中队列一个字节都不动——拖一下就改一次队列的话，用户每帧都在被改，
//     而「正在放的那首」会跟着乱跳。框架的 `ListState.Reorder` 正好是这个
//     形状：拖动时它只画一条插入线，松手那一刻才把结果交给我们。
//   - **重排之后要修正 `currentIndex`**（Nuclear 的三分支）。不修正的话
//     「正在放的那首」会错位——指针停在原来的下标上，而那一格已经是别的歌了。
//     症状是**静默**的：播放条上写着 A，抽屉里高亮的是 B，而响着的还是 A。

// MoveSongBefore 把队列里判重标识等于 id 的那些项整批挪到 before 那一项**之前**。
//
// - **id 是空串**：认不出是哪一首，不动（返回 false）。挪了也对不上任何一行。
// - **before 是空串**：挪到队尾。
// - 队列里没有 id、或者 before 那一项不在队列里：返回 false，队列不动。
//
// ⚠️ **挪的是那首歌的全部份数**：同一首在队列里可能排了好几遍（用户从两个页面
// 各加了一次），而抽屉里它只画一行（见 queueRowsFor 的去重）。拖那一行挪的是
// **那首歌**，不是它的某一份——只挪一份的话抽屉上那一行会留在原地（去重留下
// 的还是第一份），用户看到的是「拖了没反应」。
//
// ⚠️ **正在放的那首跟着走**（Nuclear 的三分支修正）：
//
//	挪的就是它          → 指针落到它的新位置。
//	挪的是它前面/后面的  → 指针跟着挪，始终指着同一首。
//
// 这三条之外一律不动：**洗牌袋和历史栈都不用动**——它们记的都是**判重标识**
// （不是下标），而重排改的只是顺序：歌还是那几首，身份没变。理由见下面那几句。
func (q *Queue) MoveSongBefore(id, before string) bool {
	q.mu.Lock()
	ok := q.moveSongBeforeLocked(id, before)
	q.mu.Unlock()
	if ok {
		// 队列的顺序变了：落盘那一格得跟着变（changed 必须在锁外面调，见
		// Queue.changed 的注释）。
		q.changed()
		// 顺序变了 → **下一首是谁**可能变了，之前备好的那份地址作废。
		q.invalidate()
	}
	return ok
}

// moveSongBeforeLocked 是 MoveSongBefore 的那一份（锁已经拿好了）。
func (q *Queue) moveSongBeforeLocked(id, before string) bool {

	n := len(q.items)
	if n == 0 || id == "" || id == before {
		return false
	}

	// 找出要挪的那几项（同一首歌在队列里可能排了好几遍），并记下它们在队列里
	// 的位置。
	var block []Item
	moved := make([]bool, n)
	for i, it := range q.items {
		if it.Identity() == id {
			block = append(block, it)
			moved[i] = true
		}
	}
	if len(block) == 0 {
		return false
	}

	// 找插入点：before 那一项在队列里的**第一份**（抽屉也是按第一份画的）。
	insertAt := n // 默认队尾
	if before != "" {
		insertAt = -1
		for i, it := range q.items {
			if it.Identity() == before {
				insertAt = i
				break
			}
		}
		if insertAt < 0 {
			return false
		}
	}

	// 块在**新**队列里的起点：插入点之前那些没被挪走的项有几个。
	blockStart := 0
	for i := 0; i < insertAt; i++ {
		if !moved[i] {
			blockStart++
		}
	}

	// 重建队列，并算出「正在放的那首」的新下标（Nuclear 的三分支）。
	//
	// ⚠️ **指针认的是「同一首」不是「同一个格子」**：挪走的那一块整体换了位置，
	// 指针要跟着它走（挪的就是它），而别的歌挪来挪去时指针也得跟着挪一格，
	// 否则它指着的就是另一首了。
	newItems := make([]Item, 0, n)
	newCur := -1
	for i := 0; i < n; i++ {
		if i == insertAt {
			newItems = append(newItems, block...)
		}
		if moved[i] {
			if i == q.cur {
				// 挪的就是正在放的那首：指针跟着它走。
				newCur = blockStart
			}
			continue
		}
		if i == q.cur {
			// 别的歌挪走了：指针跟着挪（它前面的项少了几个，它就往前挪几个）。
			newCur = len(newItems)
		}
		newItems = append(newItems, q.items[i])
	}
	if insertAt >= n {
		// 挪到队尾：块接在最后（insertAt 是 n 时上面的循环走不到它）。
		newItems = append(newItems, block...)
	}

	q.items = newItems
	q.cur = newCur
	// ⚠️ **洗牌袋不用动**：它记的是判重标识，而重排改的是**顺序**——歌还是那
	// 几首，身份没变，袋子里的那一列照样对得上（见 queue_shuffle.go）。
	//
	// **历史栈也不用动**：它记的也是判重标识（那一列退回来的时候按标识对回
	// 队列，见 historyPopLocked）。
	return true
}
