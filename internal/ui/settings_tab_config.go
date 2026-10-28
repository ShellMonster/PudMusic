// 设置页的「配置」段：导出 / 导入当前版本配置文件。
//
// 导入按字段合并，缺失字段保留当前设置；代理密码不会被配置文件覆盖。
package ui

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// settingsConfig 是常规那一页末尾的「配置」段。
//
// ⚠️ 这一段**不管只读**：设置来自更新的版本时改动照样要拒掉（`UpdateSettings`
// 返回 `ErrSettingsFromNewer`），但**导入/导出的按钮不关**——导出在只读时是唯一
// 还能做的事（用户要拿一份出来排查），而导入会让 store 转只读，点了只会得到一句
// 「这份设置来自更新的版本」，那句话由 `applySetting` 那条路统一报。
func (s *Shell) settingsConfig(c *ui.Context) {
	s.settingGroup(c, "配置", func() {
		ui.Row(c).FillWidth().Gap(space.Px8).Children(func() {
			if settingsButton(c, "导出配置").FontSize(scale.Font(c, scale.Size13)).Clicked() {
				s.exportConfig()
			}
			if settingsButton(c, "导入配置").FontSize(scale.Font(c, scale.Size13)).Clicked() {
				s.importConfig()
			}
		})
	})
}

// ⚠️ **必须在另一个 goroutine 里弹框**：MyGo 把弹框请求 post 到主线程再等系统
// 回调，主线程自己进去等就是死等，事件循环卡死（和 `pickPath` 同一个道理）。
func (s *Shell) exportConfig() {
	parent := s.win
	picker := s.picker
	name := app.ExportFileName(time.Now())
	go func() {
		path, err := picker.saveConfig(parent, name)
		if err != nil {
			s.addToast("导出配置失败："+err.Error(), notice.Danger)
			return
		}
		if path == "" {
			return // 用户取消：什么都不说
		}
		if err := app.ExportSettings(path, s.app.SettingsSnapshot()); err != nil {
			s.addToast("导出配置失败："+err.Error(), notice.Danger)
			return
		}
		s.addToast(fmt.Sprintf("已导出到 %s（不含任何登录信息）", path), notice.Success)
		s.wake.invalidate()
	}()
}

// importConfig 弹打开框，把挑回来的那份并进现在的设置。
//
// 按字段合并：文件里没有的键原样保留，所以只改主题不会重置其它设置。
func (s *Shell) importConfig() {
	parent := s.win
	picker := s.picker
	go func() {
		path, err := picker.pickConfig(parent)
		if err != nil {
			s.addToast("导入配置失败："+err.Error(), notice.Danger)
			return
		}
		if path == "" {
			return // 用户取消
		}
		next, err := app.ImportSettingsFile(path, s.app.SettingsSnapshot())
		if err != nil {
			s.addToast("导入配置失败："+err.Error(), notice.Danger)
			return
		}
		_, err = s.app.UpdateSettings(func(cur *app.Settings) { *cur = next })
		if err != nil {
			// 只读（来自更新的版本）时 UpdateSettings 拒掉。这里必须把话说清楚，
			// 否则用户看到的是「点了没反应」。
			s.addToast("导入配置失败："+trimApp(err), notice.Danger)
			return
		}
		if err := s.app.FlushSettings(); err != nil {
			log.Printf("biu: 导入配置后落盘失败：%v", err)
		}
		s.addToast("已导入配置，设置已应用", notice.Success)
		s.wake.invalidate()
	}()
}

// trimApp 把 app 包给错加的「app: 」前缀去掉，界面上要给人话（和
// `reportSettingsError` 里那一处同一个理由）。
func trimApp(err error) string {
	return strings.TrimPrefix(err.Error(), "app: ")
}
