package app

import "github.com/ShellMonster/PudMusic/internal/service/lyrics"

// lyricMemoryLimit 是进程内歌词缓存留几首。
//
// 它只存在一次运行里，切回上一首时同步命中；8 首够用户来回比较，行数据也
// 不会攒成一份第二歌词库。
const lyricMemoryLimit = 8

// lyricMemoryEntry 是一首已经解析好的歌词。
//
// entry 里的偏移和字号是**用户数据**，和 rows 一起存：切回去时不该再读一次盘
// 才能恢复它们。rows 只读，界面拿到的就是这一份切片。
type lyricMemoryEntry struct {
	entry  lyrics.Entry
	rows   []lyrics.Row
	title  string
	artist string
}

// lyricMemory 是进程内的歌词 LRU。**调用方必须持有 a.mu**（和 App 里其他
// 歌词状态同一把锁）。
type lyricMemory struct {
	entries map[string]lyricMemoryEntry
	order   []string
}

// get 拿一首的缓存。身份判据和 `lyrics.Cache.Load` 一样：记录里有的曲名/歌手
// 和来问的对不上就当没有。
func (m *lyricMemory) get(key, title, artist string) (lyricMemoryEntry, bool) {
	if m.entries == nil {
		return lyricMemoryEntry{}, false
	}
	e, ok := m.entries[key]
	if !ok {
		return lyricMemoryEntry{}, false
	}
	if e.title != "" && title != "" && e.title != title {
		return lyricMemoryEntry{}, false
	}
	if e.artist != "" && artist != "" && e.artist != artist {
		return lyricMemoryEntry{}, false
	}
	m.touch(key)
	return e, true
}

// put 存一首已经解析好的歌词。没有行的那一份不存（没有歌词时读盘很便宜，
// 而「没有」不该在内存里占一个位置挡住以后自动找回来的词）。
func (m *lyricMemory) put(key string, e lyricMemoryEntry) {
	if key == "" {
		return
	}
	if len(e.rows) == 0 {
		delete(m.entries, key)
		return
	}
	if m.entries == nil {
		m.entries = map[string]lyricMemoryEntry{}
	}
	m.entries[key] = e
	m.touch(key)
	for len(m.order) > lyricMemoryLimit {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.entries, oldest)
	}
}

// updateEntry 把用户调过的偏移/字号写回内存里的那一份，行本身不动。
func (m *lyricMemory) updateEntry(key string, entry lyrics.Entry) {
	e, ok := m.entries[key]
	if !ok {
		return
	}
	e.entry = entry
	m.entries[key] = e
}

// touch 把 key 挪到 LRU 的末尾。不在表里的键直接返回。
func (m *lyricMemory) touch(key string) {
	for i, k := range m.order {
		if k != key {
			continue
		}
		m.order = append(append(m.order[:i:i], m.order[i+1:]...), key)
		return
	}
	m.order = append(m.order, key)
}
