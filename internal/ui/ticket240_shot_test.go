// 票 #240 的画面验收：设置页只有**一种**分段控件（F-17）、说明列有**最小宽度**
// （F-27）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket240Shots
//
// # 深浅两套 × 两档窗口宽
//
// 这一票改的两处**都要看**：「同页两种分段控件」在浅色下最刺眼（白滑块 vs 绿药丸），
// 而说明列的宽度两套主题下是同一组数——所以深浅两套都要出，免得只验了浅色。
// 900 那一档是**窗口的最小尺寸**（`WindowOptions.MinWidth`），1200 是默认。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是本地设置 + 无头渲染。
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderTicket240Shots 把这一票改过的设置页各存一张 PNG。
func TestRenderTicket240Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, winW := range []int{900, 1200} {
				b := newTestShell(t, at(winW, 800), systemIsDark(dark),
					withSettings(app.DefaultSettings("")))
				tt := b.tt
				openSettings(t, tt)
				homeFrame(tt)

				save(t, dir, fmt.Sprintf("90-设置-整窗-%d-%s.png", winW, name), tt)

				// 「外观」那一段放大：那里有三处分段控件（主题 / 数据显示样式 /
				// 颜色）和说明列。
				head, ok := tt.Find("外观")
				if !ok {
					t.Fatalf("设置页上没有「外观」那个分组标题，文字有 %q", tt.Texts())
				}
				x0 := int(head.X) - 40
				y0 := int(head.Y) - 12
				saveRect(t, dir, fmt.Sprintf("91-设置-外观段-%d-%s.png", winW, name), tt,
					x0, y0, winW-x0-8, 300)

				b.Close()
			}

			// 只读态：这一票把「数据显示样式」也换成了同一套分段控件，而那一套
			// 的只读画法（中性灰底 + 禁用灰字）是 #239 特意做过的——**主色在只读
			// 时根本不出现**。所以只读那一档也要出一张。
			saveReadOnlySettingsShot(t, dir, name)

			// ⚠️ **最坏的那两档也要出**：评审抓出来的回归就在这两档上——
			// 「窗口 900 + 侧栏拉到最大 480」和「窗口 900 + 系统字号 1.4 倍」。
			// 那一档的内容区窄到放不下「说明列 320 + 缝 24 + 控件列 260」，
			// 于是控件列会被推出窗口右缘（框架的 flex 没有收缩，放不下就是溢出）。
			for _, sc := range []struct {
				name  string
				side  float64
				scale float32
			}{
				{"侧栏480", app.MaxSideMenuWidth, 1},
				{"字号1.4倍", app.DefaultSideMenuWidth, 1.4},
			} {
				saveNarrowSettingsShot(t, dir, sc.name, sc.side, sc.scale, name)
			}
		})
	}
}

// saveReadOnlySettingsShot 出一张只读态的设置页。
//
// ⚠️ **只读怎么造**：挂一份「版本号比我们新」的设置文件（`schemaVersion: 99`），
// 那一档一个字都不许写回去（`TestSettingsPageRefusesToWriteAFileFromANewerVersion`
// 验的就是它）。
func saveReadOnlySettingsShot(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion": 99, "appSettings": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := app.New()
	if err := a.MountSettings(path); err == nil {
		t.Fatal("来自更新版本的设置文件没有报错")
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	b := newTestShell(t, onApp(a))
	tt := b.tt
	tt.Frame()
	openSettings(t, tt)
	homeFrame(tt)

	save(t, dir, "92-设置-只读-"+name+".png", tt)
	head, ok := tt.Find("外观")
	if !ok {
		t.Fatalf("只读那一档的设置页上没有「外观」那个分组标题，文字有 %q", tt.Texts())
	}
	x0 := int(head.X) - 40
	saveRect(t, dir, "93-设置-只读-外观段-"+name+".png", tt, x0, int(head.Y)-12, 1200-x0-8, 300)
	b.Close()
}

// saveNarrowSettingsShot 出一张**最坏配置**的设置页。
//
// ⚠️ **侧栏宽度有 200ms 的过渡**：不推钟的话图上是动画中间那一档，而这一票要
// 证明的正是「最坏的那一档不裁控件」。
func saveNarrowSettingsShot(t *testing.T, dir, tag string, side float64, scale float32, name string) {
	t.Helper()
	st := app.DefaultSettings("")
	st.SideMenuWidth = side
	b := newTestShell(t, at(900, 800), systemIsDark(name == "深色"),
		withSettings(st),
		withPrefs(mygoui.Preferences{TextScale: scale}))
	tt := b.tt
	for range 4 {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
	}
	openSettings(t, tt)
	homeFrame(tt)

	save(t, dir, "94-设置-最坏-"+tag+"-"+name+".png", tt)
	b.Close()
}
