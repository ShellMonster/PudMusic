// 全屏播放器自己那一份偏好：频谱开不开、什么颜色。
//
// ## 为什么不进 `app.Settings`
//
// 两层理由，和更新器那份（`update_prefs.go`）一样：
//
//  1. **Biu 自己也是单独一个 store**。`biu/src/store/full-screen-player-settings.ts`
//     落盘的是 `full-screen-player-settings.json`，`app-settings.json` 里根本没有
//     那些键。照抄就是这一份。
//  2. 顺带避开了往 `Settings` 里加键——那边 `settings_test.go` 的 `biuKeys` 和
//     `ourOwnKeys` 是两张封闭的表，加一个键就得同时改那两张。
//
// ## 这一份里**只有**频谱那两样
//
// Biu 那一个 store 有七样（4 个开关 + 3 个颜色）。这里只留频谱开关和频谱颜色：
// 封面开关、歌词开关、模糊背景开关都是**别的票的活**（歌词是 BMG-65，毛玻璃
// 是阶段 6 的视觉取舍），先把它们摆上再让用户去调，调不动的东西比没有更坏。
//
// ## 零值就是默认值
//
// `FullPlayerPrefs{}` 恰好等于 Biu 的 `defaultSettings` 里我们要的那两样：
// 频谱默认**关**，颜色空串（= `#666`，见 `internal/ui/fullplayer_spectrum.go`）。
// 所以「挂上之前」和「挂上之后文件里没有」这两种情形给用户的界面一模一样，
// 不需要在这里补一份默认值。

package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	// fullPlayerPrefsFileName 是这份文件的名字，和 Biu 的 store 名对上。
	fullPlayerPrefsFileName = "full-screen-player.json"
	// fullPlayerPrefsSchema 是这份文件的格式版本。改它的**结构**就涨它。
	fullPlayerPrefsSchema = 1
)

// FullPlayerPrefs 是全屏播放器的偏好。
//
// 键名和 Biu 那一个 store 一样（`store/full-screen-player-settings.ts` 的
// `partialize`），将来从 Biu 迁过来的时候认得。
type FullPlayerPrefs struct {
	// ShowSpectrum 频谱画不画。**默认 false**：Biu 的 `defaultSettings` 就是
	// `showSpectrum: false`，用户得自己开。
	ShowSpectrum bool `json:"showSpectrum"`
	// SpectrumColor 是频谱条的颜色（`#rrggbb`）。空串 = 用默认的 `#666`。
	//
	// Biu 那边存的是字符串 `"currentColor"`，画的时候再判断一次是不是它、是就
	// 落到 `#666`（`audio-waveform/index.tsx:110`）。我们把那次判断提前到读设置
	// 这一步：空串和 `#666` 在界面上是一回事，而存一个要每次画的时候再解释的
	// 魔法值，只会让「用户到底选了哪个颜色」这件事永远说不清。
	SpectrumColor string `json:"spectrumColor,omitempty"`
}

// FullPlayerPrefsPath 报这份文件的位置。dir 为空返回空串（= 只在内存里）。
//
// ⚠️ dir 为空**不能**拿 `filepath.Join("", …)` 凑：那是当前工作目录下的一个文件。
// 用户从 Finder 双击启动时工作目录是 `/`，那份文件就写到 `/` 下面去了
// （理由同 `UpdatePrefsPath`）。
func FullPlayerPrefsPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fullPlayerPrefsFileName)
}

// fullPlayerPrefsStore 是全屏播放器偏好在内存里和磁盘上的那一份。
//
// 零值可用（path 为空就是只在内存里）：拿不到应用数据目录时频谱开关照样能用，
// 只是关掉应用之后不记得。
type fullPlayerPrefsStore struct {
	path string

	mu   sync.Mutex
	cur  FullPlayerPrefs
	save error
}

// load 读盘，读不到就用零值。**在挂上的时候调一次**。
//
// 坏文件、别的版本写的、读不出来——三种都当没有，和更新器那份同一个理由：
// 这一份丢了最坏的结果是「频谱默认关着」，而频谱本来就该默认关着。
// （和 `LoadSettings` 那边不一样是有道理的：那边丢的是用户设过的东西。）
func (p *fullPlayerPrefsStore) load() {
	if p.path == "" {
		return
	}
	b, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var f struct {
		SchemaVersion int             `json:"schemaVersion"`
		FullPlayer    FullPlayerPrefs `json:"fullPlayer"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return
	}
	if f.SchemaVersion > fullPlayerPrefsSchema {
		p.mu.Lock()
		p.path = ""
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	p.cur = f.FullPlayer
	p.normalizeLocked()
	p.mu.Unlock()
}

// get 返回现在这一份。挂上之前（p 是 nil）也给得出值：零值就是默认。
func (p *fullPlayerPrefsStore) get() FullPlayerPrefs {
	if p == nil {
		return FullPlayerPrefs{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur
}

// put 记下新的一份并落盘。
//
// ⚠️ 落盘写在**锁外面**：界面每帧要读这一份（`App.FullPlayerPrefs`），
// 拿着锁做一次文件写就是把界面的等待时间变成那一次写。
func (p *fullPlayerPrefsStore) put(next FullPlayerPrefs) {
	p.mu.Lock()
	p.cur = next
	p.normalizeLocked()
	next = p.cur
	path, prevErr := p.path, p.save
	p.mu.Unlock()

	if path == "" {
		return
	}
	b, err := json.MarshalIndent(struct {
		SchemaVersion int             `json:"schemaVersion"`
		FullPlayer    FullPlayerPrefs `json:"fullPlayer"`
	}{fullPlayerPrefsSchema, next}, "", "  ")
	if err == nil {
		err = writeFileAtomic(path, b)
	}
	if err != nil && prevErr == nil {
		p.mu.Lock()
		p.save = err
		p.mu.Unlock()
		log.Printf("biu: 全屏播放器偏好 %s 存不住：%v", path, err)
	}
}

// normalizeLocked 把明显不合理的值拉回合法区间（调用方拿着锁）。
//
// 只管颜色：用户可以直接编辑这个文件，`"spectrumColor": "红"` 不该一路穿到
// 画界面的那一行（那里 `parseHexColor` 认不出来会退回黑色，一条纯黑的频谱
// 摆在深色背景上等于没有）。
func (p *fullPlayerPrefsStore) normalizeLocked() {
	if p.cur.SpectrumColor != "" && !hexColor.MatchString(p.cur.SpectrumColor) {
		p.cur.SpectrumColor = ""
	}
}

// fullPlayerPrefs 是 App 上挂的那一份。没挂上是 nil，`App.FullPlayerPrefs`
// 判空之后给默认值。
func (a *App) MountFullPlayerPrefs(path string) {
	p := &fullPlayerPrefsStore{path: path}
	p.load()
	a.mu.Lock()
	a.fullPlayerPrefs = p
	a.mu.Unlock()
}

// FullPlayerPrefs 返回现在这一份偏好。没挂上磁盘时是默认（全屏播放器照样能用）。
func (a *App) FullPlayerPrefs() FullPlayerPrefs {
	a.mu.Lock()
	p := a.fullPlayerPrefs
	a.mu.Unlock()
	return p.get()
}

// SetFullPlayerPrefs 改全屏播放器的偏好并立刻落盘。
//
// 为什么是「立刻」而不是像设置那样合并写：这一份只有两个键，改它的动作是
// 「点一下开关」，一天点不了几次；而合并写要一个后台 goroutine 和一个停掉它
// 的钩子，为两键的东西不值当。
func (a *App) SetFullPlayerPrefs(next FullPlayerPrefs) {
	a.mu.Lock()
	if a.fullPlayerPrefs == nil {
		// 没挂上盘（拿不到应用数据目录）：建一份只在内存里的，让这一轮能用，
		// 而且**挂回 App 上**——不挂回去的话下一次改就又建一个，用户在这次会话
		// 里改的开关会自己跳回去。
		a.fullPlayerPrefs = &fullPlayerPrefsStore{}
	}
	p := a.fullPlayerPrefs
	wake := a.wake
	a.mu.Unlock()

	p.put(next)
	if wake != nil {
		wake()
	}
}
