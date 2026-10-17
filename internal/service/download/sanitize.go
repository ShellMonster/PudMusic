// 文件名净化：B 站给的歌名不能直接当文件名。
//
// B 站歌名有三种东西会砸到落盘上，每一种都是**错了不报错**的那一类：
//
//  1. **高亮标签**。搜索接口返回的 title 里夹着 `<em class="keyword">`，
//     落盘就是一个带尖括号和引号的名字。
//  2. **HTML 实体**。`&amp;`、`&#21487;` 这种，同一首歌换个接口就换了写法。
//  3. **非法字符**。歌名里最常见的是 `:`（`乐队名: 曲名`），而 `:` 在
//     macOS 的 Finder 里显示成 `/`、在 Windows 上根本建不出文件。
//
// 三步的**顺序不能反**，反了会吃掉歌名的内容：
//
//	剥标签 → 解实体 → 替换非法字符 → 截断
//
// 先解实体再剥标签的话，`&lt;em&gt;` 会被解成真的 `<em>` 然后被当成标签
// 删掉——歌名里一个正经的尖括号就没了。而先剥标签时，B 站那些**真的是标签**
// 的 `<em class="keyword">` 还在（它们是 JSON 里的字面尖括号，不是实体），
// 能被正确剥掉；歌名里**转义过**的尖括号解出来之后落在第三步，被当非法字符
// 换成下划线，那才是对的——用户写 `3<5` 不该被当成一个标签删掉。

package download

import (
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxNameBytes 是文件名（含末尾那一段）的字节上限。
//
// ⚠️ **是字节，不是字符**。APFS 上一个文件名按 UTF-8 字节算，255 字节大约
// 是 85 个汉字。用 `[]rune` 截到 255 个字符的话，一个中文歌名就是 765 字节，
// `os.Create` 直接报 `file name too long`——而且是在下到一半的时候才报。
const maxNameBytes = 255

// tagRe 匹配一个 HTML 标签，连属性一起。
var tagRe = regexp.MustCompile(`<[^>]*>`)

// ExtOf 从取流地址里取扩展名，带点。认不出来是空串。
//
// ⚠️ 不能直接对整条地址取后缀：B 站的地址后面挂着一长串查询参数
// （`upsig`、`deadline`、`oi`…），`path.Ext` 对着整条地址会取到
// `&upsig=xxx` 这种东西。所以先 `url.Parse` 再取 path。
func ExtOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	// 只认常见的音频容器，别的（`.html`、`.php`）当没有。
	// CDN 偶尔会在路径后面挂个重写规则，那时取到的东西不是音频格式。
	switch e := strings.ToLower(path.Ext(u.Path)); e {
	case ".m4a", ".m4s", ".flac", ".mp3", ".mp4", ".aac", ".wav", ".ogg":
		return e
	}
	return ""
}

// FileName 把歌名净化成能落盘的文件名。
//
// ext 是**必须留在名字末尾的那一段**——通常是扩展名（`.flac`），但也可能是
// `名字 (1).flac` 这种「同名避让」的后缀（见 `DestFor`）。它叫 ext 只是因为
// 最常见的用法就是扩展名；**凡是拼在末尾、又要一起算进 255 字节预算的
// 东西，都该从这里走一遍**，别在外面先拼上去。
//
// 空串指的是「实在给不出名字」（歌名全是非法字符、调用方也没给后备），
// 返回的也是空串——**不编一个**。让调用方拿内容 id 去当后备名：编出来的
// 「未命名」在记录页上一眼看得出是丢了名字，而内容 id 至少能对得上是哪首。
//
// # 截断为什么不能一刀切
//
// 255 字节是**含末尾那一段**的预算，而且不能从中间把一个 UTF-8 字符劈开——
// 劈开的那半个会让整个文件名变成非法 UTF-8，HFS+ 上表现为「文件打不开」，
// 而且它**不会报错**，只是 Finder 里那个名字显示成乱码。所以先给末尾留出
// 位置，剩下的按字符边界往回退。
func FileName(title, ext string) string { return fileName(title, ext, 0) }

// fileName 是真身，`reserve` 是**除了末尾那一段之外还要预留给别人的字节数**。
//
// 谁在用 `reserve`：`destFor` 预留 `.part` 的 5 个字节。半成品和正式文件名
// 同名同目录是故意的（用户在下载目录里一眼看出「这是在下的那一首」），所以
// 它不能另起一个截短的名字，只能让正式名让出位置——否则 251 个 ASCII 字符
// 的歌名会落在 255 字符、`.part` 落在 260 字符，`os.Create` 直接报
// `file name too long`，**这一首一个字节都下不了**。
func fileName(title, ext string, reserve int) string {
	name := sanitize(title)

	// 末尾那一段自己也得夹在预算里。`.flac` 5 字节，`.m4a` 4 字节，
	// 带上同名避让的话是 `名字 (1).flac` 那种 9 字节。
	budget := maxNameBytes - len(ext) - reserve
	if budget <= 0 {
		// 末尾那一段（加上别人预留给 `.part` 的）就快把 255 吃光了
		//（不会发生，但别让它变成一个没有上界的减法——那会从这一行
		// 直接切出去，panic）。
		return ""
	}
	if len(name) > budget {
		name = truncateBytes(name, budget)
	}

	// macOS 不允许结尾是点，Windows 连空格也不行。截断和替换都可能留下这个。
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return ""
	}
	return name + ext
}

// sanitize 是净化的正文，剥标签、解实体、收空白、换非法字符。
//
// **收空白在换非法字符之前**：换非法字符那一遍把 `0x00~0x1f` 全换成下划线，
// 而换行和制表符就在那个范围里。顺序反了的话歌名里的换行会变成下划线
// （`第一行_第二行`），而它本来该变成一个空格——歌名是被 B 站存着的原文，
// 空白是排版留下的 dirt，下划线不是。
func sanitize(title string) string {
	s := tagRe.ReplaceAllString(title, "")
	// 解实体用标准库：它认识全部 HTML5 命名实体和十进制/十六进制数字实体。
	//
	// Biu 上游是手写了一张六个键的表（`electron/utils.ts:83`），而且正则
	// 匹配了 `&#123;` / `&#x7b;` 却查表查不到，**结果原样返回**——数字实体
	// 根本没被解，`&#21487;` 会变成歌名里的六个字面字符。标准库没这个问题。
	s = html.UnescapeString(s)
	s = replaceIllegal(collapseSpace(s))
	return strings.TrimSpace(s)
}

// collapseSpace 把连着的空白并成一个。
//
// 歌名首尾和中间常有 `\n` + 多个空格（多行标题、`<br>` 剥完留下的空）。
// 不收的话名字里带一个换行，用户在 Finder 里看到的歌名是断成两行的。
func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if isSpace(r) {
			if prevSpace {
				continue
			}
			b.WriteByte(' ')
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	// U+00A0（不换行空格）也是空白。B 站歌名里的 `&nbsp;` 解出来就是它，
	// 用户看不见但它在——按普通空格收掉，名字才对得上用户看到的那一版。
	return r == ' '
}

// replaceIllegal 把不能进文件名的字符换成下划线，并把连着的下划线并成一个。
//
// # 为什么取并集而不是「按平台各来一套」
//
// 取的是「所有目标平台上都不合法」的**并集**：`/ \ : * ? " < > |` 加控制字符。
// 只按当前平台来（macOS 上放过 `*`、Windows 上放过 `:`）看着更「保真」，
// 代价是下载目录一旦被同步到别的机器上就有一堆打不开的文件，而且那时候
// 已经没法补救——名字已经定死了。
//
// # 为什么换成下划线而不是删掉
//
// 删掉会把 `AC/DC` 变成 `ACDC`、`陈绮贞: 旅行的意义` 变成 `陈绮贞 旅行的意义`
// （换的是全角冒号）。用户在自己的目录里按文件名找歌，名字对不上比多个
// 下划线难受得多。
func replaceIllegal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevUnderscore := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f || illegalRune(r) {
			// 连着几个非法字符只留一个下划线，不然 `a///b` 变成 `a_____b`。
			if prevUnderscore {
				continue
			}
			b.WriteByte('_')
			prevUnderscore = true
			continue
		}
		b.WriteRune(r)
		prevUnderscore = false
	}
	return b.String()
}

// illegalRune 是文件名里绝对不能有的那批字符。
func illegalRune(r rune) bool {
	switch r {
	case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
		return true
	}
	return false
}

// truncateBytes 把 s 截到至多 n 字节，**不切开一个 UTF-8 字符**。
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	// 最多回退 3 字节就是一个 UTF-8 字符的上限（4 字节），不会越界。
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
