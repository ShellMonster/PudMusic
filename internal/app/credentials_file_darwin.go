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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// legacyKeychainService 是**旧**后端（钥匙串）用的服务名。
	//
	// ⚠️ **它现在只用于一次性迁移**：读出旧的那份、搬进文件、然后删掉钥匙串里
	// 那一项。搬完之后这个服务名就不再有任何用了。
	//
	// ⚠️ **不要改它**：改了等于换了一项，钥匙串里旧的那份就再也读不回来，
	// 表现是「升级之后掉登录」。
	legacyKeychainService = "com.biu-go.biu-mygo.login"

	// legacyKeychainAccount 是旧那一项的账号名。
	legacyKeychainAccount = "primary"
)

// 权限位。
//
// ⚠️ 文件必须只有本人能读写（0600），目录必须只有本人能进（0700）。
// 这两个数写死在这里而不是散在各处，是为了让「读的时候检查、写的时候收紧」
// 用的是同一份定义。
const (
	credentialsFileMode = 0o600
	credentialsDirMode  = 0o700
)

// fileStore 是 macOS 上的凭据后端。
type fileStore struct {
	dir  string // 应用数据目录
	path string // 凭据文件的全路径

	// legacyService 是迁移时去钥匙串找的那一项的服务名。
	//
	// ⚠️ 它是个字段而不是常量，是为了**测试**：迁移要真的动钥匙串，
	// 而测试绝不能碰用户真数据（那一项里是用户真的登录态）。
	// 所以测试把它换成一次性服务名，测完删掉。
	legacyService string
}

// NewSystemStore 按平台建一个「系统保护存储」。
func NewSystemStore(dir string) (Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: 没有应用数据目录", ErrUnavailable)
	}
	return &fileStore{
		dir:           dir,
		path:          filepath.Join(dir, credentialsDirName, credentialsFileName),
		legacyService: legacyKeychainService,
	}, nil
}

func (f *fileStore) Name() string { return "macos-file" }

// Load 读出登录态。文件不存在就是 ErrNotLoggedIn。
func (f *fileStore) Load(ctx context.Context) (Session, error) {
	blob, err := f.readFile()
	if errors.Is(err, os.ErrNotExist) {
		// 文件还没有 → 可能是从旧版（钥匙串）升上来的，试着搬一次。
		//
		// ⚠️ **判据是「凭据目录在不在」，不是「文件在不在」**：目录在、文件不在
		// 是「登出过」（Clear 删了文件、目录留着），那**不该**再去钥匙串搬——
		// 钥匙串里那份早就被搬走并删掉了，搬也搬不到什么；
		// 而「目录都不在」才是「这个后端第一次上桌」。
		if _, dirErr := os.Stat(filepath.Dir(f.path)); errors.Is(dirErr, os.ErrNotExist) {
			if err := f.migrateFromKeychain(ctx); err != nil {
				// 搬不动不是错误：没登录就是没登录。
				return Session{}, ErrNotLoggedIn
			}
			blob, err = f.readFile()
			if errors.Is(err, os.ErrNotExist) {
				return Session{}, ErrNotLoggedIn
			}
		} else {
			return Session{}, ErrNotLoggedIn
		}
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

// readFile 读凭据文件，顺便**查权限**。
func (f *fileStore) readFile() ([]byte, error) {
	if err := checkPermissions(f.path); err != nil {
		return nil, err
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

// checkPermissions 查凭据文件的权限有没有被放宽。
//
// # 为什么要有这一道
//
// 权限位**不是保险栓**：它是「提醒」。一旦这个文件变成 0644（世界可读），
// **没有任何系统会报错**——它照常读、照常写，登录态照常能用，
// 只是别的用户也能看了。症状是静默的。
//
// 所以这里自己会喊：发现权限比 0600 宽，就**拒绝读**并说清楚怎么修。
//
// # 为什么是「拒绝读」而不是「读但警告」
//
// 因为「读但警告」在 GUI 应用里等于**没有警告**：窗口一闪而过，用户看不见；
// 而凭据已经躺在那儿被人读过了。宁可让用户登不上（他能按提示 chmod 回来），
// 也不要让它悄悄变宽。
func checkPermissions(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err // 文件不存在 → 原样交给调用方处理
	}
	if st.IsDir() {
		return fmt.Errorf("%w: %s 是个目录，不该是凭据文件", ErrBackend, path)
	}
	perm := st.Mode().Perm()
	if perm&^credentialsFileMode != 0 {
		return fmt.Errorf("%w: %s 的权限是 %04o，比应有的 %04o 宽"+
			"（别人能读了）；修：chmod 600 %q",
			ErrBackend, path, perm, credentialsFileMode, path)
	}
	return nil
}

// migrateFromKeychain 把旧版留在钥匙串里的登录态搬进文件，然后删掉钥匙串那一项。
//
// # 为什么用 `security` 命令行，而不是钥匙串 API
//
// 钥匙串 API 读内容要过**访问控制**那一关，而访问控制认的是**代码身份**——
// 本 app 是 ad-hoc 签名的，每一次构建都是「一个新程序」，于是每读一次
// 就要弹一次框要授权（用户看到的就是「每次打开都要钥匙串授权」）。
//
// 而 `/usr/bin/security` 是 **Apple 自己签的**，它在分区列表里有稳定的一档
// （`apple-tool:`），读我们那一项**不弹框**。
//
// ⚠️ 代价：多一个子进程依赖（`/usr/bin/security` 是 macOS 自带的，
// 但在受限环境里可能不在——那种情况下迁移失败，当没登录处理）。
//
// # 顺序：先写文件、写对了才删钥匙串
//
// 反过来（先删钥匙串再写文件）的话，中途断电就是**登录态两头都没了**：
// 钥匙串里那份已经被删了，文件里也没写成——用户只能重新扫码。
//
// # 删掉那一项是必须的
//
// 搬完之后钥匙串里还留着同一份 SESSDATA 的明文——**留着就等于没搬**。
// 而且它还在被「点一次始终允许就多一条 cdhash」的历史污染着。
func (f *fileStore) migrateFromKeychain(ctx context.Context) error {
	// ⚠️ 钥匙串锁着的时候，`security` 会弹框要密码——那个框会一直挂着。
	// 所以给它一个上限：搬不过去就当没登录（用户重新扫码），
	// 不能让整个应用卡在启动上。
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	content, err := f.readLegacyKeychainItem(ctx)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return errors.New("钥匙串里那一项是空的")
	}
	// ⚠️ **先验一下它真的是我们认得的格式**：直接写进去的话，
	// 一个解析不了的旧内容会变成「文件里有一份坏 JSON」，症状是「一直登不上」。
	var probe Session
	if err := json.Unmarshal(bytes.TrimSpace(content), &probe); err != nil {
		return fmt.Errorf("钥匙串里那份解不开: %w", err)
	}
	if !probe.LoggedIn() {
		return errors.New("钥匙串里那份不是登录态")
	}
	if err := f.Save(ctx, probe); err != nil {
		return err
	}
	// 文件已经写好并核对过了 → 现在删钥匙串那一项。
	if err := f.deleteLegacyKeychainItem(ctx); err != nil {
		// 删不掉不是致命的：文件里已经有了，用户是登录着的。
		// 但**必须说出来**——钥匙串里那份明文还在。
		return fmt.Errorf("文件已经搬好了，但钥匙串里那一项删不掉（%v）；"+
			"它里面还留着同一份登录态，手动删：security delete-generic-password -s %s",
			err, legacyKeychainService)
	}
	return nil
}

// readLegacyKeychainItem 读出钥匙串里旧的那一项（不打印内容）。
//
// ⚠️ **内容绝不能进日志**：它是 SESSDATA。这里只用子进程的标准输出，
// 捕获到内存里，不落盘、不打日志。
//
// ⚠️⚠️ **服务名必须来自实例字段，不能用包级常量。**
// 用常量的话，测试用的那个后端会去读**用户真数据**那一项——
// 而读完紧接着就是「把那一项删掉」，于是**测试把用户的登录态删了**。
// （这个坑本票真踩过一次：见收尾说明。）
func (f *fileStore) readLegacyKeychainItem(ctx context.Context) ([]byte, error) {
	security, err := exec.LookPath("security")
	if err != nil {
		return nil, fmt.Errorf("找不到 security 命令行工具: %w", err)
	}
	cmd := exec.CommandContext(ctx, security, "find-generic-password",
		"-s", f.legacyService, "-a", legacyKeychainAccount, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// 找不到是**正常状态**（没登录过 / 已经搬过了）。
		if ee := new(exec.ExitError); errors.As(err, &ee) && ee.ExitCode() == 44 {
			return nil, errors.New("钥匙串里没有旧的那一项")
		}
		return nil, fmt.Errorf("读钥匙串失败: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// deleteLegacyKeychainItem 删掉钥匙串里旧的那一项。
func (f *fileStore) deleteLegacyKeychainItem(ctx context.Context) error {
	security, err := exec.LookPath("security")
	if err != nil {
		return fmt.Errorf("找不到 security 命令行工具: %w", err)
	}
	cmd := exec.CommandContext(ctx, security, "delete-generic-password",
		"-s", f.legacyService, "-a", legacyKeychainAccount)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ee := new(exec.ExitError); errors.As(err, &ee) && ee.ExitCode() == 44 {
			return nil // 已经不在了
		}
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
