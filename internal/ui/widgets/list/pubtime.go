// 「投稿时间」那一列的字。
//
// # 为什么不是上游那份 `YYYY-MM-DD`
//
// 上游 Biu 的 `formatSecondsToDate`（`common/utils/time.ts:19`）一直是
// `moment.unix(s).format("YYYY-MM-DD")`，但票 #142 要求把这一列的**显示格式
// 定下来**，而这一列是一张「按新旧挑」的列表上的辅助信息：一列十个字符的日期
// 要心算，近几天的「3天前」不用。所以这里折成三档——
//
//	今天 / 昨天 / N天前	一周之内
//	MM-DD			今年之内、超过一周
//	YYYY-MM-DD		跨年
//
// ⚠️ **跨年那一档和上游一模一样**（`YYYY-MM-DD`）：不久以前的日期才折，
// 折法是加法不是替换。这样「哪首是哪天发的」这件事在两档上都读得出来。
//
// ⚠️ **now 是参数不是 `time.Now()`**：界面上传的是外壳那颗时钟
// （`Shell.clock`），测试里是假时钟——推一天，「今天」就变「昨天」。
// 拿墙钟的话一条 2026-10-21 的夹具过一年会自己变红，而红的时候和被测的
// 逻辑一点关系都没有。
package list

import (
	"strconv"
	"time"
)

// PubTimeText 是时间那一列该显示的字。零值是空串——那是「这一页没有这个数」，
// 和 `PlayCountText(0)` 是同一个约定：**空串不是 0，也不是 1970 年**。
func PubTimeText(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	days := calendarDaysBetween(t, now)
	switch {
	case days <= 0:
		// 未来（时钟偏差）也算今天：显示「-1天前」比显示今天更没道理。
		return "今天"
	case days == 1:
		return "昨天"
	case days < 7:
		return strconv.Itoa(days) + "天前"
	case t.Year() == now.Year():
		return t.Format("01-02")
	default:
		return t.Format("2006-01-02")
	}
}

// calendarDaysBetween 是「t 是 now 之前的第几个**日历日**」。
//
// ⚠️ **按日历日算，不是按 24 小时**：23:59 和第二天 00:01 只隔两分钟，
// 但它们是两天。两头都先截成 UTC 的零点再相减，避开夏令时那一天的
// 23 / 25 小时——直接用 `now.Sub(t)` 除 24 小时，在换季那一天会差一天。
func calendarDaysBetween(t, now time.Time) int {
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	a := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	b := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a) / (24 * time.Hour))
}
