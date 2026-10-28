// 票 #345 的出图：迷你播放器那一扇窗在**静音**时看得出来（深浅两套）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderMiniMuteShots
//
// # 出四张：静音 / 未静音 × 浅色 / 深色
//
// ⚠️ **深浅两套都要出**：那枚图标是**单色**的（跟着主题走），而静音那一张
// 是「喇叭带叉」、响那一张是「喇叭带波纹」——浅色下两者的差别全在那几个
// 像素上，不出两套就看不出它糊没糊。
//
// ⚠️ **和未静音的那一张并排出**：单独一张静音图看不出「和平时有什么不同」，
// 而**这正是这一票要断的事**（BMG-180 评审：滑块停在 20% 看不出静音）。
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestRenderMiniMuteShots 把迷你窗在静音 / 未静音两种状态各存一张 PNG。
//
// ⚠️ **静音是走应用层那一格改的**（`App.ToggleMute`）：出图要画的是
// **真状态**，不是画的时候临时改一个变量——那样出出来的图和实际那扇窗
// 画的不一样，而这一票的整个意思就是「实际那扇窗要看得出来」。
func TestRenderMiniMuteShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	requireTestAudio(t)

	for _, dark := range []bool{false, true} {
		theme := "浅色"
		if dark {
			theme = "深色"
		}
		t.Run(theme, func(t *testing.T) {
			for _, muted := range []bool{false, true} {
				name := "响"
				if muted {
					name = "静音"
				}
				s, a := newMiniShell(t, nil)
				if err := enqueueTwoWithCover(a, t); err != nil {
					t.Fatal(err)
				}
				a.Toggle() // 暂停：播放键上写的是「播放」
				// ⚠️ **挂设置要在改音量之前**：`MountSettings` 会从设置文件里把
				// **音量**和**静音**那两格**搬过来**（`settings_mount.go`：挂上就
				// 覆盖），所以挂晚了的话这一轮改的两格会被它盖掉——出出来的两张
				// 图会一模一样，而这一票要的恰恰是**看得出不同**。
				path := filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)
				if err := a.MountSettings(path); err != nil {
					t.Fatal(err)
				}
				a.SetVolume(0.2)
				if muted {
					a.ToggleMute()
				}
				// 深色那一档是用户在设置里选的，而迷你窗读的是**主窗口那个
				// App 上的设置**（`NewMini` 的第二个参数就是它）——不挂的话
				// `UpdateSettings` 改的是一个没挂盘子的 App，画面上什么都不会变。
				if dark {
					if _, err := a.UpdateSettings(func(st *app.Settings) {
						st.ThemeMode = app.ThemeModeDark
					}); err != nil {
						t.Fatal(err)
					}
				}
				tt := miniTester(s)
				waitFor(t, "封面画上来了", func() bool {
					tt.Frame()
					return !miniCoverBox(tt).Empty()
				})
				save(t, dir, fmt.Sprintf("mini-mute-%s-%s.png", name, theme), tt)
			}
		})
	}
}
