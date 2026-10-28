package lyrics

import (
	"testing"
	"time"
)

// TestOneLineSeveralTimeTagsBecomesSeveralLines 是这一票点名的那一条：
// `[00:12.00][01:30.00]词` 说的是「这句在 12 秒和 90 秒各唱一次」。
//
// 不拆的话副歌那一段整个没有高亮，而**症状看不出来是解析错了**：歌词一个字不少地摆
// 在那儿，只是从头到尾没有任何一行放大过。
func TestOneLineSeveralTimeTagsBecomesSeveralLines(t *testing.T) {
	got := Parse("[00:12.00][01:30.00][02:00.500]同一句词")

	want := []Line{
		{At: 12 * time.Second, Text: "同一句词"},
		{At: 90 * time.Second, Text: "同一句词"},
		{At: 120*time.Second + 500*time.Millisecond, Text: "同一句词"},
	}
	if len(got) != len(want) {
		t.Fatalf("一行三个时间标签应该出三行，出了 %d 行：%+v", len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("第 %d 行是 %+v，want %+v", i, got[i], w)
		}
	}
}

// TestMillisecondsArePaddedToThreeDigits 盯住上游 `padEnd(3, "0")`（index.tsx:69）。
//
// `.5` 是 500 毫秒不是 5 毫秒。补错的话每一句都差 100 倍，歌词整体往后挪半首歌——
// 而歌词本身看着完全正常，用户只会以为「这首歌的词对不上」。
func TestMillisecondsArePaddedToThreeDigits(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want time.Duration
	}{
		{"[00:12]", 12 * time.Second},
		{"[00:12.5]", 12*time.Second + 500*time.Millisecond},
		{"[00:12.05]", 12*time.Second + 50*time.Millisecond},
		{"[00:12.005]", 12*time.Second + 5*time.Millisecond},
		{"[00:12.500]", 12*time.Second + 500*time.Millisecond},
		{"[1:02.250]", 62*time.Second + 250*time.Millisecond},
	} {
		got := Parse(c.tag + "词")
		if len(got) != 1 {
			t.Fatalf("%s 解析出 %d 行，want 1", c.tag, len(got))
		}
		if got[0].At != c.want {
			t.Errorf("%s 是 %v，want %v", c.tag, got[0].At, c.want)
		}
	}
}

// TestLinesWithoutTimeOrWordsProduceNothing 是元数据标签那一支。
//
// `[ti:…]` / `[ar:…]` / `[offset:…]` 行内没有时间戳，一行至少要有一个时间戳才
// 产得出东西——所以它们自动被丢掉，和上游那个空 while 的效果一样（index.tsx:72）。
//
// 不这么做的话界面上会摆着一行 `[ti:某某某 - 某某某]`，而用户看不出那是标签。
func TestLinesWithoutTimeOrWordsProduceNothing(t *testing.T) {
	got := Parse("[ti:歌名]\n[ar:歌手]\n[offset:+500]\n[al:专辑]\n\n[00:01.00]真的词")

	if len(got) != 1 {
		t.Fatalf("四条元数据 + 一个空行应该只剩一句，剩了 %d 行：%+v", len(got), got)
	}
	if got[0].Text != "真的词" || got[0].At != time.Second {
		t.Errorf("剩下的是 %+v，want {1s 真的词}", got[0])
	}
}

// TestATaggedLineKeepsItsWholeText 盯住「元数据标签不剥」这个**故意的选择**。
//
// 独立成行的 `[ti:歌名]` 产不出任何东西（见上面那条）。但和一句词写在一行上的
// ——`[ti:歌名][00:05.00]真正的词`——标签会留在文字里，和上游一模一样
// （上游只 `replace(timeTagPattern, "")`，index.tsx:62，剥的是时间标签）。
//
// 故意不加一道「剥掉 `[xx:…]`」：有些 LRC 把段落标记写在词里（`[00:12.00][Chorus] 嘿`），
// 那不是标签是词。剥掉就会把 `嘿` 前面那半句吃掉，而漏出来一个 `[Chorus]` 至多
// 让用户看一眼就知道这份歌词脏。**错的方向不一样：一个是看不出来，一个是少字。**
func TestATaggedLineKeepsItsWholeText(t *testing.T) {
	got := Parse("[ti:歌名][00:05.00]真正的词")

	if len(got) != 1 {
		t.Fatalf("want 1 行，出了 %d 行：%+v", len(got), got)
	}
	if got[0].At != 5*time.Second {
		t.Errorf("时间是 %v，want 5s", got[0].At)
	}
	if got[0].Text != "[ti:歌名]真正的词" {
		t.Errorf("文字是 %q，want %q（和上游一样，标签留着）", got[0].Text, "[ti:歌名]真正的词")
	}
}

// TestSortedByTimeAndStableWithinATime：乱序的 LRC（用户从歌词站粘来的常常是乱的）
// 要排好；**同一时刻的两句保留文件里的先后**——那是谁在上谁在下是作者排的，
// 翻过来会让高潮那句掉到后面去。
func TestSortedByTimeAndStableWithinATime(t *testing.T) {
	got := Parse("[00:30.00]第三句\n[00:10.00]第一句\n[00:10.00]插在第一句和第二句之间\n[00:20.00]第二句")

	want := []string{"第一句", "插在第一句和第二句之间", "第二句", "第三句"}
	if len(got) != len(want) {
		t.Fatalf("want %d 行，出了 %d 行：%+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("第 %d 行是 %q，want %q", i, got[i].Text, w)
		}
	}
}

// TestWindowsLineEndings：Windows 上编辑过的 LRC 是 \r\n 的。不认它的话每一行
// 的末尾都挂一个 \r，界面上是「第一句␍」——歌名里那个回车用户看得见摸不着。
func TestWindowsLineEndings(t *testing.T) {
	got := Parse("[00:01.00]第一句\r\n[00:02.00]第二句")

	if len(got) != 2 {
		t.Fatalf("want 2 行，出了 %d 行：%+v", len(got), got)
	}
	for _, l := range got {
		if l.Text != "第一句" && l.Text != "第二句" {
			t.Errorf("文字是 %q，带着看不见的 \\r", l.Text)
		}
	}
}

func TestEmptyRawGivesNothing(t *testing.T) {
	if got := Parse(""); got != nil {
		t.Errorf("空串应该一条都没有，出了 %+v", got)
	}
	if got := Rows("", "[00:01.00]译文"); got != nil {
		t.Errorf("没有原文就只有译文，界面上是一行孤零零的译文：%+v", got)
	}
}

// TestRowsPutsTheTranslationUnderItsLine：译文按**时间戳**配对（上游 index.tsx:178）。
//
// 时间戳对不上的那些译文就丢掉：硬按行号配对的话，两份文件差了一句就会**从那
// 一句起整体错位**，界面上每一句下面都挂着另一句的翻译——那比没有翻译更难懂。
func TestRowsPutsTheTranslationUnderItsLine(t *testing.T) {
	rows := Rows(
		"[00:01.00]Hello\n[00:02.00]World",
		"[00:01.00]你好\n[00:03.00]这句对不上时间戳",
	)

	if len(rows) != 2 {
		t.Fatalf("want 2 行，出了 %d 行：%+v", len(rows), rows)
	}
	if rows[0].Text != "Hello" || rows[0].Translation != "你好" {
		t.Errorf("第一行是 %+v，want {Hello 你好}", rows[0])
	}
	if rows[1].Text != "World" || rows[1].Translation != "" {
		t.Errorf("第二行是 %+v，译文对不上时间戳就该是空的", rows[1])
	}
}

// TestActiveAt 照上游 index.tsx:181-187。
func TestActiveAt(t *testing.T) {
	rows := Rows("[00:10.00]一\n[00:20.00]二\n[00:30.00]三", "")

	for _, c := range []struct {
		at   time.Duration
		want int
		why  string
	}{
		{0, 0, "歌还没唱到第一句时高亮第一句（那是「准备唱」的那一句）"},
		{9999 * time.Millisecond, 0, "还没到第一句"},
		{10 * time.Second, 0, "刚好到第一句"},
		{15 * time.Second, 0, "两句之间停第一句"},
		{20 * time.Second, 1, "到第二句"},
		{999 * time.Second, 2, "过了最后一句就停在最后一句"},
	} {
		if got := ActiveAt(rows, c.at); got != c.want {
			t.Errorf("唱到 %v 时高亮第 %d 行，want %d（%s）", c.at, got, c.want, c.why)
		}
	}

	if got := ActiveAt(nil, time.Second); got != -1 {
		t.Errorf("一句歌词都没有时是 -1，出了 %d", got)
	}
}

func TestActiveAtScalesWithBinarySearch(t *testing.T) {
	rows := make([]Row, 100000)
	for i := range rows {
		rows[i].At = time.Duration(i) * time.Second
	}
	for _, tc := range []struct {
		at   time.Duration
		want int
	}{
		{at: -time.Second, want: 0},
		{at: 0, want: 0},
		{at: 999*time.Second + 500*time.Millisecond, want: 999},
		{at: 99999 * time.Second, want: 99999},
	} {
		if got := ActiveAt(rows, tc.at); got != tc.want {
			t.Fatalf("ActiveAt(%v) = %d, want %d", tc.at, got, tc.want)
		}
	}
}
