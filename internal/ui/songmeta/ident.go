package songmeta

import (
	"strings"
)

// 这一层是「**从标题里认出歌名**」的那一份逻辑，**显示**和**匹配**共用它。
//
// # 为什么它能同时喂饱两个消费者
//
// 显示要的是**一个歌名**（"晴天"），匹配要的是**多个写法**（"晴天"、"《晴
// 天》"、"周杰伦 - 晴天"）。
//
// ⚠️ **两者要的是同一份「认出来」的逻辑**，否则同一个视频在播放条上写着「晴
// 天」、在评论页里却筛不出提到「晴天」的评论——而两边看着都「对」。
//
// ⇒ 这一层给的是**两种取法**，走**同一份**判据：
//
//	Identify  一个歌名（**显示**用）
//	Aliases   若干个写法（**匹配**用）
//
// # 认不出的时候
//
// 🔴 **认不出就是 `OK == false`**（不是「返回原标题」）：
//
//	显示层：认不出就退回**原标题**（`widgets/title` 那一档——宁可显示脏标题，
//	        也不显示空白）
//	匹配层：认不出就给**只有原标题的表**（`songname` 那一档——而「筛不了」这个
//	        事实由 `Table.Empty` 说，理由见 `Aliases` 的注释）
//
// ⚠️ **这两个语义是有区别的**，代码里说清楚了（见上面两行）。

// Ident 是「从一条标题里认出来的歌名」。
type Ident struct {
	// Name 是认出来的歌名（**认不出时是空串**）。
	Name string
	// OK 报有没有认出来。
	//
	// ⚠️ **认不出时 `Name` 是空串，不是原标题**——原标题是**显示层**的决
	// 定（见文件头）。
	OK bool
	// Extra 是标题里那些「**不是歌名、但对人有用**」的内容（歌手 / 专辑名），
	// 多段用 " · " 连。适合拼进副标题。
	//
	// ⚠️ **它跟 `Name` 不重叠**：方括号里那一段如果是歌名（"【晴天】"），它就
	// 是 `Name`，不是 `Extra`（两处都摆一遍的话，列表那一行的副标题会变成
	// 「晴天 · UP 名」——同一个名字摆了两遍）。
	//
	// ⚠️ **认不出歌名的时候它照样有值**（"【周杰伦】"那一档）：方括号里那一
	// 段是歌手，而它是**有用**的——**内容不许丢**（旧实现里这一段在「清洗
	// 之后标题空了」那条路上整个丢了，见 `widgets/title` 的历史注释）。
	Extra string
	// Live 是「这是现场版」。
	//
	// ⚠️ **版本标记不是噪声，是曲目身份**：现场版和录音室版是**两个不同的
	// 录音**——所以它要留着（而不是被当成宣传词丢掉）。
	Live bool
	// Alts 是「**本质歧义**」那一档的**另一段**（匹配层要两个都收）。
	//
	// ⚠️ **它是匹配层专用的**：「X - Y」两边都像艺人名、而词典认不出谁是谁的
	// 时候（"白月光 - 张碧晨"），显示层只显示一个（`Name`），而匹配层两个都
	// 收——不然用户在评论里写哪一段都命不中（见 `Aliases`）。
	//
	// ⚠️ **它跟 `Name` 不重叠**，而**显示层不看它**（显示层只认一个歌名）。
	Alts []string
}

// Identify 从一条 B 站原标题里认出歌名。
//
// hints 是**已知的艺人名**（界面上拿得到的那一份：mv 的 UP 主名、音乐库的歌手
// 名）——它只用来认「标题里哪一段是署名那一段」。
//
// ⚠️ **认不出来的时候 `OK` 是 false**（理由见文件头）。
func Identify(raw string, hints ...string) Ident {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Ident{}
	}
	// ⚠️ **Live 从**原标题**判**：版本说明的括号在 `prepareTitle` 里会被整组剔
	// 掉（"(Live 2019)"），剔完就看不出「这是现场版」了。
	live := HasLive(raw)

	// ⚠️ **先剥掉「版本说明」那些括号**（`prepareTitle`）：不然 "(Live 2019)"
	// 会被切成两个**半个括号**的字段（"(Live" / "2019)"）——它们既不是版本说明
	// 也不是歌名 ⇒ **被当成了歌名**。
	prepared := prepareTitle(raw)
	groups := scanGroups(prepared)
	plain := stripSpans(prepared)

	// ⚠️ **方括号里那一段**（"【周杰伦】" / "【晴天】"）先留着——它到底是歌手
	// 还是歌名，要靠**它前后**有没有别的段来定（见 `candidatesOf`）。
	bracketExtras := bracketExtraOf(groups)

	allHints := append(append([]string{}, hints...), bracketExtras...)
	name, alt := candidatesOf(plain, allHints)

	// ⚠️ **整条标题就是那些括号**（"【晴天】"）：括号**外面**一个字都没有 ⇒
	// 那一段**就是**歌名（而"【周杰伦】"那一段是歌手 ⇒ 认不出，退出）。
	if strings.TrimSpace(outsideOf(prepared, groups)) == "" {
		name = bracketOnlySong(groups)
	}

	// ⚠️ **书名号 / 引号那一档最优先**：中文歌名最常见的写法是「歌手《晴天》」
	// 而里面那一段**就是歌名**（而方括号里装的是歌手）。
	if n := namedSongOf(raw); n != "" {
		name = n
	}

	if name == "" {
		// ⚠️ **认不出歌名，但方括号里那些内容照样给出去**（"【周杰伦】"那一
		// 档）：那一段是**有用**的（副标题上写着谁唱的），**内容不许丢**。
		return Ident{Extra: joinExtra(bracketExtras, ""), Live: live}
	}

	return Ident{
		Name:  name,
		OK:    true,
		Extra: joinExtra(bracketExtras, name),
		Live:  live,
		Alts:  altList(alt),
	}
}

// altList 把「那一段备选」包成一小片（空串 ⇒ 空片）。
func altList(alt string) []string {
	if strings.TrimSpace(alt) == "" {
		return nil
	}
	return []string{alt}
}

// bracketOnlySong 是「整条标题就只有那些括号」那一档的歌名。
//
// ⚠️ **括号里那一段是歌手的时候认不出**（"【周杰伦】"）：那一段不是歌名，而
// 标题里没有别的了 ⇒ 退回原标题。
func bracketOnlySong(groups []bracketGroup) string {
	for _, g := range groups {
		inner := strings.TrimSpace(g.inner)
		if inner == "" {
			continue
		}
		if isNoteWord(inner) || IsRoleWord(inner) {
			continue
		}
		if IsArtistName(inner) {
			continue
		}
		return inner
	}
	return ""
}

// Aliases 是这一条标题的**所有**别名写法（给「匹配」那一层收进别名表用）。
//
// ⚠️ **它和 `Identify` 走的是同一份判据**（同一个 `candidatesOf`）——两份口
// 径迟早分叉，而分叉出来的那种差异极难查（一个界面上筛出三条，另一个筛出五
// 条，而两边看着都「对」）。
//
// ⚠️ **署名那一段不进表**（理由见 `candidatesOf`）：提到歌手不等于提到这
// 首歌。
//
// # 🔴 **认不出歌名的时候，原标题**仍然**进表**（票 #706 的回归修复）
//
// ⚠️ **上一版是「认不出 ⇒ 空表」**，而那一版的代价被算成了「界面那一枚开关
// 亮着但筛不出东西」——**实际代价大得多**：表是空的 ⇒ **正文里原样提到标题的
// 那些评论一条都命不中**（用户抄了整个标题进评论，那一条本该命中）。
//
// ⇒ 而「认不出 ⇒ 筛不了」这个事实**并不需要靠空表说**：`songname.Table.Empty`
// 判的是「**除原标题外还有没有认出来的歌名**」——所以：
//
//	表里**有**原标题（用户抄了标题的那一条命得中）；
//	`Empty` **仍然是 true**（筛不了 ⇒ 界面那一枚开关按灰 ⇒ 界面把话说清楚）。
//
// ⇒ **「表里有没有东西」和「有没有认出歌名」是两件事**，这一层管前者，
// `songname` 那一层管后者。
//
// # ⚠️ **本质歧义那一档：两个都进表**
//
// "白月光 - 张碧晨" 那种两边都像艺人名的写法（`Ident.Alts`）⇒ 显示层只显示
// 一个，而**匹配层两个都收**——不然用户在评论里写哪一段都命不中。
func Aliases(raw string, hints ...string) []string {
	var out []string
	for _, s := range namedSpans(raw) {
		if IsPromoOnly(s) || isNoteWord(s) || IsRoleWord(s) {
			continue
		}
		out = append(out, s)
	}
	id := Identify(raw, hints...)
	if id.OK {
		out = append(out, id.Name)
		// ⚠️ **本质歧义那一档**：另一段也进表（理由见函数注释）。
		out = append(out, id.Alts...)
	}
	// ⚠️ **原标题**进表——**认得出认不出都进**（理由见上面那段）：用户会把整个
	// 标题抄进评论里，那一条要命得中。
	if strings.TrimSpace(raw) != "" {
		out = append(out, strings.TrimSpace(raw))
	}
	return dedupe(out)
}

// candidatesOf 从「去掉各种括号之后剩下的那一段」里取出**歌名**。
//
// # 分隔符的约定（**结构**，不是词边界）
//
//	破折号「艺人 - 歌名」     歌名在**后**那一段（B 站文件名和下载工具都是这
//	                        个约定）
//	竖线「歌名 | 艺人 | 说明」  歌名在**前**那一段（B 站上「起风了 | 吴青峰 |
//	                        官方MV」这种写法很多）
//	冒号「歌名：说明」         说明那一族剔掉，**剩下最后一段**是歌名
//
// ⚠️ **哪一边是歌名**：两边都没线索时是**本质歧义**（"于文文 - 体面" 和 "体
// 面 - 于文文" 长得一样）⇒ 按上面那个约定走；而**认得出歌手**的时候不是歧
// 义——那一段是署名（词典在 `artists.go`）⇒ 另一段才是歌名。
//
// # 返回两个值：歌名，和「**本质歧义**那一档的备选」
//
// ⚠️ **第二个值是匹配层用的**（见 `Ident.Alts`）：「两边都像艺人名、而词典认不
// 出谁是谁」的时候（"白月光 - 张碧晨"），显示层只显示一个，而**匹配层两个都
// 收**——不然用户在评论里写哪一段都命不中。
func candidatesOf(plain string, hints []string) (name string, alt string) {
	segs := splitSegments(plain)

	// 🔴 **每一段先各自算出候选**（段内是空白那一档的语义：剔掉说明 / 宣传词
	// / 角色词 / 署名那些字段）。
	var cands []segment
	for _, sg := range segs {
		c := segmentCandidate(sg.text, hints)
		if c == "" {
			continue
		}
		cands = append(cands, segment{text: c, sep: sg.sep})
	}
	if len(cands) == 0 {
		return "", ""
	}
	if len(cands) == 1 {
		return cands[0].text, ""
	}
	// ⚠️ **竖线那一档取**前**那一段，破折号那一档取**后**那一段**。
	//
	// 🔴 **判据是「那些候选中间那个分隔符」**：竖线那一档 B 站的写法是
	// 「歌名 | 艺人 | 说明」（歌名在前），而破折号那一档是「艺人 - 歌名」（歌名
	// 在后）。
	//
	// ⚠️ **两者都有的时候破折号优先**（它是那个「艺人 - 歌名」的通行约定）。
	if hasSepBetween(cands, dashSeps) {
		return dashSongName(cands)
	}
	if hasSepBetween(cands, pipeSeps) {
		return pipeSongName(cands)
	}
	// ⚠️ **只剩冒号那一档**：说明那一族剔掉之后**剩下最后一段**是歌名。
	return cands[len(cands)-1].text, ""
}

// dashSongName 是**破折号**那一档（「艺人 - 歌名」）该取哪一段。
//
// # ⚠️ **这一档不能只靠「后一段是歌名」那个约定**
//
// 那个约定是**在两边都认不出署名的时候**用的（理由见 `candidatesOf` 的注释）——
// 而「认不出」有两种：
//
//	① 那一段**真的是**歌名（"体面 - 于文文"那种写法）；
//	② 那一段**是签名**，只是词典没收它（"起风了 - 吴青峰"）。
//
// 🔴 **② 认错了，界面上就把歌手的名字当歌名显示**——而用户看到的是「这一首叫
// 『吴青峰』」。那是**界面说谎**。
//
// ⇒ 所以这一档多问一句：**后一段像不像艺人名**（`nameshape.go` 那一份**形状**
// 判据——词典收不全，而形状收得全）。
//
// # 三种结果
//
//	后一段**不像**艺人名 ⇒ 按约定取**后**那一段（"体面 - 于文文"）；
//	后一段像、前一段不像     ⇒ 约定**不可信** ⇒ 取**前**那一段（"起风了 - 吴青峰"
//	                        ⇒「起风了」）；
//	**两边都像**             ⇒ **本质歧义**（见下面）。
//
// # 🔴 **「两边都像」不是「认不出」**（票 #706 的回归）
//
// 上一版在这里**直接返回空串**——而那一次的代价被算成了「显示得更脏」。
// 🔴 **实际代价大得多**：认不出 ⇒ `Aliases` 不放原标题 ⇒ **别名表是空的** ⇒
// **评论一条都筛不出来**（比基线更差）。
//
// ⇒ 所以这里**不认不出**：按通行约定（「艺人 - 歌名」）取**后**那一段，而**另一段
// 一起当备选**给匹配层（`Ident.Alts`）——「白月光 - 张碧晨」显示「白月光」，
// 而匹配层「白月光」和「张碧晨 - 白月光」两个都收。
//
// ⚠️ **为什么显示层取后一段**：B 站文件名和下载工具都按「艺人 - 歌名」写，而
// 「白月光 - 张碧晨」按那个约定读是「艺人叫白月光、歌叫张碧晨」——那显然不
// 对。⇒ **宁可取错也不说谎**（说谎=把歌手名当歌名），而**匹配层两个都收**兜住
// 另一边的写法。
func dashSongName(cands []segment) (string, string) {
	last := cands[len(cands)-1]
	// ⚠️ **只有「最后一段本身就是被破折号分出来的」才问那一句**：中间那一段
	// 和它之间可能是**冒号**（"A - B : C"那一档），而那一档的约定是「取最后一段」。
	if !isDashSep(last.sep) || len(cands) < 2 {
		return last.text, ""
	}
	prev := cands[len(cands)-2]
	if !looksLikeArtistName(last.text, prev.text) {
		return last.text, ""
	}
	if looksLikeArtistName(prev.text, last.text) {
		// ⚠️ **两边都像 ⇒ 本质歧义**：按约定取后一段，而前一段当备选
		// （理由见函数注释）。
		return last.text, prev.text
	}
	return prev.text, ""
}

// isDashSep 报这个分隔符是不是破折号那一族。
func isDashSep(s string) bool {
	for _, sp := range dashSeps {
		if s == sp {
			return true
		}
	}
	return false
}

// pipeSongName 是**竖线**那一档（「歌名 | 艺人 | 说明」）该取哪一段。
//
// # ⚠️ **这一档和破折号那一档一样要多问一句**
//
// 竖线的通行约定是「**歌名在前**」（"起风了 | 吴青峰 | 官方MV"）——但那也是
// **在两边都认不出署名的时候**的约定。
//
// 🔴 **而「吴青峰 | 起风了」那种写法在 B 站上也有**：按约定取前一段 ⇒ 界面上
// 把歌手的名字当歌名显示（**界面说谎**）。
//
// ⇒ 所以这一档同样问一句「**前一段像不像艺人名**」，而回答和破折号那一档相反：
//
//	前一段**不像**艺人名 ⇒ 按约定取**前**那一段；
//	前一段像、后一段不像     ⇒ 约定**不可信** ⇒ 取**后**那一段；
//	**两边都像**             ⇒ **本质歧义**（理由同 `dashSongName`）。
//
// ⚠️ **「起风了 | 吴青峰 | 官方MV」那一档不受影响**：「官方MV」是宣传词，剔掉
// 之后只剩两段，而前一段「起风了」**不像**艺人名 ⇒ 取前一段（和约定一致）。
func pipeSongName(cands []segment) (string, string) {
	// ⚠️ **竖线在第一段和第二段之间**：而 `sep` 记的是「**前面**那个分隔符」
	// （见 `segment` 的注释）——所以竖线是**第二段**的 `sep`。
	if len(cands) < 2 || !isPipeSep(cands[1].sep) {
		// ⚠️ **不是竖线那一档**（`hasSepBetween` 说有竖线在**后面**那几段之间）：
		// 按约定取前一段。
		return cands[0].text, ""
	}
	first, next := cands[0], cands[1]
	if !looksLikeArtistName(first.text, next.text) {
		return first.text, ""
	}
	if looksLikeArtistName(next.text, first.text) {
		// ⚠️ **两边都像 ⇒ 本质歧义**：按约定取前一段，而后一段当备选
		// （理由见 `dashSongName`）。
		return first.text, next.text
	}
	return next.text, ""
}

// hasSepBetween 报那些候选中间有没有 seps 里某一个分隔符。
func hasSepBetween(cands []segment, seps []string) bool {
	for i := 1; i < len(cands); i++ {
		for _, sp := range seps {
			if cands[i].sep == sp {
				return true
			}
		}
	}
	return false
}

// segmentCandidate 算**一段**里的歌名候选。
//
// ⚠️ **按空格切字段，剔掉说明 / 宣传词 / 角色词 / 署名那些字段**：
//
//	「官方MV 晴天」       ⇒ 剔掉「官方MV」⇒「晴天」
//	「歌手 晴天」         ⇒ 剔掉「歌手」⇒「晴天」
//	「周杰伦 晴天」       ⇒ 剔掉「周杰伦」（它是署名）⇒「晴天」
//	「Let It Go」         ⇒ ⚠️ **全英文的时候空格是词的一部分** ⇒ 整段是歌名
//	                        （拆成 "Go" 之后，「good」那种评论也会命中）
//
// 🔴 **一个字段都没剔掉的时候整段就是歌名**："歌 3"那种标题（歌名里带个空格）
// 不该被切成「3」——**只在真的剔掉了什么的时候**才取最后那一段。
func segmentCandidate(s string, hints []string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	var kept []string
	dropped := false
	featureNext := false
	for _, f := range fields {
		if featureNext {
			featureNext = false
			// ⚠️ **feat. 后面那一段是合作歌手** ⇒ 它是署名，不是歌名。
			dropped = true
			continue
		}
		if IsFeatureLead(f) {
			featureNext = true
			dropped = true
			continue
		}
		if isNoteWord(f) {
			dropped = true
			continue
		}
		if IsRoleWord(f) {
			// ⚠️ **角色词不是歌名**（"歌手《晴天》"里那个「歌手」）。
			dropped = true
			continue
		}
		if isSectionWord(f) {
			// ⚠️ **段落标记不是歌名**（"最后一段"那一族）：它说的是**视频的哪
			// 一段**，不是歌名。
			dropped = true
			continue
		}
		if isArtistLike(f, hints) {
			// ⚠️ **署名不是歌名**（"周杰伦 晴天"那一档）。
			dropped = true
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == 0 {
		return ""
	}
	// 🔴 **一个都没剔 ⇒ 整段都是歌名**（理由见函数注释）。
	if !dropped {
		return strings.Join(kept, " ")
	}
	if !anyHan(kept) {
		return strings.Join(kept, " ")
	}
	// ⚠️ **中文标题里的空格是分隔符**（「官方MV 晴天」「歌手 歌名」）⇒ 歌名是
	// **后**那一段。
	return kept[len(kept)-1]
}

// segment 是「按分隔符切出来的一段」。
type segment struct {
	// text 是这一段的内容。
	text string
	// sep 是这一段**前面**那个分隔符（第一段是空串）。
	sep string
}

// splitSegments 把一段标题按**分隔符**切成几段。
//
// ⚠️ **破折号 / 竖线在外层，冒号在内层**：「歌手-歌名：说明」那一档要**先切
// 外层的**，不然只认出「歌名：说明」（那一段就不是歌名了）。
func splitSegments(s string) []segment {
	var out []segment
	for _, seg := range splitOn(s, dashSeps, pipeSeps) {
		for i, p := range splitOn(seg.text, colonSeps) {
			sep := seg.sep
			if i > 0 {
				// ⚠️ **组内那些段前面是冒号**：冒号那一档的约定是「取最后一段」
				// ——而 `hasSepBetween` 判的就是「中间那个分隔符」。
				sep = colonSeps[0]
			}
			out = append(out, segment{text: p.text, sep: sep})
		}
	}
	return out
}

// splitOn 按分隔符把 s 切成几段（**切所有出现的地方**）。
//
// ⚠️ **长的分隔符优先**（" - " 比 "-" 长）：不这样的话「歌手 - 歌名」会切成
// 「歌手」、「」「歌名」三段，而中间那段是空的。
//
// ⚠️ **全是英文的时候不切**（"Rock-N-Roll"、"Let It Go: Official"）：那些连
// 字符和冒号是**名字的一部分**，拆开之后 "Roll" / "Official" 会变成两个候
// 选（而正文里一个 "roll" 满屏都是）。
func splitOn(s string, seps ...[]string) []segment {
	all := make([]string, 0, len(seps))
	for _, group := range seps {
		all = append(all, group...)
	}
	var parts []segment
	rest := s
	sep := ""
	for {
		idx, found := earliestSep(rest, all)
		if idx < 0 {
			break
		}
		parts = append(parts, segment{text: rest[:idx], sep: sep})
		rest = rest[idx+len(found):]
		sep = found
	}
	parts = append(parts, segment{text: rest, sep: sep})
	// ⚠️ **切完了再判要不要用这个切法**：没有汉字的那几段是英文标题。
	if !anyHanSegments(parts) {
		return []segment{{text: s}}
	}
	return parts
}

// anyHanSegments 报这几段里有没有汉字。
func anyHanSegments(segments []segment) bool {
	for _, s := range segments {
		if hasHan(s.text) {
			return true
		}
	}
	return false
}

// earliestSep 找出 s 里**最靠前**的那个分隔符（同一个位置**长的优先**）。
func earliestSep(s string, seps []string) (int, string) {
	best := -1
	bestSep := ""
	for _, sp := range seps {
		if sp == "" {
			continue
		}
		i := strings.Index(s, sp)
		if i < 0 {
			continue
		}
		if best < 0 || i < best || (i == best && len(sp) > len(bestSep)) {
			best, bestSep = i, sp
		}
	}
	return best, bestSep
}

// isPipeSep 报这个分隔符是不是竖线那一族。
func isPipeSep(s string) bool {
	for _, p := range pipeSeps {
		if s == p {
			return true
		}
	}
	return false
}

// isArtistLike 报这一段是不是「歌手 / 署名」那一段。
//
// ⚠️ **两个凭据**：词典（`artists.go`）和**调用方给的已知艺人名**（`hints`）。
func isArtistLike(s string, hints []string) bool {
	t := trimNotePunctuation(s)
	if t == "" {
		return false
	}
	if IsArtistName(t) {
		return true
	}
	for _, h := range hints {
		if strings.TrimSpace(h) == "" {
			continue
		}
		if strings.EqualFold(t, trimNotePunctuation(h)) {
			return true
		}
	}
	return false
}

// namedSongOf 认出「明确装歌名的那一段」（书名号 / 引号）。
//
// ⚠️ **书名号那一档最明确**：「歌手《晴天》」里那一段就是歌名（而方括号里
// 装的是歌手）。
//
// ⚠️ **引号那一档**（"“Let It Go”"）：引号里装的是**作品名**，歌名和它是一
// 回事。
func namedSongOf(raw string) string {
	for _, span := range namedSpans(raw) {
		if isNoteWord(span) {
			continue
		}
		if IsRoleWord(span) {
			continue
		}
		return span
	}
	return ""
}

// namedSpans 是那些「明确装歌名」的括号里的内容（书名号 / 引号）。
//
// ⚠️ **只认这些**：书名号（`《》`「」『』）和引号（`“”` `‘’` `"`）——中文歌
// 名最常这么摆，而用户在评论里写的也是里面那一段。
//
// ⚠️ **方括号不算**：它装的是歌手（"【周杰伦】"），**也可能是**歌名
// （"【晴天】"）——那一档由 `candidatesOf` 那一档定（靠**它前后**有没有别的
// 段）。
func namedSpans(s string) []string {
	var out []string
	for _, pair := range quotePairs {
		out = append(out, spansOf(s, pair.open, pair.close)...)
	}
	return out
}

// spansOf 取出 open/close 之间那些段。
//
// ⚠️ **open 与 close 相同的那一档**（`"`）找的是**下一个**引号。
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

// bracketExtraOf 是那些方括号里**不是歌名、也不是噪声**的内容（歌手 / 专辑名）。
//
// ⚠️ **只有方括号算**（`【】` `[ ]`）：圆括号里装的是版本说明（"(Live 2019)"），
// 而书名号 / 引号里那一段是**歌名**（见 `namedSongOf`）。
func bracketExtraOf(groups []bracketGroup) []string {
	var out []string
	for _, g := range groups {
		if g.pair.kind != otherBrackets {
			continue
		}
		inner := strings.TrimSpace(g.inner)
		if inner == "" {
			continue
		}
		// ⚠️ **方括号里装的是版本说明**（"【Live】"）要剔掉。
		if isNoteWord(inner) || IsRoleWord(inner) {
			continue
		}
		out = append(out, inner)
	}
	return out
}

// joinExtra 把几段拼成 `Extra`（多段用 " · " 连）。
func joinExtra(parts []string, name string) string {
	var kept []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == name {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, " · ")
}

// dedupe 去重（保住次序）。
func dedupe(items []string) []string {
	var out []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		dup := false
		for _, old := range out {
			if old == it {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, it)
		}
	}
	return out
}
