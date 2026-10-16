//go:build windows && (amd64 || arm64)

// Windows 系统媒体会话（SMTC）：把「现在在放什么」交给 Windows 的媒体浮窗、
// 锁屏和键盘媒体键，并把那边按回来的命令递给我们。
//
// **以及任务栏缩略图上那一条按钮**——它们和 SMTC 是两套东西，但在这一票里
// 归同一个 `windowsPlatform` 管：两样都要一个窗口的 HWND，都从同一份
// `NowPlaying` 出发，都把命令交给同一个 handler。
//
// # ⚠️ 这一整份没在 Windows 上跑过
//
// 开发这一票的机器是 macOS。下面的 IID、虚表下标、枚举值全部是**照文档抄的**，
// 出处写在各处注释里。能自动断的那一半（状态/命令的映射、按钮的规则、图标）
// 都在别的文件里，见 smtc_values.go / thumbbar.go / thumbbar_glyph.go。
// 剩下这一份只能靠真机验，验哪几条见报告。
//
// 抄的对象：`SystemMediaTransportControlsInterop.h`（Windows SDK，MIDL 生成）
// 和 Qt 那份手抄的 WinRT ABI 头（cloudmus/cloudmus 的
// fronts/qt/src/Integration/SmtcAbiWin.h，它自己说是从 windows-rs 生成的绑定
// 转抄的，并交叉核对了 MS 文档）。

package media

import (
	"log"
	"sync"
	"time"
	"unsafe"
)

// ---- IID ----
//
// ⚠️ 这几个 GUID 抄错的后果是 `QueryInterface` 返回 `E_NOINTERFACE`，
// 而**症状是「功能静默地不出现」**：浮窗上没有歌、任务栏上没有按钮，
// 界面上和任务栏上都没有任何提示。

var (
	// iidSmtcInterop 是 `ISystemMediaTransportControlsInterop`
	// （SystemMediaTransportControlsInterop.h）。
	//
	// 它的虚表只有 `GetForWindow(HWND, REFIID, void**)` 一个方法（第 6 格）——
	// 前面 6 格是 IUnknown 三格加 IInspectable 三格。用它是因为
	// **`RoActivateInstance` 在 STA 线程上会失败而它不会**，而这一票最要紧的
	// 就是别在 UI 线程上失败。
	iidSmtcInterop = mustGUID("ddb0472d-c911-4a1f-86d9-dc3d71a95f5a")

	// iidSmtc 是 `ISystemMediaTransportControls`（WinRT ABI 层）。
	iidSmtc = mustGUID("99fa3ff4-1742-42a6-902e-087d41f965ec")

	// iidSmtc2 是 `ISystemMediaTransportControls2`：时间轴（浮窗上那根进度条）
	// 和「用户拖了进度条」这个事件在它上面。
	iidSmtc2 = mustGUID("ea98d2f6-7f3c-4af2-a586-72889808efb1")

	// iidSmtcButtonArgs 是 `SystemMediaTransportControlsButtonPressedEventArgs`。
	iidSmtcButtonArgs = mustGUID("b7f47116-a56f-4dc8-9e11-92031f4a87c2")

	// iidSmtcSeekArgs 是 `IPlaybackPositionChangeRequestedEventArgs`。
	iidSmtcSeekArgs = mustGUID("b4493f88-eb28-4961-9c14-335e44f3e125")

	// iidSmtcTimeline 是 `ISystemMediaTransportControlsTimelineProperties`。
	// 它是一个可激活类，所以要的是它自己那个 IID。
	iidSmtcTimeline = mustGUID("5125316a-c3a2-475b-8507-93534dc88f15")

	// ⚠️ 这里**故意没有** `IDisplayUpdater` 和 `IMusicProperties` 的 IID：
	// `get_DisplayUpdater` / `get_MusicProperties` 递出来的已经就是那两个接口
	// 本身，按 IDL 它们是该类型的指针（C++ 那边等于一次 static_cast）。
	// 再 QI 一次看着更保险，实际是把两个本可以绕开的 IID 抄错风险放到关键
	// 路径上——抄错了的症状是「浮窗上什么都没有」而日志上什么都看不到。
)

// ---- 虚表下标 ----
//
// 一律是「从 0 数，0 是 QueryInterface」的下标。WinRT 的接口在 IUnknown 三格
// 之后还有 IInspectable 三格（GetIids / GetRuntimeClassName / GetTrustLevel），
// 所以第一个自己的方法在第 6 格。

const (
	// `ISystemMediaTransportControlsInterop`
	interopGetForWindow = 6

	// `ISystemMediaTransportControls`
	smtcPutPlaybackStatus  = 7
	smtcGetDisplayUpdater = 8
	smtcPutIsEnabled      = 11
	smtcPutIsPlayEnabled  = 13
	smtcPutIsStopEnabled  = 15
	smtcPutIsPauseEnabled = 17
	smtcPutIsPrevEnabled  = 25
	smtcPutIsNextEnabled  = 27
	smtcAddButtonPressed  = 32
	smtcRemoveButton      = 33

	// `ISystemMediaTransportControls2`
	smtc2UpdateTimelineProperties = 12
	smtc2AddPositionChange        = 13
	smtc2RemovePositionChange     = 14

	// `SystemMediaTransportControlsTimelineProperties`（`IInspectable` 派生）
	timelinePutStartTime = 7
	timelinePutEndTime   = 9
	timelinePutMinSeek   = 11
	timelinePutMaxSeek   = 13
	timelinePutPosition  = 15

	// 事件参数
	buttonArgsGetButton     = 6
	seekArgsGetRequestedPos = 6

	// `SystemMediaTransportControlsDisplayUpdater`
	updaterPutType   = 7
	updaterPutThumb  = 11
	updaterGetMusic  = 12
	updaterClearAll  = 16
	updaterUpdate    = 17

	// `IMusicDisplayProperties`
	musicPutTitle       = 7
	musicPutAlbumArtist = 9
	musicPutArtist      = 11
)

// playbackTypeMusic 是 `MediaPlaybackType::Music`（1）。
//
// ⚠️ **必设**：不设的话系统按 `Unknown` 走，浮窗上那一行会走「未知类型」的
// 版式（不画歌名的样式），而歌名本身是有的——症状是「标题在那儿但排版不对」，
// 很难归因。
const playbackTypeMusic = 1

// windowsTicks 是 `Windows.Foundation.TimeSpan` 的单位（100 纳秒）。
//
// ⚠️ **不是纳秒也不是毫秒**：SMTC 的时间轴全用 100 纳秒。差 100 倍的症状是
// 「歌才播了两秒，进度条走完了整首」。
const windowsTicks = 10 * time.Millisecond

// windowsPlatform 是 Windows 那一半的平台实现。
//
// 零值不可用，请用 newPlatform。**没有窗口之前它什么都不是**：Windows 上的
// 媒体会话是**按窗口**的（`GetForWindow`），所以它由 `bindWindow` 激活，
// 而不是由 `newPlatform`。
type windowsPlatform struct {
	mu   sync.Mutex
	hwnd uintptr

	// smtc 是 `ISystemMediaTransportControls`。0 表示还没有窗口 / 建不上。
	smtc uintptr
	// smtc2 是同一个对象上的第二个接口，时间轴和拖动进度条在它上面。
	// 建不上不是致命的（系统太老），只是浮窗上没有那根进度条。
	smtc2 uintptr

	// thumb 是任务栏缩略图那一排按钮。建不上不是致命的。
	thumb *thumbbar

	// on 是系统发来命令时叫谁。由 attach 装，bindWindow 之后才开始有用。
	on func(Command, time.Duration)

	// last 是最近一次 apply 过的快照。见 replayState。
	last NowPlaying

	// 事件令牌。add 的时候系统写进来，摘的时候要原样还回去——
	// 记不住就没法摘，而摘不掉的事件处理器会在每次换窗口时多挂一个。
	buttonToken int64
	seekToken   int64
	eventsUp    bool
}

// newPlatform 造 Windows 那一半。
//
// **它在这里不做任何系统调用**，只确认「该有的入口函数在不在」——因为真正的
// 激活要一个窗口，而窗口要等 `ui.Run` 才建得出来（见 bindWindow）。
// 所以这里返回的东西在 bindWindow 成功之前是个哑巴；`Available()`（media.go
// 里的那个）说的只是「Windows 这一半在」，不是「已经连上系统了」。
func newPlatform(Options) (platform, error) {
	if procRoGetFactory.Find() != nil {
		return nil, errPlatform("这个系统上没有 WinRT 激活入口（RoGetActivationFactory）")
	}
	return &windowsPlatform{}, nil
}

// bindWindow 接到一个窗口上：建 SMTC、建缩略图按钮、把事件处理器装上。
//
// 由 `media.Session.BindWindow` 调（见 media.go），界面层在窗口建出来之后
// 调一次。**幂等**：绑第二个窗口之前会把上一个的收掉。
func (p *windowsPlatform) bindWindow(hwnd uintptr) error {
	if hwnd == 0 {
		return errPlatform("窗口句柄是 0")
	}
	p.unbind()

	roInitialize()

	// ⚠️ 这里要的是**工厂对象本身**，不是它造出来的实例：`RoGetActivationFactory`
	// 用 `IID_ISystemMediaTransportControlsInterop` 去问，返回的就是那个
	// interop 接口（工厂对象同时实现了它）。桌面应用拿媒体会话的正规途径就是
	// 直接问它 `GetForWindow`，而不是 `RoActivateInstance`（后者在 STA 线程上
	// 会失败，而这一票最要紧的就是别在 UI 线程上失败）。
	interop, err := activationFactory("Windows.Media.SystemMediaTransportControls", iidSmtcInterop)
	if err != nil {
		return err
	}
	defer release(interop)

	var smtc uintptr
	hr := comCall(interop, interopGetForWindow,
		hwnd, uintptr(unsafe.Pointer(&iidSmtc)), uintptr(unsafe.Pointer(&smtc)))
	if hresultFailed(hr) || smtc == 0 {
		return hresultError("拿这个窗口的系统媒体会话", hr)
	}
	addRef(smtc)

	p.mu.Lock()
	p.smtc, p.hwnd = smtc, hwnd
	p.smtc2 = queryInterface(smtc, iidSmtc2)
	p.mu.Unlock()

	// 总开关。**必设**：不设的话下面写的标题、进度、按钮全都不生效——
	// 而系统的反应是「安静地什么都不做」。
	comSetBool(smtc, smtcPutIsEnabled, true)

	// 缩略图按钮。建不上只记日志：SMTC 和它是两套东西，一边不成另一边照用。
	//
	// ⚠️ 那个 `on` 是在锁**外面**读的：它由 attach 装，而 attach 走的是
	// `Session.SetHandler`（装配阶段、UI 线程上）。这里再拿一次锁是为了不把
	// 「-race 偶尔红一次、而那一红点落在别人头上」这种坑留给下一个人。
	p.mu.Lock()
	on := p.on
	p.mu.Unlock()
	thumb, err := newThumbbar(hwnd, on)
	if err != nil {
		log.Printf("biu: 任务栏缩略图上没有按钮（浮窗和锁屏照常）：%v", err)
	} else {
		p.mu.Lock()
		p.thumb = thumb
		p.mu.Unlock()
	}

	p.replayState()
	p.ensureEvents()
	return nil
}

// unbind 把窗口那一摊全部收掉。可以对着没绑过的会话调。
func (p *windowsPlatform) unbind() {
	p.mu.Lock()
	smtc, smtc2, thumb := p.smtc, p.smtc2, p.thumb
	p.smtc, p.smtc2, p.thumb, p.hwnd = 0, 0, nil, 0
	p.eventsUp = false
	p.mu.Unlock()

	if thumb != nil {
		thumb.close()
	}
	if smtc != 0 {
		// 摘事件处理器必须在放掉 SMTC **之前**：系统的回调线程上可能还有一个
		// Invoke 正在路上，而它按的是我们这张表。
		p.removeEvents(smtc, smtc2)
		release(smtc2)
		release(smtc)
	}
}

// ensureEvents 把两个事件处理器装上（如果还没装）。
//
// attach 和 bindWindow 谁先到都行，所以两边都调它，而它自己是幂等的：
// 重复 add 的话系统会挂两个处理器，用户在浮窗上按一次播放**歌会跳两下**——
// 那正是这一票要防的「一次按键切两首」的另一种样子。
func (p *windowsPlatform) ensureEvents() {
	p.mu.Lock()
	if p.eventsUp || p.smtc == 0 || p.on == nil {
		p.mu.Unlock()
		return
	}
	smtc, smtc2 := p.smtc, p.smtc2
	p.eventsUp = true
	p.mu.Unlock()

	// ⚠️ **令牌写在局部变量上，不直接写进 p**：系统是把 token 写进我们给的
	// 那个地址的，而 `p.buttonToken` 同时被 removeEvents 拿着锁在读——
	// 直接写进去就是一次数据竞争，症状是「关窗口再开，偶尔摘不掉那个处理器，
	// 于是浮窗上按一次播放跳两下」，而且只在 `-race` 下面偶尔红一次。
	var buttonToken int64
	hr := withHandler(func(_, args uintptr) { p.onButton(args) },
		func(h uintptr) uintptr {
			return comCall(smtc, smtcAddButtonPressed, h,
				uintptr(unsafe.Pointer(&buttonToken)))
		})
	if hresultFailed(hr) {
		p.mu.Lock()
		p.eventsUp = false
		p.mu.Unlock()
		log.Printf("biu: 浮窗上的播放键装不上（HRESULT %#08x）", uint32(hr))
		return
	}

	var seekToken int64
	if smtc2 != 0 {
		withHandler(func(_, args uintptr) { p.onSeek(args) },
			func(h uintptr) uintptr {
				return comCall(smtc2, smtc2AddPositionChange, h,
					uintptr(unsafe.Pointer(&seekToken)))
			})
	} else {
		logOnce("这个系统没有可用的时间轴接口，浮窗上没有进度条")
	}

	p.mu.Lock()
	p.buttonToken, p.seekToken = buttonToken, seekToken
	p.mu.Unlock()
}

// removeEvents 摘掉两个事件处理器。
//
// ⚠️ **必须按令牌摘，不能只放着**：`add_*` 会把系统那边那一份引用留着，
// 而我们那个 Go 对象是靠引用计数活的。放着不摘的话，用户每关一次窗口再开
// 一次就多留一份——而浮窗上按一次播放会**跳两下**（两个处理器都响应），
// 那是最难解释的一种错：它隔很久才出现一次，而且看着像「程序偶尔抽风」。
func (p *windowsPlatform) removeEvents(smtc, smtc2 uintptr) {
	p.mu.Lock()
	buttonToken, seekToken := p.buttonToken, p.seekToken
	p.buttonToken, p.seekToken = 0, 0
	p.mu.Unlock()

	if buttonToken != 0 {
		comCall(smtc, smtcRemoveButton, uintptr(buttonToken))
	}
	if seekToken != 0 && smtc2 != 0 {
		comCall(smtc2, smtc2RemovePositionChange, uintptr(seekToken))
	}
}

// onButton 是 SMTC 的 ButtonPressed 回调。跑在系统的回调线程上。
//
// ⚠️ 这里**只排队不干活**：handler 一调就是 `Session.dispatch`，它把命令塞进
// 队列立刻就回来（见 media.go）。业务层在另一个协程上跑，那边可能要发网络
// 请求（跳位置、换流），在系统的回调线程上做就是界面卡住。
func (p *windowsPlatform) onButton(args uintptr) {
	btnArgs := queryInterface(args, iidSmtcButtonArgs)
	if btnArgs == 0 {
		return
	}
	defer release(btnArgs)
	var raw int32
	if hresultFailed(comCall(btnArgs, buttonArgsGetButton, uintptr(unsafe.Pointer(&raw)))) {
		return
	}
	cmd, ok := smtcCommand(smtcButton(raw))
	if !ok {
		// 我们没接的键（快进、倒带、录音…）。对应的 IsXxxEnabled 在 apply 里
		// 被设成 false，正常情况下系统不会发这个；真发过来了（比如用户在
		// 锁屏上翻到了别的控件）就当没听见。
		return
	}
	p.dispatch(cmd, 0)
}

// onSeek 是 SMTC 的「用户拖了浮窗上的进度条」。同样只排队。
func (p *windowsPlatform) onSeek(args uintptr) {
	seekArgs := queryInterface(args, iidSmtcSeekArgs)
	if seekArgs == 0 {
		return
	}
	defer release(seekArgs)
	var ticks int64
	if hresultFailed(comCall(seekArgs, seekArgsGetRequestedPos, uintptr(unsafe.Pointer(&ticks)))) {
		return
	}
	at := time.Duration(ticks) * windowsTicks
	if at < 0 {
		return
	}
	p.dispatch(CommandSeek, at)
}

// dispatch 把命令交给装好的那个 handler。
func (p *windowsPlatform) dispatch(cmd Command, at time.Duration) {
	p.mu.Lock()
	on := p.on
	p.mu.Unlock()
	if on != nil {
		on(cmd, at)
	}
}

// attach 装上「系统发来命令时叫谁」，并把事件处理器补上。
func (p *windowsPlatform) attach(fn func(Command, time.Duration)) {
	p.mu.Lock()
	p.on = fn
	thumb := p.thumb
	p.mu.Unlock()

	// 缩略图按钮走的是自己的消息回调，不经过 SMTC，所以那边要单独认一份。
	if thumb != nil {
		thumb.setHandler(fn)
	}
	p.ensureEvents()
}

// detach 摘掉命令处理器。**不清 SMTC 里的曲目信息**——那件事归 `apply`，
// `Session.Clear` 会专门发一次 StateNone（为什么这么写见 media.go）。
//
// ⚠️ 这里**故意不 `unbind`**（不放 SMTC、不摘窗口子类）。走到这里只有一条路：
// 进程正在退出（`ui.MountMedia` 把它挂在 `OnWillQuit` 上）。这时候去摘子类
// 反而多一次跨线程的窗口消息——而摘它唯一的理由是「免得下一个窗口收到
// 指向已释放对象的回调」，而下一个窗口在退出流程里根本不会出现。
func (p *windowsPlatform) detach() {
	p.mu.Lock()
	thumb := p.thumb
	p.on = nil
	p.mu.Unlock()

	if thumb != nil {
		thumb.setHandler(nil)
	}
}

// apply 把一份快照写给系统。
//
// 每一段失败都只是「那一项没更新」：系统的反应全是安静地不显示，而这一层
// 崩掉的话整个会话就没了。所以这里**不返回错误**，只记日志。
func (p *windowsPlatform) apply(n NowPlaying) {
	p.mu.Lock()
	p.last = n
	smtc, smtc2, thumb := p.smtc, p.smtc2, p.thumb
	p.mu.Unlock()
	if smtc == 0 {
		return
	}

	p.applyStatus(smtc, n)
	p.applyTimeline(smtc2, n)
	p.applyMetadata(smtc, n)
	if thumb != nil {
		thumb.apply(n)
	}
}

// applyStatus 写播放状态和「哪几个键现在是有效的」。
func (p *windowsPlatform) applyStatus(smtc uintptr, n NowPlaying) {
	comCall(smtc, smtcPutPlaybackStatus,
		uintptr(int32(smtcPlaybackStatus(n.State))))

	// ⚠️ **必设，而且每一次都设**：Windows 自己会按播放状态改这几个键，
	// 不告诉它就会和我们这边的对不上（用户点播放而系统以为是暂停）。
	hasTrack := n.State != StateNone
	playing := n.State == StatePlaying
	comSetBool(smtc, smtcPutIsPlayEnabled, hasTrack && !playing)
	comSetBool(smtc, smtcPutIsPauseEnabled, hasTrack && playing)
	comSetBool(smtc, smtcPutIsNextEnabled, hasTrack)
	comSetBool(smtc, smtcPutIsPrevEnabled, hasTrack)
	comSetBool(smtc, smtcPutIsStopEnabled, hasTrack)
}

// applyTimeline 写浮窗上那根进度条。
//
// ⚠️ **EndTime 和 MinSeekTime 缺一不可**：只给 EndTime 的话系统不画那根条
// （它不知道从哪开始），只给 MinSeekTime 的话它画一根永远停在 0 的。
// 这是「浮窗上没有进度条」最常见的两个原因，而它不会报任何错。
func (p *windowsPlatform) applyTimeline(smtc2 uintptr, n NowPlaying) {
	if smtc2 == 0 {
		return
	}
	props, err := activate(
		"Windows.Media.SystemMediaTransportControlsTimelineProperties", iidSmtcTimeline)
	if err != nil {
		logOnce("时间轴那个类型建不出来，浮窗上没有进度条", err)
		return
	}
	defer release(props)

	putTicks := func(slot int, d time.Duration) {
		comCall(props, slot, uintptr(int64(ticksOf(d))))
	}
	putTicks(timelinePutStartTime, 0)
	putTicks(timelinePutEndTime, n.Duration)
	putTicks(timelinePutMinSeek, 0)
	putTicks(timelinePutMaxSeek, n.Duration)
	putTicks(timelinePutPosition, n.Position)

	// ⚠️ 这一句是**这一整段的理由**：光改本地那几个属性系统看不见，
	// 必须显式推一次。漏了它的症状是「浮窗上的进度条永远停在 0」，
	// 而歌名、封面、状态全都正常——最难归因的一种。
	if hr := comCall(smtc2, smtc2UpdateTimelineProperties, props); hresultFailed(hr) {
		logOnce("往系统推时间轴失败，浮窗上的进度条不会动",
			hresultError("UpdateTimelineProperties", hr))
	}
}

// replayState 在绑定之后把手上那份快照补推一次。
//
// 绑定一定发生在第一首歌之后（窗口建出来之前没法建 SMTC），所以不补这一下
// 的话浮窗上要等下一次播放状态变化才有内容——用户看到的是「打开应用，
// 歌已经在放了，浮窗上空着」。
//
// 「没有歌」那一份**照样要推**（`apply` 会去清浮窗和收按钮）——它清掉的是
// 「上一个窗口关掉之前锁屏上那一首」。所以这里没有 if，只有一次调用。
func (p *windowsPlatform) replayState() {
	p.mu.Lock()
	last, smtc := p.last, p.smtc
	p.mu.Unlock()
	if smtc == 0 {
		return
	}
	p.apply(last)
}

// ticksOf 把时长换算成 Windows 的 100 纳秒单位。
//
// ⚠️ 往回夹到 0：负的 TimeSpan 传过去在那边是没有意义的（一个负的 EndTime），
// 而它会变成「整根进度条画不出来」。
func ticksOf(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(d / windowsTicks)
}

// logOnce 同一个错只记一次。
//
// 这一层会**反复**失败（每 5 秒一次的位置校正），而同一句话刷一百遍会把真正
// 该看的那几行淹掉。
var logOnce = func() func(string, ...any) {
	var mu sync.Mutex
	seen := map[string]bool{}
	return func(msg string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if seen[msg] {
			return
		}
		seen[msg] = true
		log.Printf("biu: "+msg, args...)
	}
}()
