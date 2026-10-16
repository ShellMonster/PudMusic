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
	"strings"

	"github.com/egoist/mygo"
)

// URLScheme 是本应用自己的 URL scheme。
//
// 它只干一件事：让第二个实例告诉第一个实例「该显示哪一页」。macOS 上点
// `biu://settings` 由系统直接转给已经跑着的那个应用（根本不会起第二个进程），
// Windows / Linux 上这条 URL 跟着命令行走，由单实例锁转交（见 Lock）。
//
// 名字跟着 bundle 标识符走：`biu` 而不是 `biugo`，用户手输时短一点，
// 撞上别的应用的概率也低一点。真撞上了系统会让用户选，默认处理器由
// RegisterScheme 决定。
const URLScheme = "biu"

// Lock 拿单实例锁。返回 true 表示「我是第一个实例，该继续跑」。
//
// **必须在 mygo.App.Run() 之前调**，晚一步就是竞态：Run 之后事件循环才起来，
// 那时候两个实例同时抢锁，谁抢到全看调度，最坏的结果是两个都在跑、
// 用户的播放列表被写坏两次。框架的锁在抢不到时会把本实例的命令行和工作目录
// 转交给第一个实例，所以拿到 false 的那个只要自己退出就行——**不要留在后台**
// 当一个没有窗口的僵尸进程，那在用户的任务栏/活动监视器里就是一个怎么点都
// 没反应、只能强杀的应用。
func Lock() bool { return mygo.App.RequestSingleInstanceLock() }

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
