package ui

// 日期范围筛选这一组盯的是两件事：
//
//  1. **一天的两头是本地时区的 00:00:00 和 23:59:59**。用 UTC 换算的话，东八区的
//     用户晚上八点以后看到的「今天」是昨天——而筛出来的是昨天的东西。
//  2. **不筛的时候两端是零值**，服务端因此不发那两个参数。

import (
	"testing"
	"time"
)

// TestTheDayEdgesAreTheLocalDay 钉住一天的两头是**本地时区**的 00:00:00 和 23:59:59。
//
// 用 UTC 换算的话，东八区的用户凌晨看到的「今天」是昨天，而筛出来的是昨天的东西。
// 下面那个用例显式换了一个非零时区再验一遍——在 UTC 的机器上只跑第一个的话，它只
// 是一句在 CI 上通过的装饰。
func TestTheDayEdgesAreTheLocalDay(t *testing.T) {
	zone := time.FixedZone("东八区", 8*3600)
	// 东八区的 2026-10-17 00:30，换成 UTC 是 10-16 16:30：用 UTC 算的话这一天会
	// 被算成 10-16。
	early := time.Date(2026, 10, 17, 0, 30, 0, 0, zone)

	start := startOfDay(early)
	if y, m, d := start.Date(); y != 2026 || m != time.October || d != 17 {
		t.Errorf("一天的开头算成了 %v，want 2026-10-17（本地时区）", start)
	}
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Errorf("一天的开头是 %v，want 00:00:00", start)
	}
	if start.Location() != zone {
		t.Errorf("算出来落在 %v 上，不是用户的时区", start.Location())
	}

	// ⚠️ 结尾是 **23:59:59**，不是「第二天 00:00:00」。接口收的是闭区间，第二天零点
	// 会把「今天 23:59:59 刚听的那一条」漏掉——而那正是用户刚听完、正要加进稍后再看
	// 的那一条。
	end := endOfDay(early)
	if end.Hour() != 23 || end.Minute() != 59 || end.Second() != 59 {
		t.Errorf("一天的结尾是 %v，want 23:59:59", end)
	}
	if y, m, d := end.Date(); y != 2026 || m != time.October || d != 17 {
		t.Errorf("一天的结尾算成了 %v，want 2026-10-17 23:59:59（跨到第二天就漏掉最后那一秒）", end)
	}
}

// TestARangeThatIsOffSaysSo 盯「不筛」这件事本身。
//
// 没在筛的时候两端必须都是零值：服务端据此**不发**那两个参数，而发了两个 0 和
// 不发在 B 站那边一样，但测试就分不出「日期筛选根本没接」和「接了但传的是 0」。
func TestARangeThatIsOffSaysSo(t *testing.T) {
	f := newDateFilter(now)
	from, to, on := f.Range()
	if on {
		t.Error("刚建好就在筛")
	}
	if !from.IsZero() || !to.IsZero() {
		t.Errorf("不筛的时候两端是 %v / %v，want 都是零值", from, to)
	}

	f.setRange(startOfDay(now), endOfDay(now), true) // 今天
	from, to, on = f.Range()
	if !on {
		t.Fatal("选了「今天」却仍然说不筛")
	}
	if y, m, d := from.Date(); y != 2026 || m != time.October || d != 16 {
		t.Errorf("「今天」的开头是 %v，want 2026-10-16", from)
	}
	// 回到「全部时间」：两端又变成零值，而**输入框里留着今天**（用户下一步多半
	// 要手动改，摆着今天他知道自己从哪儿开始改）。
	f.setPreset(presetAll, now)
	from, to, on = f.Range()
	if on || !from.IsZero() || !to.IsZero() {
		t.Errorf("回到「全部时间」之后是 %v / %v / %v", from, to, on)
	}
}
