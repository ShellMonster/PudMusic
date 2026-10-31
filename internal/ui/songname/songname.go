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
// # ⚠️ **「认出歌名」那一份逻辑在 `internal/ui/songmeta`**
//
// 这一包只管**匹配**（别名表 + 归一化 + 子串匹配），而「从标题里认出歌名」住
// 在公共层——**显示**（`internal/ui/widgets/title`）和**匹配**（这一包）**共
// 用**它。
//
// ⚠️ **为什么必须共用**：同一个视频在播放条上写着「晴天」，而在评论页里却
// 筛不出提到「晴天」的评论——那是两份口径分叉出来的症状，而两边看着都
// 「对」。
//
// ⇒ 这一包调 `songmeta.Identify` / `songmeta.Aliases`，**不自己再拼一份歌名
// 或别名**。
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
//	歌手：歌名 (Live 2019)   括号**整组**是版本说明 ⇒ 整组剔掉 ⇒ 歌名是
//	                      「歌名」（见 `brackets.go`：按空格切字段会把
//	                      那个括号切成「(Live」「2019)」两个半个括号的
//	                      字段 ⇒ 歌名被它们顶掉 ⇒ 正文里提到歌名的那一条
//	                      评论一条都命不中）
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
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ShellMonster/PudMusic/internal/ui/songmeta"
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
//
// 🔴 **认不出歌名的时候给**空表**（`Empty` 是 true）——而**不是**给一张装着
// 原标题的表：那一张表会把这一首的评论**全筛掉**（正文里没有人写整个视频
// 标题），而界面上一句话都说不清为什么（见 `Empty` 的注释）。
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

	// ⚠️ **歌名与别名都从公共层拿**（`songmeta`）——**这一包不自己再认一遍**。
	//
	// ⚠️ **艺人名要传进去**（`artist` + 方括号里挪出来的那一段）：那是认「哪
	// 一段是署名」的凭据（`songmeta` 的 `hints`）。
	hints := artistHints(rawTitle, artist)
	for _, s := range songmeta.Aliases(rawTitle, hints...) {
		add(s)
	}
	return t
}

// Mentions 报这条正文有没有提到这首歌。
//
// ⚠️ **判的是正文**（`bili.Comment.Message`），不是昵称也不是表情表：这一
// 票问的是「这条评论在不在聊这首歌」。
//
// ⚠️ **它是 `Matches` 的薄封装**：口径只有一处（「哪些写法算提到」那张表 +
// 「怎么算提到」那套归一化）——两份判据迟早分叉，而分叉出来的症状是「筛选
// 筛出来了，正文里却没有一处高亮」。
func (t Table) Mentions(body string) bool {
	return len(t.Matches(body)) > 0
}

// Range 是正文里一段命中的字符区间（**按 rune**，左闭右开）。
//
// ⚠️ **按 rune 不按字节**：正文里中日英混排还有 emoji（四个字节一个字），
// 按字节下标切的话 `[]rune(body)[r.Start:r.End]` 会切出半个字——评论页拿它
// 切 span 时那一整段就会错位。
type Range struct{ Start, End int }

// Matches 是正文里**每一处**命中的字符区间（按出现位置排好、重叠的合并）。
//
// # 为什么要单独一支（`Mentions` 不够用）
//
// `Mentions` 只答「有没有」——筛选用它够了。而评论页要把命中的那一段**染上
// 底色**（票 #684），那就得知道「在正文的第几个字上」。两份判据各写一套的
// 话迟早分叉（筛出来了却没有一处高亮），所以 `Mentions` 就是这一支的薄封装。
//
// # 🔴 **归一化串上的下标**不能**直接切原串
//
// `normalize` 做了两件事：去掉所有空白、转小写。于是「Let It Go」归一化之后
// 是 "letitgo"（7 个字），正文里写的 "let it go" 有 9 个字——拿归一化串上的
// 下标去切原串，切出来的是**短一截**的一段（还可能是完全不相干的位置）。
//
// ⇒ 构造归一化串的时候**一边记一份映射**（归一化的第 j 个字来自原串的第几个
// rune），命中之后按它翻回原串。
//
// ⚠️ **翻回来的区间会包含别名中间原本夹着的空格**（"let it go" 那 9 个字）：
// 这是**对的**——高亮该覆盖整段，而不是把空格漏成几道缝。
//
// # 性能
//
// 评论页每一条评论跑一次（排一次版、缓存下来，见 `comments.go` 的
// `commentBodyOf`——**不是每帧**）。所以这里的目标是「一趟构造 + 每个别名一趟
// 子串搜索」：`strings.Index` 走的是字节搜索（快），而下标换算靠二分查那张
// 映射表——不嵌套重扫整条正文。
func (t Table) Matches(body string) []Range {
	if body == "" || len(t.keys) == 0 {
		return nil
	}
	// 归一化串 + 两份下标：`starts[j]` 是归一化串里第 j 个字的**字节**偏移，
	// `orig[j]` 是它来自原串的第几个 rune。
	//
	// ⚠️ **`orig` 记的是「产出了这个归一化字的那个原串 rune」**，所以两个
	// 归一化字之间的原串空白不在映射里——而映射回来时那一段区间**自然**把
	// 它们包含进去（`orig[s]` 到 `orig[e-1]+1`）。
	var b strings.Builder
	b.Grow(len(body))
	norms := utf8.RuneCountInString(body)
	orig := make([]int, 0, norms)
	starts := make([]int, 0, norms)
	runeIdx := 0
	for _, r := range body {
		if unicode.IsSpace(r) {
			runeIdx++
			continue
		}
		starts = append(starts, b.Len())
		orig = append(orig, runeIdx)
		b.WriteRune(unicode.ToLower(r))
		runeIdx++
	}
	if len(orig) == 0 {
		return nil
	}
	norm := b.String()

	var out []Range
	for _, k := range t.keys {
		if k == "" {
			continue
		}
		from := 0
		for from <= len(norm)-len(k) {
			j := strings.Index(norm[from:], k)
			if j < 0 {
				break
			}
			j += from
			// 归一化侧的 rune 区间 [s, e)：`starts` 是每个字的起始字节偏移，
			// 二分就能把字节偏移翻成字下标。
			end := j + len(k)
			s := sort.SearchInts(starts, j)
			e := sort.SearchInts(starts, end)
			// 🔴 **命中必须落在字边界上**：归一化串是按 rune 拼的，而子串匹配
			// 走的是字节——理论上不会错位（两边的码点来自同一批 rune），但真撞
			// 上中间那一格时**宁可不标**，也不拿一个错位的区间去切字符串。
			if s >= len(orig) || starts[s] != j ||
				(e < len(starts) && starts[e] != end) || e <= s {
				break
			}
			out = append(out, Range{Start: orig[s], End: orig[e-1] + 1})
			// ⚠️ **同一段里同一个别名只找一次**：别名是歌名（两三个字起），
			// 自己套自己的（"aaa" 那种）不是歌名会有的形状；从头 `j+1` 再找
			// 一遍会让这一步在病态输入上退化成平方。
			from = j + len(k)
		}
	}
	if len(out) == 0 {
		return nil
	}
	// 🔴 **重叠/相邻的合并**：别名表里有长短两档命中同一段的时候（「晴天」与
	// 「晴天真好听」），不合并就是同一段两层底色——画面上看不出来，但 span
	// 会白白多切几段。
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	merged := out[:0]
	for _, r := range out {
		if len(merged) > 0 && r.Start <= merged[len(merged)-1].End {
			if r.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
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

// isHanRune 报这个写法是不是**一个汉字**。
func isHanRune(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size == len(s) && unicode.Is(unicode.Han, r)
}

// artistHints 是「这一首的艺人」那几个可能的写法。
//
// ⚠️ **两处**：条目自己的艺人（音乐库音频是歌手，mv 是 UP 主），加上标题
// 方括号里挪出来的那一段（`songmeta` 那样认出来的）。
//
// ⚠️ **方括号里那一段要算进去**：认「哪一段是署名」的时候，「【周杰伦】晴天」
// 那一档里的「周杰伦」就是靠它认出来的（不然它会被当成歌名）。
func artistHints(rawTitle, artist string) []string {
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
	// ⚠️ **方括号里挪出来的那一段**（`songmeta.Identify` 的 `Extra`）：那是
	// 「标题里的歌手」——认「哪一段是署名」的时候要算它。
	if id := songmeta.Identify(rawTitle, artist); id.Extra != "" {
		for _, seg := range strings.Split(id.Extra, " · ") {
			add(seg)
		}
	}
	return out
}
