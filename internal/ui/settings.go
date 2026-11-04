// 设置在界面层的这一半：文件放哪、挂上去、出了事告诉用户。
//
// 让用户改的那一半在 settings_page.go；这一份是「怎么落到盘上」和「出了事
// 怎么说」。
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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo"
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
		// 「上次听到哪儿」这一格：**退出前强制落一次**（见
		// `internal/app/playback_state.go`）。
		//
		// ⚠️ **它排在 flush 前面**：flush 落的是「此刻设置里是什么」，而这一格
		// 是刚刚才写进去的——顺序反了的话，退出时最后那一下就丢了。
		//
		// 丢了的症状是「重启之后回到空队列」，用户只会觉得「这应用记不住东西」。
		a.SavePlayback()
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
	s.addToast("设置："+strings.TrimPrefix(err.Error(), "app: "), notice.Danger)
}

// themePresets 是主题方案那几档（含显式的「经典」，放最后），顺序就是画面上
// 从左到右、从上到下的顺序。暖纸/暮光/柔雾的名字照设计规范 v0.1；青瓷/墨宣/
// 紫檀/靛夜是本仓库原创档（2026-10-26 补的冷色/水墨/紫/蓝四个色相）。
var themePresets = []struct {
	preset app.ThemePreset
	label  string
}{
	{app.ThemePresetWarmPaper, "暖纸"},
	{app.ThemePresetDuskAmber, "暮光"},
	{app.ThemePresetSoftPeach, "柔雾"},
	{app.ThemePresetCeladon, "青瓷"},
	{app.ThemePresetInkPaper, "墨宣"},
	{app.ThemePresetSandalwood, "紫檀"},
	{app.ThemePresetIndigoNight, "靛夜"},
	{app.ThemePresetClassic, "经典"},
}

// themeModes 是四档主题，顺序就是从左到右的顺序。设置页那一排按钮用。
//
// ⚠️ **每一档都带一枚图标**（上游那一排是「图标 + 文字」，`system-settings.tsx:
// 105-153`：跟随系统 `RiComputerLine`、浅色 `RiSunLine`、深色 `RiMoonLine`，
// 都是 `size={18}`）。图标名是 `icons.go` 里的常量——「跟随系统」那一枚是
// 票 #297 新加的资源（`Device/computer-line.svg`），照上游 RemixIcon 的目录
// 放进去，许可证那一份没变（还是 Remix Icon License，见 `internal/licensing`）。
// 「定时」是 #534 加的第四档（19:00~次日 07:00 用深色），用的是那批资源里的
// `System/time-line.svg`（`iconClock`）。
var themeModes = []struct {
	mode  app.ThemeMode
	label string
	icon  string
}{
	{app.ThemeModeSystem, "跟随系统", iconComputer},
	{app.ThemeModeLight, "浅色", iconSun},
	{app.ThemeModeDark, "深色", iconMoon},
	{app.ThemeModeSchedule, "定时", iconClock},
}
