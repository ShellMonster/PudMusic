// 设置挂到 App 上：启动时读盘、每帧给界面一份、改完落盘、退出前 flush。
//
// ## 为什么挂载关系记在包级的表里，而不是 App 的一个字段
//
// App 的结构体写在 app.go 里，那个文件本阶段不归本任务改（别的分支正在改
// 它）。Go 里**方法可以写在任意一个文件里，字段不行**——没有办法在另一个
// 文件上给一个已有的结构体加字段。所以挂载关系记在这张 `*App → 设置` 的表
// 里，对外的接口面仍然是 App 的方法（`MountSettings` / `SettingsSnapshot` /
// `UpdateSettings` …）。等 app.go 那边的改动并进来，把下面这一格搬回结构体
// 就行，调用方一行都不用改。
package app

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// settingsSlot 是一个 App 上挂着的那份设置。
//
// store 有可能是 nil：拿不到设置文件路径时就没有 store 可挂，但**照样要记下
// 这个错**，否则界面无从知道「这次改的设置存不住」。下面每一个方法都把
// store == nil 当成「没有设置可用」，不碰它。
type settingsSlot struct {
	mu    sync.Mutex
	store *SettingsStore

	// loadErr 是挂载时读盘的错：文件坏了，或者来自更新的版本。
	loadErr error

	// externalErr 是不属于 SettingsStore 的持久化错误（例如全屏播放器偏好）。
	externalErr error

	// shown 记录已经交给界面的错误文本，避免同一个错误在每帧重复出现。
	shown map[string]struct{}
}

// mountedSettings 是「哪个 App 挂的是哪份设置」。进程里通常只有一个。
var mountedSettings sync.Map // *App → *settingsSlot

// ErrSettingsNotMounted 是忘了调 MountSettings 时的错。
var ErrSettingsNotMounted = errors.New("app: 还没有挂上设置，先调 MountSettings")

// SettingsPath 是某个目录下的设置文件路径。
//
// 目录由调用方给：应用层不认识框架，不知道 MyGo 的应用数据目录在哪——那是
// `mygo.App.Path(mygo.PathUserData)` 的事，界面层替我们调（见 internal/ui）。
func SettingsPath(dir string) string { return filepath.Join(dir, SettingsFileName) }

// MountSettings 打开（或建）一份设置并挂到 a 上。
//
// ⚠️ 要在开窗口**之前**调，或者在一个 goroutine 里调完再要一帧：读盘是阻塞 IO。
//
// 返回的 error 说的不是「起不来」，而是「这份设置是怎么个坏法」：文件不是
// 合法 JSON（ErrSettingsCorrupt），或者来自更新的版本（ErrSettingsFromNewer）。
// 这两种情况下设置都照样挂上了，程序照常能起来——差别只是界面上会出现一条
// 提示，用户看得见自己丢了设置，而不是安静地按默认值用下去。
func (a *App) MountSettings(path string) error {
	store, err := OpenSettingsStore(path)
	mountedSettings.Store(a, &settingsSlot{store: store, loadErr: err})

	// 播放模式是**队列**的形状，队列不走挂载表（它跟着 App 走，见 app.go），
	// 所以得在这里把它从设置搬过去。不搬的话用户设过的档每次启动都被
	// NewQueue 的默认值盖掉——界面上写着「随机」，放完却按循环走。
	//
	// 认不出的档位 `SetPlayMode` 会落成默认档，所以这一句对一份手写坏了的
	// 设置文件也安全。
	s := store.Snapshot()
	a.queue.SetPlayMode(s.PlayMode)
	a.queue.SetKeepPagesOrderInRandom(s.ShouldKeepPagesOrderInRandomPlayMode)

	// 音量：用户设过的那一档，重启之后还在（票 #311）。
	//
	// ⚠️ **「这一格不存在」和「这一格是 0」必须分开**：前者是老版本写的文件
	// （那时候音量压根不落盘，每一次启动都是 100%），该给默认值；后者是用户自己
	// 把滑块拉到最下（= 静音），该给 0。所以这一格是指针（见 `Settings.Volume`）。
	//
	// ⚠️ **静音是它自己的一格**：静音的时候音量那一格一动不动，落盘的也是它——
	// 「静音后退出再启动，音量变成 0」那个坑就是这么踩的（Clementine 的老毛病）。
	vol := DefaultVolume
	if s.Volume != nil {
		vol = *s.Volume
	}
	a.mu.Lock()
	a.volume = vol
	a.muted = s.Muted
	a.normalize = s.NormalizeVolume
	a.trackFade = time.Duration(s.TrackFadeSeconds) * time.Second
	a.eqGains = audio.EQPresetByID(s.EQPreset).Gains
	a.vid.setQuality(bili.VideoQuality(s.VideoQuality))
	if s.PlaybackRate > 0 {
		a.rate = audio.SnapRate(s.PlaybackRate)
	}
	a.mu.Unlock()
	// 代理是网络层的事，不归 App 的锁管：存进网络层现读的那一格
	// （见 proxy.go 的 storeProxy / ProxySnapshot）。
	storeProxy(s)

	// 落盘完成了要叫醒界面：不唤醒的话，界面会一直停在「还没存」的那一帧
	//（和播放进度走的是同一条路，见 App.SetWake 的注释）。
	store.SetWake(a.invalidate)
	return err
}

// RecordExternalSettingsError makes a non-SettingsStore persistence failure visible through the settings error channel.
func (a *App) RecordExternalSettingsError(err error) {
	slot := a.settingsSlot()
	if slot == nil {
		slot = &settingsSlot{}
		actual, _ := mountedSettings.LoadOrStore(a, slot)
		slot = actual.(*settingsSlot)
	}
	slot.mu.Lock()
	slot.externalErr = err
	slot.mu.Unlock()
}

// 跑着，但**这件事用户必须看得见**（见 TakeSettingsError）。
//
// 用在拿不到设置文件路径那种场合。那不是「还没挂设置」（第一次运行），是
// 「挂了但落不了盘」——用户改了主题，下次打开又变回默认。对用户是两件事，
// 都不能安静。
func (a *App) RecordSettingsFailure(err error) {
	if err == nil {
		return
	}
	mountedSettings.Store(a, &settingsSlot{loadErr: err})
}

// CloseSettings 收尾：把还欠着的改动落完，停掉后台写。可以重复调。
//
// ⚠️ 「改设置 → 杀进程 → 重启 → 设置还在」这条验收靠的就是 store 里那次
// 同步 Flush。SIGKILL 拦不住，任何异步写盘都得配一个同步收尾。
func (a *App) CloseSettings() error {
	v, ok := mountedSettings.LoadAndDelete(a)
	if !ok {
		return nil
	}
	store := v.(*settingsSlot).store
	if store == nil {
		return nil
	}
	return store.Close()
}

// SettingsSnapshot 是这一帧该用的那份设置。
//
// 每帧都要调，所以**没挂过 store 也不能返回零值**：一份零值的设置会让主题
// 变成黑底黑字。给 Biu 的默认值，和「第一次运行」表现一致。
func (a *App) SettingsSnapshot() Settings {
	settingsReads.Add(1)
	if slot := a.settingsSlot(); slot != nil && slot.store != nil {
		return slot.store.Snapshot()
	}
	return DefaultSettings("")
}

// settingsReads 是「一共读了几次设置」的计数器。
//
// # 它数的是什么
//
// **每一次 `SettingsSnapshot()` 算一次**——不管有没有挂上 store（没挂上的那一
// 次返回的是默认值，而那也是一次读）。
//
// # 为什么要有它
//
// 「读设置」不是免费的：`Snapshot()` 要拿一把互斥锁，还要把一份 `Settings`
// **值拷贝**出去（它里面有好几个切片和指针），而 `Settings` 不小。
//
// 于是「某一帧读了几次设置」是**这一帧有多贵**的一个直接刻度：一次读 = 一次
// 锁 + 一次几百字节的拷贝 + 一次堆分配。界面层那套帧预算量的是**分配次数**
// （那是这些读的**间接**后果），而这个计数器量的是**直接**的那一头。
//
// ⚠️ **它不是给生产看的**：它一直在数，而 `Add(1)` 是原子操作——每帧几十次
// 的话，那点开销可以忽略（原子加在现代 CPU 上是几条指令），但它**确实是**
// 每次读设置都要付的一点点。真要严格，就把它挪到界面层那一层去数。
//
// ⚠️ **它是进程级的**（不是每个 App 一份）：跟 `runtime.MemStats.Mallocs` 一样，
// **别的线程**的读也算在里面。测「一帧读了几次」的时候，窗口要短。
var settingsReads atomic.Int64

// SettingsReadCount 报「一共读了几次设置」。
//
// ⚠️ **它是进程级的**：别的 App、别的线程的读也在这里面。要量「某一个 App
// 读了几次」，先看 `TakeSettingsReadCount` 那一条（它能取走并归零）。
func SettingsReadCount() int64 { return settingsReads.Load() }

// TakeSettingsReadCount 取走「到现在为止读了几次设置」，并把计数器归零。
//
// # 形状为什么是「取走并归零」而不是「读一个数」
//
// 下一张票（票 #632）要断言的是「**这一帧**读了几次设置」——而「这一帧」是
// 一个**区间**，不是「从进程启动到现在」。所以这个口子必须能：
//
//  1. **归零**（量之前把之前的读清掉）——`TakeSettingsReadCount` 归零；
//  2. **取走**（量完之后读增量）——同一句把增量拿回来并再归零。
//
// 于是量一帧的写法是：
//
//	before := app.TakeSettingsReadCount() // 归零，拿回一个可以忽略的数
//	tt.Frame()                             // 画一帧
//	reads := app.TakeSettingsReadCount()   // 这一帧读了几次
//
// ⚠️ **它会清掉别人的读**：进程里另一个 App 也在读设置的话，那一笔会算进
// 这一次。测试里通常只有一个 App，所以不影响。
//
// ⚠️ **归零不是线程安全的快照**：并发读的话可能丢一两笔。而它要量的是
// 「界面线程那一帧读了几次」，而界面线程是**唯一**会在一帧里读设置的那条
// （后台线程读设置是为了别的目的，见 `SettingsSnapshot` 的调用点），所以够用。
func TakeSettingsReadCount() int64 { return settingsReads.Swap(0) }

// UpdateSettings 改设置。改完立刻返回，落盘在后台合并写（见 settings.go）。
//
// 返回的 Settings 是改完之后的完整设置，界面可以直接拿去画下一帧。
func (a *App) UpdateSettings(fn func(*Settings)) (Settings, error) {
	slot := a.settingsSlot()
	if slot == nil || slot.store == nil {
		return DefaultSettings(""), ErrSettingsNotMounted
	}
	s, err := slot.store.Update(fn)
	if err != nil {
		return s, err
	}
	// 代理跟着每一笔设置改动走（见 proxy.go）：改完立刻生效，下一条请求
	// 就走新路，不用重启。
	storeProxy(s)
	a.mu.Lock()
	changed := a.normalize != s.NormalizeVolume
	a.normalize = s.NormalizeVolume
	a.trackFade = time.Duration(s.TrackFadeSeconds) * time.Second
	a.vid.setQuality(bili.VideoQuality(s.VideoQuality))
	eq := audio.EQPresetByID(s.EQPreset).Gains
	eqChanged := a.eqGains != eq
	a.eqGains = eq
	p, path, local := a.player, a.path, a.online == nil
	a.mu.Unlock()
	if eqChanged && p != nil {
		p.SetEQ(eq)
	}
	if changed && p != nil {
		p.SetNormalize(s.NormalizeVolume)
		if s.NormalizeVolume && local {
			a.startLoudnessScan(p, path)
		}
	}
	// 音质跟着这一笔设置走（理由同上面的代理）：设置页换档也是「换档」，
	// 正在放的那首要按新档重新取流——不然那一格只有重启才生效，是句空话。
	//
	// 🔴 **档位没变就不调**：`SetAudioQuality` 自己会把偏好写回设置
	// （`rememberAudioQuality`），不拦的话换档 → 写设置 → 再换档绕圈。
	// 没挂在线播放时 `AudioQuality()` 给的是零值（高品质），真调进去也会被
	// 它自己的「没挂取流」早退挡住。
	if q := PrefToBili(s.AudioQuality); a.AudioQuality() != q {
		a.SetAudioQuality(q)
	}
	a.invalidate()
	return s, nil
}

// SetThemeMode 换主题档位：跟随系统 / 浅色 / 深色。
//
// ⚠️ **设置页那三个按钮不走这里**，它们走 `UpdateSettings`（见
// `internal/ui/settings_page.go` 的 `applySetting`）。界面层要的是「所有设置
// 共用一条路、一条报错提示」，为此每个控件调一次通用路比调各自的快捷方法更
// 合适。这里留着是它本身就是这一项设置的**类型化写法**，语义清楚，也不怕以后
// 「换主题」这件事要做校验——那时候校验该落在这里。
//
// 档位只有这三个值，手改设置文件塞进来的别的东西 normalize 会兜回「跟随系统」。
func (a *App) SetThemeMode(m ThemeMode) error {
	_, err := a.UpdateSettings(func(s *Settings) { s.ThemeMode = m })
	return err
}

// SettingsReadOnly 报这份设置是不是只读的（文件来自更新的版本）。
//
// 只读时任何改动都会被拒掉，界面该把设置页的控件关掉。
func (a *App) SettingsReadOnly() bool {
	slot := a.settingsSlot()
	return slot != nil && slot.store != nil && slot.store.ReadOnly()
}

// FlushSettings 立刻落盘并等它完成。
//
// 退出前调一次：后台落盘是合并写，最后那一下有可能还等在退避窗口里，用户
// 改完直接退出就丢了（见 CloseSettings 的注释）。
func (a *App) FlushSettings() error {
	slot := a.settingsSlot()
	if slot == nil || slot.store == nil {
		return nil
	}
	return slot.store.Flush()
}

// TakeSettingsError 取出「界面还没报过的那一个错」，报过之后同一个错不再返回。
//
// 读盘失败、落盘失败都走这里。**同一个错只报一次**：界面每帧都问一句，不
// 记账的话屏幕上会一直是一样的话在闪。错自己好了之后记账清空，下次再坏才
// 重新报一次。
//
// ⚠️ 只能在界面主线程调：shown 是给界面那一侧记的账，没有上锁。
func (a *App) TakeSettingsError() error {
	slot := a.settingsSlot()
	if slot == nil {
		return nil
	}

	candidates := make([]error, 0, 3)
	if slot.loadErr != nil {
		candidates = append(candidates, slot.loadErr)
	}
	slot.mu.Lock()
	if slot.externalErr != nil {
		candidates = append(candidates, slot.externalErr)
	}
	slot.mu.Unlock()
	if slot.store != nil {
		if err := slot.store.LastError(); err != nil {
			candidates = append(candidates, err)
		}
	}
	for _, err := range candidates {
		msg := err.Error()
		if _, ok := slot.shown[msg]; ok {
			continue
		}
		if slot.shown == nil {
			slot.shown = make(map[string]struct{})
		}
		slot.shown[msg] = struct{}{}
		return err
	}
	if len(candidates) == 0 {
		slot.shown = nil
	}
	return nil
}

// settingsSlot 是这个 App 挂着的设置，没有就返回 nil。
func (a *App) settingsSlot() *settingsSlot {
	if v, ok := mountedSettings.Load(a); ok {
		return v.(*settingsSlot)
	}
	return nil
}

// invalidate 叫界面要一帧。
func (a *App) invalidate() {
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}
