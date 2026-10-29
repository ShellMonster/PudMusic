// 全屏播放器自己那一份偏好：五个开关、三个颜色。
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
// ## 键名和默认值都照 Biu 的 store
//
// Biu 那七样一个不少（4 个开关 + 3 个颜色）。默认值也照它：歌词、封面、虚化背景
// 开着，频谱关着，三个颜色空串（= 走各自的默认；见 `internal/ui/fullplayer.go` 的
// `fullForeground` 与 `fullplayer_spectrum.go` 的 `spectrumColorOf`）。
//
// ⚠️ **弹幕那一格是我们自己加的**（上游 Biu 没有弹幕）：一共**八样**。
//
// ⚠️ **默认值不是零值**：`FullPlayerPrefs{}` 是五个开关全关。所以读盘是「先摆一份
// 默认，再拿文件里有的键盖上去」——老版本写的那份文件只有 `showSpectrum` 和
// `spectrumColor`，新加的那些键不在里面，缺的那个必须落回默认，而不是落成 false。

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// fullPlayerPrefsFileName 是这份文件的名字，和 Biu 的 store 名对上。
	fullPlayerPrefsFileName = "full-screen-player.json"
	// fullPlayerPrefsSchema 是这份文件的格式版本。改它的**结构**就涨它。
	//
	// 2：从「频谱开关 + 频谱颜色」涨到 Biu 那七样。
	//
	// 3：加上「显示弹幕」那一个开关（**上游 Biu 没有弹幕**，所以这一项是
	// 我们自己加的，见 `FullPlayerPrefs.ShowDanmaku`）。
	//
	// ⚠️ **老文件照样读**：这一层判的是「文件比我新」（那就当没有），而老
	// 文件里**没有**的那一个键落回**默认**（弹幕默认开着）。
	fullPlayerPrefsSchema = 3
)

// FullPlayerPrefs 是全屏播放器的偏好。
//
// 键名和 Biu 那一个 store 一样（`store/full-screen-player-settings.ts` 的
// `partialize`），将来从 Biu 迁过来的时候认得。
type FullPlayerPrefs struct {
	// ShowLyrics 歌词画不画。默认 **true**（Biu 的 `defaultSettings.showLyrics`）。
	ShowLyrics bool `json:"showLyrics"`
	// ShowSpectrum 频谱画不画。**默认 true**（和 Biu 的 `showSpectrum: false` 不同，
	// 这是产品的取舍：播放页要有频谱）。
	ShowSpectrum bool `json:"showSpectrum"`
	// ShowCover 封面画不画。默认 **true**。
	ShowCover bool `json:"showCover"`
	// ShowBlurredBackground 背景用不用毛玻璃。默认 **true**；关掉时铺
	// `BackgroundColor`（空串 = 主题背景色）。
	ShowBlurredBackground bool `json:"showBlurredBackground"`
	// ShowDanmaku 弹幕画不画。默认 **true**。
	//
	// ⚠️ **上游 Biu 没有弹幕**（研究票 #538 的报告 §6 第 6 条），所以这一项
	// **不是照抄上游的键名**——它是我们自己加的。
	//
	// ⚠️ **默认开着**是产品要求的（用户 2026-10-26 明确要的）。
	ShowDanmaku bool `json:"showDanmaku"`
	// LyricsColor 是歌词的字色（`#rrggbb`）。空串 = 白（Biu 的默认值
	// `lyricsColor: "#ffffff"`）。
	LyricsColor string `json:"lyricsColor,omitempty"`
	// SpectrumColor 是频谱条的颜色（`#rrggbb`）。空串 = 用默认的 `#666`。
	//
	// Biu 那边存的是字符串 `"currentColor"`，画的时候再判断一次是不是它、是就
	// 落到 `#666`（`audio-waveform/index.tsx:110`）。我们把那次判断提前到读设置
	// 这一步：空串和 `#666` 在界面上是一回事，而存一个要每次画的时候再解释的
	// 魔法值，只会让「用户到底选了哪个颜色」这件事永远说不清。
	SpectrumColor string `json:"spectrumColor,omitempty"`
	// BackgroundColor 是关掉毛玻璃之后那层底色（`#rrggbb`）。空串 = 主题背景色
	// （Biu 的 `backgroundColor: undefined`）。
	BackgroundColor string `json:"backgroundColor,omitempty"`
}

// DefaultFullPlayerPrefs 是 Biu 的 `defaultSettings`。
//
// ⚠️ 这个结构体的**零值不等于默认值**（五个默认开着的开关会变成关）。读盘、
// 没挂盘、以及测试里「只改其中一样」都从这里起手。
func DefaultFullPlayerPrefs() FullPlayerPrefs {
	return FullPlayerPrefs{
		ShowLyrics:            true,
		ShowSpectrum:          true,
		ShowCover:             true,
		ShowBlurredBackground: true,
		ShowDanmaku:           true,
	}
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

	wmu sync.Mutex
	mu  sync.Mutex
	cur FullPlayerPrefs

	save   error
	dirty  bool
	closed bool
	gen    uint64

	req     chan struct{}
	quit    chan struct{}
	stopped chan struct{}
	flush   <-chan struct{}

	writeFile func(string, []byte) error
	wake      func()
}

// load 读盘，读不到就用默认。**在挂上的时候调一次**。
//
// 坏文件、别的版本写的、读不出来——三种都当没有，和更新器那份同一个理由：
// 这一份丢了最坏的结果是八个键全回到默认（频谱关着、另外四个开着）。
// （和 `LoadSettings` 那边不一样是有道理的：那边丢的是用户设过的东西。）
func (p *fullPlayerPrefsStore) load() {
	// 先摆默认：文件不存在、读不出来、或者没有这一份的目录，`get` 都该给出
	// Biu 的那一份默认，而不是三个开关全关的零值。
	p.mu.Lock()
	p.cur = DefaultFullPlayerPrefs()
	p.mu.Unlock()
	if p.path == "" {
		return
	}
	b, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	// ⚠️ **指针字段**：文件里**没有**的那个键要落回默认，而 `bool` 的零值会把
	// 「缺这一键」读成 false。老版本写的那份文件只有频谱那两样，少了这一层，
	// 升级之后歌词/封面/毛玻璃会一起变成关着的——而用户在文件里一个字节都没改。
	var f struct {
		SchemaVersion int `json:"schemaVersion"`
		FullPlayer    struct {
			ShowLyrics            *bool   `json:"showLyrics"`
			ShowSpectrum          *bool   `json:"showSpectrum"`
			ShowCover             *bool   `json:"showCover"`
			ShowBlurredBackground *bool   `json:"showBlurredBackground"`
			ShowDanmaku           *bool   `json:"showDanmaku"`
			LyricsColor           *string `json:"lyricsColor"`
			SpectrumColor         *string `json:"spectrumColor"`
			BackgroundColor       *string `json:"backgroundColor"`
		} `json:"fullPlayer"`
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
	cur := DefaultFullPlayerPrefs()
	if f.FullPlayer.ShowLyrics != nil {
		cur.ShowLyrics = *f.FullPlayer.ShowLyrics
	}
	if f.FullPlayer.ShowSpectrum != nil {
		cur.ShowSpectrum = *f.FullPlayer.ShowSpectrum
	}
	if f.FullPlayer.ShowCover != nil {
		cur.ShowCover = *f.FullPlayer.ShowCover
	}
	if f.FullPlayer.ShowBlurredBackground != nil {
		cur.ShowBlurredBackground = *f.FullPlayer.ShowBlurredBackground
	}
	if f.FullPlayer.ShowDanmaku != nil {
		cur.ShowDanmaku = *f.FullPlayer.ShowDanmaku
	}
	if f.FullPlayer.LyricsColor != nil {
		cur.LyricsColor = *f.FullPlayer.LyricsColor
	}
	if f.FullPlayer.SpectrumColor != nil {
		cur.SpectrumColor = *f.FullPlayer.SpectrumColor
	}
	if f.FullPlayer.BackgroundColor != nil {
		cur.BackgroundColor = *f.FullPlayer.BackgroundColor
	}
	p.mu.Lock()
	p.cur = cur
	p.normalizeLocked()
	p.mu.Unlock()
}

// get 返回现在这一份。挂上之前（p 是 nil）也给得出值：Biu 的默认那一份。
func (p *fullPlayerPrefsStore) get() FullPlayerPrefs {
	if p == nil {
		return DefaultFullPlayerPrefs()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur
}

// put 记下新的一份，先更新内存，再通知后台合并写。
func (p *fullPlayerPrefsStore) put(next FullPlayerPrefs) FullPlayerPrefs {
	p.mu.Lock()
	p.cur = next
	p.normalizeLocked()
	next = p.cur
	if p.path != "" {
		p.dirty = true
	}
	p.gen++
	closed := p.closed
	p.mu.Unlock()

	if !closed && p.path != "" {
		p.signal()
	}
	return next
}

func (p *fullPlayerPrefsStore) signal() {
	if p.req == nil {
		return
	}
	select {
	case p.req <- struct{}{}:
	default:
	}
}

func (p *fullPlayerPrefsStore) start() {
	if p.path == "" || p.req != nil {
		return
	}
	p.req = make(chan struct{}, 1)
	p.quit = make(chan struct{})
	p.stopped = make(chan struct{})
	go p.loop()
}

func (p *fullPlayerPrefsStore) Flush() error {
	p.wmu.Lock()
	defer p.wmu.Unlock()

	p.mu.Lock()
	if !p.dirty || p.path == "" {
		err := p.save
		p.mu.Unlock()
		return err
	}
	version, next, path := p.gen, p.cur, p.path
	p.mu.Unlock()

	err := p.write(path, next)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		wrapped := fmt.Errorf("app: 全屏播放器偏好存不住：%w", err)
		p.save = wrapped
		p.dirty = true
		if p.wake != nil {
			p.wake()
		}
		return wrapped
	}
	if p.gen == version {
		p.dirty = false
	}
	p.save = nil
	if p.wake != nil {
		p.wake()
	}
	return nil

}

func (p *fullPlayerPrefsStore) write(path string, next FullPlayerPrefs) error {
	b, err := json.MarshalIndent(struct {
		SchemaVersion int             `json:"schemaVersion"`
		FullPlayer    FullPlayerPrefs `json:"fullPlayer"`
	}{fullPlayerPrefsSchema, next}, "", "  ")
	if err == nil {
		if p.writeFile != nil {
			err = p.writeFile(path, b)
		} else {
			err = writeFileAtomic(path, b)
		}
	}
	if err != nil {
		log.Printf("biu: 全屏播放器偏好 %s 存不住：%v", path, err)
	}
	return err
}

func (p *fullPlayerPrefsStore) Close() error {
	p.mu.Lock()
	if p.closed {
		err := p.save
		p.mu.Unlock()
		return err
	}
	p.closed = true
	p.mu.Unlock()

	if p.quit != nil {
		close(p.quit)
		<-p.stopped
	}
	return p.Flush()
}

func (p *fullPlayerPrefsStore) loop() {
	defer close(p.stopped)
	var timer *time.Timer
	for {
		var tick <-chan time.Time
		if timer != nil {
			tick = timer.C
		}
		select {
		case <-p.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-p.req:
			if p.flush != nil {
				select {
				case <-p.flush:
				case <-p.quit:
					return
				}
				_ = p.Flush()
				continue
			}
			if timer == nil {
				timer = time.NewTimer(writeDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(writeDebounce)
			}
		case <-tick:
			timer = nil
			_ = p.Flush()
		}
	}
}

// fullPlayerPrefsError 返回最近一次异步落盘失败。
func (p *fullPlayerPrefsStore) fullPlayerPrefsError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.save
}

// normalizeLocked 把明显不合理的值拉回合法区间（调用方拿着锁）。
//
// 只管颜色：用户可以直接编辑这个文件，`"spectrumColor": "红"` 不该一路穿到
// 画界面的那一行（那里 `parseHexColor` 认不出来会退回黑色，一条纯黑的频谱
// 摆在深色背景上等于没有；背景色和歌词色同理）。
func (p *fullPlayerPrefsStore) normalizeLocked() {
	for _, c := range []*string{
		&p.cur.LyricsColor, &p.cur.SpectrumColor, &p.cur.BackgroundColor,
	} {
		if *c != "" && !hexColor.MatchString(*c) {
			*c = ""
		}
	}
}

// fullPlayerPrefs 是 App 上挂的那一份。没挂上是 nil，`App.FullPlayerPrefs`
// 判空之后给默认值。
func (a *App) MountFullPlayerPrefs(path string) {
	a.mountFullPlayerPrefs(path, nil, nil)
}

func (a *App) mountFullPlayerPrefs(path string, writeFile func(string, []byte) error, flush <-chan struct{}) *fullPlayerPrefsStore {
	a.fullPlayerPrefsMu.Lock()
	defer a.fullPlayerPrefsMu.Unlock()

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	old := a.fullPlayerPrefs
	a.fullPlayerPrefs = nil
	a.fullPlayerPrefsDetached = true
	a.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	p := &fullPlayerPrefsStore{path: path, writeFile: writeFile, flush: flush, wake: a.invalidate}
	p.load()
	p.start()
	a.mu.Lock()
	pending, hasPending := a.fullPlayerPrefsPending, a.fullPlayerPrefsPendingSet
	a.fullPlayerPrefsPendingSet = false
	a.fullPlayerPrefsDetached = false
	a.fullPlayerPrefs = p
	a.mu.Unlock()
	if hasPending {
		p.put(pending)
	}
	return p
}

// FullPlayerPrefsError 返回最近一次全屏播放器偏好落盘失败。
func (a *App) FullPlayerPrefsError() error {
	a.fullPlayerPrefsMu.Lock()
	defer a.fullPlayerPrefsMu.Unlock()
	a.mu.Lock()
	p := a.fullPlayerPrefs
	a.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.fullPlayerPrefsError()
}

// CloseFullPlayerPrefs 停止后台写入，并等待最后一个已接受版本完成。
func (a *App) CloseFullPlayerPrefs() error {
	a.fullPlayerPrefsMu.Lock()
	defer a.fullPlayerPrefsMu.Unlock()
	a.mu.Lock()
	p := a.fullPlayerPrefs
	a.fullPlayerPrefs = nil
	a.fullPlayerPrefsDetached = true
	a.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.Close()
}

// FullPlayerPrefs 返回现在这一份偏好。没挂上磁盘时是默认（全屏播放器照样能用）。
func (a *App) FullPlayerPrefs() FullPlayerPrefs {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fullPlayerPrefsValueLocked()
}

func (a *App) fullPlayerPrefsValueLocked() FullPlayerPrefs {
	if a.fullPlayerPrefs == nil {
		if a.fullPlayerPrefsPendingSet {
			return a.fullPlayerPrefsPending
		}
		return DefaultFullPlayerPrefs()
	}
	return a.fullPlayerPrefs.get()
}

// SetFullPlayerPrefs 更新内存中的全屏播放器偏好，并通知后台合并写入。
func (a *App) SetFullPlayerPrefs(next FullPlayerPrefs) {
	a.fullPlayerPrefsMu.Lock()
	defer a.fullPlayerPrefsMu.Unlock()

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	if a.fullPlayerPrefs == nil {
		if a.fullPlayerPrefsDetached {
			a.fullPlayerPrefsPending = next
			a.fullPlayerPrefsPendingSet = true
			a.mu.Unlock()
			return
		}
		a.fullPlayerPrefs = &fullPlayerPrefsStore{cur: DefaultFullPlayerPrefs()}
	}
	p := a.fullPlayerPrefs
	wake := a.wake
	player := a.player
	a.mu.Unlock()

	next = p.put(next)
	if wake != nil {
		wake()
	}
	if player != nil {
		player.SetSpectrumEnabled(next.ShowSpectrum)
	}
}
