package imgcache

import "testing"

// TestGlyphOf：占位封面的首字是**清洗后**歌名的第一个字；英文转大写；清洗空了用音符。
//
// ⚠️ **「《千里之外》」那一档是票 #706 那个公共层改的**（2026-10-31）：歌名提取抽
// 公共层（`internal/ui/songmeta`）之后，书名号那一档**认得出歌名**了⇒ 清洗结果是
// 「千里之外」⇒ 首字是「千」。
//
// 而**改之前**是「《」——那是一个**标点符号**当封面上的首字（画出来是一个书名号
// 压在渐变上）。⇒ 新行为是对的，**是这一条判据过时了**。
func TestGlyphOf(t *testing.T) {
	for in, want := range map[string]string{
		"晴天":          "晴",
		"【4K修复】晴天":    "晴",
		"hello world": "H",
		"《千里之外》":      "千",
		"":            "♪",
		"【官方MV】":      "【",
	} {
		if got := glyphOf(in); got != want {
			t.Errorf("glyphOf(%q) = %q，want %q", in, got, want)
		}
	}
}
