package lyrics

import (
	"testing"
	"time"
)

const yrcFixture = `{"t":0,"c":[{"tx":"作词: "},{"tx":"某人"}]}
[0,2000](0,500,0)路(500,500,0)灯
[2000,3000](2000,1000,0)hello (3000,1000,0)world
[5000,1000](5000,500,0)尾(5500,500,0)句
`

// TestParseYRCReadsWordsAndTimes 钉住逐字歌词的解析：行、词、开始与结束。
//
// 这一条要是错了，界面上是「整句一起亮」或者「亮早/亮晚一个字」，而歌词
// 本身看着完全正常，用户不会想到是解析错了。
func TestParseYRCReadsWordsAndTimes(t *testing.T) {
	lines := ParseYRC(yrcFixture)
	if len(lines) != 3 {
		t.Fatalf("解析出 %d 行，want 3：%+v", len(lines), lines)
	}
	first := lines[0]
	if first.At != 0 || first.End != time.Second || first.Text != "路灯" {
		t.Errorf("第一行 = %+v，want 0~1s「路灯」", first)
	}
	if len(first.Words) != 2 || first.Words[1].At != 500*time.Millisecond ||
		first.Words[1].End != time.Second || first.Words[1].Text != "灯" {
		t.Errorf("第一行的词 = %+v", first.Words)
	}
	second := lines[1]
	if second.Text != "hello world" {
		t.Errorf("英文行拼出来是 %q，want %q", second.Text, "hello world")
	}
	if second.Words[0].Text != "hello " || second.Words[1].Text != "world" {
		t.Errorf("英文词 = %q / %q", second.Words[0].Text, second.Words[1].Text)
	}
	if second.End != 4*time.Second {
		t.Errorf("英文行结束在 %v，want 4s", second.End)
	}
	// 署名行是 JSON，不是唱出来的词。
	for _, l := range lines {
		if l.Text == "作词: 某人" {
			t.Fatalf("署名行被当成歌词了：%+v", l)
		}
	}
}

// TestParseYRCDropsTimedCreditLines 钉住「带时间戳的署名行也不是歌词」。
func TestParseYRCDropsTimedCreditLines(t *testing.T) {
	raw := "[0,1000](0,1000,0)作词 : 某人\n[1000,1000](1000,1000,0)真的词\n"
	lines := ParseYRC(raw)
	if len(lines) != 1 || lines[0].Text != "真的词" {
		t.Fatalf("署名行没被过滤掉：%+v", lines)
	}
}

// TestParseYRCSortsByStart 钉住「乱序的 YRC 也会按时间排好」。
func TestParseYRCSortsByStart(t *testing.T) {
	raw := "[5000,1000](5000,500,0)后\n[0,1000](0,500,0)前\n"
	lines := ParseYRC(raw)
	if len(lines) != 2 || lines[0].Text != "前" || lines[1].Text != "后" {
		t.Fatalf("排序后 = %+v", lines)
	}
}

// TestParseYRCLineWithoutWordMarks 钉住「只有整行时间、没有词标记」的那一档：
// 整行当成一个词，调用方不用另写一条回退路。
func TestParseYRCLineWithoutWordMarks(t *testing.T) {
	lines := ParseYRC("[1000,2000]整句的词\n")
	if len(lines) != 1 {
		t.Fatalf("解析出 %d 行，want 1", len(lines))
	}
	l := lines[0]
	if l.Text != "整句的词" || l.At != time.Second || l.End != 3*time.Second ||
		len(l.Words) != 1 || l.Words[0].End != 3*time.Second {
		t.Errorf("整行那一档 = %+v", l)
	}
}

// TestParseYRCRejectsPlainLRC 钉住「普通 LRC 喂进来返回空」。
//
// 判错了的话调用方会拿一份零行去画，界面上是「这首歌没歌词」，而 LRC 明明在。
func TestParseYRCRejectsPlainLRC(t *testing.T) {
	if lines := ParseYRC("[00:12.00]普通歌词\n"); len(lines) != 0 {
		t.Fatalf("普通 LRC 被当成 YRC：%+v", lines)
	}
}

// TestSpreadWordsSplitsCJK 钉住逐字拆分的比例：两个汉字各占词时长的 42.5%，
// 最后留 15% 的缝给下一个词。
func TestSpreadWordsSplitsCJK(t *testing.T) {
	got := SpreadWords([]Word{{At: 0, End: time.Second, Text: "路灯"}})
	if len(got) != 2 {
		t.Fatalf("拆出 %d 段，want 2：%+v", len(got), got)
	}
	if got[0].At != 0 || got[0].End != 425*time.Millisecond || got[0].Text != "路" {
		t.Errorf("第一个字 = %+v", got[0])
	}
	if got[1].At != 425*time.Millisecond || got[1].End != 850*time.Millisecond || got[1].Text != "灯" {
		t.Errorf("第二个字 = %+v", got[1])
	}
}

// TestSpreadWordsLeavesLatinWhole 钉住「英文单词不拆成字母」。
func TestSpreadWordsLeavesLatinWhole(t *testing.T) {
	in := []Word{{At: 0, End: time.Second, Text: "hello"}}
	got := SpreadWords(in)
	if len(got) != 1 || got[0] != in[0] {
		t.Fatalf("英文词被拆了：%+v", got)
	}
}

// TestRowsOfPrefersYRC 钉住「有 YRC 就走逐字，没有才回退 LRC」。
func TestRowsOfPrefersYRC(t *testing.T) {
	yrc := "[0,2000](0,1000,0)路灯\n"
	lrc := "[00:00.00]普通的那一份\n"
	rows := RowsOf(lrc, "", yrc)
	if len(rows) != 1 || rows[0].Text != "路灯" || len(rows[0].Words) != 2 {
		t.Fatalf("RowsOf 用了 %+v，want YRC 的「路灯」拆成两段", rows)
	}
	rows = RowsOf(lrc, "", "[00:00.00]不是 YRC\n")
	if len(rows) != 1 || rows[0].Text != "普通的那一份" {
		t.Fatalf("坏 YRC 没有回退 LRC：%+v", rows)
	}
}

// TestRowsOfPairsTranslationNearby 钉住 YRC 行的译文按 ±1.5 秒内最近的那一条
// 配对，而普通 LRC 仍然只认精确时间戳。
func TestRowsOfPairsTranslationNearby(t *testing.T) {
	yrc := "[0,2000](0,1000,0)路(1000,1000,0)灯\n"
	trans := "[00:00.30]lights\n"
	rows := RowsOf("", trans, yrc)
	if len(rows) != 1 || rows[0].Translation != "lights" {
		t.Fatalf("近邻译文没配上：%+v", rows)
	}

	// 远到容差以外的译文不许硬配。
	rows = RowsOf("", "[00:03.00]too late\n", yrc)
	if len(rows) != 1 || rows[0].Translation != "" {
		t.Fatalf("超时译文被配上了：%+v", rows)
	}

	// LRC 那条路保持原样：只认精确时间戳。
	rows = Rows("", "[00:00.30]lights\n")
	if len(rows) != 0 {
		t.Fatalf("空原文应该没有行：%+v", rows)
	}
	rows = Rows("[00:00.00]词\n", "[00:00.30]lights\n")
	if len(rows) != 1 || rows[0].Translation != "" {
		t.Fatalf("普通 LRC 的译文配对应只认精确时间：%+v", rows)
	}
}
