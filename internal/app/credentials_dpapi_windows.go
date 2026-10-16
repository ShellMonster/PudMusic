//go:build windows

// Windows 的凭据后端：DPAPI 加密的文件。
//
// # 选型：DPAPI 而不是 Credential Manager
//
// Windows 上有两个系统保护存储：
//
//	Credential Manager（凭据管理器）—— 界面可见，用户能自己看和删，
//	                             每次读写弹一次 UI，UI 线程上绝对不能用；
//	DPAPI（CryptProtectData）——       完全静默，调用方自己管那份文件。
//
// 这里选 DPAPI：**它的调用不会弹窗**。Biu 的凭据读写发生在界面线程
// 背后的 goroutine 里，弹窗既没法点又会挡住界面。
//
// # 「加密文件」为什么仍然是系统保护存储
//
// 密钥**不在文件里，也不在应用目录里**——它在用户的凭据保管库里，
// 由 Windows 按登录账号保护：换用户打不开，从别的机器拷过来也打不开。
// 这跟「自己写个密钥存在旁边」有本质区别，后者等价于明文。
//
// 用户换密码 / 换账号之后 DPAPI 解不开，那时返回 ErrCorrupt，
// 表现是「需要重新登录一次」，而不是应用起不来。
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	crypt32          = syscall.NewLazyDLL("crypt32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtect = crypt32.NewProc("CryptProtectData")
	procCryptUnprot  = crypt32.NewProc("CryptUnprotectData")
	procLocalFree    = kernel32.NewProc("LocalFree")
)

// cryptoBlob 是 DPAPI 的 DATA_BLOB：一个长度 + 一块字节。
//
// 长度是 **uint32 而不是 int**：C 那边按 4 字节读，写成 int 会让高 4 字节
// 是垃圾，DPAPI 会拿它当长度去读，直接越界。
type cryptoBlob struct {
	cbData uint32
	pbData *byte
}

const (
	// cryptProtectUIForbidden = 0x1：不弹任何 UI。
	//
	// 不加这个标志的话，在某些环境下 CryptProtectData 会弹一个
	// 「是否允许访问」的对话框，而那时候调用在一个没人点的 goroutine 上。
	cryptProtectUIForbidden = 0x1

	// dpapiEntropy 是额外的一段「口 salts」。
	//
	// 它**不是**密钥——它只是让别的进程即使读到了这个文件、也调用
	// 同一个 DPAPI，解出来也是垃圾。真正的保护来自用户的凭据保管库。
	// 换句话说：DPAPI 挡住跨用户和跨机器，这个字符串挡同机的别的进程。
	dpapiEntropy = "Biu_MyGo.login.v1"
)

// dpapiFileName 是密文文件的名字。放在应用数据目录下。
const dpapiFileName = "credentials.dpapi"

// dpapiStore 是 Windows 上的凭据后端。
type dpapiStore struct {
	path string
}

// NewSystemStore 按平台建一个「系统保护存储」。
func NewSystemStore(dir string) (Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: 没有应用数据目录", ErrUnavailable)
	}
	return &dpapiStore{path: filepath.Join(dir, dpapiFileName)}, nil
}

func (d *dpapiStore) Name() string { return "windows-dpapi" }

// Load 读出登录态。文件不存在就是 ErrNotLoggedIn。
func (d *dpapiStore) Load(context.Context) (Session, error) {
	cipher, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNotLoggedIn
	}
	if err != nil {
		return Session{}, fmt.Errorf("%w: 读 %s: %v", ErrBackend, d.path, err)
	}
	plain, err := dpapiUnprotect(cipher)
	if err != nil {
		// 解不开的按「要重新登录」处理，而不是让应用起不来。
		// 换用户、换密码、换机器都会走到这里。
		return Session{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	var s Session
	if err := json.Unmarshal(plain, &s); err != nil {
		return Session{}, fmt.Errorf("%w: 登录态解不开: %v", ErrCorrupt, err)
	}
	if !s.LoggedIn() {
		return Session{}, ErrNotLoggedIn
	}
	return s, nil
}

// Save 加密后写文件。
func (d *dpapiStore) Save(_ context.Context, s Session) error {
	s.Normalize(time.Now())
	plain, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("app: 序列化登录态: %w", err)
	}
	cipher, err := dpapiProtect(plain)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBackend, err)
	}
	// 原子写：先写临时文件再 rename。
	// 直接写的话，写到一半被杀进程 = 文件截断 = 下次启动解不开 =
	// 用户莫名其妙掉登录。
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, cipher, 0o600); err != nil {
		return fmt.Errorf("%w: 写 %s: %v", ErrBackend, tmp, err)
	}
	if err := os.Rename(tmp, d.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: 落 %s: %v", ErrBackend, d.path, err)
	}
	return nil
}

// Clear 删掉文件。没有就返回 nil——退登连点两次不能报错。
func (d *dpapiStore) Clear(context.Context) error {
	if err := os.Remove(d.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: 删 %s: %v", ErrBackend, d.path, err)
	}
	return nil
}

// dpapiProtect 用 DPAPI 加密。
func dpapiProtect(plain []byte) ([]byte, error) {
	in := blobOf(plain)
	entropy := blobOf([]byte(dpapiEntropy))

	var out cryptoBlob
	r, _, callErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // 描述，给 0 就是没有
		uintptr(unsafe.Pointer(&entropy)),
		0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData 失败：%v", callErr)
	}
	defer localFree(out.pbData)
	return byteSliceOf(out), nil
}

// dpapiUnprotect 用 DPAPI 解密。
func dpapiUnprotect(cipher []byte) ([]byte, error) {
	in := blobOf(cipher)
	entropy := blobOf([]byte(dpapiEntropy))

	var out cryptoBlob
	r, _, callErr := procCryptUnprot.Call(
		uintptr(unsafe.Pointer(&in)),
		uintptr(unsafe.Pointer(&out)),
		0,
		uintptr(unsafe.Pointer(&entropy)),
		0,
		cryptProtectUIForbidden,
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 失败：%v", callErr)
	}
	defer localFree(out.pbData)
	return byteSliceOf(out), nil
}

func blobOf(b []byte) cryptoBlob {
	if len(b) == 0 {
		return cryptoBlob{}
	}
	return cryptoBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

// byteSliceOf 把 C 给回来的 blob 复制成 Go 切片。
//
// **必须复制**：DPAPI 分配的内存用完要 LocalFree，
// 直接返回切片等于返回一个已经释放的指针。
func byteSliceOf(b cryptoBlob) []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	out := make([]byte, int(b.cbData))
	copy(out, unsafe.Slice(b.pbData, int(b.cbData)))
	return out
}

// localFree 归还 DPAPI 分配的内存。
//
// 漏了它不会立刻出问题——表现是进程退出时那一块还没还，
// 而 Windows 会在进程结束时替我们做。但显式还掉是为了让「这段代码
// 没有泄漏」这件事能被 review 看出来。
func localFree(p *byte) {
	if p != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(p)))
	}
}
