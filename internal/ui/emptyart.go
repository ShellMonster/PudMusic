package ui

import "github.com/egoist/mygo/ui"

// emptyIllustrationH 是空状态插画的高度（宽按 240:160 的比例收在 144）。
//
// 设计稿 docs/design-mockups/empty_states.png：一枚双色调的小插画（淡的同心圆底 +
// 线条主体 + 一枚带 16% 填充的角标），取代原来 32 的单枚线性图标。
const emptyIllustrationH = 96

// emptyIllustrationFiles 是「图标名 → 插画文件」。没登记的图标（比如默认那一枚）
// 仍画原来的单枚图标，所以这张表可以一个一个往里加。
var emptyIllustrationFiles = map[string]string{
	iconEmptyMusic:    "empty-music.svg",
	iconEmptySearch:   "empty-search.svg",
	iconEmptyLater:    "empty-history.svg",
	iconEmptyHistory:  "empty-history.svg",
	iconEmptyFolder:   "empty-folder.svg",
	iconEmptyDownload: "empty-download.svg",
	iconEmptyLyrics:   "empty-lyrics.svg",
	iconFailed:        "empty-failed.svg",
	iconEmptyLogin:    "empty-login.svg",
	iconEmptyGroup:    "empty-group.svg",
	iconEmptyFeed:     "empty-group.svg",
	iconEmptyDisk:     "empty-disk.svg",
	iconEmptyUser:     "empty-user.svg",
}

// emptyIllustrations 是解析好的插画，启动时一次读完。读不到 = 有人挪了目录，当场
// panic（同 `errorArt`）。
var emptyIllustrations = func() map[string]*ui.SVG {
	out := map[string]*ui.SVG{}
	parsed := map[string]*ui.SVG{}
	for name, file := range emptyIllustrationFiles {
		v, ok := parsed[file]
		if !ok {
			b, err := artFile.ReadFile("art/" + file)
			if err != nil {
				panic("ui: 空状态插画读不到（art/" + file + "）：" + err.Error())
			}
			v = ui.MustParseSVG(b)
			parsed[file] = v
		}
		out[name] = v
	}
	return out
}()
