// Package title 把 B 站视频标题清洗成「音乐 App 该显示的歌名」。
//
// 来源是 B 站，标题里全是视频的痕迹：`【4K修复】`、`官方MV`、`[Hi-Res]`、
// `【周杰伦】`……产品是纯音乐 App（设计规范 v0.1 §0），这些不该占着歌名。
//
// # 规则（保守：只动方括号里的、和圆括号里的宣传词，不碰歌名本身）
//
//  1. `【…】` / `[…]` 里的内容：
//     - 含「宣传词」（官方 / MV / 4K / 超清 / 修复 / 完整版 / 无损 / 字幕 / 歌词版 /
//     Hi-Res / 首发 / 重制 / 双语 / 中字 / 高清 / 原创 …）→ **丢掉**；
//     - 含「现场」字样（现场 / Live / 演唱会 / 音乐会 / 巡演 …）→ 丢掉，但记下 `Live`；
//     - 其余（多半是歌手名、专辑名）→ **挪到副标题**（`Extra`）。
//  2. `(…)` / `（…）` 里**只有宣传词**的整段丢掉（`(官方MV)`、`（4K修复）`）；
//     里面有别的字的不动（`(Live)` 是现场标记：记 `Live` 并丢掉括号）。
//  3. 标题本身带「现场 / Live / 演唱会」字样也记 `Live`，但**歌名里的字不删**。
//  4. 清洗后是空的就**原样返回**——宁可显示脏标题，也不显示空白。
//
// 它只管**显示**：不改 `app.State.Title`，下载文件名、搜索、去重仍用原标题。
package title

import (
	"regexp"
	"strings"
	"sync"
)

// Cleaned 是清洗结果。
type Cleaned struct {
	// Title 是要显示的歌名。
	Title string
	// Extra 是从方括号里挪出来的非宣传内容（歌手 / 专辑名之类），多段用 " · " 连；
	// 没有就是空串。适合拼进副标题。
	Extra string
	// Live 是「这是现场版」。
	Live bool
}

var (
	// 方括号：【…】 和 […]。
	reSquare = regexp.MustCompile(`【([^】]*)】|\[([^\]]*)\]`)
	// 圆括号：(…) 和 （…）。
	reParen = regexp.MustCompile(`\(([^)]*)\)|（([^）]*)）`)
	// 清洗后头尾要去掉的分隔符。
	reTrim = regexp.MustCompile(`^[\s|丨/\-_—–·・:：]+|[\s|丨/\-_—–·・:：]+$`)
	// 多个空白合成一个。
	reSpaces = regexp.MustCompile(`\s{2,}`)

	promoWords = []string{
		"官方", "mv", "4k", "8k", "1080", "60fps", "超清", "高清", "蓝光", "修复", "完整版", "无损",
		"字幕", "歌词版", "歌词", "hi-res", "hires", "首发", "重制", "双语", "中字", "原创", "高音质",
		"音质", "动态歌词", "pv", "ost", "纯享", "官方首发", "全网首发", "最新", "收藏", "必听",
	}
	liveWords = []string{"现场", "live", "演唱会", "音乐会", "巡演", "巡回", "演出版", "舞台"}
)

// cache 记住清洗结果：列表每帧每行都要问一次，正则不该每帧跑。键是原标题，
// 满了（一万条）就整个清掉重来——够用，也不会无限长。
var (
	cacheMu sync.Mutex
	cache   = map[string]Cleaned{}
)

const cacheMax = 10000

// Clean 清洗一条标题（带缓存）。
func Clean(raw string) Cleaned {
	cacheMu.Lock()
	if c, ok := cache[raw]; ok {
		cacheMu.Unlock()
		return c
	}
	cacheMu.Unlock()
	c := clean(raw)
	cacheMu.Lock()
	if len(cache) >= cacheMax {
		clear(cache)
	}
	cache[raw] = c
	cacheMu.Unlock()
	return c
}

// clean 是真正的清洗（不带缓存）。
func clean(raw string) Cleaned {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Cleaned{Title: raw}
	}
	var out Cleaned
	var extras []string

	s = reSquare.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSpace(reSquare.FindStringSubmatch(m)[1] + reSquare.FindStringSubmatch(m)[2])
		switch {
		case hasAny(inner, liveWords):
			out.Live = true
		case hasAny(inner, promoWords):
		case inner != "":
			extras = append(extras, inner)
		}
		return " "
	})
	s = reParen.ReplaceAllStringFunc(s, func(m string) string {
		sub := reParen.FindStringSubmatch(m)
		inner := strings.TrimSpace(sub[1] + sub[2])
		switch {
		case hasAny(inner, liveWords):
			out.Live = true
			return " "
		case onlyPromo(inner):
			return " "
		}
		return m
	})
	if hasAny(s, liveWords) {
		out.Live = true
	}

	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimSpace(reTrim.ReplaceAllString(strings.TrimSpace(s), ""))
	if s == "" {
		return Cleaned{Title: raw, Live: out.Live}
	}
	out.Title = s
	out.Extra = strings.Join(extras, " · ")
	return out
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
func onlyPromo(inner string) bool {
	l := strings.ToLower(inner)
	if strings.TrimSpace(l) == "" {
		return false
	}
	for _, w := range promoWords {
		l = strings.ReplaceAll(l, w, "")
	}
	for _, r := range l {
		if !strings.ContainsRune(" ,，、/|丨+&·-_.。0123456789", r) {
			return false
		}
	}
	return true
}

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
// ⚠️ **宣传词那张表仍然只有这一处**（理由同 `IsPromoOnly`）：别处要判「这一段
// 里有没有宣传词」就走它。
//
// 🔴 **空串不算**：那是「没有这一段」。
func ContainsPromo(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return hasAny(s, promoWords)
}
