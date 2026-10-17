//go:build darwin

package media

// 最小的一层 Objective-C 胶水：消息发送、几个 Foundation 类型、一个自动
// 释放池。一个应用逻辑都不写。
//
// ⚠️ **注册必须全部发生在 init 里，也就是主 goroutine 上、事件循环起来之前。**
// 实测：有 ObjC 回调正在跑的时候再 `RegisterFunc` / `RegisterClass` 会直接
// SIGTRAP（`docs/research/GO-MEDIA-SESSION.md` §2.6）。所以本文件只做
// 「打开框架 + 取符号 + 登记类型」，一个 `media.New()` 之类的动作都不做。
//
// 这层抄的是 MyGo 自己那份（`mygo/internal/darwin/objc.go`，内部包导不进来）。
// 抄它的形状而不是自己发明：那一层是本项目唯一已经在真机上跑过几百个小时的
// purego ↔ ObjC 通路，它的每一条注意事项都是付过代价的。

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// id 是 Objective-C 对象指针。
type id = objc.ID

// nsSize 是 NSSize 的 Go 写法：两个 CGFloat，在 64 位上都是 float64。
//
// ⚠️ **只用在返回值上，不要用在参数上。** 参数那份是 `func(self, _cmd,
// w, h float64, completion)` —— 见 objc_darwin.go 里 msgRequestImage 顶上
// 那一段：纯 Go 的反射封送送不进去一个「两个 double 组成的结构」。
type nsSize struct{ W, H float64 }

// 要打开的框架。都是系统自带的，不随应用分发。
const (
	pathObjC        = "/usr/lib/libobjc.A.dylib"
	pathFoundation  = "/System/Library/Frameworks/Foundation.framework/Foundation"
	pathAppKit      = "/System/Library/Frameworks/AppKit.framework/AppKit"
	pathMediaPlayer = "/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer"
)

// NSString 的 UTF-8 编码号（Foundation 的 NSUTF8StringEncoding）。
const nsUTF8StringEncoding = 4

// 绑定好的入口。loadErr 非空时后面每个动作都直接返回它。
var (
	loadErr error

	// msgSend 是 objc_msgSend 的地址。
	//
	// ⚠️ **不要用 `objc.Send[T]` 那一支**：它每次调用都重新
	// `purego.RegisterFunc` 一次（`purego/objc` 的 objc_send.go），
	// 而注册必须在没有 ObjC 回调在跑的时候做——媒体会话的回调恰恰是
	// 一直有东西在跑。这里自己走 `SyscallN`，和 MyGo 一样。
	msgSend uintptr

	// 声明成具体签名的消息。带 double 的那些**必须**声明成 float64：
	// double 走的是浮点寄存器那一组，按整数传过去的话参数会被解释成
	// 一个恰好落在浮点寄存器里的垃圾位。
	msgDouble func(obj id, sel objc.SEL) float64
	// msgInitDouble 是「接收一个 double、返回一个对象」的那一类消息。
	// `newNumberDouble` 要用它——见那里为什么**不能**用 `+numberWithDouble:`。
	msgInitDouble func(obj id, sel objc.SEL, v float64) id
	// msgRequestImage 是 `-[MPMediaItemArtwork requestImageWithSize:completionHandler:]`
	// 的**入向**声明（返回值是 void，所以只有参数）。
	//
	// ⚠️ 那个 NSSize **声明成两个 double，不声明成结构**。纯 Go 侧的反射封送
	// 处理不了「两个 double 组成的结构」这种参数：在 arm64 上它属于同质浮点
	// 聚合，要走浮点寄存器，而封送那一层把它按整数寄存器递过去，于是
	// **值到不了**（实测：传 64×64，IMP 收到 0×0）。
	//
	// 拆成两个 double 在调用约定上是**完全等价**的：arm64 的同质浮点聚合走
	// d0/d1，x86-64 走 xmm0/xmm1，两种都是「两个 double 挨着」。系统的
	// `objc_msgSend` 按它自己那份类型编码传参，我们的实现按两个 double 收，
	// 拿到的是同一对寄存器。
	msgRequestImage func(obj id, sel objc.SEL, w, h float64, completion objc.Block)
	// msgNSSize 是返回 NSSize 的那种。返回值能封送（纯 Go 那边是照着
	// 结果缓冲写回去的），所以这里老老实实声明成结构。
	msgNSSize func(obj id, sel objc.SEL) nsSize
)

func init() {
	// 这一层只放「真正加载框架」这一步，注册在 ready 里做——
	// 靠文件名的字母序去保证 init 的先后是**靠不住的**：多一个文件就可能
	// 换一个顺序，而顺序错了的症状是「类取不到」，不报错。
	ensureReady()
}

// readyOnce 保证框架只加载一次。
var readyOnce sync.Once

// ensureReady 加载框架、取符号、登记消息签名。**每个入口都要先调它**，
// 它自己是幂等的。
//
// ⚠️ 调用它的地方**不能**已经有 ObjC 回调在跑（见顶上那段警告）。
// 现在真正的调用点是 `media.New()`，由 `ui.MountMedia` 在开窗口之前调。
func ensureReady() {
	readyOnce.Do(loadObjC)
}

// loadObjC 是真正干活的那一段。
func loadObjC() {
	h, err := purego.Dlopen(pathObjC, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		loadErr = fmt.Errorf("media: 打不开 libobjc: %w", err)
		return
	}
	msgSend, err = purego.Dlsym(h, "objc_msgSend")
	if err != nil {
		loadErr = fmt.Errorf("media: objc_msgSend: %w", err)
		return
	}
	// amd64 上返回大于 16 字节的结构要走另一个入口。这一层没有那种调用，
	// 但符号还是核一下——省得将来有人加了一个之后踩上一道极难查的坑。
	if runtime.GOARCH == "amd64" {
		if _, err := purego.Dlsym(h, "objc_msgSend_stret"); err != nil {
			loadErr = fmt.Errorf("media: objc_msgSend_stret: %w", err)
			return
		}
	}
	// MediaPlayer 里的两个类要用，所以先把它加载进来（类得先注册才能
	// GetClass 到）。AppKit 只为了 NSImage。
	for _, p := range []string{pathFoundation, pathAppKit, pathMediaPlayer} {
		if _, err := purego.Dlopen(p, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			loadErr = fmt.Errorf("media: 打不开 %s: %w", p, err)
			return
		}
	}

	purego.RegisterFunc(&msgDouble, msgSend)
	purego.RegisterFunc(&msgInitDouble, msgSend)
	purego.RegisterFunc(&msgRequestImage, msgSend)
	purego.RegisterFunc(&msgNSSize, msgSend)
}

// ---- 选择子与类：都要缓存 ----
//
// `objc.RegisterName` 和 `objc.GetClass` 每次都要拿 Objective-C 的全局锁。
// 消息发送是这条链路上最热的一处，不缓存就是每发一条消息抢一次锁。

var (
	nameMu     sync.RWMutex
	selCache   = map[string]objc.SEL{}
	classCache = map[string]objc.Class{}
)

// sel 返回名字对应的选择子，缓存。
func sel(name string) objc.SEL {
	nameMu.RLock()
	s, ok := selCache[name]
	nameMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	nameMu.Lock()
	selCache[name] = s
	nameMu.Unlock()
	return s
}

// class 返回名字对应的类，缓存。类不存在时返回 0（由调用方决定怎么办——
// 这里所有类都是系统自带的，取不到就是环境不对，不该让整个会话崩掉）。
func class(name string) objc.Class {
	nameMu.RLock()
	c, ok := classCache[name]
	nameMu.RUnlock()
	if ok {
		return c
	}
	c = objc.GetClass(name)
	nameMu.Lock()
	classCache[name] = c
	nameMu.Unlock()
	return c
}

// ---- 发消息 ----
//
// 三个 send* 都带 `//go:uintptrescapes`：用 `uintptr(unsafe.Pointer(&x))`
// 传进去的 Go 指针必须是**堆上**的，而且在调用期间活着——否则栈一增长就
// 搬家，Objective-C 那边拿到的是野指针。MyGo 的同一层有一模一样的注释，
// 它是踩过之后才写的。

//go:uintptrescapes
func send(obj id, selector string, args ...uintptr) id {
	return sendSel(obj, sel(selector), args...)
}

//go:uintptrescapes
func sendSel(obj id, s objc.SEL, args ...uintptr) id {
	var a [8]uintptr
	a[0], a[1] = uintptr(obj), uintptr(s)
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSend, a[:n+2]...)
	return id(r)
}

// sendBool 发一条返回 BOOL 的消息。
func sendBool(obj id, selector string, args ...uintptr) bool {
	return byte(send(obj, selector, args...)) != 0
}

// sendInt 发一条返回 NSInteger 的消息。
func sendInt(obj id, selector string, args ...uintptr) int {
	return int(send(obj, selector, args...))
}

// sendDouble 发一条返回 double 的消息。
func sendDouble(obj id, selector string) float64 {
	return msgDouble(obj, sel(selector))
}

// sendSize 发一条返回 NSSize 的消息。
func sendSize(obj id, selector string) nsSize {
	return msgNSSize(obj, sel(selector))
}

// ---- Foundation 类型 ----
//
// ⚠️ **这一层造出来的每一个对象都是自己持有的（+1），用完自己 release。**
//
// 一个更省事的做法是让它们 autorelease，靠 Objective-C 的自动释放池收。但那条路
// **收不了**：自动释放池是**每线程**的状态，而这一层的调用来自 Go 的任意 goroutine，
// 那些 goroutine 随时会换 OS 线程，于是「谁负责放它」这件事没人说得清——
// 实测那是必崩的（崩在纯 native 代码里，连一行 Go 栈都没有），见
// `objcthread_darwin.go` 顶上那一整段。
//
// 自己持有的代价是每个调用点都要配一次 release，而这一层每次推系统只造七八个对象，
// 那点代码换掉一整类说不清的崩溃，划算。**别改回 autorelease。**
//
// ⚠️ 而且**每一个都必须自己 alloc**：Objective-C 的那些「工厂方法」
// （`+numberWithDouble:`、`+dataWithBytes:`、`+dictionary`…）交回来的都是
// **autorelease 过的**，当 +1 用就是多放一次。本层现在用的形状，逐个是：
//
//	newString            [[NSString alloc] initWithBytes:length:encoding:]
//	newNumberDouble      [[NSNumber alloc] initWithDouble:]      ← 评审在这里翻过车
//	newData              [[NSData alloc] initWithBytes:length:]
//	newMutableDictionary [[NSMutableDictionary alloc] init]
//	nsImageFromBytes     [[NSImage alloc] initWithData:]
//
// 判据在 `TestEveryOwnedHelperSurvivesAnAutoreleasePoolPop`：在一个真池子里造、
// 放、pop，再**用一次**。多放过的对象会当场段错误。

// release 放掉一个自己持有的对象。
func release(o id) {
	if o != 0 {
		send(o, "release")
	}
}

// newString 造一个 **+1 的** NSString。
//
// ⚠️ 用 `initWithBytes:length:encoding:` 而不是 `stringWithUTF8String:`：后者要的是
// NUL 结尾的 C 字符串，而中文的 UTF-8 字节里可能就有 NUL，且按字节算长度在中文上
// 会截断。歌名全是中文——症状是锁屏上的歌名少一半。
func newString(s string) id {
	if s == "" {
		return 0
	}
	str := sendSel(send(id(class("NSString")), "alloc"), sel("initWithBytes:length:encoding:"),
		uintptr(unsafe.Pointer(unsafe.StringData(s))), uintptr(len(s)), nsUTF8StringEncoding)
	runtime.KeepAlive(s)
	return str
}

// goString 把一个 NSString 抄回 Go 字符串。objc 为 0 时是空串。
func goString(str id) string {
	if str == 0 {
		return ""
	}
	p := send(str, "UTF8String")
	if p == 0 {
		return ""
	}
	n := send(str, "lengthOfBytesUsingEncoding:", nsUTF8StringEncoding)
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
}

// newNumberDouble 造一个 **+1 的** NSNumber。
//
// ⚠️ **不能用 `+[NSNumber numberWithDouble:]`**，尽管它短得多。
// 那是个**工厂方法**：它内部 `[[NSNumber alloc] initWithDouble:]` 之后**又
// autorelease 了一次**，所以交回来的是「归自动释放池所有」的（实测
// retainCount = 1，而不是 alloc 出来的 -1）。
//
// 这一层按「自己持有、用完自己 release」的规矩用它，就成了**多放一次**：
// 现在这条线程上没有池子，autorelease 找不到主人，那份引用就一直在，
// 后面那一次 release 恰好抵消了它——**看着是好的**；一旦这条线程上有了池子
// （AppKit 内部推的、或者谁加回来的），pop 就会对着同一个对象再放一次，
// 当场段错误（评审的探针就是这么复现的，产物在
// `data/tasks/Biu_MyGo/26/review/nsnumber-ownership-and-pool.txt`）。
//
// 「恰好抵消」不是所有权，是运气。所以老老实实 alloc/init，那才是 +1。
func newNumberDouble(v float64) id {
	n := send(id(class("NSNumber")), "alloc")
	if n == 0 {
		return 0
	}
	return msgInitDouble(n, sel("initWithDouble:"), v)
}

// newData 造一个 **+1 的** NSData，拷走 b 的内容。
func newData(b []byte) id {
	var p unsafe.Pointer
	if len(b) > 0 {
		p = unsafe.Pointer(&b[0])
	}
	return sendSel(send(id(class("NSData")), "alloc"), sel("initWithBytes:length:"), uintptr(p), uintptr(len(b)))
}

// newMutableDictionary 造一个 **+1 的** NSMutableDictionary。
func newMutableDictionary() id {
	return sendSel(send(id(class("NSMutableDictionary")), "alloc"), sel("init"))
}

// dictPut 往字典里放一个值。value / key 为 0 都不放：
// Objective-C 的字典收 nil 会抛 `object cannot be nil`，而这一层宁可不放。
//
// key 自己建、自己放（`dictPut` 负责 release 掉它）——调用点只管给 value 的归属。
func dictPut(dict id, key string, value id) {
	if dict == 0 || value == 0 || key == "" {
		return
	}
	k := newString(key)
	if k == 0 {
		return
	}
	send(dict, "setObject:forKey:", uintptr(value), uintptr(k))
	release(k)
}

// allocInit 造一个 +1 的实例（`[[Cls alloc] init]`）。
func allocInit(cls objc.Class) id {
	if cls == 0 {
		return 0
	}
	return sendSel(send(id(cls), "alloc"), sel("init"))
}
