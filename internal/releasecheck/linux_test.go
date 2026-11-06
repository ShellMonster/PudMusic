// Package releasecheck 把「Linux 进首发目标」这件事钉成会红的断言。
//
// # 为什么单独一包
//
// 发行目标散在四处：`scripts/build.sh` 的 ALL_TARGETS、`verify-release-assets.sh`
// 的目标白名单与包名断言、`rename-release-assets.sh` 的命名表，以及 `mygo.json`
// 的 linux 段。任何一处漏掉 linux，构建流程都不报错——只是 release 上少一份
// Linux 资产，要等人下载时才发现。
//
// 这里断的是**文本事实**：这几份脚本里该出现的目标、标签和包名都在。行为面
// （mygo build 真正打出什么）由 CI 的 Ubuntu job 验，本包只做离线检查。
package releasecheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("拿不到工作目录：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("从工作目录往上找不到 go.mod")
		}
		dir = parent
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读 %s：%v", rel, err)
	}
	return string(b)
}

// TestMygoJSONDeclaresNativeUILinux 钉住「Linux 产物不拉 WebKitGTK」。
//
// MyGo 的打包器默认把 libwebkit2gtk-4.1-0 写进 deb 的 Depends 和 PKGBUILD 的
// depends——给「窗口里要放 webview」的默认假设。本应用是原生 UI，不需要
// WebKitGTK；`linux.webview: false` 是打包器认的那一个开关
// （`cmd/mygo/deb.go` 的 `Linux.WebView`）。
//
// 不配它的话 deb/PKGBUILD 会硬拉一个几十 MB、而应用永远不会加载的依赖。
func TestMygoJSONDeclaresNativeUILinux(t *testing.T) {
	raw := readRepoFile(t, "mygo.json")
	var cfg struct {
		Linux *struct {
			WebView *bool `json:"webview"`
		} `json:"linux"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("mygo.json 解析不了：%v", err)
	}
	if cfg.Linux == nil {
		t.Fatal("mygo.json 没有 linux 段：deb/PKGBUILD 会按默认（webview）拉 libwebkit2gtk")
	}
	if cfg.Linux.WebView == nil || *cfg.Linux.WebView {
		t.Fatalf("mygo.json 的 linux.webview 是 %v：原生 UI 应用要配 false，"+
			"否则 deb/PKGBUILD 会硬拉 libwebkit2gtk-4.1-0", cfg.Linux.WebView)
	}
}

// linuxTargets 是首发要带的两个 Linux 目标。
var linuxTargets = []string{"linux/amd64", "linux/arm64"}

// TestBuildScriptListsLinuxTargets 钉住 build.sh 的默认目标表带上 Linux。
func TestBuildScriptListsLinuxTargets(t *testing.T) {
	src := readRepoFile(t, "scripts/build.sh")
	for _, target := range linuxTargets {
		if !strings.Contains(src, target) {
			t.Errorf("scripts/build.sh 的默认目标表里没有 %s", target)
		}
	}
}

// linuxPlatformLabel 是 rename/verify 两个脚本共用的资产名标签。
var linuxPlatformLabel = map[string]string{
	"linux/amd64": "Linux_x86_64",
	"linux/arm64": "Linux_ARM64",
}

// TestRenameScriptMapsLinuxTargets 钉住改名表认识 Linux 的两种包。
//
// Linux 每目标产两类用户可见文件：
//
//   - `pudmusic-<ver>-linux-<arch>.tar.gz` → `PudMusic-<ver>-<label>.tar.gz`
//   - `pudmusic_<ver>_<arch>.deb`        → `PudMusic-<ver>-<label>.deb`
//
// 改名表少一条，产物就留着 mygo 原名混进 release——和 darwin/windows 的
// 品牌名排在一起，一眼就是「这条漏了」。
func TestRenameScriptMapsLinuxTargets(t *testing.T) {
	src := readRepoFile(t, "scripts/rename-release-assets.sh")
	for target, label := range linuxPlatformLabel {
		if !strings.Contains(src, target) && !strings.Contains(src, strings.ReplaceAll(target, "/", "_")) {
			t.Errorf("rename-release-assets.sh 没有处理 %s", target)
		}
		if !strings.Contains(src, label) {
			t.Errorf("rename-release-assets.sh 的标签表里没有 %s", label)
		}
	}
	if !strings.Contains(src, ".deb") {
		t.Error("rename-release-assets.sh 没有处理 .deb 安装包")
	}
}

// TestVerifyScriptAcceptsLinuxTargets 钉住验产物的白名单和包名规则。
func TestVerifyScriptAcceptsLinuxTargets(t *testing.T) {
	src := readRepoFile(t, "scripts/verify-release-assets.sh")
	for target, label := range linuxPlatformLabel {
		if !strings.Contains(src, target) {
			t.Errorf("verify-release-assets.sh 的目标白名单没有 %s", target)
		}
		if !strings.Contains(src, label) {
			t.Errorf("verify-release-assets.sh 的标签表里没有 %s", label)
		}
	}
	// Linux 的安装包断言要认 .deb——首发打的是 deb。
	if !strings.Contains(src, ".deb") {
		t.Error("verify-release-assets.sh 没有断言 Linux 的 .deb 安装包")
	}
}

// TestWorkflowBuildsLinuxOnUbuntu 钉住 CI 里有 Ubuntu 上的构建与产物验证。
func TestWorkflowBuildsLinuxOnUbuntu(t *testing.T) {
	root := repoRoot(t)
	ents, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		t.Fatalf("读 .github/workflows：%v", err)
	}
	var found bool
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", e.Name()))
		if err != nil {
			continue
		}
		src := string(b)
		if strings.Contains(src, "runs-on: ubuntu") && strings.Contains(src, "linux/amd64") {
			found = true
		}
	}
	if !found {
		t.Error(".github/workflows 里没有「ubuntu runner 上构建 linux/amd64」的 job")
	}
}

// TestLinuxTargetsStaySortedInScripts 钉住 ALL_TARGETS 的写法：一处列表、
// 按平台字母序，linux 排在 windows 之前。
//
// 这条只是防止将来有人把 linux 塞错位置或塞成第二份列表——三份脚本里的
// ALL_TARGETS 必须一眼是同一份。
func TestLinuxTargetsStaySortedInScripts(t *testing.T) {
	for _, rel := range []string{"scripts/build.sh", "scripts/verify-release-assets.sh"} {
		src := readRepoFile(t, rel)
		m := regexp.MustCompile(`(?s)ALL_TARGETS=\(([^)]*)\)`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s 里没有 ALL_TARGETS=( … )", rel)
		}
		fields := strings.Fields(m[1])
		var prev string
		for _, f := range fields {
			if prev != "" && f < prev {
				t.Errorf("%s 的 ALL_TARGETS 没按字母序：%q 在 %q 之前", rel, f, prev)
			}
			prev = f
		}
	}
}
