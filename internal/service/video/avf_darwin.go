//go:build darwin

package video

// AVFoundation 的 AVAssetReader 解码视频：系统硬件解码，不带 cgo、不带 ffmpeg。
//
// 一个读者 = 一条锁在 OS 线程上的 goroutine。ObjC 的自动释放池是每线程的，
// 所以这条线程每帧自己推一个池、弹一个池；对象不跨线程。

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

var (
	avfOnce sync.Once
	avfErr  error

	msgSend            uintptr
	poolPush           func() uintptr
	poolPop            func(uintptr)
	cfRelease          func(uintptr)
	cmSampleImage      func(uintptr) uintptr
	cmSamplePTS        func(uintptr) cmTime
	cvLock             func(uintptr, uint64) int32
	cvUnlock           func(uintptr, uint64) int32
	cvBase             func(uintptr) uintptr
	cvStride, cvW, cvH func(uintptr) uintptr
	keyPixFmt          uintptr
	msgCopyNext        func(obj objc.ID, sel objc.SEL) uintptr
)

func loadAVF() {
	open := func(p string) uintptr {
		h, err := purego.Dlopen(p, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil && avfErr == nil {
			avfErr = fmt.Errorf("video: %w", err)
		}
		return h
	}
	objcLib := open("/usr/lib/libobjc.A.dylib")
	open("/System/Library/Frameworks/Foundation.framework/Foundation")
	open("/System/Library/Frameworks/AVFoundation.framework/AVFoundation")
	cm := open("/System/Library/Frameworks/CoreMedia.framework/CoreMedia")
	cv := open("/System/Library/Frameworks/CoreVideo.framework/CoreVideo")
	cf := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	if avfErr != nil {
		return
	}
	sym := func(h uintptr, n string) uintptr {
		a, err := purego.Dlsym(h, n)
		if err != nil && avfErr == nil {
			avfErr = fmt.Errorf("video: %w", err)
		}
		return a
	}
	msgSend = sym(objcLib, "objc_msgSend")
	purego.RegisterLibFunc(&poolPush, objcLib, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&poolPop, objcLib, "objc_autoreleasePoolPop")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&cmSampleImage, cm, "CMSampleBufferGetImageBuffer")
	purego.RegisterLibFunc(&cmSamplePTS, cm, "CMSampleBufferGetPresentationTimeStamp")
	purego.RegisterLibFunc(&cvLock, cv, "CVPixelBufferLockBaseAddress")
	purego.RegisterLibFunc(&cvUnlock, cv, "CVPixelBufferUnlockBaseAddress")
	purego.RegisterLibFunc(&cvBase, cv, "CVPixelBufferGetBaseAddress")
	purego.RegisterLibFunc(&cvStride, cv, "CVPixelBufferGetBytesPerRow")
	purego.RegisterLibFunc(&cvW, cv, "CVPixelBufferGetWidth")
	purego.RegisterLibFunc(&cvH, cv, "CVPixelBufferGetHeight")
	if avfErr != nil {
		return
	}
	keyPixFmt = *(*uintptr)(ptr(sym(cv, "kCVPixelBufferPixelFormatTypeKey")))
	purego.RegisterFunc(&msgCopyNext, msgSend)
}

func send(o uintptr, sel string, args ...uintptr) uintptr {
	var a [8]uintptr
	a[0], a[1] = o, uintptr(objc.RegisterName(sel))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSend, a[:n+2]...)
	return r
}

func cls(n string) uintptr { return uintptr(objc.GetClass(n)) }

func nsString(s string) uintptr {
	b := append([]byte(s), 0)
	r := send(cls("NSString"), "stringWithUTF8String:", uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	return r
}

func nsDict(kv map[uintptr]uintptr) uintptr {
	keys := make([]uintptr, 0, len(kv))
	vals := make([]uintptr, 0, len(kv))
	for k, v := range kv {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	r := send(cls("NSDictionary"), "dictionaryWithObjects:forKeys:count:",
		uintptr(unsafe.Pointer(&vals[0])), uintptr(unsafe.Pointer(&keys[0])), uintptr(len(keys)))
	runtime.KeepAlive(keys)
	runtime.KeepAlive(vals)
	return r
}

// Open 从本地 fMP4 文件解码。像素缓冲都从 pool 借、用完由调用方 Recycle 交回，
// 解码器自己不再分配不记账的缓冲。
//
// ⚠️ AVAssetReader 会把时间轴重新对到这个文件的第一个分片上：帧的 PTS 是
// 相对本文件的，调用方要自己按 sidx 把它搬回流上的绝对时间。
func Open(path string, pool BufferPool) (*Source, error) {
	avfOnce.Do(loadAVF)
	if avfErr != nil {
		return nil, avfErr
	}
	s := newSource(pool)
	ready := make(chan error, 1)
	go s.run(path, ready)
	if err := <-ready; err != nil {
		<-s.done
		return nil, err
	}
	return s, nil
}

func (s *Source) run(path string, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)
	defer close(s.frames)

	pool := poolPush()
	nsurl := send(cls("NSURL"), "fileURLWithPath:", nsString(path))
	asset := send(send(cls("AVURLAsset"), "alloc"), "initWithURL:options:", nsurl, 0)
	var nserr uintptr
	reader := send(send(cls("AVAssetReader"), "alloc"), "initWithAsset:error:", asset, uintptr(unsafe.Pointer(&nserr)))
	tracks := send(asset, "tracksWithMediaType:", nsString("vide"))
	if reader == 0 || send(tracks, "count") == 0 {
		ready <- fmt.Errorf("video: 打不开视频轨 (%s)", describe(nserr))
		poolPop(pool)
		return
	}
	track := send(tracks, "objectAtIndex:", 0)
	settings := nsDict(map[uintptr]uintptr{keyPixFmt: send(cls("NSNumber"), "numberWithInt:", 0x42475241)})
	out := send(send(cls("AVAssetReaderTrackOutput"), "alloc"), "initWithTrack:outputSettings:", track, settings)
	send(reader, "addOutput:", out)
	if send(reader, "startReading")&0xff == 0 {
		ready <- fmt.Errorf("video: 读取失败 (%s)", describe(send(reader, "error")))
		poolPop(pool)
		return
	}
	ready <- nil
	poolPop(pool)
	defer send(reader, "release")
	defer send(out, "release")
	defer send(asset, "release")
	defer send(reader, "cancelReading")

	for {
		select {
		case <-s.quit:
			return
		default:
		}
		p := poolPush()
		sb := msgCopyNext(objc.ID(out), objc.RegisterName("copyNextSampleBuffer"))
		if sb == 0 {
			if send(reader, "status") == 3 {
				s.fail(fmt.Errorf("video: %s", describe(send(reader, "error"))))
			}
			poolPop(p)
			return
		}
		t := cmSamplePTS(sb)
		pts := time.Duration(0)
		if t.Timescale > 0 {
			pts = time.Duration(t.Value) * time.Second / time.Duration(t.Timescale)
		}
		f, ok := s.copyFrame(sb, pts)
		cfRelease(sb)
		poolPop(p)
		if !ok {
			if err := s.Err(); err != nil {
				// 借不到缓冲之类的硬错误：不继续按帧失败重试，直接收摊，
				// 让 Session.Err 把原因交给播放器。
				return
			}
			continue
		}
		select {
		case s.frames <- f:
		case <-s.quit:
			// 退出时这一帧已经借了缓冲，必须还回池里，否则 gauge 永远降不下来。
			s.Recycle(f.Pix)
			return
		}
	}
}

func (s *Source) copyFrame(sb uintptr, pts time.Duration) (Frame, bool) {
	pb := cmSampleImage(sb)
	if pb == 0 || cvLock(pb, 1) != 0 {
		return Frame{}, false
	}
	defer cvUnlock(pb, 1)
	w, h, stride := int(cvW(pb)), int(cvH(pb)), int(cvStride(pb))
	base := cvBase(pb)
	if base == 0 {
		return Frame{}, false
	}
	n := int64(w) * int64(h) * 4
	// 借缓冲走帧池的字节预算：预算紧的时候这里等显示端交回上一帧（背压），
	// 取消时拿 ErrFramePoolStopped 退出；一帧大到预算放不下两帧时拿
	// ErrFramePoolBudget，由 Source.fail 交给 Session.Err。
	buf, err := s.acquire(n)
	if err != nil {
		s.fail(err)
		return Frame{}, false
	}
	src := unsafe.Slice((*byte)(ptr(base)), stride*h)
	for y := 0; y < h; y++ {
		row, dst := src[y*stride:y*stride+w*4], buf[y*w*4:(y+1)*w*4]
		for x := 0; x < len(row); x += 4 {
			dst[x], dst[x+1], dst[x+2], dst[x+3] = row[x+2], row[x+1], row[x], row[x+3]
		}
	}
	return Frame{PTS: pts, W: w, H: h, Pix: buf}, true
}

func describe(e uintptr) string {
	if e == 0 {
		return "unknown"
	}
	d := send(e, "localizedDescription")
	if d == 0 {
		return "unknown"
	}
	return goString(send(d, "UTF8String"))
}

func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var n int
	for *(*byte)(ptr(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr(p)), n))
}

// ptr 把系统返回的地址变回指针。这些地址在 Go 堆外（框架全局变量、CVPixelBuffer
// 的像素内存），GC 不管它们，所以这样转是安全的；绕一道是为了不让 vet 误报。
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }
