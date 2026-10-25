package app

// 随机模式的那两样：**洗牌袋**（下一首是谁）和**历史栈**（上一首是谁）。
//
// 这一份是独立于播放模式的：不在随机模式时它们是空的（换档时清掉，见
// Queue.SetPlayMode），而「顺序 / 循环 / 单曲」三档的下一首是谁是纯按索引
// 算的（见 Queue.Advance）。
//
// ---- 洗牌袋 ----
//
// 我们原来的随机是「在除掉当前首剩下的 n-1 首里**均匀抽**一个」
// （原来的 pickOtherLocked）：每一首被抽中的机会一样大，但**会重复、会遗漏**——
// 抽过的歌下一轮还可能再来，没抽过的也可能一直不来。用户听得出来：
// 「随机播了一晚上，同一首听了三遍，另一首一首没放过」。
//
// 洗牌袋（Tauon 的 true_shuffle、foobar2000 的 Shuffle、MP2 的 m_random_list）
// 换的是**抽的次序**而不是**抽的概率**：把队列里的每一首打乱成一列，一首一首
// 地发，发完一列再打乱一列。于是一列里**每一首恰好发一次**——不重复，也不
// 遗漏。
//
// ---- 袋子是**池子**，不是快照 ----
//
// 上面那一段说的是「**轮开始时**在队列里的那些歌，这一轮各轮到一次」。而袋子里
// 的这一列是**轮开始时的快照**——这一轮中途才加进队列的歌不在里面，要等下一轮
// 重装才轮到（而票面写的是「随机**不重复、不遗漏**」，那句话对中途加的歌不
// 成立）。
//
// 这一份把它改成**池子**：新加进队列的歌**立刻就在池子里**，这一轮就轮到它。
//
// 这跟行业主流是一个东西（2026-10-25 人定的，理由见票 #387）：
//
//   - **Spotify**：shuffle 是「tracks play randomly」——它从**还没放过的**那些里
//     挑，而新加进队列的歌立刻就在候选里，下一首就有可能抽到它。
//   - **Apple**：随机播放 = 「以随机顺序播放」。
//   - **QQ音乐 / 网易云音乐**：公开讨论里它们的「伪随机」是按播放历史加权、新鲜
//     探索——**仍然是池子**，不是排好的一张表。
//
// 池子和「一张表」的区别只在**新成员进来的时候**：表是排好的（新来的只能等下一
// 轮），池子是活的（新来的立刻能抽到）。
//
// ---- 「下一首播放」压过随机 ----
//
// 「下一首播放」是**用户显式的意图**，它比随机那一档的池子优先（Spotify 社区管
// 这叫 "queue overrides"：手动排的队列压过打乱后的歌单顺序）。所以随机档下按
// 「下一首播放」，**下一首就是它**——不是「池子里随机抽一首」。

// ---- 历史栈 ----
//
// 「上一首」在随机模式下不能按索引退一格：随机模式下队列里的次序和「刚才放过
// 哪些」是两件事（用户听到的顺序是随机的，按索引退一格退回去的是队列里的上
// 一项，可能根本没放过）。历史栈记的就是「刚才放过哪些」。
//
// ---- 这两样记的是**判重标识**，不是下标 ----
//
// 下标会随着队列的改动（加歌、删歌、重排）全变，而判重标识不会：`Item.Identity`
// 就是那个键（`mv:<bvid>` / `audio:<sid>` / `local:<路径>`）。
//
// **将来队列要落盘时，落盘的键也是判重标识**——两边对得上，这两样不用推倒重来。
// 按下标做的话，落盘那一刻这两样就得整个换掉。

// historyLimit 是历史栈最多留几首。
//
// 随机模式放一晚上能压几千项，而「上一首」只看得到顶上那几项——留那么多只是
// 白占内存。200 首够退一百次「上一首」。
const historyLimit = 200

// bagResetLocked 重装洗牌袋：队列里的每一首**歌**进去打乱，**正在放的那一首挪到
// 最后**。
//
// 挪到最后而不是抽掉：抽掉的话这一列就少一首（「不遗漏」破），而挪到最后的话
// 这一列仍然有每一首、每一首恰好轮到一次，且**头一个不是正在放的那首**——
// 不挪的话用户听到的是「刚放完又放一遍」。
//
// ⚠️ **袋里是「歌」不是「项」**：同一首在队列里排了两遍（用户从两个页面各加了
// 一次），袋里也只有一份——放一遍就够了，而连着放两遍同一首用户听得出来。
//
// ⚠️ **Fisher–Yates**：从后往前，每个位置和它前面随机一个位置交换。洗得均匀
// （每一首落在每一格的机会一样大），而且 rnd.Intn 只调 n-1 次。
func (q *Queue) bagResetLocked() {
	seen := make(map[string]bool, len(q.items))
	q.bag = make([]string, 0, len(q.items))
	for _, it := range q.items {
		id := it.Identity()
		// 认不出是哪一首的（判重标识是空串）不进袋：它在队列里对不上任何一行，
		// 抽到它也放不了。
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		q.bag = append(q.bag, id)
	}
	for i := len(q.bag) - 1; i > 0; i-- {
		j := q.rnd.Intn(i + 1)
		q.bag[i], q.bag[j] = q.bag[j], q.bag[i]
	}
	// 正在放的那一首挪到最后。
	q.bagMoveToEndLocked(q.currentIdentityLocked())
}

// bagNextLocked 从袋子里抽下一首的下标。袋空了就重装。
//
// 抽出来的那一首**从袋子里去掉**——它这一轮已经轮到过了。
//
// ⚠️ **袋里认不出是哪一首了（它被删掉了）就跳过**：那一格作废，接着抽下一格。
// 袋子和队列是各自长的，中间那一首没了是常事。
func (q *Queue) bagNextLocked() (int, bool) {
	for {
		if len(q.bag) == 0 {
			q.bagResetLocked()
			if len(q.bag) == 0 {
				return 0, false
			}
		}
		id := q.bag[0]
		q.bag = q.bag[1:]
		at := q.indexOfIdentityLocked(id)
		if at < 0 {
			// 那首歌已经被删掉了：这一格作废，接着抽下一格。
			continue
		}
		return at, true
	}
}

// bagPeekLocked 报「下一次会抽到哪一首」，**不抽走**。
//
// 它是 `bagNextLocked` 的那一份「只看不拿」（预取用这一支，见
// `Queue.PeekNext`）：预取要知道下一首是谁，但它**不能替真抽把袋子抽走**
// ——抽走了的话，真播那一趟抽到的就不是这一首了。
//
// ⚠️ **袋空的时候答不出来**（答 false）：真抽的时候袋空会**重装**一袋，而
// 重装要洗牌（要消耗随机源）——预取不该替真抽把随机源用掉。所以随机那一档
// 每轮里总有一首（袋空的那一首）备不上，代价只是那一首没有无缝。
//
// ⚠️ **已经不在队列里的那些要跳过**：和 `bagNextLocked` 一样（那一格作废，
// 接着看下一格）。这里**不把作废的那些抽掉**——真抽的时候才抽。
func (q *Queue) bagPeekLocked() (int, bool) {
	for _, id := range q.bag {
		at := q.indexOfIdentityLocked(id)
		if at >= 0 {
			return at, true
		}
	}
	return 0, false
}

// bagMoveToEndLocked 把 id 从袋子里挪到最后（它不在袋子里时什么都不做）。
//
// 「正在放的那首」和「刚被点过去的那首」都归它管：它们已经在放了，这一轮最后
// 再放它们。
func (q *Queue) bagMoveToEndLocked(id string) {
	if id == "" {
		return
	}
	for k, v := range q.bag {
		if v == id {
			q.bag = append(q.bag[:k], q.bag[k+1:]...)
			q.bag = append(q.bag, id)
			return
		}
	}
}

// bagAddItemsLocked 把新加进队列的那几首装进袋子——**这一轮就轮到它们**。
//
// 这是「加到队尾」那一条路（`Queue.Append`）：随机档的袋子是**池子**（见文件头
// 那一节），新加进来的成员立刻在池中，而不是等下一轮重装。
//
// ⚠️ **顺序 / 循环 / 单曲三档一个字都不动**：袋子只在随机档有读者，而这一档
// 的「下一首」是纯按索引算的（见 Queue.Advance）。
//
// ⚠️ **已经在袋子里的不重复装**：袋子记的是判重标识，同一首在队列里排了两遍
// （用户从两个页面各加了一次）也只占一格——装两格的话它这一轮会放两遍，而
// 「一列里一首不重复」正是洗牌袋要保证的那一条。
//
// ⚠️ **袋空的时候不装**：袋空就是这一轮已经放完了（或者还没开始）——下一轮
// 重装时袋子是从**整个队列**重装的，它自然在里面（见 bagResetLocked）。
//
// ⚠️ **这一轮已经放过、用户又加了一遍的那首会再放一次**：它已经不在袋子里了
// （袋子只装「这一轮还没轮到」的），而用户是**显式**又点了一次「添加到队列」
// ——那和「下一首播放」是同一件事：**用户的意图压过随机**（见文件头）。
func (q *Queue) bagAddItemsLocked(items []Item) {
	if q.mode != PlayModeRandom {
		return
	}
	for _, it := range items {
		q.bagAddLocked(it.Identity())
	}
}

// bagAddLocked 把一首装进袋子（`bagAddItemsLocked` 的那一份）。
//
// ⚠️ **插在剩下那些里的随机一个位置**：随机是对**队列成员**洗牌，新来的成员和
// 别的成员一样从池子里抽（不是排在最后——那是「下一首播放」那一档的事）。
//
// ⚠️ **正在放的那一首占着袋尾时，新来的别插到它后面**：它这一轮最后再放（见
// bagResetLocked），插到它后面的话它就不是最后了——用户听到的是「刚放完又放
// 一遍」。
func (q *Queue) bagAddLocked(id string) {
	if id == "" || len(q.bag) == 0 {
		return
	}
	if q.bagHasLocked(id) {
		return
	}
	// 能插的位置：袋子里除掉「正在放的那一首」那一格（它在袋尾时）。
	n := len(q.bag)
	if q.bag[n-1] == q.currentIdentityLocked() {
		n--
	}
	// 随机插进池子里。
	at := q.rnd.Intn(n + 1)
	q.bag = append(q.bag, "")
	copy(q.bag[at+1:], q.bag[at:])
	q.bag[at] = id
}

// bagHasLocked 报 id 在不在袋子里。
func (q *Queue) bagHasLocked(id string) bool {
	for _, v := range q.bag {
		if v == id {
			return true
		}
	}
	return false
}

// bagPromoteLocked 把 id 挪到袋头——**下一首就是它**。
//
// 这是「下一首播放」那一条路（`Queue.InsertNext`）：随机档的「下一首」是袋子
// 说了算，而「下一首播放」是**用户显式的意图**，它压过随机（见文件头那一节）。
//
// ⚠️ **不在袋子里也放进去**：那一首可能是刚加进队列的（这一轮还没轮到），也可能
// 这一轮已经放过它了（用户又点了一次「下一首播放」）——两种情况下用户要的都是
// 「下一首就是它」。
//
// ⚠️ **挪的正是「正在放的那一首」时，「它这一轮最后再放」这条让位**：用户点了
// 「下一首播放」当前这一首，要的就是它接着放——那一条是用户的显式意图，它压过
// 随机（见文件头那一节）。
//
// ⚠️ **顺序 / 循环 / 单曲三档一个字都不动**：那一档的「下一首」是纯按索引算的
// （插进 `q.items` 的当前位置就是下一位，见 Queue.InsertNext）。
func (q *Queue) bagPromoteLocked(id string) {
	if q.mode != PlayModeRandom || id == "" {
		return
	}
	// 先从袋子里拿掉它（它在哪儿都拿掉）。
	for k, v := range q.bag {
		if v == id {
			q.bag = append(q.bag[:k], q.bag[k+1:]...)
			break
		}
	}
	// 再放到袋头。
	q.bag = append(q.bag, "")
	copy(q.bag[1:], q.bag)
	q.bag[0] = id
}

// bagClearLocked 把洗牌袋整个清掉。
//
// 用在「整个换队列」的时候——那一档下「这一轮还没轮到谁」已经和现在的队列对不上
// 了（虽然认的是判重标识，改过之后那些歌的顺序也变了）。
func (q *Queue) bagClearLocked() {
	q.bag = nil
}

// historyPushLocked 往历史栈里压一首（放过的、现在不在放的那首）。
//
// ⚠️ **压的是判重标识**：认不出是哪一首的（空串）压不进去——它在队列里对不上
// 任何一行，退不回来。
func (q *Queue) historyPushLocked(id string) {
	if id == "" {
		return
	}
	q.history = append(q.history, id)
	if len(q.history) > historyLimit {
		// 丢掉最老的那些：栈是「越靠上越近」，丢的是底下。
		q.history = append(q.history[:0], q.history[len(q.history)-historyLimit:]...)
	}
}

// historyPopLocked 从历史栈顶上取一首，并报出它在队列里的下标。
//
// 取到的那一首**可能已经不在队列里了**（用户把它删了）：那就接着取下一首。
//
// ⚠️ **按判重标识对回队列，而不是按下标**：历史栈记的是**歌**，歌不会因为别人
// 挪了位置而变样（而下标会）。抽屉那一列也是按判重标识认的，两边对得上。
func (q *Queue) historyPopLocked() (Item, int, bool) {
	for len(q.history) > 0 {
		id := q.history[len(q.history)-1]
		q.history = q.history[:len(q.history)-1]
		at := q.indexOfIdentityLocked(id)
		if at < 0 {
			continue
		}
		return q.items[at], at, true
	}
	return Item{}, 0, false
}

// historyClearLocked 把历史栈清掉。
//
// 用在「换档」和「整个换队列」的时候——那些场合下「刚才放过谁」已经和现在
// 的队列对不上了。
func (q *Queue) historyClearLocked() {
	q.history = q.history[:0]
}

// currentIdentityLocked 是正在放的那一首的判重标识（没有当前项时是空串）。
func (q *Queue) currentIdentityLocked() string {
	return q.identityAtLocked(q.cur)
}

// identityAtLocked 是队列里第 i 项的判重标识（越界、或认不出是哪一首时是空串）。
func (q *Queue) identityAtLocked(i int) string {
	if i < 0 || i >= len(q.items) {
		return ""
	}
	return q.items[i].Identity()
}

// indexOfIdentityLocked 队列里判重标识等于 id 的**第一份**的下标，没有是 -1。
func (q *Queue) indexOfIdentityLocked(id string) int {
	for i, it := range q.items {
		if it.Identity() == id {
			return i
		}
	}
	return -1
}
