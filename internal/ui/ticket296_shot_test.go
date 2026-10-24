// 票 #296 的画面验收：历史记录页头右侧那一句「记录播放历史」。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket296Shots
//
// # 深浅两套都要出
//
// 那一组是**开关 + 一句话**：浅色下那句话是深灰近黑、深色下是浅灰——同一句字
// 两套颜色，所以只出浅色的话，深色下「那句话到底有没有画出来」看不出来（深色
// 底上的开关影子也是深色的，容易把「有字」和「只有一枚旋钮」看混）。
//
// ⚠️ **一个字都不许打真 B 站接口**：这一页的数据是内存夹具，封面走本机
// `httptest`。
package ui

import (
	"os"
	"testing"
)

// TestRenderTicket296Shots 把历史记录那一页存成 PNG，深浅各一套。
func TestRenderTicket296Shots(t *testing.T) {
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
			tt, _ := historyHeaderShell(t, systemIsDark(dark))
			// 整窗：页头右侧那句话在右上角。
			save(t, dir, "109-历史记录-"+name+".png", tt)
			// 右上角放大：开关 + 那句话 + 它们右沿（内容区的右沿）。
			saveRect(t, dir, "110-历史记录-页头右侧-放大-"+name+".png", tt, 980, 58, 204, 48)
		})
	}
}
