//go:build darwin

package media

// 封面：把一个地址变成 `MPMediaItemArtwork`，交给系统去画在控制中心和锁屏上。
//
// # 为什么必须自己子类化
//
// `MPMediaItemArtwork` 的 `requestImageWithSize:completionHandler:` 是**抽象的**，
// 父类上直接调会抛异常。要用就只能造一个子类把它实现掉——Go 这一侧就是
// `objc.RegisterClass` 加一个 Go 函数当 IMP。
//
// # 为什么 IMP 里不用那个 size
//
// 参数收下、丢掉就行。但**签名必须对**：Objective-C 那边是按父类声明的
// 类型编码来传参的，我们声明成两个 float64（也就是 NSSize 的两个 CGFloat）
// 在 arm64 / x86-64 上拿到的是同一对浮点寄存器，读出来是同一个值。
// 写成别的形状就可能读到垃圾——所以只收不用，不是因为懒，是因为用错了
// 的代价是「锁屏上的封面偶尔变成一团马赛克」，而那没法查。

import (
	"log"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// artworkClassName 是我们注册的那个子类名。
//
// 起一个全局唯一的名字：Objective-C 的类名空间是**整个进程**的，重名的
// 类会让 `RegisterClass` 直接失败。
const artworkClassName = "BiuMediaItemArtwork"

// artworkSelector 是要实现的那个方法。
const artworkSelector = "requestImageWithSize:completionHandler:"

// artwork 注册结果。注册失败时 artworkClass 是 0，整张封面这条路整个走不通
// ——但那不该让 Now Playing 的其他字段（标题、进度）跟着没有，所以它是
// 「封面没有」而不是「会话崩了」。
var (
	artworkClass objc.Class
)

// initArtworkClass 注册那个子类。**只能在 init 里调**（见 objc_darwin.go 顶上）。
func initArtworkClass() {
	super := class("MPMediaItemArtwork")
	if super == 0 {
		log.Print("biu: 系统里没有 MPMediaItemArtwork，锁屏上没有封面")
		return
	}
	// RegisterClass 内部就是把 Go 函数变成 IMP（它自己 recover 了并把错误
	// 返回来），所以这里不需要另外拿一次 IMP——多拿一次只是白占一个回调槽。
	c, err := objc.RegisterClass(artworkClassName, super, nil, nil,
		[]objc.MethodDef{{Cmd: sel(artworkSelector), Fn: artworkImpl}})
	if err != nil {
		log.Printf("biu: 封面子类注册不上，锁屏上没有封面：%v", err)
		return
	}
	artworkClass = c
}

// images 是「哪个封面对象对应哪张图」。
//
// ⚠️ 这个映射是必需的，不是偷懒：`requestImageWithSize:completionHandler:`
// 只把 `self` 递进来，图得自己认。Go 侧的键是 Objective-C 对象的地址，
// 而 ObjC 那边不会因为 Go 不引用它就不释放——所以这里存着的每个
// `MPMediaItemArtwork` 都**故意不释放**（创建时的 +1 不还），
// 数量是「整个进程里换过的歌数」，一张封面几十 KB。
//
// ⚠️ **换歌之后也不释放这些对象**，这是刻意的。控制中心可能在我们换歌之后
// 才来要图（它按自己的尺寸向 MPMediaItemArtwork 要，不是我们推的时候顺便要），
// 而我们不知道那一刻是什么时候。一旦在这里 release 了它，那条迟到的消息就是
// 发到一个已释放的对象上——后果是随机的崩溃，而且和「刚切了一首歌」毫无关系，
// 现场什么都留不下。
//
// 代价是每个换过的封面留一份（一张网络封面几十 KB 到几 MB）。同一首的图只下
// 一次、只造一个对象，所以这个增长是「换过几首」而不是「放了几分钟」。
var (
	imageMu sync.RWMutex
	images  = map[objc.ID]id{}
)

// rememberImage 记下「这个封面对象是这张图」。
func rememberImage(art objc.ID, img id) {
	imageMu.Lock()
	images[art] = img
	imageMu.Unlock()
}

// forgetImage 忘掉一个封面对象。
func forgetImage(art objc.ID) {
	imageMu.Lock()
	delete(images, art)
	imageMu.Unlock()
}

// imageOf 认出这张封面对象对应的是哪张图。认不出是 0。
func imageOf(art objc.ID) id {
	imageMu.RLock()
	defer imageMu.RUnlock()
	return images[art]
}

// artworkImpl 是 `-[BiuMediaItemArtwork requestImageWithSize:completionHandler:]`。
//
// 同步给图：数据早就下好了（下载在别处，见 mp_darwin.go），系统问的时候
// 直接把 completion 叫掉。completion 的第二个参数（NSError）给 0。
func artworkImpl(self id, _cmd objc.SEL, w, h float64, completion objc.Block) {
	noteArtworkRequest(w, h)
	// ⚠️ 这里**不能**用 `objc.Block.Invoke`：它是给 purego 自己造的 block
	// 用的（它要在一个按 block 地址索引的表里找实现）。系统传进来的这个
	// 是系统造的，表里没有 → 调用它会在反射那一层 panic，而那是在 ObjC 的
	// 回调里，panic 的结局是整个进程死掉。自己按 block 的布局调。
	invokeBlock(completion, uintptr(imageOf(self)), 0)
}

// artworkSizeSeen 记下系统最近一次问的尺寸。
//
// 它存在的唯一理由是**可测**：IMP 的参数能不能真的穿过 Objective-C 那道
// 边界，从函数外面看不出来——IMP 不能往测试里写东西，而回调拿到的只有
// completion 那两个参数。记一份出来，测试才断得到「两个 double 有没有真的
// 到了」，而不是只断到「没崩」。
//
// 它也顺带把一件事钉住：那两个参数是**按两个 double** 声明的（见 objc_darwin.go
// 里 msgRequestImage 顶上那段），而系统传的是 `NSSize` 那个结构。两者在
// arm64 / x86-64 的调用约定上是同一对浮点寄存器，所以值能对上；哪天升级了
// 纯 Go 那一层，这里会第一个红。
var artworkSizeSeen = struct {
	mu sync.Mutex
	w  float64
	h  float64
}{}

// noteArtworkRequest 记一笔。
func noteArtworkRequest(w, h float64) {
	artworkSizeSeen.mu.Lock()
	artworkSizeSeen.w, artworkSizeSeen.h = w, h
	artworkSizeSeen.mu.Unlock()
}

// requestedArtworkSize 读回最近一次记下的尺寸。
func requestedArtworkSize() (float64, float64) {
	artworkSizeSeen.mu.Lock()
	defer artworkSizeSeen.mu.Unlock()
	return artworkSizeSeen.w, artworkSizeSeen.h
}

// invokeBlock 调用一个 Objective-C 那边造出来的 block。
//
// block 的头三个字是 isa / flags / invoke（见 clang 的 Block ABI），
// invoke 那个就是它的函数本体，参数是 (block, ...)。
func invokeBlock(b objc.Block, args ...uintptr) {
	if b == 0 {
		return
	}
	lit := *(**[3]uintptr)(unsafe.Pointer(&b))
	var a [8]uintptr
	a[0] = uintptr(b)
	n := copy(a[1:], args)
	purego.SyscallN(lit[2], a[:n+1]...)
}

// newArtwork 为一张已经在手上的 NSImage 造一个 MPMediaItemArtwork。
// 造不出来时返回 0（调用方只是没有封面而已）。
//
// ⚠️ **必须走 `initWithImage:`，不能走 `[[Cls alloc] init]`。**
// `MPMediaItemArtwork` 的 `init` 是指定初始化器，而它要求「必须有一张图」，
// 没有就抛 `NSInvalidArgumentException: image must be non-nil`——
// 那是个 ObjC 异常，穿透 Go 直接把进程带走（实测）。
//
// 父类**没有**实现 `requestImageWithSize:completionHandler:`（发过去是
// `unrecognized selector`），所以那个方法只能由我们注册的那个子类提供；
// 但初始化器可以继承父类的，这就是这里的分工。
func newArtwork(img id) id {
	if artworkClass == 0 || img == 0 {
		return 0
	}
	art := sendSel(send(id(artworkClass), "alloc"), sel("initWithImage:"), uintptr(img))
	if art == 0 {
		return 0
	}
	// 记下来：`requestImageWithSize:completionHandler:` 只把 self 递进来，
	// 图得自己认。父类存了图，但那个字段是私有的，从外面读不到。
	rememberImage(art, img)
	return art
}

// nsImageFromBytes 把一段图片字节变成 **+1 的** NSImage。解不开时返回 0。
//
// `NSImage` 自己认格式（走 ImageIO），所以这一层不需要知道拿到的是 PNG
// 还是 JPEG——B 站的封面两种都发过。
func nsImageFromBytes(b []byte) id {
	if len(b) == 0 {
		return 0
	}
	data := newData(b)
	if data == 0 {
		return 0
	}
	defer release(data)
	return sendSel(send(id(class("NSImage")), "alloc"), sel("initWithData:"), uintptr(data))
}

// newArtworkFromBytes 从字节造一个封面对象，中间那两个对象当场释放。
//
// 造出来的 `MPMediaItemArtwork` 是**故意不释放**的（理由见 images 那段），
// 而它内部 hold 住了这张 NSImage，所以把这张图我们自己那份放掉是安全的。
func newArtworkFromBytes(b []byte) id {
	img := nsImageFromBytes(b)
	if img == 0 {
		return 0
	}
	defer release(img)
	return newArtwork(img)
}
