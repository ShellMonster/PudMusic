package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/egoist/mygo/ui"
)

// karaokeRow 是一句带逐字时间的歌词：路 0~500ms、灯 500~1000ms。
func karaokeRow() lyrics.Row {
	return lyrics.Row{
		At:   0,
		End:  time.Second,
		Text: "路灯",
		Words: []lyrics.Word{
			{At: 0, End: 500 * time.Millisecond, Text: "路"},
			{At: 500 * time.Millisecond, End: time.Second, Text: "灯"},
		},
	}
}

// TestKaraokeLightsOnlyTheWordsAlreadySung 钉住逐字上色的切分：
// 唱到一半时前一个字亮、后一个字暗，而不是整句一起变。
func TestKaraokeLightsOnlyTheWordsAlreadySung(t *testing.T) {
	row := karaokeRow()
	lit, unlit := ui.RGB(255, 255, 255), ui.RGB(80, 80, 80)

	// 一个字都还没唱完：整句是「未唱」那一档。
	spans, ok := karaokeSpans(row, 100*time.Millisecond, 20, 700, "", lit, unlit)
	if !ok || len(spans) != 1 || spans[0].Text != "路灯" || spans[0].Color != unlit {
		t.Fatalf("一个字都没唱完时 = %+v（ok=%v）", spans, ok)
	}

	// 第一个字唱完、第二个字还没：切成两段。
	spans, ok = karaokeSpans(row, 600*time.Millisecond, 20, 700, "", lit, unlit)
	if !ok || len(spans) != 2 {
		t.Fatalf("唱到一半时 = %+v（ok=%v）", spans, ok)
	}
	if spans[0].Text != "路" || spans[0].Color != lit {
		t.Errorf("已唱的前缀 = %+v，want「路」亮色", spans[0])
	}
	if spans[1].Text != "灯" || spans[1].Color != unlit {
		t.Errorf("未唱的后缀 = %+v，want「灯」暗色", spans[1])
	}

	// 整句唱完：一段亮色。
	spans, ok = karaokeSpans(row, 2*time.Second, 20, 700, "", lit, unlit)
	if !ok || len(spans) != 1 || spans[0].Text != "路灯" || spans[0].Color != lit {
		t.Fatalf("整句唱完时 = %+v（ok=%v）", spans, ok)
	}
}

// TestKaraokeFallsBackWhenWordsDoNotMatchTheLine 钉住「词拼不回整句就不逐字」。
//
// 源那边把空格或标点放在了别处时，硬按字节切会把字切坏；回退整句高亮只是
// 少了逐字效果，不会画出半截乱码。
func TestKaraokeFallsBackWhenWordsDoNotMatchTheLine(t *testing.T) {
	row := karaokeRow()
	row.Words[0].Text = "别的"
	if _, ok := karaokeSpans(row, 600*time.Millisecond, 20, 700, "", ui.RGB(255, 255, 255), ui.RGB(80, 80, 80)); ok {
		t.Fatal("词和整句对不上时仍然逐字切了")
	}
	// 没有词级时间的那一档也回退。
	row.Words = nil
	if _, ok := karaokeSpans(row, 600*time.Millisecond, 20, 700, "", ui.RGB(255, 255, 255), ui.RGB(80, 80, 80)); ok {
		t.Fatal("没有词级时间时仍然逐字切了")
	}
}
