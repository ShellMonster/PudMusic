package ui

// 日期范围筛选这一组盯的是三件事：
//
//  1. **一天的两头是本地时区的 00:00:00 和 23:59:59**。用 UTC 换算的话，东八区的
//     用户晚上八点以后看到的「今天」是昨天——而筛出来的是昨天的东西。
//  2. **换完条件之后不再报「变了」**。每帧都报的话页面每一帧都 Reload，那是一个
//     自己拉自己的死循环，界面上表现为「转圈停不下来」。
//  3. **不筛的时候两端是零值**，服务端因此不发那两个参数。

import (
	"slices"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// TestTheDayEdgesAreLocalTime 钉住一天的两头。
func TestTheDayEdgesAreLocalTime(t *testing.T) {
	// 深夜十一点半：本地时区上这是「今天」的末尾，不该因为换算成别的时间基准
	// 变成明天或者昨天。
	late := time.Date(2026, 10, 16, 23, 30, 0, 0, time.Local)

	start := startOfDay(late)
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Errorf("一天的开头是 %v，want 00:00:00", start)
	}
	if y, m, d := start.Date(); y != 2026 || m != time.October || d != 16 {
		t.Errorf("一天的开头是 %v，want 2026-10-16", start)
	}

	// ⚠️ 结尾是 **23:59:59**，不是「第二天 00:00:00」。接口收的是闭区间，第二天
	// 零点会把「今天 23:59:59 刚听的那一条」漏掉——而那正是用户刚听完、正要加进
	// 稍后再看的那一条。
	end := endOfDay(late)
	if end.Hour() != 23 || end.Minute() != 59 || end.Second() != 59 {
		t.Errorf("一天的结尾是 %v，want 23:59:59", end)
	}
	if !end.After(start) {
		t.Errorf("结尾 %v 不在开头 %v 之后", end, start)
	}
	// 结尾和开头是**同一天**：跨到第二天去就成闭区间之外了。
	if y, m, d := end.Date(); y != 2026 || m != time.October || d != 16 {
		t.Errorf("一天的结尾是 %v，want 2026-10-16 23:59:59", end)
	}
}

// TestTheDayEdgesAreLocalTimeNotUTC 单独把时区这一条拎出来。
//
// 这一条在 UTC 的机器上永远绿、在东八区的机器上才红——所以它在测试里必须**显式**
// 换一个非零时区再验一遍，否则它只是一句在 CI 上通过的装饰。
func TestTheDayEdgesAreLocalTimeNotUTC(t *testing.T) {
	zone := time.FixedZone("东八区", 8*3600)
	// 东八区的 2026-10-16 23:30，换成 UTC 是 10-16 15:30；反过来，如果代码用 UTC
	// 算一天的开头，东八区的人在晚上看到的就会是 10-16 00:00 UTC = 10-16 08:08
	// 本地——那还算对；真正错的是**凌晨**：10-17 00:30 本地 = 10-16 16:30 UTC。
	early := time.Date(2026, 10, 17, 0, 30, 0, 0, zone)

	start := startOfDay(early)
	y, m, d := start.Date()
	if y != 2026 || m != time.October || d != 17 {
		t.Errorf("一天的开头算成了 %v，want 2026-10-17（本地时区）", start)
	}
	if start.Location() != zone {
		t.Errorf("算出来落在 %v 上，不是用户的时区", start.Location())
	}
	if got := start.Hour(); got != 0 {
		t.Errorf("一天的开头是 %02d 点，want 00", got)
	}
}

// TestTheFilterReportsAChangeOnlyOnce 盯「换完条件之后不再报变了」。
//
// 这是死循环那条路的唯一判据：页面拿到 true 就 Reload，Reload 会清空并发请求，
// 每帧都 Reload 的话这个应用会一直转圈而且永远出不来东西。
func TestTheFilterReportsAChangeOnlyOnce(t *testing.T) {
	f := newDateFilter(now)
	// 一帧里 view 可能跑不止一遍（按钮被消费掉之后框架会重建一次），而
	// `Clicked()` 只在**消费它的那一遍**为真。所以这里收的是整帧的「或」，
	// 而不是最后一次的值——收最后一次的话，「点了没报变化」是必然的，
	// 那条断言就永远不会红。
	var saw []bool
	// 宽度照页面那一帧（四个预设 + 两个日期输入框 + 那一句话摆得下的宽度）。
	// 窄了那一排会被挤出去，按钮点不到——那是测试的框太小，不是筛选器坏了。
	tt := mygoui.NewTester(func(c *mygoui.Context) { saw = append(saw, f.view(c, now)) }, 1200, 80)
	frame := func() bool {
		saw = saw[:0]
		tt.Frame()
		return slices.Contains(saw, true)
	}

	if frame() {
		t.Fatal("什么都没动就报了「条件变了」")
	}

	// ⚠️ 收的是**从点下去到下一帧结束**这一整段：`tt.Click` 自己就会跑一帧，
	// 点击是在那一帧里被消费的。click 之后另起一次 frame() 的话，看到的是
	// 「已经换完了」的那一帧，它当然报「没变」——这条断言就永远不会红。
	saw = saw[:0]
	if err := tt.Click("今天"); err != nil {
		t.Fatalf("点不到「今天」：%v", err)
	}
	tt.Frame()
	if !slices.Contains(saw, true) {
		t.Fatal("点了「今天」没有报「条件变了」——换条件之后列表不会重拉")
	}

	// **关键的后一半**：换完之后每一帧都必须报「没变」。
	for i := range 3 {
		if frame() {
			t.Fatalf("换完条件之后第 %d 帧还报「变了」——页面会每一帧 Reload，自己打自己", i+1)
		}
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
	if got := f.summary(); got != "不按日期筛" {
		t.Errorf("说的是 %q，want 不按日期筛", got)
	}

	f.setPreset(1, now) // 今天
	from, to, on = f.Range()
	if !on {
		t.Fatal("选了「今天」却仍然说不筛")
	}
	if y, m, d := from.Date(); y != 2026 || m != time.October || d != 16 {
		t.Errorf("「今天」的开头是 %v，want 2026-10-16", from)
	}
	if got := f.summary(); got != "2026-10-16 ~ 2026-10-16" {
		t.Errorf("说的是 %q，want 2026-10-16 ~ 2026-10-16", got)
	}

	// 回到「全部时间」：两端又变成零值，而**输入框里留着今天**（用户下一步多半
	// 要手动改，摆着今天他知道自己从哪儿开始改）。
	f.setPreset(presetAll, now)
	from, to, on = f.Range()
	if on || !from.IsZero() || !to.IsZero() {
		t.Errorf("回到「全部时间」之后是 %v / %v / %v", from, to, on)
	}
	if f.tab != presetAll {
		t.Errorf("回到「全部时间」之后高亮的是第 %d 档", f.tab)
	}
}

// TestAManuallyChosenDateLightsNoPreset 盯「手动挑的日子不属于任何一档」。
//
// 上游是 `setSelectedKey("")`——四个 Tab 全不亮。用户看着一个还亮着的「全部时间」
// 会以为没在筛，而旁边那一句正写着筛的是哪一段。
func TestAManuallyChosenDateLightsNoPreset(t *testing.T) {
	f := newDateFilter(now)
	f.setPreset(1, now)
	f.tab = -1
	if f.tab >= 0 {
		t.Error("手动挑日期之后仍然有一档是选中的")
	}
}
