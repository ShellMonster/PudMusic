//go:build darwin

package extaf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	mygoaudio "github.com/egoist/mygo/audio"
)

// 交给我们 PCM 的那层结构。三个结构的字段顺序和每一步填充都是实测出来的，
// 写错不会崩，只会一直返回 0 帧、状态码 0——和「文件读完了」一模一样。

// audioStreamBasicDescription 是 macOS 上的 AudioStreamBasicDescription，40 字节。
//
// ⚠️ macOS 这一版**没有** mChannelLayout 字段——那是 iOS/tvOS 的版本。两个平台
// 的结构大小不一样（40 vs 48），照抄 iOS 的定义会在 BytesPerFrame 之后整体错位。
type audioStreamBasicDescription struct {
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

// audioBuffer 是 CoreAudio 的 AudioBuffer，16 字节。
//
// ⚠️ 字段顺序是 {mNumberChannels, mDataByteSize, mData}。流传最广的
// {mNumberChannels, mData, mDataByteSize} 是错的（那是渲染回调文档里的顺序）。
// 写错的后果是 AudioToolbox 把指针当成字节数、认为缓冲区装不下，返回 0 帧
// 且状态码为 0，和「读到文件末尾」无法区分。
type audioBuffer struct {
	NumberChannels uint32
	ByteSize       uint32
	Data           uintptr
}

// audioBufferList 是 CoreAudio 的 AudioBufferList，24 字节。
//
// mBuffers 从偏移 **8** 开始，不是 4：AudioBuffer 里带一个指针，整个结构按
// 8 字节对齐，C 编译器在 mNumberBuffers 后面塞了 4 字节填充。这里是拿 cgo
// 打印过 offsetof(mBuffers) == 8、sizeof(AudioBufferList) == 24 才敢这么写的，
// 猜是猜不出来的。
type audioBufferList struct {
	NumberBuffers uint32
	_             uint32 // 对齐填充。删掉它，后面每个字段都会读错
	Buffers       [1]audioBuffer
}

// errClosed 是关掉之后再用这个解码器。
var errClosed = errors.New("extaf: decoder is closed")

// ErrSeekRange 是 SetPosition 落在 [0, Len()] 之外。
var ErrSeekRange = errors.New("extaf: seek out of range")

// Decoder 是一个 ExtAudioFileRef 的解码器，实现 MyGo 的 audio.Decoder。
//
// 和 MyGo 驱动它的方式配套：Read 和 SetPosition 只会从**同一个** goroutine 上
// 被调（audio.stream 的 decode goroutine），所以这里不需要自己加锁。
type Decoder struct {
	handle uintptr
	file   uintptr // AudioFileID
	ef     uintptr // ExtAudioFileRef，包着上面那个
	ch     int
	rate   int
	total  int64

	raw  []byte // ExtAudioFileRead 落 PCM 的地方
	done bool   // 已经 Close 过
}

var (
	_ mygoaudio.Decoder = (*Decoder)(nil)
	_ io.Closer         = (*Decoder)(nil)
)

// source 是喂给 AudioToolbox 的字节源。
//
// 本阶段只喂本地文件，但接口是「一个能 seek 的字节流」，不是「一个路径」——
// 后面接网络流时换掉的是这一层，ExtAudioFile 那一整条链不用动。
type source struct {
	r    io.ReadSeeker
	size int64

	// err 是字节源身上出的**第一条**真错（比如网络断了）。
	//
	// ⚠️ **「真错」不含 io.EOF 和 io.ErrUnexpectedEOF**：那两个是「流到头了」，
	// 不是失败（`noteReadError` 那一段写着为什么）。所以这一格记的是**硬错**。
	//
	// 为什么需要它：AudioToolbox 的读回调只有「要到了几个字节」这一个出口，
	// 读失败在回调里只能变成 0，而 0 在它眼里和「干净地读到末尾」是**同一个
	// 东西**。不把它留一份，一次网络断开就会表现成「歌播完了」——
	// 界面上什么提示都没有，用户以为歌放完了。
	//
	// 回调跑在 AudioToolbox 自己的线程上，而 Read 跑在解码 goroutine 上，
	// 所以这里要加锁（Seek 那边本来就在阻塞等，加锁的代价可以忽略）。
	mu  sync.Mutex
	err error
}

// noteReadError 记下第一次真错。读到字节源末尾不算错——那是读完。
//
// ⚠️ **io.ErrUnexpectedEOF 也要算「读完」**：io.ReadFull 读到了字节、只是没凑齐
// 被要的那么多就撞上了 EOF，于是交出这个错。它不是失败，是「流到头了」。
// 以前这里只过滤 io.EOF（一个字节都没读到时 io.ReadFull 给的就是它），
// 于是「末尾短读」被当成了硬错误——而 AudioToolbox 第一次读就要 8192 字节，
// **总长不足 8192 字节**的文件（秒开片段、缓存碎片、测试素材）因此一个样本
// 都解不出来，界面上的表现是「这歌放不出来」（#312）。
func (s *source) noteReadError(err error) {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return
	}
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// readErr 返回记下的那个错。
func (s *source) readErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// readAt 把 [position, position+n) 交给 dst，返回真的读到的字节数。
//
// ⚠️ 短读只可能意味着「字节源到头了」：io.ReadFull 会一直读到凑齐 n 字节，
// 只有字节源真的没有了才交出少于 n 的字节。中途短读是要出事的：实测
// ExtAudioFileRead 碰到中途短读一律返回 -50，整个解码就断了
// （`verify-streaming.md` §3.3）。所以字节源必须阻塞到凑齐为止；本地文件
// 天然凑得齐，将来接网络流时「等下载追上」的责任也在这一层。
//
// 「到头了」有两个出口，**都要当成读完，不能记成错**：
//
//	读到 0 字节       → io.ReadFull 给 io.EOF
//	读到 1..n-1 字节  → io.ReadFull 给 io.ErrUnexpectedEOF（没凑齐就到了头）
//
// ⚠️ **上界是 n-1，n 是这一次要的那个数**——不是某个写死的字节数（实测
// AudioToolbox 请求过 11288 字节，所以「8191」那个上界是错的）。
//
// AudioToolbox 的读回调只有一个出口（「这次要到了几个字节」），它把「少于
// 我要的」当成流到头了——所以短读正是要告诉它的话。noteReadError 负责把这
// 两种都归到「读完」那一边。
func (s *source) readAt(position int64, n int, dst []byte) int {
	if position < 0 {
		return 0
	}
	if _, err := s.r.Seek(position, io.SeekStart); err != nil {
		s.noteReadError(err)
		return 0
	}
	// ⚠️ 这个 err 以前被丢掉了（`got, _ :=`）。本地文件读不出错，所以丢
	// 掉无所谓；接了网络流之后它是**唯一**能告诉上层「歌断了」的东西。
	got, err := io.ReadFull(s.r, dst[:n])
	s.noteReadError(err)
	return got
}

// 字节源的句柄表。传给 C 的是整数句柄而不是 Go 指针：AudioToolbox 会把
// clientData 一直留着到 Dispose，Go 堆对象会不会搬家不该拿来赌。
var (
	sourcesMu sync.Mutex
	sources   = map[uintptr]*source{}
	handleSeq uintptr
)

func registerSource(s *source) uintptr {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	handleSeq++
	sources[handleSeq] = s
	return handleSeq
}

// LiveSources 报告还有几个字节源挂在表里，也就是还有几个解码器没关。
//
// 正常情况下，任何一个 Decoder 关掉之后这个数就该回到 0。这个函数是给
// 「切歌切到一半内存涨了」这类问题排查用的——AudioToolbox 的 AudioFileID /
// ExtAudioFileRef 不是文件描述符，/dev/fd 数不出来，只有这里能看见。
func LiveSources() int {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	return len(sources)
}

func sourceFor(h uintptr) *source {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	return sources[h]
}

func unregisterSource(h uintptr) {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	delete(sources, h)
}

// 两个 C 回调，全项目只创建一次。
//
// purego 的 NewCallback 会占一个永久的跳板，数量有限（2000 个），每建一个
// Decoder 就建一次迟早会用光。包级变量在 init 阶段建好，跟着进程活。
//
// C 传来的指针参数一律声明成 unsafe.Pointer 而不是 uintptr：值是一样的，
// 但后一种写法要靠 unsafe.Pointer(uintptr) 转回来，go vet 的 unsafeptr 会拦。
//
// ⚠️ macOS 上 getSize 的签名和 iOS 不一样：**直接返回 SInt64 的长度**，
// 不是返回 OSStatus 再用出参把长度写出来（iOS 是后者）。照抄 iOS 的写法，
// AudioToolbox 拿到的长度就是返回的状态码，文件被当成 0 字节。
var (
	readProc = purego.NewCallback(
		func(clientData uintptr, position int64, requestCount uint32, buffer, actualCount unsafe.Pointer) OSStatus {
			s := sourceFor(clientData)
			if s == nil {
				return -50 // paramErr：句柄没了，AudioToolbox 也该停
			}
			n := s.readAt(position, int(requestCount), unsafe.Slice((*byte)(buffer), requestCount))
			*(*uint32)(actualCount) = uint32(n)
			return noErr
		})

	getSizeProc = purego.NewCallback(
		func(clientData uintptr) int64 {
			s := sourceFor(clientData)
			if s == nil {
				return 0
			}
			return s.size
		})
)

// Open 打开一个音频文件，返回按 MyGo 的 audio.Decoder 接口解出 float32 的解码器。
//
// 链路是：字节源（阻塞、绝对定位）→ AudioFileOpenWithCallbacks → AudioFileID
// → ExtAudioFileWrapAudioFileID → 设客户端格式成交织 float32 → ExtAudioFileRead。
//
// 为什么不直接 ExtAudioFileOpenURL：那条路实测要求文件完整到 98.3% 才肯打开，
// 而且只认有文件系统支撑的 `file://` URL（`verify-streaming.md` §1.2 / §5.4）。
// 走回调之后字节从哪来完全由我们说了算。
func Open(r io.ReadSeeker) (*Decoder, error) {
	if loadErr != nil {
		return nil, loadErr
	}

	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	src := &source{r: r, size: size}
	handle := registerSource(src)
	d := &Decoder{handle: handle, total: -1}

	if err := d.open(); err != nil {
		_ = d.Close() // 半开的句柄也得还回去
		return nil, err
	}
	return d, nil
}

// open 建好 AudioToolbox 侧的两个句柄并问清楚它是什么格式。
func (d *Decoder) open() error {
	var file uintptr
	// ⚠️ macOS 这一版是**七个**参数：读、写、取大小、设大小、类型提示、出句柄。
	// 少传一个（网上和不少绑定里的六参数版是 iOS 的）不会崩，只是 AudioFile
	// 把 inFileTypeHint 读成指针、把 outAudioFile 读成类型提示，返回 -50。
	st := audioFileOpenWithCallbacks(d.handle, readProc, 0, getSizeProc, 0, audioFileUnspecifiedType, unsafe.Pointer(&file))
	if err := statusError("AudioFileOpenWithCallbacks", st); err != nil {
		return err
	}
	d.file = file

	var ef uintptr
	st = extAudioFileWrapAudioFileID(file, false /* 只读 */, unsafe.Pointer(&ef))
	if err := statusError("ExtAudioFileWrapAudioFileID", st); err != nil {
		return err
	}
	d.ef = ef

	// 先问文件本身是什么，再照着它要我们想要的。采样率和声道数只能从这里来。
	var fileFmt audioStreamBasicDescription
	if err := d.getProperty(propFileDataFormat, unsafe.Pointer(&fileFmt), uint32(unsafe.Sizeof(fileFmt))); err != nil {
		return err
	}
	d.rate = int(fileFmt.SampleRate)
	d.ch = int(fileFmt.ChannelsPerFrame)
	if d.rate < 1 {
		return fmt.Errorf("extaf: the file declares %g Hz, which is not a sample rate", fileFmt.SampleRate)
	}
	if d.ch < 1 {
		return fmt.Errorf("extaf: the file declares %d channels, so it holds no audio", d.ch)
	}

	// 要交织 float32——MyGo 的 audio.Decoder 就是这个形状。交织是对交织，
	// 不是每个声道一个缓冲（那是非交织，多一个标志位）。
	bytesPerFrame := uint32(d.ch) * 4
	want := audioStreamBasicDescription{
		SampleRate:       fileFmt.SampleRate,
		FormatID:         audioFormatLinearPCM,
		FormatFlags:      audioFormatFlagIsFloat | audioFormatFlagIsPacked,
		BytesPerFrame:    bytesPerFrame,
		FramesPerPacket:  1,
		BytesPerPacket:   bytesPerFrame,
		ChannelsPerFrame: uint32(d.ch),
		BitsPerChannel:   32,
	}
	st = extAudioFileSetProperty(d.ef, propClientDataFormat, uint32(unsafe.Sizeof(want)), unsafe.Pointer(&want))
	if err := statusError("ExtAudioFileSetProperty(cfmt)", st); err != nil {
		return err
	}

	// 总帧数。⚠️ 这是 SInt64——用 uint32 读会静默拿到 0，时长就变成 0 了。
	var frames int64
	if err := d.getProperty(propFileLengthFrames, unsafe.Pointer(&frames), uint32(unsafe.Sizeof(frames))); err != nil {
		// 时长拿不到不算致命：Len() 返回 -1，播放照常，只是进度条没有终点。
		frames = -1
	}
	if frames < 0 {
		d.total = -1
	} else {
		d.total = frames
	}
	return nil
}

// getProperty 问 ExtAudioFile 要一个属性。out 是定长结构或标量的地址。
// readSourceErr 返回字节源身上记下的那个错（没有就是 nil）。
func (d *Decoder) readSourceErr() error {
	src := sourceFor(d.handle)
	if src == nil {
		return nil
	}
	return src.readErr()
}

func (d *Decoder) getProperty(prop uint32, out unsafe.Pointer, size uint32) error {
	n := size
	st := extAudioFileGetProperty(d.ef, prop, &n, out)
	return statusError(fmt.Sprintf("ExtAudioFileGetProperty(%#x)", prop), st)
}

func (d *Decoder) SampleRate() int { return d.rate }
func (d *Decoder) Channels() int   { return d.ch }

// Len 返回这个文件一共多少帧，拿不到时是 -1。
func (d *Decoder) Len() int64 { return d.total }

// Read 解出接下来的样本，交给 MyGo 的播放链路。返回值是**样本数**（不是帧数）。
//
// ExtAudioFileRead 要的是 AudioBufferList，不是裸缓冲区（`verify-audio-playback.md`
// §5.5）。交织格式只往里放一个 buffer——「每个声道一个 buffer」说的是非交织
// 格式，我们设客户端格式时没要那个标志位。
func (d *Decoder) Read(buf []float32) (int, error) {
	if d.done {
		return 0, errClosed
	}
	frames := len(buf) / d.ch
	if frames == 0 {
		return 0, nil
	}
	want := frames * d.ch * 4
	if cap(d.raw) < want {
		d.raw = make([]byte, want)
	}
	raw := d.raw[:want]

	list := audioBufferList{
		NumberBuffers: 1,
		Buffers: [1]audioBuffer{{
			NumberChannels: uint32(d.ch),
			ByteSize:       uint32(want),
			Data:           uintptr(unsafe.Pointer(&raw[0])),
		}},
	}
	got := uint32(frames)
	st := extAudioFileRead(d.ef, unsafe.Pointer(&got), unsafe.Pointer(&list))

	// ⚠️ 字节源真出过事的话，报**它**的错，不要报 AudioToolbox 给的东西。
	//
	// 顺序很重要，而且要放在状态码判断**前面**。AudioToolbox 在读回调里
	// 只看得到「这次要到了几个字节」，它分不出「文件到头了」和「网络断了」，
	// 于是两种情况它给的都是 0 字节 + 一个被逼出来的状态码（实测 -50）。
	// 那个状态码说给用户听没有任何意义——「ExtAudioFileRead: OSStatus -50」
	// 既不像网络问题也不像歌坏了，用户只会觉得应用自己也不知道出了什么事。
	if err := d.readSourceErr(); err != nil {
		return 0, err
	}
	if err := statusError("ExtAudioFileRead", st); err != nil {
		return 0, err
	}
	if got == 0 {
		// 一帧没要到，字节源也没出过事：这是正常的读完。
		//
		// 直接返回 io.EOF 的话，`spec.md`「播放状态机」说的
		// 「解码失败不能被当成播放结束」就又破了：一次断网表现成
		// 「歌忽然放完了」，界面上什么提示都没有。
		return 0, io.EOF
	}

	n := int(got) * d.ch
	for i := range n {
		buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return n, nil
}

// SetPosition 跳到从开头算起的第 frame 帧。
func (d *Decoder) SetPosition(frame int64) error {
	if d.done {
		return errClosed
	}
	if frame < 0 || d.total >= 0 && frame > d.total {
		return fmt.Errorf("%w: frame %d of %d", ErrSeekRange, frame, d.total)
	}
	return statusError("ExtAudioFileSeek", extAudioFileSeek(d.ef, frame))
}

// Close 放掉两个句柄和字节源的登记。可以重复调用。
//
// ⚠️ ExtAudioFileWrapAudioFileID 包出来的 ExtAudioFileRef 被 Dispose 时
// **不会**顺带关掉底下的 AudioFileID（SDK 注释里写死了），两个都得关，
// 少关一个就是换歌时漏一个文件句柄。
func (d *Decoder) Close() error {
	if d.done {
		return nil
	}
	d.done = true
	if d.ef != 0 {
		_ = extAudioFileDispose(d.ef)
		d.ef = 0
	}
	if d.file != 0 {
		_ = audioFileClose(d.file)
		d.file = 0
	}
	unregisterSource(d.handle)
	d.raw = nil
	return nil
}
