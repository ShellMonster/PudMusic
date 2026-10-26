// B 站接口在文案里做的 HTML 转义，在这一层解掉。
//
// B 站的 web 接口把标题、UP 名、收藏夹名这类文案**转义过再回**：「A & B」
// 回的是「A &amp; B」。网页那边渲染 HTML 的时候顺手解掉了，我们这边是原生
// 文本，不解的话用户看到的就是「林俊杰&amp;李健合唱…」这样的串。
//
// 搜索接口还会在关键词命中处包 <em> 标签——那一半归 `stripHTML`
// （search.go），两件事各管各的：先剥标签、再解实体（search.go 的映射点
// 就是这么摆的）。
package bili

import (
	"html"
	"strings"
)

// fullwidthEntities 是「分号被换成全角」的那几种转义串。html.UnescapeString
// 只认 ASCII 的「;」，而用户手里真实出现过的坏标题里就有「&amp；」这一种
// ——解之前先把这几个常见实体的分号摆正。这几个串不会是正常文案，摆正是
// 安全的；全角的「；」本身在中文标题里合法，不在实体位置就一个都不动。
var fullwidthEntities = []struct{ bad, good string }{
	{"&amp；", "&amp;"},
	{"&quot；", "&quot;"},
	{"&#39；", "&#39;"},
	{"&gt；", "&gt;"},
	{"&lt；", "&lt;"},
}

// decodeEntities 解掉文案里的 HTML 实体（&amp; / &quot; / &#39; / 数字实体……）。
// 没有「&」的串原样返回——标题绝大多数不含转义，这一条让热路径白拿。
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	for _, p := range fullwidthEntities {
		s = strings.ReplaceAll(s, p.bad, p.good)
	}
	return html.UnescapeString(s)
}
