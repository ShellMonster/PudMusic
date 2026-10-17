//go:build darwin

package media

// 远程命令的接收端：一个 Objective-C 子类 + 一个 Go 方法实现。
//
// # 为什么用 `addTarget:action:` 而不是 `addTargetWithHandler:`
//
// 后者要传一个 Objective-C block 进来。block 在 purego 这一侧是能造的
// （`objc.NewBlock`），但那每一次都是「一个 Go 闭包 + 一层反射」，
// 而远程命令的处理器是整个进程里活得最久的那一个。这里用 `addTarget:action:`：
// 注册的是**一个类的一个方法**，实例是进程里唯一那一个，方法实现是一个
// 不捕获任何东西的 Go 函数（所有状态都在包里），没有闭包、没有反射、
// 也不会被 GC 收走。
//
// # 一个方法接七个命令
//
// `addTarget:action:` 传进来的 `_cmd` 是 target 自己那个方法的选择子，
// **不是**命令的选择子。真正是哪一个命令，得问事件上的 `command`
// （`MPRemoteCommandEvent.command`）。所以这一处的判断只有一份。

import (
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"
)

// commandClassName 是注册的那个类名。Objective-C 的类名空间是整个进程的，
// 名字撞了 `RegisterClass` 会直接失败。
const commandClassName = "BiuMediaCommandTarget"

// commandSelector 是那个方法的选择子。七个命令共用它。
const commandSelector = "handleRemoteCommand:"

// cmdUnknown 是「这个事件说不清是哪个命令」。
//
// **认不出来必须回一个错误状态，不能猜一个**：猜错的表现是按「下一首」
// 跳了两首（猜成 next + next 两次）或者按「暂停」把歌按停——而用户在
// 界面上看不出任何异样。
const cmdUnknown = Command(-1)

var (
	// commandTargetClass 是注册出来的类；commandTargetInstance 是它唯一的
	// 实例。两者有一个是 0 就是注册没成功（newPlatform 会因此报「装不上」）。
	commandTargetClass    objc.Class
	commandTargetInstance id

	handlerMu sync.RWMutex
	handlerFn func(Command, time.Duration)
)

// commandTargets 是我们接的命令。
//
// ⚠️ `play` / `pause` / `togglePlayPause` **三个都要接**，不是重复：系统按
// 当前状态挑发哪一条（控制中心的按钮在「在放」时发 pause、不在放时发 play，
// 耳机线控那一个键发的是 toggle）。少接一个，那条路上按下去就什么也不发生。
//
// 这一段是照 Apple 的分发规则写的，**不是在这台机器上实测出来的**——本票
// 没能造出真的命令事件（见 command_darwin_test.go 顶上那段），所以「三条
// 都接」是照着文档来的保险做法，不是「少一条就会坏」的实测结论。
var commandTargets = []struct {
	selector string
	cmd      Command
}{
	{"playCommand", CommandPlay},
	{"pauseCommand", CommandPause},
	{"togglePlayPauseCommand", CommandToggle},
	{"nextTrackCommand", CommandNext},
	{"previousTrackCommand", CommandPrev},
	{"stopCommand", CommandStop},
	{"changePlaybackPositionCommand", CommandSeek},
}

// commandsOff 是**故意不接**的命令。
//
// 接了就得有动作，而这一版没有它们的意思（快进 15 秒、变速、评分）。
// 更重要的是它们默认是**开的**：不显式关掉，控制中心就会摆出一个按下去
// 没反应的按钮。
//
// ⚠️ 这份清单上的名字**必须一个个核实过真的存在**。往一个不响应的选择子
// 发消息，Objective-C 抛 `unrecognized selector`，而那是 Go 拦不住的异常——
// 整个进程当场死掉，而且死在这份「本来可有可无」的清单上。
// 实测：`skipIntervalCommand` 在 macOS 上**不存在**（`respondsToSelector:`
// 回 false），加上它之后 attach 一调就崩。
// `commandIfPresent` 里那道 respondsToSelector 检查就是为它准备的。
var commandsOff = []string{
	"skipForwardCommand",
	"skipBackwardCommand",
	"changePlaybackRateCommand",
	"changeRepeatModeCommand",
	"changeShuffleModeCommand",
	"ratingCommand",
	"likeCommand",
	"dislikeCommand",
	"bookmarkCommand",
}

// commandCenter 返回 `MPRemoteCommandCenter.sharedCommandCenter`。
func commandCenter() id {
	return send(id(class("MPRemoteCommandCenter")), "sharedCommandCenter")
}

// commandEnabled 问一个命令现在是开还是关。
//
// ⚠️ **读取用的选择子是 `isEnabled`，不是 `enabled`。** 这些类是 Swift 写的
// （`MPRemoteCommand` 及其子类），一个 Swift 的 `Bool` 属性桥到 Objective-C
// 上时**读取方法会加 `is` 前缀**：属性叫 `enabled`，读取是 `isEnabled`，
// 设置才是 `setEnabled:`。照着头文件想当然地发 `enabled`，得到的是
// `unrecognized selector`——那是个 Go 拦不住的异常，整个进程当场死掉
// （实测）。头文件里写着 `@property (getter=isEnabled) BOOL enabled;`。
func commandEnabled(cmd id) bool {
	return sendBool(cmd, "isEnabled")
}

// commandIfPresent 返回命令中心上那个叫 name 的命令，没有就返回 0。
//
// ⚠️ **不能直接发那条消息**：往一个不响应的选择子发消息，Objective-C 抛
// `unrecognized selector`，而那是 Go 拦不住的异常——整个进程当场死掉。
// 这份清单上就有一个真实的例子（见 commandsOff 那段）。
func commandIfPresent(name string) id {
	center := commandCenter()
	if center == 0 {
		return 0
	}
	if !sendBool(center, "respondsToSelector:", uintptr(sel(name))) {
		return 0
	}
	return send(center, name)
}

// initCommandTarget 注册类、建出唯一的实例。
// **只能在 init 里调**（见 objc_darwin.go 顶上那段警告）。
func initCommandTarget() {
	super := class("NSObject")
	if super == 0 {
		return
	}
	c, err := objc.RegisterClass(commandClassName, super, nil, nil,
		[]objc.MethodDef{{Cmd: sel(commandSelector), Fn: commandImpl}})
	if err != nil {
		log.Printf("biu: 远程命令处理器注册不上，控制中心和锁屏上的按钮按了没反应：%v", err)
		return
	}
	commandTargetClass = c

	// ⚠️ `addTarget:action:` **不 retain target**，也不保存它——系统只记下
	// 这个指针，等命令来了再原样发消息过来。所以这个实例必须活到进程结束。
	// 这里用 `allocInit`（+1）造出来就**故意不 release**：它是全局唯一的。
	commandTargetInstance = allocInit(c)
}

// commandTarget 返回那个活着的实例。
func commandTarget() id { return commandTargetInstance }

// setCommandHandler 换掉「命令来了之后叫谁」。
func setCommandHandler(fn func(Command, time.Duration)) {
	handlerMu.Lock()
	handlerFn = fn
	handlerMu.Unlock()
}

// currentCommandHandler 取当前那个处理器。
func currentCommandHandler() func(Command, time.Duration) {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	return handlerFn
}

// commandImpl 是 `-[BiuMediaCommandTarget handleRemoteCommand:]`。
//
// 返回值是 `MPRemoteCommandHandlerStatus`：0 是「收到了，接下了」。
// 这里**必须立刻返回**：它在系统那条线程上跑。在里面等一个要走网络的
// 活儿（比如跳到一个还没下到的位置）等于把控制中心按死。真正的活儿交给
// Session 的工作协程（见 media.go 的 dispatch）。
func commandImpl(_self id, _cmd objc.SEL, event id) int {
	cmd := originalCommand(event)
	if cmd == cmdUnknown {
		return statusNoActionableItem
	}

	at := time.Duration(0)
	if cmd == CommandSeek && event != 0 {
		// `positionTime` 是这个事件带的唯一一条数据，单位是秒。
		// 没有它，跳转就是「跳到一个不知道哪儿的地方」。
		at = seekDuration(sendDouble(event, "positionTime"))
	}

	fn := currentCommandHandler()
	if fn == nil {
		// 没人接（正在退出，或者业务层还没装上）。回「没有可操作的曲目」
		// 比回成功诚实：系统至少知道这个按钮此刻按了没用。
		return statusNoActionableItem
	}
	log.Printf("biu: 系统媒体会话收到远程命令 %s", cmd)
	fn(cmd, at)
	return statusSuccess
}

// originalCommand 问出这个事件是从哪个命令发来的。
//
// `MPRemoteCommandCenter` 的命令**都是单例**，事件上带的就是那一个对象，
// 所以比指针就是精确的认法——比拿类名去反查选择子可靠得多，也不依赖任何
// 名字（名字拼错的表现是「认不出来」，而认不出来只能报错，一按就没反应）。
func originalCommand(event id) Command {
	if event == 0 {
		return cmdUnknown
	}
	obj := send(event, "command")
	if obj == 0 {
		return cmdUnknown
	}
	return commandOf(obj, commandIfPresent)
}

// commandOf 认出「这个命令对象是我们表里的哪一条」。
//
// lookup 单独传进来，是因为 **`MPRemoteCommandEvent` 拒绝外部构造**
// （实测：`-[MPRemoteCommandEvent init]` 抛
// `NSGenericException: MPRemoteCommandEvents cannot be initialized externally.`），
// 也就是说真的事件对象**永远造不出来**，这段判断没法在一个自己写的用例里
// 走一遍。把「怎么认」和「去哪里拿」分开，前者就测得到了。
func commandOf(obj id, lookup func(string) id) Command {
	if obj == 0 {
		return cmdUnknown
	}
	for _, t := range commandTargets {
		if c := lookup(t.selector); c != 0 && c == obj {
			return t.cmd
		}
	}
	return cmdUnknown
}

// seekDuration 把 `positionTime`（单位秒）翻成时长，并把越界的夹回去。
//
// 负数是从系统那边传过来的坏值，而负的时长会让进度条停在一根不存在的位置
// 上，所以夹到 0。
func seekDuration(sec float64) time.Duration {
	if sec < 0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}
