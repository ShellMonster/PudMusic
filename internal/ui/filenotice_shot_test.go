// 出图：「系统交给我们的那个文件，收下了没有」那一句在**迷你播放器**上
// 长什么样。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFileNoticeShots
//
// # 为什么出图
//
// 这一票第二轮评审抓到的那个静默故障是「**用户眼前那一扇没有那句话**」——
// 而「那句话在不在画面上」在无头 Tester 里量得到（`tt.HasText`），**长什么样**
// 量不到。所以这一条出图，好让人**看**一眼：那句话是不是**读得清**、
// **有没有被裁掉**、**底色是不是语义色**。
//
// # 出两张
//
//   - **放不下**（`.wma`）：最长的一档——「这个格式我们放不了（支持 .mp3 …）」。
//     这一档决定**三行封顶**够不够。
//   - **文件不在了**：短的那一档。
//
// ⚠️ **深浅两套都要出**：底色是语义色（红），而字色是「压在红底上读得清」的
// 那一档（`toastColors`）——深浅主题下那是两个不同的字色，只看一套的话
// 另一半是未验的。
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// TestRenderFileNoticeShots 把迷你播放器上那两句话各存一张 PNG。
func TestRenderFileNoticeShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	cases := []struct {
		name string
		text string
		tone notice.Tone
	}{
		{
			name: "放不下",
			text: "「老歌.wma」这个格式我们放不了（支持 .mp3 .flac .wav .m4a .aac .ogg .aiff .aif .aifc）",
			tone: notice.Danger,
		},
		{
			name: "文件不在了",
			text: "打不开「没了.flac」：那个文件已经不在原来的位置上了",
			tone: notice.Danger,
		},
	}

	for _, c := range cases {
		for _, dark := range []bool{false, true} {
			theme := "浅色"
			if dark {
				theme = "深色"
			}
			t.Run(c.name+"-"+theme, func(t *testing.T) {
				a := app.New()
				t.Cleanup(a.Close)
				main := newTestShell(t, onApp(a)).s
				mini := main.newMiniShell()
				t.Cleanup(mini.releaseMini)

				// 深色那一档是用户在设置里选的，而迷你窗读的是**主窗口那个
				// App 上的设置**——不挂的话 `UpdateSettings` 改的是一个没挂盘子的
				// App，画面上什么都不会变。
				if dark {
					// ⚠️ **先挂设置**：`UpdateSettings` 在没挂盘子的 App 上
					// 直接报错（设置是「读盘 + 写盘」那一份）。
					path := filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)
					if err := a.MountSettings(path); err != nil {
						t.Fatal(err)
					}
					if _, err := a.UpdateSettings(func(st *app.Settings) {
						st.ThemeMode = app.ThemeModeDark
					}); err != nil {
						t.Fatal(err)
					}
				}

				// 迷你那一扇的 Tester。
				tt := miniTester(mini)

				// 那一句话（走生产那一条路：共享的那一份 + 要一帧）。
				main.sayFileNotice(c.text, c.tone)
				tt.Frame()

				save(t, dir, fmt.Sprintf("mini-file-notice-%s-%s.png", c.name, theme), tt)
			})
		}
	}
}
