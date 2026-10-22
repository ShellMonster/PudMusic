package ui

import (
	"os"
	"testing"
)

// 这一族把顶栏搜索框存成 PNG，留给人眼和 Biu 上游的截图并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSearchShots
//
//	01 静止：280×40、无边框、浅灰底
//	02 悬停：底色再深一档（不是描边变色）
//	03 焦点 + 搜索历史：灰底可关闭的 Chip，标题 14/400
//
// ⚠️ **深浅两套都要出**：搜索框的灰在浅色下是 default-400、深色下是
// default-500，两套不是同一个数；Chip 的底也是两头各一个。只看浅色那一套，
// 「深色下是不是一块浮起来的板」就验不到。
//
// ⚠️ 一个字都不许打真 B 站接口：历史是内存里的，联想词这一组不打开。
func TestRenderSearchShots(t *testing.T) {
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
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			s.hist.add("周杰伦")
			s.hist.add("林俊杰")
			s.hist.add("五月天 温柔")
			tt.Frame()

			// 01 静止：指针放到内容区，别停在框上。
			tt.Move(900, 500)
			homeFrame(tt)
			save(t, dir, "01-搜索框-"+name+".png", tt)

			// 02 悬停：指针压在框里。
			r, ok := tt.Find(searchLabel)
			if !ok {
				t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			save(t, dir, "02-搜索框-悬停-"+name+".png", tt)

			// 03 焦点 + 历史浮层。点完把指针挪开，画面里看的是「有焦点」那一档。
			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Move(900, 500)
			tt.Frame()
			save(t, dir, "03-搜索历史-"+name+".png", tt)
		})
	}
}
