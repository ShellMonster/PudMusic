// 自动更新：查有没有新版本、下它、装它、退出并进新版本。
//
// # 用的是框架内置的那个更新器（`mygo.Updater`），不是自己实现的一套
//
// 框架 v0.5.x 把更新器拆成了两半（`mygo/updater.go` 与
// `mygo/plugins/updater`）：
//
//   - `mygo.Updater` 是**核**：问更新源要清单、比版本、下载、**验 ed25519
//     签名**、打增量补丁、失败回全量、原地换掉整个应用。这一半什么都不画。
//   - `plugins/updater`（含它的 `native` 子包）是**窗**：一个 Sparkle 那样的
//     更新窗口，14 种语言，自己落一份 `updater.json`。
//
// 这一份用的是核，界面是自己画的。理由有两条，都不是「不想用现成的」：
//
//  1. **那个窗口没有验收要的那几样**。`native/window.go` 的
//     `downloadingView` 只画一句「12.3 MB / 20.0 MB」，没有速度、没有百分比，
//     按钮也只有 Install / Skip / Later。而 Biu 上游那个弹窗要
//     `bytesPerSecond`、要 `percent.toFixed(2)`、要「退出并安装更新」。
//  2. **用了它就有两个更新窗口**。它自己在后台查到新版本时会弹窗，我们要的
//     弹窗也会弹——用户同一分钟里看两次同一件事，其中一次还少一半信息。
//
// 想换回 `native.Plugin` 的时机是：框架那个窗口补上了速度和百分比。切换成本
// 就是把下面这一整层删掉，改成一行 `mygo.Use(native.Plugin)` 加一个设置页
// 入口——所以这一层刻意**不含任何更新逻辑**，只有状态机和「把框架那三个函数
// 的结果翻译成字」。
//
// # 增量：框架已经做了，我们要做的是**别把它藏起来**
//
// `Install` 先试当前版本到新版本之间的 `.delta`，任何一步失败（补丁对不上、
// 文件被改过、磁盘满、用户中途退出）就自动改下整个归档，两处都验签名
// （`mygo/updater.go` 的 `installUpdate`）。这套能力是白送的，**但它有一处
// 用户能看见的表现，我们不处理就是撒谎**：回退发生时，进度回调的 `total` 会
// 从「增量包多大」变成「整个安装包多大」，`downloaded` 从头开始。
//
// 于是用户会看到进度条**倒退**、百分比**变小**、速度**归零再跳起来**。Biu
// 那一套（electron-updater）没有这个现象，所以上游没有这一支代码。我们这里
// 认得出这个变化（见 `updateState.observe`），并且在界面上把话说明白——
// **这条是实测出来的行为，不当 bug 藏起来**。
//
// ⚠️ dmg 装来的应用**第一次**增量最容易走到回退：磁盘镜像里的那份没有公证
// 票据（`Contents/CodeResources`），而发布出去的归档里有，于是补丁对不上磁盘
// 上的现状。框架 issue #300 记的是这件事（v0.5.0/#302 改过一轮），但它的触发
// 条件与公证绑在一起，而本项目**不做公证**——所以不能靠「我们不公证所以碰不到」
// 来免掉这条分支：它必须被看见、被处理、被测试。

package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// UpdatePhase 是更新器走到哪一步了。界面上弹窗出不出现、按钮是什么，全看它。
type UpdatePhase int

const (
	// UpdateIdle：没查过，或者查完发现已经是最新。
	UpdateIdle UpdatePhase = iota
	// UpdateChecking：正在问更新源。
	UpdateChecking
	// UpdateAvailable：查到了新版本，等用户看更新日志、决定下不下。
	UpdateAvailable
	// UpdateDownloading：正在下（含增量包和回退后的全量包）。
	UpdateDownloading
	// UpdateReady：已经装到磁盘上了，等用户退出并安装。
	//
	// 这一档和 Biu 的 `downloaded` 是一回事，而它**已经是装好的**：框架换掉磁盘
	// 上那份发生在 `Install` 里面，`Relaunch` 只是再起一个进程。所以这一档
	// 关掉弹窗不会留下「下载了一半」的烂摊子——下次开应用就是新版了。
	UpdateReady
	// UpdateFailed：查或者装失败了。`UpdateState.Err` 里有原因。
	UpdateFailed
)

// UpdateRelease 是一次检查找到的新版本。
//
// 对应框架的 `mygo.Update`，但只留界面用得上的三样——框架那个类型里的清单和
// 密钥是**私有的**，界面拿不到，更拿不到就不该假装有。
type UpdateRelease struct {
	Version string
	// Notes 是这个版本的更新日志，Markdown。Biu 用同一个组件渲染它。
	Notes string
	// Date 是这个版本构建的时刻。零值表示更新源没给。
	Date time.Time
}

// UpdateBackend 是更新器要框架做的那几件事。
//
// **它是一个接口，不是直接调 `mygo.Updater`**：框架那三个函数要一个跑起来的
// 事件循环和一份自己能写的安装目录，而无头测试两样都没有。更要紧的是
// `Enabled()` 在开发构建里**永远是 false**——`packageUpdateFeed` /
// `packageUpdateKey` 是 `mygo build` 用 `-ldflags -X` 塞进去的（`mygo/updater.go`
// 顶部的注释），不打包就没有，于是界面会永远停在「这个版本不能自动更新」，
// 一个字也测不了。真实现装在 `internal/ui/update_backend.go`。
type UpdateBackend interface {
	// Enabled 报应用能不能给自己更新。没打包、安装位置只读时是 false。
	Enabled() bool
	// Version 是正在跑的版本号。开发构建里可能是空串。
	Version() string
	// Check 要一个新版本，已经是最新时返回 (nil, nil)。
	Check(ctx context.Context) (*UpdateRelease, error)
	// Install 下 want.Version 那一版，验签名，把它装到磁盘上。progress 报已传的
	// 字节和总量。
	//
	// ⚠️ 它**自己去问一次更新源**，而不是收一个「已经查好的」来装：框架那个
	// 能装的 `Update` 类型带着清单和密钥，只活过一次 `Check` 调用，跨不过调用
	// 边界（那一层见 `internal/ui/update_backend.go`）。所以签名上多一个 `want`
	// 是为了让它知道**要装的是哪一版**——万一这中间发了一版新的，它得回
	// ErrUpdateSuperseded 而不是把用户按过的那一版装上。
	Install(ctx context.Context, want UpdateRelease, progress func(downloaded, total int64)) error
	// Relaunch 退出并起一个进程进去。
	Relaunch()
}

// ErrUpdateSuperseded 是「要装的那一版已经不是最新的了」。
//
// 它不是失败，是「这一趟白搭了」：中间有人发了新的一版，而装一个旧版本等于
// 让用户白下几十兆、装完还得再更新一次。碰到它的处理是退回「有更新可看」并重查
// 一次，用户看到的是重新开始下，而不是一句红字。
var ErrUpdateSuperseded = errors.New("app: 要装的那一版已经不是最新的了")

// UpdateState 是一帧里界面要读的更新状态。
//
// 和别处的快照一样，界面拿到的**必须是一份自洽的拷贝**：进度是后台 goroutine
// 一边下一边写的，而弹窗要在主线程上画它，「已传 30MB、总量 20MB」这种半截状态
// 会让进度条画出一条比框还宽的东西。
type UpdateState struct {
	Phase UpdatePhase
	// Enabled 报这个应用能不能自己更新。false 时界面上要把**为什么**说清楚，
	// 一个按下去只会说「不能」的按钮不如不摆——但完全不摆用户就只能去问人。
	Enabled bool
	// Current 是正在跑的版本号，Latest 是查到的那个。都可能是空串。
	Current string
	Latest  string
	// Notes 是 Latest 的更新日志，Markdown。
	Notes string
	// Released 是 Latest 的构建时刻，零值表示更新源没给。
	Released time.Time
	// Transferred / Total 是这一次下载的字节。Total 为 0 表示还不知道有多大。
	Transferred int64
	Total       int64
	// Speed 是当前的下载速度，字节/秒。样本不够时是 0。
	Speed int64
	// Percent 是 0~100 的百分比。**两位小数那一层留给界面去写**
	//（`strconv.FormatFloat(p, 'f', 2, 64)`），这里多给几位是为了界面写出来的
	// 那个数不是拿四舍五入到百分之一之后又四舍五入一次。
	Percent float64
	// FellBack 说的是**这一次下的是整个安装包，不是增量包**：增量补丁没能把新
	// 版本拼出来（见文件头 §增量）。它一为真，界面就该说一句话，而不是让进度
	// 条在用户眼前倒退却一声不响。
	FellBack bool
	// Err 是最近一次失败的原因（查或者装）。
	Err string
	// LastCheck 是上一次**成功**查完的时刻。
	LastCheck time.Time
}

// Available 报有没有新版本可看。
//
// 它认的是「`Latest` 不为空且和正在跑的不是一个版本」，**不是**「比新」：版本号
// 的比较规则在框架的 internal 包里（`update.Compare`），这一层拿不到，而拿不到
// 就不该自己另写一套（两套比较规则漂起来的那种 bug 最难查）。倒退版本这种情形
// 会被启动后的第一次自动检查纠正掉。
func (s UpdateState) Available() bool { return s.Latest != "" && s.Latest != s.Current }

// updateCheckInterval 是后台自动检查的间隔。Biu 那边是一个小时
// （`electron/updater/index.ts` 的 `checkForUpdatesInterval`），照它。
//
// 也不能更密：更新源多半是 GitHub 的 releases，匿名请求每个地址一小时 60 次
// （框架 `docs/updates.md` 记着这条限流），查密了等于自己把门锁上。
const updateCheckInterval = time.Hour

// updater 是更新这一摊。**零值不可用**，用 `App.MountUpdates` 建。
type updater struct {
	// app 是叫醒界面用的那一只手，见 wake。
	app *App

	mu      sync.Mutex
	backend UpdateBackend
	prefs   *updatePrefs
	now     func() time.Time
	// enabled / current 在挂上的时候问一次就够：界面每帧要读它们，而
	// `Enabled()` 在框架里是一次真实的建目录探测（`os.MkdirTemp` + `os.Remove`），
	// 每帧 60 次那种探测是白烧磁盘，而且**画面会跟着它闪**。
	enabled bool
	current string

	state updateState
	// notice 是给界面的一次性提示（后台查到了新版本之类），取走就没了。
	//
	// ⚠️ **tone 跟着 notice 一起存**（票 #180）：界面把这句话弹成一条提示，
	// 而提示按语义分色。话和「成没成」都是这一层产的，所以语气也在这里定。
	notice string
	tone   notice.Tone
	// cancel 是这次下载的取消函数。close 调它，用户在下载中途退出应用靠的就是它。
	cancel context.CancelFunc
	// notifiedVersion 是「已经为哪个版本发过系统通知了」。
	//
	// 🔴 **它是票面第 6 条「不重复刷通知」的另一半**（票 #559）：后台检查**每小时**
	// 一次，而**同一个新版本会被查到很多次**（用户不装它就一直查得到）——所以
	// 不记的话，用户每小时都会收到一条一模一样的「有更新了」。
	//
	// ⚠️ **稳定 ID 只决定「通知中心里那一条被替换」，不决定「不再发」**：框架那
	// 一次 `Show` 是真的会再弹一次（macOS 上同 ID 的一条再 Show 是重新弹）。
	// 所以去重得自己记一份「这个版本已经说过了」。
	//
	// ⚠️ **它跟着 `u.mu`**：它是更新器的状态，而 `check` 在后台 goroutine 上跑。
	//
	// ⚠️ **它不是「跨启动」的记忆**：应用重启之后后台检查又会查一遍那个版本，
	// 而那一次**该发**（用户重启应用说明他可能没看见上一次）。要跨启动记住得
	// 落盘，而落盘那份 `app-update.json` 是更新器的「上次查到什么」——它已经
	// 记着 `KnownVersion` 了，而那一份的语义是「界面上该显示哪个版本」，不是
	// 「通知发过没有」。两件事混在一份文件里会互相干扰（用户手动清掉了更新
	// 之后又该弹一次），所以这里只在本次运行里记。
	notifiedVersion string
	// checkCancel 是**正在查**那一次的取消函数。
	//
	// 和 `cancel` 分开是因为它们是两次独立的动作，而 `close` 要把两次都收掉：
	// 只收下载不收检查的话，一个查了很久的网络请求会在应用没了之后还在跑，而
	// 相位永远停在「正在检查」——界面上那个按钮就永远转下去。
	checkCancel context.CancelFunc
	// installWG / checkWG 等着后台那两趟收尾。close 会等它们。
	installWG sync.WaitGroup
	checkWG   sync.WaitGroup
	// closed 记着应用已经开始退了：那时候不再接新的检查。
	closed bool
	// stopChecks 停掉后台那个定时器。
	stopChecks func()
}

// updateState 是 updater 内部那一摊，锁在 mu 后面。
type updateState struct {
	phase       UpdatePhase
	latest      string
	notes       string
	released    time.Time
	transferred int64
	total       int64
	fellBack    bool
	err         string

	// speed 算下载速度，lastDownloaded 认出「这一趟从头开始了」，sampled 记着
	// 这一趟已经收到过几个采样点（第一个采样点是**建立基线**，不是回退）。
	speed          speedMeter
	lastDownloaded int64
	sampled        bool
}

// MountUpdates 把更新挂到 a 上。**要在开窗口之前调**。
//
// prefsPath 是这份文件的位置，一般是 `<UserData>/app-update.json`。空串就是
// 只在内存里：拿不到应用数据目录时更新照常能用，只是关掉之后不记得上次查过
// 什么时候（和搜索历史那一份同一个取舍）。
func (a *App) MountUpdates(b UpdateBackend, prefsPath string) {
	u := &updater{
		app:     a,
		backend: b,
		prefs:   newUpdatePrefs(prefsPath),
		now:     time.Now,
	}
	// ⚠️ 这两问必须在**开窗口之前**：Enabled() 要在磁盘上建一个临时目录试一
	// 下能不能写（框架 `mygo/updater.go`），是阻塞 IO。
	u.enabled, u.current = b.Enabled(), b.Version()
	u.prefs.load()
	// 上次查到的那个还没装上（多半是当时下到一半退出了，或者装好了没重启）：
	// 摆出来总比让用户再等一次网络强，启动后的第一次自动检查会对一遍。
	if last := u.prefs.last(); last.KnownVersion != "" && last.KnownVersion != u.current {
		u.state.latest, u.state.phase = last.KnownVersion, UpdateAvailable
	}

	a.mu.Lock()
	a.updates = u
	wake := a.wake
	a.mu.Unlock()
	// 挂的时候界面可能已经在了（挂登录就是这么补的），补一次，否则那次改动没有
	// 一帧会被叫醒。
	if wake != nil {
		wake()
	}
}

// UpdateMounted 报更新挂上没有。界面上那一格靠它决定摆不摆入口。
func (a *App) UpdateMounted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updates != nil
}

// UpdateSnapshot 返回更新状态的一份拷贝。弹窗每帧读它。
func (a *App) UpdateSnapshot() UpdateState {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return UpdateState{}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	s := UpdateState{
		Phase:       u.state.phase,
		Enabled:     u.enabled,
		Current:     u.current,
		Latest:      u.state.latest,
		Notes:       u.state.notes,
		Released:    u.state.released,
		Transferred: u.state.transferred,
		Total:       u.state.total,
		Speed:       u.state.speed.speed(u.now()),
		Percent:     percentOf(u.state.transferred, u.state.total),
		FellBack:    u.state.fellBack,
		Err:         u.state.err,
		LastCheck:   u.prefs.last().LastCheck,
	}
	return s
}

// TakeUpdateNotice 取走留给界面的那一句提示，取过就没了。
//
// 去重在这里做，不在调用方：界面每帧都会问一句，问到就弹一条 Toast，而「后台
// 查到了新版本」这种话弹两遍比不弹更糟。
func (a *App) TakeUpdateNotice() (string, notice.Tone) {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return "", notice.Plain
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	n, tone := u.notice, u.tone
	u.notice = ""
	return n, tone
}

// CheckUpdates 查一次更新（用户按了「检查更新」）。**立即返回**，结果走
// `UpdateSnapshot`。
//
// ⚠️ 检查本身在后台 goroutine 上跑，不在调它的那条线程上：它是一次 HTTPS
// 请求，而点按钮的那条线程是**主线程**。在这里同步等网络 = 窗口整个冻住，
// 而网不通的时候能冻很久。
func (a *App) CheckUpdates() {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return
	}
	go u.check(true)
}

// StartAutoChecks 开始后台自动检查：先查一次，之后每 `updateCheckInterval`
// 一次。返回值是停掉它的函数，`Close` 会调。
func (a *App) StartAutoChecks() func() {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return func() {}
	}
	stop := make(chan struct{})
	var once sync.Once
	halt := func() { once.Do(func() { close(stop) }) }
	go func() {
		u.check(false)
		t := time.NewTicker(updateCheckInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				u.check(false)
			}
		}
	}()
	u.mu.Lock()
	u.stopChecks = halt
	u.mu.Unlock()
	return halt
}

// StartUpdateDownload 开始下那个新版本。**立即返回**，进度走 `UpdateSnapshot`。
//
// 没有新版本、正在查、正在下的时候按它都没用：那一个按钮那时候是关着的，而
// 「按了没反应」是最坏的一种。
func (a *App) StartUpdateDownload() {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return
	}
	u.mu.Lock()
	// `UpdateFailed` 也放行：那是「装到一半失败了」，界面上给的就是一个
	// 「重试下载」按钮——那个按钮按下去必须真的重来一次，否则它就是个装饰。
	// （查失败不在此列：那时候还不知道有没有更新可下，按钮是另一个。）
	ok := !u.closed && (u.state.phase == UpdateAvailable ||
		(u.state.phase == UpdateFailed && u.state.latest != ""))
	if !ok {
		u.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.state.reset()
	u.state.phase = UpdateDownloading
	u.state.speed.reset()
	u.mu.Unlock()

	u.installWG.Add(1)
	go func() {
		defer u.installWG.Done()
		u.install(ctx, cancel)
	}()
}

// QuitAndInstall 退出并进新版本。
//
// 磁盘上那一份在 `Install` 里就换好了，`Relaunch` 只是再起一个进程——所以
// 这一步**必须**真退出，不能退而求其次只把弹窗关掉：Biu 那个按钮叫「退出并
// 安装更新」，用户按下去要的是新版本，不是一句「下次开就是新版了」。
func (a *App) QuitAndInstall() {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return
	}
	u.mu.Lock()
	ok := u.state.phase == UpdateReady
	u.mu.Unlock()
	if !ok {
		return
	}
	u.backend.Relaunch()
}

// DismissUpdate 把弹窗关掉。这一步只动状态机——已经装到磁盘上的那份原样留着，
// 下次开应用就是新版。
func (a *App) DismissUpdate() {
	a.mu.Lock()
	u := a.updates
	a.mu.Unlock()
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	// 下到一半不让关：进度条正在走，而这一关用户以为「取消了」，其实它还在
	// 后台把几十 MB 往磁盘上写。不给关是刻意的（和 Biu 那个弹窗的
	// `isKeyboardDismissDisabled` 一样），界面上的按钮也是关的。
	switch u.state.phase {
	case UpdateDownloading, UpdateChecking:
	case UpdateAvailable, UpdateFailed:
		u.state.phase = UpdateIdle
		u.state.err = ""
	}
}

// close 收尾：停掉后台检查、取消在下的那一次下载、等它真的停了。
//
// ⚠️ 「下载中退出应用」（SPEC 118）靠的就是这里。框架的 `Install` 在下载完之前
// **一个字节都不动磁盘上那份应用**（换目录在 `installUpdate` 的最后一步
// `swap`），而取消会让它在 `ctx.Err() != nil` 那句上直接返回、连全量包都不再
// 下。所以中途退出 = 这一趟白下，应用本身完好，下次开还是现在这个版本，
// 盘上也没有半截文件要收拾。
func (u *updater) close() {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return
	}
	u.closed = true
	cancel, stop, checkCancel := u.cancel, u.stopChecks, u.checkCancel
	u.cancel, u.stopChecks, u.checkCancel = nil, nil, nil
	u.mu.Unlock()

	if stop != nil {
		stop()
	}
	if cancel != nil {
		cancel()
	}
	if checkCancel != nil {
		checkCancel()
	}
	// ⚠️ 两个都要**等**。不等的话退出会和后台那一趟赛跑：界面拆了，而那个
	// goroutine 还在往 `state` 里写，还顺手叫一次已经不存在的窗口。
	u.installWG.Wait()
	u.checkWG.Wait()
}

// checkDone 收掉一次已经落地的检查。
func (u *updater) checkDone(cancel context.CancelFunc) {
	cancel()
	u.mu.Lock()
	u.checkCancel = nil
	u.mu.Unlock()
	u.checkWG.Done()
}

// check 问一次更新源。
//
// ⚠️ **拿着 u.mu 的时候不许调 u.wake()**：wake 会去拿 `a.mu`，而测试里装的那个
// 唤醒回调往往当场就读一次快照（那就是 `u.mu`）——锁一交叉就是当场死锁。
// 所以下面每一支都先算好、出了锁再叫。
func (u *updater) check(user bool) {
	u.mu.Lock()
	if u.closed || u.state.phase == UpdateChecking {
		u.mu.Unlock()
		return
	}
	u.state.phase = UpdateChecking
	u.state.err = ""
	// 这一趟的取消句柄在**锁里面**存下：只有握着锁判过 `closed` 的那一个能
	// `Add`，所以 `close` 里 `closed = true` 之后不会再有新的 `Add`，它在锁外面
	// 等 `checkWG` 不会踩到「Add 和 Wait 撞上」那个坑。
	ctx, cancel := context.WithCancel(context.Background())
	u.checkCancel = cancel
	u.checkWG.Add(1)
	u.mu.Unlock()
	defer u.checkDone(cancel)
	u.wake()

	rel, err := u.backend.Check(ctx)
	// ⚠️ **先问 `ctx.Err()` 再 `cancel()`**。顺序反了的话 `ctx.Err()` 永远是
	// `context.Canceled`，于是下面那句 `if aborted { return }` 会把**每一次**
	// 检查都当成「被取消了」直接返回，而相位已经写成 `UpdateChecking` 了——
	// 症状是界面上那个按钮永远停在「正在检查…」，而实际上每次检查都跑完了、
	// 只是结果被扔掉。（这个顺序反过一次，是测试 `TestACheckFindsTheNewVersion
	// AndKeepsIt` 抓出来的。）
	aborted := ctx.Err() != nil
	cancel()
	if aborted {
		// 被取消了（应用在查的过程中退了，或者这一趟是 Close 取消的）。
		//
		// ⚠️ 相位必须从 `Checking` 退回去，不能就这么留着：那三个字说的是
		// 「等一会儿就好」，而此刻**已经没有什么会来了**——界面上的按钮会永远
		// 转下去，用户能做的只有关掉应用。
		u.mu.Lock()
		if u.state.phase == UpdateChecking {
			u.state.phase = UpdateIdle
		}
		u.mu.Unlock()
		u.wake()
		return
	}
	now := u.now()

	var msg string
	var tone notice.Tone
	var phase UpdatePhase
	// found 是「这一趟查到了新版本」。
	//
	// ⚠️ **它不能靠 `phase` 判**：`phase` 那一档只在「查失败」和「查到了」两支
	// 里被赋值，而「查到了」那一支**并不写它**（相位是直接写进 `u.state.phase`
	// 的）。所以这里自己记一份。
	var found bool
	// prefs 是**出了 u.mu 之后**才写盘的那一份（见下面 ⚠️）。
	var prefs *updatePrefsFile
	u.mu.Lock()
	switch {
	case u.closed:
		u.mu.Unlock()
		return
	case err != nil:
		phase = UpdateFailed
		u.state.phase, u.state.err = phase, err.Error()
		// 后台那次失败**不弹提示**：用户没按任何键，突然冒一句红字只会让人以为
		// 应用坏了。下一次自己会好，真想知道可以去设置页按一下。
		if user {
			msg, tone = "检查更新失败："+err.Error(), notice.Danger
		}
	case rel == nil:
		// 已是最新。这一句一定要给用户看：Biu 那边是「当前版本为最新版本」的
		// 绿色提示（`components/update-check-button`），而「按了没反应」连
		// 「它到底查没查」都回答不了。
		u.state.phase = UpdateIdle
		u.state.latest, u.state.notes, u.state.released = "", "", time.Time{}
		prefs = &updatePrefsFile{LastCheck: now, KnownVersion: u.current}
		if user {
			msg, tone = "当前版本为最新版本", notice.Success
		}
	default:
		u.state.phase = UpdateAvailable
		u.state.latest, u.state.notes, u.state.released = rel.Version, rel.Notes, rel.Date
		u.state.fellBack = false
		found = true
		// 记下「上次查到的版本」：离线启动的那一次也能在设置页看到有新版本，
		// 不用干等网络。
		prefs = &updatePrefsFile{LastCheck: now, KnownVersion: rel.Version}
		if !user {
			// 后台查到的：留一句话，让界面弹一条 Toast，然后用户自己决定要不要看。
			// 硬弹窗是最招人烦的一种做法（见函数头）。
			msg, tone = "发现新版本 "+rel.Version, notice.Success
		}
	}
	if msg != "" {
		u.notice, u.tone = msg, tone
	}
	u.mu.Unlock()

	// ⚠️ **落盘写在锁外面**，这正是 `update_prefs.go` 里 `put` 的注释要求的：
	// `writeFileAtomic` 是 CreateTemp + Write + Sync + Close + Chmod + Rename
	// 六步真 IO，而 `UpdateSnapshot` 每帧要拿 `u.mu`。拿着锁写就是「一次几毫秒的
	// 写卡住界面一帧」——检查最多一小时一次、用户主动那一次也在后台 goroutine
	// 上（不占主线程），所以它不致命，但它正是那段注释说要避免的那件事，
	// 自己写的规矩自己破掉就没有规矩了。
	if prefs != nil {
		u.prefs.put(*prefs)
	}
	// 🔴 **系统通知**（票 #559）：查到了新版本，而用户多半不在应用里——
	// 而「有更新了」这一句界面上的提示，他回到应用才看得见。
	//
	// ⚠️ **只有后台查到的才发**：用户自己按「检查更新」的话，他正盯着设置页
	// 看着那句话，而系统通知是给「用户不在」时用的（那是下载完成那一档的
	// 规矩，这里是同一档）。
	//
	// ⚠️ **它在后台那条线上被调**（检查更新的 goroutine），而界面层那一头要读
	// 窗口的状态——那一头是带锁的（见 `internal/ui/notification.go`），所以
	// 跨 goroutine 读它不违反铁律一（铁律一管的是**界面状态**，不是这个）。
	//
	// 🔴 **而「用户要不要这条通知」是一格设置**（票 #559，票面第 3 条
	// 「更新提醒可复用同一基础设施但另开开关」）：
	//
	// ⚠️ **那一格在设置文件里，而这一层拿得到它**（`SettingsSnapshot` 是线程
	// 安全的，见 `settings_mount.go`）。而**不在这里判的话就没有别的地方判**
	// ——界面层那一头只管「用户在不在这几扇窗里」，它不认识设置。
	//
	// ⚠️ **「用户自己按的」那一档仍然不发**：他正盯着设置页看着那句话，而
	// 系统通知是给「用户不在」时用的（那是下载完成那一档的规矩，这里是同一档）。
	if !user && found && u.updateNotificationEnabled() && u.claimUpdateNotification(rel.Version) {
		u.notifyUpdateAvailable(rel.Version)
	}
	u.wake()
}

// claimUpdateNotification 记下「这个版本已经发过通知了」，并报「该不该发」。
//
// 🔴 **它是票面第 6 条「不重复刷通知」的另一半**（票 #559）：后台检查**每小时**
// 一次，而**同一个新版本会被查到很多次**（用户一直不装它就一直查得到）——不记
// 的话，用户每小时都会收到一条一模一样的「有更新了」。
//
// ⚠️ **而稳定 ID 不代替它**：框架那一次 `Show` 是真的会再弹一次（同一个 ID 再
// `Show` 是**重新弹**，不是「什么都不做」），而 ID 只决定通知中心里那一条被
// **替换**。所以「不再发」得自己记一份。
//
// ⚠️ **它只认版本**：换了版本就该再发一次（用户要看的正是「哪一个版本」）。
//
// ⚠️ **它跟着 `u.mu`**：检查在后台 goroutine 上跑，而这一份是更新器的状态。
func (u *updater) claimUpdateNotification(version string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if version == "" || u.notifiedVersion == version {
		return false
	}
	u.notifiedVersion = version
	return true
}

// install 下载并装好那一个版本。
func (u *updater) install(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	u.mu.Lock()
	want := UpdateRelease{Version: u.state.latest, Notes: u.state.notes, Date: u.state.released}
	u.mu.Unlock()

	err := u.backend.Install(ctx, want, u.observe)
	if ctx.Err() != nil {
		// 用户在下载中途退出了应用（或者已经进了新版本）。这一趟作废，**不能**
		// 当成失败弹一句红字，而要把状态放回「有更新可看」——磁盘上那份应用一个
		// 字节都没动过，说它「下载失败」是在撒谎。
		u.backToAvailable()
		return
	}
	if errors.Is(err, ErrUpdateSuperseded) {
		// 装之前发现中间发了新的一版。重查一次——不重查的话界面上摆的还是那个
		// 旧版本，用户再按一次还是撞同一句，那是个死循环。重查自带一句
		// 「发现新版本 X」的提示，用户看得见自己为什么被拦了一下。
		u.backToAvailable()
		u.check(false)
		return
	}
	if err != nil {
		u.fail(ctx, err)
		return
	}
	u.mu.Lock()
	u.state.phase = UpdateReady
	u.state.speed.reset()
	u.mu.Unlock()
	u.wake()
}

// backToAvailable 把状态放回「有更新可看，等用户再按一次」。
//
// ⚠️ `closed` 拦不住这一步：`closed` 管的是**不再接新的检查**，而相位是个对
// 「此刻在干什么」的陈述。下载已经被取消、此刻什么也没在下载了，相位还停在
// `Downloading` 就是**撒谎**——而撒谎的那一帧正好是退出流程里最后几帧，界面上
// 那个「正在下载」会跟着一起消失，用户只留下「它刚才说我还在下」的印象。
func (u *updater) backToAvailable() {
	u.mu.Lock()
	u.state.phase = UpdateAvailable
	u.mu.Unlock()
	u.wake()
}

// fail 记一次失败。
//
// ctx 已经取消的（用户退出了）不算失败：那不是出错，是用户不要了。
func (u *updater) fail(ctx context.Context, err error) {
	if ctx.Err() != nil || err == nil {
		return
	}
	u.mu.Lock()
	u.state.phase = UpdateFailed
	u.state.err = err.Error()
	u.mu.Unlock()
	u.wake()
}

// observe 是给框架 `Install` 的进度回调，在它的下载 goroutine 上被调。
//
// 它认两件事：**这一趟从头开始了**（增量没能用上，改下全量）和「又传了多少」。
// 认法见 updateState.observe 的注释。
func (u *updater) observe(downloaded, total int64) {
	now := u.now()
	u.mu.Lock()
	if u.state.phase != UpdateDownloading {
		u.mu.Unlock()
		return
	}
	u.state.observe(now, downloaded, total)
	u.mu.Unlock()
	u.wake()
}

// wake 叫醒界面。**投递完状态必须叫醒**，只投递不唤醒 = 0 帧刷新（铁律三）。
func (u *updater) wake() {
	a := u.app
	if a == nil {
		return
	}
	a.mu.Lock()
	fn := a.wake
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// notifyUpdateAvailable 把「查到了新版本」告诉界面层。
//
// ⚠️ **不在 `u.mu` 底下调界面层那一头**：那一头会读窗口状态（它自己带锁），
// 而这一层拿着 `u.mu` 去调另一个包的代码就是当场死锁——死锁在这里的表现
// 是「整个应用冻住」。
func (u *updater) notifyUpdateAvailable(version string) {
	a := u.app
	if a == nil {
		return
	}
	a.mu.Lock()
	notifier := a.updateNotifier
	a.mu.Unlock()
	if notifier != nil {
		notifier(version)
	}
}

// updateNotificationEnabled 报「用户要不要「有更新了」这一条系统通知」。
//
// ⚠️ **那一格在设置文件里**（`Settings.UpdateNotification`，见 `settings.go`），
// 而这里读的是**当前那一份**（`SettingsSnapshot` 拿的是 store 的锁底下那份
// 拷贝，所以跨 goroutine 读它是安全的——检查更新在后台 goroutine 上）。
//
// ⚠️ **没挂设置时按「开」算**：没挂设置那份 store 只在拿不到设置文件路径时
// 才会没有，而那时候用户连设置页都看不了——按「开」算的话通知照发（而那一档
// 本来也会降级成应用内提示），按「关」算的话用户就**永远收不到**更新提醒了。
func (a *App) updateNotificationEnabled() bool {
	return a.SettingsSnapshot().UpdateNotificationEnabled()
}

// updateNotificationEnabled 是更新器那一头用的那一版。
//
// ⚠️ **它和 `App` 那一版是同一件事**（`App` 那一版是给别处用的入口），只是
// 这一份是从 `updater` 上调的，而 `updater` 手里有 `u.app`。
func (u *updater) updateNotificationEnabled() bool {
	a := u.app
	if a == nil {
		return true
	}
	return a.updateNotificationEnabled()
}

// SetUpdateNotifier 装上「查到了新版本」的那只手，由界面层装一次。
//
// ⚠️ **没装的时候那件事也不能丢**：界面层没装（无头测试、或者这一轮界面层
// 没起来）时，更新器留给界面的那一句提示照旧弹（走的是 `notice` 那条线），
// 而它不依赖系统通知。
func (a *App) SetUpdateNotifier(fn func(version string)) {
	a.mu.Lock()
	a.updateNotifier = fn
	a.mu.Unlock()
}

// errUpdateUnavailable 是「这个应用不能给自己更新」时给界面的一句话。
//
// 为什么只能说到「打包 / 安装位置」这一层：框架的 `Enabled()` 只是一枚 bool
// （`mygo/updater.go`），把「没打包」和「装在只能管理员写的位置」压成了同一个
// false，而这两者的修法完全相反（一个是等发布，一个是挪位置）。说一个准的、
// 两种都覆盖的话，比编一个具体原因诚实。
var errUpdateUnavailable = errors.New("这个版本不能自动更新：开发构建，以及装在当前用户改不了的位置上的应用都不能")

// UpdateUnavailableReason 报「不能自动更新」时给用户看的那句话，空串表示能。
func (s UpdateState) UpdateUnavailableReason() string {
	if s.Enabled {
		return ""
	}
	return errUpdateUnavailable.Error()
}

// updateVersionUnknown 是问不到版本号时那一行上写的话。
//
// 开发构建里 `mygo.App.Version()` 是空串（版本是 `mygo build` 用 `-ldflags` 塞的，
// 见 `mygo/app.go` 的 `Version`）。那一格写「当前版本 」后面空着，比写一个假
// 版本号好。
const updateVersionUnknown = "未知"

// VersionLabel 把版本号写成界面上那一行的样子。
//
// 它住在这一层而不是界面层：那一行要写的不只是版本号，还有「问不到的时候写什么」
// 这条判断——而「开发构建里版本号是空的」是这一层知道的事（`mygo.App.Version`
// 的来源），让界面层自己判一次就得再知道一遍。
func VersionLabel(v string) string {
	if strings.TrimSpace(v) == "" {
		return updateVersionUnknown
	}
	return v
}
