// MP4（ISO BMFF）顶层盒子的走法。
//
// # 它只为一件事存在
//
// **「`moov` 盒在文件末尾」这件事，值不值得为它多发一条 Range 请求。**
//
// B 站给的完整单文件 `.m4a`（音乐库 sid 那一路）和 DASH 音频文件都是
// `ftyp` / `free` / `mdat` / **`moov`（在末尾）** 这个形状。而
// AudioToolbox 探 MP4 容器时要**扫过所有顶层盒子**才找到 `moov`——
// `moov` 在末尾时它得扫到文件末尾，于是「解码第一帧」那一段的耗时
// **等于整个文件下载完的耗时**（票 #220 的实测：200 KB/s 的网上 1.22 秒，
// 50 KB/s 的网上 5.02 秒）。
//
// # 为什么这一层是「读盒子的头」而不是「解 MP4」
//
// 我们要的**不是**解出来，只是**知道 `moov` 在第几个字节**——
// 那样才能发一条 `Range: bytes=<moov 起点>-` 把那一小段单独取回来。
// 盒子头是定长的（8 字节，特殊的 16 字节），读它不需要解任何东西。
//
// # 走法
//
// 顶层盒子的头是「大端 4 字节长度 + 4 字节类型」：
//
//	长度 == 1	真正的长度在头后面那 8 个字节里（64 位）
//	长度 == 0	这个盒子一直到文件末尾
//	其它	长度就是它自己那么大（**含头**）
//
// 走的时候只关心三件事：`moov` 在哪、`mdat` 在哪、有没有 `moof`
// （有 `moof` 就是分片流，那种流压根没有末尾那个 `moov`）。

package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// mp4TopLevel 的两个「不用取尾部」的返回值之外的错误。
var (
	// errMP4NotOurShape 是「这个文件不用为末尾那个 `moov` 操心」。
	//
	// 它涵盖三种情况：不是 MP4（FLAC / MP3 / ADTS）、分片流（有 `moof`，
	// 而 `moov` 在另一个初始化段里）、以及 `moov` 就在文件前面
	// （faststart——那种文件不用等整个文件下完，票 #220 的对照组）。
	errMP4NotOurShape = errors.New("audio: 这个文件不用取尾部")
)

// headNotReady 是「这一段文件头还没下到」。
//
// ⚠️ **它带着「等哪一段」**：调用方要**阻塞**等顺序下载追上那个偏移，
// 而不是**空转**重试——空转会把一个 CPU 核占满，直到下载追上
// （2026-10-25 的评审实测：墙钟 6.1 秒里 CPU 占了 5.8 秒，≈0.95 个核，
// 而那段时间里源唯一在做的事就是「等字节」）。
//
// ⚠️ **为什么会有这种情况**：盒子头不一定在前 512 字节里——一个 20 MB 的
// `free`/`sidx` 盒子排在 `mdat`/`moov` 前面时，`mdat` 的头在好几兆之外。
type headNotReady struct {
	// need 是「要读到第几个字节才够」。等顺序下载的 `filled` 追上它就行。
	need int64
}

func (e headNotReady) Error() string {
	return fmt.Sprintf("audio: 文件头还没下到（要等第 %d 字节）", e.need)
}

// needOffset 从一个错误里取出「要等第几个字节」。
//
// 不是一个错误 → 返回 (0, false)。
func needOffset(err error) (int64, bool) {
	var h headNotReady
	if errors.As(err, &h) {
		return h.need, true
	}
	return 0, false
}

// mp4MaxBoxes 是「走顶层盒子」最多走多少个。
//
// 一个正常的 MP4 只有三四个盒子。给 64 个是防畸形文件（盒子里写着乱七八糟
// 的长度）把 CPU 吃光——而这一层是**每个流都要走一遍**的。
const mp4MaxBoxes = 64

// mp4FirstBoxTypes 是「第一个盒子可以是什么」。
//
// ⚠️ **必须限死**：这一层看的是文件的头四个字节，而头四个字节是**随便什么
// 都可能**的——FLAC 是 `fLaC`、MP3 是 `ID3`、裸 ADTS 是同步字 `0xFFF1`。
// 不限死的话，一个 FLAC 文件的头会被当成「长度乱七八糟的盒子」，
// 然后得出一个错的结论（而那个结论会让人多发一条 Range 请求）。
var mp4FirstBoxTypes = map[string]bool{
	"ftyp": true, // 文件类型盒（正常封装）
	"styp": true, // 分片类型盒（DASH 那种）
	"moov": true, // 直接以 `moov` 开头（faststart）
	"mdat": true, // 直接以 `mdat` 开头（少见，但合法）
	"free": true, // 填充
	"skip": true, // 填充（老名字）
	"wide": true, // 填充
	"uuid": true, // 扩展
	"sidx": true, // 索引
	"pdin": true, // 逐帧的 MPEG-4 描述
	"emsg": true, // 事件消息
}

// mp4TopLevel 走一遍顶层盒子，找 `moov` 在哪。
//
// 返回值：
//
//	moovAt >= 0	`moov` 在这个偏移——它在文件前面，**不用**取尾部
//	tailAt >= 0	`moov` 在文件末尾，尾部那一段从 tailAt 开始
//	errMP4NotOurShape	不用取尾部
//	headNotReady	文件头还没下到（**带着要等第几个字节**——调用方要
//			**阻塞**等它，而不是空转重试）
//
// r 是「只读已经下到的那部分」的读者——它读不到还没下的字节时必须报
// `headNotReady`（**不是** io.EOF：io.EOF 在这一层是「文件到头了」，
// 而「还没下到」是另一回事，混起来会把「再等等」说成「不用管了」）。
//
// size 是整个文件有多长。**-1 是「还不知道」**（服务器没给
// Content-Length）——那时的判据松一点：末尾那一段照样取（服务器会对
// 「越界的 Range」回 416，而那一条路是安全的，见 `fetchTail`）。
func mp4TopLevel(r io.ReaderAt, size int64) (moovAt, tailAt int64, err error) {
	moovAt, tailAt = -1, -1
	off := int64(0)
	for i := 0; i < mp4MaxBoxes; i++ {
		var hdr [16]byte
		n, rerr := r.ReadAt(hdr[:8], off)
		if rerr != nil {
			if need, ok := needOffset(rerr); ok {
				return -1, -1, headNotReady{need: need}
			}
			return -1, -1, errMP4NotOurShape
		}
		if n < 8 {
			return -1, -1, errMP4NotOurShape
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[:4]))
		typ := string(hdr[4:8])
		head := int64(8)

		switch boxSize {
		case 0:
			// 这个盒子一直到文件末尾。
			if size < 0 {
				// 总长都不知道 → 算不出它在哪儿 → 不用取尾部。
				return -1, -1, errMP4NotOurShape
			}
			boxSize = size - off
		case 1:
			// 真正的长度在头后面那 8 个字节里（64 位大端）。
			n, rerr = r.ReadAt(hdr[8:16], off+8)
			if rerr != nil {
				if need, ok := needOffset(rerr); ok {
					return -1, -1, headNotReady{need: need}
				}
				return -1, -1, errMP4NotOurShape
			}
			if n < 8 {
				return -1, -1, errMP4NotOurShape
			}
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16]))
			head = 16
		}

		if i == 0 && !mp4FirstBoxTypes[typ] {
			return -1, -1, errMP4NotOurShape
		}

		switch typ {
		case "moov":
			// `moov` 在前面（faststart）：AudioToolbox 扫到它就停，
			// **不用**等整个文件下完。
			return off, -1, nil
		case "moof":
			// 分片流（DASH）：`moov` 在另一个初始化段里，这个文件里没有。
			return -1, -1, errMP4NotOurShape
		case "mdat":
			if boxSize < head {
				return -1, -1, errMP4NotOurShape
			}
			end := off + boxSize
			if size >= 0 && end >= size {
				// `mdat` 一直到文件末尾——`moov` 只可能在它前面，
				// 而那上面已经走过了（没看到 `moov` 就是没有）。
				return -1, -1, errMP4NotOurShape
			}
			// `mdat` 后面那一段就是 `moov`（外加可能的 `free` 之类）。
			// 那一段通常只有几 KB——单独取回来，AudioToolbox 就不用
			// 等整个音频下完才开口。
			return -1, end, nil
		}

		if boxSize < head {
			return -1, -1, errMP4NotOurShape
		}
		off += boxSize
		if size >= 0 && off >= size {
			return -1, -1, errMP4NotOurShape
		}
	}
	return -1, -1, errMP4NotOurShape
}
