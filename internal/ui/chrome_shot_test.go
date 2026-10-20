package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一族把**界面骨架**（侧栏 / 内容区 / 播放条三处底色、顶栏、设置页、播放条右边
// 那一组）存成 PNG，留给人眼和 Biu 上游的截图**并排**看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderChromeShots
//
// # ⚠️ **深浅两套都要出，而且这一票的重点是深色**
//
// 改之前侧栏和播放条取 `t.Surface`、内容区取 `t.Background`：浅色下那两个色差得
// 开（`#f4f4f5` / `#ffffff`），一眼看过去像「有层次」；**深色下差的是 `#27272a`
// / `#18181b`**，侧栏、内容区、播放条变成三条带。只出浅色的那一套，这张票等于
// 没验——而那正是它存在的原因。
//
// 每一张都挑了一个能看出层次的地方：01 看三处底色是不是一整块 + 播放条上缘那道
// 分界；02 看抽屉面板（`Surface`）和背景分不分得开；03/04 看设置页收没收在中间；
// 05 看网格卡片还分不分得开。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染（和
// `emptyshot_test.go` 那一族同一个约定）。
func TestRenderChromeShots(t *testing.T) {
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
			// ⚠️ **壳要带 `systemIsDark`**：不带的话「深色」那一档存出来和
			// 「浅色」一模一样（`emptyshot_test.go` 那一族踩过这个）。
			//
			// ⚠️ **还得挂一份设置**：设置页要读它，而 05 那一格要改显示样式——
			// 没挂上时 `UpdateSettings` 直接回「还没有挂上设置」（见
			// `internal/app/settings_mount.go`）。
			b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
			s, tt := b.s, b.tt

			// 01 整窗。指针停在内容区右上那片空地上，别停在任何一格按钮上。
			openATrack(t, s)
			s.navigate("/")
			waitFrames(t, tt, "首页画出来了", func() bool { return tt.HasText("播放") })
			tt.Move(1180, 300)
			homeFrame(tt)
			save(t, dir, "01-整窗-"+name+".png", tt)

			// 02 播放列表抽屉开着：面板要和背景分得开（`Surface` vs `Background`）。
			s.drawerOpen = true
			waitFrames(t, tt, "抽屉开出来了", func() bool {
				_, ok := tt.Find("播放列表")
				return ok
			})
			tt.Move(600, 300)
			homeFrame(tt)
			save(t, dir, "02-抽屉-"+name+".png", tt)
			s.drawerOpen = false
			tt.Frame()

			// 03 设置页：内容收在中间一段宽度里，页标题只出现一次。
			s.navigate("/settings")
			waitFrames(t, tt, "设置页", func() bool { return tt.HasText("常规设置") })
			tt.Move(1180, 400)
			homeFrame(tt)
			save(t, dir, "03-设置-"+name+".png", tt)

			// 04 代理那一页：两列输入框，是「别横跨整行」看得最清楚的一页
			//（它本来就是两列，列宽跟着容器走）。
			openSettingsTab(t, tt, "代理设置")
			tt.Move(1180, 400)
			homeFrame(tt)
			save(t, dir, "04-设置-代理-"+name+".png", tt)

			// 05 **最小窗口**（`WindowOptions` 的 900×560）：右边那一组多了三枚键
			// （模式 / 下载 / 队列），歌名那一块就是最紧的时候。这张是给「歌名有没有
			// 被挤没」看的——挤没了的**症状是「点了没反应」**，截图上一眼看不出来，
			// 所以这一张要和 01 摆在一起看。
			//
			// 单独造一个外壳：尺寸在 `newTestShell` 建的时候定死，`SetSize` 换不了
			// 已经建好的布局。
			func() {
				small := newTestShell(t, systemIsDark(dark), at(minWindowW, minWindowH),
					withSettings(app.DefaultSettings("")))
				openATrack(t, small.s)
				small.s.navigate("/")
				waitFrames(t, small.tt, "首页", func() bool {
					_, ok := small.tt.Find("播放进度")
					return ok
				})
				small.tt.Move(minWindowW-20, 300)
				homeFrame(small.tt)
				save(t, dir, "05-最小窗口-"+name+".png", small.tt)
				small.Close()
			}()

			b.Close()
		})
	}
}
