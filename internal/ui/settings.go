// 设置在界面层的这一半：文件放哪、挂上去、让用户切、出了事告诉用户。
//
// 为什么这一半留在界面层：「应用数据目录在哪」是框架的事，
// `mygo.App.Path(mygo.PathUserData)` 是 MyGo 自己的 API（`paths.go:53`）。
// **不要自己拼 `~/Library/Application Support/...`**——拼错的后果不是报错，
// 是「读不到文件」，而读不到文件按第一次运行处理：用默认值安静地开起来，
// 用户只发现设置一直存不住，换台机器再丢一次。框架自己的窗口状态文件
// （`WindowOptions.StateKey`）也放在这个目录里。
package ui

import (
	"fmt"
	"log"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// MountSettings 把设置挂到 a 上：读盘、落盘、退出前 flush。
//
// 必须在开窗口**之前**调：读盘是阻塞 IO。
func MountSettings(a *app.App) error {
	// 退出前把还欠着的改动落完。
	//
	// 挂 OnWillQuit 而不是 OnQuit：前者跑在事件循环停下**之前**，能拿到 error
	//（框架 app.go 的 prepareQuit 就是这么说的：「callers needing error
	// handling may Flush explicitly from a quit listener」），写失败还能进日志。
	//
	// 为什么非要这一次同步 flush：后台落盘是合并写（拖圆角滑块时 120ms 一次），
	// 最后那一下有可能还等在退避窗口里，用户改完直接退出就丢了。SIGKILL 拦不住，
	// 任何异步写盘都得配一个同步收尾才敢说「设置存住了」。
	//
	// 挂在最前面，下面拿不到目录直接返回时它照样兜着：那时没有 store，
	// FlushSettings 什么都不做。
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if err := a.FlushSettings(); err != nil {
			log.Printf("biu: 退出时保存设置失败：%v", err)
		}
	})

	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		// 记到 App 上，界面第一帧就会弹提示。只 log 是不够的：日志只有排障
		// 的人看得到，用户只会发现「改了主题，下次打开又变回来了」。
		a.RecordSettingsFailure(fmt.Errorf("拿不到应用数据目录，这次的改动关掉应用就没了: %w", err))
		return err
	}

	path := app.SettingsPath(dir)
	if err := a.MountSettings(path); err != nil {
		// 界面上也会弹一条给用户看；日志是给排障用的。
		log.Printf("biu: 读设置 %s 有问题，先按默认值用着：%v", path, err)
	}
	return nil
}

// reportSettingsError 把设置的问题告诉用户。**每帧都调**。
//
// 去重不在这里做，靠 `TakeSettingsError`：同一个错只交一次，所以这里每帧问
// 一句是安全的（界面上会一直是一样的话在闪，比设置本身出问题还难解释）。
//
// 用现成的 addToast：右下角、避开播放条、同时最多 3 条、两秒后自己消失。
// 不另造一套提示机制——那才会是「同一个错误提示出现两遍」。
func (s *Shell) reportSettingsError() {
	err := s.app.TakeSettingsError()
	if err == nil {
		return
	}
	// 设置的错自带中文说明，前面那个「app: 」是包名，去掉。
	s.addToast("设置：" + strings.TrimPrefix(err.Error(), "app: "))
}

// themeModes 是三档主题，顺序就是从左到右的顺序。
var themeModes = []struct {
	mode  app.ThemeMode
	label string
}{
	{app.ThemeModeSystem, "跟随系统"},
	{app.ThemeModeLight, "浅色"},
	{app.ThemeModeDark, "深色"},
}

// viewThemeSwitch 是主题档位那三个按钮：跟随系统 / 浅色 / 深色。
//
// 🔴 为什么临时放在顶栏下面，而不是设置页：Biu 的主题设置在 `/settings` 里，
// 而 `/settings` 这一页现在只画一张空状态，文案就写明「设置项还在顶栏下面」
// （routes 表里那一条）。设置页真做了，删掉这个函数、把同样的三个按钮搬过去
// 就行，`SetThemeMode` 一个字都不用改。
func (s *Shell) viewThemeSwitch(c *ui.Context) {
	t := c.Theme()
	cur := s.app.SettingsSnapshot().ThemeMode
	// 设置来自更新的版本时一个字都不许写回去。与其让用户按了没反应，不如把
	// 按钮关掉，并说清为什么。
	readonly := s.app.SettingsReadOnly()

	ui.Row(c).Key("theme-switch").Height(36).Padding(0, 12).Gap(4).
		AlignItems(ui.Center).Justify(ui.End).Children(func() {
		ui.Text(c, "主题").FontSize(12).TextColor(t.TextMuted)
		if readonly {
			ui.Text(c, "设置只读").FontSize(11).TextColor(t.TextMuted)
		}
		for _, opt := range themeModes {
			b := ui.Button(c, opt.label).Radius(6).Disabled(readonly)
			if opt.mode == cur {
				b = b.Background(t.Accent)
			}
			if b.Clicked() {
				// 改完立刻 invalidate 已经在 SetThemeMode 里做了：即时模式里
				// 按完不会自己再画一帧，不叫醒它屏幕上就是「按了没反应」。
				if err := s.app.SetThemeMode(opt.mode); err != nil {
					s.addToast("主题切不了：" + err.Error())
				}
			}
		}
	})
}
