// 弹幕批次的**身份**（票 #670 的第二半，问题 #670 记的那一条）。
//
// # 它钉的是哪一个坏
//
// `sync` 原来拿「条数 + 第一条的 rowid」认「这一份是不是新的」。可 B 站补发
// 或刷新时，**两个不同的批次完全可以条数相同、第一条也相同**（前几百条是
// 同一段历史，末了几条换掉）。那时 `count` 与 `first` 都没变 ⇒ 上一层当成
// 「同一份」，可 `itemsByTime` 已经换成新的了：游标停在旧位置、`slots` 里
// 留着上一批的条目、锚点也还是旧的——屏幕上要么一条弹幕都不出，要么旧的
// 那几条一直在原处滚。
//
// ⇒ 身份判据必须覆盖「整份」的边界：首条、末条、条数，以及时间轴的首尾。
package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestABatchWithTheSameLengthAndFirstItemIsStillANewBatch 钉住「条数与首条
// 都相同、但末条不同」时整份复位。
//
// # 判据是模型状态，不是像素
//
// 这一条要验的是「它被当成新的一份了吗」，而复位会留下三处痕迹：
//
//	slots 空       旧批次的轨道/槽位全部作废
//	lastBaseMS 重钉 锚点按这一帧的读数重新钉
//	itemsByTime    换成新批次（并按时间重排）
func TestABatchWithTheSameLengthAndFirstItemIsStillANewBatch(t *testing.T) {
	first := danmakuItem("shared", 40000, "两批都有的第一条")
	oldTail := danmakuItem("old-tail", 41000, "上一批的末条")
	newTail := danmakuItem("new-tail", 41100, "这一批的末条")

	env := danmakuPlayingEnv(t, []app.DanmakuItem{first, oldTail}, systemIsDark(true))
	env.tt.Frame()

	d := &env.s.full.danmaku
	if !d.ready || len(d.slots) == 0 {
		t.Fatalf("第一帧没有把弹幕建起来（ready=%v slots=%v）", d.ready, d.slots)
	}
	if d.count != 2 || d.first != "shared" {
		t.Fatalf("第一份的身份是 count=%d first=%q", d.count, d.first)
	}

	// 同一位置、同一份开头，**只换掉末条**：条数还是 2，首条还是 "shared"。
	env.st.Position = 42 * time.Second
	env.s.app.SetDanmakuForTest([]app.DanmakuItem{first, newTail})
	env.tt.Frame()

	if _, ok := d.slots["old-tail"]; ok {
		t.Errorf("上一批的末条还在 slots 里：%v——末条换了就是新的一份，轨道该清空", d.slots)
	}
	if !d.ready {
		t.Fatal("换了末条之后弹幕层不再 ready")
	}
	// 锚点重钉到这一帧的读数上（旧锚点是 41.5 秒那一带的读数）。
	if d.baseMS != 42000 {
		t.Errorf("换份之后锚点是 %d，want 42000——它没有被重新钉到这一帧的读数上", d.baseMS)
	}
	if d.cursor < 0 || d.cursor > len(d.itemsByTime) {
		t.Fatalf("游标 %d 落在新的时间索引（%d 条）之外", d.cursor, len(d.itemsByTime))
	}
	// 新批次的第一条仍然能分配轨道（旧的轨道门槛不该把新批次挤掉）。
	if _, ok := d.slots["shared"]; !ok {
		t.Errorf("新批次的第一条没有分配到轨道：%v", d.slots)
	}
}

// TestABatchWithTheSameIDsButAShiftedTailIsStillANewBatch 钉住指纹里的
// `TimeMS` 那一半：首末 rowid 与条数都一样，但末条的时间轴挪了。
//
// B 站补发/刷新时重新给一遍时间戳是可能的，而那时**任何一条弹幕该不该在
// 屏幕上、该滚多远**都变了；沿用旧锚点与旧槽位，画面就会停在上一次的位置。
func TestABatchWithTheSameIDsButAShiftedTailIsStillANewBatch(t *testing.T) {
	head := danmakuItem("head", 40000, "第一条")
	tail := danmakuItem("tail", 41000, "末条")

	env := danmakuPlayingEnv(t, []app.DanmakuItem{head, tail}, systemIsDark(true))
	env.st.Position = 41 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	if _, ok := d.slots["tail"]; !ok {
		t.Fatalf("第一份的末条没有进槽位：%v", d.slots)
	}

	// 首末 rowid、条数都一样，只有末条的时间轴挪了 5 秒。
	env.st.Position = 46 * time.Second
	env.s.app.SetDanmakuForTest([]app.DanmakuItem{head, danmakuItem("tail", 46000, "末条")})
	env.tt.Frame()

	if d.baseMS != 46000 {
		t.Errorf("末条的时间轴变了，可是锚点还是 %d（want 46000）——这一份被当成了同一批", d.baseMS)
	}
	// 复位之后旧槽位不该留着：末条新的入场时间是 46 秒，它自己是新分配的那一条。
	if slot, ok := d.slots["tail"]; !ok || slot.timeMS != 46000 {
		t.Errorf("末条的槽位还是旧的：%+v（want timeMS=46000）", d.slots["tail"])
	}
}
