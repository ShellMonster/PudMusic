// 标题里那些**成对括号**的处理。
//
// # 为什么括号要**整组**判
//
// 认歌名的方式是「切字段，逐个字段判」（`fields.go`）。
// ⚠️ **按空格切字段会把一个括号组切成两半**：
//
//	「周杰伦：晴天 (Live 2019)」  切成  「周杰伦：晴天」/「(Live」/「2019)」
//
// 那两个**半个括号**的字段既不是版本说明（`(Live` 认不出来——`IsVersion` 要
// 整个词是 live）、也不是歌名 ⇒ **它们被当成了歌名** ⇒ 正文里提到真歌名
// 「晴天」的那一条评论一条都命不中。
//
// ⇒ 所以括号**整组当一个单位**：组里含版本说明（live / remix / …）或含 4 位
// 年份 ⇒ 整组是版本说明，剔掉。
//
// # 两种括号不是一回事
//
//	圆括号 `(…)` `（…）`  装的是**版本说明**（"(Live 2019)"）⇒ 整组剔掉
//	方括号 `【…】` `[…]`  装的多半是**歌手**（"【周杰伦】"），但**也可能是歌名**
//	                        （"【晴天】"）⇒ 靠 `fields.go` 那一档定
//
// # 括号外的裸年份**不许剔**
//
// 歌名《1984》是真的 ⇒ 光秃秃的「2019」留在标题里要当歌名的一部分——只有
// **括号里**的年份才是版本说明。
package songmeta

import (
	"strings"
	"unicode/utf8"
)

// bracketKind 是标题里那些成对括号的**种类**。
type bracketKind int

const (
	// parenBrackets 是**圆括号**：`(`…`)`、`（`…`）`。
	parenBrackets bracketKind = iota
	// otherBrackets 是**其余成对的括号**：`【`…`】`、`[`…`]`、`《`…`》`…
	otherBrackets
)

// bracketPair 是**一对**括号。
type bracketPair struct {
	open, close string
	kind        bracketKind
}

// bracketPairs 是标题里那些成对的括号。
//
// ⚠️ **圆括号与方括号不是一回事**（理由见文件头）——所以这里带着种类。
var bracketPairs = []bracketPair{
	{"（", "）", parenBrackets},
	{"(", ")", parenBrackets},
	{"【", "】", otherBrackets},
	{"[", "]", otherBrackets},
	{"《", "》", otherBrackets},
	{"「", "」", otherBrackets},
	{"『", "』", otherBrackets},
}

// bracketGroup 是标题里**一个**成对括号里的内容。
type bracketGroup struct {
	pair  bracketPair
	inner string
	start int // 起始括号的位置
	end   int // 闭合括号**之后**的位置
}

// prepareTitle 把标题里那些「版本说明」的括号剔掉，返回剩下的那一段。
//
// ⚠️ **它跑在 `stripSpans` 之前**：`stripSpans` 把括号**换成空白**（内容留着），
// 跑到那一步就再也看不出「哪几个字段本来在同一个括号里」了——而那正是这一族
// 的关键（见文件头）。
func prepareTitle(s string) string {
	groups := scanGroups(s)
	if len(groups) == 0 {
		return s
	}
	// ⚠️ **括号外面的那一段**：「括号里是版本说明 ⇒ 整组剔掉」这件事在**整段
	// 都是那个括号**的时候要手下留情（"（1984）"整个标题就是一个括号，而
	// 那个年份是**歌名**）。
	onlyGroup := strings.TrimSpace(outsideOf(s, groups)) == ""
	var b strings.Builder
	prev := 0
	for _, g := range groups {
		b.WriteString(s[prev:g.start])
		inner, drop := g.rewrite(onlyGroup)
		if drop {
			// 剔掉的组留一个空格，免得两边的字粘在一起。
			b.WriteString(" ")
		} else {
			b.WriteString(g.pair.open)
			b.WriteString(inner)
			b.WriteString(g.pair.close)
		}
		prev = g.end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// outsideOf 是标题里那些括号**外面**的部分。
func outsideOf(s string, groups []bracketGroup) string {
	var b strings.Builder
	prev := 0
	for _, g := range groups {
		b.WriteString(s[prev:g.start])
		prev = g.end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// rewrite 算这个括号组**该变成什么**（第二个返回值是「整组剔掉」）。
//
// ⚠️ **规则**：
//
//	组里还剩下别的字  ⇒ 只把版本说明那几个字段剔掉，别的留着
//	                   （"【晴天 Live 2019】" ⇒ "【晴天】"）
//	组里全是版本说明  ⇒ 圆括号：整组剔掉（"(Live 2019)"）
//	                   方括号：有版本说明那种词（"【Live】"）也剔掉；
//	                   而「只有年份」的（"（1984）"）是**歌名**，留着
func (g bracketGroup) rewrite(onlyGroup bool) (string, bool) {
	fields := strings.Fields(g.inner)
	var kept []string
	hasVersionWord := false
	for _, f := range fields {
		if isVersionField(f) {
			if isVersionWord(f) {
				hasVersionWord = true
			}
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) > 0 {
		return strings.Join(kept, " "), false
	}
	// ⚠️ **整段就是那个括号，而里面只有一个年份** ⇒ 那个年份是**歌名**
	//（《1984》是真的），不许剔。
	if onlyGroup && !hasVersionWord {
		return g.inner, false
	}
	if g.pair.kind == parenBrackets || hasVersionWord {
		return "", true
	}
	return g.inner, false
}

// scanGroups 找出标题里那些成对的括号（按出现顺序）。
//
// ⚠️ **没闭合的算「后面那一段都在里面」**：缺了右括号的时候（"(Live 2019"），
// 若把它当成普通字，那个「2019」会漏成**括号外**的字段（于是被当成歌名）。
func scanGroups(s string) []bracketGroup {
	var out []bracketGroup
	for i := 0; i < len(s); {
		rest := s[i:]
		var hit bracketPair
		found := false
		for _, p := range bracketPairs {
			if strings.HasPrefix(rest, p.open) {
				hit, found = p, true
				break
			}
		}
		if !found {
			// ⚠️ **一个字符一个字符地走**（括号都是多字节的，不能按字节切）。
			_, size := utf8.DecodeRuneInString(rest)
			i += size
			continue
		}
		inner := rest[len(hit.open):]
		j := strings.Index(inner, hit.close)
		if j < 0 {
			out = append(out, bracketGroup{
				pair:  hit,
				inner: inner,
				start: i,
				end:   len(s),
			})
			break
		}
		out = append(out, bracketGroup{
			pair:  hit,
			inner: inner[:j],
			start: i,
			end:   i + len(hit.open) + j + len(hit.close),
		})
		i += len(hit.open) + j + len(hit.close)
	}
	return out
}

// stripSpans 把标题里那些成对的括号**去掉**（**内容留着**）。
//
// ⚠️ **内容留着是故意的**：方括号里那一段（「【周杰伦】」）有可能是**歌名**
// （「【晴天】」）——真正把它剔掉的是后面那一步（「那一段的最后一段就是歌
// 名」与「版本说明那一族不算歌名」）。
//
// ⚠️ **圆括号也去**：「晴天（Live版）」去掉括号之后是「晴天 Live版」，而
// 「Live版」是版本说明 ⇒ 剩下的就是歌名。
func stripSpans(s string) string {
	for _, pair := range spanPairs {
		s = strings.ReplaceAll(s, pair.open, " ")
		s = strings.ReplaceAll(s, pair.close, " ")
	}
	return strings.TrimSpace(s)
}

// isVersionField 报这一段是不是**版本说明**（⇒ 它不是歌名）。
//
// ⚠️ **年份只在这一档里算**：「括号外的裸年份」是歌名的一部分（《1984》），
// 而**括号里**的年份是版本说明（"(2019)"）——`prepareTitle` 保证年份只可能
// 从括号里漏到这里。
func isVersionField(s string) bool {
	return isVersionWord(s) || isYearField(s)
}

// isVersionWord 报这一段是不是**版本说明**（说明 / 宣传词 / 副标题）。
func isVersionWord(s string) bool { return isNoteWord(s) }

// isYearField 报这一段是不是「4 位年份」。
//
// ⚠️ **只认 4 位**：三位（"007"）与带后缀的（"2019 remaster"）都不是年份这一
// 档（后者是多个字段，而切字段是**按空格**切的）。
func isYearField(s string) bool {
	t := trimNotePunctuation(s)
	if len([]rune(t)) != 4 {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trimNotePunctuation 去掉一段两头的括号与末尾的标点。
//
// ⚠️ **左头的括号也要去**（"(Live" ⇒ "Live"）：版本说明常常带着括号，而
// `IsVersion` 要**整个词**才算（"live" 而不是 "(live"）。
func trimNotePunctuation(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "()（）[]【】《》「」『』")
	return strings.TrimRight(s, ".,，。、!！?？()（）[]【】《》「」『』")
}
