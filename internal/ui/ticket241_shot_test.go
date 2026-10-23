// 票 #241 的画面验收：顶栏末两枚的重量、错误原因截断、弹窗加宽、迷你窗音量
// 去主色、语义色压深、抽屉空状态的孤字、三处 600 字重。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket241Shots
//
// # 深浅两套都要出
//
// 这一票改的每一处都**只在深浅其中一套上看得出来**：语义色压深那一档在浅色下
// 才要压（深色下成功 / 警告本来就够），头像那一圈描边在深色下才看得出是「一圈
// 描边」而不是「一块圆盘」。只出浅色的话深色下那几条一次都没被看过。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// TestRenderTicket241Shots 把这一票改过的七处各存一张 PNG，深浅各一套。
func TestRenderTicket241Shots(t *testing.T) {
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
			// 80 顶栏：末两枚（头像 / 登录键）的重量要和左边那四枚线框图标
			// 同一档——整窗看的是「那一排」，放大看的是「墨迹」。
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			openATrack(t, s)
			s.navigate("/")
			waitFrames(t, tt, "首页画出来了", func() bool { return tt.HasText("播放") })
			tt.Move(1180, 300)
			homeFrame(tt)
			save(t, dir, "80-顶栏-整窗-"+name+".png", tt)
			saveRect(t, dir, "81-顶栏末两枚-放大-"+name+".png", tt, 900, 0, 300, 64)

			// 82 下载记录：**错误原因不再被截断**（整句都在），而「下载完成」那
			// 几个字是压深过的那一档成功色。
			rows := []download.Snapshot{
				dlSnapshot("第一首", download.StatusDone, 100, 100),
				dlSnapshot("第二首", download.StatusError, 200, 100),
			}
			_, dtt := shotDownloadShell(t, dark, rows)
			save(t, dir, "82-下载记录-错误原因与语义色-"+name+".png", dtt)
			saveRect(t, dir, "83-下载记录-状态列放大-"+name+".png", dtt, 760, 180, 170, 120)

			// 84 确认弹窗：面板不再是 228 宽的一条。
			//
			// ⚠️ **深色那一套要真的深色**：`collectionShell` 不带 `systemIsDark` 的
			// 话是浅色那一份，存出来的「深色」和「浅色」逐字节相同（评审查出来过）。
			_, cs, ctt := collectionShellOpts(t, []shellOpt{systemIsDark(dark)}, "", "12345",
				mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			waitSongs(t, ctt, cs, 3)
			openBatchConfirm(t, ctt, "批量删除")
			save(t, dir, "84-确认弹窗-"+name+".png", ctt)

			// 85 队列抽屉的空状态：提示句折两行，**末行不是孤字**。
			qb := newTestShell(t, systemIsDark(dark))
			qb.s.drawerOpen = true
			qb.tt.Frame()
			homeFrame(qb.tt)
			save(t, dir, "85-队列抽屉空状态-"+name+".png", qb.tt)
			saveRect(t, dir, "86-队列抽屉空状态放大-"+name+".png", qb.tt, 820, 280, 380, 120)
			qb.Close()

			// 87 UP 空间：名字和统计那几个数字是 **500**，不是 600。
			ex := newUIExplore(t)
			uiSpaceData(ex, "我自己", 0, 1)
			ss, stt, _ := newSpaceShell(t, ex, true, "77", systemIsDark(dark))
			ss.navigate("/user/77")
			waitFrames(t, stt, "空间画出来了", func() bool { return stt.HasText("我自己") })
			tt2 := stt
			tt2.Move(1180, 300)
			homeFrame(tt2)
			save(t, dir, "87-UP空间-"+name+".png", stt)

			// 88 迷你播放器：**音量滑块不是主色**了（平时次要色，悬停才主色）。
			//
			// ⚠️ **要挂设置**：深色那一档是用户在设置里选的，而迷你窗读的是**主窗口
			// 那个 App 上的设置**——不挂的话改的是一个没挂盘子的 App，画面上什么都不会
			// 变（`miniplayer_test.go` 的 `TestRenderMiniShots` 踩过这个）。
			saveMiniShot(t, dir, "88-迷你播放器-音量去主色-"+name+".png", dark)

			b.Close()
		})
	}
}

// saveMiniShot 存一张迷你播放器的图（深浅各一档由 dark 定）。
func saveMiniShot(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	a := app.New()
	if err := a.MountSettings(filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); _ = a.CloseSettings() })
	if dark {
		if _, err := a.UpdateSettings(func(st *app.Settings) { st.ThemeMode = app.ThemeModeDark }); err != nil {
			t.Fatal(err)
		}
	}
	main := newTestShell(t, onApp(a)).s
	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)

	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	// 音量拉到六成：填过的那一段**看得见**，才有得比。
	a.SetVolume(0.6)

	tt := miniTester(s)
	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return !miniCoverBox(tt).Empty()
	})
	save(t, dir, name, tt)
}

// saveRect 把画面上的一块矩形存成 PNG（顶栏那一段要放大才量得出墨迹）。
func saveRect(t *testing.T, dir, name string, tt *ui.Tester, x0, y0, w, h int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := tt.Image()
	b := img.Bounds()
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x0+w > b.Dx() {
		w = b.Dx() - x0
	}
	if y0+h > b.Dy() {
		h = b.Dy() - y0
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("要存的矩形是空的（%d×%d）", w, h)
	}
	sub := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(sub, sub.Bounds(), img, image.Pt(x0, y0), draw.Src)
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", filepath.Join(dir, name))
}
