// 票 #290 的画面验收：日期筛选排贴左（L-01）、稍后再看页头右上角的「清除已看完」
// （L-04）、下载记录的筛选排在卡片里面（L-06）、队列抽屉表头的条数紧跟标题（S-10）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket290Shots
//
// # 深浅两套都要出
//
// 这一票改的每一处**在深浅其中一套上才看得出来**：深色下那张卡片和页面底色几乎
// 同色（分开的正是那圈影），浅色下那圈影是 1 像素的黑边。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"
)

// TestRenderTicket290Shots 把这一票改过的四处各存一张 PNG，深浅各一套。
func TestRenderTicket290Shots(t *testing.T) {
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
			// ── L-01 + L-04：稍后再看那一页 ──────────────────────────
			//
			// ⚠️ **要带内容**：日期筛选排和页头右上角那枚按钮都在有内容时才
			// 看得出位置（空状态那一支压根不画页头）。
			f := newLibFake(t).withLater(1, 3, "歌 甲", "歌 乙", "歌 丙")
			tt, _ := libraryShellOpts(t, f, laterPath, systemIsDark(dark))
			waitFrames(t, tt, "稍后再看画出来了", func() bool {
				return tt.HasText("清除已看完")
			})
			// 指针停在内容区右上那片空地上，别停在任何一格按钮上。
			tt.Move(1180, 770)
			tt.Frame()
			save(t, dir, "01-稍后再看-整窗-"+name+".png", tt)
			// 页头那一块放大：右上角那枚「清除已看完」。
			saveRect(t, dir, "02-稍后再看-清除已看完-"+name+".png", tt, 980, 50, 220, 60)
			// 日期筛选排那一块放大：它贴内容区的左缘。
			saveRect(t, dir, "03-稍后再看-日期筛选排贴左-"+name+".png", tt, 200, 95, 700, 50)

			// ── L-06：下载记录（筛选排在卡片里面）────────────────────
			_, dtt := newRows(t, dlThreeShots()...)
			dtt.SetDark(dark)
			dtt.Move(1180, 770)
			dlFrame(dtt)
			save(t, dir, "04-下载记录-整窗-"+name+".png", dtt)
			// 卡片上沿那一块放大：筛选排在卡片**里面**（卡片边在筛选排上面）。
			saveRect(t, dir, "05-下载记录-筛选排在卡片里-"+name+".png", dtt, 200, 95, 700, 90)

			// ── S-10：队列抽屉表头（条数紧跟标题）────────────────────
			h := queueDrawerWithTwoRows(t)
			h.tt.SetDark(dark)
			h.tt.Move(600, 300)
			h.tt.Frame()
			save(t, dir, "06-队列抽屉-整窗-"+name+".png", h.tt)
			// 表头那一块放大：「播放列表(2)」连着，后头是两枚图标。
			saveRect(t, dir, "07-队列抽屉-表头条数-"+name+".png", h.tt,
				winW-queueDrawerWidth, 0, queueDrawerWidth, queueHeaderHeight)
		})
	}
}
