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
