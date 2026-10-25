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

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// settingsSlot 是一个 App 上挂着的那份设置。
//
// store 有可能是 nil：拿不到设置文件路径时就没有 store 可挂，但**照样要记下
// 这个错**，否则界面无从知道「这次改的设置存不住」。下面每一个方法都把
// store == nil 当成「没有设置可用」，不碰它。
type settingsSlot struct {
	store *SettingsStore

	// loadErr 是挂载时读盘的错：文件坏了，或者来自更新的版本。
	//
	// 它不拦着程序起来——设置照样用（默认值，或从坏文件里尽力恢复出来的），
	// 这里只是记下来，让界面能告诉用户出了什么事（见 TakeSettingsError）。
	loadErr error

	// shown 是已经报给用户看过的那一个错。
	//
	// 界面每帧都要问一次，不记账的话屏幕上会一直是一样的话在闪。
	shown string
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
	if s.PlaybackRate > 0 {
		a.rate = audio.SnapRate(s.PlaybackRate)
	}
	a.mu.Unlock()

	// 落盘完成了要叫醒界面：不唤醒的话，界面会一直停在「还没存」的那一帧
	//（和播放进度走的是同一条路，见 App.SetWake 的注释）。
	store.SetWake(a.invalidate)
	return err
}

// RecordSettingsFailure 记下一个「设置存不住」的错：没有 store，界面用默认值
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
	if slot := a.settingsSlot(); slot != nil && slot.store != nil {
		return slot.store.Snapshot()
	}
	return DefaultSettings("")
}

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
	err := slot.loadErr
	if err == nil && slot.store != nil {
		err = slot.store.LastError()
	}
	if err == nil {
		slot.shown = ""
		return nil
	}
	msg := err.Error()
	if msg == slot.shown {
		return nil
	}
	slot.shown = msg
	return err
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
