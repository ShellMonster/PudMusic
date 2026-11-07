package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestTheLyricKnobPopoverIsOnlyAsWideAsItsSlider 锁住：歌词字号 / 偏移那两个浮层按内容收窄
// （一条竖滑块 + 一行读数），不吃菜单那一档的最小宽 200。
//
// 曾经出错的行为：`popupPanel` 对所有浮层都加了 `MinWidth(200)`（原型 `.pop` 是给菜单 / 下拉的），
// 旋钮浮层被撑成 200 宽，一条 24 宽的滑块孤零零贴在一大块面板里。
//
// 判据：浮层贴在旋钮左边（`lyricPanelStyle`），滑块在面板里面，所以滑块左缘到旋钮左缘的距离
// 小于「面板宽 + 缝」；撑到 200 时它 ≥ 200（实测 201）。
func TestTheLyricKnobPopoverIsOnlyAsWideAsItsSlider(t *testing.T) {
	for _, k := range []struct{ knob, slider string }{
		{lyricFontKnobName, lyricFontSliderKey},
		{lyricOffsetKnobName, lyricOffsetSliderKey},
	} {
		t.Run(k.knob, func(t *testing.T) {
			s, tt := lyricShell(t)
			putLyrics(s, lyricsOfN(20), "")
			settleLyrics(t, s, tt)
			knob, ok := tt.Find(k.knob)
			if !ok {
				t.Fatalf("没有 %q 旋钮，文字有 %q", k.knob, tt.Texts())
			}
			openLyricPopover(t, s, tt, k.knob)
			sl, ok := tt.Find(k.slider)
			if !ok {
				t.Fatalf("%q 浮层里没有滑块，文字有 %q", k.knob, tt.Texts())
			}
			if span := knob.X - sl.X; span >= tok.PopMinW {
				t.Errorf("%q 浮层里的滑块离旋钮 %.1f（≥ %d）：被撑到了菜单的最小宽", k.knob, span, int(tok.PopMinW))
			}
		})
	}
}
