package ui

// 评论页「点开楼中楼」的 E2E：从**一次真的点击**开始，到**回复出现在画面上**
// 为止，中间不许有替身把 ctx 吞掉。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestOpeningRepliesAsksTheServer
//
// # 这一条为什么必须存在
//
// 2026-10-29 的视觉走查发现：点开「共 N 条回复」**完全没有反应**——那一趟拿到
// 的是一个已经取消的 ctx，一个包都没发出去。而当时**没有一条测试盯着这件事**：
// `internal/app` 那几条的替身签名是 `func(_ context.Context, …)`，ctx 被直接
// 丢掉，于是每一趟都「成功」。
//
// 这一条走**应用层 + 界面层一起**：点按钮（`tt.Click`）→ 界面记下意图 →
// 应用层发请求（走 `newCommentBili` 那台假 B 站）→ 结果回来 → 行重建 →
// 回复的昵称出现在画面上。中间任何一环断了，它就会红。
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器（`commentBili`）。

import (
	"testing"
)

// TestOpeningRepliesAsksTheServer 钉住「点开楼中楼真的会去问、并且画出来」。
func TestOpeningRepliesAsksTheServer(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	// 先等主列表画出来（表情图下来 = 第一屏画完了）。
	commentsWait(t, tt, "主列表到位", func() bool { return magentaPixels(tt) > 0 })

	// 点「共52条回复」那一枚（楼中楼的入口）。
	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共52条回复」：%v（文字：%q）", err, tt.Texts())
	}

	// 这一趟必须真的出网：假 B 站那边要收到 `/x/v2/reply/reply`。
	commentsWait(t, tt, "楼中楼请求发出去了", func() bool {
		return b.replyCallCount() > 0
	})

	// 而且结果要画出来（回复那一行的昵称来自 fixture：缺课小虎牙）。
	commentsWait(t, tt, "回复画出来了", func() bool {
		return tt.HasText("缺课小虎牙")
	})

	// 行也要真的多出来：楼中楼是**另一行**（不是塞在主评论正文里）。
	replies := 0
	for _, row := range s.comments.rows {
		if row.reply {
			replies++
		}
	}
	if replies == 0 {
		t.Errorf("行里一条楼中楼都没有（共 %d 行）", len(s.comments.rows))
	}
}
