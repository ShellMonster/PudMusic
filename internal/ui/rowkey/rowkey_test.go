package rowkey

import (
	"fmt"
	"strings"
	"testing"
)

// 这组测试量的是**「记着的那一份」到底省不省**。
//
// 这一票（#313）要的就是两件事：
//
//   - **数据换了要跟上**（不然就是铁律四的回归：前面插一行，后面所有行的状态
//     整体错配）；
//   - **数据没换就别再算**（省下的就是每帧每行那一次分配）。
//
// 下面每一条各盯一头。

// packCalls 记着一个 pack 被调了几次。
type packCalls struct {
	n int
}

// count 是那个 pack：它数自己被调了几次，然后把原料原样还回去。
func (p *packCalls) count(raw string) string {
	p.n++
	return "[" + raw + "]"
}

// TestTheSameRawIsNotRecomputed 盯「数据没换就别再算」。
func TestTheSameRawIsNotRecomputed(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}

	if got := m.At(0, "a", p.count); got != "[a]" {
		t.Fatalf("第一次算出来的是 %q，want %q", got, "[a]")
	}
	if p.n != 1 {
		t.Fatalf("第一次问就算了两遍（pack 调了 %d 次）", p.n)
	}

	// 再问十遍：原料没变，那一份是记着的，不该再算。
	for range 10 {
		if got := m.At(0, "a", p.count); got != "[a]" {
			t.Fatalf("问出来的是 %q，want %q", got, "[a]")
		}
	}
	if p.n != 1 {
		t.Errorf("同样的原料问了十一遍，pack 被调了 %d 次——"+
			"「记着的那一份」没起作用，每帧每行那一次分配还在", p.n)
	}
}

// TestAChangedRawIsRecomputed 盯「数据换了要跟上」。
//
// ⚠️ 这是**最要紧的一条**：漏了它就是铁律四的回归——行身份停在旧数据上，
// 前面插一行之后后面所有行的选中、滚动、正在放的高亮整体错配，
// 而界面上每一个字都还是对的，用户永远查不出来。
func TestAChangedRawIsRecomputed(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}

	m.At(0, "a", p.count)
	if got := m.At(0, "b", p.count); got != "[b]" {
		t.Errorf("原料换成 b 之后算出来的是 %q，want %q", got, "[b]")
	}
	if p.n != 2 {
		t.Errorf("原料变了却没重算（pack 调了 %d 次，want 2）", p.n)
	}
}

// TestTheValueGoesBackToTheOldRawWhenItComesBack 盯「换回去也对」。
//
// 数据换了又换回来是常事（用户把一首歌从队列里删掉又加回来）。那一份记着的
// 应该是**当前**原料对应的那一份，不是「第一次算的那个」。
func TestTheValueGoesBackToTheOldRawWhenItComesBack(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}

	m.At(0, "a", p.count)
	m.At(0, "b", p.count)
	if got := m.At(0, "a", p.count); got != "[a]" {
		t.Errorf("换回 a 之后算出来的是 %q，want %q", got, "[a]")
	}
}

// TestAskingForALaterRowFirstStillWorks 盯「乱着问也不会算错」。
//
// 框架有时候会跳着问（`mygo/ui/list.go` 的 `find` 从某个下标往两边找），
// 所以「第 5 行」有可能比「第 0 行」先被问到。
func TestAskingForALaterRowFirstStillWorks(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}

	if got := m.At(5, "f", p.count); got != "[f]" {
		t.Fatalf("先问第 5 行算出来的是 %q，want %q", got, "[f]")
	}
	// 再问第 0 行：它该拿到**自己**那一份，而不是第 5 行那份。
	if got := m.At(0, "a", p.count); got != "[a]" {
		t.Errorf("后问第 0 行算出来的是 %q，want %q", got, "[a]")
	}
	// 第 5 行那份还在。
	if got := m.At(5, "f", p.count); got != "[f]" {
		t.Errorf("再问第 5 行算出来的是 %q，want %q", got, "[f]")
	}
}

// TestTheZeroValueIsUsable 盯「不用建」。
//
// 这一票的每一处用法都是「这个结构体多了一个字段」——而那些结构体有一半是
// **零值造出来的**（测试里 `&upSpace{}` 那种）。所以零值必须能用，
// 不然一换页就崩。
func TestTheZeroValueIsUsable(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}
	if got := m.At(0, "a", p.count); got != "[a]" {
		t.Fatalf("零值那份算出来的是 %q，want %q", got, "[a]")
	}
}

// TestTheFastPathDoesNotAllocate 盯「省下的那一次分配真的省了」。
//
// ⚠️ **这是这一票的判据**：现算的写法每帧每行一次分配，而这一份记着的
// 写法一次都不该有。判据用**分配次数**（不是时间）——分配次数不受机器快慢
// 影响（见 `internal/ui/framebudget_test.go` 的文件头）。
func TestTheFastPathDoesNotAllocate(t *testing.T) {
	var m Memo[string, string]
	p := &packCalls{}
	m.At(0, "a", p.count)

	allocs := testing.AllocsPerRun(100, func() {
		_ = m.At(0, "a", p.count)
	})
	if allocs != 0 {
		t.Errorf("问一次记着的那一份要分配 %.0f 次，want 0——"+
			"这一票省的就是这个（每帧每行一次）", allocs)
	}
}

// TestANegativeIndexPanicsWithAMessageYouCanRead 盯「负下标不许变成天书」。
//
// 契约是「下标必须非负」（见 `Memo.At` 的注释）：负下标没有对应的行，它是
// **调用方的 bug**，不是「问不到」。
//
// ⚠️ **判据是「panic 的话能不能看懂」**，不是「有没有 panic」：不这么写的话，
// 负下标会变成 `m.rows[-1]` 越界，抛出来的是
// `runtime error: index out of range [-1]`——那一句只说「越界了」，不说
// 「哪一处越界」，下一个人得自己顺着调用栈找（本票就是被这个绊住的）。
//
// 所以这里断两件事：话里**带上那个下标**，而且**说得出是「非负」**这件事。
func TestANegativeIndexPanicsWithAMessageYouCanRead(t *testing.T) {
	var m Memo[string, string]

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatalf("负下标既没 panic 也没报错——它要么悄悄算错，要么变成天书")
		}
		msg := fmt.Sprint(rec)
		if !strings.Contains(msg, "-1") {
			t.Errorf("panic 的话里没有那个下标：%q", msg)
		}
		if !strings.Contains(msg, "非负") {
			t.Errorf("panic 的话没说出「下标必须非负」：%q", msg)
		}
		if strings.Contains(msg, "index out of range") {
			t.Errorf("panic 的话是运行时的天书（%q）——那正是这一条要挡的东西", msg)
		}
	}()

	m.At(-1, "a", func(s string) string { return "[" + s + "]" })
}

// TestIdentityBoxesTheStringItself 盯「装箱的那一份和原来那个是同一个意思」。
//
// ⚠️ 框架那边是拿 key **比相等**的（`mygo/ui/list.go` 的 `find`：`s.Key(lo) == k`），
// 所以装出来的 `any` 必须和直接返回那个 string 时**比相等**——不然行身份
// 对不上，框架按 key 对位就全乱了。
func TestIdentityBoxesTheStringItself(t *testing.T) {
	a := Identity("BV1xx411c7mD")
	b := Identity("BV1xx411c7mD")
	if a != b {
		t.Errorf("两个装出来的身份 %v 和 %v 比不相等——框架按 key 对位会失效", a, b)
	}
	if s, ok := a.(string); !ok || s != "BV1xx411c7mD" {
		t.Errorf("装出来的是 %v（%T），want 那个 string 本身", a, a)
	}
}

// TestIdentity64BoxesTheNumberItself 同上，只是原料是数字。
func TestIdentity64BoxesTheNumberItself(t *testing.T) {
	a := Identity64(12345)
	b := Identity64(12345)
	if a != b {
		t.Errorf("两个装出来的身份 %v 和 %v 比不相等", a, b)
	}
	if n, ok := a.(int64); !ok || n != 12345 {
		t.Errorf("装出来的是 %v（%T），want 那个数字本身", a, a)
	}
}
