//go:build darwin

// macOS 的凭据后端：应用数据目录里的一个普通文件。
//
// # 为什么不用钥匙串了
//
// 这一版之前，登录态放在 macOS 钥匙串的一个通用密码项里
// （服务名 `com.biu-go.biu-mygo.login`）。用户 2026-10-25 报的现象是
// **「现在每次打开都需要钥匙串授权」**。
//
// 查清之后（BMG-261 / issue #473 的调研，加上这一票的复核），
// 根因不是「钥匙串反复盘问」，而是：
//
//   - 钥匙串认的是**代码身份**，而 ad-hoc 签名的 designated requirement
//     认死那**一个字节级二进制**（`cdhash H"…"`）；
//   - 每一项的访问判定最终落在**分区列表**上，而分区 id 只有三档是稳的：
//     `apple-tool:` / `apple:` / `teamid:`；
//   - `teamid:` 只给 Developer ID / Mac Development / Mac Distribution /
//     MAS / TestFlight（都要求 `anchor apple generic`），
//     **ad-hoc 和自签证书一律落到 `cdhash:` 那一档**——每重新构建一次换一次，
//     钥匙串就认不出这是谁，弹一次框要授权。
//
// 那一项的分区列表里现在堆着 **22 条历史 cdhash**，那就是 22 次
// 「重新构建 → 重新授权」的化石记录。
//
// 旧的钥匙串那一项**不迁移、不清理**：软件还没发版，没有别的用户的老数据
// （AGENTS.md §5）。本机上留着的旧条目，想删就手动
// `security delete-generic-password -s com.biu-go.biu-mygo.login`，重新扫码登录一次。
//
// # 代价（这一条是安全倒退，说在前面）
//
// 钥匙串那一层提供的保护是「**别的程序读不到**」。改成文件之后没有了：
// **同一台机器上、同一用户下的任何进程都能读这个文件**
// （权限位 600 拦的是「别的用户」，**不拦同一个用户的别的进程**）。
//
// 真正少掉的那一层因此是「钥匙串会在你授权的时候提醒你一下」，
// 而不是「别的程序偷不到」——后者在同一个用户下本来也拦不住。
//
// 另一处差别：钥匙串里的凭据**跟着账号走**（换机、重装系统后
// 登录同一个 Apple ID 就带着），文件不会——**换机要重新扫码登录**。
//
// # 为什么文件权限仍然要紧
//
// 600 + 目录 700 挡的是**别的用户**（共享机器上的另一���人）。
// 这一层的意义和钥匙串的「别的程序读不到」不同，但也不是零——
// 它挡的是「另一个用户账号下的程序」，以及「别人拷走你的磁盘镜像」。
//
// ⚠️ **权限被放宽了要能被发现**（见 checkPermissions）：权限位不是保险栓，
// 是**提醒**——被改成 644 之后没有任何东西会报错，所以这一层自己得会喊。
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	// credentialsDirName 是凭据文件所在**子目录**的名字。
	//
	// ⚠️ **为什么是子目录，而不是直接放在应用数据目录里**：
	// 应用数据目录（`~/Library/Application Support/Biu MyGo`）是**共享**的——
	// 设置、搜索历史、快捷键、歌词缓存、下载记录、界面状态都在里面。
	// 把那个目录整体 chmod 成 700 是**这一票范围外**的全局改动，
	// 动它就是替别的数据做决定。所以凭据**自己占一个 700 的子目录**，
	// 共享目录本身一个字节都不动。
	credentialsDirName = "credentials"

	// credentialsFileName 是凭据文件的名字。
	credentialsFileName = "session.json"
)

// 权限位。
//
// ⚠️ 文件必须只有本人能读写（0600），目录必须只有本人能进（0700）。
// 这两个数写死在这里而不是散在各处，是为了让「读的时候检查、写的时候收紧」
// 用的是同一份定义。
const (
	credentialsFileMode = 0o600
	credentialsDirMode  = 0o700

	// groupOrOtherBits 是「组和其他用户」那三个权限位。
	//
	// ⚠️ 判「权限被放宽」**只看这三个位**：owner 的执行位（0700）对凭据文件
	// 不算宽——组和其他用户一个字节都读不到。
	// （早先的判据是 `perm &^ 0600 != 0`，它把 0700 也算成「宽」，
	// 而文案写着「别人能读了」——那是假的。）
	groupOrOtherBits = 0o077
)

// fileStore 是 macOS 上的凭据后端。
type fileStore struct {
	dir  string // 应用数据目录
	path string // 凭据文件的全路径
}

// NewSystemStore 按平台建一个「系统保护存储」。
func NewSystemStore(dir string) (Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: 没有应用数据目录", ErrUnavailable)
	}
	return &fileStore{
		dir:  dir,
		path: filepath.Join(dir, credentialsDirName, credentialsFileName),
	}, nil
}

func (f *fileStore) Name() string { return "macos-file" }

// Load 读出登录态。文件不存在就是 ErrNotLoggedIn。
func (f *fileStore) Load(context.Context) (Session, error) {
	blob, err := f.readFile()
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, ErrNotLoggedIn
	}
	if err != nil {
		return Session{}, fmt.Errorf("%w: 读 %s: %v", ErrBackend, f.path, err)
	}
	var s Session
	if err := json.Unmarshal(blob, &s); err != nil {
		// 解不开就**按没登录处理**：解不开的凭据不能当没登录继续用，
		// 那会让后面每个请求都带着一份坏 cookie 去撞 -101，
		// 而用户看到的只是「一直登不上」。
		return Session{}, fmt.Errorf("%w: 登录态解不开: %v", ErrCorrupt, err)
	}
	if !s.LoggedIn() {
		return Session{}, ErrNotLoggedIn
	}
	return s, nil
}

// Save 覆盖保存。
func (f *fileStore) Save(_ context.Context, s Session) error {
	s.Normalize(time.Now())
	blob, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("app: 序列化登录态: %w", err)
	}
	if err := f.writeFile(blob); err != nil {
		return err
	}
	// 写完**读回来核对**：写进去了不等于写对了。
	// 磁盘满、断电、路径被换成符号链接——每一种都不会让 Write 报错。
	got, err := f.readFile()
	if err != nil {
		return fmt.Errorf("%w: 写进去之后读不回来: %v", ErrBackend, err)
	}
	if !bytes.Equal(got, blob) {
		return fmt.Errorf("%w: 写进去的内容跟写进去的不一样", ErrBackend)
	}
	return nil
}

// Clear 清掉这一份。幂等。
func (f *fileStore) Clear(context.Context) error {
	if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: 删 %s: %v", ErrBackend, f.path, err)
	}
	return nil
}

// readFile 读凭据文件，顺便**把被放宽的权限收回来**。
//
// # 为什么权限不对**不**算读失败
//
// 这一段的第一版是「权限比 0600 宽就返回 ErrBackend」。后果是一条**没人想到的**
// 删除链：
//
//	启动 → 读凭据 → 权限被判成「宽」→ Load 返回 ErrBackend
//	     → Account.restore 判定「后端坏了」→ 调 Logout
//	     → store.Clear → **凭据文件被删掉** → 用户的登录态没了
//
// 而界面上什么也不说（只有一行日志），README 还在教用户
// 「chmod 644 之后启动应用」来自验——照着做一次就丢一次登录态。
// （这条链被评审逮到过一次，见 issue #493 的收尾说明。）
//
// 现在的口径：**权限被放宽是「该修」，不是「凭据坏了」**。
// 文件里的内容还是好的，登录态还在——所以照常读出来，
// 同时把权限**收回 0600**（能收就收），并记一行日志说清发生过什么。
//
// 「被发现」的方式也因此变得无损：权限自己收回去了，用户 `ls -l` 一眼看得见，
// 不需要为了看一条告警而丢掉登录态。
//
// # 判据只看 group / other
//
// 0700（owner 可执行）对凭据文件不算「宽」——组和其他用户一个字节都读不到。
// 早先的判据 `perm &^ 0600 != 0` 会把它算成「宽」，那是错的：
// 报错文案说的是「别人能读了」，而 0700 别人根本读不到。
func (f *fileStore) readFile() ([]byte, error) {
	perm, err := checkPermissions(f.path)
	if err != nil {
		return nil, err
	}
	if perm&groupOrOtherBits != 0 {
		// 权限被放宽了 → 收回来 + 说出来。
		if err := os.Chmod(f.path, credentialsFileMode); err != nil {
			log.Printf("biu: 凭据文件 %s 的权限是 %04o，比应有的 %04o 宽"+
				"（别人能读了），收不回来：%v；请手动 chmod 600 %q",
				f.path, perm, credentialsFileMode, err, f.path)
		} else {
			log.Printf("biu: 凭据文件 %s 的权限被放宽成 %04o，已收回 %04o",
				f.path, perm, credentialsFileMode)
		}
	}
	return os.ReadFile(f.path)
}

// writeFile 原子地写凭据文件。
//
// # 为什么要「先写临时文件再改名」
//
// 直接往目标文件写，中途断电/磁盘满会留下**半截 JSON**——而半截 JSON
// 在下次启动时会被当成「坏数据」，用户被登出。改名是原子的：
// 要么是完整的旧文件，要么是完整的新文件。
func (f *fileStore) writeFile(blob []byte) error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, credentialsDirMode); err != nil {
		return fmt.Errorf("%w: 建 %s: %v", ErrBackend, dir, err)
	}
	// 目录可能已经存在而且权限被放宽了 → 收紧。
	if err := os.Chmod(dir, credentialsDirMode); err != nil {
		return fmt.Errorf("%w: 收紧 %s 的权限: %v", ErrBackend, dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: 在 %s 里建临时文件: %v", ErrBackend, dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 改名成功之后这一步是空操作

	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return fmt.Errorf("%w: 写 %s: %v", ErrBackend, tmpName, err)
	}
	// ⚠️ **权限要在写之前就定下来**：CreateTemp 建出来的是 0600，
	// 但显式 chmod 一次是「不依赖默认值」——万一哪天默认值变了，
	// 这里不会跟着变宽。
	if err := tmp.Chmod(credentialsFileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("%w: 收紧 %s 的权限: %v", ErrBackend, tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: 关闭 %s: %v", ErrBackend, tmpName, err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("%w: 改名成 %s: %v", ErrBackend, f.path, err)
	}
	return nil
}

// checkPermissions 查凭据文件的权限有没有被放宽（只看 group / other）。
//
// ⚠️ **权限位不是保险栓**：它是「提醒」。一旦这个文件变成 0644（世界可读），
// **没有任何系统会报错**——它照常读、照常写，登录态照常能用，
// 只是别的用户也能看了。症状是静默的。
//
// 所以这里自己会喊。⚠️ **但「喊」的方式是「收回去 + 记日志」，不是「拒绝读」**：
// 拒绝读会让上层判定「后端坏了」，而那条路的尽头是**把凭据删掉**
// （见 readFile 的注释）。所以：**读照常读，权限顺手收回。**
//
// 判据只看 group / other 那三个位：owner 的执行位（0700）不算「宽」，
// 因为组和其他用户一个字节都读不到。
func checkPermissions(path string) (os.FileMode, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err // 文件不存在 → 原样交给调用方处理
	}
	if st.IsDir() {
		return st.Mode().Perm(), fmt.Errorf("%w: %s 是个目录，不该是凭据文件", ErrBackend, path)
	}
	return st.Mode().Perm(), nil
}
