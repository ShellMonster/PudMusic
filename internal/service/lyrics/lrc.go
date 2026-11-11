// Package lyrics 是歌词这一摊：**把 LRC 文本解析成带时间的行**、歌词缓存的落盘与读取，
// 以及**按歌名去两个源（网易云 / LRCLib）搜歌词**。
//
// 三件事各自独立，任何一件都不会拖累另外两件：
//
//   - 解析（lrc.go）：一段 LRC 文本 → 带时间的行。一处网络请求都没有。
//   - 缓存（cache.go）：一首歌一条，落在盘上。**不发请求**，歌词文本是调用方给的。
//   - 搜索（search.go）：两个源各能搜、各能给一份完整歌词。**B 站字幕不在这里**
//     （那是 B 站 web player 的接口，`biu/src/components/lyrics/get-lyrics.ts`）。
//
// 所以「缓存里没有」和「搜不到」是两件不同的事，界面上也是两句话：前者是「暂无歌词」
// 加一个搜索入口，后者是搜索弹窗里的「没搜到 / 搜失败」。
//
// 交互（逐行高亮、居中滚动、上下渐隐遮罩、偏移与字号、搜索与预览两个弹窗）全在界面层，
// 逐条照 Biu 的 `src/components/lyrics/index.tsx` 与
// `src/components/lyrics-search-modal/`。
//
// ## 时间标签
//
// LRC 的时间标签是 `[mm:ss.xx]`，而**一行可以有任意多个**：
// `[00:12.00][01:30.00]副歌` 说的是「这句在 12 秒和 90 秒各唱一次」。上游把一行
// 拆成几条（`index.tsx:66-76` 的 `while (exec)`），不拆的话副歌那一段整个
// 没有高亮——表现是「唱到副歌就没反应了」。
//
// ## 元数据标签和没有时间标签的续行
//
// `[ti:…]` / `[ar:…]` / `[offset:…]` 这类标签行内没有时间，统一丢掉。
//
// 而没有时间标签、又不是元数据的行，是**上一句的续行**：有些源把一句词里的
// 换行也写进了行内，直接按换行切会把后半句丢掉。现在续行并回上一句（英文
// 断行补空格，中文直接接上），元数据和 `[Chorus]` 这类段落标记仍然丢掉。
//
// ## 署名行
//
// NetEase 的 LRC 把「作词 / 作曲 / 制作人 / 版权」也写成带时间戳的行。它们
// 不是唱出来的词，按「角色词 + 冒号」整行过滤。
//
// ⚠️ 上游那版正则带 `g` 标志又拿同一个全局对象既 `replace` 又 `exec`，靠手动归零
// `lastIndex` 才没串行。Go 的 `FindAllStringSubmatch` 不带这种状态，一行调用一次就
// 干净了，不用抄那个归零。
package lyrics

import (
	"cmp"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Line 是一条歌词：什么时候开始唱、什么时候唱完、唱的是什么。
//
// Words 只在 YRC 那一份里有值（见 yrc.go）；普通 LRC 只有整句的开始时刻，
// 所以它是 nil。End 同理：LRC 不知道这一句唱到哪儿，零值表示不知道。
type Line struct {
	At    time.Duration
	End   time.Duration
	Text  string
	Words []Word
}

// Row 是一条歌词，加上叠在它下面的那句翻译。
//
// 翻译不是每行都有（`Translation` 为空时不画），而**对不上**的那些就丢掉：
// 上游是拿时间戳做键去查的（`index.tsx:178` 的 `map.set(item.time, item.text)`），
// 原文和译文的时间戳必须一模一样才叠得上。
type Row struct {
	At          time.Duration
	End         time.Duration
	Text        string
	Translation string
	// Words 是这一句的词级时间；空表示这一份歌词没有逐字信息，界面按整句
	// 高亮（见 yrc.go 的 `SpreadWords`）。
	Words []Word
}

// timeTag 认一个时间标签。
//
// 分钟和秒都是 1~2 位、毫秒可选 1~3 位，**逐字照上游**
// （`index.tsx:29`）：`[100:00.00]` 那种三分钟的时间戳上游也不认，这里同样不认。
var timeTag = regexp.MustCompile(`\[(\d{1,2}):(\d{1,2})(?:\.(\d{1,3}))?\]`)

// Parse 把一段 LRC 解析成若干行，**按时间排好序**。
//
// 一行几个时间标签就出几行（见文件头的「时间标签」）。空串、没有字、只有元数据
// 标签的行都产不出东西，跳过。
//
// 排序是**稳定**的：同一时刻的两句保留 LRC 里的先后（上游 `toSorted` 在现代
// JS 里是稳定的，`index.tsx:80`）。这不是挑刺——同一时刻的两句谁在上谁在下是
// 原作者排的，翻过来会让高潮那句掉到后面去。
func Parse(raw string) []Line {
	if raw == "" {
		return nil
	}
	var out []Line
	// lastGroup 是**上一条有时间标签的那句**落在 out 里的下标。没有时间标签的
	// 续行要并回它（见文件头「没有时间标签的续行」）。
	var lastGroup []int
	// 归一化换行：B 站的字幕和从歌词站导出的文件两种换行都有，
	// 而 Windows 上编辑过的文件就是 \r\n 的。
	for line := range strings.SplitSeq(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(timeTag.ReplaceAllString(line, ""))
		tags := timeTag.FindAllStringSubmatch(line, -1)
		if len(tags) == 0 {
			if text == "" || isMetadataLine(line) || isSectionMarker(text) || isCreditText(text) {
				continue
			}
			if len(lastGroup) == 0 {
				continue
			}
			for _, i := range lastGroup {
				out[i].Text = joinContinuation(out[i].Text, text)
			}
			continue
		}
		if text == "" || isCreditText(text) {
			// 署名行和空行不属于任何一句，后面的续行也不该并到上一句去。
			lastGroup = nil
			continue
		}
		group := make([]int, 0, len(tags))
		for _, m := range tags {
			at, ok := timeOf(m)
			if !ok {
				continue
			}
			out = append(out, Line{At: at, Text: text})
			group = append(group, len(out)-1)
		}
		lastGroup = group
	}
	slices.SortStableFunc(out, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	return out
}

// isMetadataLine 报这一行是不是 LRC 元数据（`[ti:]`、`[ar:]`、`[offset:]` 等）。
//
// 这些行没有时间标签；在没有续行合并之前它们是被自动丢掉的。现在无标签的行
// 要并回上一句，所以元数据必须**显式**认出来，否则 `[ti:歌名]` 会挂到上一句
// 歌词后面。
var metadataLine = regexp.MustCompile(`(?i)^\[(ti|ar|al|by|offset|re|ve|length|au|encoding|tool|sign|total|lyricist|composer):`)

func isMetadataLine(line string) bool { return metadataLine.MatchString(strings.TrimSpace(line)) }

// isSectionMarker 报 `[Chorus]` / `[副歌]` 这种只有一对方括号的段落标记。
//
// 它不是歌词，也不是元数据；并进上一句会变成「上一句[Chorus]」。
func isSectionMarker(text string) bool {
	return strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") &&
		strings.Count(text, "]") == 1
}

// isCreditText 报这一行是不是署名（作词 / 作曲 / 制作人 / 版权…）。
//
// 判据是「角色词 + 冒号」：NetEase 那批 LRC 把署名也带了时间戳，原样画出来
// 就是歌一开头一整屏「作词 : 某人」。**只认前缀和冒号**，所以「作词人的话」
// 这种真正的歌词不会被误伤。
func isCreditText(text string) bool {
	t := strings.TrimSpace(text)
	for _, role := range creditRoles {
		if len(t) < len(role) || !strings.EqualFold(t[:len(role)], role) {
			continue
		}
		rest := strings.TrimSpace(t[len(role):])
		if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "：") {
			return true
		}
	}
	return false
}

// creditRoles 是署名行的角色词。长词排在前面的短词之后也没关系：判据要求角色词
// 后面紧跟冒号，所以「词」不会吃掉「词版权管理方：」。
var creditRoles = []string{
	"作词", "作曲", "编曲", "制作人", "监制", "出品", "发行", "录音", "混音", "母带",
	"和声", "和声编写", "吉他", "贝斯", "鼓", "弦乐", "钢琴", "键盘", "人声",
	"词版权管理方", "曲版权管理方", "录音作品及MV版权", "联合出品", "统筹", "企划",
	"策划", "设计", "助理", "工程", "录音室", "词", "曲",
	"Lyrics", "Music", "Composer", "Arranger", "Producer", "Mixing", "Mastering",
	"Recorded", "Vocals", "Guitar", "Bass", "Drums", "Piano", "Keyboard", "Strings",
	"OP", "SP",
}

// joinContinuation 把没有时间标签的续行并回上一句。
//
// 英文断行处补一个空格（`Hello` + `world`），中文直接接上（`前半句` +
// `后半句`）。判据只看两边是不是 ASCII 词字符：CJK 之间不需要空格，而
// `Hello,` + `world` 这种上一行以标点结尾的也要补。
func joinContinuation(prev, next string) string {
	if prev == "" {
		return next
	}
	if next == "" {
		return prev
	}
	last, _ := utf8.DecodeLastRuneInString(prev)
	first, _ := utf8.DecodeRuneInString(next)
	if !unicode.IsSpace(last) {
		// 上一行以 ASCII 词字符结尾、下一行又以 ASCII 词开头：补一个空格。
		if last < utf8.RuneSelf && isASCIIWordRune(first) {
			return prev + " " + next
		}
		// 上一行以 CJK 结尾、下一行以英文单词开头：也补空格。中文句子后面直接
		// 贴一个英文词读起来是连在一起的。数字不补（「第」+「3章」还是「第3章」）。
		if isCJK(last) && isASCIILetterRune(first) {
			return prev + " " + next
		}
	}
	return prev + next
}

func isASCIIWordRune(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

func isASCIILetterRune(r rune) bool {
	return r < utf8.RuneSelf && unicode.IsLetter(r)
}

// timeOf 从一个时间标签的三段里算出时间。
//
// 毫秒**不足三位要补零**：`.5` 是 500 毫秒不是 5 毫秒（上游 `index.tsx:69` 的
// `padEnd(3, "0")`）。补错的话每一句都差一两个数量级，表现是「歌词整体早了
// 几十秒」，而歌词本身看着完全正常。
//
// 秒数不夹界（`[00:99.00]` 就是 99 秒）：上游也不夹，那一行照样出现在歌词里。
// 自己在这里加一道夹界，等于「我们比上游多丢一句词」，而丢的那一句用户根本
// 不知道为什么没了。
func timeOf(m []string) (time.Duration, bool) {
	minute, ok := atoi(m[1])
	if !ok {
		return 0, false
	}
	second, ok := atoi(m[2])
	if !ok {
		return 0, false
	}
	// m[3] 可能整个是空的（标签写成 `[01:30]`），那时就是 0 毫秒。
	milli := 0
	if m[3] != "" {
		if milli, ok = atoi(m[3] + strings.Repeat("0", 3-len(m[3]))); !ok {
			return 0, false
		}
	}
	return time.Duration(minute)*time.Minute +
		time.Duration(min(second))*time.Second +
		time.Duration(milli)*time.Millisecond, true
}

// atoi 解析一段十进制数字。带符号、超长、非数字都返回 false。
//
// ⚠️ 不用 `strconv.Atoi`：正则保证这里只有数字，所以出错只在**理论**上；而理论上的
// 错一旦发生就是「整个歌词解析成零行」，界面上表现成「这首歌没歌词」。
func atoi(s string) (int, bool) {
	if s == "" || len(s) > 6 {
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
	return n, true
}

// Rows 把原文和译文合成界面真正要画的一行行。
//
// 译文按**时间戳**配对（见 Row.Translation）。原文为空时返回 nil——没有原文就没有
// 行，译文自己飘着没有意义。
func Rows(lrc, translated string) []Row {
	return rowsOfLines(Parse(lrc), Parse(translated), false)
}

// RowsOf 把原文（YRC 优先、LRC 兜底）和译文合成界面真正要画的一行行。
//
// YRC 解析不出东西时回退 LRC：源那边给了一个坏字段不该让整首歌没词。
func RowsOf(lrc, translated, yrc string) []Row {
	if strings.TrimSpace(yrc) != "" {
		if lines := ParseYRC(yrc); len(lines) > 0 {
			return rowsOfLines(lines, Parse(translated), true)
		}
	}
	return Rows(lrc, translated)
}

// rowsOfLines 是 `Rows` 和 `RowsOf` 共用的装配。
//
// 普通 LRC 的译文按**时间戳精确**配对（上游 `map.set(item.time, …)`）；YRC
// 那份译文仍是 LRC，而逐字行和译文的标签未必一个数字都不差——所以 YRC 这一
// 支允许 ±1.5 秒内最近的译文（`yrcTranslationTolerance`）。精确的那一条仍然
// 先找，改不了已有行为。
func rowsOfLines(base, trans []Line, spread bool) []Row {
	if len(base) == 0 {
		return nil
	}
	// 后来的同一时刻覆盖先来的，和上游 `map.set` 一样。
	byTime := make(map[time.Duration]string, len(trans))
	for _, l := range trans {
		byTime[l.At] = l.Text
	}

	out := make([]Row, len(base))
	for i, l := range base {
		words := l.Words
		if spread && len(words) > 0 {
			words = SpreadWords(words)
		}
		tr := byTime[l.At]
		if tr == "" && spread && len(trans) > 0 {
			tr = nearestText(trans, l.At)
		}
		out[i] = Row{At: l.At, End: l.End, Text: l.Text, Translation: tr, Words: words}
	}
	return out
}

// yrcTranslationTolerance 是 YRC 行和译文行允许差多少。
//
// 译文是整句的，逐字行的开始和它一般只差几十毫秒；1.5 秒足够盖住前奏里
// 那些「(前奏)」和第一句的错位，又不会把隔壁那句的翻译抢过来。
const yrcTranslationTolerance = 1500 * time.Millisecond

// nearestText 找离 at 最近、且在容差内的译文。同一距离取早的那一条。
func nearestText(trans []Line, at time.Duration) string {
	i := sort.Search(len(trans), func(i int) bool { return trans[i].At >= at })
	best := ""
	bestD := yrcTranslationTolerance + 1
	try := func(j int) {
		if j < 0 || j >= len(trans) {
			return
		}
		d := trans[j].At - at
		if d < 0 {
			d = -d
		}
		if d <= yrcTranslationTolerance && d < bestD {
			best, bestD = trans[j].Text, d
		}
	}
	try(i - 1)
	try(i)
	try(i + 1)
	return best
}

// ActiveAt 报唱到 at 的时候该高亮哪一行。
//
// 照上游（`index.tsx:181-187`）：从最后一行倒着找第一句「已经到点了」的。
// 一句都没到就当**第一句**——歌还没唱到第一句词时高亮它，那就是「准备唱」的那
// 一句，而不是把整个歌词区空着。一行都没有时是 -1。
func ActiveAt(rows []Row, at time.Duration) int {
	if len(rows) == 0 {
		return -1
	}
	i := sort.Search(len(rows), func(i int) bool { return rows[i].At > at }) - 1
	if i < 0 {
		return 0
	}
	return i
}
