package app

import (
	"os"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/localmusic"
)

// 本地文件的响度扫描：开着音量均衡时，在后台把整首解一遍量出响度，量完告诉
// 播放器（见 audio.MeasureFile / Player.SetLoudness）。量完之前播放器先按开头
// 几秒估，量完后换成整首的增益，过渡是渐变的。
//
// 三档，按成本从低到高：
//
//  1. 文件自带的 ReplayGain 标签（localmusic.ReplayGain）——零解码，有就
//     直接用。标签值是「相对 -18 LUFS 参考的 dB 调整」，折成 LUFS/Peak 给
//     播放器（见 rgToLoudness）。
//  2. 响度缓存（a.loudID，按曲目 Identity，见 loudness_cache.go）——实时量
//     过的结果，同一首歌第二次放直接用。
//  3. 都没有才真去解整首（measureFile）——量完按 Identity 存进缓存。
//
// 下面那份 loudKey（路径+大小+mtime）是「这份文件变没变」的防串味口径，
// 和 Identity 不是一回事，所以两张表并存。

// rgReferenceLUFS 是 ReplayGain 规范的参考响度：-89 dB SPL ≈ -18 LUFS。
// 标签里的 gain 是相对这个数的调整，折成 LUFS 才能喂给 audio.Loudness。
const rgReferenceLUFS = -18.0

// rgToLoudness 把 ReplayGain 标签折成播放器要的 Loudness。
//
// 没写 peak（0）时给 1.0——「不知道峰值」和「峰值是 0（无声）」不能混：
// 给 0 的话 knownGain 那边会当成「没有峰值可挡」永远不抬；给 1.0 是
// 「满幅」，抬的幅度被它挡在 0 dB 以内，安全。
func rgToLoudness(gainDB, peak float64) audio.Loudness {
	if peak <= 0 {
		peak = 1.0
	}
	return audio.Loudness{LUFS: rgReferenceLUFS - gainDB, Peak: peak}
}

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

// MeasureFileForTest 是量响度的入口，做成导出字段只为了测试能换成假的
// （和生产用的 `audio.MeasureFile` 同一份，测试整个换掉它）。
var MeasureFileForTest = audio.MeasureFile

// startLoudnessScan 在后台给 p 量 path 的响度。**不拿 a.mu 的情况下调**或者拿着调都行：
// 它只开 goroutine，goroutine 里才碰锁。
func (a *App) startLoudnessScan(p *audio.Player, path string) {
	if p == nil || path == "" {
		return
	}
	id := playbackIDLocal + path
	// 第一档：文件自带的 ReplayGain 标签。
	if gainDB, peak, ok := localmusic.ReplayGain(path); ok {
		p.SetLoudness(rgToLoudness(gainDB, peak))
		return
	}
	// 第二档：这首实时量过，直接给。
	if l, ok := a.loudID.get(id); ok {
		p.SetLoudness(l)
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
			if l, err = MeasureFileForTest(path); err != nil {
				return
			}
			a.loud.put(k, l)
			a.loudID.put(id, l)
		}
		a.mu.Lock()
		cur := a.player == p
		a.mu.Unlock()
		if cur {
			p.SetLoudness(l)
		}
	}()
}
