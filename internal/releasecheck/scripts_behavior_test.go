package releasecheck

// 发行脚本（rename / verify）的**行为**断言：不是「脚本里写着某个名字」，
// 而是「拿一份 mygo build 的产物喂进去，出来的是用户真会拿到的名字」。
//
// # 为什么要有这一族
//
// linux_test.go 里那几条 `strings.Contains` 断的是**文本**——脚本的
// 目标表、标签表里有那几个字。它断不到的是**脚本跑起来产出什么**：
// rename 把 tar.gz 改成了 `update-linux-amd64.tar.gz` 时，每一个
// `Contains(".deb")` 都还绿着，而用户下载 `install.sh` + 同目录的
// tar.gz 之后 `sh install.sh` 找不到 `pudmusic-<ver>-linux-amd64.tar.gz`
// ——那是评审在 PR #769 上抓到的不一致。
//
// ⇒ 这一族在 t.TempDir 里搭一份**假 mygo build 产物**（build/<target>/ 下
// 该有的文件名 + 一份最小的 mygo.json），真跑 `bash rename-release-assets.sh`
// 和 `bash verify-release-assets.sh`，断的是**落盘的文件名**和**清单里的
// url**——和 release.yml 在 CI 上跑的是同一份脚本。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot 搭一份「mygo build 刚打完」的目录树：
//
//	<tmp>/mygo.json            —— slug + version（两个脚本都从这里读）
//	<tmp>/scripts/             —— 要跑的两份脚本（从仓库里复制进来）
//	<tmp>/build/linux-amd64/   —— mygo 原样产物：pudmusic-1.0.2-linux-amd64.tar.gz、
//	                             update-linux-amd64.json、pudmusic_1.0.2_amd64.deb、
//	                             install.sh、PKGBUILD
//
// ⚠️ **为什么是复制而不是指回仓库**：两个脚本第一句都是
// `cd "$(dirname "$0")/.."`——把它们放进 <tmp>/scripts/ 之后 `..` 就是
// <tmp>，它们读到的 mygo.json 和 build/ 就都是 fixture 的，不会碰真仓库。
func fixtureRoot(t *testing.T, targets ...string) string {
	t.Helper()
	root := t.TempDir()

	// mygo.json：脚本只读 slug / version / out 三键，给最小一份。
	writeJSON(t, filepath.Join(root, "mygo.json"), map[string]any{
		"slug":    "pudmusic",
		"version": "1.0.2",
		"out":     "build",
	})

	// 把仓库里的两份脚本复制进 fixture 的 scripts/，内容是**仓库当前版本**
	// ——断的就是它们本身，不是抄出来的一份。
	repo := repoRoot(t)
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("建 scripts 目录：%v", err)
	}
	for _, name := range []string{"rename-release-assets.sh", "verify-release-assets.sh"} {
		src := filepath.Join(repo, "scripts", name)
		dst := filepath.Join(scriptsDir, name)
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("读 %s：%v", src, err)
		}
		if err := os.WriteFile(dst, b, 0o755); err != nil {
			t.Fatalf("写 %s：%v", dst, err)
		}
	}

	// 每个 Linux 目标一份「mygo 原样」产物。名字按 mygo@v0.7.4
	// `cmd/mygo/updates.go` 的 writeArchive 和 deb.go 出：
	//   tar.gz    pudmusic-1.0.2-linux-amd64.tar.gz
	//   manifest  update-linux-amd64.json（url 指着上面那个 tar.gz）
	//   deb       pudmusic_1.0.2_amd64.deb
	for _, target := range targets {
		dir := filepath.Join(root, "build", strings.ReplaceAll(target, "/", "-"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("建 %s：%v", dir, err)
		}
		arch := target[strings.LastIndex(target, "/")+1:]
		archive := "pudmusic-1.0.2-" + strings.ReplaceAll(target, "/", "-") + ".tar.gz"
		touch(t, filepath.Join(dir, archive))
		touch(t, filepath.Join(dir, "pudmusic_1.0.2_"+arch+".deb"))
		touch(t, filepath.Join(dir, "install.sh"))
		touch(t, filepath.Join(dir, "PKGBUILD"))
		// ⚠️ manifest 的 url 按 mygo `updateFile` 的形状写：尾段是 tar.gz
		// 原名。rename 脚本要做的就是**保持**它指向落盘的那个文件。
		writeJSON(t, filepath.Join(dir, "update-"+strings.ReplaceAll(target, "/", "-")+".json"), map[string]any{
			"version":   "1.0.2",
			"url":       "https://github.com/ShellMonster/PudMusic/releases/download/v1.0.2/" + archive,
			"size":      123,
			"signature": "AAAA",
		})
	}
	return root
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("写 %s：%v", path, err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化 %s：%v", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("写 %s：%v", path, err)
	}
}

func runScript(t *testing.T, root, script string, args ...string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", script)}, args...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v 失败：%v\n%s", script, args, err, out)
	}
}

// TestLinuxTarballKeepsTheNameInstallShExpects 断 **Linux tar.gz 改名之后
// 还是 `pudmusic-<ver>-linux-<arch>.tar.gz`**——`install.sh` 在同目录找的、
// `PKGBUILD` 的 `source` 里写的就是这个名字。
//
// ⚠️ **为什么不能改成 `update-linux-*.tar.gz`**：那是评审在 PR #769 上
// 抓到的不一致。mygo 的 Linux tar.gz 是「更新归档」和「用户自装包」
// **同一份文件**，而 `install.sh` 只认 `$name-$version-$target.tar.gz`；
// 改成 update-* 之后 install.sh 同目录找不到它，`PKGBUILD` 的 `source`
// 也找不到它——两条 Linux 安装路径一起断。
func TestLinuxTarballKeepsTheNameInstallShExpects(t *testing.T) {
	for _, target := range []string{"linux/amd64", "linux/arm64"} {
		t.Run(target, func(t *testing.T) {
			root := fixtureRoot(t, target)
			runScript(t, root, "rename-release-assets.sh", target)

			dir := filepath.Join(root, "build", strings.ReplaceAll(target, "/", "-"))
			want := "pudmusic-1.0.2-" + strings.ReplaceAll(target, "/", "-") + ".tar.gz"
			if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
				ents, _ := os.ReadDir(dir)
				var names []string
				for _, e := range ents {
					names = append(names, e.Name())
				}
				t.Fatalf("rename 之后 %s 不在了（目录里是 %v）：install.sh 在同目录找的就是它", want, names)
			}
			// 清单的 url 要指着**落盘的那份**——客户端按它去拉资产。
			manifest := readFile(t, filepath.Join(dir, "update-"+strings.ReplaceAll(target, "/", "-")+".json"))
			if !strings.Contains(manifest, "/"+want) {
				t.Errorf("update manifest 的 url 不指 %s：\n%s", want, manifest)
			}
		})
	}
}

// TestVerifyScriptPassesOnRenamedLinuxArtifacts 断 verify 认的是「rename
// 跑完之后真在盘上的那套名字」——它和 rename 是一条流水线上的两步
// （release.yml 先 rename 再 verify），一个改了名另一个还按旧名找，就是
// release 上少一份资产。
func TestVerifyScriptPassesOnRenamedLinuxArtifacts(t *testing.T) {
	root := fixtureRoot(t, "linux/amd64", "linux/arm64")
	runScript(t, root, "rename-release-assets.sh", "linux/amd64", "linux/arm64")
	runScript(t, root, "verify-release-assets.sh", "linux/amd64", "linux/arm64")
}

// TestLinuxDebGetsTheBrandName 断 `.deb` 改名后是品牌名
// `PudMusic-<ver>-Linux_<arch>.deb`——和 darwin/windows 的安装包排在一起，
// 留在 mygo 原名（`pudmusic_1.0.2_amd64.deb`）就是「这条漏了」。
func TestLinuxDebGetsTheBrandName(t *testing.T) {
	for target, label := range map[string]string{
		"linux/amd64": "Linux_x86_64",
		"linux/arm64": "Linux_ARM64",
	} {
		root := fixtureRoot(t, target)
		runScript(t, root, "rename-release-assets.sh", target)
		dir := filepath.Join(root, "build", strings.ReplaceAll(target, "/", "-"))
		want := "PudMusic-1.0.2-" + label + ".deb"
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			ents, _ := os.ReadDir(dir)
			var names []string
			for _, e := range ents {
				names = append(names, e.Name())
			}
			t.Errorf("%s: rename 之后没有 %s（目录里是 %v）", target, want, names)
		}
	}
}

// TestInstallShFindsTheArchiveNextToIt 断**用户拿到的那条路**：下载
// `install.sh` + 同目录的 tar.gz，`sh install.sh <archive>` 里它先找的
// 就是 `$name-$version-$target.tar.gz`。
//
// ⚠️ **不真跑 install.sh**：它要真解包、真写 ~/.local——那是 CI 的事。
// 这一档只断**文件名层面的契约**：rename 之后 install.sh 旁边那份
// tar.gz 的名字 = install.sh 会去找的名字。两边都是
// `pudmusic-<ver>-linux-<arch>.tar.gz`，断了这一头那一头自然对得上。
func TestInstallShFindsTheArchiveNextToIt(t *testing.T) {
	root := fixtureRoot(t, "linux/amd64")
	runScript(t, root, "rename-release-assets.sh", "linux/amd64")
	dir := filepath.Join(root, "build", "linux-amd64")

	// install.sh 的查找式是 `$name-$version-$target.tar.gz`，其中
	// name=slug、version=version、target=linux-amd64。
	for _, want := range []string{"pudmusic-1.0.2-linux-amd64.tar.gz", "install.sh"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("安装入口要的 %s 不在产物目录里", want)
		}
	}
	// 不能「只有 update-*.tar.gz 而没有 install.sh 要的那份」——那是
	// 评审抓到的不一致。update-* 存在与否不管，install.sh 认的那份必须在。
	if _, err := os.Stat(filepath.Join(dir, "pudmusic-1.0.2-linux-amd64.tar.gz")); err != nil {
		t.Error("缺 pudmusic-1.0.2-linux-amd64.tar.gz：install.sh 在同目录找不到归档")
	}
}

// readFile 读 fixture 里的文件。
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return string(b)
}
