// 更新日志的滚动条是 overlay，画在滚动容器右缘往里的 16 像素上。
// 正文铺满整行时，最后一两个字会被它盖住；这里钉住日志正文给滚动条留出的槽。
package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
)

// updateNotesProbe 是一句足够长的日志正文，重复几次就会让弹窗里的滚动区出现滚动条。
const updateNotesProbe = "这一行用来量更新日志的右边距，它足够长，长到会占满整行并在弹窗里折成好几行。"

func TestTheUpdateNotesLeaveRoomForTheScrollbar(t *testing.T) {
	notes := strings.Repeat(updateNotesProbe, 12)
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0", Notes: notes})
	f.checkUpdate(t)
	f.openDialog(t)
	f.tt.Frame()

	para, ok := f.tt.Find(notes)
	if !ok {
		t.Fatalf("弹窗上没有那段日志正文，文字有 %q", f.tt.Texts())
	}

	// 日志正文的宽 = 弹窗宽减两侧 24 的内边距。滚动条轨道占右缘往里的
	// scrollbar.HotZone 那一条，正文必须让开它。
	limit := float32(updateDialogWidth-2*space.Px24) - float32(scrollbar.HotZone)
	if para.W > limit+0.5 {
		t.Errorf("更新日志正文宽 %.1f，滚动条槽只留出 %.1f，正文压进了 %.1f",
			para.W, limit, para.W-limit)
	}
	contentRight := para.X + float32(updateDialogWidth-2*space.Px24)
	if f.s.update.scroll.MaxY <= 0 {
		t.Fatal("测试用的日志没有溢出，滚动条不会出现")
	}

	if dir := os.Getenv("BIU_SHOT_DIR"); dir != "" {
		f.tt.Move(contentRight-8, para.Y+4)
		f.tt.Frame()
		save(t, dir, "update-notes-scrollbar.png", f.tt)
	}
}
