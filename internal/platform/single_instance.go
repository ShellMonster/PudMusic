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
	"log"
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
			// 兜底：等太久了，别让用户点图标没反应。**但绝不能在这里把自己说成
			// 「第一个」**。
			//
			// 🔴 走到这一支的前提是**内核锁在别人手上**（`lockExclusive` 为 false
			// 才会到这儿），而那个「别人」多半只是还没建好 socket。这时候再问框架
			// 一次，它同样答不上来，于是两个进程都以为自己是第一个——就是
			// `TestLockRacesTwoFreshProcesses` 抓的那件事：机器一满载，第一个实例从
			// flock 到建好 socket 超过 `lockReadyWait`，第二个等不到就抢答，两个应用
			// 一起起来。
			//
			// 「没等到就是没等到」的另一面是要说清楚：**内核锁是这整件事唯一说了算
			// 的**。它说有人在，那就是有人（`lockExclusive` 返回 false 只可能是
			// flock 被人拿着）。那个进程活着，它会把命令行接过去；它要是死了，内核锁
			// 也放回去了，下一轮就能抢到。
			//
			// 那句调用还留着：能递到就把命令行（含 biu://）递过去，递不到也只是
			// 白问一次——返回的是不是「第一个」在这里已经不重要了。
			mygo.App.RequestSingleInstanceLock()
			log.Printf("biu: 等了 %v 没等到第一个实例就绪，按「有别人在」处理（内核锁在别人手上）", lockReadyWait)
			return false
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

// lockFile 是那把内核锁锁住的文件句柄（unix 是 flock，Windows 是 LockFileEx）。
//
// 声明在共用文件里而不是某个平台的那一份，是为了测试在三个平台上都编得过。
var lockFile *os.File

// 🔴 **锁和 ready 信号是两个文件，不是同一个。**
//
// 这一条是被 Windows 教会的：`LockFileEx` 锁住的是一段**字节**，而持锁的进程通过
// **另一个句柄**也读不到、写不进那一段（微软文档里写明：锁住之后，locking process
// 也不能用第二个 handle 去访问被锁的区域）。所以一旦把两样东西放进同一个文件——
// 锁第 0 个字节、ready 信号也写在第 0 个字节——`writeLockFile` 的 `os.WriteFile`
// 就会失败，而那个错误是 `_ =` 吞掉的：
//
//	第一个实例写不进 ready → 后来者 `lockIsReady()` 永远读到 false
//	→ 每次第二实例、每次 deep link 都白等 lockReadyWait（5 秒）才走兜底
//	→ 而且**表面上看不出任何错**，只是「唤起这个应用要等五秒」。
//
// 分成两个文件之后，锁那份的内容永远没人读，ready 那份永远没人锁。
func lockPath() string  { return lockStatePath(".instance.lock") }
func readyPath() string { return lockStatePath(".instance.ready") }

// lockStatePath 把一个状态文件名放到「这个用户的、这个应用的」目录下。
//
// 放应用数据目录而不是临时目录：那个目录本来就是按用户分开的
// （`mygo.App.Path(mygo.PathUserData)` → `os.UserConfigDir()` 拼出来的），
// 三个平台都按用户分，不用自己再拼 uid。
func lockStatePath(name string) string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		return filepath.Join(dir, name)
	}
	// 拿不到应用数据目录（只读家目录之类）。用临时目录兜着，名字带上 uid：
	// Linux 的 /tmp 是所有用户共用的。Windows 上没有 uid（Getuid 恒为 -1），
	// 那边的临时目录本来就是每个用户一份。
	if uid := os.Getuid(); uid >= 0 {
		name = "biu-mygo-" + strconv.Itoa(uid) + "." + name
	} else {
		name = "biu-mygo." + name
	}
	return filepath.Join(os.TempDir(), name)
}

// ready 文件的内容。两行：主人的 pid，和「socket 已经建好」的信号。
//
//	pid=12345
//	ready
//
// `ready` 那一行是 **socket 建好之后**才补上的，所以「文件里有 pid」只说明
// 「有人正在抢」，「有 ready」才说明「可以往他那里递东西了」。
const (
	readyFilePid   = "pid="
	readyFileReady = "ready"
)

// markLockReady 写下自己的 pid 和 ready：我是第一个，socket 已经建好了。
func markLockReady() { writeReadyFile(true) }

// clearLockReady 在**拿到内核锁之后**把上一次留下的 ready 抹掉。
//
// 为什么要抹：ready 文件是上一次运行留下来的话，后来者会以为「第一个实例已经就绪」，
// 于是不等就去连——而那时第一个实例可能正在建 socket，连不上，它就把 socket 抢过来；
// 紧接着第一个实例 Listen 失败、重试时连上了**它**，反过来退出。两个都没了。
//
// 时机也很要紧：必须在内核锁**拿到之后、框架那把锁之前**。这中间没有别人能拿到内核
// 锁，所以不会有另一个进程看到「锁被拿了但 ready 还没抹」而误判。
func clearLockReady() { writeReadyFile(false) }

// lockIsReady 报第一个实例是不是已经可以接收转交了。
func lockIsReady() bool { _, ready := readReadyFile(); return ready }

// writeReadyFile 写 ready 文件。withReady=false 时只写 pid（等于把上一次的 ready 抹掉）。
//
// 用「整个重写」而不是「追加」：ready 是一次性的事实，追加的话重试几次就会写出好几行
// ready，读的那边也没法分辨是哪一个进程写的。
func writeReadyFile(withReady bool) {
	body := readyFilePid + strconv.Itoa(os.Getpid()) + "\n"
	if withReady {
		body += readyFileReady + "\n"
	}
	// 写坏了就写坏了：那只是让后来的实例多等一轮，超时之后照样会问框架。
	_ = os.WriteFile(readyPath(), []byte(body), 0o644)
}

// readReadyFile 读 ready 文件里的 pid 和 ready。读不出来（文件还没建、内容被写了一半、
// 格式不认识）一律当成「什么都没有」——宁可多等一轮，不能把一次半截的写当成
// 「有人就绪」。
func readReadyFile() (pid int, ready bool) {
	b, err := os.ReadFile(readyPath())
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == readyFileReady:
			ready = true
		case strings.HasPrefix(line, readyFilePid):
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, readyFilePid))
		}
	}
	if pid <= 0 {
		return 0, false
	}
	// 🔴 **写这个 ready 的进程必须还在**。
	//
	// 上一轮那个第一个实例是被 SIGKILL 掉的，它写下的 ready 就留在文件里了。这一轮
	// 的两个进程同时起来：拿到内核锁的那个还没走到 `clearLockReady`，另一个就已经
	// 读到了**上一次**留下的 ready——于是它不等，直接去问框架那把锁。那时候真正
	// 的第一个可能正在建 socket，连不上；连不上它就把那个 socket 文件删掉自己建，
	// 而第一个紧接着 Listen 失败、重试时连上了**它**，反过来退出。两个进程于是都
	// 以为自己拿到了锁（`TestLockRacesTwoFreshProcesses` 第 4 轮往后红的就是这个：
	// 家目录里那个 `.sock` 一出现，坏的轮次就接着来）。
	//
	// 判「主人还在不在」是那个文件本来就记了 pid 却一直没人用的原因（`readReadyFile`
	// 返回的 pid 到今天为止从没被读过）。
	if !processAlive(pid) {
		return 0, false
	}
	return pid, ready
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
