// Package songname 算「这一首的歌名别名表」，并按它判「一条评论的正文有没有
// 提到这首歌」。
//
// # 为什么要有这张表
//
// 评论页有两种条目，而它们拿到的评论**语义不同**（研究票 #537 报告 §4.1
// 实测）：
//
//	mv    拿到的是**这个视频**的评论——里面大部分不是在聊这首歌
//	audio 拿到的是**这首歌**的评论
//
// ⇒ 页面上加一枚开关「只看正文里提到这首歌的评论」（票 #574）。而「提到这
// 首歌」这件事**没法只拿一个歌名去判**：B 站的标题里全是视频的痕迹
// （`【4K修复】`、`官方MV`、`【歌手】`），而用户在评论里写的也是各种写法
// （「晴天」「《晴天》」「周杰伦 - 晴天」）。
//
// # 一张表，一处算
//
// 这张表是这个包里**唯一**的一处口径：界面层只调它，不自己再拼一份歌名或
// 别名——两份口径迟早分叉，而分叉出来的那种差异极难查（一个界面上筛出三条，
// 另一个筛出五条，而两边看着都「对」）。
//
// # 歌名是从标题里**认出来**的
//
// B 站音乐视频的标题有几种常见形状（每一种都把歌名藏在里面）：
//
//	【晴天】              去掉方括号之后剩下「晴天」⇒ 歌名是「晴天」
//	【周杰伦】晴天         去掉方括号之后剩下「周杰伦 晴天」⇒ 歌名是**后**
//	                      一段（前面那一段是歌手）
//	歌手《晴天》           书名号里那一段就是歌名
//	周杰伦 - 晴天          破折号**后**那一段是歌名（前面那一段是署名）
//	周杰伦-晴天            紧凑连字符，同上
//	晴天：电影主题曲         按冒号切成几段，剔掉「说明」那一族 ⇒ 歌名是
//	                      「晴天」；「歌手：歌名」⇒ 剔不掉什么 ⇒ 歌名是
//	                      最后那一段（歌手那一段丢掉）
//	Let It Go             英文标题：空格是**词的一部分**，不拆
//
// ⇒ `New` 按这些形状把歌名认出来，收进一张表；匹配是「小写 + 去空白 +
// 子串」。
//
// ⚠️ **方括号只当分隔符**（`stripSpans` 去掉那对括号、留下里面的字）：B 站
// 的方括号里装的多半是**歌手**（「【周杰伦】晴天」），把它当歌名的话，评论
// 里写「周杰伦」就会被算成「提到了这首歌」。
//
// ⇒ 歌名从**去掉括号之后剩下的那一段**里认，而那一段的**最后一段**就是歌
// 名（「【晴天】」⇒「晴天」；「【周杰伦】晴天」⇒「晴天」）。
//
// ⚠️ **宁可多算一条，也不要一条都找不到**：漏掉的症状是「筛选开着却一条都
// 筛不出来」（用户以为这个功能坏了），而多算一条的症状只是多摆一条本来也
// 在谈这首歌的评论。
package songname

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tclean "github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
)

// Table 是「这一首的别名表」。
//
// ⚠️ **它是这个包里唯一的一处口径**（理由见文件头）：界面层只调它，不自己
// 再拼一份歌名或别名。
type Table struct {
	// forms 是别名表里的那些写法（**原始写法**，给人看的）。
	forms []string
	// keys 是同一批写法的**匹配用形式**（小写 + 去空白），与 forms 一一对应。
	//
	// ⚠️ **两份是分开的**：匹配用的是 keys（`Mentions` 每条评论都要跑一次，
	// 而正文是长的），而 `Forms` 给人看的是 forms。
	keys []string
}

// New 造这一首的别名表。
//
// rawTitle 是这一首的标题（B 站的**原标题**，带【】那些视频痕迹）；artist
// 是它的艺人（音乐库音频是歌手，mv 是 UP 主）——它只用来认「标题里哪一段是
// 歌手那一段」。
func New(rawTitle, artist string) Table {
	var t Table
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := normalize(s)
		if k == "" {
			return
		}
		// ⚠️ **一个字的别名**：汉字收（那是真歌名——《鱼》《光》），拉丁字母
		// 不收（一个字母会命中满屏的评论，而那不是歌名）。
		if utf8.RuneCountInString(k) < 2 && !isHanRune(k) {
			return
		}
		for _, old := range t.keys {
			if old == k {
				return
			}
		}
		t.forms = append(t.forms, s)
		t.keys = append(t.keys, k)
	}

	cleaned := tclean.Clean(rawTitle)
	hints := artistHints(artist, cleaned.Extra)

	// ① 《歌名》：「歌手《歌名》」里那一段就是歌名。
	//
	// ⚠️ **书名号里装的几乎总是歌名**（而【】里装的常常是歌手），所以这一档
	// 不看别的——它就是歌名。
	for _, s := range bookSpans(rawTitle) {
		if tclean.IsPromoOnly(s) || isQualifier(s) {
			continue
		}
		add(s)
	}

	// ② 去掉各种括号之后剩下那一段里的歌名。
	//
	// ⚠️ **方括号只当分隔符**（`stripSpans` 去掉那对括号、留下里面的字）：
	// B 站的方括号里装的多半是**歌手**（「【周杰伦】晴天」），把它当歌名
	// 的话，评论里写「周杰伦」就会被算成「提到了这首歌」。
	//
	// ⇒ 歌名从**去掉括号之后剩下的那一段**里认，而那一段的**最后一段**就是
	// 歌名（「【晴天】」⇒「晴天」；「【周杰伦】晴天」⇒「晴天」）。
	plain := stripSpans(rawTitle)
	for _, s := range songSegments(plain, hints) {
		add(s)
	}

	// ③ 清洗过的标题与原标题：用户可能把整个标题抄进评论里。
	add(cleaned.Title)
	add(rawTitle)
	return t
}

// Mentions 报这条正文有没有提到这首歌。
//
// ⚠️ **判的是正文**（`bili.Comment.Message`），不是昵称也不是表情表：这一
// 票问的是「这条评论在不在聊这首歌」。
func (t Table) Mentions(body string) bool {
	if body == "" || len(t.keys) == 0 {
		return false
	}
	b := normalize(body)
	if b == "" {
		return false
	}
	for _, k := range t.keys {
		if strings.Contains(b, k) {
			return true
		}
	}
	return false
}

// Empty 报这张表是不是空的（这一首没有名字）。
//
// ⚠️ **空表不能拿来筛**：那样会把这一首的评论全筛掉，而界面上一句话都
// 说不清为什么（用户看到的是「一条都没有」，而真相是「不知道这首歌叫什么」）。
func (t Table) Empty() bool { return len(t.keys) == 0 }

// Forms 是别名表里的那些写法（原始写法，给人看的）。
func (t Table) Forms() []string { return t.forms }

// songSegments 从「标题去掉各种括号之后的那一段」里取出**歌名那几段**。
//
// ⚠️ **三种分隔符三种取法**（B 站的标题语法）：
//
//	破折号「歌手 - 歌名」  歌名在**后**那一段——前面那一段是署名，而提到
//	                      歌手不等于提到这首歌（票 #574 的评审：提到「周杰
//	                      伦」的那些评论不该被算成提到了「晴天」）。
//	冒号「歌手：歌名」  按冒号切成几段，**把「说明」那一族剔掉，剩下最后
//	                      那一段是歌名**——「歌手：歌名」剔不掉什么 ⇒ 最后
//	                      那一段（歌手那一段丢掉）；「歌名：说明」剔掉说明 ⇒
//	                      最后剩的就是左边那一段。
//	空白「官方MV 歌名」    歌名在**后**那一段。
//
// ⚠️ **两种分隔符挨在一起的时候，先切外层的**：「歌手-歌名：说明」那一档，
// 先按破折号切成「歌手」和「歌名：说明」，再把后一段按冒号切一刀 ⇒ 歌名是
// 「歌名」（票 #574 的第二轮评审：那一档原来只认出「歌名：说明」，于是正文
// 里提到「歌名」的那一条被判成没提到）。
func songSegments(plain string, hints []string) []string {
	s := strings.TrimSpace(plain)
	if s == "" {
		return nil
	}
	if head, tail, ok := cutSep(s, dashSeps, true); ok {
		// ⚠️ **后一段自己再认一遍**（递归）：「歌手-歌名：说明」那一档的
		// 后一段是「歌名：说明」，它自己里面还有一个冒号。
		if got := songSegments(tail, hints); len(got) > 0 {
			return got
		}
		// 「晴天 - 官方MV」那一档：后一段全是宣传词 ⇒ 回前一段。
		//
		// ⚠️ **这是「X - 说明」那一档**：X 到底是歌名还是署名，从标题本身
		// 分不出来（认得出艺人时那一段会被剔掉，见 `plainSegments`）——
		// 而这一档**本来就没有歌名可认**（后那一段是说明）。
		if got := songSegments(head, hints); len(got) > 0 {
			return got
		}
		return nil
	}
	// 🔴 **冒号那一档**：按冒号切成**几段**，把「说明」那一族剔掉，**剩下
	// 最后那一段是歌名**。
	//
	//	歌手：歌名          ⇒ 剔不掉什么 ⇒ 最后那段 = 歌名（歌手那段丢掉）
	//	歌名：说明          ⇒ 「说明」剔掉 ⇒ 最后剩的是左边那段（歌名）
	//	歌手：歌名 (Live)   ⇒ 「(Live)」是版本说明，剥掉 ⇒ 歌名
	//	歌手：歌名：说明     ⇒ 「说明」剔掉 ⇒ 最后剩的是中间那段（歌名）
	//
	// ⚠️ **「说明」是按字段判的，不是按整段判**（票 #574 第三轮评审的 F7）：
	// 「周杰伦：晴天 (Live)」那一档，那个 "Live" 是**版本说明**（要剔掉），
	// 而「晴天」不是（要留着）——按整段判的话整段被当成说明 ⇒ 歌名跑到左边
	// 去了（而左边是歌手 ⇒ 只提歌手的评论被算成提到歌，正文提到歌名的那一条
	// 反而被算成没提到）。
	if parts := splitColon(s); len(parts) > 1 {
		var last []string
		for _, p := range parts {
			got := plainSegments(p, hints)
			if len(got) == 0 {
				// ⚠️ **整段都是说明/宣传词**（「晴天：电影主题曲」那一段的
				// 右边那一段）⇒ 这一段不是歌名。
				continue
			}
			last = got
		}
		if len(last) == 0 {
			return nil
		}
		// ⚠️ **左边那几段不进表**（票 #574 的第二、三轮评审：「周杰伦：晴天」
		// 那一档把「周杰伦」收进了别名表，于是只提到歌手的评论也被算成提到
		// 了歌）——**提到歌手不等于提到这首歌**。
		return last
	}
	return plainSegments(s, hints)
}

// plainSegments 把一段文字拆成候选，并去掉宣传词、歌手名与版本说明。
func plainSegments(s string, hints []string) []string {
	var kept []string
	for _, seg := range strings.Fields(s) {
		if isNoteWord(seg) {
			continue
		}
		if isArtist(seg, hints) {
			continue
		}
		kept = append(kept, seg)
	}
	if len(kept) == 0 {
		return nil
	}
	// ⚠️ **全英文的时候空格是词的一部分**（"Let It Go" 拆成 "Go" 之后，
	// 「good」那种评论也会命中）⇒ 整段就是歌名。
	if !anyHan(kept) {
		return []string{strings.Join(kept, " ")}
	}
	// 中文标题里的空格是分隔符（「官方MV 晴天」「歌手 歌名」）⇒ 歌名是**后**
	// 那一段。
	return []string{kept[len(kept)-1]}
}

// dashSeps 是「标题里分隔两段」的那些破折号。
//
// ⚠️ **紧凑的连字符也算**（"周杰伦-晴天"）：那是 B 站上最常见的写法之一。
var dashSeps = []string{" - ", " – ", " — ", "－", "-", "–", "—"}

// colonSeps 是「标题里分隔两段」的那些冒号。
var colonSeps = []string{"：", ":"}

// cutSep 在分隔符那一处把 s 切成两段。
//
// ⚠️ **只有汉字标题才切**：英文标题里的连字符和冒号是**名字的一部分**
// （"Rock-N-Roll"、"Let It Go: Official"），拆开之后 "Roll" / "Official"
// 会变成两条别名（而正文里一个 "roll" 满屏都是）。
//
// ⚠️ **fromEnd 说的是取哪一个**：破折号取**最后**那一处（"歌手 - 歌名"），
// 冒号取**最前**那一处（"歌名：说明：更多"）。
func cutSep(s string, seps []string, fromEnd bool) (head, tail string, ok bool) {
	idx := -1
	sep := ""
	for _, sp := range seps {
		i := strings.Index(s, sp)
		if fromEnd {
			i = strings.LastIndex(s, sp)
		}
		if i < 0 {
			continue
		}
		if idx < 0 || (fromEnd && i > idx) || (!fromEnd && i < idx) {
			idx, sep = i, sp
		}
	}
	if idx < 0 {
		return "", "", false
	}
	head = strings.TrimSpace(s[:idx])
	tail = strings.TrimSpace(s[idx+len(sep):])
	if !hasHan(head) && !hasHan(tail) {
		return "", "", false
	}
	return head, tail, true
}

// spanPairs 是标题里那些成对的括号。
var spanPairs = []struct{ open, close string }{
	{"【", "】"}, {"[", "]"},
	{"（", "）"}, {"(", ")"},
	{"《", "》"}, {"「", "」"}, {"『", "』"},
}

// bookSpans 是「书名号」里那些段（「歌手《晴天》」里的「晴天」）。
//
// ⚠️ **它认的是《》「」『』**：中文歌名常这么摆，而用户在评论里写的是里面
// 那一段。
func bookSpans(s string) []string {
	var out []string
	for _, pair := range []struct{ open, close string }{
		{"《", "》"}, {"「", "」"}, {"『", "』"},
	} {
		out = append(out, spansOf(s, pair.open, pair.close)...)
	}
	return out
}

// spansOf 取出 open/close 之间那些段。
func spansOf(s, open, close string) []string {
	var out []string
	rest := s
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			break
		}
		rest = rest[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			break
		}
		inner := strings.TrimSpace(rest[:j])
		if inner != "" {
			out = append(out, inner)
		}
		rest = rest[j+len(close):]
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
// 「Live版」是版本说明（`isQualifier`）⇒ 剩下的就是歌名。
func stripSpans(s string) string {
	for _, pair := range spanPairs {
		s = strings.ReplaceAll(s, pair.open, " ")
		s = strings.ReplaceAll(s, pair.close, " ")
	}
	return strings.TrimSpace(s)
}

// qualifiers 是「版本说明 / 副标题」那一族（「纯音乐版」「现场版」「电影主题
// 曲」「副标题」…）。
//
// ⚠️ **它们不是歌名**：标题写成「歌名 纯音乐版」的时候，歌名是前面那一段。
var qualifiers = []string{
	"版", "现场", "live", "纯音乐", "伴奏", "原声", "片尾", "片头", "翻唱",
	"钢琴", "二胡", "弦乐", "吉他", "cover", "remix", "official", "字幕",
	"歌词", "纯享", "instrumental", "demo", "试听",
	// ⚠️ **版本说明的「ver」那一族**（"Live Ver."、"Live Version"）：不带它
	// 的话「Ver.」那个字段认不出来 ⇒ 歌名被它顶掉（「晴天 Live Ver.」⇒ 歌名
	// 变成「Ver.」⇒ 正文提到「晴天」的那一条被判成没提到）。
	"ver", "version",
	"主题曲", "插曲", "片尾曲", "片头曲", "原声带", "副标题", "说明",
}

// isQualifier 报这一段是不是版本说明那一族。
//
// ⚠️ **带后缀的那一档只认短的**（5 个字以内）：「告白气球新版」是**歌名的一
// 部分**，不是说明——而子串匹配照样能命中「告白气球」那一条（"告白气球" ⊂
// "告白气球新版"）。
func isQualifier(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" {
		return false
	}
	for _, q := range qualifiers {
		if l == q {
			return true
		}
	}
	for _, suf := range []string{"版", "主题曲", "插曲", "原声带", "副标题"} {
		if strings.HasSuffix(l, suf) && utf8.RuneCountInString(l) <= 5 {
			return true
		}
	}
	return false
}

// splitColon 按**全部**的冒号把一段切成几段。
//
// ⚠️ **不是一个**：冒号那一档的标题常有好几段（「歌手：歌名：说明」），而
// 哪一段是歌名要看**哪几段是说明**（`stripTrailingNotes`）。
//
// ⚠️ **没有冒号就只那一段**（调用方看 `len(parts) > 1` 才知道走没走冒号
// 那一档）。
func splitColon(s string) []string {
	var out []string
	rest := s
	for {
		head, tail, ok := cutSep(rest, colonSeps, false)
		if !ok {
			break
		}
		out = append(out, head)
		rest = tail
	}
	return append(out, rest)
}

// isNoteWord 报这一段是不是版本说明/宣传词/说明那一族（⇒ 它不是歌名）。
//
// ⚠️ **两张表**：宣传词那张在 `widgets/title`（`IsPromoOnly`），版本说明
// 那一族在这里（`qualifiers`）——**不自己再抄一份宣传词表**（理由见
// `widgets/title.IsPromoOnly` 的注释）。
func isNoteWord(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// ⚠️ **末尾的标点不算事**（"Ver."、"Live。"）：版本说明常常带个句点。
	trimmed := strings.TrimRight(s, ".,，。、!！?？")
	if trimmed == "" {
		return false
	}
	// ⚠️ **用「含有」而不是「全是」**：一个字段里带着宣传词（「官方发布」）
	// 也不该是歌名。
	//
	// 🔴 **按字段判，不按整段判**（票 #574 第三轮评审的 F7）：「晴天 (Live)」
	// 那一档里「Live」是版本说明（剔掉）、「晴天」是歌名（留着）——按整段判
	// 的话整段被当成说明 ⇒ 歌名跑到左边（歌手）去了。
	if tclean.ContainsPromo(trimmed) {
		return true
	}
	// ⚠️ **版本说明那一族**：整段就是它（"电影主题曲"、"现场版"、"Live"…）。
	if isQualifier(trimmed) {
		return true
	}
	// ⚠️ **含着「说明」那一族的中文词**：「电影主题曲」整段不是"主题曲"（所以
	// `isQualifier` 认不出来），但它**含着**"主题曲" ⇒ 它是说明。
	return containsDescriptionWord(trimmed)
}

// containsDescriptionWord 报这一段里**有没有**「说明」那一族的中文词。
//
// ⚠️ **只收中文**：英文那几个（live/cover/remix/official/demo…）是**整个
// 词**才算（`isQualifier` 那一档），而按子串收的话「Olive」「believe」这
// 种歌名会被误判成说明（`live` 是 "olive" 的子串）。
func containsDescriptionWord(s string) bool {
	for _, w := range descriptionWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// descriptionWords 是「说明」那一族里**中文**的词。
//
// ⚠️ **收得紧**：一个词只要**可能**出现在歌名里，就不进这张表——否则「歌手：
// 钢琴家的告白」那一档会被认成「歌名：说明」（而「版」「歌词」「钢琴」「吉他」
// 这些恰恰都在歌名里出现过）。
//
// ⚠️ **宣传词不在这里**：那张表在 `widgets/title` 里（`IsPromoOnly`），
// 两边不各抄一份（理由见 `widgets/title.IsPromoOnly` 的注释）。
var descriptionWords = []string{
	"主题曲", "插曲", "片尾曲", "片头曲", "原声带", "纯音乐", "伴奏",
	"现场", "演唱会", "巡演", "翻唱", "副标题", "说明", "介绍", "解说",
	"字幕", "纯享",
}

// normalize 是「匹配用的写法」：小写 + 去掉空白。
//
// ⚠️ **去空白是有意的**：歌名「Let It Go」在评论里常被写成「letitgo」，而
// 中文标题里的空格也常是可有可无的（「大 鱼」）。
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// hasHan 报这一段里有没有汉字。
func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// anyHan 报这几段里有没有汉字。
func anyHan(segs []string) bool {
	for _, s := range segs {
		if hasHan(s) {
			return true
		}
	}
	return false
}

// isHanRune 报这个写法是不是**一个汉字**。
func isHanRune(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size == len(s) && unicode.Is(unicode.Han, r)
}

// artistHints 是「这一首的艺人」那几个可能的写法。
//
// ⚠️ **两处**：条目自己的艺人（音乐库音频是歌手，mv 是 UP 主），加上标题
// 方括号里挪出来的那一段（`widgets/title` 那样拼的）。
func artistHints(artist, extra string) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, old := range out {
			if old == s {
				return
			}
		}
		out = append(out, s)
	}
	add(artist)
	// `Extra` 是几段用 " · " 连着的（`widgets/title` 那样拼的）。
	for _, seg := range strings.Split(extra, " · ") {
		add(seg)
	}
	return out
}

// isArtist 报这一段是不是艺人名。
func isArtist(seg string, hints []string) bool {
	s := strings.TrimSpace(seg)
	if s == "" {
		return false
	}
	for _, h := range hints {
		if strings.EqualFold(s, h) {
			return true
		}
	}
	return false
}
