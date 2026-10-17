// COM 的 GUID：类型，以及「从我们抄下来的那串字」到「内存里那 16 个字节」的
// 翻译。
//
// **这一层不带平台标签**，因为它就是一段字符串解析——而 Windows 那一半的
// 每一个 IID 都要过它。把纯逻辑从 `*_windows.go` 里拿出来，是为了能在没有
// Windows 的机器上断它：**这一段算错，每一个 IID 都是错的**，而错的表现是
// `QueryInterface` 返回 `E_NOINTERFACE`，也就是「那个功能静默地不出现」。
// 见 guid_test.go。

package media

// guid 是一个 COM 的 GUID，按 Windows 那边在内存里的字节序存。
//
// 就是 `GUID` 结构的内存布局：
//
//	Data1（u32，小端）  Data2（u16，小端）  Data3（u16，小端）  Data4（8 字节，按顺序）
type guid [16]byte

// mustGUID 解析 `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` 这种写法
// （8-4-4-4-12 个十六进制数字，也就是 GUID 在文档里通常写成的样子：
// **中间是四组、不是两组**——`56fdf344-fd6d-11d0-958a-006097c9a090`。
//
// 为什么用字符串而不是一串十六进制字面量：一串十六进制**抄错了编译得过**，
// 而「看着不像一个 GUID」的一串字符更容易在评审里被看出端倪。
//
// ## ⚠️ 字节序：每一段都是**倒着**铺的
//
// 一个 GUID 在内存里长这样（拿 `IID_IUnknown` 举例，
// `00000000-0000-0000-c000-000000000046`）：
//
//	00 00 00 00 │ 00 00 │ 00 00 │ c0 00 00 00 00 00 00 46
//	└ Data1 ──┘ └Data2┘ └Data3┘ └─── Data4（不倒）───┘
//
// 也就是说：文字里最左边那段 `xxxxxxxx` 在内存里是**最后四个字节**，而且是
// 字节序反着的（`ea1afb91` 落成 `91 fb 1a ea`）；`Data2`/`Data3` 同样反；
// 只有 `Data4` 是正着铺的。
//
// **写错的后果是全错**：一个 IID 只要差一个字节就是另一个接口，
// `QueryInterface` 返回 `E_NOINTERFACE`，于是那一项功能静默地不出现，而应用
// 其余部分完全正常。这不是「某个功能坏掉」，是「什么都建不起来」。
//
// 格式不对就 panic：所有调用点都是包内写死的常量，没有运行期输入。
func mustGUID(s string) guid {
	var b [32]byte
	n := 0
	for i := range len(s) {
		if s[i] == '-' {
			continue
		}
		c := hexVal(s[i])
		if c < 0 || n == len(b) {
			panic("media: 不是 GUID：" + s)
		}
		b[n] = byte(c)
		n++
	}
	if n != len(b) {
		panic("media: 不是 GUID：" + s)
	}

	// 文字里第 j 对十六进制数字（j 从 0 数）铺到内存里的哪一格。
	// 倒着铺的那三段是各自**段内**倒，不是整块倒。
	pair := func(j int) byte { return b[2*j]<<4 | b[2*j+1] }

	var g guid
	g[0], g[1], g[2], g[3] = pair(3), pair(2), pair(1), pair(0) // Data1：u32 小端
	g[4], g[5] = pair(5), pair(4)                               // Data2：u16 小端
	g[6], g[7] = pair(7), pair(6)                               // Data3：u16 小端
	for j := 8; j < 16; j++ {
		g[j] = pair(j) // Data4：按顺序
	}
	return g
}

// hexVal 是一个十六进制字符的数字值，认不出来给 -1。
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
