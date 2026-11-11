// 网易云的逐字歌词（YRC）：把 `[行开始,行时长](词开始,词时长,0)词…` 解析成
// 带词级时间的行，供卡拉 OK 上色用。
//
// # 它和 LRC 是两条路
//
// LRC 只给「这一句从什么时候开始」，一句之内哪个字唱到哪儿是不知道的。YRC 把
// 词（多半是字）各自的开始和时长都给了，所以逐字上色能对得上。
//
// `Parse`（lrc.go）不认 YRC：它只找 `[mm:ss.xx]` 那种标签。把 YRC 喂给它，
// 整份歌词会一行都出不来。所以调用方先看有没有 YRC，有就走这里，没有才回退
// `Parse`（`RowsOf` 已经把这个次序包好了）。
//
// # 署名行是 JSON
//
// 网易云的 YRC 前面常带几行 JSON：`{"t":0,"c":[{"tx":"作词: "},…]}`。它们没有
// 时间轴，不是唱出来的词。以 `{` 开头的行整行跳过。
package lyrics

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Word 是歌词里的一小段：哪个时刻开始、哪个时刻结束、唱的是什么。
//
// Text 里可能带空格（英文词之间的分隔）；一整句的 `Line.Text` 就是这些 Text
// 按顺序拼起来的，所以界面能拿它做前缀切分。
type Word struct {
	At   time.Duration
	End  time.Duration
	Text string
}

// yrcLine 认一行 YRC 的行头：`[开始毫秒,时长毫秒]`。
var yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)

// yrcMark 认一个词标记：`(开始毫秒,时长毫秒,0)`。第三个数字是保留位，
// 现有数据里是 0 或 -1，解析时不用它。
var yrcMark = regexp.MustCompile(`\((\d+),(\d+),(-?\d+)\)`)

// ParseYRC 把一份 YRC 文本解析成若干行，**按时间排好序**。
//
// 不是 YRC（没有任何一行认得出）时返回 nil，调用方据此回退普通 LRC。
func ParseYRC(raw string) []Line {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []Line
	for line := range strings.SplitSeq(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		// 署名行是 JSON（见文件头）；元数据标签和空行也在这里丢掉。
		if line == "" || strings.HasPrefix(line, "{") {
			continue
		}
		m := yrcLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineAt, ok := yrcMillis(m[1])
		if !ok {
			continue
		}
		lineDur, ok := yrcMillis(m[2])
		if !ok {
			continue
		}
		words := parseYRCWords(m[3], lineAt, lineDur)
		if len(words) == 0 {
			continue
		}
		text := wordsText(words)
		if text == "" || isCreditText(text) {
			continue
		}
		end := words[len(words)-1].End
		if end <= words[0].At {
			end = lineAt + lineDur
		}
		out = append(out, Line{At: words[0].At, End: end, Text: text, Words: words})
	}
	slices.SortStableFunc(out, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	return out
}

// parseYRCWords 解析行头之后的那一串词。
//
// 没有词标记的行（有些 YRC 只给整行）当成一个词：整行的开始和时长就是它的
// 开始和时长。这样「有逐字就逐字、没有就整句」在同一个模型里，调用方不用分。
func parseYRCWords(body string, lineAt, lineDur time.Duration) []Word {
	marks := yrcMark.FindAllStringSubmatchIndex(body, -1)
	if len(marks) == 0 {
		text := strings.TrimSpace(body)
		if text == "" {
			return nil
		}
		return []Word{{At: lineAt, End: lineAt + lineDur, Text: text}}
	}
	words := make([]Word, 0, len(marks))
	for i, idx := range marks {
		// 词文本从这一个标记的右括号，到下一个标记的左括号。
		startText := idx[1]
		endText := len(body)
		if i+1 < len(marks) {
			endText = marks[i+1][0]
		}
		text := body[startText:endText]
		at, ok := yrcMillis(body[idx[2]:idx[3]])
		if !ok {
			continue
		}
		dur, ok := yrcMillis(body[idx[4]:idx[5]])
		if !ok {
			continue
		}
		words = append(words, Word{At: at, End: at + dur, Text: text})
	}
	words = trimWords(words)
	return words
}

// trimWords 去掉整句首尾的空格，并且丢掉中间的空词。
//
// ⚠️ **只动首尾**：词与词之间的空格要留着，英文词的 `Line.Text` 靠它们才读得
// 通；中间整段只有空格的那个词并到前一个词上，否则界面切前缀时会多出一个
// 空片段。
func trimWords(words []Word) []Word {
	out := make([]Word, 0, len(words))
	for _, w := range words {
		if strings.TrimSpace(w.Text) == "" {
			if len(out) > 0 {
				out[len(out)-1].Text += w.Text
			}
			continue
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	out[0].Text = strings.TrimLeftFunc(out[0].Text, unicode.IsSpace)
	out[len(out)-1].Text = strings.TrimRightFunc(out[len(out)-1].Text, unicode.IsSpace)
	kept := out[:0]
	for _, w := range out {
		if w.Text != "" {
			kept = append(kept, w)
		}
	}
	return kept
}

// wordsText 把词按顺序拼回整句。
func wordsText(words []Word) string {
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w.Text)
	}
	return b.String()
}

// yrcMillis 解析一段毫秒数字。带符号、超长、非数字都返回 false。
//
// 和 lrc.go 的 `atoi` 分开写，是因为这里刻意不用 `strconv.Atoi` 之外的东西：
// 正则已经保证只有数字，出错只发生在「位数超过 int」这种理论上；而理论上
// 的错一旦发生就是整句丢掉。
func yrcMillis(s string) (time.Duration, bool) {
	if s == "" || len(s) > 12 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return time.Duration(n) * time.Millisecond, true
}

// SpreadWords 把词再拆成逐字的时间。
//
// 网易云给的词不一定是单字：一个词可能是两三个汉字。逐字上色要的是字，
// 所以把 CJK/假名/谚文的词按字数均分它的时长；拉丁词不拆（拆开一个英文
// 单词的字母逐字亮，看起来像打错字）。
//
// 分配只占词时长的 85%，留下 15% 的缝：全占满的话，前一个字的亮色正好
// 咬到下一个字，画面上是整句一起亮，而不是一个字一个字走。
func SpreadWords(words []Word) []Word {
	out := make([]Word, 0, len(words))
	for _, w := range words {
		runes := []rune(w.Text)
		if w.End <= w.At || !splittable(runes) {
			out = append(out, w)
			continue
		}
		sung := time.Duration(float64(w.End-w.At) * 0.85)
		step := sung / time.Duration(len(runes))
		if step <= 0 {
			out = append(out, w)
			continue
		}
		for i, r := range runes {
			at := w.At + time.Duration(i)*step
			end := at + step
			if i == len(runes)-1 {
				end = w.At + sung
			}
			out = append(out, Word{At: at, End: end, Text: string(r)})
		}
	}
	return out
}

// splittable 报这个词值不值得逐字拆：至少两个字，而且除空格和标点以外
// 全是 CJK/假名/谚文。
func splittable(runes []rune) bool {
	n := 0
	for _, r := range runes {
		switch {
		case unicode.IsSpace(r):
			continue
		case isCJK(r):
			n++
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			continue
		default:
			return false
		}
	}
	return n >= 2
}

// isCJK 是「逐字唱」的那几种文字：汉字、平假名、片假名、谚文。
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
