// Package lyrics 是歌词这一摊：**把 LRC 文本解析成带时间的行**，以及歌词缓存的落盘
// 与读取。
//
// 这一票只做解析和缓存。歌词**从哪儿来**（B 站字幕 / 网易云 / LRCLib）是 #21 的活儿，
// 所以本包**一处网络请求都没有**——`Cache.Save` 里的歌词文本是调用方给进来的，调用方
// 拿不到时这一层就一直返回「没有歌词」，界面上照实说「暂无歌词」。
//
// 交互（逐行高亮、居中滚动、上下渐隐遮罩、偏移与字号）全在界面层，见
// `internal/ui/lyrics.go`，逐条照 Biu 的 `src/components/lyrics/index.tsx`。
//
// ## 时间标签
//
// LRC 的时间标签是 `[mm:ss.xx]`，而**一行可以有任意多个**：
// `[00:12.00][01:30.00]副歌` 说的是「这句在 12 秒和 90 秒各唱一次」。上游把一行
// 拆成几条（`index.tsx:66-76` 的 `while (exec)`），不拆的话副歌那一段整个
// 没有高亮——表现是「唱到副歌就没反应了」。
//
// ## 元数据标签
//
// `[ti:…]` / `[ar:…]` / `[offset:…]` 这类标签**行内没有时间**，而一行至少要有一个
// 时间标签才产得出东西，所以它们自动被丢掉（上游也是这个效果：那一行 push 不出任何
// 东西，见 `index.tsx:72` 那个空 while）。
//
// ⚠️ 上游那版正则带 `g` 标志又拿同一个全局对象既 `replace` 又 `exec`，靠手动归零
// `lastIndex` 才没串行。Go 的 `FindAllStringSubmatch` 不带这种状态，一行调用一次就
// 干净了，不用抄那个归零。
package lyrics

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Line 是一条歌词：什么时候开始唱、唱的是什么。
type Line struct {
	At   time.Duration
	Text string
}

// Row 是一条歌词，加上叠在它下面的那句翻译。
//
// 翻译不是每行都有（`Translation` 为空时不画），而**对不上**的那些就丢掉：
// 上游是拿时间戳做键去查的（`index.tsx:178` 的 `map.set(item.time, item.text)`），
// 原文和译文的时间戳必须一模一样才叠得上。
type Row struct {
	At          time.Duration
	Text        string
	Translation string
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
	// 归一化换行：B 站的字幕和从歌词站导出的文件两种换行都有，
	// 而 Windows 上编辑过的文件就是 \r\n 的。
	for line := range strings.SplitSeq(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(timeTag.ReplaceAllString(line, ""))
		if text == "" {
			continue
		}
		for _, m := range timeTag.FindAllStringSubmatch(line, -1) {
			at, ok := timeOf(m)
			if !ok {
				continue
			}
			out = append(out, Line{At: at, Text: text})
		}
	}
	slices.SortStableFunc(out, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	return out
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
	base := Parse(lrc)
	if len(base) == 0 {
		return nil
	}
	// 后来的同一时刻覆盖先来的，和上游 `map.set` 一样。
	byTime := make(map[time.Duration]string, len(base))
	for _, l := range Parse(translated) {
		byTime[l.At] = l.Text
	}

	out := make([]Row, len(base))
	for i, l := range base {
		out[i] = Row{At: l.At, Text: l.Text, Translation: byTime[l.At]}
	}
	return out
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
	for i := len(rows) - 1; i >= 0; i-- {
		if at >= rows[i].At {
			return i
		}
	}
	return 0
}
