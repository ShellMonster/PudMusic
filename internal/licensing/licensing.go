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
	"path/filepath"
	"sort"
	"strings"
)

// NoticesRelPath 是清单文件的位置（相对仓库根），随包发出去。
//
// 它在 `resources/` 下面，所以 `mygo build` 会自动把它装进 app bundle 的
// `Contents/Resources`（macOS）或可执行文件旁边（Windows）——不需要在
// `mygo.json` 里另外登记。
//
// ⚠️ **值是拼出来的**（`ResourcesDirName + "/" + NoticesDirName + "/" + NoticesFileName`）：
// 运行时找这份文件要用的也是那两段（见 RuntimeNoticesPath），写死两遍的话，
// 改了目录名而忘了改另一处，症状是**用户点了「第三方许可」什么也没发生**。
const NoticesRelPath = ResourcesDirName + "/" + NoticesDirName + "/" + NoticesFileName

const (
	// ResourcesDirName 是仓库根下装随包资源的那一个目录。
	ResourcesDirName = "resources"
	// NoticesDirName 是随包资源里放许可证原文的那一个子目录。
	NoticesDirName = "licenses"
	// NoticesFileName 是清单文件的名字。
	NoticesFileName = "THIRD-PARTY-NOTICES.txt"
)

// RuntimeNoticesPath 返回清单在**随包资源目录**里的位置。
//
// resourcesDir 是 `mygo.App.Path(mygo.PathResources)` 给的那个目录：
// macOS 的 app bundle 里是 `Contents/Resources`，别处是可执行文件旁边。
//
// ⚠️ **它和 NoticesRelPath 不是同一个东西**：那一个是**相对仓库根**的
// （生成器写文件时用），这一个**是相对随包资源目录**的（运行时找文件时用）——
// 两者差的就是 `resources/` 这一段。
func RuntimeNoticesPath(resourcesDir string) string {
	return filepath.Join(resourcesDir, NoticesDirName, NoticesFileName)
}

// BiuLicenseRelPath 是 Biu 原作者的许可证原文（PolyForm Noncommercial 1.0.0）。
//
// ⚠️ 它**不在** `go list -deps` 的结果里：Biu 不是构建依赖，它是这个项目的
// **上游**。但 PolyForm 的 Notices 一节要求「任何拿到软件一部分副本的人也拿到
// 这份条款」，所以它必须随包发出去。
const BiuLicenseRelPath = "resources/licenses/BIU-LICENSE.txt"

// IconLicenseRelPath 是图标资源那一份许可证原文（Remix Icon），随包发出去。
//
// ⚠️ **它不进 `go list -deps` 的结果**，所以清单那张表里没有它——图标是编进
// 二进制的**资源**，不是链接进来的代码模块。后果是：
//
//   - `TestTheInventoryListsExactlyWhatGetsCompiled` 看不见它少了（那张表只会
//     因为「少一个依赖」红，看不见「少一份资源许可证」）；
//   - `gen-notices` 的 prune 会把它当「过期文件」删掉。
//
// 所以它被单独排进 `AssetLicenseRelPaths`，由 `TestTheShippedAssetLicensesExist`
// 盯着在不在。
//
// 原文是上游那份的**逐字副本**（Remix Icon License v1.0，Copyright (c) 2017–2026
// Remix Design）。取它的命令见本票的报告；换上游版本时连同这一份一起换。
const IconLicenseRelPath = "resources/licenses/remixicon-LICENSE.txt"

// VendoredGoMP3LicenseRelPath 是 **vendor 进仓库**的 MP3 解码器
// （`tphakala/go-mp3`）那份许可证原文，随包发出去。
//
// ⚠️ **它和图标那一份是同一类东西，但原因不一样**：图标是「编进二进制的资源」，
// 而它是一整份**第三方代码**——`internal/service/audio/mp3dec/gomp3/` 底下那些
// `.go` 文件**静态链接进了可执行文件**。义务和模块依赖一样（MIT 要求
// 「保留声明」）。
//
// 之所以 `go list -deps` 看不见它：vendor 进来的代码是**本仓库的一部分**，
// 不是 Go 模块，`go list` 数的是模块。所以清单那张表里没有它，
// `TestTheInventoryListsExactlyWhatGetsCompiled` 也不会因为它少了而红——
// 而少了它的后果是「用户拿到的包里没有那份声明」，法律上已经不合规，
// 而没有任何构建失败会提醒你。
//
// 所以它排进 `AssetLicenseRelPaths`（`gen-notices` 的 prune 才不会把它删掉），
// 并且由 `TestTheShippedAssetLicensesExist` 盯着「发出去的和仓库里
// vendor 那份一字不差」。
const VendoredGoMP3LicenseRelPath = "resources/licenses/github.com-tphakala-go-mp3-LICENSE.txt"

// VendoredGoMP3LicenseSource 是上面那一份的**源头**：仓库里 vendor 的那份原文。
//
// 发出去的必须和它**逐字一致**——MIT 的「保留声明」那一条要求随附的那份
// 是真的那份，手抄一遍抄错一个字，那条声明就不成立了。
const VendoredGoMP3LicenseSource = "internal/service/audio/mp3dec/gomp3/LICENSE"

// Go image/jpeg 解码器的 MCU 行存储改版，保留 Go 的 BSD-3-Clause 声明。
const GoJPEGLicenseRelPath = "resources/licenses/go-image-jpeg-LICENSE.txt"
const GoJPEGLicenseSource = "internal/ui/widgets/imgcache/internal/jpegrows/LICENSE"

// AssetLicenseRelPaths 是**不是构建依赖、但同样要随包发出去**的许可证原文。
//
// 每一份都得排进 `gen-notices` 的输出列表，否则 prune 会把它删掉——而删掉一个
// 依赖的许可证是「清单说没有它、目录里却还有」，删掉一份**资源的**许可证则是
// 「包照发、DMG 照做，只是法律上已经不合规」，两边都不会有构建失败来提醒。
var AssetLicenseRelPaths = []string{
	IconLicenseRelPath,
	VendoredGoMP3LicenseRelPath,
	GoJPEGLicenseRelPath,
}

// AssetLicenseSources 是「随包发出去的那一份是从哪儿抄来的」。
//
// 键是 `AssetLicenseRelPaths` 里的路径，值是仓库里那份原文的路径。**空缺就是
// 「发出去的和仓库里的是同一份」**——图标那一份就是（它的源头就是它自己）。
//
// 为什么要有这张表：vendor 进来的那份许可证，源头在
// `internal/service/audio/mp3dec/gomp3/LICENSE`（那份才是上游的原文），
// 随包发出去的那一份是从它**逐字复制**的。少这张表的话，生成器会拿
// 「发出去的那份」当源头——也就是拿复制品复制复制品，哪天手抄错了，
// 没有任何东西会发现。
var AssetLicenseSources = map[string]string{
	VendoredGoMP3LicenseRelPath: VendoredGoMP3LicenseSource,
	GoJPEGLicenseRelPath:        GoJPEGLicenseSource,
}

// VendoredCodeLicenseRelPaths 是**vendor 进仓库的第三方代码**那几份许可证原文。
//
// 它和图标那一份的区别是：图标是**资源**（编进二进制的图片，Remix Icon
// License 不是 SPDX 里那套），而这一份是一整份第三方**代码**——静态链接进
// 可执行文件，义务和模块依赖一样，「一行 LGPL 都不用」那条不变量也照样
// 约束它。
//
// 所以「不许有传染性许可」那道门要扫的是**这一份加上清单里那些模块**，
// 不是「所有随包发出去的许可证」——后者会把图标那份也扫进来，而 Remix Icon
// License 根本不是 SPDX 标识，认不出不等于有传染性。
var VendoredCodeLicenseRelPaths = []string{
	VendoredGoMP3LicenseRelPath,
	GoJPEGLicenseRelPath,
}

// BiuRequiredNotice 是 PolyForm 要求**逐字**保留的那一行。
//
// 「逐字」不是客气话：这行是 PolyForm 的 Notices 一节点名要的，删掉它等于
// 违反上游许可证。而违反它不会有任何工具报错——包照发，DMG 照做，只是法律上
// 已经不合规了。
const BiuRequiredNotice = "Required Notice: Copyright (c) 2022–2025 wood3n"

// Entry 是清单里的一行：一个编进去的依赖，及其许可证。
type Entry struct {
	Module  string // 例 "github.com/tphakala/go-m4a"
	Version string // 例 "v0.8.0"
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
const noticesHeader = `# BiuGo 第三方许可清单
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
#
# ── 不是 Go 模块的那几样 ─────────────────────────────────────────────────────
#
# 上面那张表只列**编进可执行文件的 Go 模块**（go list -deps 数得出来的那些）。
# 这个目录里还有四份**不是模块**的，同样随包发出去：
#
# go-image-jpeg-LICENSE.txt Go image/jpeg 的 MCU 行存储改版（BSD-3-Clause）。
#
# BIU-LICENSE.txt          Biu 本体（PolyForm Noncommercial 1.0.0）。Biu 不是构建
#                          依赖，它是这个项目的**上游**；PolyForm 的 Notices 一节
#                          点名要逐字保留它，由 TestBiuNoticeShipsVerbatim 守着。
# remixicon-LICENSE.txt     界面上那一批图标（Remix Icon，Remix Icon License v1.0）。
#                          它们是编进二进制的**资源**，不是链接进来的代码，所以
#                          go list -deps 看不见它们——而那张表只会因为「少一个
#                          依赖」红，看不见「少一份资源许可证」。
#                          该许可证 §2.3 明确允许把图标当作产品里的功能组件使用、
#                          §3.1 禁止把它们**单独当图标包卖**；本项目是前者：图标
#                          随应用分发，不作为图标库单独提供。
# github.com-tphakala-go-mp3-LICENSE.txt
#                          MP3 解码器（tphakala/go-mp3，MIT）。它是 **vendor 进
#                          仓库**的（internal/service/audio/mp3dec/gomp3/），所以
#                          它是本仓库的一部分、不是 Go 模块，go list -deps 同样
#                          看不见它——但它是一整份第三方代码，静态链接进了可执行
#                          文件，义务和模块依赖一样。发出去的那一份是仓库里
#                          vendor 那份的逐字副本，由 TestTheShippedAssetLicenses-
#                          Exist 盯着。
#
# ── 用到系统组件的那几样（这里**没有**许可证可附）───────────────────────────
#
# macOS 的音频解码走 AudioToolbox、Windows 的走 Media Foundation。两者都是
# **操作系统自带的组件**：运行时的 LoadLibrary + 系统函数调用，没有链接进来，
# 不进 go list -deps，也没有许可证需要随包发出去——它们是 Windows / macOS 的
# 一部分，权利归系统厂商，用户拿到的是操作系统，而不是我们的二进制。
#
# 「用系统组件」这件事本身是个决定，写在这里是为了免得以后有人以为漏了：
# Windows 上解 AAC 本来还有一条路（引 LGPL 的纯 Go 解码器），因为 Biu 本体
# 是 PolyForm-Noncommercial 1.0.0（和 GPL 家族不兼容）才没走它。
# TestWindowsAudioPullsInNothingNew 钉着「Windows 音频这一包只依赖 go-m4a
# 这一个模块」——将来谁往里加第二个，那里会红。

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
