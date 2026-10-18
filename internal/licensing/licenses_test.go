package licensing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gateTargets 是这道门要核的构建目标，和 `cmd/gen-notices` 的那份一样。
//
// ⚠️ **加平台就往这里和那儿各加一条。** 只核本机那一个是最容易漏的做法：Windows
// 独有的 `//go:build windows` 文件里的 import 在 macOS 上根本不会被 `go list` 看到。
var gateTargets = []struct{ GOOS, GOARCH string }{
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"windows", "amd64"},
	{"windows", "arm64"},
}

// repoRoot 往上找 `go.mod`。`go test` 的工作目录是**包目录**，不是仓库根。
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

// loadManifest 读随包发出去的那份清单。
func loadManifest(t *testing.T) []Entry {
	t.Helper()
	p := filepath.Join(repoRoot(t), filepath.FromSlash(NoticesRelPath))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读不到 %s：%v（跑 `go generate ./internal/licensing/`）", NoticesRelPath, err)
	}
	entries, err := ParseNotices(b)
	if err != nil {
		t.Fatalf("%s 解析不了：%v", NoticesRelPath, err)
	}
	if len(entries) == 0 {
		t.Fatalf("%s 里一条都没有", NoticesRelPath)
	}
	return entries
}

// TestTheInventoryListsExactlyWhatGetsCompiled 钉住「清单 == 真正的构建依赖」。
//
// 这是 R22 的核心：清单不是照 `go.mod` 抄的（go.mod 列的是**要求**，indirect 的
// 依赖可能压根没被引用），而是照 `go list -deps` 数出来的**实际**依赖。
//
// 两个方向都要断：
//
//   - **多**：清单里多一条没编进去的依赖 → 用户拿到一份用不上的许可证（小事），
//     但更常见的是清单没跟上 `go mod tidy`，久了就没人信它了。
//   - **少**：清单里少一条**编进去了**的依赖 → 那个模块的许可证没随包发出去，
//     对 Apache-2.0（§4(a) 要求给副本）和 MIT（要求附声明）都是**违反许可证**。
//     而这件事在打包、发布、用户点开之前，**不会以任何形式暴露**。
func TestTheInventoryListsExactlyWhatGetsCompiled(t *testing.T) {
	root := repoRoot(t)
	declared := map[string]Entry{}
	for _, e := range loadManifest(t) {
		declared[e.Module] = e
	}

	built := map[string]Module{}
	for _, target := range gateTargets {
		mods, err := BuildModules(context.Background(), root, os.Environ(), target.GOOS, target.GOARCH)
		if err != nil {
			t.Fatalf("%s/%s：%v", target.GOOS, target.GOARCH, err)
		}
		for _, m := range mods {
			built[m.Path] = m
		}
	}

	var missing, extra []string
	for path := range built {
		if _, ok := declared[path]; !ok {
			missing = append(missing, fmt.Sprintf("%s@%s", path, built[path].Version))
		}
	}
	for path := range declared {
		if _, ok := built[path]; !ok {
			extra = append(extra, path)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("编进去了但清单里没有（它们的许可证**没有随包发出去**）：\n  %s\n"+
			"跑 `go generate ./internal/licensing/`", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("清单里有但没编进去（清单没跟上依赖变化）：\n  %s\n"+
			"跑 `go generate ./internal/licensing/`", strings.Join(extra, "\n  "))
	}
}

// TestTheShippedAssetLicensesExist 钉住「资源的许可证也随包发出去」。
//
// 🔴 **这一条补的是 `TestTheInventoryListsExactlyWhatGetsCompiled` 的一个盲区**：
// 那条只认 `go list -deps` 数出来的**模块**。图标是 `//go:embed` 进二进制的
// 资源，不是链接进来的代码——它少一份许可证，上面那些断言**一条都不会红**，
// 而包照发、DMG 照做，只是法律上已经不合规了（Apache §4(a) / 各类 CC 的
// 「随附声明」义务都是这一类）。
//
// 顺带断一件更靠前的事：**图标在不在**。删掉 `internal/ui/icons/` 里的文件，
// `go build` 会立刻红（`//go:embed` 找不到目录），所以真正会「安静不合规」的
// 只有许可证那一份——两条一起看才不会漏。
func TestTheShippedAssetLicensesExist(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range AssetLicenseRelPaths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		b, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("%s 不在包里：%v\n"+
				"它不是构建依赖，`go list -deps` 看不见它，而 gen-notices 的 prune "+
				"会把它删掉——排进 AssetLicenseRelPaths 之后它才留得住", rel, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("%s 是空的", rel)
		}
	}
}

// TestTheShippedLicenseTextsAreTheRealOnes 钉住「发出去的原文和模块里那份一字不差」。
//
// Apache-2.0 §4(a) 给用户的承诺是「本许可证的副本」。手抄一遍抄错一个字（少一个
// 条款、把 "provided that" 抄成 "provided that you"，甚至最常见的：忘了拷
// `NOTICE` 文件），受照的人手里那份就是**残缺的**，而且没有任何工具会发现。
func TestTheShippedLicenseTextsAreTheRealOnes(t *testing.T) {
	root := repoRoot(t)
	licensesDir := filepath.Join(root, filepath.FromSlash(filepath.Dir(NoticesRelPath)))

	// 依赖表**在循环外面**问一次。
	//
	// `BuildModules` 跑的是 `go list`，参数每一轮都一样，出来的表也就每一轮都一样
	// ——原来在循环里问，等于把同一个 `go list` 跑了「清单里有多少个模块」遍。
	// 那是这个包里最贵的一步（`-race` 下十几秒），而它一次就够。
	//
	// 判据一个字没动：每一个模块仍然逐一比对它随包发出的原文和源头那份。
	mods, err := BuildModules(context.Background(), root, os.Environ(), "darwin", "arm64")
	if err != nil {
		t.Fatalf("go list：%v", err)
	}

	for _, e := range loadManifest(t) {
		want, err := os.ReadFile(filepath.Join(licensesDir, e.File))
		if err != nil {
			t.Errorf("%s 的许可证原文 %s 不在包里：%v", e.Module, e.File, err)
			continue
		}
		var dir string
		for _, m := range mods {
			if m.Path == e.Module {
				dir = m.Dir
			}
		}
		if dir == "" {
			continue // 不在依赖里，另一条测试会报
		}
		_, have, err := LicenseFile(dir)
		if err != nil {
			t.Errorf("%s：%v", e.Module, err)
			continue
		}
		if string(want) != string(have) {
			t.Errorf("%s 随包发出的许可证原文和 %s 里那份不是同一份"+
				"（发出去 %d 字节，源头 %d 字节）——多半是手抄的",
				e.Module, filepath.Base(mustLicensePath(t, dir)), len(want), len(have))
		}
	}
}

// mustLicensePath 拿模块目录里的许可证路径，报错就当场失败。
func mustLicensePath(t *testing.T, dir string) string {
	t.Helper()
	p, _, err := LicenseFile(dir)
	if err != nil {
		t.Fatalf("%s：%v", dir, err)
	}
	return p
}

// TestTheDeclaredSPDXIsWhatTheTextSays 钉住「清单上写的 SPDX 是文件里真有的」。
//
// ⚠️ 这一条是照着**真的写错过**加的：`Plan.md` §4 R13 记着 purego 曾被记成
// MIT（实际 Apache-2.0）。差别不是「两种宽松许可差不多」——Apache-2.0 §4 有
// 明确义务（给副本、说明改动、不得用商标），MIT 那一套不用。
//
// 而且这类错误在清单里是**静默**的：文件照发、构建照过，只有懂行的人翻到才会发现。
func TestTheDeclaredSPDXIsWhatTheTextSays(t *testing.T) {
	dir := filepath.Join(repoRoot(t), filepath.FromSlash(filepath.Dir(NoticesRelPath)))
	for _, e := range loadManifest(t) {
		b, err := os.ReadFile(filepath.Join(dir, e.File))
		if err != nil {
			t.Errorf("%s：%v", e.File, err)
			continue
		}
		got, err := DetectSPDX(b)
		if err != nil {
			t.Errorf("%s 认不出许可证：%v", e.File, err)
			continue
		}
		if got != e.SPDX {
			t.Errorf("%s：清单上写 %s，随包发出去的那份文件实际是 %s",
				e.Module, e.SPDX, got)
		}
	}
}

// TestTheThreeLicensesWeOnceGotWrong 把任务里点名的三条钉死。
//
// 「purego 是 Apache-2.0、mp4 库是 MIT、FLAC 解码器是 Unlicense」——这三条在
// `Plan.md` 里被明确纠正过一次，所以它们各自单独立一条，而不是只靠上面那条
// 「SPDX 和文本一致」间接覆盖：那条只会说「清单和文件对得上」，不会说
// 「清单上的那个值是对的」。
func TestTheThreeLicensesWeOnceGotWrong(t *testing.T) {
	want := map[string]string{
		"github.com/ebitengine/purego": "Apache-2.0", // 曾被写成 MIT
		"github.com/tphakala/go-m4a":   "MIT",        // mp4 容器库
		"github.com/mewkiz/flac":       "Unlicense",  // FLAC 解码器
	}
	got := map[string]string{}
	for _, e := range loadManifest(t) {
		got[e.Module] = e.SPDX
	}
	for module, spdx := range want {
		if got[module] != spdx {
			t.Errorf("%s 的许可证是 %q，want %q", module, got[module], spdx)
		}
	}
}

// TestNoDependencyIsCopyleft 守住「一行 LGPL 都不用」这句话。
//
// `Plan.md` §4 R13 把这句写成了**对外的说法**：它也是当初决定不引 `go-aac`
// （LGPL-2.1）的理由。既然对外这么讲，就得有人守着。
//
// ⚠️ 挡住它是有代价的，别绕过：Go 是**静态链接**，编进来就是编进用户的可执行文件，
// 义务落在 LGPL 第 6 条（允许修改、允许逆向调试、**允许重新链接替换**）而不是
// 第 4 条。「公开主程序源码就行」那个论证是错的，R13 里已经记着。
func TestNoDependencyIsCopyleft(t *testing.T) {
	for _, e := range loadManifest(t) {
		if IsCopyleft(e.SPDX) {
			t.Errorf("%s@%s 是 %s（带传染性）——这推翻了 Plan §4 R13 的「一行 LGPL 都不用」。"+
				"要引的话先回去核清 LGPL 第 6 条的合规义务，别只核第 4 条",
				e.Module, e.Version, e.SPDX)
		}
	}
}

// TestBiuNoticeShipsVerbatim 钉住「原作者的版权声明逐字随包发出去」。
//
// Biu 是 PolyForm-Noncommercial-1.0.0，它的 Notices 一节点名要**逐字**保留
// `Required Notice:` 那一行。本项目是它的重写，这个义务照原样落在我们身上。
//
// ⚠️ 删掉/改写这行不会让任何构建失败，包照发、DMG 照做——只是法律上已经不合规，
// 而这类错没有任何测试会替你发现。所以单独立一条。
func TestBiuNoticeShipsVerbatim(t *testing.T) {
	root := repoRoot(t)
	shipped, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(BiuLicenseRelPath)))
	if err != nil {
		t.Fatalf("读不到 %s：%v", BiuLicenseRelPath, err)
	}
	if !strings.Contains(string(shipped), BiuRequiredNotice) {
		t.Errorf("%s 里没有逐字的 %q——PolyForm 的 Notices 一节要求保留它",
			BiuLicenseRelPath, BiuRequiredNotice)
	}

	// 仓库根的 LICENSE 和随包发出去的那份必须一字不差。
	rootLicense, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatalf("读不到仓库根的 LICENSE：%v", err)
	}
	if string(rootLicense) != string(shipped) {
		t.Errorf("%s 和仓库根的 LICENSE 不是同一份（%d vs %d 字节）——它们要保持逐字一致",
			BiuLicenseRelPath, len(shipped), len(rootLicense))
	}
}

// TestDetectSPDXRecognisesTheOnesItMust 断嗅探规则本身。
//
// 用**构造的**短文本而不是真的模块文件：模块升级会换掉真文件，那时这条会跟着
// 失真；而这里断的是「规则本身对不对」，那是不会随升级变的。
//
// ⚠️ 每条都写了**不该认成谁**，因为真实事故长这样：「Apache-2.0 被当成 MIT」
// 只在两者都命中时才可能发生，单向的断言抓不住。
func TestDetectSPDXRecognisesTheOnesItMust(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "Apache-2.0 按标题和版本认，不按 'Permission is hereby granted'",
			text: "                                 Apache License\n                           Version 2.0, January 2004\n",
			want: "Apache-2.0",
		},
		{
			name: "MIT 只认 MIT 那一句",
			text: "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\n",
			want: "MIT",
		},
		{
			name: "Unlicense 不被 BSD 抢走",
			text: "This is free and unencumbered software released into the public domain.\n",
			want: "Unlicense",
		},
		{
			name: "BSD-3 有第三条背书限制",
			text: "Redistribution and use in source and binary forms are permitted.\n   3. Neither the name of Google LLC nor the names of its contributors\n",
			want: "BSD-3-Clause",
		},
		{
			name: "BSD-2 没有第三条，别当成 BSD-3",
			text: "Redistribution and use in source and binary forms are permitted.\nTHIS SOFTWARE IS PROVIDED \"AS IS\".\n",
			want: "BSD-2-Clause",
		},
		{
			name: "LGPL-2.1 与 GPL-2.0 分得开",
			text: "                   GNU LESSER GENERAL PUBLIC LICENSE\n                       Version 2.1, February 1999\n",
			want: "LGPL-2.1",
		},
		{
			name: "GPL-3.0 不被当成 LGPL",
			text: "                    GNU GENERAL PUBLIC LICENSE\n                       Version 3, 29 June 2007\n",
			want: "GPL-3.0",
		},
		{
			name: "双许可靠文件里明写的 SPDX 标识，不靠嗅探（嗅探只能认出其中一条）",
			text: "This project is provided under the terms of the UNLICENSE or\nthe BSD license.\n\nSPDX-License-Identifier: Unlicense OR BSD-3-Clause\n",
			want: "Unlicense OR BSD-3-Clause",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DetectSPDX([]byte(c.text))
			if err != nil {
				t.Fatalf("DetectSPDX：%v", err)
			}
			if got != c.want {
				t.Errorf("DetectSPDX = %q，want %q", got, c.want)
			}
		})
	}
}

// TestDetectSPDXRefusesToGuess 钉住「认不出就报错，不猜」。
//
// 猜错的后果是清单上写着 MIT 而包里发的是 LGPL 全文——而这正是 R13 记着的那类
// 事故。宁可这里红一次，让人加一条规则。
func TestDetectSPDXRefusesToGuess(t *testing.T) {
	for _, text := range []string{
		"Copyright 2026 Someone. All rights reserved. Ask nicely.",
		"",
		"// 这是一个源文件，不是许可证。",
	} {
		if got, err := DetectSPDX([]byte(text)); err == nil {
			t.Errorf("DetectSPDX(%q) = %q，want 报错", text, got)
		}
	}
}

// TestNoticesRoundTrip 断清单的写出/读回。
//
// 清单是**随包发给用户**的文件：格式坏了用户看到的就是一张乱码表，而生成器和
// 测试都读的是同一份代码，写和读一起坏掉的话谁都发现不了。
func TestNoticesRoundTrip(t *testing.T) {
	want := []Entry{
		{Module: "github.com/tphakala/go-m4a", Version: "v0.7.0", SPDX: "MIT", File: "a-LICENSE.txt"},
		{Module: "golang.org/x/text", Version: "v0.43.0", SPDX: "BSD-3-Clause", File: "b-LICENSE.txt"},
		{Module: "x/y", Version: "v0.0.0-20250101000000-abcdefabcdef", SPDX: "Unlicense OR BSD-3-Clause", File: "c-LICENSE.txt"},
	}
	got, err := ParseNotices(FormatNotices(want))
	if err != nil {
		t.Fatalf("读回刚写出的清单：%v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("读回 %d 条，want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条 = %+v，want %+v", i+1, got[i], want[i])
		}
	}
}

// TestParseNoticesRejectsBrokenLines 断「列数不对要报错」。
//
// 四列是这份文件的格式约定。写成三列（忘了版本号）时如果静默接受，清单就少了一
// 个关键信息，而**没有任何地方**看得出少了什么。
func TestParseNoticesRejectsBrokenLines(t *testing.T) {
	for _, line := range []string{
		"github.com/a/b\tv1.0.0\tMIT\n",               // 三列
		"github.com/a/b\tv1.0.0\tMIT\tf.txt\textra\n", // 五列
		"github.com/a/b v1.0.0 MIT f.txt\n",           // 用空格不是制表符
	} {
		if _, err := ParseNotices([]byte(line)); err == nil {
			t.Errorf("ParseNotices(%q) 没报错", line)
		}
	}
}
