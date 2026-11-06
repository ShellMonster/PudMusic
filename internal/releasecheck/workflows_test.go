package releasecheck

import (
	"strings"
	"testing"
)

// TestWindowsAACSpikeDoesNotCacheTheToolchain 钉住 Windows AAC spike 工作流
// 不再把 runner.tool_cache/go 存进 actions/cache。
//
// 曾经有一条「Cache Go toolchain」步骤缓存 `${{ runner.tool_cache }}/go`：
// 缓存命中时 tar 把 tool_cache 还原成符号链接（C:\hostedtoolcache →
// D:\hostedtoolcache），随后 setup-go 往同一位置写链接直接撞 EEXIST，
// go.mod 指定的版本装不上，整个 spike 在测试启动前就红了（PR #769 那次）。
// Go module / build 缓存由 setup-go 自带的 `cache: true` 负责，不碰
// tool_cache——所以这里断言：工作流里没有 `runner.tool_cache` 路径，
// 同时 setup-go 仍按 go.mod 装版本。
func TestWindowsAACSpikeDoesNotCacheTheToolchain(t *testing.T) {
	src := readRepoFile(t, ".github/workflows/windows-aac-spike.yml")
	if strings.Contains(src, "runner.tool_cache") {
		t.Error("windows-aac-spike.yml 又出现了 runner.tool_cache 路径——缓存 tool_cache 会让 setup-go 撞 EEXIST")
	}
	if !strings.Contains(src, "actions/setup-go@v5") {
		t.Error("windows-aac-spike.yml 没有用 actions/setup-go@v5 安装 Go")
	}
	if !strings.Contains(src, "go-version-file: go.mod") {
		t.Error("windows-aac-spike.yml 没有按 go.mod 装 Go 版本")
	}
}

// TestWindowsE2EWorkflowRunsTheWindowsTests 钉住 Windows E2E 工作流真的跑
// Windows 那一族测试，而且不再缓存 tool_cache（理由同上一条）。
//
// ⚠️ **要钉的两件事**：
//
//  1. 它跑 `TestWindowsLifecycle` / `TestWindowsFileAssociations`——少了
//     哪一条，那一格就是「写了测试没人跑」；
//  2. 不设 `BIU_RUN_MYGO_E2E=1` 的话，那些测试全会 skip——工作流绿着，
//     而「Windows 上有 E2E」是个谎。
func TestWindowsE2EWorkflowRunsTheWindowsTests(t *testing.T) {
	src := readRepoFile(t, ".github/workflows/windows-e2e.yml")
	if strings.Contains(src, "runner.tool_cache") {
		t.Error("windows-e2e.yml 又出现了 runner.tool_cache 路径——缓存 tool_cache 会让 setup-go 撞 EEXIST")
	}
	if !strings.Contains(src, "BIU_RUN_MYGO_E2E") {
		t.Error("windows-e2e.yml 没设 BIU_RUN_MYGO_E2E——E2E 测试全会 skip")
	}
	for _, test := range []string{
		"TestWindowsLifecycle",
		"TestWindowsFileAssociations",
	} {
		if !strings.Contains(src, test) {
			t.Errorf("windows-e2e.yml 没跑 %s", test)
		}
	}
}

// TestLinuxCIExcludesUnsupportedPackages 钉住 Linux CI 的测试包筛选：
// 最小可发行版本不实现 AAC/M4A（service/audio）、视频（service/video）、
// 随包字体（ui/widgets/typo），这三个包的测试在 Ubuntu runner 上确定性
// 失败。排除必须走一处有名字的列表（LINUX_UNSUPPORTED_TEST_PACKAGES），
// 不是把 grep 散落进 go test 命令行——否则下次有人「顺手跑全仓」就把
// 已知不支持的包又跑红了。
//
// 这里断的是**排除事实**：三个包路径都出现在筛选变量里，且 workflow
// 仍保留 build / vet / licensing / generate / resources-diff 其余检查。
// 能力真落地时的正确动作是从列表删行，不是改测试断言。
func TestLinuxCIExcludesUnsupportedPackages(t *testing.T) {
	src := readRepoFile(t, ".github/workflows/linux.yml")

	if !strings.Contains(src, "LINUX_UNSUPPORTED_TEST_PACKAGES") {
		t.Fatal("linux.yml 没有 LINUX_UNSUPPORTED_TEST_PACKAGES 列表——" +
			"Linux 未支持包的排除要写在一个有名字的变量里，不能散在 go test 命令行里")
	}
	for _, pkg := range []string{
		"github.com/ShellMonster/PudMusic/internal/service/audio",
		"github.com/ShellMonster/PudMusic/internal/service/video",
		"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo",
	} {
		if !strings.Contains(src, pkg) {
			t.Errorf("linux.yml 的排除列表里没有 %s——这个包在 Linux 上确定性失败，不能跑", pkg)
		}
	}

	// 排除列表必须真的参与过滤：go test 的包集合要经过它筛一遍，
	// 不能只是「列出来了但 go test 还在跑 ./... 或裸 go list」。
	if !strings.Contains(src, "grep -vxFf") {
		t.Error("linux.yml 的 go test 没有用 grep -vxFf 按排除列表精确过滤包")
	}

	// 其余验收一环不能少：排除是缩小测试面，不是拆掉验收。
	for _, check := range []string{
		"go build -trimpath -p 4 ./...",
		"go vet -trimpath -p 4 ./...",
		"go test ./internal/licensing/",
		"go generate ./internal/platform/ ./internal/licensing/",
		"git diff --exit-code -- resources",
	} {
		if !strings.Contains(src, check) {
			t.Errorf("linux.yml 的验收步骤里少了 %q", check)
		}
	}
}
