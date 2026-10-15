package flacdec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mewkiz/flac/frame"
	m4a "github.com/tphakala/go-m4a"
)

// mp4Source reads FLAC carried in an ISO-BMFF file: an fLaC audio sample entry
// whose dfLa FLACSpecificBox holds the STREAMINFO, with one FLAC frame per MP4
// access unit. This is the shape Bilibili's FLAC audio streams have, and
// go-m4a is what finds the frames, the sample rate and the edit list.
type mp4Source struct {
	rd   *m4a.Reader
	info m4a.Info
	au   []byte // the access unit of the frame being parsed
}

func openMP4(r io.ReadSeeker) (*Decoder, error) {
	rd, err := m4a.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("flacdec: %w", err)
	}
	info := rd.Info()
	if info.Codec != m4a.CodecFLAC {
		return nil, fmt.Errorf("%w: ISO-BMFF track is %v", ErrFormat, info.Codec)
	}
	// The bits per sample ride in the STREAMINFO block go-m4a hands back, not in
	// the sample entry: the sample entry's 16.16 rate field is reduced and cannot
	// carry the rate either, which is why the container is not the authority on
	// either field for a FLAC track.
	bits, err := streamInfoBits(info.CodecConfig)
	if err != nil {
		return nil, fmt.Errorf("flacdec: %w", err)
	}
	if info.SampleRate <= 0 || info.Channels <= 0 {
		return nil, fmt.Errorf("flacdec: ISO-BMFF FLAC track of %d channels at %d Hz",
			info.Channels, info.SampleRate)
	}
	s := &mp4Source{rd: rd, info: info}
	d := &Decoder{
		src:   s,
		ch:    info.Channels,
		rate:  info.SampleRate,
		bits:  bits,
		scale: sampleScale(bits),
		total: presentedFrames(info.Duration, info.SampleRate),
	}
	// A container's priming is not part of the presented audio, and SeekTime's
	// timeline already starts past it, so it is dropped on the way out of the
	// first frame and nowhere else.
	d.skip = int(info.EncoderDelay)
	return d, nil
}

// streamInfoBits reads bits per sample out of the 34-byte STREAMINFO block body
// go-m4a returns in Info().CodecConfig.
//
// The field is 5 bits wide and spans a byte boundary, which is what makes it
// worth spelling out. Reading from the end, the block is: 16 bits minimum block
// size, 16 bits maximum, 24 bits minimum frame size, 24 bits maximum frame size
// (80 bits, ending at the end of byte 9), then 20 bits sample rate (bits 80..99,
// so bytes 10, 11 and the top nibble of byte 12), 3 bits channel count minus one
// (bits 100..102, byte 12 bits 3..1), 5 bits sample size minus one (bits
// 103..107, byte 12 bit 0 and byte 13 bits 7..4), 36 bits total samples (bits
// 108..143) and the 128-bit MD5 of the unencoded audio.
func streamInfoBits(cfg []byte) (int, error) {
	if len(cfg) < 14 {
		return 0, fmt.Errorf("%w: STREAMINFO block is %d bytes, want 34", ErrFormat, len(cfg))
	}
	// Bits 103..107 hold bits-per-sample minus one: byte 12's lowest bit is its
	// most significant half and byte 13's top nibble is the rest.
	return (int(cfg[12]&1)<<4 | int(cfg[13])>>4) + 1, nil
}

// nextFrame returns the next FLAC frame: go-m4a hands over one access unit and
// mewkiz/flac decodes it. ReadFrameInto is the zero-allocation form; a frame
// larger than the buffer read so far grows it once.
func (s *mp4Source) nextFrame() (*frame.Frame, int, error) {
	n, err := s.rd.ReadFrameInto(s.au)
	if errors.Is(err, io.ErrShortBuffer) {
		s.au = make([]byte, n)
		if n, err = s.rd.ReadFrameInto(s.au); err != nil {
			return nil, 0, err
		}
	} else if err != nil {
		return nil, 0, err
	}
	// frame.Parse leaves the reader it is given exactly after the frame, so the
	// access unit can be parsed in place, and the frame's CRC-16 is checked on the
	// way: a truncated or damaged access unit is an error, not silence.
	f, err := frame.Parse(bytes.NewReader(s.au[:n]))
	if err != nil {
		if err == io.EOF {
			return nil, 0, io.EOF
		}
		return nil, 0, fmt.Errorf("flacdec: access unit of %d bytes: %w", n, err)
	}
	if len(f.Subframes) == 0 {
		return nil, 0, fmt.Errorf("flacdec: access unit of %d bytes holds no frame", n)
	}
	return f, len(f.Subframes[0].Samples), nil
}

// seekTo moves the container to the access unit holding sample and reports how
// many samples at its start to drop.
//
// It goes through SeekTime rather than SeekSample because MP4 addresses an
// access unit, not a sample, and the edit list decides which access unit a
// presentation time belongs to. The sample table makes that O(1): no audio data
// is read, so on a download still in progress the cost lands on the next
// ReadFrame, where a blocking reader waits only for the bytes it needs.
func (s *mp4Source) seekTo(sample int64) (int, error) {
	if sample < 0 {
		return 0, fmt.Errorf("flacdec: frame %d: %w", sample, ErrSeekRange)
	}
	d := time.Duration(sample) * time.Second / time.Duration(s.info.SampleRate)
	_, skip, err := s.rd.SeekTime(d)
	if err != nil {
		return 0, fmt.Errorf("flacdec: seek to frame %d (%v): %w", sample, d, err)
	}
	return int(skip), nil
}

func (s *mp4Source) close() error { return nil }
