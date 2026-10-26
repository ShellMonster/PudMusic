package app

import (
	"os"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// 本地文件的响度扫描：开着音量均衡时，在后台把整首解一遍量出响度，量完告诉
// 播放器（见 audio.MeasureFile / Player.SetLoudness）。量完之前播放器先按开头
// 几秒估，量完后换成整首的增益，过渡是渐变的。
//
// 结果按「路径 + 大小 + 修改时间」记在内存里，同一首歌不量第二遍。

type loudKey struct {
	path string
	size int64
	mod  int64
}

type loudnessCache struct {
	mu sync.Mutex
	m  map[loudKey]audio.Loudness
}

func (c *loudnessCache) get(k loudKey) (audio.Loudness, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.m[k]
	return l, ok
}

func (c *loudnessCache) put(k loudKey, l audio.Loudness) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[loudKey]audio.Loudness{}
	}
	c.m[k] = l
}

// measureFile 是量响度的入口，做成字段只为了测试能换成假的。
var measureFile = audio.MeasureFile

// startLoudnessScan 在后台给 p 量 path 的响度。**不拿 a.mu 的情况下调**或者拿着调都行：
// 它只开 goroutine，goroutine 里才碰锁。
func (a *App) startLoudnessScan(p *audio.Player, path string) {
	if p == nil || path == "" {
		return
	}
	go func() {
		st, err := os.Stat(path)
		if err != nil {
			return
		}
		k := loudKey{path, st.Size(), st.ModTime().UnixNano()}
		l, ok := a.loud.get(k)
		if !ok {
			if l, err = measureFile(path); err != nil {
				return
			}
			a.loud.put(k, l)
		}
		a.mu.Lock()
		cur := a.player == p
		a.mu.Unlock()
		if cur {
			p.SetLoudness(l)
		}
	}()
}
