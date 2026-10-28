// 「丢掉的那几条」的账本和它在界面上的话。
//
// # 两件事，各有各的坑
//
// **一、计数按「不同的动态」算，不按「出现过几次」算。**
//
// B 站翻页**会重叠**：同一批画不出来的动态在两页里各出现一次。按出现次数
// 记的话，界面上那个数会**偏大**——翻了三页就可能是同一个数的三倍，而用户
// 看到的是「有 12 条落在别的形态上了」，真相是「有 4 条」。
//
// **二、每一类理由各说一句，不许合并成一句。**
//
// 「B 站返回了一种我们不认识的形态」和「这一条我们读不懂（B 站改了返回的
// 形状）」是**两回事**：后者是**应用自己**跟不上 B 站的返回，不是那个 UP 主
// 发了什么。说成同一句就是**对用户说错话**（票 #344）。
package ui

import (
	"fmt"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// dropTally 是「丢掉的那几条」的账本。
//
// ⚠️ **它按身份记，不按次数记**（理由见文件头）：`seen` 是已经数过的那几条
// 的身份，同一条在两页里各出现一次也只算一次。
type dropTally struct {
	// seen 是已经数过的那几条的身份（`bili.DroppedDynamic.Key`）。
	seen map[string]bool
	// counts 是按类别的计数。
	counts bili.DropCounts
}

// add 记下这一页丢掉的那几条。
//
// ⚠️ **同一条只算一次**：B 站翻页会重叠，而那一条的**身份**（`Key`）在两页
// 里是一样的（票 #344）。
func (t *dropTally) add(drops []bili.DroppedDynamic) {
	if len(drops) == 0 {
		return
	}
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	for _, d := range drops {
		if t.seen[d.Key] {
			continue
		}
		t.seen[d.Key] = true
		t.counts.Add(d.Reason)
	}
}

// total 是丢掉的总条数（按不同的动态算）。
func (t *dropTally) total() int { return t.counts.Total() }

// dropCountsOf 是**这一页**丢掉的那几条按类别的计数（给日志用的）。
func dropCountsOf(drops []bili.DroppedDynamic) bili.DropCounts {
	var c bili.DropCounts
	for _, d := range drops {
		c.Add(d.Reason)
	}
	return c
}

// dropCountText 是按类别的计数（给日志看的一行字）。
func dropCountText(c bili.DropCounts) string {
	return fmt.Sprintf("形态不认识 %d、读不懂 %d、没有身份 %d、什么也没有 %d、"+
		"投稿没有稿件号 %d、还不认识的类别 %d",
		c.ShapeUnknown, c.Unreadable, c.NoIdentity, c.Empty, c.NoArchive, c.Other)
}

// dropPhrases 是「丢掉的那几条」按类别说的话，**一类一句**。
//
// ⚠️ **一类一句，不许合并成一句**（票 #344）：合并之后「我们读不懂」就被
// 说成「落在别的形态上了」，而那句话对用户是**错的**——B 站不是换了一种
// 形态，是**它返回的形状变了**。
//
// ⚠️ **「投稿没有稿件号」不许混进「什么也没有」**（票 #344 的评审）：那一类
// **带着正文**（UP 写的那段话就在里面），只是稿件号是空的、放不出来。
// 说成「什么也没有」就是**对用户说错话**——听起来像「他什么都没发」。
//
// 返回的顺序是固定的（形态 → 读不懂 → 没有身份 → 什么也没有 → 投稿没有稿件号
// → 还不认识的类别），所以同一份计数每次说出来都是同一句。
func dropPhrases(c bili.DropCounts) []string {
	var out []string
	if c.ShapeUnknown > 0 {
		out = append(out, fmt.Sprintf("有 %d 条落在别的形态上了：%s", c.ShapeUnknown, dynamicShapesNote))
	}
	if c.Unreadable > 0 {
		out = append(out, fmt.Sprintf("有 %d 条我们读不懂（B 站改了返回的形状）", c.Unreadable))
	}
	if c.NoIdentity > 0 {
		out = append(out, fmt.Sprintf("有 %d 条没有身份，画不出来", c.NoIdentity))
	}
	if c.Empty > 0 {
		out = append(out, fmt.Sprintf("有 %d 条什么也没有，画不出来", c.Empty))
	}
	if c.NoArchive > 0 {
		out = append(out, fmt.Sprintf("有 %d 条是投稿却放不出来（没有稿件号）", c.NoArchive))
	}
	if c.Other > 0 {
		out = append(out, fmt.Sprintf("有 %d 条属于还不认识的类别", c.Other))
	}
	return out
}

// dropSentence 是那些话拼成的一句（空状态和那一行小字共用同一份）。
func dropSentence(c bili.DropCounts) string {
	return strings.Join(dropPhrases(c), "；")
}
