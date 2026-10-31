// 「评论正文能拖选复制」（票 #702）的 E2E：从**一次真的拖选**开始，到**正文
// 原文出现在剪贴板里**为止，中间不许有替身把 ctx 吞掉。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestDraggingACommentBodyCopiesItEndToEnd
//
// # 这一条为什么必须存在
//
// 用户看到的症状是「正文一个字都复制不出来」——而**判据是剪贴板里的原文**，
// 不是「元素上有没有 Selectable」。
//
// 🔴 **它验的是整条链**：拖选 → 框架的 `pressTextSelection` → 共享选择 →
// `CopyData` → **系统剪贴板**。少接一根线（比如 `CopyData` 挂在正文自己身上
// 而不是作用域那一格），上面那些用例里**只有一条**会红——而用户那边是**一句
// 话都复制不出来**。
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器（`commentBili`）。
package ui

import (
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestDraggingACommentBodyCopiesItEndToEnd 钉住「拖选评论正文之后，剪贴板里
// 是正文原文」。
//
// 🔴 **判据是剪贴板的原文**：清空 → 拖选 → 按 ⌘C → 读。
//
// ⚠️ **⌘C 走的是应用自己的那一路**（`tt.Key(ui.Cmd, ui.KeyC)`），而不是
// `tt.Command("copy")`（那是菜单那一路）——两条路不一样，而用户按的是**键**。
func TestDraggingACommentBodyCopiesItEndToEnd(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	first := s.app.Comments()[0]
	body := commentPlainOf(first)
	if body == "" {
		t.Fatalf("夹具里第一条评论没有正文")
	}
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(body)
		return ok
	})

	dragOverFirstLine(t, tt, body)

	tt.SetClipboard("")
	tt.Key(mygoui.Cmd, mygoui.KeyC)
	tt.Frame()

	if got := tt.Clipboard(); got != body {
		t.Errorf("拖选正文之后按 ⌘C，剪贴板里是 %q，want %q", got, body)
	}
}
