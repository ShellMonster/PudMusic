package licensing

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// licenseFileNames 是模块目录里可能放着许可证的文件名，按**优先**排。
//
// 顺序不是随便排的：MIT 系和 Apache 系习惯叫 `LICENSE`，Unlicense 习惯叫
// `UNLICENSE`，老项目叫 `LICENCE` 或 `COPYING`。挑第一个存在的就好——真实模块
// 目录里同时放两个不同许可证的情况极罕见，而真出现了 `licenseFiles` 会报出来
// （见那里的说明），不会静默挑一个。
var licenseFileNames = []string{
	"LICENSE", "LICENSE.txt", "LICENSE.md",
	"LICENCE", "LICENCE.txt", "LICENCE.md",
	"UNLICENSE", "COPYING", "COPYING.txt",
}

// Copyleft 是带传染性的许可证家族。
//
// ⚠️ 「本项目一行 LGPL 都不用」是 Plan §4 R13 里的一条**声明**，不是一句描述。
// 声明要有人守着：`TestNoDependencyIsCopyleft` 就是那个守门人。Go 是静态链接，
// 一旦编进来就是编进用户的可执行文件里，义务落在 §6 而不是 §4。
var Copyleft = []string{"GPL", "LGPL", "AGPL", "MPL", "SSPL", "CC-BY-SA", "EUPL"}

// sniffRule 是「靠正文认出许可证」的一条规则。
//
// 为什么需要嗅探：11 个依赖里只有 1 个在文件里写了 `SPDX-License-Identifier`
// （typesetting）。其余的要么第一行写了名字（`MIT License`），要么什么都没写，
// 只有一整段法律文本（x/image 和 x/text 连标题都没有）。不嗅就只能靠记忆填
// ——而记忆正是这个清单当初出错的地方（purego 被记成 MIT，实际是 Apache-2.0）。
type sniffRule struct {
	SPDX string
	// All 是全部要出现的片段。
	All []string
	// None 是出现即排除的片段（用来区分同族的不同版本）。
	None []string
}

// sniffRules 按**特异性**排序：先认版本号多的、条款多的。
var sniffRules = []sniffRule{
	{SPDX: "AGPL-3.0", All: []string{"GNU AFFERO GENERAL PUBLIC LICENSE", "Version 3"}},
	{SPDX: "LGPL-3.0", All: []string{"GNU LESSER GENERAL PUBLIC LICENSE", "Version 3"}},
	{SPDX: "LGPL-2.1", All: []string{"GNU LESSER GENERAL PUBLIC LICENSE", "Version 2.1"}},
	{SPDX: "GPL-3.0", All: []string{"GNU GENERAL PUBLIC LICENSE", "Version 3"}},
	{SPDX: "GPL-2.0", All: []string{"GNU GENERAL PUBLIC LICENSE", "Version 2, June 1991"}},

	// Apache-2.0 的附录里有一句 "Copyright {yyyy} {name of copyright owner}"，
	// 但没有 "Permission is hereby granted"，所以和 MIT 不会互相误认。
	{SPDX: "Apache-2.0", All: []string{"Apache License", "Version 2.0, January 2004"}},

	{SPDX: "Unlicense", All: []string{"This is free and unencumbered software released into the public domain"}},
	{SPDX: "MIT", All: []string{"Permission is hereby granted, free of charge"}},
	{SPDX: "ISC", All: []string{"Permission to use, copy, modify, and/or distribute this software for any purpose"}},

	// BSD-3 与 BSD-2 的区别在第三条（不得用作者名背书）。
	{SPDX: "BSD-3-Clause", All: []string{"Redistribution and use in source and binary forms", "Neither the name of"}},
	{SPDX: "BSD-2-Clause", All: []string{"Redistribution and use in source and binary forms"}},
}

// DetectSPDX 从一份许可证原文认出它的 SPDX 标识。
//
// 优先认文件里**明写**的 `SPDX-License-Identifier:`——那是作者自己的声明，比
// 任何启发式都可靠，而且能表达「Unlicense OR BSD-3-Clause」这种双许可（双许可
// 不能靠嗅探还原成单选：项目实际按哪一条发布，只有作者知道）。
//
// 认不出就报错，**不猜**。猜错的后果是清单上写着 MIT 而包里发的是 LGPL 全文，
// 而这正是 R13 记着的那类事故。
func DetectSPDX(text []byte) (string, error) {
	if id, ok := explicitSPDX(string(text)); ok {
		return id, nil
	}
	for _, r := range sniffRules {
		if r.matches(string(text)) {
			return r.SPDX, nil
		}
	}
	return "", fmt.Errorf("认不出这是什么许可证（头两行：%q）", firstLines(string(text), 2))
}

// matches 报这段文本符不符合这条规则。
func (r sniffRule) matches(text string) bool {
	for _, s := range r.All {
		if !strings.Contains(text, s) {
			return false
		}
	}
	for _, s := range r.None {
		if strings.Contains(text, s) {
			return false
		}
	}
	return true
}

// explicitSPDX 找文件里明写的 SPDX 标识。
func explicitSPDX(text string) (string, bool) {
	const marker = "SPDX-License-Identifier:"
	i := strings.Index(text, marker)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(marker):]
	if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
		rest = rest[:nl]
	}
	id := strings.TrimSpace(rest)
	return id, id != ""
}

// LicenseFile 报模块目录里的许可证原文是哪一份，返回路径和内容。
//
// 一个模块目录里有**不止一种**许可证时报错：那意味着只发一份是不够的（例如
// 「本体 MIT + 内嵌的第三方代码另有 Apache」），得人来决定发哪些。
func LicenseFile(moduleDir string) (path string, content []byte, err error) {
	var found []string
	for _, name := range licenseFileNames {
		p := filepath.Join(moduleDir, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return "", nil, fmt.Errorf("%s 里没有许可证文件（试过 %s）", moduleDir, strings.Join(licenseFileNames, " / "))
	}

	kinds := map[string][]string{}
	for _, p := range found {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", nil, err
		}
		id, err := DetectSPDX(b)
		if err != nil {
			// 认不出的那一份先记下来，最后一起报——它本身可能就是问题所在。
			id = "认不出"
		}
		kinds[id] = append(kinds[id], p)
		if path == "" {
			path, content = p, b
		}
	}
	if len(kinds) > 1 {
		names := make([]string, 0, len(kinds))
		for id := range kinds {
			names = append(names, id)
		}
		sort.Strings(names)
		return "", nil, fmt.Errorf("%s 里有 %d 种不同的许可证（%s），要人决定发哪几份",
			moduleDir, len(kinds), strings.Join(names, " / "))
	}
	return path, content, nil
}

// IsCopyleft 报这个 SPDX 是不是带传染性的家族。
func IsCopyleft(spdx string) bool {
	upper := strings.ToUpper(spdx)
	for _, c := range Copyleft {
		if strings.Contains(upper, c) {
			return true
		}
	}
	return false
}

// firstLines 取头 n 行，拼成一行，给错误信息用。
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}
