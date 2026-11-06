//go:build e2e

package e2e

// Linux 上的文件关联回归（票：Linux 最小可发行不登记 AAC/M4A）。
//
// # 为什么单独一族
//
// mygo.json 的 `fileAssociations` 从这一票起只留**全平台都放得起**的
// 格式——Linux 上没有 AAC 解码器（`extaf` 只在 darwin、`mfadec` 只在
// windows），把 `.m4a` / `.aac` 登记给桌面环境的话，`pudmusic.desktop`
// 的 `MimeType=` 会带着 `application/x-pudmusic-m4a`，系统把 `.m4a`
// 列入本应用的「打开方式」，用户双击得到的只有「这个格式我们放不了」。
//
// 所以这一族验的是**用户看得见的那一半**：
//
//  1. **`.m4a` 收不下来时，界面上有一句「这个格式我们放不了」**——命令行
//     参数是桌面环境交给我们的**唯一**入口（Linux 没有 Apple Event），
//     而框架的 `fileArgs` 不投它（`packageFileExtensions` 不含 m4a），
//     于是由 `handleLaunchArgs` 那一遍接住，`checkOpenFile` 报
//     `openFileUnsupported`，`sayFileNotice` 把那句话画出来。
//  2. **`.flac` 收得下来**——它是「全平台都放得起」的格式，框架投给
//     `OnOpenFile`，界面要看见「正在放」。
//
// ⚠️ **「看不见」和「没有」在用户那里是同一件事**：这句话落在共享的
// `fileNotice` 上（`internal/ui/filenotice.go`），它自己 `repaint`——
// 而按需绘制的界面上「下一个别的原因」可能一直没有。所以这里**不另外
// 画帧**：等的是「那句话出现在元素树里」，而不是「等了几帧」。
//
// ⚠️ **验不到的那一半**：desktop entry 文件本身的 `MimeType=` 在
// `mygo build` 那一步就定死了，它由 `internal/platform` 的
// `TestMygoJSONKeepsAACOutOfTheLinuxDesktopEntry` 钉；这一族验的是
// **命令行进来的那一半**。
//
// # 怎么跑
//
//	BIU_RUN_MYGO_E2E=1 go test -tags e2e ./internal/e2e/ -run TestLinuxFileAssociations
//
// ⚠️ **要一个真窗口**：它开一个窗口并驱动它，所以本机得能开窗口
//（Linux 下可以是 Xvfb）。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLinuxFileAssociations 钉住 Linux 上「放不了的格式有句人话、放得了
// 的格式真放起来」。
func TestLinuxFileAssociations(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}
	if runtime.GOOS != "linux" {
		t.Skip("这一族只在 Linux 上跑：验的是 Linux 的文件关联")
	}

	// ⚠️ **文件由「第二个实例」转交，不是冷启动参数**：冷启动那一段
	// `handleLaunchArgs` 跑在应用 ready、**窗口还没建出来**的时候——那句
	// 提示的寿命是 5 秒（`fileNoticeLifetime`），而 Xvfb 下 GTK 初始化
	// 加上字体加载把第一帧拖过那五秒是常事，树里就永远查不到那句话。
	// 真实场景里用户双击文件时应用多半**已经在跑**（命令行经单实例锁
	// 转交，走 `OnSecondInstance` → `handleLaunchArgs`）——那条路上窗口
	// 已经在了，提示一定画进帧里。
	client, stop := startRealApp(t)
	defer stop()
	window := firstContentWindow(t, client)

	t.Run("一个不支持的 AAC 文件有一句人话", func(t *testing.T) {
		// 造一个假的 .m4a——`checkOpenFile` 只看「在不在 + 是不是普通
		// 文件 + 扩展名认不认」，**不真解**（解不开那一档是
		// `reportOpenFileFailure`，不是这里要验的）。
		m4a := filepath.Join(t.TempDir(), "歌.m4a")
		if err := os.WriteFile(m4a, []byte("not really aac"), 0o644); err != nil {
			t.Fatal(err)
		}
		handOverFile(t, m4a)
		// ⚠️ **判据是「那句话画在画面里」**：它从 `fileNotice` 那一份读，
		// 而那份在主窗口和迷你窗**各画一遍**——等它出现就行。
		//
		// 「这个格式我们放不了」是 `openFileRejectText` 对
		// `openFileUnsupported` 写的那句；Linux 上 `.m4a` 不在
		// `IsAssociatedAudioExt` 里，走到那一档。
		if !waitForTree(t, client, window, "这个格式我们放不了", 20*time.Second) {
			t.Fatalf("双击 .m4a 之后窗口里没有那句「放不了」：\n%s", treeDump(t, client, window))
		}
	})

	t.Run("一个 FLAC 文件真的放起来", func(t *testing.T) {
		// ⚠️ **用真的 FLAC 素材**：`EnqueueAndPlayNow` 真的会打开它，
		// 假文件会走 `reportOpenFileFailure`——那验的是另一件事。
		src := filepath.Join(repoRoot(t),
			"internal", "service", "audio", "flacdec", "testdata", "stereo16.flac")
		flac := filepath.Join(t.TempDir(), "歌.flac")
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("读不到测试素材 %s：%v", src, err)
		}
		if err := os.WriteFile(flac, b, 0o644); err != nil {
			t.Fatal(err)
		}
		handOverFile(t, flac)
		// ⚠️ **判据是「正在放」那句 toast**：收下并起播成功时
		// `handleOpenFile` 写它（`internal/ui/openfile.go`）。它是一条
		// toast——主窗口上画。
		if !waitForTree(t, client, window, "正在放", 20*time.Second) {
			t.Fatalf("双击 .flac 之后窗口里没有「正在放」：\n%s", treeDump(t, client, window))
		}
	})
}
