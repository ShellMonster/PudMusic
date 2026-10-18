// 设置页的「配置」那一段：导出 / 导入配置，以及从 Biu 导入。
//
// # 段名为什么和 Biu 不一样
//
// Biu 把「导出配置 / 导入配置」长在「关于应用」那一段里（`system-settings.tsx:388-402`，
// 版本号 + 检查更新之后紧接着就是 `<ImportExport />`）。我们把它拆成了独立的
// 「配置」一段，理由是**这两段改的不是同一样东西**：「关于应用」那一段动的是
// `App` 上的更新器状态（它自己那一份 `app-update.json`），而这一段动的是用户的
// 设置文件——而「只读」保护护的恰恰是设置文件（见下面 `settingsConfig` 的注释）。
// 合成一段的话，「设置来自更新的版本」这一个场景会把本来还能用的导出入口一起关掉。
//
// ⚠️ 顺带把一句过期的话改掉：这一段的文件头曾经写着「我们没摆版本号和检查更新
// ——版本号是 9.9、检查更新是 9.14，都还没做」。**两者都做完了**（自动更新，
// commit `8c936d4`，在 `update_entry.go` / `update_dialog.go`），而且 PLAN 里
// 9.9 与 9.14 指的分别是「更新日志富文本 + 检查更新按钮」和「打包与更新链集成
// 验证」，那个引用本身就是照错的。
//
// # 三个按钮，三件不同的事
//
//	导出配置  存一份**裸的** JSON（Biu 那个形状），**凭据一件都不带**
//	导入配置  读一份 JSON 并**并进**现在的设置（文件里没有的键原样保留）
//	从 Biu 导入  把 Biu 那个目录里的设置 / 快捷键 / 下载记录 / 歌词缓存搬过来
//
// 三件都**不整体回滚**：导出写的是用户新选的文件、导入只并不改（`MergeSettings`
// 的语义）、从 Biu 导入是逐项跳过的（`app.Imported.Notes`）。一件事失败不该把
// 另外两件做过的撤回去。
//
// # 凭据不进导出
//
// 导出走白名单 + 抹掉代理密码，见 `internal/app/export.go` 的文件头。
// Cookie / SESSDATA / refresh_token / 带签名的 URL **根本不在设置里**——凭据存在
// 系统钥匙串（`app/credentials.go`），不在用户数据目录的任何一份 JSON 中。
// 代理密码**在**设置里，所以要单独抹；抹完 `proxySettings` 那一格还在，
// 类型、主机、端口、用户名照导（代理地址不是秘密，换台机器接着用是想要的）。
package ui

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settingsConfig 是常规那一页末尾的「配置」段。
//
// ⚠️ 这一段**不管只读**：设置来自更新的版本时改动照样要拒掉（`UpdateSettings`
// 返回 `ErrSettingsFromNewer`），但**导入/导出的按钮不关**——导出在只读时是唯一
// 还能做的事（用户要拿一份出来排查），而导入会让 store 转只读，点了只会得到一句
// 「这份设置来自更新的版本」，那句话由 `applySetting` 那条路统一报。
func (s *Shell) settingsConfig(c *ui.Context) {
	t := c.Theme()
	rep := LastMigration()
	s.settingSection(c, "配置")
	s.settingSectionEnd(c)

	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if flatButton(c, "导出配置").FontSize(13).Clicked() {
				s.exportConfig()
			}
			if flatButton(c, "导入配置").FontSize(13).Clicked() {
				s.importConfig()
			}
		})

		// 数据目录来自更新的版本：整个目录一个字都不写，**导入按钮也就没有意义**
		// ——按下去每一样都会被各自的只读判断挡掉，用户只会看到四句「没带过来」。
		// 摆一句说明，比摆一个必然失败的按钮诚实。
		if rep.ReadOnly {
			ui.Text(c, "数据目录来自更新的版本，这一版一个字都不会写回去，所以下面几样都没有带过来。").
				FontSize(12).TextColor(t.Danger).Wrap()
		} else if dirs := rep.BiuDirs; len(dirs) > 0 {
			// 「从 Biu 导入」只在**找得到** Biu 的数据目录时才摆。摆一个点了必然说
			// 「没找到 Biu」的按钮，等于让用户自己判断这台机器上有没有装过 Biu。
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if flatButton(c, "从 Biu 导入").FontSize(13).Clicked() {
					s.importFromBiu(dirs)
				}
				ui.Text(c, "从 "+shortDir(dirs[0])+" 导入设置、快捷键、下载记录和歌词").
					FontSize(12).TextColor(t.TextMuted)
			})
		}

		// 上一次启动做了什么，原样摆出来。用户看不见发生了什么的话，下次遇到
		// 「我的设置怎么变了」没有任何线索——迁移是一条会改他数据的路。
		if note := migrationNote(rep); note != "" {
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).Wrap()
		}

		// **「没带过来的」逐条摆出来**，这是这一段最要紧的部分。
		//
		// 任务书明写「凭据可以不迁，要提示重新登录」——那句提示就在这里。
		// 不摆的后果不是「少一句话」：用户导完之后发现要重新登录，而界面上从头到尾
		// 没提过，他会以为是应用把他踢下线了，或者干脆不知道发生过一次导入。
		//
		// 之前这里写的是「见下方清单」而下方**什么都没有**（`Imported.Notes` 全程
		// 没被读过），那句话是纯骗人的。
		s.importNotes(c, rep)
	})
}

// importNotes 把「没带过来的」「故意不迁的」逐条摆出来。
//
// 一次都没有导入过时不画：那一段下面空着比摆一句「一切正常」干净，而真正有事
// 的时候它会自己出现。
func (s *Shell) importNotes(c *ui.Context, rep MigrationReport) {
	if len(rep.Imported.Notes) == 0 {
		return
	}
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, "这次没有带过来的：").FontSize(12).Bold()
		for _, n := range rep.Imported.Notes {
			ui.Text(c, "· "+n).FontSize(12).TextColor(t.TextMuted).Wrap()
		}
	})
}

// exportConfig 弹保存框，把当前设置写出去。
//
// ⚠️ **必须在另一个 goroutine 里弹框**：MyGo 把弹框请求 post 到主线程再等系统
// 回调，主线程自己进去等就是死等，事件循环卡死（和 `pickPath` 同一个道理）。
func (s *Shell) exportConfig() {
	parent := s.win
	picker := s.picker
	name := app.ExportFileName(time.Now())
	go func() {
		path, err := picker.saveConfig(parent, name)
		if err != nil {
			s.addToast("导出配置失败：" + err.Error())
			return
		}
		if path == "" {
			return // 用户取消：什么都不说
		}
		if err := app.ExportSettings(path, s.app.SettingsSnapshot()); err != nil {
			s.addToast("导出配置失败：" + err.Error())
			return
		}
		s.addToast(fmt.Sprintf("已导出到 %s（不含任何登录信息）", path))
		s.wake.invalidate()
	}()
}

// importConfig 弹打开框，把挑回来的那份并进现在的设置。
//
// 并不是替换：Biu 的导入是逐键 merge（`export-import.tsx:49-53`），文件里没有的
// 键原样保留。所以「导一份只改过主题的配置」不会把别的设置打回默认值。
func (s *Shell) importConfig() {
	parent := s.win
	picker := s.picker
	go func() {
		path, err := picker.pickConfig(parent)
		if err != nil {
			s.addToast("导入配置失败：" + err.Error())
			return
		}
		if path == "" {
			return // 用户取消
		}
		next, err := app.ImportSettingsFile(path, s.app.SettingsSnapshot())
		if err != nil {
			s.addToast("导入配置失败：" + err.Error())
			return
		}
		_, err = s.app.UpdateSettings(func(cur *app.Settings) { *cur = next })
		if err != nil {
			// 只读（来自更新的版本）时 UpdateSettings 拒掉。这里必须把话说清楚，
			// 否则用户看到的是「点了没反应」。
			s.addToast("导入配置失败：" + trimApp(err))
			return
		}
		if err := s.app.FlushSettings(); err != nil {
			log.Printf("biu: 导入配置后落盘失败：%v", err)
		}
		s.addToast("已导入配置，设置已应用")
		s.wake.invalidate()
	}()
}

// importFromBiu 从 Biu 的数据目录导入。
//
// ⚠️ **数据目录来自更新的版本时按下去什么都不做**。那条路上每一项都会被各自的
// 只读判断挡掉（设置、快捷键、队列各自判），用户看到的是四句「没带过来的」，
// 而他刚才明明点了一个看着能用的按钮。按钮在那条路上已经不摆（`settingsConfig`），
// 这里再判一次是因为**界面和这一层之间隔着一个 goroutine**：那时数据目录的版本
// 可能已经被别的东西改过了。
func (s *Shell) importFromBiu(dirs []string) {
	dir := dirs[0]
	go func() {
		if LastMigration().ReadOnly {
			s.addToast("数据目录来自更新的版本，这一版一个字都不会写回去")
			return
		}
		udir, err := userDataDir()
		if err != nil {
			s.addToast("从 Biu 导入失败：" + err.Error())
			return
		}
		r, err := importFromBiu(dir, udir)
		if err != nil && !r.Any() {
			s.addToast("从 Biu 导入失败：" + err.Error())
			return
		}
		// 设置是原地换掉的：store 内存里还是旧的，得推一份新的进去。
		if r.Settings {
			s.reloadSettings()
		}
		if r.Shortcuts {
			s.reloadShortcuts()
		}
		if r.Downloads > 0 {
			s.reloadDownloads()
		}
		// 报告落进 `LastMigration()`，设置页「配置」那一段会把它逐条列出来。
		rememberImport(r)
		s.addToast(biuImportToast(r))
		s.wake.invalidate()
	}()
}

// rememberImport 把一次手动导入的结果并进最近一次的报告。
//
// **为什么不能只弹 toast**：toast 两秒就没了，而「Biu 那份登录信息不迁、请重新
// 登录」这句话用户不在那两秒里看见，就等于没说。设置页上那一段是长期摆着的。
func rememberImport(r app.Imported) {
	migration.mu.Lock()
	migration.r.Imported = r
	migration.mu.Unlock()
}

// reloadSettings 把设置文件重新读一遍推进 store。
//
// 导入是**在 store 外面**改的文件（`app.BiuScan.Import` 直接写盘），store 内存里
// 还是导入前的那一份。不推的话设置页上看到的还是旧值，而用户重新打开应用之后
// 看到的却是新值——同一个设置两个答案。
func (s *Shell) reloadSettings() {
	path, err := userDataDir()
	if err != nil {
		return
	}
	next, err := app.LoadSettings(app.SettingsPath(path))
	if err != nil {
		log.Printf("biu: 导入后重读设置失败：%v", err)
		return
	}
	if _, err := s.app.UpdateSettings(func(cur *app.Settings) { *cur = next }); err != nil {
		log.Printf("biu: 导入后写回设置失败：%v", err)
		return
	}
	if err := s.app.FlushSettings(); err != nil {
		log.Printf("biu: 导入后落盘设置失败：%v", err)
	}
}

// reloadShortcuts 把快捷键重新读一遍挂到设置页那一格上。
func (s *Shell) reloadShortcuts() { s.mountShortcuts() }

// reloadDownloads 让下载队列重新读一次文件。
//
// `MountDownloads` 会重新 `download.New`（它里面 `Restore` 一次）并**把旧的关掉**
// （`app.MountDownloads` 的注释说了为什么关的时候不落盘）。这一点原先是个洞：
// 旧的 Manager 还在跑它自己的循环，而退出时只有新 Manager 会落盘——旧的那批正在
// 下的任务进度永远不会被写下来。
func (s *Shell) reloadDownloads() {
	dir, err := userDataDir()
	if err != nil {
		return
	}
	media := newCredentialTransport().Client(nil)
	if err := s.app.MountDownloads(app.Downloads{
		Media:       media,
		QueueFile:   filepath.Join(dir, download.QueueFileName),
		FallbackDir: downloadsDir(),
	}); err != nil {
		log.Printf("biu: 导入后重挂下载失败：%v", err)
	}
}

// userDataDir 是本应用的用户数据目录。
//
// ⚠️ **必须走这个函数，不要在调用点直接调 `mygo.App.Path`**。无头 Tester 里
// `mygo.App.Path` 会返回一个**真的、可写的**目录（`~/Library/Application
// Support/<测试二进制名>/`），于是「从 Biu 导入」那个测试会把 Biu 的设置真的
// 写进用户的目录里——下次跑同一批测试读到的是上一次留下的，而用户目录里多出
// 一个他从来没见过的文件夹。测试替掉这一格（见 `settings_tab_config_test.go`
// 里的 `withUserDataDir`）。
//
// `MountMigration` 也需要这个目录，而它走的是自己的 `mygo.App.Path`：它是
// `main` 里的第一个调用，跑在框架 ready 之前，且没有界面对应的替身，所以**测
// 不到**。这一层的逻辑（`runUpgrade` / `firstBiuImport`）都是拿目录当参数的纯
// 函数，那两个才是被测的。
var userDataDir = func() (string, error) { return mygo.App.Path(mygo.PathUserData) }

// migrationNote 把上次启动的升级/导入结果压成一句人话。
//
// 没有话说时返回空串（界面上那一行就不画）。**升级失败那一句必须带着「原文件
// 一个字节都没少」**——用户最怕的是「升级失败之后我的设置没了」。
//
// 「见下方清单」那句现在是真的了（下面紧接着就是 `importNotes`），但那句话本身
// 改成自足的一句：它说的是「导入过」，而**没带过来的那几条**由下面逐条列出来。
func migrationNote(r MigrationReport) string {
	var parts []string
	if r.Upgraded != "" {
		parts = append(parts, r.Upgraded)
	}
	if r.Imported.Any() {
		parts = append(parts, "已从 Biu 导入过数据")
	}
	if len(parts) == 0 {
		return ""
	}
	return joinNames(parts) + "。"
}

// shortDir 把一个长路径压成「…/Biu」这样的一小片字。
//
// 只留**最后一段**而不是「~/Library/Application Support/Biu」：这一段是按钮旁边
// 的一句说明，而它所在的控件宽度有限，完整路径会把说明挤成两三行、把按钮挤走。
// 完整路径在导入完成后的提示里给（`biuImportToast` 用的 `Imported.Dir`）。
func shortDir(p string) string { return "…/" + filepath.Base(p) }

// trimApp 把 app 包给错加的「app: 」前缀去掉，界面上要给人话（和
// `reportSettingsError` 里那一处同一个理由）。
func trimApp(err error) string {
	return strings.TrimPrefix(err.Error(), "app: ")
}
