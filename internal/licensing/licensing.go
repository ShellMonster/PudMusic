// Package licensing 管第三方的许可清单：发出去的包**必须**带上每一个编进去的依赖的
// 许可证原文，而且清单要和**真正的构建产物**对得上。
//
// ## 为什么这件事要变成代码
//
// `PLAN.md` §4 的 R22 记着它的来历：许可证在原来的登记册里**已经被提及过**，
// 但没有转成发布门槛——「记了但没人卡」。后果是发布出去的包里缺 `Required Notice`，
// 或者（更糟）误编进了 LGPL 代码，而两件事都不会让构建失败。
//
// 所以这里不是「维护一份文档」，是三道会红的断言：
//
//  1. 清单里的模块集合 == `go list -deps` 数出来的**实际**依赖集合（不多不少）。
//  2. 每条声明的 SPDX 标识 == 那个模块 LICENSE 文件里真正写着的东西。
//  3. 随包发出去的许可证原文 == 模块缓存里那份，一个字节不差。
//
// ## 为什么不读 go.mod
//
// `go.mod` 列的是**要求**，不是**编进去了什么**：indirect 的依赖可能没被引用，
// 而某个 `//go:build windows` 文件里的 import 在 macOS 上根本不会被看到。
// R22 的原话是「不是读 `go.mod` 推的，是构建产物核出来的」，所以这里跑的是
// `go list -deps`，并且**darwin 和 windows 各跑一遍**——两个平台的构建图不一样。
package licensing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// NoticesRelPath 是清单文件的位置（相对仓库根），随包发出去。
//
// 它在 `resources/` 下面，所以 `mygo build` 会自动把它装进 app bundle 的
// `Contents/Resources`（macOS）或可执行文件旁边（Windows）——不需要在
// `mygo.json` 里另外登记。
const NoticesRelPath = "resources/licenses/THIRD-PARTY-NOTICES.txt"

// BiuLicenseRelPath 是 Biu 原作者的许可证原文（PolyForm Noncommercial 1.0.0）。
//
// ⚠️ 它**不在** `go list -deps` 的结果里：Biu 不是构建依赖，它是这个项目的
// **上游**。但 PolyForm 的 Notices 一节要求「任何拿到软件一部分副本的人也拿到
// 这份条款」，所以它必须随包发出去。
const BiuLicenseRelPath = "resources/licenses/BIU-LICENSE.txt"

// BiuRequiredNotice 是 PolyForm 要求**逐字**保留的那一行。
//
// 「逐字」不是客气话：这行是 PolyForm 的 Notices 一节点名要的，删掉它等于
// 违反上游许可证。而违反它不会有任何工具报错——包照发，DMG 照做，只是法律上
// 已经不合规了。
const BiuRequiredNotice = "Required Notice: Copyright (c) 2022–2025 wood3n"

// Entry 是清单里的一行：一个编进去的依赖，及其许可证。
type Entry struct {
	Module  string // 例 "github.com/tphakala/go-m4a"
	Version string // 例 "v0.7.0"
	SPDX    string // 例 "MIT"
	File    string // 许可证原文在 resources/licenses 下的文件名
}

// String 是清单里那一行的样子（制表符分隔）。
func (e Entry) String() string {
	return strings.Join([]string{e.Module, e.Version, e.SPDX, e.File}, "\t")
}

// noticesHeader 是清单正文之前的那段说明。
//
// ⚠️ 每一行都以 `#` 开头，`ParseNotices` 靠这一点跳过它。改这段说明时保持这个
// 前缀，别写成别的样子。
const noticesHeader = `# Biu MyGo 第三方许可清单
#
# 这个应用把下列模块**静态链接**进了可执行文件。每一行的最后一列是这个模块的
# 许可证原文，在同一个目录里（Windows 上是装在可执行文件旁边的 licenses\ 子目录）。
#
# 清单不是照着 go.mod 抄的，是照着 ` + "`go list -deps`" + ` 的实际结果核出来的：
# 见 internal/licensing/licensing_test.go。改依赖之后跑
# ` + "`go generate ./internal/licensing/`" + ` 重新生成。
#
# module<TAB>version<TAB>SPDX<TAB>许可证原文文件
#
# ⚠️ 本项目**不含任何 LGPL 代码**（Plan §4 R13）。purego 是 Apache-2.0，不是 MIT；
# go-mp3 是 Apache-2.0，也不是 MIT。两处都曾经被写错过。

`

// FormatNotices 把一组 Entry 渲染成清单文件的内容。
func FormatNotices(entries []Entry) []byte {
	var b strings.Builder
	b.WriteString(noticesHeader)
	for _, e := range entries {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// ParseNotices 解析清单文件。
//
// 跳过空行和以 `#` 开头的说明行；正文按制表符切成四列。少一列、多一列、列数不对
// 都算错——那份文件是要随包发出去的，格式坏了用户看到的就是一张乱码表。
func ParseNotices(data []byte) ([]Entry, error) {
	var out []Entry
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("清单第 %d 行有 %d 列，want 4：%q", i+1, len(fields), line)
		}
		out = append(out, Entry{
			Module:  fields[0],
			Version: fields[1],
			SPDX:    fields[2],
			File:    fields[3],
		})
	}
	return out, nil
}

// listPackage 是一条 `go list -deps -json` 的输出。
type listPackage struct {
	ImportPath string
	Module     *struct {
		Path    string
		Version string
		Dir     string
		Main    bool
	}
}

// Module 是清单和实际依赖集合共用的那部分信息。
type Module struct {
	Path    string
	Version string
	Dir     string
}

// BuildModules 跑 `go list -deps`，报出这个目标**真正编进去**的外部模块。
//
// 同一个模块在依赖图里出现几十次（每个包一条），这里按模块路径去重。
func BuildModules(ctx context.Context, dir string, env []string, goos, goarch string) ([]Module, error) {
	cmd := exec.CommandContext(ctx, goTool(), "list", "-deps", "-json", ".")
	cmd.Dir = dir
	// 复制一份再追加：append 到调用方传进来的切片上，底层数组可能是它的，
	// 改坏了就是「跑一次之后这个进程里 GOOS 变了」。
	cmd.Env = append(append([]string{}, env...), "GOOS="+goos, "GOARCH="+goarch)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps（%s/%s）：%v\n%s", goos, goarch, err, stderr.String())
	}

	// `go list -json` 吐的是一串首尾相接的 JSON 对象，用流式解码逐个读。
	dec := json.NewDecoder(bytes.NewReader(out))
	seen := map[string]Module{}
	for {
		var p listPackage
		if err := dec.Decode(&p); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("解 go list 的输出：%w", err)
		}
		m := p.Module
		if m == nil || m.Main || m.Path == "" {
			continue // 标准库，或者本模块自己
		}
		if _, ok := seen[m.Path]; ok {
			continue
		}
		seen[m.Path] = Module{Path: m.Path, Version: m.Version, Dir: m.Dir}
	}

	out2 := make([]Module, 0, len(seen))
	for _, m := range seen {
		out2 = append(out2, m)
	}
	sort.Slice(out2, func(i, j int) bool { return out2[i].Path < out2[j].Path })
	return out2, nil
}

// goTool 找 `go`。空 PATH 上 `exec.Command` 找不到它，报的错会变成
// 「executable file not found in $PATH」，看不出是缺 Go。
func goTool() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}

//go:generate go run ./cmd/gen-notices
