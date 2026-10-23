// gen-notices 生成 `resources/licenses/`：清单 + 每个依赖的许可证原文 + Biu 的许可证。
//
//	go generate ./internal/licensing/
//
// ⚠️ **它核的是构建产物，不是 go.mod。** 跑的是 `go list -deps`，而且几个目标
// 各跑一遍（darwin/arm64、darwin/amd64、windows/amd64、windows/arm64 的依赖图
// 不完全一样，只看本机这一个会漏）。
//
// 生成的每一份许可证原文都是从模块缓存里**逐字节复制**的：手抄一遍 Apache-2.0
// 抄错一个字，Apache §4(a) 给用户的「许可证副本」就不成立了，而没有任何工具会
// 发现。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/licensing"
)

// genTargets 是要核一遍的构建目标。
//
// 只列首发支持的两个平台（Plan §5.1 第 2 条：Linux 首发不做）。加平台就往这里
// 加一条——`licensing_test.go` 的门照同样的清单核，两边要一起改。
var genTargets = []struct{ GOOS, GOARCH string }{
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"windows", "amd64"},
	{"windows", "arm64"},
}

// collected 是收集到的一次核对结果。
type collected struct {
	Entries []licensing.Entry
	// Texts 是每条对应的许可证原文，按清单里的文件名索引。
	Texts map[string][]byte
}

// Package 是要写进仓库的一个文件：相对仓库根的路径 + 内容。
type Package struct {
	Path    string
	Content []byte
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-notices:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	got, err := collect(root)
	if err != nil {
		return err
	}

	files := []Package{{Path: licensing.NoticesRelPath, Content: licensing.FormatNotices(got.Entries)}}
	for _, e := range got.Entries {
		files = append(files, Package{
			Path:    filepath.ToSlash(filepath.Join(filepath.Dir(licensing.NoticesRelPath), e.File)),
			Content: got.Texts[e.File],
		})
	}

	// Biu 的许可证原文。它不是构建依赖（是这个项目的**上游**），但 PolyForm 的
	// Notices 一节要求随包分发，见 licensing.BiuLicenseRelPath。
	biu, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		return fmt.Errorf("读仓库根的 LICENSE（Biu 的许可证原文）：%w", err)
	}
	files = append(files, Package{Path: licensing.BiuLicenseRelPath, Content: biu})

	// 资源的许可证（图标那一批）和 vendor 进来的第三方代码那份许可证：它们
	// 不是构建依赖，所以不在上面那张表里，但同样要随包发出去。**必须排进
	// files**——否则 prune 会把它们当过期文件删掉，而删掉一份资源的许可证不会
	// 让任何构建失败。
	//
	// ⚠️ **源头要问对**：vendor 那一份的源头是仓库里 vendor 的那份原文
	// （`AssetLicenseSources`），不是「发出去的那一份」自己。拿复制品复制
	// 复制品的话，哪天手抄错了，没有任何东西会发现。
	for _, rel := range licensing.AssetLicenseRelPaths {
		src := licensing.AssetLicenseSources[rel]
		if src == "" {
			src = rel // 源头就是它自己（图标那一份）
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(src)))
		if err != nil {
			return fmt.Errorf("读 %s（%s 的原文）：%w", src, rel, err)
		}
		files = append(files, Package{Path: rel, Content: b})
	}

	// 先写完再删多余的：中途失败不会留下一个空目录假装「生成过了」。
	licensesDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(licensing.NoticesRelPath)))
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return err
		}
		fmt.Printf("  %s（%d 字节）\n", f.Path, len(f.Content))
	}
	if err := prune(licensesDir, files); err != nil {
		return err
	}
	fmt.Printf("写了 %d 份（%d 个依赖）\n", len(files), len(got.Entries))
	return nil
}

// prune 删掉 licenses 目录里不在 files 里的文件。
//
// 依赖被删掉之后，它的许可证原文必须跟着消失——留着它不是「多一份没坏处」，
// 而是清单说没有它、目录里却有，而 `licensing_test.go` 正好要断这件事。
func prune(dir string, files []Package) error {
	want := map[string]bool{}
	for _, f := range files {
		if strings.HasPrefix(f.Path, filepath.ToSlash(filepath.Dir(licensing.NoticesRelPath))+"/") {
			want[filepath.Base(filepath.FromSlash(f.Path))] = true
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() || want[e.Name()] {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.Remove(p); err != nil {
			return err
		}
		fmt.Printf("  删了过期的 %s\n", filepath.ToSlash(filepath.Join(filepath.Dir(licensing.NoticesRelPath), e.Name())))
	}
	return nil
}

// collect 核一遍所有目标，数出依赖集合，认出每个的许可证，返回清单和原文。
func collect(root string) (*collected, error) {
	seen := map[string]licensing.Module{}
	for _, t := range genTargets {
		mods, err := licensing.BuildModules(context.Background(), root, os.Environ(), t.GOOS, t.GOARCH)
		if err != nil {
			return nil, err
		}
		for _, m := range mods {
			if prev, ok := seen[m.Path]; ok && prev.Version != m.Version {
				return nil, fmt.Errorf("%s 在 %s/%s 和别的目标上是两个版本（%s）",
					m.Path, t.GOOS, t.GOARCH, prev.Version)
			}
			seen[m.Path] = m
		}
	}

	out := &collected{Texts: map[string][]byte{}}
	for _, m := range sortedModules(seen) {
		_, content, err := licensing.LicenseFile(m.Dir)
		if err != nil {
			return nil, fmt.Errorf("%s@%s：%w", m.Path, m.Version, err)
		}
		spdx, err := licensing.DetectSPDX(content)
		if err != nil {
			return nil, fmt.Errorf("%s@%s：%w", m.Path, m.Version, err)
		}
		name := licenseFileName(m.Path)
		out.Entries = append(out.Entries, licensing.Entry{
			Module: m.Path, Version: m.Version, SPDX: spdx, File: name,
		})
		out.Texts[name] = content
	}
	return out, nil
}

// sortedModules 把 map 按路径排好，让清单的顺序稳定（否则每次生成 diff 都乱）。
func sortedModules(m map[string]licensing.Module) []licensing.Module {
	out := make([]licensing.Module, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// licenseFileName 给一个模块的许可证原文起名。
//
// 用 `模块路径-原文件名` 是为了两件事：**同一目录里不会撞名**（`LICENSE` 谁都有），
// 以及**看文件名就知道是谁的**（用户拿到的是 `tphakala-go-m4a-LICENSE.txt`，不是
// 一堆都叫 `LICENSE` 的文件）。
func licenseFileName(modulePath string) string {
	return strings.ReplaceAll(modulePath, "/", "-") + "-LICENSE.txt"
}

// moduleRoot 往上找 `go.mod` 所在的那层目录。
//
// `go generate` 的工作目录是**包目录**，直接拿它拼路径会写到
// `internal/licensing/resources/` 下面去——一个没人看的地方，许可证于是「不见了」
// 而不是「报错了」。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 往上找不到 go.mod", dir)
		}
		dir = parent
	}
}
