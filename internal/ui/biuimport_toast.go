package ui

import (
	"fmt"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// biuImportToast 把导入结果压成一句给用户看的话。
//
// 「跳过的项也要说出来」。只报「已导入设置和 30 条下载记录」的话，用户会以为导入
// 是全的，而下一次他找不到某首歌的歌词时会以为「这个应用没有歌词功能」。
//
// ⚠️ 这句话里的「Biu 那边是登录状态……请重新登录一次」是**任务点名要的那句**，
// 而它不能只出现在这里（toast 两秒就没了）。设置页「配置」那一段会把它逐条列出，
// 那边才是长期摆着的地方。
func biuImportToast(r app.Imported) string {
	if !r.Any() {
		return "Biu 那边没有能带过来的数据"
	}
	var parts []string
	if r.Settings {
		parts = append(parts, "设置")
	}
	if r.Shortcuts {
		parts = append(parts, "快捷键")
	}
	if r.Downloads > 0 {
		parts = append(parts, fmt.Sprintf("%d 条下载记录", r.Downloads))
	}
	if r.Lyrics > 0 {
		parts = append(parts, fmt.Sprintf("%d 首歌词", r.Lyrics))
	}
	msg := "已从 Biu 导入：" + joinNames(parts)
	if n := len(r.Notes); n > 0 {
		msg += fmt.Sprintf("。%s", r.Notes[0])
		if n > 1 {
			msg += fmt.Sprintf("（设置页「配置」那一段里有全部 %d 条）", n)
		}
	}
	return msg
}
