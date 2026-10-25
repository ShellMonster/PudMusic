// 侧栏那两组收藏夹（SD-05~SD-09）。
//
// ## 分工：条目在内存里，顺序落在设置里
//
// Biu 是这么分的（`biu/src/store/favorite.ts:157-163`）：条目每次都从 B 站接口
// 重新拉，只有用户拖出来的**顺序**单独存一份。我们照抄这个分工：
//
//   - 条目是接口的，不是用户的。存一份本地副本，下次接口返回的内容变了
//     （改名、删了、换账号了），本地那份就成了没人更新的脏数据。
//   - 顺序是用户拖出来的，接口不返回，只能存。
//
// 落盘走 SettingsStore，所以 schema 版本、原子写、退出前 flush、只读保护
// 都不用在这里重造（见 settings.go 与 SideMenuCollectionOrder 的注释）。
//
// ## 数据从哪儿来
//
// **两组现在都有源了**：
//
//   - 「我创建的」走 `created/list`（`bili.CreatedFolders`）；
//   - 「我收藏的」走 `collected/list`（`bili.CollectedFolders`）——
//     B 站「我收藏了别人的哪些收藏夹」那个接口**是有的**，本项目已经接上
//     （见 `bili/collected.go` 的文件头：参数、返回字段、`platform=web`
//     那一条都在那儿）。
//
// 两组都由 `App.RefreshFavorites` / `App.AutoRefreshFavorites` 拉，
// 拉回来的东西一次 `Favorites.Commit` 换进去（条目和状态一起换，见它的
// 注释）。
//
// ## 空列表有三种来由，界面上必须说得清是哪一种
//
// 「真的一个都没有」「还没拉过」「拉失败了」在画面上不能长得一样——
// 「还没有收藏夹」只说得清第一种，在另外两种上它是在替 B 站回答一个
// 我们还没问过的问题（AGENTS.md §4）。所以加载状态单独存着，
// 见 `FavoriteState`。
//
// 空列表时两组只画标题和那一句空状态，不会假装有内容。
package app

import (
	"slices"
	"sync"
)

// FavoriteGroup 是侧栏收藏夹的两组之一。
//
// 两组的键名照 Biu 的 `sideMenuCollectionFolded.{created,collected}`
// （`biu/shared/settings/app-settings.ts:25-28`），顺序也一样：
// 「我创建的」在上，「我收藏的」在下（`biu/src/layout/side/collection/index.tsx:545-549`）。
type FavoriteGroup int

const (
	FavoritesCreated FavoriteGroup = iota
	FavoritesCollected
	favoriteGroupCount
)

// String 是这一组在侧栏标题上写的字。Biu 的两条（`collection/index.tsx:501,519`）。
func (g FavoriteGroup) String() string {
	switch g {
	case FavoritesCreated:
		return "我创建的"
	case FavoritesCollected:
		return "我收藏的"
	}
	return "未知分组"
}

// Valid 报 g 是不是真的两组之一。分组是从界面传进来的，别的地方不保证。
func (g FavoriteGroup) Valid() bool { return g >= 0 && g < favoriteGroupCount }

// Favorite 是侧栏里的一行：一个收藏夹，或者一个合集 / 系列。
//
// 字段只留现在画得出来的那些（标题 + 跳转要用的 id / 账号 / 类型 + 封面 +
// 歌曲数量）。
//
// ⚠️ **今天侧栏上只有收藏夹那一种行**：合集 / 系列的内容列表是另一套接口
// （本项目没接），摆进去是死的路，所以 `app.collectedFavorites` 那一道把
// 它们滤掉了。类型这个字段留着——它本来就属于这一行，而等合集内容接上了，
// 那一行放开时地址和播放都已经是对的。
type Favorite struct {
	ID    int64
	Title string
	// Mid 是这个收藏夹属于哪个账号。跳转 `/collection/{id}?mid=` 要用。
	Mid int64
	// Type 是 B 站那边的类型：收藏夹 / 合集 / 系列不一样。
	// 「我创建的」全是收藏夹；「我收藏的」那边接口也会给合集 / 系列，
	// 但它们被滤掉了（见上面的注释）。
	Type int
	// Cover 是这个容器的封面地址（可能是协议相对地址）。
	//
	// ⚠️ **来源是那两个列表接口本来就给的一格**
	// （`created/list` 和 `collected/list` 的 `list[].cover`）——不是每行
	// 另发一次请求问来的（票 #270）。空串 = 没有封面，界面上画占位图。
	//
	// ⚠️ **「我收藏的」那一组里它是原主人的封面**（那一组全是别人的收藏夹，
	// 见 `bili.CollectedFolder.Cover`）。
	Cover string
	// MediaCount 是里面有几个内容。
	//
	// ⚠️ **它是 B 站上次统计的数字，不是实时的**——用户在 B 站网页上刚加
	// 一首，这边要等下一次刷新才数得对。那不是 bug（票 #270 的验收标准）。
	MediaCount int
}

// Favorites 是侧栏那两组条目。
//
// 零值可用（两组都是空的），不像 App 那些必须 New 的东西——空列表就是
// 「还没接接口」那个状态，不该是「忘了初始化」的那种状态。
type Favorites struct {
	mu     sync.Mutex
	groups [favoriteGroupCount][]Favorite

	// states 是两组的**加载状态**（见 FavoriteState）。
	//
	// ⚠️ **它和 `groups` 是两件事**：空列表有两种——拉成功而 B 站那边确实没有，
	// 和**压根没拉**。而「拉失败」是第三种（列表可能还是上一次的）。
	// 三者在画面上必须长得不一样，所以状态单独存着，不靠 `len(groups[g])` 推。
	states [favoriteGroupCount]FavoriteState

	// gen 是「这一摊属于哪一次登录」的代号。
	//
	// ⚠️ **登录态换人时它 +1**（见 `forgetAccount`）：换人之后可能**还有一趟刷新
	// 在飞**，而那一趟回来时带着的是**上一个账号**的收藏夹——填回侧栏就是把它
	// 泄露给下一个登录的人。所以刷新那一趟开始时记下代号，回来时对一下：
	// 对不上就整趟丢掉（见 `Commit`）。
	gen uint64

	// refresh 是「正在重新拉一次」那一趟的状态，跑完攒下的话也算在里面。
	// 见 favorites_refresh.go——和上面那两组是同一份侧栏收藏夹的两种状态，
	// 所以它们共用这一把锁，不另起一张包级的表。
	refresh favRefresh
}

// FavoriteState 是侧栏那一组收藏夹的加载状态。
//
// # 为什么它是独立的一份，而不是从「有几条」推出来的
//
// 空列表有三种来由，而它们在界面上必须长得**不一样**：
//
//   - **真的一 个都没有**：B 站那边确实没有。
//   - **还没拉**：压根没去拉（没登录、刚启动还没轮到）。
//   - **拉失败了**：拉过，但没拉到（断网、登录态过期、B 站在风控）。
//
// 只看 `len(items) == 0` 的话，后两种都长得和第一种一样——而「还没有收藏夹」
// 那句话在后两种上是**撒谎**（AGENTS.md §4：「没有」和「没显示出来」长得
// 一样，就是把没显示出来说成了没有）。
type FavoriteState struct {
	// Loaded 是「成功拉过一次」。**一个都没有也是 true**——那正是「B 站那边
	// 确实没有」这个结论本身。
	Loaded bool
	// Err 是最近一次拉失败的话。空串 = 没失败。
	//
	// ⚠️ **它不摆到界面上**：界面只判它是不是空串（那一句要短——侧栏最窄
	// 160，摆不下细节）；带细节的那一句走提示（见 `favErrText`）。
	//
	// 所以它在这儿的作用是**一个标记**，不是一句话。存的是人话而不是
	// `error`，是为了日志和调试时能直接读懂（服务层那几句已经是翻好的，
	// 见 `bili.Friendly`）。
	Err string
}

// FavoriteGroupsUpdate 是一次「把两组一起换掉」要写进去的东西。
//
// ⚠️ **两组和它们的状态一起换**：分两次换的话，中间那一帧是「有条目、
// 状态还停在没拉过」——界面上那一句会从「还没拉收藏夹」跳到「还没有收藏夹」，
// 而这一跳用户看得见。
type FavoriteGroupsUpdate struct {
	// Items 是两组的新条目。
	//
	// ⚠️ **nil 是「这一组这一次没拿到」**（拉失败了），而**空但不是 nil**
	// 是「这一组确实一个都没有」——这两个在界面上说的不是同一句话。
	Items [favoriteGroupCount][]Favorite
	// Order 是两组各自的保存顺序（见 `applySavedOrder`）。
	Order [favoriteGroupCount][]int64
	// States 是两组的状态（见 `FavoriteState`）。**两组都要给**。
	States [favoriteGroupCount]FavoriteState
}

// Commit 把这一次更新写进去。**但只在代号还是 gen 的时候。**
//
// 返回 false = 代号变了（登录态换人了），**什么都没写**。
//
// ⚠️ **代号变了就是登录态换人了**：退登、或者换人登录（已登录时再扫一次码）。
// 之后可能还有一趟刷新在飞，而那一趟回来时带着的是**上一个账号**的收藏夹——
// 填回侧栏就是把它泄露给下一个登录的人。清掉的那一次（`forgetAccount`）把
// 代号 +1，所以那一趟回来时对不上，整趟丢掉。
//
// ⚠️ **这一道是「检查」和「写」一起做的**：分两步的话，换人正好卡在两步
// 之间的话，那一趟还是会把上一个账号的填回去——而清掉的那一次已经
// 过去了。
func (f *Favorites) Commit(gen uint64, u FavoriteGroupsUpdate) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gen != gen {
		return false
	}
	for g := range favoriteGroupCount {
		if u.Items[g] != nil {
			f.groups[g] = applySavedOrder(u.Items[g], u.Order[g])
		}
		st := u.States[g]
		// ⚠️ **失败不清掉 Loaded**：上一次拉成功过的话，那一份列表还在界面上，
		// 而它仍然是「上一次拉到的」——把它说成「从没拉过」是另一句谎。
		if st.Err != "" && f.states[g].Loaded {
			st.Loaded = true
		}
		f.states[g] = st
	}
	return true
}

// state 读这一组此刻的加载状态。
func (f *Favorites) state(g FavoriteGroup) FavoriteState {
	if !g.Valid() {
		return FavoriteState{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[g]
}

// markFailed 记下「这一组最近一次拉失败了」，**但只在代号还是 gen 的时候**。
//
// ⚠️ **不清掉 Loaded**：上一次拉成功过的话，那一份列表还在界面上，
// 而它仍然是「上一次拉到的」——把它说成「从没拉过」是另一句谎。
//
// ⚠️ **代号对不上就什么都不写**：那说明中间换过人（退登或者换账号），
// 而这一句说的是上一个账号的事（见 `Favorites.gen`）。
func (f *Favorites) markFailed(gen uint64, g FavoriteGroup, reason string) {
	if !g.Valid() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gen != gen {
		return
	}
	st := f.states[g]
	st.Err = reason
	f.states[g] = st
}

// forgetAccount 把侧栏那两组收藏夹**整个作废**：代号 +1、条目清掉、状态复位、
// 「正在刷」放开。
//
// # 用在两处，而它们要做的是同一件事
//
//   - **退登**：登录态没了，侧栏那两组是上一个账号的。
//   - **换账号**：已登录时再扫一次码，登录成**另一个账号**。
//
// 两条路都走这一个入口（见 `App.onAccountChanged`）。
//
// ⚠️ **退登那条路早就是对的**（issue #96 起就有），而**换账号那条路漏了**——
// 漏掉的后果是「上一个账号的收藏夹被填回侧栏，而新账号的那一趟一个请求都不发」
// （issue #398）。
//
// ⚠️ **代号 +1 是这一件里最要紧的**：换人的时候可能**还有一趟刷新在飞**，
// 而那一趟回来时带着的是上一个账号的收藏夹。代号 +1 之后，那一趟回来时
// 对不上，整趟丢掉（见 `Commit`）——不然这一件就被绕过了。
//
// ⚠️ **「正在刷」也要放开**：在飞的那一趟属于**上一个账号**，而新账号会重新
// 拉一趟（那一趟才是屏幕上该转的那一个）。不放开的话新账号那一趟被
// `tryBeginRefresh` 挡掉，**一个请求都不发**——新账号的收藏夹永远不来。
//
// ⚠️ **状态也要清**：不清的话「拉到了、一个都没有」那个结论会留在那儿，
// 而新账号登录之后那一组是空的——用户读到的是「还没有收藏夹」，而真相是
// 「这个账号的还没拉过」。
//
// ⚠️ **攒着的那句话也收掉**：它是**上一个账号**那趟的话，而新账号不该看到
// 「已刷新收藏夹：「我创建的」3 个」。
func (f *Favorites) forgetAccount() {
	f.mu.Lock()
	f.gen++
	f.groups = [favoriteGroupCount][]Favorite{}
	f.states = [favoriteGroupCount]FavoriteState{}
	f.refresh.busy = false
	f.refresh.have = false
	f.mu.Unlock()
}

// Snapshot 返回这一组当前的全部条目，按当前顺序。返回的是拷贝。
func (f *Favorites) Snapshot(g FavoriteGroup) []Favorite {
	if !g.Valid() {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.groups[g])
}

// Replace 把这一组换成 items，并按存下的 order 排好。
//
// **从接口拿到新条目时按它排好**：顺序不是接口给的，是用户拖出来的，
// 忘了排就等于每次刷新都把用户的排序冲掉。
//
// ⚠️ **刷新那一趟不走它**：那一趟要一次把两组的条目和状态一起换掉，还要
// 对一下账号有没有换过（见 `Favorites.Commit`）。这一条是「只换一组」
// 那一档（`SetFavorites` 用的）。
func (f *Favorites) Replace(g FavoriteGroup, items []Favorite, order []int64) {
	if !g.Valid() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups[g] = applySavedOrder(items, order)
}

// Reorder 把 id 挪到 before 那一行的**前面**，返回挪完之后的顺序。
//
// before 是收藏夹的 ID；**before 为 0 表示挪到最后面**——B 站的 media_id
// 不会是 0，所以 0 在这里是空着的。调用方（侧栏）拿到的是「插到第几行前面」
// 或者「列表的行数」（= 最后面），由它换算成这一个参数。
//
// ⚠️ **这条语义不是「挪到目标的最终下标」，是「插到目标前面」。** 差一格：
// 往下拖（id 原本排在 before 前面）时，按旧下标插进去，被拖的那条会越过目标
// 一行——屏幕上是指示线画在目标**上边缘**、落下去却停在目标**下面**。
// 所以这里先把 id 拿出来，再在**缩短后的**列表里找 before 的位置插进去。
// 往上拖（id 原本排在 before 后面）时两种算法碰巧一样，差只差在往下拖。
//
// ⚠️ 这也是**不能照抄 Biu 的 `reorderList`** 的原因（`favorite.ts:47-59`）：
// 它是 `splice(to, 0, moved)`，to 取的是目标删之前的下标，语义是「挪到目标
// 的最终位置」。Biu 的 dnd-kit 给的 over.id 和这里给的 before 不是一回事，
// 照抄过来在往下拖的时候正好差一格。
//
// 没动过（id 不在列表里、before 不在列表里、或者本来就在那）时返回 false，
// 调用方据此判断要不要落盘——Biu 在这种情况下 `return state`，一个字都不写。
func (f *Favorites) Reorder(g FavoriteGroup, id, before int64) ([]int64, bool) {
	if !g.Valid() {
		return nil, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	from := slices.IndexFunc(f.groups[g], func(x Favorite) bool { return x.ID == id })
	if from < 0 {
		return nil, false
	}

	next := slices.Clone(f.groups[g])
	moved := next[from]
	next = slices.Delete(next, from, from+1)

	// before == 0 → 落到最后面，也就是插到末尾。
	to := len(next)
	if before != 0 {
		to = slices.IndexFunc(next, func(x Favorite) bool { return x.ID == before })
		if to < 0 {
			return nil, false
		}
	}
	// 插回原来的位置 = 没动。from 是缩短**前**的下标，恰好等于缩短后的
	// 落点时就说明顺序没变。
	if to == from {
		return nil, false
	}

	next = slices.Insert(next, to, moved)
	f.groups[g] = next
	return orderOf(next), true
}

// Upsert 把 item 加进这一组，或者在它已经在的时候换成新的。
//
// 已经有：在**原来的位置**上换掉（不挪位置、不改顺序）。没有：接在最后面。
//
// 为什么是「在原位换掉」而不是删掉再加：侧栏顺序是用户拖出来的（落盘在
// `Settings.SideMenuCollectionOrder`），改一次收藏夹的名字就把它的位置
// 冲掉的话，用户下一次打开侧栏会发现刚整理好的顺序白整理了。
//
// 走这一条的是**建完之后 / 改完之后**那一下：接口告诉了我们新收藏夹是什么，
// 但拉全量是另一趟往返（见 app 层的 `LoadCreatedFolders`），而「新建完
// 侧栏里立刻出现它」比「顺序正确」更要紧。
//
// ⚠️ **数量沿用旧的那一份**：建 / 改收藏夹那两个接口**不返回 media_count**
// （`bili.FolderInfo` 没有这一格），所以传进来的 `MediaCount` 是 0——而这里的
// 0 是「不知道」，不是「真的 0 首」。直接换上去的话，用户改一次收藏夹的名字，
// 侧栏上那个「32 首」就变成了「0 首」，而那个收藏夹里明明还有 32 首。
// 真的新建出来的那一格没有旧值，沿用不到，于是就是 0（它确实是空的）。
func (f *Favorites) Upsert(g FavoriteGroup, item Favorite) {
	if !g.Valid() || item.ID == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.groups[g], func(x Favorite) bool { return x.ID == item.ID }); i >= 0 {
		if item.MediaCount == 0 {
			item.MediaCount = f.groups[g][i].MediaCount
		}
		f.groups[g][i] = item
		return
	}
	f.groups[g] = append(f.groups[g], item)
}

// Remove 把这一组里的某一行拿掉。
//
// 用在两件事上：用户删掉了一个自己创建的收藏夹、取消收藏了别人的一个。
// **不立刻落盘**——落盘在 `App.forgetFavorite` 那一步（连着顺序一起改），
// 所以这一层只是内存里的删除。
//
// 拿掉之后**顺序里其余的行不动**：用户拖出来的相对顺序必须原样留着，
// 少一个元素不该让剩下的重排。
func (f *Favorites) Remove(g FavoriteGroup, id int64) {
	if !g.Valid() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.IndexFunc(f.groups[g], func(x Favorite) bool { return x.ID == id }); i >= 0 {
		f.groups[g] = slices.Delete(slices.Clone(f.groups[g]), i, i+1)
	}
}

// IDs 是这一组当前的 ID 顺序，落盘用。
func (f *Favorites) IDs(g FavoriteGroup) []int64 {
	if !g.Valid() {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return orderOf(f.groups[g])
}

// applySavedOrder 按存下的顺序排好 items。
//
// 逐字照 Biu 的 `applySavedOrder`（`favorite.ts:37-45`）：存下的顺序里有的
// 按它排在前面，**存下的时候还没有的按接口给的顺序接在后面**。
//
// 后半句不能省。用户 A 拖好的顺序，用户 B 登录后看同一个应用时，多出来的
// 那几个如果被丢掉，侧栏上就是「凭空少了几项」；反过来，多出来的排在前面
// 就会把用户 A 精心排的顺序整个顶掉。
func applySavedOrder(items []Favorite, order []int64) []Favorite {
	if len(order) == 0 || len(items) == 0 {
		return slices.Clone(items)
	}
	saved := make(map[int64]bool, len(order))
	for _, id := range order {
		saved[id] = true
	}

	out := make([]Favorite, 0, len(items))
	for _, id := range order {
		if i := slices.IndexFunc(items, func(x Favorite) bool { return x.ID == id }); i >= 0 {
			out = append(out, items[i])
		}
	}
	for _, it := range items {
		if !saved[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

// orderOf 是一条列表的 ID 序列。
func orderOf(items []Favorite) []int64 {
	out := make([]int64, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 挂到 App 上
// ─────────────────────────────────────────────────────────────────────────────

//
// 挂载关系记在包级的表里，而不是 App 的一个字段——和 settings_mount.go 一个
// 道理：App 的结构体写在 app.go 里，那个文件这一阶段不归本任务改，而 Go 的
// 方法能写在任意文件里、字段不能。等 app.go 那边的改动并进来，把下面这一格
// 搬回结构体就行，调用方一行都不用改。

// favoritesSlot 是一个 App 挂着的侧栏收藏夹。
var mountedFavorites sync.Map // *App → *Favorites

// FavoritesOf 是这个 App 的侧栏收藏夹。还没挂过就现建一个空的：空列表是
// 「还没接接口」那个正常状态，不该让调用方判 nil。
func (a *App) FavoritesOf() *Favorites {
	if v, ok := mountedFavorites.Load(a); ok {
		return v.(*Favorites)
	}
	f := &Favorites{}
	actual, _ := mountedFavorites.LoadOrStore(a, f)
	return actual.(*Favorites)
}

// SetFavorites 把某一组换成 items，顺序按设置里存的那份排。
//
// ⚠️ **它只换条目，不动加载状态**：加载状态（见 `FavoriteState`）是「拉过
// 没有、拉成了没有」那一层的事，而这一条是「把这一组摆成什么样」。
//
// **刷新那一趟不走它**：那一趟要一次把两组的条目和状态一起换掉，还要对一下
// 账号有没有换过（见 `Favorites.Commit`）。
//
// 服务层接到收藏夹接口之后调它；调完之后不用再管顺序，拖一次存一次。
func (a *App) SetFavorites(g FavoriteGroup, items []Favorite) {
	a.FavoritesOf().Replace(g, items, a.SettingsSnapshot().SideMenuCollectionOrder.list(g))
}

// SideFavorites 是这一帧侧栏要画的那一组条目，按当前顺序。
func (a *App) SideFavorites(g FavoriteGroup) []Favorite {
	return a.FavoritesOf().Snapshot(g)
}

// FavoriteState 是这一组此刻的加载状态。
//
// 界面拿它决定空状态那一句说什么（见 `FavoriteState` 的注释）：**空列表和
// 「没拉到」在画面上必须长得不一样**，而这一份就是那个区别。
func (a *App) FavoriteState(g FavoriteGroup) FavoriteState {
	return a.FavoritesOf().state(g)
}

// ReorderFavorite 把一组里的 id 挪到 before 那一行的**前面**，并**立刻落盘**。
//
// before 为 0 表示挪到最后面（见 Favorites.Reorder）。
//
// 落盘写在同一次调用里，是有意跟别处不一样：拖拽排序是「用户做了个决定」，
// 不是「调了个参数」。调完之后界面上还看不出任何区别（要重启才知道）的话，
// 用户只会以为没拖成。
//
// 没动过的时候返回 false 且一个字都不写（见 Favorites.Reorder）。
func (a *App) ReorderFavorite(g FavoriteGroup, id, before int64) (bool, error) {
	ids, moved := a.FavoritesOf().Reorder(g, id, before)
	if !moved {
		return false, nil
	}
	_, err := a.UpdateSettings(func(s *Settings) {
		s.SideMenuCollectionOrder = s.SideMenuCollectionOrder.setList(g, ids)
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
