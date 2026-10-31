// 标题里那些**词表**：宣传词、版本说明、现场标记、角色词、艺人名。
//
// # ⚠️ **宣传词那张表只有这一份**
//
// 别处要判「这一段是不是只有宣传词」就走 `IsPromoOnly`，要判「这一段里有没有
// 宣传词」就走 `ContainsPromo`——自己再抄一份那张表就是两份口径，而分叉出来
// 的那种差异极难查（一个说「官方MV」是宣传词，另一个说不算）。
//
// （这条规矩原本写在 `internal/ui/widgets/title` 里；公共层搬过来之后，那边
// 变成薄封装，**表仍然只有这一份**。）
package songmeta

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// promoWords 是「宣传词」那一族：说**这个视频**的，不是说**这首歌**的。
//
// ⚠️ **表只有这一份**（理由见文件头）。
var promoWords = []string{
	"官方", "mv", "4k", "8k", "1080", "60fps", "超清", "高清", "蓝光", "修复", "完整版", "无损",
	"字幕", "歌词版", "歌词", "hi-res", "hires", "首发", "重制", "双语", "中字", "原创", "高音质",
	"音质", "动态歌词", "pv", "ost", "纯享", "官方首发", "全网首发", "最新", "收藏", "必听",
	// ⚠️ **分辨率那一族带尾巴的那一档**（"1080P"、"720P"、"2160P"）：光有
	// "1080" 认不出「1080P」——把 "1080" 抠掉之后还剩一个 "p"，而字母不在
	// 「只剩标点和数字」那一档里 ⇒ 那一段会被当成歌名。
	"1080p", "720p", "2160p", // ⚠️ **「动态歌词版」那一族**："动态歌词" 已经收了，"动态歌词版" 里还有
	// 一个 "版" 字 —— 而 "版" 不在表里（它可以是歌名的一部分）。
	"动态歌词版", "高音质无损", "官方字幕版",
}

// liveWords 是「现场」那一族：出现它就说明这一版是**现场版**。
//
// ⚠️ **它不是宣传词**（那是「这个视频怎么样」），而是**曲目身份**（现场版和
// 录音室版是两个不同的录音）——所以认出来之后要记 `Live`，不能只是丢掉。
var liveWords = []string{"现场", "live", "演唱会", "音乐会", "巡演", "巡回", "演出版", "舞台"}

// versionWords 是「版本说明」那一族（钢琴版 / 伴奏 / Remix / 翻唱 …）。
//
// ⚠️ **它们也不是噪声**（跟「现场」一样是曲目身份），但**显示时只留一个
// 标记位**（`Live`）——名字里带「钢琴版」三个字的那一版，用户看到的是
// 「起风了」，而不是「起风了（钢琴版）」。
//
// ⚠️ **收得紧**：一个词只要**可能**出现在歌名里，就不进这张表——否则
// 「告白气球新版」「钢琴家的告白」那种歌名会被认成版本说明。
var versionWords = []string{
	"版", "现场", "live", "纯音乐", "伴奏", "原声", "片尾", "片头", "翻唱",
	"钢琴", "二胡", "弦乐", "吉他", "cover", "remix", "official", "字幕",
	"歌词", "纯享", "instrumental", "demo", "试听",
	// ⚠️ **版本说明的「ver」那一族**（"Live Ver."、"Live Version"）。
	"ver", "version",
	// ⚠️ **「重制」那一族**（"Remaster"、"Remastered"）。
	"remaster", "remastered",
	// ⚠️ **「纯享」那一族**（B 站音乐区的「纯享」是同一档）。
	"完整演奏", "instrumental version",
}

// descWords 是「说明」那一族（主题曲 / 插曲 / 原声带 / 副标题 …）。
//
// ⚠️ **只收中文**：英文那几个（live / cover / remix / official / demo…）是
// **整个词**才算（`IsVersion` 那一档），而按子串收的话「Olive」「believe」
// 这种歌名会被误判成说明（"live" 是 "olive" 的子串）。
var descWords = []string{
	"主题曲", "插曲", "片尾曲", "片头曲", "原声带", "纯音乐", "伴奏",
	"现场", "演唱会", "巡演", "翻唱", "副标题", "说明", "介绍", "解说",
	"字幕", "纯享",
}

// roleWords 是「角色词」那一族：说**这个字是干什么的**，不是歌名也不是歌手。
//
// ⚠️ **它跟「艺人名」不是一回事**：「歌手《晴天》」里那个「歌手」不是人，
// 只是说「后面那段是歌」——把它当 Extra 显示出去的话，列表的副标题会变成
// 「歌手 · UP 名」。
var roleWords = []string{
	"歌手", "演唱", "主唱", "演唱者", "演唱:",
	"artist", "singer", "by", "featuring", "feat", "feat.", "ft", "ft.",
	"作词", "作曲", "编曲", "监制", "出品",
}

// featureWords 是「feat.」那一族的标记词：后面那一段是**合作歌手**。
//
// ⚠️ **它是「角色词」的一个子集**（角色词是「这一整段是干什么的」，而
// feature 是「这一段的**后半**是另一个歌手」）。
var featureWords = []string{"feat", "feat.", "ft", "ft.", "featuring"}

// IsPromoOnly 报这一段是不是**全是宣传词**（「官方MV」「4K修复」「高音质」…
// ）。
//
// ⚠️ **它是宣传词那张表的唯一出口**：别处要判「这一段是不是只有宣传词」就走
// 它——自己再抄一份那张表就是两份口径，而分叉出来的那种差异极难查（一个说
// 「官方MV」是宣传词，另一个说不算）。
//
// 🔴 **空串不算**：那是「没有这一段」，不是「这一段全是宣传词」。
func IsPromoOnly(s string) bool { return onlyPromo(s) }

// ContainsPromo 报这一段里**有没有**宣传词（「官方MV」「4K修复」「高音质」…
// ）。
//
// ⚠️ **它跟 `IsPromoOnly` 的区别是「全是」和「有」**：
//
//	IsPromoOnly   这一段**全是**宣传词（"（官方MV）"那一整个括号丢掉）
//	ContainsPromo 这一段**里有**宣传词（"官方发布"是说明，不是歌名）
//
// ⚠️ **宣传词那张表仍然只有这一处**（理由同 `IsPromoOnly`）。
//
// 🔴 **空串不算**：那是「没有这一段」。
func ContainsPromo(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return hasAny(s, promoWords)
}

// hasAny 是「s（不分大小写）里含 words 里任何一个」。
func hasAny(s string, words []string) bool {
	l := strings.ToLower(s)
	for _, w := range words {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// onlyPromo 是「这一段括号里全是宣传词」：把每个宣传词抠掉后只剩标点和空白。
//
// 🔴 **长的先抠**（"1080p" 在 "1080" 之前）：不这样的话「1080P」会被先抠掉
// "1080"、剩下一个 "p"——而字母不在「只剩标点和数字」那一档里 ⇒ 「1080P」
// 不算宣传词（于是在标题里被当成了歌名）。
func onlyPromo(inner string) bool {
	l := strings.ToLower(inner)
	if strings.TrimSpace(l) == "" {
		return false
	}
	for _, w := range promoWordsByLength {
		l = strings.ReplaceAll(l, w, "")
	}
	for _, r := range l {
		if !strings.ContainsRune(" ,，、/|丨+&·-_.。0123456789", r) {
			return false
		}
	}
	return true
}

// promoWordsByLength 是宣传词那张表，**按长度从长到短**排的。
//
// ⚠️ **建一次**（那��表不长，而 `onlyPromo` 每条标题要跑一次）。
var promoWordsByLength = func() []string {
	out := append([]string(nil), promoWords...)
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}()

// HasLive 报这一段里有没有「现场」那一族。
//
// ⚠️ **它是 `Live` 那个标记的唯一出口**（理由同 `IsPromoOnly`）。
//
// ⚠️ **英文那一族按**整词**判**（"live" / "Live"）而不是按子串：**"Olive"、
// "Believe"、"Delivery" 这些歌名里有 "live" 那个子串**——按子串判的话
// 「Olive」会被当成现场版（于是列表上多一枚「现场」标签，而那首不是现场版）。
func HasLive(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	// ⚠️ **中文那一族按子串**（"演唱会"、"现场版" 都不是单独的词）。
	for _, w := range []string{"现场", "演唱会", "音乐会", "巡演", "巡回", "演出版", "舞台"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	// ⚠️ **英文那一族按整词**（理由见上）。
	return hasWholeWord(s, []string{"live"})
}

// hasWholeWord 报 s 里有没有**整个词**是 words 里某一个（不区分大小写）。
//
// ⚠️ **按非字母数字切开再比**：中英混排的标题里没有空格（"晴天Live版"），
// 而纯按空格切的话 "Live版" 认不出来。
func hasWholeWord(s string, words []string) bool {
	l := strings.ToLower(s)
	fields := splitNonWord(l)
	for _, w := range words {
		if l == w {
			return true
		}
		for _, f := range fields {
			if f == w {
				return true
			}
		}
	}
	return false
}

// splitNonWord 按**非字母数字**把 s 切成几段。
func splitNonWord(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// isSectionWord 报这一段是不是「段落标记」那一族（"最后一段"、"副歌" …）。
//
// ⚠️ **它说的是视频的哪一段，不是歌名**：「周杰伦 - 晴天 最后一段」那一档里
// 「最后一段」不是歌名。
func isSectionWord(s string) bool {
	l := strings.TrimSpace(s)
	if l == "" {
		return false
	}
	for _, w := range sectionWords {
		if l == w {
			return true
		}
	}
	return false
}

// sectionWords 是「段落标记」那一族。
var sectionWords = []string{
	"最后一段", "第一段", "第二段", "第三段", "结尾", "开头", "前奏", "间奏",
	"副歌", "主歌", "桥段", "尾奏", "高潮", "片段", "纯享版", "试听片段",
}

// IsVersion 报这一段是不是**版本说明**（钢琴版 / 伴奏 / Remix / 翻唱 …）。
//
// ⚠️ **带后缀的那一档只认短的**（5 个字以内）：「告白气球新版」是**歌名的一
// 部分**，不是说明——而子串匹配照样能命中「告白气球」那一条（"告白气球" ⊂
// "告白气球新版"）。
func IsVersion(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" {
		return false
	}
	for _, q := range versionWords {
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

// isNoteWord 报这一段是不是**说明 / 宣传词 / 副标题**那一族（⇒ 它不是歌名）。
//
// ⚠️ **用「含有」而不是「全是」**：一个字段里带着宣传词（「官方发布」）也不该
// 是歌名。
//
// 🔴 **按字段判，不按整段判**：「晴天 (Live)」那一档里「Live」是版本说明（剔
// 掉）、「晴天」是歌名（留着）——按整段判的话整段被当成说明 ⇒ 歌名跑到左边
// （歌手）去了。
func isNoteWord(s string) bool {
	// ⚠️ **末尾的标点不算事**（"Ver."、"Live。"）：版本说明常常带个句点。
	//
	// ⚠️ **两头的括号也要去**（"(Live" ⇒ "Live"）：`IsVersion` 要**整个词**才
	// 是版本说明，而版本说明常常带着括号。
	trimmed := trimNotePunctuation(s)
	if trimmed == "" {
		return false
	}
	if ContainsPromo(trimmed) {
		return true
	}
	if IsVersion(trimmed) {
		return true
	}
	// ⚠️ **含着「说明」那一族的中文词**：「电影主题曲」整段不是"主题曲"（所以
	// `IsVersion` 认不出来），但它**含着**"主题曲" ⇒ 它是说明。
	return containsDescriptionWord(trimmed)
}

// containsDescriptionWord 报这一段里**有没有**「说明」那一族的中文词。
//
// ⚠️ **只收中文**：英文那几个（live/cover/remix/official/demo…）是**整个
// 词**才算（`IsVersion` 那一档），而按子串收的话「Olive」「believe」这种歌
// 名会被误判成说明（`live` 是 "olive" 的子串）。
func containsDescriptionWord(s string) bool {
	l := strings.ToLower(s)
	for _, w := range descWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// IsRoleWord 报这一段是不是「角色词」那一族（歌手 / feat. / 演唱 …）。
func IsRoleWord(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" {
		return false
	}
	for _, w := range roleWords {
		if l == w {
			return true
		}
	}
	return false
}

// IsFeatureLead 报这一段是不是「feat.」那一族的**标记词**（后面那一段是合作
// 歌手）。
func IsFeatureLead(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	for _, w := range featureWords {
		if l == w {
			return true
		}
	}
	return false
}

// IsArtistName 报这一段是不是**词典里的艺人名**（实现在 `artists.go`）。
//
// ⚠️ **这是「哪一边是歌名」那一档唯一的凭据**（理由见 `artists.go`）：两边都
// 没线索时那一条是**本质歧义**，而词典认得出哪一段是署名 ⇒ 另一段才是歌名。
