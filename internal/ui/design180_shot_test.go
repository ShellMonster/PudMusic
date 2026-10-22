// 票 #180 的画面验收：右下角那条提示的四种语义色，和它的 250ms 入场。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDesign180Shots
//
// **深浅两套都出**（DESIGN §8）：那三个语义色深浅同值，而**主色那一档**在
// 深色底下不一样；入场那一段在两套里也该是同一段。
//
// ⚠️ **图是给人看的，判据在 `toast_test.go` 里**：这里只负责把画面摆出来，
// 免得「像素对了但画面难看得没人要」。
package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// TestRenderDesign180Shots 把四种语义色和入场那一段存成 PNG。
func TestRenderDesign180Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ── 四种语义色 ─────────────────────────────────────────────
			//
			// ⚠️ **分两轮**：同时最多 3 条（`toastLimit`），四种一起画的话
			// 最旧的那条会被挤掉——图上就少一种颜色。
			for i, round := range [][]struct {
				tone notice.Tone
				text string
			}{
				{
					{notice.Success, "已加进下载队列"},
					{notice.Danger, "下载失败：网络不通"},
					{notice.Warning, "这个收藏夹还没接 B 站接口"},
				},
				{
					{notice.Plain, "从 Biu 导入了一批数据"},
					{notice.Success, "已加进下载队列"},
					{notice.Danger, "下载失败：网络不通"},
				},
			} {
				b := newTestShell(t, systemIsDark(dark))
				s, tt := b.s, b.tt
				shortToasts(t, time.Minute)
				for _, one := range round {
					s.addToast(one.text, one.tone)
				}
				tt.Frame()
				settleToast(t, tt)
				save(t, dir, fmt.Sprintf("102-提示-语义色-%d-%s.png", i+1, name), tt)
			}

			// ── 入场那一段：刚出现 / 中间 / 到位 ───────────────────────
			eb := newTestShell(t, systemIsDark(dark))
			e, et := eb.s, eb.tt
			shortToasts(t, time.Minute)
			e.addToast("它是从右边滑进来的", notice.Plain)
			et.Frame()
			save(t, dir, "103-提示-入场-刚出现-"+name+".png", et)
			et.Advance(60 * time.Millisecond)
			et.Frame()
			save(t, dir, "104-提示-入场-中间-"+name+".png", et)
			settleToast(t, et)
			save(t, dir, "105-提示-入场-到位-"+name+".png", et)
		})
	}
}
