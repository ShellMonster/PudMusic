// 票 #360 的画面验收：列表行里的 UP 名是一枚**可点的**扁平按钮。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderListOwnerShots
//
// # 深浅两套都要出
//
// 这一票改的那一枚是**次要色**的字（不是主色），而主色在深浅两套里差得远——
// 只出浅色的话深色下那一枚是不是还「读得出来」就看不出来。
//
// # ⚠️ 一个字都不许打真 B 站接口
//
// 全部是内存夹具 + 无头渲染：首页那一屏的歌是直接塞进去的（`homeSongs`）。
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// TestRenderListOwnerShots 把「列表行里的 UP 名」存成 PNG。
func TestRenderListOwnerShots(t *testing.T) {
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
			songs := homeSongs{
				tabs: [homeTabCount][]list.Music{0: homeSongsWithOwnerMids("a", 8)},
			}
			s, tt := newShell(t, systemIsDark(dark))
			s.home.SetSongs(songs)
			homeFrame(tt)

			// ① 平常的样子：一列 UP 名（次要色，无描边）。
			save(t, dir, "360-列表里的UP名-"+name+".png", tt)

			// ② 指针停在第二行的 UP 名上：那一枚要显出扁平按钮的悬停反馈。
			//
			// ⚠ **推钟要按 AGENTS §2**：提示是延时的（框架那份 600ms 出），
			// 所以推钟而不是数帧。这一张**不推钟**——推了就出提示，而提示正好
			// 盖住那一枚（它摆在锚点上面），反馈就看不见了。
			r, ok := tt.Find("a 的 UP 主 2")
			if !ok {
				t.Fatalf("画面上找不到那个 UP 名，文字有 %q", tt.Texts())
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			save(t, dir, "360-列表里的UP名-悬停-"+name+".png", tt)

			// ③ 再推 700 毫秒：上面挂起那句提示（「打开 X 的空间」）。
			tt.Advance(700 * time.Millisecond)
			tt.Frame()
			save(t, dir, "360-列表里的UP名-提示-"+name+".png", tt)
		})
	}
}
