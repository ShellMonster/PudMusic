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
// ## 现在还没有接口
//
// B 站接口在阶段 3 的另一张票里。所以这里给的是**模型和接线**，不是数据：
// 界面侧栏照画两组、折叠、右键菜单、拖拽排序，条目由谁调 `SetFavorites`
// 谁负责——服务层接上接口之后调它，测试调它。空列表时两组只画标题和一行
// 「还没有收藏夹」，不会假装有内容。
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
// 字段只留现在画得出来的那些（标题 + 跳转要用的 id / 账号 / 类型）。
// 封面那一格等 imgcache 接上再补——**先立字段、后接数据**，和
// `State.CoverURL` 是同一个理由：本地文件永远没有封面，但字段先立着。
type Favorite struct {
	ID    int64
	Title string
	// Mid 是这个收藏夹属于哪个账号。跳转 `/collection/{id}?mid=` 要用。
	Mid int64
	// Type 是 B 站那边的类型：收藏夹 / 合集 / 系列不一样。
	// 「我创建的」全是收藏夹，「我收藏的」三种都有（见 collection/index.tsx:327）。
	Type int
}

// Favorites 是侧栏那两组条目。
//
// 零值可用（两组都是空的），不像 App 那些必须 New 的东西——空列表就是
// 「还没接接口」那个状态，不该是「忘了初始化」的那种状态。
type Favorites struct {
	mu     sync.Mutex
	groups [favoriteGroupCount][]Favorite
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
// **每次从接口拿到新条目都要过这里**：顺序不是接口给的，是用户拖出来的，
// 忘了排就等于每次刷新都把用户的排序冲掉。
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
// 服务层接到收藏夹接口之后调它；调完之后不用再管顺序，拖一次存一次。
func (a *App) SetFavorites(g FavoriteGroup, items []Favorite) {
	a.FavoritesOf().Replace(g, items, a.SettingsSnapshot().SideMenuCollectionOrder.list(g))
}

// SideFavorites 是这一帧侧栏要画的那一组条目，按当前顺序。
func (a *App) SideFavorites(g FavoriteGroup) []Favorite {
	return a.FavoritesOf().Snapshot(g)
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
