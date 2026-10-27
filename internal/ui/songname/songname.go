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
// # 表里有什么
//
//	歌名   清洗过的标题（`widgets/title` 那一套：去【】[]、去宣传词、括号里
//	       的歌手名挪到副标题）
//	别名   原标题（用户可能把整个标题抄进评论里）、去掉书名号的写法、
//	       《》里那一段、标题里空格分开的那些段（宣传词那一段丢掉）
//	简称   「歌手 - 歌名」那种标题里的**歌名那一段**
//
// ⚠️ **简称认不准的时候宁可多算一条**：分不出「歌手 - 歌名」哪一段是歌名
// 时，两段都进表——漏掉的症状是「筛选开着却一条都筛不出来」（用户以为这个
// 功能坏了），而多算一条的症状只是多摆一条本来也在谈这首歌的评论。
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
		// ⚠️ **一个字的别名不收**：一个字的歌名会命中满屏的评论（正文里
		// 到处是那个字），而筛选出来的那些跟「提到这首歌」没关系。
		if utf8.RuneCountInString(k) < 2 {
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
	// 歌名：清洗过的那一份（界面上摆的就是它）。
	add(cleaned.Title)
	// 别名：原标题（用户可能把整个标题抄进评论里）。
	add(rawTitle)
	// 别名：去掉书名号那一层（「《晴天》」→「晴天」）。
	add(stripMarks(cleaned.Title))
	add(stripMarks(rawTitle))
	// 别名：《》/「」里那一段（「周杰伦《晴天》」里的「晴天」）。
	for _, inner := range markedSpans(cleaned.Title) {
		add(inner)
	}
	for _, inner := range markedSpans(rawTitle) {
		add(inner)
	}
	// 简称：「歌手 - 歌名」那种形状里的歌名那一段。
	//
	// ⚠️ **这一段不收英文**：英文标题里的空格是**词的一部分**（"Let It Go"
	// 拆成 "Go" 之后，「good」那种评论也会命中）。
	for _, seg := range splitDash(cleaned.Title) {
		// 歌手那一段不是歌名（评论里写「周杰伦」不等于提到「晴天」）。
		if isArtist(seg, hints) {
			continue
		}
		// 「官方MV」那一段是宣传词（宣传词那张表在 `widgets/title` 里，
		// 别处不抄第二份）。
		if tclean.IsPromoOnly(seg) {
			continue
		}
		add(seg)
	}
	// 别名：标题里**空格分开的**那些段（「官方MV 晴天」里的「晴天」）。
	//
	// ⚠️ **中文标题里的空格是分隔符，英文标题里的空格是词的一部分**——所以
	// 这一档只收**含汉字**的那些段（理由同上面那一条）。
	for _, seg := range splitSpaces(cleaned.Title) {
		if isArtist(seg, hints) {
			continue
		}
		if tclean.IsPromoOnly(seg) {
			continue
		}
		if !hasHan(seg) {
			continue
		}
		add(seg)
	}
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

// stripMarks 去掉书名号和引号那一层。
//
// ⚠️ **它不去【】**：那是 `widgets/title` 的活（那一层去掉的是「视频的痕迹」
// 和「宣传词」，而这里是「用户抄进评论里的写法」）。
func stripMarks(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '《', '》', '「', '」', '『', '』', '“', '”', '‘', '’', '"', '\'':
			return -1
		}
		return r
	}, s)
}

// dashSeps 是「标题里分隔两段」的那些写法。
//
// ⚠️ **不带空格的连字符不算**：「Rock-N-Roll」拆成三段之后，「N」会变成
// 一条别名（而正文里一个「n」满屏都是）。
var dashSeps = []string{" - ", " – ", " — ", "－"}

// splitDash 按分隔符把标题拆成几段。
//
// ⚠️ **拆不出两段就返回 nil**：那一句不是「歌手 - 歌名」那种形状，没有
// 简称可认。
func splitDash(s string) []string {
	for _, sep := range dashSeps {
		if !strings.Contains(s, sep) {
			continue
		}
		parts := strings.Split(s, sep)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) >= 2 {
			return out
		}
	}
	return nil
}

// splitSpaces 按空白把标题拆成几段。
//
// ⚠️ **它拆的是空白，不是词**：中文标题里的空格是分隔符（「官方MV 晴天」），
// 而英文标题里的空格是词的一部分（"Let It Go"）——所以调用方只收含汉字的
// 那些段（见 `New`）。
func splitSpaces(s string) []string {
	return strings.Fields(s)
}

// markedSpans 取出书名号里那些段（「周杰伦《晴天》」里的「晴天」）。
//
// ⚠️ **它认的是《》和「」**：中文歌名常这么摆，而用户在评论里写的是里面
// 那一段。
func markedSpans(s string) []string {
	var out []string
	for _, pair := range []struct{ open, close string }{
		{"《", "》"}, {"「", "」"}, {"『", "』"},
	} {
		rest := s
		for {
			i := strings.Index(rest, pair.open)
			if i < 0 {
				break
			}
			rest = rest[i+len(pair.open):]
			j := strings.Index(rest, pair.close)
			if j < 0 {
				break
			}
			inner := strings.TrimSpace(rest[:j])
			if inner != "" {
				out = append(out, inner)
			}
			rest = rest[j+len(pair.close):]
		}
	}
	return out
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

// artistHints 是「这一首的艺人」那几个可能的写法。
//
// ⚠️ **两处**：条目自己的艺人（音乐库音频是歌手，mv 是 UP 主），加上标题
// 方括号里挪出来的那一段（`widgets/title` 的 `Extra`，多半也是歌手）。
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
