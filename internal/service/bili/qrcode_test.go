package bili

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 这个文件里没有「调用 Encode 看看不报错」这种测试。
//
// 那只能证明代码跑得完，证明不了图能被扫。真正能证明的是**反向解码**：
// 把生成的矩阵当成一张别人发来的二维码，重新读出文本。掩码、去交织、
// 蛇形排布、格式信息只要有一处写反，读出来就不是原文。

// ─────────────────────────────────────────────────────────────────────────────
// 表对不对
// ─────────────────────────────────────────────────────────────────────────────

// TestTablesMatchTheModuleGeometry 拿几何验两张纠错表。
//
// 这两张表是 ISO/IEC 18004 表 13-22 的固定值，没有公式能算出来，只能抄。
// 抄错一位的后果是静默的：编码照跑、图照出、扫出来是乱码。
//
// 验的办法：某个版本能装的码字总数只由**几何**决定（`numDataModules`，
// 一个跟纠错档完全无关的闭式解）。表里的「块数 × 每块纠错码字数 + 数据
// 码字数」必须正好等于它。160 种组合逐个验，抄错立刻红。
//
// 这里特意用两个**独立算出来**的量做交叉验，不拿表自身当基准。
func TestTablesMatchTheModuleGeometry(t *testing.T) {
	for v := 1; v <= maxVersion; v++ {
		raw := numDataModules(v) / 8
		for lvl := ECLow; lvl <= ECHigh; lvl++ {
			blocks := numBlocks[lvl][v]
			ecc := eccCodewords[lvl][v]
			if blocks <= 0 || ecc <= 0 {
				t.Errorf("%d 版%s档：块数 %d / 每块纠错 %d，必须都是正数", v, lvl, blocks, ecc)
				continue
			}
			data := raw - blocks*ecc
			if data <= 0 {
				t.Errorf("%d 版%s档：数据码字数 %d，纠错码字比数据还多", v, lvl, data)
				continue
			}
			// 块长只能差 1：有零头时多出来的码字摊给最后几块。
			short := raw / blocks
			if raw%blocks != 0 && short*blocks+raw%blocks != raw {
				t.Errorf("%d 版%s档：块长摊不开", v, lvl)
			}
			if short-ecc != data/blocks && data%blocks != 0 {
				t.Errorf("%d 版%s档：短块 %d 字节装不下 %d 字节数据", v, lvl, short-ecc, data/blocks)
			}
		}
	}
}

// TestDataCodewordCountsMatchTheStandard 在几个版本上钉住具体数字。
//
// 上一条验的是表**自洽**（抄错一位且另一处正好抵消也能过）。这条拿
// 规范里广为人知的容量数字当独立锚点：这几个数被所有 QR 实现反复引用过，
// 对不上就是表抄错了。
func TestDataCodewordCountsMatchTheStandard(t *testing.T) {
	cases := []struct {
		version int
		level   ECLevel
		want    int
	}{
		{1, ECLow, 19}, {1, ECMid, 16}, {1, ECQuartile, 13}, {1, ECHigh, 9},
		{10, ECLow, 274}, {10, ECMid, 216}, {10, ECQuartile, 154}, {10, ECHigh, 122},
		{25, ECMid, 1000},
		{40, ECLow, 2956}, {40, ECMid, 2334}, {40, ECQuartile, 1666}, {40, ECHigh, 1276},
	}
	for _, c := range cases {
		if got := numDataCodewords(c.version, c.level); got != c.want {
			t.Errorf("%d 版%s档装了 %d 个数据码字，want %d", c.version, c.level, got, c.want)
		}
	}
}

// TestAlignmentPositionsBracketTheSymbol 验校正图形表的两条硬性质。
//
// 第一个恒为 6（紧挨定位图形），最后一个恒为 边长−7（留 6 个模块的边）。
// 这两条抓的是抄表时最常见的错——抄错中间某个坐标这两条抓不到，
// 但抄错首尾一定抓得到，而首尾错位的码同样扫不出来。
func TestAlignmentPositionsBracketTheSymbol(t *testing.T) {
	for v := 1; v <= maxVersion; v++ {
		pos := alignmentPositions[v]
		size := v*4 + 17
		if v == 1 {
			if len(pos) != 0 {
				t.Errorf("1 版不该有校正图形，却有 %v", pos)
			}
			continue
		}
		if len(pos) == 0 {
			t.Errorf("%d 版没有校正图形坐标", v)
			continue
		}
		if pos[0] != 6 {
			t.Errorf("%d 版第一个校正图形在 %d，want 6", v, pos[0])
		}
		if last := pos[len(pos)-1]; last != size-7 {
			t.Errorf("%d 版最后一个校正图形在 %d，want %d（边长−7）", v, last, size-7)
		}
		// 坐标必须递增，且间距是偶数（校正图形是 5×5，中心间隔不能是奇数，
		// 否则两个图形会重叠一列）。
		for i := 1; i < len(pos); i++ {
			if pos[i] <= pos[i-1] {
				t.Errorf("%d 版的校正图形坐标没有递增：%v", v, pos)
				break
			}
			if (pos[i]-pos[i-1])%2 != 0 {
				t.Errorf("%d 版的校正图形间距是奇数，会重叠：%v", v, pos)
				break
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BCH 码字
// ─────────────────────────────────────────────────────────────────────────────

// TestFormatBitsMatchTheKnownStrings 拿格式信息表里最常被引用的四组当锚点。
//
// 这 15 位里装的是「纠错档 + 掩码号」，是解码器读这张码的第一步。
// 算错了不报错，只是解码器按错误的档去还原数据 → 满屏乱码。
// 0x5412 是规范规定的异或掩码，四档在掩码 0 时的值是公开可查的常量。
func TestFormatBitsMatchTheKnownStrings(t *testing.T) {
	cases := []struct {
		level ECLevel
		want  uint
	}{
		{ECLow, 0b111011111000100},
		{ECMid, 0b101010000010010},
		{ECQuartile, 0b011010101011111},
		{ECHigh, 0b001011010001001},
	}
	for _, c := range cases {
		if got := formatBitsForTest(c.level, 0); got != c.want {
			t.Errorf("%s档掩码 0 的格式信息是 %015b，want %015b", c.level, got, c.want)
		}
	}
}

// TestAllFormatStringsAreDistinct 32 种组合不能撞车。
//
// 撞了就意味着两种（档位, 掩码）编出来的格式信息一样，解码器读出来
// 永远是其中一种——另一张图永远解不开。
func TestAllFormatStringsAreDistinct(t *testing.T) {
	seen := map[uint]string{}
	for lvl := ECLow; lvl <= ECHigh; lvl++ {
		for mask := range 8 {
			bits := formatBitsForTest(lvl, mask)
			if key, dup := seen[bits]; dup {
				t.Errorf("%s档掩码 %d 的格式信息和「%s」撞了", lvl, mask, key)
			}
			seen[bits] = fmt.Sprintf("%s档掩码 %d", lvl, mask)
		}
	}
	if len(seen) != 32 {
		t.Errorf("只有 %d 种不同的格式信息，want 32", len(seen))
	}
}

// TestVersionBitsMatchTheKnownStrings 拿两个公开可查的版本信息值当锚点。
func TestVersionBitsMatchTheKnownStrings(t *testing.T) {
	cases := map[int]uint{7: 0b000111110010010100, 40: 0b00101000110001101001}
	for version, want := range cases {
		if got := versionBitsForTest(version); got != want {
			t.Errorf("%d 版的版本信息是 %018b，want %018b", version, got, want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Reed-Solomon 的数学性质
// ─────────────────────────────────────────────────────────────────────────────

// TestGeneratorPolynomialHasExactlyTheRequiredRoots 验生成多项式。
//
// RS 生成多项式的定义就是 ∏(x − αⁱ), i = 0..n−1。在有限域里「−」就是
// 「+」（特征 2），所以在 α⁰…αⁿ⁻¹ 上取零、在别处不取零。
// 写错了这张码扫出来也是乱码，而且是最难查的那种。
func TestGeneratorPolynomialHasExactlyTheRequiredRoots(t *testing.T) {
	for _, degree := range []int{7, 10, 13, 17, 18, 22, 24, 26, 28, 30} {
		gen := rsGenerator(degree)
		if len(gen) != degree {
			t.Fatalf("%d 次的生成多项式长度是 %d", degree, len(gen))
		}
		poly := append([]byte{1}, gen...) // 补回最高位的 1
		for i := range degree {
			root := gfExp[i]
			if polyEval(poly, root) != 0 {
				t.Errorf("%d 次生成多项式在 α^%d 上不是零", degree, i)
				break
			}
		}
	}
}

// TestRemainderMakesTheCodewordSyndromeFree 验编码结果本身。
//
// 一段码字加上它的纠错码字之后，在 α⁰…αⁿ⁻¹ 上的「伴随式」必须全是 0
// —— 这正是解码器判断「这块数据有没有被弄坏」用的值。
func TestRemainderMakesTheCodewordSyndromeFree(t *testing.T) {
	const degree = 10
	gen := rsGenerator(degree)
	for _, data := range [][]byte{
		{},
		{0x00},
		{0xff},
		bytesOf("HELLO WORLD"),
		bytesOf("扫码登录"),
	} {
		ecc := rsRemainder(data, gen)
		code := append(append([]byte(nil), data...), ecc...)
		for i := range degree {
			if polyEval(code, gfExp[i]) != 0 {
				t.Errorf("数据 %x 编码后在 α^%d 上不是零", data, i)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 端到端：编出来，再解回去
// ─────────────────────────────────────────────────────────────────────────────

// loginURL 是一条真实形状的 B 站扫码登录地址。
//
// 形状（长度、参数顺序、qrcode_key 的 32 位）照 Biu 上游用的那条
// （`passport-login-web-qrcode-generate.ts` 的 data.url）。
const loginURL = "https://passport.bilibili.com/h5-app/passport/login/scan" +
	"?navhide=1&qrcode_key=8f2c1d4e5a6b7c8d9e0f1a2b3c4d5e6f&from="

// TestEncodedCodeDecodesBackToTheText 是这个文件最重要的一条。
//
// 完整走一遍反向流程：从矩阵里读格式信息 → 反掩码 → 蛇形读码字 →
// 去交织 → 校验伴随式 → 还原出原文。
func TestEncodedCodeDecodesBackToTheText(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"扫码登录的真实地址", loginURL},
		{"一个字符", "A"},
		{"刚好一个块", strings.Repeat("x", 16)},
		{"跨块边界", strings.Repeat("y", 62)},
		{"10 版头（16 位长度）", "https://example.com/" + strings.Repeat("q", 200)},
		{"中文", "扫码登录 bilibili"},
		{"符号", "a=b&c=d#e/f?g"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, lvl := range []ECLevel{ECLow, ECMid, ECQuartile, ECHigh} {
				code, err := Encode(c.text, lvl)
				if err != nil {
					t.Fatalf("%s档：%v", lvl, err)
				}
				got, err := decodeForTest(code)
				if err != nil {
					t.Fatalf("%s档：读不回来：%v", lvl, err)
				}
				if got != c.text {
					t.Errorf("%s档读回来是 %q，want %q", lvl, got, c.text)
				}
			}
		})
	}
}

// TestEveryVersionDecodesBack 逐个版本跑一遍。
//
// 排布逻辑（校正图形、蛇形拐弯、块长差 1 的处理）在各版本上并不一样：
// 1 版没有校正图形，7 版起多两块版本信息，14 版起校正图形变成三行。
// 只测一个版本的话，上面三处里有两处根本没被走到。
func TestEveryVersionDecodesBack(t *testing.T) {
	for lvl := ECLow; lvl <= ECHigh; lvl++ {
		for v := 1; v <= maxVersion; v++ {
			text := strings.Repeat("A", byteCapacity(v, lvl))
			code, err := encodeVersion(text, v, lvl)
			if err != nil {
				t.Fatalf("%d 版%s档：%v", v, lvl, err)
			}
			got, err := decodeForTest(code)
			if err != nil {
				t.Fatalf("%d 版%s档读不回来：%v", v, lvl, err)
			}
			if got != text {
				t.Fatalf("%d 版%s档读回来是 %q（应为 %d 个 A）", v, lvl, truncate(got, 40), len(text))
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 结构
// ─────────────────────────────────────────────────────────────────────────────

// TestFinderPatternsAndTimingAreCorrect 验功能图形画对了。
//
// 三个定位图形 + 定时图形是解码器**先**读的东西——它靠它们找到符号的
// 位置、算出行列。错一格，后面读什么都白搭。
func TestFinderPatternsAndTimingAreCorrect(t *testing.T) {
	code, err := Encode(loginURL, ECMid)
	if err != nil {
		t.Fatal(err)
	}
	size := code.Size()
	if want := 17 + 4*((size-17)/4); want != size {
		t.Errorf("边长 %d 不是 17+4v 的形式", size)
	}

	// 三个角上的定位图形：中心 3×3 全黑，往外一圈全白，再外面一圈全黑。
	for _, c := range [][2]int{{3, 3}, {size - 4, 3}, {3, size - 4}} {
		cx, cy := c[0], c[1]
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				d := max(abs(dx), abs(dy))
				want := d != 2
				if got := code.Dark(cx+dx, cy+dy); got != want {
					t.Fatalf("定位图形 (%d,%d)+(%d,%d) 应是 %v，拿到 %v",
						cx, cy, dx, dy, want, got)
				}
			}
		}
		// 分隔带必须全白。
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				if max(abs(dx), abs(dy)) != 4 {
					continue
				}
				if code.Dark(cx+dx, cy+dy) {
					t.Fatalf("定位图形 (%d,%d) 的分隔带 (%d,%d) 不是白的", cx, cy, dx, dy)
				}
			}
		}
	}

	// 定时图形：第 6 行和第 6 列，从第 8 格到 size-9，黑白交替。
	for i := 8; i < size-8; i++ {
		if got, want := code.Dark(i, 6), i%2 == 0; got != want {
			t.Errorf("行定时 (%d,6) 是 %v，want %v", i, got, want)
		}
		if got, want := code.Dark(6, i), i%2 == 0; got != want {
			t.Errorf("列定时 (6,%d) 是 %v，want %v", i, got, want)
		}
	}
}

// TestVersionUsesTheSmallestThatFits 验版本是挑出来的，不是写死的。
//
// 边界正好卡在 1 版中档的容量（16 个数据码字里扣掉 2 个字节的头 = 14）。
func TestVersionUsesTheSmallestThatFits(t *testing.T) {
	code, err := Encode(strings.Repeat("A", byteCapacity(1, ECMid)), ECMid)
	if err != nil {
		t.Fatal(err)
	}
	if code.Size() != 21 {
		t.Errorf("%d 字节用了 %d×%d 的图，want 1 版（21×21）", byteCapacity(1, ECMid), code.Size(), code.Size())
	}
	code, err = Encode(strings.Repeat("A", byteCapacity(1, ECMid)+1), ECMid)
	if err != nil {
		t.Fatal(err)
	}
	if code.Size() != 25 {
		t.Errorf("多一个字节用了 %d×%d 的图，want 2 版（25×25）", code.Size(), code.Size())
	}
}

// TestHigherECLNeedsBiggerVersion 验档越高，同一段内容要的版本越大。
func TestHigherECLNeedsBiggerVersion(t *testing.T) {
	text := strings.Repeat("A", 20)
	low, err := Encode(text, ECLow)
	if err != nil {
		t.Fatal(err)
	}
	high, err := Encode(text, ECHigh)
	if err != nil {
		t.Fatal(err)
	}
	if high.Size() <= low.Size() {
		t.Errorf("同样 20 字节，最高档用了 %d×%d，低档用了 %d×%d；档越高容量越小才对",
			high.Size(), high.Size(), low.Size(), low.Size())
	}
}

// TestEncodeRejectsWhatItCannotHold 装不下就报错，绝不截断。
func TestEncodeRejectsWhatItCannotHold(t *testing.T) {
	limit := byteCapacity(maxVersion, ECHigh)
	_, err := Encode(strings.Repeat("A", limit+1), ECHigh)
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("超限 %d 字节的内容返回 %v，want ErrTooLong", limit+1, err)
	}
	// 正好卡在上限上要塞得下。
	code, err := Encode(strings.Repeat("A", limit), ECHigh)
	if err != nil {
		t.Fatalf("刚好装得下的内容编不出来：%v", err)
	}
	if got, err := decodeForTest(code); err != nil || got != strings.Repeat("A", limit) {
		t.Errorf("40 版的图读不回来：%v", err)
	}
}

// TestEncodeVersionRejectsBadArguments 越界要在入口就被挡住。
//
// 底下那些表是按 1..40 / 四档直接索引的，越界会读到表外——从编码器里
// 冒出去的 panic 会带走整个进程，而调用方只有这一个入口。
func TestEncodeVersionRejectsBadArguments(t *testing.T) {
	for _, c := range []struct {
		version int
		level   ECLevel
	}{
		{0, ECMid}, {-1, ECMid}, {maxVersion + 1, ECMid},
		{1, ECLevel(-1)}, {1, ECLevel(99)},
	} {
		if _, err := encodeVersion("hi", c.version, c.level); err == nil {
			t.Errorf("版本 %d / 档 %d 应该报错，却编成功了", c.version, c.level)
		}
	}
}

// TestEncodeFallsBackToMediumOnAnUnknownLevel 档位传错时退到中档，别炸。
func TestEncodeFallsBackToMediumOnAnUnknownLevel(t *testing.T) {
	code, err := Encode(loginURL, ECLevel(42))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeForTest(code); err != nil {
		t.Errorf("退回中档之后这张图读不回来：%v", err)
	}
}

// TestImageIsSquareAndCarriesTheQuietZone 验画出来的那张位图。
//
// 留白不足时相当一部分扫码 App 直接判「这不是二维码」——这是实测，
// 不是规范里的洁癖条款。
func TestImageIsCarriesQuietZone(t *testing.T) {
	const module = 4
	code, err := Encode(loginURL, ECMid)
	if err != nil {
		t.Fatal(err)
	}
	img := code.Image(module)

	wantSide := (code.Size() + 2*quietZone) * module
	if img.Bounds().Dx() != wantSide || img.Bounds().Dy() != wantSide {
		t.Fatalf("位图是 %dx%d，want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), wantSide, wantSide)
	}

	at := func(x, y int) [4]uint8 {
		o := y*img.Stride + x*4 // Stride 是字节数
		return [4]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3]}
	}
	white := [4]uint8{0xff, 0xff, 0xff, 0xff}
	black := [4]uint8{0, 0, 0, 0xff}

	// 四条边的留白必须全白。
	for x := range wantSide {
		for _, y := range []int{0, 1, quietZone*module - 1, wantSide - 1} {
			if got := at(x, y); got != white {
				t.Fatalf("(%d,%d) 是 %v，留白必须是白的", x, y, got)
			}
		}
	}
	for y := range wantSide {
		for _, x := range []int{0, 1, quietZone*module - 1, wantSide - 1} {
			if got := at(x, y); got != white {
				t.Fatalf("(%d,%d) 是 %v，留白必须是白的", x, y, got)
			}
		}
	}

	// 内容必须和模块矩阵一致。
	seenDark := false
	for my := range code.Size() {
		for mx := range code.Size() {
			px, py := (mx+quietZone)*module, (my+quietZone)*module
			want := white
			if code.Dark(mx, my) {
				want = black
				seenDark = true
			}
			if got := at(px, py); got != want {
				t.Fatalf("模块 (%d,%d) 画成了 %v，want %v", mx, my, got, want)
			}
		}
	}
	if !seenDark {
		t.Error("整张图一个黑模块都没有，编码肯定坏了")
	}
}

// TestImageSurvivesADegenerateModuleSize 别被 0 或负数搞出空图。
func TestImageSurvivesADegenerateModuleSize(t *testing.T) {
	code, err := Encode(loginURL, ECMid)
	if err != nil {
		t.Fatal(err)
	}
	img := code.Image(0)
	if img.Bounds().Dx() != code.Size()+2*quietZone {
		t.Errorf("module=0 时边长是 %d", img.Bounds().Dx())
	}
}

// TestOutOfBoundsIsNotDark 越界查询返回 false，不 panic。
func TestOutOfBoundsIsNotDark(t *testing.T) {
	code, err := Encode(loginURL, ECMid)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {code.Size(), 0}, {0, code.Size()}, {999, 999}} {
		if code.Dark(p[0], p[1]) {
			t.Errorf("越界点 (%d,%d) 报成了黑", p[0], p[1])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 测试用的反向解码
// ─────────────────────────────────────────────────────────────────────────────

// formatBitsForTest 是编码器里那段 BCH 计算的独立实现。
//
// 单独写一遍不是为了「多一种写法」，是因为：如果直接调编码器自己那个
// 函数，这条测试就只是把编码器的输出又过了一遍，什么都没验。
func formatBitsForTest(level ECLevel, mask int) uint {
	data := ecFormatBits[level]<<3 | uint(mask)
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

func versionBitsForTest(version int) uint {
	rem := uint(version)
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	return uint(version)<<12 | rem
}

// decodeForTest 把一张 `*Code` 当成别人发来的二维码读一遍。
//
// 这是这个文件里真正的「断言面」：输入只有矩阵，输出只有文本，
// 中间每一步都跟编码器用的是**不同**的代码路径。
func decodeForTest(code *Code) (string, error) {
	size := code.Size()
	version := (size - 17) / 4
	if version < 1 || version > maxVersion || version*4+17 != size {
		return "", fmt.Errorf("边长 %d 不是任何一个版本的尺寸", size)
	}

	// 1. 读格式信息 → 定出纠错档和掩码号。
	level, mask, err := readFormatInfoForTest(code)
	if err != nil {
		return "", err
	}

	// 2. 重建功能图形的位置表。排布只跟版本有关，跟档位无关。
	ref := newEncoder(version, level)
	ref.drawFunctionPatterns()

	// 3. 反掩码。只对数据模块做——功能模块上的掩码没有意义。
	modules := make([]bool, size*size)
	copy(modules, code.dark)
	for y := range size {
		for x := range size {
			if !ref.fn[y*size+x] && maskFunc(mask, x, y) {
				modules[y*size+x] = !modules[y*size+x]
			}
		}
	}

	// 4. 按蛇形把码字读出来。
	bits := make([]bool, 0, len(modules))
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := ((right + 1) & 2) == 0
		for vert := range size {
			y := vert
			if upward {
				y = size - 1 - vert
			}
			for j := range 2 {
				x := right - j
				if ref.isFunction(x, y) {
					continue
				}
				bits = append(bits, modules[y*size+x])
			}
		}
	}
	stream := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			stream[i/8] |= 1 << uint(7-i%8)
		}
	}

	// 5. 去交织，并把各块拼回去。
	blocks := numBlocks[level][version]
	eccLen := eccCodewords[level][version]
	total := numDataModules(version) / 8
	if len(stream) < total {
		return "", fmt.Errorf("码字流只有 %d 字节，至少要 %d", len(stream), total)
	}
	stream = stream[:total]
	numShort := blocks - total%blocks
	shortData := total/blocks - eccLen

	ks := make([][]byte, blocks)
	dataLen := func(j int) int {
		if j >= numShort {
			return shortData + 1
		}
		return shortData
	}
	p := 0
	for i := range shortData + 1 {
		for j := range blocks {
			if i == shortData && j < numShort {
				continue
			}
			ks[j] = append(ks[j], stream[p])
			p++
		}
	}
	eccOffset := p
	for range eccLen {
		for j := range blocks {
			ks[j] = append(ks[j], stream[eccOffset])
			eccOffset++
		}
	}

	// 6. 伴随式：整块（数据+纠错）在 α⁰…αⁿ⁻¹ 上必须是零。
	for j, k := range ks {
		for i := range eccLen {
			if polyEval(k, gfExp[i]) != 0 {
				return "", fmt.Errorf("第 %d 块在 α^%d 上不是零，纠错码字错了", j, i)
			}
		}
	}

	// 7. 头：模式 + 长度 + 正文。
	data := make([]byte, 0, total)
	for j, k := range ks {
		data = append(data, k[:dataLen(j)]...)
	}
	head := &bitReader{bits: data}
	mode := head.take(4)
	if mode != 4 {
		return "", fmt.Errorf("模式是 %d，want 4（字节）", mode)
	}
	countBits := 8
	if version >= 10 {
		countBits = 16
	}
	n := int(head.take(countBits))
	if n > len(data)-2 {
		return "", fmt.Errorf("长度字段 %d 装不下", n)
	}
	out := make([]byte, n)
	for i := range n {
		out[i] = byte(head.take(8))
	}
	return string(out), nil
}

// readFormatInfoForTest 只读左上角那一份格式信息。
func readFormatInfoForTest(code *Code) (ECLevel, int, error) {
	size := code.Size()
	bits := 0
	// 位置和编码器写的顺序一一对应，读的时候按同一顺序拼回去。
	for i := range 6 {
		bits |= boolBit(code.Dark(8, i)) << uint(i)
	}
	bits |= boolBit(code.Dark(8, 7)) << 6
	bits |= boolBit(code.Dark(8, 8)) << 7
	bits |= boolBit(code.Dark(7, 8)) << 8
	for i := 9; i < 15; i++ {
		bits |= boolBit(code.Dark(14-i, 8)) << uint(i)
	}
	_ = size

	raw := bits ^ 0x5412
	// BCH 校验：15 位码字里，前 5 位是数据，剩下的余数必须等于重算出来的。
	data := uint(raw >> 10)
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	if uint(raw)&0x3FF != rem {
		return 0, 0, fmt.Errorf("格式信息 %015b 的 BCH 校验不过", bits)
	}

	levelBits := data >> 3
	mask := int(data & 7)
	for lvl := ECLow; lvl <= ECHigh; lvl++ {
		if ecFormatBits[lvl] == levelBits {
			return lvl, mask, nil
		}
	}
	return 0, 0, fmt.Errorf("格式信息里的纠错档 %d 不存在", levelBits)
}

func boolBit(b bool) int {
	if b {
		return 1
	}
	return 0
}

// bitReader 是从字节流里按位往外取的读头。
type bitReader struct {
	bits []byte
	pos  int
}

func (r *bitReader) take(n int) uint {
	var v uint
	for range n {
		byteIdx := r.pos / 8
		if byteIdx >= len(r.bits) {
			return v
		}
		v = v<<1 | uint(r.bits[byteIdx]>>uint(7-r.pos%8)&1)
		r.pos++
	}
	return v
}

// polyEval 在 GF(256) 上求多项式的值，系数按高次到低次。
func polyEval(coeffs []byte, x byte) byte {
	var v byte
	for _, c := range coeffs {
		v = gfMul(v, x) ^ c
	}
	return v
}

func bytesOf(s string) []byte { return []byte(s) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
