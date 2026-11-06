//go:build e2e

package e2e

// Windows 上的文件关联回归（票：AAC/M4A 由 windows.installerInclude 补回）。
//
// # 为什么要有这一族
//
// mygo.json 的 `fileAssociations` 从这一票起只留**全平台都放得起**的
// 格式——Windows 的 AAC/M4A 关联改由 `windows.installerInclude`
// （`resources/windows/fileassoc.nsh`）在**安装器**写注册表。
//
// ⇒ 后果是：Windows 上「双击一个 `.m4a`」之后，框架的 `fileArgs`
// **不会**把它投给 `OnOpenFile`（`packageFileExtensions` 不含 m4a）——
// 它要由 `handleLaunchArgs` 那一遍接住，再由 `checkOpenFile` 用
// `IsAssociatedAudioExt`（Windows 上有 m4a）收下。
//
// 这一族验的就是**那条缝没断**：
//
//  1. **`.m4a` 真的放起来**——命令行带它，窗口里要看见「正在放」；
//  2. **`.flac` 放起来**——基线，证明「放起来」这件事没坏；
//  3. **`.wma` 有一句「这个格式我们放不了」**——扫描白名单里有它、
//     而解码器没有，那一档必须有一句人话（不是静默没反应）。
//
// ⚠️ **验不到的那一半**：资源管理器**真的**把 `.m4a` 关联给我们这件事
// 要装过 NSIS 安装包才成立（`fileassoc.nsh` 写的注册表项），那一半在
// 这份 E2E 里验不到——验到的是「命令行把 .m4a 递进来时应用真的放」。
//
// ⚠️ **退出那一档**：Windows 的托盘菜单走 `plugins/automation` 的
// `menu` 方法（`windows.TestActivateMenuItem`）**吗？**——不，那条只走
// **窗口菜单栏**（`w.menubar`），而 Windows 的托盘是通知区图标（
// NotifyIcon），不是窗口菜单。和 Linux 同一个理由走 E2E 钩子
// （`automation_e2e.go` 的 `serveTrayHook` → `E2EActivateTrayItem`）。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestWindowsFileAssociations
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口
//（GitHub 的 `windows-2022` runner 上有交互桌面）。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestWindowsFileAssociations 钉住 Windows 上「AAC 收得下、FLAC 放得起来、
// 放不了的格式有句人话」。
func TestWindowsFileAssociations(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "windows" {
		t.Skip("这一族只在 Windows 上跑：验的是 Windows 的文件关联")
	}

	// ⚠️ **文件由「第二个实例」转交，不是冷启动参数**：理由同
	// `linux_fileassoc_test.go` 头上那一段——冷启动时窗口还没建出来，
	// 那句提示的 5 秒寿命可能在第一帧之前就耗尽。
	//
	// ⚠️ **素材目录挂在父测试上**：Windows 不许删一个**正被打开**的文件，
	// 而子测试末尾 app 还在播（`stop` 是父测试的 `defer`，要等全部子测试
	// 跑完才杀进程）。把 `TempDir` 挂在**父测试**上之后，cleanup 在
	// `stop` 收掉进程、内核放开句柄**之后**才跑；再配每个子测试末尾的
	// `playback.stop`（让 `App.ClearTrack` 当场放掉句柄），子测试自己的
	// 素材在退出前就已经不被占用了。
	tmp := t.TempDir()
	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	t.Run("一个 AAC 文件真的放起来", func(t *testing.T) {
		// ⚠️ **用真的 AAC 素材**：`EnqueueAndPlayNow` 真的会打开它——
		// 假文件会走 `reportOpenFileFailure`（「放不了」那句），那验的是
		// 另一件事（Windows 上的 Media Foundation 解不了一个假 MP4）。
		src := filepath.Join(repoRoot(t),
			"internal", "service", "audio", "mfadec", "testdata", "aaclc-stereo.m4a")
		m4a := filepath.Join(tmp, "歌.m4a")
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("读不到测试素材 %s：%v", src, err)
		}
		if err := os.WriteFile(m4a, b, 0o644); err != nil {
			t.Fatal(err)
		}
		handOverFile(t, client, window, m4a)
		// ⚠️ **先断「命令行进了第一实例」再断「那句话画出来了」**：
		// `handoff.args` 那条探针分清「handoff 断了」和「toast 没画出来」。
		waitForHandoffArgs(t, m4a)
		// ⚠️ **判据是「正在放」那句 toast**：收下并起播成功时
		// `handleOpenFile` 写它。这一句出现的意思是：
		// `fileArgs` 没投它（m4a 不在 mygo.json）→ `handleLaunchArgs`
		// 认下来 → `checkOpenFile` 走 `IsAssociatedAudioExt`（Windows
		// 上有 m4a）→ `EnqueueAndPlayNow` 起播成功。
		if !waitForTreeNudged(t, client, window, "正在放", 30*time.Second) {
			t.Fatalf("双击 .m4a 之后窗口里没有「正在放」：\n%s", treeDump(t, client, window))
		}
		stopPlayback(t)
	})

	t.Run("一个 FLAC 文件真的放起来", func(t *testing.T) {
		// ⚠️ **基线**：FLAC 是「全平台都放得起」里那一档的代表——它在
		// `mygo.json` 的 `fileAssociations` 里（框架 `fileArgs` 会投），
		// 也在 `internal/service/audio/flacdec` 里有自己的解码器。它放不
		// 起来的话，「放起来」这件事本身就坏了，和 AAC 那条缝没关系。
		//
		// ⚠️ **用真的 FLAC 素材**：`EnqueueAndPlayNow` 真的会打开它，
		// 假文件会走 `reportOpenFileFailure`——那验的是另一件事。
		src := filepath.Join(repoRoot(t),
			"internal", "service", "audio", "flacdec", "testdata", "stereo16.flac")
		flac := filepath.Join(tmp, "歌.flac")
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("读不到测试素材 %s：%v", src, err)
		}
		if err := os.WriteFile(flac, b, 0o644); err != nil {
			t.Fatal(err)
		}
		handOverFile(t, client, window, flac)
		waitForHandoffArgs(t, flac)
		if !waitForTreeNudged(t, client, window, "正在放", 20*time.Second) {
			t.Fatalf("双击 .flac 之后窗口里没有「正在放」：\n%s", treeDump(t, client, window))
		}
		stopPlayback(t)
	})

	t.Run("一个 WMA 文件有一句人话", func(t *testing.T) {
		// 扫描白名单里有 `.wma`（`localmusic.Extensions`），但我们放不了
		// ——双击它要有一句「这个格式我们放不了」，而不是「应用起来了什么
		// 也没发生」。
		wma := filepath.Join(t.TempDir(), "老歌.wma")
		if err := os.WriteFile(wma, []byte("ASF fake"), 0o644); err != nil {
			t.Fatal(err)
		}
		handOverFile(t, client, window, wma)
		waitForHandoffArgs(t, wma)
		if !waitForTreeNudged(t, client, window, "这个格式我们放不了", 20*time.Second) {
			t.Fatalf("双击 .wma 之后窗口里没有那句「放不了」：\n%s", treeDump(t, client, window))
		}
	})
}
