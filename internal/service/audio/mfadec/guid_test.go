package mfadec

import (
	"fmt"
	"strings"
	"testing"
)

// 这一组是**在 macOS 上就能验**的那一半：Media Foundation 的常量表。
//
// 为什么要单独断：GUID 和 vtable 下标是这一层里**最没法靠读代码发现写错**的
// 两样东西。写错了不会崩——`GetUINT32` 拿着一个不存在的键去问，
// 返回 `E_INVALIDARG` 或者「这个属性不存在」，两种都长得像「这台机器上解不了
// AAC」。而这一整层按设计只能在 Windows 上跑，也就是说改错了要等到发版那天
// 才可能被发现。所以这里把两组常量都钉死。

// guidFromSDKDef 按 Windows SDK 的 `DEFINE_GUID` 宏重建一个 GUID。
//
// 参数就是那个宏的实参：`Data1、Data2、Data3、Data4[0..7]`，也就是头文件里
// 一行十六进制逗号对。GUID 结构体这三个头字段是**小端**存的，宏按书写顺序给，
// 所以这里要拆开摆——顺带这也是「为什么不能把头文件那串十六进制直接当字节」
// 的答案：Data1 = 0x5FAEEAE7 在内存里是 E7 EA AE 5F，不是 5F AE EA E7。
func guidFromSDKDef(a [11]uint32) guid {
	var g guid
	g.Data1 = a[0]
	g.Data2 = uint16(a[1])
	g.Data3 = uint16(a[2])
	for i := range g.Data4 {
		g.Data4[i] = byte(a[3+i])
	}
	return g
}

// TestEveryGUIDMatchesTheWindowsSDK 断常量表的**值**。
//
// 右边逐字抄自 SDK：媒体类型和属性键来自 `um/mfapi.h`，类与接口的标识来自
// `um/wmcodecdsp.h` 和 `um/mftransform.h`，音频子类型来自 `um/mfapi.h` 里那个
// `DEFINE_MEDIATYPE_GUID` 宏的展开。以后有人「顺手整理」常量、或者升 SDK
// 时改了哪个，这里立刻红。
func TestEveryGUIDMatchesTheWindowsSDK(t *testing.T) {
	cases := []struct {
		what string
		got  guid
		sdk  [11]uint32
	}{
		// um/mfapi.h：major type。
		{"MFMediaType_Audio", guidMediaTypeAudio, [11]uint32{
			0x73647561, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		// um/mfapi.h：IMFMediaType 上的属性键。
		{"MF_MT_SUBTYPE", guidMTSubtype, [11]uint32{
			0xf7e34c9a, 0x42e8, 0x4714, 0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}},
		{"MF_MT_MAJOR_TYPE", guidMTMajorType, [11]uint32{
			0x48eba18e, 0xf8c9, 0x4687, 0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}},
		{"MF_MT_AUDIO_NUM_CHANNELS", guidMTAudioNumChannels, [11]uint32{
			0x37e48bf5, 0x645e, 0x4c5b, 0x89, 0xde, 0xad, 0xa9, 0xe2, 0x9b, 0x69, 0x6a}},
		{"MF_MT_AUDIO_SAMPLES_PER_SECOND", guidMTAudioSamplesPerSecond, [11]uint32{
			0x5faeeae7, 0x0290, 0x4c31, 0x9e, 0x8a, 0xc5, 0x34, 0xf6, 0x8d, 0x9d, 0xba}},
		{"MF_MT_AUDIO_BITS_PER_SAMPLE", guidMTAudioBitsPerSample, [11]uint32{
			0xf2deb57f, 0x40fa, 0x4764, 0xaa, 0x33, 0xed, 0x4f, 0x2d, 0x1f, 0xf6, 0x69}},
		{"MF_MT_AUDIO_AVG_BYTES_PER_SECOND", guidMTAudioAvgBytesPerSecond, [11]uint32{
			0x1aab75c8, 0xcfef, 0x451c, 0xab, 0x95, 0xac, 0x03, 0x4b, 0x8e, 0x17, 0x31}},
		{"MF_MT_AUDIO_SAMPLES_PER_BLOCK", guidMTAudioSamplesPerBlock, [11]uint32{
			0xaab15aac, 0xe13a, 0x4995, 0x92, 0x22, 0x50, 0x1e, 0xa1, 0x5c, 0x68, 0x77}},
		{"MF_MT_AUDIO_BLOCK_ALIGNMENT", guidMTAudioBlockAlignment, [11]uint32{
			0x322de230, 0x9eeb, 0x43bd, 0xab, 0x7a, 0xff, 0x41, 0x22, 0x51, 0x54, 0x1d}},
		{"MF_MT_AUDIO_CHANNEL_MASK", guidMTAudioChannelMask, [11]uint32{
			0x55fb5765, 0x644a, 0x4caf, 0x84, 0x79, 0x93, 0x89, 0x83, 0xbb, 0x15, 0x88}},
		{"MF_MT_AAC_PAYLOAD_TYPE", guidMTAACPayloadType, [11]uint32{
			0xbfbabe79, 0x7434, 0x4d1c, 0x94, 0xf0, 0x72, 0xa3, 0xb9, 0xe1, 0x71, 0x88}},
		{"MF_MT_AAC_AUDIO_PROFILE_LEVEL_INDICATION", guidMTAACProfileLevelIndication, [11]uint32{
			0x7632f0e6, 0x9538, 0x4d61, 0xac, 0xda, 0xea, 0x29, 0xc8, 0xc1, 0x44, 0x56}},
		{"MF_MT_USER_DATA", guidMTUserData, [11]uint32{
			0xb6bc765f, 0x4c3b, 0x40a4, 0xbd, 0x51, 0x25, 0x35, 0xb6, 0x6f, 0xe0, 0x9d}},
		// um/mfapi.h 的 DEFINE_MEDIATYPE_GUID(name, format) 展开成
		// DEFINE_GUID(name, format, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa,
		//              0x00, 0x38, 0x9b, 0x71)。
		{"MFAudioFormat_Base", guidAudioFormatBase, [11]uint32{
			0x00000000, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		{"MFAudioFormat_PCM", guidAudioFormatPCM, [11]uint32{
			0x00000001, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		{"MFAudioFormat_Float", guidAudioFormatFloat, [11]uint32{
			0x00000003, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		// ⚠️ 这两个的 wFormatTag **不是**按顺序猜的：0x1600 是 ADTS、0x1601 是
		// RAW、0x1610 才是 AAC。出处是 ksmedia.h 的
		// `DEFINE_GUIDSTRUCT("00001610-…", KSDATAFORMAT_SUBTYPE_MPEG_HEAAC)`
		// 和 mmregapi.h 的 WAVE_FORMAT_* 定义。
		{"MFAudioFormat_AAC", guidAudioFormatAAC, [11]uint32{
			0x00001610, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		{"MFAudioFormat_ADTS", guidAudioFormatADTS, [11]uint32{
			0x00001600, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		{"MFAudioFormat_RawAAC", guidAudioFormatRawAAC, [11]uint32{
			0x00001601, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}},
		// um/wmcodecdsp.h：`class DECLSPEC_UUID("32d186a7-…") CMSAACDecMFT;`
		{"CLSID_CMSAACDecMFT", guidCLSIDCMSAACDecMFT, [11]uint32{
			0x32d186a7, 0x218f, 0x4c75, 0x88, 0x76, 0xdd, 0x77, 0x27, 0x3a, 0x89, 0x99}},
		// um/mftransform.h：`MIDL_INTERFACE("bf94c121-…")`
		{"IID_IMFTransform", guidIIDIMFTransform, [11]uint32{
			0xbf94c121, 0x5b05, 0x4e6f, 0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d}},
	}
	for _, c := range cases {
		want := guidFromSDKDef(c.sdk)
		if c.got != want {
			t.Errorf("%s = %s，Windows SDK 的 DEFINE_GUID 是 %s",
				c.what, c.got, want)
		}
	}
}

// TestTheAudioSubtypesShareMFSTypesTail 钉一条抄错时最容易漏掉的结构性事实。
//
// `MFAudioFormat_*` 和 `MFMediaType_Audio` 那一批是 `DEFINE_MEDIATYPE_GUID`
// 生成的：格式固定成**前四个字节是 wFormatTag，后面 12 个字节一律相同**。
// 所以「同一段尾巴 + 不同的头」这一条一旦破了，就是抄的时候把两半弄混了。
func TestTheAudioSubtypesShareMFSTypesTail(t *testing.T) {
	const tail = "-0000-0010-8000-00AA00389B71}"
	for name, g := range map[string]guid{
		"MFAudioFormat_Base":  guidAudioFormatBase,
		"MFAudioFormat_PCM":   guidAudioFormatPCM,
		"MFAudioFormat_Float": guidAudioFormatFloat,
		"MFAudioFormat_AAC":   guidAudioFormatAAC,
		"MFAudioFormat_ADTS":  guidAudioFormatADTS,
		"MFMediaType_Audio":   guidMediaTypeAudio,
	} {
		if s := g.String(); !strings.HasSuffix(s, tail) {
			t.Errorf("%s = %s，KSDATAFORMAT 那一段尾巴应该都是 %s", name, s, tail)
		}
	}
	// 反过来钉一下：wFormatTag 那一头必须是对应的值。
	//
	// 这一段**曾经整体错位一格而测试全绿**——因为断言值和被测值来自同一个
	// 错误假设。教训写在这里：拿 SDK 头文件当权威时，要核对的是**宏展开后的
	// 最终值**（`WAVE_FORMAT_MPEG_HEAAC` 展开成几），不是宏参数的位置。
	// 现在的值来自 `ksmedia.h`：
	//   KSDATAFORMAT_SUBTYPE_MPEG_HEAAC    = 00001610-…
	//   WAVE_FORMAT_MPEG_ADTS_AAC           = 0x1600
	//   WAVE_FORMAT_MPEG_RAW_AAC            = 0x1601
	for name, c := range map[string]struct {
		g   guid
		tag uint32
	}{
		"MFAudioFormat_PCM":    {guidAudioFormatPCM, 0x1},
		"MFAudioFormat_Float":  {guidAudioFormatFloat, 0x3},
		"MFAudioFormat_AAC":    {guidAudioFormatAAC, 0x1610},
		"MFAudioFormat_ADTS":   {guidAudioFormatADTS, 0x1600},
		"MFAudioFormat_RawAAC": {guidAudioFormatRawAAC, 0x1601},
	} {
		if c.g.Data1 != c.tag {
			t.Errorf("%s 的头四个字节是 %#08x，wFormatTag 应该是 %#x", name, c.g.Data1, c.tag)
		}
	}
}

// TestTheThreeAACSubtypesAreNotInterchangeable 把最容易错位的那三个钉死。
//
// 「裸 AAC / 带 ADTS 头的 AAC / HE-AAC 的容器」三者在 MF 里是三个不同的
// subtype，而它们的头四个字节只差一个十六进制位——正好是最容易整体错位一格
// 的那种排布。B 站的 .m4a 里是裸 AAC 帧、.aac 文件里是带 ADTS 头的，
// 认错的表现不是报错，是 MF 说「这个类型我不认识」。
func TestTheThreeAACSubtypesAreNotInterchangeable(t *testing.T) {
	if guidAudioFormatAAC.equal(guidAudioFormatADTS) {
		t.Error("MFAudioFormat_AAC 和 MFAudioFormat_ADTS 成了同一个")
	}
	if guidAudioFormatAAC.equal(guidAudioFormatRawAAC) {
		t.Error("MFAudioFormat_AAC 和 MFAudioFormat_RawAAC 成了同一个")
	}
	if guidAudioFormatADTS.equal(guidAudioFormatRawAAC) {
		t.Error("MFAudioFormat_ADTS 和 MFAudioFormat_RawAAC 成了同一个")
	}
}

// TestParseGUIDRefusesGarbage 断解析器不猜。
//
// 这是上面那两条能成立的前提：常量写错的时候必须在 init 阶段就炸，
// 而不是解析出一个「看起来很像」的 GUID 一路跑到底。
func TestParseGUIDRefusesGarbage(t *testing.T) {
	for _, s := range []string{
		"",
		"5FAEEAE7-0290-4C31", // 少一段
		"5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA-11", // 多一段
		"5FAEEAE7_0290_4C31_9E8A_C534F68D9DBA",    // 分隔符不对
		"5FAEEAE7-0290-4C31-9E8A-C534F68D9D",      // 少一位
		"5FAEEAE7-0290-4C31-9E8A-C534F68D9BG",     // 不是十六进制
	} {
		if g, err := parseGUID(s); err == nil {
			t.Errorf("parseGUID(%q) = %s，没有报错", s, g)
		}
	}
}

// TestParseGUIDAcceptsBothSpellings 断花括号收不收都认。
func TestParseGUIDAcceptsBothSpellings(t *testing.T) {
	want := "{5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA}"
	for _, s := range []string{
		"5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA",
		"{5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA}",
		"  {5faeeae7-0290-4c31-9e8a-c534f68d9dba}  ",
	} {
		got, err := parseGUID(s)
		if err != nil {
			t.Errorf("parseGUID(%q): %v", s, err)
			continue
		}
		if g := got.String(); g != want {
			t.Errorf("parseGUID(%q).String() = %s，want %s", s, g, want)
		}
	}
}

// TestTheVtableSlotsAreWhereTheSDKPutsThem 断 vtable 下标。
//
// ⚠️ **这条证明的东西很有限，说清楚免得以后有人拿它当护身符**：它能发现
// 「有人动过这些下标」，发现不了「当初抄的时候就已经抄错了」——后者只有 Windows
// 真机能发现。它的价值是：下标错在第一次跑 spike 时只会表现为一个说不清的
// HRESULT，而这里能让人在读代码时就看出「这个数不对」。
//
// 数字来自 SDK 的 `um/mftransform.h`、`um/mfobjects.h` 里那些接口的
// `virtual ... = 0` 声明顺序，IUnknown 的三个（QueryInterface / AddRef /
// Release）算在里面。
//
// 这一版把**方法名**也钉上了：只钉「第 n 个方法落在第 n+2 格」的话，
// 少一个方法或者多一个方法都可能自洽；钉了名字，改动就得是「有意为之」。
func TestTheVtableSlotsAreWhereTheSDKPutsThem(t *testing.T) {
	// slotOf 是唯一的换算口径：一个接口自己的第 n 个方法（**从 1 数**）
	// 落在 vtable 的第 n+2 格——前面 3 格是 IUnknown 的。
	slotOf := func(n int) int { return n + 2 }

	t.Run("IMFTransform：23 个方法，名字和下标一一对上", func(t *testing.T) {
		// 顺序照 SDK 的 um/mftransform.h 里 IMFTransform 的声明顺序。
		names := []string{
			"GetStreamLimits", "GetStreamCount", "GetStreamIDs",
			"GetInputStreamInfo", "GetOutputStreamInfo", "GetAttributes",
			"GetInputStreamAttributes", "GetOutputStreamAttributes",
			"DeleteInputStream", "AddInputStreams",
			"GetInputAvailableType", "GetOutputAvailableType",
			"SetInputType", "SetOutputType",
			"GetInputCurrentType", "GetOutputCurrentType",
			"GetInputStatus", "GetOutputStatus", "SetOutputBounds",
			"ProcessEvent", "ProcessMessage", "ProcessInput", "ProcessOutput",
		}
		if len(names) != transformMethodsAfterIUnknown {
			t.Fatalf("方法名单 %d 个，transformMethodsAfterIUnknown = %d",
				len(names), transformMethodsAfterIUnknown)
		}
		// 下标表：方法名 → 槽位。
		got := map[string]int{
			"GetStreamLimits":           vtblTransformGetStreamLimits,
			"GetStreamCount":            vtblTransformGetStreamCount,
			"GetStreamIDs":              vtblTransformGetStreamIDs,
			"GetInputStreamInfo":        vtblTransformGetInputStreamInfo,
			"GetOutputStreamInfo":       vtblTransformGetOutputStreamInfo,
			"GetAttributes":             vtblTransformGetAttributes,
			"GetInputStreamAttributes":  vtblTransformGetInputStreamAttributes,
			"GetOutputStreamAttributes": vtblTransformGetOutputStreamAttrs,
			"DeleteInputStream":         vtblTransformDeleteInputStream,
			"AddInputStreams":           vtblTransformAddInputStreams,
			"GetInputAvailableType":     vtblTransformGetInputAvailableType,
			"GetOutputAvailableType":    vtblTransformGetOutputAvailableType,
			"SetInputType":              vtblTransformSetInputType,
			"SetOutputType":             vtblTransformSetOutputType,
			"GetInputCurrentType":       vtblTransformGetInputCurrentType,
			"GetOutputCurrentType":      vtblTransformGetOutputCurrentType,
			"GetInputStatus":            vtblTransformGetInputStatus,
			"GetOutputStatus":           vtblTransformGetOutputStatus,
			"SetOutputBounds":           vtblTransformSetOutputBounds,
			"ProcessEvent":              vtblTransformProcessEvent,
			"ProcessMessage":            vtblTransformProcessMessage,
			"ProcessInput":              vtblTransformProcessInput,
			"ProcessOutput":             vtblTransformProcessOutput,
		}
		if len(got) != len(names) {
			t.Fatalf("下标表 %d 项，方法名单 %d 项", len(got), len(names))
		}
		for i, name := range names {
			want := slotOf(i + 1)
			if got[name] != want {
				t.Errorf("%s = %d，它是本接口第 %d 个方法，该落在 %d",
					name, got[name], i+1, want)
			}
		}
	})

	t.Run("IMFAttributes", func(t *testing.T) {
		// 本接口 30 个方法。GetItem 是第 1 个，GetUINT32 是第 5 个，
		// GetGUID 是第 8 个，GetCount 是第 28 个，GetItemByIndex 是第 29 个，
		// CopyAllItems 是第 30 个。
		for _, c := range []struct {
			name string
			got  int
			n    int
		}{
			{"GetUINT32", vtblAttributesGetUINT32, 5},
			{"GetGUID", vtblAttributesGetGUID, 8},
			{"GetCount", vtblAttributesGetCount, 28},
			{"GetItemByIndex", vtblAttributesGetItemByIdx, 29},
			// SetUINT32 是第 19 个、SetGUID 是第 22 个、SetBlob 是第 24 个。
			// （SDK 里这几个的顺序是 SetItem、DeleteItem、DeleteAllItems 之后
			// 才轮到 SetUINT32——不是「Set* 一串连着」。）
			{"SetUINT32", vtblAttributesSetUINT32, 19},
			{"SetGUID", vtblAttributesSetGUID, 22},
			{"SetBlob", vtblAttributesSetBlob, 24},
		} {
			if c.got != slotOf(c.n) {
				t.Errorf("%s = %d，它是本接口第 %d 个方法，该落在 %d", c.name, c.got, c.n, slotOf(c.n))
			}
		}
		// CopyAllItems 是最后一个，该落在 32；方法数对不上说明整体抄错了。
		if want := slotOf(attributesMethodsAfterIUnknown); want != 32 {
			t.Errorf("本接口最后一个方法该落在 32，得到 %d——方法数 %d 和 SDK 对不上",
				want, attributesMethodsAfterIUnknown)
		}
	})

	t.Run("IMFSample：前面 38 个槽位是祖先的", func(t *testing.T) {
		// 3（IUnknown）+ 30（IMFAttributes）+ 5（IMFMediaType）= 38。
		const ancestors = 3 + attributesMethodsAfterIUnknown + mediaTypeMethodsAfterIUnknown
		if ancestors != mediaTypeTotalSlotsIncludingIUnk {
			t.Fatalf("祖先占了 %d 个槽位，IMFMediaType 算下来是 %d 个，和 SDK 对不上",
				ancestors, mediaTypeTotalSlotsIncludingIUnk)
		}
		// 本接口 14 个方法：GetSampleFlags、SetSampleFlags、GetSampleTime、
		// SetSampleTime、GetSampleDuration、SetSampleDuration、GetBufferCount、
		// GetBufferByIndex、ConvertToContiguousBuffer、AddBuffer、
		// RemoveBufferByIndex、RemoveAllBuffers、GetTotalLength、CopyToBuffer。
		if want := ancestors + 9 - 1; vtblSampleConvertToContig != want {
			t.Errorf("ConvertToContiguousBuffer = %d，本接口第 9 个方法，该落在 %d",
				vtblSampleConvertToContig, want)
		}
		// AddBuffer 是第 10 个。
		if want := ancestors + 10 - 1; vtblSampleAddBuffer != want {
			t.Errorf("AddBuffer = %d，本接口第 10 个方法，该落在 %d",
				vtblSampleAddBuffer, want)
		}
		// GetBufferCount 是第 7 个，GetBufferByIndex 是第 8 个。
		if want := ancestors + 7 - 1; vtblSampleGetBufferCount != want {
			t.Errorf("GetBufferCount = %d，本接口第 7 个方法，该落在 %d",
				vtblSampleGetBufferCount, want)
		}
		if want := ancestors + 8 - 1; vtblSampleGetBufferByIndex != want {
			t.Errorf("GetBufferByIndex = %d，本接口第 8 个方法，该落在 %d",
				vtblSampleGetBufferByIndex, want)
		}
		if want := ancestors + 13 - 1; vtblSampleGetTotalLength != want {
			t.Errorf("GetTotalLength = %d，本接口第 13 个方法，该落在 %d",
				vtblSampleGetTotalLength, want)
		}
		// CopyToBuffer 是第 14 个，也就是最后一个。
		if want := ancestors + sampleMethodsAfterIUnknown - 1; vtblSampleGetTotalLength+1 != want {
			t.Errorf("最后一个方法（CopyToBuffer）该落在 %d", want)
		}
	})

	t.Run("IMFMediaType：自己的 5 个方法排在 IMFAttributes 之后", func(t *testing.T) {
		// GetMajorType 是本接口第 1 个方法，落在 33（3 + 30 之后）。
		if want := 3 + attributesMethodsAfterIUnknown; vtblMediaTypeGetMajorType != want {
			t.Errorf("GetMajorType = %d，该落在 %d", vtblMediaTypeGetMajorType, want)
		}
		if mediaTypeMethodsAfterIUnknown != 5 {
			t.Errorf("IMFMediaType 一共 %d 个方法，SDK 里是 5 个", mediaTypeMethodsAfterIUnknown)
		}
	})

	t.Run("IMFMediaBuffer：Lock/Unlock 就是头两个", func(t *testing.T) {
		if vtblBufferLock != slotOf(1) {
			t.Errorf("Lock = %d", vtblBufferLock)
		}
		if vtblBufferUnlock != slotOf(2) {
			t.Errorf("Unlock = %d", vtblBufferUnlock)
		}
		// GetCurrentLength 是第 3 个，SetCurrentLength 是第 4 个。
		if vtblBufferGetCurrentLength != slotOf(3) {
			t.Errorf("GetCurrentLength = %d", vtblBufferGetCurrentLength)
		}
		if vtblBufferSetCurrentLength != slotOf(4) {
			t.Errorf("SetCurrentLength = %d", vtblBufferSetCurrentLength)
		}
		// 本接口 5 个方法：Lock、Unlock、GetCurrentLength、SetCurrentLength、
		// GetMaxLength。
		if bufferMethodsAfterIUnknown != 5 {
			t.Errorf("IMFMediaBuffer 一共 %d 个方法，SDK 里是 5 个", bufferMethodsAfterIUnknown)
		}
	})

	t.Run("Release 在 IUnknown 的第三格（0 起数）", func(t *testing.T) {
		// COM 的 vtable 是 **0 起数**：0 = QueryInterface，1 = AddRef，
		// 2 = Release。所以上面那个 slotOf 的公式是 n+2 而不是 n+3——
		// 两边差一位，别混。
		if vtblRelease != 2 {
			t.Errorf("Release = %d", vtblRelease)
		}
	})
}

// 说明为什么「GUID 字符串」和「内存字节」不是一回事——这一条是为了让下一个
// 改这里的人不会把 `String()` 改回 `%08X`。
func TestGUIDStringIsNotTheMemoryByteOrder(t *testing.T) {
	g := mustGUID("5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA")
	// Data1 = 0x5FAEEAE7 在内存里是 E7 EA AE 5F。
	wantMemory := [8]byte{0xE7, 0xEA, 0xAE, 0x5F, 0x90, 0x02, 0x31, 0x4C}
	for i, b := range wantMemory {
		binary := uint8(b)
		if got := byteOf(i, g); got != binary {
			t.Errorf("内存第 %d 字节 = %#02x，want %#02x", i, got, binary)
		}
	}
	if s := g.String(); s != "{5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA}" {
		t.Errorf("String() = %s", s)
	}
}

// byteOf 取一个 GUID 结构的第 i 个内存字节（0 是最前面那一个）。
func byteOf(i int, g guid) byte {
	var b []byte
	b = append(b, byte(g.Data1), byte(g.Data1>>8), byte(g.Data1>>16), byte(g.Data1>>24))
	b = append(b, byte(g.Data2), byte(g.Data2>>8), byte(g.Data3), byte(g.Data3>>8))
	b = append(b, g.Data4[:]...)
	if i < 0 || i >= len(b) {
		panic(fmt.Sprintf("byteOf: %d is out of range", i))
	}
	return b[i]
}
