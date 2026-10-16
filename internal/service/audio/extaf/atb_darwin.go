//go:build darwin

package extaf

// AudioToolbox 的纯 Go 绑定，只做这一层的函数声明和常量，一个逻辑都不写。
//
// ⚠️ 注册必须全部发生在 init 里，也就是主 goroutine 上。实测：有 ObjC 回调正在
// 跑的时候再 RegisterFunc 会直接 SIGTRAP（`docs/research/verify-streaming.md`
// §9.4）。这一层是全项目唯一碰 purego 注册的地方，就是为了让这个约定只有一个
// 地方需要遵守。

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// 框架路径。AudioToolbox 在 macOS 上一直是系统自带的，不随应用分发。
const audioToolboxPath = "/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox"

// OSStatus 是 AudioToolbox 自己的错误码：0 是成功，负数是错误，
// 正数里有一批是「四个字符拼成的错误码」（0x7768743F = 'wht?'）。
type OSStatus = int32

const noErr OSStatus = 0

// AudioFile 的类型提示。传 unspecified 让 AudioToolbox 自己去认容器。
const audioFileUnspecifiedType = 0

// ExtAudioFile 的属性都是四个字符的常量，Go 源码里没有生成，只能按值写。
// 写错的代价是静默的：'flen' 是 AudioFile 层的同名属性，用它去问 ExtAudioFile
// 会报 -66561（kExtAudioFileError_InvalidProperty）。
//
// 写成十六进制是因为 Go 的 rune 字面量只收一个字符，'#frm' 写不出来。
// 读法是一样的：四个 ASCII 字符按大端拼成一个 uint32。
//
// ⚠️ 文件格式那个属性是 **'ffmt'** 不是 'fdat'——'fdat' 是老版本 SDK 里的写法，
// 现在拿它去问会得到 -66561（kExtAudioFileError_InvalidProperty）。
const (
	propFileDataFormat   uint32 = 0x66666D74 // 'ffmt'：这个文件本身是什么格式
	propClientDataFormat uint32 = 0x63666D74 // 'cfmt'：我要它解成什么格式
	propFileLengthFrames uint32 = 0x2366726D // '#frm'：一共多少帧，**SInt64**
)

// 线性 PCM 的格式 ID 与标志位。
const (
	audioFormatLinearPCM    uint32 = 0x6C70636D // 'lpcm'
	audioFormatFlagIsFloat  uint32 = 1 << 0     // 样本是 32 位浮点，不是定点整数
	audioFormatFlagIsPacked uint32 = 1 << 8     // 多声道交织在一起，而不是每个声道一个缓冲
)

// 绑定好的入口。init 失败时 loadErr 非空，之后每个 Open 都直接返回它。
var (
	loadErr error

	// ⚠️ 所有指针参数都声明成 unsafe.Pointer，**不能**用 uintptr。
	//
	// purego 只有在参数类型是指针（unsafe.Pointer / *T）时才把它登记成
	// 「写回缓冲」，C 那边往里写的东西才会回到 Go 变量里。声明成 uintptr 的话
	// 地址照样传过去了、C 也照样往里写，但 purego 不知道那是块可写的内存——
	// 结果就是**函数返回成功、出参却是 NULL**。实测踩过：
	// AudioFileOpenWithCallbacks 返回 0 而 file 拿到 0，
	// 下一步 ExtAudioFileWrapAudioFileID 拿 NULL 当 AudioFileID，报 -50。
	audioFileOpenWithCallbacks func(clientData, readFunc, writeFunc, getSizeFunc, setSizeFunc uintptr, typeHint uint32, outFile unsafe.Pointer) OSStatus
	audioFileClose             func(file uintptr) OSStatus

	extAudioFileWrapAudioFileID func(file uintptr, forWriting bool, out unsafe.Pointer) OSStatus
	// ⚠️ 这两个的**长度在指针前面**，和 AudioFileGetProperty / 几乎所有别的
	// AudioToolbox 函数相反。写反了会得到 0x2173697A（'!siz'），字面意思就是
	// 「这个 size 不对」——报错信息比函数签名诚实。
	extAudioFileSetProperty func(ef uintptr, prop uint32, size uint32, data unsafe.Pointer) OSStatus
	extAudioFileGetProperty func(ef uintptr, prop uint32, size *uint32, data unsafe.Pointer) OSStatus
	extAudioFileRead        func(ef uintptr, frames, data unsafe.Pointer) OSStatus
	extAudioFileSeek        func(ef uintptr, frame int64) OSStatus
	extAudioFileDispose     func(ef uintptr) OSStatus
)

func init() {
	h, err := purego.Dlopen(audioToolboxPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		loadErr = fmt.Errorf("extaf: opening AudioToolbox: %w", err)
		return
	}

	// 名字、目标函数、符号——三张表对不齐就一定会用错签名，而用错签名不报错，
	// 只是一直返回 -50，排查起来能绕一整天。
	for _, b := range []struct {
		name string
		fn   any
		sym  string
	}{
		{"AudioFileOpenWithCallbacks", &audioFileOpenWithCallbacks, "AudioFileOpenWithCallbacks"},
		{"AudioFileClose", &audioFileClose, "AudioFileClose"},
		{"ExtAudioFileWrapAudioFileID", &extAudioFileWrapAudioFileID, "ExtAudioFileWrapAudioFileID"},
		{"ExtAudioFileSetProperty", &extAudioFileSetProperty, "ExtAudioFileSetProperty"},
		{"ExtAudioFileGetProperty", &extAudioFileGetProperty, "ExtAudioFileGetProperty"},
		{"ExtAudioFileRead", &extAudioFileRead, "ExtAudioFileRead"},
		{"ExtAudioFileSeek", &extAudioFileSeek, "ExtAudioFileSeek"},
		{"ExtAudioFileDispose", &extAudioFileDispose, "ExtAudioFileDispose"},
	} {
		addr, err := purego.Dlsym(h, b.sym)
		if err != nil {
			loadErr = fmt.Errorf("extaf: %s: %w", b.sym, err)
			return
		}
		purego.RegisterFunc(b.fn, addr)
	}
}

// statusError 把一个非零的 OSStatus 说成人话。
//
// AudioToolbox 的错误码大半是四个可打印字符，把它们打出来比打十进制有用得多：
// 0x73696E63 一眼能看出是 'sync'（容器不对），0x7768743F 是 'wht?'（笼统失败）。
func statusError(what string, s OSStatus) error {
	if s == noErr {
		return nil
	}
	if v := uint32(s); v > 0x20 && v&0xff == 0 && v>>8&0xff != 0 {
		b := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		if isPrintable(b) {
			return fmt.Errorf("extaf: %s: %s (OSStatus %d, %#x)", what, b, s, uint32(s))
		}
	}
	return fmt.Errorf("extaf: %s: OSStatus %d (%#x)", what, s, uint32(s))
}

// isPrintable 报告 b 是不是四个能直接看的 ASCII 字符。
func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
