package ui

import (
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/egoist/mygo/ui"
)

// lyricKaraokeIdleAlpha 是「还没唱到」那一截的透明度。
//
// 当前句原本整句都是亮的；逐字上色之后，没唱到的部分要退下去，已唱的部分
// 才是当前句的颜色。0.5 在深浅两套主题上都还读得清（再低就和非当前句混了）。
const lyricKaraokeIdleAlpha = 0.5

// lyricSungPrefix 报这一句唱到哪个前缀。
//
// 第二个返回值是「这一份歌词能逐字上色吗」：没有词级时间、词和整句对不上
// （源那边把空格/标点放在了别处），都返回 false，调用方回退整句高亮。
func lyricSungPrefix(row lyrics.Row, at time.Duration) (string, bool) {
	if len(row.Words) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, w := range row.Words {
		if at < w.End {
			break
		}
		b.WriteString(w.Text)
	}
	lit := b.String()
	if !strings.HasPrefix(row.Text, lit) {
		return "", false
	}
	return lit, true
}

// karaokeSpans 把当前句拆成「已唱」和「未唱」两段。
//
// ⚠️ **只有当前句走这里**：其它行照旧是整段的 `Text`。每帧给整屏每一句都
// 建 RichText 的话，滚动时每一行的跨度都要重新排版；而除当前句以外，逐字
// 颜色没有信息可加。
func karaokeSpans(row lyrics.Row, at time.Duration, size float32, weight int, family string,
	lit, unlit ui.Color) ([]ui.Span, bool) {
	prefix, ok := lyricSungPrefix(row, at)
	if !ok {
		return nil, false
	}
	style := ui.Span{Font: family, Size: size, Weight: weight}
	if prefix == "" {
		style.Text, style.Color = row.Text, unlit
		return []ui.Span{style}, true
	}
	if prefix == row.Text {
		style.Text, style.Color = row.Text, lit
		return []ui.Span{style}, true
	}
	sung := style
	sung.Text, sung.Color = prefix, lit
	rest := style
	rest.Text, rest.Color = row.Text[len(prefix):], unlit
	return []ui.Span{sung, rest}, true
}
