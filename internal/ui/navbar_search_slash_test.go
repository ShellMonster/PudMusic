package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestBareSlashFocusesNavbarSearch 锁定焦点不在搜索框时按裸斜杠会聚焦搜索框，后续文字落入搜索词。
func TestBareSlashFocusesNavbarSearch(t *testing.T) {
	b := newTestShell(t)
	b.tt.SetFocused(true)
	b.tt.Frame()
	if b.tt.Focused(searchLabel) {
		t.Fatal("搜索框初始就有焦点，无法验证裸斜杠聚焦行为")
	}

	b.tt.Key(0, ui.KeySlash)
	b.tt.Frame()
	if !b.tt.Focused(searchLabel) {
		t.Fatal("按裸斜杠后搜索框没有获得焦点")
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
