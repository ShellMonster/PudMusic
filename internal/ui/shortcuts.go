// 快捷键：Biu 的 `shared/settings/shortcut-settings.ts` + `src/store/shortcuts.ts`。
//
// ## 为什么是单独一份文件，不进 app.Settings
//
// Biu 自己也是**单独一个 store**（zustand persist 的 `"shortcut-settings"`，
// `biu/src/store/shortcuts.ts:20`），`app-settings.json` 里根本没有这个键
// （`shared/settings/app-setting.d.ts` 逐字对过）。照抄就是这一份文件。
// `PLAN.md` §3.4 的持久化表也是这么写的：`| 快捷键 | <UserData>/shortcuts.json |`。
//
// 加进设置的理由（`sideMenuCollectionOrder` 那种）在快捷键这里**不成立**：设置
// 那边的键名要逐字等于 Biu 的导出，多一个键就是多一个 Biu 认不出的键；而快捷键
// 本来就是另一份文件，合成一份的话坏了要一起恢复。
//
// 落盘的做法照 `internal/app/settings.go` 和 `searchhistory.go`：schema 号 +
// 临时文件 + rename 原子写 + 后台合并写 + 收尾同步落完。
//
// ## 这一票只做「录下来」，不做「生效」
//
// 录完、存住、冲突能看出来，是这一票的事（PLAN 9.3）。按下去真的会做什么是
// PLAN 3.9 的事（应用内分发 + 全局注册），那一票还没做。所以现在改一个键，
// 界面上别的什么都不变——设置页那一页会把这件事说出来，而不是让用户以为坏了。
package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"
)

const (
	// shortcutFileName 是这份文件的名字。Biu 那边叫 `shortcut-settings.json`
	// （electron-store 的 `StoreNameMap` 拼出来的），这里按 PLAN §3.4 用
	// `shortcuts.json`。文件名不需要一致，**键名才需要**——下面那一段就照着它。
	shortcutFileName = "shortcuts.json"

	// shortcutSchema 是这份文件的格式版本。改它的**结构**（加键、删键、改类型）
	// 就涨它，并往 shortcutMigrations 里加一条 v→v+1 的路。
	shortcutSchema = 1

	// shortcutWriteDelay 是把连续改动合并成一次落盘的窗口。和设置那边的
	// writeDebounce（120ms）同一个量级。
	shortcutWriteDelay = 120 * time.Millisecond
)

// 快捷键存不住时的两个错。都是人能读懂的（设置页那一层负责加前缀）。
var (
	errShortcutsReadOnly  = errors.New("快捷键：这份文件来自更新的版本，本次只读不写")
	errShortcutsNoSuchCmd = errors.New("快捷键：没有这一条命令")
)

// shortcutItem 是一条快捷键。Biu 的 `ShortcutItem` 就是这三个字段
// （`shared/types/shortcut-setting.d.ts`）。
//
// Biu 还往同一条上挂 `isConflict` 和 `error` 两个**算出来的**字段：录的时候顺手
// 一起存下去。这里不存——它们每帧从 `Shortcuts` 现算（见 `conflictOf`）。存一份
// 在文件里只会和下一次算出来的对不上，而对不上的那一份没人会去改。
type shortcutItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Shortcut string `json:"shortcut"`
}

// shortcutSettings 是落盘的整体，键名照 Biu 的 `ShortcutSettings`。
type shortcutSettings struct {
	EnableGlobalShortcuts bool           `json:"enableGlobalShortcuts"`
	Shortcuts             []shortcutItem `json:"shortcuts"`
	GlobalShortcuts       []shortcutItem `json:"globalShortcuts"`
}

// shortcutFile 是落盘时外面套的那一层。Biu 那份 electron-store 的文件没有它，
// 读的时候按 v0 读（见 readShortcuts），键名一样能对上。
//
// ⚠️ `shortcutSettings` 必须是**匿名嵌入**的，不能是一个叫 `Shortcuts` 的命名字段：
// 匿名字段会被 encoding/json 摊平到外层，那三个键才在**顶层**——而 Biu 的导入是
// 逐个 `defaultShortcutSettings` 的键去顶层取的（`src/pages/settings/
// export-import.tsx:46-51`）。写成命名字段的话三个键会埋进一层对象里，Biu 那边
// 一个都取不到，而本项目这边看着「存好了」。
type shortcutFile struct {
	SchemaVersion int `json:"schemaVersion"`
	shortcutSettings
}

// shortcutMigrations 把 v 升到 v+1。
//
// v0 是**没有 schemaVersion 的那份**，而那正是用户真正拿得着的那一份：Biu 设置页
// 「导出配置」落下来的、以及 Biu 自己 electron-store 里躺着的那份
// （`biu/electron/store.ts`）。它必须有一条路能走过来——没有的话
// `readShortcuts` 会把 Biu 的文件判成「读不懂 → 只读」，用户在设置页上会一个字都
// 改不了（和设置那边 `migrations[0]` 修 Biu 那个 `"system-default"` 坏字体值是
// 同一个道理，见 `internal/app/settings.go`）。
var shortcutMigrations = map[int]func(*shortcutSettings){
	0: func(s *shortcutSettings) {
		// 现在没有要改的值。Biu 自己会往同一条上存 `isConflict` / `error` 两个
		// 算出来的字段（`shortcut-settings.tsx:31-36`），我们不读它们，也就不会
		// 把它们带回去——见 shortcutItem 的注释。
	},
}

// shortcutSlot 是设置页上那一格：哪一条命令（id）、哪一列（应用内/全局）。
//
// Comparable 是硬要求：`ui.FocusBind` 收的就是一个**可比较**的字段的指针，靠它把
// 「焦点在哪一格」和「正在录哪一格」绑成同一件事（见 shortcut_keys.go）。
type shortcutSlot struct {
	global bool
	id     string
}

// defaultShortcutSettings 是 Biu 的默认值，逐字抄
// `biu/shared/settings/shortcut-settings.ts`。
//
// ⚠️ 全局那六条比应用内的那六条多一个 ⌥：两边同用一个键的话，在别的应用里按
// 应用内那一个就会误触发别人——Biu 正是这么分的，照抄。
func defaultShortcutSettings() shortcutSettings {
	var ids = []struct{ id, key string }{
		{"togglePlay", "P"},
		{"prev", "Left"},
		{"next", "Right"},
		{"volumeUp", "Up"},
		{"volumeDown", "Down"},
		{"toggleMiniMode", "M"},
	}
	in := make([]shortcutItem, 0, len(ids))
	global := make([]shortcutItem, 0, len(ids))
	for _, it := range ids {
		// CommandOrControl 是 Biu 的写法：macOS 上是 ⌘，别处是 Ctrl。
		in = append(in, shortcutItem{ID: it.id, Name: defaultShortcutNames[it.id], Shortcut: "CommandOrControl+" + it.key})
		global = append(global, shortcutItem{ID: it.id, Name: defaultShortcutNames[it.id], Shortcut: "CommandOrControl+Alt+" + it.key})
	}
	return shortcutSettings{
		EnableGlobalShortcuts: true,
		Shortcuts:             in,
		GlobalShortcuts:       global,
	}
}

// defaultShortcutNames 是六条命令的名字，逐字抄 Biu 那一行。
var defaultShortcutNames = map[string]string{
	"togglePlay":     "播放/暂停",
	"prev":           "上一首",
	"next":           "下一首",
	"volumeUp":       "音量加",
	"volumeDown":     "音量减",
	"toggleMiniMode": "切换mini/完整模式",
}

// shortcutKeys 是快捷键在内存里和磁盘上的那一份。
//
// **零值不可用**，用 newShortcutKeys。path 为空就是只在内存里：拿不到应用数据
// 目录时设置页照常能录，只是关掉之后不记得。
type shortcutKeys struct {
	path string
	// ro 记着这份文件是别的版本写的，一个字都不许写回去。
	ro bool

	mu     sync.Mutex
	cur    shortcutSettings
	dirty  bool
	closed bool

	req  chan struct{}
	quit chan struct{}
	done chan struct{}
}

// newShortcutKeys 开一份快捷键。path 为空就是内存里的。
//
// 读盘在调用方的 goroutine 上同步做完（一份几百字节的文件），后台写盘另起一个
// goroutine。
func newShortcutKeys(path string) *shortcutKeys {
	k := &shortcutKeys{
		path: path,
		// 先摆上 Biu 的默认值：没挂上盘（path 为空）和第一次运行（文件不存在）
		// 都得有一份能用的，而不是一份零值——零值里那六条命令是空的，设置页上
		// 会一行都不画，而那正是「第一次打开设置页什么也没有」的样子。
		cur:  defaultShortcutSettings(),
		req:  make(chan struct{}, 1),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	if path != "" {
		k.cur, k.ro = readShortcuts(path)
	}
	go k.loop()
	return k
}

// readShortcuts 读盘。
//
// 认得两种形状，判据只有一条：**顶层有没有 `shortcuts` 这个键**。
//
//   - 我们写的：`{"schemaVersion": 1, "shortcuts": [...], "globalShortcuts":
//     [...], "enableGlobalShortcuts": true}`
//   - Biu 的 electron-store 文件：顶层就是那三个键，外加一个我们不认识的
//     `__internal__`（conf 的内部状态，标准库自动忽略）。**没有 schemaVersion，
//     按 v0 读**，走的是和「Biu 直接导出」同一条迁移链。
//
// 没有这个文件不是错误，返回默认值——第一次运行就是这样。
//
// 第二个返回值是「这份文件只读吗」：来自更新的 schema 版本、根本没有可走的迁移
// 路径、或者压根认不出是什么文件。两种情况下**一个字都不写回去**——按旧语义改
// 一遍就毁了，和设置那边的 ErrSettingsFromNewer 同一个道理
// （`internal/app/settings.go`）。
func readShortcuts(path string) (shortcutSettings, bool) {
	def := defaultShortcutSettings()
	b, err := os.ReadFile(path)
	if err != nil {
		return def, false
	}
	from, s, ok, err := probeShortcuts(b)
	if err != nil {
		backupShortcuts(path)
		return def, false
	}
	if !ok {
		return def, true
	}
	if from > shortcutSchema {
		return def, true
	}
	if err := runShortcutMigrations(&s, from); err != nil {
		return def, true
	}
	return s.mergeMissing(def), false
}

// mergeMissing 补上文件里没有的那几条命令。
//
// 为什么不直接信文件里的列表：加一条新命令之后，老用户的文件里没有它，而这一页
// 摆的是「默认那几条 × 两列」，少一条就等于那条命令在界面上消失了。补齐的判据
// 是 **id**：名字改了不补（界面上显示的名字应当跟着新版本走），命令没了才补。
func (s shortcutSettings) mergeMissing(def shortcutSettings) shortcutSettings {
	out := s
	out.Shortcuts = mergeShortcutList(s.Shortcuts, def.Shortcuts)
	out.GlobalShortcuts = mergeShortcutList(s.GlobalShortcuts, def.GlobalShortcuts)
	return out
}

// mergeShortcutList 把 def 里缺的 id 补到 list 末尾。
func mergeShortcutList(list, def []shortcutItem) []shortcutItem {
	out := slices.Clone(list)
	for _, d := range def {
		if !slices.ContainsFunc(out, func(it shortcutItem) bool { return it.ID == d.ID }) {
			out = append(out, d)
		}
	}
	return out
}

// backupShortcuts 把读不了的那份挪成 `<path>.bak` 留着。
//
// 挪不动也不报错——已经出过一次问题了，不能因为留个证据再报一次。
func backupShortcuts(path string) { _ = os.Rename(path, path+".bak") }

// snapshot 是这一帧该画的那一份。值拷贝，调用方别去改里面的切片。
func (k *shortcutKeys) snapshot() shortcutSettings {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.cur
}

// readOnly 报这份文件是不是只读的（来自更新的版本）。
func (k *shortcutKeys) readOnly() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.ro
}

// set 改一条。列由 slot.global 选，只动这一条，别的原样带走。
func (k *shortcutKeys) set(slot shortcutSlot, accel string) error {
	k.mu.Lock()
	list := k.cur.Shortcuts
	if slot.global {
		list = k.cur.GlobalShortcuts
	}
	idx := slices.IndexFunc(list, func(it shortcutItem) bool { return it.ID == slot.id })
	if idx < 0 {
		k.mu.Unlock()
		return errShortcutsNoSuchCmd
	}
	if k.ro {
		k.mu.Unlock()
		return errShortcutsReadOnly
	}
	next := slices.Clone(list)
	next[idx].Shortcut = accel
	if slot.global {
		k.cur.GlobalShortcuts = next
	} else {
		k.cur.Shortcuts = next
	}
	k.dirty = true
	k.mu.Unlock()
	k.signal()
	return nil
}

// setEnableGlobal 改「启用全局快捷键」那个开关。
func (k *shortcutKeys) setEnableGlobal(on bool) error {
	k.mu.Lock()
	if k.ro {
		k.mu.Unlock()
		return errShortcutsReadOnly
	}
	k.cur.EnableGlobalShortcuts = on
	k.dirty = true
	k.mu.Unlock()
	k.signal()
	return nil
}

// reset 把**每一列的每一个键**恢复成默认值（Biu 的「恢复默认」按钮）。
//
// Biu 是整份替换（`reset()` 之后 `update({shortcuts: defaultShortcutSettings.shortcuts})`），
// 这里逐列逐条覆盖：命令的**条数**以用户文件里有的为准（`mergeShortcutList` 把用户
// 多出来的那几条留着），只把对得上的那些键改回默认。加一条新命令之后点「恢复默认」
// 不会把用户自己加的键抹掉。
func (k *shortcutKeys) reset() error {
	k.mu.Lock()
	if k.ro {
		k.mu.Unlock()
		return errShortcutsReadOnly
	}
	def := defaultShortcutSettings()
	k.cur.EnableGlobalShortcuts = def.EnableGlobalShortcuts
	k.cur.Shortcuts = resetShortcutList(k.cur.Shortcuts, def.Shortcuts)
	k.cur.GlobalShortcuts = resetShortcutList(k.cur.GlobalShortcuts, def.GlobalShortcuts)
	k.dirty = true
	k.mu.Unlock()
	k.signal()
	return nil
}

// resetShortcutList 把 list 里能按 id 对上默认的那几条改回去，其余原样保留。
func resetShortcutList(list, def []shortcutItem) []shortcutItem {
	out := slices.Clone(list)
	for i := range out {
		if d, ok := findShortcut(out[i].ID, def); ok {
			out[i].Shortcut = d.Shortcut
		}
	}
	return out
}

func findShortcut(id string, list []shortcutItem) (shortcutItem, bool) {
	idx := slices.IndexFunc(list, func(it shortcutItem) bool { return it.ID == id })
	if idx < 0 {
		return shortcutItem{}, false
	}
	return list[idx], true
}

// signal 告诉后台去落盘。缓冲区满了说明已经有一个在排队，而请求的内容是「最新的
// 快捷键」而不是「一次改动」，丢掉一个没有影响。
func (k *shortcutKeys) signal() {
	select {
	case k.req <- struct{}{}:
	default:
	}
}

// loop 是后台落盘的循环：收到请求后等一个 shortcutWriteDelay，把这段时间里的
// 连续改动一起写掉，然后睡到下一次请求。
func (k *shortcutKeys) loop() {
	defer close(k.done)
	var timer *time.Timer
	for {
		var tick <-chan time.Time
		if timer != nil {
			tick = timer.C
		}
		select {
		case <-k.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-k.req:
			if timer == nil {
				timer = time.NewTimer(shortcutWriteDelay)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(shortcutWriteDelay)
			}
		case <-tick:
			timer = nil
			// 失败不重试：磁盘满、目录没了这类问题重试也是白转，还会把错误刷屏。
			// 下一次改动会自己再试一次。
			_ = k.flush()
		}
	}
}

func (k *shortcutKeys) flush() error {
	k.mu.Lock()
	// ⚠️ `k.path == ""` 也在这一支里：内存里那份**没有**地方可写，让它落到
	// writeShortcutsAtomic 里就是拿一个空路径去 rename，Windows 上直接报错。
	if k.path == "" || !k.dirty || k.ro {
		k.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(shortcutFile{SchemaVersion: shortcutSchema, shortcutSettings: k.cur})
	// 先清 dirty 再写：写失败要放回去（下面），写成功就不用再放。
	k.dirty = false
	k.mu.Unlock()

	if err == nil {
		err = writeShortcutsAtomic(k.path, b)
	}
	if err != nil {
		// 放回去，否则这次改动永远丢了：内存里有、磁盘上没有，关掉就没了。
		k.mu.Lock()
		k.dirty = true
		k.mu.Unlock()
	}
	return err
}

// Close 停掉后台写、把还欠着的改动落完。可以重复调。
//
// ⚠️ 「录了一个 → 关窗 → 下次打开还在」靠的就是这一次同步落盘。
func (k *shortcutKeys) Close() error {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil
	}
	k.closed = true
	k.mu.Unlock()

	close(k.quit)
	<-k.done
	return k.flush()
}

// writeShortcutsAtomic 原子写：临时文件 → fsync → rename。
//
// 和设置那边逐字同构（`internal/app/settings.go` 的 writeSettingsAtomic）。
func writeShortcutsAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".shortcuts-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if _, err = tmp.Write(b); err != nil {
		return err
	}
	// fsync 的是数据。数据落盘了但目录项没有的话，断电之后可能还是旧文件名。
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// CreateTemp 建出来的是 0600。快捷键本身不是秘密，但里面有用户用了哪些键的
	// 习惯，给成普通用户配置同样的权限。
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncShortcutsDir(dir)
}

// syncShortcutsDir 把目录项本身刷到盘上。Windows 上打不开目录句柄，跳过——那边
// rename 本身就已经是原子的。
func syncShortcutsDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// shortcutPath 是快捷键这份文件在哪。
func shortcutPath(dir string) string { return filepath.Join(dir, shortcutFileName) }

// conflictOf 找出与 accel 撞车的那一条（同一列之内）。
//
// 空串不算冲突：Biu 的 `handleChangeShortcut` 也是这么写的（「空快捷键不会冲突」），
// 好几条都不设键是正常的，不该把这也说成错。
func conflictOf(list []shortcutItem, id, accel string) (shortcutItem, bool) {
	if accel == "" {
		return shortcutItem{}, false
	}
	for _, it := range list {
		if it.ID != id && it.Shortcut == accel {
			return it, true
		}
	}
	return shortcutItem{}, false
}
