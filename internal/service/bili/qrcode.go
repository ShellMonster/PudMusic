// 二维码编码器：把一段文本变成一张二维码的模块矩阵。
//
// # 为什么自己写
//
// B 站的扫码登录要显示一张二维码，内容就是 B 站给的登录地址。三条路：
// 找 B 站要图片、用在线二维码服务、自己在本地编码。只有第三条能过验收：
// 「二维码本地渲染，不需要联网取图」。前两条都把「用户扫的是哪个账号」
// 这件事交给了一台第三方机器——登录地址在那一刻是一次性的授权凭据。
//
// 所以这里是一份完整的 ISO/IEC 18004 编码器：字节模式、四档纠错、
// 1~40 版、8 种掩码。Biu 上游用的是 npm 的 `qrcode.react`，它也是本地算的，
// 我们只是不引入一个前端库。
//
// # 正确性怎么保证
//
// 见 qrcode_test.go 的几条硬校验：
//
//  1. **几何自洽**：`(版本, 纠错档)` 的 160 种组合，每一种的
//     「块数 × 每块码字数」都必须等于这个版本能装下的码字总数。这条把
//     两张纠错表从「抄来的数字」变成「能被几何算术验的」——抄错一位立刻红。
//  2. **BCH 锚点**：格式信息和版本信息拿规范里已知的几组值当锚点。
//  3. **反向解码**：测试里把生成的矩阵重新读回文本。掩码、去交织、蛇形
//     排布只要有一处写反，扫出来就是乱码。
//  4. **RS 数学性质**：生成多项式在 α⁰…αⁿ⁻¹ 上取零，编码后的码字多项式
//     在同样这些点上取零。
//
// 剩下没验的是「B 站的 App 扫不扫得动」——那要真机，测试证明不了。
package bili

import (
	"errors"
	"fmt"
	"image"
	"image/color"
)

// ErrTooLong 是内容太长、40 版也装不下时的错误。
//
// QR 的容量是死的。**不许截断**——截一半的二维码扫出来是另一个地址。
var ErrTooLong = errors.New("bili: 内容太长，一张二维码装不下")

// ECLevel 是纠错档。四档按「能扛多少处损伤」排，档越高能装的数据越少。
type ECLevel int

const (
	// ECLow 约 7% 的码字可以坏掉。
	ECLow ECLevel = iota
	// ECMid 约 15%。扫码登录用它：屏幕反光、手指遮挡都要留余量。
	ECMid
	// ECQuartile 约 25%。
	ECQuartile
	// ECHigh 约 30%。
	ECHigh
)

// ecFormatBits 是这个纠错档在格式信息里的两位编码。
//
// ⚠️ 这四个值不是「低=0、中=1」那样递增的，是规范定死的。写错不会报错，
// 只是扫出来是另一个纠错档，而解码器会用错档的表去还原数据 → 满屏乱码。
var ecFormatBits = [4]uint{1, 0, 3, 2}

func (e ECLevel) String() string {
	switch e {
	case ECLow:
		return "低"
	case ECMid:
		return "中"
	case ECQuartile:
		return "高"
	case ECHigh:
		return "最高"
	}
	return "未知"
}

// maxVersion 是支持的最高版本（40 × 40 个模块）。
const maxVersion = 40

// quietZone 是四周必须留白的模块数。
//
// 规范要求至少 4 个。少一个，扫码 App 就可能把定位块当成数据——实测
// 留白不够时相当一部分机器直接扫不出来。
const quietZone = 4

// Code 是一张编好的二维码。
//
// 只存「哪些模块是黑的」。颜色、缩放、留白是显示层的事，不烧进编码里。
type Code struct {
	size int
	// dark[y*size+x] 是 (x,y) 这个模块是不是黑的。
	dark []bool
}

// Size 是边长（模块数，不含留白）。
func (c *Code) Size() int { return c.size }

// Dark 报 (x, y) 这个模块是不是黑的。越界一律 false。
func (c *Code) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= c.size || y >= c.size {
		return false
	}
	return c.dark[y*c.size+x]
}

// Image 把二维码画成一张位图，module 是每个模块多少像素。
//
// 深色模块画黑、留白画白，不反色：绝大多数扫码 App 走的是二值化，
// 反色会让部分 App 直接判成「这不是二维码」。
func (c *Code) Image(module int) *image.RGBA {
	module = max(module, 1)
	total := (c.size + 2*quietZone) * module
	img := image.NewRGBA(image.Rect(0, 0, total, total))

	white := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	black := color.RGBA{A: 0xff}

	// put 是唯一写像素的地方。Stride 是**字节**数，所以横向要 ×4。
	put := func(x, y int, c color.RGBA) {
		o := y*img.Stride + x*4
		img.Pix[o] = c.R
		img.Pix[o+1] = c.G
		img.Pix[o+2] = c.B
		img.Pix[o+3] = c.A
	}
	for y := range total {
		for x := range total {
			put(x, y, white)
		}
	}
	for my := range c.size {
		for mx := range c.size {
			if !c.dark[my*c.size+mx] {
				continue
			}
			px, py := (mx+quietZone)*module, (my+quietZone)*module
			for dy := range module {
				for dx := range module {
					put(px+dx, py+dy, black)
				}
			}
		}
	}
	return img
}

// Encode 把 text 编成一张二维码，自动挑最小装得下的版本。
//
// 只支持字节模式（UTF-8 字节流）。扫码登录的地址是 ASCII URL，够了；
// 中文按 UTF-8 的字节编，解码器还原出来也是对的。
func Encode(text string, level ECLevel) (*Code, error) {
	if level < ECLow || level > ECHigh {
		level = ECMid
	}
	data := []byte(text)

	version := 0
	for v := 1; v <= maxVersion; v++ {
		if byteCapacity(v, level) >= len(data) {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, fmt.Errorf("%w：%d 字节（40 版%s档最多 %d 字节）",
			ErrTooLong, len(data), level, byteCapacity(maxVersion, level))
	}
	return encodeVersion(text, version, level)
}

// encodeVersion 按指定的版本和纠错档编码。
//
// 参数校验全在这里做。底下那些表在越界时会读到表外，panic 从编码器里
// 冒出去会带走整个进程——而调用方（界面层）只有这一个入口能碰它。
func encodeVersion(text string, version int, level ECLevel) (*Code, error) {
	if version < 1 || version > maxVersion {
		return nil, fmt.Errorf("bili: 版本 %d 不存在（1~%d）", version, maxVersion)
	}
	if level < ECLow || level > ECHigh {
		return nil, fmt.Errorf("bili: 纠错档 %d 不存在", level)
	}
	// 字节模式的长度字段在 10 版之前是 8 位，之后才是 16 位。
	// 忘了这一条，10 版以上会因为长度被截断而编出错误的内容。
	if n := len(text); (version <= 9 && n > 0xFF) || n > 0xFFFF {
		return nil, fmt.Errorf("%w：%d 字节", ErrTooLong, len(text))
	}

	bb := &bitBuffer{}
	bb.append(4, 4) // 字节模式 = 0100
	bb.append(uint(len(text)), uint(countBitsForVersion(version)))
	for i := range len(text) {
		bb.append(uint(text[i]), 8)
	}

	capacity := numDataCodewords(version, level) * 8
	bb.append(0, uint(min(4, capacity-bb.length()))) // 结束符
	bb.append(0, uint((8-bb.length()%8)%8))          // 补到整字节
	for pad := 0xEC; bb.length() < capacity; pad ^= 0xEC ^ 0x11 {
		bb.append(uint(pad), 8) // 交替填充字节 0xEC / 0x11
	}

	e := newEncoder(version, level)
	e.drawFunctionPatterns()
	e.drawCodewords(interleave(bb.bytes(), version, level))

	mask := e.chooseMask()
	e.applyMask(mask)
	e.drawFormatBits(mask)
	return e.code(), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 纠错：GF(256) 上的 Reed-Solomon
// ─────────────────────────────────────────────────────────────────────────────

// gfPrimitive 是本原多项式 x⁸+x⁴+x³+x²+1，也就是 QR 规范定的 0x11D。
// 换成别的（比如 AES 用的 0x11B）出来的码，扫码器解不开。
const gfPrimitive = 0x11D

var (
	gfExp [512]byte // gfExp[i] = α^i（开两张是为了乘法时省一次取模）
	gfLog [256]byte // gfLog[α^i] = i。gfLog[0] 恒为 0，0 不参与对数运算
)

func init() {
	x := 1
	for i := range 255 {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= gfPrimitive
		}
	}
	for i := 255; i < len(gfExp); i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// rsGenerator 生成 degree 次的 RS 生成多项式。
//
// 返回值**不含最高位的 1**，系数按高次到低次排（长度就是 degree）。
func rsGenerator(degree int) []byte {
	res := make([]byte, degree)
	res[degree-1] = 1
	root := byte(1)
	for range degree {
		for j := range degree {
			res[j] = gfMul(res[j], root)
			if j+1 < degree {
				res[j] ^= res[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return res
}

// rsRemainder 算 data 除以生成多项式的余数，即纠错码字。
func rsRemainder(data, gen []byte) []byte {
	res := make([]byte, len(gen))
	for _, b := range data {
		factor := b ^ res[0]
		copy(res, res[1:])
		res[len(res)-1] = 0
		for i, g := range gen {
			res[i] ^= gfMul(g, factor)
		}
	}
	return res
}

// eccCodewords 是「版本 × 纠错档」→ 每块的纠错码字数。
//
// 数据来自 ISO/IEC 18004 的表 13-22，是规范里的固定值，没有公式能算出来。
//
// ⚠️ **抄错一位的后果是静默的**：编码照样跑完、图照样出得来，但扫出来是乱码。
// qrcode_test.go 的 TestTablesMatchTheModuleGeometry 拿 160 种组合的
// 「块数 × 每块码字数 = 该版本能装下的码字总数」逐个验，抄错立刻红。
var eccCodewords = [4][maxVersion + 1]int{
	//          v0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40
	ECLow:      {-1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	ECMid:      {-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
	ECQuartile: {-1, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	ECHigh:     {-1, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
}

// numBlocks 是「版本 × 纠错档」→ 纠错块数。同样来自表 13-22。
var numBlocks = [4][maxVersion + 1]int{
	//          v0 1  2  3  4  5  6  7  8  9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40
	ECLow:      {-1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
	ECMid:      {-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
	ECQuartile: {-1, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
	ECHigh:     {-1, 1, 1, 2, 4, 4, 4, 5, 5, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
}

// alignmentPositions 是每个版本的校正图形中心坐标（ISO/IEC 18004 表 E.1）。
//
// 不用公式生成：这张表不是等差数列（7~13 版跳一次，之后步长还会变），
// 任何「看着像规律」的算法都会在某一版上错位，而错位的后果是这张码
// 永远扫不出来——校正图形的中心和真正的数据区对不上。
//
// qrcode_test.go 的 TestAlignmentPositionsBracketTheSymbol 验两条硬性质：
// 第一个恒为 6，最后一个恒为 边长−7。
var alignmentPositions = [maxVersion + 1][]int{
	0:  {},
	1:  {},
	2:  {6, 18},
	3:  {6, 22},
	4:  {6, 26},
	5:  {6, 30},
	6:  {6, 34},
	7:  {6, 22, 38},
	8:  {6, 24, 42},
	9:  {6, 26, 46},
	10: {6, 28, 50},
	11: {6, 30, 54},
	12: {6, 32, 58},
	13: {6, 34, 62},
	14: {6, 26, 46, 66},
	15: {6, 26, 48, 70},
	16: {6, 26, 50, 74},
	17: {6, 30, 54, 78},
	18: {6, 30, 56, 82},
	19: {6, 30, 58, 86},
	20: {6, 34, 62, 90},
	21: {6, 28, 50, 72, 94},
	22: {6, 26, 50, 74, 98},
	23: {6, 30, 54, 78, 102},
	24: {6, 28, 54, 80, 106},
	25: {6, 32, 58, 84, 110},
	26: {6, 30, 58, 86, 114},
	27: {6, 34, 62, 90, 118},
	28: {6, 26, 50, 74, 98, 122},
	29: {6, 30, 54, 78, 102, 126},
	30: {6, 26, 52, 78, 104, 130},
	31: {6, 30, 56, 82, 108, 134},
	32: {6, 34, 60, 86, 112, 138},
	33: {6, 30, 58, 86, 114, 142},
	34: {6, 34, 62, 90, 118, 146},
	35: {6, 30, 54, 78, 102, 126, 150},
	36: {6, 24, 50, 76, 102, 128, 154},
	37: {6, 28, 54, 80, 106, 132, 158},
	38: {6, 32, 58, 84, 110, 136, 162},
	39: {6, 26, 54, 82, 110, 138, 166},
	40: {6, 30, 58, 86, 114, 142, 170},
}

// numDataModules 是某个版本能装数据的模块数（不算功能图形）。
//
// 这是规范表 1 的闭式解，**不是查表**——它同时也是纠错表的校验基准。
func numDataModules(version int) int {
	result := (16*version+128)*version + 64
	if version >= 2 {
		numAlign := version/7 + 2
		result -= (25*numAlign-10)*numAlign - 55
		if version >= 7 {
			result -= 36 // 两块版本信息各 18 位
		}
	}
	return result
}

// numDataCodewords 是「版本 × 档」能装的数据码字数。
func numDataCodewords(version int, level ECLevel) int {
	return numDataModules(version)/8 -
		numBlocks[level][version]*eccCodewords[level][version]
}

// countBitsForVersion 是字节模式里「内容有多长」那个字段占几位。
//
// 10 版及以下 8 位，10 版以上 16 位。忘了这一条，长度会被截断，
// 编出来的是另一个字符串的二维码——扫得进去，登的是别的账号。
func countBitsForVersion(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

// headerBytes 是字节模式的头占几个字节：4 位模式 + 长度字段。
func headerBytes(version int) int {
	return (4 + countBitsForVersion(version) + 7) / 8
}

// byteCapacity 是「版本 × 档」能装多少字节的正文。
//
// ⚠️ **别拿 numDataCodewords 直接比正文长度**：数据码字里还占着头，
// 差的那两三个字节决定了同样长度是 1 版还是 2 版。
// 这个 bug 特别阴险——它只在「长度正好卡在容量边界上」时才发作，
// 而且发作时图照样出得来，只是扫出来是另一个长度。
func byteCapacity(version int, level ECLevel) int {
	return numDataCodewords(version, level) - headerBytes(version)
}

// interleave 补上各块的纠错码字，再把块交织成一条连续的码字流。
//
// 交织是必须的：解码器按「每块的前第 i 个码字」来读，不交织的话第二块
// 以后的位置全错。
//
// **块长差 1 时，短块排在前面**（规范要求，解码器就是按这个顺序跳的）。
// 排反了照样编得出来，扫出来是乱码。
func interleave(data []byte, version int, level ECLevel) []byte {
	blocks := numBlocks[level][version]
	eccLen := eccCodewords[level][version]
	total := numDataModules(version) / 8

	// 有零头时多出来的码字摊给**最后**几块，前面的保持短。
	numShort := blocks - total%blocks
	shortData := total/blocks - eccLen // 短块的数据码字数

	gen := rsGenerator(eccLen)
	ks := make([][]byte, blocks)
	offset := 0
	for j := range blocks {
		dataLen := shortData
		if j >= numShort {
			dataLen++
		}
		block := append([]byte(nil), data[offset:offset+dataLen]...)
		offset += dataLen
		ks[j] = append(block, rsRemainder(block, gen)...)
	}

	out := make([]byte, 0, total)
	for i := range shortData + 1 {
		for j, k := range ks {
			// 第 shortData 位上短块已经没有数据了——解码器在这里跳块，
			// 编码器也必须跳，差一位后面全错。
			if i == shortData && j < numShort {
				continue
			}
			out = append(out, k[i])
		}
	}
	for i := range eccLen {
		for _, k := range ks {
			out = append(out, k[len(k)-eccLen+i])
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 矩阵
// ─────────────────────────────────────────────────────────────────────────────

type encoder struct {
	version int
	level   ECLevel
	size    int
	modules []bool
	// fn 记哪些模块是功能图形。数据码字**绝不能**写进去——写了定位块
	// 就被数据冲掉，这张码谁都扫不出来。
	fn []bool
}

func newEncoder(version int, level ECLevel) *encoder {
	size := version*4 + 17
	return &encoder{
		version: version,
		level:   level,
		size:    size,
		modules: make([]bool, size*size),
		fn:      make([]bool, size*size),
	}
}

func (e *encoder) code() *Code {
	return &Code{size: e.size, dark: append([]bool(nil), e.modules...)}
}

func (e *encoder) at(x, y int) bool { return e.modules[y*e.size+x] }

func (e *encoder) set(x, y int, dark bool) { e.modules[y*e.size+x] = dark }

// setFunction 画一个功能图形模块，同时标成「不许放数据」。
func (e *encoder) setFunction(x, y int, dark bool) {
	e.set(x, y, dark)
	e.fn[y*e.size+x] = true
}

func (e *encoder) isFunction(x, y int) bool {
	if x < 0 || y < 0 || x >= e.size || y >= e.size {
		return true
	}
	return e.fn[y*e.size+x]
}

func (e *encoder) drawFunctionPatterns() {
	e.drawFinder(3, 3)
	e.drawFinder(e.size-4, 3)
	e.drawFinder(3, e.size-4)
	e.drawAlignment()
	e.drawTiming()
	e.drawFormatBits(0) // 占位，选定掩码后重画
	e.drawVersionBits()
}

// drawFinder 画定位图形（7×7 同心方框）加上四周的分隔带。
//
// 半径取 [-4,4] 而不是 [-3,3]：那圈「分隔带」也必须是白的，漏掉的话
// 相邻的数据模块会贴上来，定位图形被污染，扫不出来。
func (e *encoder) drawFinder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= e.size || y >= e.size {
				continue
			}
			dist := max(abs(dx), abs(dy)) // 切比雪夫距离
			e.setFunction(x, y, dist != 2 && dist != 4)
		}
	}
}

func (e *encoder) drawAlignment() {
	pos := alignmentPositions[e.version]
	n := len(pos)
	if n == 0 {
		return
	}
	for i, ax := range pos {
		for j, ay := range pos {
			// 三个角上的校正图形和定位图形重叠，跳过。
			if (i == 0 && j == 0) || (i == 0 && j == n-1) || (i == n-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					e.setFunction(ax+dx, ay+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
}

func (e *encoder) drawTiming() {
	for i := 8; i < e.size-8; i++ {
		e.setFunction(i, 6, i%2 == 0)
		e.setFunction(6, i, i%2 == 0)
	}
}

// drawVersion 写版本信息（7 版以上才有，两份）。
func (e *encoder) drawVersionBits() {
	if e.version < 7 {
		return
	}
	rem := uint(e.version)
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	bits := uint(e.version)<<12 | rem
	for i := range 18 {
		bit := bits>>uint(i)&1 != 0
		a, b := e.size-11+i%3, i/3
		e.setFunction(a, b, bit)
		e.setFunction(b, a, bit)
	}
}

// drawFormatBits 写格式信息（纠错档 + 掩码号，BCH(15,5) 码，两份）。
func (e *encoder) drawFormatBits(mask int) {
	data := ecFormatBits[e.level]<<3 | uint(mask)
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412 // 15 位，低位在前

	// 定位图形周围那一份。
	for i := range 6 {
		e.setFunction(8, i, bits>>uint(i)&1 != 0)
	}
	e.setFunction(8, 7, bits>>6&1 != 0)
	e.setFunction(8, 8, bits>>7&1 != 0)
	e.setFunction(7, 8, bits>>8&1 != 0)
	for i := 9; i < 15; i++ {
		e.setFunction(14-i, 8, bits>>uint(i)&1 != 0)
	}
	// 右下角那一份。
	for i := range 8 {
		e.setFunction(e.size-1-i, 8, bits>>uint(i)&1 != 0)
	}
	for i := 8; i < 15; i++ {
		e.setFunction(8, e.size-15+i, bits>>uint(i)&1 != 0)
	}
	e.setFunction(8, e.size-8, true) // 恒为黑的那一格
}

// drawCodewords 把码字流按蛇形填进矩阵。
func (e *encoder) drawCodewords(codewords []byte) {
	bit := 0
	for right := e.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5 // 跳过纵向定时图形那一列
		}
		upward := ((right + 1) & 2) == 0
		for vert := range e.size {
			y := vert
			if upward {
				y = e.size - 1 - vert
			}
			for j := range 2 {
				x := right - j
				if e.isFunction(x, y) {
					continue
				}
				// 码字填不满剩下的模块留白——那是规范里的余数位。
				dark := bit < len(codewords)*8 &&
					codewords[bit>>3]&(1<<uint(7-bit&7)) != 0
				e.set(x, y, dark)
				bit++
			}
		}
	}
}

// maskFunc 是八种掩码之一。参数是 (mask, x, y)。
func maskFunc(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	case 7:
		return ((x+y)%2+x*y%3)%2 == 0
	}
	return false
}

func (e *encoder) applyMask(mask int) {
	for y := range e.size {
		for x := range e.size {
			if !e.fn[y*e.size+x] && maskFunc(mask, x, y) {
				e.modules[y*e.size+x] = !e.modules[y*e.size+x]
			}
		}
	}
}

// chooseMask 挨个试八种掩码，取罚分最低的。
//
// ⚠️ 这一步**只影响扫得容不容易，不影响扫不扫得出**：格式信息里记着
// 用的是第几种掩码，解码器照着去反掩码，八种都合法。选低罚分的，是
// 为了在反光、缩放、脏屏面上更稳。
func (e *encoder) chooseMask() int {
	before := append([]bool(nil), e.modules...)
	best, bestPenalty := 0, -1
	for mask := range 8 {
		e.modules = append([]bool(nil), before...)
		e.applyMask(mask)
		e.drawFormatBits(mask)
		if p := e.penalty(); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = mask, p
		}
	}
	e.modules = before
	return best
}

// penalty 是规范四条罚分之和。四条各自对应一种会让扫码器误判的图案，
// 一条都不能少。
func (e *encoder) penalty() int {
	const (
		n1 = 3  // 规则 1：行/列里连续 5 个以上同色
		n2 = 3  // 规则 2：2×2 同色块
		n3 = 40 // 规则 3：1:1:3:1:1 的定位图形样式
		n4 = 10 // 规则 4：深色占比的偏离
	)

	p := 0
	// 规则 1 和 3：按行扫一遍、按列扫一遍。
	for _, byRow := range []bool{true, false} {
		p += e.scanLine(n1, n3, byRow)
	}
	// 规则 2：数同色的 2×2 块。
	for y := range e.size - 1 {
		for x := range e.size - 1 {
			c := e.at(x, y)
			if c == e.at(x+1, y) && c == e.at(x, y+1) && c == e.at(x+1, y+1) {
				p += n2
			}
		}
	}
	// 规则 4：深色占比「向下取到 5% 的整数倍」之后差多少，每差 1% 记 10 分。
	//
	// 按规范原文算的（离最近的**下方** 5% 整数倍，不是离 50%）。
	// 用千分位整数运算，避免浮点在不同平台上给出不同的掩码——
	// 掩码变了图就变了，而图变了没法解释「为什么昨天那张能扫今天不能」。
	dark := 0
	for _, d := range e.modules {
		if d {
			dark++
		}
	}
	total := e.size * e.size
	permille := dark * 1000 / total
	base := permille / 50 * 50
	p += n4 * abs(permille-base) / 10
	return p
}

// scanLine 扫一整行或一整列，算规则 1 和规则 3 的罚分。
func (e *encoder) scanLine(n1, n3 int, byRow bool) int {
	bits := make([]bool, e.size)
	for i := range e.size {
		if byRow {
			bits[i] = e.at(i, 0)
		} else {
			bits[i] = e.at(0, i)
		}
	}

	p := 0

	// 规则 1：连续同色 5 个记 3 分，之后每多一个再记 1 分。
	run, prev := 1, bits[0]
	for i := 1; i < e.size; i++ {
		if bits[i] == prev {
			run++
			continue
		}
		if run >= 5 {
			p += n1 + (run - 5)
		}
		prev, run = bits[i], 1
	}
	if run >= 5 {
		p += n1 + (run - 5)
	}

	// 规则 3：扫「1:0:1:1:1:0:1 + 4 个浅色」这个 11 格窗口。
	//
	// 窗口之外还有 4 个浅色才计数——图边上没有「之外」，所以窗口必须
	// 完全落在图内、且那 4 格也在图内。
	const windowLen = 11
	for start := 0; start+windowLen <= e.size; start++ {
		w := bits[start : start+windowLen]
		if !w[0] || !w[2] || !w[3] || !w[4] || !w[6] || w[1] || w[5] {
			continue // 不是 1:1:3:1:1
		}
		if !w[7] && !w[8] && !w[9] && !w[10] && start+windowLen+4 <= e.size {
			p += n3 // 后面跟着 4 个浅色
			continue
		}
		if start >= 4 && !bits[start-1] && !bits[start-2] && !bits[start-3] && !bits[start-4] {
			p += n3 // 前面有 4 个浅色
		}
	}
	return p
}

// bitBuffer 是只往末尾追加的位串，**高位在前**存进字节。
type bitBuffer struct {
	data []byte
	n    int // 已用位数
}

func (b *bitBuffer) append(val, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		if b.n%8 == 0 {
			b.data = append(b.data, 0)
		}
		if val>>uint(i)&1 != 0 {
			b.data[b.n/8] |= 1 << uint(7-b.n%8)
		}
		b.n++
	}
}

func (b *bitBuffer) length() int { return b.n }

func (b *bitBuffer) bytes() []byte { return b.data }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
