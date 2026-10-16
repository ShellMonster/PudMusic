// 搜索历史：Biu 的 `src/store/search-history.ts`，一条一条照它。
//
// ## 为什么是单独一份文件，不进 app.Settings
//
// Biu 自己也是**单独一个 store**（zustand persist 的 `"search-history"`），
// `app-settings.json` 里根本没有这个键（`shared/settings/app-setting.d.ts` 逐字
// 对过）。照抄就是这一份文件。
//
// 顺带也避开了往 `internal/app` 里加键——那边 `settings_test.go` 的 `biuKeys` 和
// `ourOwnKeys` 是两张封闭的表，加一个键就得同时改那两张，而那一块不是这张卡片的
// 活儿。
//
// 落盘的做法照 `internal/app/settings.go`：schema 号 + 临时文件 + rename 原子写 +
// 后台合并写 + 收尾同步落完。少任何一件都能想出坏法——只 merge 不 rename，写到
// 一半断电会留下半个文件；只 rename 不带 schema 号，就分不清这份文件是哪个版本
// 写的。
//
// ## 设置里的 `showSearchHistory` 只管显不显示，不管存不存
//
// Biu 就是这样（`layout/navbar/search/index.tsx:121-124` 只在 topContent 里判
// `showSearchHistory`）：用户在设置里把它关掉，历史照样还在文件里，重新打开又
// 回来了。关掉只该是「我不想看见」，不该是「替我把它们删了」。
package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// searchHistoryFileName 是搜索历史这份文件的名字。
	searchHistoryFileName = "search-history.json"

	// searchHistorySchema 是这份文件的格式版本。改它的**结构**（加键、删键、
	// 改类型）就涨它，并往 searchHistoryMigrations 里加一条 v→v+1 的路。
	searchHistorySchema = 1

	// searchHistoryShown 是浮层上最多摆几条。Biu 是 `items.slice(0, 10)`
	// （`layout/navbar/search/index.tsx:141`）。文件里存的比这多。
	searchHistoryShown = 10

	// searchHistoryStored 是文件里最多留几条。
	//
	// Biu 的 store 不设上限，一年下来能攒几千条：一个只用来「回看最近搜过什么」
	// 的列表存那么多，读的时候要全量反序列化，收益却全在最近那十条上。多出来
	// 的从**最旧的一头**丢。
	searchHistoryStored = 50

	// searchHistoryWriteDelay 是把连续改动合并成一次落盘的窗口。和设置那边的
	// writeDebounce 是同一个量级（那边是 120ms）。
	searchHistoryWriteDelay = 120 * time.Millisecond
)

// searchItem 是一条历史。Biu 的 `SearchItem` 就是这两个字段。
type searchItem struct {
	Value string `json:"value"`
	// Time 是记下这条的时刻，Unix 毫秒。Biu 用 `Date.now()`。
	Time int64 `json:"time"`
}

// searchHistoryFile 是落盘的整体。
//
// 外面这层信封只有设置那边才有（那边要和 Biu 的 electron-store 双向兼容，
// 才需要分得出「有没有 appSettings 键」）。这一份没有别的应用会来读它，信封就
// 只是把版本号记下来。
type searchHistoryFile struct {
	SchemaVersion int          `json:"schemaVersion"`
	Items         []searchItem `json:"items"`
}

// searchHistoryMigrations 把 v 升到 v+1。现在没有要迁的——留着是为了以后加键时
// 有一条明着的地方可写，而不是「到时候再说」。
var searchHistoryMigrations = map[int]func(*searchHistoryFile){}

// searchHistory 是搜索历史。
//
// **零值不可用**，用 newSearchHistory。path 为空就是只在内存里：没挂上盘，或者
// 拿不到应用数据目录——那时搜索照常能用，只是关掉之后不记得搜过什么。
type searchHistory struct {
	path string
	// readOnly 记着这份文件是别的版本写的，一个字都不许写回去（见 read）。
	readOnly bool

	mu     sync.Mutex
	items  []searchItem
	dirty  bool
	closed bool

	req  chan struct{}
	quit chan struct{}
	done chan struct{}
}

// newSearchHistory 开一份历史。path 为空就是内存里的。
//
// 读盘在调用方的 goroutine 上同步做完（一份几百字节的文件），后台写盘另起一个
// goroutine。
func newSearchHistory(path string) *searchHistory {
	h := &searchHistory{
		path: path,
		req:  make(chan struct{}, 1),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	if path != "" {
		h.items, h.readOnly = readSearchHistory(path)
	}
	go h.loop()
	return h
}

// readSearchHistory 读盘。坏文件不是错误：备份走，返回空的一份接着用。
//
// 「读不出来」和「读出来是空的」必须分开说：前者要留一份 `.bak` 让用户知道自己
// 丢了什么，后者是正常状态（还没搜过）。
//
// 第二个返回值是「这份文件只读吗」：来自更新的 schema 版本，或者根本认不出是第几
// 版。两种情况下**一个字都不写回去**——按旧语义改一遍就毁了，和设置那边的
// ErrSettingsFromNewer 同一个道理（`internal/app/settings.go`）。
func readSearchHistory(path string) (items []searchItem, readOnly bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f searchHistoryFile
	if err := json.Unmarshal(b, &f); err != nil {
		backupSearchHistory(path)
		return nil, false
	}
	if f.SchemaVersion > searchHistorySchema {
		return nil, true
	}
	for v := f.SchemaVersion; v < searchHistorySchema; v++ {
		m, ok := searchHistoryMigrations[v]
		if !ok {
			// 没有迁移的路就别改它：带着一份读不懂的文件原样留着，好过按错误的
			// 语义重写一遍用户的文件。
			return nil, true
		}
		m(&f)
	}
	return f.Items, false
}

// backupSearchHistory 把读不了的那份挪成 `<path>.bak` 留着。
//
// 挪不动也不报错——已经出过一次问题了，不能因为留个证据再报一次。
func backupSearchHistory(path string) { _ = os.Rename(path, path+".bak") }

// add 记下搜过的一个词。Biu 的 `add`：去重、把新的顶到最前。
//
// 空串和纯空格不记：Biu 的 `submitSearch` 先 `if (!keyword?.trim()) return`
// （`search/index.tsx:54`），同一个门槛在这儿——不然浮层上会出现一条搜不出任何
// 东西的历史。
func (h *searchHistory) add(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	h.mu.Lock()
	if h.readOnly {
		h.mu.Unlock()
		return
	}
	h.items = slices.DeleteFunc(h.items, func(it searchItem) bool { return it.Value == value })
	h.items = append([]searchItem{{Value: value, Time: time.Now().UnixMilli()}}, h.items...)
	if len(h.items) > searchHistoryStored {
		h.items = h.items[:searchHistoryStored]
	}
	h.dirty = true
	h.mu.Unlock()
	h.signal()
}

// remove 删掉一条（浮层上那个 ×）。Biu 的 `delete` 按值删。
func (h *searchHistory) remove(value string) {
	h.mu.Lock()
	before := len(h.items)
	h.items = slices.DeleteFunc(h.items, func(it searchItem) bool { return it.Value == value })
	if len(h.items) != before {
		h.dirty = true
	}
	h.mu.Unlock()
	h.signal()
}

// clear 清空全部。Biu 的 `clearSearchHistory`（浮层右上角那个「清除全部」）。
func (h *searchHistory) clear() {
	h.mu.Lock()
	if len(h.items) == 0 {
		h.mu.Unlock()
		return
	}
	h.items = nil
	h.dirty = true
	h.mu.Unlock()
	h.signal()
}

// list 是浮层上要摆的那几条：最近的在前，最多 searchHistoryShown 条。
func (h *searchHistory) list() []searchItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.items[:min(len(h.items), searchHistoryShown)])
}

// signal 告诉后台去落盘。缓冲区满了说明已经有一个在排队，而请求的内容是「最新的
// 历史」而不是「一次改动」，丢掉一个没有影响。
func (h *searchHistory) signal() {
	select {
	case h.req <- struct{}{}:
	default:
	}
}

// loop 是后台落盘的循环。
func (h *searchHistory) loop() {
	defer close(h.done)
	var timer *time.Timer
	for {
		var tick <-chan time.Time
		if timer != nil {
			tick = timer.C
		}
		select {
		case <-h.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-h.req:
			if timer == nil {
				timer = time.NewTimer(searchHistoryWriteDelay)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(searchHistoryWriteDelay)
			}
		case <-tick:
			timer = nil
			// 失败不重试：磁盘满、目录没了这类问题重试也是白转，还会把错误刷屏。
			// 下一次改动会自己再试一次。
			_ = h.flush()
		}
	}
}

func (h *searchHistory) flush() error {
	h.mu.Lock()
	// ⚠️ `h.path == ""` 也在这一支里：内存里那份**没有**地方可写，让它落到
	// writeSearchHistoryAtomic 里就是拿一个空路径去 rename，Windows 上直接报错。
	if h.path == "" || !h.dirty || h.readOnly {
		h.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(searchHistoryFile{
		SchemaVersion: searchHistorySchema,
		Items:         h.items,
	})
	// 先清 dirty 再写：写失败要放回去（下面），写成功就不用再放。
	h.dirty = false
	h.mu.Unlock()

	if err == nil {
		err = writeSearchHistoryAtomic(h.path, b)
	}
	if err != nil {
		// 放回去，否则这次改动永远丢了：内存里有、磁盘上没有，关掉就没了。
		h.mu.Lock()
		h.dirty = true
		h.mu.Unlock()
	}
	return err
}

// Close 停掉后台写、把还欠着的改动落完。可以重复调。
//
// ⚠️ 「搜过 → 关窗 → 下次打开还记得」靠的就是这一次同步落盘。
func (h *searchHistory) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	close(h.quit)
	<-h.done
	return h.flush()
}

// writeSearchHistoryAtomic 原子写：临时文件 → fsync → rename。
//
// 和设置那边逐字同构（`internal/app/settings.go` 的 writeSettingsAtomic）。
//
// ⚠️ CreateTemp 建出来的是 0600，这里改成 0644：设置文件那边也要改，理由一样——
// 搜索历史里有用户搜过的词，拷一份给别人就等于把人家在搜什么交出去。
func writeSearchHistoryAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".search-history-*.tmp")
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
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncHistoryDir(dir)
}

// syncHistoryDir 把目录项本身刷到盘上。Windows 上打不开目录句柄，跳过——那边
// rename 本身就已经是原子的。
func syncHistoryDir(dir string) error {
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

// searchHistoryPath 是搜索历史这份文件在哪。
func searchHistoryPath(dir string) string { return filepath.Join(dir, searchHistoryFileName) }

// searchHistorySummary 把一条历史压成浮层上摆的那一小片字。
//
// 摆全值是 Biu 的做法（`Chip` 的 classNames 里 truncate，让它自己截断）。这里
// 限长是为了不让一条超长的关键词把浮层顶变宽——限制写在浮层那一层，存下来的
// 还是完整的一条。
func searchHistorySummary(v string) string {
	const max = 24
	r := []rune(v)
	if len(r) <= max {
		return v
	}
	return string(r[:max]) + "…"
}
