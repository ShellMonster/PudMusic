// 弹幕的**增量时间窗口**（票 #670）。
//
// # 这一组钉的是哪件事
//
// #647 交付了按 `TimeMS` 排序的索引和窗口二分，但绘制帧仍然每帧把窗口里
// 那一段**重新过一遍**：每一条都再查一次 `widthOf`，再把可见列表重新排一次，
// 已经在 `slots` 里的还要再写一次 map。密集弹幕（上千条）下，这些工作随
// 「窗口里有多少条」线性增长，而窗口起点是由**最长那条弹幕**估出来的上界
// ——一条长弹幕就能把窗口拉出几分钟。
//
// ⇒ #670 把「新进入窗口的条目」做成一个**只前进的游标**：
//
//   - 条目第一次跨过当前播放位置时，才做轨道分配和一次字宽测量；
//   - 之后它留在活动集合里，每帧只按年龄算位置，不再查索引、不再量字宽；
//   - 出界时从活动集合里收掉（`slots` / `widths` 同步删）。
//
// # 判据为什么是计数器 + 模型状态
//
// 计数器（`danmakuLayer.entered` / `assigned` / `measures`）是**可测的整数
// 状态**，只由模型层自己加，不参与绘制。它们让「分配次数只和新进入的条数
// 相关」变成一条能断言的等式，而不是靠观察帧耗时。
//
// ⚠️ **不用固定帧数当判据**：下面的推进量都是**时间**（`tt.Advance`），
// 条目按自己的 `TimeMS` 进入窗口，帧数是那段时间里画面自己刷出来的。
package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// denseDanmaku 造一批「先密集、后稀疏」的弹幕：
//
//	前 500 条   40.000~40.998 秒，每 2 毫秒一条（挤在八条轨道里放不下）
//	后 500 条   60.000~109.900 秒，每 100 毫秒一条
//
// 两段之间留了 19 秒的空档：位置从 41 秒往前推半秒时**没有任何新条目
// 进入窗口**——那半秒里的计数只要动了，就是「已经在活动集合里的条目被重新
// 算了一遍」。
func denseDanmaku() []app.DanmakuItem {
	items := make([]app.DanmakuItem, 0, 1000)
	for i := range 500 {
		items = append(items, danmakuItem(
			fmt.Sprintf("front-%03d", i), 40000+int64(i)*2, "前段弹幕"))
	}
	for i := range 500 {
		items = append(items, danmakuItem(
			fmt.Sprintf("back-%03d", i), 60000+int64(i)*100, "后段弹幕"))
	}
	return items
}

// TestTheDanmakuOnlyAssignsAndMeasuresTheNewlyEntered 钉住「只有新进入窗口
// 的条目才分配轨道、才量字宽」。
//
// # 场景
//
// 一千条弹幕、位置在 41 秒：
//
//   - 第一帧要吞下前 500 条（它们都在窗口里），但**八条轨道同时只放得下
//     八条**——2 毫秒一条的密集弹幕在入场那一刻全都过不了 1.8 秒的时间
//     门槛，后面那 492 条该被丢掉，**而且不该为它们量字宽**；
//   - 接下来 500 毫秒没有新条目进入，三个计数一个都不许涨；
//   - 跳到 60 秒之后，每 100 毫秒才进来一条，分配次数只跟着这几条涨，
//     不跟「总共一千条」线性增长。
func TestTheDanmakuOnlyAssignsAndMeasuresTheNewlyEntered(t *testing.T) {
	env := danmakuPlayingEnv(t, denseDanmaku(), systemIsDark(true))
	env.st.Position = 41 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	if got := d.entered; got != 500 {
		t.Fatalf("第一帧进入窗口的条目是 %d 条，want 500（40.000~40.998 秒那一段全在窗口里）", got)
	}
	// 八条轨道各进一条之后，2 毫秒一条的密集弹幕在入场那一刻全放不下
	// （时间门槛 1.8 秒）——被丢掉的那些**不该量字宽**。
	if got := d.assigned; got != danmakuTracks {
		t.Errorf("第一帧做了 %d 次轨道分配，want %d——密集弹幕放不下时不该再为它们"+
			"量字宽、算轨道", got, danmakuTracks)
	}
	if d.measures != d.assigned {
		t.Errorf("量了 %d 次字宽、分配了 %d 次——每一次分配只该量一次字宽",
			d.measures, d.assigned)
	}
	if d.live > danmakuMaxOnScreen {
		t.Errorf("同屏 %d 条，超过了上限 %d", d.live, danmakuMaxOnScreen)
	}
	if len(d.slots) > danmakuMaxOnScreen {
		t.Errorf("槽位里记着 %d 条，超过了同屏上限 %d——被丢掉的条目不该留成活动槽位",
			len(d.slots), danmakuMaxOnScreen)
	}

	entered, assigned, measures := d.entered, d.assigned, d.measures
	env.tt.Advance(500 * time.Millisecond)
	if d.entered != entered || d.assigned != assigned || d.measures != measures {
		t.Errorf("接下来 500 毫秒没有新条目进入窗口，计数却从 (%d, %d, %d) 变成 "+
			"(%d, %d, %d)——已经在活动集合里的条目被重新过了一遍",
			entered, assigned, measures, d.entered, d.assigned, d.measures)
	}
	if d.live > danmakuMaxOnScreen {
		t.Errorf("推进 500 毫秒之后同屏 %d 条，超过了上限 %d", d.live, danmakuMaxOnScreen)
	}

	// 跳到第二段：旧的那 8 条已经整个出界，该被收掉。
	env.st.Position = 60 * time.Second
	env.tt.Frame()
	if _, ok := d.slots["front-000"]; ok {
		t.Error("跳到 60 秒之后第 40 秒那一条还记在槽位里——它早就出界了")
	}
	baseEntered, baseAssigned, baseMeasures := d.entered, d.assigned, d.measures
	env.tt.Advance(500 * time.Millisecond)
	newEntered := d.entered - baseEntered
	newAssigned := d.assigned - baseAssigned
	newMeasures := d.measures - baseMeasures
	// 60.0 秒那条在跳过去那一帧进入；之后 100 毫秒一条，半秒里进来 4~5 条。
	if newEntered < 4 || newEntered > 6 {
		t.Errorf("60 秒之后的半秒里进入窗口 %d 条，want 4~6 条", newEntered)
	}
	// ⚠️ **新进入的每一条不一定都拿得到轨道**：100 毫秒一条比 1.8 秒的
	// 时间门槛密得多，同一批里只有第一条进得去。判据因此是「分配次数只跟
	// 那几条走」——它远小于总条数，且每条分配只量一次字宽。
	if newAssigned > newEntered {
		t.Errorf("新进入 %d 条，却做了 %d 次轨道分配——分配不该超过新进入的条数",
			newEntered, newAssigned)
	}
	if newAssigned > danmakuMaxOnScreen {
		t.Errorf("半秒里做了 %d 次轨道分配，超过同屏上限 %d——分配次数跟着总条数涨了",
			newAssigned, danmakuMaxOnScreen)
	}
	if newMeasures != newAssigned {
		t.Errorf("新进入 %d 条、分配 %d 次，量了 %d 次字宽——每条分配只该量一次",
			newEntered, newAssigned, newMeasures)
	}
	if got := d.assigned; got > 2*danmakuTracks {
		t.Errorf("整段跑下来一共分配了 %d 次轨道，而这份弹幕有一千条——"+
			"分配次数不该跟着总条数走", got)
	}
	if d.live > danmakuMaxOnScreen {
		t.Errorf("60 秒之后同屏 %d 条，超过了上限 %d", d.live, danmakuMaxOnScreen)
	}
}

// TestTheDanmakuIsNeverAssignedTwiceWhileItIsStillOnScreen 钉住「已经进了
// 活动集合的条目直接复用稳定状态」。
//
// 位置每次往前半秒（整窗重建），没有新条目进入窗口：计数与轨道一条都不许
// 变——变了就是「每帧重新分配」那件事又回来了。
func TestTheDanmakuIsNeverAssignedTwiceWhileItIsStillOnScreen(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("a", 41000, "第一条"),
		danmakuItem("b", 41800, "第二条"),
	}, systemIsDark(true))
	env.st.Position = 42 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	assigned, measures := d.assigned, d.measures
	lanes := map[string]int{}
	for id, slot := range d.slots {
		lanes[id] = slot.lane
	}
	if len(lanes) != 2 {
		t.Fatalf("这一帧的槽位是 %v，want 两条", lanes)
	}

	for range 3 {
		env.st.Position += 500 * time.Millisecond
		env.tt.Frame()
	}
	if d.assigned != assigned || d.measures != measures {
		t.Errorf("三次整窗重建都没有新条目进入，分配/量字却从 (%d, %d) 变成 "+
			"(%d, %d)——已经在活动集合里的条目被重新分配了",
			assigned, measures, d.assigned, d.measures)
	}
	for id, lane := range lanes {
		slot, ok := d.slots[id]
		if !ok {
			t.Errorf("重建之后 %q 从槽位里不见了", id)
			continue
		}
		if slot.lane != lane {
			t.Errorf("重建之后 %q 从第 %d 条轨道换到了第 %d 条——"+
				"轨道是入场那一刻定的，不该在重建时重算", id, lane, slot.lane)
		}
	}
}

// TestGrowingTheFontRemakesTheMeasuredWidths 钉住「字号变了整体复位」。
//
// 字宽是按字号量出来的：字号一变大，旧宽度全部作废（否则长弹幕会按旧宽度
// 提前出界、轨道门槛也按旧宽度算）。判据是模型层——槽位从第 0 条轨道重新
// 分配，`widths` 与 `slots` 对得上，而且真的重新量了字宽。
func TestGrowingTheFontRemakesTheMeasuredWidths(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("a", 40000, "第一条"),
		danmakuItem("b", 41000, "第二条"),
		danmakuItem("c", 42000, "第三条"),
	}, systemIsDark(true))
	env.st.Position = 43 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	beforeFont, beforeMeasures := d.fontSize, d.measures
	env.tt.SetPreferences(mygoui.Preferences{TextScale: 1.5})

	if d.fontSize <= beforeFont {
		t.Fatalf("系统字号调大之后弹幕字号还是 %v（原来 %v）——它该跟着缩放",
			d.fontSize, beforeFont)
	}
	if d.measures <= beforeMeasures {
		t.Errorf("字号变了之后只量了 %d 次字宽（原来 %d）——旧宽度按旧字号量的，"+
			"必须重新量", d.measures, beforeMeasures)
	}
	if len(d.widths) != len(d.slots) {
		t.Errorf("记着 %d 条字宽、%d 条槽位——字号复位之后两者该一一对应",
			len(d.widths), len(d.slots))
	}
	if slot, ok := d.slots["a"]; !ok || slot.lane != 0 {
		t.Errorf("字号变了之后第一条弹幕的槽位是 %+v（want 第 0 条轨道）——"+
			"字号变化该把轨道整份复位", slot)
	}
}

// TestChangingTheCoverWidthRebuildsTheLanes 钉住「封面宽度变了整体复位」。
//
// 轨道门槛、「整个出界」的时长、窗口粗筛的上界**都按封面宽算**：
// 窗口从 1200 缩到 900（封面从 360 变成 280）之后，旧的字宽和轨道时间
// 全部作废。
func TestChangingTheCoverWidthRebuildsTheLanes(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("a", 40000, "第一条"),
		danmakuItem("b", 41000, "第二条"),
		danmakuItem("c", 42000, "第三条"),
	}, systemIsDark(true))
	env.st.Position = 43 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	if d.coverW != danmakuCoverW {
		t.Fatalf("封面宽 %v，want %d", d.coverW, danmakuCoverW)
	}
	beforeAssigned := d.assigned

	env.tt.SetSize(900, 800)
	if got := env.s.full.coverRect.W; got >= danmakuCoverW {
		t.Fatalf("窗口缩到 900 之后封面宽是 %v，它该缩到窄窗口那一档", got)
	}
	if d.coverW != env.s.full.coverRect.W {
		t.Errorf("弹幕记的封面宽是 %v，而封面那一块是 %v——它没跟上布局变化",
			d.coverW, env.s.full.coverRect.W)
	}
	if d.assigned <= beforeAssigned {
		t.Errorf("封面宽度变了之后没有重新分配轨道（分配次数 %d → %d）——"+
			"轨道门槛按封面宽算，必须整份复位", beforeAssigned, d.assigned)
	}
	if len(d.widths) != len(d.slots) {
		t.Errorf("记着 %d 条字宽、%d 条槽位——复位之后两者该一一对应",
			len(d.widths), len(d.slots))
	}
}

// TestADanmakuWithoutAnIDNeverTakesALane 钉住异常 row ID：没有 rowid 的
// 弹幕认不出身份（槽位按 rowid 记），**不占轨道、不进槽位、不量字宽**，
// 而同一批里正常的那些照旧入场。
func TestADanmakuWithoutAnIDNeverTakesALane(t *testing.T) {
	env := danmakuShell(t, []app.DanmakuItem{
		{TimeMS: 40000, Mode: 1, Color: danmakuRed, Text: "没有 rowid"},
		danmakuItem("ok", 40500, "有 rowid"),
		{TimeMS: 41000, Mode: 1, Color: danmakuRed, Text: "也没有"},
	}, systemIsDark(true))
	env.st.Position = 42 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	if _, ok := d.slots[""]; ok {
		t.Error("没有 rowid 的弹幕进了槽位——槽位按 rowid 记，它没有身份")
	}
	if got := len(d.slots); got != 1 {
		t.Errorf("槽位里记着 %d 条，want 1 条（只有有 rowid 的那条）", got)
	}
	if slot, ok := d.slots["ok"]; !ok || slot.lane != 0 {
		t.Errorf("有 rowid 的那条槽位是 %+v（want 第 0 条轨道）", slot)
	}
	if got := d.entered; got != 1 {
		t.Errorf("进入窗口的条目记了 %d 条，want 1 条——没有 rowid 的不算入场",
			got)
	}
	if got := d.measures; got != 1 {
		t.Errorf("量了 %d 次字宽，want 1 次——没有 rowid 的不该量字宽", got)
	}
}

// TestSeekingBackwardsRewindsTheIncrementalWindow 钉住「大幅回退把增量窗口
// 整份复位」。
//
// # 它钉的是哪一个坏
//
// 游标只前进。用户从第 60 秒拖回第 20 秒时，如果只把游标留在末尾，新位置
// 上的条目永远不会入场（屏幕上一条弹幕都没有）；如果只倒游标、不换轨道，
// 旧轨道记着的入场时刻还在**将来**（第 60 秒附近），新位置上的条目一条都
// 放不进去。所以回退要两件事一起做：轨道状态整份复位、游标回到新窗口起点。
func TestSeekingBackwardsRewindsTheIncrementalWindow(t *testing.T) {
	items := make([]app.DanmakuItem, 0, 61)
	for i := range 61 {
		items = append(items, danmakuItem(
			fmt.Sprintf("t-%02d", i), int64(i)*1000, "一"))
	}
	env := danmakuPlayingEnv(t, items, systemIsDark(true))
	env.st.Position = 60 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	if d.cursor != len(d.itemsByTime) {
		t.Fatalf("第 60 秒时游标在 %d（共 %d 条）——它该已经吞掉整份列表",
			d.cursor, len(d.itemsByTime))
	}
	if _, ok := d.slots["t-59"]; !ok {
		t.Fatalf("第 60 秒时 t-59 不在槽位里（槽位：%v）——测试场景不对", d.slots)
	}

	// 用户把进度条拖回第 20 秒。
	env.st.Position = 20 * time.Second
	env.tt.Frame()

	if d.cursor >= len(d.itemsByTime) {
		t.Fatalf("拖回第 20 秒之后游标还在末尾（%d）——新位置上的条目永远不会入场",
			d.cursor)
	}
	if _, ok := d.slots["t-59"]; ok {
		t.Error("拖回第 20 秒之后第 59 秒那一条还在槽位里——旧活动集合没复位")
	}
	for id, slot := range d.slots {
		if slot.timeMS > 20000 {
			t.Errorf("%q 的槽位入场时刻是 %d，在当前位置之后——"+
				"回退之后旧轨道状态还留着", id, slot.timeMS)
		}
		if slot.lane < 0 {
			t.Errorf("%q 的槽位没有轨道（lane=%d）——被丢掉的不该进槽位", id, slot.lane)
		}
	}
	if d.live != len(d.slots) {
		t.Errorf("在屏条数记着 %d，槽位里有 %d 条——两者该一一对应",
			d.live, len(d.slots))
	}
	if d.live == 0 || d.live > danmakuMaxOnScreen {
		t.Errorf("回退之后在屏 %d 条（上限 %d）——新位置上的弹幕该重新入场",
			d.live, danmakuMaxOnScreen)
	}
	// ⚠️ **第 0 条轨道上不一定是最老的那一条**：窗口里那些条目每 1 秒一条，
	// 而同轨门槛是 1.8 秒 ⇒ 第 0 条轨道会被窗口里的几条轮流复用。判据因此
	// 是「它记的是**新窗口里**某一条的时刻」，不是回退前那个（54000）。
	if top := d.lanes[0].timeMS; top > 20000 || top < 14000 {
		t.Errorf("第 0 条轨道记的入场时刻是 %d，want 新窗口（14000..20000）里的某一条"+
			"——回退之后轨道该整份复位", top)
	}
	if _, ok := d.slots["t-20"]; !ok {
		t.Errorf("回退之后当前位置那一条（t-20）没有入场（槽位：%v）", d.slots)
	}
}

// TestTheIncrementalWindowKeepsTheLaneRules 钉住增量路径下的两条既有规则：
// 同轨不叠字、长弹幕按真实字宽离场。
//
// # 为什么还要在增量路径上验一遍
//
// 这两条原来由「每帧扫窗口」那条路径保证。改成游标之后，「谁在活动集合里」
// 由入场那一刻决定，于是规则要能在这条新路径上独立成立：
//
//   - 同一条轨道上相邻两条的入场间隔必须 ≥ max(30% 封面宽 / 速度, 前一条字宽 / 速度)；
//   - 一条 40 字的长弹幕在第 17 秒（右缘还在封面里）必须还在活动集合里、
//     而且真的画在画面上。
func TestTheIncrementalWindowKeepsTheLaneRules(t *testing.T) {
	long := strings.Repeat("长弹幕", 13) + "啊" // 40 个字
	items := []app.DanmakuItem{
		danmakuItemColor("long", 40000, long, danmakuRed),
		// 1.9 秒后入场：过了「走出 30%」那条时间门槛，但前一条比 108 像素宽
		// ⇒ 几何门槛不让它同轨。
		danmakuItemColor("short", 41900, "哈哈哈哈", danmakuGreen),
	}
	// 再加一批密集的短弹幕，把增量路径压在「连续入场」这一档上。
	for i := range 200 {
		items = append(items, danmakuItem(
			fmt.Sprintf("dense-%03d", i), 45000+int64(i)*10, "短"))
	}
	env := danmakuPlayingEnv(t, items, systemIsDark(true))

	// 第 44 秒：两条都还在屏幕上（长的那条滚了 4 秒、短的那条滚了 2.1 秒）
	// ⇒ 它们必须落在不同的轨道上。短的那条比 108 像素窄，光按「走出 30%」
	// 那条时间门槛（1.9 秒 > 1.8 秒）就会跟长的那条同轨，叠一百多像素。
	env.st.Position = 44 * time.Second
	env.tt.Frame()
	d := &env.s.full.danmaku
	longSlot, ok := d.slots["long"]
	if !ok {
		t.Fatalf("第 4 秒长弹幕不在活动集合里（槽位：%v）", d.slots)
	}
	shortSlot, ok := d.slots["short"]
	if !ok {
		t.Fatalf("第 2.1 秒那条短弹幕不在活动集合里（槽位：%v）——它该在另一条轨道上",
			d.slots)
	}
	if longSlot.lane == shortSlot.lane {
		t.Errorf("两条挨得太近却进了同一条轨道（lane %d）——"+
			"1.9 秒里前一条只走了 114 像素，而它自己宽 %.1f ⇒ 叠字",
			longSlot.lane, d.widths["long"])
	}

	// 第 57 秒（长弹幕滚了 17 秒）：它的右缘还在封面里 ⇒ 必须还在活动集合里、
	// 而且真的画在画面上。（短弹幕按真实字宽早就出界了，不该还在。）
	env.st.Position = 57 * time.Second
	env.tt.Frame()
	longSlot, ok = d.slots["long"]
	if !ok {
		t.Fatalf("第 17 秒长弹幕不在活动集合里（槽位：%v）——它按真实字宽还没出界",
			d.slots)
	}
	if _, ok := d.slots["short"]; ok {
		t.Error("第 17 秒那条短弹幕还在活动集合里——它按真实字宽早就出界了")
	}
	if l, _ := redSpan(env.tt, danmakuArea(), longSlot.lane); l < 0 {
		t.Error("第 17 秒时长弹幕在画面上一个像素都没有——它被提前收掉了")
	}

	// 同轨不叠字：活动集合里同一条轨道上的两条，入场间隔必须过得了两条门槛。
	// ⚠️ **用记下来的字宽**（`widths`），不是再量一次——量字宽要排版，而这里
	// 验的是「分配时用的那个数」。
	byLane := map[int][]danmakuActive{}
	for _, a := range d.active {
		byLane[a.slot.lane] = append(byLane[a.slot.lane], a)
	}
	spacingMS := int64(float64(danmakuCoverW) * danmakuTrackSpacingRatio * 1000 / danmakuSpeed)
	for lane, rows := range byLane {
		for i := 1; i < len(rows); i++ {
			prev, cur := rows[i-1], rows[i]
			need := spacingMS
			if geom := int64(float64(d.widths[prev.item.ID]) * 1000 / danmakuSpeed); geom > need {
				need = geom
			}
			if gap := cur.slot.timeMS - prev.slot.timeMS; gap < need {
				t.Errorf("第 %d 条轨道上两条弹幕只隔了 %d 毫秒（门槛 %d）——"+
					"同轨的两条会叠字", lane, gap, need)
			}
		}
	}
	if len(d.active) > danmakuMaxOnScreen {
		t.Errorf("同屏 %d 条，超过上限 %d", len(d.active), danmakuMaxOnScreen)
	}
}
