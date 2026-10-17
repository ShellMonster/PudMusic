package media

import "testing"

// mustGUID 的字节序是这一层最需要钉住的一段：算错了，**每一个 IID 都是错的**，
// 而错的表现是 `QueryInterface` 返回 `E_NOINTERFACE` ——
//
//	「那个功能静默地不出现」：浮窗上没有歌、任务栏上没有按钮、事件处理器挂不上，
//	而应用其余部分完全正常。
//
// 它错的时候编译得过、vet 不报、跑在 macOS 上连影子都看不见。所以这一段从
// `*_windows.go` 里搬出来单独立了 `guid.go`，就是为了能在这里断。

// TestGUIDByteOrder 断「文字 → 内存」的字节序。
//
// 右边那些是 Windows SDK 头文件里 `GUID` 结构真正的内存布局（也就是
// `DEFINE_GUID` 宏展开出来的那 16 个字节），左边是我们抄下来的字符串。
//
// ⚠️ 这一条是这一票里**唯一一个把 IID 抄错的活**。三个例子分别覆盖三种形状：
// 全零的头（IID_IUnknown）、头三段都非零的（一个 WinRT 接口 IID）、
// 头三段和尾段都非零的（一个 COM 的 CLSID）。
func TestGUIDByteOrder(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want [16]byte
	}{
		{
			// IID_IUnknown —— 头三段全零，只有 Data4 的第一个字节非零。
			// 这一条钉的是「Data4 不倒」：把它也倒了就变成 c0 在最后，
			// 看着还挺像，而每一个 IID 都会错。
			name: "IID_IUnknown",
			in:   "00000000-0000-0000-c000-000000000046",
			want: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0xc0, 0, 0, 0, 0, 0, 0, 0x46},
		},
		{
			// 一个 WinRT 接口 IID（IID_ISystemMediaTransportControls）——
			// 头三段每一段都非零，三段的倒法各不相同。
			name: "ISystemMediaTransportControls",
			in:   "99fa3ff4-1742-42a6-902e-087d41f965ec",
			want: [16]byte{
				0xf4, 0x3f, 0xfa, 0x99, // Data1：u32 小端
				0x42, 0x17, // Data2：u16 小端
				0xa6, 0x42, // Data3：u16 小端
				0x90, 0x2e, 0x08, 0x7d, 0x41, 0xf9, 0x65, 0xec, // Data4：正着
			},
		},
		{
			// CLSID_TaskbarList —— 任务栏缩略图那一条用的那个。
			// 选它是因为这个 CLSID 的 16 个字节在 shell32 的文档里到处写着，
			// 是最容易拿一份独立的来对的一串。
			name: "CLSID_TaskbarList",
			in:   "56fdf344-fd6d-11d0-958a-006097c9a090",
			want: [16]byte{
				0x44, 0xf3, 0xfd, 0x56,
				0x6d, 0xfd,
				0xd0, 0x11,
				0x95, 0x8a, 0x00, 0x60, 0x97, 0xc9, 0xa0, 0x90,
			},
		},
	} {
		got := mustGUID(c.in)
		if got != c.want {
			t.Errorf("%s：%s\n  拿到 %v\n  想要 %v", c.name, c.in, got, c.want)
		}
	}
}

// TestGUIDUpperAndLowerCaseAreTheSame 断大小写不拘。
//
// 我们抄下来的 GUID 里有大写有小写（Windows 头文件是大写，Qt 那份是小写，
// MyGo 的 `guid()` 是小写），而它们必须是同一个 IID——
// 抄成大写就 `E_NOINTERFACE`，症状和抄错一个字节一模一样。
func TestGUIDUpperAndLowerCaseAreTheSame(t *testing.T) {
	const lower = "d2b04d40-6d91-4f1f-9ec7-0d4a1e7bb9dd"
	if mustGUID(lower) != mustGUID("D2B04D40-6D91-4F1F-9EC7-0D4A1E7BB9DD") {
		t.Error("同一个 GUID 的大小写两种写法解析出了不同的东西")
	}
}

// TestGUIDRejectsGarbage 断认不出来的东西当场 panic。
//
// 格式不对而**不**报错的后果是 `mustGUID` 返回一个全零的 IID——而全零正好
// 是 `IID_IUnknown`。于是后面每一个 `queryInterface(obj, <这个全零>)` 都
// 成功，而拿到的是**完全不是我们要的那个接口**的指针：症状是随机崩，或者
// 更糟——安静地什么都不发生。
func TestGUIDRejectsGarbage(t *testing.T) {
	for _, bad := range []string{
		"",
		"1234",
		"zzzzzzzz-0000-0000-c000-000000000046",
		"00000000-0000-0000-c000-00000000004",   // 少一位
		"000000000-0000-0000-c000-000000000046", // 多一位
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q 被当成合法 GUID 收下了", bad)
				}
			}()
			_ = mustGUID(bad)
		}()
	}
}
