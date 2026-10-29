// 搜索历史：独立文件保存，按时间倒序维护最近搜索词。
//
// 独立文件便于限制存储数量，并与应用设置的生命周期分开。
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

	// searchHistorySchema 是当前搜索历史文件的格式版本。
	searchHistorySchema = 1

	// searchHistoryShown 是浮层上最多显示的条数。文件里存的比这多。
	searchHistoryShown = 10

	// searchHistoryStored 是文件里最多留几条。
	//
	// 文件里最多保留 50 条，超出时从最旧的一头丢弃。
	searchHistoryStored = 50

	// searchHistoryWriteDelay 是把连续改动合并成一次落盘的窗口。和设置那边的
	// writeDebounce 是同一个量级（那边是 120ms）。
	searchHistoryWriteDelay = 120 * time.Millisecond
)

// searchItem 是一条搜索历史记录。
type searchItem struct {
	Value string `json:"value"`
	// Time 是记录搜索词的时刻，单位是 Unix 毫秒。
	Time int64 `json:"time"`
}

// searchHistoryFile 是当前版本的搜索历史文件。
type searchHistoryFile struct {
	SchemaVersion int          `json:"schemaVersion"`
	Items         []searchItem `json:"items"`
}

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

// readSearchHistory 只接受当前版本的搜索历史文件；旧版本或无法识别的文件保持原样并只读。
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
	if f.SchemaVersion != searchHistorySchema {
		return nil, true
	}
	return f.Items, false
}

// backupSearchHistory 把读不了的那份挪成 `<path>.bak` 留着。
//
// 挪不动也不报错——已经出过一次问题了，不能因为留个证据再报一次。
func backupSearchHistory(path string) { _ = os.Rename(path, path+".bak") }

// add 记录一个搜索词：去重，并把新的记录放到最前面。
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

// remove 删除一条搜索历史记录。
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

// clear 清空全部搜索历史。
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
// 限长是为了不让一条超长关键词把浮层撑宽；存储内容仍然保留完整关键词。
func searchHistorySummary(v string) string {
	const max = 24
	r := []rune(v)
	if len(r) <= max {
		return v
	}
	return string(r[:max]) + "…"
}
