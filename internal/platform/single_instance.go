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
// ## 为什么在框架那把锁之外还要自己拿一把
//
// 框架那把锁（`mygo/single_instance.go`）抢得到的时候是好用的：后一个实例 Dial
// 连上第一个、把手里的命令行递过去、拿到 false、退出。**但两个实例真正同时启动
// 时，它会对两个都返回 true。**
//
// 原因在它的循环里：谁都还没建好 socket，于是两个进程 Dial 都失败；接着两个都
// `os.Remove(path)`——后到的那个把先到的刚建出来的 socket 文件删了；然后两个
// `net.Listen(path)` 都成功（路径已经空出来，inode 不同），两个都认为自己拿到
// 了锁。实测（`TestLockRacesTwoFreshProcesses`，两个进程同一瞬间启动，20 轮）
// **17 轮两边都拿到**。
//
// 现象是用户把程序坞图标连点两下，就有两个 Biu MyGo 同时起来，两个播放列表、
// 两个进度条。这条验收就是防它的，所以上面再压一把**内核给的独占锁**：同一个
// 文件上 flock，同时来的进程里内核只认一个。框架那把锁继续管「第二个实例把
// biu:// 递过来」那件事，两层分工不重叠。
//
// 上游修好这个竞态之后，这层可以整个删掉——`TestLockRacesTwoFreshProcesses`
// 会一直是绿的。
func Lock() bool {
	// 第一层，框架那把：抢不到就说明确实已经有一个在跑，它会把我们的命令行
	// 接过去。抢到了也不作数，见下面。
	first := mygo.App.RequestSingleInstanceLock()
	if !first {
		return false
	}
	// 第二层，内核那把。同时启动的两个进程里只有一个拿得到。
	return lockExclusive()
}

// lockFile 是 unix 那把 flock 锁住的文件句柄（见 single_instance_lock_unix.go）。
//
// 声明在共用文件里而不是 unix 那一份，是为了测试在三个平台上都编得过。Windows
// 那边永远不设它——那边的互斥是「锁文件存在」这件事，不需要留句柄。
var lockFile *os.File

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
