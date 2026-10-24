package lyrics

import (
	"strconv"
	"testing"
	"time"
)

// 这一组测的是「B 站给的那份 JSON 字幕」这一段：形状、解析的判据、编回 LRC。
//
// # 素材是照着 B 站真的给的样子造的
//
// B 站那份字幕的形状是 `{"body":[{"from":…,"to":…,"location":…,"content":"…"}]}`
// （`from` / `to` 是**秒**，带小数）。上游 Biu 的 `get-lyrics.ts` 从那里头取三样：
// `from`（时间）、`content`（字），以及「去掉首尾的 ♪♫」那一步。
//
// ⚠️ **这里没有任何一条断言是在证明「B 站真的这么返回」**——那要靠真机回归。
// 这里证明的是：形状对不对、判据抄得对不对。

// biliFixture 是那份字幕的素材。
//
// ⚠️ **几行是故意摆成乱序的**：B 站给的时间本来就是乱序的（那是「这一句从第几秒
// 开始」，不是一份排好序的表），而解析要把它排好。
const biliFixture = `{
  "body": [
    {"from": 12.345, "to": 15.0, "location": 2, "content": "♪ 这一句带着音符 ♫"},
    {"from": 0.5,   "to": 2.0,  "location": 2, "content": "   "},
    {"from": 3.0,   "to": 5.0,  "location": 2, "content": "普通的一句"},
    {"from": 1.25,  "to": 2.0,  "location": 2, "content": "♪"},
    {"from": 20.0,  "to": 22.0, "location": 2, "content": "  两头有空格  "}
  ]
}`

// TestTheBiliSubtitleIsParsedTheWayUpstreamDoes 是这一票点名的那一条：
// B 站那份 JSON 放进去，断言解析出来的行对。
//
// 判据逐条照上游 `src/components/lyrics/get-lyrics.ts` 的 `getLyricsByBili`：
//
//	时间   `Math.round(from * 1000)` 毫秒
//	文字   去掉首尾的 `♪♫` 再 trim
//	空的   丢掉
//	次序   按时间排序
func TestTheBiliSubtitleIsParsedTheWayUpstreamDoes(t *testing.T) {
	got, err := ParseBiliSubtitle([]byte(biliFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{
		{At: 3 * time.Second, Text: "普通的一句"},
		{At: 12345 * time.Millisecond, Text: "这一句带着音符"},
		{At: 20 * time.Second, Text: "两头有空格"},
	}
	if len(got) != len(want) {
		t.Fatalf("解析出 %d 行，want %d 行：%+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("第 %d 行是 %+v，want %+v", i, got[i], w)
		}
	}
}

// TestTheBiliSubtitleTimeIsRoundedToMilliseconds 盯住 `Math.round(from * 1000)`。
//
// ⚠️ **抄错这一项的症状是「歌词整体早/晚几秒」**，而歌词本身看着完全正常——
// 用户只会以为「这首歌的词对不上」，不会想到是解析错了。
//
// 而**截断**（`int(from*1000)`）与**取整**的差别在 0.5 毫秒那一档上最难看：
// 0.0015 秒 → 取整是 2 毫秒、截断是 1 毫秒。
func TestTheBiliSubtitleTimeIsRoundedToMilliseconds(t *testing.T) {
	for _, c := range []struct {
		from float64
		want time.Duration
	}{
		{0, 0},
		{0.5, 500 * time.Millisecond},
		{0.0015, 2 * time.Millisecond},
		{0.0025, 3 * time.Millisecond},
		{12.345, 12345 * time.Millisecond},
		{61.999, 61999 * time.Millisecond},
	} {
		got, err := ParseBiliSubtitle([]byte(`{"body":[{"from":` +
			formatFloat(c.from) + `,"content":"词"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("from=%v 解析出 %d 行，want 1", c.from, len(got))
		}
		if got[0].At != c.want {
			t.Errorf("from=%v 是 %v，want %v", c.from, got[0].At, c.want)
		}
	}
}

// TestTheBiliSubtitleKeepsTheOrderOfOneMoment 同一时刻的两句保留先后。
//
// 上游那边是 `toSorted`（现代 JS 里是稳定的），而**翻过来会让高潮那一句掉到后面
// 去**——那是原作者排的顺序。
func TestTheBiliSubtitleKeepsTheOrderOfOneMoment(t *testing.T) {
	got, err := ParseBiliSubtitle([]byte(
		`{"body":[{"from":1,"content":"先"},{"from":1,"content":"后"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "先" || got[1].Text != "后" {
		t.Fatalf("同一时刻的两句顺序变了：%+v", got)
	}
}

// TestAnUnreadableSubtitleIsAnError 读不出来要报错。
//
// ⚠️ **「读不出来」和「这一份没有词」是两件事**：后者是**正常**的一种（很多歌
// 压根没有字幕），前者是 B 站改了格式。混成一件的话，界面上会把一次格式变化
// 说成「这首歌没有歌词」。
func TestAnUnreadableSubtitleIsAnError(t *testing.T) {
	if _, err := ParseBiliSubtitle([]byte("这不是 JSON")); err == nil {
		t.Fatal("读不懂的字幕没有报错")
	}
	// 空的那一份**不是**错误。
	lines, err := ParseBiliSubtitle([]byte(`{"body":[]}`))
	if err != nil {
		t.Fatalf("空的那一份不该报错：%v", err)
	}
	if len(lines) != 0 {
		t.Errorf("空的那一份解析出 %d 行", len(lines))
	}
}

// TestTheBiliSubtitleSurvivesTheRoundTripThroughLRC 是「编回 LRC 再读回来」那一步。
//
// ⚠️ **缓存里存的是 LRC 文本**（`Entry.Lyrics`），而 B 站给的是 JSON——中间隔着
// 这一次转换。转坏了的症状是**整首歌的词差一个数量级**（毫秒那三位），而歌词
// 本身看着完全正常。
func TestTheBiliSubtitleSurvivesTheRoundTripThroughLRC(t *testing.T) {
	lines, err := ParseBiliSubtitle([]byte(biliFixture))
	if err != nil {
		t.Fatal(err)
	}
	back := Parse(ToLRC(lines))
	if len(back) != len(lines) {
		t.Fatalf("转回来 %d 行，原来的 %d 行", len(back), len(lines))
	}
	for i := range lines {
		if back[i] != lines[i] {
			t.Errorf("第 %d 行转回来是 %+v，原来的 %+v", i, back[i], lines[i])
		}
	}
}

// TestANewlineInTheContentIsFlattened 盯「内容里带着换行」那一句。
//
// ⚠️ **缓存里存的是 LRC 文本，而 LRC 是一行一句的**：内容里带着换行的话，那一句
// 会被劈成两半，后半截成了没有时间标签的一行（读的时候整个丢掉）——用户看到的是
// 「这一句说到一半没了」。
func TestANewlineInTheContentIsFlattened(t *testing.T) {
	got, err := ParseBiliSubtitle([]byte(
		`{"body":[{"from":1,"content":"前半句\n后半句"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("解析出 %d 行，want 1：%+v", len(got), got)
	}
	if got[0].Text != "前半句 后半句" {
		t.Errorf("那一句是 %q，want 「前半句 后半句」", got[0].Text)
	}
}

// formatFloat 把一个 float64 写成 JSON 里认得的字面量。
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
