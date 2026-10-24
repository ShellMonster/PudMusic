package imgcache

import "testing"

// TestGlyphOf：占位封面的首字是**清洗后**歌名的第一个字；英文转大写；清洗空了用音符。
func TestGlyphOf(t *testing.T) {
	for in, want := range map[string]string{
		"晴天":          "晴",
		"【4K修复】晴天":    "晴",
		"hello world": "H",
		"《千里之外》":      "《",
		"":            "♪",
		"【官方MV】":      "【",
	} {
		if got := glyphOf(in); got != want {
			t.Errorf("glyphOf(%q) = %q，want %q", in, got, want)
		}
	}
}
