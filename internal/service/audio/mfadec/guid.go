package mfadec

import (
	"fmt"
	"strconv"
	"strings"
)

// Media Foundation 的常量，全部来自 Windows SDK 的 `um/mfapi.h` 和
// `um/mfidl.h`（本文每个常量上面都写了它在那两个文件里的名字）。
//
// # 为什么要写成一个字符串再解析，而不是直接摆 uint32
//
// 这一整层**只能在 Windows 真机上验**，而 GUID 写错的后果不是崩——
// `GetUINT32` 拿着一个不存在的键去问，返回 `E_INVALIDARG` 或者干脆
// 「这个属性不存在」，两种都长得像「这台机器上解不了 AAC」。一个手抄错的
// 十六进制位数会安静地毁掉整条链路，而且排查它要花的时间比写它多得多。
//
// 所以这里把每个常量写成**规范里那串 GUID 原文**，再解析成 16 字节：
// 抄错的话至少是源码里那串东西和 SDK 对不上，肉眼一秒能看出来，
// `guid_test.go` 还能在 macOS 上把每一串都对着 SDK 的字节核一遍。
//
// MF 的 `MF_MT_*` 属性键**不是** `PROPERTYKEY`——`IMFAttributes::GetItem`
// 收的是 `REFGUID`，没有 pid。下面这些就是完整的 GUID，没有第四列。

// guid 是 Windows 的 GUID：16 字节，小端。
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// parseGUID 解析 `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` 这样的写法。
//
// ⚠️ **前四组按书写顺序读，第五组按字节读**——这是 GUID 结构体的实际约定：
// `Data1`/`Data2`/`Data3` 在内存里是小端整数，文本里却是正常书写顺序；
// `Data4` 是 8 个字节，文本里逐个照抄。`5FAEEAE7-0290-…` 存进内存是
// `E7 EA AE 5F 90 02 …`，所以按字节读会得到一个**完全合法、却谁都不是**
// 的 GUID，而且它和从 SDK 头文件真读出来的那个永远比不相等。
//
// ⚠️ 大写小写都收，两边的花括号收不收都收——SDK 的注释里两种写法都有，
// 抄进来的时候不必先统一。
func parseGUID(s string) (guid, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return guid{}, fmt.Errorf("mfadec: %q is not a GUID: want 5 dash-separated groups, got %d", s, len(parts))
	}
	// 每一组的十六进制位数：4-2-2-2-6 字节。
	widths := [5]int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != widths[i] {
			return guid{}, fmt.Errorf("mfadec: %q is not a GUID: group %d has %d hex digits, want %d",
				s, i+1, len(p), widths[i])
		}
	}

	var g guid
	var err error
	d1, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return guid{}, fmt.Errorf("mfadec: %q is not a GUID: %w", s, err)
	}
	g.Data1 = uint32(d1)
	u16 := func(p string) (uint16, error) {
		v, err := strconv.ParseUint(p, 16, 16)
		return uint16(v), err
	}
	if g.Data2, err = u16(parts[1]); err != nil {
		return guid{}, fmt.Errorf("mfadec: %q is not a GUID: %w", s, err)
	}
	if g.Data3, err = u16(parts[2]); err != nil {
		return guid{}, fmt.Errorf("mfadec: %q is not a GUID: %w", s, err)
	}
	// 第 4 组是 Data4 的头两个字节，第 5 组是后 6 个，两组都逐字节照抄。
	hexBytes := func(p string, dst []byte) error {
		for j := 0; j < len(dst); j++ {
			v, err := strconv.ParseUint(p[j*2:j*2+2], 16, 8)
			if err != nil {
				return fmt.Errorf("mfadec: %q is not a GUID: %w", s, err)
			}
			dst[j] = byte(v)
		}
		return nil
	}
	if err := hexBytes(parts[3], g.Data4[0:2]); err != nil {
		return guid{}, err
	}
	if err := hexBytes(parts[4], g.Data4[2:8]); err != nil {
		return guid{}, err
	}
	return g, nil
}

// mustGUID 是包级常量用的版本：写错了就在 init 阶段炸，不是在解码的时候炸。
func mustGUID(s string) guid {
	g, err := parseGUID(s)
	if err != nil {
		panic(err)
	}
	return g
}

// String 把 GUID 按 Windows 惯例打印成大写带花括号的样子。
//
// ⚠️ 这里印的是**规范里的书写顺序**，不是内存布局。Data1/Data2/Data3 在内存里
// 是小端存的，`0x5FAEEAE7` 躺在内存里是 `E7 EA AE 5F`。所以
// `%08X-%04X-%04X` 才是对的写法，逐字节印会得到 `E7EAAE5F`——一个看上去
// 完全合法、实际上和 SDK 里的那串对不上的 GUID。
//
// （和它对着干的坑在 `parseGUID` 那一段：从**文本**进来要走书写顺序，
// 往 COM 那边**交出去**的是内存里的那 16 个字节。）
func (g guid) String() string {
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// equal 报两个 GUID 是不是同一个（Windows 的 IsEqualGUID 逐字节比）。
func (g guid) equal(o guid) bool {
	return g == o
}

// ── 媒体类型的大类（SDK: MFMediaType_Audio / MFMediaType_Default）───────────

var (
	// guidMediaTypeAudio 是 `MFMediaType_Audio`，四个 ASCII 字符 'saud'
	// 加上 KSDATAFORMAT 那一段固定尾巴。
	//
	// 它是**大**类型：输入和输出两个媒体类型的大类都填它。
	guidMediaTypeAudio = mustGUID("73647561-0000-0010-8000-00AA00389B71")
)

// ── 媒体类型里的属性键（SDK: um/mfapi.h，注释里都带 `MF_MT_` 前缀）──────────

var (
	// `MF_MT_SUBTYPE`：这个媒体类型到底是什么编码。
	guidMTSubtype = mustGUID("F7E34C9A-42E8-4714-B74B-CB29D72C35E5")

	// `MF_MT_MAJOR_TYPE`：大类型，音频恒为 `MFMediaType_Audio`。
	guidMTMajorType = mustGUID("48EBA18E-F8C9-4687-BF11-0A74C9F96A8F")

	// `MF_MT_AUDIO_NUM_CHANNELS`：声道数。
	//
	// ⚠️ 文档里叫 `MF_MT_AUDIO_CHANNEL_COUNT` 的是**别名**，SDK 头文件里
	// 只有 `MF_MT_AUDIO_NUM_CHANNELS` 这一条 GUID。写哪个名字不影响值。
	guidMTAudioNumChannels = mustGUID("37E48BF5-645E-4C5B-89DE-ADA9E29B696A")

	// `MF_MT_AUDIO_SAMPLES_PER_SECOND`：采样率。
	//
	// ⚠️ 同上：文档里的 `MF_MT_SAMPLERATE` 是这条的别名，SDK 头文件里
	// 没有那个名字。
	guidMTAudioSamplesPerSecond = mustGUID("5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA")

	// `MF_MT_AUDIO_BITS_PER_SAMPLE`：一个样本几位。
	guidMTAudioBitsPerSample = mustGUID("F2DEB57F-40FA-4764-AA33-ED4F2D1FF669")

	// `MF_MT_AUDIO_AVG_BYTES_PER_SECOND`：平均字节率，诊断用。
	guidMTAudioAvgBytesPerSecond = mustGUID("1AAB75C8-CFEF-451C-AB95-AC034B8E1731")

	// `MF_MT_AUDIO_SAMPLES_PER_BLOCK`：压缩帧里装几个样本。
	// AAC 恒为 1024，spike 拿它对一下就知道 MF 给的块是不是 1024。
	guidMTAudioSamplesPerBlock = mustGUID("AAB15AAC-E13A-4995-9222-501EA15C6877")

	// `MF_MT_AUDIO_BLOCK_ALIGNMENT`：一个 PCM 帧占几个字节。
	// 16 位立体声是 4。
	guidMTAudioBlockAlignment = mustGUID("322DE230-9EEB-43BD-AB7A-FF412251541D")

	// `MF_MT_AUDIO_CHANNEL_MASK`：声道在哪些扬声器位置。
	// 立体声是 `SPEAKER_FRONT_LEFT | SPEAKER_FRONT_RIGHT` = 0x3。
	guidMTAudioChannelMask = mustGUID("55FB5765-644A-4CAF-8479-938983BB1588")

	// `MF_MT_AAC_PAYLOAD_TYPE`：码流里装的是什么。
	//
	// 0 = 裸 AAC（只有 raw_data_block，一个样本一帧，B 站 .m4a 就是它）；
	// 1 = ADTS（每个样本前面带 ADTS 头，.aac 文件是它）。
	// 不设时默认 0。
	guidMTAACPayloadType = mustGUID("BFBABE79-7434-4D1C-94F0-72A3B9E17188")

	// `MF_MT_AAC_AUDIO_PROFILE_LEVEL_INDICATION`：profile 与 level，
	// 取 ISO/IEC 14496-3 里的 `audioProfileLevelIndication`。
	// 不知道就填 0 或 0xFE（"没指定"）。
	guidMTAACProfileLevelIndication = mustGUID("7632F0E6-9538-4D61-ACDA-EA29C8C14456")

	// `MF_MT_USER_DATA`：附加的格式信息，一个 blob。
	//
	// 对 `MFAudioFormat_AAC` 来说，这里放的是 **`HEAACWAVEINFO` 去掉
	// `WAVEFORMATEX` 之后的那 12 字节**，紧跟着 **AudioSpecificConfig()**。
	// 少这一段，AAC 解码器不认这个媒体类型（它要靠 ASC 知道采样率和声道）。
	guidMTUserData = mustGUID("B6BC765F-4C3B-40A4-BD51-2535B66FE09D")
)

// ── 音频子类型（SDK: `DEFINE_MEDIATYPE_GUID`，值就是 WAVEFORMATEX 的 wFormatTag）──

var (
	// `MFAudioFormat_Base`：所有「由 wFormatTag 推出来」的音频子类型的共同前缀。
	guidAudioFormatBase = mustGUID("00000000-0000-0010-8000-00AA00389B71")
	// `MFAudioFormat_PCM` = wFormatTag 1，16 位定点整数 PCM。
	guidAudioFormatPCM = mustGUID("00000001-0000-0010-8000-00AA00389B71")
	// `MFAudioFormat_Float` = wFormatTag 3，IEEE 浮点。
	//
	// ⚠️ **它叫 `Float` 不叫 `IEEE_FLOAT`**——SDK 里是 `MFAudioFormat_Float`，
	// 按 `MFAudioFormat_IEEE_FLOAT` 去 mfapi.h 里 grep 是找不到的。
	guidAudioFormatFloat = mustGUID("00000003-0000-0010-8000-00AA00389B71")
	// `MFAudioFormat_AAC` = wFormatTag **0x1610**（`WAVE_FORMAT_MPEG_HEAAC`）。
	//
	// ⚠️⚠️ **这里写错过一次，而且是我自己的测试盖不住的**，记下来别再踩：
	// 头文件里这三行是
	//
	//	DEFINE_MEDIATYPE_GUID( MFAudioFormat_AAC,  WAVE_FORMAT_MPEG_HEAAC );
	//	DEFINE_MEDIATYPE_GUID( MFAudioFormat_ADTS, WAVE_FORMAT_MPEG_ADTS_AAC );
	//	DEFINE_MEDIATYPE_GUID( MFAudioFormat_Base, 0x00000000 );
	//
	// 宏把**名字**展开成 GUID，而 `WAVE_FORMAT_MPEG_HEAAC` 本身在
	// `ksmedia.h` 里是 `0x1610`（`KSDATAFORMAT_SUBTYPE_MPEG_HEAAC`），
	// **不是** `0x1600`。`0x1600` 是 `WAVE_FORMAT_MPEG_ADTS_AAC`，
	// `0x1601` 是 `WAVE_FORMAT_MPEG_RAW_AAC`。
	//
	// 也就是说把宏的参数**按位置猜**（「第一个 0x16xx 一定是 AAC」）会整体错位一格，
	// 而且**测试也是绿的**——因为测试拿的是同一个错误假设去断言。
	// 「测过了」和「测对了」是两件事。
	guidAudioFormatAAC = mustGUID("00001610-0000-0010-8000-00AA00389B71")
	// `MFAudioFormat_ADTS` = wFormatTag 0x1600（`WAVE_FORMAT_MPEG_ADTS_AAC`），
	// 即**带 ADTS 头**的 AAC。
	guidAudioFormatADTS = mustGUID("00001600-0000-0010-8000-00AA00389B71")
	// `MFAudioFormat_RawAAC` = wFormatTag 0x1601（`WAVE_FORMAT_MPEG_RAW_AAC`），
	// 即**裸** AAC（没有 ADTS 头，靠带外配置）。
	//
	// 它和上面那个的关系就是这一对最容易搞混的：
	// B 站的 .m4a 里是**裸** AAC 帧，`.aac` 文件里是带 ADTS 头的。
	guidAudioFormatRawAAC = mustGUID("00001601-0000-0010-8000-00AA00389B71")
)

// ── 类与接口的标识 ──────────────────────────────────────────────────────────

var (
	// guidCLSIDCMSAACDecMFT 是 `CLSID_CMSAACDecMFT`：Windows 自带的 AAC
	// 解码 MFT。
	//
	// 出处是 SDK 的 `um/wmcodecdsp.h`：
	//
	//	EXTERN_C const CLSID CLSID_CMSAACDecMFT;
	//	class DECLSPEC_UUID("32d186a7-218f-4c75-8876-dd77273a8999")
	//	CMSAACDecMFT;
	//
	// 实现它的 DLL：Windows 7 是 `Msmpeg2adec.dll`，Windows 8 之后是
	// `MSAudDecMFT.dll`（`windows-2022` runner 上是后者）。
	//
	// 它是**系统组件**，不是第三方库：不静态链接进我们的二进制，
	// 没有许可证要随包发出去。见 `internal/licensing` 里的说明。
	guidCLSIDCMSAACDecMFT = mustGUID("32D186A7-218F-4C75-8876-DD77273A8999")

	// guidIIDIMFTransform 是 `IID_IMFTransform`，在 SDK 的 `um/mftransform.h`。
	//
	// ⚠️ **SDK 里只有这一个 `IMFTransform`**，不要去记 DirectShow 那套
	// 「`ProcessInput` 收 `IMFMediaBuffer*`」的旧形状——那个形状在
	// 现在的 SDK 头文件里**不存在**。现在这个的 `ProcessInput` 收
	// `IMFSample*`，`ProcessOutput` 收 `MFT_OUTPUT_DATA_BUFFER*`。
	// 两者的下标表完全不同，抄错就是「调到隔壁那个方法去了」。
	guidIIDIMFTransform = mustGUID("BF94C121-5B05-4E6F-8000-BA598961414D")
)

// ── COM vtable 的下标 ───────────────────────────────────────────────────────
//
// 下标是**接口头文件里方法的声明顺序**，前面三个（QueryInterface / AddRef /
// Release）属于 IUnknown，一起数进去。顺序照 SDK 的 `um/mftransform.h` 和
// `um/mfobjects.h` 抄——
// 抄错的话最常见的表现是「调到隔壁那个方法去了」，而隔壁那个的参数个数
// 还可能刚好一样，于是全程不报错、只是行为完全不对。
//
// `guid_test.go` 把这几个接口的方法名单按顺序钉在仓库里，改错下标会红。

// IMFTransform（um/mftransform.h）：前 3 个是 IUnknown 的，本接口 23 个。
const (
	vtblTransformGetStreamLimits          = 3
	vtblTransformGetStreamCount           = 4
	vtblTransformGetStreamIDs             = 5
	vtblTransformGetInputStreamInfo       = 6
	vtblTransformGetOutputStreamInfo      = 7
	vtblTransformGetAttributes            = 8
	vtblTransformGetInputStreamAttributes = 9
	vtblTransformGetOutputStreamAttrs     = 10
	vtblTransformDeleteInputStream        = 11
	vtblTransformAddInputStreams          = 12
	vtblTransformGetInputAvailableType    = 13
	vtblTransformGetOutputAvailableType   = 14
	vtblTransformSetInputType             = 15
	vtblTransformSetOutputType            = 16
	vtblTransformGetInputCurrentType      = 17
	vtblTransformGetOutputCurrentType     = 18
	vtblTransformGetInputStatus           = 19
	vtblTransformGetOutputStatus          = 20
	vtblTransformSetOutputBounds          = 21
	vtblTransformProcessEvent             = 22
	vtblTransformProcessMessage           = 23
	vtblTransformProcessInput             = 24
	vtblTransformProcessOutput            = 25
	transformMethodsAfterIUnknown         = 23
)

// IMFAttributes（um/mfobjects.h）：前 3 个是 IUnknown 的，本接口 30 个。
const (
	vtblAttributesGetUINT32        = 7
	vtblAttributesGetGUID          = 10
	vtblAttributesGetCount         = 30
	vtblAttributesGetItemByIdx     = 31
	vtblAttributesSetUINT32        = 21
	vtblAttributesSetGUID          = 24
	vtblAttributesSetBlob          = 26
	attributesMethodsAfterIUnknown = 30
)

// IMFMediaType（um/mfobjects.h）：它**继承** IMFAttributes，所以前面那 33 个
// （3 + 30）是祖先的，一个都不能跳。本接口自己 5 个——所以它的虚表一共
// 38 格。
//
// 本包用不到 IMFMediaType 自己的方法（`GetMajorType` 那些），但槽位数得
// 记在这儿。
const (
	vtblMediaTypeGetMajorType        = 33
	mediaTypeMethodsAfterIUnknown    = 5
	mediaTypeTotalSlotsIncludingIUnk = 38
)

// IMFSample（um/mfobjects.h）：它**继承** IMFAttributes——**不是** IMFMediaType。
// 所以前面那 33 个（3 + 30）是祖先的，一个都不能跳，本接口自己 14 个，
// 虚表一共 47 格。
//
// ⚠️⚠️ **这里写错过一次（BMG-68 传下来的），而且它自己的测试是绿的。**
// 原来以为 IMFSample 继承 IMFMediaType，按「38 个祖先槽位」算，得出
// AddBuffer = 47、GetBufferByIndex = 45。
//
// **真相**：微软的 `IMFSample` 文档白纸黑字写着「The IMFSample interface
// inherits from IMFAttributes」，SDK 的 `IMFSampleVtbl` 结构体也数得清清楚楚：
// 47 格，33~46 是它自己的 14 个方法。
//
// 错成 47 的后果**不是报错，是崩**：那张虚表只有 47 格（0~46），
// 读第 47 格是**越过数组末尾**，拿到的是一串垃圾，当函数指针调下去就是崩。
// 错成 45 的更阴：`GetBufferByIndex` 那一格其实是 `GetTotalLength`——
// 返回一个 DWORD，被当指针用。
//
// 教训：**「测过了」和「测对了」是两件事**。断言值和被测值来自同一个错误
// 假设的时候，测试一定是绿的——所以下标要对**头文件里的虚表结构体**核，
// 而不是对着「继承链」推。
const (
	vtblSampleGetBufferCount      = 39
	vtblSampleGetBufferByIndex    = 40
	vtblSampleConvertToContig     = 41
	vtblSampleAddBuffer           = 42
	vtblSampleGetTotalLength      = 45
	sampleMethodsAfterIUnknown    = 14
	sampleTotalSlotsIncludingIUnk = 47
)

// IMFMediaBuffer（um/mfobjects.h）：前 3 个是 IUnknown 的，本接口 5 个。
const (
	vtblBufferLock             = 3
	vtblBufferUnlock           = 4
	vtblBufferGetCurrentLength = 5
	vtblBufferSetCurrentLength = 6
	bufferMethodsAfterIUnknown = 5
)

// vtblRelease 是所有 COM 接口的第三个槽位，Release。
const vtblRelease = 2
