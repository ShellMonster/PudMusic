package ui

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestBareSlashFocusesNavbarSearch 防止裸斜杠只聚焦搜索框却不显示搜索历史。
func TestBareSlashFocusesNavbarSearch(t *testing.T) {
	b := newTestShell(t)
	b.tt.SetFocused(true)
	b.s.hist.add("周杰伦")
	b.tt.Frame()
	if b.tt.Focused(searchLabel) {
		t.Fatal("搜索框初始就有焦点，无法验证裸斜杠聚焦行为")
	}

	b.tt.Key(0, ui.KeySlash)
	b.tt.Frame()
	if !b.tt.Focused(searchLabel) {
		t.Fatal("按裸斜杠后搜索框没有获得焦点")
	}

	if !b.tt.HasText("周杰伦") {
		t.Fatal("按裸斜杠后没有显示搜索历史")
	}

	b.tt.Type("hello")
	b.tt.Frame()
	if got := b.s.searchKeyword; got != "hello" {
		t.Fatalf("裸斜杠聚焦后输入的文字是 %q，want %q", got, "hello")
	}
}

// TestSlashTypedInNavbarSearchRemainsText 锁定搜索框已聚焦时输入斜杠会保留为文本且不改变焦点。
func TestSlashTypedInNavbarSearchRemainsText(t *testing.T) {
	b := newTestShell(t)
	b.tt.SetFocused(true)
	b.tt.Frame()
	if err := b.tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	b.tt.Frame()
	if !b.tt.Focused(searchLabel) {
		t.Fatal("点击搜索框后没有获得焦点")
	}

	// 真实键盘发的是「按下 /、文本 /、松开 /」三件事：焦点在框里时，输入框要
	// 先吃下这个键，窗口那一层的聚焦逻辑不能把后面的文字挡掉。
	b.tt.TypeKey(0, ui.KeySlash, "/")
	b.tt.Frame()
	if got := b.s.searchKeyword; got != "/" {
		t.Fatalf("搜索框输入斜杠后内容是 %q，want %q", got, "/")
	}
	if !b.tt.Focused(searchLabel) {
		t.Fatal("搜索框输入斜杠后失去焦点")
	}
}

// TestModifiedSlashDoesNotFocusNavbarSearch 锁定带 Cmd 修饰键的斜杠不会触发搜索框聚焦快捷键。
func TestModifiedSlashDoesNotFocusNavbarSearch(t *testing.T) {
	b := newTestShell(t)
	b.tt.SetFocused(true)
	b.tt.Frame()
	if b.tt.Focused(searchLabel) {
		t.Fatal("搜索框初始就有焦点，无法验证带修饰键的斜杠")
	}

	b.tt.Key(ui.Cmd, ui.KeySlash)
	b.tt.Frame()
	if b.tt.Focused(searchLabel) {
		t.Fatal("按 Cmd+/ 后搜索框获得了焦点")
	}
}

// TestSlashSearchPanelCloses 验证快捷键打开的面板可用 Esc 和点击其他位置关闭。
func TestSlashSearchPanelCloses(t *testing.T) {
	for _, closePanel := range []string{"escape", "outside"} {
		t.Run(closePanel, func(t *testing.T) {
			s, tt := newShell(t)
			tt.SetFocused(true)
			s.hist.add("周杰伦")
			tt.Frame()
			tt.Key(0, ui.KeySlash)
			tt.Frame()
			if !tt.HasText("周杰伦") {
				t.Fatal("按斜杠后搜索历史未显示")
			}
			if closePanel == "escape" {
				tt.Key(0, ui.KeyEscape)
			} else if err := tt.Click("本地音乐"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if tt.HasText("周杰伦") {
				t.Fatal("关闭后搜索历史仍然显示")
			}
			tt.Frame()
			if tt.HasText("周杰伦") {
				t.Fatal("搜索历史关闭后重新出现")
			}
		})
	}
}

// TestRenderSlashSearchShots 用搜索历史呈现斜杠打开的面板，供深浅主题视觉检查。
func TestRenderSlashSearchShots(t *testing.T) {
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
			tt.Key(0, ui.KeySlash)
			tt.Frame()
			if !tt.HasText("周杰伦") {
				t.Fatal("按斜杠后搜索历史未显示")
			}
			settleSuggestEnter(t, tt)
			save(t, dir, "斜杠搜索历史-"+name+".png", tt)
		})
	}
}
