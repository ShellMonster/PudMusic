package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 这一族存**更新弹窗**的图：深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket235Shots
//
// # 存的是「这一层的层次」
//
// 弹窗里有三行字，各自有各的字号：标题「✨ 有新版本更新」16、版本那一行
// 「当前版本 0.1.0 → 0.2.0」13、日志正文 12。层次**只靠字号**说话——
// 票 #235 把标题从 16/700 改成 16/400 之后，版本那一行如果还留着 700，就成了
// 整个弹窗里唯一最粗的字（它比标题还小一号），层次反了。所以那一行一起改成
// 13/400（`update_dialog.go` 的 `updateVersionLine`）。
//
// 这一族拍的就是那个：一张图里三行字的粗细该是「标题 = 版本行 = 正文」，
// 只有字号在变。
func TestRenderTicket235Shots(t *testing.T) {
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
			// ⚠️ **必须带 `systemIsDark`**：不带的话「深色」那一档存出来和「浅色」
			// 逐字节相同（票 #241 的评审在确认弹窗那一族踩过这个）。
			f := newUpdFixture(t, &app.UpdateRelease{
				Version: "0.2.0",
				Notes:   "## 修好了什么\n\n- 播放列表不再乱序\n- 歌词滚到底会停\n",
			}, systemIsDark(dark))
			f.checkUpdate(t)
			f.openDialog(t)
			save(t, dir, "97-更新弹窗-"+name+".png", f.tt)
		})
	}
}
