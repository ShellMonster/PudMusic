// 播放量那一列的字。
//
// 抄上游的 `Intl.NumberFormat` compact：往上折成「1.2万」「1.2亿」。不折的话七位数
// 播放量会把那一列撑到别列头上，而网格卡片封面底部那一条只有 22 高——七位数那一串字
// 直接盖过封面去了。
//
// ⚠️ **它在 `widgets/list` 里而不在页面里**：网格卡片（`grid.go`）和「稍后再看」
// 「历史记录」那两页（`internal/ui`）都要画同一个数，而 `internal/ui` 依赖本包——反过
// 来就循环依赖了。
package list

import "strconv"

// PlayCountText 是播放量该显示的字。
//
// ⚠️ **n <= 0 返回空串**：那是「这一页没有这个数」（本地音乐、历史记录、搜索的
// 「用户」那一档，见 `Music.View`），而**空串不是 0**——调用方靠它决定那一半画不画。
// 把 0 显示成「0」的话，用户会以为这一格真的是零播放。
func PlayCountText(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n >= 100_000_000:
		return strconv.FormatFloat(float64(n)/1e8, 'f', 1, 64) + "亿"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e4, 'f', 1, 64) + "万"
	default:
		return strconv.FormatInt(n, 10)
	}
}
