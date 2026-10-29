// 快捷键存储：当前版本的 schema 文件、原子写和后台合并写。

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
	// shortcutFileName 是当前快捷键文件的名字。
	shortcutFileName = "shortcuts.json"

	// shortcutSchema 是当前快捷键文件的格式版本。
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

// shortcutItem 是一条快捷键。冲突状态只在内存中计算，不落盘。
type shortcutItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Shortcut string `json:"shortcut"`
}

// shortcutSettings 是落盘的整体。
type shortcutSettings struct {
	EnableGlobalShortcuts bool           `json:"enableGlobalShortcuts"`
	Shortcuts             []shortcutItem `json:"shortcuts"`
	GlobalShortcuts       []shortcutItem `json:"globalShortcuts"`
}

// shortcutFile 是当前版本的快捷键文件信封。匿名嵌入保证快捷键字段落在顶层。
type shortcutFile struct {
	SchemaVersion int `json:"schemaVersion"`
	shortcutSettings
}

// shortcutSlot 是设置页上那一格：哪一条命令（id）、哪一列（应用内/全局）。
//
// Comparable 是硬要求：`ui.FocusBind` 收的就是一个**可比较**的字段的指针，靠它把
// 「焦点在哪一格」和「正在录哪一格」绑成同一件事（见 shortcut_keys.go）。
type shortcutSlot struct {
	global bool
	id     string
}

// defaultShortcutSettings 返回应用内和全局快捷键的默认值。
//
// 全局快捷键比应用内快捷键多一个 ⌥，避免两者使用同一个组合键时互相触发。
// 快退和快进只登记在应用内，不占用其他应用的全局组合键。
func defaultShortcutSettings() shortcutSettings {
	var ids = []struct{ id, key string }{
		{"togglePlay", "P"},
		{"prev", "Left"},
		{"next", "Right"},
		{"volumeUp", "Up"},
		{"volumeDown", "Down"},
		{"toggleMiniMode", "M"},
	}
	in := make([]shortcutItem, 0, len(ids)+len(inAppOnlyShortcuts))
	global := make([]shortcutItem, 0, len(ids))
	for _, it := range ids {
		// CommandOrControl 在 macOS 上表示 ⌘，其他平台表示 Ctrl。
		in = append(in, shortcutItem{ID: it.id, Name: defaultShortcutNames[it.id], Shortcut: "CommandOrControl+" + it.key})
		global = append(global, shortcutItem{ID: it.id, Name: defaultShortcutNames[it.id], Shortcut: "CommandOrControl+Alt+" + it.key})
	}
	// 只在应用内的那几条。**用 ⇧ 加方向键，不用裸方向键**：裸方向键是内容区
	// 键盘走位的那一对（列表 / 表格的上下左右，见 focus.go 的文件头），认领了
	// 它们走位就断了。
	for _, it := range inAppOnlyShortcuts {
		in = append(in, shortcutItem{ID: it.id, Name: defaultShortcutNames[it.id], Shortcut: it.key})
	}
	return shortcutSettings{
		EnableGlobalShortcuts: true,
		Shortcuts:             in,
		GlobalShortcuts:       global,
	}
}

// inAppOnlyShortcuts 是只登记在应用内、没有全局那一半的命令：id 和默认键。
// 快退和快进是播放器内的操作，不占用系统级快捷键。
var inAppOnlyShortcuts = []struct{ id, key string }{
	{"seekBack", "Shift+Left"},
	{"seekForward", "Shift+Right"},
}

// defaultShortcutNames 是每条命令的显示名称。
var defaultShortcutNames = map[string]string{
	"togglePlay":     "播放/暂停",
	"prev":           "上一首",
	"next":           "下一首",
	"volumeUp":       "音量加",
	"volumeDown":     "音量减",
	"toggleMiniMode": "切换mini/完整模式",
	"seekBack":       "快退 5 秒",
	"seekForward":    "快进 5 秒",
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
		// 没挂上盘或第一次运行时使用默认快捷键。
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

// probeShortcuts 解析当前版本的快捷键文件。
func probeShortcuts(raw []byte) (from int, s shortcutSettings, ok bool, err error) {
	var probe struct {
		SchemaVersion *int            `json:"schemaVersion"`
		Shortcuts     *[]shortcutItem `json:"shortcuts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, shortcutSettings{}, false, err
	}
	if probe.SchemaVersion == nil || probe.Shortcuts == nil {
		return 0, shortcutSettings{}, false, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, shortcutSettings{}, false, err
	}
	return *probe.SchemaVersion, s, true, nil
}

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
	if !ok || from != shortcutSchema {
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

// reset 把每一列的每一个键恢复成默认值。
// 用户自己增加的命令仍然保留，只重置能与默认命令对应的快捷键。
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
// 空串不算冲突：多个命令都可以不设置快捷键。
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
