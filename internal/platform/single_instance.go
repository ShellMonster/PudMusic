// 单实例、URL scheme 与「把已有实例叫回来」。
//
// 框架已经给了整套机制（`mygo/single_instance.go` 是 unix socket 上的锁，
// `mygo/deeplink.go` 是 URL scheme 的登记与投递），所以这里**没有另造 IPC**：
// 自己开一条通道传消息要自己处理平台差异、权限和僵尸进程，框架那条路已经
// 把这些做完了，还顺手处理了「崩溃之后留下一个没人应答的 socket」。
//
// 这一层做两件事：把上面那套包成调用方不需要记调用顺序的接口，以及把
// `biu://…` 翻成应用自己的路由（翻路由这件事框架不管，只有我们懂「/settings」
// 在本应用里是哪一页）。
package platform

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// URLScheme 是本应用自己的 URL scheme。
//
// 它只干一件事：让第二个实例告诉第一个实例「该显示哪一页」。macOS 上点
// `biu://settings` 由系统直接转给已经跑着的那个应用（根本不会起第二个进程），
// Windows / Linux 上这条 URL 跟着命令行走，由单实例锁转交（见 Lock）。
//
// 取 `biu` 是图短、好手输，撞上别的应用的概率低。真撞上了系统会让用户选，
// 默认处理器由 RegisterScheme 决定。
const URLScheme = "biu"

// Lock 拿单实例锁。返回 true 表示「我是第一个实例，该继续跑」。
//
// **必须在 mygo.App.Run() 之前调**，晚一步就是竞态：Run 之后事件循环才起来，
// 那时候两个实例同时抢锁，谁抢到全看调度。框架的锁在抢不到时会把本实例的命令行
// 和工作目录转交给第一个实例，所以拿到 false 的那个只要自己退出就行——**不要留在
// 后台**当一个没有窗口的僵尸进程，那在用户的任务栏/活动监视器里就是一个怎么点都
// 没反应、只能强杀的应用。
//
// ## 两把锁的分工，以及为什么是这个顺序
//
// 框架那把锁（`mygo/single_instance.go`）管的是「第二个实例把 biu:// 递过来」。
// 但它**挡不住两个实例真正同时启动**：谁都还没建好 socket，两个进程 Dial 都失败；
// 接着两个都 `os.Remove(path)`——后到的把先到的刚建出来的 socket 文件删了；然后
// 两个 `net.Listen(path)` 都成功（路径已经空出来，inode 不同），两个都返回 true。
// 实测 20 轮里 17 轮两边都拿到。
//
// 更麻烦的是上面那个 `os.Remove`：它不只让两个都自称第一个，还会让**活下来的那个
// 失去 socket**——它的 socket 文件被另一个进程删了、那个进程又建了个新的然后退出，
// 于是之后每一个新实例都连不上它，deep link 再也递不进来（实测 40/40 轮）。
//
// 所以顺序是：**先内核锁（谁说了算），再框架锁（递命令行的通道）**，中间隔一个
// 「我准备好了」的信号：
//
//  1. 抢到内核锁的那个就是第一个。它先建 socket，再把 `ready` 写进锁文件。
//  2. 没抢到的**先等那个信号**，等到了才去调框架那把锁——这时 socket 一定在，
//     连得上、递得进去，自己退出。
//
// 「先等信号」这一条是整件事的关键：不等的话，没抢到内核锁的那个会趁第一个还没
// 建 socket 就把它的 socket 文件删掉、自己建一个，第一个紧接着 Listen 失败、
// 重试时连上了**它**，反过来退出——两个都没了。
func Lock() bool {
	deadline := time.Now().Add(lockReadyWait)
	for {
		// 一、谁说了算：内核锁。同时来的进程里内核只认一个。
		if lockExclusive() {
			// 建 socket（此刻没有别人跟我抢），再告诉后来者「可以递了」。
			first := mygo.App.RequestSingleInstanceLock()
			markLockReady()
			return first
		}

		// 二、别人占着：等它的 socket 建好。
		if lockIsReady() {
			// 连上去，把命令行（含 biu://）递过去，返回值必然是 false。
			mygo.App.RequestSingleInstanceLock()
			return false
		}

		// 三、它还没就绪（刚起来），或者它已经死了（内核锁被内核放回去了，
		// 下一轮就能抢到）。两种都再等一会儿重来。
		if time.Now().After(deadline) {
			// 兜底：等太久了，别让用户点图标没反应。直接问框架——有人在就递过去，
			// 没有人就当自己是第一个（这一刻内核锁不在我们手上，属于极窄的降级）。
			return mygo.App.RequestSingleInstanceLock()
		}
		time.Sleep(lockPollEvery)
	}
}

// 等「第一个实例就绪」的两个参数。
//
// 轮询要密：一个正常跑着的第一个实例，它那一行 `ready` 早就写好了，所以常见的
// 情况是**第一次读就读到、一次都不睡**。只有真的撞上同时启动才会等，而那一次
// 等的是另一个进程建 socket 的时间——微秒级。5 秒的上限只是防「等一个永远不会
// 就绪的实例」，用户的图标不该点下去之后毫无反应。
const (
	lockPollEvery = 15 * time.Millisecond
	lockReadyWait = 5 * time.Second
)

// lockFile 是 unix 那把 flock 锁住的文件句柄（见 single_instance_lock_unix.go）。
//
// 声明在共用文件里而不是 unix 那一份，是为了测试在三个平台上都编得过。Windows
// 那边永远不设它——那边的互斥是「锁文件存在」这件事，不需要留句柄。
var lockFile *os.File

// 锁文件的两行：主人的 pid，和「socket 已经建好」的信号。
//
// 内容长这样：
//
//	pid=12345
//	ready
//
// `ready` 那一行是 **socket 建好之后**才补上的，所以「文件里有 pid」只说明
// 「有人正在抢」，「有 ready」才说明「可以往他那里递东西了」。
const (
	lockFilePid   = "pid="
	lockFileReady = "ready"
)

// markLockReady 写下自己的 pid 和 ready：我是第一个，socket 已经建好了。
func markLockReady() { writeLockFile(true) }

// lockIsReady 报第一个实例是不是已经可以接收转交了。
func lockIsReady() bool { _, ready := readLockFile(); return ready }

// writeLockFile 写锁文件。ready=false 时只写 pid。
//
// 用「整个重写」而不是「追加」：ready 是一次性的事实，追加的话重试几次就会写出
// 好几行 ready，读的那边也没法分辨是哪一个进程写的。
func writeLockFile(ready bool) {
	body := lockFilePid + strconv.Itoa(os.Getpid()) + "\n"
	if ready {
		body += lockFileReady + "\n"
	}
	// 写坏了就写坏了：那只是让后来的实例多等一轮，超时之后照样会问框架。
	_ = os.WriteFile(lockPath(), []byte(body), 0o644)
}

// readLockFile 读锁文件里的 pid 和 ready。读不出来（文件还没建、内容被写了一半、
// 格式不认识）一律当成「什么都没有」——宁可多等一轮，不能把一次半截的写当成
// 「有人就绪」。
func readLockFile() (pid int, ready bool) {
	b, err := os.ReadFile(lockPath())
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == lockFileReady:
			ready = true
		case strings.HasPrefix(line, lockFilePid):
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, lockFilePid))
		}
	}
	if pid <= 0 {
		return 0, false
	}
	return pid, ready
}

// lockPath 是那把内核锁锁在哪个文件上。
//
// 放在应用数据目录而不是临时目录：那个目录本来就是「按用户分开」的
// （`mygo.App.Path(mygo.PathUserData)` → `os.UserConfigDir()` 拼出来的），
// 三个平台都按用户分，不用自己再拼 uid。
func lockPath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		return filepath.Join(dir, ".instance.lock")
	}
	// 拿不到应用数据目录（只读家目录之类）。用临时目录兜着，名字带上 uid：
	// Linux 的 /tmp 是所有用户共用的。Windows 上没有 uid（Getuid 恒为 -1），
	// 那边的临时目录本来就是每个用户一份。
	name := "biu-mygo.lock"
	if uid := os.Getuid(); uid >= 0 {
		name = "biu-mygo-" + strconv.Itoa(uid) + ".lock"
	}
	return filepath.Join(os.TempDir(), name)
}

// RegisterScheme 注册 URLScheme，让 `biu://…` 找得到这个应用。
//
// 必须在 mygo.App.Run() 之后、启动参数被交付之前调：界面层把它放在
// WhenReady 里，而 MyGo 的 handleReady 把那次交付排在所有 WhenReady 回调
// **之后**，所以启动命令行里的 `biu://` 一定认得出来。
//
// macOS 上只有打包成 bundle 并且在 `mygo.json` 的 `urlSchemes` 里声明了这个
// scheme 才注册得上，`go run` 和测试里必然报错——那是预期的，不是启动失败，
// 调用方记一条日志继续走就行。
func RegisterScheme() error { return mygo.App.RegisterURLScheme(URLScheme) }

// RouteFromURL 把一个 `biu://…` URL 翻成应用里的路由。
//
// 路由横跨 host 和 path：`biu://settings` → `/settings`，
// `biu://user/123` → `/user/123`，光秃秃一个 `biu://` → `/`。
//
// 不是本应用的 scheme 一律不认（返回 false）。这条不是安全边界——URL 是任何人
// 都能构造的，认识不认识都得由界面的路由表说话（见 `ui.Shell.navigate`），
// 这里只是别把别人的 URL 当成自己的路由。
func RouteFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, URLScheme) {
		return "", false
	}
	return "/" + strings.Trim(u.Host+u.Path, "/"), true
}
