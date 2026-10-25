// 「冷启动」那一段的计时（票 #478）。
//
// # 为什么要有它
//
// 「双击 app → 第一首歌开始出声」是一条**串行**的链，而它的每一段慢起来的
// 原因**完全不同**：
//
//	进程起来 → 零件挂好	读盘（设置、下载队列）+ **读钥匙串**
//	零件挂好 → 应用就绪	事件循环转起来
//	应用就绪 → 窗口首帧	建窗口、画第一帧
//	首帧 → 取流开始		（恢复上次那一首时，它**在开窗之前**就开始了）
//	取流三段			① 问地址 ② 下音频 ③ 解码
//
// 混在一起只有一个「总的等了多久」，而那个数**说不出慢的是哪一段**——
// 而这几段的 remedies 完全不同：读盘慢是磁盘的事，钥匙串慢是「等人」，
// 首帧慢是界面线程的事，① 是 B 站接口慢，② 是网络慢，③ 是这台机器的解码器。
//
// # 「慢」和「等人」是两件事
//
// ⚠️ **钥匙串那一段可能是「等人」而不是「慢」**：macOS 会在应用
// **还没有窗口**的时候弹一个系统授权框（「BiuGo 想要访问钥匙串里的机密
// 信息」），用户要点一下才继续。那一段的时长**是用户点的**，不是我们慢——
// 所以它**单独一栏**，不跟别的段混在一起说。
//
// （另一张票 #473 在查「为什么每次开 app 都要钥匙串授权」，那是根因；
// 这一票只负责**把它从「慢」里分出来**。）
//
// # 它只写日志，不进界面
//
// 界面上要的是「现在卡在哪一步」，那是 `State.LoadingStage` 的活
// （见 `internal/app/online.go`）。而这一份是**事后查**用的——
// 用户能描述的只有「它不响」（票 #282）。

package app

import (
	"log"
	"sync"
	"time"
)

// startupBorn 是「进程起来」的时刻。
//
// ⚠️ **它是包初始化时刻，不是 `main()` 的第一行**——而这**更接近**
// 「用户双击的那一刻」：`main()` 的第一行之前还有 Go 运行时的初始化
// （而包初始化也在运行时初始化之后）。拿它当起点，量出来的数**偏大**，
// 而偏大的那一段是「应用自己起的」，不是「用户等到的」。
//
// ⚠️ **它是包级变量，不是一个函数的结果**：包初始化在 `main()` 之前跑，
// 所以它比 `main()` 里的任何一句都早。
var startupBorn = time.Now()

// keychainWaitThreshold 是「钥匙串那一段超过多少就算『等过人』」。
//
// ⚠️ **这是一个阈值，不是一个事实**：我们**量不出**用户到底点没点
// （系统那个授权框不归我们管）。而一次正常的钥匙串读是**毫秒级**的
// （它就是读一条记录），所以「超过一秒」几乎只能是「弹了框、用户在想
// 要不要点」。
//
// 取 1 秒：比它小的话，一次慢一点的磁盘就能误判；比它大的话，用户
// 盯着一个没有窗口的进程等三秒就足以让人觉得「坏了」。
const keychainWaitThreshold = time.Second

// StartupTiming 是「冷启动」那一段的分段耗时。
//
// ⚠️ **零值表示「没量到」**：某一段压根没发生（比如没登录、没恢复出
// 什么可放的，那一段的耗时是 0 而不是「瞬间」）。
type StartupTiming struct {
	// Mount 是「进程起来 → 所有零件挂好（开窗口之前）」。
	Mount time.Duration
	// Ready 是「进程起来 → 应用就绪（事件循环转起来了）」。
	Ready time.Duration
	// Frame 是「进程起来 → 窗口第一帧画出来」。
	Frame time.Duration
	// Fetch 是「进程起来 → 第一次取流开始」。
	//
	// ⚠️ **它可以是 0**：没登录 / 没有可恢复的那首时，根本没有取流。
	Fetch time.Duration
	// FetchBeforeFrame 说「第一次取流是在首帧**之前**开始的」。
	//
	// 🔴 **恢复上次那一首时它多半是 true**：取流在开窗口**之前**就
	// 发出去了（`RestorePlayback` 排在 `openWindow` 前面，见
	// `internal/ui/shell_window.go`）——于是用户看到窗口的时候，
	// 那一趟**已经在路上**了。
	//
	// 这一栏是「首帧 → 第一次取流开始」那一段的**答案**：它是负的
	// （= 取流更早），而那一段问的就是「用户看到窗口的时候，
	// 取流是不是已经在走了」。
	FetchBeforeFrame bool
	// Keychain 是「挂登录那一段花了多久」——**它可能是「等人」**。
	//
	// ⚠️ **它单独一栏**：见文件头那一节。
	Keychain time.Duration
	// KeychainWaited 说「钥匙串那一段慢到不像自动的」（多半是等过人）。
	//
	// ⚠️ **这是推断，不是事实**（见 `keychainWaitThreshold`）。
	KeychainWaited bool
}

// startupLog 是「冷启动分段耗时」那一行日志的落点。
//
// ⚠️ **它不是 `a.logf`**：那一层是「播放路径」的日志出口（每个 App 一份），
// 而冷启动这一行**每个进程只有一条**——它属于应用整体，不属于某一次播放。
var startupLog = func(format string, args ...any) { log.Printf(format, args...) }

// startupState 是冷启动那一段的计时状态。
//
// ⚠️ **它自己带锁**：冷启动的这些时刻是从**好几个地方**记的（主线程、
// 界面线程、后台 goroutine），而 App 那把锁是界面每帧都要的——
// 在冷启动这一小段里抢它没有必要。
type startupState struct {
	mu sync.Mutex

	// once 保证「首帧」只被处理一次——而 `Shell.View` **每帧**都调它。
	//
	// ⚠️ **它必须是 `sync.Once` 而不是「拿锁判一下」**：界面每帧都要调
	// 这一句，而每帧抢一次锁是白花的（`a.mu` 那种每帧都要的锁已经是
	// 这个仓库反复强调的忌讳了）。
	once     sync.Once
	mounted  time.Time
	ready    time.Time
	frame    time.Time
	fetch    time.Time
	keychain time.Duration
	logged   bool
}

// note 记下一个时刻。**调用者必须已经拿着 `s.mu`**。
//
// ⚠️ **「第一个记下的赢」**：同一件事可能被记两次（比如首帧那条 view 被
// 调了两遍），而「第一次」才是用户等的那一次。
func (s *startupState) note(slot *time.Time, at time.Time) {
	if slot.IsZero() {
		*slot = at
	}
}

// NoteMounted 记「所有零件挂好了」（开窗口之前）。
//
// 调用点：`main.go` 里那一串 `Mount*` 之后。
func (a *App) NoteMounted() {
	a.startup.mu.Lock()
	a.startup.note(&a.startup.mounted, time.Now())
	a.startup.mu.Unlock()
}

// NoteReady 记「应用就绪了」（事件循环转起来了，可以建窗口了）。
//
// 调用点：`internal/ui/shell_window.go` 的 `WhenReady` 回调开头。
func (a *App) NoteReady() {
	a.startup.mu.Lock()
	a.startup.note(&a.startup.ready, time.Now())
	a.startup.mu.Unlock()
}

// NoteFirstFrame 记「窗口第一帧画出来了」。
//
// 调用点：`internal/ui/shell_view.go` 的 `Shell.View`——**只记第一次**。
//
// 🔴 **它顺手把冷启动那一行写进日志**：那是「用户双击 → 窗口出来」
// 这一段的**唯一凭据**，而用户能描述的只有「它半天没反应」。
func (a *App) NoteFirstFrame() {
	a.startup.once.Do(func() {
		now := time.Now()
		a.startup.mu.Lock()
		a.startup.frame = now
		a.startup.logged = true
		a.startup.mu.Unlock()
		// 🔴 **「启动那一段」到这儿为止**——除非**还有一趟取流在路上**
		//（冷启动恢复上次那一首：窗口开了，而那一首还在取）。
		//
		// ⚠️ **没有取流在路上就收掉**：那时候应用已经准备好了（队列恢复了、
		// 该放的放了），界面上不该再写「正在启动」。
		a.mu.Lock()
		if !a.loading {
			a.starting = false
		}
		a.mu.Unlock()
		startupLog("biu: 冷启动分段耗时：%s", a.StartupTiming().String())
	})
}

// NoteFetchStart 记「第一次取流开始了」。
//
// 调用点：`internal/app/online.go` 的 `runOnline` 开头。
//
// ⚠️ **只记第一次**：连着点两首时，第二首不该把第一首的时刻顶掉。
func (a *App) NoteFetchStart() {
	now := time.Now()
	a.startup.mu.Lock()
	// 🔴 **「第一次」这个闸要管住**写日志**那一句**：它在闸门外面的话，
	// 每点一首歌都会往「冷启动」那一栏里写一行「首帧之后才开始取流，
	// 中间隔了 5m0s」——而那一栏是**专门为「事后查冷启动」**建的，
	// 混进一次次点歌的记录之后，它就什么也答不出了。
	first := a.startup.fetch.IsZero()
	if first {
		a.startup.fetch = now
	}
	frame := a.startup.frame
	logged := a.startup.logged
	a.startup.mu.Unlock()
	// 🔴 **首帧已经记过了**：那时候还没有取流，于是那一行日志里
	// 「取流开始」是空的。现在补一行「首帧 → 取流开始」——
	// 那正是票面问的「首帧 → 第一次取流开始」那一段。
	//
	// ⚠️ **只补这一次**（`first`）：这一栏说的是**冷启动**，
	// 而冷启动只有一次。
	if first && logged && !frame.IsZero() {
		startupLog("biu: 冷启动：首帧之后才开始取流，中间隔了 %v", now.Sub(frame))
	}
}

// NoteKeychainRead 记「挂登录（读钥匙串）那一段花了多久」。
//
// 调用点：`internal/app/account.go` 的 `App.MountLogin`。
//
// ⚠️ **这一段可能是「等人」**：macOS 会在应用还没有窗口的时候弹一个
// 系统授权框，用户要点一下才继续。那一段的时长**是用户点的**，
// 不是我们慢——所以它单独一栏（见 `StartupTiming.Keychain`）。
func (a *App) NoteKeychainRead(d time.Duration) {
	a.startup.mu.Lock()
	a.startup.keychain = d
	a.startup.mu.Unlock()
}

// StartupTiming 报「冷启动」那一段的分段耗时。
func (a *App) StartupTiming() StartupTiming {
	a.startup.mu.Lock()
	defer a.startup.mu.Unlock()

	var t StartupTiming
	if !a.startup.mounted.IsZero() {
		t.Mount = a.startup.mounted.Sub(startupBorn)
	}
	if !a.startup.ready.IsZero() {
		t.Ready = a.startup.ready.Sub(startupBorn)
	}
	if !a.startup.frame.IsZero() {
		t.Frame = a.startup.frame.Sub(startupBorn)
	}
	if !a.startup.fetch.IsZero() {
		t.Fetch = a.startup.fetch.Sub(startupBorn)
	}
	t.FetchBeforeFrame = !a.startup.fetch.IsZero() &&
		(a.startup.frame.IsZero() || a.startup.fetch.Before(a.startup.frame))
	t.Keychain = a.startup.keychain
	t.KeychainWaited = t.Keychain >= keychainWaitThreshold
	return t
}

// String 把这一份排成一句能进日志的话。
//
// ⚠️ **它按「人读得懂的顺序」排**：从进程起来往后，一段一段。
func (t StartupTiming) String() string {
	s := "起来 → "
	if t.Mount > 0 {
		s += "零件挂好 " + t.Mount.Round(time.Millisecond).String() + " → "
	}
	if t.Ready > 0 {
		s += "应用就绪 " + t.Ready.Round(time.Millisecond).String() + " → "
	}
	if t.Frame > 0 {
		s += "首帧 " + t.Frame.Round(time.Millisecond).String()
	} else {
		s += "首帧 —"
	}
	// 🔴 **取流那一栏要说清「在首帧之前还是之后」**：恢复上次那一首时
	// 它在首帧之前，而那正是「用户看到窗口时那一趟已经在路上」这件事。
	if t.Fetch > 0 {
		if t.FetchBeforeFrame {
			s += "（取流在首帧之前就开始了，" + t.Fetch.Round(time.Millisecond).String() + "）"
		} else {
			s += "（取流在首帧之后才开始，" + t.Fetch.Round(time.Millisecond).String() + "）"
		}
	} else {
		s += "（没有取流）"
	}
	// ⚠️ **钥匙串单独说**：它可能是「等人」而不是「慢」——
	// 混进上面那一串里的话，用户读到的是「应用慢」。
	if t.Keychain > 0 {
		s += "；读钥匙串 " + t.Keychain.Round(time.Millisecond).String()
		if t.KeychainWaited {
			s += "（多半是在等系统授权框点一下——那是「等人」，不是「慢」）"
		}
	}
	return s
}
