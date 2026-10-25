package video

import (
	"encoding/binary"
	"errors"
	"time"
)

// Segment 是 fMP4 里一个可以单独解码的片段（sidx 的一项，起点是关键帧）。
type Segment struct {
	Offset, Size int64
	Start, Dur   time.Duration
}

// Index 是一条 DASH 视频流的目录：初始化段（ftyp+moov）加上 sidx 列出的片段。
type Index struct {
	Init     []byte
	Segments []Segment
}

// Duration 是整条流的时长。
func (x *Index) Duration() time.Duration {
	if n := len(x.Segments); n > 0 {
		return x.Segments[n-1].Start + x.Segments[n-1].Dur
	}
	return 0
}

// At 返回包含 t 的那个片段的下标。
func (x *Index) At(t time.Duration) int {
	for i := len(x.Segments) - 1; i >= 0; i-- {
		if x.Segments[i].Start <= t {
			return i
		}
	}
	return 0
}

var errNeedMore = errors.New("video: 索引还没读全")

// ParseIndex 从流开头的字节里找 ftyp / moov / sidx。字节不够时返回 errNeedMore，
// 调用方多取一些再来。
func ParseIndex(b []byte) (*Index, error) {
	var init []byte
	pos := 0
	for pos+8 <= len(b) {
		size := int64(binary.BigEndian.Uint32(b[pos:]))
		typ := string(b[pos+4 : pos+8])
		hdr := 8
		if size == 1 {
			if pos+16 > len(b) {
				return nil, errNeedMore
			}
			size = int64(binary.BigEndian.Uint64(b[pos+8:]))
			hdr = 16
		}
		if size < int64(hdr) {
			return nil, errors.New("video: 盒子大小不对")
		}
		end := int64(pos) + size
		switch typ {
		case "ftyp", "moov":
			if end > int64(len(b)) {
				return nil, errNeedMore
			}
			init = append(init, b[pos:end]...)
		case "sidx":
			if end > int64(len(b)) {
				return nil, errNeedMore
			}
			if init == nil {
				return nil, errors.New("video: sidx 前面没有 moov")
			}
			segs, err := parseSidx(b[pos+hdr:end], end)
			if err != nil {
				return nil, err
			}
			return &Index{Init: init, Segments: segs}, nil
		case "moof", "mdat":
			return nil, errors.New("video: 没有 sidx，没法按片段定位")
		}
		pos = int(end)
	}
	return nil, errNeedMore
}

func parseSidx(b []byte, end int64) ([]Segment, error) {
	if len(b) < 12 {
		return nil, errors.New("video: sidx 太短")
	}
	ver := b[0]
	ts := int64(binary.BigEndian.Uint32(b[8:]))
	if ts == 0 {
		return nil, errors.New("video: sidx timescale 为 0")
	}
	p := 12
	var first, off int64
	if ver == 0 {
		if len(b) < p+8 {
			return nil, errors.New("video: sidx 太短")
		}
		first = int64(binary.BigEndian.Uint32(b[p:]))
		off = int64(binary.BigEndian.Uint32(b[p+4:]))
		p += 8
	} else {
		if len(b) < p+16 {
			return nil, errors.New("video: sidx 太短")
		}
		first = int64(binary.BigEndian.Uint64(b[p:]))
		off = int64(binary.BigEndian.Uint64(b[p+8:]))
		p += 16
	}
	p += 2
	if len(b) < p+2 {
		return nil, errors.New("video: sidx 太短")
	}
	n := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	if len(b) < p+12*n {
		return nil, errors.New("video: sidx 项不全")
	}
	segs := make([]Segment, 0, n)
	at := end + off
	t := first
	for i := 0; i < n; i++ {
		r := binary.BigEndian.Uint32(b[p:])
		d := int64(binary.BigEndian.Uint32(b[p+4:]))
		p += 12
		if r>>31 != 0 {
			return nil, errors.New("video: 不支持分层 sidx")
		}
		size := int64(r & 0x7fffffff)
		segs = append(segs, Segment{
			Offset: at, Size: size,
			Start: time.Duration(t) * time.Second / time.Duration(ts),
			Dur:   time.Duration(d) * time.Second / time.Duration(ts),
		})
		at += size
		t += d
	}
	return segs, nil
}
