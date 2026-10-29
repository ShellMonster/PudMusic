// B 站那一份弹幕：来自 `/x/v1/dm/list.so`。
//
// # 这一条路回的不是 JSON
//
// 它回的是**原始 deflate 的 XML**（实测 2026-10-27）：
//
//	Content-Type: text/xml
//	Content-Encoding: deflate
//	body: **裸 deflate**（**不是** zlib 的 `78 xx`，也不是 gzip 的 `1f 8b`）
//
// ⇒ `Client.Do` 那条路解不了（它把响应体当 JSON 解），所以另开
// `Client.DoRaw`（见 `client.go`）；解压用 `compress/flate`。
//
// 🔴 **它是原始 deflate，不是 zlib**——这是**静默**的一档坑：拿
// `compress/zlib` 去解会报一句看不懂的错（`flate: corrupt input before
// offset 5`），而症状是「一条弹幕都没有」。
//
// ⚠️ **别拿「头两个字节」去认它**：deflate 的首字节是那个变长整数的一部分，
// **每份响应都不一样**（实测四份：`ac fd` / `ec fd` / `94 bd` / `bc fd`）。
// 认它的办法只有两个：**头是 `78 xx` 就是 zlib**（那就不是它），
// 或者**直接拿 `compress/flate` 解**——解开了就是它。
//
// # 两个门其实是同一个门
//
// - `/x/v1/dm/list.so?oid={cid}`（**用的这个**）
// - `comment.bilibili.com/{cid}.xml`
//
// 实测同一个 cid：URL 不同、字节数相同、内容相同 ⇒ 后者是前者的另一个门，
// **不是第二条数据路**。这里只走第一个。
//
// # 鉴权
//
// 实测**不需要登录态**，只要 `buvid3`（白拿）加一个 `Referer`。带不带
// cookie、要不要 WBI 签名，实测**都不影响**能不能拿到数据。
//
// ⚠️ **它会被风控拦**：连续打同一类请求之后 B 站会回 **HTTP 412**（一页
// HTML）。那不是「这条视频没有弹幕」，是「现在问不出来」。
//
// # 条数上限
//
// B 站**只给一部分**：实测三个视频拿到 600 / 1200 / 3600 条，而 B 站自己
// 统计的总条数是几万到十几万（上限跟着响应里的 `<maxlimit>` 走）。
// 好消息是**给的这部分铺满整条时间轴**，不是某个时间段的切片。
package bili

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// pathDanmaku 是弹幕接口。
const pathDanmaku = "/x/v1/dm/list.so"

// Danmaku 是一条弹幕。
type Danmaku struct {
	// TimeMS 是**视频时间**（毫秒）。
	//
	// ⚠️ 它是视频的时间轴，而我们放的是这个视频的**音轨**——两者 1:1 对齐
	// （弹幕的时间基准就是视频时间，没有额外 offset）。
	TimeMS int64
	// Mode 是弹幕模式：1 滚动 / 4 底部 / 5 顶部。
	//
	// ⚠️ 实测还有 6（高级）和 7（代码）——**没查到它们是什么意思**，但
	// 它们确实会出现（见 fixture 里的那两条）。所以这一档是「原样带出来」，
	// 不是「只认三个」。
	Mode int
	// FontSize 是字号：25 标准 / 18 小。
	//
	// ⚠️ 实测还有 45、64、70——同样原样带出来（见 fixture）。
	FontSize int
	// Color 是**十进制 RGB**（B 站给的就是十进制，不是十六进制）。
	//
	// 16777215 = 白。⚠️ 它没有 alpha 通道。
	Color uint32
	// Text 是正文（XML 实体已解开：`&amp;` → `&`）。
	Text string
	// ID 是这条弹幕的 id（`p` 的**第 8 段**，那个 19 位的大整数）。
	//
	// ⚠️ **它是字符串**：那个数走过 `float64` 会掉精度（变成另一个数）。
	//
	// ⚠️ **票 #576 给的 struct 里没有这一项**，而它是**下一张票的必需品**
	//（#578：满屏播放器上滚动弹幕，每条弹幕要一个**稳定且唯一**的 key——
	// 报告 §3.1 实测过：同一个 key 换一批弹幕会继承上一批的位移，元素直接
	// 跑到屏幕外、**一个字都画不出来**）。所以这里加上它，
	// **其余五项与票面给的 struct 一字不差**。
	ID string
}

// danmakuXML 是弹幕响应里我们关心的那一部分。
//
// ⚠️ **只声明 `<d>` 元素**：B 站的响应里还有 `chatserver` / `chatid` /
// `maxlimit` / `state` 那些字段，而这一层只需要弹幕本身。`encoding/xml`
// 会把没声明的元素**直接跳过**。
type danmakuXML struct {
	Items []danmakuElem `xml:"d"`
}

// danmakuElem 是一个 `<d>` 元素。
//
// ⚠️ **正文用 `,chardata`，不是 `,innerxml`**：`encoding/xml` 的
// `chardata` **已经把实体解开了**（`B&amp;O Level` → `B&O Level`），
// 而 `innerxml` 给的是**没解的原文**。用 `innerxml` 的症状是界面上出现
// 「B&amp;O Level」这一串符号。
type danmakuElem struct {
	// P 是那 9 段（逗号分隔）。字段表见 `danmakuP` 的注释。
	P string `xml:"p,attr"`
	// Text 是正文（实体已解开）。
	Text string `xml:",chardata"`
}

// danmakuP 是 `p` 属性那 9 段的字段表（实测逐段对上，2026-10-27）。
//
//	1  时间（秒，5 位小数）      70.12000
//	2  模式                     1 滚动 / 4 底部 / 5 顶部（另有 6 / 7）
//	3  字号                     25 标准 / 18 小（另有 45 / 64 / 70）
//	4  颜色（十进制 RGB）        16777215 = 白
//	5  ctime（秒）              1792814614
//	6  pool                    0
//	7  用户哈希（8 位十六进制）   ce48882b
//	8  rowid（弹幕 id）         2229049562097370368
//	9  额外位                   10（**没查到它是什么**，不影响渲染）
//
// ⚠️ **第 8 段是 19 位的大整数**——走过 `float64` 会掉精度（弹幕 id 会
// 变成另一个数），所以它只能留在字符串里，用到的时候再按整数解。
type danmakuP struct {
	// TimeSec 是第 1 段：视频时间（秒，5 位小数）。
	TimeSec float64
	// Mode 是第 2 段。
	Mode int
	// FontSize 是第 3 段。
	FontSize int
	// Color 是第 4 段：十进制 RGB。
	Color uint32
	// RowID 是第 8 段：弹幕 id（19 位整数，**不转成 float**）。
	RowID string
}

// Danmaku 取一个 cid 的弹幕。
//
// 空列表 = **这个 cid 没有弹幕**（正常的一种，不是错误）。
//
// ⚠️ **顺序是 B 站给的顺序，不是按时间排的**（实测：第一条是 70.12 秒、
// 第二条 16.205 秒）。要「按时间取现在该在屏幕上的那几条」得自己排——
// 而**排在这里是不对的**：这一层返回的是**B 站给的那份列表**，谁排谁负责。
func (c *Client) Danmaku(ctx context.Context, cid string) ([]Danmaku, error) {
	if cid == "" {
		// 不是网络错也不是重试能救的：调用方没给全。
		return nil, nil
	}

	raw, err := c.DoRaw(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathDanmaku,
		Query:  url.Values{"oid": {cid}},
		// ⚠️ **不要 WBI、不要 CSRF**：实测这个接口不需要签名，
		// 而带上 WBI 会多发一个 nav 请求（那一次本身可能撞风控）。
		Buvid: true,
	})
	if err != nil {
		return nil, err
	}

	xmlBytes, err := inflateDanmaku(raw)
	if err != nil {
		return nil, err
	}
	// 🔴 **「回的不是弹幕」要当成错误，而不是当成「没有弹幕」**。
	//
	// 这一档是**静默**的：B 站被风控挡的时候回的是一页 HTML，而那一页
	// **也是合法的 XML**——于是「拿 XML 去解」只会得出「零条弹幕」。
	// ⇒ 症状是「这个视频没有弹幕」，而真相是「现在问不出来」。
	//
	// ⚠️ **判据放在解压之后**：B 站给的**每一份**响应都是压着的
	// （实测连「弹幕被关闭」那一份也是），所以挡下来的那一页也是压着的
	// ——不解压就判不出来。
	if looksLikeABlockedPage(xmlBytes) {
		return nil, fmt.Errorf("%w: %s: 响应不是弹幕（多半是被风控挡了）",
			ErrBadResponse, pathDanmaku)
	}
	return parseDanmaku(xmlBytes)
}

// inflateDanmaku 把 B 站给的那一档 deflate 解开。
//
// 🔴 **用 `compress/flate`（原始 deflate），不是 `compress/zlib`**。
//
// zlib 的头是 `78 xx`、gzip 的是 `1f 8b`，而 B 站给的是**两者都不是**的
// 那一档。拿 zlib 去解会报 `flate: corrupt input before offset 5`
// （一句看不懂的错），而症状是「一条弹幕都没有」。
//
// ⚠️ **它不会自动解**：`net/http` 只自动解 gzip，不解 deflate——所以
// 响应体到这一层还是压着的。
// maxDanmakuInflatedBytes 是一份弹幕解压之后的字节上限。
//
// 压缩这一侧已经有响应体上限（见 client.go 的 32 MiB），但 deflate 的压缩比
// 可以到几十上百倍：一份几百 KB 的响应完全能解出几百 MB，而解压时**原始压缩
// 字节和展开结果会同时在内存里**。B 站实测最长的弹幕列表是几 MB 量级，所以
// 32 MiB 是「正常永远撞不到、异常一定挡住」的那一档。
const maxDanmakuInflatedBytes = 32 << 20

func inflateDanmaku(raw []byte) ([]byte, error) {
	// ⚠️ **0 字节不是错**：B 站对「这个 cid 没有弹幕」有可能回一个**空的**
	// body（实测过），而那是**正常**的一种——零条弹幕，不是错误。
	//
	// ⚠️ **这一条必须先判**：不然 `flate.NewReader` 会拿一个空的 reader 去解，
	// 报出来的是一句 `unexpected EOF`——而那说的是「压缩层坏了」，
	// 和真相（压根没有内容）**不是一回事**。
	if len(raw) == 0 {
		return nil, nil
	}

	r := flate.NewReader(bytes.NewReader(raw))
	defer r.Close()

	// 多读一个字节就能分出「刚好到上限」和「超过上限」：限制本身不接受
	// 第 limit+1 个字节。
	out, err := io.ReadAll(io.LimitReader(r, maxDanmakuInflatedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("bili: 弹幕的压缩层解不开（不是原始 deflate？）：%w", err)
	}
	if len(out) > maxDanmakuInflatedBytes {
		return nil, fmt.Errorf("bili: 弹幕解压之后超过 %d 字节，不当成正常响应", maxDanmakuInflatedBytes)
	}
	return out, nil
}

// parseDanmaku 解那份 XML。
//
// # 🔴 **为什么不是「一个 decoder 读到底」**
//
// 最直观的写法是 `xml.NewDecoder` + `Token()` + `DecodeElement`。那一档
// 有个**静默**的失败：`DecodeElement` 遇到**解不开**的 `<d>` 会**报错**，
// 而那个错**让整个 decoder 卡住**——后面的合法弹幕**一条都读不到**。
//
// ⚠️ **实测会踩到的那一档**：弹幕的正文是**用户自己打的字**，什么字符都
// 可能有。`encoding/xml` 对**非法**的 XML 是**报错**的——
// 一个**控制字符**（实测 U+0001）或一个**没定义**的实体都会触发。
//
// ⇒ 症状是「这个视频只有一条弹幕」（第一条之后的全丢），而界面上看着是
// 「这个视频弹幕很少」——**看不出坏了**。
//
// ⇒ 所以这里**不用一个 decoder 读到底**，而是**先把 `<d>` 的边界找出来、
// 再一条一条单独解**：解不开的**只丢那一条**，后面的照读。
//
// # 边界怎么找
//
// B 站的格式是紧挨着的 `<d p="…">正文</d>`，而实测**正文里不会出现
// `</d>`**（三个 cid 一共 5400 条，`</d>` 的出现次数与 `<d p="` 的次数
// **相等**，正文里也没有转义过的 `&lt;/d&gt;`）。⇒ 按 `<d p="` 开、按
// `</d>` 收，切出来的一定是一条完整的弹幕。
//
// ⚠️ **切分靠的是字节查找，不是 XML 解析**——所以它对**解不开的那一条**
// 也照样成立（那正是要靠它才救得回来的那一档）。
func parseDanmaku(xmlBytes []byte) ([]Danmaku, error) {
	var out []Danmaku
	for _, seg := range danmakuSegments(xmlBytes) {
		// ⚠️ **每一条单独解**：解不开的**只丢那一条**。
		el, err := decodeDanmakuSegment(seg)
		if err != nil {
			continue
		}
		d, ok := danmakuOf(el)
		if !ok {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// decodeDanmakuSegment 解**一条**弹幕。
//
// ⚠️ **非严格模式**：弹幕的正文是**用户自己打的字**，什么字符都可能有。
// 一个**没定义**的实体（`&foo;`）在严格模式下是**报错**的，而它**原样留着
// 就挺好**（那是用户打的字）——非严格模式下 `encoding/xml` 把它原样带出来。
//
// ⚠️ **但控制字符仍然解不开**：U+0001 这类字符 XML 一律不许，即使非严格
// 模式也会报错（实测）。⇒ 那一**条**被丢掉，**别的条不受影响**。
func decodeDanmakuSegment(seg []byte) (danmakuElem, error) {
	dec := xml.NewDecoder(bytes.NewReader(seg))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return danmakuElem{}, err
		}
		if err != nil {
			return danmakuElem{}, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "d" {
			continue
		}
		var el danmakuElem
		if err := dec.DecodeElement(&el, &se); err != nil {
			return danmakuElem{}, err
		}
		return el, nil
	}
}

// danmakuSegments 把那份 XML 切成一条条 `<d>…</d>`。
//
// ⚠️ **切出来的是原始字节**（不是解过的）——所以调用方能一条一条**单独**
// 解，而一条解不开不会影响别的。
func danmakuSegments(xmlBytes []byte) [][]byte {
	// ⚠️ **找的是 `<d p="` 而不是 `<d `**：B 站的弹幕标签**一定**带 `p`
	// 属性（那 9 段就在那儿），而正文里出现 `<d `（一个**没转义**的 `<`）
	// 只在「那一条本来就解不开」时可能——那时按 `<d ` 找会把它当成新的一条，
	// 于是**接上**半截正文。
	const start = `<d p="`
	var out [][]byte
	for i := 0; i < len(xmlBytes); {
		j := bytes.Index(xmlBytes[i:], []byte(start))
		if j < 0 {
			break
		}
		j += i
		k := bytes.Index(xmlBytes[j:], []byte("</d>"))
		if k < 0 {
			// 没有收尾的标签（响应被截断了）：后面没有了，收工。
			break
		}
		k += j
		out = append(out, xmlBytes[j:k+len("</d>")])
		i = k + len("</d>")
	}
	return out
}

// looksLikeABlockedPage 报这堆字节是不是**B 站风控挡下来的那一页 HTML**。
//
// 🔴 **这一档是静默的**：那一页**也是合法的 XML**
// （`<html><body>412</body></html>` 能解），于是「拿 XML 去解」只会得出
// 「**零条弹幕**」——而真相是「现在问不出来」。
//
// ⇒ 判据是**头一个非空白字符**：B 站的弹幕响应以 `<?xml` 或 `<i>` 开头，
// 而风控挡下来的那一页以 `<html` 开头。
func looksLikeABlockedPage(b []byte) bool {
	trimmed := bytes.TrimLeft(b, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	lower := bytes.ToLower(trimmed)
	return bytes.HasPrefix(lower, []byte("<html")) ||
		bytes.HasPrefix(lower, []byte("<!doctype html"))
}

// danmakuOf 把一个 `<d>` 元素变成一条弹幕。
//
// 读不出来就**丢掉这一条**（返回 false），不报错——弹幕是装饰，一条读不懂
// 不该把整份丢掉（那一档的表现是「界面上少了一句弹幕」，而不是「没弹幕」）。
func danmakuOf(el danmakuElem) (Danmaku, bool) {
	p, ok := parseDanmakuP(el.P)
	if !ok {
		return Danmaku{}, false
	}
	text := danmakuText(el.Text)
	if text == "" {
		return Danmaku{}, false
	}
	return Danmaku{
		TimeMS:   int64(math.Round(p.TimeSec * 1000)),
		Mode:     p.Mode,
		FontSize: p.FontSize,
		Color:    p.Color,
		Text:     text,
		ID:       p.RowID,
	}, true
}

// parseDanmakuP 切那 9 段。
//
// ⚠️ **段数必须够 9**：实测每一段都在（600/1200/3600 条全是 9 段），
// 而段数不够的话后面几段是空的——那时候颜色会变成 0（全黑），而界面上
// 一条黑字弹幕**看着完全正常**。所以这里直接判「够不够」。
func parseDanmakuP(p string) (danmakuP, bool) {
	fields := strings.Split(p, ",")
	if len(fields) < 9 {
		return danmakuP{}, false
	}

	// ⚠️ **第 1 段是「秒，5 位小数」**：`70.12000`。
	// 用 `float64` 解是对的（毫秒那一档只要精确到 1 毫秒）。
	//
	// ⚠️ **转毫秒要 `math.Round`**：`16.205 * 1000` 在浮点里是
	// `16204.999999999998`，直接截断就少了一毫秒——而弹幕的时间是要跟
	// 播放位置对齐的，差一毫秒不致命，**但那说明这一档是随手写的**。
	sec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return danmakuP{}, false
	}
	mode, err := strconv.Atoi(fields[1])
	if err != nil {
		return danmakuP{}, false
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil {
		return danmakuP{}, false
	}
	// ⚠️ **颜色是十进制**：16777215 = 白。`strconv.ParseUint(…, 10, 32)`——
	// 走 `base 0` 的话 `0x…` 那样的写法会被当成十六进制，而 B 站给的是
	// 十进制。
	color, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return danmakuP{}, false
	}
	return danmakuP{
		TimeSec:  sec,
		Mode:     mode,
		FontSize: size,
		Color:    uint32(color),
		RowID:    fields[7],
	}, true
}

// danmakuText 把正文收拾干净。
//
// ⚠️ **实体已经解过了**（`encoding/xml` 的 `chardata` 干的活），这里只
// 去掉首尾的空白、丢掉空的。
//
// ⚠️ **空正文的那一条要丢掉**：B 站偶尔给一条**没有字**的弹幕（实测有
// `<d p="…"></d>`），而它在界面上是「一行看不见的字」——占着一条轨道的
// 位置，却什么都不显示。
func danmakuText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return text
}
