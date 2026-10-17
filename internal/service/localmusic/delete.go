package localmusic

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 删除：真的把磁盘上的那个文件删掉，并且在删之前把「不该删的东西」拦下来。
//
// # 为什么这一层要单独一个文件
//
// 删文件是整个应用里**唯一**会永久毁掉用户数据的动作。Biu 上游那 12 行
// （`local-music.ts:82-94`）只做了两件事：`stat` 一下看是不是文件、`unlink`。
// 在 Electron 里那够用，因为路径是主进程从渲染进程拿的、渲染进程是应用自己；
// 这里是三条规矩，每一条都能单独写一个「拿掉就红」的测试。
//
// # 三条规矩，删之前全都要过
//
//	1. **路径必须落在用户配的某个根目录下面。**
//	   这一条挡的是「列表里的路径不是用户给的那个目录里的东西」：设置被改过、
//	   别的代码塞了个路径进来、或者一份旧状态还没刷新。界面上那一行的路径
//	   从哪来我们管不了，但**删之前重新问一次「它还在不在某个根下面」**。
//
//	2. **必须是普通文件，或者指向普通文件的符号链接。**
//	   目录、设备、管道、套接字一律拒绝。目录尤其要紧：`os.Remove` 对空目录
//	   是会成功的，对非空目录会报错——也就是说「删目录」在最好的情况下删掉
//	   一个空目录、最坏的情况下报一个用户看不懂的错。两条都不该发生。
//
//	3. **符号链接删的是链接本身，不是它指向的东西。**
//	   POSIX 的 `unlink` 就是这个语义，`os.Remove` 直接就是它。**不做**任何
//	   `EvalSymlinks`：一旦解析了就分不清「删这个链接」和「删那个文件」，
//   而后者就是删到用户音乐库外面去了——这正是包头那条安全线。
//
// # 为什么不多做一层「文件换了就别删」
//
// 第 4 条本来可以是「stat 出来的大小和列表里那一行对不上就拒绝」。这里不做：
// 它挡不住真攻击（检查和删除之间还有时间差），而它挡得住的是**误伤**——
// 用户在别的播放器里刚改过标签，再回来点删除却被拒绝，而界面上没有任何
// 一句话解释为什么。个人音乐目录不是多租户环境，一个诚实的 TOCTOU 窗口
// 换一次「按了没反应」的代价不划算。
//
// 每一条规矩的回归测试在 delete_test.go，测试断言的是**磁盘上还剩下什么**，
// 不是「有没有报错」。

// 删不掉时的几种原因，界面据此说不同的话。
var (
	// ErrOutsideRoots 是「这个路径不在你配的任何音乐目录下面」。
	ErrOutsideRoots = errors.New("不在任何一个音乐目录里，不删")
	// ErrNotRegular 是「它不是普通文件（目录、设备、套接字……）」。
	ErrNotRegular = errors.New("不是一个普通文件，不删")
	// ErrMissing 是「它已经不在了」。
	ErrMissing = errors.New("文件已经不在了")
	// ErrMoved 是「这个位置上的东西已经不是你列表里看到的那一个了」。
	ErrMoved = errors.New("这个位置上的文件已经换了，不删")
)

// Delete 把 tr 那一项从磁盘上删掉。
//
// ⚠️ **传的是整个 `Track`，不是光一条路径**：这一层要拿 `Track.Canonical`
// 做一道身份检查（见下面「规矩 4」），只有路径的话它无从判断「这个位置
// 上的东西还是不是扫���时那个」。
//
// roots 是用户当前配的音乐目录。传空的话什么都不会删——那不是「有权限删」，
// 是「用户没告诉我能删哪儿」。
func Delete(tr Track, roots []string) error {
	path := tr.Path
	// ⚠️ **不 TrimSpace**：文件名末尾带空格是完全合法的，而
	// `filepath.Clean(strings.TrimSpace("/Music /歌.mp3"))` 指向的是
	// `/Music/歌.mp3`——**另一个文件**。这一层是删除前的最后一道门，
	// 在这里换掉用户选的那一个是最坏的一种错。
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return fmt.Errorf("%w：路径是空的", ErrOutsideRoots)
	}
	// 规矩 1：必须在某个根下面。
	if !underAnyRoot(clean, roots) {
		return fmt.Errorf("%w：%s", ErrOutsideRoots, clean)
	}

	// 规矩 4：**这一行记的还是不是扫���时那个文件。**
	//
	// 前面那条「在不在某个根下面」挡不住这一种：把根目录 `library` 改名，
	// 再在原处建一个指向 `outside` 的符号链接，那么 `library/歌.mp3` 解析出来
	// 已经是 `outside/歌.mp3`——一个**从没被扫过、也不在音乐库里**的文件，
	// 而路径在词法上仍然规规矩矩地待在 `library` 底下。
	//
	// 比对 `EvalSymlinks` 现在的结果和 `Track.Canonical`（扫描那一刻记下的），
	// 两者不等就说明这个位置上换过东西了，拒删。
	//
	// ⚠️ 这里解析只是为了**比对**；真正删的仍然是词法上的 `clean`（规矩 3）。
	now, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return fmt.Errorf("%w：%s", ErrMissing, clean)
	}
	if tr.Canonical != "" && now != tr.Canonical {
		return fmt.Errorf("%w：%s 现在指向 %s", ErrMoved, clean, now)
	}

	// Lstat 而不是 Stat：我们要知道**这一条路径本身**是什么。
	// Stat 会跟着符号链接走，那样就分不出「这是个链接」和「这是个文件」了，
	// 而规矩 3 靠的正是这个区别。
	info, err := os.Lstat(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w：%s", ErrMissing, clean)
		}
		return err
	}

	// 规矩 2：类型对得上吗。
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// 链接本身可以删（规矩 3）。它指向的东西存不存在都不影响——
		// 断掉的链接在列表里本来就看不到，但用户仍可能想把它清掉。
	case info.Mode().IsRegular():
	default:
		return fmt.Errorf("%w：%s 是 %s", ErrNotRegular, clean, describeMode(info.Mode()))
	}

	// 规矩 3：`os.Remove` 就是 `unlink`，对符号链接删的是链接自己。
	// ⚠️ 这里**不能**先 EvalSymlinks 再删——那会把「删链接」变成「删目标」。
	return os.Remove(clean)
}

// underAnyRoot 报 path 是不是落在 roots 里某一个的下面。
//
// 比较用 `filepath.Rel` 而不是字符串前缀：`/music2/x.mp3` 以 `/music` 开头，
// 前缀比较会说它在下面，而它其实在隔壁目录。
//
// ⚠️ 拿 `Rel` 的结果判 `..` 时必须判**分隔符之前**：一个真叫 `..foo` 的文件
// 用 `strings.HasPrefix(rel, "..")` 会被错当成在外面。
func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		// ⚠️ **只跳空串，绝不 TrimSpace**（理由同 `uniqueRoots` 那一行）：
		// 把 `/Music ` 裁成 `/Music` 就等于把「用户选的那个文件夹」换成了
		// 隔壁那个，而这一层是**最后一道**删除前的门。
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = filepath.Clean(root)
		}
		rel, err := filepath.Rel(filepath.Clean(abs), path)
		if err != nil {
			// 一个盘上的根和另一个盘上的路径算不出相对路径，那就不在下面。
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

// describeMode 把一个文件模式说成人话，给「不是一个普通文件」那条错用。
//
// 界面上直接把这句摆给用户看，所以宁可说「一个文件夹」也不要丢一个 `dm---`
// 出去——没人看得懂那串，也就没法判断下一步该做什么。
func describeMode(m fs.FileMode) string {
	switch {
	case m.IsDir():
		return "一个文件夹"
	case m&fs.ModeSymlink != 0:
		return "一个符号链接"
	case m&fs.ModeDevice != 0:
		return "一个设备文件"
	case m&fs.ModeNamedPipe != 0:
		return "一个管道"
	case m&fs.ModeSocket != 0:
		return "一个套接字"
	case m&fs.ModeCharDevice != 0:
		return "一个字符设备"
	}
	return m.String()
}
