package licensing

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// 这一组钉的是「Windows 上的音频解码没有偷偷带进新的第三方代码」。
//
// # 为什么值得单立一条
//
// `TestTheInventoryListsExactlyWhatGetsCompiled` 已经会拿 windows/amd64 和
// windows/arm64 各跑一遍 `go list -deps`，所以「Windows 构建图里多了某个模块
// 而清单里没有」这件事**它本来就拦得住**。这一组拦的是另一头：
//
// **清单里有、但 Windows 音频那一包自己带进来的。** 换个说法：假如哪天有人
// 为了图省事，在 `internal/service/audio/mfadec/` 里 import 了一个新的解码器
// 库（`go-aac` 那类 LGPL 的、或者随便什么别的），并且顺手把许可证加进了清单
// ——那么上面那条会**全绿**（清单和构建图确实对得上），而「本项目不含任何
// LGPL 代码」那句话已经在报告和 NOTICE 里发出去了。
//
// 所以这里直接问那个包：「你到底 import 了什么？」并且把答案**写死**成
// purego + MyGo 两个。将来想加第三个，这里会红，而且红的地方写清楚了原因。
//
// # 这条为什么是「加依赖」这件事的第一道闸
//
// Media Foundation 是 **Windows 自带的系统组件**，不是第三方库：不静态链接
// 进我们的二进制（是运行时的 `LoadLibrary` + 系统函数调用），也不需要随包
// 附它的许可证。Biu 本体是 PolyForm-Noncommercial 1.0.0，那个许可和 LGPL
// 不兼容，而 PLAN §6.1 的 B 方案（引 LGPL 的纯 Go AAC 解码器）正是为了绕开
// 才有 A 方案。引一个 LGPL 模块进来就得先回去改项目的不变量，
// 那不是工程决定。

// windowsAudioAllowlist 是 `internal/service/audio/mfadec` 在 Windows 上
// **允许**依赖的外部模块，一个不多。
//
// go-m4a 是拆 MP4 容器的（MIT，已在 go.mod 里，容器层早就在用）。
// **纯 Go 标准库之外就它一个**——COM 那部分用的是标准库的
// `syscall.SyscallN`，没有引 purego。
//
// ⚠️ 这一条曾经写的是「purego + mygo」：那是 `IMFSourceReader` 那条路
// （BMG-68）的依赖。换成 `IMFTransform` 之后 purego 用不上了（COM 虚表
// 走标准库的 `syscall.SyscallN`），mygo 也不在这一包里（spike 还没接进
// 产品的解码器接口）——所以现在只剩 go-m4a。
var windowsAudioAllowlist = []string{
	"github.com/tphakala/go-m4a",
}

// TestWindowsAudioPullsInNothingNew 断 Windows 音频那一包只依赖上面那两个。
func TestWindowsAudioPullsInNothingNew(t *testing.T) {
	root := repoRoot(t)
	pkg := filepath.Join(root, "internal", "service", "audio", "mfadec")

	for _, target := range []struct{ GOOS, GOARCH string }{
		{"windows", "amd64"},
		{"windows", "arm64"},
	} {
		mods, err := BuildModules(context.Background(), pkg, os.Environ(), target.GOOS, target.GOARCH)
		if err != nil {
			t.Fatalf("go list -deps（%s/%s）：%v", target.GOOS, target.GOARCH, err)
		}

		got := make([]string, 0, len(mods))
		for _, m := range mods {
			got = append(got, m.Path)
		}
		slices.Sort(got)

		if !slices.Equal(got, windowsAudioAllowlist) {
			t.Errorf("%s/%s 上 mfadec 依赖的是 %v，只允许 %v。\n"+
				"多出来的每一个都是**静态链接进可执行文件的第三方代码**，意味着：\n"+
				"  ① 它的许可证必须随包发出去（`go generate ./internal/licensing/`）\n"+
				"  ② 引入 LGPL 的话要回去改 Plan §4 R13 那条不变量，"+
				"而那不是工程决定\n"+
				"  ③ Media Foundation 是 Windows 自带的系统组件，"+
				"本来**不需要**任何第三方代码就能解 AAC",
				target.GOOS, target.GOARCH, got, windowsAudioAllowlist)
		}
	}
}

// TestEveryAllowedDependencyShipsItsLicense 断上面那两个的许可证原文确实在包里。
//
// 上一条只管「不多引」，这一条管「引了的那两个发得出去」——两件事都会让用户
// 手里缺一份该有的许可证，而两件事都不会让构建失败。
func TestEveryAllowedDependencyShipsItsLicense(t *testing.T) {
	declared := map[string]bool{}
	for _, e := range loadManifest(t) {
		declared[e.Module] = true
	}
	for _, m := range windowsAudioAllowlist {
		if !declared[m] {
			t.Errorf("%s 是 Windows 音频路径的依赖，但清单里没有它的许可证"+
				"（跑 `go generate ./internal/licensing/`）", m)
		}
	}
}

// TestNothingWeLinkIsCopyleftAndWindowsIsNoException 把上面那条和 R13 放在一起。
//
// `TestNoDependencyIsCopyleft` 走的是**清单**（发出去的那份文件），这一条走
// **构建图**。两边要都对得上才算数：清单漏了、构建图引了，清单那条会安静地
// 放过，而用户拿到的可执行文件里确实有那份代码。
func TestNothingWeLinkIsCopyleftAndWindowsIsNoException(t *testing.T) {
	root := repoRoot(t)
	for _, target := range []struct{ GOOS, GOARCH string }{
		{"windows", "amd64"},
		{"windows", "arm64"},
	} {
		mods, err := BuildModules(context.Background(), root, os.Environ(), target.GOOS, target.GOARCH)
		if err != nil {
			t.Fatalf("go list -deps（%s/%s）：%v", target.GOOS, target.GOARCH, err)
		}
		for _, m := range mods {
			if IsCopyleft(spdxOf(t, m.Dir)) {
				t.Errorf("%s@%s 是带传染性的许可——这推翻了 Plan §4 R13 的"+
					"「一行 LGPL 都不用」，而那句话已经写在随包的清单里了", m.Path, m.Version)
			}
		}
	}
}

// spdxOf 读一个模块目录里那份许可证，认出它的 SPDX 标识。
func spdxOf(t *testing.T, dir string) string {
	t.Helper()
	_, text, err := LicenseFile(dir)
	if err != nil {
		t.Fatalf("%s：%v", dir, err)
	}
	id, err := DetectSPDX(text)
	if err != nil {
		t.Fatalf("%s：认不出许可证：%v", dir, err)
	}
	return id
}
