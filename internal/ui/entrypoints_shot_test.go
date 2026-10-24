package ui

// 这一族把**这一票动过的那几处入口**存成 PNG：下载记录页那两句提示、本地音乐
// 那个「添加目录」、侧栏里那一句「还没有收藏夹」、折叠态那枚「收藏夹」图标，
// 以及设置页里被指着的「下载目录」那一行。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderEntryPointShots
//
// ⚠️ **深浅两套都要出**：这一票改的几处都是「有没有一层底 / 是不是主色」这类
// 只在某一套主题下才看得出来的差别——浅色下 #f4f4f5 和 #ffffff 只差一级，深色
// 下那圈描边压在 #18181b 上才是一道线。
//
// ⚠️ **一个字都不许打真 B 站接口**：全是内存夹具 + 无头渲染。

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// shotShell 造一个**没挂下载**的外壳（下载目录是空的），停在某一页。
func shotShell(t *testing.T, dark bool, path string) (*Shell, *ui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	s, tt := newShell(t, withSettings(st))
	tt.SetDark(dark)
	s.navigate(path)
	for range 3 {
		tt.Frame()
	}
	return s, tt
}

func TestRenderEntryPointShots(t *testing.T) {
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
			// 01 下载记录页：**没有下载目录**的时候，中央那一格说的是「去设置」
			// 而不是「搜一首歌」（报告 F3），页头那一句是能点的（报告 C1）。
			{
				_, tt := shotShell(t, dark, downloadListPath)
				save(t, dir, "01-下载记录-空-没有目录-"+name+".png", tt)
			}

			// 02 设置页：从下载记录页跳过来，「下载目录」那一行被**指着**
			// （标题是主色，而且被滚进视野）。
			{
				_, tt := shotShell(t, dark, downloadListPath)
				if err := tt.Click("还没有下载目录"); err != nil {
					t.Fatalf("点页头那一句：%v", err)
				}
				for range 6 {
					tt.Frame()
				}
				save(t, dir, "02-设置-指着下载目录-"+name+".png", tt)
			}

			// 03 本地音乐页（一个目录都没加）：空状态里**有一枚「添加目录」
			// 按钮**，右上角那一枚带着上游的图标（报告 C2）。
			{
				tt, _, _, _ := shellOnLocalMusicIn(t, dark)
				save(t, dir, "03-本地音乐-空-"+name+".png", tt)
			}

			// 04 侧栏（展开态，收藏夹空）：那一句空状态**只有一句**
			// （没登录时是「登录后能看到收藏夹」——见 `favEmptyText`），
			// 而且缩进和分组标题对齐（报告 C3）。
			{
				_, tt := shotShell(t, dark, "/")
				save(t, dir, "04-侧栏-收藏夹-空-"+name+".png", tt)
			}

			// 05 侧栏（折叠态）：**收藏夹还在**——一枚图标，点开是一层浮层
			//（报告 C4）。
			{
				st := app.DefaultSettings("")
				st.SideMenuCollapsed = true
				s, tt := newShell(t, withSettings(st))
				tt.SetDark(dark)
				s.navigate("/")
				for range 3 {
					tt.Frame()
				}
				if err := tt.Click(favCollapsedName); err != nil {
					t.Fatalf("点「收藏夹」那一枚：%v", err)
				}
				for range 3 {
					tt.Frame()
				}
				save(t, dir, "05-侧栏-折叠-收藏夹浮层-"+name+".png", tt)
			}
		})
	}
}
