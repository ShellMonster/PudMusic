// 评论页（票 #573）的**画面验收**：整页、页头、一条评论、播放条那一枚键，
// 深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderCommentsShots
//
// # 出图是材料，审查才是那道门
//
// 对照的是**网易云移动端评论页**的形态（研究票 #537 报告 §1.1）：
//
//	返回 → 歌曲信息区 → 歌手信息区 → 评论列表 → 输入区
//
// ⚠️ **输入区我们没有**：这一页是**只读**的（票 #573 的纪律：不发任何写
// 接口），所以那一块换成「不摆输入框」——而这一点要在审查里写清楚。
//
// ⚠️ **一个字都不许打真 B 站**：数据走一台假 B 站（回的是真 fixture），图走
// 一台假图床（AGENTS.md §6）。
package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestRenderCommentsShots 把评论页改过的那几处各存一张 PNG，深浅各一套。
func TestRenderCommentsShots(t *testing.T) {
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
			b := newCommentBili(t, "reply-main.json")
			_, tt := newCommentsShell(t, b, mvItem(), 1400, 1000, systemIsDark(dark))

			// 等表情图下来（图片是后台下的，图好了才画得出来）。
			commentsWait(t, tt, "表情图下来了", func() bool {
				return magentaPixels(tt) > 0
			})

			// ── 整页 ──────────────────────────────────────────────
			save(t, dir, fmt.Sprintf("10-评论页-整页-%s.png", name), tt)

			// ── 页头（歌曲信息区 + 那一枚角标 + 排序 + 刷新）──────
			//
			// ⚠️ **裁的是页头那一整条**（从歌曲信息区到刷新那一枚），
			// 而不只是角标——角标和标题、和排序档、和刷新键之间的**间距**
			// 才是这一页最要紧的「层级」。
			if r, ok := tt.Find("这个视频的评论"); ok {
				saveRect(t, dir, fmt.Sprintf("11-评论页-页头-%s.png", name), tt,
					int(r.X)-420, int(r.Y)-46, 900, 110)
			} else {
				t.Fatalf("页头上没有「这个视频的评论」，文字有 %q", tt.Texts())
			}

			// ── 一条评论（头像 / 昵称 / 等级 / 徽章 / 正文 / 计数）──
			if r, ok := tt.Find("大圆镜科普"); ok {
				saveRect(t, dir, fmt.Sprintf("12-评论页-一条评论-%s.png", name), tt,
					int(r.X)-52, int(r.Y)-26, 1000, 190)
			} else {
				t.Fatalf("画面上没有第一条评论，文字有 %q", tt.Texts())
			}

			// ── 楼中楼那一行（缩进 + 头像小一号）──────────────────
			if r, ok := tt.Find("耄耋_不是耄"); ok {
				saveRect(t, dir, fmt.Sprintf("13-评论页-第二条-%s.png", name), tt,
					int(r.X)-52, int(r.Y)-26, 1000, 150)
			}

			// ── 楼中楼展开之后（点回复数）─────────────────────────
			//
			// ⚠️ **要点真的点**（`tt.Click`）：楼中楼是**点回复数**才展开的
			// （只读，不发写请求），而那一段在折叠状态下根本不在画面上。
			if err := tt.Click("共52条回复"); err != nil {
				t.Fatalf("点不动「共 52 条回复」：%v（文字：%q）", err, tt.Texts())
			}
			commentsWait(t, tt, "楼中楼展开了", func() bool {
				return tt.HasText("缺课小虎牙")
			})
			saveRect(t, dir, fmt.Sprintf("15-评论页-楼中楼-%s.png", name), tt,
				220, 150, 1180, 420)

			// ── 播放条上那一枚键（900 那一档最挤）────────────────
			//
			// ⚠️ **900 是应用自己的窗口下限**（`WindowOptions.MinWidth`），
			// 而左列的保底宽（272）在那一档撑出轨道最多——所以那一档最该看。
			for _, w := range []int{900, 1200} {
				pb, _ := commentsPlaybarTester(t, audioItem(), w, systemIsDark(dark))
				save(t, dir, fmt.Sprintf("14-播放条-评论键-%d-%s.png", w, name), pb)
			}

			// ── 满屏播放器里那一枚键（满屏里它是唯一的入口）────
			full, _ := commentsFullTester(t, audioItem(), systemIsDark(dark))
			save(t, dir, fmt.Sprintf("17-满屏-评论键-%s.png", name), full)
		})
	}
}

// TestRenderCommentsLongTitleShot 出**超长标题**那一张：角标必须还在标题
// 旁边（不许被推出那一格），而标题自己省略号。
func TestRenderCommentsLongTitleShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := newCommentBili(t, "reply-main.json")
	it := mvItem()
	it.Title = "这是一个非常非常非常非常非常非常长的视频标题，长到一行根本放不下，" +
		"而角标必须还在它的旁边，不许被推出那一格"
	_, tt := newCommentsShell(t, b, it, 1400, 1000)
	commentsWait(t, tt, "评论画出来了", func() bool {
		return tt.HasText("大圆镜科普")
	})
	save(t, dir, "16-评论页-长标题.png", tt)
}

// TestRenderCommentsFoldedEmoteShot 出**长正文被折起来、而那枚表情仍然是图**
// 那一张。
//
// 🔴 **这一张是评审提的那条缺陷的画面证据**（票 #573 第三轮）：截断不许把
// 表情占位符切成半个——切坏了的话，画面上留下的是 `[妙啊` 三个字（而不是
// 一枚图）。
func TestRenderCommentsFoldedEmoteShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBodyAndEmote(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1000)

	// 等表情图下来（图片是后台下的，图好了才画得出来）。
	commentsWait(t, tt, "表情图下来了", func() bool {
		return magentaPixels(tt) > 0
	})

	// 画面上不许有表情的字面量。
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "[妙啊") {
			t.Fatalf("画面上留着表情的字面量 %q——它该是一张图", txt)
		}
	}

	if r, ok := tt.Find("大圆镜科普"); ok {
		saveRect(t, dir, "18-评论页-长正文带表情.png", tt,
			int(r.X)-52, int(r.Y)-26, 1140, 320)
		// ⚠️ **正文那一列比这一张宽**（窗口 1400，而这一张只裁了 1140）：
		// 而**折叠的末尾（那个省略号）在正文那一列的最右端**——所以另裁一张
		// 右半边，那里有「折起来了」的那句话。
		saveRect(t, dir, "19-评论页-长正文折起来.png", tt,
			int(r.X)-52+1000, int(r.Y)-26, 340, 320)
	} else {
		t.Fatalf("画面上没有第一条评论，文字有 %q", tt.Texts())
	}
}
