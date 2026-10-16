// 开机自启：系统怎么记，应用怎么对。
//
// 这一层直接用框架的 API（`mygo/login.go` → `App.SetOpenAtLogin`）：
// macOS 13+ 是 SMAppService（「系统设置 → 通用 → 登录项」里那一条），
// macOS 12 是 LaunchAgent，Windows 是当前用户的注册表 Run 项（任务管理器
// 「启动」那一页里能看到、能禁），Linux 是 XDG autostart 的 .desktop。
//
// **不要自己拼 plist 路径**：手写的 LaunchAgent 在系统设置里是一个叫不出名字
// 的启动项，用户关不掉，卸载应用之后还留在 `~/Library/LaunchAgents` 里。
// 框架已经把这些平台的差异都做完了。
package platform

import "github.com/egoist/mygo"

// SetAutoStart 让系统开机时启动/不再启动这个应用。
//
// 只能从主线程调：框架的这两个 API 要过事件循环（`mygo/login.go` 用的是
// `onMainValue`），从一个没在跑事件循环的 goroutine 进去就是死等。
// macOS 上还需要打包成 bundle，`go run` 和测试里必然报错。
func SetAutoStart(enabled bool) error { return mygo.App.SetOpenAtLogin(enabled) }

// AutoStartEnabled 报系统里现在是不是登记着开机自启。
//
// 同样是主线程限定。它和设置文件里那个 `autoStart` 是**两个东西**：设置里
// 的那个是用户的意图，这个是系统里的事实，两者可能对不上（用户在系统设置里
// 手动关掉了；或者换机器时把配置拷过来、那台机器上并没有这条启动项）。
func AutoStartEnabled() bool { return mygo.App.OpenAtLogin() }

// AutoStartAction 是启动时要把系统对齐到设置时要做的动作。
type AutoStartAction int

const (
	// AutoStartKeep 是不动系统。
	AutoStartKeep AutoStartAction = iota
	// AutoStartTurnOn 是把系统的开机自启打开。
	AutoStartTurnOn
)

// PlanAutoStart 算出启动时该怎么对齐系统的开机自启。
//
// **只补「设置说开、系统说关」这一种，反过来一律不管。** 反过来（设置说关、
// 系统说开）多半是用户自己在系统设置里加的登录项——macOS 的「登录项」那一栏
// 就有这个开关——启动时按我们文件里的一个字节把它按掉，是替用户做决定，而且
// 没有任何好处：要关的话设置里那个开关就在那里。
//
// 这一段不是可有可无的洁癖。没有它，设置页里那个「开机自启」开关就是一句
// 空话：翻遍 Biu 上游也找不到一次 `setLoginItemSettings` 调用——它只把开关的
// 值存进了设置文件，从来没真的去注册过登录项（`biu/` 全库只有
// `app-settings.ts:2` 那个默认值和设置页里的读/写）。我们照抄那个行为的话，
// 用户开了开关、关了电脑、第二天发现应用没起来，而界面上那个开关还亮着。
func PlanAutoStart(want, applied bool) AutoStartAction {
	if want && !applied {
		return AutoStartTurnOn
	}
	return AutoStartKeep
}
