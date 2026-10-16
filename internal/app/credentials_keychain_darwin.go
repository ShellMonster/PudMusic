//go:build darwin

// macOS 的凭据后端：钥匙串里的通用密码项。
//
// # 选它不是因为「它是最现代的 API」
//
// 用的是 `SecKeychainFindGenericPassword` / `SecKeychainAddGenericPassword`
// 这组被标了 deprecated 的函数，而不是新的 `SecItemAdd` / `SecItemCopyMatching`。
// 理由是**它们吃 C 字符串**：`SecItem*` 要传 CFDictionary，从纯 Go 构造
// 一个得同时绑 CFStringCreateWithBytes、CFDictionaryCreateMutable、
// CFDictionarySetValue，外加一堆 CFRelease 的所有权处理——而那份代码
// 一次都跑不对都不会报错，只是悄悄泄漏或者崩在 GC 上。
//
// 这里那组函数全吃 `const char *`。macOS 至今仍然导出并支持它们，
// 钥匙串项的行为完全一致。本项目用 purego 在运行时取符号
// （跟 extaf/atb_darwin.go 同一套做法），编译器看不到 SDK 头文件，
// 所以也看不到 deprecation 警告。
//
// 换句话说：**用旧的这一组，换的是可读性和可测性，不是牺牲安全性**。
//
// # 为什么不是「加密一下存文件」
//
// 因为解密密钥也得存在某处。密钥放在应用自己的目录里，那份文件被拷走
// 就等于账号被拷走；放在钥匙串里，拷走文件没用——这就是「明文文件」
// 和「系统保护存储」的真实差别。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// 两个系统框架：钥匙串 API 和 CFRelease。
//
// 都随系统自带，不随应用分发——所以这里只有路径，没有打包的代价。
const (
	securityPath    = "/System/Library/Frameworks/Security.framework/Security"
	coreFoundation  = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	libcPath        = "/usr/lib/libSystem.B.dylib"
	securityDepNote = "这几个框架都是系统自带的，装应用时不用一起带。"
)

// OSStatus 是 Security.framework 的返回码。
type OSStatus = int32

// 本文件真正会遇到的返回码。
//
// errSecItemNotFound / errSecDuplicateItem 是**正常结果**：前者是
// 「还没登录过」，后者是「已经有一项了，改它」。当成错误的话，
// 第一次登录和重复登录会双双失败。
const (
	errSecSuccess       OSStatus = 0
	errSecItemNotFound  OSStatus = -25300
	errSecDuplicateItem OSStatus = -25299
	errSecUserCanceled  OSStatus = -128
)

// keychainService 是这一项在钥匙串里的「服务名」。
//
// ⚠️ 改了它等于**换了一项**：旧的那项还留在钥匙串里，用户登过的账号
// 就再也读不回来了（表现为「升级之后掉登录」）。测试里也**不要**用
// 同一个名字——那会往用户真正的钥匙串里写东西。
const (
	keychainService = "com.biu-go.biu-mygo.login"
	// keychainAccount 是「服务名下面那一条」的账号名。
	// 留了它是为了以后能放第二个账号（多账号切换）。
	keychainAccount = "primary"
)

// 绑好的入口。init 失败时 loadErr 非空，之后每次调用都直接返回它。
var (
	loadErr error

	// ⚠️ 出参必须声明成指针类型（`unsafe.Pointer` / `*T`），不能是
	// uintptr。purego 只有在参数类型是指针时才把它登记成「可写回的缓冲」，
	// 声明成 uintptr 的话地址照样传过去、C 那边往里写的东西回不来，
	// 结果是「函数返回成功、出参却是空的」。
	secKeychainFindGenericPassword func(
		keychain uintptr, // 0 = 默认搜索列表
		serviceLen uint32,
		service *byte,
		accountLen uint32,
		account *byte,
		passwordLen *uint32,
		passwordData *unsafe.Pointer,
		itemRef *unsafe.Pointer,
	) OSStatus

	secKeychainAddGenericPassword func(
		keychain uintptr,
		serviceLen uint32,
		service *byte,
		accountLen uint32,
		account *byte,
		passwordLen uint32,
		passwordData *byte,
		itemRef *unsafe.Pointer,
	) *OSStatus

	secKeychainItemModifyContent func(
		item unsafe.Pointer,
		attrList uintptr, // 0 = 只改数据，不改属性
		length uint32,
		data *byte,
	) OSStatus

	secKeychainItemDelete func(item unsafe.Pointer) OSStatus

	// cfRelease 归还 itemRef（SecKeychainItemRef 是个 +1 的
	// CoreFoundation 对象引用）。
	//
	// ⚠️ 老的 SecKeychainFreeContent 在当前 macOS 上**已经被移出**导出表
	// （实测 dlsym 找不到），Apple 现在让用 CFRelease 还。写死旧名字的
	// 后果是整个后端在 init 阶段就 loadErr，于是「钥匙串不可用」变成
	// 一个没人查得出来的静默降级。
	cfRelease func(cf unsafe.Pointer)

	// libcFree 归还 FindGenericPassword 给的那块**密码数据**。
	//
	// ⚠️ **它不是 CoreFoundation 对象**，是 malloc 出来的裸内存。
	// 对它调 CFRelease 会直接 SIGTRAP（实测：整进程带栈挂掉，
	// 栈顶就停在 CFRelease 那一帧）。
	//
	// 文档两处自相矛盾：老文档说用 SecKeychainFreeContent 释放，
	// 新文档说改用 CFRelease —— 但后者对这块数据不成立。
	// 所以这里按**它实际上是什么**来还：malloc 出来的就用 free。
	// itemRef 才是 CF 对象，走 CFRelease。**两个别弄反**，
	// 弄反了不是报错，是 SIGTRAP。
	libcFree func(p unsafe.Pointer)
)

func init() {
	if loadErr = bindSystemSymbols(); loadErr != nil {
		return
	}
}

func bindSystemSymbols() error {
	sec, err := purego.Dlopen(securityPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("app: opening Security.framework（%s）: %w", securityDepNote, err)
	}
	cf, err := purego.Dlopen(coreFoundation, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("app: opening CoreFoundation: %w", err)
	}
	for _, b := range []struct {
		name string
		fn   any
		sym  string
	}{
		{"SecKeychainFindGenericPassword", &secKeychainFindGenericPassword, "SecKeychainFindGenericPassword"},
		{"SecKeychainAddGenericPassword", &secKeychainAddGenericPassword, "SecKeychainAddGenericPassword"},
		{"SecKeychainItemModifyContent", &secKeychainItemModifyContent, "SecKeychainItemModifyContent"},
		{"SecKeychainItemDelete", &secKeychainItemDelete, "SecKeychainItemDelete"},
	} {
		addr, err := purego.Dlsym(sec, b.sym)
		if err != nil {
			return fmt.Errorf("app: %s: %w", b.sym, err)
		}
		purego.RegisterFunc(b.fn, addr)
	}
	addr, err := purego.Dlsym(cf, "CFRelease")
	if err != nil {
		return fmt.Errorf("app: CFRelease: %w", err)
	}
	purego.RegisterFunc(&cfRelease, addr)

	libc, err := purego.Dlopen(libcPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("app: opening libSystem: %w", err)
	}
	addr, err = purego.Dlsym(libc, "free")
	if err != nil {
		return fmt.Errorf("app: free: %w", err)
	}
	purego.RegisterFunc(&libcFree, addr)
	return nil
}

// keychainStore 是 macOS 上的凭据后端。
type keychainStore struct {
	service string
	account string
}

// NewSystemStore 按平台建一个「系统保护存储」。
//
// macOS 上不需要路径——钥匙串自己知道往哪儿写。给路径是为了 Windows
// 那边的 DPAPI（要落一个密文文件），接口统一而已。
func NewSystemStore(dir string) (Store, error) {
	if loadErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, loadErr)
	}
	return &keychainStore{service: keychainService, account: keychainAccount}, nil
}

func (k *keychainStore) Name() string { return "macos-keychain" }

// Load 读出登录态。没存过就是 ErrNotLoggedIn。
func (k *keychainStore) Load(context.Context) (Session, error) {
	blob, found, err := k.readPassword()
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, ErrNotLoggedIn
	}
	var s Session
	if err := json.Unmarshal(blob, &s); err != nil {
		// 解不开就**按没登录处理**（见 credentials.go 的 ErrCorrupt 语义）：
		// 解不开的凭据不能当没登录继续用，那会让后面每个请求都带着一份
		// 坏 cookie 去撞 -101，而用户看到的只是「一直登不上」。
		return Session{}, fmt.Errorf("%w: 钥匙串里的登录态解不开: %v", ErrCorrupt, err)
	}
	if !s.LoggedIn() {
		return Session{}, ErrNotLoggedIn
	}
	return s, nil
}

// Save 覆盖保存。没有就先加一项，有就改内容。
func (k *keychainStore) Save(_ context.Context, s Session) error {
	s.Normalize(time.Now())
	blob, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("app: 序列化登录态: %w", err)
	}

	item, found, err := k.findItem()
	if err != nil {
		return err
	}
	if found {
		defer cfRelease(item)
		if status := secKeychainItemModifyContent(item, 0, uint32(len(blob)), &blob[0]); status != errSecSuccess {
			return keychainError("改写", status)
		}
		return nil
	}
	return k.addAndVerify(blob)
}

// addAndVerify 新建这一项，然后**读回来核对**。
//
// # 为什么不看 SecKeychainAddGenericPassword 的返回值
//
// 它的返回类型是 `OSStatus *`（一个**指针**），而 purego 对指针类型的
// 返回值给的是寄存器里的原始数值——成功时那是 0，在 Go 里就是 nil。
//
// 于是写「if *status == errSecSuccess」的后果是：**每次成功的新建都被
// 当成失败**，表现为「第一次扫码登录存不进去，第二次（走改内容的路径）
// 又好了」。实测就是这个。
//
// 与其猜 purego 对指针返回怎么处理，不如**不依赖那个返回值**：写完直接
// 读回来比对。读回来的才是事实——「我以为写进去了」不算数。
func (k *keychainStore) addAndVerify(blob []byte) error {
	secKeychainAddGenericPassword(0,
		uint32(len(k.service)), cstr(k.service),
		uint32(len(k.account)), cstr(k.account),
		uint32(len(blob)), &blob[0], nil)

	got, found, err := k.readPassword()
	if err != nil {
		return err
	}
	switch {
	case !found:
		// 可能是并发：刚有人加上了同一项（errSecDuplicateItem）。
		// 退回去走改内容那条路。
		item, exists, err := k.findItem()
		if err == nil && exists {
			defer cfRelease(item)
			if status := secKeychainItemModifyContent(item, 0, uint32(len(blob)), &blob[0]); status != errSecSuccess {
				return keychainError("改写", status)
			}
			return nil
		}
		return fmt.Errorf("%w: 写进钥匙串之后读不回来", ErrBackend)
	case string(got) != string(blob):
		return fmt.Errorf("%w: 写进钥匙串的内容跟写进去的不一样", ErrBackend)
	}
	return nil
}

// Clear 清掉这一项。没有就返回 nil——退登按钮连点两次不能报错。
func (k *keychainStore) Clear(context.Context) error {
	item, found, err := k.findItem()
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	defer cfRelease(item)
	if status := secKeychainItemDelete(item); status != errSecSuccess {
		return keychainError("删除", status)
	}
	return nil
}

// readPassword 读出这一项的密码字节。找到过没有用一个 bool 返回。
//
// **找不到不是错误**——那意味着还没登录过，是正常起点。
func (k *keychainStore) readPassword() ([]byte, bool, error) {
	var (
		length uint32
		data   unsafe.Pointer
		item   unsafe.Pointer
	)
	status := secKeychainFindGenericPassword(0,
		uint32(len(k.service)), cstr(k.service),
		uint32(len(k.account)), cstr(k.account),
		&length, &data, &item)
	switch status {
	case errSecSuccess:
	case errSecItemNotFound:
		return nil, false, nil
	default:
		return nil, false, keychainError("读取", status)
	}

	// 上面那两个出参是 C 分配的 / +1 引用的，用完必须还。
	// 漏还不会立刻出问题——表现是钥匙串内存一条条地涨，
	// 而且这种泄漏在测试里完全看不见。
	defer func() {
		if data != nil {
			libcFree(data) // malloc 的裸内存，见 libcFree 的注释
		}
		if item != nil {
			cfRelease(item) // CF 对象
		}
	}()
	if data == nil || length == 0 {
		return nil, true, nil
	}
	// **必须复制**：上面那个 defer 会把这块内存还给 C，
	// 直接返回切片等于返回一个已经释放的指针。
	out := make([]byte, int(length))
	copy(out, unsafe.Slice((*byte)(data), int(length)))
	return out, true, nil
}

// findItem 找这一项的句柄。**调用者必须 cfRelease 它。**
func (k *keychainStore) findItem() (unsafe.Pointer, bool, error) {
	var (
		length uint32
		data   unsafe.Pointer
		item   unsafe.Pointer
	)
	status := secKeychainFindGenericPassword(0,
		uint32(len(k.service)), cstr(k.service),
		uint32(len(k.account)), cstr(k.account),
		&length, &data, &item)
	switch status {
	case errSecSuccess:
	case errSecItemNotFound:
		return nil, false, nil
	default:
		return nil, false, keychainError("查找", status)
	}
	if data != nil {
		libcFree(data)
	}
	if item == nil {
		return nil, false, nil
	}
	return item, true, nil
}

// cstr 拿一个 Go 字符串的首字节，不复制。
//
// ⚠️ 调用期间那个字符串必须一直活着。本文件传的都是包级常量，
// 永远是活的。**不要**拿临时拼出来的字符串调用它。
func cstr(s string) *byte {
	if s == "" {
		return nil
	}
	return unsafe.StringData(s)
}

// keychainError 把 OSStatus 说成人话。
func keychainError(what string, s OSStatus) error {
	switch s {
	case errSecItemNotFound:
		return fmt.Errorf("%w: 钥匙串%s时这一项不存在", ErrBackend, what)
	case errSecDuplicateItem:
		return fmt.Errorf("%w: 钥匙串%s时发现重复项", ErrBackend, what)
	case errSecUserCanceled:
		return fmt.Errorf("%w: 钥匙串%s被用户取消了", ErrBackend, what)
	}
	return fmt.Errorf("%w: 钥匙串%s失败，OSStatus %d (%#x)", ErrBackend, what, int32(s), uint32(s))
}
