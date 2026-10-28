// 票 BMG-253 的画面验收：能点的看起来能点、不可逆动作看得出来。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket253Shots
//
// # 深浅两套 × 两套主题方案
//
// 这一票改的每一处都是**颜色**上的事（主色那一档的反馈、下划线、垃圾桶图标），
// 而主色在四套方案里是四个数——所以「浅色下看得见」不代表「经典那一套也看得见」。
// 这里出**暖纸**（默认）和**经典**两套，各深浅各一张。
//
// # 存的图
//
//	12  顶栏右半：搜索框（放大镜静止 / 悬停）+ 头像（悬停）
//	13  搜索建议浮层：历史 chip（连着那个「×」）+「清除全部」+ 它那句提示
//	14  收藏夹头图区：创建者那一行（悬停在下划线上）
package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// TestRenderTicket253Shots 把这一票改过的那几处各存一张 PNG。
func TestRenderTicket253Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, c := range []struct {
		label  string
		preset app.ThemePreset
		dark   bool
	}{
		{"暖纸", app.ThemePresetWarmPaper, false},
		{"暖纸", app.ThemePresetWarmPaper, true},
		{"经典", app.ThemePresetClassic, false},
		{"经典", app.ThemePresetClassic, true},
	} {
		name := "浅色"
		if c.dark {
			name = "深色"
		}
		t.Run(c.label+"-"+name, func(t *testing.T) {
			st := app.DefaultSettings("")
			st.ThemePreset = c.preset
			if c.dark {
				st.ThemeMode = app.ThemeModeDark
			}
			saveTicket253TopBar(t, dir, st, c.dark, fmt.Sprintf("12-顶栏-%s-%s.png", c.label, name))
			saveTicket253Suggest(t, dir, st, c.dark, fmt.Sprintf("13-搜索浮层-%s-%s.png", c.label, name))
			saveTicket253Creator(t, dir, st, c.dark, fmt.Sprintf("14-收藏夹创建者-%s-%s.png", c.label, name))
			saveTicket253RowOp(t, dir, st, c.dark, fmt.Sprintf("15-行尾操作键-%s-%s.png", c.label, name))
		})
	}
}

// saveTicket253TopBar 存顶栏右半：搜索框（放大镜静止 + 悬停）和头像（悬停）。
func saveTicket253TopBar(t *testing.T, dir string, st app.Settings, dark bool, name string) {
	t.Helper()
	b := newTestShell(t, at(1200, 800), systemIsDark(dark), withSettings(st))
	tt := b.tt
	homeFrame(tt)

	mag, ok := tt.Find(searchActionLabel)
	if !ok {
		t.Fatalf("顶栏上没有那枚放大镜，文字有 %q", tt.Texts())
	}
	// 静止那一档。
	saveRect(t, dir, name, tt, int(mag.X)-40, int(mag.Y)-10, 150, 48)
	// 悬停那一档：图形染主色。
	tt.Move(mag.X+mag.W/2, mag.Y+mag.H/2)
	tt.Frame()
	saveRect(t, dir, "12b-顶栏放大镜悬停-"+name, tt, int(mag.X)-40, int(mag.Y)-10, 150, 48)

	// 头像：悬停时外面多一圈主色的环。
	av, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("顶栏上没有头像，文字有 %q", tt.Texts())
	}
	tt.Move(av.X+av.W/2, av.Y+av.H/2)
	tt.Frame()
	saveRect(t, dir, "12c-顶栏头像悬停-"+name, tt, int(av.X)-10, int(av.Y)-10, 52, 52)
	b.Close()
}

// saveTicket253Suggest 存搜索建议浮层：历史 chip（连着那个「×」）+「清除全部」。
func saveTicket253Suggest(t *testing.T, dir string, st app.Settings, dark bool, name string) {
	t.Helper()
	b := newTestShell(t, at(1200, 800), systemIsDark(dark), withSettings(st))
	s, tt := b.s, b.tt
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	homeFrame(tt)

	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	tt.Frame()

	head, ok := tt.Find("搜索历史")
	if !ok {
		t.Fatalf("浮层没开，文字有 %q", tt.Texts())
	}
	// 指针停在「清除全部」上：那一句提示（删的是什么、能不能撤）就该出来。
	clear, ok := tt.Find("清除全部")
	if !ok {
		t.Fatalf("浮层上没有「清除全部」，文字有 %q", tt.Texts())
	}
	tt.Move(clear.X+clear.W/2, clear.Y+clear.H/2)
	tt.Frame()
	saveRect(t, dir, name, tt, int(head.X)-12, int(head.Y)-46, 400, 190)
	b.Close()
}

// saveTicket253Creator 存收藏夹头图区那个「创建者」那一行（指针停在名字上）。
func saveTicket253Creator(t *testing.T, dir string, st app.Settings, dark bool, name string) {
	t.Helper()
	bili := newCollectionBili(t)
	bili.setMedias(mediaOf(1, "歌 一"))
	bili.infos["upper"] = map[string]any{"mid": 7, "name": "UP 小明"}
	_, s, tt := collectionShellFrom(t, bili, []shellOpt{withSettings(st)}, "", "12345")
	waitSongs(t, tt, s, 1)

	label, ok := tt.Find("UP 小明")
	if !ok {
		t.Fatalf("头图区里没有创建者的名字，文字有 %q", tt.Texts())
	}
	tt.Move(label.X+label.W/2, label.Y+label.H/2)
	tt.Frame()
	saveRect(t, dir, name, tt, int(label.X)-44, int(label.Y)-14, 150, 40)
}

// saveTicket253RowOp 存列表行尾那枚「⋯」（悬停着，提示也出来）。
//
// 它是那一行里**唯一**的操作入口，而它原先是三个 2 像素的淡灰点——和旁边那个
// 12 像素的时长数字一样重，用户读不出「这是一枚菜单键」。
func saveTicket253RowOp(t *testing.T, dir string, st app.Settings, dark bool, name string) {
	t.Helper()
	bili := newCollectionBili(t)
	bili.setMedias(mediaOf(1, "歌 一"))
	bili.infos["upper"] = map[string]any{"mid": 7, "name": "UP 小明"}
	_, s, tt := collectionShellFrom(t, bili, []shellOpt{withSettings(st)}, "", "12345")
	waitSongs(t, tt, s, 1)

	op, ok := tt.Find(list.OperationLabel("歌 一"))
	if !ok {
		t.Fatalf("画面上找不到行尾那枚「⋯」，文字有 %q", tt.Texts())
	}
	tt.Move(op.X+op.W/2, op.Y+op.H/2)
	tt.Frame()
	// 提示是框架那一份（600ms 出），推一下钟。
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	saveRect(t, dir, name, tt, int(op.X)-150, int(op.Y)-40, 300, 80)
}
