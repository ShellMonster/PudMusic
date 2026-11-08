package app

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// 响度缓存：每首歌**实时量出来**的 LUFS 按 `Item.Identity()` 记住，同一首
// 第二次放直接用缓存，不再整首重解一遍（一遍几十 MB 解码，几秒 CPU）。
//
// # 键为什么是 Identity 而不是路径
//
// `loudness_scan.go` 里那份 `loudKey`（路径+大小+修改时间）是「同一份文件
// 变了没有」的口径——防的是文件被换。这里是「这首歌是谁」：Identity 对
// 本地项是 `local:<路径>`，正好把「换目录/换名」和「同一首歌」分开。同一
// 份键在队列去重、播放恢复那边也是它，口径只有一处（queue.go 的
// `trackKey` 注释写死了「它是判重口径的唯一出处」）。
//
// # 落盘
//
// 内存表按进程存活，重启就没了——每首歌第一次放都要重新量。加一个 JSON
// 落盘（`loudness.json`，跟设置文件同一个目录）：挂载设置时装进来，退出
// 时同步写。**读错/写错都不致命**：缓存只是省 CPU，丢了就是重新量一次。

// loudnessIdentityCache 是按曲目 Identity 的响度缓存。
//
// 键是 `Item.Identity()` 的返回串；值是「实时量出来的」响度——**标签来的
// 那一档不进这张表**（标签零成本，本来就不用缓存；混进来反而把「标签被
// 工具改过」这条更新路堵死）。
type loudnessIdentityCache struct {
	mu sync.Mutex
	m  map[string]audio.Loudness
	// dirty 记着有没有新条目没落盘——没有就不写，省一次无谓的文件重写。
	dirty bool
	// path 是落盘文件；空串表示「这次运行不落盘」（没挂设置目录）。
	path string
}

// loudnessCacheFile 是落盘文件名，和设置文件同一个目录。
const loudnessCacheFile = "loudness.json"

// load 从落盘文件把缓存装回来。文件不存在/坏了都当成「空缓存」，不报错——
// 理由见文件头。
func (c *loudnessIdentityCache) load(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]audio.Loudness
	if json.Unmarshal(b, &m) != nil || len(m) == 0 {
		return
	}
	if c.m == nil {
		c.m = map[string]audio.Loudness{}
	}
	for k, v := range m {
		c.m[k] = v
	}
}

func (c *loudnessIdentityCache) get(id string) (audio.Loudness, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.m[id]
	return l, ok
}

func (c *loudnessIdentityCache) put(id string, l audio.Loudness) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]audio.Loudness{}
	}
	c.m[id] = l
	c.dirty = true
}

// flush 把新条目落盘。CloseSettings 那一趟同步调它（同「改设置→杀进程→
// 重启→设置还在」一个道理：缓存丢了不致命，但每次都重量是白花的 CPU）。
func (c *loudnessIdentityCache) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty || c.path == "" {
		return
	}
	b, err := json.Marshal(c.m)
	if err != nil {
		return
	}
	if diskcache.WriteAtomicMode(c.path, b, 0o600) != nil {
		return
	}
	c.dirty = false
}
