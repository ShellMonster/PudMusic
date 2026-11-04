// 播放条三个弹层的画面验收（T8 A：分集、出错详情、封面右键，原型 `popHtml('parts' / 'err' / 'cover')`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarPopoverShots
//
// 深浅两套。对照 `ref/{dark,light}-pop-*.png` 的 `.pop` 外观（surface 底、1 像素 line 描边、圆角 12、
// 内边距 8、最小宽 200、选项 36 高）。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

func TestRenderPlayBarPopoverShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name+"-parts", func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false, at(1200, 800), systemIsDark(dark))
			partsReady(t, s, tt, 3)
			openPartsDrawer(t, tt)
			homeFrame(tt)
			save(t, dir, name+"-pop-parts.png", tt)
		})
		t.Run(name+"-err", func(t *testing.T) {
			st := app.State{Err: errLongSentence, CanRetry: false, BiliURL: "https://www.bilibili.com/video/BV1xx"}
			tt, _ := errBarTester(t, &st, 1200, 800, dark)
			openErrDetail(t, tt, errLongSentence)
			homeFrame(tt)
			save(t, dir, name+"-pop-err.png", tt)
		})
		t.Run(name+"-cover", func(t *testing.T) {
			_, tt, _ := copyShell(t, app.State{
				HasTrack: true, Title: "一首歌", CoverURL: "https://example.invalid/cover.png",
				BiliURL: "https://www.bilibili.com/video/BV1xx",
			})
			tt.SetDark(dark)
			if err := tt.RightClick("当前封面"); err != nil {
				t.Fatalf("右键不了封面：%v", err)
			}
			homeFrame(tt)
			save(t, dir, name+"-pop-cover.png", tt)
		})
	}
}
