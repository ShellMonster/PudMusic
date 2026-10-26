// Package localmusictest 造「系统回收站」那一份的假零件。
//
// # 为什么单独一个包
//
// 「移到废纸篓」那一层（`service/localmusic` 的 `Trash` 接口）有三层要测：
// 服务层、应用层、界面层。而**同一段造数据的代码抄两份，迟早会抄歪**——
// 抄歪了的那一份会让某一层的测试验不到它该验的东西，而**没有一层会红**
// （这正是 `internal/service/mux/muxtest` 存在的同一个理由）。
//
// # 为什么它**不** import `localmusic`
//
// `localmusic` 自己的测试文件在**那个包里面**（`package localmusic`），而
// 一个 import `localmusic` 的包被它 import 就是**循环依赖**。所以这一份
// **不认识** `localmusic`：它靠结构化类型满足那个接口（Go 的接口是结构化
// 的），而它返回的是**系统的原话**——由 `localmusic.ClassifyTrashError`
// 归类。那正好也是生产路径上发生的事：实现层给的就是系统原话。
package localmusictest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Fake 是「系统回收站」的假零件。
//
// 零值就是**成功**那一种：它把文件**搬进一个假废纸篓目录**，并记下搬的是
// 哪一条路径。
//
// ⚠️ **它搬的是「真的搬」**（`os.Rename`），不是删：搬走之后原处自然就没有
// 它了，而文件在假废纸篓里还在——所以测试能验「用户找得回来」，也能验
// 「**没有** `os.Remove` 那种一个字节都不剩的下场」。
//
// ⚠️ **失败时它一律不碰文件**（契约：失败必须把文件留在原处）。
type Fake struct {
	mu sync.Mutex
	// calls 是被搬过的路径，按顺序。
	calls []string
	// err 是每次都返回的那个错。nil = 成功。
	err error
	// dir 是假废纸篓放在哪儿。空串 = 只记调用、不搬东西（那一种用来测
	// 「服务层有没有把**词法上的那一条路径**交给回收站」）。
	dir string
}

// Success 造一个成功的假零件：它把文件搬进 dir。
//
// dir 空的时候它只记调用、不搬东西——那一种用来验「交给回收站的是哪条
// 路径」，而验「文件还在不在」的那些用例要给一个真的目录。
func Success(dir string) *Fake { return &Fake{dir: dir} }

// Fail 造一个每次都失败的假零件，err 是它给出的原话。
//
// ⚠️ **失败时它不碰文件**（契约：失败必须把文件留在原处）。
func Fail(err error) *Fake { return &Fake{err: err} }

// NotTrashable 造一个「那个位置没有回收站」的假零件。
//
// 它给的是 Windows 那边框架的**原话**（`mygo/internal/windows/trash.go`：
// 「it would be deleted permanently」）——那正是「进不了回收站、系统本来要
// 永久删掉」时用户会看到的东西。
func NotTrashable() *Fake {
	return Fail(errors.New(
		"mygo: cannot move it to the recycle bin: it would be deleted permanently"))
}

// NameConflict 造一个「废纸篓里已经有同名的东西」的假零件。
func NameConflict() *Fake {
	return Fail(errors.New("The file “歌.mp3” already exists."))
}

// TrashItem 搬走 path（或者按它被配好的那样失败）。
func (f *Fake) TrashItem(path string) error {
	f.mu.Lock()
	err := f.err
	f.calls = append(f.calls, path)
	dir := f.dir
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if dir == "" {
		return nil
	}
	// 模拟系统把文件搬进废纸篓。
	//
	// ⚠️ **搬过去而不是删掉**：搬走之后原处自然就没有它了，而它在废纸篓
	// 里还在——这正是「用户找得回来」的那件事。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	info, statErr := os.Lstat(path)
	if statErr != nil {
		// 文件已经不在了：系统那边会报错，而这一层把它归成
		// 「已经不在了」（`ClassifyTrashError` 认「no such file」那几句）。
		return fmt.Errorf("mygo: cannot move %s to the trash: no such file", path)
	}
	target := uniqueIn(dir, filepath.Base(path))
	if err := os.Rename(path, target); err == nil {
		return nil
	}
	// ⚠️ **跨设备搬不动**（临时目录和假废纸篓不在同一个卷上——真实系统里
	// 那就是「网盘 / 外接盘」那一档）：退回「复制 + 删原件」。
	//
	// ⚠️ **这里的 `os.Remove` 是这一份假零件自己的事**，不是生产代码里
	// 那种「删掉用户的文件」：搬进废纸篓本来就是「它从原处没了」。
	return moveAcrossDevices(path, target, info)
}

// moveAcrossDevices 把一个文件搬到另一个卷上的目标路径。
//
// ⚠️ **符号链接搬的是链接自己**（`os.Readlink` + `os.Symlink`）——那正是
// 生产路径上那条安全线（移链接、不移它指向的东西）在这一层的对应。
func moveAcrossDevices(path, target string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		dst, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if err := os.Symlink(dst, target); err != nil {
			return err
		}
		return os.Remove(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Remove(path)
}

// uniqueIn 在 dir 里给 base 找一个还没被占用的名字。
//
// 同名的那一份是**真的会被系统改掉**的（macOS 改成「歌 2.mp3」，Windows 也会
// 自己加后缀）——所以这一份照着做，而**它给的那句「重名」错误**由
// `NameConflict` 那一份单独造（真实系统里那一句很少出现）。
func uniqueIn(dir, base string) string {
	name := base
	for i := 2; ; i++ {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err != nil {
			return p
		}
		ext := filepath.Ext(base)
		stem := base[:len(base)-len(ext)]
		name = fmt.Sprintf("%s %d%s", stem, i, ext)
	}
}

// Calls 返回被搬过的那些路径（按顺序的拷贝）。
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// CallCount 返回它被调了几次。
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}
